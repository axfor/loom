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
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const ext = "../editors/vscode"

// mirror runs the extension's own module and returns the tables it exports.
func mirror(t *testing.T) map[string]any {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed; the editor mirror cannot be checked here")
	}
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
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed; the editor mirror cannot be checked here")
	}
	cmd := exec.Command(node, "-e", `const s = require("fs").readFileSync("lib/completion.js", "utf8");
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
