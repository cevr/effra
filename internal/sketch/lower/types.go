package lower

import (
	"go/constant"
	"strconv"
	"strings"

	"ergo.local/prototype/internal/sketch/check"
	"ergo.local/prototype/internal/sketch/syntax"
)

// typeCtx spells checked types in Go inside one declaration, whose type
// parameters it names.
type typeCtx struct {
	l      *lowerer
	params map[*check.TypeParam]string
	site   syntax.Node // where a refusal of a type is located
}

// goType is t's Go type. Basic types keep their Go names (duration is
// time.Duration); a declared type is its lowered declaration with its
// type-kinded arguments; a function type is the uniform function value
// (funcType).
func (tc *typeCtx) goType(t check.Type) string {
	switch t := t.(type) {
	case *check.Basic:
		switch t.Kind {
		case check.Invalid:
			ice(tc.site, "an invalid type reached lowering")
		case check.Duration:
			return tc.l.timePkg() + ".Duration"
		}
		return t.Name
	case *check.Named:
		td := tc.l.useType(tc.l.typeDecl(t.Obj, tc.site))
		origin := t
		if t.Orig != nil {
			origin = t.Orig
		}
		var args []string
		for i, a := range t.TArgs {
			if i < len(origin.TParams) && origin.TParams[i].Kind == check.KindType {
				args = append(args, tc.goType(a))
			}
		}
		if len(args) == 0 {
			return td.name
		}
		return td.name + "[" + strings.Join(args, ", ") + "]"
	case *check.Slice:
		return "[]" + tc.goType(t.Elem)
	case *check.Map:
		return "map[" + tc.goType(t.Key) + "]" + tc.goType(t.Elem)
	case *check.Pointer:
		return "*" + tc.goType(t.Elem)
	case *check.Signature:
		return tc.funcType(t)
	case *check.TypeParam:
		if name, ok := tc.params[t]; ok {
			return name
		}
		ice(tc.site, "the type parameter %s appears outside its declaration", t.Name)
	case *check.Tuple:
		// A tuple is structural (196t): an unnamed Go struct, identical
		// wherever its element types are, and comparable when they are.
		fields := make([]string, len(t.Types))
		for i, e := range t.Types {
			fields[i] = tupleField(i) + " " + tc.goType(e)
		}
		return "struct{ " + strings.Join(fields, "; ") + " }"
	case *check.Union:
		// The row value of `case err:` (D-C3) is a Go error holding one of
		// its error types.
		if tc.l.isErrorValue(t) {
			return "error"
		}
		ice(tc.site, "the anonymous union %s is not a row value; the checker builds no other", t)
	case *check.Interface:
		refuse(tc.site, "the type %s is lowered after check-v1 stage E (no any, constraints)", t)
	}
	refuse(tc.site, "the type %s is not lowered yet", t)
	return ""
}

// funcType is the Go type of an Ergo function value. Every function value
// has one shape whatever its rows: it takes the environment first and
// returns an error last, so a row parameter never changes a Go type and a
// generic function over rows needs no specialised copy (spec D5). A call of
// a declared function by name uses the declaration's own signature, which
// has neither when its rows are empty.
func (tc *typeCtx) funcType(s *check.Signature) string {
	parts := []string{"*" + tc.l.env()}
	for _, p := range s.Params {
		parts = append(parts, tc.goType(p.Type))
	}
	var result check.Type
	if len(s.Results) > 1 {
		refuse(tc.site, "a function type with several results is a tuple, lowered after check-v1 stage B (196t)")
	}
	if len(s.Results) == 1 {
		result = s.Results[0]
	}
	return "func(" + strings.Join(parts, ", ") + ")" + tc.resultList(result, true)
}

// resultList is a Go signature's results: the value, then the error when
// fallible.
func (tc *typeCtx) resultList(result check.Type, fallible bool) string {
	switch {
	case result != nil && fallible:
		return " (" + tc.goType(result) + ", error)"
	case result != nil:
		return " " + tc.goType(result)
	case fallible:
		return " error"
	}
	return ""
}

// methodSig is an operation's Go method signature, after its name.
func (tc *typeCtx) methodSig(s *check.Signature) string {
	params := make([]string, len(s.Params))
	for i, p := range s.Params {
		params[i] = tc.goType(p.Type)
	}
	if len(s.Results) > 1 {
		refuse(tc.site, "an operation with several results is a tuple, lowered after check-v1 stage B (196t)")
	}
	var result check.Type
	if len(s.Results) == 1 {
		result = s.Results[0]
	}
	return "(" + strings.Join(params, ", ") + ")" + tc.resultList(result, len(rowElems(s.Errors)) > 0)
}

// zero is the zero value of t in Go, for the value slot of a failure
// return.
func (tc *typeCtx) zero(t check.Type) string {
	if b, ok := t.(*check.Basic); ok {
		switch {
		case b.Kind == check.Bool:
			return "false"
		case b.Kind == check.String:
			return `""`
		case b.Kind == check.Duration:
			return "0"
		case b.Kind >= check.Int && b.Kind <= check.Complex128:
			return "0"
		}
	}
	return "*new(" + tc.goType(t) + ")"
}

// defaultValue is the default of t in Go, for a let without a value and for
// a record field a literal omits (types.md, Zero values): Go's zero value,
// except that a Map is an empty, writable Map, never Go's nil map, which
// panics on a write. A Map inside a record or a tuple is one too, at any
// depth. The empty value is "" when Go's zero value is the default, so a
// declaration can omit it.
func (tc *typeCtx) defaultValue(t check.Type) string {
	if !holdsMap(t, map[*check.Named]bool{}) {
		return ""
	}
	return tc.emptyValue(t)
}

// emptyValue is defaultValue's Go text for a type that holds a Map.
func (tc *typeCtx) emptyValue(t check.Type) string {
	goType := tc.goType(t)
	switch u := t.(type) {
	case *check.Map:
		return goType + "{}"
	case *check.Tuple:
		var parts []string
		for i, e := range u.Types {
			if v := tc.defaultValue(e); v != "" {
				parts = append(parts, tupleField(i)+": "+v)
			}
		}
		return goType + "{" + strings.Join(parts, ", ") + "}"
	case *check.Named:
		switch under := u.Underlying().(type) {
		case *check.Map:
			return goType + "{}"
		case *check.Struct:
			td := tc.l.typeDecl(u.Obj, tc.site)
			parts := tc.fieldDefaults(td, under, nil)
			return goType + "{" + strings.Join(parts, ", ") + "}"
		}
	}
	ice(tc.site, "the type %s holds a Map but has no empty value", t)
	return ""
}

// fieldDefaults is the keyed Go elements that set each field of the record
// s, not in set, whose default is not Go's zero value.
func (tc *typeCtx) fieldDefaults(td *typeDecl, s *check.Struct, set map[string]bool) []string {
	var parts []string
	for _, f := range s.Fields {
		if set[f.Name] {
			continue
		}
		if v := tc.defaultValue(f.Type); v != "" {
			name, ok := td.fieldName[f.Name]
			if !ok {
				ice(tc.site, "%s has no field %s", td.name, f.Name)
			}
			parts = append(parts, name+": "+v)
		}
	}
	return parts
}

// holdsMap reports whether t's default holds a Map: t is a Map, or a record
// or tuple with a field that holds one. A slice's default is empty, so its
// elements hold nothing. A type parameter has no default (the checker
// refuses it).
func holdsMap(t check.Type, seen map[*check.Named]bool) bool {
	switch u := t.(type) {
	case *check.Map:
		return true
	case *check.Tuple:
		for _, e := range u.Types {
			if holdsMap(e, seen) {
				return true
			}
		}
	case *check.Named:
		if seen[u] {
			return false
		}
		seen[u] = true
		switch under := u.Underlying().(type) {
		case *check.Map:
			return true
		case *check.Struct:
			for _, f := range under.Fields {
				if holdsMap(f.Type, seen) {
					return true
				}
			}
		}
	}
	return false
}

// constant is a checked constant of type t in Go: the exact value, converted
// to t's Go type, so no Go untyped-constant rule picks another type (196u,
// spec D4). A float is written as an exact fraction, so float32 rounds once,
// from the exact value.
func (tc *typeCtx) constant(t check.Type, v constant.Value, site syntax.Node) string {
	goType := tc.goType(t)
	// A defined type over a basic type keeps its own name: Flag(true).
	basic, _ := t.(*check.Basic)
	nominal := false
	if n, ok := t.(*check.Named); ok {
		basic, _ = n.Underlying().(*check.Basic)
		nominal = true
	}
	switch v.Kind() {
	// An untyped Go string or bool constant becomes bool or string as a
	// bare literal; a defined type over one is written as a conversion, so
	// the constant keeps its nominal type.
	case constant.Bool:
		if basic == nil || basic.Kind != check.Bool {
			ice(site, "a bool constant of type %s", t)
		}
		if nominal {
			return goType + "(" + strconv.FormatBool(constant.BoolVal(v)) + ")"
		}
		return strconv.FormatBool(constant.BoolVal(v))
	case constant.String:
		if basic == nil || !isString(basic) {
			ice(site, "a string constant of type %s", t)
		}
		if nominal {
			return goType + "(" + strconv.Quote(constant.StringVal(v)) + ")"
		}
		return strconv.Quote(constant.StringVal(v))
	case constant.Int:
		return goType + "(" + v.ExactString() + ")"
	case constant.Float:
		if isInteger(t) {
			if i := constant.ToInt(v); i.Kind() == constant.Int {
				return goType + "(" + i.ExactString() + ")"
			}
		}
		return goType + "(" + exactFloat(v) + ")"
	case constant.Complex:
		return goType + "(complex(" + exactFloat(constant.Real(v)) + ", " + exactFloat(constant.Imag(v)) + "))"
	}
	ice(site, "the constant %s has no Go spelling", v)
	return ""
}

// exactFloat spells a float constant exactly as Go untyped constant
// arithmetic, num.0/den.
func exactFloat(v constant.Value) string {
	if v.Kind() == constant.Int {
		return v.ExactString() + ".0"
	}
	num, den := constant.Num(v), constant.Denom(v)
	if den.ExactString() == "1" {
		return "(" + num.ExactString() + ".0)"
	}
	return "(" + num.ExactString() + ".0 / " + den.ExactString() + ")"
}

func isInteger(t check.Type) bool {
	b, ok := t.(*check.Basic)
	return ok && b.Kind >= check.Int && b.Kind <= check.Uintptr
}

// isErrorValue reports a value of an error type: returning it is a failure
// (types.md, Error types 2). The checker's ErrorTypes of the type's
// package, and of this package, decide it.
func (l *lowerer) isErrorValue(t check.Type) bool {
	if u, row := t.(*check.Union); row {
		// A row value (D-C3): every variant is an error type.
		for _, v := range u.Variants {
			if !l.isErrorValue(v) {
				return false
			}
		}
		return len(u.Variants) > 0
	}
	n, ok := t.(*check.Named)
	if !ok {
		return false
	}
	origin := n
	if n.Orig != nil {
		origin = n.Orig
	}
	if l.pkg.ErrorTypes[origin] {
		return true
	}
	return origin.Obj.Pkg != nil && origin.Obj.Pkg.ErrorTypes[origin]
}
