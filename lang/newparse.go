package lang

// The constructs SYNTAX.md adds to the statement language: predicates in brackets, axes, `if`,
// captured results, `return`, `fn`, and the `Self:` resource sections after a line of dashes.
//
// Two of them cannot be answered while parsing, because the answer is a property of the document
// being woven: whether a section exists, and whether a write succeeded. Those become statements of
// their own and the weave decides them. Everything else is settled here — `fn` in particular is
// inlined at its call, so nothing at build time needs a call stack.

import (
	"fmt"
	"strings"
)

// ── the added syntax tree ────────────────────────────────────────────────────

// oNode is one thing at the top level of a file, or inside a fn or an if.
type oNode struct {
	expr   *oExpr // a write, or a bare call to a fn
	assign string // `ok = base.X.after(...)`: the name the result is caught in
	ifN    *oIf
	ret    *oReturn
	rebase string // `base = "path"`: the upstream file this template is woven onto
	pos    Pos
}

type oIf struct {
	not  bool
	cond *oExpr
	then []oNode
	els  []oNode
	pos  Pos
}

type oReturn struct {
	what   *oExpr // `return self`
	errArg []oArg // `return err.format("...", ok)`
	reason string
	pos    Pos
}

type oFn struct {
	params []string
	body   []oNode
	pos    Pos
}

// oResource is one `Self:` or `Self as name:` section: the fences it holds, in order.
type oResource struct {
	name   string
	fences []Tok
	pos    Pos
}

// oPred is a predicate written in brackets: base.sections[level == 2 && !empty].
type oPred struct {
	op    string  // "and" / "or" / "not" / a leaf
	kids  []oPred // for and / or / not
	field string  // leaf: level / name / empty / calls / has
	cmp   string  // leaf: == != < <= > >= ~
	str   string
	num   int
	pos   Pos
}

// ── parsing ──────────────────────────────────────────────────────────────────

func (p *oparser) block(open Pos) ([]oNode, error) {
	if t := p.next(); t.Kind != KLBrace {
		return nil, fmt.Errorf("%s: expected `{`, got %s", t.Pos, t)
	}
	var out []oNode
	for {
		p.skipNewlines()
		t := p.peek()
		if t.Kind == KRBrace {
			p.next()
			return out, nil
		}
		if t.Kind == KEOF {
			return nil, fmt.Errorf("%s: this block is never closed", open)
		}
		n, err := p.node()
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
}

// node reads one statement: a write, an assignment, an if, a return, or a call.
func (p *oparser) node() (oNode, error) {
	t := p.peek()
	switch {
	case t.Kind == KIdent && t.Text == "if":
		n, err := p.ifNode()
		return oNode{ifN: n, pos: t.Pos}, err
	case t.Kind == KIdent && t.Text == "return":
		n, err := p.returnNode()
		return oNode{ret: n, pos: t.Pos}, err
	case t.Kind == KIdent && t.Text == "base" && p.toks[p.i+1].Kind == KAssign && p.toks[p.i+2].Kind == KString:
		// base = "old/NAME.md": upstream moved the file this one is woven onto.
		p.next()
		p.next()
		sp := p.next()
		if err := p.endOfStatement(); err != nil {
			return oNode{}, err
		}
		return oNode{rebase: sp.Text, pos: t.Pos}, nil
	case t.Kind == KIdent && p.toks[p.i+1].Kind == KAssign:
		name := p.next().Text
		p.next() // =
		e, err := p.expr()
		if err != nil {
			return oNode{}, err
		}
		if err := p.endOfStatement(); err != nil {
			return oNode{}, err
		}
		return oNode{expr: e, assign: name, pos: t.Pos}, nil
	}
	e, err := p.expr()
	if err != nil {
		return oNode{}, err
	}
	if err := p.endOfStatement(); err != nil {
		return oNode{}, err
	}
	return oNode{expr: e, pos: t.Pos}, nil
}

func (p *oparser) ifNode() (*oIf, error) {
	at := p.next().Pos // if
	n := &oIf{pos: at}
	if p.peek().Kind == KBang {
		p.next()
		n.not = true
	}
	p.noBlock = true
	e, err := p.expr()
	p.noBlock = false
	if err != nil {
		return nil, err
	}
	n.cond = e
	if n.then, err = p.block(at); err != nil {
		return nil, err
	}
	if t := p.peek(); t.Kind == KIdent && t.Text == "else" {
		p.next()
		if n.els, err = p.block(t.Pos); err != nil {
			return nil, err
		}
	}
	return n, p.endOfStatement()
}

func (p *oparser) returnNode() (*oReturn, error) {
	at := p.next().Pos // return
	r := &oReturn{pos: at}
	switch t := p.peek(); {
	case t.Kind == KNewline || t.Kind == KEOF || t.Kind == KRBrace:
	case t.Kind == KIdent && t.Text == "err":
		p.next()
		if d := p.next(); d.Kind != KDot {
			return nil, fmt.Errorf("%s: err takes format: return err.format(\"...\")", d.Pos)
		}
		if f := p.next(); f.Kind != KIdent || f.Text != "format" {
			return nil, fmt.Errorf("%s: err takes format: return err.format(\"...\")", f.Pos)
		}
		if o := p.next(); o.Kind != KLParen {
			return nil, fmt.Errorf("%s: err.format takes a message: return err.format(\"...\")", o.Pos)
		}
		args, err := p.parenArgs()
		if err != nil {
			return nil, err
		}
		if len(args) == 0 || args[0].val.kind != vString {
			return nil, fmt.Errorf("%s: err.format needs a message: return err.format(\"upstream has no %%s\", ok)", at)
		}
		r.errArg = args
	default:
		e, err := p.expr()
		if err != nil {
			return nil, err
		}
		r.what = e
	}
	// `return self` needs a reason, and a reason reads best as the comment it already is. The
	// lexer drops comments, so it is taken straight from the line.
	r.reason = reasonOn(p.lines, at.Line)
	if p.peek().Kind == KRBrace {
		return r, nil
	}
	return r, p.endOfStatement()
}

// fnDecl reads `fn name(a, b) { ... }`.
func (p *oparser) fnDecl() (string, *oFn, error) {
	at := p.next().Pos // fn
	t := p.next()
	if t.Kind != KIdent {
		return "", nil, fmt.Errorf("%s: fn needs a name: fn sections() { ... }", t.Pos)
	}
	f := &oFn{pos: at}
	if o := p.next(); o.Kind != KLParen {
		return "", nil, fmt.Errorf("%s: fn needs parentheses, even with no parameters: fn %s() { ... }", o.Pos, t.Text)
	}
	for p.peek().Kind != KRParen {
		a := p.next()
		if a.Kind != KIdent {
			return "", nil, fmt.Errorf("%s: a parameter is a name, got %s", a.Pos, a)
		}
		f.params = append(f.params, a.Text)
		if p.peek().Kind == KComma {
			p.next()
		}
	}
	p.next() // )
	var err error
	if f.body, err = p.block(at); err != nil {
		return "", nil, err
	}
	return t.Text, f, p.endOfStatement()
}

// predicate reads what is inside brackets: base.sections[level == 2 && !empty].
func (p *oparser) predicate() (*oPred, error) {
	open := p.next().Pos // [
	n, err := p.predOr()
	if err != nil {
		return nil, err
	}
	if t := p.next(); t.Kind != KRBracket {
		return nil, fmt.Errorf("%s: this predicate is never closed (opened at %s)", t.Pos, open)
	}
	return n, nil
}

func (p *oparser) predOr() (*oPred, error) {
	left, err := p.predAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().Kind == KOr {
		at := p.next().Pos
		right, err := p.predAnd()
		if err != nil {
			return nil, err
		}
		left = &oPred{op: "or", kids: []oPred{*left, *right}, pos: at}
	}
	return left, nil
}

func (p *oparser) predAnd() (*oPred, error) {
	left, err := p.predTerm()
	if err != nil {
		return nil, err
	}
	for p.peek().Kind == KAnd {
		at := p.next().Pos
		right, err := p.predTerm()
		if err != nil {
			return nil, err
		}
		left = &oPred{op: "and", kids: []oPred{*left, *right}, pos: at}
	}
	return left, nil
}

var predFields = []string{"level", "name", "empty", "calls", "has"}

func (p *oparser) predTerm() (*oPred, error) {
	t := p.peek()
	switch t.Kind {
	case KBang:
		p.next()
		k, err := p.predTerm()
		if err != nil {
			return nil, err
		}
		return &oPred{op: "not", kids: []oPred{*k}, pos: t.Pos}, nil
	case KLParen:
		p.next()
		n, err := p.predOr()
		if err != nil {
			return nil, err
		}
		if c := p.next(); c.Kind != KRParen {
			return nil, fmt.Errorf("%s: expected `)` to close the group opened at %s", c.Pos, t.Pos)
		}
		return n, nil
	case KIdent:
	default:
		return nil, fmt.Errorf("%s: a predicate asks about %s, got %s", t.Pos, strings.Join(predFields, " / "), t)
	}
	p.next()
	n := &oPred{field: t.Text, pos: t.Pos}
	switch t.Text {
	case "empty":
		return n, nil
	case "calls":
		s := p.next()
		if s.Kind != KString {
			return nil, fmt.Errorf("%s: calls takes a quoted name: calls \"curl\"", s.Pos)
		}
		n.str = s.Text
		return n, nil
	case "has":
		if d := p.next(); d.Kind != KDot {
			return nil, fmt.Errorf("%s: has names a part: has.\"Usage\"", d.Pos)
		}
		s := p.next()
		if s.Kind != KString && s.Kind != KIdent {
			return nil, fmt.Errorf("%s: has names a part: has.\"Usage\"", s.Pos)
		}
		n.str = s.Text
		return n, nil
	case "level":
		c := p.next()
		n.cmp = c.Text
		switch c.Kind {
		case KEq, KNe, KLt, KLe, KGt, KGe:
		default:
			return nil, fmt.Errorf("%s: level is compared with == != < <= > >=, got %s", c.Pos, c)
		}
		v := p.next()
		if v.Kind != KNumber {
			return nil, fmt.Errorf("%s: level is compared with a number, got %s", v.Pos, v)
		}
		fmt.Sscanf(v.Text, "%d", &n.num)
		if n.num < 1 || n.num > 6 {
			return nil, fmt.Errorf("%s: level %d — markdown headings run from 1 to 6", v.Pos, n.num)
		}
		return n, nil
	case "name":
		c := p.next()
		switch c.Kind {
		case KEq, KNe, KMatch:
			n.cmp = c.Text
		default:
			return nil, fmt.Errorf("%s: name is compared with == != or ~ (a regular expression), got %s", c.Pos, c)
		}
		s := p.next()
		if s.Kind != KString {
			return nil, fmt.Errorf("%s: name is compared with a quoted string, got %s", s.Pos, s)
		}
		n.str = s.Text
		return n, nil
	}
	return nil, unknownIn(t.Text, predFields, "predicate", t.Pos)
}

// resources reads everything after the line of dashes: one or more `Self:` sections.
func (p *oparser) resources() ([]oResource, error) {
	var out []oResource
	for {
		p.skipNewlines()
		t := p.peek()
		if t.Kind == KEOF {
			return out, nil
		}
		if t.Kind == KSep {
			p.next()
			continue
		}
		if t.Kind != KIdent || t.Text != "Self" {
			return nil, fmt.Errorf("%s: after the dashes come resource sections: `Self:` or `Self as name:`, got %s", t.Pos, t)
		}
		p.next()
		r := oResource{pos: t.Pos}
		if a := p.peek(); a.Kind == KIdent && a.Text == "as" {
			p.next()
			n := p.next()
			if n.Kind != KIdent {
				return nil, fmt.Errorf("%s: `Self as` needs a name: Self as notes:", n.Pos)
			}
			r.name = n.Text
		}
		if c := p.next(); c.Kind != KColon {
			return nil, fmt.Errorf("%s: a resource section is `Self:` — the colon is what opens it", c.Pos)
		}
		for {
			p.skipNewlines()
			f := p.peek()
			if f.Kind != KRaw {
				break
			}
			p.next()
			r.fences = append(r.fences, f)
		}
		if len(r.fences) == 0 {
			return nil, fmt.Errorf("%s: this resource section holds nothing — a section is one or more fenced documents", r.pos)
		}
		out = append(out, r)
	}
}

// reasonOn reads `// reason: ...` from the end of a line.
func reasonOn(lines []string, line int) string {
	if line < 1 || line > len(lines) {
		return ""
	}
	i := strings.Index(lines[line-1], "//")
	if i < 0 {
		return ""
	}
	c := strings.TrimSpace(lines[line-1][i+2:])
	if !strings.HasPrefix(strings.ToLower(c), "reason:") {
		return ""
	}
	return strings.TrimSpace(c[len("reason:"):])
}
