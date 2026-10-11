// Package gosyntax compares Go programs up to layout and comments.
package gosyntax

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
)

// Tree prints the syntax tree Go parses from src without comments, and with
// every position reduced to whether it is set. Two programs print alike
// exactly when they differ only in layout and comments.
//
// Unlike a token stream, the tree keeps the semicolons that carry meaning,
// such as the one in switch f(); { ... }. Positions cannot simply be dropped:
// go/ast records some syntax only as a position, such as a type alias's =
// (TypeSpec.Assign) and a call's trailing ... (CallExpr.Ellipsis).
func Tree(src []byte) (string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", src, parser.SkipObjectResolution)
	if err != nil {
		return "", err
	}
	setPositions(reflect.ValueOf(file))
	var tree strings.Builder
	if err := ast.Fprint(&tree, nil, file, nil); err != nil {
		return "", err
	}
	return tree.String(), nil
}

var positionType = reflect.TypeFor[token.Pos]()

// setPositions replaces every set position under value with the same set
// position, keeping unset ones unset. Without object resolution the tree has
// no cycles.
func setPositions(value reflect.Value) {
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !value.IsNil() {
			setPositions(value.Elem())
		}
	case reflect.Slice, reflect.Array:
		for i := range value.Len() {
			setPositions(value.Index(i))
		}
	case reflect.Struct:
		for i := range value.NumField() {
			setPositions(value.Field(i))
		}
	case reflect.Int:
		if value.Type() == positionType && value.CanSet() && token.Pos(value.Int()).IsValid() {
			value.SetInt(1)
		}
	}
}
