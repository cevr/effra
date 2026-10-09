package compiler

import (
	"reflect"
	"testing"
)

// TestEvidenceSchemaIsClassified audits schema accounting, not the behavior of
// occurrence rewrites or joins. Evidence and row fields are traversed;
// explicitly classified metadata ends traversal.
func TestEvidenceSchemaIsClassified(t *testing.T) {
	audit := func(slots map[string]occurrenceSlotClass) (missing, stale []string) {
		seen := map[reflect.Type]bool{}
		classified := map[string]bool{}
		var walk func(reflect.Type)
		walk = func(typ reflect.Type) {
			switch typ.Kind() {
			case reflect.Pointer, reflect.Slice, reflect.Array:
				walk(typ.Elem())
				return
			case reflect.Map:
				walk(typ.Key())
				walk(typ.Elem())
				return
			case reflect.Struct:
			default:
				return
			}
			if seen[typ] {
				return
			}
			seen[typ] = true
			for i := 0; i < typ.NumField(); i++ {
				field := typ.Field(i)
				name := typ.Name() + "." + field.Name
				class, ok := slots[name]
				if !ok {
					missing = append(missing, name)
					continue
				}
				classified[name] = true
				if class != occurrenceMetadata {
					walk(field.Type)
				}
			}
		}
		walk(reflect.TypeOf(checkedExpression{}))
		for name := range slots {
			if !classified[name] {
				stale = append(stale, name)
			}
		}
		return missing, stale
	}
	cloneSlots := func() map[string]occurrenceSlotClass {
		slots := make(map[string]occurrenceSlotClass, len(occurrenceSlots))
		for name, class := range occurrenceSlots {
			slots[name] = class
		}
		return slots
	}

	t.Run("complete schema", func(t *testing.T) {
		missing, stale := audit(occurrenceSlots)
		if len(missing) != 0 || len(stale) != 0 {
			t.Fatalf("schema classification: missing %v; stale %v", missing, stale)
		}
	})
	t.Run("missing live field", func(t *testing.T) {
		slots := cloneSlots()
		delete(slots, "CheckedValue.ownership")
		missing, stale := audit(slots)
		if len(missing) != 1 || missing[0] != "CheckedValue.ownership" || len(stale) != 0 {
			t.Fatalf("missing-field audit: missing %v; stale %v", missing, stale)
		}
	})
	t.Run("stale field", func(t *testing.T) {
		slots := cloneSlots()
		slots["CheckedValue.noSuchField"] = occurrenceEvidence
		missing, stale := audit(slots)
		if len(missing) != 0 || len(stale) != 1 || stale[0] != "CheckedValue.noSuchField" {
			t.Fatalf("stale-field audit: missing %v; stale %v", missing, stale)
		}
	})
}
