package rt

import (
	"errors"
	"sync/atomic"
	"testing"
)

// Reviewer probe (round 3, M1): a release nested inside a restore, inside an
// acquisition, inside a GoMasked body still protects its helper from a stop
// that arrives after the helper starts.
func TestReleaseShieldAcrossRestoreAndAcquire(t *testing.T) {
	for _, nesting := range []string{"plain restored body", "inside acquire", "restore inside acquire"} {
		t.Run(nesting, func(t *testing.T) {
			root := Root("main")
			outer := NewScope(root)
			started := make(chan *Task, 1)
			allow, controlDone := make(chan struct{}), make(chan struct{})
			var helperStopped atomic.Bool
			go func() {
				defer close(controlDone)
				child := <-started
				root.Interrupt()
				helperStopped.Store(child.StopRequested())
				close(allow)
			}()
			var completed atomic.Bool
			h := GoMasked(&outer, "masked owner", func(task *Task, restore Mask) (int, error) {
				work := func() (int, error) {
					return Scoped(task, func(s *Scope) (int, error) {
						s.Defer(func() {
							local := NewScope(task)
							defer local.End(nil)
							helper := Go(&local, "release helper", func(child *Task) (int, error) {
								started <- child
								select {
								case <-child.Done():
									return 0, Interrupted
								case <-allow:
									return 7, nil
								}
							})
							if _, err := helper.Wait(); err != nil {
								panic(err)
							}
							completed.Store(true)
						})
						return 0, nil
					})
				}
				var err error
				if nesting == "plain restored body" {
					restore.Interruptible(func() { _, err = work() })
				} else {
					_, err = Scoped(task, func(s *Scope) (int, error) {
						return Acquire(s, func() (int, error) {
							if nesting == "restore inside acquire" {
								var exit error
								restore.Interruptible(func() { _, exit = work() })
								return 0, exit
							}
							return work()
						}, func(int) {})
					})
				}
				return 0, err
			})
			_, err := h.Wait()
			<-controlDone
			_ = endScope(&outer)
			if helperStopped.Load() {
				t.Fatal("the ambient stop cascaded into a release-owned helper")
			}
			if !completed.Load() {
				t.Fatal("the ambient stop aborted a release nested inside Acquire and restore")
			}
			if !errors.Is(err, Interrupted) {
				t.Errorf("waiter exit %v, want the interruption", err)
			}
		})
	}
}

// The region stack: each kind's effect on polling and on a child's shield.
func TestRegionKinds(t *testing.T) {
	root := Root("main")
	root.Interrupt()
	shield := func() bool { return root.regions.shields() }
	if !root.Stopped() || shield() {
		t.Fatal("an empty stack must be interruptible and must not shield")
	}
	m := root.Mask() // a defer body or a release
	if root.Stopped() || !shield() {
		t.Fatal("a protecting region must mask and shield")
	}
	a := root.acquire() // Acquire's registration step inside it
	if root.Stopped() || shield() {
		t.Fatal("an acquiring region must mask and must not shield")
	}
	m.Interruptible(func() {
		// The restore takes the state outside m: unmasked.
		if !root.Stopped() {
			t.Error("a restore of an outermost region did not unmask")
		}
		if shield() {
			t.Error("the nearest non-restore region is the acquisition, which must not shield")
		}
		inner := root.Mask()
		if root.Stopped() || !shield() {
			t.Error("a release inside a restore inside an acquisition did not protect")
		}
		inner.Interruptible(func() {
			if !root.Stopped() {
				t.Error("a restore inside a restore did not take the unmasked outside state")
			}
		})
		inner.Unmask()
	})
	root.regions.truncate(a)
	m.Interruptible(func() {
		if !root.Stopped() || !shield() {
			t.Error("a child started in a restore of a protecting region lost its shield")
		}
		nested := root.Mask()
		deeper := root.Mask()
		deeper.Interruptible(func() {
			if root.Stopped() {
				t.Error("an inner restore unmasked its enclosing protecting region")
			}
		})
		nested.Unmask()
		if root.regions.n != 2 {
			t.Error("Unmask did not close the regions opened inside it")
		}
	})
	m.Unmask()
	if root.Masked() || root.regions.n != 0 {
		t.Fatal("the stack did not empty")
	}
}

// Every region pops on a panic: Acquire's open and a restore.
func TestRegionsPopOnPanic(t *testing.T) {
	root := Root("main")
	_, err := Scoped(root, func(s *Scope) (int, error) {
		return Acquire(s, func() (int, error) { panic("open defect") }, func(int) { t.Error("released a failed acquisition") })
	})
	if err == nil || root.Masked() {
		t.Fatalf("exit %v, masked %v; want the defect and an empty stack", err, root.Masked())
	}
	m := root.Mask()
	func() {
		defer func() { _ = recover() }()
		m.Interruptible(func() { panic("wait defect") })
	}()
	if root.regions.n != 1 || !root.Masked() {
		t.Fatal("a panic in a restore did not pop the restore entry")
	}
	m.Unmask()
	if root.Masked() {
		t.Fatal("the stack did not empty")
	}
}

// Regions deeper than the inline array spill to the overflow slice and back.
func TestDeepRegionNesting(t *testing.T) {
	root := Root("main")
	root.Interrupt()
	var masks []Mask
	for range 3 * inlineRegions {
		masks = append(masks, root.Mask())
	}
	_ = root.acquire() // innermost, in the overflow slice
	if root.Stopped() || root.regions.shields() {
		t.Fatal("the innermost acquiring region must mask without shielding")
	}
	inner := masks[len(masks)-1]
	inner.Interruptible(func() {
		// The restore of a protecting region nested in another one takes the
		// outer protecting state: still masked, and shielding again.
		if root.Stopped() || !root.regions.shields() {
			t.Error("a deep restore took the wrong outside state")
		}
	})
	if root.regions.shields() {
		t.Fatal("the restore entry was not popped")
	}
	masks[inlineRegions].Unmask()
	if int(root.regions.n) != inlineRegions || !root.regions.shields() || root.Stopped() {
		t.Fatalf("after closing a spilled region: depth %d, shields %v", root.regions.n, root.regions.shields())
	}
	masks[0].Unmask()
	if root.Masked() || !root.Stopped() {
		t.Fatal("the stack did not empty")
	}
}
