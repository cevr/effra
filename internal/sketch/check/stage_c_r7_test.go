package check_test

import (
	"strings"
	"testing"

	"ergo.local/prototype/internal/sketch/check"
)

// Stage C round 6 fuzz finding (fuzz-check-C-r6-c346120f71c0e59a): the
// reference walk and typing agree on every name an arm binds, in valid
// and rejected programs alike. Each shape names a package function e (or
// err) and binds the same name in an arm, then reads it there: typing must
// resolve the read to the arm's binding, as the walk does, never to the
// package function, whatever is invalid around it.
func TestArmBindingsAgreeWithTheWalk(t *testing.T) {
	const decls = "type Order { N int }\ntype E { N int }\ntype F {}\ntype U = E | F\n" +
		"func g() int fails E { 1 }\nfunc gf() int fails E | F { 1 }\nfunc use(x any) {}\n"
	for _, tc := range []struct{ name, src string }{
		// the fuzz input
		{"fuzz", "func e() {(0) catch {case a as e: Order{e}}}"},
		// an `as` after each kind of invalid catch
		{"rest-as", "func e() { g() catch { case a as e: use(e) } }"},
		{"rest-as-cannot-fail", "func e() { (0) catch { case a as e: use(e) } }"},
		{"rest-with-another", "func e() { gf() catch { case E as e, a: use(e) } }"},
		{"wildcard-as", "func e() { g() catch { case _ as e: use(e) } }"},
		{"type-as-cannot-fail", "func e() { (0) catch { case E as e: use(e) } }"},
		{"type-as-not-in-row", "func e() { g() catch { case F as e: use(e) } }"},
		{"unknown-type-as", "func e() { g() catch { case Nope as e: use(e) } }"},
		{"record-as-unknown-field", "func e() { g() catch { case E{M: 1} as e: use(e) } }"},
		{"record-as-cannot-fail", "func e() { (0) catch { case E{N: 1} as e: use(e) } }"},
		{"unreachable-as", "func e() { g() catch { case E: 1; case E as e: use(e) } }"},
		{"after-rest-as", "func e() { g() catch { case err: use(err); case E as e: use(e) } }"},
		{"bind-in-record", "func e() { g() catch { case E{N: e}: use(e) } }"},
		{"bind-in-record-cannot-fail", "func e() { (0) catch { case E{N: e}: use(e) } }"},
		{"rest-name", "func err() { g() catch { case err: use(err) } }"},
		{"rest-name-cannot-fail", "func err() { (0) catch { case err: use(err) } }"},
		{"rest-name-shadowed-by-local", "func e() { let err = 1; g() catch { case err: use(err) } }"},
		{"rest-name-type-parameter", "func e[err any](x err) { g() catch { case err: use(x) } }"},
		{"rest-valid", "func e() { g() catch { case err: use(err) } }"},
		{"nested-match-in-arm", "func e(u U) { g() catch { case E as x: match u { case E as e: use(e); case F: use(x) } } }"},
		// match arms and nested patterns
		{"match-as", "func e(u U) { match u { case E as e: use(e); case F: use(0) } }"},
		{"match-as-invalid", "func e(u U) { match u { case Nope as e: use(e); case _: use(0) } }"},
		{"match-nested-as", "func e(p (U, int)) { match p { case (E{N: n} as e, _): use(e); case _: use(0) } }"},
		{"match-nested-invalid", "func e(p (U, int)) { match p { case (E{M: n} as e, _): use(e); case _: use(0) } }"},
		{"match-bind", "func e(n int) { match n { case e: use(e) } }"},
		{"match-bind-in-tuple-invalid", "func e(p (int, int)) { match p { case (e, e): use(e) } }"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := decls + tc.src + "\n"
			if p := check.RefWalkProblems(src); len(p) > 0 {
				_, d := check.CheckSource(src)
				t.Fatalf("reference walk: %s\ndiagnostics: %s", strings.Join(p, "; "), describe(d))
			}
		})
	}
}
