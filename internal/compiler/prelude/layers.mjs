// The compiler supplies a bounded checked graph and explicit aggregate-field
// adapters. Effect remains the scheduler, interruption and resource authority.
// Effect deduplicates equal reason values when combining Causes. Each observed
// node/cleanup reason is an occurrence, so retain a private identity through
// later Effect joins without wrapping its typed payload or replacing tracing.
const __ef_layerOccurrences = cause => __ef_causeOccurrences(cause);
// One build owns an acquisition table with one producer Fiber per selected
// node. Dependent producers and other waiters observe an entry's terminal
// outcome; interrupting a waiter never interrupts the build-owned producer.
// This deliberately differs from Effect's MemoMap first-requester build.
const __ef_layerBuild = plan => Effect.gen(function* () {
  const state = yield* Effect.sync(plan.init);
  const changes = yield* Queue.make({ capacity: Math.max(1, plan.nodes.length) });
  const indices = new Map(plan.nodes.map((node, index) => [node.id, index]));
  const entries = plan.nodes.map(node => ({ state: "waiting", dependencies: node.dependencies.map(id => indices.get(id)),
    owner: undefined, fiber: undefined, cause: Cause.empty, abortCancelled: false, dependents: [] }));
  const order = [];
  const counts = entries.map(entry => entry.dependencies.length);
  const ready = [];
  entries.forEach((entry, index) => {
    for (const dependency of entry.dependencies) entries[dependency].dependents.push(index);
    if (counts[index] === 0) ready.push(index);
  });
  const compare = (a, b) => plan.nodes[a].id < plan.nodes[b].id ? -1 : plan.nodes[a].id > plan.nodes[b].id ? 1 : 0;
  while (ready.length > 0) {
    ready.sort(compare);
    const index = ready.shift();
    order.push(index);
    for (const dependent of entries[index].dependents) if (--counts[dependent] === 0) ready.push(dependent);
  }
  const build = { aborted: false, callerCause: Cause.empty };
  const terminal = entry => entry.state === "succeeded" || entry.state === "failed" || entry.state === "skipped";
  // The terminal entry is recorded before its producer exits, so every waiter
  // reads one outcome and the coordinator counts each producer once.
  const finish = (index, state, cause) => {
    const entry = entries[index];
    entry.state = state;
    entry.cause = cause;
    if (state === "failed") build.abort();
    Queue.offerUnsafe(changes, index);
  };
  build.abort = () => {
    if (build.aborted) return;
    build.aborted = true;
    for (const entry of entries) {
      if (entry.state === "constructing") entry.abortCancelled = true;
      // A producer can fail while the coordinator awaits fork admission.
      // start delivers this request after receiving the Fiber too.
      if (!terminal(entry)) entry.fiber?.interruptUnsafe();
    }
  };
  build.await = index => Effect.suspend(() => {
    const entry = entries[index];
    const outcome = Effect.suspend(() => entry.state === "succeeded" ? Effect.void
      : entry.state === "failed" && entry.cause.reasons.length > 0 ? Effect.failCause(entry.cause)
      // Skipped or abort-interrupted entries carry no fabricated failure.
      : Effect.interrupt);
    return terminal(entry) || !entry.fiber ? outcome : Effect.flatMap(Fiber.await(entry.fiber), () => outcome);
  });
  const produce = index => Effect.uninterruptibleMask(restore => Effect.gen(function* () {
    const entry = entries[index];
    for (const dependency of entry.dependencies) {
      // The dependency's own entry retains its reasons once.
      if (Exit.isFailure(yield* Effect.exit(restore(build.await(dependency))))) return finish(index, "skipped", Cause.empty);
    }
    if (build.aborted) return finish(index, "skipped", Cause.empty);
    entry.owner = yield* __ef_openOwner;
    // Effect can scheduler-yield during masked admission. Retain an admitted
    // owner's rollback obligation even when abort wins before construction.
    if (build.aborted) return finish(index, "skipped", Cause.empty);
    entry.state = "constructing";
    const exit = yield* Effect.exit(restore(__ef_useOwner(entry.owner, Effect.suspend(() => plan.nodes[index].construct(state)))));
    let cause = Exit.isFailure(exit) ? exit.cause : Cause.empty;
    if (entry.abortCancelled) cause = Cause.fromReasons(cause.reasons.filter(reason => reason._tag !== "Interrupt"));
    // Success removes evaluation cancellation authority; the owner lives until ordered close.
    finish(index, Exit.isFailure(exit) ? "failed" : "succeeded", __ef_layerOccurrences(cause));
  }));
  // Producers belong to the build, dependencies first; only abort cancels them.
  build.start = Effect.gen(function* () {
    for (const index of order) {
      const entry = entries[index];
      if (build.aborted) { finish(index, "skipped", Cause.empty); continue; }
      entry.fiber = yield* Effect.forkDetach(produce(index), { startImmediately: false, uninterruptible: false });
      if (build.aborted && !terminal(entry)) entry.fiber.interruptUnsafe();
    }
  });
  // Joins every producer. Caller interruption is whole-build cancellation: it
  // aborts producers and still waits for their cooperative completion.
  build.settle = restore => Effect.gen(function* () {
    // After abort, joining the Fibers replaces counting: a producer interrupted
    // before its first step never records an outcome.
    for (let processed = 0; processed < entries.length && !build.aborted; processed++) {
      const changed = yield* Effect.exit(restore(Queue.take(changes)));
      if (Exit.isFailure(changed)) { build.callerCause = changed.cause; build.abort(); break; }
    }
    for (const entry of entries) if (entry.fiber) yield* Fiber.await(entry.fiber);
    yield* Queue.shutdown(changes);
    for (const entry of entries) if (!terminal(entry)) entry.state = "skipped";
  });
  // Retain each producer's reasons once in canonical node order; waiters never
  // contribute copies of a shared entry's failure.
  build.cause = () => {
    const canonical = plan.nodes.map((_, index) => index).sort(compare);
    const reasons = canonical.flatMap(index => entries[index].cause.reasons);
    let primary = -1;
    if (build.callerCause.reasons.length === 0) {
      for (const tag of ["Fail", "Die", "Interrupt"]) {
        primary = reasons.findIndex(reason => reason._tag === tag);
        if (primary >= 0) break;
      }
    }
    let cause = __ef_causeOccurrences(build.callerCause);
    if (primary >= 0) cause = Cause.combine(cause, __ef_causeOccurrences(Cause.fromReasons([reasons[primary]])));
    return Cause.combine(cause, __ef_causeOccurrences(Cause.fromReasons(reasons.filter((_, index) => index !== primary))));
  };
  // Dependents release before dependencies; every admitted owner closes even
  // when an earlier one reports cleanup defects.
  build.close = cause => Effect.gen(function* () {
    for (const index of order.toReversed()) {
      if (!entries[index].owner) continue;
      const cleanup = yield* __ef_closeOwner(entries[index].owner, Exit.succeed(undefined));
      if (Exit.isFailure(cleanup)) cause = Cause.combine(cause, __ef_layerOccurrences(cleanup.cause));
    }
    return cause;
  });
  build.state = state;
  build.entries = entries;
  return build;
});
const __ef_provideLayer = (plan, program) => Effect.uninterruptibleMask(restore => Effect.gen(function* () {
  const build = yield* __ef_layerBuild(plan);
  yield* build.start;
  yield* build.settle(restore);
  let cause = build.cause();
  let body;
  if (cause.reasons.length > 0) body = Exit.failCause(cause);
  else if (build.aborted) body = yield* Effect.exit(Effect.interrupt);
  else body = yield* Effect.exit(restore(__ef_scoped(Effect.suspend(() => program(plan.expose(build.state))))));
  cause = yield* build.close(Exit.isFailure(body) ? body.cause : Cause.empty);
  return yield* cause.reasons.length === 0 ? body : Effect.failCause(cause);
}));
