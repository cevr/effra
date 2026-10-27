package rt

// region is the kind of one open mask region of a task (I2).
type region uint8

const (
	// interruptible is a restore (Mask.Interruptible) whose enclosing state
	// is unmasked. An empty stack is interruptible too.
	interruptible region = iota
	// protecting is a release, a defer body (Task.Mask) or a GoMasked body.
	// It masks the task, and a child started under it is shielded from the
	// owner's ambient stop.
	protecting
	// acquiring is Acquire's registration step (I2 item 4, R8). It masks the
	// acquiring task, and it is transparent to the shield decision: work
	// started by an ordinary open stays cancellable, and an open inside a
	// cleanup keeps that cleanup's protection.
	acquiring
)

const inlineRegions = 7

// regionStack is a task's stack of open mask regions, innermost on top. Only
// the owner goroutine touches it; a child copies the one bit it needs, its
// shield, when it starts. Regions are explicit, so a restore cannot leave a
// counter that later misclassifies an inner release.
type regionStack struct {
	top    region                // kind of the innermost entry, or interruptible when empty
	inline [inlineRegions]region // with top, one 8-byte word
	n      int                   // depth; deeper entries spill to more, with no limit
	more   []region
}

func (s *regionStack) at(i int) region {
	if i < inlineRegions {
		return s.inline[i]
	}
	return s.more[i-inlineRegions]
}

// push opens a region and returns its index, the depth to truncate to when it
// closes.
func (s *regionStack) push(r region) int {
	i := s.n
	if i < inlineRegions {
		s.inline[i] = r
	} else {
		s.more = append(s.more[:i-inlineRegions], r)
	}
	s.n++
	s.top = r
	return i
}

// truncate closes every region at index n and above.
func (s *regionStack) truncate(n int) {
	s.n = n
	if len(s.more) > 0 {
		s.more = s.more[:max(n-inlineRegions, 0)]
	}
	s.top = interruptible
	if n > 0 {
		s.top = s.at(n - 1)
	}
}

// shields reports whether a child started now is shielded. The walk goes down
// from the top and skips acquiring entries: the nearest protecting entry
// shields, the nearest interruptible (restore) entry does not, and an empty
// walk does not. An acquisition masks only its own registration window; it is
// not a cleanup region, so it neither shields a child nor hides the cleanup
// region around it.
func (s *regionStack) shields() bool {
	for i := s.n - 1; i >= 0; i-- {
		switch s.at(i) {
		case protecting:
			return true
		case interruptible:
			return false
		}
	}
	return false
}

// Mask is an open masked region of one task (I2).
type Mask struct {
	t     *Task
	index int // the region's entry in the task's stack
}

// Mask opens a protecting region: the mask a defer body or a release runs
// under. Close it with Unmask on every path. Regions nest: closing an inner
// one returns to the outer state, never to an unmasked one.
func (t *Task) Mask() Mask {
	return Mask{t: t, index: t.regions.push(protecting)}
}

// Unmask closes the region and any region still open inside it. A stop
// requested while it was open is observed at the next poll, unless an outer
// region is still open.
func (m Mask) Unmask() {
	if m.t.regions.n <= m.index {
		panic("rt: Unmask without Mask")
	}
	m.t.regions.truncate(m.index)
}

// Interruptible runs f under a restore of this region: the poll pattern of
// Cats Effect, the restore of ZIO and Effect's uninterruptibleMask. The restore
// entry takes the state outside the region: interruptible when the region is
// outermost, so f can be stopped, and the enclosing region's kind otherwise,
// so an inner restore never unmasks an outer region. A release that f enters
// pushes its own protecting entry. The entry is popped on every exit of f,
// panics included.
func (m Mask) Interruptible(f func()) {
	outside := interruptible
	if m.index > 0 {
		outside = m.t.regions.at(m.index - 1)
	}
	n := m.t.regions.push(outside)
	defer m.t.regions.truncate(n)
	f()
}

// acquire opens Acquire's registration region and returns its index.
func (t *Task) acquire() int { return t.regions.push(acquiring) }
