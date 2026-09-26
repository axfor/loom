package build_test

// Everything SYNTAX.md adds to the statement language, each construct woven against a real tree
// and checked by what it produced — not by what it parsed into.

import (
	"path/filepath"
	"regexp"
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
		// Axes chain: walk to a group, take one of it, walk again.
		{`base.Setup.children.first.demote()`, "# T | ## Setup | #### Step A | ### Step B | ## Other"},
		{`base.sections[level == 2].first.children.demote()`, "# T | ## Setup | #### Step A | #### Step B | ## Other"},
		{`base.Setup.next.prev.demote()`, "# T | ### Setup | ### Step A | ### Step B | ## Other"},
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
		{`base.Setup.children.first.first.demote()`, "picks from a group"},
		{`base.Setup.children.next.demote()`, "walks from one node"},
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
		// Two of the same kind are told apart by their section, not their kind; and a section with
		// no name cannot be named until it has one, which is what the author is told to do.
		{"base.Setup.after(self.job)\n---\nSelf as a:\n    ```markdown\n    ## job\n    ```\n---\nSelf as b:\n    ```markdown\n    ## job\n    ```\n", "name the section: self.a.\"job\""},
		{"base.Setup.after(self.job)\n---\nSelf:\n    ```markdown\n    ## job\n    ```\n---\nSelf as b:\n    ```markdown\n    ## job\n    ```\n", "give it one: Self as main:"},
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
	if !strings.Contains(err.Error(), "writes inside upstream lines") {
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

	// None holds and there is no else: nothing is written, and the report says how each question
	// went — every link that did not run, not only the last.
	p, _ := run("if base.has.Nope {\n\tbase.Setup.after(self.Ours)\n} else if base.has.Missing {\n\tbase.Other.after(self.Ours)\n}\nbase.Other.after(self.Ours)\n")
	if len(p.Report.Skipped) != 2 {
		t.Fatalf("each link that did not run is reported: %+v", p.Report.Skipped)
	}
	if !strings.Contains(p.Report.Skipped[0].Detail, "base.has.Nope did not hold") || !strings.Contains(p.Report.Skipped[1].Detail, "base.has.Missing did not hold") {
		t.Errorf("the report names each question, in order: %+v", p.Report.Skipped)
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
	if !strings.Contains(err.Error(), "upstream lines 2-3") {
		t.Errorf("has found the wrong keys: %v", err)
	}

	// Asked of one key instead of the group, only the ones that hold it answer.
	mustWrite(t, filepath.Join(dir, "me", "w.yaml.lm"), "base.keys[has.\"jobs.test.needs\"].drop(reason: \"ours\")\nbase.append(self.extra)\n")
	tm, err = lang.LoadTemplate(c, filepath.Join(dir, "me", "w.yaml.lm"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := build.Weave(c, tm); err == nil || !strings.Contains(err.Error(), "upstream lines 4-5") {
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

// The rest of what the syntax adds, each against a real product: pointing base somewhere else,
// counting instead of asking, values as a class, resource documents that are not markdown,
// importing alongside a resource section, and what err.format does with its arguments.
func TestTheRestOfTheSyntax(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\nmark shell \"# B\" \"# E\"\nmark toml \"# B\" \"# E\"\n")
	mustWrite(t, filepath.Join(dir, "up", "old", "f.md"), "# T\n\n## Setup\n\nu\n")
	mustWrite(t, filepath.Join(dir, "me", "notes.md"), "## Note\n\nn\n")
	mustWrite(t, filepath.Join(dir, "up", "r.sh"), "#!/bin/sh\nmain() {\n  echo up\n}\n")
	mustWrite(t, filepath.Join(dir, "up", "c.toml"), "a = 1\nb = 2\n")

	weave := func(name, tpl string) (string, error) {
		mustWrite(t, filepath.Join(dir, "me", name+".lm"), tpl)
		c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
		if err != nil {
			t.Fatal(err)
		}
		tm, err := lang.LoadTemplate(c, filepath.Join(dir, "me", name+".lm"))
		if err != nil {
			return "", err
		}
		return build.Weave(c, tm)
	}
	const res = "\n---\nSelf:\n    ```markdown\n    ## job\n\n    x\n    ```\n"

	// base = "...": upstream moved the file this template is woven onto.
	got, err := weave("f.md", "base = \"/old/f.md\"\nbase.Setup.after(self.job)"+res)
	if err != nil {
		t.Fatalf("base = : %v", err)
	}
	if !strings.Contains(got, "## Setup") || !strings.Contains(got, "## job") {
		t.Errorf("base did not move:\n%s", got)
	}

	// .count asks the same question .any does: did the predicate find anything.
	if _, err := weave("f.md", "base = \"/old/f.md\"\nif base.sections[level == 2].count {\n\tbase.Setup.after(self.job)\n}"+res); err != nil {
		t.Errorf(".count: %v", err)
	}

	// An import and a resource section live together: self is the section, the import has its
	// own name.
	got, err = weave("f.md", "base = \"/old/f.md\"\nimport n \"/notes\"\nbase.Setup.after(n.Note)\nbase.append(self.job)"+res)
	if err != nil {
		t.Fatalf("import beside a resource section: %v", err)
	}
	if !strings.Contains(got, "## Note") || !strings.Contains(got, "## job") {
		t.Errorf("one of the two sources did not arrive:\n%s", got)
	}

	// A resource document is whatever its fence says it is.
	got, err = weave("r.sh", "base.main.after(self.helper)\n---\nSelf:\n    ```shell\n    helper() {\n      echo ours\n    }\n    ```\n")
	if err != nil {
		t.Fatalf("a shell resource: %v", err)
	}
	if !strings.Contains(got, "helper() {") {
		t.Errorf("the shell document did not reach the product:\n%s", got)
	}

	// values names the same nodes keys does — what differs is what you then do with them.
	for _, cls := range []string{"keys", "values"} {
		got, err = weave("c.toml", "base."+cls+"[name == \"b\"].drop(reason: \"ours\")\nbase.append(self.extra)\n---\nSelf:\n    ```toml\n    extra = 3\n    ```\n")
		if err != nil {
			t.Fatalf("%s: %v", cls, err)
		}
		if strings.Contains(got, "b = 2") || !strings.Contains(got, "extra = 3") {
			t.Errorf("%s:\n%s", cls, got)
		}
	}

	// err.format takes the message and what goes into it, and only the message is formatted.
	_, err = weave("f.md", "base = \"/old/f.md\"\nok = base.Nope.after(self.job)\nif !ok {\n\treturn err.format(\"100%% done, twice: %s / %s\", ok, ok)\n}"+res)
	if err == nil {
		t.Fatal("err.format should fail the build")
	}
	if !strings.Contains(err.Error(), "100% done, twice:") || strings.Count(err.Error(), "Nope not found") != 2 {
		t.Errorf("the message did not come out as written: %v", err)
	}
	// The count of placeholders and of values has to agree. Left unchecked the build still fails —
	// it is a return err, after all — but with %!s(MISSING) in the message instead of a word about
	// the typo, so the test has to look at what it says.
	_, err = weave("f.md", "base = \"/old/f.md\"\nok = base.Nope.after(self.job)\nif !ok {\n\treturn err.format(\"%s and %s\", ok)\n}"+res)
	if err == nil {
		t.Fatal("two placeholders and one value were accepted")
	}
	if !strings.Contains(err.Error(), "2 placeholder(s) and 1 value(s)") {
		t.Errorf("the mismatch should be named where it is written: %v", err)
	}

	// The path may be written with or without a leading slash: both name the same upstream file.
	for _, spec := range []string{"/old/f.md", "old/f.md"} {
		got, err := weave("f.md", "base = \""+spec+"\"\nbase.Setup.after(self.job)"+res)
		if err != nil {
			t.Errorf("base = %q: %v", spec, err)
		} else if !strings.Contains(got, "## Setup") {
			t.Errorf("base = %q did not reach the file:\n%s", spec, got)
		}
	}
}

// The spellings SYNTAX.md's own appendix uses, each of which the implementation refused until now.
// The appendix is a checklist of every construct; a language that does not accept its own
// checklist has a gap whatever its tests say.
func TestTheSpellingsTheAppendixUses(t *testing.T) {
	const doc = "---\nd: Up.\n---\n\n# T\n\n## A\n\na\n\n### AA\n\naa\n\n## B\n\nb\n\n## C\n\nc\n"
	weave := newTree(t, "base \"up\"\nself \"me\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n", doc, "---\nd: Ours.\n---\n\n## Ours\n\no\n")
	const fm = "base.frontmatter.d.start(self.frontmatter.d)\n"

	for _, c := range []struct{ tpl, want string }{
		// has["X"] and has."X" ask the same thing.
		{`base.sections[has["AA"]].demote()`, "# T | ### A | ### AA | ## B | ## C"},
		// move takes a place as well as a side.
		{`base.A.move(base.B.after)`, "# T | ### AA | ## B | ## A | ## C"},
		// A bare class name is every node of that kind.
		{`base.sections.demote()`, "# T | ### A | #### AA | ### B | ### C"},
	} {
		got, err := weave(fm + c.tpl + "\n")
		if err != nil {
			t.Errorf("%s: %v", c.tpl, err)
			continue
		}
		if h := headings(got); h != c.want {
			t.Errorf("%s\n  got  %s\n  want %s", c.tpl, h, c.want)
		}
	}

	// end writes a value after upstream's, which is what append has always done — and it is
	// turned into append rather than carried as a second word, so the engine has one mode to
	// reason about and the report says what happened in the words it already uses.
	got, err := weave("base.frontmatter.d.end(self.frontmatter.d)\n")
	if err != nil {
		t.Fatalf("value end: %v", err)
	}
	if !strings.Contains(got, "d: Up. Ours.") {
		t.Errorf("end should put ours after upstream's:\n%s", got)
	}
	{
		dir2 := t.TempDir()
		mustWrite(t, filepath.Join(dir2, "loom.om"), "base \"up\"\nself \"me\"\n")
		mustWrite(t, filepath.Join(dir2, "up", "g.md"), "---\nd: Up.\n---\n\n## A\n\na\n")
		mustWrite(t, filepath.Join(dir2, "me", "g.md"), "---\nd: Ours.\n---\n")
		mustWrite(t, filepath.Join(dir2, "me", "g.md.lm"), "base.frontmatter.d.end(self.frontmatter.d)\n")
		c2, err := lang.LoadConfig(filepath.Join(dir2, "loom.om"))
		if err != nil {
			t.Fatal(err)
		}
		p2, err := build.PlanBuild(c2, true)
		if err != nil {
			t.Fatal(err)
		}
		var said string
		for _, l := range p2.Report.Extended {
			said += l.Detail
		}
		if !strings.Contains(said, "(append)") || strings.Contains(said, "(end)") {
			t.Errorf("the report should say append, the word the engine has: %q", said)
		}
	}

	// project on a derived address, with the template on lines of its own.
	got, err = weave(fm + "base.start.project(base.sections[level == 2]){\n```markdown\n- {name}\n```\n}\n")
	if err != nil {
		t.Fatalf("project at a place: %v", err)
	}
	if !strings.Contains(got, "- A\n- B\n- C") {
		t.Errorf("the projection did not land at the start:\n%s", got)
	}
	// And at the end, reading a whole class.
	got, err = weave(fm + "base.append.project(base.sections){\n```markdown\n- {name} ({level})\n```\n}\n")
	if err != nil {
		t.Fatalf("project at the end: %v", err)
	}
	if !strings.Contains(got, "- AA (3)") {
		t.Errorf("a bare class did not reach the projection:\n%s", got)
	}

	// An empty call is still a call someone meant to fill in.
	if _, err := weave(fm + "base.sections().demote()\n"); err == nil || !strings.Contains(err.Error(), "needs a predicate") {
		t.Errorf("base.sections() should still ask for a predicate: %v", err)
	}
}

// SYNTAX.md §10: content and the place it lands in have to be the same kind of document. A shell
// function pasted into a markdown section carries no heading, so the accounting that works by name
// cannot see it — upstream stays whole and our own content ends up on no ledger at all.
func TestContentHasToFitWhereItLands(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\nmark toml \"# B\" \"# E\"\n")
	mustWrite(t, filepath.Join(dir, "up", "f.md"), "# T\n\n## A\n\na\n\n## B\n\nb\n")
	mustWrite(t, filepath.Join(dir, "me", "f.md"), "## Ours\n\no\n")
	mustWrite(t, filepath.Join(dir, "me", "r.sh"), "#!/bin/sh\nboot() {\n  echo b\n}\n")
	mustWrite(t, filepath.Join(dir, "up", "c.toml"), "description = \"u\"\nprompt = \"\"\"\n## Steps\n\nu\n\"\"\"\n")
	mustWrite(t, filepath.Join(dir, "me", "c.toml"), "description = \"o\"\nprompt = \"\"\"\n## Ours\n\no\n\"\"\"\n")

	weave := func(name, tpl string) error {
		mustWrite(t, filepath.Join(dir, "me", name+".lm"), tpl)
		c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
		if err != nil {
			t.Fatal(err)
		}
		tm, err := lang.LoadTemplate(c, filepath.Join(dir, "me", name+".lm"))
		if err != nil {
			return err
		}
		_, err = build.Weave(c, tm)
		return err
	}

	// A shell function does not go into a markdown section.
	err := weave("f.md", "import r \"/r.sh\"\nbase.A.after(r.boot)\nbase.B.after(self.Ours)\n")
	if err == nil {
		t.Fatal("a shell function was written into a markdown section")
	}
	if !strings.Contains(err.Error(), "is shell and this writes into markdown") {
		t.Errorf("unexpected error: %v", err)
	}

	// Our own file, read through a view, is the view's type: base.prompt.as(markdown) makes
	// self.body the markdown in our prompt rather than the toml around it.
	if err := weave("c.toml", "base.description.start(self.description)\nbase.prompt.as(markdown).append(self.body)\n"); err != nil {
		t.Errorf("our own file inside a view should fit: %v", err)
	}
	// A file named explicitly is still whatever its own extension says, view or no view.
	err = weave("c.toml", "base.description.start(self.description)\nimport r \"/r.sh\"\nbase.prompt.as(markdown).append(r.boot)\n")
	if err == nil || !strings.Contains(err.Error(), "is shell and this writes into markdown") {
		t.Errorf("an imported shell file should not fit a markdown view: %v", err)
	}

	// A fence says what it holds with its tag, and that is checked the same way.
	err = weave("f.md", "base.A.after{\n```json\n{}\n```\n}\nbase.B.after(self.Ours)\n")
	if err == nil || !strings.Contains(err.Error(), "holds json and it writes into markdown") {
		t.Errorf("a tagged fence should be checked: %v", err)
	}
	// A tag the language knows nothing about is a label for the reader, not a claim it can check.
	for _, tag := range []string{"", "nope", "python"} {
		if err := weave("f.md", "base.A.after{\n```"+tag+"\nx\n```\n}\nbase.B.after(self.Ours)\n"); err != nil {
			t.Errorf("a fence tagged %q should pass: %v", tag, err)
		}
	}
	// sh and bash name a type this language has, so they are checked.
	if err := weave("f.md", "base.A.after{\n```sh\necho hi\n```\n}\nbase.B.after(self.Ours)\n"); err == nil {
		t.Error("a fence tagged sh should not go into markdown")
	}
}

// split at a heading already inside the section needs no name: that heading comes up to this
// section's level and becomes the second half, with everything under it following.
func TestSplitAtAHeading(t *testing.T) {
	const doc = "# T\n\n## Setup\n\nintro\n\n### Step A\n\na\n\n#### Deep\n\nd\n\n### Step B\n\nb\n\n## Other\n\no\n"
	weave := newTree(t, "base \"up\"\nself \"me\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n", doc, "## Ours\n\no\n")

	got, err := weave("base.Setup.split(base.Setup.\"Step B\")\nbase.Other.after(self.Ours)\n")
	if err != nil {
		t.Fatalf("split at a heading: %v", err)
	}
	want := "# T | ## Setup | ### Step A | #### Deep | ## Step B | ## Other | ## Ours"
	if h := headings(got); h != want {
		t.Errorf("\n  got  %s\n  want %s", h, want)
	}
	// Nothing was added, so upstream is still proved whole — the guarantee a level change keeps.
	// That has to be asked of the accounting, not of the product: weaving alone does not check.
	if strings.Contains(got, "<!-- B -->\n## Step B") {
		t.Error("the second half is upstream's own heading, so it wears no marks of ours")
	}
	{
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n")
		mustWrite(t, filepath.Join(dir, "up", "f.md"), doc)
		mustWrite(t, filepath.Join(dir, "me", "f.md"), "## Ours\n\no\n")
		mustWrite(t, filepath.Join(dir, "me", "f.md.lm"), "base.Setup.split(base.Setup.\"Step B\")\nbase.Other.after(self.Ours)\n")
		c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
		if err != nil {
			t.Fatal(err)
		}
		p, err := build.PlanBuild(c, true)
		if err != nil {
			t.Fatalf("a nameless split is a level change, and the accounting has to know: %v", err)
		}
		var said string
		for _, l := range p.Report.Moved {
			said += l.Detail + " " + l.Path
		}
		if !strings.Contains(said, "Setup") {
			t.Errorf("it should be reported as restructured: %+v", p.Report.Moved)
		}
	}

	// Cutting somewhere that is not a heading still needs a name for the half it makes.
	if _, err := weave("base.Setup.split(base.Setup.line(\"intro\"))\nbase.Other.after(self.Ours)\n"); err == nil {
		t.Error("cutting at a line with no name was accepted")
	}
	// And a heading outside the section is not a cut at all.
	if _, err := weave("base.Setup.split(base.Other)\nbase.Other.after(self.Ours)\n"); err == nil {
		t.Error("a heading outside the section was accepted as a cut")
	}
}

// A bare string names a section of ours in Loom 1 and is the text itself from Loom 2. The meaning
// could not change under trees already written, so what a tree declares decides — and a tree that
// declares nothing is read the way it always was.
func TestBareStringDependsOnTheVersion(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "up", "f.md"), "# T\n\n## A\n\na\n")
	mustWrite(t, filepath.Join(dir, "me", "f.md"), "## Where this fits\n\nours\n")
	mustWrite(t, filepath.Join(dir, "me", "f.md.lm"), "base.A.after(\"Where this fits\")\nbase.append(self.\"Where this fits\")\n")

	for _, c := range []struct{ decl, want string }{
		{"", "## Where this fits"},
		{"loom \"1.0\"\n", "## Where this fits"},
		{"loom \"2.0\"\n", "Where this fits"},
	} {
		mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n"+c.decl)
		cfg, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
		if err != nil {
			t.Fatal(err)
		}
		tm, err := lang.LoadTemplate(cfg, filepath.Join(dir, "me", "f.md.lm"))
		if err != nil {
			t.Fatalf("%q: %v", c.decl, err)
		}
		got, err := build.Weave(cfg, tm)
		if err != nil {
			t.Fatalf("%q: %v", c.decl, err)
		}
		first := strings.SplitN(strings.SplitN(got, "<!-- B -->\n", 2)[1], "\n", 2)[0]
		if first != c.want {
			t.Errorf("declared %q: the string landed as %q, want %q", c.decl, first, c.want)
		}
	}
}

// A dotted name matches the name, one character for one character, from Loom 2. Before that an
// underscore stood for a space; a tree written then still means what it meant.
func TestDottedNameDependsOnTheVersion(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "up", "f.md"), "# T\n\n## How Skills Work\n\nu\n\n## Other\n\no\n")
	mustWrite(t, filepath.Join(dir, "me", "f.md"), "## Ours\n\no\n")
	weave := func(decl, tpl string) error {
		mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n"+decl)
		mustWrite(t, filepath.Join(dir, "me", "f.md.lm"), tpl)
		c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
		if err != nil {
			t.Fatal(err)
		}
		tm, err := lang.LoadTemplate(c, filepath.Join(dir, "me", "f.md.lm"))
		if err != nil {
			return err
		}
		_, err = build.Weave(c, tm)
		return err
	}
	const under = "base.How_Skills_Work.after(self.Ours)\n"
	const quoted = "base.\"How Skills Work\".after(self.Ours)\n"

	if err := weave("", under); err != nil {
		t.Errorf("a tree that declares nothing keeps the underscore rule: %v", err)
	}
	if err := weave("loom \"1.0\"\n", under); err != nil {
		t.Errorf("Loom 1 keeps the underscore rule: %v", err)
	}
	if err := weave("loom \"2.0\"\n", under); err == nil {
		t.Error("Loom 2 should look for the name as written")
	}
	if err := weave("loom \"2.0\"\n", quoted); err != nil {
		t.Errorf("Loom 2 takes the name in quotes: %v", err)
	}

	// Every segment of a path is a name, not only the first: below a section, and on our side.
	mustWrite(t, filepath.Join(dir, "up", "f.md"), "# T\n\n## How Skills Work\n\n### Step One\n\nu\n\n## Other\n\no\n")
	mustWrite(t, filepath.Join(dir, "me", "f.md"), "## Our Part\n\n### Sub Part\n\no\n")
	for _, tpl := range []string{
		"base.\"How Skills Work\".Step_One.after(self.\"Our Part\")\n",
		"base.Other.after(self.Our_Part)\n",
		"base.Other.after(self.\"Our Part\".Sub_Part)\n",
	} {
		if err := weave("", tpl); err != nil {
			t.Errorf("Loom 1 reads every segment by the underscore rule: %s%v", tpl, err)
		}
		if err := weave("loom \"2.0\"\n", tpl); err == nil {
			t.Errorf("Loom 2 looks for every segment as written, and there is no such name: %s", tpl)
		}
	}
}

// `value` asks what a key holds. It is not `empty`, which asks whether anything is under the key:
// `a = 1` and `b = ""` are both empty by that question, and only this one tells them apart.
func TestValuePredicate(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\nmark toml \"# B\" \"# E\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n")
	mustWrite(t, filepath.Join(dir, "up", "c.toml"), "a = 1\nb = \"\"\nc = \"keep\"\n")
	mustWrite(t, filepath.Join(dir, "me", "c.toml"), "x = 1\n")
	weave := func(name, tpl string) (string, error) {
		mustWrite(t, filepath.Join(dir, "me", name+".lm"), tpl)
		c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
		if err != nil {
			t.Fatal(err)
		}
		tm, err := lang.LoadTemplate(c, filepath.Join(dir, "me", name+".lm"))
		if err != nil {
			return "", err
		}
		return build.Weave(c, tm)
	}
	kept := func(s string) string {
		var out []string
		for _, l := range strings.Split(s, "\n") {
			if regexp.MustCompile(`^[abc] = `).MatchString(l) {
				out = append(out, l)
			}
		}
		return strings.Join(out, " ")
	}
	for _, c := range []struct{ pred, want string }{
		{`value == ""`, `a = 1 c = "keep"`},
		{`value != ""`, `b = ""`},
		{`value ~ "^ke"`, `a = 1 b = ""`},
		{`value == "1"`, `b = "" c = "keep"`},
	} {
		got, err := weave("c.toml", "base.keys["+c.pred+"].drop(reason: \"ours\")\nbase.append(self.x)\n")
		if err != nil {
			t.Errorf("%s: %v", c.pred, err)
			continue
		}
		if k := kept(got); k != c.want {
			t.Errorf("%s\n  kept %s\n  want %s", c.pred, k, c.want)
		}
	}

	// empty asks a different question, and on these keys it finds all three.
	got, err := weave("c.toml", "base.keys[empty].drop(reason: \"ours\")\nbase.append(self.x)\n")
	if err != nil {
		t.Fatal(err)
	}
	if k := kept(got); k != "" {
		t.Errorf("empty is about what sits under a key, so all three are empty; kept %s", k)
	}

	// A heading holds no value of its own, and is told so where it is written.
	mustWrite(t, filepath.Join(dir, "up", "f.md"), "# T\n\n## A\n\na\n")
	mustWrite(t, filepath.Join(dir, "me", "f.md"), "## Ours\n\no\n")
	if _, err := weave("f.md", "base.sections[value == \"\"].drop(reason: \"x\")\n"); err == nil {
		t.Error("value on a markdown section was accepted")
	} else if !strings.Contains(err.Error(), "holds no value of its own") {
		t.Errorf("unexpected error: %v", err)
	}
}

// The grammar lists `lines` beside sections and keys, and it had nothing to select: lines were
// findable by name and not enumerable, so a group of them was always empty. A line is named by
// what it says, which is what Find already matched on.
func TestLinesAreAClass(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\nmark text \"# B\" \"# E\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n")
	mustWrite(t, filepath.Join(dir, "up", "t.txt"), "keep me\ndrop l1\ndrop l2\n\n")
	mustWrite(t, filepath.Join(dir, "me", "t.txt"), "ours line\n")
	mustWrite(t, filepath.Join(dir, "up", "f.md"), "# T\n\n## A\n\nalpha\n\nbeta\n")
	mustWrite(t, filepath.Join(dir, "me", "f.md"), "## Ours\n\no\n")
	weave := func(name, tpl string) (string, error) {
		mustWrite(t, filepath.Join(dir, "me", name+".lm"), tpl)
		c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
		if err != nil {
			t.Fatal(err)
		}
		tm, err := lang.LoadTemplate(c, filepath.Join(dir, "me", name+".lm"))
		if err != nil {
			return "", err
		}
		return build.Weave(c, tm)
	}

	// A predicate over lines, in a text file.
	got, err := weave("t.txt", "base.lines[name ~ \"^drop \"].drop(reason: \"ours covers them\")\nbase.append(self.body)\n")
	if err != nil {
		t.Fatalf("lines in text: %v", err)
	}
	if strings.Contains(got, "drop l1") || !strings.Contains(got, "keep me") {
		t.Errorf("the predicate picked the wrong lines:\n%s", got)
	}

	// And in markdown, where a line is a node beside a section rather than instead of one.
	got, err = weave("f.md", "base.lines[name == \"beta\"].drop(reason: \"ours says it\")\nbase.A.after(self.Ours)\n")
	if err != nil {
		t.Fatalf("lines in markdown: %v", err)
	}
	if strings.Contains(got, "beta") || !strings.Contains(got, "alpha") {
		t.Errorf("the predicate picked the wrong lines:\n%s", got)
	}

	// A blank line has no name, so nothing selects it — otherwise a file would be full of nodes
	// all called the same thing, and any anchor could match any of them. Upstream's blank line is
	// still there afterwards, which is how that shows.
	got, err = weave("t.txt", "base.lines.drop(reason: \"all of upstream's\")\nbase.append(self.body)\n")
	if err != nil {
		t.Fatalf("every line: %v", err)
	}
	if want := "\n\n# B\nours line\n# E\n"; got != want {
		t.Errorf("every named line goes and the blank one stays\n  got  %q\n  want %q", got, want)
	}
}

// A projection template names fields, and a field there is no value for used to go into the
// product as written — `{anchor}` became a link to "#{anchor}", with the build saying nothing.
// anchor is GitHub's rule for a heading's link target, named for whose rule it is.
func TestProjectionFields(t *testing.T) {
	const doc = "# T\n\n## Quick Start (Any Agent)\n\na\n\n## How It Works\n\nb\n\n## 安装说明\n\nc\n\n## How It Works\n\nd\n"
	weave := newTree(t, "base \"up\"\nself \"me\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n", doc, "## Ours\n\no\n")

	got, err := weave("base.start.project(base.sections[level == 2]){\n```markdown\n- [{name}](#{anchor})\n```\n}\nbase.append(self.Ours)\n")
	if err != nil {
		t.Fatalf("anchor: %v", err)
	}
	for _, want := range []string{
		"- [Quick Start (Any Agent)](#quick-start-any-agent)", // punctuation goes, spaces become hyphens
		"- [How It Works](#how-it-works)",
		"- [安装说明](#安装说明)",                   // letters are any script's
		"- [How It Works](#how-it-works-1)", // the second of a name is numbered
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}

	// A field with no value is refused where it is written, not passed into the product.
	_, err = weave("base.start.project(base.sections[level == 2]){\n```markdown\n- {nmae}\n```\n}\nbase.append(self.Ours)\n")
	if err == nil {
		t.Fatal("a misspelt field was accepted")
	}
	if !strings.Contains(err.Error(), "did you mean name") {
		t.Errorf("unexpected error: %v", err)
	}
	// The same check holds for the one-line form.
	if _, err := weave("base.start(base.sections[level == 2].project(`- {slug}`))\nbase.append(self.Ours)\n"); err == nil {
		t.Error("an unknown field in the one-line form was accepted")
	}
}

// The same path is read the same way wherever it is written. Loom 1 reads an unquoted segment by
// the underscore rule where a statement starts — and where a move, swap or split points, and in
// what an if asks, which had kept only some of it.
func TestEveryPathIsReadTheSameWay(t *testing.T) {
	const up = "# T\n\n## How It\n\n### Step A\n\na\n\n## Other\n\no\n"
	for _, c := range []struct{ decl, tpl string }{
		{"", "base.Other.move(after: base.How_It.Step_A)\n"},
		{"", "base.Other.move(base.How_It.Step_A.after)\n"},
		{"", "base.Other.swap(base.How_It.Step_A)\n"},
		{"", "base.How_It.split(base.How_It.Step_A)\n"},
	} {
		weave := newTree(t, "base \"up\"\nself \"me\"\n"+c.decl, up, "")
		if _, err := weave(c.tpl); err != nil {
			t.Errorf("Loom 1 reads every segment by the underscore rule: %s%v", c.tpl, err)
		}
		weave2 := newTree(t, "base \"up\"\nself \"me\"\nloom \"2.0\"\n", up, "")
		if _, err := weave2(c.tpl); err == nil {
			t.Errorf("Loom 2 reads every segment as written, and there is no How_It: %s", c.tpl)
		}
	}

	// if base.has.How_It asks about "How It" in Loom 1: the then-branch is taken.
	weave := newTree(t, "base \"up\"\nself \"me\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n", up, "## Ours\n\no\n")
	got, err := weave("if base.has.How_It {\n    base.Other.after(self.Ours)\n} else {\n    base.start(self.Ours)\n}\n")
	if err != nil {
		t.Fatal(err)
	}
	if h := headings(got); h != "# T | ## How It | ### Step A | ## Other | ## Ours" {
		t.Errorf("has.How_It found \"How It\", so ours goes after Other:\n  %s", h)
	}
}

// A node moved or promoted out of its parent is accounted for where it now is. Its path says
// where it was; looking for it there reported a promote that had worked as a node gone missing.
func TestLeavingItsParentIsNotGoingMissing(t *testing.T) {
	for _, tpl := range []string{
		"base.Setup.Step_A.promote()\nbase.Other.after(self.Ours)\n",
		"base.Setup.Step_A.move(after: base.Other)\nbase.Other.after(self.Ours)\n",
	} {
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n")
		mustWrite(t, filepath.Join(dir, "up", "a.md"), "# T\n\n## Setup\n\ns\n\n### Step A\n\na\n\n## Other\n\no\n")
		mustWrite(t, filepath.Join(dir, "me", "a.md"), "## Ours\n\nours\n")
		mustWrite(t, filepath.Join(dir, "me", "a.md.lm"), tpl)
		c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := buildTree(t, c, filepath.Join(dir, "out")); err != nil {
			t.Errorf("%s%v", tpl, err)
		}
	}
}

// Dropping a key takes the keys nested under it, and accounts for them: they are inside its lines.
// Dropping a markdown section does not, since a section ends at the next heading.
func TestDroppingAKeyTakesWhatIsUnderIt(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\n")
	mustWrite(t, filepath.Join(dir, "up", "ci.yaml"), "jobs:\n  old:\n    runs-on: ubuntu\n  keep:\n    runs-on: macos\n")
	mustWrite(t, filepath.Join(dir, "me", "ci.yaml.lm"), "base.jobs.old.drop(reason: \"we do not run it\")\n")
	c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	if _, err := buildTree(t, c, out); err != nil {
		t.Fatalf("a dropped key accounts for what is under it: %v", err)
	}
	if got := readFile(t, filepath.Join(out, "ci.yaml")); got != "jobs:\n  keep:\n    runs-on: macos\n" {
		t.Errorf("the key and what is under it are gone, the rest stays:\n%s", got)
	}
}

// A text file's lines are accounted for like any other type's nodes. A merge that rewrote an
// upstream line and said nothing used to pass: the line was simply gone. Now it is named, with the
// statement that accounts for it — and that statement is checked to be true.
func TestATextFileLosesNoLineInSilence(t *testing.T) {
	const up = "// check the skills\nconst a = require('./a');\nrun(a);\n"
	build := func(ours, tpl string) error {
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\n")
		mustWrite(t, filepath.Join(dir, "up", "v.js"), up)
		mustWrite(t, filepath.Join(dir, "me", "v.js"), ours)
		mustWrite(t, filepath.Join(dir, "me", "v.lm"), tpl)
		c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
		if err != nil {
			t.Fatal(err)
		}
		_, err = buildTree(t, c, filepath.Join(dir, "out"))
		return err
	}
	const ours = "// check the skills, ours too\nconst a = require('./a');\nrun(a);\n"
	err := build(ours, "base.merge(self)\n")
	if err == nil || !strings.Contains(err.Error(), `line "// check the skills" is not in the product`) ||
		!strings.Contains(err.Error(), `base.line("// check the skills").drop(reason:`) {
		t.Errorf("a rewritten line is lost unless a reason is given, and the error says how: %v", err)
	}
	if err := build(ours, "base.merge(self)\nbase.line(\"// check the skills\").drop(reason: \"we check ours too\")\n"); err != nil {
		t.Errorf("a line dropped with a reason is accounted for: %v", err)
	}
	// A drop is a claim, and a merge's product is checked against it.
	if err := build(up, "base.merge(self)\nbase.line(\"// check the skills\").drop(reason: \"x\")\n"); err == nil || !strings.Contains(err.Error(), "still there") {
		t.Errorf("a line declared dropped that is still there is refused: %v", err)
	}
	if err := build(up+"more();\n", "base.merge(self)\n"); err != nil {
		t.Errorf("adding lines loses nothing: %v", err)
	}
}

// Where a move, swap or split points is read as the same path is read where it starts a
// statement: axes, a node picked from a group, a kind such as line("…"). They had been taken for
// heading names, so the spec's own base.Setup.split(base.Setup.children.first) found no "children".
func TestAMoveTargetIsReadLikeAStatement(t *testing.T) {
	const up = "# T\n\n## Overview\n\no\n\n## Setup\n\nintro\n\n### Step A\n\na\n\n### Step B\n\nb\n\n## B\n\nbb\n"
	for _, c := range []struct{ tpl, want string }{
		{"base.Setup.split(base.Setup.children.first)\n", "# T | ## Overview | ## Setup | ## Step A | ### Step B | ## B"},
		{"base.B.move(after: base.Setup.children.first)\n", "# T | ## Overview | ## Setup | ### Step A | ## B | ### Step B"},
		// a section is its heading to the next heading, so Setup's subsections stay where they are
		{"base.B.swap(base.Overview.next)\n", "# T | ## Overview | ## B | ### Step A | ### Step B | ## Setup"},
		{"base.B.move(base.Overview.after)\n", "# T | ## Overview | ## B | ## Setup | ### Step A | ### Step B"},
		{"base.B.move(before: base.sections[level == 3].last)\n", "# T | ## Overview | ## Setup | ### Step A | ## B | ### Step B"},
		{"base.Setup.split(base.line(\"intro\"), \"Setup, part two\")\n", "# T | ## Overview | ## Setup | ## Setup, part two | ### Step A | ### Step B | ## B"},
	} {
		weave := newTree(t, "base \"up\"\nself \"me\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n", up, "")
		got, err := weave(c.tpl)
		if err != nil {
			t.Errorf("%s%v", c.tpl, err)
			continue
		}
		if h := headings(got); h != c.want {
			t.Errorf("%s  got  %s\n  want %s", c.tpl, h, c.want)
		}
	}
}

// The advice an error gives has to be something that builds. Two gave forms the parser refuses: a
// { } block on one line, and a line("…") below a section.
func TestTheAdviceInAnErrorBuilds(t *testing.T) {
	const up = "# T\n\n## Setup\n\nintro\n\n## B\n\nbb\n"
	weave := newTree(t, "base \"up\"\nself \"me\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n", up, "")
	_, err := weave("base.Setup.split()\n")
	if err == nil || !strings.Contains(err.Error(), `base.X.split(base.line("…"), "X, part two")`) {
		t.Fatalf("split says how to cut at a line: %v", err)
	}
	if _, err := weave("base.Setup.split(base.line(\"intro\"), \"Setup, part two\")\n"); err != nil {
		t.Errorf("and that form builds: %v", err)
	}
	_, err = weave("base.start.project()\n")
	if err == nil || !strings.Contains(err.Error(), "base.start.project(base.sections[level == 2], `- {name}`)") {
		t.Fatalf("project says how it is written: %v", err)
	}
	if _, err := weave("base.start.project(base.sections[level == 2], `- {name}`)\n"); err != nil {
		t.Errorf("and that form builds: %v", err)
	}
	// Both spellings of a projection read the group the same way: a bare class is all of it.
	if _, err := weave("base.start(base.sections.project(`- {name}`))\n"); err != nil {
		t.Errorf("a bare class projects in the one-line form too: %v", err)
	}
}

// An insertion at the line a rewrite starts on survives it. Edits apply back to front, and at the
// same line the later statement went first — so `base.Install.drop(...)` followed by
// `base.Install.before(x)` inserted x inside the span the drop then deleted by its old line
// numbers. The product lost our content and the build said nothing.
func TestAnInsertionWhereARewriteStartsSurvives(t *testing.T) {
	const up = "# T\n\n## Examples\n\nex\n\n## Install\n\ni\n\n## Usage\n\nu\n"
	for _, rewrite := range []string{
		`base.Install.drop(reason: "r")`,
		"base.Install.replace(`y`, reason: \"r\")",
		`base.Install.demote()`,
		`base.Install.move(after: base.Usage)`,
		`base.Install.swap(base.Usage)`,
	} {
		for _, insert := range []string{"base.Examples.after(`x`)", "base.Install.before(`x`)"} {
			weave := newTree(t, "base \"up\"\nself \"me\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n", up, "")
			got, err := weave(rewrite + "\n" + insert + "\n")
			if err != nil {
				t.Errorf("%s / %s: %v", rewrite, insert, err)
				continue
			}
			if !strings.Contains(got, "<!-- B -->\nx\n<!-- E -->") {
				t.Errorf("%s then %s lost our content:\n%s", rewrite, insert, got)
			}
		}
	}
}

// A split at a heading changes that heading's level and nothing else, and it is that heading the
// build checks — not the section being cut, whose own lines a split never touches.
func TestASplitChecksTheHeadingItCutsAt(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n")
	mustWrite(t, filepath.Join(dir, "up", "a.md"), "# T\n\n## Setup\n\nintro\n\n### Step B\n\nb\n\n## Other\n\no\n")
	mustWrite(t, filepath.Join(dir, "me", "a.md.lm"), "base.Setup.split(base.Setup.\"Step B\")\n")
	c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := buildTree(t, c, filepath.Join(dir, "out")); err != nil {
		t.Fatalf("a split at a heading builds: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "me", "a.md.lm"), "base.Setup.split(base.Setup.\"Step B\")\nbase.Setup.drop(reason: \"x\")\n")
	if _, err := buildTree(t, c, filepath.Join(dir, "out2")); err == nil || strings.Contains(err.Error(), "splitd") {
		t.Errorf("a split of a section also dropped is refused, in words: %v", err)
	}
}

// Anchor completion writes what the tree reads. From Loom 2 a bare string is the text itself, so
// a section of ours written back as "More Stuff" put those two words in the product instead of the
// section — and the report called it a literal.
func TestAnchorCompletionWritesOurSectionsByTheVersion(t *testing.T) {
	for _, c := range []struct{ decl, want string }{
		{"", "base.Overview.after(self.Ours, \"More Stuff\")\n"},
		{"loom \"2.0\"\n", "base.Overview.after(self.Ours, self.\"More Stuff\")\n"},
	} {
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n"+c.decl)
		mustWrite(t, filepath.Join(dir, "up", "a.md"), "# T\n\n## Overview\n\no\n")
		mustWrite(t, filepath.Join(dir, "me", "a.md"), "## Ours\n\no\n\n## More Stuff\n\nthe section itself\n")
		mustWrite(t, filepath.Join(dir, "me", "a.md.lm"), "base.Overview.after(self.Ours)\n")
		cfg, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
		if err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(dir, "out")
		if _, err := buildTree(t, cfg, out); err != nil {
			t.Fatalf("%q: %v", c.decl, err)
		}
		if got := readFile(t, filepath.Join(dir, "me", "a.md.lm")); got != c.want {
			t.Errorf("%q: completed as\n  %s  want\n  %s", c.decl, got, c.want)
		}
		if got := readFile(t, filepath.Join(out, "a.md")); !strings.Contains(got, "## More Stuff\n\nthe section itself") {
			t.Errorf("%q: the section itself is in the product, not its name:\n%s", c.decl, got)
		}
	}
}

// A section of ours reaches the product in the branch that runs. Anchor completion put a section
// beside its neighbour in one branch of an if only, and the check asked only whether some
// statement named it — so when upstream sent the build down the other branch, the section was
// gone and the build was green.
func TestOurSectionsReachTheProductWhicheverBranchRuns(t *testing.T) {
	const tpl = "if base.has.Overview {\n    base.Overview.after(self.Ours)\n} else {\n    base.start(self.Ours)\n}\n"
	for _, up := range []string{"# T\n\n## Overview\n\no\n", "# T\n\n## Intro\n\ni\n"} {
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n")
		mustWrite(t, filepath.Join(dir, "up", "a.md"), up)
		mustWrite(t, filepath.Join(dir, "me", "a.md"), "## Ours\n\no\n\n## Next\n\nn\n")
		mustWrite(t, filepath.Join(dir, "me", "a.md.lm"), tpl)
		cfg, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
		if err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(dir, "out")
		if _, err := buildTree(t, cfg, out); err != nil {
			t.Fatalf("%v", err)
		}
		want := "if base.has.Overview {\n    base.Overview.after(self.Ours, \"Next\")\n} else {\n    base.start(self.Ours, \"Next\")\n}\n"
		if got := readFile(t, filepath.Join(dir, "me", "a.md.lm")); got != want {
			t.Errorf("Next follows Ours into both branches:\n%s", got)
		}
		if got := readFile(t, filepath.Join(out, "a.md")); !strings.Contains(got, "## Next") {
			t.Errorf("whichever branch ran, Next is in the product:\n%s", got)
		}
	}

	// Placed in one branch by hand, a section is missing when the other runs, and that is an error.
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n")
	mustWrite(t, filepath.Join(dir, "up", "a.md"), "# T\n\n## Intro\n\ni\n")
	mustWrite(t, filepath.Join(dir, "me", "a.md"), "## Ours\n\no\n\n## Next\n\nn\n")
	mustWrite(t, filepath.Join(dir, "me", "a.md.lm"), "if base.has.Overview {\n    base.Overview.after(self.Ours, \"Next\")\n} else {\n    base.start(self.Ours)\n}\n")
	cfg, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := buildTree(t, cfg, filepath.Join(dir, "out")); err == nil || !strings.Contains(err.Error(), `our section "Next" (in our file) is not in the product`) {
		t.Errorf("a section the branch that ran does not place is an error: %v", err)
	}

	// A Self: section nothing names is ours as much as a section of our file is.
	weave := func(tpl string) error {
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n")
		mustWrite(t, filepath.Join(dir, "up", "a.md"), "# T\n\n## Overview\n\no\n")
		mustWrite(t, filepath.Join(dir, "me", "a.md.lm"), tpl)
		cfg, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
		if err != nil {
			t.Fatal(err)
		}
		_, err = buildTree(t, cfg, filepath.Join(dir, "out"))
		return err
	}
	err = weave("base.Overview.after(self.job)\n---\nSelf:\n    ```markdown\n    ## job\n\n    j\n\n    ## forgotten\n\n    f\n    ```\n")
	if err == nil || !strings.Contains(err.Error(), `our section "forgotten" (in Self:) is not in the product`) {
		t.Errorf("a Self: section nothing places is an error: %v", err)
	}
	if err := weave("base.Overview.after(self.job, self.forgotten)\n---\nSelf:\n    ```markdown\n    ## job\n\n    j\n\n    ## forgotten\n\n    f\n    ```\n"); err != nil {
		t.Errorf("placed, it builds: %v", err)
	}
}

// A Self: section is our content written in the template, and our content is expanded wherever
// it is written. It was not: {{@url}} reached the product as written, and lm.e warned that url was
// never used.
func TestVariablesInSelfSections(t *testing.T) {
	build := func(vars string) (string, error) {
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n")
		mustWrite(t, filepath.Join(dir, "lm.e"), vars)
		mustWrite(t, filepath.Join(dir, "up", "a.md"), "# T\n\n## Overview\n\no\n")
		mustWrite(t, filepath.Join(dir, "me", "a.md.lm"), "base.Overview.after(self.job)\n---\nSelf:\n    ```markdown\n    ## job\n\n    see {{@url}}\n    ```\n")
		cfg, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Vars, err = lang.LoadVars(filepath.Join(dir, "lm.e")); err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(dir, "out")
		if _, err := buildTree(t, cfg, out); err != nil {
			return "", err
		}
		return readFile(t, filepath.Join(out, "a.md")), nil
	}
	got, err := build("url = https://x.example\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "see https://x.example") {
		t.Errorf("the variable is expanded in the product:\n%s", got)
	}
	if _, err := build("other = 1\n"); err == nil || !strings.Contains(err.Error(), "a.md.lm:7") || !strings.Contains(err.Error(), "url") {
		t.Errorf("an undefined variable is named where it is written, in the template: %v", err)
	}
}

// The report says which way every question went. It said so only when the branch taken was
// empty, so a template whose else ran — the then-branch's statements not written — reported
// nothing skipped at all.
func TestTheReportSaysWhichWayEachQuestionWent(t *testing.T) {
	const tpl = "if base.has.Overview {\n    base.Overview.after(self.Ours)\n} else {\n    base.start(self.Ours)\n}\n"
	for _, c := range []struct{ up, want string }{
		{"# T\n\n## Overview\n\no\n", "a.md: else skipped (base.has.Overview held)"},
		{"# T\n\n## Intro\n\ni\n", "a.md skipped (base.has.Overview did not hold)"},
	} {
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, "loom.om"), "base \"up\"\nself \"me\"\nmark markdown \"<!-- B -->\" \"<!-- E -->\"\n")
		mustWrite(t, filepath.Join(dir, "up", "a.md"), c.up)
		mustWrite(t, filepath.Join(dir, "me", "a.md"), "## Ours\n\no\n")
		mustWrite(t, filepath.Join(dir, "me", "a.md.lm"), tpl)
		cfg, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
		if err != nil {
			t.Fatal(err)
		}
		p, err := buildTree(t, cfg, filepath.Join(dir, "out"))
		if err != nil {
			t.Fatal(err)
		}
		var lines []string
		for _, l := range p.Report.Skipped {
			lines = append(lines, l.Detail)
		}
		if !strings.Contains(strings.Join(lines, "\n"), c.want) {
			t.Errorf("want %q in skipped, got %q", c.want, lines)
		}
	}
}
