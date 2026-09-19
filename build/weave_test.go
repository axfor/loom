package build_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/axfor/loom/build"
	"github.com/axfor/loom/lang"
)

const fixture = "testdata/repo"

func load(t *testing.T) *lang.Config {
	t.Helper()
	c, err := lang.LoadConfig(filepath.Join(fixture, lang.ConfigName))
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	return c
}

func weave(t *testing.T, c *lang.Config, tpl string) string {
	t.Helper()
	tm, err := lang.LoadTemplate(c, filepath.Join(fixture, "templates", tpl))
	if err != nil {
		t.Fatalf("%s: parse failed: %v", tpl, err)
	}
	out, err := build.Weave(c, tm)
	if err != nil {
		t.Fatalf("%s: weave failed: %v", tpl, err)
	}
	return out
}

// The product must match the golden file byte for byte: weaving is deterministic, the same sources always weave the same cloth.
func TestWeaveGolden(t *testing.T) {
	c := load(t)
	for _, cse := range []struct{ tpl, golden string }{
		{"doc.lm", "doc.md"},
		{"run.lm", "run.sh"},
	} {
		got := weave(t, c, cse.tpl)
		want, err := os.ReadFile(filepath.Join(fixture, "golden", cse.golden))
		if err != nil {
			t.Fatal(err)
		}
		if got != string(want) {
			t.Errorf("%s differs from golden\n--- got ---\n%s\n--- want ---\n%s", cse.tpl, got, want)
		}
	}
}

// Why the language exists: **pull the weft out and the warp is still there**.
// This is not a sentence in the docs; it is a property that can be checked mechanically.
func TestWarpSurvivesStrip(t *testing.T) {
	c := load(t)
	got := weave(t, c, "doc.lm")
	stripped := stripMarks(got, "<!-- MINE:BEGIN -->", "<!-- MINE:END -->")

	up, err := os.ReadFile(filepath.Join(fixture, "upstream", "doc.md"))
	if err != nil {
		t.Fatal(err)
	}
	// the frontmatter is replaced on purpose (frontmatter.set) and is not inside marks, so compare the body only
	if body(stripped) != body(string(up)) {
		t.Errorf("with the weft stripped, the body differs from the warp\n--- stripped ---\n%q\n--- warp ---\n%q",
			body(stripped), body(string(up)))
	}
}

func stripMarks(s, begin, end string) string {
	var out []string
	skip := false
	for _, l := range strings.Split(s, "\n") {
		switch {
		case l == begin:
			skip = true
		case l == end:
			skip = false
		case !skip:
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

func body(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return s
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return strings.TrimRight(strings.Join(lines[i+1:], "\n"), "\n")
		}
	}
	return s
}

// Errors must be loud and say which kind they are: a product woven in the wrong place looks perfectly normal.
func TestErrorsAreLoud(t *testing.T) {
	c, dir := objRepo(t, nil)
	for _, cse := range []struct{ name, target, src, want string }{
		{"upstream anchor not found", "doc.md", `base."No Such Section".after("Appendix")`, "anchor not found"},
		{"our section not found", "doc.md", `base.Overview.after("No such section of ours")`, "anchor not found"},
		{"wrong node kind", "doc.md", `base.key("Overview").after("Appendix")`, "has no `key(...)` node"},
		{"missing base", "nope.md", `base.append("Appendix")`, "has no nope.md"},
	} {
		t.Run(cse.name, func(t *testing.T) {
			_, err := weaveObj(t, c, dir, cse.target, cse.src)
			if err == nil {
				t.Fatal("expected an error, but weaving succeeded — exactly the failure this language exists to prevent")
			}
			if !strings.Contains(err.Error(), cse.want) {
				t.Errorf("wrong error\ngot: %v\nwant it to contain: %s", err, cse.want)
			}
		})
	}
}

// An anchor matching several places is an error; never take the first.
func TestAmbiguousAnchorRefuses(t *testing.T) {
	c, dir := objRepo(t, map[string]string{
		"upstream/a.md": "## Same\n\nx\n\n## Same\n\ny\n",
		"mine/a.md":     "## Mine\n\nz\n",
	})
	if _, err := weaveObj(t, c, dir, "a.md", `base.Same.after("Mine")`); err == nil || !strings.Contains(err.Error(), "matches 2 places") {
		t.Fatalf("a duplicate anchor must be refused, got: %v", err)
	}
}

// Statement order matters: of two insertions at the same anchor, the one written first comes first.
func TestStatementOrderIsSourceOrder(t *testing.T) {
	c, dir := objRepo(t, nil)
	out, err := weaveObj(t, c, dir, "doc.md", "base.Overview.after(\"Appendix\")\nbase.Overview.after(\"Where this fits\")\n")
	if err != nil {
		t.Fatal(err)
	}
	i, j := strings.Index(out, "## Appendix"), strings.Index(out, "## Where this fits")
	if i < 0 || j < 0 || i > j {
		t.Errorf("insertion order does not follow the template: Appendix@%d Where this fits@%d", i, j)
	}
}

// list must report metadata from the same parse that weaving uses: outside tools rely on it, and
// "every tool parses templates again on its own" is exactly what this command exists to remove.
func TestDescribeMatchesWeave(t *testing.T) {
	c := load(t)
	i, err := build.Describe(c, filepath.Join(fixture, "templates", "doc.lm"))
	if err != nil {
		t.Fatal(err)
	}
	if i.Target != "doc.md" || i.Type != "markdown" || i.From != "base" {
		t.Errorf("wrong metadata: %+v", i)
	}
	if i.Path != "doc.md" {
		t.Errorf("the base path should default to the product path, got %q", i.Path)
	}
	if len(i.Anchors) != 1 || i.Anchors[0].Kind != "heading" || i.Anchors[0].Anchor != "Overview" {
		t.Errorf("anchors not all reported: %+v", i.Anchors)
	}
	// frontmatter.description.start + after + append
	if len(i.Inserts) != 3 {
		t.Errorf("expected 3 content sources, got %d: %+v", len(i.Inserts), i.Inserts)
	}
}

func mustWrite(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(strings.TrimLeft(s, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
}

// yaml, end to end: a product woven by key path, and the recursion rule that the
// whole design rests on — a key whose value is a document, opened as that document
// and woven section by section.
func TestWeaveYaml(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, text string) {
		t.Helper()
		mustWrite(t, filepath.Join(dir, rel), text)
	}
	write("loom.om", "base \"up\"\nself \"me\"\n")
	write("up/ci.yaml", "name: build\n\njobs:\n  test:\n    runs-on: ubuntu-latest\n")
	write("me/ci.yaml", "  lint:\n    runs-on: ubuntu-latest\n")
	write("me/ci.lm", "base.jobs.test.after(self.lint)\n")

	c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := lang.LoadTemplate(c, filepath.Join(dir, "me", "ci.lm"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := build.Weave(c, tpl)
	if err != nil {
		t.Fatalf("weave: %v", err)
	}
	// Inserted content is separated by a blank line, as it is in every other type;
	// a blank line between two entries of a mapping is still yaml.
	want := "name: build\n\njobs:\n  test:\n    runs-on: ubuntu-latest\n\n  lint:\n    runs-on: ubuntu-latest\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}

	// The quoted whole path means the same thing, for a segment a dot cannot spell.
	write("me/ci.lm", "base.\"jobs.test\".after(self.lint)\n")
	tpl2, err := lang.LoadTemplate(c, filepath.Join(dir, "me", "ci.lm"))
	if err != nil {
		t.Fatal(err)
	}
	quoted, err := build.Weave(c, tpl2)
	if err != nil {
		t.Fatalf("weave (quoted path): %v", err)
	}
	if quoted != got {
		t.Errorf("the quoted path wove something else:\n%q", quoted)
	}
}

// A markdown document inside a yaml block scalar has sections, and weaving one in
// re-indents it back under its key. This is L1.2 — a part opens as a document of
// its own — on the one pair of types where it had never been possible.
func TestWeaveYamlValueAsMarkdown(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, text string) {
		t.Helper()
		mustWrite(t, filepath.Join(dir, rel), text)
	}
	write("loom.om", "base \"up\"\nself \"me\"\n")
	write("up/skill.yaml", "prompt: |\n  ## Overview\n\n  up\n\n  ## Process\n\n  p\n")
	write("me/skill.yaml", "prompt: |\n  ## Ours\n\n  mine with `code`\n")
	write("me/skill.lm", "base.prompt.as(markdown).Overview.after(\"Ours\")\n")

	c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := lang.LoadTemplate(c, filepath.Join(dir, "me", "skill.lm"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := build.Weave(c, tpl)
	if err != nil {
		t.Fatalf("weave: %v", err)
	}
	for _, want := range []string{"  ## Ours", "  mine with `code`", "  ## Process"} {
		if !strings.Contains(got, want) {
			t.Errorf("product is missing %q:\n%s", want, got)
		}
	}
	// Upstream's own sections are still there, still indented under the key.
	if !strings.Contains(got, "  ## Overview") {
		t.Errorf("upstream's section was lost:\n%s", got)
	}
}

// move: a node changes where it is, never what it is. The promise is stronger than the
// one an insertion makes about upstream, and the build checks it rather than trusting
// the engine that did the moving.
func TestWeaveMove(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, text string) {
		t.Helper()
		mustWrite(t, filepath.Join(dir, rel), text)
	}
	write("loom.om", "base \"up\"\nself \"me\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n")
	write("up/doc.md", "## A\n\na\n\n## Troubleshooting\n\nt with `code`\n\n## B\n\nb\n")

	weave := func(tpl string) (string, error) {
		write("me/doc.lm", tpl)
		c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
		if err != nil {
			t.Fatal(err)
		}
		tm, err := lang.LoadTemplate(c, filepath.Join(dir, "me", "doc.lm"))
		if err != nil {
			return "", err
		}
		return build.Weave(c, tm)
	}

	got, err := weave("base.Troubleshooting.move(after: base.B)\n")
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	want := "## A\n\na\n\n## B\n\nb\n\n## Troubleshooting\n\nt with `code`\n"
	if got != want {
		t.Errorf("after:\ngot  %q\nwant %q", got, want)
	}

	got, err = weave("base.Troubleshooting.move(before: base.A)\n")
	if err != nil {
		t.Fatalf("move before: %v", err)
	}
	want = "## Troubleshooting\n\nt with `code`\n\n## A\n\na\n\n## B\n\nb\n"
	if got != want {
		t.Errorf("before:\ngot  %q\nwant %q", got, want)
	}

	// Every upstream line is still there — a move loses nothing.
	for _, line := range []string{"## A", "## B", "## Troubleshooting", "t with `code`"} {
		if !strings.Contains(got, line) {
			t.Errorf("a move lost %q", line)
		}
	}
}

func TestMoveRefusals(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, text string) {
		t.Helper()
		mustWrite(t, filepath.Join(dir, rel), text)
	}
	write("loom.om", "base \"up\"\nself \"me\"\n")
	write("up/doc.md", "## A\n\na\n\n### A1\n\nsub\n\n## B\n\nb\n")
	write("me/doc.md", "")

	for _, c := range []struct{ tpl, want string }{
		{"base.A.move(after: base.A)\n", "different node"},
		{"base.A.move(base.B)\n", "takes one side"},
		{"base.A.move(after: self.X)\n", "which is upstream"},
		// A markdown section stops at the next heading, so no heading is ever inside another —
		// the guard that a target must be outside the moved node is exercised on yaml below.
		{"base.A.move(reason: \"x\")\n", "reason: is only for replace / drop"},
	} {
		write("me/doc.lm", c.tpl)
		cfg, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
		if err != nil {
			t.Fatal(err)
		}
		tm, err := lang.LoadTemplate(cfg, filepath.Join(dir, "me", "doc.lm"))
		if err == nil {
			_, err = build.Weave(cfg, tm)
		}
		if err == nil {
			t.Errorf("%s: accepted, want an error mentioning %q", strings.TrimSpace(c.tpl), c.want)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v\nwant an error mentioning %q", strings.TrimSpace(c.tpl), err, c.want)
		}
	}
}

// A yaml key covers what is indented under it, so a nested key really is inside its
// parent — which is the one shape where moving a node next to its own child is possible
// to write, and has to be refused.
func TestMoveTargetInsideItself(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\n")
	mustWrite(t, filepath.Join(dir, "up", "ci.yaml"), "name: x\n\njobs:\n  test:\n    runs-on: a\n")
	mustWrite(t, filepath.Join(dir, "me", "ci.lm"), "base.jobs.move(after: base.jobs.test)\n")

	c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
	if err != nil {
		t.Fatal(err)
	}
	tm, err := lang.LoadTemplate(c, filepath.Join(dir, "me", "ci.lm"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := build.Weave(c, tm); err == nil {
		t.Error("moving a key next to its own child was accepted")
	} else if !strings.Contains(err.Error(), "outside the node being moved") {
		t.Errorf("wrong error: %v", err)
	}
}

// promote and demote change one thing: the heading markers. Everything the section holds —
// its text, a code fence with a # in it — comes through untouched, and the build checks it.
func TestWeaveHeadingLevel(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\n")
	mustWrite(t, filepath.Join(dir, "up", "doc.md"),
		"## Setup\n\ntext with `code`\n\n```sh\n# not a heading\n```\n\n## Other\n\no\n")

	weave := func(tpl string) (string, error) {
		mustWrite(t, filepath.Join(dir, "me", "doc.lm"), tpl)
		c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
		if err != nil {
			t.Fatal(err)
		}
		tm, err := lang.LoadTemplate(c, filepath.Join(dir, "me", "doc.lm"))
		if err != nil {
			return "", err
		}
		return build.Weave(c, tm)
	}

	got, err := weave("base.Setup.demote()\n")
	if err != nil {
		t.Fatalf("demote: %v", err)
	}
	if !strings.HasPrefix(got, "### Setup\n") {
		t.Errorf("demote did not add a level: %q", got[:20])
	}
	for _, keep := range []string{"text with `code`", "# not a heading", "## Other"} {
		if !strings.Contains(got, keep) {
			t.Errorf("demote disturbed %q", keep)
		}
	}

	got, err = weave("base.Setup.promote()\n")
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if !strings.HasPrefix(got, "# Setup\n") {
		t.Errorf("promote did not remove a level: %q", got[:20])
	}

	// The ends of the range are refused rather than silently doing nothing.
	if _, err := weave("base.Setup.promote()\nbase.Setup.promote()\n"); err == nil {
		t.Error("promoting past the top level was accepted")
	}
	if _, err := weave("base.Setup.demote()\nbase.Other.demote()\nbase.Setup.after(self.x)\n"); err == nil {
		t.Error("expected an error for the unknown self.x, got none")
	}
}

// A predicate selects a group, and the operation is done to each of them. The guarantee is
// not weakened by there being several: each node is checked on its own, and the report lists
// each one, so a group is a way of writing less rather than of knowing less.
func TestWeaveSelector(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\n")
	mustWrite(t, filepath.Join(dir, "up", "doc.md"),
		"## Intro\n\ni\n\n## Step 1\n\na\n\n## Step 2\n\nb\n\n## Empty\n\n## Outro\n\no\n")

	weave := func(tpl string) (string, error) {
		mustWrite(t, filepath.Join(dir, "me", "doc.lm"), tpl)
		c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
		if err != nil {
			t.Fatal(err)
		}
		tm, err := lang.LoadTemplate(c, filepath.Join(dir, "me", "doc.lm"))
		if err != nil {
			return "", err
		}
		return build.Weave(c, tm)
	}

	got, err := weave("base.sections(match: \"^Step \").demote()\n")
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if !strings.Contains(got, "### Step 1") || !strings.Contains(got, "### Step 2") {
		t.Errorf("one statement did not reach both sections:\n%s", got)
	}
	if !strings.Contains(got, "## Intro") || !strings.Contains(got, "## Outro") {
		t.Errorf("the predicate reached sections it should not have:\n%s", got)
	}

	got, err = weave("base.sections(empty).drop(reason: \"upstream left a shell\")\n")
	if err != nil {
		t.Fatalf("empty: %v", err)
	}
	if strings.Contains(got, "## Empty") {
		t.Error("the empty section was not dropped")
	}
	for _, keep := range []string{"## Intro", "## Step 1", "## Outro"} {
		if !strings.Contains(got, keep) {
			t.Errorf("%s was dropped and should not have been", keep)
		}
	}

	for _, c := range []struct{ tpl, want string }{
		// A predicate that matches nothing is an error: a statement that quietly did nothing is
		// the kind of quiet this language exists to prevent.
		{"base.sections(match: \"^Nope\").demote()\n", "matched nothing"},
		{"base.sections().demote()\n", "needs a predicate"},
		{"base.sections(match: \"[\").demote()\n", "match:"},
		// A group takes only what means the same done to each of them.
		{"base.sections(empty).after(self.x)\n", "not something to do to each"},
		{"base.sections(empty).move(after: base.Intro)\n", "not something to do to each"},
		{"base.Intro.sections(empty).drop(reason: \"x\")\n", "comes first"},
	} {
		if _, err := weave(c.tpl); err == nil {
			t.Errorf("%s: accepted, want an error mentioning %q", strings.TrimSpace(c.tpl), c.want)
		} else if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v\nwant %q", strings.TrimSpace(c.tpl), err, c.want)
		}
	}
}

// project derives content from the shape of upstream — the names it gave things — and writes
// something that was not there before. It copies nothing upstream says, so upstream is still
// proved whole, and what it writes counts as ours.
func TestWeaveProject(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n")
	mustWrite(t, filepath.Join(dir, "up", "doc.md"),
		"## Intro\n\ni\n\n## Step 1\n\nfirst\n\n## Step 2\n\nsecond\n")

	weave := func(tpl string) (string, error) {
		mustWrite(t, filepath.Join(dir, "me", "doc.lm"), tpl)
		c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
		if err != nil {
			t.Fatal(err)
		}
		tm, err := lang.LoadTemplate(c, filepath.Join(dir, "me", "doc.lm"))
		if err != nil {
			return "", err
		}
		return build.Weave(c, tm)
	}

	got, err := weave("base.start(base.sections(match: \"^Step \").project(`- {name}`))\n")
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	if !strings.Contains(got, "- Step 1\n- Step 2") {
		t.Errorf("the projection did not run over both sections:\n%s", got)
	}
	// It is ours, so it sits inside our marks — which is what lets upstream still be compared.
	if !strings.Contains(got, "<!-- B -->\n- Step 1") {
		t.Errorf("the projection was not marked as ours:\n%s", got)
	}
	if !strings.Contains(got, "## Intro") || !strings.Contains(got, "second") {
		t.Errorf("upstream was disturbed:\n%s", got)
	}

	// {level} and {body} are the other things a document's structure actually has.
	got, err = weave("base.append(base.sections(match: \"^Step 1\").project(`{level}: {body}`))\n")
	if err != nil {
		t.Fatalf("fields: %v", err)
	}
	if !strings.Contains(got, "2: first") {
		t.Errorf("level and body did not expand:\n%s", got)
	}

	for _, c := range []struct{ tpl, want string }{
		{"base.start(base.sections(match: \"^Nope\").project(`- {name}`))\n", "matched nothing"},
		{"base.start(base.sections(empty).project())\n", "takes one template"},
	} {
		if _, err := weave(c.tpl); err == nil {
			t.Errorf("%s: accepted, want %q", strings.TrimSpace(c.tpl), c.want)
		} else if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v\nwant %q", strings.TrimSpace(c.tpl), err, c.want)
		}
	}
}

// level: picks by how deep a heading sits rather than by what it is called. It is the one
// predicate that reads structure instead of text, which is why it is refused on node kinds
// that have no level: a predicate that can only ever match nothing is a typo, not a query.
func TestWeaveSelectorLevel(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\n")
	mustWrite(t, filepath.Join(dir, "up", "doc.md"),
		"# Top\n\nt\n\n## Step 1\n\na\n\n### Detail\n\nd\n\n## Step 2\n\nb\n\n### Note\n\nn\n")
	mustWrite(t, filepath.Join(dir, "up", "conf.toml"), "a = 1\nb = 2\n")

	weave := func(file, tpl string) (string, error) {
		mustWrite(t, filepath.Join(dir, "me", file), tpl)
		c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
		if err != nil {
			t.Fatal(err)
		}
		tm, err := lang.LoadTemplate(c, filepath.Join(dir, "me", file))
		if err != nil {
			return "", err
		}
		return build.Weave(c, tm)
	}

	// Compared as whole lines: "## Top" contains "# Top", so a substring check would pass
	// even if every heading had been demoted.
	headings := func(s string) []string {
		var out []string
		for _, l := range strings.Split(s, "\n") {
			if strings.HasPrefix(l, "#") {
				out = append(out, l)
			}
		}
		return out
	}

	got, err := weave("doc.lm", "base.sections(level: 3).demote()\n")
	if err != nil {
		t.Fatalf("level: %v", err)
	}
	want := []string{"# Top", "## Step 1", "#### Detail", "## Step 2", "#### Note"}
	if h := headings(got); !reflect.DeepEqual(h, want) {
		t.Errorf("level: 3 reached the wrong headings\ngot  %q\nwant %q", h, want)
	}

	// Two predicates narrow each other rather than widening: name and depth must both hold.
	got, err = weave("doc.lm", "base.sections(level: 2, match: \"^Step 1\").demote()\n")
	if err != nil {
		t.Fatalf("level with match: %v", err)
	}
	want = []string{"# Top", "### Step 1", "### Detail", "## Step 2", "### Note"}
	if h := headings(got); !reflect.DeepEqual(h, want) {
		t.Errorf("the two predicates did not narrow each other\ngot  %q\nwant %q", h, want)
	}

	for _, c := range []struct{ file, tpl, want string }{
		{"doc.lm", "base.sections(level: 0).demote()\n", "1 to 6"},
		{"doc.lm", "base.sections(level: 7).demote()\n", "1 to 6"},
		{"doc.lm", "base.sections(level: \"2\").demote()\n", "takes a number"},
		{"doc.lm", "base.sections(level: 4).demote()\n", "matched nothing"},
		// A line has no level, and neither has a toml key: say so rather than match nothing.
		{"doc.lm", "base.lines(level: 2).drop(reason: \"x\")\n", "a line has no level"},
		{"conf.lm", "base.keys(level: 1).drop(reason: \"x\")\n", "a key has no level"},
	} {
		if _, err := weave(c.file, c.tpl); err == nil {
			t.Errorf("%s: accepted, want an error mentioning %q", strings.TrimSpace(c.tpl), c.want)
		} else if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v\nwant %q", strings.TrimSpace(c.tpl), err, c.want)
		}
	}
}

// A json tree is values, not lines, so there is no list of keys to run a predicate over. The
// build says so instead of reporting that the pattern matched nothing, which would send the
// author looking for a typo that is not there. A whole json file must be `base.merge(self)`, so
// the way to reach a json tree with a predicate is a view opened on a value inside another type.
func TestWeaveSelectorOnJSON(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\n")
	mustWrite(t, filepath.Join(dir, "up", "c.toml"), "data = \"\"\"\n{\"a\": 1, \"b\": 2}\n\"\"\"\n")
	mustWrite(t, filepath.Join(dir, "me", "c.toml"), "x = 1\n")
	mustWrite(t, filepath.Join(dir, "me", "c.toml.lm"), "base.data.as(json).keys(match: \"^b\").drop(reason: \"not ours\")\n")

	c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
	if err != nil {
		t.Fatal(err)
	}
	tm, err := lang.LoadTemplate(c, filepath.Join(dir, "me", "c.toml.lm"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = build.Weave(c, tm)
	if err == nil {
		t.Fatal("a predicate over json keys was accepted")
	}
	if !strings.Contains(err.Error(), "no list of keys") {
		t.Errorf("the error does not say why:\n%v", err)
	}
}
