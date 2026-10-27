package actorlib

import (
	"reflect"
	"sync"
	"unsafe"
)

// snapshot is the lowering of a value copy across the actor boundary: the
// rule that values copy into a task (runtime.md Tasks rule 6) with value
// semantics (types.md Structs rule 6), applied where a value leaves one task
// for another: a message, a reply, an agent call's result and a machine's
// state snapshot and decision. It returns a value that shares no mutable
// storage with v: slices and maps are copied, deeply. Strings, numbers,
// channels, functions and pointers are not copied: they are immutable or are
// synchronised handles (a Reply's slot, a lowered capability environment). A
// resource (a type with Close) moves rather than copies, so it is returned as
// it is.
//
// The compiler knows each type statically and would emit the copy, or none,
// at the boundary, and skip it on a last use (move on last use); for an
// interface it would emit a type switch on the dynamic type. Generic library
// code cannot know V, so this stand-in decides per type at run time and
// caches the decision: a type with no slice, map or interface anywhere costs
// one cache lookup (research note, Gap A11; the measured cost is in Q8).
func snapshot[V any](v V) V {
	t := reflect.TypeFor[V]()
	p := planFor(t)
	if p == planNone {
		return v
	}
	if t.Kind() == reflect.Interface {
		d := reflect.TypeOf(any(v)) // V is an interface type: no allocation
		if d == nil || planFor(d) == planNone {
			return v
		}
	}
	return deepSnapshot(v)
}

// deepSnapshot is apart from snapshot so that only a value that may need a
// copy escapes to the heap.
//
//go:noinline
func deepSnapshot[V any](v V) V {
	rv := reflect.ValueOf(&v).Elem()
	if !needsCopy(rv) {
		return v
	}
	rv.Set(deepCopy(rv)) // v is this call's own copy: overwrite it
	return v
}

// plan is what a type needs to be copied. The order matters: a composite's
// plan is the largest plan of its parts.
type plan uint8

const (
	planNone    plan = iota // no slice, map or interface reachable: the value is its own copy
	planDynamic             // an interface is reachable: decide by its dynamic value
	planDeep                // a slice or map is reachable through fields
)

// plans caches each type's plan. A copy-on-write map behind an atomic
// pointer measured slower than sync.Map here (19 against 15 ns/op for
// int64).
var plans sync.Map // reflect.Type -> plan

func planFor(t reflect.Type) plan {
	if p, ok := plans.Load(t); ok {
		return p.(plan)
	}
	p := computePlan(t, map[reflect.Type]bool{})
	plans.Store(t, p)
	return p
}

var closer = reflect.TypeFor[interface{ Close() }]()

func computePlan(t reflect.Type, visiting map[reflect.Type]bool) plan {
	if t.Kind() != reflect.Interface && t.Implements(closer) {
		return planNone // a resource moves
	}
	switch t.Kind() {
	case reflect.Slice, reflect.Map:
		return planDeep
	case reflect.Interface:
		return planDynamic
	case reflect.Array:
		return computePlan(t.Elem(), visiting)
	case reflect.Struct:
		if visiting[t] {
			return planNone
		}
		visiting[t] = true
		p := planNone
		for i := range t.NumField() {
			p = max(p, computePlan(t.Field(i).Type, visiting))
		}
		return p
	}
	return planNone
}

// needsCopy reports whether v reaches a slice or map, looking through the
// dynamic values of its interfaces. It does not allocate.
func needsCopy(v reflect.Value) bool {
	switch planFor(v.Type()) {
	case planNone:
		return false
	case planDeep:
		return true
	}
	switch v.Kind() {
	case reflect.Interface:
		return !v.IsNil() && needsCopy(v.Elem())
	case reflect.Array:
		for i := range v.Len() {
			if needsCopy(v.Index(i)) {
				return true
			}
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if needsCopy(v.Field(i)) {
				return true
			}
		}
	}
	return false
}

// deepCopy copies v so that no slice or map is shared. Unexported struct
// fields are written through their address, as generated code would write
// them directly.
func deepCopy(v reflect.Value) reflect.Value {
	if !needsCopy(v) {
		return v
	}
	t := v.Type()
	switch t.Kind() {
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeSlice(t, v.Len(), v.Len())
		reflect.Copy(out, v)
		if planFor(t.Elem()) != planNone {
			for i := range v.Len() {
				e := out.Index(i)
				e.Set(deepCopy(e))
			}
		}
		return out
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeMapWithSize(t, v.Len())
		iter := v.MapRange()
		for iter.Next() {
			out.SetMapIndex(iter.Key(), deepCopy(iter.Value()))
		}
		return out
	case reflect.Interface:
		out := reflect.New(t).Elem()
		out.Set(deepCopy(v.Elem()))
		return out
	case reflect.Array, reflect.Struct:
		out := reflect.New(t).Elem()
		out.Set(v)
		if t.Kind() == reflect.Array {
			for i := range v.Len() {
				e := out.Index(i)
				e.Set(deepCopy(e))
			}
			return out
		}
		for i := range t.NumField() {
			f := out.Field(i)
			if !needsCopy(f) {
				continue
			}
			f = reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem()
			f.Set(deepCopy(f)) // read from the shallow copy, which is addressable and writable
		}
		return out
	}
	return v
}
