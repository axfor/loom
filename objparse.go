package loom

// Templates: object syntax.
//
//	import cmd "/.claude/commands/ship"
//	base.Install.after("Install XSDD")
//	base."How it compares".drop(reason: "Not applicable to XSDD")
//	base.prompt.as(markdown).append(cmd.body)
//
// Files are objects (base / self / imported names), nodes are names on an object, and
// operations are methods on a node. Only base can be changed: a template produces exactly
// one file, and that file is base after weaving.
//
// Same result as the legacy syntax: the Template read here is the same intermediate result
// as the same template read in the legacy syntax, so the engine (weave.go) need not know
// which syntax a template uses — the proof that migration leaves products byte-for-byte
// unchanged rests on this.

import (
	"fmt"
	"path"
	"strings"
)

// defaultKind is the node kind a bare name refers to (in engine terms).
var defaultKind = map[string]string{
	"markdown": "heading",
	"shell":    "function",
	"toml":     "key",
	"json":     "path",
	"text":     "line",
}

// kindCalls lists, per type, the kinds accepted in the kind("name") form (a json key is a dotted path).
var kindCalls = map[string]map[string]string{
	"markdown": {"section": "heading", "line": "line"},
	"shell":    {"function": "function", "marker": "marker", "line": "line"},
	"toml":     {"key": "key"},
	"json":     {"key": "path"},
	"text":     {"line": "line"},
}

// editMethods are the methods that change base.
var editMethods = []string{"after", "before", "start", "append", "replace", "drop", "set", "join", "patch"}

func typeWords() []string { return []string{"markdown", "toml", "json", "shell", "text"} }

// TypeOf infers the type from a file path. Anything it cannot infer is plain text.
func TypeOf(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".md", ".markdown":
		return "markdown"
	case ".toml":
		return "toml"
	case ".json":
		return "json"
	case ".sh", ".bash":
		return "shell"
	}
	return "text"
}

// ── Syntax tree ─────────────────────────────────────────────────────────────

type vkind int

const (
	vString vkind = iota
	vRaw
	vExpr
)

type oValue struct {
	kind vkind
	str  string
	expr *oExpr
	pos  Pos
}

type oArg struct {
	name string // named argument (reason); empty = positional
	val  oValue
	pos  Pos
}

type oStep struct {
	name  string
	str   bool // name written as a string: ."How it compares"
	call  bool // has arguments: .after(...) / .after { ... }
	block bool
	args  []oArg
	pos   Pos
}

type oExpr struct {
	root  string
	pos   Pos
	steps []oStep
}

type oImport struct {
	name  string
	named bool
	spec  string
	pos   Pos
}

type oparser struct {
	toks []tok
	i    int
}

func (p *oparser) peek() tok { return p.toks[p.i] }

func (p *oparser) next() tok {
	t := p.toks[p.i]
	if t.kind != kEOF {
		p.i++
	}
	return t
}

func (p *oparser) skipNewlines() {
	for p.peek().kind == kNewline {
		p.next()
	}
}

func (p *oparser) endOfStatement() error {
	t := p.peek()
	if t.kind == kNewline || t.kind == kEOF {
		p.next()
		return nil
	}
	return fmt.Errorf("%s: statement should end here, got %s — one statement per line", t.pos, t)
}

// parseObjects reads the imports and statements.
func parseObjects(file string, src []byte) ([]oImport, []*oExpr, error) {
	toks, err := lexLoom(file, src)
	if err != nil {
		return nil, nil, err
	}
	p := &oparser{toks: toks}
	var imports []oImport
	var stmts []*oExpr
	for {
		p.skipNewlines()
		t := p.peek()
		if t.kind == kEOF {
			return imports, stmts, nil
		}
		if t.kind == kIdent && t.text == "import" {
			p.next()
			im := oImport{pos: t.pos}
			if n := p.peek(); n.kind == kIdent {
				p.next()
				im.name, im.named = n.text, true
			}
			s := p.next()
			if s.kind != kString {
				return nil, nil, fmt.Errorf("%s: import takes a quoted path: import cmd \"/.claude/commands/ship\"", s.pos)
			}
			im.spec = s.text
			if err := p.endOfStatement(); err != nil {
				return nil, nil, err
			}
			imports = append(imports, im)
			continue
		}
		e, err := p.expr()
		if err != nil {
			return nil, nil, err
		}
		if err := p.endOfStatement(); err != nil {
			return nil, nil, err
		}
		stmts = append(stmts, e)
	}
}

func (p *oparser) expr() (*oExpr, error) {
	t := p.next()
	if t.kind != kIdent {
		return nil, fmt.Errorf("%s: expected an object (base / self / an imported name), got %s", t.pos, t)
	}
	e := &oExpr{root: t.text, pos: t.pos}
	for p.peek().kind == kDot {
		p.next()
		n := p.next()
		switch n.kind {
		case kString:
			e.steps = append(e.steps, oStep{name: n.text, str: true, pos: n.pos})
		case kIdent:
			st := oStep{name: n.text, pos: n.pos}
			switch p.peek().kind {
			case kLParen:
				p.next()
				args, err := p.parenArgs()
				if err != nil {
					return nil, err
				}
				st.call, st.args = true, args
			case kLBrace:
				b := p.next()
				args, err := p.blockArgs(b.pos)
				if err != nil {
					return nil, err
				}
				st.call, st.block, st.args = true, true, args
			}
			e.steps = append(e.steps, st)
			if st.block {
				return e, nil // a block-form method is always the last step of the statement
			}
		default:
			return nil, fmt.Errorf("%s: expected a node or method name after `.`, got %s", n.pos, n)
		}
	}
	return e, nil
}

func (p *oparser) parenArgs() ([]oArg, error) {
	var out []oArg
	for {
		p.skipNewlines()
		if p.peek().kind == kRParen {
			p.next()
			return out, nil
		}
		a, err := p.arg()
		if err != nil {
			return nil, err
		}
		out = append(out, a)
		p.skipNewlines()
		switch t := p.next(); t.kind {
		case kComma:
		case kRParen:
			return out, nil
		default:
			return nil, fmt.Errorf("%s: separate arguments with commas and close with `)`, got %s", t.pos, t)
		}
	}
}

func (p *oparser) blockArgs(open Pos) ([]oArg, error) {
	if t := p.peek(); t.kind != kNewline {
		return nil, fmt.Errorf("%s: expected a newline after `{` — the block form takes one argument per line", t.pos)
	}
	var out []oArg
	for {
		p.skipNewlines()
		t := p.peek()
		switch t.kind {
		case kRBrace:
			p.next()
			if len(out) == 0 {
				return nil, fmt.Errorf("%s: empty block — { } with nothing inside", open)
			}
			return out, nil
		case kEOF:
			return nil, fmt.Errorf("%s: block has no closing `}`", open)
		}
		a, err := p.arg()
		if err != nil {
			return nil, err
		}
		out = append(out, a)
		switch n := p.peek(); n.kind {
		case kNewline:
			p.next()
		case kRBrace:
		case kComma:
			return nil, fmt.Errorf("%s: the block form takes one argument per line, without commas", n.pos)
		default:
			return nil, fmt.Errorf("%s: the block form takes one argument per line; unexpected %s", n.pos, n)
		}
	}
}

func (p *oparser) arg() (oArg, error) {
	t := p.peek()
	if t.kind == kIdent && p.toks[p.i+1].kind == kColon {
		p.next()
		p.next()
		v, err := p.value()
		return oArg{name: t.text, val: v, pos: t.pos}, err
	}
	v, err := p.value()
	return oArg{val: v, pos: t.pos}, err
}

func (p *oparser) value() (oValue, error) {
	t := p.peek()
	switch t.kind {
	case kString:
		p.next()
		return oValue{kind: vString, str: t.text, pos: t.pos}, nil
	case kRaw:
		p.next()
		return oValue{kind: vRaw, str: t.text, pos: t.pos}, nil
	case kIdent:
		e, err := p.expr()
		if err != nil {
			return oValue{}, err
		}
		return oValue{kind: vExpr, expr: e, pos: t.pos}, nil
	}
	return oValue{}, fmt.Errorf("%s: expected an argument: \"name\", `literal`, or something like self.xxx, got %s", t.pos, t)
}

// ── Interpretation: syntax tree → engine statements ───────────────────────────

// Resolver resolves an import path to a real file in the layer (a path relative to the
// layer root). The extension may be omitted.
type Resolver func(layer, spec, fromDir string) (string, error)

type object struct {
	layer string // "up" / "me"
	file  string // path in the layer; empty for self = product path
	typ   string
}

type interp struct {
	t       *Template
	objects map[string]object
}

// ParseTemplateSyntax reads a template in object syntax. target is the product path
// (derived from the template path). When resolve is nil, import paths are used as layer
// paths unchanged (for tests).
func ParseTemplateSyntax(file, target string, src []byte, resolve Resolver) (*Template, error) {
	imports, stmts, err := parseObjects(file, src)
	if err != nil {
		return nil, err
	}
	t := &Template{Path: file, Target: target, Type: TypeOf(target), BasePath: target, Anchors: map[string]Stmt{}}
	in := &interp{t: t, objects: map[string]object{
		"base": {layer: "up", file: target, typ: TypeOf(target)},
		"self": {layer: "me", typ: TypeOf(target)},
	}}
	seen := map[string]Pos{}
	for _, im := range imports {
		name := im.name
		if !im.named {
			base := path.Base(im.spec)
			name = strings.TrimSuffix(base, path.Ext(base))
			if !isIdent(name) {
				return nil, fmt.Errorf("%s: name `%s` taken from the path is not a valid object name — give it one: import name %q", im.pos, name, im.spec)
			}
		}
		if name == "self" {
			return nil, fmt.Errorf("%s: self is this file and cannot be redirected", im.pos)
		}
		if first, dup := seen[name]; dup {
			return nil, fmt.Errorf("%s: `%s` imported twice (first at %s)", im.pos, name, first)
		}
		seen[name] = im.pos
		layer := "me"
		if name == "base" {
			layer = "up"
		}
		rel := im.spec
		if resolve != nil {
			if rel, err = resolve(layer, im.spec, path.Dir(target)); err != nil {
				return nil, fmt.Errorf("%s: %v", im.pos, err)
			}
		}
		in.objects[name] = object{layer: layer, file: rel, typ: TypeOf(rel)}
		if name == "base" {
			t.BasePath = rel
		}
	}
	for _, e := range stmts {
		if err := in.statement(e); err != nil {
			return nil, err
		}
	}
	if err := checkPatchAlone(t); err != nil {
		return nil, err
	}
	if t.From == "me" {
		// Whole file replaced with ours: the product is our file, so other weave statements have nothing to weave into
		for _, s := range t.Stmts {
			if s.Op != "drop" {
				return nil, fmt.Errorf("%s: this template replaces the whole file with ours, so this statement has no effect — a whole-file replace only goes with drop (to account for where upstream sections went)", s.Rng)
			}
		}
	}
	return t, nil
}

func isIdent(s string) bool {
	if s == "" {
		return false
	}
	toks, err := lexLoom("", []byte(s))
	return err == nil && len(toks) == 3 && toks[0].kind == kIdent && toks[0].text == s
}

// receiver is the thing a statement changes.
type receiver struct {
	node   bool   // false = the whole file (or a view)
	kind   string // node kind (in engine terms)
	anchor string
	ident  bool // name is in identifier form; _ matches a space or an underscore
	within []Seg
	typ    string
	viewOf string // non-empty = inside an as(...) view: the key name
	viewAs string
	pos    Pos
}

func (in *interp) statement(e *oExpr) error {
	obj, ok := in.objects[e.root]
	if !ok {
		return in.unknownObject(e.root, e.pos)
	}
	if e.root != "base" {
		return fmt.Errorf("%s: %s is only a content source and cannot be changed — only base can", e.pos, e.root)
	}
	if len(e.steps) == 0 || !e.steps[len(e.steps)-1].call {
		return fmt.Errorf("%s: this statement does nothing — end it with a method call such as .after(...)", e.pos)
	}
	r := receiver{typ: obj.typ, pos: e.pos}
	for _, st := range e.steps[:len(e.steps)-1] {
		if err := in.select_(&r, st); err != nil {
			return err
		}
	}
	return in.method(r, e.steps[len(e.steps)-1])
}

func (in *interp) unknownObject(name string, at Pos) error {
	var have []string
	for n := range in.objects {
		have = append(have, n)
	}
	if near := nearest(name, have); near != "" {
		return fmt.Errorf("%s: unknown object `%s` — did you mean %s?", at, name, near)
	}
	return fmt.Errorf("%s: unknown object `%s` — import it first, or use base / self", at, name)
}

// select_ takes one node-selection step: ."name" / .name / .kind("name") / .frontmatter / .as(type).
func (in *interp) select_(r *receiver, st oStep) error {
	if r.node && st.call && st.name == "as" {
		if len(st.args) != 1 || st.args[0].val.kind != vExpr || len(st.args[0].val.expr.steps) != 0 {
			return fmt.Errorf("%s: as takes a type: as(markdown)", st.pos)
		}
		typ := st.args[0].val.expr.root
		if _, ok := defaultKind[typ]; !ok {
			return unknownIn(typ, typeWords(), "type", st.args[0].pos)
		}
		if r.viewOf != "" || r.kind != defaultKind[r.typ] || (r.typ != "toml" && r.typ != "json") {
			return fmt.Errorf("%s: as only applies to a toml / json key: base.prompt.as(markdown)", st.pos)
		}
		*r = receiver{typ: typ, viewOf: r.anchor, viewAs: typ, pos: r.pos}
		return nil
	}
	if r.node {
		// Section path: base."Example 2"."Phase 1" — look for the child within the parent's whole section
		if st.call || r.typ != "markdown" || r.kind != "heading" || (!st.str && (st.name == "frontmatter" || st.name == "body")) {
			return fmt.Errorf("%s: below a node you can only select one more markdown section by name: base.\"Parent\".\"Child\"", st.pos)
		}
		r.within = append(r.within, Seg{r.anchor, r.ident})
		r.anchor, r.ident, r.pos = st.name, !st.str, st.pos
		return nil
	}
	switch {
	case st.call:
		k, ok := kindCalls[r.typ][st.name]
		if !ok {
			if contains(editMethods, st.name) {
				return fmt.Errorf("%s: %s must come last in the statement", st.pos, st.name)
			}
			return fmt.Errorf("%s: %s has no `%s(...)` node", st.pos, r.typ, st.name)
		}
		if len(st.args) != 1 || st.args[0].val.kind != vString {
			return fmt.Errorf("%s: %s takes a quoted name: %s(\"...\")", st.pos, st.name, st.name)
		}
		r.node, r.kind, r.anchor = true, k, st.args[0].val.str
	case !st.str && st.name == "frontmatter" && r.typ == "markdown":
		r.node, r.kind, r.anchor = true, "frontmatter", ""
	default:
		r.node, r.kind, r.anchor, r.ident = true, defaultKind[r.typ], st.name, !st.str
	}
	r.pos = st.pos
	return nil
}

func (in *interp) method(r receiver, st oStep) error {
	if !contains(editMethods, st.name) {
		if near := nearest(st.name, editMethods); near != "" {
			return fmt.Errorf("%s: no method `%s` — did you mean %s?", st.pos, st.name, near)
		}
		return fmt.Errorf("%s: no method `%s` (available: %s)", st.pos, st.name, strings.Join(editMethods, " / "))
	}
	var pos []oArg
	reason := ""
	for _, a := range st.args {
		if a.name == "" {
			pos = append(pos, a)
			continue
		}
		if a.name != "reason" {
			return fmt.Errorf("%s: unknown named argument `%s:` (only reason: is allowed)", a.pos, a.name)
		}
		if st.name != "replace" && st.name != "drop" {
			return fmt.Errorf("%s: reason: is only for replace / drop — %s does not change upstream content and needs no reason", a.pos, st.name)
		}
		if a.val.kind != vString || strings.TrimSpace(a.val.str) == "" {
			return fmt.Errorf("%s: reason: takes a quoted reason", a.pos)
		}
		reason = a.val.str
	}
	at := st.pos
	var out []Stmt
	switch st.name {
	case "after", "before":
		if !r.node {
			return fmt.Errorf("%s: %s applies to a node: base.Install.%s(...)", at, st.name, st.name)
		}
		srcs, err := in.contents(pos, r.typ, r.viewOf != "")
		if err != nil {
			return err
		}
		if len(srcs) == 0 {
			return fmt.Errorf("%s: %s needs something to insert: %s(\"Install XSDD\")", at, st.name, st.name)
		}
		out = append(out, Stmt{Op: st.name, Kind: r.kind, Anchor: r.anchor, Ident: r.ident, Within: r.within, Srcs: srcs, Rng: at})
	case "start", "append":
		if r.node {
			return fmt.Errorf("%s: %s applies to the whole file: base.%s(...)", at, st.name, st.name)
		}
		srcs, err := in.contents(pos, r.typ, r.viewOf != "")
		if err != nil {
			return err
		}
		if len(srcs) == 0 {
			return fmt.Errorf("%s: %s needs something to insert", at, st.name)
		}
		op := "append"
		if st.name == "start" {
			op = "prepend"
		}
		out = append(out, Stmt{Op: op, Srcs: srcs, Rng: at})
	case "replace":
		if reason == "" {
			return fmt.Errorf("%s: replace changes upstream content and needs a reason: reason: \"...\"", at)
		}
		if len(pos) != 1 {
			return fmt.Errorf("%s: replace takes exactly one replacement", at)
		}
		if !r.node && r.viewOf == "" {
			// Whole file: base.replace(self, reason: ...)
			v := pos[0].val
			if v.kind != vExpr || len(v.expr.steps) != 0 {
				return fmt.Errorf("%s: a whole-file replace takes a file object: base.replace(self, reason: \"...\")", pos[0].pos)
			}
			obj, ok := in.objects[v.expr.root]
			if !ok {
				return in.unknownObject(v.expr.root, v.pos)
			}
			if obj.layer != "me" {
				return fmt.Errorf("%s: a whole-file replace must use a file from our layer (self or an imported one)", v.pos)
			}
			in.t.From, in.t.UseReason, in.t.Source = "me", reason, obj.file
			return nil
		}
		if !r.node {
			return fmt.Errorf("%s: replace applies to a node", at)
		}
		srcs, err := in.contents(pos, r.typ, r.viewOf != "")
		if err != nil {
			return err
		}
		out = append(out, Stmt{Op: "replace", Kind: r.kind, Anchor: r.anchor, Ident: r.ident, Within: r.within, Srcs: srcs, Reason: reason, Rng: at})
	case "drop":
		if reason == "" {
			return fmt.Errorf("%s: drop changes upstream content and needs a reason: drop(reason: \"...\")", at)
		}
		if !r.node || len(pos) != 0 {
			return fmt.Errorf("%s: drop applies to a node and takes only a reason: base.X.drop(reason: \"...\")", at)
		}
		out = append(out, Stmt{Op: "drop", Kind: r.kind, Anchor: r.anchor, Ident: r.ident, Within: r.within, Reason: reason, Rng: at})
	case "set":
		if !r.node || len(pos) != 1 {
			return fmt.Errorf("%s: set applies to a node and takes one value: base.frontmatter.set(self.frontmatter)", at)
		}
		src, err := in.contents(pos, r.typ, false)
		if err != nil {
			return err
		}
		ref := src[0]
		if r.kind == "frontmatter" {
			if ref.Kind != "frontmatter" {
				return fmt.Errorf("%s: frontmatter can only be set to another frontmatter: set(self.frontmatter)", pos[0].pos)
			}
			out = append(out, Stmt{Op: "frontmatter", Layer: "me", File: ref.File, Rng: at})
			break
		}
		if r.typ != "toml" && r.typ != "json" {
			return fmt.Errorf("%s: set applies to frontmatter or a toml / json key", at)
		}
		if ref.IsLit || ref.Kind == "body" || ref.Kind == "all" {
			return fmt.Errorf("%s: set takes its value from a key in another file: set(self.description)", pos[0].pos)
		}
		out = append(out, Stmt{Op: "setgroup", Rng: at, Kids: []Stmt{{Op: "set", SetKey: r.anchor,
			SetRef: Ref{Layer: "me", Kind: ref.Anchor, File: ref.File, Rng: pos[0].pos}, Rng: at}}})
	case "join":
		if r.typ == "markdown" && r.kind != "frontmatter" {
			return fmt.Errorf("%s: in markdown, join applies to frontmatter keys: base.frontmatter.join(\"description\")", at)
		}
		if r.typ != "markdown" && r.node {
			return fmt.Errorf("%s: join applies to the whole file: base.join(\"description\")", at)
		}
		var keys []string
		for _, a := range pos {
			if a.val.kind != vString {
				return fmt.Errorf("%s: join takes quoted key names", a.pos)
			}
			keys = append(keys, a.val.str)
		}
		if len(keys) == 0 {
			return fmt.Errorf("%s: join needs key names: join(\"description\")", at)
		}
		out = append(out, Stmt{Op: "bilingual", Names: keys, Rng: at})
	case "patch":
		if r.node || r.viewOf != "" {
			return fmt.Errorf("%s: patch applies to the whole file: base.patch(\"x.diff\")", at)
		}
		for _, a := range pos {
			if a.val.kind != vString {
				return fmt.Errorf("%s: patch file names must be quoted", a.pos)
			}
			out = append(out, Stmt{Op: "patch", Body: a.val.str, Rng: at})
		}
		if len(out) == 0 {
			return fmt.Errorf("%s: patch needs a patch file: patch(\"x.diff\")", at)
		}
	}
	if r.viewOf != "" {
		// Statements in a view go into an in statement; adjacent ones on the same key and type
		// merge into one (matching a single in block in the legacy syntax)
		if n := len(in.t.Stmts); n > 0 {
			last := &in.t.Stmts[n-1]
			if last.Op == "in" && last.Key == r.viewOf && last.As == r.viewAs {
				last.Kids = append(last.Kids, out...)
				return nil
			}
		}
		in.t.Stmts = append(in.t.Stmts, Stmt{Op: "in", Key: r.viewOf, As: r.viewAs, Kids: out, Rng: r.pos})
		return nil
	}
	in.t.Stmts = append(in.t.Stmts, out...)
	return nil
}

// contents interprets "which of our content to insert". typ is the type of the position it
// goes into; with inView, a bare name refers to content inside the view.
func (in *interp) contents(args []oArg, typ string, inView bool) ([]Ref, error) {
	var out []Ref
	for _, a := range args {
		v := a.val
		switch v.kind {
		case vString:
			out = append(out, Ref{Layer: "me", Kind: defaultKind[typ], Anchor: v.str, Rng: v.pos})
		case vRaw:
			out = append(out, Ref{Layer: "me", IsLit: true, Literal: v.str, Rng: v.pos})
		case vExpr:
			e := v.expr
			obj, ok := in.objects[e.root]
			if !ok {
				if len(e.steps) == 0 && (e.root == "body" || e.root == "frontmatter") {
					return nil, fmt.Errorf("%s: write self.%s", v.pos, e.root)
				}
				return nil, in.unknownObject(e.root, v.pos)
			}
			if obj.layer != "me" {
				return nil, fmt.Errorf("%s: inserted content must come from our layer (self or an imported file); %s is upstream", v.pos, e.root)
			}
			ref := Ref{Layer: "me", File: obj.file, Rng: v.pos}
			switch {
			case len(e.steps) == 0:
				ref.Kind = "all"
			case len(e.steps) > 1:
				// Section path: self."Example 2"."Phase 1"
				kt := obj.typ
				if inView && e.root == "self" {
					kt = typ
				}
				for _, st := range e.steps {
					if kt != "markdown" || st.call || (!st.str && (st.name == "body" || st.name == "frontmatter")) {
						return nil, fmt.Errorf("%s: content selects a single node, or markdown sections by path: %s.\"Parent\".\"Child\"", v.pos, e.root)
					}
				}
				for _, st := range e.steps[:len(e.steps)-1] {
					ref.Within = append(ref.Within, Seg{st.name, !st.str})
				}
				last := e.steps[len(e.steps)-1]
				ref.Kind, ref.Anchor, ref.Ident = "heading", last.name, !last.str
			default:
				st := e.steps[0]
				switch {
				case st.call:
					k, ok := kindCalls[obj.typ][st.name]
					if !ok || len(st.args) != 1 || st.args[0].val.kind != vString {
						return nil, fmt.Errorf("%s: %s has no `%s(...)` node", st.pos, obj.typ, st.name)
					}
					ref.Kind, ref.Anchor = k, st.args[0].val.str
				case !st.str && st.name == "body":
					ref.Kind = "body"
				case !st.str && st.name == "frontmatter":
					ref.Kind = "frontmatter"
				default:
					kt := obj.typ
					if inView && e.root == "self" {
						kt = typ
					}
					ref.Kind, ref.Anchor, ref.Ident = defaultKind[kt], st.name, !st.str
				}
			}
			out = append(out, ref)
		}
	}
	return out, nil
}

// checkPatchAlone rejects weave statements alongside a patch: once a template applies a
// patch, the patch result is the product, so other weave statements would have no effect —
// and letting them silently do nothing is far worse than an error. drop is the exception:
// it accounts for where an upstream section went and needs no weaving.
func checkPatchAlone(t *Template) error {
	var patch *Stmt
	for k := range t.Stmts {
		if t.Stmts[k].Op == "patch" {
			patch = &t.Stmts[k]
			break
		}
	}
	if patch == nil {
		return nil
	}
	for _, s := range t.Stmts {
		// In a patch-based template, drop is a declaration: this section is intentionally
		// absent from the product (the build checks that it really is)
		if s.Op != "patch" && s.Op != "drop" {
			return fmt.Errorf("%s: this template applies a patch (%s) and the patch result is the product, so this statement has no effect — move the change into the patch, or don't use a patch",
				s.Rng, patch.Rng)
		}
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func unknownIn(w string, words []string, what string, at Pos) error {
	if near := nearest(w, words); near != "" {
		return fmt.Errorf("%s: unknown %s `%s` — did you mean %s?", at, what, w, near)
	}
	return fmt.Errorf("%s: unknown %s `%s` (available: %s)", at, what, w, strings.Join(words, " / "))
}

// nearest returns the closest word within edit distance 2, or "" if there is none.
func nearest(w string, words []string) string {
	best, bestD := "", 3
	for _, c := range words {
		if d := editDistance(w, c); d < bestD {
			best, bestD = c, d
		}
	}
	return best
}

func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
