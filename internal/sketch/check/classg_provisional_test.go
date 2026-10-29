package check_test

import (
	"fmt"
	"strings"
	"testing"

	"ergo.local/prototype/internal/sketch/check"
)

// The generic catch arm `case G as g` takes the instance the caught row
// holds, so while the component's rows are inferred (the silent pass whose
// record class G reads) g's type waits on a row: it is provisional, not
// invalid. A callback frame that typing skips under it keeps its class G
// uses, so the member is refused as when the arm writes `case G[int]`
// (round 9 review M1; rows.md, Capability rows 6).
const garmHeader = "type Cap { M() }\ntype G[T] {}\nfunc load() fails G[int] { return G[int]{} }\n"

var provisionalRows = []struct {
	name, src string
	want      []string // line:col code message
}{
	{
		name: "garm-classg (function literal, capability row)",
		src: garmHeader + "func keep[T, F](g G[T], f F) {}\n" +
			"func walk() {\n\tCap.M()\n\tw() catch {\n\tcase G as g:\n\t\tg.keep(func() { walk() })\n\t}\n}\n" +
			"func w() { try load(); walk() }\nfunc main() {}\n",
		want: []string{"5:6 S108 walk is used as a value through the function literal at 9:10 inside its own recursive group; write its capability row: func walk() needs …"},
	},
	{
		name: "garm-classg-errors (error row, try w() catch)",
		src: "type Bang {}\ntype G[T] {}\nextern go fixture.mark func mark() fails Bang\n" +
			"func load() fails G[int] { return G[int]{} }\nfunc keep[T, F](g G[T], f F) {}\n" +
			"func walk() {\n\ttry mark()\n\ttry w() catch {\n\tcase G as g:\n\t\tg.keep(func() { try walk() })\n\t}\n}\n" +
			"func w() { try load(); try walk() }\nfunc main() {}\n",
		want: []string{"6:6 S108 walk is used as a value through the function literal at 10:10 inside its own recursive group; write its error row: func walk() error[…]"},
	},
	{
		name: "garm-classg-block (trailing block)",
		src: garmHeader + "func run[T, R](g G[T], body func() needs R) needs R { body() }\n" +
			"func walk() {\n\tCap.M()\n\tw() catch {\n\tcase G as g:\n\t\tg.run() { walk() }\n\t}\n}\n" +
			"func w() { try load(); walk() }\nfunc main() {}\n",
		want: []string{"5:6 S108 walk is used as a value through the trailing block at 9:11 inside its own recursive group; write its capability row: func walk() needs …"},
	},
	{
		name: "garm-classg-control (case G[int] as g)",
		src: garmHeader + "func keep[T, F](g G[T], f F) {}\n" +
			"func walk() {\n\tCap.M()\n\tw() catch {\n\tcase G[int] as g:\n\t\tg.keep(func() { walk() })\n\t}\n}\n" +
			"func w() { try load(); walk() }\nfunc main() {}\n",
		want: []string{"5:6 S108 walk is used as a value through the function literal at 9:10 inside its own recursive group; write its capability row: func walk() needs …"},
	},
	// The same frame reached by other routes: an argument of a generic
	// call that fails on g, a local bound to g, a value derived from g, a
	// frame inside the skipped frame, a trailing block of a valid callee.
	{
		name: "argument of a call that fails on g",
		src: garmHeader + "func keep[T, F](g G[T], f F) {}\n" +
			"func walk() {\n\tCap.M()\n\tw() catch {\n\tcase G as g:\n\t\tkeep(g, func() { walk() })\n\t}\n}\n" +
			"func w() { try load(); walk() }\nfunc main() {}\n",
		want: []string{"5:6 S108 walk is used as a value through the function literal at 9:11 inside its own recursive group; write its capability row: func walk() needs …"},
	},
	{
		name: "local bound to g",
		src: garmHeader + "func keep[T, F](g G[T], f F) {}\n" +
			"func walk() {\n\tCap.M()\n\tw() catch {\n\tcase G as g:\n\t\tlet h = g\n\t\th.keep(func() { walk() })\n\t}\n}\n" +
			"func w() { try load(); walk() }\nfunc main() {}\n",
		want: []string{"5:6 S108 walk is used as a value through the function literal at 10:10 inside its own recursive group; write its capability row: func walk() needs …"},
	},
	{
		name: "local bound to a call on g",
		src: garmHeader + "func keep[T, F](g G[T], f F) {}\nfunc id[T](x T) T { return x }\n" +
			"func walk() {\n\tCap.M()\n\tw() catch {\n\tcase G as g:\n\t\tlet h = id(g)\n\t\th.keep(func() { walk() })\n\t}\n}\n" +
			"func w() { try load(); walk() }\nfunc main() {}\n",
		want: []string{"6:6 S108 walk is used as a value through the function literal at 11:10 inside its own recursive group; write its capability row: func walk() needs …"},
	},
	{
		name: "frame inside the skipped frame",
		src: garmHeader + "func keep[T, F](g G[T], f F) {}\n" +
			"func walk() {\n\tCap.M()\n\tw() catch {\n\tcase G as g:\n\t\tg.keep(func() { let k = func() { walk() }\n\t\t\tk() })\n\t}\n}\n" +
			"func w() { try load(); walk() }\nfunc main() {}\n",
		want: []string{"5:6 S108 walk is used as a value through the function literal at 9:27 inside its own recursive group; write its capability row: func walk() needs …"},
	},
	{
		name: "trailing block of a valid callee",
		src: garmHeader + "func run[T, R](g G[T], body func() needs R) needs R { body() }\n" +
			"func walk() {\n\tCap.M()\n\tw() catch {\n\tcase G as g:\n\t\trun(g) { walk() }\n\t}\n}\n" +
			"func w() { try load(); walk() }\nfunc main() {}\n",
		want: []string{"5:6 S108 walk is used as a value through the trailing block at 9:10 inside its own recursive group; write its capability row: func walk() needs …"},
	},
	// A frame under a callee invalid for a diagnosed reason stays invalid
	// beside a provisional value in the same body: only the S104.
	{
		name: "invalid frame beside a provisional value",
		src: garmHeader + "func keep[T, F](g G[T], f F) {}\n" +
			"func walk() {\n\tCap.M()\n\tw() catch {\n\tcase G as g:\n\t\tkeep(g, 1)\n\t}\n\tlet x = nope\n\tx.m(func() { walk() })\n}\n" +
			"func w() { try load(); walk() }\nfunc main() {}\n",
		want: []string{"11:10 S104 unknown name nope"},
	},
}

func TestClassGProvisionalFrame(t *testing.T) {
	for _, settleOnly := range []bool{false, true} {
		for _, tc := range provisionalRows {
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
	for _, tc := range provisionalRows {
		if p := check.RefWalkProblems(tc.src); len(p) > 0 {
			t.Errorf("%s: RefWalkProblems:\n\t%s", tc.name, strings.Join(p, "\n\t"))
		}
		if p := check.OrderProblems(tc.src, 8); len(p) > 0 {
			t.Errorf("%s: OrderProblems:\n\t%s", tc.name, strings.Join(p, "\n\t"))
		}
	}
}
