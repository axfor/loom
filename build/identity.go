package build

// Identity across a sync.
//
// A node is addressed by the words its author wrote, so a name is not a property of a document —
// it is a property of one version of it. When upstream renames a heading, every anchor pointing at
// it stops resolving, and the build stops with "not found", which is true and useless.
//
// The one moment both versions are in hand is the sync that replaces upstream. That is where a
// rename can be told apart from a deletion: the name is gone and another has appeared holding
// exactly the same content. Anything less certain than that is reported, never guessed — a
// template rewritten to point at the wrong section reads exactly like one pointing at the right
// one, which is the failure this whole language exists to prevent.

import (
	"fmt"
	"os"
	"strings"

	"github.com/axfor/loom/ast"
	"github.com/axfor/loom/lang"
)

// Rename is one name upstream changed, and what it changed to.
type Rename struct {
	File string // upstream path
	Kind string
	Old  string
	New  string
}

// Ambiguity is a name that vanished and could have become any of several — so it is left alone.
type Ambiguity struct {
	File  string
	Kind  string
	Old   string
	Could []string
}

// renames compares one file's two versions and reports what was renamed. A name is treated as
// renamed only when exactly one new name holds byte-identical content; a vanished name with no
// such partner was deleted, and one with several is ambiguous.
func renames(file, typ, old, now string) ([]Rename, []Ambiguity) {
	oldTree, nowTree := ast.New(typ, old), ast.New(typ, now)
	if oldTree == nil || nowTree == nil {
		return nil, nil
	}
	var out []Rename
	var amb []Ambiguity
	for _, kind := range ast.Kinds(typ) {
		oldNodes := ast.Addressable(oldTree, kind)
		nowNodes := ast.Addressable(nowTree, kind)
		nowNames := map[string]bool{}
		for _, n := range nowNodes {
			nowNames[n.Name] = true
		}
		oldNames := map[string]bool{}
		for _, n := range oldNodes {
			oldNames[n.Name] = true
		}
		for _, g := range oldNodes {
			if nowNames[g.Name] {
				continue
			}
			want := body(oldTree, g)
			if strings.TrimSpace(want) == "" {
				continue // an empty node says nothing about which new name it became
			}
			var could []string
			for _, f := range nowNodes {
				if oldNames[f.Name] {
					continue
				}
				if body(nowTree, f) == want {
					could = append(could, f.Name)
				}
			}
			switch len(could) {
			case 0:
			case 1:
				out = append(out, Rename{file, kind, g.Name, could[0]})
			default:
				amb = append(amb, Ambiguity{file, kind, g.Name, could})
			}
		}
	}
	return out, amb
}

// body is a node's content without the line its name is on — the name is what changed, so
// comparing it would answer the question with itself.
func body(tree ast.Tree, n ast.Named) string {
	if n.Line+1 >= n.End {
		return ""
	}
	return strings.Join(tree.Lines()[n.Line+1:n.End], "\n")
}

// follow rewrites the anchors of one template so they point at upstream's new names. It edits the
// name where it is written and nothing else, so the diff a person reviews is one word per rename.
func follow(t *lang.Template, rs []Rename) ([]string, error) {
	by := map[string]Rename{}
	for _, r := range rs {
		by[r.Kind+"\x00"+r.Old] = r
	}
	src, err := os.ReadFile(t.Path)
	if err != nil {
		return nil, err
	}
	toks, err := lang.Lex(t.Path, src)
	if err != nil {
		return nil, err
	}
	type edit struct {
		at, end int
		text    string
		note    string
	}
	var edits []edit
	var walk func(ss []lang.Stmt)
	walk = func(ss []lang.Stmt) {
		for _, s := range ss {
			walk(s.Kids)
			r, ok := by[s.Kind+"\x00"+s.Anchor]
			if !ok || s.At.Line == 0 {
				continue
			}
			for _, tk := range toks {
				if tk.Pos.Line != s.At.Line || tk.Pos.Col != s.At.Col {
					continue
				}
				edits = append(edits, edit{tk.Off, tk.End, lang.NameText(r.New),
					fmt.Sprintf("%s → %s", r.Old, r.New)})
				break
			}
		}
	}
	walk(t.Stmts)
	if len(edits) == 0 {
		return nil, nil
	}
	// Back to front, so an earlier edit does not move a later one.
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		src = append(src[:e.at], append([]byte(e.text), src[e.end:]...)...)
	}
	if err := os.WriteFile(t.Path, src, 0o644); err != nil {
		return nil, err
	}
	var notes []string
	for _, e := range edits {
		notes = append(notes, e.note)
	}
	return notes, nil
}
