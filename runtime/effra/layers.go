package effra

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"
)

type NodeID string
type PlanID string
type NodeKind uint8

const (
	NodeBinding NodeKind = iota
	NodeStartup
)

type NodeSource struct {
	Module string
	Offset int
	Length int
	Line   int
	Column int
}

type NodeSpec struct {
	ID           NodeID
	Dependencies []NodeID
	Kind         NodeKind
	Source       NodeSource
}

// Node carries lifecycle metadata and a statically typed state adapter, never
// an erased service value. Adapters read completed declared dependencies and
// write their own field only; arbitrary native callbacks are a trusted boundary.
type Node[S any] struct {
	Spec      NodeSpec
	Construct func(*FiberContext, *S) Exit[Unit]
}

type Plan[In, S, Out any] struct {
	id           PlanID
	nodes        []Node[S]
	dependencies [][]int
	order        []int
	init         func(In) S
	expose       func(*S) Out
	invalid      error
}

const maxPlanNodes = 1000
const maxPlanEdges = 10000
const maxPlanMetadata = 100000

// NewPlan retains bounded immutable metadata and executes no callbacks.
// Invalid native graphs become defects when explicitly provided, before init.
func NewPlan[In, S, Out any](id PlanID, nodes []Node[S], init func(In) S, expose func(*S) Out) Plan[In, S, Out] {
	plan := Plan[In, S, Out]{id: id, init: init, expose: expose}
	invalid := func(message string) Plan[In, S, Out] {
		plan.invalid = fmt.Errorf("invalid layer plan: %s", message)
		return plan
	}
	if id == "" || init == nil || expose == nil {
		return invalid("missing identity, initializer or output adapter")
	}
	if len(nodes) > maxPlanNodes {
		return invalid("more than 1000 nodes")
	}
	// Identity text must be well-formed Unicode before it is measured or
	// ordered, so byte order is code point order and the JS seam, which sees
	// UTF-16, reaches the same refusal, bounds and canonical order.
	const malformed = "identity is not well-formed Unicode"
	if !utf8.ValidString(string(id)) {
		return invalid(malformed)
	}
	metadata, edges := len(id), 0
	if metadata > maxPlanMetadata {
		return invalid("metadata bound exceeded")
	}
	for _, node := range nodes {
		if !utf8.ValidString(string(node.Spec.ID)) || !utf8.ValidString(node.Spec.Source.Module) {
			return invalid(malformed)
		}
		metadata += 1 + len(node.Spec.ID) + len(node.Spec.Source.Module)
		edges += len(node.Spec.Dependencies)
		if metadata > maxPlanMetadata || edges > maxPlanEdges {
			return invalid("metadata bound exceeded")
		}
		for _, dependency := range node.Spec.Dependencies {
			if !utf8.ValidString(string(dependency)) {
				return invalid(malformed)
			}
			metadata += 1 + len(dependency)
			if metadata > maxPlanMetadata {
				return invalid("metadata bound exceeded")
			}
		}
	}
	indices := make(map[NodeID]int, len(nodes))
	for i, node := range nodes {
		if node.Spec.ID == "" || node.Construct == nil || (node.Spec.Kind != NodeBinding && node.Spec.Kind != NodeStartup) {
			return invalid("missing node identity/constructor or unsupported kind")
		}
		if _, duplicate := indices[node.Spec.ID]; duplicate {
			return invalid("duplicate node identity")
		}
		indices[node.Spec.ID] = i
	}
	dependents := make([][]int, len(nodes))
	plan.dependencies = make([][]int, len(nodes))
	remaining := make([]int, len(nodes))
	ready := []int{}
	for i, node := range nodes {
		seen := map[NodeID]bool{}
		for _, dependency := range node.Spec.Dependencies {
			index, exists := indices[dependency]
			if !exists || seen[dependency] {
				return invalid("unknown or duplicate dependency")
			}
			seen[dependency] = true
			dependents[index] = append(dependents[index], i)
			plan.dependencies[i] = append(plan.dependencies[i], index)
		}
		remaining[i] = len(node.Spec.Dependencies)
		if remaining[i] == 0 {
			ready = append(ready, i)
		}
	}
	less := func(a, b int) int {
		if nodes[a].Spec.ID < nodes[b].Spec.ID {
			return -1
		}
		if nodes[a].Spec.ID > nodes[b].Spec.ID {
			return 1
		}
		return 0
	}
	for len(ready) > 0 {
		slices.SortFunc(ready, less)
		index := ready[0]
		ready = ready[1:]
		plan.order = append(plan.order, index)
		for _, dependent := range dependents[index] {
			remaining[dependent]--
			if remaining[dependent] == 0 {
				ready = append(ready, dependent)
			}
		}
	}
	if len(plan.order) != len(nodes) {
		return invalid("dependency cycle")
	}
	plan.nodes = make([]Node[S], len(nodes))
	for i, node := range nodes {
		plan.nodes[i] = node
		plan.nodes[i].Spec.Dependencies = append([]NodeID{}, node.Spec.Dependencies...)
	}
	return plan
}

type layerNodeState uint8

const (
	layerWaiting layerNodeState = iota
	layerConstructing
	layerSucceeded
	layerFailed
	layerSkipped
)

// layerEntry is one acquisition-table row. The build owns its single producer;
// dependent producers and other waiters observe its one terminal outcome.
type layerEntry struct {
	state          layerNodeState
	outcome        *managedSignal
	scope          *Scope
	cancel         context.CancelFunc
	cause          Cause
	abortCancelled bool
}

type layerBuild[S any] struct {
	mu              sync.Mutex
	nodes           []Node[S]
	dependencies    [][]int
	order           []int
	entries         []layerEntry
	state           *S
	parent          *FiberContext
	settled         *managedSignal
	finished        int
	aborted         bool
	callerInterrupt error
}

func newLayerBuild[In, S, Out any](plan Plan[In, S, Out], state *S, parent *FiberContext) *layerBuild[S] {
	build := &layerBuild[S]{nodes: plan.nodes, dependencies: plan.dependencies, order: plan.order,
		entries: make([]layerEntry, len(plan.nodes)), state: state, parent: parent, settled: newManagedSignal()}
	for i := range build.entries {
		build.entries[i].outcome = newManagedSignal()
	}
	return build
}

// start admits every selected producer once, dependencies first. Producers
// belong to the build rather than to any waiter, so only abort cancels them.
func (b *layerBuild[S]) start() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, i := range b.order {
		b.launchLocked(i)
	}
	if len(b.nodes) == 0 {
		b.settled.signal()
	}
}

func (b *layerBuild[S]) abortLocked() {
	if b.aborted {
		return
	}
	b.aborted = true
	for i := range b.entries {
		entry := &b.entries[i]
		switch entry.state {
		case layerWaiting:
			entry.cancel()
		case layerConstructing:
			entry.abortCancelled = true
			entry.cancel()
		}
	}
}

// Each producer gets an independent managed turn. It first waits on its
// dependencies' entries, then receives a node owner only once all succeeded.
func (b *layerBuild[S]) launchLocked(index int) {
	entry := &b.entries[index]
	base := withoutSchedulerContinuation(context.WithoutCancel(b.parent.ctx))
	waiting, cancel := context.WithCancel(base)
	entry.cancel = cancel
	driver := b.parent.timerDriver()
	waiter := &FiberContext{ctx: waiting, driver: driver}
	scheduler, virtual := driver.(*TestScheduler)
	if virtual {
		waiter.turn = scheduler
		scheduler.reserve()
	}
	go func() {
		if virtual {
			finish := scheduler.enter(true)
			defer finish()
		}
		for _, dependency := range b.dependencies[index] {
			if b.awaitEntry(waiter, dependency).IsFailure() {
				// The dependency's own entry retains its reasons once.
				b.finish(index, layerSkipped, nil)
				return
			}
		}
		fc := b.admit(index, base, driver)
		if fc == nil {
			b.finish(index, layerSkipped, nil)
			return
		}
		exit := Invoke(fc, func(fc *FiberContext) Exit[Unit] { return b.nodes[index].Construct(fc, b.state) })
		b.publish(index, exit)
	}()
}

// admit opens the node owner under the build mutex, so abort either refuses
// the producer or later cancels its evaluation; there is no unowned window.
func (b *layerBuild[S]) admit(index int, base context.Context, driver timerDriver) *FiberContext {
	b.mu.Lock()
	defer b.mu.Unlock()
	entry := &b.entries[index]
	entry.cancel()
	if b.aborted {
		return nil
	}
	entry.state = layerConstructing
	entry.scope = newScopeWithDriver(base, b.parent.scope, driver)
	evaluation, cancel := context.WithCancel(entry.scope.ctx)
	entry.cancel = cancel
	fc := &FiberContext{ctx: evaluation, admission: entry.scope.ctx, scope: entry.scope, driver: entry.scope.driver}
	if scheduler, virtual := driver.(*TestScheduler); virtual {
		fc.turn = scheduler
	}
	return fc
}

// awaitEntry is the acquisition-table waiter. Every waiter observes the same
// terminal outcome; cancelling one waiter abandons only its own wait and never
// the build-owned producer. This deliberately differs from Effect's MemoMap,
// where interrupting the first requester interrupts the shared acquisition.
func (b *layerBuild[S]) awaitEntry(fc *FiberContext, index int) Exit[Unit] {
	entry := &b.entries[index]
	waiter, complete := entry.outcome.register(fc.turnScheduler())
	if !complete {
		resume := fc.suspendScheduler()
		select {
		case <-waiter.done:
			resume()
		case <-fc.ctx.Done():
			resume()
			entry.outcome.cancel(waiter)
			return Interrupt[Unit](fc.ctx.Err())
		}
	}
	entry.outcome.consume(waiter)
	// The build mutex also publishes the producer's completed field before a
	// dependent producer reads it.
	b.mu.Lock()
	defer b.mu.Unlock()
	switch {
	case entry.state == layerSucceeded:
		return Succeed(Unit{})
	case entry.state == layerFailed && len(entry.cause) > 0:
		return FromCause[Unit](entry.cause)
	default:
		// Skipped or abort-interrupted entries carry no fabricated failure.
		return Interrupt[Unit](context.Canceled)
	}
}

func (b *layerBuild[S]) publish(index int, exit Exit[Unit]) {
	cause := exit.Cause()
	b.mu.Lock()
	defer b.mu.Unlock()
	entry := &b.entries[index]
	if entry.abortCancelled {
		retained := Cause{}
		for _, reason := range cause {
			if reason.Kind != "interrupt" {
				retained = append(retained, reason)
			}
		}
		cause = retained
	}
	if exit.IsFailure() {
		b.abortLocked()
		b.finishLocked(index, layerFailed, cause)
		return
	}
	// Success removes evaluation cancellation authority from the build.
	// The token then lives until this node's ordered close.
	b.finishLocked(index, layerSucceeded, nil)
}

func (b *layerBuild[S]) finish(index int, state layerNodeState, cause Cause) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.finishLocked(index, state, cause)
}

// The terminal entry is published before its waiters wake and before the
// build settles, so every observer reads one recorded outcome.
func (b *layerBuild[S]) finishLocked(index int, state layerNodeState, cause Cause) {
	entry := &b.entries[index]
	entry.state, entry.cause = state, cause
	b.finished++
	entry.outcome.signal()
	if b.finished == len(b.nodes) {
		b.settled.signal()
	}
}

// await joins every producer. Caller cancellation is whole-build cancellation:
// it aborts producers and still waits for their cooperative completion.
func (b *layerBuild[S]) await() {
	fc := b.parent
	waiter, complete := b.settled.register(fc.turnScheduler())
	if complete {
		b.settled.consume(waiter)
		return
	}
	resume := fc.suspendScheduler()
	select {
	case <-waiter.done:
		resume()
		b.settled.consume(waiter)
	case <-fc.ctx.Done():
		// Settle cancellation under the same mutex as successful publication.
		b.mu.Lock()
		if !b.settled.isComplete() {
			b.callerInterrupt = fc.ctx.Err()
			b.abortLocked()
		}
		b.mu.Unlock()
		// Producers always join, including masked late native acquisitions.
		<-waiter.done
		resume()
		b.settled.consume(waiter)
	}
}

// constructionCause retains each producer's reasons once in canonical node
// order. Waiters never contribute copies of a shared entry's failure.
func (b *layerBuild[S]) constructionCause() Cause {
	indices := make([]int, len(b.nodes))
	for i := range indices {
		indices[i] = i
	}
	slices.SortFunc(indices, func(a, c int) int { return strings.Compare(string(b.nodes[a].Spec.ID), string(b.nodes[c].Spec.ID)) })
	cause := Cause{}
	if b.callerInterrupt != nil {
		cause = append(cause, Reason{Kind: "interrupt", Err: b.callerInterrupt})
	}
	primaryNode, primaryReason := -1, -1
	if b.callerInterrupt == nil {
		for _, kind := range []string{"failure", "defect", "interrupt"} {
			for _, i := range indices {
				for j, reason := range b.entries[i].cause {
					if reason.Kind == kind {
						primaryNode, primaryReason = i, j
						break
					}
				}
				if primaryNode >= 0 {
					break
				}
			}
			if primaryNode >= 0 {
				break
			}
		}
		if primaryNode >= 0 {
			cause = append(cause, b.entries[primaryNode].cause[primaryReason])
		}
	}
	for _, i := range indices {
		for j, reason := range b.entries[i].cause {
			if i != primaryNode || j != primaryReason {
				cause = append(cause, reason)
			}
		}
	}
	return cause
}

// close releases admitted node owners in reverse canonical topology, so
// dependents finish before dependencies. Every owner is closed even when an
// earlier one reports cleanup defects.
func (b *layerBuild[S]) close() Cause {
	cause := Cause{}
	for i := len(b.order) - 1; i >= 0; i-- {
		entry := &b.entries[b.order[i]]
		if entry.scope != nil {
			cause = append(cause, entry.scope.closeWithContext(b.parent)...)
		}
	}
	return cause
}

// Provide creates a fresh build on execution. Its program scope closes before
// node owners close in reverse canonical topology. No service lookup occurs.
func Provide[In, S, Out, A any](plan Plan[In, S, Out], input In, program func(Out) Effect[A]) Effect[A] {
	return func(fc *FiberContext) Exit[A] {
		if plan.invalid != nil {
			return Die[A](plan.invalid)
		}
		if plan.init == nil || plan.expose == nil || program == nil {
			return Die[A](fmt.Errorf("invalid layer plan or program"))
		}
		initialized := Invoke(fc, func(*FiberContext) Exit[S] { return Succeed(plan.init(input)) })
		if initialized.IsFailure() {
			return Propagate[A](initialized)
		}
		state := initialized.Value
		build := newLayerBuild(plan, &state, fc)
		build.start()
		build.await()
		cause := build.constructionCause()
		var result Exit[A]
		if len(cause) > 0 {
			result = FromCause[A](cause)
		} else if build.aborted {
			result = Interrupt[A](context.Canceled)
		} else {
			result = Invoke(fc, Scoped(func(child *FiberContext) Exit[A] {
				outputs := plan.expose(&state)
				return Invoke(child, program(outputs))
			}))
		}
		return withCleanup(result, build.close())
	}
}
