package ast

import "testing"

// Markdown nodes are headings — but a # inside a code fence is not a heading. We hit this in practice.
func TestMarkdownIgnoresHeadingsInFences(t *testing.T) {
	m := NewMarkdown("## Real\n\n```\n## Fake\n```\n\n## Also Real\n")
	if got := len(m.Find("heading", "Fake")); got != 0 {
		t.Errorf("## Fake inside a fence was treated as a heading (%d matches)", got)
	}
	if got := len(m.Find("heading", "Real")); got != 1 {
		t.Errorf("## Real should match once, got %d", got)
	}
}

// A section runs until the next heading of any level — subsections belong to the parent.
func TestMarkdownSectionSpan(t *testing.T) {
	m := NewMarkdown("## A\n\nbody\n\n### A1\n\nsub\n\n## B\n")
	hits := m.Find("heading", "A")
	if len(hits) != 1 {
		t.Fatalf("found %d matches", len(hits))
	}
	if hits[0][0] != 0 || hits[0][1] != 4 {
		t.Errorf("span of A should be [0,4), got %v", hits[0])
	}
}

func TestMarkdownFrontmatter(t *testing.T) {
	m := NewMarkdown("---\nname: x\n---\n\nbody\n")
	fm, ok := m.BodyOf("frontmatter")
	if !ok || fm != "---\nname: x\n---" {
		t.Errorf("wrong frontmatter: %q", fm)
	}
	// No lstrip: the blank line after the frontmatter is part of the body
	b, _ := m.BodyOf("body")
	if b != "\nbody\n" {
		t.Errorf("body should keep its leading blank line, got %q", b)
	}
	if _, ok := NewMarkdown("no fm here\n").BodyOf("frontmatter"); ok {
		t.Error("should not report frontmatter when there is none")
	}
}

// Shell function splitting is stateless — an approach that keeps swallowing lines
// when the previous function did not close at column 0 **silently drops functions**.
func TestShellFunctionsAreIndependent(t *testing.T) {
	s := NewShell("a() {\n  x\n}\n\nb() {\n  y\n}\n")
	for _, n := range []string{"a", "b"} {
		if got := len(s.Find("function", n)); got != 1 {
			t.Errorf("function %s should match once, got %d", n, got)
		}
	}
}

func TestShellUnclosedIsReported(t *testing.T) {
	s := NewShell("a() {\n  x\n")
	if len(s.Unclosed) != 1 || s.Unclosed[0] != "a" {
		t.Errorf("a function with a head but no closing brace should be reported, got %v", s.Unclosed)
	}
	if len(s.Find("function", "a")) != 0 {
		t.Error("a function that cannot be parsed must not count as found")
	}
}

// Banner anchors match by **prefix** — a banner often ends in a run of decorative ─;
// requiring the full text forces people to copy the decoration, and one wrong character means no match.
func TestShellMarkerPrefixMatch(t *testing.T) {
	s := NewShell("# ── Test 3: blocks ─────────\nx\n# ── Test 4 ───\ny\n")
	hits := s.Find("marker", "# ── Test 3")
	if len(hits) != 1 {
		t.Fatalf("banner should match once by prefix, got %d", len(hits))
	}
	if hits[0][1] != 2 {
		t.Errorf("banner section should end at the next banner, got %v", hits[0])
	}
}

func TestTomlBlockAndScalar(t *testing.T) {
	tm := NewToml("description = \"x\"\nprompt = \"\"\"\nline1\nline2\n\"\"\"\n")
	if v, _ := tm.BodyOf("description"); v != `"x"` {
		t.Errorf("wrong scalar: %q", v)
	}
	if v, _ := tm.BodyOf("prompt"); v != "line1\nline2" {
		t.Errorf("wrong triple-quoted block: %q", v)
	}
	if !tm.SetBody("description", "y") {
		t.Fatal("write failed")
	}
	if v, _ := tm.BodyOf("description"); v != `"y"` {
		t.Errorf("wrong value after write-back: %q", v)
	}
}

// Text does exact whole-line matching only, never fuzzy — better to make people write more precisely.
func TestTextExactLineOnly(t *testing.T) {
	x := NewText("alpha\nbeta\nbetamax\n")
	if got := len(x.Find("line", "beta")); got != 1 {
		t.Errorf("beta should match only the whole line, got %d", got)
	}
}

// JSON must preserve order — key order is chosen by a person and carries meaning; reorder it once and it can never be matched back.
func TestJSONPreservesKeyOrder(t *testing.T) {
	src := `{"z":1,"a":{"y":"two","b":[1,2]},"m":null}`
	v, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"z\": 1,\n  \"a\": {\n    \"y\": \"two\",\n    \"b\": [\n      1,\n      2\n    ]\n  },\n  \"m\": null\n}"
	if got := v.Marshal(); got != want {
		t.Errorf("wrong serialization\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// Non-ASCII is not turned into \u — non-English text should stay readable in the output.
func TestJSONKeepsUnicodeLiteral(t *testing.T) {
	v, _ := Parse(`{"k":"Café ≠ résumé"}`)
	if got := v.Marshal(); got != "{\n  \"k\": \"Café ≠ résumé\"\n}" {
		t.Errorf("got %q", got)
	}
}

func TestJSONRejectsGarbage(t *testing.T) {
	if _, err := Parse(`{"a":}`); err == nil {
		t.Error("malformed JSON should be an error")
	}
}
