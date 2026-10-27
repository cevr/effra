package rt

import "time"

// After is timeout.After (runtime.md, Timeouts): an ordinary library function
// over a child task, Interrupt, a drain and a Scheduler timer. It runs body as
// a child of t and returns only after the child has finished, releases
// included.
//
//   - The body finishes first: its value or its exit.
//   - The deadline passes first: the child is stopped and drained, and the exit
//     is the child's defects followed by Expired. The abandoned child's typed
//     failure and the interruption After caused are normalized (laws R3, R8),
//     so a lone Expired stays a catchable typed failure.
//   - t is stopped first or while the body runs: the child is stopped and
//     drained, and the exit is the child's defects with an interruption.
//
// A defect in the child is kept on every path (Exit causes, rule 3).
func After[T any](t *Task, s Scheduler, d time.Duration, body func(*Task) (T, error)) (T, error) {
	var zero T
	if t.Stopped() {
		return zero, Interrupted
	}
	timer := s.NewTimer(d)
	defer timer.Stop()
	h := spawn(t, "timeout.After", body)
	var c Cause
	select {
	case <-h.done:
		c.Add(h.err)
	case <-timer.C():
		h.task.Interrupt()
		<-h.done
		c.Add(defectsOf(h.err))
		c.AddFailure(Expired{})
	case <-t.Done():
		h.task.Interrupt()
		<-h.done
		c.Add(defectsOf(h.err))
	}
	if t.Stopped() {
		c.AddInterruption()
	}
	if err := c.Err(); err != nil {
		return zero, err
	}
	return h.value, nil
}
