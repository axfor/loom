package loom_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/axfor/loom"
)

// treeRepo creates a repository for whole-tree builds. Keys of files are paths from the repository root;
// values starting with "#!" are written as executable.
func treeRepo(t *testing.T, settings string, files map[string]string) (*loom.Config, string) {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "loom.lm"), "base \"up\"\nself \"me\"\ntemplates \"t\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n"+settings)
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
	c, err := loom.LoadConfig(filepath.Join(dir, "loom.lm"))
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	return c, dir
}

func build(t *testing.T, c *loom.Config, out string) (*loom.Plan, error) {
	t.Helper()
	plan, err := loom.PlanBuild(c, true)
	if err != nil {
		return nil, err
	}
	return plan, loom.WriteBuild(c, plan, out)
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

	plan, err := build(t, c, out)
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

	plan, err = build(t, c, out)
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
	_, err := build(t, c, filepath.Join(dir, "out"))
	if err == nil || !strings.Contains(err.Error(), "shadows the upstream file") {
		t.Fatalf("must be refused, got: %v", err)
	}
	if strings.Contains(err.Error(), "same.md") {
		t.Errorf("an identical file loses nothing and must not be reported: %v", err)
	}
}

func makePatch(t *testing.T, dir, name, from, to string) {
	t.Helper()
	a, b := filepath.Join(t.TempDir(), "a"), filepath.Join(t.TempDir(), "b")
	mustWrite(t, a, from)
	mustWrite(t, b, to)
	out, _ := exec.Command("diff", "-u", a, b).Output()
	if len(out) == 0 {
		t.Fatal("diff printed nothing")
	}
	mustWrite(t, filepath.Join(dir, name), string(out))
}

// An upstream section missing from the product with nothing in the template to account for it is a build
// error; a drop accounts for it. Dropping a section that is still there, or dropping a section but leaving
// its subsections behind, is an error too.
func TestLostContent(t *testing.T) {
	up := "## Intro\n\nx\n\n## Old\n\ny\n\n## Keep\n\nz\n"
	c, dir := treeRepo(t, "", map[string]string{
		"up/a.md": up,
		"up/b.md": "## Parent\n\np\n\n### Child\n\nc\n\n## Next\n\nn\n",
	})
	makePatch(t, filepath.Join(dir, "t"), "a.md.diff", up, "## Intro\n\nx\n\n## Keep\n\nz\n")
	out := filepath.Join(dir, "out")

	write := func(p, s string) { mustWrite(t, filepath.Join(dir, "t", p), s) }
	write("a.md.lm", `base.patch("a.md.diff")`)
	_, err := build(t, c, out)
	if err == nil || !strings.Contains(err.Error(), `upstream content lost: in a.md, section "Old"`) {
		t.Fatalf("a section removed by the patch without a drop must be reported as lost, got: %v", err)
	}

	write("a.md.lm", "base.patch(\"a.md.diff\")\nbase.Old.drop(reason: \"does not apply\")\n")
	plan, err := build(t, c, out)
	if err != nil {
		t.Fatalf("the drop accounts for it, so no error, got: %v", err)
	}
	if len(plan.Report.Dropped) != 1 || plan.Report.Dropped[0].Path != "a.md § Old" {
		t.Errorf("wrong dropped entries: %+v", plan.Report.Dropped)
	}

	write("a.md.lm", "base.patch(\"a.md.diff\")\nbase.Old.drop(reason: \"does not apply\")\nbase.Keep.drop(reason: \"does not apply\")\n")
	if _, err := build(t, c, out); err == nil || !strings.Contains(err.Error(), `"Keep" is declared absent from the product but is still there`) {
		t.Errorf("a dropped section that is still there must be an error, got: %v", err)
	}

	write("a.md.lm", "")
	write("b.md.lm", `base.Parent.drop(reason: "does not apply")`)
	if _, err := build(t, c, out); err == nil || !strings.Contains(err.Error(), `its subsections "Child" are not accounted for`) {
		t.Errorf("dropping a section but leaving its subsection must be an error, got: %v", err)
	}
	write("b.md.lm", "base.Parent.drop(reason: \"does not apply\")\nbase.Child.drop(reason: \"does not apply\")\n")
	if _, err := build(t, c, out); err != nil {
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
	v, err := loom.LoadVars(filepath.Join(dir, "lm.e"))
	if err != nil {
		t.Fatal(err)
	}
	c.Vars = v
	plan, err := build(t, c, out)
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
	inner, err := loom.LoadVars(filepath.Join(dir, "inner.e"))
	if err != nil {
		t.Fatal(err)
	}
	c.Vars = inner
	if _, err := loom.PlanBuild(c, true); err == nil || !strings.Contains(err.Error(), "variable {{@npm}} is not defined in") || !strings.Contains(err.Error(), "doc.md.lm:1:") {
		t.Errorf("npm is missing from the -e file; must be an error pointing at the template, got: %v", err)
	}

	c.Vars = loom.NoVars()
	_, err = loom.PlanBuild(c, true)
	if err == nil || !strings.Contains(err.Error(), "install.md:3:7: variable {{@url}} is used, but there is no variables file") {
		t.Errorf("a variable used without a variables file must be an error at file:line:col (columns count characters, not bytes), got: %v", err)
	}

	mustWrite(t, filepath.Join(dir, "bad.e"), "url https://x\n")
	if _, err := loom.LoadVars(filepath.Join(dir, "bad.e")); err == nil || !strings.Contains(err.Error(), "bad.e:1:") {
		t.Errorf("a malformed variables file must report the line, got: %v", err)
	}
}

// A wrong output path would delete source: an output directory containing the source tree, or inside a layer, is refused.
func TestOutputGuard(t *testing.T) {
	c, dir := treeRepo(t, "", map[string]string{"up/a.md": "## A\n"})
	for _, out := range []string{dir, filepath.Dir(dir), filepath.Join(dir, "me", "out")} {
		plan, err := loom.PlanBuild(c, true)
		if err != nil {
			t.Fatal(err)
		}
		if err := loom.WriteBuild(c, plan, out); err == nil {
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
	if _, err := build(t, c, filepath.Join(dir, "out")); err == nil || !strings.Contains(err.Error(), "the mirror would overwrite it") {
		t.Errorf("a mirror colliding with an existing file must be an error, got: %v", err)
	}
}
