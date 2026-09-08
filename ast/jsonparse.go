package ast

// JSON 的解析。写自己的而不是用 encoding/json，只为一件事：**记住键的顺序**。

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Parse 读一段 JSON 文本。
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
		return nil, fmt.Errorf("第 %d 字节之后还有多余内容", p.i)
	}
	return v, nil
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
		return nil, fmt.Errorf("内容提前结束")
	}
	switch c := p.s[p.i]; {
	case c == '{':
		return p.object()
	case c == '[':
		return p.array()
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
			return nil, fmt.Errorf("第 %d 字节：键后面要跟 :", p.i)
		}
		p.i++
		val, err := p.value()
		if err != nil {
			return nil, err
		}
		v.Set(k, val)
		p.ws()
		if p.i >= len(p.s) {
			return nil, fmt.Errorf("对象没有收尾的 }")
		}
		if p.s[p.i] == ',' {
			p.i++
			continue
		}
		if p.s[p.i] == '}' {
			p.i++
			return v, nil
		}
		return nil, fmt.Errorf("第 %d 字节：对象里要么是 , 要么是 }", p.i)
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
			return nil, fmt.Errorf("数组没有收尾的 ]")
		}
		if p.s[p.i] == ',' {
			p.i++
			continue
		}
		if p.s[p.i] == ']' {
			p.i++
			return v, nil
		}
		return nil, fmt.Errorf("第 %d 字节：数组里要么是 , 要么是 ]", p.i)
	}
}

func (p *jparser) str() (string, error) {
	if p.i >= len(p.s) || p.s[p.i] != '"' {
		return "", fmt.Errorf("第 %d 字节：这里要一个字符串", p.i)
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
				return "", fmt.Errorf("第 %d 字节：\\u 后面不足四位", p.i)
			}
			n, err := strconv.ParseUint(p.s[p.i+1:p.i+5], 16, 32)
			if err != nil {
				return "", err
			}
			p.i += 4
			r := rune(n)
			// 代理对：😀 是一个字符，分开写会得到两个废码点
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
			return "", fmt.Errorf("第 %d 字节：认不出的转义 \\%c", p.i, p.s[p.i])
		}
		p.i++
	}
	return "", fmt.Errorf("字符串没有收尾的引号")
}

func (p *jparser) number() (*Value, error) {
	st := p.i
	for p.i < len(p.s) && strings.ContainsRune("-+.eE0123456789", rune(p.s[p.i])) {
		p.i++
	}
	if st == p.i {
		return nil, fmt.Errorf("第 %d 字节：认不出的值", p.i)
	}
	lit := p.s[st:p.i]
	if _, err := strconv.ParseFloat(lit, 64); err != nil {
		return nil, fmt.Errorf("第 %d 字节：`%s` 不是合法数字", st, lit)
	}
	return &Value{Kind: Number, Num: lit}, nil
}
