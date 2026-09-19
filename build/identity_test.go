package build

import (
	"strings"
	"testing"
)

// A rename is only ever recorded when a name vanished and exactly one new name holds
// byte-identical content. Everything less certain is a deletion or an ambiguity, because a
// template rewritten to point at the wrong section reads exactly like one pointing at the right
// one — and that is the failure this language exists to prevent.
func TestRenames(t *testing.T) {
	const old = "## Overview\n\nsame text\n\n## Keep\n\nk\n"

	for _, c := range []struct {
		what      string
		now       string
		wantOld   string
		wantNew   string
		wantAmbig []string
	}{
		{
			what:    "a rename: the name changed and the content did not",
			now:     "## Introduction\n\nsame text\n\n## Keep\n\nk\n",
			wantOld: "Overview", wantNew: "Introduction",
		},
		{
			what: "a deletion: nothing new holds that content",
			now:  "## Keep\n\nk\n",
		},
		{
			what: "a rewrite: both the name and the content changed",
			now:  "## Introduction\n\nrewritten\n\n## Keep\n\nk\n",
		},
		{
			what:      "ambiguous: two new names hold it",
			now:       "## A\n\nsame text\n\n## B\n\nsame text\n\n## Keep\n\nk\n",
			wantAmbig: []string{"A", "B"},
		},
		{
			what: "untouched: nothing vanished",
			now:  old,
		},
	} {
		rs, amb := renames("doc.md", "markdown", old, c.now)
		switch {
		case c.wantNew != "":
			if len(rs) != 1 {
				t.Errorf("%s: %d renames, want 1 (%+v)", c.what, len(rs), rs)
				continue
			}
			if rs[0].Old != c.wantOld || rs[0].New != c.wantNew {
				t.Errorf("%s: %s → %s, want %s → %s", c.what, rs[0].Old, rs[0].New, c.wantOld, c.wantNew)
			}
		case len(c.wantAmbig) > 0:
			if len(rs) != 0 {
				t.Errorf("%s: followed something ambiguous: %+v", c.what, rs)
			}
			if len(amb) != 1 || strings.Join(amb[0].Could, ",") != strings.Join(c.wantAmbig, ",") {
				t.Errorf("%s: ambiguities %+v, want %v", c.what, amb, c.wantAmbig)
			}
		default:
			if len(rs) != 0 {
				t.Errorf("%s: should follow nothing, got %+v", c.what, rs)
			}
		}
	}
}

// An empty node says nothing about which new name it became, so it is never matched by content.
func TestRenamesIgnoresEmptyNodes(t *testing.T) {
	rs, _ := renames("doc.md", "markdown", "## Gone\n\n## Keep\n\nk\n", "## Fresh\n\n## Keep\n\nk\n")
	if len(rs) != 0 {
		t.Errorf("two empty sections were paired by their emptiness: %+v", rs)
	}
}

// Identity is not a markdown affair: a shell function or a yaml key is named by its author too.
func TestRenamesAcrossKinds(t *testing.T) {
	rs, _ := renames("run.sh", "shell",
		"old_name() {\n  echo hi\n}\n",
		"new_name() {\n  echo hi\n}\n")
	if len(rs) != 1 || rs[0].Old != "old_name" || rs[0].New != "new_name" {
		t.Errorf("shell function rename not found: %+v", rs)
	}

	rs, _ = renames("ci.yaml", "yaml",
		"jobs:\n  old:\n    runs-on: ubuntu\n",
		"jobs:\n  new:\n    runs-on: ubuntu\n")
	found := false
	for _, r := range rs {
		if r.Old == "jobs.old" && r.New == "jobs.new" {
			found = true
		}
	}
	if !found {
		t.Errorf("yaml key rename not found: %+v", rs)
	}
}
