package build_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/axfor/loom/lang"
)

// regRepo is a repository whose json products are merged as a registry: a table of event -> handlers,
// where an entry's identity is the script it calls.
func regRepo(t *testing.T, files map[string]string) (*lang.Config, string) {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "build.lm"), "base \"up\"\nself \"me\"\n")
	for p, s := range files {
		mustWrite(t, filepath.Join(dir, filepath.FromSlash(p)), s)
	}
	c, err := lang.LoadConfig(filepath.Join(dir, "build.lm"))
	if err != nil {
		t.Fatal(err)
	}
	return c, dir
}

func entry(script string) string {
	return `{"type":"command","command":"bash \"${ROOT}/hooks/` + script + `\""}`
}

// The registry merge: upstream's registrations stay, ours replace the ones calling the same script,
// and ours alone are added. A duplicate registration is what must not happen — the file stays valid
// JSON and the handler simply runs twice.
func TestRegistryMerge(t *testing.T) {
	c, dir := regRepo(t, map[string]string{
		"up/hooks/hooks.json": `{"hooks":{"SessionStart":[{"hooks":[` + entry("session-start.sh") + `]}],` +
			`"PreToolUse":[{"matcher":"Write","hooks":[` + entry("upstream-only.sh") + `]}]}}`,
		"me/hooks/hooks.json": `{"hooks":{"SessionStart":[{"hooks":[` + entry("session-start.sh") + `]}],` +
			`"PreToolUse":[{"matcher":"Write|Edit","hooks":[` + entry("ours.sh") + `]}]}}`,
		"me/hooks/hooks.lm": "base.merge(self)\n",
	})
	if _, err := buildTree(t, c, filepath.Join(dir, "out")); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, filepath.Join(dir, "out", "hooks", "hooks.json"))
	if n := strings.Count(got, "session-start.sh"); n != 1 {
		t.Errorf("our registration replaces upstream's for the same script, got %d of them:\n%s", n, got)
	}
	for _, want := range []string{"upstream-only.sh", "ours.sh", `"matcher": "Write|Edit"`} {
		if !strings.Contains(got, want) {
			t.Errorf("%s is missing from the product:\n%s", want, got)
		}
	}
}

// The registry setting is gone: there is nothing to configure, and a repository still carrying the
// line is told what to do instead of being merged by a rule it does not name.
func TestRegistrySettingIsGone(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "build.lm"), "base \"up\"\nself \"me\"\nregistry \"hooks\" \"hooks/(x)\"\n")
	_, err := lang.LoadConfig(filepath.Join(dir, "build.lm"))
	if err == nil || !strings.Contains(err.Error(), "base.merge(self)") {
		t.Errorf("want the line refused with what to do instead, got: %v", err)
	}
}

// A registry is merged whole, so weaving statements have no part in it. Keeping them would be the
// worst kind of no-op: the template says something the build does not do, and nothing says so.
func TestStatementsInAJsonTemplateAreRefused(t *testing.T) {
	c, dir := regRepo(t, map[string]string{
		"up/hooks/hooks.json": `{"hooks":{"SessionStart":[{"hooks":[` + entry("session-start.sh") + `]}]}}`,
		"me/hooks/hooks.json": `{"hooks":{"SessionStart":[{"hooks":[` + entry("ours.sh") + `]}]}}`,
		"me/hooks/hooks.lm":   "base.merge(self)\nbase.SessionStart.after(`x`)\n",
	})
	_, err := buildTree(t, c, filepath.Join(dir, "out"))
	if err == nil || !strings.Contains(err.Error(), "base.merge(self)") {
		t.Fatalf("want an error naming the one statement a registry takes, got: %v", err)
	}

	// and an empty template is refused too: it would be the only way to ask for this, and it says nothing
	mustWrite(t, filepath.Join(dir, "me", "hooks", "hooks.lm"), "// merged somehow?\n")
	if _, err := buildTree(t, c, filepath.Join(dir, "out")); err == nil || !strings.Contains(err.Error(), "base.merge(self)") {
		t.Fatalf("want the same error for a template with no statement, got: %v", err)
	}
}

// The same handler under two different matchers is deliberate — the matchers cover different tools —
// and both of ours must survive. Only the same handler twice in one list of entries is the mistake.
func TestSameHandlerUnderTwoMatchers(t *testing.T) {
	c, dir := regRepo(t, map[string]string{
		"up/hooks/hooks.json": `{"hooks":{"PreToolUse":[{"matcher":"Write","hooks":[` + entry("gate.sh") + `]}]}}`,
		"me/hooks/hooks.json": `{"hooks":{"PreToolUse":[` +
			`{"matcher":"Write|Edit","hooks":[` + entry("gate.sh") + `]},` +
			`{"matcher":"Bash","hooks":[` + entry("gate.sh") + `]}]}}`,
		"me/hooks/hooks.lm": "base.merge(self)\n",
	})
	if _, err := buildTree(t, c, filepath.Join(dir, "out")); err != nil {
		t.Fatalf("two matchers for one handler must be allowed: %v", err)
	}
	got := readFile(t, filepath.Join(dir, "out", "hooks", "hooks.json"))
	for _, want := range []string{`"matcher": "Write|Edit"`, `"matcher": "Bash"`} {
		if !strings.Contains(got, want) {
			t.Errorf("%s is missing:\n%s", want, got)
		}
	}
	if strings.Contains(got, `"matcher": "Write"`+"\n") {
		t.Errorf("upstream's registration of the same handler is replaced by ours:\n%s", got)
	}
}
