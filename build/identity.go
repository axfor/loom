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
	"sort"
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
func follow(t *lang.Template, rs []Rename, loom string) ([]string, error) {
	src, err := os.ReadFile(t.Path)
	if err != nil {
		return nil, err
	}
	toks, err := lang.Lex(t.Path, src)
	if err != nil {
		return nil, err
	}
	// Every place the template names an upstream node: a statement's own node, each parent in its
	// path, where a move, swap or split points, and what an if asks about. Following only the
	// first left the others naming what upstream no longer has — and an if asking about it would
	// quietly take its other branch.
	type use struct {
		kind, name string
		ident      bool
		at         lang.Pos
	}
	var uses []use
	add := func(kind, name string, ident bool, at lang.Pos) {
		if name != "" && at.Line > 0 {
			uses = append(uses, use{kind, name, ident, at})
		}
	}
	var walk func(ss []lang.Stmt)
	walk = func(ss []lang.Stmt) {
		for _, s := range ss {
			walk(s.Kids)
			walk(s.Else) // an if's other branch anchors into upstream too
			add(s.Kind, s.Anchor, s.Ident, s.At)
			for _, seg := range s.Within {
				add("heading", seg.Name, seg.Ident, seg.At)
			}
			if m := s.Move; m != nil {
				add(m.Kind, m.Anchor, m.Ident, m.At)
				for _, seg := range m.Within {
					add("heading", seg.Name, seg.Ident, seg.At)
				}
			}
			if c := s.Cond; c != nil {
				add(c.Kind, c.Has, c.HasIdent, c.HasAt)
			}
		}
	}
	walk(t.Stmts)

	type edit struct {
		at, end int
		text    string
		note    string
	}
	var edits []edit
	done := map[int]bool{}
	// at finds the token of a use; back > 0 steps back over that many `.segment`s of its path, to
	// a parent's segment in a key's dotted path.
	at := func(p lang.Pos, back int) int {
		k := -1
		for i, tk := range toks {
			if tk.Pos.Line == p.Line && tk.Pos.Col == p.Col {
				k = i
				break
			}
		}
		for ; k >= 0 && back > 0; back-- {
			if k < 2 || toks[k-1].Kind != lang.KDot || (toks[k-2].Kind != lang.KIdent && toks[k-2].Kind != lang.KString) {
				return -1
			}
			k -= 2
		}
		return k
	}
	rewrite := func(k int, text, note string) {
		if k >= 0 && !done[toks[k].Off] {
			done[toks[k].Off] = true
			edits = append(edits, edit{toks[k].Off, toks[k].End, text, note})
		}
	}
	for _, u := range uses {
		for _, r := range rs {
			if r.Kind != u.kind {
				continue
			}
			note := fmt.Sprintf("%s → %s", r.Old, r.New)
			if !strings.Contains(r.Old, ".") && !strings.Contains(u.name, ".") {
				// Loom 1 reads an unquoted name by the underscore rule, so Set_Up names "Set Up".
				if u.name == r.Old || (u.ident && identMatch(r.Old, u.name)) {
					rewrite(at(u.at, 0), lang.NameTextFor(r.New, loom), note)
				}
				continue
			}
			// A key is named by its dotted path, one segment per token. A rename of the key or of
			// a parent rewrites that one segment; a rename that also moved it under another
			// parent is not one word, and is left for a person.
			old, now, name := strings.Split(r.Old, "."), strings.Split(r.New, "."), strings.Split(u.name, ".")
			n := len(old)
			if len(now) != n || strings.Join(old[:n-1], ".") != strings.Join(now[:n-1], ".") {
				continue
			}
			if u.name == r.Old || strings.HasPrefix(u.name, r.Old+".") {
				rewrite(at(u.at, len(name)-n), lang.NameTextFor(now[n-1], loom), note)
			}
		}
	}
	if len(edits) == 0 {
		return nil, nil
	}
	// Back to front, so an earlier edit does not move a later one.
	sort.Slice(edits, func(i, j int) bool { return edits[i].at < edits[j].at })
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		src = append(src[:e.at], append([]byte(e.text), src[e.end:]...)...)
	}
	if err := os.WriteFile(t.Path, src, 0o644); err != nil {
		return nil, err
	}
	// One line per rename, however many places in the template it was written.
	var notes []string
	seen := map[string]bool{}
	for _, e := range edits {
		if !seen[e.note] {
			seen[e.note] = true
			notes = append(notes, e.note)
		}
	}
	return notes, nil
}
