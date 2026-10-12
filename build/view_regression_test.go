package build_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/axfor/loom/build"
)

func TestViewChecksLostSubsections(t *testing.T) {
	const up = "prompt = \"\"\"\n## A\n\na\n\n### Child\n\nchild\n\n## B\n\nb\n\"\"\"\n"
	c, _ := treeRepo(t, "", map[string]string{
		"up/cmd.toml":   up,
		"t/cmd.toml.lm": "up.prompt.as(markdown).A.drop(reason: \"remove A\")\n",
	})
	if _, err := build.PlanBuild(c, false); err == nil || !strings.Contains(err.Error(), "subsections") {
		t.Errorf("a view must reject orphaned subsections too: %v", err)
	}
}

func TestViewReportsDroppedBytes(t *testing.T) {
	const up = "prompt = \"\"\"\n## A\n\na\n\n## B\n\nb\n\"\"\"\nA = \"unchanged\"\n"
	c, _ := treeRepo(t, "", map[string]string{
		"up/cmd.toml":   up,
		"t/cmd.toml.lm": "up.prompt.as(markdown).A.drop(reason: \"remove A\")\n",
	})
	p, err := build.PlanBuild(c, false)
	if err != nil {
		t.Fatal(err)
	}
	if p.Report.UpBytes != len(up) || p.Report.VerifiedBytes != len(up)-len("## A\n\na\n\n") {
		t.Errorf("count the file once and subtract the inner section: %+v", p.Report)
	}
}

func TestViewChecksAllStatementsTogether(t *testing.T) {
	const up = "prompt = \"\"\"\n## A\n\na\n\n### Child\n\nchild\n\n## B\n\nb\n\"\"\"\n"
	c, dir := treeRepo(t, "", map[string]string{
		"up/cmd.toml":   up,
		"t/cmd.toml.lm": "up.prompt.as(markdown).A.drop(reason: \"remove A\")\nif up.has.prompt {\n up.prompt.as(markdown).Child.drop(reason: \"remove its child too\")\n}\n",
	})
	out := filepath.Join(dir, "out")
	if _, err := buildTree(t, c, out); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(out, "cmd.toml")); strings.Contains(got, "Child") || strings.Contains(got, "## A") {
		t.Errorf("both resolved drops must reach the output:\n%s", got)
	}
}

func TestCaptureInsideAView(t *testing.T) {
	const up = "prompt = \"\"\"\n## A\n\na\n\n## B\n\nb\n\"\"\"\n"
	for _, prefix := range []string{"", "up.prompt.as(markdown).A.after(`first`)\n"} {
		for _, target := range []string{"A", "Missing", "MissingKey"} {
			t.Run(prefix+target, func(t *testing.T) {
				key := "prompt"
				if target == "MissingKey" {
					key = "absent"
				}
				tpl := prefix + "ok = up." + key + ".as(markdown)." + target + ".after(`success`)\nif !ok {\n up.prompt.as(markdown).B.after(`fallback`)\n}\n"
				c, dir := treeRepo(t, "mark toml \"# B\" \"# E\"\n", map[string]string{
					"up/cmd.toml":   up,
					"t/cmd.toml.lm": tpl,
				})
				out := filepath.Join(dir, "out")
				if _, err := buildTree(t, c, out); err != nil {
					t.Fatal(err)
				}
				got := readFile(t, filepath.Join(out, "cmd.toml"))
				if prefix != "" && !strings.Contains(got, "first") {
					t.Errorf("capturing the next operation must keep the previous insertion:\n%s", got)
				}
				if strings.Contains(got, "success") != (target == "A") || strings.Contains(got, "fallback") != (target != "A") {
					t.Errorf("the captured result must describe the inner target:\n%s", got)
				}
			})
		}
	}
}

func TestSwapAndSplitKeepReceiverAxes(t *testing.T) {
	const up = "## A\n\na\n\n## B\n\nb\n\n### Child\n\nc\n\n## C\n\nz\n"
	for _, tc := range []struct{ tpl, want string }{
		{"up.A.next.swap(up.C)\n", "## A | ## C | ### Child | ## B"},
		{"up.A.next.swap(up.A)\n", "## B | ## A | ### Child | ## C"},
		{"up.A.next.move(before: up.A)\n", "## B | ## A | ### Child | ## C"},
		{"up.A.next.split(up.B.Child)\n", "## A | ## B | ## Child | ## C"},
		{"up.A.next.split(up.B.Child, \"Part\")\n", "## A | ## B | ## Part | ### Child | ## C"},
	} {
		c, dir := treeRepo(t, "", map[string]string{"up/doc.md": up, "t/doc.md.lm": tc.tpl})
		out := filepath.Join(dir, "out")
		if _, err := buildTree(t, c, out); err != nil {
			t.Errorf("%s: %v", tc.tpl, err)
			continue
		}
		if got := headings(readFile(t, filepath.Join(out, "doc.md"))); got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.tpl, got, tc.want)
		}
	}
}

func TestViewStatementsKeepSourceOrderAcrossBranches(t *testing.T) {
	c, dir := treeRepo(t, "mark toml \"# B\" \"# E\"\n", map[string]string{
		"up/cmd.toml":   "prompt = \"\"\"\n## A\n\na\n\"\"\"\n",
		"t/cmd.toml.lm": "up.prompt.as(markdown).A.after(`## First`)\nif up.has.prompt {\n up.prompt.as(markdown).A.after(`## Second`)\n}\n",
	})
	out := filepath.Join(dir, "out")
	if _, err := buildTree(t, c, out); err != nil {
		t.Fatal(err)
	}
	if got := headings(readFile(t, filepath.Join(out, "cmd.toml"))); got != "## A | # B | ## First | # E | # B | ## Second | # E" {
		t.Errorf("insertions at one upstream position must keep template order: %s", got)
	}
}
