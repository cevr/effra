package compiler

import (
	"fmt"
	"strings"
	"testing"
)

func TestGenericDataLayoutRegistrationAndSubstitution(t *testing.T) {
	const source = `record User { name: string }
record Box<T: type> { value: T }
enum Presence<T: type> { None; Some { value: Box<T> } }
record Envelope<T: type> { label: string; presence: Presence<T> }
effect fn main() -> void { void }`
	for _, target := range []string{"go", "js"} {
		r := CompileFor(source, target)
		if !r.Checked {
			t.Fatal("finite generic layout rejected", r.Diagnostics)
		}
		if _, err := r.EmitGo(); err != nil {
			t.Fatal(err)
		}
		if _, _, err := r.Emit(true); err != nil {
			t.Fatal(err)
		}
		c := r.projector
		user := c.canonicalRef(typeRef("User"))
		envelope := c.templateByName("Envelope")
		id, err := c.templateApplication(envelope, []TypeID{user})
		if err != nil {
			t.Fatal(err)
		}
		fields, ok := c.applicationFields(id)
		if !ok || len(fields) != 2 || c.node(fields[0].typeID).Name != "string" {
			t.Fatal("mixed concrete/variable layout lost", fields)
		}
		variants, ok := c.applicationVariants(fields[1].typeID)
		if !ok || len(variants) != 2 || len(variants[0].Fields) != 0 {
			t.Fatal("closed alternatives lost", variants)
		}
		boxFields, ok := c.applicationFields(variants[1].Fields[0].typeID)
		if !ok || len(boxFields) != 1 || boxFields[0].typeID != user {
			t.Fatal("nested variant substitution lost nominal argument", boxFields)
		}
		if envelope.Identity == c.templateByName("Presence").Identity || envelope.Parameters[0].Identity == c.templateByName("Presence").Parameters[0].Identity {
			t.Fatal("declaration parameter owners collapsed")
		}
		if _, err := c.templateApplication(envelope, nil); err == nil {
			t.Fatal("missing complete arguments admitted")
		}
	}
}

func TestGenericDataRecursiveLayoutRefusal(t *testing.T) {
	for _, source := range []string{
		`record Link<T: type> { next: Link<T> }`,
		`record A<T: type> { next: B<T> } enum B<T: type> { Value { next: A<T> } }`,
		`record Link<T: type> { next: Holder } record Holder { value: Link<string> }`,
		`record Link<T: type> { factory: fn() -> Link<T> }`,
	} {
		r := Compile(source + ` effect fn main() -> void { void }`)
		found := false
		for _, diagnostic := range r.Diagnostics {
			found = found || strings.Contains(diagnostic.Message, "recursive or excessive generic data layout")
		}
		if r.Checked || !found {
			t.Fatal("recursive generic layout lacks truthful refusal", r.Diagnostics)
		}
	}
}

func TestGenericDataLayoutWorkBudgetCountsRepeatedVariantEdges(t *testing.T) {
	var source strings.Builder
	source.WriteString("enum Many<T: type> {\n")
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&source, "V%d {", i)
		for j := 0; j < 45; j++ {
			fmt.Fprintf(&source, " f%d: T;", j)
		}
		source.WriteString("}\n")
	}
	source.WriteString("}")
	r := Compile(source.String())
	found := false
	for _, diagnostic := range r.Diagnostics {
		found = found || strings.Contains(diagnostic.Message, "layout exceeds 4096 work budget")
	}
	if !found || r.Checked {
		t.Fatal("repeated layout edges escaped the work budget", r.Diagnostics)
	}
	response := r.CheckResponse()
	if response["typeProjectionComplete"] != false || response["declarations"] != nil {
		t.Fatal("refused source published authoritative generic layouts", response)
	}
}

func TestGenericEnumFormattingAdmissionIdentity(t *testing.T) {
	const source = `enum Presence<T: type> { None; Some { value: T } }`
	formatted, err := FormatSource(source)
	if err != nil {
		t.Fatal(err)
	}
	second, err := FormatSource(formatted.Text)
	if err != nil || second.Text != formatted.Text || FormatterIdentity != "effra/formatter-5" || FormatterSchemaVersion != 1 {
		t.Fatal("generic syntax formatting identity/idempotence", formatted, second, err)
	}
}

func TestGenericDataSharedLayoutsAndExistingCodecOwner(t *testing.T) {
	var source strings.Builder
	source.WriteString(`import Convert "effra/conversions"
record Leaf<T: type> { value: T }
record Witness<T: type> { codec: Convert.Codec<T, string, effect fn(string) -> T, effect fn(T) -> string> }
`)
	previous := "Leaf"
	for i := 0; i < 20; i++ {
		name := fmt.Sprintf("Pair%d", i)
		fmt.Fprintf(&source, "record %s<T: type> { left: %s<T>; right: %s<T> }\n", name, previous, previous)
		previous = name
	}
	r := Compile(source.String())
	for _, diagnostic := range r.Diagnostics {
		if strings.Contains(diagnostic.Message, "layout") {
			t.Fatal("shared finite layouts or existing Codec owner falsely refused", diagnostic)
		}
	}
	if !r.Checked {
		t.Fatal("finite shared layouts rejected", r.Diagnostics)
	}
}
