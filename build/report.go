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
	Added      []string     // files only in our layer
	Untaken    []string     // files in upstream, not taken into the product
	Anchored   []ReportLine // anchors completed during the build

	VarsUsed, VarsUnused []string
	Warnings             []string
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
	var inserted []string
	var drops, replaces []lang.Stmt
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
			case "after", "before", "append", "prepend":
				for _, ref := range s.Srcs {
					inserted = append(inserted, refName(ref))
				}
			case "frontmatter":
				inserted = append(inserted, "frontmatter")
			case "value":
				inserted = append(inserted, s.SetKey+" ("+s.Mode+")")
			case "in":
				walk(s.Kids)
			}
		}
	}
	walk(t.Stmts)

	// Our frontmatter must reach the product too: a key only in our file, left out because the template
	// neither sets nor starts / appends a frontmatter key, would disappear with nothing to say so.
	if t.Type == "markdown" && !whole && !merged && c.Weft != "" {
		if ours, ok, _ := c.Read(c.Weft, weftRel(c, t, c.Weft, t.Target)); ok {
			for _, k := range frontmatterKeys(ours) {
				// Both sides are read as weaving writes them, so an escaped quote compares equal.
				want, _ := keyValue("markdown", ours, k)
				got, found := keyValue("markdown", out, k)
				// A value over several lines (a list, a nested map) can't be joined on one line, so it
				// only arrives whole. Upstream's would otherwise stand in the product with ours nowhere.
				if strings.TrimSpace(want) == "" {
					if keyBlock(ours, k) != keyBlock(out, k) {
						errs = append(errs, fmt.Errorf("%s: our frontmatter key %q has a value over several lines and the product has upstream's — a value like that can only be taken whole: base.frontmatter.set(self.frontmatter)", t.Path, k))
					}
					continue
				}
				mine, _ := unquote(want)
				theirs, _ := unquote(got)
				if !found || !strings.Contains(theirs, mine) {
					errs = append(errs, fmt.Errorf("%s: our frontmatter key %q is not in the product %s — base.frontmatter.set(self.frontmatter) takes all of ours, base.frontmatter.%s.start(self.frontmatter.%s) puts ours before upstream's",
						t.Path, k, t.Target, k, k))
				}
			}
		}
	}

	up, upOK, _ := c.Read(c.Warp, t.BasePath)
	var upTree ast.Tree
	kind := ""
	switch t.Type {
	case "markdown":
		kind = "heading"
	case "shell":
		kind = "function"
	}
	if upOK && kind != "" {
		upTree = ast.New(t.Type, up)
	}
	// realName maps a name written as an identifier to its real name in upstream
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

	if upTree == nil {
		return nil
	}

	// Strip our marks and count how often each name appears in the product
	outText := out
	if m, ok := c.MarksFor(c.Weft, t.Type); ok && c.Weft != "" {
		outText = stripMarked(out, m)
		// This is why the language exists: for an insert-only template, the body with marks stripped
		// must be byte-identical to upstream. It is checked on every build instead of by a separate
		// outside gate: an engine one byte off on a blank line still yields a normal-looking product.
		// The frontmatter may be changed by set / start / append, so only the body is compared.
		if t.Type == "markdown" && !whole && !merged && len(drops)+len(replaces) == 0 {
			body := func(s string) string {
				b, _ := ast.NewMarkdown(s).BodyOf("body")
				return strings.TrimRight(b, "\n")
			}
			if body(outText) != body(up) {
				errs = append(errs, fmt.Errorf("%s: with our marks stripped, the body of %s differs from upstream — an insert-only template must not change a single upstream byte", t.Path, t.Target))
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
	accounted := append(append([]lang.Stmt{}, drops...), replaces...)
	for _, s := range accounted {
		if s.Kind == kind {
			for _, l := range nodeLines(s) {
				coveredAt[l] = true
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
			what := "section"
			if kind == "function" {
				what = "function"
			}
			errs = append(errs, fmt.Errorf("%s: upstream content lost: in %s, %s %q is not in the product and no drop / replace gives a reason. "+
				"Weave it in, or write: base.%s.drop(reason: \"...\")", t.Path, t.Target, what, name, lang.NameText(name)))
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
