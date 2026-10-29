package check_test

import (
	"fmt"
	"strings"
	"testing"

	"ergo.local/prototype/internal/sketch/check"
)

// TestInvalidCalleeNoCascade (round 7 review M2): a callee whose type is
// invalid (a std module lower-v1 brings, refused at its import with S109;
// a name that resolves to nothing) checks none of the literals and blocks
// passed to it, wherever the value flows: pkg.F itself, a local bound to
// it (once or twice), a parameter, a field and a call result whose
// declared type is invalid. So b, called only inside the callback, is no
// class G value use of its group with a, and F, which returns b(), does not
// "return no value": the one diagnostic is the refusal itself. The first
// two forms with a trailing block and with a function literal are the
// reviewer's witnesses.
func TestInvalidCalleeNoCascade(t *testing.T) {
	const tail = "type Bang {}\nfunc F() fails Bang { return b() }\nfunc main() {}\n"
	const refused = "S109 1:8"
	forms := []struct {
		name string
		src  func(cb string) string
		want string
	}{
		{"pkg.F", func(cb string) string {
			return "import task\nfunc a() { " + strings.ReplaceAll(cb, "@", "task.Go") + "; return Bang{} }\nfunc b() { return a() }\n"
		}, refused},
		{"a local bound to pkg.F", func(cb string) string {
			return "import task\nfunc a() { let launch = task.Go; " + strings.ReplaceAll(cb, "@", "launch") + "; return Bang{} }\nfunc b() { return a() }\n"
		}, refused},
		{"a local bound to such a local", func(cb string) string {
			return "import task\nfunc a() { let l1 = task.Go; let launch = l1; " + strings.ReplaceAll(cb, "@", "launch") + "; return Bang{} }\nfunc b() { return a() }\n"
		}, refused},
		{"a parameter of a refused type", func(cb string) string {
			return "import task\nfunc a(launch task.Fn) { " + strings.ReplaceAll(cb, "@", "launch") + "; return Bang{} }\nfunc b() { return a(task.Go) }\n"
		}, refused},
		{"a field of a refused type", func(cb string) string {
			return "import task\ntype S { run task.Fn }\nfunc a(s S) { " + strings.ReplaceAll(cb, "@", "s.run") + "; return Bang{} }\nfunc b() { return a(S{}) }\n"
		}, refused},
		{"a call result of a refused type", func(cb string) string {
			return "import task\nfunc mk() task.Fn { return task.Go }\nfunc a() { " + strings.ReplaceAll(cb, "@", "mk()") + "; return Bang{} }\nfunc b() { return a() }\n"
		}, refused},
		{"an unknown name", func(cb string) string {
			return "func a() { " + strings.ReplaceAll(cb, "@", "nope") + "; return Bang{} }\nfunc b() { return a() }\n"
		}, "S104 1:12"},
		{"a local bound to an unknown name", func(cb string) string {
			return "func a() { let launch = nope; " + strings.ReplaceAll(cb, "@", "launch") + "; return Bang{} }\nfunc b() { return a() }\n"
		}, "S104 1:25"},
	}
	callbacks := []struct{ name, cb string }{
		{"trailing block", "@() { b() }"},
		{"function literal", "@(func() { b() })"},
	}
	for _, f := range forms {
		for _, cb := range callbacks {
			t.Run(f.name+", "+cb.name, func(t *testing.T) {
				src := f.src(cb.cb) + tail
				_, diags := check.CheckSource(src)
				var got []string
				for _, d := range diags {
					got = append(got, fmt.Sprintf("%s %d:%d", d.Code, d.Line, d.Column))
				}
				if strings.Join(got, ", ") != f.want {
					t.Errorf("want only %s; got %v\n%s", f.want, diags, src)
				}
				if p := check.RefWalkProblems(src); len(p) > 0 {
					t.Errorf("references: %s", strings.Join(p, "; "))
				}
				if p := check.OrderProblems(src, 5); len(p) > 0 {
					t.Errorf("order: %s", strings.Join(p, "; "))
				}
			})
		}
	}
}
