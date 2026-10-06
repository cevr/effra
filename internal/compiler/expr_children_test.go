package compiler

import (
	"strconv"
	"strings"
	"testing"
)

func TestCheckedPositionalDataCallsUseOneCanonicalChildPath(t *testing.T) {
	const deepest = 30
	var source strings.Builder
	for index := 0; index <= deepest; index++ {
		source.WriteString("record R")
		source.WriteString(strconv.Itoa(index))
		source.WriteString(" { value: ")
		if index == 0 {
			source.WriteString("()")
		} else {
			source.WriteString("R")
			source.WriteString(strconv.Itoa(index - 1))
		}
		source.WriteString(" }\n")
	}
	source.WriteString("fn deep() -> R")
	source.WriteString(strconv.Itoa(deepest))
	source.WriteString(" { ")
	for index := deepest; index >= 0; index-- {
		source.WriteString("R")
		source.WriteString(strconv.Itoa(index))
		source.WriteString("(")
	}
	source.WriteString("()")
	for index := 0; index <= deepest; index++ {
		source.WriteString(")")
	}
	source.WriteString(" }")

	result := Compile(source.String())
	if !result.Checked {
		t.Fatalf("positional data constructor fixture did not check: %+v", result.Diagnostics)
	}
	if lint := result.Lint(true); !lint.LintPassed {
		t.Fatalf("checked positional fixture changed lint admission: %+v", lint)
	}
	if _, err := result.Graph(); err != nil {
		t.Fatalf("checked positional fixture graph failed: %v", err)
	}
	if err := result.TestMode(false); err != nil {
		t.Fatalf("checked positional fixture test mode failed: %v", err)
	}
}
