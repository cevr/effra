package rt

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// Reviewer probe (round 5, M1): a child started inside a restore is
// cancellable by the parent's stop, whatever the restore's outside state, so a
// masked owner waiting on it cannot deadlock. The owner's mask follows the
// outside state. Round 6 decision: a restore is its own kind and stops the
// shield walk, so the reviewer's "acquisition inside outer cleanup" case is
// not shielded either; a release entered inside the restore still is
// (TestReleaseShieldAcrossRestoreAndAcquire).
func TestRestoreChildCancellation(t *testing.T) {
	cases := []struct {
		name                                string
		inAcquire, outerCleanup, wantMasked bool
	}{
		{"outermost cleanup restore", false, false, false},
		{"ordinary acquisition", true, false, true},
		{"acquisition inside outer cleanup", true, true, true},
	}
	for _, tc := range cases {
		for _, timing := range []string{"stop before child start", "stop after child start"} {
			t.Run(tc.name+"/"+timing, func(t *testing.T) {
				root := Root("main")
				started := make(chan *Task, 1)
				var childStarted, sawMasked atomic.Bool
				operation := func(task *Task) (int, error) {
					m := task.Mask()
					defer m.Unmask()
					var err error
					m.Interruptible(func() {
						sawMasked.Store(task.Masked())
						_, err = Scoped(task, func(local *Scope) (int, error) {
							if timing == "stop before child start" {
								root.Interrupt()
							}
							h := Go(local, "restored helper", func(child *Task) (int, error) {
								started <- child
								<-child.Done() // ends only when a stop reaches it
								return 0, Interrupted
							})
							return h.Wait()
						})
					})
					return 0, err
				}
				var err error
				// No rescue: if the stop does not reach the child, the owner
				// waits forever and the watchdog fails the test.
				controlDone := make(chan struct{})
				within(t, 10*time.Second, "the masked owner's Wait on its restored child (no deadlock)", func() {
					go func() {
						defer close(controlDone)
						child := <-started
						if timing == "stop after child start" {
							root.Interrupt()
						}
						_ = child
						childStarted.Store(true)
					}()
					_, err = Run(root, func(task *Task) (int, error) {
						if !tc.inAcquire {
							return operation(task)
						}
						acquire := func() (int, error) {
							return Scoped(task, func(s *Scope) (int, error) {
								return Acquire(s, func() (int, error) { return operation(task) }, func(int) {})
							})
						}
						if !tc.outerCleanup {
							return acquire()
						}
						return Scoped(task, func(s *Scope) (int, error) {
							s.Defer(func() {
								if _, exit := acquire(); exit != nil {
									panic(exit)
								}
							})
							return 0, nil
						})
					})
				})
				<-controlDone
				if !childStarted.Load() {
					t.Fatal("the child never started")
				}
				if !errors.Is(err, Interrupted) || root.Masked() {
					t.Fatalf("exit %v, mask leaked %v", err, root.Masked())
				}
				if sawMasked.Load() != tc.wantMasked {
					t.Errorf("owner masked inside the restore = %v, want %v", sawMasked.Load(), tc.wantMasked)
				}
			})
		}
	}
}
