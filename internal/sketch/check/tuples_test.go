package check_test

import (
	"strings"
	"testing"

	"ergo.local/prototype/internal/sketch/check"
)

// TestTupleCallbackFrame: a tuple is typed (196t, D-B6), so a tuple inside
// a callback frame that names a package function resolves the name in
// typing as the reference walk does (stage A follow-up, class H; the
// fuzzer's FuzzCheckPastParser-610ab4459d29f278).
func TestTupleCallbackFrame(t *testing.T) {
	src := "func H() int { return 1 }\nfunc run(f func()) {}\nfunc F() { run(func() { _ = (H(), 1) }) }\n"
	if _, diags := check.CheckSource(src); len(diags) > 0 {
		t.Fatalf("want a clean program, got %s", describe(diags))
	}
	if p := check.RefWalkProblems(src); len(p) > 0 {
		t.Fatalf("reference walk: %s", strings.Join(p, "; "))
	}
}
