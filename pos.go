package loom

import "fmt"

// Pos is a position in a template. Every error carries one, formatted like the Go
// compiler: file:line:col.
type Pos struct {
	File string
	Line int
	Col  int
}

func (p Pos) String() string {
	if p.Line == 0 {
		return p.File
	}
	return fmt.Sprintf("%s:%d:%d", p.File, p.Line, p.Col)
}
