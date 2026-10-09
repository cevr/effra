// The deadline won: the timed computation is abandoned. Its ordinary typed
// failures are discarded; defects and typed cleanup failures are kept.
const __ef_abandoned = (cause, workExit) => {
  if (!Exit.isFailure(workExit)) return cause;
  for (const reason of workExit.cause.reasons) {
    if (reason._tag === "Die" || (reason._tag === "Fail" && reason.annotations.has(__ef_causeCleanupReason.key))) {
      cause = Cause.combine(cause, __ef_causeOccurrences(Cause.fromReasons([reason])));
    }
  }
  return cause;
};

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
    const timerExit = yield* Fiber.await(timer.fiber);
    if (winner.work) {
      let cause = Exit.isFailure(winner.exit) ? __ef_causeOccurrences(winner.exit.cause) : Cause.empty;
      if (Exit.isFailure(timerExit)) {
        for (const reason of timerExit.cause.reasons) {
          if (reason._tag === "Fail" || reason._tag === "Die") cause = Cause.combine(cause, __ef_causeOccurrences(Cause.fromReasons([reason])));
        }
      }
      if (cause.reasons.length > 0) return yield* Effect.failCause(cause);
      return yield* winner.exit;
    }
    if (Exit.isFailure(winner.exit)) {
      let cause = __ef_causeOccurrences(winner.exit.cause);
      cause = __ef_abandoned(cause, workExit);
      return yield* Effect.failCause(cause);
    }
    let cause = __ef_causeOccurrences(Cause.fail({ _tag: "Timeout" }));
    cause = __ef_abandoned(cause, workExit);
    return yield* Effect.failCause(cause);
  })));
});
