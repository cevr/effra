const __ef_join = child => Effect.gen(function* () {
  yield* __ef_checkChild(child);
  const exit = yield* Fiber.await(child.fiber);
  child.observed = true;
  return yield* exit;
});
