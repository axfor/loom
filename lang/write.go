package lang

// Writing Loom syntax back: names and strings the way a person would write them. Used where the
// build writes into templates (anchor completion) and where messages show a statement to write.

import "strings"

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
