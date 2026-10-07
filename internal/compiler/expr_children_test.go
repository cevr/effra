package compiler

import (
	"strconv"
	"strings"
	"testing"
)

func TestCheckedPositionalDataCallsUseOneCanonicalChildPath(t *testing.T) {
	source := checkedPositionalSource(30)
	result := Compile(source)
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

func checkedPositionalSource(deepest int) string {
	var source strings.Builder
	for index := 0; index <= deepest; index++ {
		source.WriteString("record R")
		source.WriteString(strconv.Itoa(index))
		source.WriteString(" { value: ")
		if index == 0 {
			source.WriteString("void")
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
	source.WriteString("void")
	for index := 0; index <= deepest; index++ {
		source.WriteString(")")
	}
	source.WriteString(" }")
	return source.String()
}
