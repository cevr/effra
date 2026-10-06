package effra

import (
	"context"
	"fmt"
	"slices"
	"sync"
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
	id         PlanID
	nodes      []Node[S]
	dependents [][]int
	order      []int
	init       func(In) S
	expose     func(*S) Out
	invalid    error
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
	metadata, edges := len(id), 0
	if metadata > maxPlanMetadata {
		return invalid("metadata bound exceeded")
	}
	for _, node := range nodes {
		metadata += 1 + len(node.Spec.ID) + len(node.Spec.Source.Module)
		edges += len(node.Spec.Dependencies)
		if metadata > maxPlanMetadata || edges > maxPlanEdges {
			return invalid("metadata bound exceeded")
		}
		for _, dependency := range node.Spec.Dependencies {
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
	plan.dependents = make([][]int, len(nodes))
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
			plan.dependents[index] = append(plan.dependents[index], i)
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
		for _, dependent := range plan.dependents[index] {
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
	layerPending layerNodeState = iota
	layerConstructing
	layerSucceeded
	layerFailed
	layerSkipped
)

type layerNodeRun struct {
	state          layerNodeState
	remaining      int
	scope          *Scope
	cancel         context.CancelFunc
	cause          Cause
	abortCancelled bool
}

type layerBuild[S any] struct {
	mu              sync.Mutex
	nodes           []Node[S]
	dependents      [][]int
	order           []int
	runs            []layerNodeRun
	state           *S
	parent          *FiberContext
	settled         *managedSignal
	running         int
	completed       int
	aborted         bool
	callerInterrupt error
}

func (b *layerBuild[S]) abortLocked() {
	if b.aborted {
		return
	}
	b.aborted = true
	for i := range b.runs {
		run := &b.runs[i]
		switch run.state {
		case layerPending:
			run.state = layerSkipped
		case layerConstructing:
			run.abortCancelled = true
			run.cancel()
		}
	}
}

// The mutex publishes completed fields before any dependent is launched.
// Each node gets an owner only when ready, and an independent managed turn.
func (b *layerBuild[S]) launchLocked(index int) {
	run := &b.runs[index]
	run.state = layerConstructing
	ctx := withoutSchedulerContinuation(context.WithoutCancel(b.parent.ctx))
	run.scope = newScopeWithDriver(ctx, b.parent.scope, b.parent.timerDriver())
	evaluation, cancel := context.WithCancel(run.scope.ctx)
	run.cancel = cancel
	fc := &FiberContext{ctx: evaluation, admission: run.scope.ctx, scope: run.scope, driver: run.scope.driver}
	scheduler, virtual := run.scope.driver.(*TestScheduler)
	if virtual {
		fc.turn = scheduler
		scheduler.reserve()
	}
	b.running++
	go func() {
		if virtual {
			finish := scheduler.enter(true)
			defer finish()
		}
		exit := Invoke(fc, func(fc *FiberContext) Exit[Unit] { return b.nodes[index].Construct(fc, b.state) })
		b.publish(index, exit)
	}()
}

func (b *layerBuild[S]) publish(index int, exit Exit[Unit]) {
	b.mu.Lock()
	defer b.mu.Unlock()
	run := &b.runs[index]
	run.cause = exit.Cause()
	if run.abortCancelled {
		retained := Cause{}
		for _, reason := range run.cause {
			if reason.Kind != "interrupt" {
				retained = append(retained, reason)
			}
		}
		run.cause = retained
	}
	b.running--
	b.completed++
	if exit.IsFailure() {
		run.state = layerFailed
		b.abortLocked()
	} else {
		// Success removes evaluation cancellation authority from the build.
		// The token then lives until this node's ordered close.
		run.state = layerSucceeded
		if !b.aborted {
			for _, dependent := range b.dependents[index] {
				b.runs[dependent].remaining--
				if b.runs[dependent].remaining == 0 {
					b.launchLocked(dependent)
				}
			}
		}
	}
	if b.running == 0 && (b.aborted || b.completed == len(b.nodes)) {
		b.settled.signal()
	}
}

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
			if b.running == 0 {
				b.settled.signal()
			}
		}
		b.mu.Unlock()
		// Producers always join, including masked late native acquisitions.
		<-waiter.done
		resume()
		b.settled.consume(waiter)
	}
}

func (b *layerBuild[S]) constructionCause() Cause {
	indices := make([]int, len(b.nodes))
	for i := range indices {
		indices[i] = i
	}
	slices.SortFunc(indices, func(a, c int) int {
		if b.nodes[a].Spec.ID < b.nodes[c].Spec.ID {
			return -1
		}
		if b.nodes[a].Spec.ID > b.nodes[c].Spec.ID {
			return 1
		}
		return 0
	})
	cause := Cause{}
	if b.callerInterrupt != nil {
		cause = append(cause, Reason{Kind: "interrupt", Err: b.callerInterrupt})
	}
	primaryNode, primaryReason := -1, -1
	if b.callerInterrupt == nil {
		for _, kind := range []string{"failure", "defect", "interrupt"} {
			for _, i := range indices {
				for j, reason := range b.runs[i].cause {
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
			cause = append(cause, b.runs[primaryNode].cause[primaryReason])
		}
	}
	for _, i := range indices {
		for j, reason := range b.runs[i].cause {
			if i != primaryNode || j != primaryReason {
				cause = append(cause, reason)
			}
		}
	}
	return cause
}

func (b *layerBuild[S]) close() Cause {
	cause := Cause{}
	for i := len(b.order) - 1; i >= 0; i-- {
		run := &b.runs[b.order[i]]
		if run.scope != nil {
			cause = append(cause, run.scope.closeWithContext(b.parent)...)
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
		build := &layerBuild[S]{nodes: plan.nodes, dependents: plan.dependents, order: plan.order,
			runs: make([]layerNodeRun, len(plan.nodes)), state: &state, parent: fc, settled: newManagedSignal()}
		build.mu.Lock()
		for i, node := range plan.nodes {
			build.runs[i].remaining = len(node.Spec.Dependencies)
		}
		for _, i := range plan.order {
			if build.runs[i].remaining == 0 {
				build.launchLocked(i)
			}
		}
		if len(plan.nodes) == 0 {
			build.settled.signal()
		}
		build.mu.Unlock()
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
