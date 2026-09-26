package build

// Build report: our layer versus upstream. What was extended, overridden or dropped, and whether
// any upstream content was lost.
//
// Lost content is a build error. If an upstream section (a markdown heading, a shell function) is
// not in the product and the template gives no reason with drop / replace, the build fails. Merge
// templates need this most: our file can lose an upstream section with nothing else noticing.
// A section newly added upstream may never reach the product, while the product looks perfectly normal.
//
// The check counts by name: for every name, the product (with our marks stripped) must have as many
// sections as upstream, and each missing one must be accounted for by a drop / replace. A whole-file
// replace with a reason is not counted section by section.

import (
	"fmt"
	"github.com/axfor/loom/lang"
	"io"
	"sort"
	"strings"

	"github.com/axfor/loom/ast"
)

// Report is the account of one build.
type Report struct {
	Out      string
	Files    int
	Written  int
	Removed  int
	VarsFile string

	Extended   []ReportLine // upstream kept whole, ours inserted
	Overridden []ReportLine // ours replaces part or all of upstream
	Dropped    []ReportLine // upstream nodes left out of the product on purpose
	Moved      []ReportLine // upstream nodes put somewhere else, byte for byte
	Skipped    []ReportLine // statements an `if` or a caught result kept from running
	Added      []string     // files only in our layer
	Untaken    []string     // files in upstream, not taken into the product
	Anchored   []ReportLine // anchors completed during the build

	VarsUsed, VarsUnused []string
	Warnings             []string

	// How much of upstream the build proved is still there, byte for byte. This is the
	// language's one claim, so the report states it as a quantity rather than as a category:
	// everything outside the write set is verified, and what is inside it is not.
	UpBytes, VerifiedBytes int
}

// splitKept checks the heading a split cut at: in the product once, and identical to upstream's
// but for its # markers.
func splitKept(up, out ast.Tree, s lang.Stmt) error {
	span, err := moveTo(up, s)
	if err != nil {
		return nil // the anchor error is reported where the weave failed
	}
	name := ""
	for _, n := range ast.Addressable(up, "heading") {
		if n.Line == span[0] {
			name = n.Name
		}
	}
	var got []string
	found := 0
	for _, n := range ast.Addressable(out, "heading") {
		if n.Name == name {
			found++
			got = out.Lines()[n.Line:n.End]
		}
	}
	if found != 1 {
		return fmt.Errorf("%s: %q, where %s was split, is not in the product once", s.Rng, name, s.Anchor)
	}
	trim := func(ls []string) string {
		for len(ls) > 0 && strings.TrimSpace(ls[len(ls)-1]) == "" {
			ls = ls[:len(ls)-1]
		}
		return strings.Join(ls, "\n")
	}
	if unlevel(trim(got)) != unlevel(trim(up.Lines()[span[0]:span[1]])) {
		return fmt.Errorf("%s: %s was split at %q, and something other than its heading level changed", s.Rng, s.Anchor, name)
	}
	return nil
}

// moveName says where a move went as the template says it: the node, and the axes from it.
func moveName(m *lang.Move) string {
	name := m.Anchor
	if m.Axis != "" {
		name += "." + m.Axis
	}
	return name
}

// nowAt reads a node that was moved or had its level changed from where it is now. Its path says
// where it was: a promote takes it out of its parent, a move can take it anywhere. So the path is
// shortened from the end — the ancestors it still has — down to none, and the first that holds
// the node is where it is.
func nowAt(tree ast.Tree, s lang.Stmt) (string, error) {
	var err error
	for k := len(s.Within); k >= 0; k-- {
		var text string
		if text, err = nodeText(tree, s.Kind, s.Within[:k], s.Anchor, s.Ident, s); err == nil {
			return text, nil
		}
	}
	return "", err
}

// ReportLine is one line of the report: which product file, and what happened to it.
type ReportLine struct {
	Path, Detail string
}

// account records a woven template in the report and checks that no upstream section was lost.
func account(c *lang.Config, t *lang.Template, out string, r *Report) []error {
	var errs []error
	whole := t.From != "" && t.From != c.Warp
	merged := false

	up, upOK, _ := c.Read(c.Warp, t.BasePath)

	// The same resolution the weave did: an if took one branch there, and the report has to
	// account for that branch and no other. Two lists would be a report about a build that did
	// not happen.
	stmts := t.Stmts
	if upOK {
		if tree := ast.New(t.Type, up); tree != nil {
			if rr, err := resolve(c, t, tree); err == nil {
				stmts = rr.stmts
				r.Skipped = append(r.Skipped, rr.skipped...)
			}
		}
	}

	var inserted []string
	var drops, replaces, moves, levels, values, inserts []lang.Stmt
	// A view's statements work in the view's own document, so they are not counted by name
	// against the file's; but one that changes upstream's text inside the view still means the
	// template is not insert-only.
	viewChanged := false
	var walk func(ss []lang.Stmt)
	walk = func(ss []lang.Stmt) {
		for _, s := range ss {
			switch s.Op {
			case "merge":
				merged = true
			case "drop":
				drops = append(drops, s)
			case "replace":
				replaces = append(replaces, s)
			case "move", "swap":
				moves = append(moves, s)
			case "promote", "demote":
				levels = append(levels, s)
			case "unwrap":
				// The heading is gone from the product on purpose, which is a drop with a reason;
				// what was under it is still there, and still accounted for by its own name.
				drops = append(drops, s)
			case "split":
				if s.Reason == "" {
					// Cut at a heading already there: nothing is added, a level changes.
					levels = append(levels, s)
					continue
				}
				inserted = append(inserted, s.Reason)
			case "after", "before", "append", "prepend":
				inserts = append(inserts, s)
				for _, ref := range s.Srcs {
					inserted = append(inserted, refName(ref))
				}
			case "frontmatter":
				inserted = append(inserted, "frontmatter")
			case "value":
				values = append(values, s)
				inserted = append(inserted, s.SetKey+" ("+s.Mode+")")
			case "in":
				for _, k := range s.Kids {
					switch k.Op {
					case "drop", "replace", "move", "swap", "promote", "demote", "unwrap", "join", "split":
						viewChanged = true
					}
				}
				walk(s.Kids)
			}
		}
	}
	walk(stmts)

	// Our frontmatter must reach the product too: a key only in our file, left out because the template
	// neither sets nor starts / appends a frontmatter key, would disappear with nothing to say so.
	//
	// The same holds one level out, for the top-level keys of a toml or yaml file. That case used to
	// pass in silence: our description simply was not in the product, the build reported everything
	// proved, and nothing said a value of ours had been dropped on the floor.
	if (t.Type == "markdown" || lang.HasValues(t.Type)) && !whole && !merged && c.Weft != "" {
		// A key opened as a view is woven into, not written over: what lands in the product is
		// upstream's value with ours worked into it, so our text is not expected to appear whole.
		// The statements inside the view are accounted for on their own terms.
		viewed := map[string]bool{}
		var views func([]lang.Stmt)
		views = func(ss []lang.Stmt) {
			for _, s := range ss {
				if s.Op == "in" {
					viewed[s.Key] = true
				}
				views(s.Kids)
			}
		}
		views(t.Stmts)
		if ours, ok, _ := c.Read(c.Weft, weftRel(c, t, c.Weft, t.Target)); ok {
			for _, k := range ourKeys(t.Type, ours) {
				if viewed[k] {
					continue
				}
				// Both sides are read as weaving writes them, so an escaped quote compares equal.
				want, _ := keyValue(t.Type, ours, k)
				got, found := keyValue(t.Type, out, k)
				// A value over several lines (a list, a nested map) can't be joined on one line, so it
				// only arrives whole. Upstream's would otherwise stand in the product with ours nowhere.
				if strings.TrimSpace(want) == "" {
					if t.Type == "markdown" && keyBlock(ours, k) != keyBlock(out, k) {
						errs = append(errs, fmt.Errorf("%s: our frontmatter key %q has a value over several lines and the product has upstream's — a value like that can only be taken whole: base.frontmatter.set(self.frontmatter)", t.Path, k))
					}
					continue
				}
				mine, _ := unquote(want)
				theirs, _ := unquote(got)
				if !found || !strings.Contains(theirs, mine) {
					if t.Type == "markdown" {
						errs = append(errs, fmt.Errorf("%s: our frontmatter key %q is not in the product %s — base.frontmatter.set(self.frontmatter) takes all of ours, base.frontmatter.%s.start(self.frontmatter.%s) puts ours before upstream's",
							t.Path, k, t.Target, k, k))
						continue
					}
					// set, start and append on a key all write into upstream's, so they need
					// upstream to have it. Where it does not, the key is new to the file and is
					// added to it: naming a mode here would send the author to an error instead.
					if _, shared := keyValue(t.Type, up, k); !shared {
						errs = append(errs, fmt.Errorf("%s: our key %q is not in the product %s, and upstream has no such key — add it to the file: base.append(self.%s)",
							t.Path, k, t.Target, lang.NameTextFor(k, c.Loom)))
						continue
					}
					errs = append(errs, fmt.Errorf("%s: our key %q is not in the product %s — say what to do with it: base.%s.start(self.%s), or once for the whole tree with `keys start` in loom.om",
						t.Path, k, t.Target, lang.NameTextFor(k, c.Loom), lang.NameTextFor(k, c.Loom)))
				}
			}
		}
	}

	// The kind whose nodes are counted by name, upstream against product. Every type whose nodes
	// ast can enumerate belongs here: leaving toml and yaml out meant a merge could drop a key of
	// upstream's and nothing would notice, which is the one thing this file exists to prevent.
	var upTree ast.Tree
	kind := ""
	switch t.Type {
	case "markdown":
		kind = "heading"
	case "shell":
		kind = "function"
	case "toml", "yaml":
		kind = "key"
	case "text":
		// A text file's nodes are its lines, and a line is named by what it says: changing one
		// loses it as surely as deleting it. That is stricter than the other types, where a node
		// keeps its name while its body changes — and it is the only way a merge of a .js or .txt
		// file can be held to the rule that nothing of upstream's goes missing in silence.
		kind = "line"
	}
	// A tree for every type, not just the two that have names worth accounting for: the byte count
	// below has to locate a dropped node to subtract it, and without a tree it either crashes or,
	// worse, quietly counts the dropped bytes as proved.
	if upOK {
		upTree = ast.New(t.Type, up)
	}
	// realName maps a name written as an identifier to its real name in upstream
	// A statement with a predicate stands for one statement per node it found. Expanding it
	// here means everything downstream — the accounting, the checks, the report — keeps working
	// by name and never has to know a predicate was involved.
	expand := func(in []lang.Stmt) []lang.Stmt {
		if upTree == nil {
			return in
		}
		var out []lang.Stmt
		for _, s := range in {
			if s.Select == nil {
				out = append(out, s)
				continue
			}
			found, err := selected(upTree, s.Select)
			if err != nil {
				continue
			}
			for _, n := range found {
				c := s
				c.Select, c.Anchor, c.Ident, c.Within = nil, n.Name, false, nil
				out = append(out, c)
			}
		}
		return out
	}
	drops, replaces, moves, levels = expand(drops), expand(replaces), expand(moves), expand(levels)

	realName := func(s lang.Stmt) string {
		if (s.Ident || len(s.Within) > 0) && upTree != nil {
			if _, n, err := locate(upTree, s.Kind, s.Within, s.Anchor, s.Ident, s); err == nil {
				return n
			}
		}
		return s.Anchor
	}

	for _, s := range drops {
		r.Dropped = append(r.Dropped, ReportLine{t.Target + " § " + realName(s), s.Reason})
	}
	for _, s := range moves {
		detail := s.Move.Side + " " + moveName(s.Move) + " · bytes unchanged"
		if s.Op == "swap" {
			detail = "traded places with " + moveName(s.Move) + " · bytes unchanged"
		}
		r.Moved = append(r.Moved, ReportLine{t.Target + " § " + realName(s), detail})
	}
	for _, s := range levels {
		detail := s.Op + "d · only the heading level changed"
		if s.Op == "split" {
			detail = "split at " + moveName(s.Move) + " · only that heading's level changed"
		}
		r.Moved = append(r.Moved, ReportLine{t.Target + " § " + realName(s), detail})
	}
	switch {
	case whole:
		r.Overridden = append(r.Overridden, ReportLine{t.Target, "whole file · " + t.UseReason})
	case merged:
		detail := "merge"
		if t.Type == "shell" && upOK {
			detail += " · " + functionStats(up, out)
		}
		r.Overridden = append(r.Overridden, ReportLine{t.Target, detail})
	case len(replaces) > 0:
		var parts []string
		for _, s := range replaces {
			parts = append(parts, fmt.Sprintf("%s · %s", realName(s), s.Reason))
		}
		r.Overridden = append(r.Overridden, ReportLine{t.Target, strings.Join(parts, "; ")})
	}
	if !whole && !merged && len(inserted) > 0 {
		r.Extended = append(r.Extended, ReportLine{t.Target, fmt.Sprintf("+%d: %s", len(inserted), strings.Join(inserted, " / "))})
	}

	// The share of upstream this product proves. Everything outside the write set is compared
	// byte for byte; a dropped or replaced node is inside it, and a merged or wholly replaced
	// file is all of it.
	if upOK {
		verified := len(up)
		switch {
		case whole || merged:
			verified = 0
		default:
			for _, s := range append(append([]lang.Stmt{}, drops...), replaces...) {
				if span, _, err := locate(upTree, s.Kind, s.Within, s.Anchor, s.Ident, s); err == nil {
					for _, l := range upTree.Lines()[span[0]:span[1]] {
						verified -= len(l) + 1
					}
				}
			}
		}
		if verified < 0 {
			verified = 0
		}
		r.UpBytes += len(up)
		r.VerifiedBytes += verified
		if len(up) > 0 && verified < len(up) {
			pct := verified * 100 / len(up)
			for i := range r.Overridden {
				if r.Overridden[i].Path == t.Target {
					r.Overridden[i].Detail = fmt.Sprintf("%d%% of upstream kept · %s", pct, r.Overridden[i].Detail)
				}
			}
		}
	}

	// Strip our marks and compare what is left with upstream.
	//
	// This is why the language exists: for an insert-only template, the product with marks
	// stripped must be byte-identical to upstream. It is checked on every build instead of by a
	// separate outside gate: an engine one byte off on a blank line still yields a
	// normal-looking product.
	//
	// It ran for markdown alone, and sat behind the return below besides, so on every other kind
	// of file the report asserted a byte-identity it had never looked at. What is compared
	// differs by type: a markdown file's frontmatter and a value type's keys are changed on
	// purpose by set / start / append, so a template that does any of that is not insert-only to
	// begin with. A move reorders upstream, so the file cannot match it line for line — what a
	// move promises instead is checked further down, and it is a stronger promise.
	outText := out
	if m, ok := c.MarksFor(c.Weft, t.Type); ok && c.Weft != "" && upOK {
		outText = stripMarked(out, m)
		insertOnly := !whole && !merged && len(drops)+len(replaces)+len(moves)+len(levels) == 0 && !viewChanged
		if lang.HasValues(t.Type) && len(values) > 0 {
			insertOnly = false
		}
		if insertOnly {
			if a, b := insertable(t.Type, outText), insertable(t.Type, up); a != b {
				errs = append(errs, fmt.Errorf("%s: with our marks stripped, %s differs from upstream — an insert-only template must not change a single upstream byte", t.Path, t.Target))
			}
		}
	}

	// What follows is accounting by name: markdown headings, shell functions, toml and yaml keys,
	// and the lines of a text file. Everything found so far still counts: returning nil here threw away
	// errors already collected, which is how a key of ours could go missing in silence.
	if upTree == nil || kind == "" {
		return errs
	}
	// What a move promises: the node's own bytes are unchanged, only its place is. That is
	// stronger than what an insertion promises about upstream, and it is checkable — so it is
	// checked, rather than being taken on the word of the engine that did the moving.
	if len(moves) > 0 && upOK {
		moved := ast.New(t.Type, outText)
		for _, s := range moves {
			want, err := nodeText(upTree, s.Kind, s.Within, s.Anchor, s.Ident, s)
			if err != nil {
				continue // the anchor error is reported where the weave failed
			}
			got, err := nowAt(moved, s)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %s was moved but is not in the product", s.Rng, s.Anchor))
				continue
			}
			if got != want {
				errs = append(errs, fmt.Errorf("%s: %s was moved and its own bytes changed — a move may change where a node is, never what it is", s.Rng, s.Anchor))
			}
		}
	}

	// What promote and demote promise: only the heading markers moved. Strip the markers from
	// both sides and the section must be identical — its text, its code fences, everything.
	for _, s := range levels {
		if !upOK {
			break
		}
		if s.Op == "split" {
			// A split at a heading changes that heading, not the section it cuts, so the cut is
			// what has to come through unchanged but for its level.
			if err := splitKept(upTree, ast.New(t.Type, outText), s); err != nil {
				errs = append(errs, err)
			}
			continue
		}
		want, err := nodeText(upTree, s.Kind, s.Within, s.Anchor, s.Ident, s)
		if err != nil {
			continue
		}
		got, err := nowAt(ast.New(t.Type, outText), s)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %s was %s but is not in the product", s.Rng, s.Anchor, s.Op+"d"))
			continue
		}
		if unlevel(got) != unlevel(want) {
			errs = append(errs, fmt.Errorf("%s: %s was %sd and something other than its heading level changed", s.Rng, s.Anchor, s.Op))
		}
	}

	// What a statement inserts must be in the product. Nothing else checks it: the byte comparison
	// strips our marks, and the rest counts upstream's nodes. So when the weave lost a section of
	// ours — an insertion at the very line a drop started once was deleted with it — the product
	// only looked like a template that inserted less, and the report said +1.
	if !whole && !merged {
		woven := ast.Addressable(ast.New(t.Type, out), kind)
		for _, s := range inserts {
			for _, ref := range s.Srcs {
				if ref.Layer != "self" || ref.IsLit || ref.Kind != kind || ref.Anchor == "" {
					continue
				}
				found := false
				for _, n := range woven {
					if n.Name == ref.Anchor || (ref.Ident && identMatch(n.Name, ref.Anchor)) {
						found = true
						break
					}
				}
				if !found {
					errs = append(errs, fmt.Errorf("%s: our %s %q is inserted by this statement but is not in the product %s", s.Rng, nodeWord(kind), ref.Anchor, t.Target))
				}
			}
		}
	}

	// Every section of ours has to reach the product — in the branch the questions took, not just
	// somewhere in the template. Being named by some statement was the only test, so a section
	// placed only in an if's then-branch went missing whenever upstream sent the build down the
	// else, and one written in a Self: section that nothing named was never woven at all.
	// What is ours sits inside our marks, so that is where each is looked for, counted by name.
	if m, ok := c.MarksFor(c.Weft, t.Type); ok && !whole && !merged && (t.Type == "markdown" || t.Type == "shell") {
		woven := map[string]int{}
		for _, n := range ast.Addressable(ast.New(t.Type, markedText(out, m)), kind) {
			woven[n.Name]++
		}
		for _, src := range oursOf(c, t) {
			need := map[string]int{}
			var order []string
			for _, n := range ast.Addressable(ast.New(t.Type, src.text), kind) {
				if need[n.Name] == 0 {
					order = append(order, n.Name)
				}
				need[n.Name]++
			}
			for _, name := range order {
				if woven[name] < need[name] {
					errs = append(errs, fmt.Errorf("%s: our %s %q (%s) is not in the product %s — a statement has to place it in the branch that runs; where a question can go either way, place it in each",
						t.Path, nodeWord(kind), name, src.where, t.Target))
				}
			}
		}
	}

	outTree := ast.New(t.Type, outText)
	have := map[string]int{}
	for _, n := range ast.Addressable(outTree, kind) {
		have[n.Name]++
	}

	upNames := ast.Addressable(upTree, kind)
	need := map[string]int{}
	var order []string
	for _, n := range upNames {
		if need[n.Name] == 0 {
			order = append(order, n.Name)
		}
		need[n.Name]++
	}

	// Nodes accounted for by drop / replace, keyed by position in upstream: several sections may share a name, and this records which one
	coveredAt := map[int]bool{}
	nodeLines := func(s lang.Stmt) []int {
		if len(s.Within) > 0 {
			if span, _, err := locate(upTree, kind, s.Within, s.Anchor, s.Ident, s); err == nil {
				return []int{span[0]}
			}
			return nil
		}
		var lines []int
		for _, span := range upTree.Find(kind, realName(s)) {
			lines = append(lines, span[0])
		}
		return lines
	}
	nodeSpans := func(s lang.Stmt) [][2]int {
		if len(s.Within) > 0 {
			if span, _, err := locate(upTree, kind, s.Within, s.Anchor, s.Ident, s); err == nil {
				return [][2]int{span}
			}
			return nil
		}
		return upTree.Find(kind, realName(s))
	}
	accounted := append(append([]lang.Stmt{}, drops...), replaces...)
	for _, s := range accounted {
		if s.Kind == kind {
			for _, l := range nodeLines(s) {
				coveredAt[l] = true
			}
			// A key's lines hold the keys nested under it, so dropping or replacing it takes them
			// too, and they are accounted for by it — naming them again would write over the same
			// lines twice. A markdown section ends at the next heading, subsections included, so
			// each of those is still its own to account for.
			if kind != "heading" {
				for _, sp := range nodeSpans(s) {
					for _, n := range upNames {
						if n.Line > sp[0] && n.Line < sp[1] {
							coveredAt[n.Line] = true
						}
					}
				}
			}
		}
	}
	covered := map[string]int{}
	for _, n := range upNames {
		if coveredAt[n.Line] {
			covered[n.Name]++
		}
	}

	// In merge and whole-file replace templates, drop is a declaration: verify the section really is absent from the product
	if whole || merged {
		for _, s := range drops {
			if name := realName(s); s.Kind == kind && have[name] > need[name]-covered[name] {
				errs = append(errs, fmt.Errorf("%s: %q is declared absent from the product but is still there — remove this drop, or really take it out", s.Rng, name))
			}
		}
	}
	if whole {
		return errs
	}

	// A section runs from its heading to the next heading, and its subsections are nodes of their own.
	// Dropping a section but leaving its subsections in place hangs them under the previous section,
	// so they must be accounted for too.
	if kind == "heading" {
		for _, s := range drops {
			// unwrap keeps what was under the heading, one level up: its subsections are in the
			// product, and each is still accounted for under its own name.
			if s.Op == "unwrap" {
				continue
			}
			for _, l := range nodeLines(s) {
				for k, n := range upNames {
					if n.Line != l {
						continue
					}
					var orphans []string
					for _, sub := range upNames[k+1:] {
						if sub.Level <= n.Level {
							break
						}
						if !coveredAt[sub.Line] {
							orphans = append(orphans, fmt.Sprintf("%q", sub.Name))
						}
					}
					if len(orphans) > 0 {
						errs = append(errs, fmt.Errorf("%s: %q is dropped but its subsections %s are not accounted for — a section ends at the next heading, "+
							"so the subsections would stay and hang under the previous section. Drop them too, or weave them elsewhere", s.Rng, n.Name, strings.Join(orphans, ", ")))
					}
				}
			}
		}
	}
	for _, name := range order {
		if missing := need[name] - have[name] - covered[name]; missing > 0 {
			what := nodeWord(kind)
			// A line is named by its text, which is rarely an identifier: say it the way that
			// always reads as a line.
			how := "base." + lang.NameTextFor(name, c.Loom)
			if kind == "line" {
				how = "base.line(" + lang.Quote(name) + ")"
			}
			errs = append(errs, fmt.Errorf("%s: upstream content lost: in %s, %s %q is not in the product and no drop / replace gives a reason. "+
				"Weave it in, or write: %s.drop(reason: \"...\")", t.Path, t.Target, what, name, how))
		}
	}
	return errs
}

// refName is what the report calls a content source.
func refName(r lang.Ref) string {
	switch {
	case r.IsLit:
		return "literal"
	case r.Kind == "body":
		return "body"
	case r.Kind == "all":
		return "whole file"
	}
	return r.Anchor
}

// stripMarked removes marked blocks (marks included); what remains is the upstream part.
// markedText is what stripMarked takes out: our content, block by block.
func markedText(s string, m lang.Marks) string {
	var blocks []string
	var cur []string
	in := false
	for _, l := range strings.Split(s, "\n") {
		switch {
		case l == m.Begin:
			in, cur = true, nil
		case l == m.End:
			in = false
			blocks = append(blocks, strings.Join(cur, "\n"))
		case in:
			cur = append(cur, l)
		}
	}
	return strings.Join(blocks, "\n\n")
}

// ourSource is a document of ours a product is woven from: our file beside the template, or a
// Self: document of the product's type written in it.
type ourSource struct{ text, where string }

func oursOf(c *lang.Config, t *lang.Template) []ourSource {
	if len(t.Resources) > 0 {
		var out []ourSource
		for _, res := range t.Resources {
			for _, d := range res.Docs {
				if d.Kind != t.Type {
					continue
				}
				where := "in Self:"
				if res.Name != "" {
					where = "in Self as " + res.Name + ":"
				}
				out = append(out, ourSource{d.Text, where})
			}
		}
		return out
	}
	if c.Weft == "" {
		return nil
	}
	if src, ok, _ := c.Read(c.Weft, weftRel(c, t, c.Weft, t.Target)); ok {
		return []ourSource{{src, "in our file"}}
	}
	return nil
}

func stripMarked(s string, m lang.Marks) string {
	var out []string
	skip := false
	for _, l := range strings.Split(s, "\n") {
		switch {
		case l == m.Begin:
			skip = true
		case l == m.End:
			skip = false
		case !skip:
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// keyBlock is a frontmatter key's whole value: its line and the lines under it that belong to it (a
// list, a nested map). Empty when the file has no such key.
func keyBlock(src, key string) string {
	fm, ok := ast.NewMarkdown(src).BodyOf("frontmatter")
	if !ok {
		return ""
	}
	var out []string
	for _, l := range strings.Split(fm, "\n") {
		switch {
		case len(out) == 0:
			if strings.HasPrefix(l, key+":") {
				out = append(out, l)
			}
		case belongsToKey(l):
			out = append(out, l)
		default:
			return strings.Join(out, "\n")
		}
	}
	return strings.Join(out, "\n")
}

// frontmatterKeys lists the top-level keys of a markdown file's frontmatter, in order.
func frontmatterKeys(src string) []string {
	fm, ok := ast.NewMarkdown(src).BodyOf("frontmatter")
	if !ok {
		return nil
	}
	var keys []string
	for _, l := range strings.Split(fm, "\n") {
		if i := strings.Index(l, ":"); i > 0 && l != "---" && !strings.ContainsAny(l[:i], " \t#") {
			keys = append(keys, l[:i])
		}
	}
	return keys
}

// functionStats counts functions in upstream and in the product: kept (untouched) · changed · added · removed.
func functionStats(up, out string) string {
	bodies := func(src string) map[string]string {
		t := ast.NewShell(src)
		m := map[string]string{}
		for _, n := range ast.Addressable(t, "function") {
			if hits := t.Find("function", n.Name); len(hits) == 1 {
				m[n.Name] = strings.Join(t.Lines()[hits[0][0]:hits[0][1]], "\n")
			}
		}
		return m
	}
	a, b := bodies(up), bodies(out)
	var kept, changed, added []string
	for name, body := range a {
		if ob, ok := b[name]; ok {
			if ob == body {
				kept = append(kept, name)
			} else {
				changed = append(changed, name)
			}
		}
	}
	for name := range b {
		if _, ok := a[name]; !ok {
			added = append(added, name)
		}
	}
	sort.Strings(changed)
	sort.Strings(added)
	part := func(label string, names []string, list bool) string {
		if list && len(names) > 0 {
			return fmt.Sprintf("%s %d (%s)", label, len(names), strings.Join(names, ", "))
		}
		return fmt.Sprintf("%s %d", label, len(names))
	}
	return "functions: " + strings.Join([]string{part("kept", kept, false), part("changed", changed, true), part("added", added, true)}, " · ")
}

// Print writes the report to the terminal.
func (r *Report) Print(w io.Writer) {
	head := "lm check" // nothing was written
	if r.Out != "" {
		head = "lm build → " + r.Out
	}
	src := "no variables file"
	if r.VarsFile != "" {
		src = "variables from " + r.VarsFile
	}
	fmt.Fprintf(w, "%s  (%d files · %d written · %d removed · %s)\n\n", head, r.Files, r.Written, r.Removed, src)
	section := func(label string, lines []ReportLine, unit, note string, show int) {
		fmt.Fprintf(w, "%s %4d %s  %s\n", label, len(lines), unit, note)
		for i, l := range lines {
			if show >= 0 && i == show {
				fmt.Fprintf(w, "  ... and %d more\n", len(lines)-show)
				break
			}
			fmt.Fprintf(w, "  %-44s %s\n", l.Path, l.Detail)
		}
	}
	section("extended         ", r.Extended, "files ", "upstream kept whole, ours inserted", 5)
	section("overridden       ", r.Overridden, "files ", "ours replaces part or all of upstream", -1)
	section("dropped          ", r.Dropped, "places", "left out on purpose", -1)
	section("restructured     ", r.Moved, "places", "moved or re-levelled, content unchanged", -1)
	// A template that asks a question writes different things to different upstreams. That is not
	// an error, but it is the one thing a reader cannot see by looking at the template alone.
	section("skipped          ", r.Skipped, "places", "a question decided against it", -1)
	fmt.Fprintf(w, "added             %4d files   only in our layer\n", len(r.Added))
	fmt.Fprintf(w, "not taken         %4d files   in upstream, not listed in take\n", len(r.Untaken))
	for i, p := range r.Untaken {
		if i == 5 {
			fmt.Fprintf(w, "  ... and %d more\n", len(r.Untaken)-5)
			break
		}
		fmt.Fprintf(w, "  %s\n", p)
	}
	fmt.Fprintf(w, "lost                 0 places  ← must be 0, otherwise the build fails\n")
	if r.UpBytes > 0 {
		pct := r.VerifiedBytes * 100 / r.UpBytes
		note := "proved unchanged, byte for byte"
		if pct < 100 {
			note = fmt.Sprintf("proved unchanged; %s is inside what we wrote", size(r.UpBytes-r.VerifiedBytes))
		}
		fmt.Fprintf(w, "guaranteed         %3d%% of upstream  %s\n", pct, note)
	}
	if len(r.Anchored) > 0 {
		section("anchors completed", r.Anchored, "places", "written back to templates; review them with your commit", -1)
	}
	fmt.Fprintf(w, "variables         %4d         %s\n", len(r.VarsUsed), strings.Join(r.VarsUsed, ", "))
	for _, n := range r.VarsUnused {
		fmt.Fprintf(w, "  ⚠ variable %s is defined but never used\n", n)
	}
	for _, msg := range r.Warnings {
		fmt.Fprintf(w, "  ⚠ %s\n", msg)
	}
}

// Markdown is the full, untruncated report, written to a file by -report.
func (r *Report) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# lm build report\n\nProduct: %s · %d files\n", r.Out, r.Files)
	if r.VarsFile != "" {
		fmt.Fprintf(&b, "Variables: %s (%s)\n", r.VarsFile, strings.Join(r.VarsUsed, ", "))
	}
	table := func(title string, lines []ReportLine) {
		fmt.Fprintf(&b, "\n## %s (%d)\n\n", title, len(lines))
		if len(lines) == 0 {
			return
		}
		b.WriteString("| Product | Detail |\n|---|---|\n")
		for _, l := range lines {
			fmt.Fprintf(&b, "| %s | %s |\n", escCell(l.Path), escCell(l.Detail))
		}
	}
	list := func(title string, paths []string) {
		fmt.Fprintf(&b, "\n## %s (%d)\n\n", title, len(paths))
		for _, p := range paths {
			fmt.Fprintf(&b, "- %s\n", p)
		}
	}
	table("Extended", r.Extended)
	table("Overridden", r.Overridden)
	table("Dropped", r.Dropped)
	table("Moved", r.Moved)
	list("Added", r.Added)
	list("Not taken", r.Untaken)
	table("Anchors completed", r.Anchored)
	if len(r.VarsUnused)+len(r.Warnings) > 0 {
		b.WriteString("\n## Warnings\n\n")
		for _, n := range r.VarsUnused {
			fmt.Fprintf(&b, "- variable %s is defined but never used\n", n)
		}
		for _, msg := range r.Warnings {
			fmt.Fprintf(&b, "- %s\n", msg)
		}
	}
	return b.String()
}

func escCell(s string) string { return strings.ReplaceAll(s, "|", "\\|") }

// nodeText is a node's own lines, with the blank lines that trail it left out — they belong
// to the gap around a node, not to the node, and a move changes the gaps.
func nodeText(tree ast.Tree, kind string, within []lang.Seg, anchor string, ident bool, s lang.Stmt) (string, error) {
	span, _, err := locate(tree, kind, within, anchor, ident, s)
	if err != nil {
		return "", err
	}
	lines := tree.Lines()[span[0]:span[1]]
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n"), nil
}

// unlevel drops the leading # markers of the first line, so two sections can be compared
// for everything except the level promote and demote are allowed to change.
func unlevel(sec string) string {
	i := strings.IndexByte(sec, '\n')
	head := sec
	rest := ""
	if i >= 0 {
		head, rest = sec[:i], sec[i:]
	}
	return strings.TrimLeft(head, "#") + rest
}

// size prints a byte count the way a person reads one.
func size(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f kB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}

// ourKeys are the keys of ours that have to reach the product: a markdown file's frontmatter, or
// the top-level keys of a file whose nodes hold values. Nested keys are left out — what to do with
// a table is not what to do with a value, and the product takes the table from whoever wrote it.
func ourKeys(typ, src string) []string {
	if typ == "markdown" {
		return frontmatterKeys(src)
	}
	var out []string
	for _, n := range ast.Addressable(ast.New(typ, src), "key") {
		if !strings.Contains(n.Name, ".") {
			out = append(out, n.Name)
		}
	}
	return out
}

// insertable is the part of a file an insert-only template must leave byte for byte alone. For
// markdown that is the body: the frontmatter is changed on purpose by set / start / append. For
// every other kind it is the whole file — a value type only gets here when no value was written.
func insertable(typ, src string) string {
	if typ == "markdown" {
		b, _ := ast.NewMarkdown(src).BodyOf("body")
		return strings.TrimRight(b, "\n")
	}
	return strings.TrimRight(src, "\n")
}

// nodeWord is what to call a node of this kind in a message to a person: a markdown heading is a
// section, and a key is a key.
func nodeWord(kind string) string {
	if kind == "heading" {
		return "section"
	}
	return kind
}
