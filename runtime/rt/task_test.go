package rt

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

// waitUntil yields until cond holds. Tests use it instead of sleeping.
func waitUntil(cond func() bool) {
	for !cond() {
		runtime.Gosched()
	}
}

func blockUntilStopped(t *Task) (int, error) {
	<-t.Done()
	return 0, Interrupted
}

func TestStopPropagatesToDescendants(t *testing.T) {
	const fan = 3
	const total = 1 + fan + fan*fan
	for _, via := range []string{"handle Stop", "root Interrupt"} {
		t.Run(via, func(t *testing.T) {
			root := Root("main")
			s := NewScope(root)
			var started sync.WaitGroup
			started.Add(total)
			var observed atomic.Int32
			observe := func(task *Task) (int, error) {
				started.Done()
				<-task.Done()
				observed.Add(1)
				return 0, Interrupted
			}
			top := Go(&s, "top", func(task *Task) (int, error) {
				return Scoped(task, func(s *Scope) (int, error) {
					for range fan {
						Go(s, "mid", func(task *Task) (int, error) {
							return Scoped(task, func(s *Scope) (int, error) {
								for range fan {
									Go(s, "leaf", observe)
								}
								return observe(task)
							})
						})
					}
					return observe(task)
				})
			})
			started.Wait()
			switch via {
			case "handle Stop":
				if err := top.Stop(); err != nil {
					t.Fatalf("Stop returned %v, want nil", err)
				}
			case "root Interrupt":
				root.Interrupt()
				if _, err := top.Wait(); err != Interrupted {
					t.Fatalf("Wait returned %v, want Interrupted", err)
				}
			}
			// Stop and Wait return only after the whole subtree has drained.
			if got := observed.Load(); got != total {
				t.Fatalf("%d of %d tasks observed the stop before the drain finished", got, total)
			}
			if top.task.first != nil || root.first != nil {
				t.Fatal("finished tasks stayed linked to their parents")
			}
			s.End(nil)
		})
	}
}

func TestConcurrentStopAndChildAdmission(t *testing.T) {
	for range 50 {
		root := Root("main")
		parent := &Task{}
		parent.init(root, "parent")
		start := make(chan struct{})
		children := make([]*Task, 16)
		var workers sync.WaitGroup
		for i := range children {
			workers.Go(func() {
				<-start
				children[i] = &Task{}
				children[i].init(parent, "child")
			})
		}
		for range 4 {
			workers.Go(func() { <-start; root.Interrupt() })
		}
		close(start)
		workers.Wait()
		for _, c := range children {
			if !c.Stopped() {
				t.Fatal("a child admitted concurrently with Interrupt stayed live")
			}
			c.detach()
		}
		parent.detach()
		if root.first != nil || parent.first != nil {
			t.Fatal("detached children stayed linked")
		}
	}
}

func TestPollObservesStop(t *testing.T) {
	root := Root("main")
	if allocs := testing.AllocsPerRun(100, func() { _ = root.Stopped() }); allocs != 0 {
		t.Fatalf("a poll allocates %v times", allocs)
	}
	s := NewScope(root)
	const pollers = 8
	var polling sync.WaitGroup
	polling.Add(pollers)
	handles := make([]*Handle[int], pollers)
	for i := range handles {
		handles[i] = Go(&s, "poller", func(task *Task) (int, error) {
			polling.Done()
			n := 0
			for !task.Stopped() {
				n++
				if n%64 == 0 {
					runtime.Gosched()
				}
			}
			return n, Interrupted
		})
	}
	checker := Go(&s, "checker", func(task *Task) (int, error) {
		for {
			task.Check() // no error row: the boundary recovers the panic
			runtime.Gosched()
		}
	})
	polling.Wait()
	root.Interrupt()
	for _, h := range handles {
		if _, err := h.Wait(); err != Interrupted {
			t.Fatalf("poller exit %v, want Interrupted", err)
		}
	}
	if _, err := checker.Wait(); err != Interrupted {
		t.Fatalf("Check exit %v, want Interrupted", err)
	}
	s.End(nil)
}

func TestMaskDefersStopUntilUnmask(t *testing.T) {
	root := Root("main")
	s := NewScope(root)
	masked := make(chan struct{})
	fail := func(msg string) (string, error) { return "", errors.New(msg) }
	h := Go(&s, "masked", func(task *Task) (string, error) {
		m := task.Mask()
		close(masked)
		waitUntil(task.StopRequested)
		if task.Stopped() {
			return fail("stop observed inside a mask")
		}
		if task.Done() != nil {
			return fail("Done is interruptible inside a mask")
		}
		inner := task.Mask()
		var innerSaw bool
		inner.Interruptible(func() { innerSaw = task.Stopped() })
		if innerSaw {
			return fail("an inner restore unmasked the outer region")
		}
		inner.Unmask()
		if task.Stopped() {
			return fail("closing the inner region exposed the stop")
		}
		var outerSaw bool
		m.Interruptible(func() { outerSaw = task.Stopped() && task.Done() != nil })
		if !outerSaw {
			return fail("the poll pattern did not observe the stop")
		}
		if task.Stopped() {
			return fail("Interruptible did not restore the mask")
		}
		m.Unmask()
		if !task.Stopped() {
			return fail("the stop was not observed after unmask")
		}
		return "done", nil
	})
	<-masked
	h.Task().Interrupt()
	if _, err := h.Wait(); err != Interrupted {
		t.Fatalf("exit %v, want Interrupted", err)
	}

	// Release always runs masked, even when the scope ends by a stop.
	var releaseMasked, releaseSawStop bool
	release := Go(&s, "release", func(task *Task) (int, error) {
		return Scoped(task, func(s *Scope) (int, error) {
			s.Defer(func() { releaseMasked, releaseSawStop = task.Masked(), task.Stopped() })
			return blockUntilStopped(task)
		})
	})
	if err := release.Stop(); err != nil {
		t.Fatalf("Stop returned %v", err)
	}
	if !releaseMasked || releaseSawStop {
		t.Fatalf("release masked=%v sawStop=%v, want masked and no stop", releaseMasked, releaseSawStop)
	}
	s.End(nil)
}

func TestChildOfMaskedRegionInheritsMask(t *testing.T) {
	root := Root("main")
	m := root.Mask()
	root.Interrupt()
	s := NewScope(root)
	h := Go(&s, "in release", func(task *Task) (string, error) {
		if task.Stopped() {
			return "", Interrupted
		}
		return "flushed", nil
	})
	v, err := h.Wait()
	m.Unmask()
	if v != "flushed" || err != nil {
		t.Fatalf("got %q, %v: a task started in a masked region was interrupted", v, err)
	}
	s.End(nil)
}

func TestChildInheritsContextValues(t *testing.T) {
	type key struct{}
	root := Root("main")
	s := NewScope(root)
	region := root.WithValue(key{}, "eu")
	h := Go(&s, "child", func(task *Task) (any, error) { return task.Value(key{}), nil })
	region.End()
	if v, err := h.Wait(); v != "eu" || err != nil {
		t.Fatalf("child saw %v, %v; want eu", v, err)
	}
	if root.Value(key{}) != nil {
		t.Fatal("End did not restore the outer values")
	}
	s.End(nil)
}
