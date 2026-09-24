package ast

// JSON parsing. We write our own instead of using encoding/json for one reason
// only: **remembering key order**.

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Parse reads a piece of JSON text.
func Parse(text string) (*Value, error) {
	p := &jparser{s: text}
	p.ws()
	if p.i >= len(p.s) {
		return NewObject(), nil
	}
	v, err := p.value()
	if err != nil {
		return nil, err
	}
	p.ws()
	if p.i != len(p.s) {
		return nil, fmt.Errorf("unexpected trailing content after byte %d", p.i)
	}
	if v.Inline != "" {
		// A whole document on one line was not laid out by a person; it gets the standard layout.
		v.relayout()
	}
	return v, nil
}

// relayout forgets how v and everything in it were written.
func (v *Value) relayout() {
	v.Inline = ""
	for _, c := range v.Props {
		c.relayout()
	}
	for _, e := range v.Elems {
		e.relayout()
	}
}

type jparser struct {
	s string
	i int
}

func (p *jparser) ws() {
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

func (p *jparser) value() (*Value, error) {
	p.ws()
	if p.i >= len(p.s) {
		return nil, fmt.Errorf("unexpected end of input")
	}
	switch c := p.s[p.i]; {
	case c == '{', c == '[':
		start := p.i
		var v *Value
		var err error
		if c == '{' {
			v, err = p.object()
		} else {
			v, err = p.array()
		}
		if err == nil {
			if raw := p.s[start:p.i]; !strings.Contains(raw, "\n") {
				v.Inline = raw
			}
		}
		return v, err
	case c == '"':
		s, err := p.str()
		return &Value{Kind: String, Str: s}, err
	case strings.HasPrefix(p.s[p.i:], "true"):
		p.i += 4
		return &Value{Kind: Bool, Bool: true}, nil
	case strings.HasPrefix(p.s[p.i:], "false"):
		p.i += 5
		return &Value{Kind: Bool}, nil
	case strings.HasPrefix(p.s[p.i:], "null"):
		p.i += 4
		return &Value{Kind: Null}, nil
	default:
		return p.number()
	}
}

func (p *jparser) object() (*Value, error) {
	v := NewObject()
	p.i++ // {
	p.ws()
	if p.i < len(p.s) && p.s[p.i] == '}' {
		p.i++
		return v, nil
	}
	for {
		p.ws()
		k, err := p.str()
		if err != nil {
			return nil, err
		}
		p.ws()
		if p.i >= len(p.s) || p.s[p.i] != ':' {
			return nil, fmt.Errorf("byte %d: expected : after key", p.i)
		}
		p.i++
		val, err := p.value()
		if err != nil {
			return nil, err
		}
		v.Set(k, val)
		p.ws()
		if p.i >= len(p.s) {
			return nil, fmt.Errorf("object is missing its closing }")
		}
		if p.s[p.i] == ',' {
			p.i++
			continue
		}
		if p.s[p.i] == '}' {
			p.i++
			return v, nil
		}
		return nil, fmt.Errorf("byte %d: expected , or } in object", p.i)
	}
}

func (p *jparser) array() (*Value, error) {
	v := NewArray()
	p.i++ // [
	p.ws()
	if p.i < len(p.s) && p.s[p.i] == ']' {
		p.i++
		return v, nil
	}
	for {
		e, err := p.value()
		if err != nil {
			return nil, err
		}
		v.Elems = append(v.Elems, e)
		p.ws()
		if p.i >= len(p.s) {
			return nil, fmt.Errorf("array is missing its closing ]")
		}
		if p.s[p.i] == ',' {
			p.i++
			continue
		}
		if p.s[p.i] == ']' {
			p.i++
			return v, nil
		}
		return nil, fmt.Errorf("byte %d: expected , or ] in array", p.i)
	}
}

func (p *jparser) str() (string, error) {
	if p.i >= len(p.s) || p.s[p.i] != '"' {
		return "", fmt.Errorf("byte %d: expected a string", p.i)
	}
	p.i++
	var b strings.Builder
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == '"' {
			p.i++
			return b.String(), nil
		}
		if c != '\\' {
			b.WriteByte(c)
			p.i++
			continue
		}
		p.i++
		if p.i >= len(p.s) {
			break
		}
		switch p.s[p.i] {
		case '"', '\\', '/':
			b.WriteByte(p.s[p.i])
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'u':
			if p.i+4 >= len(p.s) {
				return "", fmt.Errorf("byte %d: fewer than four hex digits after \\u", p.i)
			}
			n, err := strconv.ParseUint(p.s[p.i+1:p.i+5], 16, 32)
			if err != nil {
				return "", err
			}
			p.i += 4
			r := rune(n)
			// Surrogate pair: an emoji such as U+1F600 is one character; decoding the
			// halves separately would yield two garbage code points.
			if utf16.IsSurrogate(r) && p.i+6 < len(p.s) && p.s[p.i+1] == '\\' && p.s[p.i+2] == 'u' {
				if n2, err := strconv.ParseUint(p.s[p.i+3:p.i+7], 16, 32); err == nil {
					if dec := utf16.DecodeRune(r, rune(n2)); dec != 0xFFFD {
						r = dec
						p.i += 6
					}
				}
			}
			b.WriteRune(r)
		default:
			return "", fmt.Errorf("byte %d: unrecognized escape \\%c", p.i, p.s[p.i])
		}
		p.i++
	}
	return "", fmt.Errorf("string is missing its closing quote")
}

func (p *jparser) number() (*Value, error) {
	st := p.i
	for p.i < len(p.s) && strings.ContainsRune("-+.eE0123456789", rune(p.s[p.i])) {
		p.i++
	}
	if st == p.i {
		return nil, fmt.Errorf("byte %d: unrecognized value", p.i)
	}
	lit := p.s[st:p.i]
	if _, err := strconv.ParseFloat(lit, 64); err != nil {
		return nil, fmt.Errorf("byte %d: `%s` is not a valid number", st, lit)
	}
	return &Value{Kind: Number, Num: lit}, nil
}
