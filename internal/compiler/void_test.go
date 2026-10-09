package compiler

import (
	"strings"
	"testing"
)

func TestVoidIsOneCanonicalPrimitiveAcrossContracts(t *testing.T) {
	const source = `import Data "effra/data"
error Stopped
record Box<T: type> { value: T }
fn pure() -> void { void }
fn invoke(callback: fn() -> void) -> void { callback(); void }
effect fn effectInvoke(callback: effect fn() -> void raises {Stopped}) -> void raises {Stopped} { run callback() }
fn present() -> Data.Option<void> { Data.Option.Some { value: void } }
fn absent() -> Data.Option<void> { Data.Option<void>.None {}
}
effect fn main() -> void { void }`

	for _, target := range []string{"go", "js"} {
		t.Run(target, func(t *testing.T) {
			r := CompileFor(source, target)
			if !r.Checked {
				t.Fatalf("void source rejected: %+v", r.Diagnostics)
			}
			for _, name := range []string{"pure", "invoke", "effectInvoke", "present", "absent", "main"} {
				symbol := r.Find(name)
				if symbol == nil {
					t.Fatalf("missing symbol %s", name)
				}
				if name == "pure" || name == "invoke" || name == "effectInvoke" || name == "main" {
					if symbol.Contract.Success != voidTypeName || symbol.Contract.Type.Name != voidTypeName {
						t.Fatalf("%s lost canonical void result: %+v", name, symbol.Contract)
					}
				}
			}
			if _, _, err := r.Emit(false); err != nil {
				t.Fatalf("void library emission failed: %v", err)
			}
			if target == "go" {
				if _, err := r.EmitGo(); err != nil {
					t.Fatalf("void native emission failed: %v", err)
				}
			}
		})
	}
}

func TestVoidExpressionProjectionAndEmptyListsRemainDistinct(t *testing.T) {
	const source = `fn pure() -> void { void }
fn invoke(callback: fn() -> void) -> void { callback(); void }`
	r := Compile(source)
	if !r.Checked {
		t.Fatalf("void projection source rejected: %+v", r.Diagnostics)
	}
	offset := strings.LastIndex(source, "void")
	info, err := r.TypeAt(offset)
	if err != nil {
		t.Fatal(err)
	}
	if info.Kind != "void" || info.Type.Success != voidTypeName || info.Type.Type.Name != voidTypeName {
		t.Fatalf("wrong void expression projection: %+v", info)
	}
	if !strings.Contains(source, "fn() -> void") || !strings.Contains(source, "callback()") {
		t.Fatal("empty callable lists were not retained in the control fixture")
	}
}

func TestLegacyNoValueSpellingsAreFocusedSyntaxDiagnostics(t *testing.T) {
	cases := []struct {
		name    string
		source  string
		message string
	}{
		{"type", `fn bad() -> () { void }`, "no-value type"},
		{"expression", `fn bad() -> void { () }`, "no-value expression"},
		{"commented type", "fn bad() -> ( // legacy\n ) { void }", "no-value type"},
		{"commented expression", "fn bad() -> void { ( // legacy\n ) }", "no-value expression"},
		{"block-commented type", `fn bad() -> ( /* legacy */ ) { void }`, "no-value type"},
		{"block-commented expression", `fn bad() -> void { ( /* legacy */ ) }`, "no-value expression"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Compile(tc.source)
			if r.Checked || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "EF002" || !strings.Contains(r.Diagnostics[0].Message, tc.message) {
				t.Fatalf("legacy spelling was not rejected at its syntax boundary: %+v", r.Diagnostics)
			}
			if r.Diagnostics[0].Span.Length < 2 {
				t.Fatalf("legacy spelling span did not cover the pair: %+v", r.Diagnostics[0].Span)
			}
		})
	}

	if result := Compile(`fn bad() -> void { void() }`); result.Checked || !hasCode(result, "EF102") {
		t.Fatalf("void call did not reach ordinary callable checking: %+v", result.Diagnostics)
	}
	if result := Compile(`fn bad() -> unit { void }`); result.Checked || !hasCode(result, "EF102") || !strings.Contains(result.Diagnostics[0].Message, "use void") {
		t.Fatalf("unit did not retain a targeted unknown-type diagnostic: %+v", result.Diagnostics)
	}
}

func TestFormatterRejectsLegacyNoValueWithoutReplacement(t *testing.T) {
	result, err := FormatSource("fn bad() -> () { () }")
	failure, ok := err.(FormatFailure)
	if !ok || len(failure.Diagnostics) != 1 || failure.Diagnostics[0].Code != "EF002" || result.Text != "" || result.OutputDigest != "" {
		t.Fatalf("formatter accepted or rewrote legacy no-value syntax: result=%+v err=%v", result, err)
	}
	if FormatterIdentity != "effra/formatter-9" {
		t.Fatalf("formatter identity did not advance with syntax admission: %s", FormatterIdentity)
	}
}
