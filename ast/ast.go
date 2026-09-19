package ast

import "strings"

// ast — each resource type's own structure.
//
// [Why no generic AST] A generic model either cannot express things (markdown's
// section-level operations do not map onto shell), or expresses them unsafely
// (stitching shell into a program nobody has ever run).
// Structure has to grow out of each type's own shape — hence one file per type,
// each growing its own way.

// Tree is a parsed resource tree. The shared interface does only four things:
// find a node, get/set a key, edit a line range, emit text.
type Tree interface {
	Kinds() []string
	Find(kind, anchor string) [][2]int
	BodyOf(key string) (string, bool)
	SetBody(key, val string) bool
	Lines() []string
	Splice(s, e int, repl []string)
	Text() string
}

// Types keeps a fixed order — evaluation picks the first type in this order that
// knows a given node kind, so reordering it would pick a different tree.
var Types = []string{"markdown", "toml", "yaml", "json", "shell", "text"}

// New builds a tree for the given type.
func New(typ, text string) Tree {
	switch typ {
	case "markdown":
		return NewMarkdown(text)
	case "toml":
		return NewToml(text)
	case "yaml":
		return NewYaml(text)
	case "json":
		return NewJSON(text)
	case "shell":
		return NewShell(text)
	case "text":
		return NewText(text)
	}
	return nil
}

// Kinds reports which node kinds a type can anchor to.
func Kinds(typ string) []string {
	switch typ {
	case "markdown":
		return []string{"heading", "line", "frontmatter"}
	case "toml":
		return []string{"key"}
	case "yaml":
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

// Has reports whether a type knows a given node kind.
func Has(kinds []string, k string) bool {
	for _, x := range kinds {
		if x == k {
			return true
		}
	}
	return false
}

// splice is the replacement shared by all line-based trees.
func splice(lines []string, s, e int, repl []string) []string {
	out := make([]string, 0, len(lines)-(e-s)+len(repl))
	out = append(out, lines[:s]...)
	out = append(out, repl...)
	out = append(out, lines[e:]...)
	return out
}

// Named is an addressable node: its name, where it starts, and where it ends.
type Named struct {
	Name  string
	Line  int
	End   int // one past the last line the node covers
	Level int // level of a markdown heading (## is 2); 0 for other nodes
}

// Addressable lists every addressable name a tree has for a node kind —
// what a template author really wants to know is "which points in this file can I anchor to".
func Addressable(t Tree, kind string) []Named {
	switch v := t.(type) {
	case *Markdown:
		if kind != "heading" {
			return nil
		}
		var out []Named
		for _, n := range v.nodes {
			out = append(out, Named{Name: n.title, Line: n.start, End: n.end, Level: n.level})
		}
		return out
	case *Toml:
		if kind != "key" {
			return nil
		}
		var out []Named
		for _, n := range v.nodes {
			out = append(out, Named{Name: n.key, Line: n.start, End: n.end})
		}
		return out
	case *Yaml:
		if kind != "key" {
			return nil
		}
		var out []Named
		for _, n := range v.nodes {
			out = append(out, Named{Name: n.path, Line: n.start, End: n.end})
		}
		return out
	case *Shell:
		var out []Named
		if kind == "function" {
			for _, n := range v.nodes {
				out = append(out, Named{Name: n.name, Line: n.start, End: n.end})
			}
		}
		if kind == "marker" {
			for _, n := range v.markers {
				out = append(out, Named{Name: strings.TrimSpace(n.name), Line: n.start, End: n.end})
			}
		}
		return out
	}
	return nil
}
