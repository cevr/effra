package compiler

import "testing"

func TestGenericDataTemplatePayloadTypeAdmission(t *testing.T) {
	for _, source := range []string{
		`record Holder<T:type>{operation:effect fn(T)->T uses {Missing}}`,
		`record Holder<T:type>{operation:effect fn(T)->T raises {Missing}}`,
		`service Users {effect fn get()->string} record Holder<T:type>{operation:fn(T)->T uses {Users}}`,
		`record Ops<F:callable effect fn(A)->A,A:type>{operation:F} record Holder<T:type>{operation:Ops<effect fn(T)->T uses {Missing},T>}`,
		`record Ops<F:callable effect fn(A)->A,A:type>{operation:F} record Holder<T:type>{operation:Ops<effect fn(T)->T raises {Missing},T>}`,
		`service Users {effect fn get()->string} record Ops<F:callable fn(A)->A,A:type>{operation:F} enum Holder<T:type>{Some{operation:Ops<fn(T)->T uses {Users},T>}}`,
	} {
		r := CompileFor(source, "go")
		if r.Checked || len(r.Diagnostics) == 0 {
			t.Fatalf("go invalid generic payload contract was admitted: %+v source=%s", r.Diagnostics, source)
		}
	}
	valid := CompileFor(`error Trouble service Users {effect fn get()->string raises {Trouble}} record Holder<T:type>{operation:effect fn(T)->T raises {Trouble} uses {Users}}`, "go")
	if !valid.Checked {
		t.Fatalf("go declared generic callable rows were refused: %+v", valid.Diagnostics)
	}
	nestedValid := CompileFor(`service Users {effect fn get()->string} record Ops<F:callable effect fn(A)->A,A:type>{operation:F} record Holder<T:type>{operation:Ops<effect fn(T)->T uses {Users},T>}`, "go")
	if !nestedValid.Checked {
		t.Fatalf("go declared nested generic callable rows were refused: %+v", nestedValid.Diagnostics)
	}
}

func TestGenericDataBracePayloadCannotExecuteEffects(t *testing.T) {
	const service = `import Data "ergo/data" service Secret {effect fn get()->string}`
	bad := CompileFor(service+` effect fn bad()->Data.Option<string>{Data.Option.Some {value:run Secret.get()}}`, "go")
	if bad.Checked || !hasCode(bad, "EF105") {
		t.Fatalf("go brace payload must remain pure: checked=%v diagnostics=%+v", bad.Checked, bad.Diagnostics)
	}
	good := CompileFor(service+` effect fn good()->Data.Option<string> uses {Secret}{let value=run Secret.get(); Data.Option.Some {value}}`, "go")
	if !good.Checked {
		t.Fatalf("go effectful let followed by pure construction was refused: %+v", good.Diagnostics)
	}
	actual := good.Find("good")
	if actual == nil || len(actual.Actual.Services) != 1 || actual.Actual.Services[0] != "Secret" {
		t.Fatalf("go executed row was not retained on the function: %+v", actual)
	}
}
