// Effra's Latch is Effect's Latch. Its effectful `open` completes the latch,
// moves every parked waiter to one task on the signaller's dispatcher and
// returns, so the signaller keeps its turn and each waiter resumes on that
// task, never inline inside the signaller's turn. A waiter interrupted before
// that task runs leaves the queue and is interrupted; the latch stays open.
// `openUnsafe` resumes inline and is never used.
const __ef_latch={
  make:Effect.sync(()=>Latch.makeUnsafe(false)),
  valid:latch=>latch!==null&&typeof latch==='object'&&typeof latch.openUnsafe==='function',
  invalid:()=>Effect.die(new Error('invalid latch handle')),
  await:latch=>__ef_latch.valid(latch)?latch.await:__ef_latch.invalid(),
  signal:latch=>__ef_latch.valid(latch)?Effect.asVoid(latch.open):__ef_latch.invalid()
};
