package build

// Resolving the statements that actually run.
//
// `if` picks one branch, a caught result may let a failed write through, and `return` stops the
// rest. None of that can be settled while parsing, because the answers are properties of the
// document — so it is settled here, once, before anything is written, and both the weave and the
// report read the same list. Two of them working from different lists would be a report about a
// build that did not happen.
//
// Every question is asked of upstream as it arrives, not of the product half-built. That is what
// the questions mean: `base.has.Overview` asks whether upstream has that section, and a write
// fails when upstream has no such anchor. Neither depends on the order edits are applied in, which
// is why one pass up front is enough.

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/axfor/loom/ast"
	"github.com/axfor/loom/lang"
)

type run struct {
	stmts   []lang.Stmt
	skipped []ReportLine
	stopped bool
	vars    map[string]string // a caught result: "" when the write went through, else why not
}

// resolve walks the statements in order and returns the ones that run.
func resolve(c *lang.Config, t *lang.Template, tree ast.Tree) (*run, error) {
	r := &run{vars: map[string]string{}}
	if err := r.walk(c, t, tree, t.Stmts); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *run) walk(c *lang.Config, t *lang.Template, tree ast.Tree, ss []lang.Stmt) error {
	for _, s := range ss {
		if r.stopped {
			return nil
		}
		switch s.Op {
		case "stop":
			r.stopped = true
			return nil
		case "fail":
			return fmt.Errorf("%s: %s", s.Rng, r.format(s.Errf))
		case "if":
			yes, err := r.ask(tree, s.Cond)
			if err != nil {
				return err
			}
			branch, taken := s.Kids, "then"
			if !yes {
				branch, taken = s.Else, "else"
			}
			if len(branch) == 0 {
				r.skipped = append(r.skipped, ReportLine{lang.Rel(c, t.Path),
					fmt.Sprintf("%s skipped (%s)", t.Target, describeCond(s.Cond))})
				continue
			}
			_ = taken
			if err := r.walk(c, t, tree, branch); err != nil {
				return err
			}
		default:
			if s.Assign != "" {
				// The result is caught, so a write that cannot land is this statement's answer
				// rather than the build's end. It does not run.
				if _, err := targets(tree, s); err != nil {
					r.vars[s.Assign] = err.Error()
					r.skipped = append(r.skipped, ReportLine{lang.Rel(c, t.Path),
						fmt.Sprintf("%s not written (%s caught it)", s.Anchor, s.Assign)})
					continue
				}
				r.vars[s.Assign] = ""
			}
			// join is unwrap applied to the section after this one: the two sections run together
			// because the second one's heading goes. Saying it once, here, means everything after
			// this point deals with one operation instead of two that differ only in which node.
			if s.Op == "join" {
				name := s.Anchor
				span, real, err := locate(tree, s.Kind, s.Within, s.Anchor, s.Ident, s)
				if err != nil {
					return err
				}
				name = real
				var next string
				for _, n := range ast.Addressable(tree, s.Kind) {
					if n.Line == span[1] {
						next = n.Name
					}
				}
				if next == "" {
					return fmt.Errorf("%s: %q has no section after it to join", s.Rng, name)
				}
				s.Op, s.Anchor, s.Ident, s.Within, s.Axis = "unwrap", next, false, nil, ""
			}
			// An axis names a node the document relates to this one; expanding it here means the
			// accounting, the checks and the report all work by the name of the node actually hit,
			// exactly as they do for a predicate.
			if s.Axis != "" {
				hit, err := targets(tree, s)
				if err != nil {
					return err
				}
				nodes := ast.Addressable(tree, s.Kind)
				for _, span := range hit {
					cp := s
					cp.Axis, cp.Ident, cp.Within, cp.Select = "", false, nil, nil
					for _, n := range nodes {
						if n.Line == span[0] {
							cp.Anchor = n.Name
						}
					}
					if cp.Anchor == "" {
						return fmt.Errorf("%s: the %s step landed somewhere with no name", s.Rng, s.Axis)
					}
					r.stmts = append(r.stmts, cp)
				}
				continue
			}
			r.stmts = append(r.stmts, s)
		}
	}
	return nil
}

// ask answers an if. Both forms read the document and write nothing.
func (r *run) ask(tree ast.Tree, c *lang.Cond) (bool, error) {
	var yes bool
	switch {
	case c.Var != "":
		why, ok := r.vars[c.Var]
		if !ok {
			return false, fmt.Errorf("%s: `%s` has no result yet — the statement that catches it comes later", c.Rng, c.Var)
		}
		yes = why == ""
	case c.Has != "":
		yes = len(tree.Find(c.Kind, c.Has)) > 0
	case c.Any:
		found, err := selected(tree, c.Sel)
		if err != nil {
			return false, fmt.Errorf("%s: %v", c.Rng, err)
		}
		yes = len(found) > 0
	default:
		return false, fmt.Errorf("%s: this condition asks nothing", c.Rng)
	}
	if c.Not {
		yes = !yes
	}
	return yes, nil
}

// format builds the message of `return err.format(...)`. Only the message is formatted: nothing
// here can reach the product, so a build that fails says why without weakening what a product is.
func (r *run) format(refs []lang.Ref) string {
	args := make([]any, 0, len(refs)-1)
	for _, ref := range refs[1:] {
		if ref.IsLit {
			args = append(args, ref.Literal)
			continue
		}
		why := r.vars[ref.Anchor]
		if why == "" {
			why = "it went through"
		}
		args = append(args, why)
	}
	return fmt.Sprintf(refs[0].Literal, args...)
}

func describeCond(c *lang.Cond) string {
	var what string
	switch {
	case c.Var != "":
		what = c.Var
	case c.Has != "":
		what = "base.has." + lang.NameText(c.Has)
	case c.Any:
		what = "the predicate found nothing"
		if c.Not {
			return what + ", as asked"
		}
		return what
	}
	if c.Not {
		return what + " held"
	}
	return what + " did not hold"
}

// match answers one predicate about one node.
func match(tree ast.Tree, p *lang.Pred, n ast.Named) (bool, error) {
	switch p.Op {
	case "not":
		v, err := match(tree, &p.Kids[0], n)
		return !v, err
	case "and", "or":
		a, err := match(tree, &p.Kids[0], n)
		if err != nil {
			return false, err
		}
		if p.Op == "and" && !a {
			return false, nil
		}
		if p.Op == "or" && a {
			return true, nil
		}
		return match(tree, &p.Kids[1], n)
	}
	switch p.Field {
	case "level":
		switch p.Cmp {
		case "==":
			return n.Level == p.Num, nil
		case "!=":
			return n.Level != p.Num, nil
		case "<":
			return n.Level < p.Num, nil
		case "<=":
			return n.Level <= p.Num, nil
		case ">":
			return n.Level > p.Num, nil
		case ">=":
			return n.Level >= p.Num, nil
		}
		return false, fmt.Errorf("level %s is not a comparison", p.Cmp)
	case "name":
		switch p.Cmp {
		case "==":
			return n.Name == p.Str, nil
		case "!=":
			return n.Name != p.Str, nil
		case "~":
			re, err := regexp.Compile(p.Str)
			if err != nil {
				return false, fmt.Errorf("name ~ %q: %v", p.Str, err)
			}
			return re.MatchString(n.Name), nil
		}
		return false, fmt.Errorf("name %s is not a comparison", p.Cmp)
	case "empty":
		return strings.TrimSpace(strings.Join(tree.Lines()[n.Line+1:n.End], "")) == "", nil
	case "calls":
		return calls(tree.Lines()[n.Line+1:n.End], p.Str), nil
	case "has":
		// A part this one holds, by name — not a word that happens to appear in its text. For
		// markdown that is a heading below it; elsewhere a node whose lines fall inside this one.
		for _, m := range ast.Addressable(tree, kindOf(tree, n)) {
			if m.Line > n.Line && m.Line < n.End && m.Name == p.Str {
				return true, nil
			}
			if n.Level > 0 && m.Level > n.Level && m.Line > n.Line && m.Name == p.Str {
				// A section ends at the next heading, so what is under it is the deeper
				// headings that follow — they are nodes of their own.
				if deeper(tree, n, m) {
					return true, nil
				}
			}
		}
		return false, nil
	}
	return false, fmt.Errorf("unknown predicate %q", p.Field)
}

// calls reports whether these lines invoke a command by that name. A command sits at the start of
// a statement, so `curl` in `echo curly` and `curl` in a comment are not calls to it — which is
// the whole difference between asking what a function does and grepping it.
func calls(lines []string, name string) bool {
	// A command starts a statement: at the beginning of a line, after a separator, inside a
	// substitution, or after one of the words that introduce one.
	word := regexp.MustCompile(`(^|[;|&({]|\$\(|` + "`" + `|&&|\|\||\b(?:if|then|else|elif|do|while|until|!)\s)\s*` + regexp.QuoteMeta(name) + `(\s|$|;|\)|` + "`" + `)`)
	for _, l := range lines {
		if i := comment(l); i >= 0 {
			l = l[:i]
		}
		if word.MatchString(strings.TrimSpace(l)) {
			return true
		}
	}
	return false
}

// comment is where a shell comment starts on a line, or -1. A # inside quotes is text.
func comment(l string) int {
	var q byte
	for i := 0; i < len(l); i++ {
		switch c := l[i]; {
		case q != 0:
			if c == q {
				q = 0
			}
		case c == '\'' || c == '"':
			q = c
		case c == '#' && (i == 0 || l[i-1] == ' ' || l[i-1] == '\t'):
			return i
		}
	}
	return -1
}

// kindOf is the node kind a tree gives this node, so `has` looks among nodes of the same sort.
func kindOf(tree ast.Tree, n ast.Named) string {
	for _, k := range tree.Kinds() {
		for _, m := range ast.Addressable(tree, k) {
			if m.Line == n.Line && m.Name == n.Name {
				return k
			}
		}
	}
	return ""
}

// deeper reports whether m sits under n: every heading between them is deeper than n.
func deeper(tree ast.Tree, n, m ast.Named) bool {
	for _, k := range ast.Addressable(tree, kindOf(tree, n)) {
		if k.Line > n.Line && k.Line <= m.Line && k.Level <= n.Level {
			return false
		}
	}
	return true
}
