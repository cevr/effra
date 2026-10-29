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
	// byName holds the methods sorted by name, once, when the type is
	// resolved (go/types NewInterfaceType): identity reads it and never
	// sorts (round 11 M3).
	byName []*Method
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
	// unchecked: its constraint is a decided one this checker does not
	// check yet (S109 at the constraint): a body that declares it is not
	// checked, so nothing cascades from the missing operations (D3).
	unchecked bool
	id        int
}

// RowVar is the inferred row of a function that writes no row (class F):
// the error row or the capability row of an unexported function, one
// variable each, as the only element of the signature's row. Types never
// change: inference writes the row it solves into the variable's side slot,
// never into the signature. While the function's component is inferred the
// variable is unbound, and a type that holds it keys no cache that outlives
// the inference (intern.go). When the component is final the variable is
// bound for good, and resolving a row replaces it by its solution.
type RowVar struct {
	Fn    *Func
	Kind  Kind // KindErrors or KindUses
	sol   *Row // the solution so far (final when final is set); no variable
	final bool

	// A use of a generic function's variable (a call, an explicit
	// instantiation, a saved value) is the variable of with a delayed
	// substitution m of the function's type parameters: its row is the
	// solution with m applied (round 12 M1; rustc EarlyBinder). ctx is the
	// check that made the use.
	of  *RowVar
	m   map[*TypeParam]Type
	ctx *checker
}

// root is the variable itself, for a use.
func (v *RowVar) root() *RowVar {
	if v.of != nil {
		return v.of
	}
	return v
}

// Solution returns the variable's solved row (final after the check), with
// a use's substitution applied.
func (v *RowVar) Solution() *Row {
	if v.of != nil && v.ctx != nil {
		r, _ := v.ctx.types.varRow(v)
		return r
	}
	return v.root().sol
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
func (*RowVar) aType()      {}

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
	if errs := t.Errors.String(); errs != "" || t.Fallible {
		results = append(results, "error["+errs+"]")
	}
	switch {
	case len(results) == 1 && len(t.Results) == 1:
		b.WriteString(" " + results[0])
	case len(results) > 0:
		b.WriteString(" (" + strings.Join(results, ", ") + ")")
	}
	if uses := t.Uses.list(", "); uses != "" {
		b.WriteString(" " + requirementKeyword + " " + uses)
	}
	return b.String()
}
func (t *TypeParam) String() string { return t.Name }
func (v *RowVar) String() string {
	if sol := v.Solution(); sol != nil {
		return sol.String()
	}
	return "the inferred row of " + v.root().Fn.Name
}
func (t *RowArg) String() string { return t.Row.String() }
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
// meaning). Elements are kept sorted by their interned ids (intern.go), so
// set operations merge in linear time and dedup by id. Every operation that
// may intern takes the check's table; a row built by another check is
// re-interned in this one, never written to. Printing sorts the elements by
// typeString (String, list), so output does not follow id order.
type Row struct {
	elems []Type
	ids   []int32
	tab   *typeTable // the table of ids; nil for an empty row
	rid   int32      // the row's own id in tab, once computed and final
	// vars: an element is a row variable (class F). infer: an element's id
	// was built while a variable in it was unbound, so it holds only until
	// that variable's component is final (intern.go local).
	vars, infer bool
	gen         int32 // the table's inference generation when the ids were built
}

func newRow() *Row { return &Row{} }

// Empty reports whether the row has no element.
func (r *Row) Empty() bool { return r == nil || len(r.elems) == 0 }

// Elems returns the row's elements in the check's identity order, which
// depends on the source alone. Print them with String.
func (r *Row) Elems() []Type {
	if r == nil {
		return nil
	}
	return r.elems
}

func (r *Row) find(id int32) (int, bool) {
	if r == nil {
		return 0, false
	}
	i := sort.Search(len(r.ids), func(i int) bool { return r.ids[i] >= id })
	return i, i < len(r.ids) && r.ids[i] == id
}

// has reports whether t is an element.
func (r *Row) has(tab *typeTable, t Type) bool {
	r = tab.set(r)
	tab.charge(1)
	_, ok := r.find(tab.id(t))
	return ok
}

func (r *Row) with(tab *typeTable, t Type) *Row {
	if ra, ok := t.(*RowArg); ok {
		return r.union(tab, ra.Row)
	}
	r = tab.local(r)
	id, inf := tab.intern(t)
	i, ok := r.find(id)
	if ok {
		return r
	}
	tab.charge(1 + len(r.Elems()))
	_, isVar := t.(*RowVar)
	out := &Row{tab: tab, gen: tab.gen, vars: isVar, infer: inf}
	if r != nil {
		out.vars, out.infer = out.vars || r.vars, out.infer || r.infer
		out.elems = append(append(append(make([]Type, 0, len(r.elems)+1), r.elems[:i]...), t), r.elems[i:]...)
		out.ids = append(append(append(make([]int32, 0, len(r.ids)+1), r.ids[:i]...), id), r.ids[i:]...)
	} else {
		out.elems, out.ids = []Type{t}, []int32{id}
	}
	return out
}

func (r *Row) union(tab *typeTable, o *Row) *Row {
	if o.Empty() {
		if r == nil {
			return &Row{}
		}
		return r
	}
	if r.Empty() {
		return o
	}
	r, o = tab.local(r), tab.local(o)
	tab.charge(len(r.ids) + len(o.ids))
	out := &Row{tab: tab, gen: tab.gen, vars: r.vars || o.vars, infer: r.infer || o.infer, elems: make([]Type, 0, len(r.elems)+len(o.elems)), ids: make([]int32, 0, len(r.ids)+len(o.ids))}
	i, j := 0, 0
	for i < len(r.ids) || j < len(o.ids) {
		switch {
		case j >= len(o.ids) || i < len(r.ids) && r.ids[i] < o.ids[j]:
			out.elems, out.ids = append(out.elems, r.elems[i]), append(out.ids, r.ids[i])
			i++
		case i >= len(r.ids) || o.ids[j] < r.ids[i]:
			out.elems, out.ids = append(out.elems, o.elems[j]), append(out.ids, o.ids[j])
			j++
		default:
			out.elems, out.ids = append(out.elems, r.elems[i]), append(out.ids, r.ids[i])
			i++
			j++
		}
	}
	return out
}

// minus returns the elements of r that are not in o.
func (r *Row) minus(tab *typeTable, o *Row) *Row {
	out := &Row{}
	if r.Empty() {
		return out
	}
	r, o = tab.set(r), tab.set(o)
	tab.charge(len(r.Elems()) + len(o.Elems()))
	j := 0
	for i, id := range r.ids {
		for o != nil && j < len(o.ids) && o.ids[j] < id {
			j++
		}
		if o != nil && j < len(o.ids) && o.ids[j] == id {
			continue
		}
		out.tab, out.gen, out.vars, out.infer = tab, tab.gen, r.vars, r.infer
		out.elems, out.ids = append(out.elems, r.elems[i]), append(out.ids, id)
	}
	return out
}

// without returns the elements of r whose ids are not in drop: minus
// against a hash set of ids, linear in r (a catch's arms, handlers.go).
func (r *Row) without(tab *typeTable, drop map[int32]bool) *Row {
	out := &Row{}
	if r.Empty() {
		return out
	}
	r = tab.set(r)
	tab.charge(len(r.Elems()))
	for i, id := range r.ids {
		if drop[id] {
			continue
		}
		out.tab, out.gen, out.vars, out.infer = tab, tab.gen, r.vars, r.infer
		out.elems, out.ids = append(out.elems, r.elems[i]), append(out.ids, id)
	}
	return out
}

// subsetOf reports whether every element of r is in o.
func (r *Row) subsetOf(tab *typeTable, o *Row) bool {
	return r.minus(tab, o).Empty()
}

func (r *Row) equal(tab *typeTable, o *Row) bool {
	r, o = tab.set(r), tab.set(o)
	return len(r.Elems()) == len(o.Elems()) && r.subsetOf(tab, o)
}

// list prints the elements in typeString order (ties, which only distinct
// declarations of one name can make, keep the identity order). A row
// variable prints as its solution so far.
func (r *Row) list(sep string) string {
	elems := r.Elems()
	if r != nil && r.vars {
		var flat []Type
		for _, e := range elems {
			if v, ok := e.(*RowVar); ok && v.Solution() != nil {
				flat = append(flat, v.Solution().Elems()...)
				continue
			} else if ok && v.root().final {
				continue
			}
			flat = append(flat, e)
		}
		elems = flat
	}
	keys := make([]string, len(elems))
	for i, e := range elems {
		keys[i] = typeString(e)
	}
	order := make([]int, len(elems))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return keys[order[a]] < keys[order[b]] })
	parts := make([]string, 0, len(elems))
	for i, k := range order {
		// A solved variable may repeat an element the row lists.
		if i > 0 && elems[k] == elems[order[i-1]] {
			continue
		}
		parts = append(parts, elems[k].String())
	}
	return strings.Join(parts, sep)
}

func (r *Row) String() string { return r.list(" | ") }

// typeString is a type's canonical text for printing order only: package
// paths qualify names, and it carries no identity (two distinct declarations
// of one name print alike). The checker compares, caches and indexes types
// by their interned ids (intern.go), never by this string; a test keeps its
// callers to printing.
func typeString(t Type) string {
	switch t := t.(type) {
	case *Named:
		orig := t.origin()
		key := orig.Obj.Name
		if orig.Obj.Pkg != nil {
			key = orig.Obj.Pkg.Path + "." + key
		}
		if len(t.TArgs) > 0 {
			parts := make([]string, len(t.TArgs))
			for i, a := range t.TArgs {
				parts[i] = typeString(a)
			}
			key += "[" + strings.Join(parts, ",") + "]"
		}
		return key
	case *TypeParam:
		return "$" + t.Name
	case *RowArg:
		return "{" + t.Row.list("|") + "}"
	case *Slice:
		return "[]" + typeString(t.Elem)
	case *Array:
		return "[" + strconv.FormatInt(t.Len, 10) + "]" + typeString(t.Elem)
	case *Map:
		return "map[" + typeString(t.Key) + "]" + typeString(t.Elem)
	case *Chan:
		return fmt.Sprintf("chan%d ", t.Dir) + typeString(t.Elem)
	case *Pointer:
		return "*" + typeString(t.Elem)
	case *Signature:
		var b strings.Builder
		b.WriteString("func(")
		for _, p := range t.Params {
			b.WriteString(typeString(p.Type) + ",")
		}
		if t.Variadic {
			b.WriteString("...")
		}
		b.WriteString(")(")
		for _, r := range t.Results {
			b.WriteString(typeString(r) + ",")
		}
		b.WriteString(")e{" + t.Errors.list("|") + "}u{" + t.Uses.list("|") + "}")
		return b.String()
	case *Tuple:
		parts := make([]string, len(t.Types))
		for i, x := range t.Types {
			parts[i] = typeString(x)
		}
		return "(" + strings.Join(parts, ",") + ")"
	}
	return t.String()
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
// type and union variant. A type still
// being declared (on a cycle through the declaration that asks) has no
// underlying type yet; its declaration decides, so the answer does not
// depend on the order of the declarations.
func isStructNamed(t Type) bool {
	n, ok := t.(*Named)
	if !ok {
		return false
	}
	if o := n.origin(); o.state == 1 && o.Obj != nil && o.Obj.Decl != nil {
		_, ok = o.Obj.Decl.Spec.(*syntax.StructType)
		return ok
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
