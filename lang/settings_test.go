package lang

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A tree may say which language it was written for. One asking for something this compiler
// does not speak is refused with what to do about it; an older one is fine, because nothing
// it can say has changed meaning.
func TestLoomVersion(t *testing.T) {
	for _, c := range []struct{ decl, wantErr string }{
		{`loom "1.0"`, ""},
		{`loom "0.9"`, ""},
		{`loom "2.0"`, ""},
		{`loom "3.0"`, "run `lm update`"},
		{`loom "2.99"`, "run `lm update`"},
		{`loom "x"`, `takes a version like`},
		{`loom "1"`, `takes a version like`},
		{`loom`, "takes the language version"},
	} {
		dir := t.TempDir()
		p := filepath.Join(dir, ConfigName)
		if err := os.WriteFile(p, []byte(c.decl+"\nbase \"up\"\nself \"me\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadConfig(p)
		switch {
		case c.wantErr == "" && err != nil:
			t.Errorf("%s: %v", c.decl, err)
		case c.wantErr == "" && cfg.Loom == "":
			t.Errorf("%s: the declared version was not kept", c.decl)
		case c.wantErr != "" && err == nil:
			t.Errorf("%s: accepted, want an error mentioning %q", c.decl, c.wantErr)
		case c.wantErr != "" && err != nil && !strings.Contains(err.Error(), c.wantErr):
			t.Errorf("%s: %v\nwant %q", c.decl, err, c.wantErr)
		}
	}

	// Saying nothing is still allowed: a tree that predates the declaration builds.
	dir := t.TempDir()
	p := filepath.Join(dir, ConfigName)
	if err := os.WriteFile(p, []byte("base \"up\"\nself \"me\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(p); err != nil {
		t.Errorf("a tree that declares no version should build: %v", err)
	}
}
