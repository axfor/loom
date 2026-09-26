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
	Res     string // resource section the content comes from (`Self as notes:`); "" = the plain one
	ResKind string // which document inside it (markdown / toml / ...); "" = whichever holds the name
	Within  []Seg  // section path: look under these headings (self."Parent"."Name")

	// Project: content derived from upstream's own structure rather than taken from our
	// layer — the one content source that does come from upstream, because it reads the
	// shape of it rather than copying what it says.
	Project *Select
	// ProjectView is the key whose value was projected, read as ProjectAs: base.prompt.as(markdown)
	ProjectView, ProjectAs string
	Rng                    Pos
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
	At     Pos    // where the name is written, so sync can rewrite it when upstream renames it
	Within []Seg  // section path: look under these headings (base."Parent"."Name")
	Axis   string // step from the named node to one related to it: children / next / prev / parent / first / last
	Srcs   []Ref  // what to insert

	As   string // in: the type to view the key's value as
	Key  string // in: which key
	Kids []Stmt // in: the statements inside

	Layer  string  // frontmatter: layer to take the whole block from
	File   string  // frontmatter: file it comes from; empty = product path
	SetKey string  // value: key to write
	SetRef Ref     // value: where our value comes from (a key of ours, or a literal)
	Mode   string  // value: set / start / append
	Reason string  // replace / drop: why the upstream content is changed
	Move   *Move   // move: where the node goes
	Select *Select // a group instead of one node: every match gets the same operation

	// The constructs that cannot be settled before the document is in hand.
	Assign string // the name this statement's result is caught in; empty = failure stops the build
	Cond   *Cond  // if: what to ask
	Else   []Stmt // if: the other branch
	Errf   []Ref  // return err.format: the message and what goes into it

	Rng Pos
}

// Select picks every node of a kind that the predicates allow. A selection is read-only in
// itself; what makes it a statement is the operation applied to each node it found.
type Select struct {
	Kind  string
	Pred  *Pred  // the predicate written in brackets; nil = the older named-argument form
	All   bool   // written as a bare class name: every node of this kind
	Match string // regular expression the name must match; empty = any
	Level int    // markdown heading level the node must be; 0 = any
	Empty bool   // only nodes with nothing under them
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
	Axis   string  // steps from Anchor to the node meant: children.first
	Select *Select // the target was picked from a group: base.sections[...].first
	At     Pos     // where Anchor is written, so lm sync can follow a rename of it
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
	Resources []Resource // documents written inline in the template, after the line of dashes
}

// Seg is one segment of a section path: a name, and whether it is in identifier form.
type Seg struct {
	Name  string
	Ident bool
	At    Pos // where the segment is written, so lm sync can follow a rename of it
}

// Cond is what an `if` asks. Either a variable an earlier statement's result was caught in, or a
// read-only question about the document — which is always safe, because asking writes nothing.
type Cond struct {
	Not  bool
	Var  string // `if !ok`
	Kind string // the node kind the question is about
	Has  string // `if base.has.Overview`
	// HasIdent: Has is written unquoted and read by the underscore rule (Loom 1); HasAt is where,
	// so lm sync can follow a rename of it.
	HasIdent bool
	HasAt    Pos
	Sel      *Select // `if base.sections[level == 2].any`
	Any      bool
	Rng      Pos
}

// Resource is one `Self:` section: the documents written inline in the template.
type Resource struct {
	Name string // from `Self as notes:`; empty for the plain form
	Docs []ResourceDoc
	Rng  Pos
}

// ResourceDoc is one fenced document inside a resource section. The fence's language tag is the
// kind, so nothing has to be declared twice.
type ResourceDoc struct {
	Kind string
	Text string
	Rng  Pos
}

// Pred is a predicate over a document's own structure: what a node is called, how deep it sits,
// whether it holds anything, what a shell function calls, what a section contains. Deliberately
// not an expression language — it can ask about the document and compute nothing.
type Pred struct {
	Op    string // "and" / "or" / "not"; empty for a leaf
	Kids  []Pred
	Field string // level / name / empty / calls / has
	Cmp   string // == != < <= > >= ~
	Str   string
	Num   int
}
