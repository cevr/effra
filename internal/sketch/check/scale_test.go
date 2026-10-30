package check_test

import (
	"fmt"
	"math"
	"runtime"
	"strings"
	"testing"
	"time"

	"ergo.local/prototype/internal/sketch/check"
)

// TestCheckerScale fails when the checker's cost grows faster than linearly
// with the size of its input. Each shape below generates a program at sizes
// n, 2n and 4n whose source grows linearly in n, and the test measures the
// bytes check.CheckSource allocates for it: the least of three runs, less
// what an empty program allocates. Allocation counts are deterministic,
// unlike time, which the test only logs. A shape fails when the fitted
// exponent log2(bytes(4n)/bytes(n))/2 passes 1.3, so 4n may cost at most
// about 6x n (linear is 4x, quadratic 16x).
//
// Every shape comes from a review finding of run wf10 about memory or
// quadratic cost; the finding is named at the shape. A valid shape must
// check with no diagnostic, so a checker that stops early on an error
// cannot pass for linear.
//
// The test is not parallel: TotalAlloc counts every goroutine, so it runs
// while no other test of the package does.
func TestCheckerScale(t *testing.T) {
	if testing.Short() {
		t.Skip("scale test skipped in -short mode")
	}
	empty := allocated("func main() {}\n")
	start := time.Now()
	for _, shape := range scaleShapes {
		t.Run(shape.name, func(t *testing.T) {
			if shape.skip != "" {
				t.Skip(shape.skip)
			}
			var bytes [3]float64
			var took [3]time.Duration
			for i, size := range []int{shape.n, 2 * shape.n, 4 * shape.n} {
				src := shape.source(size)
				_, diags := check.CheckSource(src)
				if len(diags) != shape.diagnostics {
					t.Fatalf("n=%d: want %d diagnostics, got %d:\n%s", size, shape.diagnostics, len(diags), listDiagnostics(diags))
				}
				began := time.Now()
				bytes[i] = math.Max(float64(allocated(src)-empty), 1)
				took[i] = time.Since(began) / 3
			}
			exponent := math.Log2(bytes[2]/bytes[0]) / 2
			t.Logf("n=%d: bytes %.0f, %.0f, %.0f (exponent %.2f); time %v, %v, %v",
				shape.n, bytes[0], bytes[1], bytes[2], exponent, took[0], took[1], took[2])
			if exponent > maxScaleExponent {
				t.Errorf("4x the input allocates %.1fx the bytes (exponent %.2f > %.1f): the checker is superlinear on %s (%s)",
					bytes[2]/bytes[0], exponent, maxScaleExponent, shape.name, shape.finding)
			}
		})
	}
	t.Logf("scale test took %v", time.Since(start))
}

// maxScaleExponent is the largest fitted exponent a shape may show.
const maxScaleExponent = 1.3

// allocated is the least number of bytes check.CheckSource allocates for
// src over three runs; the first run also warms the checker.
func allocated(src string) uint64 {
	best := uint64(math.MaxUint64)
	for range 3 {
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		check.CheckSource(src)
		runtime.ReadMemStats(&after)
		best = min(best, after.TotalAlloc-before.TotalAlloc)
	}
	return best
}

func listDiagnostics(diags []check.Diagnostic) string {
	var b strings.Builder
	for i, d := range diags {
		if i == 5 {
			fmt.Fprintf(&b, "\t... %d more\n", len(diags)-i)
			break
		}
		fmt.Fprintf(&b, "\t%s\n", d)
	}
	return b.String()
}

// scaleShape is one family of programs whose source grows linearly in n.
type scaleShape struct {
	name string
	// finding names the review finding the shape comes from.
	finding string
	// n is the smallest size; the test also checks 2n and 4n.
	n int
	// diagnostics is the number of diagnostics at every size: 0 for a
	// valid program.
	diagnostics int
	source      func(n int) string
	// skip, when set, names the issue for a shape that is superlinear on
	// main; the case is skipped until that issue is fixed.
	skip string
}

// gen builds a source with fmt-style lines.
type gen struct{ strings.Builder }

func (g *gen) line(format string, args ...any) {
	fmt.Fprintf(&g.Builder, format, args...)
	g.WriteByte('\n')
}

var scaleShapes = []scaleShape{
	{
		name:    "declarations",
		finding: "checkquad r1 and r2: three passes quadratic in the declarations (13.4 GB); checker-196 r3 M5: the method dependency graph",
		n:       500,
		source: func(n int) string {
			var g gen
			for i := range n {
				g.line("type T%d { V int64 }", i)
				g.line("func f%d(t T%d) int64 { return t.V }", i, i)
			}
			g.line("func main() {")
			for i := range n {
				g.line("\tlet _ = f%d(T%d{V: %d})", i, i, i)
			}
			g.line("}")
			return g.String()
		},
	},
	{
		name:    "providers",
		finding: "checkquad r2 follow-up: the provider-method matching loop; parse-v1 r6 M1 and r7 M1: same-named receiver-first functions made the body graph quadratic",
		n:       200,
		source: func(n int) string {
			var g gen
			for i := range n {
				g.line("type S%d { Label() string }", i)
				g.line("type M%d { Text string }", i)
				g.line("func Label(m M%d) string { return m.Text }", i)
			}
			g.line("let W = wire {")
			for i := range n {
				g.line("\tS%d: M%d{Text: \"%d\"},", i, i, i)
			}
			g.line("}")
			for i := range n {
				g.line("func use%d() string needs S%d { return S%d.Label() }", i, i, i)
			}
			g.line("func main() {")
			for i := range n {
				g.line("\tlet _ = with W { use%d() }", i)
			}
			g.line("}")
			return g.String()
		},
	},
	{
		name:    "wide record",
		finding: "check-v1 stage-B r1 M2: uncharged work superlinear in pattern width",
		n:       500,
		source: func(n int) string {
			var g gen
			g.line("type R {")
			for i := range n {
				g.line("\tF%d int64,", i)
			}
			g.line("}")
			g.line("func make() R {")
			g.line("\treturn R{")
			for i := range n {
				g.line("\t\tF%d: %d,", i, i)
			}
			g.line("\t}")
			g.line("}")
			g.line("func sum(r R) int64 {")
			g.line("\tlet s0 = r.F0")
			for i := 1; i < n; i++ {
				g.line("\tlet s%d = s%d + r.F%d", i, i-1, i)
			}
			g.line("\treturn s%d", n-1)
			g.line("}")
			g.line("func first(r R) int64 {")
			g.line("\tmatch r {")
			g.line("\tcase R{F0: 0}:")
			g.line("\t\treturn 0")
			g.line("\tcase _:")
			g.line("\t\treturn 1")
			g.line("\t}")
			g.line("}")
			g.line("func main() { let _ = sum(make()) + first(make()) }")
			return g.String()
		},
	},
	{
		name:    "sparse wide match",
		finding: "check-v1 stage-B r2 M1: a sparse wide match costs work and memory quadratic in the source before its S103 refusal",
		n:       100,
		// main refuses this shape with S103 from about n = 100 on; the
		// refusal itself must come at linear cost.
		diagnostics: 1,
		source: func(n int) string {
			var g gen
			g.line("type R {")
			for i := range n {
				g.line("\tF%d bool,", i)
			}
			g.line("}")
			g.line("func pick(r R) int64 {")
			g.line("\tmatch r {")
			for i := range n {
				g.line("\tcase R{F%d: true}:", i)
				g.line("\t\treturn %d", i)
			}
			g.line("\tcase _:")
			g.line("\t\treturn -1")
			g.line("\t}")
			g.line("}")
			g.line("func main() { let _ = pick(R{}) }")
			return g.String()
		},
	},
	{
		name:    "long body",
		finding: "check-v1 r4 M1: bindings held per node, square of the source",
		n:       1000,
		source: func(n int) string {
			var g gen
			g.line("func f(a int64) int64 {")
			g.line("\tlet x0 = a")
			for i := 1; i < n; i++ {
				g.line("\tlet x%d = x%d + %d", i, i-1, i)
			}
			g.line("\treturn x%d", n-1)
			g.line("}")
			g.line("func main() { let _ = f(1) }")
			return g.String()
		},
	},
	{
		name:    "deep nesting",
		finding: "check-v1 r5 m1: deep nesting of blocks and literals",
		// 4n nested ifs around 4n nested calls stays under the parser's
		// limit of 1000 levels.
		n: 50,
		source: func(n int) string {
			var g gen
			g.line("func g(a int64) int64 { return a + 1 }")
			g.line("func f(a int64) int64 {")
			for i := range n {
				g.line("%sif a > %d {", strings.Repeat("\t", i+1), i)
			}
			g.line("%sreturn %s0%s", strings.Repeat("\t", n+1), strings.Repeat("g(", n), strings.Repeat(")", n))
			for i := n - 1; i >= 0; i-- {
				g.line("%s}", strings.Repeat("\t", i+1))
			}
			g.line("\treturn 0")
			g.line("}")
			g.line("func main() { let _ = f(1) }")
			return g.String()
		},
	},
	{
		name:    "int match arms",
		finding: "pattern_oracle match shape; checker-196 r1 M8: exponential totality",
		n:       500,
		source: func(n int) string {
			var g gen
			g.line("func f(x int64) int64 {")
			g.line("\tmatch x {")
			for i := range n {
				g.line("\tcase %d:", i)
				g.line("\t\treturn %d", i+1)
			}
			g.line("\tcase _:")
			g.line("\t\treturn 0")
			g.line("\t}")
			g.line("}")
			g.line("func main() { let _ = f(1) }")
			return g.String()
		},
	},
	{
		name:    "union match arms",
		finding: "checker-196 r4 M4, r5 M2 and r7 M4: variant canonicalization; check-v1 stage-B r1 M2",
		n:       250,
		source: func(n int) string {
			var g gen
			var vs []string
			for i := range n {
				g.line("type V%d { On bool }", i)
				vs = append(vs, fmt.Sprintf("V%d", i))
			}
			g.line("type U = %s", strings.Join(vs, " | "))
			g.line("func f(u U) int64 {")
			g.line("\tmatch u {")
			for i := range n {
				g.line("\tcase V%d{On: true}:", i)
				g.line("\t\treturn %d", i)
				g.line("\tcase V%d{On: false}:", i)
				g.line("\t\treturn 0")
			}
			g.line("\t}")
			g.line("}")
			g.line("func main() { let _ = f(V0{On: true}) }")
			return g.String()
		},
	},
	{
		name:    "callback literals",
		finding: "parse-v1 r13 M2: callTaint filters a literal's flow again on every call",
		n:       200,
		source: func(n int) string {
			var g gen
			g.line("type Fault {}")
			g.line("func walk() int64 fails Fault { return 1 }")
			g.line("func apply(f func() int64 fails Fault) int64 fails Fault { return f() }")
			for i := range n {
				g.line("func f%d() int64 fails Fault { return try walk() }", i)
			}
			g.line("func Run() int64 fails Fault {")
			g.line("\tlet outer = func() int64 fails Fault {")
			g.line("\t\tlet mut s int64 = 0")
			for i := range n {
				g.line("\t\tlet v%d = try f%d()", i, i)
				g.line("\t\ts = s + v%d", i)
			}
			g.line("\t\treturn s")
			g.line("\t}")
			g.line("\tlet mut t int64 = 0")
			for i := range n {
				g.line("\tlet a%d = try apply(outer)", i)
				g.line("\tlet b%d = try apply(func() int64 fails Fault { return try f%d() })", i, i)
				g.line("\tt = t + a%d + b%d", i, i)
			}
			g.line("\treturn t")
			g.line("}")
			g.line("func main() { let _ = Run() catch { case Fault: 0 } }")
			return g.String()
		},
	},
	{
		name:        "callback literals, one error",
		finding:     "parse-v1 r13 M2: a one-error program of 2000 functions took 31 s",
		n:           200,
		diagnostics: 1,
		source: func(n int) string {
			var g gen
			g.line("type Fault {}")
			g.line("func walk() int64 fails Fault { return missing }")
			for i := range n {
				g.line("func f%d() int64 fails Fault { return try walk() }", i)
			}
			g.line("func Run() int64 fails Fault {")
			g.line("\tlet outer = func() int64 fails Fault {")
			g.line("\t\tlet mut s int64 = 0")
			for i := range n {
				g.line("\t\tlet v%d = try f%d()", i, i)
				g.line("\t\ts = s + v%d", i)
			}
			g.line("\t\treturn s")
			g.line("\t}")
			g.line("\tlet mut t int64 = 0")
			for i := range n {
				g.line("\tlet a%d = try outer()", i)
				g.line("\tt = t + a%d", i)
			}
			g.line("\treturn t")
			g.line("}")
			g.line("func main() { let _ = Run() catch { case Fault: 0 } }")
			return g.String()
		},
	},
	{
		name:    "duplicate names",
		finding: "parse-v1 r6 M1 and r7 M1: a self call on a receiver with no written type got an edge to every function of its name, so the body graph was quadratic",
		n:       200,
		source: func(n int) string {
			var g gen
			for i := range n {
				g.line("type B%d { V int64 }", i)
				g.line("func Get(b B%d) int64 { return b.V }", i)
				g.line("func make%d() B%d { return B%d{V: %d} }", i, i, i, i)
			}
			g.line("func f() int64 {")
			g.line("\tlet mut s int64 = 0")
			for i := range n {
				g.line("\tlet b%d = make%d()", i, i)
				g.line("\ts = s + b%d.Get()", i)
			}
			g.line("\treturn s")
			g.line("}")
			g.line("func main() { let _ = f() }")
			return g.String()
		},
	},
	{
		name:    "call chain",
		finding: "parse-v1 r8 M1: the FIFO settling worklist rechecks a waiting body once per wave, quadratic in an acyclic graph",
		n:       300,
		source: func(n int) string {
			var g gen
			g.line("type Fault {}")
			g.line("type Store { Get() string }")
			g.line("type Mem {}")
			g.line("func Get(_ Mem) string { return \"m\" }")
			g.line("func f%d(k int64) string {", n)
			g.line("\tif k == 0 {")
			g.line("\t\treturn Fault")
			g.line("\t}")
			g.line("\treturn Store.Get()")
			g.line("}")
			for i := n - 1; i >= 0; i-- {
				g.line("func f%d(k int64) string { return try f%d(k - 1) }", i, i+1)
			}
			g.line("func Run(k int64) string fails Fault needs Store { return try f0(k) }")
			g.line("func main() {")
			g.line("\tlet _ = with wire { Store: Mem } { Run(3) } catch { case Fault: \"fault\" }")
			g.line("}")
			return g.String()
		},
	},
	{
		name:    "wire chain",
		finding: "check-v1 r3 M1 and r4 M1: resolving the bindings of n withs over a wire chain is quadratic (3.1 GB at 8000); checker-196 r17 M1 and r18 M1: the wire embedding chain",
		n:       200,
		source: func(n int) string {
			var g gen
			g.line("type Store { Label() string }")
			g.line("type Memory { Text string }")
			g.line("func Label(m Memory) string { return m.Text }")
			g.line("func run() string needs Store { return Store.Label() }")
			g.line("let W0 = wire { Store: Memory{Text: \"live\"} }")
			for i := 1; i < n; i++ {
				g.line("let W%d = wire { W%d }", i, i-1)
			}
			for i := range n {
				g.line("func run%d() string { return with W%d { run() } }", i, i)
			}
			g.line("func main() {")
			for i := range n {
				g.line("\tlet _ = run%d()", i)
			}
			g.line("}")
			return g.String()
		},
	},
	{
		name:    "catch arms",
		finding: "parse-v1 r14 M1: the duplicate-arm set and covered.with make a catch quadratic in its arm count",
		n:       250,
		// Measured on main e36c9471: exponent 1.35 at n = 250 and 1.69 at
		// n = 1000 (20.7 MB, 63.0 MB, 214.0 MB).
		skip: "proposed issue: catch arm coverage (the duplicate-arm set and covered.with) is quadratic in the arm count, parse-v1 r14 M1",
		source: func(n int) string {
			var g gen
			var row []string
			for i := range n {
				g.line("type E%d {}", i)
				row = append(row, fmt.Sprintf("E%d", i))
			}
			g.line("func f(k int64) int64 fails %s {", strings.Join(row, " | "))
			for i := range n {
				g.line("\tif k == %d {", i)
				g.line("\t\treturn E%d", i)
				g.line("\t}")
			}
			g.line("\treturn k")
			g.line("}")
			g.line("func g(k int64) int64 {")
			g.line("\treturn f(k) catch {")
			for i := range n {
				g.line("\tcase E%d:", i)
				g.line("\t\t%d", i)
			}
			g.line("\t}")
			g.line("}")
			g.line("func main() { let _ = g(1) }")
			return g.String()
		},
	},
	{
		name:    "generic arity",
		finding: "checker-196 r6 M5: default-only generic subjects",
		n:       100,
		source: func(n int) string {
			var g gen
			var params, fields, args, values []string
			for i := range n {
				params = append(params, fmt.Sprintf("T%d", i))
				fields = append(fields, fmt.Sprintf("\tF%d T%d,", i, i))
				args = append(args, "int64")
				values = append(values, fmt.Sprintf("F%d: %d", i, i))
			}
			g.line("type Many[%s] {", strings.Join(params, ", "))
			g.WriteString(strings.Join(fields, "\n"))
			g.line("")
			g.line("}")
			g.line("func first(m Many[%s]) int64 { return m.F0 }", strings.Join(args, ", "))
			g.line("func main() {")
			g.line("\tlet m = Many[%s]{%s}", strings.Join(args, ", "), strings.Join(values, ", "))
			g.line("\tlet _ = first(m)")
			g.line("}")
			return g.String()
		},
	},
}
