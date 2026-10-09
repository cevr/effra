package compiler

import (
	"strings"
	"testing"
)

func TestScalarConstantAliasAndBundledImportFacts(t *testing.T) {
	source := `import Defaults "effra/constants"
const LocalSuffix: string = Defaults.defaultSuffix
const AliasSuffix: string = LocalSuffix
const Enabled: bool = true
const EnabledAlias: bool = Enabled
const Minimum: i64 = -9223372036854775808
const MinimumAlias: i64 = Minimum`
	for _, target := range []string{"go", "js"} {
		t.Run(target, func(t *testing.T) {
			r := CompileFor(source, target)
			if !r.Checked {
				t.Fatalf("scalar constants rejected on %s: %+v", target, r.Diagnostics)
			}
			imported := r.Program.BundledConstantBindings["Defaults"]["defaultSuffix"]
			if imported == nil || imported.Identity != constantIdentity("effra/constants", "defaultSuffix") || imported.Value == nil || imported.Value.Value != "!" {
				t.Fatalf("compiler-distributed constant binding missing: %+v", r.Program.BundledConstantBindings)
			}
			values := map[string]string{}
			for _, declaration := range r.Declarations {
				if declaration.Kind == "constant" && declaration.Constant != nil {
					values[declaration.Name] = declaration.Constant.Value
				}
			}
			if values["AliasSuffix"] != "!" || values["EnabledAlias"] != "true" || values["MinimumAlias"] != "-9223372036854775808" {
				t.Fatalf("direct aliases lost checked scalar values: %v", values)
			}
			foundSource := false
			for _, importedSource := range r.Sources {
				foundSource = foundSource || importedSource.Module == "effra/constants" && importedSource.Digest != ""
			}
			if !foundSource {
				t.Fatalf("bundled constant source digest missing: %+v", r.Sources)
			}
			localOffset := strings.Index(source, "AliasSuffix")
			localQuery, err := r.QueryType(TypeSelection{Offset: &localOffset})
			if err != nil || localQuery.Selection.Target == nil || localQuery.Selection.Target.Kind != "constant" || localQuery.Selection.Target.Constant == nil || localQuery.Selection.Target.Constant.Value != "!" || localQuery.Selection.Declaration == nil || localQuery.Selection.Declaration.Kind != "constant" {
				t.Fatalf("local constant declaration was not projected: selection=%+v err=%v", localQuery.Selection, err)
			}
			offset := strings.Index(source, "Defaults.defaultSuffix") + len("Defaults.")
			query, err := r.QueryType(TypeSelection{Offset: &offset})
			if err != nil || query.Selection.Target == nil || query.Selection.Target.Kind != "constant" || query.Selection.Target.Constant == nil || query.Selection.Target.Constant.Value != "!" {
				t.Fatalf("imported constant reference was not projected: selection=%+v err=%v", query.Selection, err)
			}
		})
	}
}

func TestScalarConstantAdmissionBoundary(t *testing.T) {
	refusals := []struct {
		name, source, message string
	}{
		{"computed value", `const Total: i64 = 1 + 2`, "constant expression must be a scalar literal or direct constant alias"},
		{"call value", `fn source() -> string { "x" }
const Suffix: string = source()`, "constant expression must be a scalar literal or direct constant alias"},
		{"wrong scalar type", `const Wrong: i64 = "x"`, "constant value must be i64"},
		{"nominal constructor", `record Box { value: string }
const DefaultBox: Box = Box { value: "x" }`, "constant type must be i64, string or bool"},
		{"unary alias", `const One: i64 = 1
const Negative: i64 = -One`, "constant value must be a scalar literal or direct constant alias"},
		{"nested unary", `const One: i64 = --1`, "constant value must be a scalar literal or direct constant alias"},
		{"unary plus", `const One: i64 = +1`, "constant value must be a scalar literal or direct constant alias"},
		{"alias cycle", `const First: string = Second
const Second: string = First`, "constant alias cycle"},
		{"unknown package", `import External "example/private"
const Suffix: string = External.value`, "unsupported bundled module"},
		{"positive minimum magnitude", `const TooLarge: i64 = 9223372036854775808`, "i64 constant is outside the signed 64-bit range"},
		{"ordinary positive minimum magnitude", `fn tooLarge() -> i64 { 9223372036854775808 }`, "integer exceeds i64 range"},
	}
	for _, refusal := range refusals {
		t.Run(refusal.name, func(t *testing.T) {
			r := Compile(refusal.source)
			if r.Checked || !strings.Contains(diagnosticText(r), refusal.message) {
				t.Fatalf("constant admission did not fail at its owner: %+v", r.Diagnostics)
			}
		})
	}
	computedNegative := Compile(`const Total: i64 = -1 + 2`)
	if computedNegative.Checked || len(computedNegative.Diagnostics) == 0 {
		t.Fatal("computed negative constant was admitted")
	}
}

func TestDuplicateConstantsHaveOneCheckedOwner(t *testing.T) {
	r := Compile(`const Duplicate: i64 = 1
const Duplicate: i64 = 2`)
	if r.Checked || !strings.Contains(diagnosticText(r), "duplicate constant Duplicate") {
		t.Fatalf("duplicate constant was not refused: %+v", r.Diagnostics)
	}
	count := 0
	for _, declaration := range r.Declarations {
		if declaration.Kind == "constant" && declaration.Name == "Duplicate" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("duplicate constant produced %d checked declarations", count)
	}
}

func TestConstantDeclarationFormattingIdentityAndIdempotence(t *testing.T) {
	source := `const Ending:string="!"
const Alias:string=Ending`
	first, err := FormatSource(source)
	if err != nil {
		t.Fatalf("format constant declaration syntax: %v", err)
	}
	if FormatterIdentity != "effra/formatter-10" || !strings.Contains(first.Text, "const Ending: string = \"!\"") {
		t.Fatalf("constant syntax has stale producer identity or formatting: identity=%s text=%s", FormatterIdentity, first.Text)
	}
	second, err := FormatSource(first.Text)
	if err != nil || second.Text != first.Text {
		t.Fatalf("constant formatter was not idempotent: %q / %v", second.Text, err)
	}
}
