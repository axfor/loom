package ast

import (
	"regexp"
	"strings"
)

// yaml — nodes are keys, addressed by the dotted path of their parents:
// `jobs.build.steps`. A key whose value is an indented block covers that block;
// a key whose value is a block scalar (`|` or `>`) can be re-opened as another
// type, which is how a markdown document living inside a yaml value gets sections
// (@in ... as markdown), the same way a toml `"""` block can.
//
// Line-based on purpose, like the other trees here: the product must keep every
// byte the author wrote, and a parse-and-print round trip through a real yaml
// library rewrites quoting, key order and comments.

type yamlNode struct {
	path   string // dotted path from the root
	indent int
	start  int
	end    int
	block  bool // value is a block scalar (| or >)
}

type Yaml struct {
	lines []string
	nodes []yamlNode
}

// A key line: indentation, the key (bare, "quoted" or 'quoted'), a colon, then the rest.
// A list item may carry a key too (`- name: x`), which yaml treats as a map in a sequence.
var reYamlKey = regexp.MustCompile(`^(` + ws + `*)(?:-` + ws + `+)?("[^"]*"|'[^']*'|[^\s:#][^:]*?)` + ws + `*:(?:` + ws + `+(.*))?$`)

func NewYaml(text string) *Yaml {
	y := &Yaml{lines: strings.Split(text, "\n")}
	y.reparse()
	return y
}

func unquoteYamlKey(k string) string {
	if len(k) >= 2 && (k[0] == '"' && k[len(k)-1] == '"' || k[0] == '\'' && k[len(k)-1] == '\'') {
		return k[1 : len(k)-1]
	}
	return k
}

// blockScalar reports whether a value opens a literal (|) or folded (>) block,
// allowing the chomping and indentation indicators yaml permits after it.
var reBlockScalar = regexp.MustCompile(`^[|>][+-]?[0-9]*` + ws + `*$`)

func (y *Yaml) reparse() {
	y.nodes = nil
	type frame struct {
		indent int
		name   string
	}
	var stack []frame

	for i := 0; i < len(y.lines); i++ {
		line := y.lines[i]
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		g := reYamlKey.FindStringSubmatch(line)
		if g == nil {
			continue
		}
		indent := len(g[1])
		// A list item's key sits deeper than the dash that introduces it, so that
		// siblings under the same dash share a parent.
		if strings.Contains(g[0][len(g[1]):], "- ") && strings.HasPrefix(strings.TrimSpace(line), "- ") {
			indent++
		}
		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}
		name := unquoteYamlKey(strings.TrimSpace(g[2]))
		path := name
		if len(stack) > 0 {
			path = stack[len(stack)-1].name + "." + name
		}
		stack = append(stack, frame{indent, path})

		value := strings.TrimSpace(g[3])
		end := i + 1
		block := reBlockScalar.MatchString(value)
		// The node covers whatever is indented under it: a nested map, a sequence,
		// or the body of a block scalar.
		for j := i + 1; j < len(y.lines); j++ {
			t := strings.TrimSpace(y.lines[j])
			if t == "" {
				continue // a blank line inside a block belongs to it
			}
			if len(y.lines[j])-len(strings.TrimLeft(y.lines[j], " \t")) <= indent {
				break
			}
			end = j + 1
		}
		y.nodes = append(y.nodes, yamlNode{path, indent, i, end, block})
	}
}

func (y *Yaml) Kinds() []string { return Kinds("yaml") }
func (y *Yaml) Lines() []string { return y.lines }
func (y *Yaml) Text() string    { return strings.Join(y.lines, "\n") }

func (y *Yaml) Splice(s, e int, repl []string) {
	y.lines = splice(y.lines, s, e, repl)
	y.reparse()
}

// Find matches a full dotted path, or a trailing part of one when it is unambiguous —
// `steps` finds `jobs.build.steps` as long as no other path ends in `steps`.
func (y *Yaml) Find(kind, anchor string) [][2]int {
	var exact, suffix [][2]int
	for _, n := range y.nodes {
		switch {
		case n.path == anchor:
			exact = append(exact, [2]int{n.start, n.end})
		case strings.HasSuffix(n.path, "."+anchor):
			suffix = append(suffix, [2]int{n.start, n.end})
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return suffix
}

// BodyOf returns a key's value: the dedented body for a block scalar, the scalar
// itself otherwise. A block scalar's body is what `as markdown` re-parses.
func (y *Yaml) BodyOf(key string) (string, bool) {
	n, ok := y.node(key)
	if !ok {
		return "", false
	}
	if !n.block {
		parts := strings.SplitN(y.lines[n.start], ":", 2)
		if len(parts) < 2 {
			return "", false
		}
		return strings.TrimSpace(parts[1]), true
	}
	body := y.lines[n.start+1 : n.end]
	return strings.Join(dedent(body), "\n"), true
}

func (y *Yaml) SetBody(key, val string) bool {
	n, ok := y.node(key)
	if !ok {
		return false
	}
	if n.block {
		indent := strings.Repeat(" ", n.indent+2)
		var repl []string
		for _, l := range strings.Split(val, "\n") {
			if l == "" {
				repl = append(repl, "")
				continue
			}
			repl = append(repl, indent+l)
		}
		y.lines = splice(y.lines, n.start+1, n.end, repl)
	} else {
		head := strings.SplitN(y.lines[n.start], ":", 2)[0]
		y.lines[n.start] = head + ": " + val
	}
	y.reparse()
	return true
}

func (y *Yaml) node(key string) (yamlNode, bool) {
	var hit yamlNode
	found := false
	for _, n := range y.nodes {
		if n.path == key {
			return n, true
		}
		if strings.HasSuffix(n.path, "."+key) && !found {
			hit, found = n, true
		}
	}
	return hit, found
}

// dedent removes the common leading whitespace of a block scalar's body, which is
// how yaml itself reads one.
func dedent(lines []string) []string {
	min := -1
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		n := len(l) - len(strings.TrimLeft(l, " \t"))
		if min < 0 || n < min {
			min = n
		}
	}
	if min <= 0 {
		return lines
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		if len(l) >= min {
			out[i] = l[min:]
		} else {
			out[i] = strings.TrimLeft(l, " \t")
		}
	}
	return out
}
