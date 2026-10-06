// Effra ownership policy over Effect's scheduler, fibers, scopes and causes.
const __ef_owner = Context.Service("effra/runtime/Owner");
const __ef_autoScope = program => Effect.flatMap(Effect.serviceOption(__ef_owner), owner =>
  Option.isSome(owner) ? program : __ef_scoped(program));

const __ef_scoped = program => Effect.uninterruptibleMask(restore => Effect.gen(function* () {
  const parent = yield* Effect.serviceOption(__ef_owner);
  const resources = yield* Scope.make("sequential");
  const owner = { open: true, parent: Option.getOrNull(parent), children: [] };
  const body = yield* Effect.exit(restore(Effect.provideService(
    Effect.provideService(program, Scope.Scope, resources), __ef_owner, owner)));
  owner.open = false;
  // Request every cancellation before awaiting any child. Effect owns scheduling.
  yield* Effect.sync(() => { for (const child of owner.children) child.fiber.interruptUnsafe(); });
  let cause = Exit.isFailure(body) ? body.cause : Cause.empty;
  for (const child of owner.children) {
    const exit = yield* Fiber.await(child.fiber);
    if (!child.observed && Exit.isFailure(exit) && !Cause.hasInterruptsOnly(exit.cause)) {
      cause = Cause.combine(cause, exit.cause);
    }
  }
  const cleanup = yield* Effect.exit(Scope.close(resources, body));
  if (Exit.isFailure(cleanup)) cause = Cause.combine(cause, cleanup.cause);
  owner.children.length = 0;
  return yield* cause.reasons.length === 0 ? body : Effect.failCause(cause);
}));

const __ef_fork = program => Effect.uninterruptibleMask(() => Effect.gen(function* () {
  const owner = yield* __ef_owner;
  if (!owner.open) return yield* Effect.die(new Error("scope is closing"));
  const fiber = yield* Effect.forkDetach(__ef_scoped(program), { uninterruptible: false });
  const child = { fiber, owner, observed: false };
  owner.children.push(child);
  return child;
}));

const __ef_checkChild = child => Effect.gen(function* () {
  let owner = yield* __ef_owner;
  while (owner && owner !== child.owner) owner = owner.parent;
  if (!owner || !owner.open) return yield* Effect.die(new Error("fiber owner is closed or not an ancestor"));
});
const __ef_join = child => Effect.gen(function* () {
  yield* __ef_checkChild(child);
  const exit = yield* Fiber.await(child.fiber);
  child.observed = true;
  return yield* exit;
});
const __ef_cancel = child => Effect.sync(() => child.fiber.interruptUnsafe());
const __ef_interrupt = child => Effect.uninterruptibleMask(() => Effect.gen(function* () {
  yield* __ef_checkChild(child);
  yield* __ef_cancel(child);
  const exit = yield* Fiber.await(child.fiber);
  child.observed = true;
  if (Exit.isFailure(exit) && !Cause.hasInterruptsOnly(exit.cause)) return yield* exit;
}));

const __ef_timeout = (program, ms) => Effect.suspend(() => {
  if (ms < 0n || ms > 2147483647n) return Effect.die(new Error("invalid millisecond duration"));
  return __ef_scoped(Effect.uninterruptibleMask(restore => Effect.gen(function* () {
    const work = yield* __ef_fork(program);
    const timer = yield* __ef_fork(__ef_call(__ef_service_Scheduler, "sleep", [ms]));
    const winner = yield* restore(Effect.raceFirst(
      Effect.map(Fiber.await(work.fiber), exit => ({ work: true, exit })),
      Effect.map(Fiber.await(timer.fiber), exit => ({ work: false, exit }))));
    work.observed = timer.observed = true;
    yield* Effect.sync(() => { work.fiber.interruptUnsafe(); timer.fiber.interruptUnsafe(); });
    const workExit = yield* Fiber.await(work.fiber);
    yield* Fiber.await(timer.fiber);
    if (winner.work) return yield* winner.exit;
    if (Exit.isFailure(winner.exit)) {
      let cause = winner.exit.cause;
      if (Exit.isFailure(workExit)) {
        for (const reason of workExit.cause.reasons) {
          if (reason._tag === "Fail") cause = Cause.combine(cause, Cause.fail(reason.error));
          else if (reason._tag === "Die") cause = Cause.combine(cause, Cause.die(reason.defect));
        }
      }
      return yield* Effect.failCause(cause);
    }
    let cause = Cause.fail({ _tag: "Timeout" });
    if (Exit.isFailure(workExit)) {
      for (const reason of workExit.cause.reasons) {
        if (reason._tag === "Fail") cause = Cause.combine(cause, Cause.fail(reason.error));
        else if (reason._tag === "Die") cause = Cause.combine(cause, Cause.die(reason.defect));
      }
    }
    return yield* Effect.failCause(cause);
  })));
});

const __ef_catch = (program, tag, fallback) => Effect.catchCause(program, cause => {
  const reason = cause.reasons[0];
  return cause.reasons.length === 1 && reason._tag === "Fail" && reason.error?._tag === tag
    ? Effect.sync(fallback) : Effect.failCause(cause);
});
