package compiler

import (
	"strings"
	"testing"
)

func TestTypeAtUsesCanonicalAliasedChildren(t *testing.T) {
	tests := []struct {
		name   string
		source string
		needle string
		typeOf string
		kind   string
		length int
	}{
		{
			name:   "named call",
			source: nestedNamedCallSource(30),
			needle: `"ok"`,
			typeOf: "string",
			kind:   "string",
			length: 4,
		},
		{
			name:   "checked positional constructor",
			source: checkedPositionalSource(30),
			needle: "()",
			typeOf: "()",
			kind:   "unit",
			length: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := Compile(test.source)
			if !result.Checked {
				t.Fatalf("fixture did not check: %+v", result.Diagnostics)
			}
			offset := strings.LastIndex(test.source, test.needle)
			if offset < 0 {
				t.Fatalf("fixture does not contain %q", test.needle)
			}
			info, err := result.TypeAt(offset)
			if err != nil {
				t.Fatal(err)
			}
			if info.Kind != test.kind || info.Type.Success != test.typeOf {
				t.Fatalf("wrong expression identity: kind=%q type=%q", info.Kind, info.Type.Success)
			}
			if info.Span.Offset != offset || info.Span.Length != test.length {
				t.Fatalf("wrong expression span: got=%+v want offset=%d length=%d", info.Span, offset, test.length)
			}
		})
	}
}
