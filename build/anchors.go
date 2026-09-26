package build

// Three anchor tools: check, list, and generate the derived view.
//
// Anchoring by name buys "lands in the right place even when the warp changes nearby", at the cost of
// **not seeing the anchors when reading a warp file**. These three tools give that visibility back, and
// more accurately than writing anchors into the warp would — they show where anchors resolve right now,
// not where they were when last injected.

import (
	"fmt"
	"github.com/axfor/loom/lang"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/axfor/loom/ast"
)

type anchorUse struct {
	kind, anchor string
	within       []lang.Seg
	ident        bool
	rng          lang.Pos
}

// anchorUses collects every anchor a template places something on, in upstream.
//
// Inline anchors count too, not only named ones. A tool that checks only named declarations reports
// "all 0 valid", which reads exactly like all green: a check that covered 0 items is not working.
func anchorUses(t *lang.Template) []anchorUse {
	var out []anchorUse
	var walk func([]lang.Stmt)
	walk = func(ss []lang.Stmt) {
		for _, s := range ss {
			switch s.Op {
			case "after", "before", "replace", "drop", "unwrap", "join", "split", "move", "swap", "promote", "demote":
				out = append(out, anchorUse{kind: s.Kind, anchor: s.Anchor, within: s.Within, ident: s.Ident, rng: s.Rng})
				// Where a move goes, what a swap trades with, where a split cuts: a node of
				// upstream's the template depends on as much as the one it changes.
				if m := s.Move; m != nil {
					out = append(out, anchorUse{kind: m.Kind, anchor: m.Anchor, within: m.Within, ident: m.Ident, rng: m.At})
				}
			case "if":
				// What an if asks about is a dependency too: rename it upstream and the question
				// quietly answers the other way.
				if c := s.Cond; c != nil && c.Has != "" {
					out = append(out, anchorUse{kind: c.Kind, anchor: c.Has, ident: c.HasIdent, rng: c.HasAt})
				}
				// Both branches anchor into upstream. Which one runs depends on upstream, and a
				// list of anchors that left one out would be a list of half the dependencies.
				walk(s.Kids)
				walk(s.Else)
			case "in":
				// anchors inside a view refer to the key's value, with coordinates of their own; not listed here
			}
		}
	}
	walk(t.Stmts)
	return out
}

// ListAnchors lists the upstream line each anchor currently resolves to. It resolves names the same
// way weaving does (identifier form, section paths), so what it shows is what the build will use.
func ListAnchors(c *lang.Config, w io.Writer) error {
	tpls, err := lang.Templates(c)
	if err != nil {
		return err
	}
	n := 0
	for _, p := range tpls {
		t, err := lang.LoadTemplate(c, p)
		if err != nil {
			return err
		}
		uses := anchorUses(t)
		if len(uses) == 0 {
			continue
		}
		src, _, _ := c.Read(c.Warp, t.BasePath)
		tree := ast.New(t.Type, src)
		fmt.Fprintln(w, t.Target)
		for _, u := range uses {
			label := u.anchor
			var where string
			if span, name, err := locate(tree, u.kind, u.within, u.anchor, u.ident, lang.Stmt{Rng: u.rng}); err == nil {
				label, where = name, fmt.Sprintf("upstream line %d", span[0]+1)
			} else if hits := len(tree.Find(u.kind, u.anchor)); hits > 1 {
				where = fmt.Sprintf("★ %d matches", hits)
			} else {
				where = "★ not found"
			}
			if len(u.within) > 0 {
				var q []string
				for _, seg := range u.within {
					q = append(q, seg.Name)
				}
				label = strings.Join(append(q, label), " › ")
			}
			fmt.Fprintf(w, "  %-40s %-9s %s\n", trunc(label, 40), u.kind, where)
			n++
		}
	}
	fmt.Fprintf(w, "%d anchors total\n", n)
	return nil
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-3]) + "..."
}

var extType = map[string]string{".md": "markdown", ".toml": "toml", ".sh": "shell", ".json": "json"}
var commentOf = map[string][2]string{
	"markdown": {"<!-- ", " -->"},
	"toml":     {"# ", ""},
	"shell":    {"# ", ""},
	"text":     {"# ", ""},
}

// AnchoredView generates the derived view — the warp copy plus a note at each **addressable anchor**.
//
// What it is and is not: a derived view, neither source nor product — it takes no part in weaving, does
// not go into the product, and is not committed. So the warp layer stays byte-identical to upstream, and
// "strip the weft and compare byte for byte" is still a one-line diff.
//
// Why mark every anchor, not just the used ones: when writing a template, what you really want to know
// is "which points in this file can I anchor to". Marking only the used ones shows only what is already
// used — and the unused ones are what you are looking for.
func AnchoredView(c *lang.Config, w io.Writer) error {
	if c.Anchored == "" {
		return fmt.Errorf("loom.om has no anchored setting — it does not say where to write the view")
	}
	tpls, err := lang.Templates(c)
	if err != nil {
		return err
	}
	// Only generate views for files that are actually woven — most of the hundreds of warp files have no
	// template, and generating them all would bury the few dozen that matter.
	want := map[string]bool{}
	for _, p := range tpls {
		t, err := lang.LoadTemplate(c, p)
		if err != nil {
			return err
		}
		want[t.BasePath] = true
	}
	outRoot := filepath.Join(c.Root, c.Anchored)
	if err := os.RemoveAll(outRoot); err != nil {
		return err
	}
	warpDir := filepath.Join(c.Root, c.Layers[c.Warp].Dir)
	files, marks := 0, 0
	err = filepath.WalkDir(warpDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(warpDir, p)
		if !want[rel] {
			return nil
		}
		typ, ok := extType[filepath.Ext(rel)]
		if !ok {
			typ = "text"
		}
		if typ == "json" {
			return nil // JSON has no comment syntax, so nothing can be marked
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		text := string(b)
		tree := ast.New(typ, text)
		at := map[int][]string{}
		for _, kind := range tree.Kinds() {
			if kind == "line" {
				continue // every line is a line anchor; marking them would just be noise
			}
			for _, nd := range ast.Addressable(tree, kind) {
				at[nd.Line] = append(at[nd.Line], fmt.Sprintf("%s %q", kind, nd.Name))
			}
		}
		cm := commentOf[typ]
		lines := strings.Split(text, "\n")
		res := []string{
			fmt.Sprintf("%sThis is a **derived view**; do not edit or reference it. Source: %s/%s; notes generated by lm view%s",
				cm[0], c.Layers[c.Warp].Dir, rel, cm[1]),
			fmt.Sprintf("%sEach ⚓ below marks an addressable anchor; a template can use it directly, e.g. base.\"<name>\".after(\"...\")%s", cm[0], cm[1]),
			"",
		}
		for i, l := range lines {
			for _, m := range at[i] {
				res = append(res, fmt.Sprintf("%s⚓ %s%s", cm[0], m, cm[1]))
				marks++
			}
			res = append(res, l)
		}
		dst := filepath.Join(outRoot, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		files++
		return os.WriteFile(dst, []byte(strings.Join(res, "\n")), 0o644)
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "✓ derived view: %d files, %d addressable anchors -> %s (not committed; regenerate any time)\n",
		files, marks, c.Anchored)
	return nil
}
