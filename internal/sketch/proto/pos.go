// Package proto is the prototype checker and direct-Go emitter of sketch 9
// (moved unchanged from prototypes/sketch9/sketch: ast.go, check.go,
// emit.go, rt/), behind the bridge FromSyntax from the promoted parser's
// AST (internal/sketch/syntax). It is Ergo's checker and emitter
// until #196 replaces the checker and #199/#207 the emitter and runtime.
//
// The bridge maps the prototype's subset one to one and refuses every other
// node with S109 ("outside the implemented subset") at that node: it never
// drops a node and never reports S001, so a program the checker cannot model
// never builds.
package proto

import "fmt"

// Pos is a 1-based source position; Col counts bytes.
type Pos struct{ Line, Col int }

// tokKind is a literal's kind in the prototype AST.
type tokKind int

const (
	tInt tokKind = iota + 1
	tDuration
	tString
)

// Diagnostic is one checker or bridge message.
type Diagnostic struct {
	Pos  Pos
	Code string
	Msg  string
}

func (d Diagnostic) Format(file string) string {
	return fmt.Sprintf("%s:%d:%d: %s %s", file, d.Pos.Line, d.Pos.Col, d.Code, d.Msg)
}
