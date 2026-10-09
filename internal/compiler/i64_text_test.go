package compiler

import (
	"strings"
	"testing"
)

func TestI64TextRefusesImplicitAndMisusedConversions(t *testing.T) {
	const header = "import I64 \"effra/i64\"\n"
	for _, target := range []string{"go", "js"} {
		for _, test := range []struct {
			name, source, code string
		}{
			{"i64 returned as string", `fn bad(n: i64) -> string { n }`, "EF106"},
			{"string returned as i64", `fn bad(s: string) -> i64 { s }`, "EF106"},
			{"string concatenated with i64", `fn bad(n: i64) -> string { "n=" + n }`, "EF106"},
			{"i64 argument for a string parameter", `fn take(s: string) -> string { s } fn bad(n: i64) -> string { take(n) }`, "EF106"},
			{"format of a string", `fn bad(s: string) -> string { I64.format(s) }`, "EF106"},
			{"parse of an i64", `effect fn bad(n: i64) -> i64 raises { I64ParseFailure } { run I64.parse(n) }`, "EF106"},
			{"parse in a pure function", `fn bad(s: string) -> i64 { run I64.parse(s) }`, "EF105"},
			{"parse failure not declared", `effect fn bad(s: string) -> i64 { run I64.parse(s) }`, "EF107"},
			{"unknown member", `fn bad(n: i64) -> string { I64.toString(n) }`, "EF126"},
			{"no string conversion call", `fn bad(n: i64) -> string { string(n) }`, "EF102"},
		} {
			t.Run(target+"/"+test.name, func(t *testing.T) {
				r := CompileFor(header+test.source, target)
				if r.Checked || !hasCode(r, test.code) {
					t.Fatalf("want %s, got %+v", test.code, r.Diagnostics)
				}
			})
		}
	}
	if r := CompileFor(`effect fn main() -> i64 raises { I64ParseFailure } { 1 }`, "go"); r.Checked || !hasCode(r, "EF107") && !hasCode(r, "EF102") {
		t.Fatalf("I64ParseFailure must not exist without importing effra/i64: %+v", r.Diagnostics)
	}
}

func TestI64TextRetainsOnlyReachedOperations(t *testing.T) {
	formatOnly := "import I64 \"effra/i64\"\neffect fn main() -> string { I64.format(-9223372036854775808) }\n"
	unused := "import I64 \"effra/i64\"\neffect fn main() -> string { \"x\" }\n"
	for _, test := range []struct {
		source                 string
		strconv, format, parse bool
	}{
		{formatOnly, true, true, false},
		{unused, false, false, false},
		{signed64MultiplicativeCallers + "effect fn main() -> void { void }", false, false, false},
	} {
		r := CompileFor(test.source, "go")
		if !r.Checked {
			t.Fatal(r.Diagnostics)
		}
		native, err := r.EmitGo()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(native, "\"strconv\"\n") != test.strconv || strings.Contains(native, "func efI64Format") != test.format || strings.Contains(native, "func efI64Parse") != test.parse {
			t.Fatalf("native retention for %q: strconv=%v format=%v parse=%v\n%s", test.source, strings.Contains(native, "\"strconv\""), strings.Contains(native, "func efI64Format"), strings.Contains(native, "func efI64Parse"), native)
		}
		jsResult := CompileFor(test.source, "js")
		js, _, err := jsResult.Emit(false)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(js, "const __ef_i64Format") != test.format || strings.Contains(js, "const __ef_i64Parse") != test.parse {
			t.Fatalf("JavaScript retention for %q\n%s", test.source, js)
		}
	}
	if output := runGeneratedGo(t, CompileFor(formatOnly, "go")); output != "-9223372036854775808\n" {
		t.Fatalf("native format returned %q", output)
	}
}

func TestI64TextDeclarationsAndJavaScriptGrammarMutant(t *testing.T) {
	source := `import I64 "effra/i64"
fn label(count: i64) -> string {
    I64.format(count)
}
effect fn quantity(text: string) -> i64 raises { I64ParseFailure } {
    run I64.parse(text)
}
effect fn main() -> string {
    let value = run quantity(" 1").catch<I64ParseFailure>(0)
    label(value)
}
`
	r := CompileFor(source, "js")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	js, declarations, err := r.Emit(false)
	if err != nil {
		t.Fatal(err)
	}
	checkStrictTypeScript(t, declarations, `import { label } from "./generated.mjs";
const text: string = label(-9223372036854775808n);
void text;
// @ts-expect-error i64 formatting takes bigint, not number
label(1);`)
	if output := runJS(t, source, `if ((await Effect.runPromise(__ef_function_main())) !== "0") throw new Error("leading space parsed");`); output != "" {
		t.Fatalf("JavaScript grammar probe returned %q", output)
	}
	// BigInt() trims whitespace and accepts 0x prefixes; the explicit grammar
	// is what keeps JavaScript aligned with Go's strconv.
	mutant := strings.Replace(js, "if (!/^[+-]?[0-9]+$/.test(text)) return", "if (false) return", 1)
	if mutant == js {
		t.Fatal("grammar mutant did not alter generated JavaScript")
	}
	if output := runJSProbe(t, withProbeImports(mutant), `if ((await Effect.runPromise(__ef_function_main())) !== "1") throw new Error("grammar mutant did not change behavior");`); output != "" {
		t.Fatalf("grammar mutant probe returned %q", output)
	}
	goResult := CompileFor(source, "go")
	if output := runGeneratedGo(t, goResult); output != "0\n" {
		t.Fatalf("native grammar probe returned %q", output)
	}
}

func TestI64TextMarkerMustMatchItsIndexedOperation(t *testing.T) {
	parsed, diagnostics := parse("fn format(value: i64) -> string {\n    \"effra:intrinsic i64.parse\"\n}\n")
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	if admitBundledIntrinsic(i64FormatOp, parsed.Functions[0]) {
		t.Fatal("a body marker naming another operation was admitted")
	}
	plain, _ := parse("fn format(value: i64) -> string {\n    \"text\"\n}\n")
	if admitBundledIntrinsic(i64FormatOp, plain.Functions[0]) {
		t.Fatal("an ordinary body was admitted as compiler-owned")
	}
	user := CompileFor("fn format(value: i64) -> string {\n    \"effra:intrinsic i64.format\"\n}\neffect fn main() -> string { format(1) }\n", "go")
	if !user.Checked {
		t.Fatal(user.Diagnostics)
	}
	if output := runGeneratedGo(t, user); output != "effra:intrinsic i64.format\n" {
		t.Fatalf("user source spelled the compiler-owned operation: %q", output)
	}
}
