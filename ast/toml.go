package ast

import (
	"regexp"
	"strings"
)

type tomlNode struct {
	key   string
	start int
	end   int
	block bool
}

type Toml struct {
	lines []string
	nodes []tomlNode
}

var reTomlKey = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_-]*)` + ws + `*=` + ws + `*(.*)$`)

// NewToml：节点 = 顶层键。prompt 这种三引号块可以再换成 markdown（@in … as markdown）。
func NewToml(text string) *Toml {
	t := &Toml{lines: strings.Split(text, "\n")}
	t.reparse()
	return t
}

func (t *Toml) reparse() {
	t.nodes = nil
	i := 0
	for i < len(t.lines) {
		if g := reTomlKey.FindStringSubmatch(t.lines[i]); g != nil {
			if strings.HasPrefix(g[2], `"""`) {
				j := i + 1
				for j < len(t.lines) && !strings.HasPrefix(t.lines[j], `"""`) {
					j++
				}
				t.nodes = append(t.nodes, tomlNode{g[1], i, j + 1, true})
				i = j + 1
				continue
			}
			t.nodes = append(t.nodes, tomlNode{g[1], i, i + 1, false})
		}
		i++
	}
}

func (t *Toml) Kinds() []string { return Kinds("toml") }
func (t *Toml) Lines() []string { return t.lines }
func (t *Toml) Text() string    { return strings.Join(t.lines, "\n") }
func (t *Toml) Splice(s, e int, repl []string) {
	t.lines = splice(t.lines, s, e, repl)
	t.reparse()
}

func (t *Toml) Find(kind, anchor string) [][2]int {
	var out [][2]int
	for _, n := range t.nodes {
		if n.key == anchor {
			out = append(out, [2]int{n.start, n.end})
		}
	}
	return out
}

func (t *Toml) BodyOf(key string) (string, bool) {
	for _, n := range t.nodes {
		if n.key != key {
			continue
		}
		if n.block {
			return strings.Join(t.lines[n.start+1:n.end-1], "\n"), true
		}
		parts := strings.SplitN(t.lines[n.start], "=", 2)
		return strings.TrimSpace(parts[1]), true
	}
	return "", false
}

func (t *Toml) SetBody(key, val string) bool {
	for _, n := range t.nodes {
		if n.key != key {
			continue
		}
		if n.block {
			repl := strings.Split(val, "\n")
			t.lines = append(t.lines[:n.start+1], append(append([]string{}, repl...), t.lines[n.end-1:]...)...)
		} else {
			t.lines[n.start] = n.key + ` = "` + val + `"`
		}
		t.reparse()
		return true
	}
	return false
}

// ── shell ─────────────────────────────────────────────────────────────
