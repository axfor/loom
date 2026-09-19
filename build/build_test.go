package build_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/axfor/loom/build"
	"github.com/axfor/loom/lang"
)

// treeRepo creates a repository for whole-tree builds. Keys of files are paths from the repository root;
// values starting with "#!" are written as executable.
func treeRepo(t *testing.T, settings string, files map[string]string) (*lang.Config, string) {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\ntemplates \"t\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n"+settings)
	for p, s := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		mustWrite(t, full, s)
		if strings.HasPrefix(s, "#!") {
			if err := os.Chmod(full, 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, d := range []string{"up", "me", "t"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	return c, dir
}

func buildTree(t *testing.T, c *lang.Config, out string) (*build.Plan, error) {
	t.Helper()
	plan, err := build.PlanBuild(c, true)
	if err != nil {
		return nil, err
	}
	return plan, build.WriteBuild(c, plan, out)
}

func listTree(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			rel, _ := filepath.Rel(root, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// The whole tree: templates woven, our layer copied, upstream only as listed in take, mirrors, manifest,
// executable bits, symbolic links, stale product files deleted, unchanged files not rewritten.
func TestBuildTree(t *testing.T) {
	c, dir := treeRepo(t, `
take     "references/**" "LICENSE"
mirror   "cmds" "commands"
manifest ".manifest"
`, map[string]string{
		"up/doc.md":          "## Overview\n\nup\n",
		"up/references/a.md": "ref\n",
		"up/LICENSE":         "MIT\n",
		"up/evals/case.json": "{}\n",
		"up/run.sh":          "#!/bin/sh\necho up\n",
		"me/doc.md":          "## Our section\n\nme\n",
		"me/tool.sh":         "#!/bin/sh\necho tool\n",
		"me/cmds/ship.toml":  "x = 1\n",
		"me/run.sh":          "#!/bin/sh\necho me\n",
		"t/doc.md.lm":        `base.Overview.after("Our section")`,
		"t/run.sh.lm":        `base.replace(self, reason: "we write our own script")`,
	})
	if err := os.Symlink("../references", filepath.Join(dir, "me", "link")); err != nil {
		t.Fatal(err)
	}
	// a group-writable source must not make the product group-writable
	if err := os.Chmod(filepath.Join(dir, "me", "cmds", "ship.toml"), 0o664); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	mustWrite(t, filepath.Join(out, "stale.txt"), "old\n")

	plan, err := buildTree(t, c, out)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".manifest", "LICENSE", "cmds/ship.toml", "commands/ship.toml", "doc.md", "link", "references/a.md", "run.sh", "tool.sh"}
	if got := listTree(t, out); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("wrong product files\ngot  %v\nwant %v", got, want)
	}
	doc, _ := os.ReadFile(filepath.Join(out, "doc.md"))
	if !strings.Contains(string(doc), "<!-- B -->\n## Our section") {
		t.Errorf("the template was not woven:\n%s", doc)
	}
	for _, exe := range []string{"tool.sh", "run.sh"} {
		if st, _ := os.Stat(filepath.Join(out, exe)); st.Mode().Perm()&0o111 == 0 {
			t.Errorf("%s lost its executable bit", exe)
		}
	}
	if st, _ := os.Stat(filepath.Join(out, "cmds", "ship.toml")); st.Mode().Perm() != 0o644 {
		t.Errorf("product permissions must be 0644 or 0755, got %o", st.Mode().Perm())
	}
	if target, err := os.Readlink(filepath.Join(out, "link")); err != nil || target != "../references" {
		t.Errorf("the symbolic link was not kept as a link: %q %v", target, err)
	}
	manifest, _ := os.ReadFile(filepath.Join(out, ".manifest"))
	if !strings.Contains(string(manifest), "\ncommands/ship.toml\n") || strings.Contains(string(manifest), ".manifest\n") {
		t.Errorf("wrong manifest:\n%s", manifest)
	}
	r := plan.Report
	if strings.Join(r.Untaken, " ") != "evals/case.json" {
		t.Errorf("upstream files not taken must be reported: %v", r.Untaken)
	}
	if r.Removed != 1 {
		t.Errorf("expected 1 stale file removed, removed %d", r.Removed)
	}
	if len(r.Overridden) != 1 || r.Overridden[0].Path != "run.sh" || !strings.Contains(r.Overridden[0].Detail, "we write our own script") {
		t.Errorf("a whole-file replace belongs under overridden: %+v", r.Overridden)
	}

	plan, err = buildTree(t, c, out)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Report.Written != 0 {
		t.Errorf("sources did not change, so the second build must not rewrite files; wrote %d", plan.Report.Written)
	}
}

// A file of ours shadows the upstream file at the same path without a template: upstream is gone, with no reason.
func TestShadowNeedsTemplate(t *testing.T) {
	c, dir := treeRepo(t, "", map[string]string{
		"up/a.md":    "## A\n\nup\n",
		"me/a.md":    "## A\n\nme\n",
		"up/same.md": "same\n",
		"me/same.md": "same\n",
	})
	_, err := buildTree(t, c, filepath.Join(dir, "out"))
	if err == nil || !strings.Contains(err.Error(), "shadows the upstream file") {
		t.Fatalf("must be refused, got: %v", err)
	}
	if strings.Contains(err.Error(), "same.md") {
		t.Errorf("an identical file loses nothing and must not be reported: %v", err)
	}
}

// An upstream section missing from the product with nothing in the template to account for it is a build
// error; a drop accounts for it. Dropping a section that is still there, or dropping a section but leaving
// its subsections behind, is an error too.
func TestLostContent(t *testing.T) {
	up := "## Intro\n\nx\n\n## Old\n\ny\n\n## Keep\n\nz\n"
	c, dir := treeRepo(t, "", map[string]string{
		"up/a.md": up,
		"up/b.md": "## Parent\n\np\n\n### Child\n\nc\n\n## Next\n\nn\n",
		"me/a.md": "## Intro\n\nx\n\n## Keep\n\nz\n",
	})
	out := filepath.Join(dir, "out")

	write := func(p, s string) { mustWrite(t, filepath.Join(dir, "t", p), s) }
	write("a.md.lm", `base.merge(self)`)
	_, err := buildTree(t, c, out)
	if err == nil || !strings.Contains(err.Error(), `upstream content lost: in a.md, section "Old"`) {
		t.Fatalf("a section our merged file no longer has, without a drop, must be reported as lost, got: %v", err)
	}

	write("a.md.lm", "base.merge(self)\nbase.Old.drop(reason: \"does not apply\")\n")
	plan, err := buildTree(t, c, out)
	if err != nil {
		t.Fatalf("the drop accounts for it, so no error, got: %v", err)
	}
	if len(plan.Report.Dropped) != 1 || plan.Report.Dropped[0].Path != "a.md § Old" {
		t.Errorf("wrong dropped entries: %+v", plan.Report.Dropped)
	}

	write("a.md.lm", "base.merge(self)\nbase.Old.drop(reason: \"does not apply\")\nbase.Keep.drop(reason: \"does not apply\")\n")
	if _, err := buildTree(t, c, out); err == nil || !strings.Contains(err.Error(), `"Keep" is declared absent from the product but is still there`) {
		t.Errorf("a dropped section that is still there must be an error, got: %v", err)
	}

	write("a.md.lm", "base.merge(self)\nbase.Old.drop(reason: \"does not apply\")\n")
	write("b.md.lm", `base.Parent.drop(reason: "does not apply")`)
	if _, err := buildTree(t, c, out); err == nil || !strings.Contains(err.Error(), `its subsections "Child" are not accounted for`) {
		t.Errorf("dropping a section but leaving its subsection must be an error, got: %v", err)
	}
	write("b.md.lm", "base.Parent.drop(reason: \"does not apply\")\nbase.Child.drop(reason: \"does not apply\")\n")
	if _, err := buildTree(t, c, out); err != nil {
		t.Errorf("the subsection is dropped too, so no error, got: %v", err)
	}
}

// Variables: expanded in our content, never in upstream; a variable missing from the -e file is an error,
// with no fallback; {{@@x}} is a literal.
func TestVariables(t *testing.T) {
	c, dir := treeRepo(t, `take "notes.md"`, map[string]string{
		"me/install.md": "# Install\n\nCafé: {{@url}}\nLiteral: {{@@url}}\n",
		"up/notes.md":   "upstream {{@url}} is not expanded\n",
		"up/doc.md":     "## A\n\nwoven upstream {{@url}} is not expanded either\n",
		"t/doc.md.lm":   "base.append(`registry {{@npm}}`)\n",
		"lm.e":          "# public\nurl = https://example.com/x\nnpm = https://registry.example\nunused = 1\n",
		"inner.e":       "url = https://inner.example/x\n",
	})
	out := filepath.Join(dir, "out")
	v, err := lang.LoadVars(filepath.Join(dir, "lm.e"))
	if err != nil {
		t.Fatal(err)
	}
	c.Vars = v
	plan, err := buildTree(t, c, out)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(out, "install.md"))
	if string(got) != "# Install\n\nCafé: https://example.com/x\nLiteral: {{@url}}\n" {
		t.Errorf("variables not expanded correctly:\n%s", got)
	}
	notes, _ := os.ReadFile(filepath.Join(out, "notes.md"))
	if !strings.Contains(string(notes), "{{@url}}") {
		t.Errorf("upstream content must not be expanded: %s", notes)
	}
	doc, _ := os.ReadFile(filepath.Join(out, "doc.md"))
	if !strings.Contains(string(doc), "woven upstream {{@url}} is not expanded either") {
		t.Errorf("woven upstream text must not be expanded:\n%s", doc)
	}
	if !strings.Contains(string(doc), "registry https://registry.example") {
		t.Errorf("a literal in a template is our content too and must be expanded:\n%s", doc)
	}
	if strings.Join(plan.Report.VarsUnused, ",") != "unused" {
		t.Errorf("unused variables must be reported: %v", plan.Report.VarsUnused)
	}

	// -e inner.e: it has no npm, and there is no fallback to lm.e
	inner, err := lang.LoadVars(filepath.Join(dir, "inner.e"))
	if err != nil {
		t.Fatal(err)
	}
	c.Vars = inner
	if _, err := build.PlanBuild(c, true); err == nil || !strings.Contains(err.Error(), "variable {{@npm}} is not defined in") || !strings.Contains(err.Error(), "doc.md.lm:1:") {
		t.Errorf("npm is missing from the -e file; must be an error pointing at the template, got: %v", err)
	}

	c.Vars = lang.NoVars()
	_, err = build.PlanBuild(c, true)
	if err == nil || !strings.Contains(err.Error(), "install.md:3:7: variable {{@url}} is used, but there is no variables file") {
		t.Errorf("a variable used without a variables file must be an error at file:line:col (columns count characters, not bytes), got: %v", err)
	}

	mustWrite(t, filepath.Join(dir, "bad.e"), "url https://x\n")
	if _, err := lang.LoadVars(filepath.Join(dir, "bad.e")); err == nil || !strings.Contains(err.Error(), "bad.e:1:") {
		t.Errorf("a malformed variables file must report the line, got: %v", err)
	}
}

// A wrong output path would delete source: an output directory containing the source tree, or inside a layer, is refused.
func TestOutputGuard(t *testing.T) {
	c, dir := treeRepo(t, "", map[string]string{"up/a.md": "## A\n"})
	for _, out := range []string{dir, filepath.Dir(dir), filepath.Join(dir, "me", "out")} {
		plan, err := build.PlanBuild(c, true)
		if err != nil {
			t.Fatal(err)
		}
		if err := build.WriteBuild(c, plan, out); err == nil {
			t.Errorf("output directory %s must be refused", out)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "up", "a.md")); err != nil {
		t.Fatalf("a source file was deleted: %v", err)
	}
}

// A mirror overwriting a file already in the product is an error; the later write does not win.
func TestMirrorConflict(t *testing.T) {
	c, dir := treeRepo(t, `mirror "a" "b"`, map[string]string{
		"me/a/x.md": "a\n",
		"me/b/x.md": "b\n",
	})
	if _, err := buildTree(t, c, filepath.Join(dir, "out")); err == nil || !strings.Contains(err.Error(), "the mirror would overwrite it") {
		t.Errorf("a mirror colliding with an existing file must be an error, got: %v", err)
	}
}

// lm check also answers "would lm build change the output directory?": every way an output
// directory drifts from its sources is reported, and a fresh build is clean.
func TestCheckFindsStaleOutput(t *testing.T) {
	c, dir := treeRepo(t, `manifest ".manifest"`, map[string]string{
		"up/doc.md":   "## Overview\n\nup\n",
		"me/doc.md":   "## Ours\n\nme\n",
		"me/tool.sh":  "#!/bin/sh\necho tool\n",
		"me/gone.md":  "x\n",
		"t/doc.md.lm": `base.Overview.after("Ours")`,
	})
	if err := os.Symlink("doc.md", filepath.Join(dir, "me", "link")); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	plan, err := buildTree(t, c, out)
	if err != nil {
		t.Fatal(err)
	}
	if problems, err := build.StaleOutputs(c, plan, out); err != nil || len(problems) != 0 {
		t.Fatalf("a fresh build must be clean, got %v %v", problems, err)
	}

	mustWrite(t, filepath.Join(out, "doc.md"), "edited by hand\n")
	os.Chmod(filepath.Join(out, "tool.sh"), 0o644)
	os.Remove(filepath.Join(out, "gone.md"))
	mustWrite(t, filepath.Join(out, "extra.md"), "x\n")
	os.Remove(filepath.Join(out, "link"))
	os.Symlink("tool.sh", filepath.Join(out, "link"))
	mustWrite(t, filepath.Join(out, ".manifest"), "stale\n")

	problems, err := build.StaleOutputs(c, plan, out)
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(problems, "\n")
	for _, want := range []string{
		"doc.md: differs from what the sources build",
		"tool.sh: lost its executable bit",
		"gone.md: missing from the output directory",
		"extra.md: not produced by the sources",
		"link: should be a link to doc.md",
		".manifest: differs",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("not reported: %q\ngot:\n%s", want, all)
		}
	}

	if plan, err = buildTree(t, c, out); err != nil {
		t.Fatal(err)
	}
	if problems, _ := build.StaleOutputs(c, plan, out); len(problems) != 0 {
		t.Errorf("after lm build the output must be clean again, got %v", problems)
	}
	if problems, _ := build.StaleOutputs(c, plan, filepath.Join(dir, "never-built")); len(problems) != 0 {
		t.Errorf("an output directory that does not exist is not stale, got %v", problems)
	}
}

// Without a templates setting, templates live next to our files. A template is a source: it is never
// copied into the product or counted as added.
func TestTemplatesBesideOurFiles(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"loom.om":          "base \"up\"\nself \"me\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n",
		"up/doc.md":        "## Overview\n\nup\n",
		"me/doc.md":        "## Ours\n\nme\n",
		"me/doc.md.lm":     "base.Overview.after(\"Ours\")\n",
		"up/run.sh":        "#!/bin/sh\necho up\n",
		"me/run.sh":        "#!/bin/sh\necho me\n",
		"me/run.sh.lm":     "base.merge(self)\n",
		"me/notes/free.md": "only ours\n",
		// doc.md has its template beside it; an import without the extension still means doc.md
		"up/guide.md":    "## Intro\n\nup\n",
		"me/guide.md.lm": "import \"/doc\"\nbase.Intro.after(doc.Ours)\n",
	}
	for p, s := range files {
		mustWrite(t, filepath.Join(dir, filepath.FromSlash(p)), s)
	}
	c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	plan, err := buildTree(t, c, out)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(listTree(t, out), " "); got != "doc.md guide.md notes/free.md run.sh" {
		t.Errorf("templates must not reach the product, got %s", got)
	}
	if got := strings.Join(plan.Report.Added, " "); got != "notes/free.md" {
		t.Errorf("only real files of ours count as added, got %s", got)
	}
	doc, _ := os.ReadFile(filepath.Join(out, "doc.md"))
	if !strings.Contains(string(doc), "## Ours") {
		t.Errorf("the template beside doc.md was not woven:\n%s", doc)
	}
	if guide, _ := os.ReadFile(filepath.Join(out, "guide.md")); !strings.Contains(string(guide), "## Ours") {
		t.Errorf("the import of /doc did not bring in doc.md:\n%s", guide)
	}
}

// The guarantee is a quantity, not a category: the report says what share of upstream the
// build proved is still there, byte for byte, and a file that kept less says so on its line.
func TestReportGuaranteedShare(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\n")
	mustWrite(t, filepath.Join(dir, "up", "keep.md"), "## A\n\naaaa\n\n## B\n\nbbbb\n")
	mustWrite(t, filepath.Join(dir, "me", "keep.md"), "## Ours\n\nx\n")
	mustWrite(t, filepath.Join(dir, "me", "keep.lm"), "base.A.after(self.Ours)\n")
	mustWrite(t, filepath.Join(dir, "up", "gone.md"), "## A\n\naaaa\n\n## B\n\nbbbb\n")
	mustWrite(t, filepath.Join(dir, "me", "gone.md"), "## Ours\n\nx\n")
	mustWrite(t, filepath.Join(dir, "me", "gone.lm"), "base.replace(self, reason: \"all ours\")\n")

	c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := build.PlanBuild(c, false)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	r := plan.Report
	if r.UpBytes == 0 {
		t.Fatal("no upstream bytes were counted")
	}
	// One file kept everything, the other kept none of it, and they are the same size.
	if pct := r.VerifiedBytes * 100 / r.UpBytes; pct != 50 {
		t.Errorf("guaranteed %d%% of upstream, want 50%%", pct)
	}
	var line string
	for _, l := range r.Overridden {
		if l.Path == "gone.md" {
			line = l.Detail
		}
	}
	if !strings.Contains(line, "0% of upstream kept") {
		t.Errorf("a wholly replaced file should say it kept none: %q", line)
	}
}

// The same frontmatter statement was written by hand in 37 of the 81 templates of the tree that
// uses Loom. A tree can say once what it wants done with a key of ours that upstream also has,
// and the build writes the statement into the template — the way it already completes anchors, so
// the template still says it and a person still reviews it.
func TestFrontmatterCompleted(t *testing.T) {
	dir := t.TempDir()
	tpl := filepath.Join(dir, "me", "doc.lm")
	setup := func(settings string) *lang.Config {
		t.Helper()
		mustWrite(t, filepath.Join(dir, "loom.om"), settings)
		mustWrite(t, filepath.Join(dir, "up", "doc.md"), "---\nname: doc\ndescription: Upstream.\n---\n\n## Overview\n\nup\n")
		mustWrite(t, filepath.Join(dir, "me", "doc.md"), "---\ndescription: Ours.\n---\n\n## Ours\n\nx\n")
		mustWrite(t, tpl, "base.Overview.after(self.Ours)\n")
		c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	// Without the setting nothing is written: whether ours replaces upstream's value or goes
	// before it changes what the product says, and that is not the compiler's to decide.
	c := setup("base \"up\"\nself \"me\"\n")
	if _, err := build.PlanBuild(c, true); err == nil {
		t.Error("an unaccounted frontmatter key should fail when the tree has said nothing")
	}
	if got := readFile(t, tpl); !strings.HasPrefix(got, "base.Overview") {
		t.Errorf("the template was written to without a policy: %q", got)
	}

	// With it, the statement is written, reported, and the product takes both values.
	c = setup("frontmatter start\nbase \"up\"\nself \"me\"\n")
	plan, err := build.PlanBuild(c, true)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	got := readFile(t, tpl)
	if !strings.HasPrefix(got, "base.frontmatter.description.start(self.frontmatter.description)\n") {
		t.Errorf("the statement was not written into the template:\n%s", got)
	}
	var noted bool
	for _, l := range plan.Report.Anchored {
		if strings.Contains(l.Detail, "frontmatter description") {
			noted = true
		}
	}
	if !noted {
		t.Errorf("writing it was not reported: %+v", plan.Report.Anchored)
	}

	// Running again writes nothing more: the statement is there now.
	if _, err := build.PlanBuild(c, true); err != nil {
		t.Fatalf("second build: %v", err)
	}
	if again := readFile(t, tpl); again != got {
		t.Errorf("a second build changed the template again:\n%s", again)
	}
}

// `keys` is the frontmatter rule one level out: a top-level key of our toml or yaml file that
// upstream also has. Same machinery, same refusal to decide — the setting says what to do with
// such a key, and the build only works out which keys those are and writes it down.
func TestKeysCompleted(t *testing.T) {
	dir := t.TempDir()
	tpl := filepath.Join(dir, "me", "c.lm")
	setup := func(settings, template string) *lang.Config {
		t.Helper()
		mustWrite(t, filepath.Join(dir, "loom.om"), settings)
		mustWrite(t, filepath.Join(dir, "up", "c.toml"),
			"description = \"Upstream.\"\nprompt = \"\"\"\n## Steps\n\nup\n\"\"\"\nkept = 1\n")
		mustWrite(t, filepath.Join(dir, "me", "c.toml"),
			"description = \"Ours.\"\nprompt = \"\"\"\n## Ours\n\nx\n\"\"\"\nonlyours = 2\n")
		mustWrite(t, tpl, template)
		c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	// A key a view already opens is not written for a second time. Getting this wrong is not
	// harmless: the value statement and the view then both write the key, and the product changes.
	// onlyours is ours alone, so nothing in the setting's remit covers it: it needs a set of its
	// own, and the build says so rather than letting the key vanish.
	const view = "base.prompt.as(markdown).Steps.after(\"Ours\")\nbase.append(self.onlyours)\n"

	c := setup("base \"up\"\nself \"me\"\n", view)
	if _, err := build.PlanBuild(c, true); err == nil {
		t.Error("an unaccounted key should fail when the tree has said nothing")
	}
	if got := readFile(t, tpl); got != view {
		t.Errorf("the template was written to without a policy: %q", got)
	}

	c = setup("keys start\nbase \"up\"\nself \"me\"\n", view)
	if _, err := build.PlanBuild(c, true); err != nil {
		t.Fatalf("build: %v", err)
	}
	got := readFile(t, tpl)
	if !strings.HasPrefix(got, "base.description.start(self.description)\n") {
		t.Errorf("the statement was not written into the template:\n%s", got)
	}
	if strings.Count(got, "base.prompt") != 1 {
		t.Errorf("a key the view already opens was written for again:\n%s", got)
	}
	// The setting only covers keys both layers have; the one that is ours alone keeps the single
	// statement it was written with, and gains no second one.
	if strings.Count(got, "onlyours") != 1 {
		t.Errorf("the setting wrote for a key upstream does not have:\n%s", got)
	}

	// Running again writes nothing more.
	if _, err := build.PlanBuild(c, true); err != nil {
		t.Fatalf("second build: %v", err)
	}
	if again := readFile(t, tpl); again != got {
		t.Errorf("a second build changed the template again:\n%s", again)
	}

	// A key an edit already names is spoken for too, whatever the edit is. Dropping it fails for
	// its own reason — our value really is not in the product then — but the setting must not add
	// a second statement on top of the drop, which would be two statements fighting over one key.
	c = setup("keys start\nbase \"up\"\nself \"me\"\n", view+"base.key(\"description\").drop(reason: \"upstream's is wrong for us\")\n")
	_, err := build.PlanBuild(c, true)
	if err == nil {
		t.Error("dropping the key leaves our value out of the product, which should be said")
	} else if !strings.Contains(err.Error(), `our key "description" is not in the product`) {
		t.Errorf("unexpected error: %v", err)
	}
	if got := readFile(t, tpl); strings.Contains(got, "base.description.start") {
		t.Errorf("a key an edit already names was written for:\n%s", got)
	}
}

// A drop or replace on a toml or yaml key used to crash: the report built an upstream tree only
// for the two types that have names worth accounting for, then located the dropped node in it
// anyway. Guarding the call would have hidden a second fault — the dropped bytes would have been
// counted as proved — so the tree is built for every type and the guarantee is real.
func TestValueTypeDropIsCountedNotCrashed(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\n")
	mustWrite(t, filepath.Join(dir, "up", "c.toml"), "description = \"Upstream.\"\nkept = \"stays\"\n")
	mustWrite(t, filepath.Join(dir, "me", "c.toml"), "kept = \"stays\"\n")
	mustWrite(t, filepath.Join(dir, "me", "c.toml.lm"), "base.key(\"description\").drop(reason: \"not for us\")\n")

	c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := build.PlanBuild(c, true)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	// The dropped key is inside the write set, so it cannot be part of what is proved unchanged.
	if plan.Report.VerifiedBytes >= plan.Report.UpBytes {
		t.Errorf("the dropped key was counted as proved: %d of %d bytes", plan.Report.VerifiedBytes, plan.Report.UpBytes)
	}
	if plan.Report.VerifiedBytes == 0 {
		t.Error("nothing was counted as proved, though the rest of the file is untouched")
	}
}

// `body` answers the one case anchor completion cannot: our headings share nothing with
// upstream's — a translation, most often — so no section of ours has a neighbour to follow and
// none ever will. The build says so today and stops; with the setting the tree has said once
// what to do, and the statement is written down like any other.
func TestBodyTakenWhole(t *testing.T) {
	dir := t.TempDir()
	tpl := filepath.Join(dir, "me", "doc.lm")
	setup := func(settings, template string) *lang.Config {
		t.Helper()
		mustWrite(t, filepath.Join(dir, "loom.om"), settings)
		mustWrite(t, filepath.Join(dir, "up", "doc.md"), "# Title\n\n## Overview\n\nup\n\n## Usage\n\nu\n")
		mustWrite(t, filepath.Join(dir, "me", "doc.md"), "# Title\n\n## 概述\n\no\n\n## 用法\n\ny\n")
		mustWrite(t, tpl, template)
		c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	c := setup("base \"up\"\nself \"me\"\n", "")
	if _, err := build.PlanBuild(c, true); err == nil || !strings.Contains(err.Error(), "can't infer an anchor") {
		t.Errorf("without the setting this is still a build error, not a guess: %v", err)
	}

	c = setup("body append\nbase \"up\"\nself \"me\"\n", "")
	plan, err := build.PlanBuild(c, true)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got := readFile(t, tpl); got != "base.append(self.body)\n" {
		t.Errorf("the statement was not written into the template: %q", got)
	}
	var noted bool
	for _, l := range plan.Report.Anchored {
		if strings.Contains(l.Detail, "body taken whole") {
			noted = true
		}
	}
	if !noted {
		t.Errorf("writing it was not reported: %+v", plan.Report.Anchored)
	}
	if _, err := build.PlanBuild(c, true); err != nil {
		t.Fatalf("second build: %v", err)
	}
	if again := readFile(t, tpl); again != "base.append(self.body)\n" {
		t.Errorf("a second build wrote again: %q", again)
	}

	// Where a section of ours can follow an upstream one, the neighbour rule still decides: the
	// setting answers having no neighbour, it does not switch that rule off.
	mustWrite(t, filepath.Join(dir, "me", "doc.md"), "# Title\n\n## Overview\n\nours\n\n## 用法\n\ny\n")
	mustWrite(t, tpl, "base.Overview.after(self.Overview)\n")
	if _, err := build.PlanBuild(c, true); err != nil {
		t.Fatalf("build with a placeable section: %v", err)
	}
	if got := readFile(t, tpl); strings.Contains(got, "self.body") {
		t.Errorf("the whole body was taken though a section could be placed:\n%s", got)
	}

	// And where some sections can be placed and others cannot, taking the body whole would write
	// the placeable ones twice — once in their inferred place and once in the body. 甲 follows a
	// section that is woven in, so it has a place; 乙 follows one that is replaced, so it has none.
	mustWrite(t, filepath.Join(dir, "me", "doc.md"),
		"# Title\n\n## Overview\n\nours\n\n## 甲\n\na\n\n## Usage\n\nmine\n\n## 乙\n\nb\n")
	mustWrite(t, tpl, "base.Overview.after(self.Overview)\nbase.Usage.replace(self.Usage, reason: \"ours entirely\")\n")
	_, err = build.PlanBuild(c, true)
	if err == nil {
		t.Error("a section with no neighbour should still stop the build when others have one")
	} else if !strings.Contains(err.Error(), "can't infer an anchor") {
		t.Errorf("unexpected error: %v", err)
	}
	if got := readFile(t, tpl); strings.Contains(got, "self.body") {
		t.Errorf("the body was taken whole while some sections had a place:\n%s", got)
	}
}

// The promise the language is named for: with our marks stripped, an insert-only product is
// upstream byte for byte. It was checked for markdown alone, so on every other kind of file the
// report asserted byte-identity it had never looked at.
func TestInsertOnlyIsCheckedForEveryType(t *testing.T) {
	dir := t.TempDir()
	setup := func(settings, up, ours, template string) *lang.Config {
		t.Helper()
		mustWrite(t, filepath.Join(dir, "loom.om"), settings)
		mustWrite(t, filepath.Join(dir, "up", "r.sh"), up)
		mustWrite(t, filepath.Join(dir, "me", "r.sh"), ours)
		mustWrite(t, filepath.Join(dir, "me", "r.sh.lm"), template)
		c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	const ours = "helper() {\n  echo ours\n}\n"
	const tpl = "base.main.after(self.helper)\n"

	// A correct insert leaves upstream alone, and the check says nothing.
	c := setup("base \"up\"\nself \"me\"\nmark shell \"# XS:BEGIN\" \"# XS:END\"\n",
		"#!/bin/sh\nmain() {\n  echo up\n}\n", ours, tpl)
	if _, err := build.PlanBuild(c, true); err != nil {
		t.Fatalf("a correct shell insert should build: %v", err)
	}

	// Marks that upstream's own text already contains: stripping them takes upstream content with
	// it, so the product no longer holds upstream whole. Before, for a shell file, nothing looked.
	c = setup("base \"up\"\nself \"me\"\nmark shell \"# SECTION\" \"# END\"\n",
		"#!/bin/sh\n# SECTION\nmain() {\n  echo up\n}\n", ours, tpl)
	_, err := build.PlanBuild(c, true)
	if err == nil {
		t.Fatal("a shell product that no longer holds upstream whole was accepted")
	}
	if !strings.Contains(err.Error(), "must not change a single upstream byte") {
		t.Errorf("unexpected error: %v", err)
	}

	// A value written into a toml key changes upstream's bytes on purpose, so such a template is
	// not insert-only and the check must not fire on it. Without that exception the tree below
	// fails, which is the whole reason the exception is there.
	mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\nmark toml \"# XS:BEGIN\" \"# XS:END\"\n")
	mustWrite(t, filepath.Join(dir, "up", "c.toml"), "description = \"Upstream.\"\nkept = 1\n")
	mustWrite(t, filepath.Join(dir, "me", "c.toml"), "description = \"Ours.\"\n")
	mustWrite(t, filepath.Join(dir, "me", "c.toml.lm"), "base.description.start(self.description)\n")
	c, err = lang.LoadConfig(filepath.Join(dir, "loom.om"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := build.PlanBuild(c, true); err != nil {
		t.Fatalf("writing a value is not an insert, and must not trip the byte check: %v", err)
	}
}

// Anchor completion worked for markdown only, so a function of ours that no template placed simply
// was not in the product — no error, no warning, and the report still said upstream was whole,
// which it was. Ours was the part that went missing.
func TestAnchorCompletionForShell(t *testing.T) {
	dir := t.TempDir()
	tpl := filepath.Join(dir, "me", "r.sh.lm")
	mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\nmark shell \"# XS:BEGIN\" \"# XS:END\"\n")
	mustWrite(t, filepath.Join(dir, "up", "r.sh"), "#!/bin/sh\nmain() {\n  echo up\n}\n")
	mustWrite(t, filepath.Join(dir, "me", "r.sh"), "one() {\n  echo a\n}\ntwo() {\n  echo b\n}\n")
	mustWrite(t, tpl, "base.main.after(self.one)\n")

	c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := build.PlanBuild(c, true)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	// two follows one in our file, and one is woven in, so two goes with it.
	if got := readFile(t, tpl); got != "base.main.after(self.one, \"two\")\n" {
		t.Errorf("the anchor was not completed: %q", got)
	}
	var product string
	for _, o := range plan.Outputs {
		if o.Rel == "r.sh" {
			product = string(o.Data)
		}
	}
	if !strings.Contains(product, "two() {") {
		t.Errorf("our second function is still not in the product:\n%s", product)
	}

	// Running again writes nothing more.
	if _, err := build.PlanBuild(c, true); err != nil {
		t.Fatalf("second build: %v", err)
	}
	if again := readFile(t, tpl); again != "base.main.after(self.one, \"two\")\n" {
		t.Errorf("a second build changed the template again: %q", again)
	}
}
