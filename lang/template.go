package lang

// The template's intermediate form: what a template says, independent of how it is written.
// objparse.go reads object syntax into it; the engine (weave.go) and the tools (list, anchors,
// report) read it.

import "fmt"

// Ref is one content source: a node of a file in our layer, or a literal.
type Ref struct {
	Layer   string // layer name (always our layer: content never comes from upstream)
	Kind    string // heading / line / frontmatter / key / path / function / marker / body / all
	Anchor  string // body / all have no anchor
	Literal string // literal content
	IsLit   bool
	File    string // file the content comes from (path in the layer); empty = product path
	Ident   bool   // name is in identifier form: _ matches a space or an underscore
	Within  []Seg  // section path: look under these headings (self."Parent"."Name")
	Rng     Pos
}

func (r Ref) String() string {
	if r.IsLit {
		return "<literal>"
	}
	if r.Anchor == "" {
		return r.Layer + "." + r.Kind
	}
	return fmt.Sprintf("%s.%s[%q]", r.Layer, r.Kind, r.Anchor)
}

// Stmt is one statement. Op decides which fields are read.
type Stmt struct {
	Op     string // after / before / replace / drop / append / prepend / frontmatter / value / in / merge / registry
	Kind   string // position: node kind
	Anchor string // position: node name
	Ident  bool   // the name is in identifier form: _ matches a space or an underscore
	Within []Seg  // section path: look under these headings (base."Parent"."Name")
	Srcs   []Ref  // what to insert

	As   string // in: the type to view the key's value as
	Key  string // in: which key
	Kids []Stmt // in: the statements inside

	Layer  string // frontmatter: layer to take the whole block from
	File   string // frontmatter: file it comes from; empty = product path
	SetKey string // value: key to write
	SetRef Ref    // value: where our value comes from (a key of ours, or a literal)
	Mode   string // value: set / start / append
	Reason string // replace / drop: why the upstream content is changed
	Move   *Move  // move: where the node goes

	Rng Pos
}

// Move is where a moved node goes: which side of which other node of the same file.
// Nothing is inserted and nothing is lost, so a move needs no reason — the node's own
// bytes are still in the product, which the build checks.
type Move struct {
	Side   string // "after" or "before"
	Kind   string
	Anchor string
	Ident  bool
	Within []Seg
}

// Template is one template: which product it weaves, from which base, and how.
type Template struct {
	Path      string
	Target    string
	Type      string
	From      string // "" = upstream is the base; "self" = the whole file is replaced with ours
	BasePath  string // path of the base in upstream (import base; default = Target)
	Source    string // whole-file replace: file of ours to use (default = Target)
	UseReason string // whole-file replace: why
	Stmts     []Stmt
}

// Seg is one segment of a section path: a name, and whether it is in identifier form.
type Seg struct {
	Name  string
	Ident bool
}
