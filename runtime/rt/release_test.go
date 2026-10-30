package rt

import (
	"sync"
	"testing"
)

// releaseEvents records the kinds a traced tree raises.
type releaseEvents struct {
	mu    sync.Mutex
	kinds []EventKind
	panic []Event
}

func (r *releaseEvents) Event(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.kinds = append(r.kinds, e.Kind)
	if e.Kind == TracePanic {
		r.panic = append(r.panic, e)
	}
}

func (r *releaseEvents) count(k EventKind) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, x := range r.kinds {
		if x == k {
			n++
		}
	}
	return n
}

// raw is the classify of a caller that keeps rt's own defect values.
func raw(p any) (any, error) {
	if p == Interrupted {
		return nil, Interrupted
	}
	return p, nil
}

// TestReleaseRecordsAsAScopeRelease (stage 3b-runtime review o1): on an
// untraced tree, Release records what runRelease records: nothing for a
// release that returns, a fresh defect marked as raised in a release, and
// an interruption as an interruption; after runs once, after the defect,
// with the carried exit, and its result is added.
func TestReleaseRecordsAsAScopeRelease(t *testing.T) {
	task := Root("release")
	var c Cause
	Release(task, &c, func() {}, raw, nil)
	if len(c.Reasons) != 0 {
		t.Fatalf("a release that returned recorded %+v", c.Reasons)
	}
	Release(task, &c, func() { panic("broke") }, raw, nil)
	Release(task, &c, func() { panic(Interrupted) }, raw, nil)
	var order []string
	Release(task, &c, func() { panic("broke again") }, raw, func(carried error) error {
		order = append(order, "after")
		if len(c.Reasons) != 3 {
			t.Errorf("after ran before the defect was recorded: %d reasons", len(c.Reasons))
		}
		return errBody
	})
	kinds := []Kind{}
	for _, r := range c.Reasons {
		kinds = append(kinds, r.Kind)
	}
	if len(c.Reasons) != 4 || c.Reasons[0].Kind != KindDefect || !c.Reasons[0].InRelease || c.Reasons[0].Value != "broke" ||
		c.Reasons[1].Kind != KindInterruption || c.Reasons[2].Value != "broke again" || !c.Reasons[2].InRelease ||
		c.Reasons[3].Kind != KindFailure || c.Reasons[3].Err != errBody || len(order) != 1 {
		t.Fatalf("reasons %+v (kinds %v), after ran %d times", c.Reasons, kinds, len(order))
	}
}

// TestReleaseKeepsAPanicOfItsFollowUp (stage 3b-runtime review m2): a panic
// that escapes classify or after does not leave Release. It is recorded as
// one more release defect, the release event is still raised on a traced
// tree, and the caller's mask can still be closed, so its later releases
// and its unmask run.
func TestReleaseKeepsAPanicOfItsFollowUp(t *testing.T) {
	for name, release := range map[string]func(*Task, *Cause){
		"classify panics": func(task *Task, c *Cause) {
			Release(task, c, func() { panic("body broke") }, func(any) (any, error) { panic("classify broke") }, nil)
		},
		"after panics": func(task *Task, c *Cause) {
			Release(task, c, func() { panic("body broke") }, raw, func(error) error { panic("after broke") })
		},
	} {
		t.Run(name, func(t *testing.T) {
			rec := &releaseEvents{}
			task := TracedRoot("release", rec)
			var c Cause
			m := task.Mask()
			escaped := func() (p any) {
				defer func() { p = recover() }()
				release(task, &c)
				Release(task, &c, func() {}, raw, nil) // a later release still runs
				return nil
			}()
			m.Unmask()
			if escaped != nil {
				t.Fatalf("a panic escaped Release: %v", escaped)
			}
			if task.Masked() {
				t.Fatal("the task is still masked")
			}
			last := c.Reasons[len(c.Reasons)-1]
			if last.Kind != KindDefect || !last.InRelease || (last.Value != "classify broke" && last.Value != "after broke") {
				t.Fatalf("reasons %+v; want the follow-up's panic as the last release defect", c.Reasons)
			}
			if got := rec.count(TraceRelease); got != 2 {
				t.Fatalf("%d release events; want 2, one per release", got)
			}
		})
	}
}
