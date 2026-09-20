package build_test

// Everything SYNTAX.md adds to the statement language, each construct woven against a real tree
// and checked by what it produced — not by what it parsed into.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/axfor/loom/build"
	"github.com/axfor/loom/lang"
)

// newTree builds a small tree and returns a function that weaves one template against it.
func newTree(t *testing.T, settings, up, ours string) func(string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "loom.om"), settings)
	mustWrite(t, filepath.Join(dir, "up", "f.md"), up)
	if ours != "" {
		mustWrite(t, filepath.Join(dir, "me", "f.md"), ours)
	}
	return func(tpl string) (string, error) {
		mustWrite(t, filepath.Join(dir, "me", "f.md.lm"), tpl)
		c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
		if err != nil {
			t.Fatal(err)
		}
		tm, err := lang.LoadTemplate(c, filepath.Join(dir, "me", "f.md.lm"))
		if err != nil {
			return "", err
		}
		return build.Weave(c, tm)
	}
}

const upDoc = "# T\n\n## Setup\n\nintro\n\n### Step A\n\na\n\n### Step B\n\nb\n\n## Other\n\no\n"

func headings(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	return strings.Join(out, " | ")
}

// A predicate in brackets asks about the document's own structure, and the terms compose.
func TestPredicateInBrackets(t *testing.T) {
	weave := newTree(t, "base \"up\"\nself \"me\"\n", upDoc, "## Ours\n\no\n")
	for _, c := range []struct{ tpl, want string }{
		{`base.sections[level == 3].demote()`, "# T | ## Setup | #### Step A | #### Step B | ## Other"},
		{`base.sections[level == 3 && name ~ "A$"].demote()`, "# T | ## Setup | #### Step A | ### Step B | ## Other"},
		{`base.sections[name == "Other" || level == 3].demote()`, "# T | ## Setup | #### Step A | #### Step B | ### Other"},
		{`base.sections[level == 3 && !(name ~ "A$")].demote()`, "# T | ## Setup | ### Step A | #### Step B | ## Other"},
		{`base.sections[level == 3].first.demote()`, "# T | ## Setup | #### Step A | ### Step B | ## Other"},
		{`base.sections[level == 3].last.demote()`, "# T | ## Setup | ### Step A | #### Step B | ## Other"},
	} {
		got, err := weave(c.tpl + "\n")
		if err != nil {
			t.Errorf("%s: %v", c.tpl, err)
			continue
		}
		if h := headings(got); h != c.want {
			t.Errorf("%s\n  got  %s\n  want %s", c.tpl, h, c.want)
		}
	}

	for _, c := range []struct{ tpl, want string }{
		{`base.lines[level == 2].drop(reason: "r")`, "level is for markdown headings"},
		{`base.sections[calls "x"].drop(reason: "r")`, "`calls` is for shell functions"},
		{`base.sections[level == 9].demote()`, "1 to 6"},
		{`base.sections[nope == 2].demote()`, "unknown predicate"},
		{`base.sections[level == 2][empty].demote()`, "join them with &&"},
	} {
		if _, err := weave(c.tpl + "\n"); err == nil {
			t.Errorf("%s: accepted", c.tpl)
		} else if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v\n  want %q", c.tpl, err, c.want)
		}
	}
}

// An axis walks from a node to one the document relates it to.
func TestAxes(t *testing.T) {
	weave := newTree(t, "base \"up\"\nself \"me\"\n", upDoc, "## Ours\n\no\n")
	for _, c := range []struct{ tpl, want string }{
		{`base.Setup.children.demote()`, "# T | ## Setup | #### Step A | #### Step B | ## Other"},
		{`base.Setup.next.demote()`, "# T | ## Setup | ### Step A | ### Step B | ### Other"},
		{`base.Other.prev.demote()`, "# T | ### Setup | ### Step A | ### Step B | ## Other"},
		{`base.Setup."Step A".parent.demote()`, "# T | ### Setup | ### Step A | ### Step B | ## Other"},
	} {
		got, err := weave(c.tpl + "\n")
		if err != nil {
			t.Errorf("%s: %v", c.tpl, err)
			continue
		}
		if h := headings(got); h != c.want {
			t.Errorf("%s\n  got  %s\n  want %s", c.tpl, h, c.want)
		}
	}
	for _, c := range []struct{ tpl, want string }{
		{`base.Other.next.demote()`, "has no next at its own level"},
		{`base.Setup.first.demote()`, "picks from a group"},
		{`base.sections[level == 3].parent.demote()`, "walks from one node"},
		{`base.Setup.children.next.demote()`, "one step at a time"},
	} {
		if _, err := weave(c.tpl + "\n"); err == nil {
			t.Errorf("%s: accepted", c.tpl)
		} else if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v\n  want %q", c.tpl, err, c.want)
		}
	}
}

// The operations that restructure: each keeps a promise the build can check.
func TestNewOperations(t *testing.T) {
	weave := newTree(t, "base \"up\"\nself \"me\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n", upDoc, "## Ours\n\no\n")
	for _, c := range []struct{ tpl, want string }{
		// unwrap takes the heading out and lifts what was under it.
		{`base.Setup.unwrap(reason: "no meaning in the product")`, "# T | ## Step A | ## Step B | ## Other"},
		// join runs this section and the next together.
		{`base.Setup.join(reason: "read as one")`, "# T | ## Setup | ### Step B | ## Other"},
		// split puts a heading of ours in the middle; upstream's own bytes do not move.
		{`base.Setup.split(base.Setup."Step B", "Setup, part two")`, "# T | ## Setup | ### Step A | ## Setup, part two | ### Step B | ## Other"},
		// swap exchanges two spans.
		{`base.Setup.swap(base.Other)`, "# T | ## Other | ### Step A | ### Step B | ## Setup"},
		// wrap is before and after in one statement.
		{`base.Setup.wrap(self.Ours, self.Ours)`, "# T | ## Ours | ## Setup | ## Ours | ### Step A | ### Step B | ## Other"},
	} {
		got, err := weave(c.tpl + "\n")
		if err != nil {
			t.Errorf("%s: %v", c.tpl, err)
			continue
		}
		if h := headings(got); h != c.want {
			t.Errorf("%s\n  got  %s\n  want %s", c.tpl, h, c.want)
		}
	}
	for _, c := range []struct{ tpl, want string }{
		{`base.Setup.unwrap()`, "needs a reason"},
		{`base.Setup.join()`, "needs a reason"},
		{`base.Other.join(reason: "r")`, "has no section after it"},
		{`base.Setup.swap(base.Setup)`, "a different node"},
		{`base.Setup.split(base.Other, "x")`, "inside Setup"},
	} {
		if _, err := weave(c.tpl + "\n"); err == nil {
			t.Errorf("%s: accepted", c.tpl)
		} else if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v\n  want %q", c.tpl, err, c.want)
		}
	}
}

// `if` asks the document a question that writes nothing, and the report says which way it went.
func TestIfAndCaughtResults(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\n")
	mustWrite(t, filepath.Join(dir, "up", "f.md"), upDoc)
	mustWrite(t, filepath.Join(dir, "me", "f.md"), "## Ours\n\no\n")
	plan := func(tpl string) (*build.Plan, error) {
		mustWrite(t, filepath.Join(dir, "me", "f.md.lm"), tpl)
		c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
		if err != nil {
			t.Fatal(err)
		}
		return build.PlanBuild(c, true)
	}

	p, err := plan("if base.has.Setup {\n\tbase.Setup.after(self.Ours)\n}\nif base.has.Nope {\n\tbase.Other.after(self.Ours)\n}\n")
	if err != nil {
		t.Fatalf("if: %v", err)
	}
	if n := len(p.Report.Skipped); n != 1 {
		t.Errorf("one branch should have been skipped and reported, got %d: %+v", n, p.Report.Skipped)
	}
	if got := readFile(t, filepath.Join(dir, "me", "f.md.lm")); strings.Count(got, "self.Ours") != 2 {
		t.Errorf("the template was rewritten: %q", got)
	}

	// A predicate can be the question too.
	if _, err := plan("if base.sections[level == 3].any {\n\tbase.Setup.after(self.Ours)\n}\n"); err != nil {
		t.Errorf("a predicate question: %v", err)
	}

	// Catching a result lets a write that cannot land be handled instead of stopping the build.
	p, err = plan("ok = base.Nope.after(self.Ours)\nif !ok {\n\tbase.Setup.after(self.Ours)\n}\n")
	if err != nil {
		t.Fatalf("caught result: %v", err)
	}
	if len(p.Report.Skipped) == 0 {
		t.Error("the write that did not land should be reported")
	}

	// And the reason it did not land is what err.format is given.
	_, err = plan("ok = base.Nope.after(self.Ours)\nif !ok {\n\treturn err.format(\"upstream lost it: %s\", ok)\n}\n")
	if err == nil {
		t.Fatal("return err.format should fail the build")
	}
	if !strings.Contains(err.Error(), "upstream lost it:") || !strings.Contains(err.Error(), "Nope not found") {
		t.Errorf("the message should carry both what we said and why: %v", err)
	}

	for _, c := range []struct{ tpl, want string }{
		{"ok = base.Setup.after(self.Ours)\n", "catches a result that nothing reads"},
		{"if ok {\n\tbase.Setup.after(self.Ours)\n}\n", "is not a result caught earlier"},
		{"if base.Setup {\n\tbase.Setup.after(self.Ours)\n}\n", "asks a question that writes nothing"},
		{"if base.has.Setup {\n}\n", "does nothing either way"},
	} {
		if _, err := plan(c.tpl); err == nil {
			t.Errorf("%q: accepted", c.tpl)
		} else if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: %v\n  want %q", c.tpl, err, c.want)
		}
	}
}

// `fn` groups statements inside a file, and is inlined where it is called.
func TestFunctions(t *testing.T) {
	weave := newTree(t, "base \"up\"\nself \"me\"\n", upDoc, "## Ours\n\no\n")
	got, err := weave("fn part(up, ours) {\n\tup.after(ours)\n}\npart(base.Setup, self.Ours)\npart(base.Other, self.Ours)\n")
	if err != nil {
		t.Fatalf("fn: %v", err)
	}
	if strings.Count(got, "## Ours") != 2 {
		t.Errorf("the function did not run twice:\n%s", got)
	}
	for _, c := range []struct{ tpl, want string }{
		{"fn a() {\n\ta()\n}\na()\n", "calls itself"},
		{"fn a(x) {\n\tx.after(self.Ours)\n}\na()\n", "takes 1 argument(s), got 0"},
		{"nope()\n", "no fn named `nope`"},
		{"fn a() {\n\tbase.Setup.after(self.Ours)\n}\nfn a() {\n}\n", "defined twice"},
	} {
		if _, err := weave(c.tpl); err == nil {
			t.Errorf("%q: accepted", c.tpl)
		} else if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: %v\n  want %q", c.tpl, err, c.want)
		}
	}
}

// `Self:` puts our content in the template, addressed exactly as a file of ours would be.
func TestResourceSections(t *testing.T) {
	weave := newTree(t, "base \"up\"\nself \"me\"\n", upDoc, "")
	got, err := weave("base.Setup.after(self.job)\n---\nSelf:\n    ```markdown\n    ## job\n\n    what it does\n    ```\n")
	if err != nil {
		t.Fatalf("resource section: %v", err)
	}
	if !strings.Contains(got, "## job") || !strings.Contains(got, "what it does") {
		t.Errorf("the inline document did not reach the product:\n%s", got)
	}

	// Named sections, and a kind to tell two documents apart.
	got, err = weave("base.Setup.after(self.docs.job)\n---\nSelf as docs:\n    ```markdown\n    ## job\n\n    d\n    ```\n---\nSelf as notes:\n    ```markdown\n    ## job\n\n    n\n    ```\n")
	if err != nil {
		t.Fatalf("named sections: %v", err)
	}
	if !strings.Contains(got, "\nd\n") || strings.Contains(got, "\nn\n") {
		t.Errorf("the wrong section was used:\n%s", got)
	}

	for _, c := range []struct{ tpl, want string }{
		{"base.Setup.after(self.nope)\n---\nSelf:\n    ```markdown\n    ## job\n    ```\n", "has \"nope\""},
		{"base.Setup.after(self.job)\n---\nSelf:\n    ```\n    ## job\n    ```\n", "no language tag"},
		{"base.Setup.after(self.job)\n---\nSelf:\n    ```nope\n    x\n    ```\n", "unknown kind"},
		{"base.Setup.after(self.job)\n---\nSelf:\n", "holds nothing"},
		{"base.Setup.after(self.job)\n---\nSelf:\n    ```markdown\n    ## job\n    ```\n---\nSelf:\n    ```markdown\n    ## other\n    ```\n", "written twice"},
	} {
		if _, err := weave(c.tpl); err == nil {
			t.Errorf("%q: accepted", c.tpl)
		} else if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: %v\n  want %q", c.tpl, err, c.want)
		}
	}
}

// `return self` is what a whole-file replace has always been, said once and with a reason.
func TestReturn(t *testing.T) {
	weave := newTree(t, "base \"up\"\nself \"me\"\n", upDoc, "## Ours\n\no\n")
	got, err := weave("return self // reason: upstream's and ours would both run\n")
	if err != nil {
		t.Fatalf("return self: %v", err)
	}
	if strings.Contains(got, "## Setup") {
		t.Errorf("the product should be our file:\n%s", got)
	}
	for _, c := range []struct{ tpl, want string }{
		{"return self\n", "needs a reason"},
		{"return base // reason: r\n", "takes self"},
	} {
		if _, err := weave(c.tpl); err == nil {
			t.Errorf("%q: accepted", c.tpl)
		} else if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: %v\n  want %q", c.tpl, err, c.want)
		}
	}
}

// An insertion that lands inside lines another statement rewrites is lost with them, and the
// product still looks ordinary — the same failure the overlap check exists to prevent, reached
// from the other side. split writes a heading of ours inside a section; unwrap rewrites that
// whole section. Before this was caught, the two together duplicated a subsection and reported
// the build as fine.
func TestAnInsertionInsideARewriteIsRefused(t *testing.T) {
	weave := newTree(t, "base \"up\"\nself \"me\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n", upDoc, "## Ours\n\no\n")
	_, err := weave("base.Setup.split(base.Setup.\"Step B\", \"X\")\nbase.Setup.unwrap(reason: \"c\")\n")
	if err == nil {
		t.Fatal("an insertion inside a rewritten span was accepted")
	}
	if !strings.Contains(err.Error(), "writes inside the lines") {
		t.Errorf("unexpected error: %v", err)
	}

	// Insertions that meet an edge still stack, and a span that holds no insertion is untouched.
	for _, tpl := range []string{
		"base.Setup.after(self.Ours)\nbase.Setup.before(self.Ours)\n",
		"base.Setup.wrap(self.Ours, self.Ours)\nbase.Other.after(self.Ours)\n",
		"base.Setup.split(base.Setup.\"Step A\", \"X\")\nbase.Other.after(self.Ours)\n",
	} {
		if _, err := weave(tpl); err != nil {
			t.Errorf("%q should still build: %v", strings.ReplaceAll(tpl, "\n", "; "), err)
		}
	}
}

// self has one source. A resource section says our content is written in the template; a file of
// ours at the same path says it is written there. With both, every self.x would have two places
// to look and nothing in the template would say which.
func TestResourcesAndOurFileAreOneOrTheOther(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\n")
	mustWrite(t, filepath.Join(dir, "up", "f.md"), upDoc)
	mustWrite(t, filepath.Join(dir, "me", "f.md.lm"), "base.Setup.after(self.job)\n---\nSelf:\n    ```markdown\n    ## job\n\n    x\n    ```\n")
	load := func() error {
		c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
		if err != nil {
			t.Fatal(err)
		}
		_, err = lang.LoadTemplate(c, filepath.Join(dir, "me", "f.md.lm"))
		return err
	}
	if err := load(); err != nil {
		t.Fatalf("a resource section on its own: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "me", "f.md"), "## job\n\nelsewhere\n")
	err := load()
	if err == nil {
		t.Fatal("two sources for self were accepted")
	}
	if !strings.Contains(err.Error(), "two sources") {
		t.Errorf("unexpected error: %v", err)
	}
}

// A markdown document in a resource section has frontmatter like any other, and a key of it is
// addressed the same way.
func TestResourceFrontmatter(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n")
	mustWrite(t, filepath.Join(dir, "up", "f.md"), "---\nname: d\ndescription: Upstream.\n---\n\n## Overview\n\nu\n")
	mustWrite(t, filepath.Join(dir, "me", "f.md.lm"),
		"base.frontmatter.description.start(self.frontmatter.description)\n---\nSelf:\n    ````markdown\n    ---\n    description: Ours.\n    ---\n\n    ## job\n\n    x\n    ````\n")
	c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
	if err != nil {
		t.Fatal(err)
	}
	tm, err := lang.LoadTemplate(c, filepath.Join(dir, "me", "f.md.lm"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := build.Weave(c, tm)
	if err != nil {
		t.Fatalf("weave: %v", err)
	}
	if !strings.Contains(got, "description: Ours. Upstream.") {
		t.Errorf("our frontmatter key did not reach the product:\n%s", got)
	}
}

// `return self` says what the product is, so it is a property of the template rather than of a
// branch — a conditional answer would be two products.
func TestReturnSelfIsNotConditional(t *testing.T) {
	weave := newTree(t, "base \"up\"\nself \"me\"\n", upDoc, "## Ours\n\no\n")
	_, err := weave("if base.has.Setup {\n\treturn self // reason: r\n}\nbase.Other.after(self.Ours)\n")
	if err == nil {
		t.Fatal("a conditional return self was accepted")
	}
	if !strings.Contains(err.Error(), "cannot depend on a question") {
		t.Errorf("unexpected error: %v", err)
	}

	// The constructs that do nest still do.
	for _, tpl := range []string{
		"if base.has.Setup {\n\tif base.has.Other {\n\t\tbase.Setup.after(self.Ours)\n\t}\n}\n",
		"fn p(u, o) {\n\tif base.has.Other {\n\t\tu.after(o)\n\t}\n}\np(base.Setup, self.Ours)\n",
		"if base.has.Setup {\n\treturn\n}\nbase.Other.after(self.Ours)\n",
	} {
		if _, err := weave(tpl); err != nil {
			t.Errorf("%q: %v", strings.ReplaceAll(tpl, "\n", "; "), err)
		}
	}
}

// `else if` is the else branch holding one if, so a chain of questions reads as a chain. The one
// that holds decides, the rest are not asked twice, and where none holds the report says so.
func TestElseIfChain(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\n")
	mustWrite(t, filepath.Join(dir, "up", "f.md"), upDoc)
	mustWrite(t, filepath.Join(dir, "me", "f.md"), "## Ours\n\no\n")
	run := func(tpl string) (*build.Plan, string) {
		t.Helper()
		mustWrite(t, filepath.Join(dir, "me", "f.md.lm"), tpl)
		c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
		if err != nil {
			t.Fatal(err)
		}
		tm, err := lang.LoadTemplate(c, filepath.Join(dir, "me", "f.md.lm"))
		if err != nil {
			t.Fatalf("%s: %v", tpl, err)
		}
		out, err := build.Weave(c, tm)
		if err != nil {
			t.Fatalf("%s: %v", tpl, err)
		}
		p, err := build.PlanBuild(c, true)
		if err != nil {
			t.Fatalf("%s: %v", tpl, err)
		}
		return p, out
	}

	// The third link holds, and only it writes.
	_, out := run("if base.has.Nope {\n\tbase.Setup.before(self.Ours)\n} else if base.has.Missing {\n\tbase.Other.before(self.Ours)\n} else if base.has.Other {\n\tbase.Other.after(self.Ours)\n} else {\n\tbase.Setup.after(self.Ours)\n}\n")
	if n := strings.Count(out, "## Ours"); n != 1 {
		t.Errorf("exactly one link should have written, got %d:\n%s", n, out)
	}
	if i, j := strings.Index(out, "## Other"), strings.Index(out, "## Ours"); i > j {
		t.Errorf("the link that held wrote in the wrong place:\n%s", out)
	}

	// The first link holds, and the rest are not reached.
	_, out = run("if base.has.Setup {\n\tbase.Setup.after(self.Ours)\n} else if base.has.Other {\n\tbase.Other.after(self.Ours)\n}\n")
	if n := strings.Count(out, "## Ours"); n != 1 {
		t.Errorf("only the first link should have written, got %d:\n%s", n, out)
	}

	// None holds and there is no else: nothing is written, and the report says which question it was.
	p, _ := run("if base.has.Nope {\n\tbase.Setup.after(self.Ours)\n} else if base.has.Missing {\n\tbase.Other.after(self.Ours)\n}\nbase.Other.after(self.Ours)\n")
	if len(p.Report.Skipped) != 1 {
		t.Fatalf("the chain writing nothing should be reported once: %+v", p.Report.Skipped)
	}
	if !strings.Contains(p.Report.Skipped[0].Detail, "base.has.Missing") {
		t.Errorf("the report should name the last question asked: %q", p.Report.Skipped[0].Detail)
	}
}

// `calls` and `has` ask what a document does and what it holds. Written as a search for the word
// they would answer a different question — one that is right often enough to be trusted and wrong
// exactly where it matters.
func TestCallsAndHasAreNotSearches(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\nmark shell \"# B\" \"# E\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n")
	mustWrite(t, filepath.Join(dir, "up", "r.sh"),
		"#!/bin/sh\nfetch() {\n  curl -s \"$1\"\n}\ncurly() {\n  echo curly\n}\nnote() {\n  # curl -s \"$1\"   ← the old way, kept for reference\n  echo hi  # ; curl x\n}\npiped() {\n  echo x | curl -\n}\nguarded() {\n  if curl -f x; then echo ok; fi\n}\n")
	mustWrite(t, filepath.Join(dir, "me", "r.sh"), "ours() {\n  echo o\n}\n")
	mustWrite(t, filepath.Join(dir, "up", "m.md"),
		"# T\n\n## A\n\n### Usage\n\nu\n\n## B\n\nthe word Usage appears here\n\n## C\n\n### Usage\n\nu2\n")
	mustWrite(t, filepath.Join(dir, "me", "m.md"), "## Ours\n\no\n")

	plan := func(tpl, name string) *build.Plan {
		t.Helper()
		mustWrite(t, filepath.Join(dir, "me", name+".lm"), tpl)
		c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
		if err != nil {
			t.Fatal(err)
		}
		p, err := build.PlanBuild(c, true)
		if err != nil {
			t.Fatalf("%s: %v", tpl, err)
		}
		return p
	}

	// curly has the letters in a word of its own; note has them in a comment. Neither calls it.
	mustWrite(t, filepath.Join(dir, "me", "m.md.lm"), "base.C.after(self.Ours)\n")
	p := plan("base.functions[calls \"curl\"].drop(reason: \"no network\")\nbase.note.after(self.ours)\n", "r.sh")
	var dropped []string
	for _, l := range p.Report.Dropped {
		dropped = append(dropped, l.Path)
	}
	want := "r.sh § fetch r.sh § piped r.sh § guarded"
	if got := strings.Join(dropped, " "); got != want {
		t.Errorf("calls found the wrong functions\n  got  %s\n  want %s", got, want)
	}

	// B has the word in its text; A and C hold a section by that name.
	plan("base.sections[has.\"Usage\" && level == 2].demote()\nbase.B.after(self.Ours)\n", "m.md")
	var product string
	for _, o := range plan("base.sections[has.\"Usage\" && level == 2].demote()\nbase.B.after(self.Ours)\n", "m.md").Outputs {
		if o.Rel == "m.md" {
			product = string(o.Data)
		}
	}
	got := headings(product)
	if !strings.Contains(got, "### A") || !strings.Contains(got, "### C") {
		t.Errorf("has did not find the sections that hold a Usage: %s", got)
	}
	if !strings.Contains(got, "## B") || strings.Contains(got, "### B") {
		t.Errorf("has matched B on the word in its text: %s", got)
	}

	// A yaml key holds keys too, and they nest by path rather than by level.
	mustWrite(t, filepath.Join(dir, "up", "w.yaml"), "jobs:\n  build:\n    runs-on: x\n  test:\n    needs: build\n")
	mustWrite(t, filepath.Join(dir, "me", "w.yaml"), "extra: 1\n")
	mustWrite(t, filepath.Join(dir, "me", "w.yaml.lm"), "base.keys[has.\"jobs.build.runs-on\"].drop(reason: \"we set it elsewhere\")\nbase.append(self.extra)\n")
	c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
	if err != nil {
		t.Fatal(err)
	}
	tm, err := lang.LoadTemplate(c, filepath.Join(dir, "me", "w.yaml.lm"))
	if err != nil {
		t.Fatal(err)
	}
	// jobs and jobs.build both hold that key, and jobs.test does not — so the two that matched
	// cover each other's lines, which is exactly what the overlap check is for. The spans it
	// names are the proof of which keys `has` found.
	_, err = build.Weave(c, tm)
	if err == nil {
		t.Fatal("dropping a key and the key inside it should collide")
	}
	if !strings.Contains(err.Error(), "(1-5 and 2-3)") {
		t.Errorf("has found the wrong keys: %v", err)
	}

	// Asked of one key instead of the group, only the ones that hold it answer.
	mustWrite(t, filepath.Join(dir, "me", "w.yaml.lm"), "base.keys[has.\"jobs.test.needs\"].drop(reason: \"ours\")\nbase.append(self.extra)\n")
	tm, err = lang.LoadTemplate(c, filepath.Join(dir, "me", "w.yaml.lm"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := build.Weave(c, tm); err == nil || !strings.Contains(err.Error(), "(1-5 and 4-5)") {
		t.Errorf("has should have found jobs and jobs.test: %v", err)
	}
}

// The six ways to compare a level, and the shape of a predicate: && binds tighter than ||, ! binds
// tighter than both, and each is left-associative. Getting any of those wrong gives an answer that
// looks reasonable and is not the one written.
func TestPredicateShapeAndComparisons(t *testing.T) {
	const doc = "# T\n\n## A\n\na\n\n### B\n\nb\n\n#### C\n\nc\n\n## D\n\nd\n"
	weave := newTree(t, "base \"up\"\nself \"me\"\n", doc, "## Ours\n\no\n")
	for _, c := range []struct{ cmp, want string }{
		{"== 3", "# T | ## A | #### B | #### C | ## D"},
		{"!= 3", "# T | ### A | ### B | ##### C | ### D"},
		{"< 3", "# T | ### A | ### B | #### C | ### D"},
		{"<= 3", "# T | ### A | #### B | #### C | ### D"},
		{"> 3", "# T | ## A | ### B | ##### C | ## D"},
		{">= 3", "# T | ## A | #### B | ##### C | ## D"},
	} {
		got, err := weave("base.sections[level " + c.cmp + "].demote()\n")
		if err != nil {
			t.Errorf("level %s: %v", c.cmp, err)
			continue
		}
		if h := headings(got); h != c.want {
			t.Errorf("level %s\n  got  %s\n  want %s", c.cmp, h, c.want)
		}
	}

	// && binds tighter than ||: level 2 alone, or level 3 that is also empty — and none is empty.
	got, err := weave("base.sections[level == 2 || level == 3 && empty].demote()\n")
	if err != nil {
		t.Fatal(err)
	}
	if h := headings(got); h != "# T | ### A | ### B | #### C | ### D" {
		t.Errorf("&& should bind tighter than ||: %s", h)
	}
	// With the grouping written the other way, nothing matches.
	if _, err := weave("base.sections[(level == 2 || level == 3) && empty].demote()\n"); err == nil {
		t.Error("nothing is empty, so this should have matched nothing")
	}
	// ! binds tighter than &&.
	got, err = weave("base.sections[!empty && level == 4].demote()\n")
	if err != nil {
		t.Fatal(err)
	}
	if h := headings(got); h != "# T | ## A | ### B | ##### C | ## D" {
		t.Errorf("! should bind tighter than &&: %s", h)
	}
}
