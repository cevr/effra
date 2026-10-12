package machineplan

import (
	"reflect"
	"testing"
)

// The package counts its fixed sizes in words because it imports neither
// reflect nor unsafe (boundary_test.go). The tests may: each count must equal
// what reflect reports, for any value type V.
func TestFixedSizesMatchTheRuntime(t *testing.T) {
	type wide [256]byte
	size := func(typ reflect.Type) int { return int(typ.Size()) }
	for _, c := range []struct {
		name        string
		count, want int
	}{
		{"check[struct{}]", checkBytes, size(reflect.TypeFor[check[struct{}]]())},
		{"check[wide]", checkBytes, size(reflect.TypeFor[check[wide]]())},
		{"scope", scopeBytes, size(reflect.TypeFor[scope]())},
		{"payloadWalk[struct{}]", payloadWalkBytes, size(reflect.TypeFor[payloadWalk[struct{}]]())},
		{"payloadWalk[wide]", payloadWalkBytes, size(reflect.TypeFor[payloadWalk[wide]]())},
		{"Diagnostic", diagnosticBytes, size(reflect.TypeFor[Diagnostic]())},
		{"reportKey", reportKeyBytes, size(reflect.TypeFor[reportKey]())},
		{"Plan[struct{}]", PlanBaseBytes, size(reflect.TypeFor[Plan[struct{}]]())},
	} {
		if c.count != c.want {
			t.Errorf("%s: counted %d bytes, reflect reports %d", c.name, c.count, c.want)
		}
	}
	// A wider V makes a wider plan, which is why the caller passes PlanBytes.
	if size(reflect.TypeFor[Plan[wide]]()) <= PlanBaseBytes {
		t.Errorf("Plan[wide] is %d bytes, no wider than Plan[struct{}]", size(reflect.TypeFor[Plan[wide]]()))
	}
	slot := func(key, element reflect.Type) int {
		return size(reflect.StructOf([]reflect.StructField{{Name: "K", Type: key}, {Name: "V", Type: element}}))
	}
	slots := map[string]int{
		"reports seen":     slot(reflect.TypeFor[reportKey](), reflect.TypeFor[bool]()),
		"transition keys":  slot(reflect.TypeFor[string](), reflect.TypeFor[bool]()),
		"helper ordinals":  slot(reflect.TypeFor[string](), reflect.TypeFor[int]()),
		"enum tables":      slot(reflect.TypeFor[*Enum](), reflect.TypeFor[map[string]*set]()),
		"variant indexes":  slot(reflect.TypeFor[string](), reflect.TypeFor[int]()),
		"narrowing tables": slot(reflect.TypeFor[string](), reflect.TypeFor[*set]()),
		"refinements":      slot(reflect.TypeFor[refinement](), reflect.TypeFor[*set]()),
		"subsets":          slot(reflect.TypeFor[[2]*set](), reflect.TypeFor[bool]()),
		"role scopes":      slot(reflect.TypeFor[*Local](), reflect.TypeFor[*Expr]()),
		"payload visits":   slot(reflect.TypeFor[TypeRef](), reflect.TypeFor[bool]()),
	}
	for _, m := range declarationMaps {
		want, ok := slots[m.name]
		if !ok {
			t.Errorf("no key and element types for map %s", m.name)
			continue
		}
		if m.slot != want {
			t.Errorf("map %s: counted a %d-byte slot, reflect reports %d", m.name, m.slot, want)
		}
	}
	if len(slots) != len(declarationMaps) {
		t.Errorf("%d slot types for %d maps", len(slots), len(declarationMaps))
	}
}
