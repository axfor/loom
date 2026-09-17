package loom

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
)

// Pos is a position in a template. Every error carries one, formatted like the Go
// compiler: file:line:col.
//
// Why not hcl.Range: the object syntax has its own lexer with no HCL to lean on, and
// the engine only needs "where", not which syntax the position was read from.
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

func posOf(r hcl.Range) Pos {
	return Pos{File: r.Filename, Line: r.Start.Line, Col: r.Start.Column}
}
