package build_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axfor/loom/build"
	"github.com/axfor/loom/lang"
)

// besideRepo writes a repository whose templates sit next to our files (no templates setting).
func besideRepo(t *testing.T, files map[string]string) (*lang.Config, string) {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n")
	for p, s := range files {
		mustWrite(t, filepath.Join(dir, filepath.FromSlash(p)), s)
	}
	c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
	if err != nil {
		t.Fatal(err)
	}
	return c, dir
}

// A template may leave out the product file's extension: SKILL.lm builds SKILL.md, run.lm merges
// into run.sh, and a file without an extension is named as it is.
func TestShortTemplateNames(t *testing.T) {
	c, dir := besideRepo(t, map[string]string{
		"up/skills/t/SKILL.md": "## Overview\n\nup\n",
		"me/skills/t/SKILL.md": "## Ours\n\nme\n",
		"me/skills/t/SKILL.lm": "base.Overview.after(\"Ours\")\n",
		"up/run.sh":            "#!/bin/sh\necho up\n",
		"me/run.sh":            "#!/bin/sh\necho me\n",
		"me/run.lm":            "base.merge(self)\n",
		"up/LICENSE":           "up\n",
		"me/LICENSE":           "me\n",
		"me/LICENSE.lm":        "base.replace(self, reason: \"ours\")\n",
		// run.lm next to run.sh does not make an import without the extension ambiguous
		"up/guide.md":    "## Intro\n\nup\n",
		"me/guide.md.lm": "import \"/run\"\nbase.Intro.after(`see run`)\n",
	})
	out := filepath.Join(dir, "out")
	if _, err := buildTree(t, c, out); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(listTree(t, out), " "); got != "LICENSE guide.md run.sh skills/t/SKILL.md" {
		t.Errorf("products: got %s", got)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "skills/t/SKILL.md")); !strings.Contains(string(b), "## Ours") {
		t.Errorf("SKILL.lm did not weave SKILL.md:\n%s", b)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "run.sh")); string(b) != "#!/bin/sh\necho me\n" {
		t.Errorf("run.lm did not merge into run.sh:\n%s", b)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "LICENSE")); string(b) != "me\n" {
		t.Errorf("LICENSE.lm did not replace LICENSE:\n%s", b)
	}
}

// When several files could be meant, the short name is an error that asks for the full one;
// two templates for the same product are an error too.
func TestShortTemplateNameConflicts(t *testing.T) {
	c, _ := besideRepo(t, map[string]string{
		"up/notes.md":  "## A\n",
		"me/notes.yml": "a: 1\n",
		"me/notes.lm":  "base.append(`x`)\n",
	})
	_, err := build.PlanBuild(c, false)
	if err == nil || !strings.Contains(err.Error(), "notes.md, notes.yml") || !strings.Contains(err.Error(), "notes.md.lm") {
		t.Errorf("an ambiguous short name must name the candidates and the full name, got: %v", err)
	}

	// names that differ only in case are one file on macOS and Windows, so they are ambiguous too
	c, _ = besideRepo(t, map[string]string{
		"up/GUIDE.md": "## A\n",
		"me/guide.sh": "echo\n",
		"me/guide.lm": "base.append(`x`)\n",
	})
	_, err = build.PlanBuild(c, false)
	if err == nil || !strings.Contains(err.Error(), "GUIDE.md, guide.sh") {
		t.Errorf("a short name matching files that differ in case must be ambiguous, got: %v", err)
	}

	c, _ = besideRepo(t, map[string]string{
		"up/doc.md":    "## A\n\nup\n",
		"me/doc.md":    "## B\n\nme\n",
		"me/doc.lm":    "base.A.after(\"B\")\n",
		"me/doc.md.lm": "base.A.before(\"B\")\n",
	})
	_, err = build.PlanBuild(c, false)
	if err == nil || !strings.Contains(err.Error(), "doc.lm") || !strings.Contains(err.Error(), "doc.md.lm") || !strings.Contains(err.Error(), "both build doc.md") {
		t.Errorf("two templates for one product must be an error, got: %v", err)
	}
}

// A template reached through a symbolic link builds the same product as through its real path.
func TestTemplateThroughSymlink(t *testing.T) {
	c, dir := besideRepo(t, map[string]string{
		"up/a.md": "## A\n\nup\n",
		"me/a.md": "## B\n\nme\n",
		"me/a.lm": "base.A.after(\"B\")\n",
	})
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	got, err := lang.TargetOf(c, filepath.Join(link, "me", "a.lm"))
	if err != nil || got != "a.md" {
		t.Errorf("got %q, %v", got, err)
	}
}

// A frontmatter key of ours that does not reach the product is an error, like a section of ours no
// statement places: without set, start or append it would disappear with nothing to say so.
func TestOurFrontmatterReachesProduct(t *testing.T) {
	c, dir := besideRepo(t, map[string]string{
		"up/a.md": "---\nname: a\ndescription: Up.\n---\n\n## A\n\nup\n",
		"me/a.md": "---\nname: a\ndescription: Ours.\nargument-hint: <file>\n---\n\n## B\n\nme\n",
	})
	tpl := filepath.Join(dir, "me", "a.lm")
	for src, want := range map[string]string{
		"base.A.after(\"B\")\n": `"description"`,
		"base.frontmatter.description.start(self.frontmatter.description)\nbase.A.after(\"B\")\n":                                         `"argument-hint"`,
		"base.frontmatter.set(self.frontmatter)\nbase.frontmatter.description.start(self.frontmatter.description)\nbase.A.after(\"B\")\n": "",
		"base.frontmatter.set(self.frontmatter)\nbase.A.after(\"B\")\n":                                                                   "",
	} {
		mustWrite(t, tpl, src)
		_, err := build.PlanBuild(c, false)
		switch {
		case want == "" && err != nil:
			t.Errorf("%q: our frontmatter is all in the product, got: %v", src, err)
		case want != "" && (err == nil || !strings.Contains(err.Error(), "our frontmatter key "+want)):
			t.Errorf("%q: want an error naming %s, got: %v", src, want, err)
		}
	}
}
