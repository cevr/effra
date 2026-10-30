package v1

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

// TestBodiesFollowTheHiddenConvention reads the Go bodies of the std
// packages from the embedded source, as the lowering reads them, and pins
// each one's hidden parameters and the parameters and results that stand
// for its Ergo signature (docs/spec/stdlib.md, Hidden arguments). The
// internal/sketch/std test ties each to its Ergo declaration.
func TestBodiesFollowTheHiddenConvention(t *testing.T) {
	task := &Body{Hidden: []Hidden{CallerTask}, Results: []string{"T", "error"}}
	want := map[string]Body{
		// task.Go: the caller's frame owns the task, the site names it, and
		// the block receives the child's task.
		"Go": {Hidden: []Hidden{Frame, CallSite}, Params: []Param{{Type: "func(*rt.Task) (T, error)", Func: task}}, Results: []string{"*Task[T]"}},
		// scope.OnExit: the caller's frame takes the block.
		"OnExit": {Hidden: []Hidden{Frame}, Params: []Param{{Type: "func()", Func: &Body{}}}},
		// queue.New and slice.WithCapacity take no hidden argument.
		"New":          {Params: []Param{{Type: "int"}}, Results: []string{"*Queue[T]"}},
		"WithCapacity": {Params: []Param{{Type: "int"}}, Results: []string{"[]T"}},
		// The operations of queue.Queue: the caller's task where they wait.
		"Queue.Push":   {Hidden: []Hidden{CallerTask}, Params: []Param{{Type: "T"}}, Results: []string{"error"}},
		"Queue.Pop":    {Hidden: []Hidden{CallerTask}, Results: []string{"T", "error"}},
		"Queue.Values": {Hidden: []Hidden{CallerTask}, Results: []string{"iter.Seq[T]"}},
		"Queue.Close":  {},
		// The prelude's Task operations, and the move a return makes into
		// the caller's frame, the hidden destination.
		"Task.Wait": {Results: []string{"T", "error"}},
		"Task.Stop": {},
		"Task.Move": {Hidden: []Hidden{Frame}, Results: []string{"*Task[T]"}},
	}
	for name, w := range want {
		got, err := BodyOf(name)
		if err != nil {
			t.Errorf("BodyOf(%q): %v", name, err)
			continue
		}
		if !reflect.DeepEqual(got, w) {
			t.Errorf("BodyOf(%q) = %+v, want %+v", name, got, w)
		}
	}
	if _, err := BodyOf("Missing"); err == nil {
		t.Error("BodyOf(Missing) gave no error")
	}
}

// TestHiddenConventionRefusals feeds bodies that break the convention, and
// a few that keep it, through the same reader.
func TestHiddenConventionRefusals(t *testing.T) {
	src := `package v1

import rtx "ergo.local/prototype/runtime/rt"
import other "example.com/other/rt"

type Scope struct{}
type Site string
type Task[T any] struct{}

func Ordered(s *Scope, at Site, t *rtx.Task, n int) {}
func AfterParam(n int, t *rtx.Task) {}
func OutOfOrder(t *rtx.Task, s *Scope) {}
func Twice(s *Scope, p *Scope) {}
func Grouped(s, p *Scope) {}
func OwnTask(t *Task[int]) {}
func OtherRt(t *other.Task) {}
func BadBlock(f func(n int, t *rtx.Task)) {}
func unexported(s *Scope) {}
`
	decls, err := parseDecls(fstest.MapFS{"a.go": {Data: []byte(src)}, "a_test.go": {Data: []byte("package v1\nfunc Skipped() {}\n")}})
	if err != nil {
		t.Fatal(err)
	}
	ok := map[string][]Hidden{
		"Ordered": {Frame, CallSite, CallerTask},
		"OwnTask": nil, // v1's Task is an Ergo value, not the hidden task
		"OtherRt": nil, // only runtime/rt's Task is the hidden task
	}
	for name, hidden := range ok {
		b, err := decls.body(name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !reflect.DeepEqual(b.Hidden, hidden) {
			t.Errorf("%s: hidden %v, want %v", name, b.Hidden, hidden)
		}
	}
	bad := map[string]string{
		"AfterParam": "follows an Ergo parameter",
		"OutOfOrder": "out of order",
		"Twice":      "repeated",
		"Grouped":    "repeated",
		"BadBlock":   "function parameter 1",
		"unexported": "no exported Go body",
		"Skipped":    "no exported Go body",
	}
	for name, msg := range bad {
		_, err := decls.body(name)
		if err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("%s: error %v, want one containing %q", name, err, msg)
		}
	}
}
