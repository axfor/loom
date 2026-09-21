// Package loomrt is what a compiled weaver links against.
//
// `lm compile` turns one template into a Go program: the template's control flow — its ifs, its
// caught results, its return — becomes Go control flow, and each statement becomes a call into
// this package. The program is then built into a native binary that takes upstream as its input
// and writes the product to its output.
//
// So the compiled thing is a weaver for one document, and the language version it was compiled
// with is fixed inside it. A tree built by a binary from last year builds the same way this year,
// whatever lm has learned since.
package loomrt

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/axfor/loom/ast"
	"github.com/axfor/loom/build"
	"github.com/axfor/loom/lang"
)

// Weaver holds one compiled template and the documents it is running against.
type Weaver struct {
	cfg  *lang.Config
	tmpl *lang.Template
	tree ast.Tree
	vars map[string]string // a caught result: "" when the write went through, else why not
	out  []lang.Stmt
	done bool
}

// Meta is what the compiler knows about a template and the runtime needs at run time.
type Meta struct {
	Target      string // the product path this template builds
	Type        string // markdown / shell / toml / yaml / json / text
	BasePath    string // the upstream file, relative to the upstream layer
	From        string // "self" = the product is our file
	Source      string // whole-file replace: which file of ours
	UseReason   string //
	Marks       map[string]lang.Marks
	Resources   []lang.Resource
	Loom        string // the language version this was compiled against
	Frontmatter string
	Keys        string
	Body        string
}

// New prepares a weaver: upstream's text, ours if there is any, and the template the compiler
// recorded. Everything lives in memory — a compiled weaver has no tree to walk.
func New(m Meta, base, self string) (*Weaver, error) {
	cfg := &lang.Config{
		Root:   ".",
		Layers: map[string]*lang.Layer{},
		Warp:   "base", Weft: "self",
		Loom:        m.Loom,
		Frontmatter: m.Frontmatter, Keys: m.Keys, Body: m.Body,
		Files: map[string]string{},
	}
	cfg.Layers["base"] = &lang.Layer{Name: "base", Dir: "base", Role: "warp", Marks: m.Marks}
	cfg.Layers["self"] = &lang.Layer{Name: "self", Dir: "self", Role: "weft", Marks: m.Marks}
	cfg.Order = []string{"base", "self"}
	cfg.Files["base/"+m.BasePath] = base
	if self != "" {
		cfg.Files["self/"+m.Target] = self
	}
	t := &lang.Template{
		Path: m.Target + lang.Ext, Target: m.Target, Type: m.Type, BasePath: m.BasePath,
		From: m.From, Source: m.Source, UseReason: m.UseReason, Resources: m.Resources,
	}
	return &Weaver{cfg: cfg, tmpl: t, tree: ast.New(m.Type, base), vars: map[string]string{}}, nil
}

// Do records a statement. The compiled program calls these in the order the template wrote them;
// which ones it calls is what its own control flow decided.
func (w *Weaver) Do(s lang.Stmt) {
	if w.done {
		return
	}
	w.out = append(w.out, s)
}

// Try records a statement whose result is caught. A write that cannot land is this statement's
// answer rather than the build's end, so it simply does not run.
func (w *Weaver) Try(name string, s lang.Stmt) {
	if w.done {
		return
	}
	if err := build.CanPlace(w.tree, s); err != nil {
		w.vars[name] = err.Error()
		return
	}
	w.vars[name] = ""
	w.out = append(w.out, s)
}

// Held reports whether a caught result went through.
func (w *Weaver) Held(name string) bool { return w.vars[name] == "" }

// Why is what a caught result says about itself, for err.format.
func (w *Weaver) Why(name string) string {
	if v := w.vars[name]; v != "" {
		return v
	}
	return "it went through"
}

// Has answers `base.has.Name` — a read-only question, so it is always safe to ask.
func (w *Weaver) Has(kind, name string) bool { return len(w.tree.Find(kind, name)) > 0 }

// Any answers `base.sections[...].any` — did the predicate find anything.
func (w *Weaver) Any(sel *lang.Select) bool {
	n, err := build.Selected(w.tree, sel)
	return err == nil && len(n) > 0
}

// Stop ends the template early: what has been written stands.
func (w *Weaver) Stop() { w.done = true }

// Fail ends the build with a message of the template's own.
func (w *Weaver) Fail(msg string) error { return fmt.Errorf("%s", msg) }

// Weave applies what was recorded and returns the product.
func (w *Weaver) Weave() (string, error) {
	w.tmpl.Stmts = w.out
	return build.Weave(w.cfg, w.tmpl)
}

// Main is the compiled program's entry point: read upstream, weave, write the product.
func Main(m Meta, run func(*Weaver) error) {
	base := flag.String("base", "", "the upstream file this weaves onto (required)")
	self := flag.String("self", "", "our file, when the template's content is not written into it")
	out := flag.String("o", "", "where to write the product (default: standard output)")
	show := flag.Bool("version", false, "print what this was compiled from and stop")
	flag.Parse()
	if *show {
		fmt.Printf("%s — woven from %s, loom %s\n", m.Target, m.BasePath, or(m.Loom, "unstated"))
		return
	}
	if *base == "" {
		fmt.Fprintf(os.Stderr, "⛔ this weaver needs the upstream file it was compiled against: --base %s\n", m.BasePath)
		os.Exit(2)
	}
	up, err := os.ReadFile(*base)
	if err != nil {
		die(err)
	}
	var ours string
	if *self != "" {
		b, err := os.ReadFile(*self)
		if err != nil {
			die(err)
		}
		ours = string(b)
	}
	w, err := New(m, string(up), ours)
	if err != nil {
		die(err)
	}
	if err := run(w); err != nil {
		die(err)
	}
	text, err := w.Weave()
	if err != nil {
		die(err)
	}
	if *out == "" {
		fmt.Print(text)
		return
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		die(err)
	}
	if err := os.WriteFile(*out, []byte(text), 0o644); err != nil {
		die(err)
	}
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "⛔", err)
	os.Exit(1)
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
