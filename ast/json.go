package ast

// JSON 是一棵**保序**的 JSON 树。
//
// 【为什么不用 encoding/json 的 map】map 的键无序，序列化时按字典序排 ——
// 而注册表这类文件的键顺序是人排的、有含义的，重排一次就再也对不回去了。
// 保序还带来一条更硬的性质：**没改到的地方逐字节不变**，于是 diff 只显示真的改动。

import (
	"fmt"
	"strings"
)

// Value 是一个 JSON 值。对象记住键的插入顺序。
type Value struct {
	Kind  Kind
	Keys  []string          // Object：键的顺序
	Props map[string]*Value // Object
	Elems []*Value          // Array
	Str   string            // String
	Num   string            // Number：保留原字面量，1 不会变成 1.0
	Bool  bool
}

type Kind int

const (
	Null Kind = iota
	Bool
	Number
	String
	Array
	Object
)

// Get 取对象的一个字段。
func (v *Value) Get(k string) (*Value, bool) {
	if v == nil || v.Kind != Object {
		return nil, false
	}
	c, ok := v.Props[k]
	return c, ok
}

// Set 写一个字段；新键追加到末尾，已有键就地替换（顺序不变）。
func (v *Value) Set(k string, val *Value) {
	if v.Props == nil {
		v.Props = map[string]*Value{}
	}
	if _, ok := v.Props[k]; !ok {
		v.Keys = append(v.Keys, k)
	}
	v.Props[k] = val
}

// NewObject 造一个空对象。
func NewObject() *Value { return &Value{Kind: Object, Props: map[string]*Value{}} }

// NewArray 造一个空数组。
func NewArray() *Value { return &Value{Kind: Array} }

// Clone 深拷贝。
func (v *Value) Clone() *Value {
	if v == nil {
		return nil
	}
	c := &Value{Kind: v.Kind, Str: v.Str, Num: v.Num, Bool: v.Bool}
	if v.Kind == Object {
		c.Props = map[string]*Value{}
		c.Keys = append([]string{}, v.Keys...)
		for k, p := range v.Props {
			c.Props[k] = p.Clone()
		}
	}
	for _, e := range v.Elems {
		c.Elems = append(c.Elems, e.Clone())
	}
	return c
}

// Marshal 按两空格缩进输出，非 ASCII 原样保留。
func (v *Value) Marshal() string {
	var b strings.Builder
	v.write(&b, 0)
	return b.String()
}

func (v *Value) write(b *strings.Builder, depth int) {
	pad := strings.Repeat("  ", depth+1)
	end := strings.Repeat("  ", depth)
	switch v.Kind {
	case Object:
		if len(v.Keys) == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteString("{\n")
		for i, k := range v.Keys {
			if i > 0 {
				b.WriteString(",\n")
			}
			b.WriteString(pad)
			writeString(b, k)
			b.WriteString(": ")
			v.Props[k].write(b, depth+1)
		}
		b.WriteString("\n" + end + "}")
	case Array:
		if len(v.Elems) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString("[\n")
		for i, e := range v.Elems {
			if i > 0 {
				b.WriteString(",\n")
			}
			b.WriteString(pad)
			e.write(b, depth+1)
		}
		b.WriteString("\n" + end + "]")
	case String:
		writeString(b, v.Str)
	case Number:
		b.WriteString(v.Num)
	case Bool:
		if v.Bool {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	default:
		b.WriteString("null")
	}
}

// writeString 只转义必须转义的：引号、反斜杠、控制字符。
// 非 ASCII **不转成 \u** —— 中文该在产物里读得出来。
func writeString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if r < 0x20 {
				fmt.Fprintf(b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}

// JSONTree 让 json 也满足 Tree 接口。按行编辑对 json 没有意义 ——
// json 走的是结构化合并，不是插行。
type JSONTree struct {
	Root  *Value
	lines []string
}

func NewJSON(text string) *JSONTree {
	v, err := Parse(text)
	if err != nil {
		v = NewObject()
	}
	return &JSONTree{Root: v, lines: strings.Split(text, "\n")}
}

func (j *JSONTree) Kinds() []string { return Kinds("json") }
func (j *JSONTree) Lines() []string { return j.lines }
func (j *JSONTree) Text() string    { return strings.Join(j.lines, "\n") }
func (j *JSONTree) Splice(s, e int, repl []string) {
	j.lines = splice(j.lines, s, e, repl)
}
func (j *JSONTree) BodyOf(string) (string, bool) { return "", false }
func (j *JSONTree) SetBody(string, string) bool  { return false }

// Find 按点分路径找一个字段。存在就返回一个空区间 —— 存在性是它唯一能回答的问题。
func (j *JSONTree) Find(kind, anchor string) [][2]int {
	cur := j.Root
	for _, part := range strings.Split(anchor, ".") {
		c, ok := cur.Get(part)
		if !ok {
			return nil
		}
		cur = c
	}
	return [][2]int{{0, 0}}
}
