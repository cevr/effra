package check

import (
	"fmt"
	"go/ast"
	"go/token"
	"reflect"
	"regexp"
)

// The differential reads the Go text as the lowering would emit it (196u
// rule 8): every literal expression is converted to the one type Ergo gave
// it, int8(1) for a 1 that took int8, so go/types sees the types the
// checker chose instead of Go's untyped constants. lowerLiterals rewrites
// the Go syntax tree in place, keeping every position, so the converted
// expressions still align with the Ergo text (align_test.go).

// goIsLiteral is isLiteral over Go syntax: a basic literal, true or false,
// a unary or arithmetic operation on literals, and a shift of a literal, in
// parentheses or not. A comparison of two literals is one here too: Ergo
// types it bool, while Go keeps it an untyped bool constant, so the
// lowering converts it as well.
func goIsLiteral(x ast.Expr) bool {
	switch x := x.(type) {
	case *ast.BasicLit:
		return true
	case *ast.Ident:
		return x.Name == "true" || x.Name == "false"
	case *ast.ParenExpr:
		return goIsLiteral(x.X)
	case *ast.UnaryExpr:
		return x.Op != token.AND && goIsLiteral(x.X)
	case *ast.BinaryExpr:
		if x.Op == token.SHL || x.Op == token.SHR {
			return goIsLiteral(x.X)
		}
		return goIsLiteral(x.X) && goIsLiteral(x.Y)
	}
	return false
}

// goLiteralDefault is a Go basic literal's type under 196u rule 2.
func goLiteralDefault(x ast.Expr) string {
	switch x := x.(type) {
	case *ast.BasicLit:
		switch x.Kind {
		case token.INT:
			return "int"
		case token.FLOAT:
			return "float64"
		case token.CHAR:
			return "int32"
		case token.STRING:
			return "string"
		}
	case *ast.Ident:
		return "bool"
	}
	return ""
}

var goTypeName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// lowerLiterals converts each maximal literal expression of f to typeOf's
// answer for it, Ergo's type. An expression Ergo did not type (it is in a
// program Ergo rejects) is converted part by part: each basic literal in
// it to its default, a shift's left operand only. It returns the
// conversions it made, keyed by the call, or an error for a type that is
// not a Go identifier.
func lowerLiterals(f *ast.File, typeOf func(ast.Expr) (string, bool)) (map[*ast.CallExpr]bool, error) {
	made := map[*ast.CallExpr]bool{}
	var failure error
	convert := func(x ast.Expr, t string) ast.Expr {
		if !goTypeName.MatchString(t) {
			failure = fmt.Errorf("a literal of type %s, which is not a Go identifier", t)
			return x
		}
		call := &ast.CallExpr{Fun: &ast.Ident{NamePos: x.Pos(), Name: t}, Lparen: x.Pos(), Args: []ast.Expr{x}, Rparen: x.End() - 1}
		made[call] = true
		return call
	}
	var lower func(x ast.Expr) ast.Expr
	lower = func(x ast.Expr) ast.Expr {
		if t, ok := typeOf(x); ok {
			return convert(x, t)
		}
		switch y := x.(type) {
		case *ast.BasicLit, *ast.Ident:
			return convert(x, goLiteralDefault(x))
		case *ast.ParenExpr:
			y.X = lower(y.X)
		case *ast.UnaryExpr:
			y.X = lower(y.X)
		case *ast.BinaryExpr:
			y.X = lower(y.X)
			if y.Op != token.SHL && y.Op != token.SHR {
				y.Y = lower(y.Y)
			}
		}
		return x
	}
	rewriteExprs(f, func(x ast.Expr) (ast.Expr, bool) {
		if !goIsLiteral(x) {
			return x, true
		}
		return lower(x), false
	})
	return made, failure
}

var (
	astNode = reflect.TypeFor[ast.Node]()
	astExpr = reflect.TypeFor[ast.Expr]()
)

// rewriteExprs visits every expression in an interface-typed field of n's
// syntax tree, outermost first, and stores fn's answer in its place;
// fn's second result says whether to visit the expression's own parts.
func rewriteExprs(n ast.Node, fn func(ast.Expr) (ast.Expr, bool)) {
	var walk func(v reflect.Value)
	visit := func(slot reflect.Value) {
		if slot.IsNil() {
			return
		}
		if slot.Type() == astExpr {
			x, descend := fn(slot.Interface().(ast.Expr))
			slot.Set(reflect.ValueOf(x))
			if !descend {
				return
			}
		}
		walk(slot)
	}
	walk = func(v reflect.Value) {
		for v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return
			}
			v = v.Elem()
		}
		if v.Kind() != reflect.Struct {
			return
		}
		for i := range v.NumField() {
			f := v.Field(i)
			switch {
			case f.Kind() == reflect.Slice && f.Type().Elem().Implements(astNode):
				for j := range f.Len() {
					visit(f.Index(j))
				}
			case (f.Kind() == reflect.Interface || f.Kind() == reflect.Pointer) && f.Type().Implements(astNode):
				visit(f)
			}
		}
	}
	walk(reflect.ValueOf(n))
}
