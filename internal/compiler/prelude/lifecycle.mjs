// Effra ownership policy over Effect's scheduler, fibers, scopes and causes.
const __ef_owner = Context.Service("effra/runtime/Owner");
const __ef_causeOccurrence = Context.Service("effra/runtime/CauseOccurrence");
const __ef_causeCleanupReason = Context.Service("effra/runtime/CauseCleanup");
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
// Scope.close returns only finalizer failures. Mark them before combining with
// the body's cause so equal-valued body failures keep distinct provenance.
const __ef_causeCleanup = cause => Cause.fromReasons(cause.reasons.map(reason => {
  if (reason._tag === "Interrupt") return reason;
  let marked = reason;
  if (!marked.annotations.has(__ef_causeOccurrence.key)) {
    marked = marked.annotate(Context.make(__ef_causeOccurrence, () => undefined), { overwrite: true });
  }
  if (!marked.annotations.has(__ef_causeCleanupReason.key)) {
    marked = marked.annotate(Context.make(__ef_causeCleanupReason, () => undefined), { overwrite: true });
  }
  return marked;
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

// An owner is cancellation-aborted when its body exit contains interruption or
// cancellation was already requested on its executing fiber. The request is
// read before close asks the children to stop, so the classification cannot
// change while the children shut down. A cancellation-aborted owner discards
// the ordinary typed failures of children nobody observed; defects and cleanup
// failures stay. A close that is not aborted raises them.
const __ef_cancelRequested = Effect.withFiber(fiber => Effect.succeed(fiber._interruptedCause !== undefined));
const __ef_withoutFailures = cause => Cause.fromReasons(cause.reasons.filter(reason =>
  reason._tag !== "Fail" || reason.annotations.has(__ef_causeCleanupReason.key)
));

// A layer node's owner closes after its construction ended, so the caller passes
// the construction's classification as constructionAborted.
const __ef_closeOwner = (owner, body, constructionAborted = false) => Effect.uninterruptible(Effect.gen(function* () {
  owner.open = false;
  const requested = yield* __ef_cancelRequested;
  const aborted = requested || constructionAborted || (Exit.isFailure(body) && body.cause.reasons.some(reason => reason._tag === "Interrupt"));
  // Request every cancellation before awaiting any child. Effect owns scheduling.
  yield* Effect.sync(() => { for (const child of owner.children) child.fiber.interruptUnsafe(); });
  let cause = Exit.isFailure(body) ? __ef_causeOccurrences(body.cause) : Cause.empty;
  for (const child of owner.children) {
    const exit = yield* Fiber.await(child.fiber);
    if (!child.observed && Exit.isFailure(exit)) {
      const kept = aborted ? __ef_withoutFailures(exit.cause) : exit.cause;
      if (kept.reasons.length > 0 && !Cause.hasInterruptsOnly(kept)) cause = Cause.combine(cause, __ef_causeOccurrences(kept));
    }
  }
  const cleanup = yield* Effect.exit(Scope.close(owner.resources, body));
  if (Exit.isFailure(cleanup)) cause = Cause.combine(cause, __ef_causeCleanup(cleanup.cause));
  owner.children.length = 0;
  return cause.reasons.length === 0 ? body : Exit.failCause(cause);
}));

const __ef_scoped = program => Effect.uninterruptibleMask(restore => Effect.gen(function* () {
  const owner = yield* __ef_openOwner;
  const body = yield* Effect.exit(restore(__ef_useOwner(owner, program)));
  return yield* (yield* __ef_closeOwner(owner, body));
}));
