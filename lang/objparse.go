package lang

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
// What this produces is the template's intermediate form (template.go), which the engine and
// the tools read.

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// defaultKind is the node kind a bare name refers to (in engine terms).
var defaultKind = map[string]string{
	"markdown": "heading",
	"shell":    "function",
	"toml":     "key",
	"yaml":     "key",
	"json":     "path",
	"text":     "line",
}

// classCalls lists, per type, the plural form that selects every node of a kind:
// base.sections(...) against base.section("Name"). The singular names one node, the plural
// names a group and takes predicates rather than a name.
var classCalls = map[string]map[string]string{
	"markdown": {"sections": "heading", "lines": "line"},
	"shell":    {"functions": "function", "markers": "marker", "lines": "line"},
	"toml":     {"keys": "key"},
	"yaml":     {"keys": "key"},
	"json":     {"keys": "path"},
	"text":     {"lines": "line"},
}

// kindCalls lists, per type, the kinds accepted in the kind("name") form (a json key is a dotted path).
var kindCalls = map[string]map[string]string{
	"markdown": {"section": "heading", "line": "line"},
	"shell":    {"function": "function", "marker": "marker", "line": "line"},
	"toml":     {"key": "key"},
	"yaml":     {"key": "key"},
	"json":     {"key": "path"},
	"text":     {"line": "line"},
}

// editMethods are the methods that change base.
// groupMethods are the operations that mean the same thing done to every node a predicate
// found. Anything that needs a target or a source of its own is not one of them.
var groupMethods = []string{"drop", "promote", "demote"}

// namedArgs is every argument name the language knows: reason: on an edit, after: / before: on a
// move, match: / level: in a predicate. It is one list because it has to be mirrored exactly —
// the editor's grammar highlights these and marks anything else a typo — and because a
// misspelled argument is worth a suggestion rather than a list to read.
var namedArgs = []string{"reason", "after", "before", "match", "level"}

var editMethods = []string{"after", "before", "start", "append", "replace", "drop", "move", "promote", "demote", "set", "merge"}

func typeWords() []string { return []string{"markdown", "toml", "yaml", "json", "shell", "text"} }

// TypeOf infers the type from a file path. Anything it cannot infer is plain text.
func TypeOf(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".md", ".markdown":
		return "markdown"
	case ".toml":
		return "toml"
	case ".yaml", ".yml":
		return "yaml"
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
	vNumber
)

type oValue struct {
	kind vkind
	str  string
	num  int
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
	toks []Tok
	i    int
}

func (p *oparser) peek() Tok { return p.toks[p.i] }

func (p *oparser) next() Tok {
	t := p.toks[p.i]
	if t.Kind != KEOF {
		p.i++
	}
	return t
}

func (p *oparser) skipNewlines() {
	for p.peek().Kind == KNewline {
		p.next()
	}
}

func (p *oparser) endOfStatement() error {
	t := p.peek()
	if t.Kind == KNewline || t.Kind == KEOF {
		p.next()
		return nil
	}
	return fmt.Errorf("%s: statement should end here, got %s — one statement per line", t.Pos, t)
}

// parseObjects reads the imports and statements.
func parseObjects(file string, src []byte) ([]oImport, []*oExpr, error) {
	toks, err := Lex(file, src)
	if err != nil {
		return nil, nil, err
	}
	p := &oparser{toks: toks}
	var imports []oImport
	var stmts []*oExpr
	for {
		p.skipNewlines()
		t := p.peek()
		if t.Kind == KEOF {
			return imports, stmts, nil
		}
		if t.Kind == KIdent && t.Text == "import" {
			p.next()
			im := oImport{pos: t.Pos}
			if n := p.peek(); n.Kind == KIdent {
				p.next()
				im.name, im.named = n.Text, true
			}
			s := p.next()
			if s.Kind != KString {
				return nil, nil, fmt.Errorf("%s: import takes a quoted path: import cmd \"/.claude/commands/ship\"", s.Pos)
			}
			im.spec = s.Text
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
	if t.Kind != KIdent {
		return nil, fmt.Errorf("%s: expected an object (base / self / an imported name), got %s", t.Pos, t)
	}
	e := &oExpr{root: t.Text, pos: t.Pos}
	for p.peek().Kind == KDot {
		p.next()
		n := p.next()
		switch n.Kind {
		case KString:
			e.steps = append(e.steps, oStep{name: n.Text, str: true, pos: n.Pos})
		case KIdent:
			st := oStep{name: n.Text, pos: n.Pos}
			switch p.peek().Kind {
			case KLParen:
				p.next()
				args, err := p.parenArgs()
				if err != nil {
					return nil, err
				}
				st.call, st.args = true, args
			case KLBrace:
				b := p.next()
				args, err := p.blockArgs(b.Pos)
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
			return nil, fmt.Errorf("%s: expected a node or method name after `.`, got %s", n.Pos, n)
		}
	}
	return e, nil
}

func (p *oparser) parenArgs() ([]oArg, error) {
	var out []oArg
	for {
		p.skipNewlines()
		if p.peek().Kind == KRParen {
			p.next()
			return out, nil
		}
		a, err := p.arg()
		if err != nil {
			return nil, err
		}
		out = append(out, a)
		p.skipNewlines()
		switch t := p.next(); t.Kind {
		case KComma:
		case KRParen:
			return out, nil
		default:
			return nil, fmt.Errorf("%s: separate arguments with commas and close with `)`, got %s", t.Pos, t)
		}
	}
}

func (p *oparser) blockArgs(open Pos) ([]oArg, error) {
	if t := p.peek(); t.Kind != KNewline {
		return nil, fmt.Errorf("%s: expected a newline after `{` — the block form takes one argument per line", t.Pos)
	}
	var out []oArg
	for {
		p.skipNewlines()
		t := p.peek()
		switch t.Kind {
		case KRBrace:
			p.next()
			if len(out) == 0 {
				return nil, fmt.Errorf("%s: empty block — { } with nothing inside", open)
			}
			return out, nil
		case KEOF:
			return nil, fmt.Errorf("%s: block has no closing `}`", open)
		}
		a, err := p.arg()
		if err != nil {
			return nil, err
		}
		out = append(out, a)
		switch n := p.peek(); n.Kind {
		case KNewline:
			p.next()
		case KRBrace:
		case KComma:
			return nil, fmt.Errorf("%s: the block form takes one argument per line, without commas", n.Pos)
		default:
			return nil, fmt.Errorf("%s: the block form takes one argument per line; unexpected %s", n.Pos, n)
		}
	}
}

func (p *oparser) arg() (oArg, error) {
	t := p.peek()
	if t.Kind == KIdent && p.toks[p.i+1].Kind == KColon {
		p.next()
		p.next()
		v, err := p.value()
		return oArg{name: t.Text, val: v, pos: t.Pos}, err
	}
	v, err := p.value()
	return oArg{val: v, pos: t.Pos}, err
}

func (p *oparser) value() (oValue, error) {
	t := p.peek()
	switch t.Kind {
	case KString:
		p.next()
		return oValue{kind: vString, str: t.Text, pos: t.Pos}, nil
	case KRaw:
		p.next()
		return oValue{kind: vRaw, str: t.Text, pos: t.Pos}, nil
	case KNumber:
		p.next()
		n, err := strconv.Atoi(t.Text)
		if err != nil {
			return oValue{}, fmt.Errorf("%s: %s is too large a number", t.Pos, t.Text)
		}
		return oValue{kind: vNumber, num: n, pos: t.Pos}, nil
	case KIdent:
		e, err := p.expr()
		if err != nil {
			return oValue{}, err
		}
		return oValue{kind: vExpr, expr: e, pos: t.Pos}, nil
	}
	return oValue{}, fmt.Errorf("%s: expected an argument: \"name\", `literal`, a number, or something like self.xxx, got %s", t.Pos, t)
}

// ── Interpretation: syntax tree → engine statements ───────────────────────────

// Resolver resolves an import path to a real file in the layer (a path relative to the
// layer root). The extension may be omitted.
type Resolver func(layer, spec, fromDir string) (string, error)

type object struct {
	layer string // "base" / "self"
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
	t := &Template{Path: file, Target: target, Type: TypeOf(target), BasePath: target}
	in := &interp{t: t, objects: map[string]object{
		"base": {layer: "base", file: target, typ: TypeOf(target)},
		"self": {layer: "self", typ: TypeOf(target)},
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
		layer := "self"
		if name == "base" {
			layer = "base"
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
	if err := checkMergeAlone(t); err != nil {
		return nil, err
	}
	if t.From == "self" {
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
	toks, err := Lex("", []byte(s))
	return err == nil && len(toks) == 3 && toks[0].Kind == KIdent && toks[0].Text == s
}

// receiver is the thing a statement changes.
type receiver struct {
	node   bool   // false = the whole file (or a view)
	kind   string // node kind (in engine terms)
	anchor string
	ident  bool // name is in identifier form; _ matches a space or an underscore
	within []Seg
	typ    string
	sel    *Select // non-nil = a group, not one node
	viewOf string  // non-empty = inside an as(...) view: the key name
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
		if r.viewOf != "" || r.kind != defaultKind[r.typ] || !hasValues(r.typ) {
			return fmt.Errorf("%s: as only applies to a key whose value is a document — toml, yaml or json: base.prompt.as(markdown)", st.pos)
		}
		*r = receiver{typ: typ, viewOf: r.anchor, viewAs: typ, pos: r.pos}
		return nil
	}
	if r.node {
		// A predicate picks from the whole file, so it cannot hang off a node already chosen.
		if st.call && classCalls[r.typ][st.name] != "" {
			return fmt.Errorf("%s: %s selects from the whole file, so it comes first: base.%s(...)", st.pos, st.name, st.name)
		}
		// A frontmatter key is a node of its own: base.frontmatter.description
		if r.kind == "frontmatter" && !st.call {
			r.kind, r.anchor, r.ident, r.pos = "fmkey", st.name, false, st.pos
			return nil
		}
		// Key path: base.jobs.test — yaml and json address a nested key by the dotted path of
		// its parents, so a name below a key extends that path instead of starting a new lookup.
		if !st.call && hasValues(r.typ) && r.kind == defaultKind[r.typ] && r.viewOf == "" {
			r.anchor, r.ident, r.pos = r.anchor+"."+st.name, false, st.pos
			return nil
		}
		// Section path: base."Example 2"."Phase 1" — look for the child within the parent's whole section
		if st.call || r.typ != "markdown" || r.kind != "heading" || (!st.str && (st.name == "frontmatter" || st.name == "body")) {
			return fmt.Errorf("%s: below a node you can only select a markdown section by name, base.\"Parent\".\"Child\", or a frontmatter key, base.frontmatter.description", st.pos)
		}
		r.within = append(r.within, Seg{r.anchor, r.ident})
		r.anchor, r.ident, r.pos = st.name, !st.str, st.pos
		return nil
	}
	switch {
	case st.call && classCalls[r.typ][st.name] != "":
		sel, err := in.predicate(st, classCalls[r.typ][st.name])
		if err != nil {
			return err
		}
		r.node, r.kind, r.sel, r.pos = true, sel.Kind, sel, st.pos
		return nil
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
	if st.name == "join" {
		return fmt.Errorf("%s: join is gone — say where our value goes: base.frontmatter.description.start(self.frontmatter.description) puts ours before upstream's, append after it; in toml or json, base.description.start(self.description)", st.pos)
	}
	if st.name == "patch" {
		return fmt.Errorf("%s: there are no patch files any more — our file is upstream plus our edits, and lm sync carries the edits onto each new upstream: write base.merge(self) and delete the .diff", st.pos)
	}
	if !contains(editMethods, st.name) {
		if near := nearest(st.name, editMethods); near != "" {
			return fmt.Errorf("%s: no method `%s` — did you mean %s?", st.pos, st.name, near)
		}
		return fmt.Errorf("%s: no method `%s` (available: %s)", st.pos, st.name, strings.Join(editMethods, " / "))
	}
	var pos []oArg
	reason := ""
	var moveTo *Move
	for _, a := range st.args {
		if a.name == "" {
			pos = append(pos, a)
			continue
		}
		if a.name == "after" || a.name == "before" {
			if st.name != "move" {
				return fmt.Errorf("%s: `%s:` is only for move: base.X.move(%s: base.Y)", a.pos, a.name, a.name)
			}
			if moveTo != nil {
				return fmt.Errorf("%s: move takes one side, after: or before:", a.pos)
			}
			m, err := in.moveTarget(a.name, a.val, r.typ)
			if err != nil {
				return err
			}
			moveTo = m
			continue
		}
		if a.name != "reason" {
			if near := nearest(a.name, namedArgs); near != "" {
				return fmt.Errorf("%s: unknown named argument `%s:` — did you mean `%s:`?", a.pos, a.name, near)
			}
			return fmt.Errorf("%s: unknown named argument `%s:` (reason: for replace / drop, after: / before: for move)", a.pos, a.name)
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
	if r.sel != nil && !contains(groupMethods, st.name) {
		return fmt.Errorf("%s: %s selected a group, and %s is not something to do to each of them — a group takes %s",
			st.pos, "the predicate", st.name, strings.Join(groupMethods, " / "))
	}
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
		out = append(out, Stmt{Op: st.name, Kind: r.kind, Anchor: r.anchor, Ident: r.ident, At: r.pos, Within: r.within, Srcs: srcs, Rng: at})
	case "start", "append":
		if r.node {
			if !isValue(r) {
				return fmt.Errorf("%s: %s applies to the whole file or to a key's value: base.%s(...), base.frontmatter.description.%s(self.frontmatter.description)", at, st.name, st.name, st.name)
			}
			v, err := in.valueArg(pos, r, st.name, at)
			if err != nil {
				return err
			}
			out = append(out, Stmt{Op: "value", Mode: st.name, Kind: r.kind, SetKey: r.anchor, SetRef: v, Rng: at})
			break
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
			if obj.layer != "self" {
				return fmt.Errorf("%s: a whole-file replace must use a file from our layer (self or an imported one)", v.pos)
			}
			in.t.From, in.t.UseReason, in.t.Source = "self", reason, obj.file
			return nil
		}
		if !r.node {
			return fmt.Errorf("%s: replace applies to a node", at)
		}
		srcs, err := in.contents(pos, r.typ, r.viewOf != "")
		if err != nil {
			return err
		}
		out = append(out, Stmt{Op: "replace", Kind: r.kind, Anchor: r.anchor, Ident: r.ident, At: r.pos, Within: r.within, Srcs: srcs, Reason: reason, Rng: at})
	case "drop":
		if reason == "" {
			return fmt.Errorf("%s: drop changes upstream content and needs a reason: drop(reason: \"...\")", at)
		}
		if !r.node || len(pos) != 0 {
			return fmt.Errorf("%s: drop applies to a node and takes only a reason: base.X.drop(reason: \"...\")", at)
		}
		out = append(out, Stmt{Op: "drop", Kind: r.kind, Anchor: r.anchor, Ident: r.ident, At: r.pos, Within: r.within, Select: r.sel, Reason: reason, Rng: at})
	case "move":
		if !r.node || len(pos) != 0 {
			return fmt.Errorf("%s: move applies to a node and takes one side: base.X.move(after: base.Y)", at)
		}
		if moveTo == nil {
			return fmt.Errorf("%s: move needs a side: move(after: base.Y) or move(before: base.Y)", at)
		}
		if moveTo.Kind == r.kind && moveTo.Anchor == r.anchor && len(moveTo.Within) == len(r.within) {
			return fmt.Errorf("%s: move takes a different node as its target", at)
		}
		out = append(out, Stmt{Op: "move", Kind: r.kind, Anchor: r.anchor, Ident: r.ident, At: r.pos, Within: r.within, Move: moveTo, Rng: at})
	case "promote", "demote":
		if !r.node || len(pos) != 0 {
			return fmt.Errorf("%s: %s applies to a node and takes nothing: base.X.%s()", at, st.name, st.name)
		}
		if r.typ != "markdown" || r.kind != "heading" {
			return fmt.Errorf("%s: %s is about heading level, so it applies to a markdown section", at, st.name)
		}
		out = append(out, Stmt{Op: st.name, Kind: r.kind, Anchor: r.anchor, Ident: r.ident, At: r.pos, Within: r.within, Select: r.sel, Rng: at})
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
			out = append(out, Stmt{Op: "frontmatter", Layer: "self", File: ref.File, Rng: at})
			break
		}
		if !isValue(r) {
			return fmt.Errorf("%s: set applies to the frontmatter, a frontmatter key or a toml / json key", at)
		}
		v, err := in.valueArg(pos, r, st.name, at)
		if err != nil {
			return err
		}
		out = append(out, Stmt{Op: "value", Mode: "set", Kind: r.kind, SetKey: r.anchor, SetRef: v, Rng: at})
	case "merge":
		if r.node || r.viewOf != "" {
			return fmt.Errorf("%s: merge applies to the whole file: base.merge(self)", at)
		}
		if len(pos) != 1 || pos[0].val.kind != vExpr || pos[0].val.expr.root != "self" || len(pos[0].val.expr.steps) != 0 {
			return fmt.Errorf("%s: merge takes our file at the same path: base.merge(self)", at)
		}
		// Both layers hold a file and the product is the two of them together — what that means follows
		// the type: for a script our file is the product (lm sync carries upstream's changes onto it),
		// for a registry the entries are merged by identity while building.
		op := "merge"
		if in.t.Type == "json" {
			op = "registry"
		}
		out = append(out, Stmt{Op: op, Rng: at})
	}
	if r.viewOf != "" {
		// Statements in a view go into an in statement; adjacent ones on the same key and type
		// merge into one, so the key's value is parsed and written back once
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
// moveTarget resolves the node a move goes next to: `after: base.X`, or a path —
// `after: base."A"."B"` for a markdown section, `after: base.jobs.test` for a key.
// The target is in the same file as the node being moved, so it is named on the warp.
func (in *interp) moveTarget(side string, v oValue, typ string) (*Move, error) {
	if v.kind != vExpr {
		return nil, fmt.Errorf("%s: move takes a node of this file: move(%s: base.X)", v.pos, side)
	}
	e := v.expr
	obj, ok := in.objects[e.root]
	if !ok {
		return nil, in.unknownObject(e.root, v.pos)
	}
	if obj.layer == "self" {
		return nil, fmt.Errorf("%s: a move goes next to a node of this same file, which is upstream: move(%s: base.X)", v.pos, side)
	}
	if len(e.steps) == 0 {
		return nil, fmt.Errorf("%s: move needs a node to go next to: move(%s: base.X)", v.pos, side)
	}
	m := &Move{Side: side, Kind: defaultKind[typ]}
	var names []string
	for _, st := range e.steps {
		if st.call {
			return nil, fmt.Errorf("%s: move takes a plain node, not a call", st.pos)
		}
		names = append(names, st.name)
		if !st.str {
			m.Ident = true
		}
	}
	if hasValues(typ) {
		m.Anchor = strings.Join(names, ".")
		m.Ident = false
		return m, nil
	}
	for _, n := range names[:len(names)-1] {
		m.Within = append(m.Within, Seg{Name: n})
	}
	m.Anchor = names[len(names)-1]
	return m, nil
}

// predicate reads the arguments of a class call into a selection.
func (in *interp) predicate(st oStep, kind string) (*Select, error) {
	sel := &Select{Kind: kind}
	for _, a := range st.args {
		switch {
		case a.name == "match":
			if a.val.kind != vString {
				return nil, fmt.Errorf("%s: match: takes a quoted regular expression", a.pos)
			}
			if _, err := regexp.Compile(a.val.str); err != nil {
				return nil, fmt.Errorf("%s: match: %v", a.pos, err)
			}
			sel.Match = a.val.str
		case a.name == "level":
			// Only a markdown heading has a level. Saying so here, rather than letting the
			// predicate quietly match nothing, is the difference between a typo and a mystery.
			if kind != "heading" {
				return nil, fmt.Errorf("%s: level: is for markdown headings; a %s has no level", a.pos, kind)
			}
			if a.val.kind != vNumber {
				return nil, fmt.Errorf("%s: level: takes a number, 1 to 6", a.pos)
			}
			if a.val.num < 1 || a.val.num > 6 {
				return nil, fmt.Errorf("%s: level: %d — markdown headings run from 1 to 6", a.pos, a.val.num)
			}
			sel.Level = a.val.num
		case a.name != "":
			if near := nearest(a.name, namedArgs); near != "" {
				return nil, fmt.Errorf("%s: unknown predicate `%s:` — did you mean `%s:`?", a.pos, a.name, near)
			}
			return nil, fmt.Errorf("%s: unknown predicate `%s:` (match: and level: are the ones that take a value)", a.pos, a.name)
		case a.val.kind == vExpr && len(a.val.expr.steps) == 0 && a.val.expr.root == "empty":
			sel.Empty = true
		default:
			return nil, fmt.Errorf("%s: a predicate is match: \"...\", level: N, or empty", a.pos)
		}
	}
	if sel.Match == "" && !sel.Empty && sel.Level == 0 {
		return nil, fmt.Errorf("%s: %s needs a predicate, or it would select the whole file: %s(match: \"...\")", st.pos, st.name, st.name)
	}
	return sel, nil
}

// projection recognises base.<class>(predicate).project(`template`) in an argument. It is the
// one content source rooted at upstream: it copies none of what upstream says, only the shape,
// so what it writes is new rather than duplicated.
func (in *interp) projection(e *oExpr, pos Pos, typ string) (Ref, bool, error) {
	if len(e.steps) != 2 || !e.steps[0].call || !e.steps[1].call || e.steps[1].name != "project" {
		return Ref{}, false, nil
	}
	obj, ok := in.objects[e.root]
	if !ok || obj.layer == "self" {
		return Ref{}, false, nil
	}
	kind := classCalls[obj.typ][e.steps[0].name]
	if kind == "" {
		return Ref{}, false, nil
	}
	sel, err := in.predicate(e.steps[0], kind)
	if err != nil {
		return Ref{}, false, err
	}
	a := e.steps[1].args
	if len(a) != 1 || a[0].name != "" || (a[0].val.kind != vRaw && a[0].val.kind != vString) {
		return Ref{}, false, fmt.Errorf("%s: project takes one template: project(`- {name}`)", e.steps[1].pos)
	}
	return Ref{Layer: obj.layer, Kind: "project", Project: sel, Literal: a[0].val.str, IsLit: true, Rng: pos}, true, nil
}

func (in *interp) contents(args []oArg, typ string, inView bool) ([]Ref, error) {
	var out []Ref
	for _, a := range args {
		v := a.val
		switch v.kind {
		case vString:
			out = append(out, Ref{Layer: "self", Kind: defaultKind[typ], Anchor: v.str, Rng: v.pos})
		case vRaw:
			out = append(out, Ref{Layer: "self", IsLit: true, Literal: v.str, Rng: v.pos})
		case vExpr:
			e := v.expr
			// A projection reads upstream's structure and writes something new from it:
			// base.sections(match: "...").project(`- {name}`)
			if ref, ok, err := in.projection(e, v.pos, typ); err != nil {
				return nil, err
			} else if ok {
				out = append(out, ref)
				continue
			}
			obj, ok := in.objects[e.root]
			if !ok {
				if len(e.steps) == 0 && (e.root == "body" || e.root == "frontmatter") {
					return nil, fmt.Errorf("%s: write self.%s", v.pos, e.root)
				}
				return nil, in.unknownObject(e.root, v.pos)
			}
			if obj.layer != "self" {
				return nil, fmt.Errorf("%s: inserted content must come from our layer (self or an imported file); %s is upstream", v.pos, e.root)
			}
			ref := Ref{Layer: "self", File: obj.file, Rng: v.pos}
			switch {
			case len(e.steps) == 0:
				ref.Kind = "all"
			case len(e.steps) == 2 && !e.steps[0].str && !e.steps[0].call && e.steps[0].name == "frontmatter" && !e.steps[1].call:
				// a frontmatter key of ours: self.frontmatter.description
				ref.Kind, ref.Anchor = "fmkey", e.steps[1].name
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

// hasValues reports whether a type's nodes are keys holding a value — the types whose
// value can be written with set / start / append, and re-opened as another type with as.
func hasValues(typ string) bool { return HasValues(typ) }

// HasValues reports whether a type's nodes are keys holding a value that set / start / append can
// write. build needs to ask the same question the parser does.
func HasValues(typ string) bool {
	return typ == "toml" || typ == "yaml" || typ == "json"
}

// isValue reports whether a receiver is a key whose value set / start / append can write: a frontmatter
// key, or a key of a type that holds values, outside a view.
func isValue(r receiver) bool {
	return r.node && r.viewOf == "" && (r.kind == "fmkey" || (hasValues(r.typ) && r.kind == defaultKind[r.typ]))
}

// valueArg reads the one value set / start / append takes: a key of our file or a literal.
func (in *interp) valueArg(pos []oArg, r receiver, method string, at Pos) (Ref, error) {
	example := "self.frontmatter.description"
	if r.kind != "fmkey" {
		example = "self.description"
	}
	if len(pos) != 1 {
		return Ref{}, fmt.Errorf("%s: %s takes one value: %s(%s)", at, method, method, example)
	}
	srcs, err := in.contents(pos, r.typ, false)
	if err != nil {
		return Ref{}, err
	}
	v := srcs[0]
	if !v.IsLit && v.Kind != "fmkey" && v.Kind != "key" && v.Kind != "path" {
		return Ref{}, fmt.Errorf("%s: %s takes a value, a key of our file or a `literal`: %s(%s)", pos[0].pos, method, method, example)
	}
	return v, nil
}

// checkMergeAlone rejects weave statements alongside a merge: our file is the product, so other
// weave statements would have no effect — and letting them silently do nothing is far worse than
// an error. drop is the exception: it accounts for where an upstream section went and needs no
// weaving.
func checkMergeAlone(t *Template) error {
	var merge *Stmt
	for k := range t.Stmts {
		if t.Stmts[k].Op == "merge" {
			merge = &t.Stmts[k]
			break
		}
	}
	if merge == nil {
		return nil
	}
	for _, s := range t.Stmts {
		// In a merge template, drop is a declaration: this section is intentionally absent from the
		// product (the build checks that it really is)
		if s.Op != "merge" && s.Op != "drop" {
			return fmt.Errorf("%s: this template merges upstream into our file (%s) and our file is the product, so this statement has no effect — make the change in our file",
				s.Rng, merge.Rng)
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
		if d := EditDistance(w, c); d < bestD {
			best, bestD = c, d
		}
	}
	return best
}

func EditDistance(a, b string) int {
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
