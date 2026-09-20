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
