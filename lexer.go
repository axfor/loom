package loom

// Lexer shared by templates (object syntax) and loom.lm (one setting per line).
//
// Token kinds: names (base / Install / Install_XSDD), strings "...", literals `...`,
// punctuation . , : ( ) { }, and newlines. Comments (// to end of line) and whitespace
// are dropped here; only newlines are kept, because a newline ends a statement.

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

type tkind int

const (
	kIdent  tkind = iota
	kString       // "..."
	kRaw          // `...`
	kDot
	kComma
	kColon
	kLParen
	kRParen
	kLBrace
	kRBrace
	kNewline
	kEOF
)

type tok struct {
	kind tkind
	text string // source text for names; the unquoted value for strings and literals
	pos  Pos
	off  int // byte range [off, end) in the source; used when rewriting the template
	end  int
}

func (t tok) String() string {
	switch t.kind {
	case kIdent:
		return "`" + t.text + "`"
	case kString:
		return fmt.Sprintf("%q", t.text)
	case kRaw:
		return "literal"
	case kNewline:
		return "newline"
	case kEOF:
		return "end of file"
	}
	return "`" + t.text + "`"
}

func lexLoom(file string, src []byte) ([]tok, error) {
	s := string(src)
	var out []tok
	line, col, i := 1, 1, 0
	adv := func() rune {
		r, w := utf8.DecodeRuneInString(s[i:])
		i += w
		if r == '\n' {
			line++
			col = 1
		} else {
			col++
		}
		return r
	}
	peek := func(k int) byte {
		if i+k < len(s) {
			return s[i+k]
		}
		return 0
	}
	for i < len(s) {
		here := Pos{File: file, Line: line, Col: col}
		r, _ := utf8.DecodeRuneInString(s[i:])
		startOff, n := i, len(out)
		switch {
		case r == ' ' || r == '\t' || r == '\r':
			adv()
		case r == '\n':
			adv()
			out = append(out, tok{kind: kNewline, text: "\n", pos: here})
		case r == '/' && peek(1) == '/':
			for i < len(s) && s[i] != '\n' {
				adv()
			}
		case r == '"':
			adv()
			var b strings.Builder
			closed := false
			for i < len(s) {
				c := s[i]
				if c == '"' {
					adv()
					closed = true
					break
				}
				if c == '\n' {
					break
				}
				// Only \" and \\ are escapes; any other backslash is kept as is, so a regex \. need not be written \\.
				if c == '\\' && (peek(1) == '"' || peek(1) == '\\') {
					b.WriteByte(peek(1))
					adv()
					adv()
					continue
				}
				b.WriteRune(adv())
			}
			if !closed {
				return nil, fmt.Errorf("%s: unterminated string (strings cannot span lines; use backticks `...` for multi-line content)", here)
			}
			out = append(out, tok{kind: kString, text: b.String(), pos: here})
		case r == '`':
			adv()
			start := i
			for i < len(s) && s[i] != '`' {
				adv()
			}
			if i >= len(s) {
				return nil, fmt.Errorf("%s: unterminated literal: missing closing backtick", here)
			}
			body := s[start:i]
			adv()
			out = append(out, tok{kind: kRaw, text: dedent(body), pos: here})
		case r == '.':
			adv()
			out = append(out, tok{kind: kDot, text: ".", pos: here})
		case r == ',':
			adv()
			out = append(out, tok{kind: kComma, text: ",", pos: here})
		case r == ':':
			adv()
			out = append(out, tok{kind: kColon, text: ":", pos: here})
		case r == '(':
			adv()
			out = append(out, tok{kind: kLParen, text: "(", pos: here})
		case r == ')':
			adv()
			out = append(out, tok{kind: kRParen, text: ")", pos: here})
		case r == '{':
			adv()
			out = append(out, tok{kind: kLBrace, text: "{", pos: here})
		case r == '}':
			adv()
			out = append(out, tok{kind: kRBrace, text: "}", pos: here})
		case r == '_' || unicode.IsLetter(r):
			start := i
			for i < len(s) {
				c, _ := utf8.DecodeRuneInString(s[i:])
				if c != '_' && !unicode.IsLetter(c) && !unicode.IsDigit(c) {
					break
				}
				adv()
			}
			out = append(out, tok{kind: kIdent, text: s[start:i], pos: here})
		default:
			return nil, fmt.Errorf("%s: unexpected `%c` — names with spaces or punctuation must be quoted", here, r)
		}
		if len(out) > n {
			out[n].off, out[n].end = startOff, i
		}
	}
	end := Pos{File: file, Line: line, Col: col}
	out = append(out, tok{kNewline, "\n", end, len(s), len(s)}, tok{kEOF, "", end, len(s), len(s)})
	return out, nil
}

// dedent tidies a multi-line literal: it drops leading and trailing blank lines and
// strips the common indentation, so a literal in a block can follow the code's
// indentation without that indentation leaking into the product.
func dedent(s string) string {
	if !strings.Contains(s, "\n") {
		return s
	}
	lines := strings.Split(s, "\n")
	if strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	if n := len(lines); n > 0 && strings.TrimSpace(lines[n-1]) == "" {
		lines = lines[:n-1]
	}
	indent := -1
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		w := len(l) - len(strings.TrimLeft(l, " \t"))
		if indent < 0 || w < indent {
			indent = w
		}
	}
	for k, l := range lines {
		if len(l) >= indent && indent > 0 {
			lines[k] = l[indent:]
		} else {
			lines[k] = strings.TrimLeft(l, " \t")
		}
	}
	return strings.Join(lines, "\n")
}
