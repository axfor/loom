package build

// The editor resolves anchors too — go-to-definition has to answer "which upstream node is this"
// without running the compiler, so it carries a second implementation of the resolution rules:
// the identifier form where an underscore matches a space, section paths, the node kinds of each
// type. lang/mirror_test.go watches the tables, the lexer and the node finders; this watches the
// answer they are all in aid of.
//
// Checked against the real tree first, where all 171 anchors agree — but every one of those is a
// markdown heading, so the cases below are the ones that tree does not have.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axfor/loom/ast"
	"github.com/axfor/loom/lang"
)

func TestEditorResolvesAnchorsTheSameWay(t *testing.T) {
	exe, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed; the editor mirror cannot be checked here")
	}
	const ext = "../editors/vscode"
	for _, f := range []string{"lib/loom.js", "lib/definition.js"} {
		if _, err := os.ReadFile(filepath.Join(ext, f)); err != nil {
			t.Fatal(err) // registers the mirror as an input, so the test cache tracks it
		}
	}

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
	write("up/doc.md", "# Top\n\nt\n\n## How Skills Work\n\nh\n\n## Example 1\n\n### Phase 1\n\na\n\n## Example 2\n\n### Phase 1\n\nb\n")
	write("me/doc.md", "## Ours\n\no\n")
	write("me/doc.md.lm", strings.Join([]string{
		`base.How_Skills_Work.after(self.Ours)`,
		`base."How Skills Work".after(self.Ours)`,
		`base."Example 2"."Phase 1".after(self.Ours)`,
		`base.section("Example 1").after(self.Ours)`,
	}, "\n")+"\n")
	write("up/r.sh", "#!/bin/sh\n# ── Setup ──\nmain() {\n  echo m\n}\nhelp() {\n  echo h\n}\n")
	write("me/r.sh", "ours() {\n  echo o\n}\n")
	write("me/r.sh.lm", "base.main.after(self.ours)\nbase.function(\"help\").after(self.ours)\nbase.marker(\"# ── Setup\").after(self.ours)\n")
	write("up/c.toml", "alpha = 1\nbeta = 2\n")
	write("me/c.toml", "alpha = 9\n")
	write("me/c.toml.lm", "base.beta.drop(reason: \"not ours\")\nbase.key(\"alpha\").drop(reason: \"nor this\")\n")
	write("up/w.yaml", "name: n\njobs:\n  build:\n    runs-on: x\n")
	write("me/w.yaml", "name: o\n")
	write("me/w.yaml.lm", "base.jobs.build.drop(reason: \"ours differs\")\nbase.key(\"jobs.build.runs-on\").drop(reason: \"and this\")\n")

	c, err := lang.LoadConfig(filepath.Join(dir, "loom.om"))
	if err != nil {
		t.Fatal(err)
	}
	tpls, err := lang.Templates(c)
	if err != nil {
		t.Fatal(err)
	}

	type ask struct {
		Tpl  string `json:"tpl"`
		Line int    `json:"line"`
		Col  int    `json:"col"`
	}
	var asks []ask
	var want []int
	var what []string
	for _, path := range tpls {
		tp, err := lang.LoadTemplate(c, path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		src, ok, err := c.Read(c.Warp, tp.BasePath)
		if err != nil || !ok {
			t.Fatalf("no upstream for %s", tp.BasePath)
		}
		src2, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		tree := ast.New(tp.Type, src)
		for _, s := range tp.Stmts {
			if s.Anchor == "" || s.At.Line == 0 {
				continue
			}
			span, _, err := locate(tree, s.Kind, s.Within, s.Anchor, s.Ident, s)
			if err != nil {
				t.Fatalf("%s: the compiler cannot resolve %q: %v", path, s.Anchor, err)
			}
			// At points at the step that carries the anchor, which for a call is the method name
			// rather than the anchor itself. Find the anchor's own text on that line: in identifier
			// form a space is written as an underscore.
			// A dotted path is several names, and the cursor has to sit on the one the statement
			// as a whole resolves to — the last.
			line := strings.Split(string(src2), "\n")[s.At.Line-1]
			last := s.Anchor
			if i := strings.LastIndex(last, "."); i >= 0 && s.Kind == "key" {
				last = last[i+1:]
			}
			col := strings.Index(line, last)
			if col < 0 {
				col = strings.Index(line, strings.ReplaceAll(last, " ", "_"))
			}
			if col < 0 {
				t.Fatalf("%s:%d: cannot find %q on the line to point at", path, s.At.Line, s.Anchor)
			}
			asks = append(asks, ask{path, s.At.Line, col + 1})
			want = append(want, span[0])
			what = append(what, filepath.Base(path)+" "+s.Kind+" "+s.Anchor)
		}
	}
	if len(asks) < 11 {
		t.Fatalf("only %d anchors to compare; the fixture is not exercising the forms it claims to", len(asks))
	}

	in, err := json.Marshal(asks)
	if err != nil {
		t.Fatal(err)
	}
	// The compiler counts lines and columns from 1, the editor from 0.
	cmd := exec.Command(exe, "-e", `const { definition } = require("./lib/definition.js");
		const fs = require("fs");
		let src = ""; process.stdin.on("data", (d) => (src += d)).on("end", () => {
			console.log(JSON.stringify(JSON.parse(src).map((a) => {
				const hit = definition(a.tpl, fs.readFileSync(a.tpl, "utf8"), a.line - 1, a.col);
				return hit ? hit.line : -1;
			})));
		});`)
	cmd.Dir = ext
	cmd.Stdin = strings.NewReader(string(in))
	out, err := cmd.Output()
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			t.Fatalf("resolving with the editor: %v\n%s", err, e.Stderr)
		}
		t.Fatalf("resolving with the editor: %v", err)
	}
	var got []int
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	for k := range asks {
		if got[k] != want[k] {
			t.Errorf("%s: the editor resolves to line %d, the compiler to %d", what[k], got[k]+1, want[k]+1)
		}
	}
}
