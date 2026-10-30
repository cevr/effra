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

func TestMatchBranchesTargetEnclosingLoop(t *testing.T) {
	for _, tc := range []struct {
		name, src, code string
		count           int
	}{
		{"no-loop", `func F(n int) { match n { case 0: break
case _: continue } }`, check.CodeType, 2},
		{"loop", `func F(n int) int { let mut out = 0
for { match n { case 0: out = 1; break
case _: continue } }
return out }`, "", 0},
		{"loop-can-exit", `func F(n int) int { for { match n { case 0: break
case _: continue } } }`, check.CodeMissingRet, 1},
		{"nested-for", `func F(n int) int { match n { case 0: for { break }; return 1
case _: return 2 } }`, "", 0},
		{"barrier", `func F(n int) { for { with wire {} { match n { case 0: break
case _: continue } } } }`, check.CodeType, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, d := check.CheckSource(tc.src + "\n")
			if len(d) != tc.count {
				t.Fatalf("want %d diagnostics, got %s", tc.count, describe(d))
			}
			for _, x := range d {
				if x.Code != tc.code {
					t.Fatalf("want %s, got %s", tc.code, describe(d))
				}
			}
		})
	}
}
