package check_test

import (
	"fmt"
	"strings"
	"testing"

	"ergo.local/prototype/internal/sketch/check"
)

// A protected name in a binder names the prelude rule. Comparing is advice
// for match arms; it cannot repair a local declaration.
func TestPreludeLetNamesUseBinderDiagnostic(t *testing.T) {
	for _, name := range []string{"Sleep", "len"} {
		t.Run(name, func(t *testing.T) {
			_, d := check.CheckSource(fmt.Sprintf("func F() { let %s = 3; _ = %s }\n", name, name))
			want := name + " is a prelude name; choose another name (rows.md, Totality 6; D-D3)"
			if len(d) != 1 || d[0].Code != check.CodeType || d[0].Message != want {
				t.Fatalf("want one prelude-binder diagnostic %q, got %s", want, describe(d))
			}
			_, arm := check.CheckSource(fmt.Sprintf("func F(n int) int { match n { case %s: return %s\ncase _: return 0 } }\n", name, name))
			if len(arm) != 1 || arm[0].Code != check.CodeType || !strings.Contains(arm[0].Message, "compare with if") {
				t.Fatalf("match-arm advice changed: %s", describe(arm))
			}
		})
	}
}
