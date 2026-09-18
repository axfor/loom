package loom_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axfor/loom"
)

const objConfig = `
base      "upstream"
self      "mine"
templates "t"
mark      markdown "<!-- MINE:BEGIN -->" "<!-- MINE:END -->"
`

// objRepo creates a repository with new-style settings: upstream and our doc.md / run.sh come from
// the golden repository, and files are written in by relative path.
func objRepo(t *testing.T, files map[string]string) (*loom.Config, string) {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "loom.lm"), objConfig)
	for _, layer := range []string{"upstream", "mine"} {
		for _, name := range []string{"doc.md", "run.sh"} {
			b, err := os.ReadFile(filepath.Join(fixture, layer, name))
			if err != nil {
				t.Fatal(err)
			}
			mustWrite(t, filepath.Join(dir, layer, name), string(b))
		}
	}
	for p, s := range files {
		mustWrite(t, filepath.Join(dir, filepath.FromSlash(p)), s)
	}
	c, err := loom.LoadConfig(filepath.Join(dir, "loom.lm"))
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	return c, dir
}

// weaveObj writes src as the template of product target, loads it and weaves it.
func weaveObj(t *testing.T, c *loom.Config, dir, target, src string) (string, error) {
	t.Helper()
	p := filepath.Join(dir, "t", filepath.FromSlash(target)+loom.Ext)
	mustWrite(t, p, src)
	tm, err := loom.LoadTemplate(c, p)
	if err != nil {
		return "", err
	}
	return loom.Weave(c, tm)
}

// (...) on one line and { ... } over several lines are two layouts of the same statement.
func TestParenAndBlockAreEquivalent(t *testing.T) {
	c, dir := objRepo(t, nil)
	paren, err := weaveObj(t, c, dir, "doc.md", `base.Overview.after("Where this fits", "Appendix")`)
	if err != nil {
		t.Fatal(err)
	}
	block, err := weaveObj(t, c, dir, "doc.md", `
base.Overview.after{
    "Where this fits"
    "Appendix"
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if paren != block {
		t.Errorf("the two layouts weave differently\n--- (...) ---\n%s\n--- { ... } ---\n%s", paren, block)
	}
}

// _ in an identifier matches a space or an underscore; when both exist, no guessing: the string form is required.
func TestIdentifierNames(t *testing.T) {
	c, dir := objRepo(t, map[string]string{
		"upstream/a.md": "## Usage Tips\n\nx\n\n## Setup\n\ny\n",
		"upstream/b.md": "## A B\n\nx\n\n## A_B\n\ny\n",
		"mine/a.md":     "## Usage notes\n\nz\n",
		"mine/b.md":     "## Usage notes\n\nz\n",
	})
	out, err := weaveObj(t, c, dir, "a.md", `base.Usage_Tips.after("Usage notes")`)
	if err != nil {
		t.Fatal(err)
	}
	if i, j := strings.Index(out, "## Usage notes"), strings.Index(out, "## Setup"); i < 0 || i > j {
		t.Errorf("not inserted after Usage Tips:\n%s", out)
	}
	if _, err := weaveObj(t, c, dir, "b.md", `base.A_B.after("Usage notes")`); err == nil || !strings.Contains(err.Error(), "use the string form") {
		t.Errorf("A_B matches two headings and must be refused, got: %v", err)
	}
	if _, err := weaveObj(t, c, dir, "b.md", `base."A B".after("Usage notes")`); err != nil {
		t.Errorf("the string form names one heading exactly and must not be ambiguous: %v", err)
	}
	if _, err := weaveObj(t, c, dir, "a.md", `base.Usage_Tip.after("Usage notes")`); err == nil || !strings.Contains(err.Error(), `closest is "Usage Tips"`) {
		t.Errorf("a misspelled name must suggest the closest one, got: %v", err)
	}
}

// A backtick literal is our content: it is wrapped in marks, and stripping them leaves upstream intact.
func TestLiteralIsMarkedAsOurs(t *testing.T) {
	c, dir := objRepo(t, nil)
	out, err := weaveObj(t, c, dir, "doc.md", "base.append(`\n    ## Written in place\n\n    A paragraph.\n    `)\n")
	if err != nil {
		t.Fatal(err)
	}
	want := "<!-- MINE:BEGIN -->\n## Written in place\n\nA paragraph.\n<!-- MINE:END -->"
	if !strings.Contains(out, want) {
		t.Errorf("the literal was not dedented, or not wrapped in marks:\n%s", out)
	}
	up, _ := os.ReadFile(filepath.Join(dir, "upstream", "doc.md"))
	if strings.TrimRight(stripMarks(out, "<!-- MINE:BEGIN -->", "<!-- MINE:END -->"), "\n") != strings.TrimRight(string(up), "\n") {
		t.Errorf("with marks stripped, the product differs from upstream:\n%s", out)
	}
}

// start inserts at the start of the body: in a file with frontmatter that is after the frontmatter,
// which only counts on the first line. The frontmatter must still work afterwards (a key value reads it).
func TestStartGoesAfterFrontmatter(t *testing.T) {
	c, dir := objRepo(t, nil)
	out, err := weaveObj(t, c, dir, "doc.md", "base.start(\"Appendix\", `> literal`)\nbase.frontmatter.description.start(self.frontmatter.description)\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "---\n") {
		t.Fatalf("the frontmatter is no longer on the first line:\n%s", out)
	}
	fmEnd := strings.Index(out[4:], "---\n") + 4
	if i, j := strings.Index(out, "## Appendix"), strings.Index(out, "> literal"); i < fmEnd || j < i || j > strings.Index(out, "## Overview") {
		t.Errorf("start content must follow the frontmatter, in order, before the first section:\n%s", out)
	}
	up, _ := os.ReadFile(filepath.Join(dir, "upstream", "doc.md"))
	if body(stripMarks(out, "<!-- MINE:BEGIN -->", "<!-- MINE:END -->")) != body(string(up)) {
		t.Errorf("with marks stripped, the body differs from upstream:\n%s", out)
	}
}

// Removing upstream content needs a reason; only then is it removed.
func TestDropNeedsReason(t *testing.T) {
	c, dir := objRepo(t, nil)
	if _, err := weaveObj(t, c, dir, "doc.md", `base.Process.drop()`); err == nil || !strings.Contains(err.Error(), "needs a reason") {
		t.Errorf("a drop without a reason must be refused, got: %v", err)
	}
	out, err := weaveObj(t, c, dir, "doc.md", `base.Process.drop(reason: "we describe our own process")`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "## Process") || !strings.Contains(out, "## Overview") {
		t.Errorf("only the Process section should be removed:\n%s", out)
	}
}

// Replace the whole file with ours: base.replace(self, reason: ...).
func TestWholeFileReplace(t *testing.T) {
	c, dir := objRepo(t, nil)
	out, err := weaveObj(t, c, dir, "doc.md", `base.replace(self, reason: "rewritten whole")`)
	if err != nil {
		t.Fatal(err)
	}
	mine, _ := os.ReadFile(filepath.Join(dir, "mine", "doc.md"))
	if out != string(mine) {
		t.Errorf("a whole-file replace must equal our file:\n%s", out)
	}
}

// import: the extension may be left out, but several matching files is an error; base can point at another upstream file.
func TestImports(t *testing.T) {
	c, dir := objRepo(t, map[string]string{
		"mine/shared/notes.md":   "## Überblick\n\nShared content.\n",
		"mine/shared/twice.md":   "## x\n",
		"mine/shared/twice.txt":  "x\n",
		"upstream/old/guide.md":  "## Old Overview\n\nUpstream under its old name.\n",
		"mine/cmd.md":            "---\ndescription: ours\n---\n\n## Our section\n\nContent.\n",
		"mine/cmd.toml":          "description = \"our description\"\n",
		"upstream/cmd.toml":      "description = \"Upstream\"\nprompt = \"\"\"\n## Steps\n\ndo it\n\"\"\"\n",
		"upstream/sub/page.md":   "## Page\n\np\n",
		"mine/sub/page-extra.md": "## Extra\n\nq\n",
	})
	out, err := weaveObj(t, c, dir, "doc.md", `
import notes "/shared/notes"
base.append(notes.Überblick)
`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Shared content.") {
		t.Errorf("imported content is missing:\n%s", out)
	}

	if _, err := weaveObj(t, c, dir, "doc.md", `import twice "/shared/twice"`+"\nbase.append(twice.body)\n"); err == nil || !strings.Contains(err.Error(), "add the extension") {
		t.Errorf("without an extension two files match; must be refused, got: %v", err)
	}
	if _, err := weaveObj(t, c, dir, "doc.md", `import x "shared/notes"`+"\nbase.append(x.body)\n"); err == nil || !strings.Contains(err.Error(), "must start with ./, ../ or /") {
		t.Errorf("a path not starting with ./ ../ / must be refused, got: %v", err)
	}
	if _, err := weaveObj(t, c, dir, "doc.md", `import x "../../etc/passwd"`+"\nbase.append(x.body)\n"); err == nil || !strings.Contains(err.Error(), "escapes the") {
		t.Errorf("a path escaping the layer must be refused, got: %v", err)
	}

	// a relative path starts at the product's directory
	out, err = weaveObj(t, c, dir, "sub/page.md", `
import extra "./page-extra"
base.Page.after(extra.Extra)
`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "## Extra") {
		t.Errorf("relative path not resolved:\n%s", out)
	}

	// base points at another upstream file (a file renamed upstream)
	out, err = weaveObj(t, c, dir, "doc.md", `
import base "/old/guide"
base.Old_Overview.after("Appendix")
`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Upstream under its old name.") || !strings.Contains(out, "## Appendix") {
		t.Errorf("base does not point at old/guide.md:\n%s", out)
	}

	// a toml key viewed as markdown, with content from another file
	out, err = weaveObj(t, c, dir, "cmd.toml", `
import cmdmd "/cmd.md"
base.description.set(self.description)
base.prompt.as(markdown).append(cmdmd.body)
`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"our description", "## Steps", "## Our section"} {
		if !strings.Contains(out, want) {
			t.Errorf("toml is missing %q:\n%s", want, out)
		}
	}
}

// An error says where, what is wrong and how to write it, with file:line:col.
func TestObjectSyntaxErrors(t *testing.T) {
	c, dir := objRepo(t, nil)
	for _, cse := range []struct{ name, src, want string }{
		{"misspelled object", "bsae.Overview.after(\"Appendix\")", "did you mean base"},
		{"misspelled method", "base.Overview.aftr(\"Appendix\")", "did you mean after"},
		{"changing something other than base", "self.Overview.after(\"Appendix\")", "is only a content source"},
		{"no method call", "base.Overview", "does nothing"},
		{"two statements on one line", "base.Overview.after(\"Appendix\") base.append(\"Appendix\")", "one statement per line"},
		{"reason where none is allowed", "base.Overview.after(\"Appendix\", reason: \"x\")", "reason: is only for replace / drop"},
		{"unterminated string", "base.Overview.after(\"Appendix)", "unterminated string"},
		{"block without newline", "base.Overview.after{ \"Appendix\" }", "expected a newline after `{`"},
		{"commas in a block", "base.Overview.after{\n  \"Appendix\",\n  \"Where this fits\"\n}", "without commas"},
		{"append on a node", "base.Overview.append(\"Appendix\")", "applies to the whole file"},
		{"content from upstream", "base.Overview.after(base.Process)", "must come from our layer"},
		{"duplicate import", "import a \"/doc.md\"\nimport a \"/doc.md\"", "imported twice"},
		{"anchor not found", "base.\"No Such\".after(\"Appendix\")", "anchor not found"},
	} {
		t.Run(cse.name, func(t *testing.T) {
			_, err := weaveObj(t, c, dir, "doc.md", cse.src)
			if err == nil {
				t.Fatal("expected an error, but weaving succeeded")
			}
			if !strings.Contains(err.Error(), cse.want) {
				t.Errorf("wrong error\ngot: %v\nwant it to contain: %s", err, cse.want)
			}
		})
	}
	_, err := weaveObj(t, c, dir, "doc.md", "// the first line is a comment\nbase.Overview.aftr(\"Appendix\")")
	if err == nil || !strings.Contains(err.Error(), "doc.md.lm:2:15") {
		t.Errorf("the error must point at file:line:col (aftr at line 2, column 15), got: %v", err)
	}
}

// list reports real headings, not the identifier form: tools reading list compare them with upstream headings.
func TestDescribeResolvesIdentifiers(t *testing.T) {
	c, dir := objRepo(t, map[string]string{
		"upstream/a.md": "## Usage Tips\n\nx\n",
		"mine/a.md":     "## Café Notes\n\nz\n",
	})
	p := filepath.Join(dir, "t", "a.md"+loom.Ext)
	mustWrite(t, p, "base.Usage_Tips.after(self.Café_Notes)\n")
	i, err := loom.Describe(c, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(i.Anchors) != 1 || i.Anchors[0].Anchor != "Usage Tips" {
		t.Errorf("the anchor must be reported as the real heading Usage Tips: %+v", i.Anchors)
	}
	if len(i.Inserts) != 1 || i.Inserts[0].Anchor != "Café Notes" {
		t.Errorf("the content source must be reported as the real heading Café Notes: %+v", i.Inserts)
	}
}

// list carries the reasons written in the template, so tools never parse templates themselves.
func TestDescribeReportsReasons(t *testing.T) {
	c, dir := objRepo(t, nil)
	whole := filepath.Join(dir, "t", "doc.md"+loom.Ext)
	mustWrite(t, whole, "base.replace(self, reason: \"ours is a rewrite\")\nbase.Process.drop(reason: \"described in ours\")\n")
	i, err := loom.Describe(c, whole)
	if err != nil {
		t.Fatal(err)
	}
	if i.Reason != "ours is a rewrite" {
		t.Errorf("whole-file reason: %q", i.Reason)
	}
	if len(i.Drops) != 1 || i.Drops[0].Anchor != "Process" || i.Drops[0].Reason != "described in ours" {
		t.Errorf("drops: %+v", i.Drops)
	}
	mustWrite(t, whole, "base.Overview.replace(\"Where this fits\", reason: \"ours says it better\")\n")
	if i, err = loom.Describe(c, whole); err != nil {
		t.Fatal(err)
	}
	if len(i.Replaces) != 1 || i.Replaces[0].Anchor != "Overview" || i.Replaces[0].Reason != "ours says it better" || i.Reason != "" {
		t.Errorf("replaces: %+v, reason %q", i.Replaces, i.Reason)
	}
}

// A frontmatter key is a node: base.frontmatter.description. Its value takes ours with set, or ours
// before (start) or after (append) upstream's.
func TestFrontmatterKeyValues(t *testing.T) {
	c, dir := objRepo(t, map[string]string{
		"upstream/k.md": "---\nname: k\ndescription: Up.\n---\n\n## A\n\nup\n",
		"mine/k.md":     "---\nname: k\ndescription: Ours.\nargument-hint: <file>\n---\n\n## B\n\nme\n",
	})
	fm := func(src string) (string, error) {
		out, err := weaveObj(t, c, dir, "k.md", src)
		if err != nil || len(out) < 3 {
			return out, err
		}
		if i := strings.Index(out[3:], "---"); i > 0 {
			return out[:i+3], nil
		}
		return out, nil
	}
	for src, want := range map[string]string{
		"base.frontmatter.description.start(self.frontmatter.description)":           "description: Ours. Up.",
		"base.frontmatter.description.append(self.frontmatter.description)":          "description: Up. Ours.",
		"base.frontmatter.description.set(self.frontmatter.description)":             "description: Ours.\n",
		"base.frontmatter.description.start(`Draft:`)":                               "description: Draft: Up.",
		"base.frontmatter.\"argument-hint\".set(self.frontmatter.\"argument-hint\")": "argument-hint: <file>",
		// set on the whole frontmatter first: upstream's half still comes from upstream's file
		"base.frontmatter.set(self.frontmatter)\nbase.frontmatter.description.start(self.frontmatter.description)": "description: Ours. Up.",
	} {
		got, err := fm(src)
		if err != nil || !strings.Contains(got, want) {
			t.Errorf("%s\nwant %q in:\n%s (%v)", src, want, got, err)
		}
	}
	for src, want := range map[string]string{
		"base.frontmatter.nope.start(self.frontmatter.description)": `upstream has no key "nope"`,
		"base.frontmatter.description.start(self.frontmatter.nope)": `no key "nope"`,
		"base.frontmatter.description.start(self.description)":      "self.frontmatter.description",
		"base.frontmatter.join(\"description\")":                    "base.frontmatter.description.start(self.frontmatter.description)",
		"base.A.start(self.frontmatter.description)":                "whole file or to a key",
	} {
		if _, err := fm(src); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want an error mentioning %q, got: %v", src, want, err)
		}
	}
}

// A quoted frontmatter value stays quoted: unquoting it would change what the file says, and a value
// with a colon or a leading * is not a plain scalar any more.
func TestQuotedFrontmatterValueStaysQuoted(t *testing.T) {
	c, dir := objRepo(t, map[string]string{
		"upstream/q.md": "---\nname: q\ndescription: \"Up: the upstream half\"\n---\n\n## A\n\nup\n",
		"mine/q.md":     "---\nname: q\ndescription: \"Ours: our half\"\n---\n\n## B\n\nme\n",
	})
	out, err := weaveObj(t, c, dir, "q.md", "base.frontmatter.description.start(self.frontmatter.description)\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `description: "Ours: our half Up: the upstream half"`) {
		t.Errorf("the joined value must stay quoted:\n%s", out[:120])
	}
	out, err = weaveObj(t, c, dir, "q.md", "base.frontmatter.description.set(`Ours \"quoted\" half`)\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `description: Ours "quoted" half`) {
		t.Errorf("a plain literal stays plain:\n%s", out[:120])
	}
	_ = dir
}

// Quotes inside a value are content: only the quotes that wrap a whole value are its quotes, and
// the \" inside a quoted one belongs to the file's syntax, not to the text.
func TestQuotesInsideAValueAreContent(t *testing.T) {
	c, dir := objRepo(t, map[string]string{
		"upstream/q.md": "---\nname: q\ndescription: Use it when they say \"go\"\n---\n\n## A\n\nup\n",
		"mine/q.md":     "---\nname: q\ndescription: Ours, they say \"now\"\n---\n\n## B\n\nme\n",
	})
	out, err := weaveObj(t, c, dir, "q.md", "base.frontmatter.description.start(self.frontmatter.description)\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `description: Ours, they say "now" Use it when they say "go"`) {
		t.Errorf("an unquoted value keeps the quotes it ends with:\n%s", out)
	}

	c, dir = objRepo(t, map[string]string{
		"upstream/c.toml": "description = \"Up says \\\"go\\\"\"\n",
		"mine/c.toml":     "description = \"Ours says \\\"now\\\"\"\n",
	})
	out, err = weaveObj(t, c, dir, "c.toml", "base.description.start(self.description)\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `description = "Ours says \"now\" Up says \"go\""`) {
		t.Errorf("an escaped quote survives the round trip:\n%s", out)
	}
	_ = dir
}

// A multi-line value (a toml """ block holding markdown) is joined by a blank line, not a space:
// one line would run two documents together.
func TestValueJoinSeparator(t *testing.T) {
	c, dir := objRepo(t, map[string]string{
		"upstream/p.toml": "prompt = \"\"\"\n## Up\n\nup\n\"\"\"\n",
		"mine/p.toml":     "prompt = \"\"\"\n## Ours\n\nme\n\"\"\"\n",
	})
	out, err := weaveObj(t, c, dir, "p.toml", "base.prompt.start(self.prompt)\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "## Ours\n\nme\n\n## Up") {
		t.Errorf("a multi-line value must be joined by a blank line:\n%s", out)
	}
	_ = dir
}

// A toml key takes the same methods: base.description.start(self.description).
func TestTomlKeyValues(t *testing.T) {
	c, dir := objRepo(t, map[string]string{
		"upstream/c.toml": "description = \"Up\"\nprompt = \"x\"\n",
		"mine/c.toml":     "description = \"Ours\"\nprompt = \"y\"\n",
	})
	out, err := weaveObj(t, c, dir, "c.toml", "base.description.start(self.description)\nbase.prompt.set(self.prompt)\n")
	if err != nil || !strings.Contains(out, `description = "Ours Up"`) || !strings.Contains(out, `prompt = "y"`) {
		t.Errorf("got:\n%s (%v)", out, err)
	}
}

// The completeness check must read a value the same way weaving writes it: our key is in the product
// even when quotes inside it were escaped on the way in.
func TestCompletenessReadsEscapedValues(t *testing.T) {
	c, dir := objRepo(t, map[string]string{
		"upstream/e.md": "---\nname: e\ndescription: \"Up: the upstream half\"\n---\n\n## A\n\nup\n",
		"mine/e.md":     "---\nname: e\ndescription: Ours, they say \"now\"\n---\n\n## B\n\nme\n",
	})
	out, err := weaveObj(t, c, dir, "e.md", "base.frontmatter.description.start(self.frontmatter.description)\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `description: "Ours, they say \"now\" Up: the upstream half"`) {
		t.Errorf("the value quotes ours and escapes the quotes inside it:\n%s", out)
	}
	// The fixture files of objRepo have complaints of their own; this is about the key.
	if _, err := loom.PlanBuild(c, false); err != nil && strings.Contains(err.Error(), "frontmatter key") {
		t.Errorf("our key is in the product, so the build must not refuse it: %v", err)
	}
}
