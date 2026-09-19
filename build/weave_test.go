package build_test

import (
	"os"
	"path/filepath"
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
