package check_test

import (
	"fmt"
	"strings"
	"testing"

	"ergo.local/prototype/internal/sketch/check"
	"ergo.local/prototype/internal/sketch/syntax"
)

// A dropped class G S108 leaves the member's pinned error row a lower bound
// of its real row (rows.md, Capability rows 6; round 12 review). Checks that
// report presence stay; checks that report absence are silent only where
// the row they read holds poison; poison starts at the member whose S108
// was dropped and follows row flow, which a total catch stops. Each row
// names the review probe it comes from (round-12-review-receipts/probes).
const poisonHeader = `type E {}
type Q {}
type G[T] {}
type H[T] {}
func load() fails G[int] { return G[int]{} }
func boom() fails E { return E{} }
func quit() fails Q { return Q{} }
func keep[T, F](g H[T], f F) {}
func walk() {
	try boom()
	try w() catch {
	case G[int]:
	case H as h:
		h.keep(func() { walk() })
	}
}
func w() {
	try load()
	try walk()
}
`

const armError = "13:7 S108 generic type main.H needs type arguments"

var poisonRows = []struct {
	name, src string
	want      []string // line:col code message
}{
	{
		// Catch 4 reports presence: G[int] comes from load, a final
		// callee, so it is in w's real row. Catch 2 on E reads w's
		// poisoned row and is silent.
		name: "p1: a missing arm for an element of a poisoned row",
		src: `type E {}
type G[T] {}
type H[T] {}
func load() fails G[int] { return G[int]{} }
func boom() fails E { return E{} }
func keep[T, F](g H[T], f F) {}
func walk() {
	try boom()
	try w() catch {
	case G[int]:
	case H as h:
		h.keep(func() { walk() })
	}
}
func w() {
	try load()
	try walk()
}
func outer() {
	w() catch {
	case E:
	}
}
func main() {}
`,
		want: []string{
			"11:7 S108 generic type main.H needs type arguments",
			"20:6 S103 this catch does not handle {main.G[int]}; a catch without try handles every error of the row (rows.md, Catch 4)",
		},
	},
	{
		name: "p2: a reader in another component misses another callee's element",
		src: poisonHeader + `func both() {
	try w()
	try quit()
}
func outer() {
	both() catch {
	case G[int]:
	case E:
	}
}
func main() {}
`,
		want: []string{armError, "26:9 S103 this catch does not handle {main.Q}; a catch without try handles every error of the row (rows.md, Catch 4)"},
	},
	{
		name: "p3: the same through a generic reader",
		src: poisonHeader + `func gen[T](x T) {
	try w()
	try quit()
}
func outer() {
	gen(1) catch {
	case G[int]:
	case E:
	}
}
func main() {}
`,
		want: []string{armError, "26:9 S103 this catch does not handle {main.Q}; a catch without try handles every error of the row (rows.md, Catch 4)"},
	},
	{
		// A function literal's inferred row carries the poison like a
		// named function's.
		name: "p4: a local function literal reads the poisoned row",
		src: poisonHeader + `func outer() {
	let f = func() {
		try w()
		try quit()
	}
	f() catch {
	case G[int]:
	case E:
	}
}
func main() {}
`,
		want: []string{armError, "26:6 S103 this catch does not handle {main.Q}; a catch without try handles every error of the row (rows.md, Catch 4)"},
	},
	{
		name: "p5: a local and a package let bound to the poisoned function",
		src: `type E {}
type G[T] {}
type H[T] {}
func load() fails G[int] { return G[int]{} }
func boom() fails E { return E{} }
func keep[T, F](g H[T], f F) {}
func walk() {
	try boom()
	try w() catch {
	case G[int]:
	case H as h:
		h.keep(func() { walk() })
	}
}
func w() {
	try load()
	try walk()
}
let hook = w
func outer() {
	let k = w
	k() catch {
	case G[int]:
	case E:
	}
	hook() catch {
	case G[int]:
	case E:
	}
}
func main() {}
`,
		want: []string{"11:7 S108 generic type main.H needs type arguments"},
	},
	{
		name: "p6: callers with declared rows",
		src: poisonHeader + `func Outer() fails Q {
	w() catch {
	case G[int]:
	case E:
	}
	try quit()
}
func main() {
	w() catch {
	case E:
	}
}
`,
		want: []string{armError, "29:6 S103 this catch does not handle {main.G[int]}; a catch without try handles every error of the row (rows.md, Catch 4)"},
	},
	{
		// x1 reads w, so x1's row holds poison and x2's Catch 2 is silent.
		// x2's catch is total, so x2's row holds none: x1's try on it
		// cannot fail. The unrelated y1/y2 keep every diagnostic.
		name: "p7: a second component, and a total catch stops the poison",
		src: poisonHeader + `func x2() {
	x1() catch {
	case G[int]:
	case E:
	}
}
func x1() {
	try w()
	try quit()
	try x2()
}
func y1() {
	try quit()
	try y2()
}
func y2() {
	y1() catch {
	case E:
	}
}
func main() {}
`,
		want: []string{
			armError,
			"22:7 S103 this catch does not handle {main.Q}; a catch without try handles every error of the row (rows.md, Catch 4)",
			"30:2 S108 try on an expression that cannot fail (rows.md, Error rows 6)",
			"34:2 S108 try on an expression that cannot fail (rows.md, Error rows 6)",
			"37:7 S103 this catch does not handle {main.Q}; a catch without try handles every error of the row (rows.md, Catch 4)",
			"38:7 S108 main.E is not in the caught row {main.Q} (rows.md, Catch 2)",
		},
	},
	{
		// walk's S108 is kept (its capability row), so nothing is
		// poisoned: Q from quit is really unhandled.
		name: "p8: a kept S108 poisons nothing",
		src: `type Cap { M() }
type Q {}
type G[T] {}
func load() fails G[int] { return G[int]{} }
func quit() fails Q { return Q{} }
func keep[T, F](g G[T], f F) {}
func walk() {
	Cap.M()
	w() catch {
	case G as g:
		g.keep(func() { walk() })
	}
}
func w() { try load(); try quit(); walk() }
func main() {}
`,
		want: []string{
			"7:6 S108 walk is used as a value through the function literal at 11:10 inside its own recursive group; write its capability row: func walk() needs …",
			"9:6 S103 this catch does not handle {main.Q}; a catch without try handles every error of the row (rows.md, Catch 4)",
		},
	},
	{
		// The reporting pass decides z's row check after reportGroup: the
		// S108 is kept, so the check is reported.
		name: "p9: an absence check inside the component, S108 kept",
		src: `type Cap { M() }
type G[T] {}
func load() fails G[int] { return G[int]{} }
func keep[T, F](g G[T], f F) {}
func walk() {
	Cap.M()
	w() catch {
	case G as g:
		g.keep(func() { walk() })
	}
	try z()
}
func w() { try load(); walk() }
func z() {
	w() catch {
	case G[int]:
	}
	walk()
}
func main() {}
`,
		want: []string{
			"5:6 S108 walk is used as a value through the function literal at 9:10 inside its own recursive group; write its capability row: func walk() needs …",
			"11:2 S108 try on an expression that cannot fail (rows.md, Error rows 6)",
		},
	},
	{
		// w's row is walk's pinned row, empty: inside the component the
		// catch on it, and outside the try on it, read poison and are
		// silent (round 12 review T1, mutant K).
		name: "k1: try and catch on a poisoned row that is empty",
		src: `type E {}
type H[T] {}
func boom() fails E { return E{} }
func keep[T, F](g H[T], f F) {}
func walk() {
	try boom()
	try w() catch {
	case H as h:
		h.keep(func() { walk() })
	}
}
func w() { try walk() }
func outer() { try w() }
func main() {}
`,
		want: []string{"8:7 S108 generic type main.H needs type arguments"},
	},
	{
		// Poison starts only at a dropped S108: walk's S108 is reported,
		// so the absence checks on w's row stay, inside the component and
		// outside it.
		name: "kept error-row S108 poisons nothing",
		src: `type E {}
type Q {}
type G[T] {}
func boom() fails E { return E{} }
func load() fails G[int] { return G[int]{} }
func keep[T, F](g G[T], f F) {}
func walk() {
	try boom()
	try w() catch {
	case G[int] as g:
		g.keep(func() { try walk() })
	case Q:
	}
}
func w() { try load(); try walk() }
func outer() {
	w() catch {
	case G[int]:
	case Q:
	}
}
func main() {}
`,
		want: []string{
			"7:6 S108 walk is used as a value through the function literal at 11:10 inside its own recursive group; write its error row: func walk() error[…]",
			"12:7 S108 main.Q is not in the caught row {main.G[int]} (rows.md, Catch 2)",
			"19:7 S108 main.Q is not in the caught row {main.G[int]} (rows.md, Catch 2)",
		},
	},
}

func TestPoisonLowerBound(t *testing.T) {
	for _, settleOnly := range []bool{false, true} {
		for _, tc := range poisonRows {
			name := fmt.Sprintf("%s (settle-only %v)", tc.name, settleOnly)
			restore := func() {}
			if settleOnly {
				restore = check.SelfSettleOnly()
			}
			_, diags := check.CheckSource(tc.src)
			restore()
			var have []string
			for _, d := range diags {
				have = append(have, fmt.Sprintf("%d:%d %s %s", d.Line, d.Column, d.Code, d.Message))
			}
			if strings.Join(have, "\n") != strings.Join(tc.want, "\n") {
				t.Errorf("%s:\nhave %q\nwant %q", name, have, tc.want)
			}
		}
	}
	for _, tc := range poisonRows {
		if p := check.RefWalkProblems(tc.src); len(p) > 0 {
			t.Errorf("%s: RefWalkProblems:\n\t%s", tc.name, strings.Join(p, "\n\t"))
		}
		if p := check.OrderProblems(tc.src, 8); len(p) > 0 {
			t.Errorf("%s: OrderProblems:\n\t%s", tc.name, strings.Join(p, "\n\t"))
		}
	}
}

// The component in one file and its readers in another give the same
// diagnostics in both file orders, and two checks of one source in one
// process agree: the poison belongs to the check (round 12 review,
// TestR12ReviewMultiFile).
func TestPoisonTwoFiles(t *testing.T) {
	fileA := `type E {}
type Q {}
type G[T] {}
type H[T] {}
func load() fails G[int] { return G[int]{} }
func boom() fails E { return E{} }
func quit() fails Q { return Q{} }
func keep[T, F](g H[T], f F) {}
func walk() {
	try boom()
	try w() catch {
	case G[int]:
	case H as h:
		h.keep(func() { walk() })
	}
}
func w() {
	try load()
	try walk()
}
`
	fileB := `func both() {
	try w()
	try quit()
}
func outer() {
	both() catch {
	case G[int]:
	case E:
	}
}
func y1() {
	try quit()
	try y2()
}
func y2() {
	y1() catch {
	case E:
	}
}
func main() {}
`
	wantA := []string{"S108@13:7 generic type main.H needs type arguments"}
	wantB := []string{
		"S103@6:9 this catch does not handle {main.Q}; a catch without try handles every error of the row (rows.md, Catch 4)",
		"S108@13:2 try on an expression that cannot fail (rows.md, Error rows 6)",
		"S103@16:7 this catch does not handle {main.Q}; a catch without try handles every error of the row (rows.md, Catch 4)",
		"S108@17:7 main.E is not in the caught row {main.Q} (rows.md, Catch 2)",
	}
	for _, aFirst := range []bool{true, false} {
		order := []string{fileA, fileB}
		if !aFirst {
			order = []string{fileB, fileA}
		}
		var files []*syntax.File
		for _, s := range order {
			f, err := syntax.Parse(s)
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, f)
		}
		_, diags := check.Check("main", files, nil)
		byFile := map[int][]string{}
		for _, d := range diags {
			byFile[d.File] = append(byFile[d.File], fmt.Sprintf("%s@%d:%d %s", d.Code, d.Line, d.Column, d.Message))
		}
		a, b := 0, 1
		if !aFirst {
			a, b = 1, 0
		}
		if strings.Join(byFile[a], "\n") != strings.Join(wantA, "\n") || strings.Join(byFile[b], "\n") != strings.Join(wantB, "\n") {
			t.Errorf("A first %v:\nhave A %q\nwant A %q\nhave B %q\nwant B %q", aFirst, byFile[a], wantA, byFile[b], wantB)
		}
	}
	var runs [2]string
	for i := range runs {
		_, diags := check.CheckSource(fileA + fileB)
		var have []string
		for _, d := range diags {
			have = append(have, fmt.Sprintf("%s@%d:%d", d.Code, d.Line, d.Column))
		}
		runs[i] = strings.Join(have, " ")
	}
	if runs[0] != runs[1] {
		t.Errorf("two checks of one source differ:\n%s\n%s", runs[0], runs[1])
	}
}
