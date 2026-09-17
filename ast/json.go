package ast

// JSON is an **order-preserving** JSON tree.
//
// [Why not encoding/json's map] Map keys are unordered and get sorted on
// serialization — but in files like registries the key order is chosen by a person
// and carries meaning; reorder it once and it can never be matched back.
// Preserving order also gives a stronger property: **untouched parts stay
// byte-for-byte identical**, so a diff shows only the real changes.

import (
	"fmt"
	"strings"
)

// Value is a JSON value. Objects remember key insertion order.
type Value struct {
	Kind  Kind
	Keys  []string          // Object: key order
	Props map[string]*Value // Object
	Elems []*Value          // Array
	Str   string            // String
	Num   string            // Number: keeps the original literal, so 1 never becomes 1.0
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

// Get returns a field of an object.
func (v *Value) Get(k string) (*Value, bool) {
	if v == nil || v.Kind != Object {
		return nil, false
	}
	c, ok := v.Props[k]
	return c, ok
}

// Set writes a field; a new key is appended at the end, an existing key is
// replaced in place (order unchanged).
func (v *Value) Set(k string, val *Value) {
	if v.Props == nil {
		v.Props = map[string]*Value{}
	}
	if _, ok := v.Props[k]; !ok {
		v.Keys = append(v.Keys, k)
	}
	v.Props[k] = val
}

// NewObject makes an empty object.
func NewObject() *Value { return &Value{Kind: Object, Props: map[string]*Value{}} }

// NewArray makes an empty array.
func NewArray() *Value { return &Value{Kind: Array} }

// Clone makes a deep copy.
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

// Marshal outputs with two-space indentation, keeping non-ASCII as is.
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

// writeString escapes only what must be escaped: quotes, backslashes, control characters.
// Non-ASCII is **not turned into \u** — non-English text should stay readable in the output.
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

// JSONTree makes json satisfy the Tree interface too. Line-based editing makes no
// sense for json — json goes through structural merging, not line insertion.
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

// Find looks up a field by dotted path. If it exists, it returns an empty range —
// existence is the only question it can answer.
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
