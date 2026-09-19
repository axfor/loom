package build

// Weaving: take the warp copy as the base and thread the weft's content into it, statement by statement.
//
// Errors must be loud. An anchor that is not found, matches several places, or uses the wrong node kind
// always fails with a position and makes the caller exit non-zero. A product that silently threads into
// the wrong section looks perfectly normal — exactly the shape this language exists to eliminate.

import (
	"fmt"
	"github.com/axfor/loom/lang"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/axfor/loom/ast"
)

// reHeading splits a heading into its # markers and the rest.
var reHeading = regexp.MustCompile(`^(#{1,6})(\s.*)$`)

type edit struct {
	s, e int
	repl []string
	idx  int
}

// Weave weaves one template and returns the product content.
func Weave(c *lang.Config, t *lang.Template) (string, error) {
	from := t.From
	if from == "" {
		from = c.Warp
	}

	// merge: our file is upstream plus our edits, so it is the product. lm sync carries the edits onto
	// each new upstream, and leaves conflict markers where it can't; those must not reach the product.
	for _, s := range t.Stmts {
		if s.Op != "merge" {
			continue
		}
		if _, ok, err := c.Read(from, weftRel(c, t, from, t.BasePath)); err != nil || !ok {
			return "", fmt.Errorf("%s: layer %s has no %s to merge into", s.Rng, from, t.BasePath)
		}
		ours, ok, err := c.Read(c.Weft, t.Target)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("%s: merge needs our file %s", s.Rng, t.Target)
		}
		if hasConflictMarkers(ours) {
			return "", fmt.Errorf("%s: our %s still has conflict markers from lm sync — resolve them, then build", s.Rng, t.Target)
		}
		return ours, nil
	}

	src, ok, err := c.Read(from, weftRel(c, t, from, t.BasePath))
	if err != nil {
		return "", fmt.Errorf("%s: %v", t.Path, err)
	}
	if !ok {
		return "", fmt.Errorf("%s: layer %s has no %s — `from` names the layer the product is based on", t.Path, from, t.BasePath)
	}

	if t.Type == "json" {
		// A registry is merged whole, by entry identity, so weaving statements have no part in it — and a
		// template that says something the build does not do is worse than one that says nothing. The
		// template still has to say what happens, or an empty file would be the only way to ask for this.
		if len(t.Stmts) != 1 || t.Stmts[0].Op != "registry" {
			at := t.Path
			if len(t.Stmts) > 0 {
				at = t.Stmts[0].Rng.String()
			}
			return "", fmt.Errorf("%s: %s is a registry: the product is upstream's entries plus ours, ours replacing the upstream entry that calls the same scripts — the template is exactly `base.merge(self)`",
				at, t.Target)
		}
		return mergeRegistry(c, t)
	}

	tree := ast.New(t.Type, src)
	if sh, isSh := tree.(*ast.Shell); isSh && len(sh.Unclosed) > 0 {
		return "", fmt.Errorf("%s: these functions have no end and cannot be parsed: %s — they must not be treated as \"no such function\"",
			t.Path, strings.Join(sh.Unclosed, ", "))
	}

	// in <key> as <type>: switch to a nested AST (the prompt in toml is markdown)
	for _, s := range t.Stmts {
		if s.Op != "in" {
			continue
		}
		body, ok := tree.BodyOf(s.Key)
		if !ok {
			return "", fmt.Errorf("%s: the base has no key `%s`", s.Rng, s.Key)
		}
		inner := ast.New(s.As, body)
		if err := apply(c, t, s.Kids, inner, t.Target, &nestCtx{typ: t.Type, key: s.Key}); err != nil {
			return "", err
		}
		tree.SetBody(s.Key, inner.Text())
	}

	if err := apply(c, t, t.Stmts, tree, t.Target, nil); err != nil {
		return "", err
	}

	// Key values run after apply: set(self.frontmatter) replaces the whole frontmatter block there, and a
	// value written before it would be overwritten while the product still looks normal.
	for _, s := range t.Stmts {
		if s.Op != "value" {
			continue
		}
		if err := applyValue(c, t, tree, s); err != nil {
			return "", err
		}
	}

	out := tree.Text()
	// Text files end with a newline. Appending content at the end swallows the base's trailing newline.
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out, nil
}

type nestCtx struct {
	typ, key string
}

// apply applies positional statements to the AST.
//
// Edits apply in reverse order, with equal positions reversed once more. Editing from the back keeps
// earlier line numbers from being shifted by earlier insertions. But when two statements target the
// same anchor, reverse order would flip their relative order — so equal positions are sorted in
// reverse template order, and once applied they end up in template order again.
func apply(c *lang.Config, t *lang.Template, stmts []lang.Stmt, tree ast.Tree, rel string, nest *nestCtx) error {
	var edits []edit
	plain := t.Type == "text" || t.Type == "json"
	if nest != nil {
		plain = nest.typ == "text" || nest.typ == "json"
	}
	for _, s := range stmts {
		switch s.Op {
		case "after", "before", "replace":
			kind, anchor := s.Kind, s.Anchor
			if !ast.Has(tree.Kinds(), kind) {
				return fmt.Errorf("%s: this type has no `%s` nodes (it has %s)",
					s.Rng, kind, strings.Join(tree.Kinds(), "/"))
			}
			span, _, err := locate(tree, kind, s.Within, anchor, s.Ident, s)
			if err != nil {
				return err
			}
			body, err := payloads(c, t, s.Srcs, rel, nest)
			if err != nil {
				return err
			}
			marked := isMarked(c, t, s.Srcs, body)
			lines := tree.Lines()
			switch s.Op {
			case "after":
				// Count blank lines instead of adding one blindly: the previous section may already end
				// with a blank line, and adding one makes two; yet without a blank line before a following
				// heading, markdown glues the insertion onto the previous paragraph.
				// Marked insertions get no outer blank lines: once the marked blocks are stripped the result
				// must be byte-identical to the warp, so a blank line added **outside** the marks would remain.
				if marked || plain {
					edits = append(edits, edit{span[1], span[1], []string{body}, len(edits)})
				} else {
					var repl []string
					if !(span[1]-1 >= 0 && strings.TrimSpace(lines[span[1]-1]) == "") {
						repl = append(repl, "")
					}
					repl = append(repl, body)
					if !(span[1] < len(lines) && strings.TrimSpace(lines[span[1]]) == "") {
						repl = append(repl, "")
					}
					edits = append(edits, edit{span[1], span[1], repl, len(edits)})
				}
			case "before":
				if marked {
					edits = append(edits, edit{span[0], span[0], []string{body}, len(edits)})
				} else {
					var repl []string
					if !(span[0]-1 >= 0 && strings.TrimSpace(lines[span[0]-1]) == "") {
						repl = append(repl, "")
					}
					repl = append(repl, body, "")
					edits = append(edits, edit{span[0], span[0], repl, len(edits)})
				}
			default:
				edits = append(edits, edit{span[0], span[1], strings.Split(body, "\n"), len(edits)})
			}
		case "drop":
			// Deliberately keep this upstream node out of the product: delete it entirely. The reason is
			// in the template, and the build report lists it.
			// When the whole file is replaced with ours, drop only records where the upstream section went —
			// the base is not upstream, so there is nothing to delete.
			if t.From != "" && t.From != c.Warp {
				continue
			}
			if !ast.Has(tree.Kinds(), s.Kind) {
				return fmt.Errorf("%s: this type has no `%s` nodes (it has %s)",
					s.Rng, s.Kind, strings.Join(tree.Kinds(), "/"))
			}
			spans, err := targets(tree, s)
			if err != nil {
				return err
			}
			for _, span := range spans {
				edits = append(edits, edit{span[0], span[1], nil, len(edits)})
			}
		case "move":
			// A move is one deletion and one insertion of the very same lines. Both spans are
			// resolved against the source, and edits apply back to front, so the two never
			// disturb each other's line numbers.
			//
			// Nothing is inserted from our layer and nothing is lost, so a move needs no reason:
			// the node's own bytes are still in the product, and the check that upstream survived
			// finds them there.
			if t.From != "" && t.From != c.Warp {
				continue
			}
			src, _, err := locate(tree, s.Kind, s.Within, s.Anchor, s.Ident, s)
			if err != nil {
				return err
			}
			dst, _, err := locate(tree, s.Move.Kind, s.Move.Within, s.Move.Anchor, s.Move.Ident, s)
			if err != nil {
				return err
			}
			if dst[0] >= src[0] && dst[0] < src[1] {
				return fmt.Errorf("%s: move takes a target outside the node being moved", s.Rng)
			}
			body := append([]string{}, tree.Lines()[src[0]:src[1]]...)
			// Trailing blank lines belong to the gap the node leaves behind, not to the node.
			for len(body) > 0 && strings.TrimSpace(body[len(body)-1]) == "" {
				body = body[:len(body)-1]
			}
			at := dst[1]
			if s.Move.Side == "before" {
				at = dst[0]
			}
			var repl []string
			// No blank line before the very start of the file, and none where there is one already.
			if at > 0 && !(at-1 < len(tree.Lines()) && strings.TrimSpace(tree.Lines()[at-1]) == "") {
				repl = append(repl, "")
			}
			repl = append(repl, body...)
			if !(at < len(tree.Lines()) && strings.TrimSpace(tree.Lines()[at]) == "") {
				repl = append(repl, "")
			}
			edits = append(edits, edit{at, at, repl, len(edits)})
			edits = append(edits, edit{src[0], src[1], nil, len(edits)})
		case "promote", "demote":
			// Only the heading markers change. Everything else the section holds — its own text,
			// its code fences, its trailing blank lines — is spliced back exactly as it was, which
			// is what the build checks afterwards.
			if t.From != "" && t.From != c.Warp {
				continue
			}
			spans, err := targets(tree, s)
			if err != nil {
				return err
			}
			for _, span := range spans {
				lines := append([]string{}, tree.Lines()[span[0]:span[1]]...)
				g := reHeading.FindStringSubmatch(lines[0])
				if g == nil {
					return fmt.Errorf("%s: %s is not a heading", s.Rng, s.Anchor)
				}
				level := len(g[1])
				if s.Op == "promote" && level == 1 {
					return fmt.Errorf("%s: %s is already a top-level heading", s.Rng, s.Anchor)
				}
				if s.Op == "demote" && level == 6 {
					return fmt.Errorf("%s: %s is already the deepest heading markdown has", s.Rng, s.Anchor)
				}
				if s.Op == "promote" {
					lines[0] = lines[0][1:]
				} else {
					lines[0] = "#" + lines[0]
				}
				edits = append(edits, edit{span[0], span[1], lines, len(edits)})
			}
		case "append", "prepend":
			// Each item in the list is its own edit — exactly equivalent to writing several append statements.
			for _, ref := range s.Srcs {
				body, err := payload(c, t, ref, rel, nest)
				if err != nil {
					return err
				}
				marked := refMarked(c, t, ref, body)
				n := len(tree.Lines())
				if s.Op == "append" {
					repl := []string{"", body}
					if marked {
						repl = []string{body}
					}
					edits = append(edits, edit{n, n, repl, len(edits)})
				} else if fm := tree.Find("frontmatter", ""); len(fm) > 0 {
					// The start of a file with frontmatter is right after the frontmatter: frontmatter only
					// counts on the first line, so inserting above it would silently turn it into body text.
					at := fm[0][1]
					repl := []string{"", body}
					if marked {
						repl = []string{body}
					}
					edits = append(edits, edit{at, at, repl, len(edits)})
				} else {
					repl := []string{body, ""}
					if marked {
						repl = []string{body}
					}
					edits = append(edits, edit{0, 0, repl, len(edits)})
				}
			}
		case "frontmatter":
			fmRel := weftRel(c, t, s.Layer, rel)
			if s.File != "" {
				fmRel = s.File
			}
			other, ok, err := c.Read(s.Layer, fmRel)
			if err != nil {
				return fmt.Errorf("%s: %v", s.Rng, err)
			}
			if !ok {
				return fmt.Errorf("%s: layer %s has no %s", s.Rng, s.Layer, rel)
			}
			fm, ok := ast.NewMarkdown(other).BodyOf("frontmatter")
			if !ok {
				return fmt.Errorf("%s: the %s copy of %s has no frontmatter", s.Rng, s.Layer, rel)
			}
			hits := tree.Find("frontmatter", "")
			if len(hits) > 0 {
				edits = append(edits, edit{hits[0][0], hits[0][1], strings.Split(fm, "\n"), len(edits)})
			} else {
				edits = append(edits, edit{0, 0, append(strings.Split(fm, "\n"), ""), len(edits)})
			}
		}
	}
	// Two statements writing over the same lines is a conflict, not a precedence question:
	// the product would depend on which ran last, and the one that lost would be invisible.
	// Insertions are empty spans and stack in template order, so only spans that cover
	// something can collide.
	for i := range edits {
		if edits[i].s == edits[i].e {
			continue
		}
		for j := i + 1; j < len(edits); j++ {
			if edits[j].s == edits[j].e {
				continue
			}
			if edits[i].s < edits[j].e && edits[j].s < edits[i].e {
				return fmt.Errorf("%s: two statements write over the same lines (%d-%d and %d-%d) — one of them would be lost",
					t.Path, edits[i].s+1, edits[i].e, edits[j].s+1, edits[j].e)
			}
		}
	}
	sort.SliceStable(edits, func(i, j int) bool {
		if edits[i].s != edits[j].s {
			return edits[i].s > edits[j].s
		}
		return edits[i].idx > edits[j].idx
	})
	for _, e := range edits {
		tree.Splice(e.s, e.e, e.repl)
	}
	return nil
}

// locate finds the unique node and returns its span and its real name in the tree.
//
// With a path (base."Example 2"."Phase 1"), each leading segment is searched within the **whole section**
// of the previous one — up to the next heading of the same or higher level; the last segment takes its
// own section (up to the next heading), the same as a single name.
// The same subsection name appearing once under each of several sections is common; the path says which one.
func locate(tree ast.Tree, kind string, within []lang.Seg, anchor string, ident bool, s lang.Stmt) ([2]int, string, error) {
	if len(within) == 0 {
		name := anchor
		if ident {
			real, err := resolveIdent(tree, kind, anchor, s.Rng)
			if err != nil {
				return [2]int{}, "", err
			}
			name = real
		}
		span, err := oneIn(tree, s, kind, name)
		return span, name, err
	}
	if kind != "heading" {
		return [2]int{}, "", fmt.Errorf("%s: only markdown sections can be found by path: \"Parent\".\"Child\"", s.Rng)
	}
	nodes := ast.Addressable(tree, "heading")
	lo, hi := 0, len(tree.Lines())
	var path []string
	pick := func(seg lang.Seg) (int, error) {
		var hits []int
		for k, n := range nodes {
			if n.Line >= lo && n.Line < hi && (n.Name == seg.Name || (seg.Ident && identMatch(n.Name, seg.Name))) {
				hits = append(hits, k)
			}
		}
		where := "in the file"
		if len(path) > 0 {
			var q []string
			for _, p := range path {
				q = append(q, fmt.Sprintf("%q", p))
			}
			where = "under " + strings.Join(q, " › ")
		}
		switch len(hits) {
		case 1:
			return hits[0], nil
		case 0:
			return -1, fmt.Errorf("%s: anchor not found %s: %q", s.Rng, where, seg.Name)
		}
		return -1, fmt.Errorf("%s: anchor matches %d places %s: %q — add a parent heading to say which one", s.Rng, len(hits), where, seg.Name)
	}
	for _, seg := range within {
		k, err := pick(seg)
		if err != nil {
			return [2]int{}, "", err
		}
		n, end := nodes[k], hi
		for _, m := range nodes[k+1:] {
			if m.Line >= hi {
				break
			}
			if m.Level <= n.Level {
				end = m.Line
				break
			}
		}
		lo, hi = n.Line+1, end
		path = append(path, n.Name)
	}
	k, err := pick(lang.Seg{Name: anchor, Ident: ident})
	if err != nil {
		return [2]int{}, "", err
	}
	end := len(tree.Lines())
	if k+1 < len(nodes) {
		end = nodes[k+1].Line
	}
	return [2]int{nodes[k].Line, end}, nodes[k].Name, nil
}

// oneIn finds the unique node in the tree; when none is found it suggests the closest name.
func oneIn(tree ast.Tree, s lang.Stmt, kind, anchor string) ([2]int, error) {
	hits := tree.Find(kind, anchor)
	if len(hits) == 0 {
		if near := nearestName(tree, kind, anchor); near != "" {
			return [2]int{}, fmt.Errorf("%s: anchor not found: %s %q; closest is %q", s.Rng, kind, anchor, near)
		}
	}
	return one(s, hits, kind, anchor)
}

// resolveIdent resolves an identifier-form name (_ matches a space or an underscore) to the real name in the tree.
// More than one match is an error — no guessing; it must be rewritten in string form.
func resolveIdent(tree ast.Tree, kind, ident string, at lang.Pos) (string, error) {
	names := ast.Addressable(tree, kind)
	var hits []string
	seen := map[string]bool{}
	for _, n := range names {
		if identMatch(n.Name, ident) && !seen[n.Name] {
			seen[n.Name] = true
			hits = append(hits, n.Name)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		if names == nil {
			return "", fmt.Errorf("%s: %s nodes can only be named in string form: .%q", at, kind, strings.ReplaceAll(ident, "_", " "))
		}
		if near := nearestName(tree, kind, strings.ReplaceAll(ident, "_", " ")); near != "" {
			return "", fmt.Errorf("%s: %s not found; closest is %q", at, ident, near)
		}
		return "", fmt.Errorf("%s: %s not found", at, ident)
	}
	var qs []string
	for _, h := range hits {
		qs = append(qs, fmt.Sprintf("%q", h))
	}
	return "", fmt.Errorf("%s: %s matches %s — use the string form to say which one", at, ident, strings.Join(qs, " and "))
}

// identMatch reports whether name matches ident: an _ in ident matches a space or an underscore,
// and every other character must be equal.
func identMatch(name, ident string) bool {
	rn, ri := []rune(name), []rune(ident)
	if len(rn) != len(ri) {
		return false
	}
	for k := range rn {
		if ri[k] == '_' && (rn[k] == ' ' || rn[k] == '_') {
			continue
		}
		if rn[k] != ri[k] {
			return false
		}
	}
	return true
}

func nearestName(tree ast.Tree, kind, want string) string {
	best, bestD := "", -1
	limit := len([]rune(want))/3 + 2
	for _, n := range ast.Addressable(tree, kind) {
		d := lang.EditDistance(strings.ToLower(n.Name), strings.ToLower(want))
		if d <= limit && (bestD < 0 || d < bestD) {
			best, bestD = n.Name, d
		}
	}
	return best
}

func one(s lang.Stmt, hits [][2]int, kind, anchor string) ([2]int, error) {
	if len(hits) == 0 {
		return [2]int{}, fmt.Errorf("%s: anchor not found: %s %q — "+
			"upstream most likely changed here; not a malfunction, but a signal to take a look", s.Rng, kind, anchor)
	}
	if len(hits) > 1 {
		return [2]int{}, fmt.Errorf("%s: anchor matches %d places: %s %q — "+
			"no \"take the first one\": that would silently insert in the wrong place; be more specific", s.Rng, len(hits), kind, anchor)
	}
	return hits[0], nil
}

func payloads(c *lang.Config, t *lang.Template, refs []lang.Ref, rel string, nest *nestCtx) (string, error) {
	var parts []string
	for _, r := range refs {
		p, err := payload(c, t, r, rel, nest)
		if err != nil {
			return "", err
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, "\n"), nil
}

func isMarked(c *lang.Config, t *lang.Template, refs []lang.Ref, body string) bool {
	if len(refs) == 0 {
		return false
	}
	return refMarked(c, t, refs[0], body)
}

func refMarked(c *lang.Config, t *lang.Template, r lang.Ref, body string) bool {
	layer := r.Layer
	if r.Project != nil {
		layer = c.Weft
	}
	m, ok := c.MarksFor(layer, t.Type)
	return ok && strings.HasPrefix(body, m.Begin)
}

// payload evaluates one content source.
func payload(c *lang.Config, t *lang.Template, r lang.Ref, rel string, nest *nestCtx) (string, error) {
	if r.Project != nil {
		body, err := project(c, t, r, rel)
		if err != nil {
			return "", err
		}
		// A projection reads upstream's shape and writes something that was not there before,
		// so it is ours: it goes inside our marks like any other content of ours, and the
		// report counts it against what we wrote rather than against what upstream kept.
		return mark(c, t, c.Weft, body), nil
	}
	if r.IsLit {
		// A literal belongs to our layer: wrap it in marks like the rest of our content, or the
		// "byte-identical to upstream once marks are stripped" check fails.
		lit := r.Literal
		if c.Vars != nil {
			b, err := c.Vars.Expand([]byte(lit), r.Rng.File, r.Rng.Line, r.Rng.Col+1)
			if err != nil {
				return "", err
			}
			lit = string(b)
		}
		return mark(c, t, r.Layer, lit), nil
	}
	src, useNest, err := refSource(c, t, r, rel, nest)
	if err != nil {
		return "", err
	}
	if r.Kind == "body" || r.Kind == "all" {
		v := strings.TrimRight(src, "\n")
		if r.Kind == "body" {
			if b, ok := ast.NewMarkdown(src).BodyOf("body"); ok {
				v = strings.TrimRight(b, "\n")
			}
		}
		return mark(c, t, r.Layer, v), nil
	}
	if r.Anchor == "" {
		return "", fmt.Errorf("%s: `%s.%s` needs an anchor: write %s.%s[\"...\"]", r.Rng, r.Layer, r.Kind, r.Layer, r.Kind)
	}
	a := refTree(t, r.Kind, src, useNest)
	span, _, err := locate(a, r.Kind, r.Within, r.Anchor, r.Ident, lang.Stmt{Rng: r.Rng})
	if err != nil {
		return "", err
	}
	return mark(c, t, r.Layer, strings.TrimRight(strings.Join(a.Lines()[span[0]:span[1]], "\n"), "\n")), nil
}

// refSource gets the text a content source lives in: which file to read it from, and in a nested
// context only the nested body. Weaving and list share it — the name list reports must be the one
// weaving actually looks up.
func refSource(c *lang.Config, t *lang.Template, r lang.Ref, rel string, nest *nestCtx) (string, *nestCtx, error) {
	srcRel := weftRel(c, t, r.Layer, rel)
	if r.File != "" {
		srcRel = r.File
	}
	src, ok, err := c.Read(r.Layer, srcRel)
	if err != nil {
		return "", nil, fmt.Errorf("%s: %v", r.Rng, err)
	}
	if !ok {
		return "", nil, fmt.Errorf("%s: layer %s has no %s", r.Rng, r.Layer, srcRel)
	}
	useNest := nest
	if srcRel != rel {
		useNest = nil // the source is another file; its structure follows its own extension
	}
	if useNest != nil {
		// In a nested context the source must also be narrowed to the nested body, or the closing
		// triple-quote line is carried into the product as section content.
		inner, ok := ast.New(useNest.typ, src).BodyOf(useNest.key)
		if !ok {
			return "", nil, fmt.Errorf("%s: the %s copy of %s has no key `%s`", r.Rng, r.Layer, srcRel, useNest.key)
		}
		src = inner
	}
	return src, useNest, nil
}

// refTree picks a tree for the source text based on the node kind.
func refTree(t *lang.Template, kind, src string, useNest *nestCtx) ast.Tree {
	// The template's own type first: `key` means a yaml key in a yaml template and a
	// toml key in a toml one, and a fixed order would always hand it to whichever
	// type is listed earlier.
	if useNest == nil && ast.Has(ast.Kinds(t.Type), kind) {
		return ast.New(t.Type, src)
	}
	for _, tn := range ast.Types {
		if ast.Has(ast.Kinds(tn), kind) && (useNest == nil || tn != t.Type) {
			return ast.New(tn, src)
		}
	}
	return ast.NewMarkdown(src)
}

// weftRel is the file a layer's content is read from: the product path by default; when the template
// sets source, the weft layer reads from source instead.
// Only the weft changes: the warp always reads by product path (or by the upstream original name given with rename).
func weftRel(c *lang.Config, t *lang.Template, layer, rel string) string {
	if t.Source == "" || rel != t.Target {
		return rel
	}
	if l, ok := c.Layer(layer); ok && l.Role == "weft" {
		return t.Source
	}
	return rel
}

// mark wraps weft content in marks — "100% of the warp preserved" is verified by stripping the marks
// and comparing byte for byte.
func mark(c *lang.Config, t *lang.Template, layer, v string) string {
	m, ok := c.MarksFor(layer, t.Type)
	if !ok || strings.TrimSpace(v) == "" {
		return v
	}
	return m.Begin + "\n" + v + "\n" + m.End
}

// hasConflictMarkers reports whether a file still holds the markers a three-way merge leaves.
func hasConflictMarkers(s string) bool {
	begin, end := false, false
	for _, l := range strings.Split(s, "\n") {
		begin = begin || strings.HasPrefix(l, "<<<<<<< ")
		end = end || strings.HasPrefix(l, ">>>>>>> ")
	}
	return begin && end
}

// belongsToKey reports whether a frontmatter line continues the key above it: an indented line or a
// list item.
func belongsToKey(l string) bool {
	return strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t") || strings.HasPrefix(l, "-")
}

// unquote takes a value out of its quotes. Only the quotes that wrap a whole value on one line are
// its quotes, and a \" inside such a value belongs to the file's syntax; anywhere else a quote is
// part of the text — `says "go"` is a plain YAML scalar and keeps both.
func unquote(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if len(v) < 2 || strings.Contains(v, "\n") || !strings.HasPrefix(v, `"`) || !strings.HasSuffix(v, `"`) {
		return v, false
	}
	in := v[1 : len(v)-1]
	if strings.Contains(strings.ReplaceAll(in, `\"`, ""), `"`) {
		return v, false // two quoted words, not one quoted value
	}
	return strings.ReplaceAll(in, `\"`, `"`), true
}

// applyValue writes a key's value: ours (set), ours before upstream's (start) or after it (append).
// Upstream's value is read from upstream's file, not from the product being built, so it is still
// there after set(self.frontmatter) replaced the block. Joining is idempotent: a value that already
// contains the other half is not doubled.
func applyValue(c *lang.Config, t *lang.Template, tree ast.Tree, s lang.Stmt) error {
	typ := t.Type
	if s.Kind == "fmkey" {
		typ = "markdown"
	}
	quoted := false
	ours := s.SetRef.Literal
	if !s.SetRef.IsLit {
		rel := weftRel(c, t, s.SetRef.Layer, t.Target)
		srcTyp := t.Type
		if s.SetRef.File != "" {
			rel, srcTyp = s.SetRef.File, lang.TypeOf(s.SetRef.File)
		}
		if s.SetRef.Kind == "fmkey" {
			srcTyp = "markdown"
		}
		src, ok, err := c.Read(s.SetRef.Layer, rel)
		if err != nil {
			return fmt.Errorf("%s: %v", s.Rng, err)
		}
		if !ok {
			return fmt.Errorf("%s: layer %s has no %s", s.Rng, s.SetRef.Layer, rel)
		}
		v, ok := keyValue(srcTyp, src, s.SetRef.Anchor)
		if !ok {
			return fmt.Errorf("%s: our %s has no key %q", s.Rng, rel, s.SetRef.Anchor)
		}
		ours = v
	}
	// A quoted value stays quoted: unquoting it would change what the file says (a colon or a leading
	// * makes it something other than a plain scalar). A literal is content as written.
	if s.SetRef.IsLit {
		ours = strings.TrimSpace(ours)
	} else {
		ours, quoted = unquote(ours)
	}

	val := ours
	if s.Mode != "set" {
		from := t.From
		if from == "" {
			from = c.Warp
		}
		up, _, err := c.Read(from, t.BasePath)
		if err != nil {
			return fmt.Errorf("%s: %v", s.Rng, err)
		}
		uv, ok := keyValue(typ, up, s.SetKey)
		if !ok {
			return fmt.Errorf("%s: upstream has no key %q to %s ours to — to add the key, use set", s.Rng, s.SetKey, s.Mode)
		}
		uv, ok = unquote(uv)
		quoted = quoted || ok
		// A multi-line value (a toml """ block holding markdown) needs a blank line between the halves:
		// on one line the two documents run together.
		sep := " "
		if strings.Contains(ours, "\n") || strings.Contains(uv, "\n") {
			sep = "\n\n"
		}
		switch {
		case uv == "" || strings.Contains(ours, uv):
		case ours == "" || strings.Contains(uv, ours):
			val = uv
		case s.Mode == "start":
			val = ours + sep + uv
		default:
			val = uv + sep + ours
		}
	}

	if quoted && s.Kind == "fmkey" {
		val = `"` + strings.ReplaceAll(val, `"`, `\"`) + `"`
	}
	if s.Kind != "fmkey" {
		if !tree.SetBody(s.SetKey, val) {
			return fmt.Errorf("%s: upstream has no key %q", s.Rng, s.SetKey)
		}
		return nil
	}
	hits := tree.Find("frontmatter", "")
	if len(hits) == 0 {
		return fmt.Errorf("%s: upstream's file has no frontmatter", s.Rng)
	}
	fm, _ := tree.BodyOf("frontmatter")
	lines := strings.Split(fm, "\n")
	done := false
	for i, l := range lines {
		if !strings.HasPrefix(l, s.SetKey+":") {
			continue
		}
		// Upstream's value may be a list or a nested map. Writing one line over its first line would
		// leave the rest of it behind, as lines belonging to a key that is no longer there.
		if i+1 < len(lines) && strings.TrimSpace(l) == s.SetKey+":" && belongsToKey(lines[i+1]) {
			return fmt.Errorf("%s: upstream's key %q has a value over several lines — a value like that can only be taken whole: base.frontmatter.set(self.frontmatter)", s.Rng, s.SetKey)
		}
		lines[i], done = s.SetKey+": "+val, true
		break
	}
	if !done {
		// set adds a key upstream does not have, just before the closing ---
		last := len(lines) - 1
		lines = append(lines[:last], s.SetKey+": "+val, lines[last])
	}
	tree.Splice(hits[0][0], hits[0][1], lines)
	return nil
}

// keyValue reads one key from a source: markdown looks in the frontmatter, other types in their own keys.
func keyValue(typ, src, key string) (string, bool) {
	if typ != "markdown" {
		return ast.New(typ, src).BodyOf(key)
	}
	fm, ok := ast.NewMarkdown(src).BodyOf("frontmatter")
	if !ok {
		return "", false
	}
	for _, l := range strings.Split(fm, "\n") {
		if strings.HasPrefix(l, key+":") {
			return strings.TrimPrefix(l, key+":"), true
		}
	}
	return "", false
}

// targets is what a statement applies to: the one node it names, or every node its
// predicate found. A predicate that matches nothing is an error — a statement that
// silently did nothing is the kind of quiet the language exists to prevent.
func targets(tree ast.Tree, s lang.Stmt) ([][2]int, error) {
	if s.Select == nil {
		span, _, err := locate(tree, s.Kind, s.Within, s.Anchor, s.Ident, s)
		if err != nil {
			return nil, err
		}
		return [][2]int{span}, nil
	}
	found, err := selected(tree, s.Select)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", s.Rng, err)
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("%s: the predicate matched nothing", s.Rng)
	}
	out := make([][2]int, 0, len(found))
	for _, n := range found {
		out = append(out, [2]int{n.Line, n.End})
	}
	return out, nil
}

// selected is every node a predicate allows, in file order. It is the one place the
// predicates are read, so the weave and the checks that follow it agree on what was chosen.
func selected(tree ast.Tree, sel *lang.Select) ([]ast.Named, error) {
	var re *regexp.Regexp
	if sel.Match != "" {
		var err error
		if re, err = regexp.Compile(sel.Match); err != nil {
			return nil, err
		}
	}
	var out []ast.Named
	for _, n := range ast.Addressable(tree, sel.Kind) {
		if re != nil && !re.MatchString(n.Name) {
			continue
		}
		if sel.Level != 0 && n.Level != sel.Level {
			continue
		}
		if sel.Empty && strings.TrimSpace(strings.Join(tree.Lines()[n.Line+1:n.End], "")) != "" {
			continue
		}
		out = append(out, n)
	}
	return out, nil
}

// project derives content from the shape of upstream: every node the predicate found, put
// through the template once. It copies nothing upstream says — only the names it gave things —
// so what it writes is new, and the report counts it as ours like any other literal.
//
// The fields are the ones a document's structure actually has. There is deliberately no field
// for a link target: an anchor is a convention of whichever renderer reads the product, not a
// property of the document, and this language does not guess conventions.
func project(c *lang.Config, t *lang.Template, r lang.Ref, rel string) (string, error) {
	src, ok, err := c.Read(r.Layer, weftRel(c, t, r.Layer, rel))
	if err != nil || !ok {
		return "", fmt.Errorf("%s: %s has no %s to project from", r.Rng, r.Layer, rel)
	}
	tree := ast.New(t.Type, src)
	if tree == nil {
		return "", fmt.Errorf("%s: %s cannot be projected from", r.Rng, t.Type)
	}
	found, err := selected(tree, r.Project)
	if err != nil {
		return "", fmt.Errorf("%s: %v", r.Rng, err)
	}
	if len(found) == 0 {
		return "", fmt.Errorf("%s: the predicate matched nothing to project", r.Rng)
	}
	var out []string
	for _, n := range found {
		line := r.Literal
		line = strings.ReplaceAll(line, "{name}", n.Name)
		line = strings.ReplaceAll(line, "{level}", strconv.Itoa(n.Level))
		body := strings.Join(tree.Lines()[n.Line+1:n.End], "\n")
		line = strings.ReplaceAll(line, "{body}", strings.TrimSpace(body))
		out = append(out, line)
	}
	return strings.Join(out, "\n"), nil
}
