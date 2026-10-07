// Effra ownership policy over Effect's scheduler, fibers, scopes and causes.
const __ef_owner = Context.Service("effra/runtime/Owner");
const __ef_causeOccurrence = Context.Service("effra/runtime/CauseOccurrence");
// Effect deduplicates equal reason values when combining Causes. A reason's
// occurrence is lifecycle identity, not public failure identity, so publish a
// private marker before the first coordinator merge and retain it thereafter.
const __ef_causeOccurrences = cause => Cause.fromReasons(cause.reasons.map(reason => {
  // Interrupt is cancellation control, not an independent typed failure
  // occurrence; retain Effect's structural interruption normalization.
  if (reason._tag === "Interrupt") return reason;
  if (reason.annotations.has(__ef_causeOccurrence.key)) return reason;
  return reason.annotate(Context.make(__ef_causeOccurrence, () => undefined), { overwrite: true });
}));
const __ef_autoScope = program => Effect.flatMap(Effect.serviceOption(__ef_owner), owner =>
  Option.isSome(owner) ? program : __ef_scoped(program));

const __ef_openOwner = Effect.gen(function* () {
  const parent = yield* Effect.serviceOption(__ef_owner);
  const resources = yield* Scope.make("sequential");
  return { open: true, parent: Option.getOrNull(parent), children: [], resources };
});
const __ef_useOwner = (owner, program) => Effect.provideService(
  Effect.provideService(program, Scope.Scope, owner.resources), __ef_owner, owner);

const __ef_closeOwner = (owner, body) => Effect.uninterruptible(Effect.gen(function* () {
  owner.open = false;
  // Request every cancellation before awaiting any child. Effect owns scheduling.
  yield* Effect.sync(() => { for (const child of owner.children) child.fiber.interruptUnsafe(); });
  let cause = Exit.isFailure(body) ? __ef_causeOccurrences(body.cause) : Cause.empty;
  for (const child of owner.children) {
    const exit = yield* Fiber.await(child.fiber);
    if (!child.observed && Exit.isFailure(exit) && !Cause.hasInterruptsOnly(exit.cause)) {
      cause = Cause.combine(cause, __ef_causeOccurrences(exit.cause));
    }
  }
  const cleanup = yield* Effect.exit(Scope.close(owner.resources, body));
  if (Exit.isFailure(cleanup)) cause = Cause.combine(cause, __ef_causeOccurrences(cleanup.cause));
  owner.children.length = 0;
  return cause.reasons.length === 0 ? body : Exit.failCause(cause);
}));

const __ef_scoped = program => Effect.uninterruptibleMask(restore => Effect.gen(function* () {
  const owner = yield* __ef_openOwner;
  const body = yield* Effect.exit(restore(__ef_useOwner(owner, program)));
  return yield* (yield* __ef_closeOwner(owner, body));
}));
