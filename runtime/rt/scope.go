package rt

// Resource is a value with a release (resources.md R1, R2). Close has no
// error result; a panic in Close is a defect that joins the exit cause.
type Resource interface {
	Close()
}

// entry is one registered release: a releaser and its resource. Each releaser
// is a struct of at most one func field, which Go stores in an interface
// without allocating, so a pointer resource and a static close function
// register with no allocation. (A func value of a generic instantiation would
// instead bind its dictionary in a heap closure.)
type entry struct {
	run releaser
	v   any
}

type releaser interface{ release(v any) }

const inlineEntries = 4

// Scope is an owner scope (resources.md R4, R5): it releases what it owns in
// reverse acquisition order when it ends, on every exit. Make one with
// NewScope and end it with a deferred End. A Scope belongs to one task's
// goroutine; do not copy it after first use.
type Scope struct {
	task   *Task
	n      int
	inline [inlineEntries]entry
	more   []entry
}

// NewScope returns an empty scope owned by task t.
func NewScope(t *Task) Scope { return Scope{task: t} }

// Task is the task that owns s.
func (s *Scope) Task() *Task { return s.task }

func (s *Scope) push(e entry) {
	if s.n < inlineEntries {
		s.inline[s.n] = e
	} else {
		s.more = append(s.more, e)
	}
	s.n++
}

func (s *Scope) pop() entry {
	s.n--
	if s.n >= inlineEntries {
		i := s.n - inlineEntries
		e := s.more[i]
		s.more = s.more[:i]
		return e
	}
	return s.inline[s.n]
}

type closeWith[R any] struct{ close func(R) }

func (c closeWith[R]) release(v any) { c.close(v.(R)) }

type closeResource struct{}

func (closeResource) release(v any) { v.(Resource).Close() }

type callFunc struct{ f func() }

func (c callFunc) release(any) { c.f() }

// Acquire runs open and, if it succeeds, registers close(r) with s in the same
// masked step (R8): a stop that arrives during open or between open and the
// registration is observed only after the release is registered, at the next
// poll. A failed open registers nothing.
func Acquire[R any](s *Scope, open func() (R, error), close func(R)) (R, error) {
	m := s.task.Mask()
	defer m.Unmask()
	r, err := open()
	if err == nil {
		s.push(entry{run: closeWith[R]{close}, v: r})
	}
	return r, err
}

// Own registers r's Close with s. Call it inside the mask that acquired r.
func (s *Scope) Own(r Resource) { s.push(entry{run: closeResource{}, v: r}) }

// Defer registers f as a release of s. Like every release it runs masked, and
// a panic in it is a defect, not a lost failure.
func (s *Scope) Defer(f func()) { s.push(entry{run: callFunc{f}}) }

// End ends the scope; defer it directly, as `defer s.End(&err)`. It recovers a
// panic of the body as a defect, combines it with *err, then runs every
// release in reverse order, masked, recording each release defect (R5, R9).
// With a non-nil err it stores the exit in canonical form; with a nil err (a
// body with no error row) it panics with the exit, and the next boundary
// recovers it.
func (s *Scope) End(err *error) {
	p := recover()
	if p == nil && s.n == 0 {
		return
	}
	var c Cause
	if err != nil {
		c.Add(*err)
	}
	if p != nil {
		c.recovered(s.task, p, false)
	}
	s.release(&c)
	exit := c.Err()
	if err != nil {
		*err = exit
	} else if exit != nil {
		panic(exit)
	}
}

func (s *Scope) release(c *Cause) {
	m := s.task.Mask()
	for s.n > 0 {
		s.runRelease(s.pop(), c)
	}
	m.Unmask()
}

func (s *Scope) runRelease(e entry, c *Cause) {
	defer func() {
		if p := recover(); p != nil {
			c.recovered(s.task, p, true)
		}
	}()
	e.run.release(e.v)
}

// Scoped runs body with a fresh scope owned by t and ends the scope on every
// exit, returning the body's value or the combined exit.
func Scoped[T any](t *Task, body func(*Scope) (T, error)) (v T, err error) {
	s := NewScope(t)
	defer func() {
		if err != nil {
			var zero T
			v = zero
		}
	}()
	defer s.End(&err)
	return body(&s)
}
