package lang

// Turning the added syntax into statements: `if`, caught results, `return`, and the resource
// sections. What a fn meant is already gone by the time this runs — it was inlined where it was
// called — so only the constructs whose answer depends on the document being woven survive into
// the statement tree.

import (
	"fmt"
	"strings"
)

func (in *interp) out() *[]Stmt {
	if in.sink != nil {
		return in.sink
	}
	return &in.t.Stmts
}

// nodes reads a run of statements into a list of its own, so an if's branches are separate from
// the statements around them.
func (in *interp) nodes(body []oNode) ([]Stmt, error) {
	var out []Stmt
	prev := in.sink
	in.sink = &out
	defer func() { in.sink = prev }()
	for _, n := range body {
		if err := in.node(n); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (in *interp) node(n oNode) error {
	switch {
	case n.ifN != nil:
		return in.ifStmt(n.ifN)
	case n.ret != nil:
		return in.returnStmt(n.ret)
	case n.rebase != "":
		if in.rebased {
			return fmt.Errorf("%s: base is already pointed somewhere else", n.pos)
		}
		in.rebased = true
		in.t.BasePath = strings.TrimPrefix(n.rebase, "/")
		o := in.objects["base"]
		o.file = in.t.BasePath
		in.objects["base"] = o
		return nil
	}
	before := len(*in.out())
	if err := in.statement(n.expr); err != nil {
		return err
	}
	if n.assign == "" {
		return nil
	}
	// Catching the result is what lets a failure through, so exactly one statement may be caught:
	// with two, "it failed" would not say which.
	added := (*in.out())[before:]
	if len(added) != 1 {
		return fmt.Errorf("%s: `%s = ...` catches the result of one statement, and this is %d", n.pos, n.assign, len(added))
	}
	if first, dup := in.vars[n.assign]; dup {
		return fmt.Errorf("%s: `%s` already holds a result (from %s) — give this one its own name", n.pos, n.assign, first)
	}
	in.vars[n.assign] = n.pos
	added[0].Assign = n.assign
	return nil
}

func (in *interp) ifStmt(n *oIf) error {
	c, err := in.cond(n.cond, n.not)
	if err != nil {
		return err
	}
	in.branch++
	then, err := in.nodes(n.then)
	if err != nil {
		in.branch--
		return err
	}
	els, err := in.nodes(n.els)
	in.branch--
	if err != nil {
		return err
	}
	if len(then) == 0 && len(els) == 0 {
		return fmt.Errorf("%s: this `if` does nothing either way", n.pos)
	}
	*in.out() = append(*in.out(), Stmt{Op: "if", Cond: c, Kids: then, Else: els, Rng: n.pos})
	return nil
}

// cond reads what an if asks: a result caught earlier, or a read-only question about the document.
func (in *interp) cond(e *oExpr, not bool) (*Cond, error) {
	c := &Cond{Not: not, Rng: e.pos}
	if len(e.steps) == 0 {
		if _, ok := in.vars[e.root]; !ok {
			return nil, fmt.Errorf("%s: `%s` is not a result caught earlier — write `ok = base....` first, or ask about the document: if base.has.Overview", e.pos, e.root)
		}
		in.used[e.root] = true
		c.Var = e.root
		return c, nil
	}
	obj, ok := in.objects[e.root]
	if !ok {
		return nil, in.unknownObject(e.root, e.pos)
	}
	c.Kind = defaultKind[obj.typ]
	// base.has.Overview — does the document have a part by this name
	if len(e.steps) == 2 && e.steps[0].name == "has" && !e.steps[0].call && !e.steps[1].call {
		c.Has = e.steps[1].name
		return c, nil
	}
	// base.sections[...].any — does the predicate find anything
	if len(e.steps) == 2 && (e.steps[1].name == "any" || e.steps[1].name == "count") && !e.steps[1].call {
		kind := classCalls[obj.typ][e.steps[0].name]
		if kind == "" || e.steps[0].pred == nil {
			return nil, fmt.Errorf("%s: `.%s` asks whether a predicate found anything: if base.sections[level == 2].any", e.pos, e.steps[1].name)
		}
		sel, err := selectOf(e.steps[0].pred, kind, e.steps[0].pos)
		if err != nil {
			return nil, err
		}
		c.Sel, c.Any, c.Kind = sel, true, kind
		return c, nil
	}
	return nil, fmt.Errorf("%s: an `if` asks a question that writes nothing: `if base.has.Name` or `if base.sections[...].any`", e.pos)
}

func (in *interp) returnStmt(r *oReturn) error {
	switch {
	case r.errArg != nil:
		refs, err := in.errRefs(r.errArg)
		if err != nil {
			return err
		}
		*in.out() = append(*in.out(), Stmt{Op: "fail", Errf: refs, Rng: r.pos})
		return nil
	case r.what == nil:
		*in.out() = append(*in.out(), Stmt{Op: "stop", Rng: r.pos})
		return nil
	}
	// return self — the product is our file, which is what the whole-file replace has always been.
	// It is a property of the whole template, not of a branch: the file is the function body, so
	// returning from it says what the product is, and a conditional answer would be two products.
	if in.branch > 0 {
		return fmt.Errorf("%s: `return self` says what this product is, so it cannot depend on a question — put it at the top level, or write what changes inside the branch", r.pos)
	}
	if r.what.root != "self" || len(r.what.steps) != 0 {
		return fmt.Errorf("%s: `return` takes self (the product is our file), err.format(...), or nothing", r.pos)
	}
	if r.reason == "" {
		return fmt.Errorf("%s: `return self` gives up every guarantee about upstream, so it needs a reason: return self // reason: ...", r.pos)
	}
	if in.t.From != "" {
		return fmt.Errorf("%s: this file already returns ours", r.pos)
	}
	in.t.From, in.t.UseReason = "self", r.reason
	return nil
}

// errRefs reads err.format's arguments: the message, then what goes into it.
func (in *interp) errRefs(args []oArg) ([]Ref, error) {
	out := []Ref{{IsLit: true, Literal: args[0].val.str, Rng: args[0].pos}}
	for _, a := range args[1:] {
		switch {
		case a.val.kind == vString:
			out = append(out, Ref{IsLit: true, Literal: a.val.str, Rng: a.pos})
		case a.val.kind == vExpr && len(a.val.expr.steps) == 0:
			name := a.val.expr.root
			if _, ok := in.vars[name]; !ok {
				return nil, fmt.Errorf("%s: `%s` is not a result caught earlier", a.pos, name)
			}
			in.used[name] = true
			out = append(out, Ref{Layer: "", Anchor: name, Rng: a.pos})
		default:
			return nil, fmt.Errorf("%s: err.format takes the message and results caught earlier", a.pos)
		}
	}
	if n := strings.Count(out[0].Literal, "%") - 2*strings.Count(out[0].Literal, "%%"); n != len(out)-1 {
		return nil, fmt.Errorf("%s: the message has %d placeholder(s) and %d value(s) follow it", args[0].pos, n, len(out)-1)
	}
	return out, nil
}

// readResources turns the fences of each `Self:` section into documents. The fence's language tag
// is the kind: nothing is declared twice, and an untagged fence has no kind to weave by.
func readResources(rs []oResource) ([]Resource, error) {
	var out []Resource
	seen := map[string]Pos{}
	for _, r := range rs {
		if first, dup := seen[r.name]; dup {
			what := "the resource section"
			if r.name != "" {
				what = "`Self as " + r.name + "`"
			}
			return nil, fmt.Errorf("%s: %s is written twice (first at %s)", r.pos, what, first)
		}
		seen[r.name] = r.pos
		res := Resource{Name: r.name, Rng: r.pos}
		kinds := map[string]bool{}
		for _, f := range r.fences {
			if f.Tag == "" {
				return nil, fmt.Errorf("%s: this fence has no language tag, so there is nothing to say what kind of document it is: ```markdown", f.Pos)
			}
			if !contains(typeWords(), f.Tag) {
				return nil, unknownIn(f.Tag, typeWords(), "kind", f.Pos)
			}
			if kinds[f.Tag] {
				return nil, fmt.Errorf("%s: this section already holds a %s document — one of each kind, so `self.%s` means one thing", f.Pos, f.Tag, f.Tag)
			}
			kinds[f.Tag] = true
			res.Docs = append(res.Docs, ResourceDoc{Kind: f.Tag, Text: f.Text, Rng: f.Pos})
		}
		out = append(out, res)
	}
	return out, nil
}

// selectOf turns a bracketed predicate into the selection the build runs. Match and Level are the
// two the engine already had; the rest arrive with the tree of terms.
func selectOf(p *oPred, kind string, at Pos) (*Select, error) {
	if p == nil {
		return nil, fmt.Errorf("%s: a group needs a predicate, or it would select the whole file", at)
	}
	if kind != "heading" && usesLevel(p) {
		return nil, fmt.Errorf("%s: level is for markdown headings; a %s has no level", at, kind)
	}
	if !hasValues(kindType(kind)) && p.uses("value") {
		return nil, fmt.Errorf("%s: value is what a key holds; a %s holds no value of its own", at, kind)
	}
	if kind != "function" && usesCalls(p) {
		return nil, fmt.Errorf("%s: `calls` is for shell functions; a %s calls nothing", at, kind)
	}
	return &Select{Kind: kind, Pred: p.node()}, nil
}

func usesLevel(p *oPred) bool { return p.uses("level") }
func usesCalls(p *oPred) bool { return p.uses("calls") }

func (p *oPred) uses(field string) bool {
	if p == nil {
		return false
	}
	if p.field == field {
		return true
	}
	for i := range p.kids {
		if p.kids[i].uses(field) {
			return true
		}
	}
	return false
}

// node is the predicate in the form the engine reads. The parser's tree is private to lang; this
// is the same shape with public fields, so build can walk it without importing the parser.
func (p *oPred) node() *Pred {
	if p == nil {
		return nil
	}
	out := &Pred{Op: p.op, Field: p.field, Cmp: p.cmp, Str: p.str, Num: p.num}
	for i := range p.kids {
		out.Kids = append(out.Kids, *p.kids[i].node())
	}
	return out
}

// kindType is the type whose default node kind this is, so a predicate can ask whether that type
// has values at all.
func kindType(kind string) string {
	for typ, k := range defaultKind {
		if k == kind {
			return typ
		}
	}
	return ""
}
