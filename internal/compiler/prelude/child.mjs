const __ef_checkChild = child => Effect.gen(function* () {
  let owner = yield* __ef_owner;
  while (owner && owner !== child.owner) owner = owner.parent;
  if (!owner || !owner.open) return yield* Effect.die(new Error("fiber owner is closed or not an ancestor"));
});
