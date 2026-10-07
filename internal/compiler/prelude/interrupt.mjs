const __ef_interrupt = child => Effect.uninterruptibleMask(() => Effect.gen(function* () {
  yield* __ef_checkChild(child);
  yield* __ef_cancel(child);
  const exit = yield* Fiber.await(child.fiber);
  child.observed = true;
  if (Exit.isFailure(exit) && !Cause.hasInterruptsOnly(exit.cause)) return yield* exit;
}));
