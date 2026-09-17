package loom_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axfor/loom"
)

// completeOnce builds with anchor completion, returns the rewritten template, and builds again:
// what completion writes must itself build, and must not change on a second build.
func completeOnce(t *testing.T, c *loom.Config, dir, tpl string) string {
	t.Helper()
	p := filepath.Join(dir, "t", tpl)
	if _, err := loom.PlanBuild(c, true); err != nil {
		t.Fatalf("build with anchor completion: %v", err)
	}
	got, _ := os.ReadFile(p)
	plan, err := loom.PlanBuild(c, false)
	if err != nil {
		t.Fatalf("the completed template does not build:\n%s\n%v", got, err)
	}
	if len(plan.Report.Anchored) != 0 {
		t.Errorf("a second build completed anchors again: %+v", plan.Report.Anchored)
	}
	return string(got)
}

// A section of ours no statement mentions is placed next to its neighbour, in the same call.
func TestAnchorCompletionShortStaysOnOneLine(t *testing.T) {
	c, dir := treeRepo(t, "", map[string]string{
		"up/a.md":   "## Intro\n\nx\n\n## Setup\n\ny\n",
		"me/a.md":   "## Ours\n\na\n\n## More\n\nb\n",
		"t/a.md.lm": "base.Intro.after(\"Ours\")\n",
	})
	if got := completeOnce(t, c, dir, "a.md.lm"); got != "base.Intro.after(\"Ours\", \"More\")\n" {
		t.Errorf("got:\n%s", got)
	}
}

// A section path is easy to misread inside one long line, so the call becomes a block.
func TestAnchorCompletionPathMakesABlock(t *testing.T) {
	c, dir := treeRepo(t, "", map[string]string{
		"up/a.md":   "## Ex 1\n\nx\n\n## Ex 2\n\ny\n",
		"me/a.md":   "## E1\n\na\n\n### Phase\n\nb\n\n## E2\n\nc\n\n### Phase\n\nd\n",
		"t/a.md.lm": "base.\"Ex 1\".after(\"E1\")\nbase.\"Ex 2\".after(\"E2\")\n",
	})
	want := "base.\"Ex 1\".after{\n    \"E1\"\n    self.\"E1\".\"Phase\"\n}\n" +
		"base.\"Ex 2\".after{\n    \"E2\"\n    self.\"E2\".\"Phase\"\n}\n"
	if got := completeOnce(t, c, dir, "a.md.lm"); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// A call that would get too long becomes a block, one argument per line.
func TestAnchorCompletionLongLineMakesABlock(t *testing.T) {
	long := func(n string) string { return "A section title that is rather long, number " + n }
	c, dir := treeRepo(t, "", map[string]string{
		"up/a.md":   "## Intro\n\nx\n",
		"me/a.md":   "## " + long("1") + "\n\na\n\n## " + long("2") + "\n\nb\n\n## " + long("3") + "\n\nc\n",
		"t/a.md.lm": "base.Intro.after(\"" + long("1") + "\")\n",
	})
	got := completeOnce(t, c, dir, "a.md.lm")
	want := "base.Intro.after{\n    \"" + long("1") + "\"\n    \"" + long("2") + "\"\n    \"" + long("3") + "\"\n}\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// A call already written as a block gets one more line, with the same indentation.
func TestAnchorCompletionIntoBlock(t *testing.T) {
	c, dir := treeRepo(t, "", map[string]string{
		"up/a.md":   "## Intro\n\nx\n",
		"me/a.md":   "## Ours\n\na\n\n## More\n\nb\n",
		"t/a.md.lm": "base.Intro.after{\n    \"Ours\"\n}\n",
	})
	if got := completeOnce(t, c, dir, "a.md.lm"); got != "base.Intro.after{\n    \"Ours\"\n    \"More\"\n}\n" {
		t.Errorf("got:\n%s", got)
	}
}

// check reports a missing anchor and writes nothing; with no neighbour to follow, nothing is guessed.
func TestAnchorCompletionCheckAndNoNeighbour(t *testing.T) {
	c, dir := treeRepo(t, "", map[string]string{
		"up/a.md":   "## Intro\n\nx\n",
		"me/a.md":   "## Ours\n\na\n\n## More\n\nb\n",
		"t/a.md.lm": "base.Intro.after(\"Ours\")\n",
	})
	p := filepath.Join(dir, "t", "a.md.lm")
	if _, err := loom.PlanBuild(c, false); err == nil || !strings.Contains(err.Error(), "not woven into the product") {
		t.Errorf("check must report the section no statement places, got: %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "base.Intro.after(\"Ours\")\n" {
		t.Errorf("check must not write the template, got:\n%s", b)
	}

	mustWrite(t, p, "base.append(`only a literal`)\n")
	if _, err := loom.PlanBuild(c, true); err == nil || !strings.Contains(err.Error(), "can't infer an anchor") {
		t.Errorf("no section to follow must be an error, got: %v", err)
	}
}
