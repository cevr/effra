package std_test

import (
	"strings"
	"testing"

	"ergo.local/prototype/internal/sketch/check"
	"ergo.local/prototype/internal/sketch/std"
	v1 "ergo.local/prototype/runtime/v1"
)

// v1Packages are the std packages whose Go bodies are runtime/v1's
// (docs/spec/stdlib.md, v1 packages).
var v1Packages = []string{"queue", "scope", "slice", "task"}

const v1Path = "ergo.local/prototype/runtime/v1"

// unbound are the exported Go methods, of a type a std extern returns,
// that no Ergo operation binds, each with the reason. A body that binds no
// declaration and is not listed here fails TestEachExternHasItsGoBody.
var unbound = map[string]string{
	// Info request for check-v1 stage D: Values() Seq[T] needs the
	// prelude's Seq[T] and its loop domain (lower-v1 Q1).
	"Queue.Values": "the prelude has no Seq[T] yet",
	// The return of a Task (Tasks rule 8): only the lowering calls it.
	"Task.Move": "lowering only",
}

// TestEachModuleChecks checks each v1 package with the checker that
// checks user code, so no std declaration is a compiler branch.
func TestEachModuleChecks(t *testing.T) {
	for _, path := range v1Packages {
		if _, ok := std.Package(path); !ok {
			t.Errorf("std.Package(%q): not embedded", path)
			continue
		}
		if _, err := check.Std.Import(path); err != nil {
			t.Errorf("check.Std.Import(%q): %v", path, err)
		}
	}
}

// TestCallsToEachFunctionCheck checks a program that imports each v1
// package and calls each function and each operation it declares; and,
// as a control, that the signatures carry their rows: a Push whose
// failure is not handled is refused.
func TestCallsToEachFunctionCheck(t *testing.T) {
	const program = `import task
import scope
import slice
import queue

type Fault {}

func work(n int64) int64 fails Fault {
	if n < 0 {
		return Fault
	}
	return n * 2
}

func produce(q queue.Queue[int64]) fails queue.Closed {
	try q.Push(1)
	try q.Push(2)
	q.Close()
}

func main() fails Fault | queue.Closed needs Console {
	scope.OnExit() { Console.Println("bye") }
	let t = task.Go() { work(21) }
	let v = try t.Wait()
	let u = task.Go() { Console.Println("child") }
	u.Stop()
	let q = queue.New[int64](2)
	try produce(q)
	let a = try q.Pop()
	let b = try q.Pop()
	let xs = slice.WithCapacity[int64](4)
	let ys = xs.Append(a + b + v)
	Console.Println("\(ys)")
}
`
	if _, diags := check.CheckSource(program); len(diags) > 0 {
		t.Fatalf("diagnostics: %v", diags)
	}
	const unhandled = `import queue

func main() {
	let q = queue.New[string](1)
	q.Push("x")
}
`
	// The code is the checker's (S101 since check-v1 stage C); this test
	// pins only that the row reaches the call.
	_, diags := check.CheckSource(unhandled)
	if len(diags) != 1 || diags[0].Line != 5 || !strings.Contains(diags[0].Message, "queue.Closed") {
		t.Fatalf("an unhandled queue.Closed: got %v, want one diagnostic at line 5 naming queue.Closed", diags)
	}
}

// TestEachExternHasItsGoBody ties each extern declaration of the v1
// packages, and each operation of a type such an extern returns, to the Go
// body that implements it, read from the embedded runtime source as the
// lowering reads it (v1.BodyOf). The body's parameters after its hidden
// ones stand for the Ergo parameters, one each, with a Go function for a
// function parameter; its results are the Ergo results and error when the
// Ergo signature has an error row; and a function whose result holds a
// Task takes the caller's frame, the hidden destination (Tasks rule 8).
func TestEachExternHasItsGoBody(t *testing.T) {
	bound := map[string]bool{}
	for _, path := range v1Packages {
		pkg, err := check.Std.Import(path)
		if err != nil {
			t.Fatalf("check.Std.Import(%q): %v", path, err)
		}
		externs := 0
		for _, f := range pkg.Funcs {
			if f.Decl == nil || f.Decl.Extern == nil {
				continue
			}
			externs++
			target, name := f.Decl.Extern.Split()
			if target != v1Path {
				t.Errorf("%s.%s binds %s.%s; a v1 package binds %s", path, f.Name, target, name, v1Path)
				continue
			}
			body, err := v1.BodyOf(name)
			if err != nil {
				t.Errorf("%s.%s: %v", path, f.Name, err)
				continue
			}
			bound[name] = true
			where := path + "." + f.Name
			matchSig(t, where, f.Sig, body)
			for i, r := range f.Sig.Results {
				if isTask(r) && !hasHidden(body, v1.Frame) {
					t.Errorf("%s returns a Task but its body takes no hidden frame: %+v", where, body)
				}
				ops, ok := operations(r)
				if !ok {
					continue
				}
				goType := baseName(body.Results[i])
				for _, m := range ops.Methods {
					opBody, err := v1.BodyOf(goType + "." + m.Name)
					if err != nil {
						t.Errorf("%s's %s: %v", where, m.Name, err)
						continue
					}
					bound[goType+"."+m.Name] = true
					matchSig(t, where+" "+m.Name, m.Sig, opBody)
				}
			}
		}
		if externs == 0 {
			t.Errorf("std package %s declares no extern", path)
		}
	}
	for _, name := range []string{"Go", "OnExit", "New", "WithCapacity", "Queue.Push", "Queue.Pop", "Queue.Close", "Task.Wait", "Task.Stop"} {
		if !bound[name] {
			t.Errorf("the Go body %s binds no std declaration", name)
		}
	}
	for name, why := range unbound {
		if bound[name] {
			t.Errorf("%s is bound now; remove it from unbound (%s)", name, why)
		}
		if _, err := v1.BodyOf(name); err != nil {
			t.Errorf("unbound %s: %v", name, err)
		}
	}
}

// matchSig checks a Go body's shape against an Ergo signature.
func matchSig(t *testing.T, where string, sig *check.Signature, body v1.Body) {
	t.Helper()
	if len(body.Params) != len(sig.Params) {
		t.Errorf("%s: %d Ergo parameters, but the Go body has %d after its hidden ones: %+v", where, len(sig.Params), len(body.Params), body)
		return
	}
	for i, p := range sig.Params {
		inner, isFunc := p.Type.(*check.Signature)
		switch {
		case isFunc && body.Params[i].Func == nil:
			t.Errorf("%s: parameter %s is a function, the Go parameter %s is not", where, p.Name, body.Params[i].Type)
		case !isFunc && body.Params[i].Func != nil:
			t.Errorf("%s: parameter %s is not a function, the Go parameter %s is", where, p.Name, body.Params[i].Type)
		case isFunc:
			matchSig(t, where+" "+p.Name, inner, *body.Params[i].Func)
		}
	}
	want := len(sig.Results)
	if sig.Fallible {
		want++
	}
	if len(body.Results) != want {
		t.Errorf("%s: want %d Go results (fallible %v), got %v", where, want, sig.Fallible, body.Results)
		return
	}
	if sig.Fallible && body.Results[want-1] != "error" {
		t.Errorf("%s: an error row needs a last Go result error, got %v", where, body.Results)
	}
}

func hasHidden(b v1.Body, h v1.Hidden) bool {
	for _, x := range b.Hidden {
		if x == h {
			return true
		}
	}
	return false
}

// isTask reports the prelude's Task.
func isTask(t check.Type) bool {
	n, ok := t.(*check.Named)
	return ok && n.Obj.Name == "Task" && n.Obj.Pkg != nil && n.Obj.Pkg.Path == std.PreludePath
}

// operations is the operation set of an operations type.
func operations(t check.Type) (*check.Interface, bool) {
	n, ok := t.(*check.Named)
	if !ok {
		return nil, false
	}
	i, ok := n.Underlying().(*check.Interface)
	return i, ok && len(i.Methods) > 0
}

// baseName is the Go type name of a result: Queue for *Queue[T].
func baseName(goType string) string {
	s := strings.TrimPrefix(goType, "*")
	if i := strings.IndexByte(s, '['); i >= 0 {
		s = s[:i]
	}
	return s
}
