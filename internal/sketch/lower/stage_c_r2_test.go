package lower

import (
	"strings"
	"testing"
)

// Stage C round 1 review witnesses, run through the public CLI.

// M1, M3, M4: cumulative catch coverage runs each arm; try over a partial
// catch propagates a failure of the caught expression's operand before the
// outer call runs; with blocks, trailing blocks and catch arms yield
// through a final if or match, and a void catch arm discards its tail.
func TestErgoRunReviewCForms(t *testing.T) {
	wantRun(t, `type E { B bool }
type X {}
let W = wire {}

func flag(b bool) int fails E { E{B: b} }
func collective(b bool) int { flag(b) catch { case E{B: true}: 1; case E{B: false}: 2 } }

func h(n int) int fails X needs Console {
	Console.Println("h ran")
	n
}
func whole() int fails E needs Console { try h(flag(true)) catch { case X: 0 } }
func caught() int fails E needs Console { try h(3) catch { case X: 0 } }

func withIf(b bool) int { with W { if b { 1 } else { 2 } } }
func withMatch(b bool) int { with W { match b { case true: 3; case false: 4 } } }
func withFallible(b bool) int fails E { with W { if b { flag(true) } else { 5 } } }
func voidFail() fails E { E{B: false} }
func voidCatch() { voidFail() catch { case E: 1 } }
func use(f func() int) int { f() }
func trailing(b bool) int { use() { if b { 6 } else { 7 } } }
func arm(b bool) int { flag(b) catch { case E{B: true}: if b { 8 } else { 0 }; case E: 9 } }

func main() needs Console {
	if collective(true) == 1 && collective(false) == 2 {
		Console.Println("collective")
	}
	let w = whole() catch { case E: 9 }
	if w == 9 {
		Console.Println("whole")
	}
	let c = caught() catch { case E: 0 }
	if c == 3 {
		Console.Println("caught")
	}
	if withIf(true) == 1 && withIf(false) == 2 && withMatch(true) == 3 && withMatch(false) == 4 {
		Console.Println("with")
	}
	let f1 = withFallible(true) catch { case E: 10 }
	let f2 = withFallible(false) catch { case E: 10 }
	if f1 == 10 && f2 == 5 {
		Console.Println("with fails")
	}
	voidCatch()
	if trailing(true) == 6 && trailing(false) == 7 && arm(true) == 8 && arm(false) == 9 {
		Console.Println("bodies")
	}
}
`, "collective\nwhole\nh ran\ncaught\nwith\nwith fails\nbodies\n")
}

// M2, M8: a catch alternative that leaves a name unbound and equality on a
// row value are refused before Go; neither program runs.
func TestErgoRunReviewCRefusals(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"unbound-alternative", `type E { N int }
type F {}
func g() int fails E | F { F }
func f() int { g() catch { case E{N: x}, F: x } }
func main() needs Console { if f() == 0 { Console.Println("unbound-is-zero") } }
`},
		{"unbound-alternative-reversed", `type E { N int }
type F {}
func g() int fails E | F { F }
func f() int { g() catch { case F, E{N: x}: x } }
func main() needs Console { if f() == 0 { Console.Println("unbound-is-zero") } }
`},
		{"row-equality", `type E { N int }
func g() int fails E { E{N: 1} }
func f() int { g() catch { case err: if err == err { 1 } else { 0 } } }
func main() needs Console { if f() == 1 { Console.Println("row equality accepted") } }
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := ergoRun(t, tc.src)
			if err == nil || !strings.Contains(out, "S108") || strings.Contains(out, "accepted") || strings.Contains(out, "zero") {
				t.Fatalf("want an S108 refusal; got %v\n%s", err, out)
			}
		})
	}
}
