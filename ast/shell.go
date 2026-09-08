package ast

import (
	"regexp"
	"strings"
)

type shNode struct {
	name  string
	start int
	end   int
}

type Shell struct {
	lines    []string
	nodes    []shNode
	markers  []shNode
	Unclosed []string
}

var reBanner = regexp.MustCompile(`^` + ws + `*#` + ws + `*[─=—-]{2,}`)
var reFunc = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)` + ws + `*\(\)` + ws + `*\{`)

// NewShell：节点 = 函数定义，**以及标记段（marker）**。
// 上游的 shell 大量是平铺的顶层流程 —— 顶层其实有天然的名字：`# ── Test 3: … ───` 这类横幅。
func NewShell(text string) *Shell {
	s := &Shell{lines: strings.Split(text, "\n")}
	var b []int
	for i, l := range s.lines {
		if reBanner.MatchString(l) {
			b = append(b, i)
		}
	}
	for k, i := range b {
		end := len(s.lines)
		if k+1 < len(b) {
			end = b[k+1]
		}
		s.markers = append(s.markers, shNode{s.lines[i], i, end})
	}
	for i, l := range s.lines {
		g := reFunc.FindStringSubmatch(l)
		if g == nil {
			continue
		}
		end := -1
		for j := i + 1; j < len(s.lines); j++ {
			if s.lines[j] == "}" {
				end = j + 1
				break
			}
		}
		if end == -1 {
			s.Unclosed = append(s.Unclosed, g[1])
		} else {
			s.nodes = append(s.nodes, shNode{g[1], i, end})
		}
	}
	return s
}

func (s *Shell) Kinds() []string                { return Kinds("shell") }
func (s *Shell) Lines() []string                { return s.lines }
func (s *Shell) Text() string                   { return strings.Join(s.lines, "\n") }
func (s *Shell) Splice(a, b int, repl []string) { s.lines = splice(s.lines, a, b, repl) }
func (s *Shell) BodyOf(string) (string, bool)   { return "", false }
func (s *Shell) SetBody(string, string) bool    { return false }

func (s *Shell) Find(kind, anchor string) [][2]int {
	var out [][2]int
	switch kind {
	case "line":
		for i, l := range s.lines {
			if l == anchor {
				out = append(out, [2]int{i, i + 1})
			}
		}
	case "marker":
		for _, m := range s.markers {
			if strings.HasPrefix(strings.TrimSpace(m.name), strings.TrimSpace(anchor)) {
				out = append(out, [2]int{m.start, m.end})
			}
		}
	default:
		for _, n := range s.nodes {
			if n.name == anchor {
				out = append(out, [2]int{n.start, n.end})
			}
		}
	}
	return out
}
