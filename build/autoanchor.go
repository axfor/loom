package build

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
	"github.com/axfor/loom/lang"
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
	ref      lang.Ref
}

// findGaps finds our sections that a template doesn't weave into the product, and infers where they go.
func findGaps(c *lang.Config, t *lang.Template) ([]anchorGap, error) {
	if t.Type != "markdown" || c.Weft == "" || (t.From != "" && t.From != c.Warp) {
		return nil, nil
	}
	for _, s := range t.Stmts {
		if s.Op == "merge" {
			return nil, nil
		}
	}
	src, ok, err := c.Read(c.Weft, weftRel(c, t, c.Weft, t.Target))
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
	refs := map[int]lang.Ref{}
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
				span, _, err := locate(tree, "heading", r.Within, r.Anchor, r.Ident, lang.Stmt{Rng: r.Rng})
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
		return lang.Quote(n.Name), nil
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
		var within []lang.Seg
		for d := depth - 1; d >= 0; d-- {
			within = append(within, lang.Seg{Name: ancestors[d].Name})
		}
		if span, _, err := locate(tree, "heading", within, n.Name, false, lang.Stmt{}); err == nil && span[0] == n.Line {
			parts := []string{"self"}
			for _, w := range within {
				parts = append(parts, lang.Quote(w.Name))
			}
			return strings.Join(append(parts, lang.Quote(n.Name)), "."), nil
		}
	}
	return "", fmt.Errorf("%q appears %d times in our file and parent headings can't tell them apart — can't infer an anchor; say where it goes", n.Name, same)
}

func isWeft(c *lang.Config, layer string) bool { return layer == c.Weft }

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
	toks, err := lang.Lex(path, src)
	if err != nil {
		return nil, nil, err
	}
	type insert struct {
		at, end int // replace src[at:end] with text (end == at: a plain insertion)
		text    string
	}
	var ins []insert
	var lines []string

	// Gaps whose neighbour sits in the same (...) call are rewritten together, so the call is
	// rendered once: on one line, or as a block with one argument per line.
	type call struct {
		open, close int // token indexes of ( and )
		gaps        []anchorGap
	}
	calls := map[int]*call{}
	var order []int

	for _, g := range gaps {
		k := -1
		for i, t := range toks {
			if t.Pos.Line == g.ref.Rng.Line && t.Pos.Col == g.ref.Rng.Col && t.Kind != lang.KNewline {
				k = i
				break
			}
		}
		if k < 0 {
			return nil, nil, fmt.Errorf("%s: can't find the %q reference in the template", g.ref.Rng, g.neighbor)
		}
		where := "after"
		if !g.after {
			where = "before"
		}
		for _, n := range g.labels {
			lines = append(lines, fmt.Sprintf("%q placed %s %q (line %d)", n, where, g.neighbor, g.ref.Rng.Line))
		}

		if open, close := enclosingParens(toks, k); open >= 0 {
			if calls[open] == nil {
				calls[open] = &call{open: open, close: close}
				order = append(order, open)
			}
			calls[open].gaps = append(calls[open].gaps, g)
			continue
		}

		// Block form already: one argument per line, next to the neighbour's line
		last := argEnd(toks, k)
		start, end := toks[k].Off, toks[last].End
		lineStart := strings.LastIndexByte(string(src[:start]), '\n') + 1
		indent := string(src[lineStart:start])
		if g.after {
			lineEnd := len(src)
			if i := strings.IndexByte(string(src[end:]), '\n'); i >= 0 {
				lineEnd = end + i
			}
			ins = append(ins, insert{lineEnd, lineEnd, "\n" + indent + strings.Join(g.args, "\n"+indent)})
		} else {
			ins = append(ins, insert{start, start, strings.Join(g.args, "\n"+indent) + "\n" + indent})
		}
	}

	for _, open := range order {
		cl := calls[open]
		// the call's current arguments, as written
		type arg struct {
			first int
			text  string
		}
		var args []arg
		first, depth := -1, 0
		for i := cl.open + 1; i <= cl.close; i++ {
			t := toks[i]
			if t.Kind == lang.KNewline {
				continue
			}
			if i == cl.close || (depth == 0 && t.Kind == lang.KComma) {
				if first >= 0 {
					args = append(args, arg{first, string(src[toks[first].Off:toks[i-1-trailingNewlines(toks, i-1)].End])})
				}
				first = -1
				continue
			}
			if first < 0 {
				first = i
			}
			switch t.Kind {
			case lang.KLParen:
				depth++
			case lang.KRParen:
				depth--
			}
		}
		var out []string
		path := false
		for _, a := range args {
			var before, after []string
			for _, g := range cl.gaps {
				if toks[a.first].Pos != g.ref.Rng {
					continue
				}
				for _, na := range g.args {
					path = path || strings.HasPrefix(na, "self.")
				}
				if g.after {
					after = append(after, g.args...)
				} else {
					before = append(before, g.args...)
				}
			}
			out = append(append(append(out, before...), a.text), after...)
		}

		openOff, closeEnd := toks[cl.open].Off, toks[cl.close].End
		lineStart := strings.LastIndexByte(string(src[:openOff]), '\n') + 1
		prefix := string(src[lineStart:openOff])
		indent := prefix[:len(prefix)-len(strings.TrimLeft(prefix, " \t"))]
		oneLine := "(" + strings.Join(out, ", ") + ")"
		// A section path is easy to misread inside a long line ("..."."..." looks like a stray quote),
		// and so is a long argument list: both are written as a block, one argument per line.
		if path || len([]rune(prefix+oneLine)) > maxLine {
			ins = append(ins, insert{openOff, closeEnd, "{\n" + indent + "    " + strings.Join(out, "\n"+indent+"    ") + "\n" + indent + "}"})
		} else {
			ins = append(ins, insert{openOff, closeEnd, oneLine})
		}
	}

	sort.SliceStable(ins, func(i, j int) bool { return ins[i].at > ins[j].at })
	res := append([]byte{}, src...)
	for _, in := range ins {
		res = append(res[:in.at], append([]byte(in.text), res[in.end:]...)...)
	}
	return res, lines, nil
}

// maxLine is the longest statement anchor completion writes on one line.
const maxLine = 100

// enclosingParens finds the ( ) call that directly contains token k, or -1 when k is not inside
// parentheses (it is in a { } block).
func enclosingParens(toks []lang.Tok, k int) (int, int) {
	open, depth := -1, 0
	for i := k - 1; i >= 0; i-- {
		switch toks[i].Kind {
		case lang.KRParen:
			depth++
		case lang.KLParen:
			if depth == 0 {
				open = i
			} else {
				depth--
			}
		case lang.KLBrace, lang.KRBrace:
			if depth == 0 {
				return -1, -1
			}
		}
		if open >= 0 {
			break
		}
	}
	if open < 0 {
		return -1, -1
	}
	depth = 0
	for i := open + 1; i < len(toks); i++ {
		switch toks[i].Kind {
		case lang.KLParen:
			depth++
		case lang.KRParen:
			if depth == 0 {
				return open, i
			}
			depth--
		}
	}
	return -1, -1
}

// argEnd is the last token of the argument starting at token k: a string is one token;
// self.X or self."A"."B" runs to the end of the expression.
func argEnd(toks []lang.Tok, k int) int {
	last := k
	if toks[k].Kind == lang.KIdent {
		for last+2 < len(toks) && toks[last+1].Kind == lang.KDot && (toks[last+2].Kind == lang.KIdent || toks[last+2].Kind == lang.KString) {
			last += 2
		}
	}
	return last
}

// trailingNewlines counts newline tokens ending at index i (going backwards).
func trailingNewlines(toks []lang.Tok, i int) int {
	n := 0
	for i-n >= 0 && toks[i-n].Kind == lang.KNewline {
		n++
	}
	return n
}

// completeAnchors completes the anchors of one template. With write false it only reports (lm check):
// even an anchor that can be inferred is an error, because template and product disagree and check
// doesn't modify files. It returns the completed template.
func completeAnchors(c *lang.Config, t *lang.Template, write bool, r *Report) (*lang.Template, error) {
	src, err := os.ReadFile(t.Path)
	if err != nil {
		return nil, err
	}
	if keys := frontmatterGaps(c, t); len(keys) > 0 {
		rel := lang.Rel(c, t.Path)
		if !write {
			return t, fmt.Errorf("%s: our frontmatter %s has no statement — lm build writes one, since loom.om says `frontmatter %s`",
				t.Path, quoteAll(keys), c.Frontmatter)
		}
		var add []string
		for _, k := range keys {
			add = append(add, fmt.Sprintf("base.frontmatter.%s.%s(self.frontmatter.%s)",
				lang.NameText(k), c.Frontmatter, lang.NameText(k)))
			r.Anchored = append(r.Anchored, ReportLine{rel,
				fmt.Sprintf("frontmatter %s written (loom.om says %s)", k, c.Frontmatter)})
		}
		src = append([]byte(strings.Join(add, "\n")+"\n"), src...)
		if err := os.WriteFile(t.Path, src, 0o644); err != nil {
			return nil, err
		}
		if t, err = lang.LoadTemplate(c, t.Path); err != nil {
			return nil, err
		}
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
	return lang.LoadTemplate(c, t.Path)
}

// frontmatterGaps lists the keys of our frontmatter that no statement accounts for, when the tree
// has said what to do with them. Without that setting there is nothing to write: whether our value
// replaces upstream's or goes before it changes what the product says, and the compiler does not
// decide that on anyone's behalf — the build fails with both spellings instead, as it always has.
func frontmatterGaps(c *lang.Config, t *lang.Template) []string {
	if c.Frontmatter == "" || t.Type != "markdown" || c.Weft == "" || (t.From != "" && t.From != c.Warp) {
		return nil
	}
	for _, s := range t.Stmts {
		if s.Op == "merge" || s.Op == "frontmatter" {
			return nil
		}
	}
	ours, ok, err := c.Read(c.Weft, weftRel(c, t, c.Weft, t.Target))
	if err != nil || !ok {
		return nil
	}
	fm, ok := ast.NewMarkdown(ours).BodyOf("frontmatter")
	if !ok {
		return nil
	}
	up, upOK, _ := c.Read(c.Warp, t.BasePath)
	if !upOK {
		return nil
	}
	upTree := ast.NewMarkdown(up)
	done := map[string]bool{}
	for _, s := range t.Stmts {
		if s.Op == "value" {
			done[s.SetKey] = true
		}
	}
	var out []string
	for _, n := range ast.Addressable(ast.New("yaml", fm), "key") {
		if done[n.Name] || strings.Contains(n.Name, ".") {
			continue
		}
		// A key upstream does not have is simply added by the product; there is nothing to
		// reconcile and nothing for the setting to decide.
		if _, has := upTree.BodyOf("frontmatter"); !has {
			continue
		}
		if fmUp, ok := upTree.BodyOf("frontmatter"); ok {
			if _, there := ast.New("yaml", fmUp).BodyOf(n.Name); !there {
				continue
			}
		}
		out = append(out, n.Name)
	}
	return out
}
