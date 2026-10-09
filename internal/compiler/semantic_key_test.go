package compiler

import (
	"fmt"
	"strings"
	"testing"
)

// The interning key's spelling is part of the arena's identity: the
// append-built key must match the original formatted spelling exactly.
func TestSemanticNodeKeyKeepsItsSpelling(t *testing.T) {
	formatted := func(kind, name, declaration, mode string, args []TypeID, result TypeID, failure, service RowID) string {
		var key strings.Builder
		for _, field := range [][2]string{{"kind", kind}, {"name", name}, {"decl", declaration}, {"mode", mode}} {
			fmt.Fprintf(&key, "%s%d:%s;", field[0], len(field[1]), field[1])
		}
		fmt.Fprintf(&key, "args%d:", len(args))
		for _, arg := range args {
			fmt.Fprintf(&key, "%d,", arg)
		}
		fmt.Fprintf(&key, ";result%d;failure%d;service%d;", result, failure, service)
		return key.String()
	}
	for _, c := range []struct {
		kind, name, declaration, mode string
		args                          []TypeID
		result                        TypeID
		failure, service              RowID
	}{
		{},
		{kind: "primitive", name: "i64"},
		{kind: "callable", mode: "effect", args: []TypeID{1, 22, 4294967295}, result: 7, failure: 3, service: 4294967295},
		{kind: "record", name: "Grüße;:,", declaration: "decl:1;x", args: []TypeID{0}},
	} {
		got := semanticNodeKey(c.kind, c.name, c.declaration, c.mode, c.args, c.result, c.failure, c.service)
		if want := formatted(c.kind, c.name, c.declaration, c.mode, c.args, c.result, c.failure, c.service); got != want {
			t.Fatalf("key = %q, want %q", got, want)
		}
	}
}
