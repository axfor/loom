package lang

// The editor carries a second implementation of some of this package's rules, in JavaScript, so
// that completion and highlighting work without running the compiler. Two of its tables had
// silently fallen behind: the grammar was marking match:, level:, after: and before: as typos,
// match: since the day predicates landed. Nothing was watching, because a mirror that drifts
// produces no error — it produces an editor that quietly lies about a correct template.
//
// This test watches. It asks the real mirror for its tables (node, not a regular expression over
// the source) and the real grammar for its argument list, and holds both against the Go tables
// they claim to copy.

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const ext = "../editors/vscode"

// node finds the interpreter, and registers the mirror's own sources as inputs of this test.
// Without that last part the guard is worth little: go test caches a result against the files the
// test process opened, and the JavaScript is read by a child process, which the cache cannot see —
// so editing exactly the file being watched would replay a stale pass.
func node(t *testing.T) string {
	t.Helper()
	exe, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed; the editor mirror cannot be checked here")
	}
	for _, f := range []string{"lib/loom.js", "lib/completion.js"} {
		if _, err := os.ReadFile(filepath.Join(ext, f)); err != nil {
			t.Fatal(err)
		}
	}
	return exe
}

// mirror runs the extension's own module and returns the tables it exports.
func mirror(t *testing.T) map[string]any {
	t.Helper()
	node := node(t)
	const dump = `const m = require("./lib/loom.js");
		const set = (s) => [...s].sort();
		console.log(JSON.stringify({
			CLASS_CALLS: m.CLASS_CALLS,
			KIND_CALLS: m.KIND_CALLS,
			DEFAULT_KIND: m.DEFAULT_KIND,
			METHODS: set(m.METHODS),
			TYPES: set(m.TYPES),
		}));`
	cmd := exec.Command(node, "-e", dump)
	cmd.Dir = ext
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("reading the editor mirror: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the mirror's tables are not JSON: %v", err)
	}
	return got
}

func TestEditorMirrorsTheTables(t *testing.T) {
	js := mirror(t)

	// A table of tables: markdown → sections → heading.
	nested := func(name string, want map[string]map[string]string) {
		t.Helper()
		raw, err := json.Marshal(js[name])
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]map[string]string
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for typ, calls := range want {
			for call, kind := range calls {
				if got[typ][call] != kind {
					t.Errorf("%s: the editor has %s.%s = %q, the compiler has %q", name, typ, call, got[typ][call], kind)
				}
			}
			for call := range got[typ] {
				if _, ok := calls[call]; !ok {
					t.Errorf("%s: the editor offers %s.%s, which the compiler does not have", name, typ, call)
				}
			}
		}
		for typ := range got {
			if _, ok := want[typ]; !ok {
				t.Errorf("%s: the editor has a type %q the compiler does not", name, typ)
			}
		}
	}
	nested("CLASS_CALLS", classCalls)
	nested("KIND_CALLS", kindCalls)

	raw, _ := json.Marshal(js["DEFAULT_KIND"])
	var kinds map[string]string
	if err := json.Unmarshal(raw, &kinds); err != nil {
		t.Fatal(err)
	}
	for typ, kind := range defaultKind {
		if kinds[typ] != kind {
			t.Errorf("DEFAULT_KIND: the editor has %s = %q, the compiler has %q", typ, kinds[typ], kind)
		}
	}
	if len(kinds) != len(defaultKind) {
		t.Errorf("DEFAULT_KIND: the editor has %d types, the compiler has %d", len(kinds), len(defaultKind))
	}

	list := func(name string, want []string) {
		t.Helper()
		var got []string
		raw, _ := json.Marshal(js[name])
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		w := append([]string(nil), want...)
		sort.Strings(w)
		if strings.Join(got, " ") != strings.Join(w, " ") {
			t.Errorf("%s:\n  editor:   %s\n  compiler: %s", name, strings.Join(got, " "), strings.Join(w, " "))
		}
	}
	list("METHODS", editMethods)
	list("TYPES", typeWords())
}

// The grammars are the third copy, and the one that had been wrong in four places: a word the
// grammar does not list is painted as a typo, so a correct file looks broken.
func TestGrammarsKnowEveryWord(t *testing.T) {
	alt(t, "loom.tmLanguage.json", "named-argument", namedArgs)
	alt(t, "loom.tmLanguage.json", "format", typeWords())
	alt(t, "loom-om.tmLanguage.json", "format", typeWords())
	// Two rules list the settings: the one that highlights them, and the one that paints
	// everything else illegal. Both live under their own key and both are checked.
	alt(t, "loom-om.tmLanguage.json", "setting", settingKeywords)
	alt(t, "loom-om.tmLanguage.json", "unknown-setting", settingKeywords)
}

// alt holds one rule of a grammar against the list of words the language really has. The rule is
// found by its name in the grammar's repository rather than by pattern, so a rule that goes wrong
// is still the rule being checked — which is the whole point.
func alt(t *testing.T, file, key string, want []string) {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(ext, "syntaxes", file))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Repository map[string]json.RawMessage `json:"repository"`
	}
	if err := json.Unmarshal(src, &doc); err != nil {
		t.Fatal(err)
	}
	rule, ok := doc.Repository[key]
	if !ok {
		t.Fatalf("%s: no rule named %q; if it was renamed, this test must follow", file, key)
	}
	w := append([]string(nil), want...)
	sort.Strings(w)
	// An alternation of lowercase words, with or without the (?: of a lookahead group.
	found := regexp.MustCompile(`\(\??:?([a-z]+(?:\|[a-z]+)+)\)`).FindAllStringSubmatch(string(rule), -1)
	if len(found) == 0 {
		t.Fatalf("%s: rule %q lists no words; if it was rewritten, this test must follow", file, key)
	}
	for _, m := range found {
		got := strings.Split(m[1], "|")
		sort.Strings(got)
		if strings.Join(got, " ") != strings.Join(w, " ") {
			t.Errorf("%s: rule %q lists different words than the language has;\nanything missing is painted as a typo in a correct file\n  grammar:  %s\n  language: %s",
				file, key, strings.Join(got, " "), strings.Join(w, " "))
		}
	}
}

// The editor also offers settings in completion, from a list of its own.
func TestEditorOffersEverySetting(t *testing.T) {
	cmd := exec.Command(node(t), "-e", `const s = require("fs").readFileSync("lib/completion.js", "utf8");
		const m = s.match(/const SETTINGS = \[([\s\S]*?)\n\];/);
		console.log(JSON.stringify([...m[1].matchAll(/\['([a-z]+)'/g)].map((x) => x[1]).sort()));`)
	cmd.Dir = ext
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("reading the editor's settings list: %v", err)
	}
	var got []string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	want := append([]string(nil), settingKeywords...)
	sort.Strings(want)
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the editor offers a different set of settings than the compiler accepts;\nanything missing here is a setting nobody is told about\n  editor:   %s\n  compiler: %s",
			strings.Join(got, " "), strings.Join(want, " "))
	}
}

// The tables were only half of it. The editor lexes Loom itself, in JavaScript, and that half had
// drifted too: it read a fence by counting to the next single backtick, so one ` in a line of
// prose put everything after it out of step and the rest of the file went dead in the editor
// while the compiler built it happily. The number token added for level: was dropped outright.
//
// Neither is a table, so nothing above would have caught either. This feeds both lexers the same
// templates and compares what comes back.
func TestEditorLexesTheSameWay(t *testing.T) {
	exe := node(t)
	cases := map[string]string{
		"plain":        "base.Intro.after(self.Notes)\n",
		"string":       "base.\"How it compares\".drop(reason: \"upstream only\")\n",
		"escape":       "base.X.after(\"a \\\" b \\\\ c\")\n",
		"comment":      "// a note\nbase.X.drop(reason: \"r\") // trailing\n",
		"number":       "base.sections(level: 3).demote()\n",
		"short":        "base.X.after(`one line`)\n",
		"fence":        "base.X.after(```markdown\n## H\n\ntext\n```)\n",
		"fence odd":    "base.X.after(```markdown\nUse the ` character.\n```)\nbase.Y.drop(reason: \"r\")\n",
		"fence nested": "base.X.after(````markdown\nA ` then:\n```sh\necho hi\n```\n````)\nbase.Y.drop(reason: \"r\")\n",
		"fence tagged": "base.X.after(```sh\necho `date`\n```)\n",
		"unterminated": "base.X.after(```markdown\nstill typing\n",
		"empty":        "",
	}
	kinds := map[Kind]string{
		KIdent: "id", KString: "str", KRaw: "raw", KNumber: "num", KDot: ".", KComma: ",",
		KColon: ":", KLParen: "(", KRParen: ")", KLBrace: "{", KRBrace: "}", KNewline: "nl", KEOF: "eof",
	}

	var names []string
	for name := range cases {
		names = append(names, name)
	}
	sort.Strings(names)

	in, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	// The editor's token has a kind and, for names and strings, a value. Newlines it keeps; the
	// synthetic one the compiler appends before EOF it does not, so both sides drop that.
	cmd := exec.Command(exe, "-e", `const m = require("./lib/loom.js");
		let src = ""; process.stdin.on("data", (d) => (src += d)).on("end", () => {
			const cases = JSON.parse(src), out = {};
			for (const [k, text] of Object.entries(cases)) {
				out[k] = m.lex(text).map((t) => t.t + (t.v !== undefined && t.t !== "raw" ? ":" + t.v : ""));
			}
			console.log(JSON.stringify(out));
		});`)
	cmd.Dir = ext
	cmd.Stdin = strings.NewReader(string(in))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("lexing with the editor: %v\n%s", err, stderr(err))
	}
	var js map[string][]string
	if err := json.Unmarshal(out, &js); err != nil {
		t.Fatal(err)
	}

	for _, name := range names {
		src := cases[name]
		toks, err := Lex(name, []byte(src))
		if err != nil {
			// The editor is deliberately tolerant of a half-written template, so where the
			// compiler refuses there is nothing to compare.
			continue
		}
		var want []string
		for k, tk := range toks {
			// The compiler always appends a newline before EOF, whether the file ended with one
			// or not, because a newline is what ends a statement. The editor's parser is tolerant
			// and needs no such thing, so that one token is dropped before comparing.
			if k == len(toks)-2 {
				continue
			}
			s := kinds[tk.Kind]
			if tk.Kind == KIdent || tk.Kind == KString || tk.Kind == KNumber {
				s += ":" + tk.Text
			}
			want = append(want, s)
		}
		if got := js[name]; strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("%s: the editor reads this template differently than the compiler does\n  source:   %q\n  editor:   %s\n  compiler: %s",
				name, src, strings.Join(got, " "), strings.Join(want, " "))
		}
	}
}

// The editor also finds the nodes of a file itself — the headings of a markdown document, the
// keys of a yaml one — because completion cannot run the compiler on every keystroke. That is a
// second implementation of ast/*.go, and it was wrong for yaml in the plainest way available: the
// kind is called "key" for both toml and yaml, as it is in the compiler, so the editor handed
// yaml to its toml parser and found nothing at all. A whole document type, invisible.
//
// json is deliberately absent. Its tree is parsed values, not lines, so the compiler can answer
// whether a path exists but not where it sits, and listing nodes with invented line numbers would
// be a lie. The editor scans the text and can say roughly where — a weaker answer, but the right
// one for completion. The two differ on purpose, so comparing them would only encode the lie.
//
// ast lives in another package, so this test asks the compiler through a tiny program rather than
// calling in: the point is to compare the two answers, not to reach past either.
func TestEditorFindsTheSameNodes(t *testing.T) {
	exe := node(t)
	files := map[string]struct {
		Typ  string `json:"typ"`
		Kind string `json:"kind"`
		Text string `json:"text"`
	}{
		"markdown":    {"markdown", "heading", "# Top\n\nt\n\n## A\n\na\n\n### A1\n\nx\n\n## B\n\nb\n"},
		"yaml":        {"yaml", "key", "name: t\non:\n  push:\n    branches: [main]\njobs:\n  build:\n    runs-on: ubuntu\n    steps:\n      - name: one\n        run: |\n          echo hi\n"},
		"yaml list":   {"yaml", "key", "steps:\n  - name: a\n    run: x\n  - name: b\n    run: y\n"},
		"yaml quoted": {"yaml", "key", "\"a b\": 1\n'c d': 2\n# comment: no\n"},
		"toml":        {"toml", "key", "alpha = 1\nbeta = \"two\"\n"},
		"shell":       {"shell", "function", "#!/bin/sh\nmain() {\n  echo\n}\nhelp() {\n  echo\n}\n"},
	}

	var names []string
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	in, err := json.Marshal(files)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-e", `const m = require("./lib/loom.js");
		let src = ""; process.stdin.on("data", (d) => (src += d)).on("end", () => {
			const cases = JSON.parse(src), out = {};
			for (const [k, c] of Object.entries(cases)) out[k] = m.nodesOf(c.text, c.kind, c.typ).map((n) => n.name);
			console.log(JSON.stringify(out));
		});`)
	cmd.Dir = ext
	cmd.Stdin = strings.NewReader(string(in))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("finding nodes with the editor: %v\n%s", err, stderr(err))
	}
	var js map[string][]string
	if err := json.Unmarshal(out, &js); err != nil {
		t.Fatal(err)
	}

	for _, name := range names {
		c := files[name]
		want := compilerNodes(t, c.Typ, c.Kind, c.Text)
		if got := js[name]; strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("%s: the editor finds different nodes than the compiler does\n  editor:   %s\n  compiler: %s",
				name, strings.Join(got, " "), strings.Join(want, " "))
		}
	}
}

// compilerNodes asks ast what a file's addressable nodes are, through a program built for the
// purpose. lang cannot import build and must not import ast's internals to answer this.
func compilerNodes(t *testing.T, typ, kind, text string) []string {
	t.Helper()
	dir := t.TempDir()
	prog := `package main

import (
	"encoding/json"
	"os"

	"github.com/axfor/loom/ast"
)

func main() {
	b, _ := os.ReadFile(os.Args[3])
	var out []string
	for _, n := range ast.Addressable(ast.New(os.Args[1], string(b)), os.Args[2]) {
		out = append(out, n.Name)
	}
	_ = json.NewEncoder(os.Stdout).Encode(out)
}
`
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte(prog), 0o644); err != nil {
		t.Fatal(err)
	}
	in := filepath.Join(dir, "in")
	if err := os.WriteFile(in, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", src, typ, kind, in)
	cmd.Dir = "."
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("asking the compiler for %s nodes: %v", typ, err)
	}
	var got []string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

// stderr is what the child wrote before failing — without it a broken mirror reports only an
// exit status, which says nothing about what went wrong inside it.
func stderr(err error) []byte {
	var e *exec.ExitError
	if errors.As(err, &e) {
		return e.Stderr
	}
	return nil
}

// The last of the mirror is the plain functions over strings: what type a path is, and how a name
// is written back into a template. They look too small to drift and are not — the editor escaped
// every backslash where the compiler escapes only the ones that would be read as escapes, so the
// two wrote the same name differently and a sync turned one into the other.
func TestEditorWritesTheSameText(t *testing.T) {
	exe := node(t)
	names := []string{
		"plain", "Quick Start", `a\.b`, `say "hi"`, `back\\slash`, `tail\`, "已有中文", "A  B", " lead", "x_y", "",
		"drop", "as", "frontmatter", "body", "One Two Three", "section",
	}
	paths := []string{
		"a.md", "a.markdown", "A.MD", "b.toml", "c.yaml", "c.yml", "d.json", "e.sh", "e.bash", "f.txt", "g", "h.tar.gz",
	}
	// The editor quotes a few names the compiler does not: `section`, `key` and the other kind
	// names are never ambiguous to the compiler, since a kind is only ever a call, but quoting
	// them costs nothing and spares the reader the question. Those are checked separately.
	in, err := json.Marshal(map[string][]string{"names": names, "paths": paths})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-e", `const { nodeText } = require("./lib/completion.js");
		const m = require("./lib/loom.js");
		let src = ""; process.stdin.on("data", (d) => (src += d)).on("end", () => {
			const { names, paths } = JSON.parse(src);
			console.log(JSON.stringify({
				quote: names.map(m.quote),
				typeOf: paths.map(m.typeOf),
				nameText: names.map(nodeText),
			}));
		});`)
	cmd.Dir = ext
	cmd.Stdin = strings.NewReader(string(in))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("writing text with the editor: %v\n%s", err, stderr(err))
	}
	var js map[string][]string
	if err := json.Unmarshal(out, &js); err != nil {
		t.Fatal(err)
	}
	for k, n := range names {
		if got, want := js["quote"][k], Quote(n); got != want {
			t.Errorf("quote(%q): editor %s, compiler %s", n, got, want)
		}
	}
	for k, p := range paths {
		if got, want := js["typeOf"][k], TypeOf(p); got != want {
			t.Errorf("typeOf(%q): editor %q, compiler %q", p, got, want)
		}
	}
	kindName := map[string]bool{}
	for _, calls := range kindCalls {
		for call := range calls {
			kindName[call] = true
		}
	}
	for k, n := range names {
		got := js["nameText"][k]
		if kindName[n] {
			// Stricter on purpose: `section` is not ambiguous to the compiler, because a kind is
			// only ever a call, but the editor quotes it anyway. Asserted so it stays a decision.
			if want := Quote(n); got != want {
				t.Errorf("nameText(%q): the editor should quote a kind name; got %s, want %s", n, got, want)
			}
			continue
		}
		if want := NameText(n); got != want {
			t.Errorf("nameText(%q): editor %s, compiler %s", n, got, want)
		}
	}
}

// Import resolution is the last piece of the mirror, and the one with the most branches: from the
// layer root or from the file's own directory, with the extension or without, refusing what is
// ambiguous, missing, or outside the layer. The real tree exercises almost none of it — nine
// imports, eight of them the same shape — so the cases are built here.
func TestEditorResolvesImportsTheSameWay(t *testing.T) {
	exe := node(t)
	dir := t.TempDir()
	write := func(rel, text string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("loom.om", "base \"up\"\nself \"me\"\n")
	for _, f := range []string{
		"up/a/b.md",         // an exact path
		"up/a/only.md",      // one extension: found without it
		"up/a/dup.md",       // two extensions: ambiguous without it
		"up/a/dup.sh",       //
		"up/a/tpl.lm",       // a template is never content
		"up/x/y/sib.md",     // next to the product
		"up/x/over.md",      // one directory up from it
		"up/over.md",        // two up: still inside the layer
		"up/a/thing.md.bak", // a stem a written-out extension must not reach
	} {
		write(f, "x\n")
	}
	// Outside the layer entirely: a path that escapes must not find this, and the only way to
	// prove the guard does anything is to put something there for it to find.
	write("escape.md", "x\n")

	// The product is up/x/y/doc.md, so ./ and ../ are relative to x/y.
	const target = "x/y/doc.md"
	specs := []string{
		"/a/b.md", "/a/only", "/a/dup", "/a/missing", "/a/tpl", "/",
		"./sib.md", "./sib", "./missing.md",
		"../over.md", "../../over.md", "../../../escape.md",
		"a/b.md", "", "/a/../a/b.md", "/a/thing.md",
	}

	c, err := LoadConfig(filepath.Join(dir, "loom.om"))
	if err != nil {
		t.Fatal(err)
	}

	in, err := json.Marshal(map[string]any{"root": dir, "target": target, "specs": specs})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-e", `const m = require("./lib/loom.js"), path = require("path");
		let src = ""; process.stdin.on("data", (d) => (src += d)).on("end", () => {
			const { root, target, specs } = JSON.parse(src);
			const cfg = m.findConfig(path.join(root, "loom.om"));
			console.log(JSON.stringify(specs.map((s) => {
				const abs = m.resolveImport(cfg, "base", s, target);
				return abs == null ? "" : path.relative(path.join(root, "up"), abs).split(path.sep).join("/");
			})));
		});`)
	cmd.Dir = ext
	cmd.Stdin = strings.NewReader(string(in))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("resolving imports with the editor: %v\n%s", err, stderr(err))
	}
	var js []string
	if err := json.Unmarshal(out, &js); err != nil {
		t.Fatal(err)
	}

	for k, spec := range specs {
		want, err := c.resolveImport("base", spec, path.Dir(target))
		if err != nil {
			want = "" // refused; the editor says so by returning nothing
		}
		if js[k] != want {
			t.Errorf("import %q: editor %q, compiler %q", spec, js[k], want)
		}
	}
}

// A template's product path: its path under the templates directory without .lm, with the
// extension filled in from the layers when it was left out. One name is the product; several are
// an error that asks for the full name, never a guess.
func TestEditorFindsTheSameTarget(t *testing.T) {
	exe := node(t)
	dir := t.TempDir()
	write := func(rel string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("loom.om")
	if err := os.WriteFile(filepath.Join(dir, "loom.om"), []byte("base \"up\"\nself \"me\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{
		"up/doc.md",      // the extension can be left out of the template name
		"up/dup.md",      // two files of the same name: ambiguous
		"up/dup.sh",      //
		"up/keep.md",     // written out in full below
		"me/only.toml",   // the other layer counts too
		"me/doc.md",      //
		"me/keep.md",     //
		"me/doc.lm",      // the templates themselves
		"me/keep.md.lm",  //
		"me/dup.lm",      //
		"me/only.lm",     //
		"me/new.lm",      // no layer has a file: the name as written
		"me/deep/a.lm",   // a subdirectory
		"up/deep/a.json", //
		"up/stray.lm",    // a template-shaped file outside the template directory
	} {
		write(f)
	}

	rels := []string{
		"me/doc.lm", "me/keep.md.lm", "me/dup.lm", "me/only.lm", "me/new.lm", "me/deep/a.lm",
		"up/doc.md",   // not a template at all
		"up/stray.lm", // .lm, but not under the template directory
	}
	c, err := LoadConfig(filepath.Join(dir, "loom.om"))
	if err != nil {
		t.Fatal(err)
	}

	in, err := json.Marshal(map[string]any{"root": dir, "rels": rels})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-e", `const m = require("./lib/loom.js"), path = require("path");
		let src = ""; process.stdin.on("data", (d) => (src += d)).on("end", () => {
			const { root, rels } = JSON.parse(src);
			const cfg = m.findConfig(path.join(root, "loom.om"));
			console.log(JSON.stringify(rels.map((r) => m.targetOf(cfg, path.join(root, r)) ?? "")));
		});`)
	cmd.Dir = ext
	cmd.Stdin = strings.NewReader(string(in))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("finding targets with the editor: %v\n%s", err, stderr(err))
	}
	var js []string
	if err := json.Unmarshal(out, &js); err != nil {
		t.Fatal(err)
	}

	for k, rel := range rels {
		want, err := TargetOf(c, filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			want = ""
		}
		if js[k] != want {
			t.Errorf("target of %s: editor %q, compiler %q", rel, js[k], want)
		}
	}
}
