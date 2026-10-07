const __ef_fork = program => Effect.uninterruptibleMask(() => Effect.gen(function* () {
  const owner = yield* __ef_owner;
  if (!owner.open) return yield* Effect.die(new Error("scope is closing"));
  const fiber = yield* Effect.forkDetach(__ef_scoped(program), { uninterruptible: false });
  const child = { fiber, owner, observed: false };
  owner.children.push(child);
  return child;
}));
