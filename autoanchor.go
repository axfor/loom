package loom

// Anchor completion: our layer's file has a section the template doesn't place. Its place is
// inferred from its position in our file and written back to the template.
//
// There is one rule: follow the neighbor. The nearest preceding section that is already woven in
// sits at some position in some template statement; the new section goes right after it (same
// statement, same anchor). With no preceding one, it goes before the nearest following section.
// So in the product its order relative to its neighbors matches our file. A section with no
// neighbor at all can't be placed: that is a build error, never a guess.
//
// Why automate it: a section runs from its heading to the next heading, and subsections are nodes
// of their own. If the template only names `## Install`, a `### Option 1` newly written under
// `## Install` in our file never reaches the product. The product looks perfectly normal, just
// missing a piece, with no error at all.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/axfor/loom/ast"
)

// anchorGap is one anchor to complete: the sections in args go next to the neighbor reference.
type anchorGap struct {
	args     []string // arguments written into the template: "name", or self."parent"."name" when the name repeats
	labels   []string // what the report calls them
	neighbor string
	after    bool // true = after neighbor; false = before neighbor
	ref      Ref
}

// findGaps finds our sections that a template doesn't weave into the product, and infers where they go.
func findGaps(c *Config, t *Template) ([]anchorGap, error) {
	if t.Type != "markdown" || c.Weft == "" || (t.From != "" && t.From != c.Warp && t.From != "up") {
		return nil, nil
	}
	for _, s := range t.Stmts {
		if s.Op == "patch" {
			return nil, nil
		}
	}
	src, ok, err := c.read(c.Weft, weftRel(c, t, c.Weft, t.Target))
	if err != nil || !ok {
		return nil, err
	}
	tree := ast.NewMarkdown(src)
	nodes := ast.Addressable(tree, "heading")
	if len(nodes) == 0 {
		return nil, nil
	}

	// Sections the template takes from self, keyed by position in our file (several sections may share a name).
	// References in nested views use other coordinates and don't count. Nothing can follow a replace: it accepts only one content.
	refs := map[int]Ref{}
	replaced := map[int]bool{}
	for _, s := range t.Stmts {
		switch s.Op {
		case "after", "before", "replace", "append", "prepend":
		default:
			continue
		}
		for _, r := range s.Srcs {
			if r.IsLit || r.File != "" || !isWeft(c, r.Layer) {
				continue
			}
			switch r.Kind {
			case "body", "all":
				return nil, nil // the whole file is woven in
			case "heading":
				span, _, err := locate(tree, "heading", r.Within, r.Anchor, r.Ident, Stmt{Rng: r.Rng})
				if err != nil {
					return nil, nil // weaving reports this error
				}
				if _, seen := refs[span[0]]; !seen {
					refs[span[0]] = r
					replaced[span[0]] = s.Op == "replace"
				}
			}
		}
	}

	var gaps []anchorGap
	var missing []string
	for k := 0; k < len(nodes); k++ {
		if _, ok := refs[nodes[k].Line]; ok {
			continue
		}
		// A run of consecutive unwoven sections follows the same neighbor together
		j := k
		for j < len(nodes) {
			if _, ok := refs[nodes[j].Line]; ok {
				break
			}
			j++
		}
		var labels, args []string
		for i := k; i < j; i++ {
			a, err := argFor(tree, nodes, i)
			if err != nil {
				return nil, fmt.Errorf("%s: %v", t.Path, err)
			}
			labels, args = append(labels, nodes[i].Name), append(args, a)
		}
		switch {
		case k > 0 && !replaced[nodes[k-1].Line]:
			prev := nodes[k-1]
			gaps = append(gaps, anchorGap{args, labels, prev.Name, true, refs[prev.Line]})
		case k == 0 && j < len(nodes) && !replaced[nodes[j].Line]:
			next := nodes[j]
			gaps = append(gaps, anchorGap{args, labels, next.Name, false, refs[next.Line]})
		default:
			missing = append(missing, labels...)
		}
		k = j
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%s: in our layer's %s, %d sections are not woven into the product (%s), and the template has no section for them to follow — "+
			"can't infer an anchor; say where they go: base.<upstream section>.after(\"...\")",
			t.Path, t.Target, len(missing), quoteAll(missing))
	}
	return gaps, nil
}

// argFor is the template argument for section k. A name unique in the file is written as "name";
// a repeated one gets parent headings added until it resolves uniquely: self."parent"."name".
// What is written back must resolve to the same section, never to a guess.
func argFor(tree ast.Tree, nodes []ast.Named, k int) (string, error) {
	n := nodes[k]
	same := 0
	for _, m := range nodes {
		if m.Name == n.Name {
			same++
		}
	}
	if same == 1 {
		return quote(n.Name), nil
	}
	var ancestors []ast.Named // nearest first
	level := n.Level
	for i := k - 1; i >= 0; i-- {
		if nodes[i].Level < level {
			ancestors = append(ancestors, nodes[i])
			level = nodes[i].Level
		}
	}
	for depth := 1; depth <= len(ancestors); depth++ {
		var within []Seg
		for d := depth - 1; d >= 0; d-- {
			within = append(within, Seg{Name: ancestors[d].Name})
		}
		if span, _, err := locate(tree, "heading", within, n.Name, false, Stmt{}); err == nil && span[0] == n.Line {
			parts := []string{"self"}
			for _, w := range within {
				parts = append(parts, quote(w.Name))
			}
			return strings.Join(append(parts, quote(n.Name)), "."), nil
		}
	}
	return "", fmt.Errorf("%q appears %d times in our file and parent headings can't tell them apart — can't infer an anchor; say where it goes", n.Name, same)
}

func isWeft(c *Config, layer string) bool { return layer == c.Weft || layer == "me" }

func quoteAll(names []string) string {
	var q []string
	for _, n := range names {
		q = append(q, fmt.Sprintf("%q", n))
	}
	return strings.Join(q, ", ")
}

// fillGaps writes the inferred placements into the template source by inserting new arguments next
// to the neighbor's argument: `, "name"` in a single-line call, a `"name"` line in a multi-line one.
// It returns the new source and the report lines.
func fillGaps(path string, src []byte, gaps []anchorGap) ([]byte, []string, error) {
	toks, err := lexLoom(path, src)
	if err != nil {
		return nil, nil, err
	}
	type insert struct {
		at   int
		text string
	}
	var ins []insert
	var lines []string
	for _, g := range gaps {
		k := -1
		for i, t := range toks {
			if t.pos.Line == g.ref.Rng.Line && t.pos.Col == g.ref.Rng.Col && t.kind != kNewline {
				k = i
				break
			}
		}
		if k < 0 {
			return nil, nil, fmt.Errorf("%s: can't find the %q reference in the template", g.ref.Rng, g.neighbor)
		}
		// Where this argument ends: a string is one token; self.X runs to the end of the expression
		last := k
		if toks[k].kind == kIdent {
			for last+2 < len(toks) && toks[last+1].kind == kDot && (toks[last+2].kind == kIdent || toks[last+2].kind == kString) {
				last += 2
			}
		}
		start, end := toks[k].off, toks[last].end
		next := last + 1
		for next < len(toks) && toks[next].kind == kNewline {
			next++
		}
		paren := next < len(toks) && (toks[next].kind == kComma || toks[next].kind == kRParen)

		quoted := g.args
		lineStart := strings.LastIndexByte(string(src[:start]), '\n') + 1
		indent := string(src[lineStart:start])
		switch {
		case paren && g.after:
			ins = append(ins, insert{end, ", " + strings.Join(quoted, ", ")})
		case paren:
			ins = append(ins, insert{start, strings.Join(quoted, ", ") + ", "})
		case g.after:
			lineEnd := len(src)
			if i := strings.IndexByte(string(src[end:]), '\n'); i >= 0 {
				lineEnd = end + i
			}
			ins = append(ins, insert{lineEnd, "\n" + indent + strings.Join(quoted, "\n"+indent)})
		default:
			ins = append(ins, insert{start, strings.Join(quoted, "\n"+indent) + "\n" + indent})
		}
		where := "after"
		if !g.after {
			where = "before"
		}
		for _, n := range g.labels {
			lines = append(lines, fmt.Sprintf("%q placed %s %q (line %d)", n, where, g.neighbor, g.ref.Rng.Line))
		}
	}
	sort.SliceStable(ins, func(i, j int) bool { return ins[i].at > ins[j].at })
	out := append([]byte{}, src...)
	for _, in := range ins {
		out = append(out[:in.at], append([]byte(in.text), out[in.at:]...)...)
	}
	return out, lines, nil
}

// completeAnchors completes the anchors of one template. With write false it only reports (lm check):
// even an anchor that can be inferred is an error, because template and product disagree and check
// doesn't modify files. It returns the completed template.
func completeAnchors(c *Config, t *Template, write bool, r *Report) (*Template, error) {
	src, err := os.ReadFile(t.Path)
	if err != nil {
		return nil, err
	}
	gaps, err := findGaps(c, t)
	if err != nil || len(gaps) == 0 {
		return t, err
	}
	out, lines, err := fillGaps(t.Path, src, gaps)
	if err != nil {
		return nil, err
	}
	rel := t.Path
	if rp, err := filepath.Rel(c.Root, t.Path); err == nil {
		rel = rp
	}
	if !write {
		return t, fmt.Errorf("%s: %d sections in our layer are not woven into the product: %s — lm build completes the anchors and writes them back to the template",
			t.Path, len(lines), strings.Join(lines, "; "))
	}
	if err := os.WriteFile(t.Path, out, 0o644); err != nil {
		return nil, err
	}
	for _, l := range lines {
		r.Anchored = append(r.Anchored, ReportLine{rel, l})
	}
	return LoadTemplate(c, t.Path)
}
