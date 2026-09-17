package loom

// Template syntax layer (legacy syntax): reads a .lm file (HCL) into a list of statements.
//
// Why HCL rather than a home-grown syntax: every home-grown rule has to be implemented,
// error-reported and taught from scratch. HCL is Terraform's syntax, a learning cost people
// have already paid: blocks + attributes + expressions, errors with file:line:col, editor
// highlighting for free. What is truly unique to this language is how to weave, not what
// the brackets look like — effort saved on brackets belongs to the weaving.
//
// Statement order in a template matters (which of two inserts on the same anchor comes
// first), but HCL keeps a body's attributes in a map. So each statement remembers its byte
// offset and is sorted back into template order after parsing.

import (
	"fmt"
	"sort"

	"github.com/axfor/loom/ast"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// Ref is one content source: a node in some layer, or a literal.
type Ref struct {
	Layer   string // layer name
	Kind    string // heading / line / frontmatter / key / function / marker / body / all
	Anchor  string // body / all have no anchor
	Literal string // literal content (string or heredoc)
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
	Op     string // after/before/replace/append/prepend/frontmatter/set/bilingual/in/anchor/patch/inherit/override/new
	Kind   string // position: node kind
	Anchor string // position: anchor
	Srcs   []Ref  // what to insert

	As    string // in: the type to parse the contents as
	Reuse string // in: file to take our layer's content from for this block
	Kids  []Stmt // in: statements inside the block

	Layer  string   // frontmatter: layer to take the whole block from
	SetKey string   // set: key to write
	SetRef Ref      // set: where the value comes from
	Key    string   // bilingual: which key
	Name   string   // anchor: anchor name
	Body   string   // patch: unified diff
	Names  []string // inherit/override/new: function names

	Reason string // replace / drop: why the upstream content is changed
	Ident  bool   // anchor is in identifier form: _ matches a space or an underscore
	Within []Seg  // section path: look under these headings (base."Parent"."Name")
	File   string // frontmatter: file it comes from; empty = product path

	Byte int // byte offset in the template — used to restore statement order
	Rng  Pos
}

// Template is one template: what to weave, by which structure, on which base layer, and how.
type Template struct {
	Path      string
	Target    string
	Type      string
	From      string // upstream (warp) layer name
	BasePath  string // path in the upstream (for renamed assets; default = Target)
	Source    string // file to take our layer's content from instead (default = Target)
	UseReason string // use me: reason for dropping the whole upstream file
	Covers    []string
	Stmts     []Stmt
	Anchors   map[string]Stmt
}

var weaveSchema = &hcl.BodySchema{
	Blocks: []hcl.BlockHeaderSchema{{Type: "weave", LabelNames: []string{"target"}}},
}

var bodyAttrs = []hcl.AttributeSchema{
	{Name: "type"}, {Name: "from"}, {Name: "path"}, {Name: "covers"},
	{Name: "frontmatter"}, {Name: "bilingual"}, {Name: "set"},
	{Name: "inherit"}, {Name: "override"}, {Name: "new"},
	{Name: "patch"}, {Name: "append"}, {Name: "prepend"},
	{Name: "as"}, {Name: "reuse"}, {Name: "insert"}, {Name: "with"},
}

var bodyBlocks = []hcl.BlockHeaderSchema{
	{Type: "after", LabelNames: []string{"kind", "anchor"}},
	{Type: "before", LabelNames: []string{"kind", "anchor"}},
	{Type: "replace", LabelNames: []string{"kind", "anchor"}},
	{Type: "in", LabelNames: []string{"key"}},
	{Type: "anchor", LabelNames: []string{"name"}},
}

var stmtSchema = &hcl.BodySchema{Attributes: bodyAttrs, Blocks: bodyBlocks}

// ParseTemplate reads a .lm file.
func ParseTemplate(path string, src []byte) (*Template, error) {
	f, diags := hclsyntax.ParseConfig(src, path, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, diags
	}
	top, diags := f.Body.Content(weaveSchema)
	if diags.HasErrors() {
		return nil, diags
	}
	if len(top.Blocks) != 1 {
		return nil, fmt.Errorf("%s: a template must have exactly one weave block, got %d", path, len(top.Blocks))
	}
	b := top.Blocks[0]
	t := &Template{Path: path, Target: b.Labels[0], Anchors: map[string]Stmt{}}
	stmts, hdr, err := parseBody(b.Body, true)
	if err != nil {
		return nil, err
	}
	t.Type = hdr.typ
	t.From = hdr.from
	t.BasePath = hdr.path
	t.Covers = hdr.covers
	t.Stmts = stmts
	if t.Type == "" {
		return nil, fmt.Errorf("%s: weave block is missing `type` (one of %v)", path, ast.Types)
	}
	if ast.Kinds(t.Type) == nil {
		return nil, fmt.Errorf("%s: unknown resource type `%s` (one of %v)", path, t.Type, ast.Types)
	}
	if t.BasePath == "" {
		t.BasePath = t.Target
	}
	for _, s := range stmts {
		if s.Op != "anchor" {
			continue
		}
		if _, dup := t.Anchors[s.Name]; dup {
			return nil, fmt.Errorf("%s: anchor `%s` defined twice", s.Rng, s.Name)
		}
		t.Anchors[s.Name] = s
	}
	return t, nil
}

type header struct {
	typ, from, path string
	covers          []string
}

func parseBody(body hcl.Body, top bool) ([]Stmt, header, error) {
	var hdr header
	content, diags := body.Content(stmtSchema)
	if diags.HasErrors() {
		return nil, hdr, diags
	}
	var out []Stmt
	for name, a := range content.Attributes {
		switch name {
		case "type", "from", "path", "as", "reuse":
			if !top && (name == "type" || name == "from" || name == "path") {
				return nil, hdr, fmt.Errorf("%s: `%s` is only allowed on the weave block", a.Range, name)
			}
			v, err := strAttr(a)
			if err != nil {
				return nil, hdr, err
			}
			switch name {
			case "type":
				hdr.typ = v
			case "from":
				hdr.from = v
			case "path":
				hdr.path = v
			}
			continue // as / reuse are read by the parent block
		case "covers":
			v, err := strListAttr(a)
			if err != nil {
				return nil, hdr, err
			}
			hdr.covers = v
			continue
		}
		s, err := attrStmt(name, a)
		if err != nil {
			return nil, hdr, err
		}
		out = append(out, s)
	}
	for _, blk := range content.Blocks {
		s, err := blockStmt(blk)
		if err != nil {
			return nil, hdr, err
		}
		out = append(out, s)
	}
	// Restore template order — HCL attributes are a map, but order carries meaning
	sort.SliceStable(out, func(i, j int) bool { return out[i].Byte < out[j].Byte })
	return out, hdr, nil
}

func attrStmt(name string, a *hcl.Attribute) (Stmt, error) {
	s := Stmt{Op: name, Byte: a.Range.Start.Byte, Rng: posOf(a.Range)}
	switch name {
	case "append", "prepend":
		refs, err := parseRefs(a.Expr)
		if err != nil {
			return s, err
		}
		s.Srcs = refs
	case "frontmatter":
		v, err := layerName(a.Expr)
		if err != nil {
			return s, err
		}
		s.Layer = v
	case "bilingual":
		keys, err := strListAttr(a)
		if err != nil {
			return s, err
		}
		if len(keys) == 0 {
			return s, fmt.Errorf("%s: bilingual needs at least one key", a.Range)
		}
		s.Names = keys
	case "inherit", "override", "new":
		names, err := strListAttr(a)
		if err != nil {
			return s, err
		}
		s.Names = names
	case "patch":
		// Why the patch is not inline in the template: HCL heredocs interpolate ${...}, and
		// diffs naturally contain ${VAR}. Inlining means escaping, and once escaped the text in
		// the template is no longer a diff: editors don't recognize it, patch(1) rejects it,
		// and copying it out to use directly fails. In a separate .diff file it stays a diff.
		v, err := strAttr(a)
		if err != nil {
			return s, err
		}
		s.Body = v
	case "set":
		obj, ok := a.Expr.(*hclsyntax.ObjectConsExpr)
		if !ok {
			return s, fmt.Errorf("%s: set must be written as `set = { <key> = <layer>.<key> }`", a.Range)
		}
		var kids []Stmt
		for _, it := range obj.Items {
			k, err := objKey(it.KeyExpr)
			if err != nil {
				return s, err
			}
			r, err := parseRef(it.ValueExpr)
			if err != nil {
				return s, err
			}
			kids = append(kids, Stmt{Op: "set", SetKey: k, SetRef: r,
				Byte: it.KeyExpr.Range().Start.Byte, Rng: posOf(it.KeyExpr.Range())})
		}
		s.Op = "setgroup"
		s.Kids = kids
	case "insert", "with":
		return s, fmt.Errorf("%s: `%s` is only allowed inside after / before / replace blocks", a.Range, name)
	default:
		return s, fmt.Errorf("%s: unknown attribute `%s`", a.Range, name)
	}
	return s, nil
}

func blockStmt(b *hcl.Block) (Stmt, error) {
	s := Stmt{Op: b.Type, Byte: b.DefRange.Start.Byte, Rng: posOf(b.DefRange)}
	switch b.Type {
	case "after", "before", "replace":
		// A position block accepts exactly one insert / with — other statements belong outside;
		// nesting them in a position block only turns "what does this apply to" into a guess.
		s.Kind, s.Anchor = b.Labels[0], b.Labels[1]
		want := "insert"
		if b.Type == "replace" {
			want = "with"
		}
		content, diags := b.Body.Content(&hcl.BodySchema{
			Attributes: []hcl.AttributeSchema{{Name: want, Required: true}},
		})
		if diags.HasErrors() {
			return s, diags
		}
		refs, err := parseRefs(content.Attributes[want].Expr)
		if err != nil {
			return s, err
		}
		s.Srcs = refs
	case "in":
		s.Key = b.Labels[0]
		content, diags := b.Body.Content(stmtSchema)
		if diags.HasErrors() {
			return s, diags
		}
		var err error
		if a, ok := content.Attributes["as"]; ok {
			if s.As, err = strAttr(a); err != nil {
				return s, err
			}
		}
		if a, ok := content.Attributes["reuse"]; ok {
			if s.Reuse, err = strAttr(a); err != nil {
				return s, err
			}
		}
		if s.As == "" {
			return s, fmt.Errorf("%s: in block needs `as = \"<type>\"`", b.DefRange)
		}
		if ast.Kinds(s.As) == nil {
			return s, fmt.Errorf("%s: unknown nested type `%s`", b.DefRange, s.As)
		}
		kids, _, err := parseBody(b.Body, false)
		if err != nil {
			return s, err
		}
		s.Kids = kids
	case "anchor":
		s.Name = b.Labels[0]
		content, diags := b.Body.Content(stmtSchema)
		if diags.HasErrors() {
			return s, diags
		}
		if len(content.Attributes) != 1 {
			return s, fmt.Errorf("%s: anchor block must contain exactly one `<node kind> = \"<locator>\"`", b.DefRange)
		}
		for k, a := range content.Attributes {
			v, err := strAttr(a)
			if err != nil {
				return s, err
			}
			s.Kind, s.Anchor = k, v
		}
	}
	return s, nil
}

// parseRefs accepts a single reference or a list of them.
func parseRefs(e hcl.Expression) ([]Ref, error) {
	if tup, ok := e.(*hclsyntax.TupleConsExpr); ok {
		var out []Ref
		for _, it := range tup.Exprs {
			r, err := parseRef(it)
			if err != nil {
				return nil, err
			}
			out = append(out, r)
		}
		return out, nil
	}
	r, err := parseRef(e)
	if err != nil {
		return nil, err
	}
	return []Ref{r}, nil
}

// parseRef accepts three forms:
//
//	xsdd.body                  the whole body
//	xsdd.heading["X"]          one section
//	"literal" / <<EOT ... EOT  content written directly in the template
func parseRef(e hcl.Expression) (Ref, error) {
	rng := e.Range()
	if tr, ok := e.(*hclsyntax.ScopeTraversalExpr); ok {
		t := tr.Traversal
		if len(t) < 2 || len(t) > 3 {
			return Ref{}, fmt.Errorf("%s: a reference must be `<layer>.<node kind>` or `<layer>.<node kind>[\"<anchor>\"]`", rng)
		}
		r := Ref{Layer: t.RootName(), Rng: posOf(rng)}
		attr, ok := t[1].(hcl.TraverseAttr)
		if !ok {
			return Ref{}, fmt.Errorf("%s: `%s` must be followed by a node kind", rng, r.Layer)
		}
		r.Kind = attr.Name
		if len(t) == 3 {
			idx, ok := t[2].(hcl.TraverseIndex)
			if !ok {
				return Ref{}, fmt.Errorf("%s: an anchor must be written as [\"...\"]", rng)
			}
			if idx.Key.Type() != cty.String {
				return Ref{}, fmt.Errorf("%s: an anchor must be a string", rng)
			}
			r.Anchor = idx.Key.AsString()
		}
		return r, nil
	}
	v, diags := e.Value(nil)
	if diags.HasErrors() {
		return Ref{}, diags
	}
	if v.Type() != cty.String {
		return Ref{}, fmt.Errorf("%s: expected a reference or a literal", rng)
	}
	return Ref{IsLit: true, Literal: v.AsString(), Rng: posOf(rng)}, nil
}

func layerName(e hcl.Expression) (string, error) {
	if tr, ok := e.(*hclsyntax.ScopeTraversalExpr); ok && len(tr.Traversal) == 1 {
		return tr.Traversal.RootName(), nil
	}
	v, diags := e.Value(nil)
	if diags.HasErrors() {
		return "", diags
	}
	if v.Type() != cty.String {
		return "", fmt.Errorf("%s: expected a layer name", e.Range())
	}
	return v.AsString(), nil
}

func objKey(e hcl.Expression) (string, error) {
	if k, ok := e.(*hclsyntax.ObjectConsKeyExpr); ok {
		if tr, ok := k.Wrapped.(*hclsyntax.ScopeTraversalExpr); ok && len(tr.Traversal) == 1 {
			return tr.Traversal.RootName(), nil
		}
	}
	v, diags := e.Value(nil)
	if diags.HasErrors() {
		return "", diags
	}
	if v.Type() != cty.String {
		return "", fmt.Errorf("%s: a key must be an identifier or a string", e.Range())
	}
	return v.AsString(), nil
}

func strListAttr(a *hcl.Attribute) ([]string, error) {
	v, diags := a.Expr.Value(nil)
	if diags.HasErrors() {
		return nil, diags
	}
	if v.Type() == cty.String {
		return []string{v.AsString()}, nil
	}
	if !v.Type().IsTupleType() && !v.Type().IsListType() {
		return nil, fmt.Errorf("%s: expected a string or a list of strings", a.Range)
	}
	var out []string
	for it := v.ElementIterator(); it.Next(); {
		_, ev := it.Element()
		if ev.Type() != cty.String {
			return nil, fmt.Errorf("%s: every list element must be a string", a.Range)
		}
		out = append(out, ev.AsString())
	}
	return out, nil
}

// Seg is one segment of a section path: a name, and whether it is in identifier form.
type Seg struct {
	Name  string
	Ident bool
}
