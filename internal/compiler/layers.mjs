// The compiler supplies a bounded checked graph and explicit aggregate-field
// adapters. Effect remains the scheduler, interruption and resource authority.
const __ef_provideLayer = (plan, program) => Effect.uninterruptibleMask(restore => Effect.gen(function* () {
  const state = yield* Effect.sync(plan.init);
  const changes = yield* Queue.make({ capacity: Math.max(1, plan.nodes.length) });
  const indices = new Map(plan.nodes.map((node, index) => [node.id, index]));
  const runs = plan.nodes.map(node => ({ state: "pending", remaining: node.dependencies.length,
    owner: undefined, fiber: undefined, cause: Cause.empty, abortCancelled: false, dependents: [] }));
  const order = [];
  const counts = runs.map(run => run.remaining);
  const ready = [];
  for (let index = 0; index < plan.nodes.length; index++) {
    for (const dependency of plan.nodes[index].dependencies) runs[indices.get(dependency)].dependents.push(index);
    if (counts[index] === 0) ready.push(index);
  }
  const compare = (a, b) => plan.nodes[a].id < plan.nodes[b].id ? -1 : plan.nodes[a].id > plan.nodes[b].id ? 1 : 0;
  while (ready.length > 0) {
    ready.sort(compare);
    const index = ready.shift();
    order.push(index);
    for (const dependent of runs[index].dependents) if (--counts[dependent] === 0) ready.push(dependent);
  }
  let aborted = false, completed = 0, callerCause = Cause.empty;
  const abort = () => {
    if (aborted) return;
    aborted = true;
    for (const run of runs) {
      if (run.state === "pending") run.state = "skipped";
      else if (run.state === "constructing") {
        run.abortCancelled = true;
        // A peer can fail while the coordinator is awaiting fork admission.
        // The coordinator delivers this request after receiving the Fiber too.
        run.fiber?.interruptUnsafe();
      }
    }
  };
  const launch = index => Effect.gen(function* () {
    if (aborted) return;
    const run = runs[index];
    run.owner = yield* __ef_openOwner;
    // Effect can scheduler-yield during masked admission. Retain an admitted
    // owner's rollback obligation even when abort wins before producer launch.
    if (aborted) return;
    run.state = "constructing";
    run.fiber = yield* Effect.forkDetach(Effect.uninterruptibleMask(evaluate => Effect.gen(function* () {
      const exit = yield* Effect.exit(evaluate(__ef_useOwner(run.owner, Effect.suspend(() => plan.nodes[index].construct(state)))));
      yield* Effect.sync(() => {
        run.cause = Exit.isFailure(exit) ? exit.cause : Cause.empty;
        if (run.abortCancelled) run.cause = Cause.fromReasons(run.cause.reasons.filter(reason => reason._tag !== "Interrupt"));
        run.state = Exit.isFailure(exit) ? "failed" : "succeeded";
        completed++;
        if (Exit.isFailure(exit)) abort();
        Queue.offerUnsafe(changes, index);
      });
    })), { startImmediately: false, uninterruptible: false });
    if (run.abortCancelled) run.fiber.interruptUnsafe();
  });
  for (const index of order) if (runs[index].remaining === 0 && !aborted) yield* launch(index);
  let processed = 0;
  while (!aborted && processed < plan.nodes.length) {
    const changed = yield* Effect.exit(restore(Queue.take(changes)));
    if (Exit.isFailure(changed)) {
      if (completed < plan.nodes.length) { callerCause = changed.cause; abort(); }
      break;
    }
    processed++;
    const run = runs[changed.value];
    if (run.state === "succeeded" && !aborted) {
      for (const dependent of run.dependents) if (!aborted && --runs[dependent].remaining === 0) yield* launch(dependent);
    }
  }
  for (const run of runs) if (run.fiber) yield* Fiber.await(run.fiber);
  yield* Queue.shutdown(changes);
  const canonical = plan.nodes.map((_, index) => index).sort(compare);
  const reasons = canonical.flatMap(index => runs[index].cause.reasons);
  let primary = -1;
  if (callerCause.reasons.length === 0) {
    for (const tag of ["Fail", "Die", "Interrupt"]) {
      primary = reasons.findIndex(reason => reason._tag === tag);
      if (primary >= 0) break;
    }
  }
  let cause = callerCause;
  if (primary >= 0) cause = Cause.combine(cause, Cause.fromReasons([reasons[primary]]));
  cause = Cause.combine(cause, Cause.fromReasons(reasons.filter((_, index) => index !== primary)));
  let body;
  if (cause.reasons.length > 0) body = Exit.failCause(cause);
  else if (aborted) body = yield* Effect.exit(Effect.interrupt);
  else body = yield* Effect.exit(restore(__ef_scoped(Effect.suspend(() => program(plan.expose(state))))));
  cause = Exit.isFailure(body) ? body.cause : Cause.empty;
  for (const index of order.toReversed()) {
    if (!runs[index].owner) continue;
    const cleanup = yield* __ef_closeOwner(runs[index].owner, Exit.succeed(undefined));
    if (Exit.isFailure(cleanup)) cause = Cause.combine(cause, cleanup.cause);
  }
  return yield* cause.reasons.length === 0 ? body : Effect.failCause(cause);
}));
