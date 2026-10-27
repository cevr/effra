package check

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"ergo.local/prototype/internal/sketch/syntax"
)

// Type is a checked type. Named types are nominal: two are identical only
// when they come from one declaration with identical type arguments.
type Type interface {
	String() string
	aType()
}

// BasicKind is the kind of a basic type.
type BasicKind int

// The basic kinds. The untyped kinds are the types of untyped constants
// (Go's model): they take a default type where no other type applies.
const (
	Invalid BasicKind = iota
	Bool
	String
	Int
	Int8
	Int16
	Int32
	Int64
	Uint
	Uint8
	Uint16
	Uint32
	Uint64
	Uintptr
	Float32
	Float64
	Complex64
	Complex128
	Duration
	UntypedBool
	UntypedInt
	UntypedRune
	UntypedFloat
	UntypedString
)

// Basic is a predeclared basic type.
type Basic struct {
	Kind BasicKind
	Name string
}

// Named is a declared type: a struct, an interface (a capability or an
// interface type), a union or a machine. An instance of a generic
// declaration has Orig set and TArgs filled; the origin has TParams.
type Named struct {
	Obj     *TypeName
	Orig    *Named
	TArgs   []Type
	TParams []*TypeParam

	under   Type
	methods map[string]*Func // origin only, keyed: method sets are linear (#122)
	mlist   []*Func
	state   int // 0 unresolved, 1 resolving, 2 resolved
	foreign bool
	ctx     *checker // the checker that created an instance; it substitutes the body lazily
}

// Struct is a struct type.
type Struct struct {
	Fields []*Field
	index  map[string]int
}

// Field is a struct field.
type Field struct {
	Name string
	Type Type
	Pos  syntax.Pos
}

// Interface is an interface: method signatures (a capability when it is
// named), or no methods (any, comparable).
type Interface struct {
	Methods    []*Method
	index      map[string]*Method
	Comparable bool // the predeclared constraint comparable
}

// Method is a method of an interface.
type Method struct {
	Name string
	Sig  *Signature
	Pos  syntax.Pos
}

// Union is a closed set of struct variants.
type Union struct {
	Variants []Type
	// Bad: a variant failed to resolve (reported), so a case that names no
	// listed variant may name the lost one: no case diagnostic follows (D3).
	Bad bool

	// vindex is the variant index (variantindex.go), built on the first
	// lookup and shared by every instance of a generic union.
	vindex *variantIndex
}

// MachineType is a `machine[S, E]` declaration's type.
type MachineType struct {
	State, Signal Type
}

// Slice is []T.
type Slice struct{ Elem Type }

// Array is [N]T.
type Array struct {
	Len  int64
	Elem Type
}

// Map is map[K]V.
type Map struct{ Key, Elem Type }

// Chan is chan T, with a direction.
type Chan struct {
	Dir  syntax.ChanDir
	Elem Type
}

// Pointer is *T, a borrow: only a parameter or receiver type.
type Pointer struct{ Elem Type }

// Signature is a function type: parameters, value results and two rows. A
// declaration's signature also holds its type parameters (bracket-listed
// first, then implicit ones).
type Signature struct {
	TParams  []*TypeParam
	NBracket int
	Params   []*Var
	Variadic bool
	Results  []Type
	// Fallible is true when the signature writes an error row; it decides
	// method-row compatibility (types.md, Interface assignability 1).
	Fallible bool
	Errors   *Row
	Uses     *Row
}

// Kind is the kind of a type parameter (rows-and-constraints §3: by
// position).
type Kind int

// The kinds.
const (
	KindType   Kind = iota // a type
	KindErrors             // an error row
	KindUses               // a capability row
)

func (k Kind) String() string {
	switch k {
	case KindErrors:
		return "an error row"
	case KindUses:
		return "a capability row"
	}
	return "a type"
}

// TypeParam is a type or row parameter.
type TypeParam struct {
	Name       string
	Kind       Kind
	Constraint Type // nil for any
	Pos        syntax.Pos
	Implicit   bool
	conflict   bool // a kind conflict, already reported: resolves to nothing
	id         int
}

// RowArg is a row bound to a row-kinded type parameter: the type argument of
// E in Task[T, E] or a row inferred for E or R.
type RowArg struct{ Row *Row }

// Tuple is the value of a call with several results.
type Tuple struct{ Types []Type }

// noValue is the "type" of an expression with no value, and the binding of a
// result-position type parameter by a block with no value (Q3).
type noValue struct{}

// NoValue is the one no-value type.
var NoValue Type = &noValue{}

func (*Basic) aType()       {}
func (*Named) aType()       {}
func (*Struct) aType()      {}
func (*Interface) aType()   {}
func (*Union) aType()       {}
func (*MachineType) aType() {}
func (*Slice) aType()       {}
func (*Array) aType()       {}
func (*Map) aType()         {}
func (*Chan) aType()        {}
func (*Pointer) aType()     {}
func (*Signature) aType()   {}
func (*TypeParam) aType()   {}
func (*RowArg) aType()      {}
func (*Tuple) aType()       {}
func (*noValue) aType()     {}

// Typ holds the basic types by kind.
var Typ = func() []*Basic {
	names := map[BasicKind]string{
		Invalid: "invalid type", Bool: "bool", String: "string", Int: "int", Int8: "int8", Int16: "int16", Int32: "int32", Int64: "int64",
		Uint: "uint", Uint8: "uint8", Uint16: "uint16", Uint32: "uint32", Uint64: "uint64", Uintptr: "uintptr",
		Float32: "float32", Float64: "float64", Complex64: "complex64", Complex128: "complex128", Duration: "duration",
		UntypedBool: "untyped bool", UntypedInt: "untyped int", UntypedRune: "untyped rune", UntypedFloat: "untyped float", UntypedString: "untyped string",
	}
	out := make([]*Basic, UntypedString+1)
	for k := range out {
		out[k] = &Basic{Kind: BasicKind(k), Name: names[BasicKind(k)]}
	}
	return out
}()

var invalid = Typ[Invalid]

func (t *Basic) String() string { return t.Name }
func (t *Named) String() string {
	name := t.Obj.Name
	if t.Obj.Pkg != nil && t.Obj.Pkg.qualify {
		name = t.Obj.Pkg.Name + "." + name
	}
	if len(t.TArgs) > 0 {
		parts := make([]string, len(t.TArgs))
		for i, a := range t.TArgs {
			parts[i] = a.String()
		}
		name += "[" + strings.Join(parts, ", ") + "]"
	}
	return name
}
func (t *Struct) String() string {
	parts := make([]string, len(t.Fields))
	for i, f := range t.Fields {
		parts[i] = f.Name + " " + f.Type.String()
	}
	return "struct{" + strings.Join(parts, "; ") + "}"
}
func (t *Interface) String() string {
	if t.Comparable {
		return "comparable"
	}
	if len(t.Methods) == 0 {
		return "any"
	}
	parts := make([]string, len(t.Methods))
	for i, m := range t.Methods {
		parts[i] = m.Name + strings.TrimPrefix(m.Sig.String(), "func")
	}
	return "interface{ " + strings.Join(parts, "; ") + " }"
}
func (t *Union) String() string {
	parts := make([]string, len(t.Variants))
	for i, v := range t.Variants {
		parts[i] = v.String()
	}
	return "interface{ " + strings.Join(parts, " | ") + " }"
}
func (t *MachineType) String() string {
	return "machine[" + t.State.String() + ", " + t.Signal.String() + "]"
}
func (t *Slice) String() string   { return "[]" + t.Elem.String() }
func (t *Array) String() string   { return "[" + strconv.FormatInt(t.Len, 10) + "]" + t.Elem.String() }
func (t *Map) String() string     { return "map[" + t.Key.String() + "]" + t.Elem.String() }
func (t *Pointer) String() string { return "*" + t.Elem.String() }
func (t *Chan) String() string {
	switch t.Dir {
	case syntax.ChanSend:
		return "chan<- " + t.Elem.String()
	case syntax.ChanRecv:
		return "<-chan " + t.Elem.String()
	}
	return "chan " + t.Elem.String()
}
func (t *Signature) String() string {
	var b strings.Builder
	b.WriteString("func(")
	for i, p := range t.Params {
		if i > 0 {
			b.WriteString(", ")
		}
		if t.Variadic && i == len(t.Params)-1 {
			b.WriteString("..." + p.Type.(*Slice).Elem.String())
			continue
		}
		b.WriteString(p.Type.String())
	}
	b.WriteString(")")
	results := make([]string, 0, len(t.Results)+1)
	for _, r := range t.Results {
		results = append(results, r.String())
	}
	if t.Errors != nil && !t.Errors.Empty() || t.Fallible {
		results = append(results, "error["+t.Errors.String()+"]")
	}
	switch {
	case len(results) == 1 && len(t.Results) == 1:
		b.WriteString(" " + results[0])
	case len(results) > 0:
		b.WriteString(" (" + strings.Join(results, ", ") + ")")
	}
	if t.Uses != nil && !t.Uses.Empty() {
		b.WriteString(" " + requirementKeyword + " " + t.Uses.list(", "))
	}
	return b.String()
}
func (t *TypeParam) String() string { return t.Name }
func (t *RowArg) String() string    { return t.Row.String() }
func (t *Tuple) String() string {
	parts := make([]string, len(t.Types))
	for i, x := range t.Types {
		parts[i] = x.String()
	}
	return "(" + strings.Join(parts, ", ") + ")"
}
func (*noValue) String() string { return "no value" }

// ---------------------------------------------------------------- rows

// Row is a closed set of error types or capabilities, plus row parameters in
// a generic signature (rows.md: a row is a set; order and repetition carry no
// meaning). Elements are kept sorted by a canonical key, so every operation is
// deterministic.
type Row struct {
	elems []Type
	keys  []string
}

func newRow(elems ...Type) *Row {
	r := &Row{}
	for _, e := range elems {
		r = r.with(e)
	}
	return r
}

// Empty reports whether the row has no element.
func (r *Row) Empty() bool { return r == nil || len(r.elems) == 0 }

// Elems returns the row's elements in canonical order.
func (r *Row) Elems() []Type {
	if r == nil {
		return nil
	}
	return r.elems
}

func (r *Row) find(key string) (int, bool) {
	if r == nil {
		return 0, false
	}
	i := sort.SearchStrings(r.keys, key)
	return i, i < len(r.keys) && r.keys[i] == key
}

// Has reports whether t is an element.
func (r *Row) Has(t Type) bool {
	_, ok := r.find(typeKey(t))
	return ok
}

func (r *Row) with(t Type) *Row {
	if ra, ok := t.(*RowArg); ok {
		return r.union(ra.Row)
	}
	key := typeKey(t)
	i, ok := r.find(key)
	if ok {
		if r == nil {
			return &Row{}
		}
		return r
	}
	out := &Row{}
	if r != nil {
		out.elems = append(append(append(make([]Type, 0, len(r.elems)+1), r.elems[:i]...), t), r.elems[i:]...)
		out.keys = append(append(append(make([]string, 0, len(r.keys)+1), r.keys[:i]...), key), r.keys[i:]...)
	} else {
		out.elems, out.keys = []Type{t}, []string{key}
	}
	return out
}

func (r *Row) union(o *Row) *Row {
	if o.Empty() {
		if r == nil {
			return &Row{}
		}
		return r
	}
	if r.Empty() {
		return o
	}
	out := &Row{elems: make([]Type, 0, len(r.elems)+len(o.elems)), keys: make([]string, 0, len(r.keys)+len(o.keys))}
	i, j := 0, 0
	for i < len(r.keys) || j < len(o.keys) {
		switch {
		case j >= len(o.keys) || i < len(r.keys) && r.keys[i] < o.keys[j]:
			out.elems, out.keys = append(out.elems, r.elems[i]), append(out.keys, r.keys[i])
			i++
		case i >= len(r.keys) || o.keys[j] < r.keys[i]:
			out.elems, out.keys = append(out.elems, o.elems[j]), append(out.keys, o.keys[j])
			j++
		default:
			out.elems, out.keys = append(out.elems, r.elems[i]), append(out.keys, r.keys[i])
			i++
			j++
		}
	}
	return out
}

func (r *Row) minus(o *Row) *Row {
	out := &Row{}
	for i, e := range r.Elems() {
		if !o.Has(e) {
			out.elems, out.keys = append(out.elems, e), append(out.keys, r.keys[i])
		}
	}
	return out
}

// subsetOf reports whether every element of r is in o.
func (r *Row) subsetOf(o *Row) bool {
	for _, e := range r.Elems() {
		if !o.Has(e) {
			return false
		}
	}
	return true
}

func (r *Row) equal(o *Row) bool {
	return len(r.Elems()) == len(o.Elems()) && r.subsetOf(o)
}

func (r *Row) list(sep string) string {
	parts := make([]string, len(r.Elems()))
	for i, e := range r.Elems() {
		parts[i] = e.String()
	}
	return strings.Join(parts, sep)
}

func (r *Row) String() string { return r.list(" | ") }

// typeKey is a canonical identity key of a type, used for row elements,
// instantiation caches and determinism.
func typeKey(t Type) string {
	switch t := t.(type) {
	case *Named:
		orig := t
		if t.Orig != nil {
			orig = t.Orig
		}
		key := orig.Obj.Pkg.Path + "." + orig.Obj.Name + "#" + strconv.Itoa(orig.Obj.id)
		if len(t.TArgs) > 0 {
			parts := make([]string, len(t.TArgs))
			for i, a := range t.TArgs {
				parts[i] = typeKey(a)
			}
			key += "[" + strings.Join(parts, ",") + "]"
		}
		return key
	case *TypeParam:
		return "$" + t.Name + "#" + strconv.Itoa(t.id)
	case *RowArg:
		parts := make([]string, len(t.Row.Elems()))
		copy(parts, t.Row.keys)
		return "{" + strings.Join(parts, "|") + "}"
	case *Slice:
		return "[]" + typeKey(t.Elem)
	case *Array:
		return "[" + strconv.FormatInt(t.Len, 10) + "]" + typeKey(t.Elem)
	case *Map:
		return "map[" + typeKey(t.Key) + "]" + typeKey(t.Elem)
	case *Chan:
		return fmt.Sprintf("chan%d ", t.Dir) + typeKey(t.Elem)
	case *Pointer:
		return "*" + typeKey(t.Elem)
	case *Signature:
		var b strings.Builder
		b.WriteString("func(")
		for _, p := range t.Params {
			b.WriteString(typeKey(p.Type) + ",")
		}
		if t.Variadic {
			b.WriteString("...")
		}
		b.WriteString(")(")
		for _, r := range t.Results {
			b.WriteString(typeKey(r) + ",")
		}
		b.WriteString(")e{" + strings.Join(t.Errors.keysOrNil(), "|") + "}u{" + strings.Join(t.Uses.keysOrNil(), "|") + "}")
		return b.String()
	case *Tuple:
		parts := make([]string, len(t.Types))
		for i, x := range t.Types {
			parts[i] = typeKey(x)
		}
		return "(" + strings.Join(parts, ",") + ")"
	}
	return t.String()
}

func (r *Row) keysOrNil() []string {
	if r == nil {
		return nil
	}
	return r.keys
}

// ---------------------------------------------------------------- helpers

// under returns the underlying type of t: a Named type's declaration body
// (substituted for an instance), and t itself otherwise.
func under(t Type) Type {
	if n, ok := t.(*Named); ok {
		return n.underlying()
	}
	return t
}

func (n *Named) underlying() Type {
	if n.under == nil {
		if n.Orig != nil {
			n.under = n.Orig.instanceUnder(n)
		} else {
			return invalid
		}
	}
	return n.under
}

func (n *Named) origin() *Named {
	if n.Orig != nil {
		return n.Orig
	}
	return n
}

func isBasic(t Type, kinds ...BasicKind) bool {
	b, ok := t.(*Basic)
	if !ok {
		return false
	}
	for _, k := range kinds {
		if b.Kind == k {
			return true
		}
	}
	return false
}

func isUntyped(t Type) bool {
	b, ok := t.(*Basic)
	return ok && b.Kind >= UntypedBool
}

func isInteger(t Type) bool {
	b, ok := under(t).(*Basic)
	return ok && (b.Kind >= Int && b.Kind <= Uintptr || b.Kind == UntypedInt || b.Kind == UntypedRune)
}

func isUnsigned(t Type) bool {
	b, ok := under(t).(*Basic)
	return ok && b.Kind >= Uint && b.Kind <= Uintptr
}

func isFloat(t Type) bool {
	b, ok := under(t).(*Basic)
	return ok && (b.Kind == Float32 || b.Kind == Float64 || b.Kind == UntypedFloat)
}

// isComplex reports a complex type. Complex values have arithmetic and ==,
// no ordering, and convert only to and from complex types (Go's rules; Q5
// admits every Go numeric type, N2). There is no imaginary literal and no
// complex, real or imag builtin, so no untyped complex constant arises.
func isComplex(t Type) bool {
	b, ok := under(t).(*Basic)
	return ok && (b.Kind == Complex64 || b.Kind == Complex128)
}

func isNumeric(t Type) bool { return isInteger(t) || isFloat(t) || isComplex(t) }

func isString(t Type) bool {
	b, ok := under(t).(*Basic)
	return ok && (b.Kind == String || b.Kind == UntypedString)
}

func isBoolean(t Type) bool {
	b, ok := under(t).(*Basic)
	return ok && (b.Kind == Bool || b.Kind == UntypedBool)
}

func isDuration(t Type) bool { return isBasic(t, Duration) }

func isInvalid(t Type) bool { return t == nil || t == Type(invalid) }

// containsInvalid reports whether an invalid type occurs in t's structure
// (not through named types, whose bodies are reported where declared): a
// mismatch involving it would cascade from a reported mistake (D3).
func containsInvalid(t Type) bool {
	switch t := t.(type) {
	case nil:
		return true
	case *Basic:
		return t == invalid
	case *Pointer:
		return containsInvalid(t.Elem)
	case *Slice:
		return containsInvalid(t.Elem)
	case *Array:
		return containsInvalid(t.Elem)
	case *Map:
		return containsInvalid(t.Key) || containsInvalid(t.Elem)
	case *Chan:
		return containsInvalid(t.Elem)
	case *Tuple:
		for _, e := range t.Types {
			if containsInvalid(e) {
				return true
			}
		}
	case *Signature:
		for _, p := range t.Params {
			if containsInvalid(p.Type) {
				return true
			}
		}
		for _, r := range t.Results {
			if containsInvalid(r) {
				return true
			}
		}
	}
	return false
}

func asStruct(t Type) *Struct {
	s, _ := under(t).(*Struct)
	return s
}

func asUnion(t Type) *Union {
	if u, ok := t.(*Union); ok {
		// The binding of a catch arm with several types (rows.md, Catch 7).
		return u
	}
	if _, ok := t.(*Named); !ok {
		return nil
	}
	u, _ := under(t).(*Union)
	return u
}

func asInterface(t Type) *Interface {
	i, _ := under(t).(*Interface)
	return i
}

// isCapability reports whether t is a capability: a named interface with
// method signatures (types.md, Capabilities 1), or the predeclared
// go.Foreign.
func isCapability(t Type) bool {
	n, ok := t.(*Named)
	if !ok {
		return false
	}
	if n.foreign {
		return true
	}
	if len(n.origin().TParams) > 0 {
		return false
	}
	i, ok := n.underlying().(*Interface)
	return ok && len(i.Methods) > 0
}

// isStructNamed reports whether t is a named struct type: a possible error
// type and union variant.
func isStructNamed(t Type) bool {
	n, ok := t.(*Named)
	if !ok {
		return false
	}
	_, ok = n.underlying().(*Struct)
	return ok
}

func (s *Struct) field(name string) (*Field, int) {
	if s.index == nil {
		s.index = make(map[string]int, len(s.Fields))
		for i, f := range s.Fields {
			if _, dup := s.index[f.Name]; !dup {
				s.index[f.Name] = i
			}
		}
	}
	i, ok := s.index[name]
	if !ok {
		return nil, -1
	}
	return s.Fields[i], i
}

func (i *Interface) method(name string) *Method {
	if i.index == nil {
		i.index = make(map[string]*Method, len(i.Methods))
		for _, m := range i.Methods {
			if _, dup := i.index[m.Name]; !dup {
				i.index[m.Name] = m
			}
		}
	}
	return i.index[name]
}

func defaultType(t Type) Type {
	b, ok := t.(*Basic)
	if !ok {
		return t
	}
	switch b.Kind {
	case UntypedBool:
		return Typ[Bool]
	case UntypedInt:
		return Typ[Int]
	case UntypedRune:
		return Typ[Int32]
	case UntypedFloat:
		return Typ[Float64]
	case UntypedString:
		return Typ[String]
	}
	return t
}
