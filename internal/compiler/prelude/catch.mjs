const __ef_catch = (program, tag, fallback) => Effect.uninterruptibleMask(restore =>
  Effect.flatMap(Effect.exit(restore(program)), exit => {
    if (Exit.isSuccess(exit)) return exit;
    const cause = exit.cause, reason = cause.reasons[0];
    return cause.reasons.length === 1 && reason._tag === "Fail" && reason.error?._tag === tag
      ? restore(Effect.sync(fallback)) : Effect.failCause(cause);
  }));
