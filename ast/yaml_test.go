package ast

import "strings"

import "testing"

const ci = `name: build

on:
  push:
    branches: [main]

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: test
        run: go test ./...

# a comment is not a key
notes: |
  ## Heading

  Body with ` + "`code`" + ` in it.
`

// A key is addressed by the dotted path of its parents, so two keys named the same
// under different parents are different nodes.
func TestYamlPaths(t *testing.T) {
	y := NewYaml(ci)
	for _, p := range []string{"name", "on.push.branches", "jobs.build.runs-on", "jobs.build.steps"} {
		if got := len(y.Find("key", p)); got != 1 {
			t.Errorf("%s: %d matches, want 1", p, got)
		}
	}
	if got := len(y.Find("key", "nothing.here")); got != 0 {
		t.Errorf("a path that is not there matched %d times", got)
	}
}

// A trailing part of a path is enough while it is unambiguous — `steps` is only one node.
func TestYamlSuffixMatch(t *testing.T) {
	y := NewYaml(ci)
	if got := len(y.Find("key", "steps")); got != 1 {
		t.Errorf("steps: %d matches, want 1", got)
	}
	// `name` is both the top-level key and one inside a step, so the exact path wins
	// over the suffix rather than returning both.
	if got := len(y.Find("key", "name")); got != 1 {
		t.Errorf("name: %d matches, want the exact top-level one", got)
	}
}

// A key covers whatever is indented under it: the nested map, the sequence, the block.
func TestYamlSpanCoversTheBlock(t *testing.T) {
	y := NewYaml(ci)
	hits := y.Find("key", "jobs")
	if len(hits) != 1 {
		t.Fatalf("jobs: %d matches", len(hits))
	}
	span := strings.Join(y.Lines()[hits[0][0]:hits[0][1]], "\n")
	for _, want := range []string{"jobs:", "runs-on:", "actions/checkout@v4", "go test ./..."} {
		if !strings.Contains(span, want) {
			t.Errorf("the span of jobs does not cover %q", want)
		}
	}
	if strings.Contains(span, "notes:") {
		t.Error("the span of jobs ran past its own block")
	}
}

// Neither a comment nor a blank line is a key.
func TestYamlIgnoresComments(t *testing.T) {
	y := NewYaml("# key: not-a-key\n\nreal: 1\n")
	if got := len(y.Find("key", "key")); got != 0 {
		t.Errorf("a commented-out key matched %d times", got)
	}
	if got := len(y.Find("key", "real")); got != 1 {
		t.Errorf("real: %d matches, want 1", got)
	}
}

// ── the recursion rule: a part opens as a document of its own ──────────────
//
// This is what phase one is for. The whole design rests on it, in both directions.

// Down: a yaml block scalar holding markdown has sections.
func TestYamlValueOpensAsMarkdown(t *testing.T) {
	y := NewYaml(ci)
	body, ok := y.BodyOf("notes")
	if !ok {
		t.Fatal("notes has no body")
	}
	if strings.HasPrefix(body, "  ") {
		t.Errorf("the block was not dedented: %q", body)
	}
	if !strings.Contains(body, "`code`") {
		t.Error("a backtick inside the block did not survive")
	}
	m := NewMarkdown(body)
	if got := len(m.Find("heading", "Heading")); got != 1 {
		t.Errorf("the markdown inside the yaml has %d headings named Heading, want 1", got)
	}
}

// Up: markdown frontmatter is yaml, so it opens as one and its keys are addressable.
func TestMarkdownFrontmatterOpensAsYaml(t *testing.T) {
	m := NewMarkdown("---\nname: doc\ndescription: a thing\ntags:\n  - a\n  - b\n---\n\n## Overview\n")
	fm, ok := m.BodyOf("frontmatter")
	if !ok {
		t.Fatal("no frontmatter")
	}
	y := NewYaml(fm)
	if got := len(y.Find("key", "description")); got != 1 {
		t.Errorf("description: %d matches in the frontmatter read as yaml, want 1", got)
	}
	if got, _ := y.BodyOf("description"); got != "a thing" {
		t.Errorf("description = %q, want %q", got, "a thing")
	}
	if got := len(y.Find("key", "tags")); got != 1 {
		t.Errorf("tags: %d matches, want 1", got)
	}
}

// ── writing ────────────────────────────────────────────────────────────────

func TestYamlSetScalar(t *testing.T) {
	y := NewYaml("name: build\nother: 1\n")
	if !y.SetBody("name", "release") {
		t.Fatal("SetBody said no")
	}
	if got, _ := y.BodyOf("name"); got != "release" {
		t.Errorf("name = %q", got)
	}
	if !strings.Contains(y.Text(), "other: 1") {
		t.Error("the other key was disturbed")
	}
}

// Writing a block scalar re-indents it under its key, so the file stays valid yaml.
func TestYamlSetBlockReindents(t *testing.T) {
	y := NewYaml("notes: |\n  old\n\nafter: 1\n")
	if !y.SetBody("notes", "## New\n\nbody") {
		t.Fatal("SetBody said no")
	}
	// The blank line between the block and the next key is not part of the block,
	// so it stays where the author put it.
	want := "notes: |\n  ## New\n\n  body\n\nafter: 1\n"
	if y.Text() != want {
		t.Errorf("got:\n%q\nwant:\n%q", y.Text(), want)
	}
	back, _ := y.BodyOf("notes")
	if back != "## New\n\nbody" {
		t.Errorf("round trip lost it: %q", back)
	}
}

// Splice reparses, so a node added by an edit is addressable straight away.
func TestYamlSpliceReparses(t *testing.T) {
	y := NewYaml("a: 1\nb: 2\n")
	hits := y.Find("key", "b")
	y.Splice(hits[0][0], hits[0][0], []string{"inserted: 3"})
	if got := len(y.Find("key", "inserted")); got != 1 {
		t.Errorf("the inserted key is not addressable: %d matches", got)
	}
	if got := len(y.Find("key", "b")); got != 1 {
		t.Errorf("b went missing after the splice")
	}
}
