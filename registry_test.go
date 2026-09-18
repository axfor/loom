package loom_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/axfor/loom"
)

// regRepo is a repository whose json products are merged as a registry: a table of event -> handlers,
// where an entry's identity is the script it calls.
func regRepo(t *testing.T, files map[string]string) (*loom.Config, string) {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "loom.lm"),
		"base \"up\"\nself \"me\"\nregistry \"hooks\" \"hooks/([A-Za-z0-9._-]+\\.sh)\"\n")
	for p, s := range files {
		mustWrite(t, filepath.Join(dir, filepath.FromSlash(p)), s)
	}
	c, err := loom.LoadConfig(filepath.Join(dir, "loom.lm"))
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
		"me/hooks/hooks.lm": "// merged as a registry\n",
	})
	if _, err := build(t, c, filepath.Join(dir, "out")); err != nil {
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

// Without a registry block there is no way to tell what an entry's identity is, so duplicates could
// not be removed: that must be said, not guessed at.
func TestRegistryNeedsSettings(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "loom.lm"), "base \"up\"\nself \"me\"\n")
	mustWrite(t, filepath.Join(dir, "up", "hooks.json"), `{"hooks":{}}`)
	mustWrite(t, filepath.Join(dir, "me", "hooks.json"), `{"hooks":{}}`)
	mustWrite(t, filepath.Join(dir, "me", "hooks.lm"), "// merged as a registry\n")
	c, err := loom.LoadConfig(filepath.Join(dir, "loom.lm"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := build(t, c, filepath.Join(dir, "out")); err == nil || !strings.Contains(err.Error(), "registry") {
		t.Errorf("want an error asking for the registry block, got: %v", err)
	}
}
