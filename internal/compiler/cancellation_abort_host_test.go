package compiler

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// ownerLifecycleSource roots every owner helper the probes below call: a
// scope, fork, join, cancel, interrupt, a timeout and layer provision. The
// JavaScript module selects its prelude by reachability, so each helper must
// be reached from main.
const ownerLifecycleSource = layerDiamondSource + `
effect fn names() -> string uses {Accounts,Invoice} {
 let account=run Accounts.name()
 let invoice=run Invoice.name()
 account+":"+invoice
}
effect fn ping() -> string { "ping" }
effect fn lifecycle() -> string raises {Timeout} uses {Scheduler} {
 scope {
  let first = fork ping()
  run first.cancel()
  let second = fork ping()
  run second.interrupt()
  let third = fork ping()
  run third.join()
  run ping().timeout(5)
 }
}
effect fn main() -> string {
 let probeLifecycle = lifecycle().provide<Scheduler>(LiveScheduler)
 run names().provide(TestApp)
}
`

// A cancellation-aborted owner discards the ordinary typed failures of the
// children it had not observed, and a deadline which wins abandons the timed
// work (design R6 X1). The JS owner must classify an owner as aborted when
// its body exit contains interruption or cancellation was already requested
// on its fiber, exactly as the Go runtime does (runtime/effra
// cancellation_abort_test.go).
func TestJSCancellationAbortedOwnerDiscardsChildFailures(t *testing.T) {
	output := runJS(t, ownerLifecycleSource, `
const tags = exit => Exit.isFailure(exit) ? exit.cause.reasons.map(r => r._tag) : ['Success'];
const failing = Effect.fail({ _tag: 'Boom' });
const settled = child => Fiber.await(child.fiber);
const results = {};

// b1: the body ends by interruption (it joined a cancelled sibling) although
// the owner was never asked to cancel.
results.b1 = tags(await Effect.runPromiseExit(__ef_scoped(Effect.gen(function* () {
  const g = yield* __ef_fork(failing);
  const p = yield* __ef_fork(Effect.never);
  yield* settled(g);
  yield* __ef_cancel(p);
  return yield* __ef_join(p);
}))));

// A normal close raises the unobserved child failure.
results.normal = tags(await Effect.runPromiseExit(__ef_scoped(Effect.gen(function* () {
  const g = yield* __ef_fork(failing);
  yield* settled(g);
  return 'done';
}))));

// w2: the outer owner closes normally and cancels the slow fiber, an aborted
// owner of the failing grandchild.
results.nested = tags(await Effect.runPromiseExit(__ef_scoped(Effect.gen(function* () {
  let notify;
  const grandchildFailed = new Promise(done => { notify = done; });
  yield* __ef_fork(Effect.gen(function* () {
    const g = yield* __ef_fork(failing);
    yield* settled(g);
    notify();
    return yield* Effect.never;
  }));
  yield* Effect.promise(() => grandchildFailed);
  return 'done';
}))));

// A request on the owner's fiber classifies the close as aborted although the
// body exit is a success.
const requested = await Effect.runPromiseExit(Effect.gen(function* () {
  const fiber = yield* Effect.forkChild(__ef_scoped(Effect.gen(function* () {
    const g = yield* __ef_fork(failing);
    yield* settled(g);
    yield* Effect.uninterruptible(Effect.sleep(80));
    return 'done';
  })));
  yield* Effect.sleep(20);
  yield* Fiber.interrupt(fiber);
  return yield* Fiber.await(fiber);
}));
results.requested = Exit.isSuccess(requested) ? tags(requested.value) : tags(requested);

// m1: a deadline which fails while the work also fails keeps the deadline's
// cause and the work's defect, not the work's typed failure.
const scheduler = { sleep: () => Effect.fail({ _tag: 'TimerBroken' }), advance: () => Effect.void, awaitRegistration: () => Effect.void };
const slowFail = Effect.gen(function* () { yield* Effect.sleep(5); return yield* Effect.fail({ _tag: 'Boom' }); });
const deadline = await Effect.runPromiseExit(Effect.provideService(__ef_timeout(Effect.uninterruptible(slowFail), 1n), __ef_service_Scheduler, scheduler));
results.deadline = Exit.isFailure(deadline) ? deadline.cause.reasons.filter(r => r._tag === 'Fail').map(r => r.error._tag) : ['Success'];

console.log(JSON.stringify(results));
`)
	want := `{"b1":["Interrupt"],"normal":["Fail"],"nested":["Success"],"requested":["Interrupt"],"deadline":["TimerBroken"]}` + "\n"
	if output != want {
		t.Fatalf("got  %swant %s", output, want)
	}
}

// Defects and cleanup failures survive a cancellation-aborted close: only the
// ordinary typed failures of unobserved children are discarded. A later
// "drop everything" simplification must fail here.
func TestJSCancellationAbortedOwnerKeepsDefectsAndCleanupFailures(t *testing.T) {
	output := runJS(t, ownerLifecycleSource, `
const reasons = exit => Exit.isFailure(exit) ? exit.cause.reasons.map(r => r._tag === 'Fail' ? 'Fail:' + r.error._tag : r._tag === 'Die' ? 'Die:' + r.defect.message : r._tag) : ['Success'];
const settled = child => Fiber.await(child.fiber);
const aborted = (child, withCleanup, cleanup) => Effect.runPromiseExit(__ef_scoped(Effect.gen(function* () {
  if (withCleanup) yield* Effect.addFinalizer(() => cleanup);
  const g = yield* __ef_fork(child);
  const p = yield* __ef_fork(Effect.never);
  yield* settled(g);
  yield* __ef_cancel(p);
  return yield* __ef_join(p); // ends by interruption: the owner is aborted
})));
const results = {};
results.defect = reasons(await aborted(Effect.die(new Error('child defect')), false));
results.cleanup = reasons(await aborted(Effect.fail({ _tag: 'Boom' }), true, Effect.die(new Error('release failed'))));
results.cleanupTyped = reasons(await aborted(Effect.fail({ _tag: 'Boom' }), true, Effect.fail({ _tag: 'CleanupTyped' })));
console.log(JSON.stringify(results));
`)
	want := `{"defect":["Interrupt","Die:child defect"],"cleanup":["Interrupt","Die:release failed"],"cleanupTyped":["Interrupt","Fail:CleanupTyped"]}` + "\n"
	if output != want {
		t.Fatalf("got  %swant %s", output, want)
	}
}

// A layer node's constructor is an owner like any other: one which forks a
// failing worker, never observes it and ends by interruption is
// cancellation-aborted, so the ordered close of the node discards the typed
// failure (the Go twin is TestLayerNodeOwnerDiscardsUnobservedChildFailure...).
// A construction which ends by an ordinary failure keeps the worker's failure.
func TestJSLayerNodeOwnerIsCancellationAborted(t *testing.T) {
	output := runJS(t, ownerLifecycleSource, `
const tags = exit => Exit.isFailure(exit) ? exit.cause.reasons.map(r => r._tag === 'Fail' ? 'Fail:' + r.error._tag : r._tag) : ['Success'];
const settled = child => Fiber.await(child.fiber);
const plan = construct => ({ id: 'node-owner', init: () => ({}), nodes: [{ id: 'node', dependencies: [], construct: () => construct }], expose: s => s });
const build = construct => Effect.runPromiseExit(__ef_provideLayer(plan(construct), () => Effect.void));
const results = {};
results.interrupted = tags(await build(Effect.gen(function* () {
  const g = yield* __ef_fork(Effect.fail({ _tag: 'Boom' }));
  const p = yield* __ef_fork(Effect.never);
  yield* settled(g);
  yield* __ef_cancel(p);
  return yield* __ef_join(p);
})));
results.failed = tags(await build(Effect.gen(function* () {
  const g = yield* __ef_fork(Effect.fail({ _tag: 'Boom' }));
  yield* settled(g);
  return yield* Effect.fail({ _tag: 'Construct' });
})));
console.log(JSON.stringify(results));
`)
	want := `{"interrupted":["Interrupt"],"failed":["Fail:Construct","Fail:Boom"]}` + "\n"
	if output != want {
		t.Fatalf("got  %swant %s", output, want)
	}
}

// Typed failures returned by a Scope.close finalizer are cleanup failures even
// when an abandoned owner would discard the same failure from its body. This
// exercises the native adapter boundary directly; it does not claim that .ef
// source currently admits typed resource releases.
func TestJSCleanupProvenanceAtScopeCloseOnBunAndNode(t *testing.T) {
	assertions := `
const labels = exit => Exit.isFailure(exit) ? exit.cause.reasons.map(reason =>
  reason._tag === 'Fail' ? 'Fail:' + reason.error._tag :
  reason._tag === 'Die' ? 'Die:' + reason.defect.message : reason._tag
) : ['Success'];

async function nested(cleanup, aborted) {
  let signal;
  const registered = new Promise(resolve => signal = resolve);
  return labels(await Effect.runPromiseExit(__ef_scoped(Effect.gen(function* () {
    yield* __ef_fork(Effect.gen(function* () {
      yield* Effect.addFinalizer(() => cleanup);
      signal();
      return yield* Effect.never;
    }));
    yield* Effect.promise(() => registered);
    if (!aborted) return 'done';
    const sibling = yield* __ef_fork(Effect.never);
    yield* __ef_cancel(sibling);
    return yield* __ef_join(sibling);
  }))));
}

async function timed(cleanup, failedDeadline) {
  let signal;
  const registered = new Promise(resolve => signal = resolve);
  const scheduler = {
    sleep: () => Effect.gen(function* () {
      yield* Effect.promise(() => registered);
      if (failedDeadline) return yield* Effect.fail({ _tag: 'TimerBroken' });
    }),
    advance: () => Effect.void,
    awaitRegistration: () => Effect.void
  };
  const work = Effect.gen(function* () {
    yield* Effect.addFinalizer(() => cleanup);
    signal();
    return yield* Effect.never;
  });
  return labels(await Effect.runPromiseExit(Effect.provideService(
    __ef_timeout(work, 1n), __ef_service_Scheduler, scheduler
  )));
}

const results = {};
results.normalChildCleanup = await nested(Effect.fail({ _tag: 'CleanupTyped' }), false);
results.abortedChildCleanup = await nested(Effect.fail({ _tag: 'CleanupTyped' }), true);
results.abortedChildCleanupDefect = await nested(Effect.die(new Error('release failed')), true);
results.timeoutChildCleanup = await timed(Effect.fail({ _tag: 'CleanupTyped' }), false);
results.failedDeadlineChildCleanup = await timed(Effect.fail({ _tag: 'CleanupTyped' }), true);
results.timeoutChildCleanupDefect = await timed(Effect.die(new Error('release failed')), false);

results.normalChildBody = labels(await Effect.runPromiseExit(__ef_scoped(Effect.gen(function* () {
  const child = yield* __ef_fork(Effect.fail({ _tag: 'BodyTyped' }));
  yield* Fiber.await(child.fiber);
  return 'done';
}))));
results.abortedChildBody = labels(await Effect.runPromiseExit(__ef_scoped(Effect.gen(function* () {
  const child = yield* __ef_fork(Effect.fail({ _tag: 'BodyTyped' }));
  yield* Fiber.await(child.fiber);
  const sibling = yield* __ef_fork(Effect.never);
  yield* __ef_cancel(sibling);
  return yield* __ef_join(sibling);
}))));

const observed = await Effect.runPromiseExit(__ef_scoped(Effect.gen(function* () {
  const child = yield* __ef_fork(Effect.gen(function* () {
    yield* Effect.addFinalizer(() => Effect.fail({ _tag: 'ObservedCleanup' }));
    return yield* Effect.fail({ _tag: 'ObservedBody' });
  }));
  const childExit = yield* Effect.exit(__ef_join(child));
  return labels(childExit);
})));
results.observedChild = Exit.isSuccess(observed) ? observed.value : labels(observed);

results.outerFinalizer = labels(await Effect.runPromiseExit(__ef_scoped(Effect.gen(function* () {
  yield* Effect.addFinalizer(() => Effect.fail({ _tag: 'OuterCleanup' }));
  const child = yield* __ef_fork(Effect.fail({ _tag: 'BodyTyped' }));
  yield* Fiber.await(child.fiber);
  const sibling = yield* __ef_fork(Effect.never);
  yield* __ef_cancel(sibling);
  return yield* __ef_join(sibling);
}))));

const same = Object.freeze({ _tag: 'Same' });
results.equalBodyCleanup = labels(await Effect.runPromiseExit(__ef_scoped(Effect.gen(function* () {
  const child = yield* __ef_fork(Effect.gen(function* () {
    yield* Effect.addFinalizer(() => Effect.fail(same));
    return yield* Effect.fail(same);
  }));
  yield* Fiber.await(child.fiber);
  const sibling = yield* __ef_fork(Effect.never);
  yield* __ef_cancel(sibling);
  return yield* __ef_join(sibling);
}))));

let innerRegistered;
const innerReady = new Promise(resolve => innerRegistered = resolve);
let outerRunning;
const outerReady = new Promise(resolve => outerRunning = resolve);
const sameCleanup = Object.freeze({ _tag: 'SameCleanup' });
results.equalNestedCleanupOccurrences = labels(await Effect.runPromiseExit(__ef_scoped(Effect.gen(function* () {
  yield* __ef_fork(Effect.gen(function* () {
    yield* Effect.addFinalizer(() => Effect.fail(sameCleanup));
    yield* __ef_fork(Effect.gen(function* () {
      yield* Effect.addFinalizer(() => Effect.fail(sameCleanup));
      innerRegistered();
      return yield* Effect.never;
    }));
    yield* Effect.promise(() => innerReady);
    outerRunning();
    return yield* Effect.never;
  }));
  yield* Effect.promise(() => outerReady);
  const sibling = yield* __ef_fork(Effect.never);
  yield* __ef_cancel(sibling);
  return yield* __ef_join(sibling);
}))));

console.log(JSON.stringify(results));
`

	r := CompileFor(ownerLifecycleSource, "js")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	js, _, err := r.Emit(false)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join("..", "..")
	dir, err := os.MkdirTemp(root, ".cleanup-provenance-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "probe.mjs")
	if err := os.WriteFile(path, []byte(js+"\n"+assertions), 0644); err != nil {
		t.Fatal(err)
	}

	outputs := map[string]string{}
	for _, name := range []string{"bun", "node"} {
		runtime, err := exec.LookPath(name)
		if err != nil {
			t.Fatalf("%s is required for cleanup provenance controls: %v", name, err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		output, err := exec.CommandContext(ctx, runtime, path).CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("%s cleanup provenance probe: %v\n%s", name, err, output)
		}
		outputs[name] = string(output)
	}

	want := `{"normalChildCleanup":["Interrupt","Fail:CleanupTyped"],"abortedChildCleanup":["Interrupt","Fail:CleanupTyped"],"abortedChildCleanupDefect":["Interrupt","Die:release failed"],"timeoutChildCleanup":["Fail:Timeout","Fail:CleanupTyped"],"failedDeadlineChildCleanup":["Fail:TimerBroken","Fail:CleanupTyped"],"timeoutChildCleanupDefect":["Fail:Timeout","Die:release failed"],"normalChildBody":["Fail:BodyTyped"],"abortedChildBody":["Interrupt"],"observedChild":["Fail:ObservedBody","Fail:ObservedCleanup"],"outerFinalizer":["Interrupt","Fail:OuterCleanup"],"equalBodyCleanup":["Interrupt","Fail:Same"],"equalNestedCleanupOccurrences":["Interrupt","Fail:SameCleanup","Fail:SameCleanup"]}` + "\n"
	for _, name := range []string{"bun", "node"} {
		if outputs[name] != want {
			t.Errorf("%s got  %swant %s", name, outputs[name], want)
		}
	}
}
