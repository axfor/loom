package loom

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
	Op     string // after / before / replace / drop / append / prepend / frontmatter / setgroup / bilingual / in / patch
	Kind   string // position: node kind
	Anchor string // position: node name
	Ident  bool   // the name is in identifier form: _ matches a space or an underscore
	Within []Seg  // section path: look under these headings (base."Parent"."Name")
	Srcs   []Ref  // what to insert

	As   string // in: the type to view the key's value as
	Key  string // in: which key
	Kids []Stmt // in / setgroup: the statements inside

	Layer  string   // frontmatter: layer to take the whole block from
	File   string   // frontmatter: file it comes from; empty = product path
	SetKey string   // set: key to write
	SetRef Ref      // set: where the value comes from
	Names  []string // bilingual: key names
	Body   string   // patch: patch file, relative to the template
	Reason string   // replace / drop: why the upstream content is changed

	Rng Pos
}

// Template is one template: which product it weaves, from which base, and how.
type Template struct {
	Path      string
	Target    string
	Type      string
	From      string // "" = upstream is the base; "me" = the whole file is replaced with ours
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
