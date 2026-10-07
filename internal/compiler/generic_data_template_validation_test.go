package compiler

import "testing"

func TestGenericDataTemplatePayloadTypeAdmission(t *testing.T) {
	for _, target := range []string{"go", "js"} {
		for _, source := range []string{
			`record Holder<T:type>{operation:effect fn(T)->T uses {Missing}}`,
			`record Holder<T:type>{operation:effect fn(T)->T raises {Missing}}`,
			`service Users {effect fn get()->string} record Holder<T:type>{operation:fn(T)->T uses {Users}}`,
		} {
			r := CompileFor(source, target)
			if r.Checked || len(r.Diagnostics) == 0 {
				t.Fatalf("%s invalid generic payload contract was admitted: %+v source=%s", target, r.Diagnostics, source)
			}
		}
		valid := CompileFor(`error Trouble service Users {effect fn get()->string raises {Trouble}} record Holder<T:type>{operation:effect fn(T)->T raises {Trouble} uses {Users}}`, target)
		if !valid.Checked {
			t.Fatalf("%s declared generic callable rows were refused: %+v", target, valid.Diagnostics)
		}
	}
}

func TestGenericDataBracePayloadCannotExecuteEffects(t *testing.T) {
	const service = `import Data "effra/data" service Secret {effect fn get()->string}`
	for _, target := range []string{"go", "js"} {
		bad := CompileFor(service+` effect fn bad()->Data.Option<string>{Data.Option.Some {value:run Secret.get()}}`, target)
		if bad.Checked || !hasCode(bad, "EF105") {
			t.Fatalf("%s brace payload must remain pure: checked=%v diagnostics=%+v", target, bad.Checked, bad.Diagnostics)
		}
		good := CompileFor(service+` effect fn good()->Data.Option<string> uses {Secret}{let value=run Secret.get(); Data.Option.Some {value}}`, target)
		if !good.Checked {
			t.Fatalf("%s effectful let followed by pure construction was refused: %+v", target, good.Diagnostics)
		}
		actual := good.Find("good")
		if actual == nil || len(actual.Actual.Services) != 1 || actual.Actual.Services[0] != "Secret" {
			t.Fatalf("%s executed row was not retained on the function: %+v", target, actual)
		}
	}
}
