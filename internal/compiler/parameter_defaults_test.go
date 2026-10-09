package compiler

import (
	"strings"
	"testing"
)

func TestParameterDefaultSyntaxFormatsUnderFormatterTen(t *testing.T) {
	source := `fn ordinary(required: string, suffix:string="!") -> string { required }
fn marked(required required:string = "x") -> string { required }`
	first, err := FormatSource(source)
	if err != nil {
		t.Fatalf("format contextual parameter syntax: %v", err)
	}
	if FormatterIdentity != "effra/formatter-10" || !strings.Contains(first.Text, `ordinary(required: string,`) || !strings.Contains(first.Text, `fn marked(required required: string = "x")`) || !strings.Contains(first.Text, `suffix: string = "!"`) {
		t.Fatalf("formatter epoch or parameter formatting is stale: identity=%s text=%s", FormatterIdentity, first.Text)
	}
	second, err := FormatSource(first.Text)
	if err != nil || second.Text != first.Text {
		t.Fatalf("parameter formatting was not idempotent: %q / %v", second.Text, err)
	}
}

func TestParameterContractSyntaxRemainsUnadmittedUntilPublicFactsLand(t *testing.T) {
	ordinary := Compile(`fn plain(required: string) -> string { required }`)
	if !ordinary.Checked {
		t.Fatalf("a parameter named required was mistaken for a role marker: %+v", ordinary.Diagnostics)
	}

	refusals := []struct {
		name, source, message string
	}{
		{
			name:    "unused required-choice declaration",
			source:  `fn unused(required value: string) -> string { "ok" }`,
			message: "required-choice parameter syntax is not yet admitted by the checker",
		},
		{
			name:    "unused default declaration",
			source:  `fn unused(value: string = "x") -> string { "ok" }`,
			message: "parameter defaults are not yet admitted by the checker",
		},
		{
			name: "service declaration",
			source: `service Settings {
    effect fn get(required key: string, suffix: string = "x") -> string
}`,
			message: "parameter defaults are not yet admitted by the checker",
		},
		{
			name: "unused service required-choice declaration",
			source: `service Settings {
    effect fn get(required key: string) -> string
}`,
			message: "required-choice parameter syntax is not yet admitted by the checker",
		},
		{
			name: "provider method default",
			source: `service Settings { effect fn get(suffix: string) -> string }
impl SettingsLive for Settings {
    effect fn get(suffix: string = "x") -> string { suffix }
}`,
			message: "parameter defaults are not yet admitted by the checker",
		},
		{
			name:    "generic declaration default",
			source:  `fn generic<T: type>(value: string = "x") -> string { value }`,
			message: "parameter defaults are not yet admitted by the checker",
		},
	}
	for _, refusal := range refusals {
		t.Run(refusal.name, func(t *testing.T) {
			r := Compile(refusal.source)
			if r.Checked || !strings.Contains(diagnosticText(r), refusal.message) {
				t.Fatalf("new parameter contract leaked into checked output: %+v", r.Diagnostics)
			}
		})
	}
}
