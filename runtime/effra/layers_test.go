package effra

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func unitPlan[S any](nodes []Node[S], init func(Unit) S) Plan[Unit, S, *S] {
	return NewPlan("test/app", nodes, init, func(state *S) *S { return state })
}

func TestLayerDiamondIsLazySharedFreshAndClosesProgramFirst(t *testing.T) {
	type state struct{ database, account, audit int }
	var initializations, acquisitions atomic.Int32
	closed := []string{}
	resource := func(fc *FiberContext, name string) Exit[Unit] {
		return Invoke(fc, AcquireRelease(name, func(context.Context) (Unit, error) {
			return Unit{}, nil
		}, func(Unit, context.Context) error { closed = append(closed, name); return nil }))
	}
	nodes := []Node[state]{
		{Spec: NodeSpec{ID: "0/database"}, Construct: func(fc *FiberContext, s *state) Exit[Unit] {
			acquisitions.Add(1)
			s.database = 42
			return resource(fc, "database")
		}},
		{Spec: NodeSpec{ID: "1/account", Dependencies: []NodeID{"0/database"}}, Construct: func(fc *FiberContext, s *state) Exit[Unit] {
			s.account = s.database
			return resource(fc, "account")
		}},
		{Spec: NodeSpec{ID: "2/audit", Dependencies: []NodeID{"0/database"}}, Construct: func(fc *FiberContext, s *state) Exit[Unit] {
			s.audit = s.database
			return resource(fc, "audit")
		}},
	}
	plan := unitPlan(nodes, func(Unit) state { initializations.Add(1); return state{} })
	if initializations.Load() != 0 || acquisitions.Load() != 0 {
		t.Fatal("plan declaration executed construction")
	}
	// Caller mutation cannot change the retained dependency graph.
	nodes[1].Spec.Dependencies[0] = "missing"
	for range 2 {
		out := Run(Provide(plan, Unit{}, func(s *state) Effect[int] {
			return func(fc *FiberContext) Exit[int] {
				if s.account != 42 || s.audit != 42 {
					return Die[int](errors.New("dependent saw uninitialized shared field"))
				}
				if out := resource(fc, "program"); out.IsFailure() {
					return Propagate[int](out)
				}
				return Succeed(s.account + s.audit)
			}
		}))
		if out.IsFailure() || out.Value != 84 {
			t.Fatal(out)
		}
	}
	if initializations.Load() != 2 || acquisitions.Load() != 2 || !reflect.DeepEqual(closed, []string{"program", "audit", "account", "database", "program", "audit", "account", "database"}) {
		t.Fatalf("independent builds or close order: init=%d acquisitions=%d closed=%v", initializations.Load(), acquisitions.Load(), closed)
	}
}

func TestLayerCapturedContextLivesThroughProgramAndDependentClose(t *testing.T) {
	type state struct{ dependency context.Context }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	programStarted := make(chan struct{})
	var programClosed, dependentClosed, dependencyClosed atomic.Bool
	plan := unitPlan([]Node[state]{
		{Spec: NodeSpec{ID: "0/dependency"}, Construct: func(fc *FiberContext, s *state) Exit[Unit] {
			captured := Invoke(fc, FromGo(func(ctx context.Context) (context.Context, error) { return ctx, nil }))
			if captured.IsFailure() {
				return Propagate[Unit](captured)
			}
			s.dependency = captured.Value.Value
			return Invoke(fc, AcquireRelease("dependency", func(context.Context) (Unit, error) { return Unit{}, nil }, func(Unit, context.Context) error {
				dependencyClosed.Store(true)
				if s.dependency.Err() == nil || !dependentClosed.Load() {
					return errors.New("dependency token or dependent close order is wrong")
				}
				return nil
			}))
		}},
		{Spec: NodeSpec{ID: "1/dependent", Dependencies: []NodeID{"0/dependency"}}, Construct: func(fc *FiberContext, s *state) Exit[Unit] {
			return Invoke(fc, AcquireRelease("dependent", func(context.Context) (Unit, error) { return Unit{}, nil }, func(Unit, context.Context) error {
				dependentClosed.Store(true)
				if s.dependency.Err() != nil || !programClosed.Load() {
					return errors.New("captured dependency context ended before dependent release")
				}
				return nil
			}))
		}},
	}, func(Unit) state { return state{} })
	done := make(chan Exit[Unit], 1)
	go func() {
		done <- RunContext(ctx, Provide(plan, Unit{}, func(s *state) Effect[Unit] {
			return func(fc *FiberContext) Exit[Unit] {
				if s.dependency.Err() != nil {
					return Die[Unit](errors.New("captured context ended at constructor return"))
				}
				out := Invoke(fc, AcquireRelease("program", func(context.Context) (Unit, error) { return Unit{}, nil }, func(Unit, context.Context) error {
					programClosed.Store(true)
					if s.dependency.Err() != nil {
						return errors.New("caller cancellation broadcast to completed dependency")
					}
					return nil
				}))
				if out.IsFailure() {
					return out
				}
				close(programStarted)
				<-fc.Context().Done()
				return Interrupt[Unit](fc.Context().Err())
			}
		}))
	}()
	wait(t, programStarted)
	cancel()
	out := wait(t, done)
	if !out.Interrupted || len(out.Additional) != 0 || !programClosed.Load() || !dependentClosed.Load() || !dependencyClosed.Load() {
		t.Fatalf("context lifetime: %+v program=%v dependent=%v dependency=%v", out, programClosed.Load(), dependentClosed.Load(), dependencyClosed.Load())
	}
}

func TestLayerDiamondConsumersOverlapAfterOneSharedAcquisition(t *testing.T) {
	type state struct{ dependency, left, right int }
	started := make(chan string, 2)
	release := make(chan struct{})
	var acquisitions atomic.Int32
	plan := unitPlan([]Node[state]{
		{Spec: NodeSpec{ID: "dependency"}, Construct: func(_ *FiberContext, s *state) Exit[Unit] {
			acquisitions.Add(1)
			s.dependency = 21
			return Succeed(Unit{})
		}},
		{Spec: NodeSpec{ID: "left", Dependencies: []NodeID{"dependency"}}, Construct: func(_ *FiberContext, s *state) Exit[Unit] {
			started <- "left"
			<-release
			s.left = s.dependency
			return Succeed(Unit{})
		}},
		{Spec: NodeSpec{ID: "right", Dependencies: []NodeID{"dependency"}}, Construct: func(_ *FiberContext, s *state) Exit[Unit] {
			started <- "right"
			<-release
			s.right = s.dependency
			return Succeed(Unit{})
		}},
	}, func(Unit) state { return state{} })
	done := make(chan Exit[int], 1)
	go func() {
		done <- Run(Provide(plan, Unit{}, func(s *state) Effect[int] {
			return func(*FiberContext) Exit[int] { return Succeed(s.left + s.right) }
		}))
	}()
	first, second := wait(t, started), wait(t, started)
	if first == second || acquisitions.Load() != 1 {
		t.Fatalf("consumers did not share one concurrent acquisition: %s %s, count=%d", first, second, acquisitions.Load())
	}
	close(release)
	if out := wait(t, done); out.IsFailure() || out.Value != 42 {
		t.Fatal(out)
	}
}

func TestLayerCallerCancellationJoinsProducersPreservesReasonsAndSkipsConsumers(t *testing.T) {
	type state struct{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan context.Context, 1)
	finish := make(chan struct{})
	var consumer, program, closed atomic.Int32
	plan := unitPlan([]Node[state]{
		{Spec: NodeSpec{ID: "producer"}, Construct: func(fc *FiberContext, _ *state) Exit[Unit] {
			resource := Invoke(fc, AcquireRelease("producer", func(context.Context) (Unit, error) { return Unit{}, nil }, func(Unit, context.Context) error {
				closed.Add(1)
				return errors.New("cleanup defect")
			}))
			if resource.IsFailure() {
				return resource
			}
			started <- fc.Context()
			<-finish
			return Fail[Unit]("ConfigError", 42)
		}},
		{Spec: NodeSpec{ID: "consumer", Dependencies: []NodeID{"producer"}}, Construct: func(*FiberContext, *state) Exit[Unit] {
			consumer.Add(1)
			return Succeed(Unit{})
		}},
	}, func(Unit) state { return state{} })
	done := make(chan Exit[Unit], 1)
	go func() {
		done <- RunContext(ctx, Provide(plan, Unit{}, func(*state) Effect[Unit] {
			program.Add(1)
			return func(*FiberContext) Exit[Unit] { return Succeed(Unit{}) }
		}))
	}()
	evaluation := wait(t, started)
	cancel()
	wait(t, evaluation.Done())
	select {
	case <-done:
		t.Fatal("cancelled build returned before its producer joined")
	default:
	}
	close(finish)
	out := wait(t, done)
	cause := out.Cause()
	if len(cause) != 3 || cause[0].Kind != "interrupt" || !errors.Is(cause[0].Err, context.Canceled) || cause[1].Failure == nil || cause[1].Failure.Tag != "ConfigError" || cause[1].Failure.Payload != 42 || cause[2].Err.Error() != "cleanup defect" || closed.Load() != 1 || consumer.Load() != 0 || program.Load() != 0 {
		t.Fatalf("cancelled build: cause=%v closed=%d consumer=%d program=%d", cause, closed.Load(), consumer.Load(), program.Load())
	}
}

func TestLayerWorkerAdmissionUsesNodeButNestedScopedUsesLocalOwner(t *testing.T) {
	type state struct{ worker context.Context }
	localStarted := make(chan struct{})
	plan := unitPlan([]Node[state]{{Spec: NodeSpec{ID: "node"}, Construct: func(fc *FiberContext, s *state) Exit[Unit] {
		local := Invoke(fc, Scoped(func(local *FiberContext) Exit[Unit] {
			child := Invoke(local, Fork(func(child *FiberContext) Exit[Unit] {
				close(localStarted)
				<-child.Context().Done()
				return Interrupt[Unit](child.Context().Err())
			}))
			if child.IsFailure() {
				return Propagate[Unit](child)
			}
			<-localStarted
			return Succeed(Unit{})
		}))
		if local.IsFailure() {
			return local
		}
		started := make(chan context.Context, 1)
		child := Invoke(fc, Fork(func(child *FiberContext) Exit[Unit] {
			started <- child.Context()
			<-child.Context().Done()
			return Interrupt[Unit](child.Context().Err())
		}))
		if child.IsFailure() {
			return Propagate[Unit](child)
		}
		s.worker = <-started
		return Succeed(Unit{})
	}}}, func(Unit) state { return state{} })
	var worker context.Context
	out := Run(Provide(plan, Unit{}, func(s *state) Effect[Unit] {
		worker = s.worker
		return func(*FiberContext) Exit[Unit] {
			if worker.Err() != nil {
				return Die[Unit](errors.New("direct worker ended at constructor return"))
			}
			return Succeed(Unit{})
		}
	}))
	if out.IsFailure() || worker == nil || worker.Err() == nil {
		t.Fatal("node worker did not live until ordered node close", out)
	}
}

func TestLayerAbortRefusesNativeForkWithoutCancellingExistingNodeWorker(t *testing.T) {
	type state struct{}
	ready := make(chan struct{})
	var worker context.Context
	var refused, sawAlive atomic.Bool
	plan := unitPlan([]Node[state]{
		{Spec: NodeSpec{ID: "0/worker"}, Construct: func(fc *FiberContext, _ *state) Exit[Unit] {
			started := make(chan context.Context, 1)
			child := Invoke(fc, Fork(func(child *FiberContext) Exit[Unit] {
				started <- child.Context()
				<-child.Context().Done()
				return Interrupt[Unit](child.Context().Err())
			}))
			if child.IsFailure() {
				return Propagate[Unit](child)
			}
			worker = <-started
			close(ready)
			<-fc.Context().Done()
			// Deliberately bypass Invoke to exercise the native admission guard.
			late := Fork(func(*FiberContext) Exit[Unit] { return Succeed(Unit{}) })(fc)
			refused.Store(late.Interrupted)
			sawAlive.Store(worker.Err() == nil)
			return Interrupt[Unit](fc.Context().Err())
		}},
		{Spec: NodeSpec{ID: "1/failure"}, Construct: func(*FiberContext, *state) Exit[Unit] {
			<-ready
			return Fail[Unit]("ConfigError", Unit{})
		}},
	}, func(Unit) state { return state{} })
	program := Provide(plan, Unit{}, func(*state) Effect[bool] { return func(*FiberContext) Exit[bool] { return Succeed(false) } })
	out := Run(Catch(program, "ConfigError", func() bool { return true }))
	if out.IsFailure() || !out.Value || !refused.Load() || !sawAlive.Load() || worker.Err() == nil {
		t.Fatalf("abort/admission or catchability: %+v refused=%v alive=%v", out, refused.Load(), sawAlive.Load())
	}
}

func TestLayerConcurrentFailuresAreCanonicalAndPendingConsumersNeverRun(t *testing.T) {
	for _, first := range []int{0, 1} {
		type state struct{}
		ready := make(chan int, 2)
		evaluations := []chan context.Context{make(chan context.Context, 1), make(chan context.Context, 1)}
		release := []chan struct{}{make(chan struct{}), make(chan struct{})}
		var consumer atomic.Int32
		nodes := []Node[state]{}
		for i, id := range []NodeID{"0/alpha", "1/beta"} {
			nodes = append(nodes, Node[state]{Spec: NodeSpec{ID: id}, Construct: func(fc *FiberContext, _ *state) Exit[Unit] {
				out := Invoke(fc, AcquireRelease(string(id), func(context.Context) (Unit, error) { return Unit{}, nil }, func(Unit, context.Context) error { return errors.New(string(id) + " cleanup") }))
				if out.IsFailure() {
					return out
				}
				ready <- i
				evaluations[i] <- fc.Context()
				<-release[i]
				return Fail[Unit](string(id), Unit{})
			}})
		}
		nodes = append(nodes, Node[state]{Spec: NodeSpec{ID: "2/consumer", Dependencies: []NodeID{"0/alpha", "1/beta"}}, Construct: func(*FiberContext, *state) Exit[Unit] { consumer.Add(1); return Succeed(Unit{}) }})
		plan := unitPlan(nodes, func(Unit) state { return state{} })
		done := make(chan Exit[Unit], 1)
		go func() {
			done <- Run(Provide(plan, Unit{}, func(*state) Effect[Unit] { return func(*FiberContext) Exit[Unit] { return Succeed(Unit{}) } }))
		}()
		wait(t, ready)
		wait(t, ready)
		close(release[first])
		peer := wait(t, evaluations[1-first])
		wait(t, peer.Done())
		close(release[1-first])
		out := wait(t, done)
		cause := out.Cause()
		if consumer.Load() != 0 || len(cause) != 4 || cause[0].Failure == nil || cause[0].Failure.Tag != "0/alpha" || cause[1].Failure == nil || cause[1].Failure.Tag != "1/beta" || cause[2].Err.Error() != "1/beta cleanup" || cause[3].Err.Error() != "0/alpha cleanup" {
			t.Fatalf("first=%d cause=%v consumer=%d", first, cause, consumer.Load())
		}
	}
}

func TestLayerLateMaskedAcquisitionJoinsThenRollsBackExactlyOnce(t *testing.T) {
	type state struct{}
	acquisitionReady := make(chan struct{})
	controllerContext := make(chan context.Context, 1)
	finish := make(chan struct{})
	var released atomic.Int32
	closed := []string{}
	plan := unitPlan([]Node[state]{
		{Spec: NodeSpec{ID: "0/dependency"}, Construct: func(fc *FiberContext, _ *state) Exit[Unit] {
			return Invoke(fc, AcquireRelease("dependency", func(context.Context) (Unit, error) { return Unit{}, nil }, func(Unit, context.Context) error { closed = append(closed, "dependency"); return nil }))
		}},
		{Spec: NodeSpec{ID: "1/late", Dependencies: []NodeID{"0/dependency"}}, Construct: func(fc *FiberContext, _ *state) Exit[Unit] {
			return Invoke(fc, AcquireRelease("late", func(ctx context.Context) (Unit, error) {
				controllerContext <- fc.Context()
				close(acquisitionReady)
				<-finish
				if ctx.Err() != nil {
					return Unit{}, errors.New("masked acquisition was interrupted")
				}
				return Unit{}, nil
			}, func(Unit, context.Context) error { released.Add(1); closed = append(closed, "late"); return nil }))
		}},
		{Spec: NodeSpec{ID: "2/failure"}, Construct: func(*FiberContext, *state) Exit[Unit] {
			<-acquisitionReady
			return Fail[Unit]("ConfigError", Unit{})
		}},
	}, func(Unit) state { return state{} })
	done := make(chan Exit[Unit], 1)
	go func() {
		done <- Run(Provide(plan, Unit{}, func(*state) Effect[Unit] { return func(*FiberContext) Exit[Unit] { return Succeed(Unit{}) } }))
	}()
	evaluation := wait(t, controllerContext)
	wait(t, evaluation.Done())
	if released.Load() != 0 {
		t.Fatal("late resource released before acquisition completed")
	}
	select {
	case <-done:
		t.Fatal("provision abandoned its interrupted foreign acquisition")
	default:
	}
	close(finish)
	out := wait(t, done)
	if out.Failure == nil || out.Failure.Tag != "ConfigError" || len(out.Additional) != 0 || released.Load() != 1 || !reflect.DeepEqual(closed, []string{"late", "dependency"}) {
		t.Fatalf("late rollback: %+v releases=%d closed=%v", out, released.Load(), closed)
	}
}

func TestLayerMalformedPlansAndAdapterPanicsDefectBeforeOrAfterJoinedRollback(t *testing.T) {
	var calls atomic.Int32
	construct := func(*FiberContext, *Unit) Exit[Unit] { calls.Add(1); return Succeed(Unit{}) }
	for _, nodes := range [][]Node[Unit]{
		{{Spec: NodeSpec{ID: "same"}, Construct: construct}, {Spec: NodeSpec{ID: "same"}, Construct: construct}},
		{{Spec: NodeSpec{ID: "a", Dependencies: []NodeID{"missing"}}, Construct: construct}},
		{{Spec: NodeSpec{ID: "a", Dependencies: []NodeID{"b"}}, Construct: construct}, {Spec: NodeSpec{ID: "b", Dependencies: []NodeID{"a"}}, Construct: construct}},
		{{Spec: NodeSpec{ID: NodeID(strings.Repeat("x", maxPlanMetadata+1))}, Construct: construct}},
	} {
		plan := unitPlan(nodes, func(Unit) Unit { calls.Add(1); return Unit{} })
		out := Run(Provide(plan, Unit{}, func(*Unit) Effect[Unit] {
			calls.Add(1)
			return func(*FiberContext) Exit[Unit] { return Succeed(Unit{}) }
		}))
		if out.Defect == nil || calls.Load() != 0 {
			t.Fatal("invalid graph reached native callbacks", out, calls.Load())
		}
	}
	oversizedID := NewPlan(PlanID(strings.Repeat("x", maxPlanMetadata+1)), []Node[Unit]{}, func(Unit) Unit { calls.Add(1); return Unit{} }, func(*Unit) Unit { calls.Add(1); return Unit{} })
	out := Run(Provide(oversizedID, Unit{}, func(Unit) Effect[Unit] {
		calls.Add(1)
		return func(*FiberContext) Exit[Unit] { return Succeed(Unit{}) }
	}))
	if out.Defect == nil || calls.Load() != 0 {
		t.Fatal("oversized empty graph reached native callbacks", out, calls.Load())
	}
	for _, phase := range []string{"init", "construct", "expose", "program"} {
		var closed atomic.Int32
		plan := NewPlan("panic", []Node[Unit]{{Spec: NodeSpec{ID: "node"}, Construct: func(fc *FiberContext, _ *Unit) Exit[Unit] {
			out := Invoke(fc, AcquireRelease("resource", func(context.Context) (Unit, error) { return Unit{}, nil }, func(Unit, context.Context) error { closed.Add(1); return nil }))
			if phase == "construct" {
				panic("construct")
			}
			return out
		}}}, func(Unit) Unit {
			if phase == "init" {
				panic("init")
			}
			return Unit{}
		}, func(*Unit) Unit {
			if phase == "expose" {
				panic("expose")
			}
			return Unit{}
		})
		out := Run(Provide(plan, Unit{}, func(Unit) Effect[Unit] {
			if phase == "program" {
				panic("program")
			}
			return func(*FiberContext) Exit[Unit] { return Succeed(Unit{}) }
		}))
		wantClosed := int32(1)
		if phase == "init" {
			wantClosed = 0
		}
		if out.Defect == nil || closed.Load() != wantClosed {
			t.Fatalf("%s: %+v closed=%d", phase, out, closed.Load())
		}
	}
}

func TestLayerSchedulerMasksProducerContinuationsAndDrainsOrderedClose(t *testing.T) {
	scheduler := NewTestScheduler()
	points := make(chan int64, 4)
	nodes := []Node[Unit]{}
	for i, duration := range []int64{20, 30} {
		id := NodeID("a")
		cleanup := int64(7)
		if i == 1 {
			id, cleanup = "b", 5
		}
		nodes = append(nodes, Node[Unit]{Spec: NodeSpec{ID: id}, Construct: func(fc *FiberContext, _ *Unit) Exit[Unit] {
			resource := Invoke(fc, AcquireRelease(string(id), func(context.Context) (Unit, error) { return Unit{}, nil }, func(_ Unit, ctx context.Context) error {
				out := RunContextWithScheduler(ctx, scheduler, Sleep(cleanup))
				points <- scheduler.Now()
				if out.IsFailure() {
					return out.Cause()
				}
				return nil
			}))
			if resource.IsFailure() {
				return resource
			}
			out := Invoke(fc, Sleep(duration))
			points <- scheduler.Now()
			return out
		}})
	}
	plan := unitPlan(nodes, func(Unit) Unit { return Unit{} })
	done := make(chan Exit[Unit], 1)
	go func() {
		done <- RunContextWithScheduler(context.Background(), scheduler, AcquireRelease("outer", func(context.Context) (Unit, error) { return Unit{}, nil }, func(_ Unit, ctx context.Context) error {
			out := RunContextWithScheduler(ctx, scheduler, Provide(plan, Unit{}, func(*Unit) Effect[Unit] { return func(*FiberContext) Exit[Unit] { return Succeed(Unit{}) } }))
			if out.IsFailure() {
				return out.Cause()
			}
			return nil
		}))
	}()
	if err := scheduler.AwaitRegistration(context.Background()); err != nil {
		t.Fatal(err)
	}
	adjustWithin(t, scheduler, 42)
	for _, want := range []int64{20, 30, 35, 42} {
		if got := wait(t, points); got != want {
			t.Fatalf("managed constructor/close resumed at %d, want %d", got, want)
		}
	}
	if out := wait(t, done); out.IsFailure() {
		t.Fatal(out.Cause())
	}
}
