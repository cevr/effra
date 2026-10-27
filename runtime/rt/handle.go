package rt

// Handle is a started task: the runtime side of `Task[T, E]`. It is a
// resource (runtime.md Tasks, rule 3): Wait and Stop consume it, and an owner
// scope stops and drains it at scope end if neither ran. Its methods belong to
// the owner task's goroutine.
type Handle[T any] struct {
	task     Task
	done     chan struct{}
	value    T
	err      error // canonical exit; written before done closes
	consumed bool
}

// Go starts body as a child of s's task, on its own goroutine, owned by s.
// span names the task in defect chains (G2). When s ends with the handle
// unconsumed, s stops and drains it before releasing anything acquired
// earlier: the structured-lend guarantee (resources.md R27).
func Go[T any](s *Scope, span string, body func(*Task) (T, error)) *Handle[T] {
	h := spawn(s.task, span, body)
	s.push(entry{run: closeResource{}, v: h})
	return h
}

func spawn[T any](parent *Task, span string, body func(*Task) (T, error)) *Handle[T] {
	h := &Handle[T]{done: make(chan struct{})}
	h.task.init(parent, span)
	go h.run(body)
	return h
}

func (h *Handle[T]) run(body func(*Task) (T, error)) {
	var c Cause
	defer h.finish(&c)
	v, err := body(&h.task)
	c.Add(err)
	h.value = v
}

// finish is the task boundary: it recovers a panic as a defect, lets a stop
// requested while the body ran win over success (runtime.md, Exit causes rule
// 5), detaches the task, and publishes the exit. A task started in a masked
// region can never observe a stop, so its exit stands.
func (h *Handle[T]) finish(c *Cause) {
	if p := recover(); p != nil {
		c.recovered(&h.task, p, false)
	}
	if h.task.stopped.Load() && h.task.base == 0 {
		c.AddInterruption()
	}
	h.err = c.Err()
	if h.err != nil {
		var zero T
		h.value = zero
	}
	h.task.detach()
	close(h.done)
}

// Task is the started task, for its ID and span.
func (h *Handle[T]) Task() *Task { return &h.task }

// Wait waits for the task and returns its value, or its exit in canonical
// form; a defect keeps the chain of tasks that led to it (G2). Wait is an
// interruption point of the owner: if the owner is stopped first, Wait stops
// and drains the task, and returns the task's defects with the interruption.
// Wait consumes the handle.
func (h *Handle[T]) Wait() (T, error) {
	h.consumed = true
	select {
	case <-h.done:
		return h.value, h.err
	default:
	}
	owner := h.task.parent
	if !owner.Stopped() {
		select {
		case <-h.done:
			return h.value, h.err
		case <-owner.Done():
		}
	}
	h.task.Interrupt()
	<-h.done
	// An interrupted join does not observe the task (law R4): keep only its
	// defects, then the interruption.
	var c Cause
	c.Add(defectsOf(h.err))
	c.AddInterruption()
	var zero T
	return zero, c.Err()
}

// Stop interrupts the task and waits, uninterruptibly, until it has finished,
// releases included (runtime.md Tasks, rule 4). It returns only the task's
// defects: its interruption and an abandoned typed failure are the expected
// result of the stop (laws R3, R6). Stop consumes the handle; on a consumed
// handle it returns nil.
func (h *Handle[T]) Stop() error {
	if h.consumed {
		return nil
	}
	h.consumed = true
	h.task.Interrupt()
	<-h.done
	return defectsOf(h.err)
}

// Close is the release an owner runs: Stop, with any defect raised as a panic
// so the releasing scope records it. It does nothing on a consumed handle.
func (h *Handle[T]) Close() {
	if err := h.Stop(); err != nil {
		panic(err)
	}
}
