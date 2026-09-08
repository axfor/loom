package ast

import "strings"

// ast —— 每类资源自己的结构。
//
// 【为什么不做通用 AST】通用模型要么表达不了（markdown 的节级操作套不到 shell 上），
// 要么表达了但不安全（把 shell 拼成一个谁也没跑过的程序）。
// 结构必须从每个类型自己的形状长出来 —— 所以一种类型一个文件，各长各的。

// Tree 是一棵解析好的资源树。统一接口只有四件事：
// 找节点、取/写一个键、按行区间改、出文本。
type Tree interface {
	Kinds() []string
	Find(kind, anchor string) [][2]int
	BodyOf(key string) (string, bool)
	SetBody(key, val string) bool
	Lines() []string
	Splice(s, e int, repl []string)
	Text() string
}

// Types 保持一个固定顺序 —— 求值时按这个顺序挑第一个认识某种节点的类型，
// 顺序换了就会挑到另一棵树。
var Types = []string{"markdown", "toml", "json", "shell", "text"}

// New 按类型建树。
func New(typ, text string) Tree {
	switch typ {
	case "markdown":
		return NewMarkdown(text)
	case "toml":
		return NewToml(text)
	case "json":
		return NewJSON(text)
	case "shell":
		return NewShell(text)
	case "text":
		return NewText(text)
	}
	return nil
}

// Kinds 说某个类型能锚到哪些节点。
func Kinds(typ string) []string {
	switch typ {
	case "markdown":
		return []string{"heading", "line", "frontmatter"}
	case "toml":
		return []string{"key"}
	case "json":
		return []string{"path"}
	case "shell":
		return []string{"function", "marker", "line"}
	case "text":
		return []string{"line"}
	}
	return nil
}

// Has 说某个类型认不认识某种节点。
func Has(kinds []string, k string) bool {
	for _, x := range kinds {
		if x == k {
			return true
		}
	}
	return false
}

// splice 是所有按行树共用的替换。
func splice(lines []string, s, e int, repl []string) []string {
	out := make([]string, 0, len(lines)-(e-s)+len(repl))
	out = append(out, lines[:s]...)
	out = append(out, repl...)
	out = append(out, lines[e:]...)
	return out
}

// Named 是一个可寻址的节点：名字 + 它在第几行。
type Named struct {
	Name string
	Line int
}

// Addressable 列出一棵树在某种节点下所有可寻址的名字 ——
// 写模板时真正想知道的是「这个文件我能锚到哪些点」。
func Addressable(t Tree, kind string) []Named {
	switch v := t.(type) {
	case *Markdown:
		if kind != "heading" {
			return nil
		}
		var out []Named
		for _, n := range v.nodes {
			out = append(out, Named{n.title, n.start})
		}
		return out
	case *Toml:
		if kind != "key" {
			return nil
		}
		var out []Named
		for _, n := range v.nodes {
			out = append(out, Named{n.key, n.start})
		}
		return out
	case *Shell:
		var out []Named
		if kind == "function" {
			for _, n := range v.nodes {
				out = append(out, Named{n.name, n.start})
			}
		}
		if kind == "marker" {
			for _, n := range v.markers {
				out = append(out, Named{strings.TrimSpace(n.name), n.start})
			}
		}
		return out
	}
	return nil
}
