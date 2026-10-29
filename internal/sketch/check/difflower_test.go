package check

import (
	"fmt"
	"go/ast"
	"go/token"
	"reflect"
	"regexp"
)

// The differential reads the Go text as the lowering would emit it (196u
// rule 8): every basic literal is converted to the one type Ergo gave it,
// int8(1) for a 1 that took int8, so go/types sees the types the checker
// chose instead of Go's untyped constants. The conversion is per literal,
// never around a constant expression: float64(1) / float64(2) is 0.5, the
// value Ergo computes, where float64(1 / 2) would be 0. A signed literal is
// one literal (operators.go folds the sign, so -128 is an int8): it lowers
// to int8(-128), since -int8(128) overflows. lowerLiterals rewrites the Go
// syntax tree in place, keeping every position, so the converted
// expressions still align with the Ergo text (align_test.go).

// goLiteralUnit reports whether x is one literal of the lowering: a basic
// literal, true or false, or a sign on a basic literal in parentheses or
// not (the form unary in operators.go folds).
func goLiteralUnit(x ast.Expr) bool {
	switch x := x.(type) {
	case *ast.BasicLit:
		return true
	case *ast.Ident:
		return x.Name == "true" || x.Name == "false"
	case *ast.UnaryExpr:
		if x.Op != token.SUB && x.Op != token.ADD {
			return false
		}
		inner := x.X
		for {
			p, ok := inner.(*ast.ParenExpr)
			if !ok {
				break
			}
			inner = p.X
		}
		_, ok := inner.(*ast.BasicLit)
		return ok
	}
	return false
}

// goLiteralDefault is a Go literal's type under 196u rule 2.
func goLiteralDefault(x ast.Expr) string {
	switch x := x.(type) {
	case *ast.UnaryExpr:
		return goLiteralDefault(x.X)
	case *ast.ParenExpr:
		return goLiteralDefault(x.X)
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

// lowerLiterals converts each literal of f (goLiteralUnit) to typeOf's
// answer for it, Ergo's type, or to its default when Ergo did not type it
// (it is in a program Ergo rejects). It returns the conversions it made,
// keyed by the call, or an error for a type that is not a Go identifier.
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
	rewriteExprs(f, func(x ast.Expr) (ast.Expr, bool) {
		if !goLiteralUnit(x) {
			return x, true
		}
		if t, ok := typeOf(x); ok {
			return convert(x, t), false
		}
		return convert(x, goLiteralDefault(x)), false
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
