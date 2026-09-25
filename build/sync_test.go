package build_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axfor/loom/build"
	"github.com/axfor/loom/lang"
)

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const (
	upRun = "#!/bin/sh\nmain() {\n  echo up\n}\n\nhelp() {\n  echo help\n}\n"
	meRun = "#!/bin/sh\nmain() {\n  echo me\n}\n\nhelp() {\n  echo help\n}\n"
)

// A merge template builds our file as it is, still checked for upstream content it lost.
func TestMergeTemplate(t *testing.T) {
	c, dir := besideRepo(t, map[string]string{
		"up/run.sh": upRun,
		"me/run.sh": meRun,
		"me/run.lm": "base.merge(self)\n",
	})
	out := filepath.Join(dir, "out")
	if _, err := buildTree(t, c, out); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(out, "run.sh")); got != meRun {
		t.Errorf("product:\n%s", got)
	}

	mustWrite(t, filepath.Join(dir, "me", "run.sh"), "#!/bin/sh\nmain() {\n  echo me\n}\n")
	if _, err := build.PlanBuild(c, false); err == nil || !strings.Contains(err.Error(), "help") {
		t.Errorf("an upstream function our file lost without a drop must be an error, got: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "me", "run.lm"), "base.merge(self)\nbase.help.drop(reason: \"not ours\")\n")
	if _, err := build.PlanBuild(c, false); err != nil {
		t.Errorf("a drop accounts for it: %v", err)
	}

	for src, want := range map[string]string{
		"base.merge(self)\nbase.main.after(`x`)\n": "our file is the product",
		"base.merge(other)\n":                      "merge takes our file",
		"base.patch(\"run.diff\")\n":               "base.merge(self)",
	} {
		mustWrite(t, filepath.Join(dir, "me", "run.lm"), src)
		if _, err := build.PlanBuild(c, false); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: want an error mentioning %q, got: %v", src, want, err)
		}
	}
}

// lm sync mirrors the new upstream into the upstream layer and carries our edits onto it. Where both
// changed the same lines, our file gets conflict markers and the build refuses it.
func TestSync(t *testing.T) {
	c, dir := besideRepo(t, map[string]string{
		"up/run.sh":  upRun,
		"up/same.sh": "#!/bin/sh\necho same\n",
		"up/doc.md":  "## A\n",
		"up/old.txt": "old\n",
		"me/run.sh":  meRun,
		"me/run.lm":  "base.merge(self)\n",
		"me/same.sh": "#!/bin/sh\necho same\n",
		"me/same.lm": "base.merge(self)\n",
	})
	if err := os.Symlink("doc.md", filepath.Join(dir, "up", "link.md")); err != nil {
		t.Fatal(err)
	}
	next := t.TempDir()
	upFixed := strings.Replace(upRun, "echo help", "echo fixed help", 1)
	for p, s := range map[string]string{
		"run.sh":  upFixed,
		"same.sh": "#!/bin/sh\necho same, newer\n",
		"doc.md":  "## A\n",
		"new.txt": "new\n",
	} {
		mustWrite(t, filepath.Join(next, p), s)
	}
	if err := os.Symlink("doc.md", filepath.Join(next, "link.md")); err != nil {
		t.Fatal(err)
	}

	if _, err := build.Sync(c, filepath.Join(dir, "up")); err == nil {
		t.Error("syncing the upstream layer from itself must be refused")
	}
	r, err := build.Sync(c, next)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r.Added, " ") + " | " + strings.Join(r.Changed, " ") + " | " + strings.Join(r.Removed, " "); got != "new.txt | run.sh same.sh | old.txt" {
		t.Errorf("added | changed | removed: %s", got)
	}
	if got := strings.Join(r.Merged, " "); got != "me/run.sh me/same.sh" || len(r.Conflicts) != 0 {
		t.Errorf("merged %v, conflicts %v", r.Merged, r.Conflicts)
	}
	if got := readFile(t, filepath.Join(dir, "me", "run.sh")); got != strings.Replace(meRun, "echo help", "echo fixed help", 1) {
		t.Errorf("upstream's fix must be carried into our file, keeping our edit:\n%s", got)
	}
	if got := readFile(t, filepath.Join(dir, "me", "same.sh")); got != "#!/bin/sh\necho same, newer\n" {
		t.Errorf("a file we never edited takes upstream as it is:\n%s", got)
	}
	if link, err := os.Readlink(filepath.Join(dir, "up", "link.md")); err != nil || link != "doc.md" {
		t.Errorf("a symbolic link stays a link: %q %v", link, err)
	}
	if _, err := buildTree(t, c, filepath.Join(dir, "out")); err != nil {
		t.Errorf("the synced tree must build: %v", err)
	}

	// upstream now changes the very line we changed
	next2 := t.TempDir()
	if err := os.Symlink("doc.md", filepath.Join(next2, "link.md")); err != nil {
		t.Fatal(err)
	}
	for p, s := range map[string]string{
		"run.sh":  strings.Replace(upFixed, "echo up", "echo upstream", 1),
		"same.sh": "#!/bin/sh\necho same, newer\n",
		"doc.md":  "## A\n",
		"new.txt": "new\n",
	} {
		mustWrite(t, filepath.Join(next2, p), s)
	}
	r, err = build.Sync(c, next2)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.Conflicts, " ") != "me/run.sh" || !strings.Contains(readFile(t, filepath.Join(dir, "me", "run.sh")), "<<<<<<< ours") {
		t.Errorf("a conflict must leave markers in our file: %+v", r)
	}
	if _, err := build.PlanBuild(c, false); err == nil || !strings.Contains(err.Error(), "conflict markers") {
		t.Errorf("the build must refuse a file with conflict markers, got: %v", err)
	}
}

// Sync refuses a staging directory with no files: a failed copy would otherwise empty the upstream
// layer and only fail at the build. It also survives upstream turning a file into a directory.
func TestSyncRefusesEmptyAndFollowsShape(t *testing.T) {
	c, dir := besideRepo(t, map[string]string{
		"up/run.sh": upRun,
		"up/doc.md": "## A\n",
		"me/run.sh": meRun,
		"me/run.lm": "base.merge(self)\n",
	})
	empty := t.TempDir()
	if _, err := build.Sync(c, empty); err == nil || !strings.Contains(err.Error(), "no files") {
		t.Errorf("an empty upstream must be refused, got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "up", "run.sh")); err != nil {
		t.Errorf("the upstream layer must be untouched: %v", err)
	}

	// upstream turns doc.md into a directory and run.sh stays a file
	next := t.TempDir()
	mustWrite(t, filepath.Join(next, "run.sh"), upRun)
	mustWrite(t, filepath.Join(next, "doc.md", "inner.md"), "## B\n")
	if _, err := build.Sync(c, next); err != nil {
		t.Fatalf("a file that became a directory upstream: %v", err)
	}
	if st, err := os.Stat(filepath.Join(dir, "up", "doc.md")); err != nil || !st.IsDir() {
		t.Errorf("doc.md must now be a directory: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "up", "doc.md", "inner.md")); err != nil || string(b) != "## B\n" {
		t.Errorf("its file must be there: %q %v", b, err)
	}
}

// A conflict from the last sync must be resolved before the next one: the markers can only be
// resolved against the upstream they came from, and syncing replaces it.
func TestSyncRefusesUnresolvedConflict(t *testing.T) {
	c, dir := besideRepo(t, map[string]string{
		"up/run.sh": upRun,
		"me/run.sh": "#!/bin/sh\n<<<<<<< ours\nmain() {\n  echo me\n}\n=======\nmain() {\n  echo up\n}\n>>>>>>> upstream after sync\n\nhelp() {\n  echo help\n}\n",
		"me/run.lm": "base.merge(self)\n",
	})
	next := t.TempDir()
	mustWrite(t, filepath.Join(next, "run.sh"), strings.Replace(upRun, "echo help", "echo fixed help", 1))

	_, err := build.Sync(c, next)
	if err == nil || !strings.Contains(err.Error(), "conflict markers") || !strings.Contains(err.Error(), "me/run.sh") {
		t.Fatalf("want a refusal naming the file, got: %v", err)
	}
	if got := readFile(t, filepath.Join(dir, "up", "run.sh")); got != upRun {
		t.Errorf("the upstream layer must be untouched, so the conflict can still be resolved:\n%s", got)
	}
}

// Upstream removed the file our file is merged into: nobody can decide that but a person, so it is
// reported, our file is left alone, and the exit is non-zero.
func TestSyncReportsAFileUpstreamRemoved(t *testing.T) {
	c, dir := besideRepo(t, map[string]string{
		"up/run.sh":  upRun,
		"up/keep.md": "## A\n",
		"me/run.sh":  meRun,
		"me/run.lm":  "base.merge(self)\n",
	})
	next := t.TempDir()
	mustWrite(t, filepath.Join(next, "keep.md"), "## A\n")

	r, err := build.Sync(c, next)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.Gone, " ") != "me/run.sh" || len(r.Merged) != 0 || len(r.Conflicts) != 0 {
		t.Errorf("want our file reported as gone, got: %+v", r)
	}
	if got := readFile(t, filepath.Join(dir, "me", "run.sh")); got != meRun {
		t.Errorf("our file must be left as it is:\n%s", got)
	}
}

// A rename followed into a template is written the way the tree reads names. Loom 2 takes an
// unquoted name as written, so the underscore form Loom 1 writes would point at a heading called
// "New_Name" — a template sync itself had just broken.
func TestSyncWritesTheNameByTheVersion(t *testing.T) {
	for _, c := range []struct{ decl, want string }{
		{"", "base.New_Name.after(self.Ours)\n"},
		{"loom \"2.0\"\n", "base.\"New Name\".after(self.Ours)\n"},
	} {
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n"+c.decl)
		mustWrite(t, filepath.Join(dir, "up", "doc.md"), "# T\n\n## Old Name\n\nbody text\n")
		mustWrite(t, filepath.Join(dir, "me", "doc.md"), "## Ours\n\no\n")
		mustWrite(t, filepath.Join(dir, "me", "doc.md.lm"), "base.\"Old Name\".after(self.Ours)\n")
		next := t.TempDir()
		mustWrite(t, filepath.Join(next, "doc.md"), "# T\n\n## New Name\n\nbody text\n")
		cfg, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := build.Sync(cfg, next); err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, filepath.Join(dir, "me", "doc.md.lm")); got != c.want {
			t.Errorf("%q: the rename is written as\n  %s  want\n  %s", c.decl, got, c.want)
		}
		if _, err := buildTree(t, cfg, filepath.Join(dir, "out")); err != nil {
			t.Errorf("%q: the followed template must build: %v", c.decl, err)
		}
	}
}
