package lang

// Writing Loom syntax back: names and strings the way a person would write them. Used where the
// build writes into templates (anchor completion) and where messages show a statement to write.

import (
	"strings"
	"unicode"
)

// nameText writes a node name: as an identifier when possible (spaces become _), otherwise as a string.
// Names that contain an underscore or collide with a method name, frontmatter or body are always
// strings, so a reader never has to wonder which one is meant.
func NameText(name string) string {
	if name == "" || strings.Contains(name, "_") || contains(editMethods, name) ||
		name == "as" || name == "frontmatter" || name == "body" {
		return Quote(name)
	}
	id := strings.ReplaceAll(name, " ", "_")
	if strings.Contains(name, "  ") || strings.HasPrefix(name, " ") || strings.HasSuffix(name, " ") || !isIdent(id) {
		return Quote(name)
	}
	return id
}

// Words is a name as the words it is made of, lower-cased: split at _, -, whitespace and where
// camelCase starts a word. Two names with the same words are the same name written two ways, so
// this is what an unquoted name is matched by.
func Words(s string) string {
	rs := []rune(s)
	var out []rune
	gap := func() {
		if len(out) > 0 && out[len(out)-1] != ' ' {
			out = append(out, ' ')
		}
	}
	for i, r := range rs {
		if r == '_' || r == '-' || unicode.IsSpace(r) {
			gap()
			continue
		}
		if unicode.IsUpper(r) && i > 0 {
			prev := rs[i-1]
			next := rune(0)
			if i+1 < len(rs) {
				next = rs[i+1]
			}
			// fooBar, v2Setup, and the S in HTTPServer: a capital that starts a word
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && unicode.IsLower(next)) {
				gap()
			}
		}
		out = append(out, unicode.ToLower(r))
	}
	return strings.TrimSpace(string(out))
}

// NameTextFor writes a node name for a tree that declares a version. An unquoted name is matched
// by its words in every version, so it is written the same way in every version: as NameText does.
func NameTextFor(name, loom string) string {
	return NameText(name)
}

// quote writes a string literal, escaping only what it must: `"` becomes \"; a backslash becomes \\
// only when followed by `"` or a backslash, or at the end. So \. in a regexp is written as is and
// reads like hand-written code.
func Quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch ch := s[i]; ch {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			if i+1 == len(s) || s[i+1] == '"' || s[i+1] == '\\' {
				b.WriteString(`\\`)
			} else {
				b.WriteByte('\\')
			}
		default:
			b.WriteByte(ch)
		}
	}
	b.WriteByte('"')
	return b.String()
}
