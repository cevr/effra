package check

import (
	"fmt"

	"ergo.local/prototype/internal/sketch/syntax"
)

// ---------------------------------------------------------------- substitution

// subst replaces type parameters by their bindings. A row parameter's
// binding is a RowArg, flattened into the row it occurs in; a result-position
// parameter bound to no value drops out of the result list (Q3).
func (c *checker) subst(t Type, m map[*TypeParam]Type) Type {
	if len(m) == 0 || t == nil {
		return t
	}
	switch t := t.(type) {
	case *TypeParam:
		if b, ok := m[t]; ok && b != nil {
			return b
		}
		return t
	case *Named:
		if len(t.TArgs) == 0 {
			return t
		}
		args := make([]Type, len(t.TArgs))
		changed := false
		for i, a := range t.TArgs {
			args[i] = c.subst(a, m)
			if args[i] != a {
				changed = true
			}
		}
		if !changed {
			return t
		}
		return c.instantiate(t.origin(), args)
	case *RowArg:
		return &RowArg{Row: c.substRow(t.Row, m)}
	case *RowVar:
		// A use of a generic function's inferred row: the substitution is
		// recorded on the variable and applied to its solution when the row
		// is read (round 12 M1), composed with one recorded before.
		root := t.root()
		var tps []*TypeParam
		if root.Fn != nil && root.Fn.Sig != nil {
			tps = root.Fn.Sig.TParams
		}
		var nm map[*TypeParam]Type
		changed := false
		for _, tp := range tps {
			cur := Type(tp)
			if b, ok := t.m[tp]; ok {
				cur = b
			}
			nb := c.subst(cur, m)
			changed = changed || nb != cur
			if nb != Type(tp) {
				if nm == nil {
					nm = map[*TypeParam]Type{}
				}
				nm[tp] = nb
			}
		}
		if !changed {
			return t
		}
		if nm == nil {
			return root
		}
		return &RowVar{of: root, m: nm, ctx: c}
	case *Struct:
		s := &Struct{Fields: make([]*Field, len(t.Fields))}
		for i, f := range t.Fields {
			s.Fields[i] = &Field{Name: f.Name, Type: c.subst(f.Type, m), Pos: f.Pos}
		}
		return s
	case *Interface:
		it := &Interface{Comparable: t.Comparable, Methods: make([]*Method, len(t.Methods))}
		newOf := make(map[*Method]*Method, len(t.Methods))
		for i, mt := range t.Methods {
			it.Methods[i] = &Method{Name: mt.Name, Sig: c.subst(mt.Sig, m).(*Signature), Pos: mt.Pos}
			newOf[mt] = it.Methods[i]
		}
		// The order sorted at resolution carries over (no sort here).
		for _, mt := range t.byName {
			it.byName = append(it.byName, newOf[mt])
		}
		return it
	case *Union:
		u := &Union{Variants: make([]Type, len(t.Variants)), Bad: t.Bad}
		for i, v := range t.Variants {
			u.Variants[i] = c.subst(v, m)
		}
		return u
	case *MachineType:
		return &MachineType{State: c.subst(t.State, m), Signal: c.subst(t.Signal, m)}
	case *Slice:
		return &Slice{Elem: c.subst(t.Elem, m)}
	case *Array:
		return &Array{Len: t.Len, Elem: c.subst(t.Elem, m)}
	case *Map:
		return &Map{Key: c.subst(t.Key, m), Elem: c.subst(t.Elem, m)}
	case *Chan:
		return &Chan{Dir: t.Dir, Elem: c.subst(t.Elem, m)}
	case *Pointer:
		return &Pointer{Elem: c.subst(t.Elem, m)}
	case *Signature:
		s := &Signature{Variadic: t.Variadic, Fallible: t.Fallible}
		for _, p := range t.Params {
			s.Params = append(s.Params, &Var{Name: p.Name, Type: c.subst(p.Type, m), Pos: p.Pos, param: true})
		}
		for _, r := range t.Results {
			r2 := c.subst(r, m)
			if r2 == NoValue {
				continue
			}
			s.Results = append(s.Results, r2)
		}
		s.Errors = c.substRow(t.Errors, m)
		s.Uses = c.substRow(t.Uses, m)
		// Type parameters that stay unbound remain the signature's own.
		for _, tp := range t.TParams {
			if _, bound := m[tp]; !bound {
				s.TParams = append(s.TParams, tp)
			}
		}
		return s
	case *Tuple:
		out := &Tuple{}
		for _, x := range t.Types {
			out.Types = append(out.Types, c.subst(x, m))
		}
		return out
	}
	return t
}

func (c *checker) substRow(r *Row, m map[*TypeParam]Type) *Row {
	if r.Empty() {
		return newRow()
	}
	out := newRow()
	for _, e := range r.Elems() {
		out = out.with(c.types, c.subst(e, m))
	}
	return out
}

// ---------------------------------------------------------------- identity

// identical reports type identity: equal interned ids (intern.go). The ids
// follow Go's identity, minus what Ergo removes: a named type by its origin
// and identical type arguments; a struct by field names and types in order;
// an interface by method names and identical signatures; a signature by
// identical parameters and results and equal rows; a row argument by its
// set; a union and a machine only to themselves.
func (c *checker) identical(a, b Type) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return c.types.id(a) == c.types.id(b)
}

// sameShape reports whether two signatures have identical parameters and
// results.
func (c *checker) sameShape(a, b *Signature) bool {
	if len(a.Params) != len(b.Params) || len(a.Results) != len(b.Results) || a.Variadic != b.Variadic {
		return false
	}
	for i := range a.Params {
		if !c.identical(a.Params[i].Type, b.Params[i].Type) {
			return false
		}
	}
	for i := range a.Results {
		if !c.identical(a.Results[i], b.Results[i]) {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------- assignability

// assignable reports whether a value of type v may be assigned to type t,
// with the reason when it may not (types.md: identity, a variant to its
// union, a type to an interface by method-row compatibility, a function
// value to a function type whose rows are supersets; #128, Gap 24).
func (c *checker) assignable(v, t Type) (bool, string) {
	if isInvalid(v) || isInvalid(t) || c.identical(v, t) {
		return true, ""
	}
	if v == NoValue {
		return false, "it has no value"
	}
	if u := asUnion(t); u != nil {
		if _, isNamed := t.(*Named); isNamed {
			for _, variant := range u.Variants {
				if c.identical(variant, v) {
					return true, ""
				}
			}
			return false, fmt.Sprintf("%s is not a variant of %s", v, t)
		}
	}
	if it := asInterface(t); it != nil {
		if _, isTuple := v.(*Tuple); isTuple {
			return false, "it is several values"
		}
		if it.Comparable {
			return false, "comparable is a constraint only"
		}
		if why := c.implements(v, it); why != "" {
			return false, why
		}
		return true, ""
	}
	switch tt := t.(type) {
	case *Signature:
		vs, ok := v.(*Signature)
		if !ok {
			return false, ""
		}
		if !c.sameShape(vs, tt) {
			return false, "the parameters or results differ"
		}
		if !vs.Errors.subsetOf(c.types, tt.Errors) {
			return false, fmt.Sprintf("its error row %s is wider than %s", rowText(vs.Errors), rowText(tt.Errors))
		}
		if !vs.Uses.subsetOf(c.types, tt.Uses) {
			return false, fmt.Sprintf("its capability row %s is wider than %s", rowText(vs.Uses), rowText(tt.Uses))
		}
		return true, ""
	case *Chan:
		if vc, ok := v.(*Chan); ok && vc.Dir == 0 && c.identical(vc.Elem, tt.Elem) {
			return true, ""
		}
	}
	return false, ""
}

func rowText(r *Row) string {
	if r.Empty() {
		return "{}"
	}
	return "{" + r.list(", ") + "}"
}

// methodSet returns the method of t named name and its signature, for a
// value of type t: a named type's declared methods, or an interface's.
func (c *checker) methodOf(t Type, name string) (*Signature, *Func) {
	switch t := t.(type) {
	case *Named:
		if f := t.method(name); f != nil {
			if f.owner != nil {
				return c.selfSig(t, f), f
			}
			sig := c.funcSig(f)
			if t.Orig != nil {
				m := map[*TypeParam]Type{}
				for i, tp := range t.Orig.TParams {
					if i < len(t.TArgs) {
						m[tp] = t.TArgs[i]
					}
				}
				sig = c.subst(sig, m).(*Signature)
			}
			return sig, f
		}
		if it, ok := t.underlying().(*Interface); ok {
			if m := it.method(name); m != nil {
				return m.Sig, nil
			}
		}
	case *Interface:
		if m := t.method(name); m != nil {
			return m.Sig, nil
		}
	case *Pointer:
		return c.methodOf(t.Elem, name)
	case *TypeParam:
		if it := asInterface(t.Constraint); it != nil {
			if m := it.method(name); m != nil {
				return m.Sig, nil
			}
		}
	}
	return nil, nil
}

// selfSig is the signature x.f(…) calls for a value of t (196m): f's
// without its first parameter, which x fills. On an instance of a generic
// t, the type parameters the first parameter names take t's type
// arguments; f's other type parameters stay the signature's own.
func (c *checker) selfSig(t *Named, f *Func) *Signature {
	sig := c.funcSig(f)
	if len(sig.Params) == 0 {
		return sig
	}
	m := selfBinding(t, f)
	out := dropSelf(sig)
	if len(m) == 0 {
		return out
	}
	nb := 0
	for i, tp := range sig.TParams {
		if _, bound := m[tp]; !bound && i < sig.NBracket {
			nb++
		}
	}
	s := c.subst(out, m).(*Signature)
	s.NBracket = nb
	return s
}

// selfBinding maps the type parameters of f that its first parameter's
// type arguments name to t's type arguments, by position.
func selfBinding(t *Named, f *Func) map[*TypeParam]Type {
	m := map[*TypeParam]Type{}
	for i, a := range f.selfArgs {
		id, _ := a.(*syntax.Ident)
		if id == nil || i >= len(t.TArgs) || f.Sig == nil {
			continue
		}
		for _, tp := range f.Sig.TParams {
			if tp.Name == id.Name {
				m[tp] = t.TArgs[i]
			}
		}
	}
	return m
}

// dropSelf is sig without its first parameter, which x of x.f(…) fills.
func dropSelf(sig *Signature) *Signature {
	out := *sig
	out.Params = sig.Params[1:]
	return &out
}

// implements checks method-row compatibility: for every method of it, v has
// a method with identical parameters and results, the same fallibility, and
// error and capability rows that are subsets (types.md, Interface
// assignability 1; rows.md, Wire 2). Each lookup is a keyed map access, so
// the check is linear in the method count (#122).
func (c *checker) implements(v Type, it *Interface) string {
	// The method set of *T holds the *T-receiver methods too, as in Go
	// (N1); T's holds only the value-receiver ones.
	ptr := false
	if p, ok := v.(*Pointer); ok {
		v, ptr = p.Elem, true
	}
	for _, m := range it.Methods {
		got, fn := c.methodOf(v, m.Name)
		if got == nil {
			return fmt.Sprintf("%s has no method %s", v, m.Name)
		}
		if fn != nil && fn.Ptr && !ptr {
			return fmt.Sprintf("%s.%s has a *T receiver", v, m.Name)
		}
		if !c.sameShape(got, m.Sig) {
			return fmt.Sprintf("%s.%s has signature %s, want %s", v, m.Name, got, m.Sig)
		}
		if !got.Errors.subsetOf(c.types, m.Sig.Errors) {
			return fmt.Sprintf("%s.%s can fail with %s, wider than %s", v, m.Name, rowText(got.Errors.minus(c.types, m.Sig.Errors)), rowText(m.Sig.Errors))
		}
		if !got.Uses.subsetOf(c.types, m.Sig.Uses) {
			return fmt.Sprintf("%s.%s requires %s, which the interface's method does not (README Gap 17 stays with #37)", v, m.Name, rowText(got.Uses.minus(c.types, m.Sig.Uses)))
		}
		if got.Fallible != m.Sig.Fallible {
			return fmt.Sprintf("%s.%s differs in fallibility from the interface's method", v, m.Name)
		}
	}
	return ""
}

// comparable reports whether == applies to values of t (Go's rule; a
// union compares as an interface).
func comparable(t Type) bool {
	return comparableSeen(t, map[Type]bool{})
}

func comparableSeen(t Type, seen map[Type]bool) bool {
	switch u := under(t).(type) {
	case *Basic:
		return u.Kind != Invalid
	case *Struct:
		// A recursive struct reaches itself through a named type, the same
		// type object (instances are cached by id).
		if seen[t] {
			return true
		}
		seen[t] = true
		for _, f := range u.Fields {
			if !comparableSeen(f.Type, seen) {
				return false
			}
		}
		return true
	case *Array:
		return comparableSeen(u.Elem, seen)
	case *Interface, *Union, *Chan, *Pointer:
		return true
	case *TypeParam:
		ci := asInterface(u.Constraint)
		return ci != nil && ci.Comparable
	}
	return false
}

// satisfies checks a type argument against its constraint.
func (c *checker) satisfies(t Type, constraint Type) string {
	if constraint == nil {
		return ""
	}
	it := asInterface(constraint)
	if it == nil {
		return ""
	}
	if it.Comparable {
		if !comparable(t) {
			return fmt.Sprintf("%s is not comparable", t)
		}
		return ""
	}
	return c.implements(t, it)
}

// defaultable reports whether t has a valid zero value (types.md, Zero
// values 1): a basic type, a slice, map or channel, or a struct of
// defaultable fields. A union, an interface, a function type, a pointer and
// a struct that holds any of them are not.
func defaultable(t Type) bool {
	return defaultableSeen(t, map[Type]bool{})
}

func defaultableSeen(t Type, seen map[Type]bool) bool {
	switch u := under(t).(type) {
	case *Basic:
		return true
	case *Slice, *Map, *Chan:
		return true
	case *Array:
		return defaultableSeen(u.Elem, seen)
	case *Struct:
		// A recursive struct reaches itself through a named type, the same
		// type object (instances are cached by id).
		if seen[t] {
			return true
		}
		seen[t] = true
		for _, f := range u.Fields {
			if !defaultableSeen(f.Type, seen) {
				return false
			}
		}
		return true
	}
	return false
}
