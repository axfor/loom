package ast

import (
	"regexp"
	"strings"
)

type mdNode struct {
	level int
	title string
	start int
	end   int
}

type Markdown struct {
	lines []string
	nodes []mdNode
}

var reHeading = regexp.MustCompile(`^(#{2,6})` + ws + `+(.*)$`)

// NewMarkdown：节点 = 标题（## / ### …）。代码围栏内的 # 不算标题。
func NewMarkdown(text string) *Markdown {
	m := &Markdown{lines: strings.Split(text, "\n")}
	fence := false
	var cur *mdNode
	for i, l := range m.lines {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			fence = !fence
			continue
		}
		if fence {
			continue
		}
		if g := reHeading.FindStringSubmatch(l); g != nil {
			if cur != nil {
				cur.end = i
				m.nodes = append(m.nodes, *cur)
			}
			cur = &mdNode{level: len(g[1]), title: strings.TrimSpace(g[2]), start: i}
		}
	}
	if cur != nil {
		cur.end = len(m.lines)
		m.nodes = append(m.nodes, *cur)
	}
	return m
}

func (m *Markdown) Kinds() []string                { return Kinds("markdown") }
func (m *Markdown) Lines() []string                { return m.lines }
func (m *Markdown) Text() string                   { return strings.Join(m.lines, "\n") }
func (m *Markdown) Splice(s, e int, repl []string) { m.lines = splice(m.lines, s, e, repl) }

// fmSpan：frontmatter = 文件开头的 --- … --- 块。没有就返回 (0,0)（可插入的空位）。
func (m *Markdown) fmSpan() (int, int) {
	if len(m.lines) == 0 || strings.TrimSpace(m.lines[0]) != "---" {
		return 0, 0
	}
	for i := 1; i < len(m.lines); i++ {
		if strings.TrimSpace(m.lines[i]) == "---" {
			return 0, i + 1
		}
	}
	return 0, 0
}

func (m *Markdown) BodyOf(key string) (string, bool) {
	switch key {
	case "frontmatter":
		s, e := m.fmSpan()
		if e == 0 {
			return "", false
		}
		return strings.Join(m.lines[s:e], "\n"), true
	case "body", "content":
		// 不 lstrip：frontmatter 之后那个空行是正文的一部分。
		_, e := m.fmSpan()
		return strings.Join(m.lines[e:], "\n"), true
	}
	return "", false
}

func (m *Markdown) SetBody(key, val string) bool { return false }

func (m *Markdown) Find(kind, anchor string) [][2]int {
	var out [][2]int
	switch kind {
	case "frontmatter":
		s, e := m.fmSpan()
		if e != 0 {
			out = append(out, [2]int{s, e})
		}
	case "line":
		for i, l := range m.lines {
			if l == anchor {
				out = append(out, [2]int{i, i + 1})
			}
		}
	default:
		for _, n := range m.nodes {
			if n.title == anchor {
				out = append(out, [2]int{n.start, n.end})
			}
		}
	}
	return out
}

// ── toml ──────────────────────────────────────────────────────────────
