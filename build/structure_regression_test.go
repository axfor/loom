package build_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/axfor/loom/build"
)

// Structural edits change real headings, never heading-like text in a code fence.
func TestRestructureKeepsFencedContent(t *testing.T) {
	const up = "## Wrap\n\ntext\n\n### Child\n\n```markdown\n#### literal\n```\n\n#### Grandchild\n\ngrand\n\n## End\n\nend\n"
	for _, tc := range []struct{ name, tpl, want string }{
		{"unwrap", "up.Wrap.unwrap(reason: \"flatten\")\n", "text\n\n## Child\n\n```markdown\n#### literal\n```\n\n### Grandchild\n\ngrand\n\n## End\n\nend\n"},
		{"split", "up.Wrap.split(up.Wrap.Child)\n", "## Wrap\n\ntext\n\n## Child\n\n```markdown\n#### literal\n```\n\n### Grandchild\n\ngrand\n\n## End\n\nend\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, dir := treeRepo(t, "", map[string]string{"up/doc.md": up, "t/doc.md.lm": tc.tpl})
			out := filepath.Join(dir, "out")
			if _, err := buildTree(t, c, out); err != nil {
				t.Fatal(err)
			}
			if got := readFile(t, filepath.Join(out, "doc.md")); got != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

// Keep the selected node's identity throughout a path and every axis step.
func TestAxesKeepScopedNodes(t *testing.T) {
	const up = "## First\n\nf\n\n### Shared\n\none\n\n## Second\n\ns\n\n### Shared\n\ntwo\n\n### Last\n\nlast\n"
	for _, tc := range []struct{ tpl, before, after string }{
		{"up.Second.Shared.parent.demote()\n", "## Second\n", "### Second\n"},
		{"up.Second.children.first.demote()\n", "### Shared\n\ntwo", "#### Shared\n\ntwo"},
		{"up.Second.Shared.next.demote()\n", "### Last\n", "#### Last\n"},
		{"up.Second.Last.prev.demote()\n", "### Shared\n\ntwo", "#### Shared\n\ntwo"},
		{"up.sections[name == \"Shared\"].last.parent.demote()\n", "## Second\n", "### Second\n"},
	} {
		t.Run(strings.TrimSpace(tc.tpl), func(t *testing.T) {
			c, dir := treeRepo(t, "", map[string]string{"up/doc.md": up, "t/doc.md.lm": tc.tpl})
			out := filepath.Join(dir, "out")
			if _, err := buildTree(t, c, out); err != nil {
				t.Fatal(err)
			}
			want := strings.Replace(up, tc.before, tc.after, 1)
			if got := readFile(t, filepath.Join(out, "doc.md")); got != want {
				t.Errorf("got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func TestAxisKeepsAUniqueNameUnderRepeatedParents(t *testing.T) {
	const up = "## Parent\n\np\n\n### First\n\nf\n\n## Parent\n\np\n\n### Unique\n\nu\n"
	c, dir := treeRepo(t, "", map[string]string{
		"up/doc.md":   up,
		"t/doc.md.lm": "up.sections[level == 3].last.demote()\n",
	})
	out := filepath.Join(dir, "out")
	if _, err := buildTree(t, c, out); err != nil {
		t.Fatal(err)
	}
	if got, want := readFile(t, filepath.Join(out, "doc.md")), strings.Replace(up, "### Unique", "#### Unique", 1); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestJoinResolvesItsReceiver(t *testing.T) {
	const up = "## A\n\na\n\n## B\n\nb\n\n## C\n\nc\n"
	for _, receiver := range []string{"up.A.next", "up.sections[name == \"B\"].first"} {
		t.Run(receiver, func(t *testing.T) {
			c, dir := treeRepo(t, "", map[string]string{
				"up/doc.md":   up,
				"t/doc.md.lm": receiver + ".join(reason: \"combine B and C\")\n",
			})
			out := filepath.Join(dir, "out")
			plan, err := buildTree(t, c, out)
			if err != nil {
				t.Fatal(err)
			}
			want := "## A\n\na\n\n## B\n\nb\n\nc\n"
			if got := readFile(t, filepath.Join(out, "doc.md")); got != want {
				t.Errorf("got:\n%s\nwant:\n%s", got, want)
			}
			if len(plan.Report.Dropped) != 1 || plan.Report.Dropped[0].Path != "doc.md § C" {
				t.Errorf("report must name the heading actually removed: %+v", plan.Report.Dropped)
			}
		})
	}
}

func TestJoinInsideAView(t *testing.T) {
	const up = "prompt = \"\"\"\n## A\n\na\n\n## B\n\nb\n\n## C\n\nc\n\"\"\"\n"
	for _, receiver := range []string{"B", "A.next"} {
		t.Run(receiver, func(t *testing.T) {
			c, dir := treeRepo(t, "", map[string]string{
				"up/cmd.toml":   up,
				"t/cmd.toml.lm": "up.prompt.as(markdown)." + receiver + ".join(reason: \"combine\")\n",
			})
			out := filepath.Join(dir, "out")
			plan, err := buildTree(t, c, out)
			if err != nil {
				t.Fatal(err)
			}
			want := strings.Replace(up, "## C\n\n", "", 1)
			if got := readFile(t, filepath.Join(out, "cmd.toml")); got != want {
				t.Errorf("got:\n%s\nwant:\n%s", got, want)
			}
			if len(plan.Report.Dropped) != 1 || plan.Report.Dropped[0].Path != "cmd.toml § C" {
				t.Errorf("report must name the heading removed inside the view: %+v", plan.Report.Dropped)
			}
		})
	}
	for _, receiver := range []string{"C", "A.next.next"} {
		c, _ := treeRepo(t, "", map[string]string{
			"up/cmd.toml":   up,
			"t/cmd.toml.lm": "up.prompt.as(markdown)." + receiver + ".join(reason: \"combine\")\n",
		})
		if _, err := build.PlanBuild(c, false); err == nil || !strings.Contains(err.Error(), "has no section after it") {
			t.Errorf("%s: a join with no next section must fail, got %v", receiver, err)
		}
	}
}

func TestProjectionAnchorsIncludeUnselectedHeadings(t *testing.T) {
	for _, tc := range []struct{ name, up, want string }{
		{"unselected level", "### Same\n\nfirst\n\n## Same\n\nsecond\n", "[Same](#same-1)"},
		{"document title", "# Same\n\nfirst\n\n## Same\n\nsecond\n", "[Same](#same-1)"},
		{"fenced text", "```markdown\n# Same\n### Same\n```\n\n## Same\n\nsecond\n", "[Same](#same)"},
		{"slug collision", "### Same\n\nfirst\n\n### Same\n\nsecond\n\n## Same-1\n\nthird\n", "[Same-1](#same-1-1)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, dir := treeRepo(t, "", map[string]string{
				"up/doc.md":   tc.up,
				"t/doc.md.lm": "up.start(up.sections[level == 2].project(`[{name}](#{anchor})`))\n",
			})
			out := filepath.Join(dir, "out")
			if _, err := buildTree(t, c, out); err != nil {
				t.Fatal(err)
			}
			if got := readFile(t, filepath.Join(out, "doc.md")); !strings.Contains(got, tc.want) {
				t.Errorf("missing link %q in:\n%s", tc.want, got)
			}
		})
	}
}
