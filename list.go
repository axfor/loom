package loom

// loom list — prints each template's metadata as JSON.
//
// Why this command exists: projects using this language often have a series of gates that
// ask about template metadata: where does this template weave to? Which layer is its base?
// Which upstream files does it replace? Which functions does it declare as inherited or
// overridden? If every gate re-parses with its own awk or regex, every gate parses slightly
// differently. In practice one gate scanned templates with Python's glob, which by default
// skips dot-directories, so 17 templates never applied to it — yet it still reported
// "57 templates" and read as if it had checked everything. A blind spot in default behavior
// is harder to find than a wrong criterion. There should be exactly one parser.

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"

	"github.com/axfor/loom/ast"
)

// Info is all the externally visible metadata of one template.
type Info struct {
	Template string `json:"template"` // template path (relative to the repo root)
	Target   string `json:"target"`   // product path
	Type     string `json:"type"`
	From     string `json:"from"`    // "base": woven on upstream; "self": the whole file is replaced with ours
	Path     string `json:"path"`    // base path in upstream
	Merge    bool   `json:"merge"`   // our file is upstream plus our edits: base.merge(self)
	Anchors  []Use  `json:"anchors"` // anchor points on the upstream
	Inserts  []Src  `json:"inserts"` // content taken from the layers

	// Why upstream content is changed, as written with reason: in the template. Tools that account
	// for upstream content read these instead of parsing templates themselves.
	Reason   string     `json:"reason,omitempty"` // whole-file replace: base.replace(self, reason: "...")
	Drops    []Reasoned `json:"drops"`            // base.X.drop(reason: "...")
	Replaces []Reasoned `json:"replaces"`         // base.X.replace(..., reason: "...")
}

// Reasoned is an upstream node changed on purpose, with its reason.
type Reasoned struct {
	Kind   string `json:"kind"`
	Anchor string `json:"anchor"` // the real name in upstream (identifier forms and section paths resolved)
	Reason string `json:"reason"`
	Where  string `json:"where"` // position in the template
}

// Use is an anchor point: which kind of upstream node, with which name.
type Use struct {
	Kind   string `json:"kind"`
	Anchor string `json:"anchor"`
	Where  string `json:"where"` // position in the template
}

// Src is one content source.
type Src struct {
	Layer  string `json:"layer"`
	Kind   string `json:"kind"`
	Anchor string `json:"anchor,omitempty"`
	File   string `json:"file,omitempty"` // file of ours the content comes from; empty = the product's path
	Lit    bool   `json:"literal,omitempty"`
}

// Describe reads one template's metadata.
func Describe(c *Config, path string) (*Info, error) {
	t, err := LoadTemplate(c, path)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(c.Root, path)
	if err != nil {
		rel = path
	}
	from := t.From
	if from == "" {
		from = c.Warp
	}
	i := &Info{
		Template: rel, Target: t.Target, Type: t.Type, From: from, Path: t.BasePath,
		Anchors: []Use{}, Inserts: []Src{},
		Reason: t.UseReason, Drops: []Reasoned{}, Replaces: []Reasoned{},
	}
	// Names in identifier form (base.Usage_Tips) are reported as their real names in the
	// tree — gates reading list match them against headings, and reporting the spelling as
	// written would flag a perfectly good anchor as missing. Names that cannot be resolved
	// are reported as written, leaving the gate or the weave to say what is wrong.
	baseTree := func(in *Stmt) ast.Tree {
		src, ok, err := c.read("base", t.BasePath)
		if err != nil || !ok {
			return nil
		}
		tree := ast.New(t.Type, src)
		if in == nil {
			return tree
		}
		body, ok := tree.BodyOf(in.Key)
		if !ok {
			return nil
		}
		return ast.New(in.As, body)
	}
	real := func(tree ast.Tree, kind string, within []Seg, name string, ident bool) string {
		if tree == nil || (!ident && len(within) == 0) {
			return name
		}
		if _, r, err := locate(tree, kind, within, name, ident, Stmt{}); err == nil {
			return r
		}
		return name
	}
	srcs := func(rs []Ref, in *Stmt) []Src {
		var out []Src
		for _, r := range rs {
			anchor := r.Anchor
			if r.Ident || len(r.Within) > 0 {
				var nest *nestCtx
				if in != nil {
					nest = &nestCtx{typ: t.Type, key: in.Key}
				}
				if src, useNest, err := refSource(c, t, r, t.Target, nest); err == nil {
					anchor = real(refTree(t, r.Kind, src, useNest), r.Kind, r.Within, anchor, r.Ident)
				}
			}
			out = append(out, Src{Layer: r.Layer, Kind: r.Kind, Anchor: anchor, File: r.File, Lit: r.IsLit})
		}
		return out
	}
	var walk func(ss []Stmt, in *Stmt)
	walk = func(ss []Stmt, in *Stmt) {
		for _, s := range ss {
			switch s.Op {
			case "merge":
				i.Merge = true
			case "after", "before", "replace", "drop":
				u := Use{Kind: s.Kind, Anchor: s.Anchor, Where: s.Rng.String()}
				if s.Ident || len(s.Within) > 0 {
					u.Anchor = real(baseTree(in), s.Kind, s.Within, s.Anchor, s.Ident)
				}
				i.Anchors = append(i.Anchors, u)
				i.Inserts = append(i.Inserts, srcs(s.Srcs, in)...)
				switch s.Op {
				case "drop":
					i.Drops = append(i.Drops, Reasoned{u.Kind, u.Anchor, s.Reason, u.Where})
				case "replace":
					i.Replaces = append(i.Replaces, Reasoned{u.Kind, u.Anchor, s.Reason, u.Where})
				}
			case "append", "prepend":
				i.Inserts = append(i.Inserts, srcs(s.Srcs, in)...)
			case "frontmatter":
				i.Inserts = append(i.Inserts, Src{Layer: s.Layer, Kind: "frontmatter", File: s.File})
			case "setgroup":
				for _, kv := range s.Kids {
					i.Inserts = append(i.Inserts, Src{Layer: kv.SetRef.Layer, Kind: kv.SetRef.Kind, File: kv.SetRef.File})
				}
			case "in":
				walk(s.Kids, &s)
			}
		}
	}
	walk(t.Stmts, nil)
	return i, nil
}

// ListTSV prints just six columns: product path / template path / type / base layer /
// base path / "merge" for a merge template (empty otherwise).
//
// Why not let callers pick fields out of the JSON with jq: the build script itself needs
// this list most, and the build is the lowest link in the chain — each dependency it adds
// is one more way a fresh clone can fail to set up. Six columns of plain text are
// readable by awk.
func ListTSV(c *Config, w io.Writer) error {
	tpls, err := Templates(c)
	if err != nil {
		return err
	}
	for _, p := range tpls {
		i, err := Describe(c, p)
		if err != nil {
			return err
		}
		merge := ""
		if i.Merge {
			merge = "merge"
		}
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", i.Target, i.Template, i.Type, i.From, i.Path, merge); err != nil {
			return err
		}
	}
	return nil
}

// List writes every template's metadata as JSON.
func List(c *Config, w io.Writer) error {
	tpls, err := Templates(c)
	if err != nil {
		return err
	}
	out := make([]*Info, 0, len(tpls))
	for _, p := range tpls {
		i, err := Describe(c, p)
		if err != nil {
			return err
		}
		out = append(out, i)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(out)
}
