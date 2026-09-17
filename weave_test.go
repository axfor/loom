package loom_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axfor/loom"
)

const fixture = "testdata/repo"

func load(t *testing.T) *loom.Config {
	t.Helper()
	c, err := loom.LoadConfig(filepath.Join(fixture, loom.ConfigName))
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	return c
}

func weave(t *testing.T, c *loom.Config, tpl string) string {
	t.Helper()
	tm, err := loom.LoadTemplate(c, filepath.Join(fixture, "templates", tpl))
	if err != nil {
		t.Fatalf("%s: parse failed: %v", tpl, err)
	}
	out, err := loom.Weave(c, tm)
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
		{"run.sh.lm", "run.sh"},
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
	// the frontmatter is replaced whole on purpose (frontmatter = mine) and is not inside marks, so compare the body only
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
	c := load(t)
	cases := []struct {
		name, tpl, want string
	}{
		{"anchor not found", `weave "doc.md" {
  type = "markdown"
  after "heading" "No Such Section" { insert = mine.heading["Appendix"] }
}`, "anchor not found"},
		{"wrong node kind", `weave "doc.md" {
  type = "markdown"
  after "key" "Overview" { insert = mine.heading["Appendix"] }
}`, "this type has no"},
		{"unknown layer", `weave "doc.md" {
  type = "markdown"
  append = nosuch.body
}`, "unknown layer name"},
		{"reference without anchor", `weave "doc.md" {
  type = "markdown"
  append = mine.heading
}`, "needs an anchor"},
		{"missing base", `weave "nope.md" {
  type = "markdown"
  append = mine.body
}`, "has no nope.md"},
	}
	for _, cse := range cases {
		t.Run(cse.name, func(t *testing.T) {
			tm, err := loom.ParseTemplate("t.lm", []byte(cse.tpl))
			if err == nil {
				_, err = loom.Weave(c, tm)
			}
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
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "loom.lm"), `
layer "up" {
  dir  = "up"
  role = "warp"
}
layer "me" {
  dir  = "me"
  role = "weft"
}
templates = "t"
`)
	mustWrite(t, filepath.Join(dir, "up", "a.md"), "## Same\n\nx\n\n## Same\n\ny\n")
	mustWrite(t, filepath.Join(dir, "me", "a.md"), "## Mine\n\nz\n")
	mustWrite(t, filepath.Join(dir, "t", "a.lm"), `
weave "a.md" {
  type = "markdown"
  from = "up"
  after "heading" "Same" { insert = me.heading["Mine"] }
}`)
	c, err := loom.LoadConfig(filepath.Join(dir, "loom.lm"))
	if err != nil {
		t.Fatal(err)
	}
	tm, err := loom.LoadTemplate(c, filepath.Join(dir, "t", "a.lm"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loom.Weave(c, tm); err == nil || !strings.Contains(err.Error(), "matches 2 places") {
		t.Fatalf("a duplicate anchor must be refused, got: %v", err)
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

// Statement order matters: of two insertions at the same anchor, the one written first comes first.
func TestStatementOrderIsSourceOrder(t *testing.T) {
	c := load(t)
	tm, err := loom.ParseTemplate("t.lm", []byte(`weave "doc.md" {
  type = "markdown"
  append = [mine.heading["Where this fits"], mine.heading["Appendix"]]
}`))
	if err != nil {
		t.Fatal(err)
	}
	out, err := loom.Weave(c, tm)
	if err != nil {
		t.Fatal(err)
	}
	i, j := strings.Index(out, "## Where this fits"), strings.Index(out, "## Appendix")
	if i < 0 || j < 0 || i > j {
		t.Errorf("append order does not follow the template: Where this fits@%d Appendix@%d", i, j)
	}
}

// list must report metadata from the same parse that weaving uses: outside tools rely on it, and
// "every tool parses templates again on its own" is exactly what this command exists to remove.
func TestDescribeMatchesWeave(t *testing.T) {
	c := load(t)
	i, err := loom.Describe(c, filepath.Join(fixture, "templates", "doc.lm"))
	if err != nil {
		t.Fatal(err)
	}
	if i.Target != "doc.md" || i.Type != "markdown" || i.From != "upstream" {
		t.Errorf("wrong metadata: %+v", i)
	}
	if i.Path != "doc.md" {
		t.Errorf("the base path should default to the product path, got %q", i.Path)
	}
	if len(i.Anchors) != 1 || i.Anchors[0].Kind != "heading" || i.Anchors[0].Anchor != "Overview" {
		t.Errorf("anchors not all reported: %+v", i.Anchors)
	}
	// frontmatter + the insert of after + the insert of append
	if len(i.Inserts) != 3 {
		t.Errorf("expected 3 content sources, got %d: %+v", len(i.Inserts), i.Inserts)
	}
}

// Two templates for one product: which one wins would depend on enumeration order, so the loom refuses on the spot.
// Why here: only the loom reads every template. Leaving it to downstream tools with their own regexes produces
// checks that can never fire, such as a tool keyed by product path where duplicates were already merged away.
func TestDuplicateTargetRefused(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "loom.lm"), `
layer "up" {
  dir  = "up"
  role = "warp"
}
templates = "t"
`)
	mustWrite(t, filepath.Join(dir, "up", "a.md"), "## A\n\nx\n")
	one := `
weave "a.md" {
  type = "markdown"
  from = "up"
}`
	mustWrite(t, filepath.Join(dir, "t", "a.lm"), one)
	c, err := loom.LoadConfig(filepath.Join(dir, "loom.lm"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loom.Templates(c); err != nil {
		t.Fatalf("a single template must not be an error: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "t", "copy-of-a.lm"), one)
	_, err = loom.Templates(c)
	if err == nil || !strings.Contains(err.Error(), "has two templates") {
		t.Fatalf("a duplicate target must be refused, got: %v", err)
	}
}
