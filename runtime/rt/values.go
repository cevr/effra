package rt

// values is an immutable list of context values (G1). A task shares its list
// with every child it starts, so a child sees the values in force when it was
// started and no later change.
type values struct {
	key, value any
	next       *values
}

// Context is an open context-value region of one task, the runtime side of
// `with(trace.Span(…), log.Annotate(…)) { … }` (G1).
type Context struct {
	t    *Task
	prev *values
}

// WithValue binds key to value for t and every task t starts until End. An
// inner binding of the same key shadows the outer one. Call it and End from
// the task's own goroutine, on every path.
func (t *Task) WithValue(key, value any) Context {
	prev := t.values
	t.values = &values{key: key, value: value, next: prev}
	return Context{t: t, prev: prev}
}

// End restores the values in force before WithValue. Tasks already started
// keep the values they inherited.
func (c Context) End() { c.t.values = c.prev }

// Value returns the innermost value bound to key, or nil.
func (t *Task) Value(key any) any {
	for v := t.values; v != nil; v = v.next {
		if v.key == key {
			return v.value
		}
	}
	return nil
}
