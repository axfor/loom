package ast

import "strings"

// Text: no structure. An anchor is the **exact text** of a line — no fuzzy matching:
// multiple matches are always an error; better to make people write a more precise
// anchor than to silently insert at the first match.
type Text struct{ lines []string }

func NewText(t string) *Text                 { return &Text{lines: strings.Split(t, "\n")} }
func (t *Text) Kinds() []string              { return Kinds("text") }
func (t *Text) Lines() []string              { return t.lines }
func (t *Text) Text() string                 { return strings.Join(t.lines, "\n") }
func (t *Text) BodyOf(string) (string, bool) { return "", false }
func (t *Text) SetBody(string, string) bool  { return false }

func (t *Text) Splice(s, e int, repl []string) { t.lines = splice(t.lines, s, e, repl) }

func (t *Text) Find(kind, anchor string) [][2]int {
	var out [][2]int
	for i, l := range t.lines {
		if l == anchor {
			out = append(out, [2]int{i, i + 1})
		}
	}
	return out
}
