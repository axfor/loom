package lang

// Lexer shared by templates (object syntax) and loom.om (one setting per line).
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

type Kind int

const (
	KIdent  Kind = iota
	KString      // "..."
	KRaw         // `...`
	KDot
	KComma
	KColon
	KLParen
	KRParen
	KLBrace
	KRBrace
	KNewline
	KEOF
)

// Tok is one token. The fields are public: rewriting a template's text (adding an anchor argument
// to a statement, say) needs the byte range a token covers.
type Tok struct {
	Kind Kind
	Text string // source text for names; the unquoted value for strings and literals
	Pos  Pos
	Tag  string // the language written after an opening fence, if any
	Off  int    // byte range [Off, End) in the source
	End  int
}

func (t Tok) String() string {
	switch t.Kind {
	case KIdent:
		return "`" + t.Text + "`"
	case KString:
		return fmt.Sprintf("%q", t.Text)
	case KRaw:
		return "literal"
	case KNewline:
		return "newline"
	case KEOF:
		return "end of file"
	}
	return "`" + t.Text + "`"
}

func Lex(file string, src []byte) ([]Tok, error) {
	s := string(src)
	var out []Tok
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
			out = append(out, Tok{Kind: KNewline, Text: "\n", Pos: here})
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
			out = append(out, Tok{Kind: KString, Text: b.String(), Pos: here})
		case r == '`':
			// Three or more backticks open a fence, closed by as many again: the content is
			// taken as it is, so a document with inline `code` or a nested ``` block in it can
			// be written here. A single backtick keeps the older, shorter form, which ends at
			// the next backtick and therefore cannot hold either.
			n := 0
			for i+n < len(s) && s[i+n] == '`' {
				n++
			}
			if n >= 3 {
				for k := 0; k < n; k++ {
					adv()
				}
				// A language tag after the opening fence says what the content is; it is not
				// part of the content.
				tagStart := i
				for i < len(s) && s[i] != '\n' {
					adv()
				}
				tag := strings.TrimSpace(s[tagStart:i])
				if i < len(s) {
					adv() // the newline
				}
				start := i
				fence := strings.Repeat("`", n)
				end := -1
				for j := i; j < len(s); j++ {
					if s[j] != '`' || (j > 0 && s[j-1] != '\n') {
						continue
					}
					k := j
					for k < len(s) && s[k] == '`' {
						k++
					}
					if k-j == n {
						end = j
						break
					}
				}
				if end < 0 {
					return nil, fmt.Errorf("%s: unterminated fence: missing a closing %s at the start of a line", here, fence)
				}
				body := s[start:end]
				for i < end {
					adv()
				}
				for k := 0; k < n; k++ {
					adv()
				}
				out = append(out, Tok{Kind: KRaw, Text: dedent(strings.TrimSuffix(body, "\n")), Pos: here, Tag: tag})
				break
			}
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
			out = append(out, Tok{Kind: KRaw, Text: dedent(body), Pos: here})
		case r == '.':
			adv()
			out = append(out, Tok{Kind: KDot, Text: ".", Pos: here})
		case r == ',':
			adv()
			out = append(out, Tok{Kind: KComma, Text: ",", Pos: here})
		case r == ':':
			adv()
			out = append(out, Tok{Kind: KColon, Text: ":", Pos: here})
		case r == '(':
			adv()
			out = append(out, Tok{Kind: KLParen, Text: "(", Pos: here})
		case r == ')':
			adv()
			out = append(out, Tok{Kind: KRParen, Text: ")", Pos: here})
		case r == '{':
			adv()
			out = append(out, Tok{Kind: KLBrace, Text: "{", Pos: here})
		case r == '}':
			adv()
			out = append(out, Tok{Kind: KRBrace, Text: "}", Pos: here})
		case r == '_' || unicode.IsLetter(r):
			start := i
			for i < len(s) {
				c, _ := utf8.DecodeRuneInString(s[i:])
				if c != '_' && !unicode.IsLetter(c) && !unicode.IsDigit(c) {
					break
				}
				adv()
			}
			out = append(out, Tok{Kind: KIdent, Text: s[start:i], Pos: here})
		default:
			return nil, fmt.Errorf("%s: unexpected `%c` — names with spaces or punctuation must be quoted", here, r)
		}
		if len(out) > n {
			out[n].Off, out[n].End = startOff, i
		}
	}
	end := Pos{File: file, Line: line, Col: col}
	out = append(out, Tok{Kind: KNewline, Text: "\n", Pos: end, Off: len(s), End: len(s)}, Tok{Kind: KEOF, Pos: end, Off: len(s), End: len(s)})
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
