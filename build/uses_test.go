package build_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axfor/loom/build"
	"github.com/axfor/loom/lang"
)

// The build already resolves every anchor; this only turns the answer round. What it must get
// right is the direction: given a piece of upstream, everything that names it — including the
// same template naming it twice, which is invisible from the other side.
func TestUses(t *testing.T) {
	dir := t.TempDir()
	w := func(rel, text string) { mustWrite(t, filepath.Join(dir, rel), text) }
	w("loom.om", "base \"up\"\nself \"me\"\n")
	w("up/doc.md", "## Overview\n\no\n\n## Install\n\ni\n")
	w("up/other.md", "## Only\n\nx\n")
	w("me/doc.md", "## A\n\na\n\n## B\n\nb\n")
	w("me/doc.lm", "base.Overview.after(self.A)\nbase.Overview.before(self.B)\n")
	w("me/other.md", "## C\n\nc\n")
	w("me/other.lm", "base.replace(self, reason: \"all ours\")\n")

	c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := build.Uses(c, &out, ""); err != nil {
		t.Fatalf("uses: %v", err)
	}
	got := out.String()

	if !strings.Contains(got, "doc.md") || !strings.Contains(got, "other.md") {
		t.Errorf("not every upstream file that is named appears:\n%s", got)
	}
	// Overview is named twice by one template, which is the case the forward view hides.
	var line string
	for _, l := range strings.Split(got, "\n") {
		if strings.Contains(l, "Overview") {
			line = l
		}
	}
	if strings.Count(line, "doc.lm") != 2 {
		t.Errorf("both places naming Overview should be listed: %q", line)
	}
	// Install is named by nobody, so it is not in the answer.
	if strings.Contains(got, "Install") {
		t.Errorf("a node nothing names should not be listed:\n%s", got)
	}
	// A whole-file replace names no node but plainly depends on the file.
	if !strings.Contains(got, "(the whole file)") {
		t.Errorf("a whole-file dependency was not reported:\n%s", got)
	}

	// The pattern narrows by upstream path.
	out.Reset()
	if err := build.Uses(c, &out, "other"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "Overview") || !strings.Contains(out.String(), "other.md") {
		t.Errorf("the pattern did not narrow to one file:\n%s", out.String())
	}

	out.Reset()
	if err := build.Uses(c, &out, "nothing-like-this"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "nothing names") {
		t.Errorf("a pattern matching nothing should say so: %q", out.String())
	}
}

// A template depends on more than the nodes it changes: where a move goes, and what an if asks
// about. Left out, lm uses said nothing named Usage, and removing Usage upstream would have
// sent the question the other way in silence.
func TestUsesIncludesTargetsAndQuestions(t *testing.T) {
	dir := t.TempDir()
	w := func(rel, text string) { mustWrite(t, filepath.Join(dir, rel), text) }
	w("loom.om", "base \"up\"\nself \"me\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n")
	w("up/doc.md", "# T\n\n## Overview\n\no\n\n## Install\n\ni\n\n## Set Up\n\ns\n")
	w("me/doc.md", "## A\n\na\n")
	w("me/doc.lm", "if base.has.Set_Up {\n    base.Overview.after(self.A)\n}\nbase.Install.move(after: base.\"Set Up\")\n")
	c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := build.Uses(c, &out, ""); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "Set Up") || !strings.Contains(got, "doc.lm:1:13") || !strings.Contains(got, "doc.lm:4:31") {
		t.Errorf("the question and the move target both name Set Up:\n%s", got)
	}
	out.Reset()
	if err := build.ListAnchors(c, &out); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(out.String(), "Set Up"); n != 2 {
		t.Errorf("lm anchors lists both, found %d:\n%s", n, out.String())
	}
}
