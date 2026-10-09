// Unlike Effect.catchTag, which selects the first Fail reason of a cause,
// recovery admits only a solitary typed failure. Defects, interruption and
// cleanup failures keep the complete composite cause.
const __ef_recover = (program, tag, handler) => Effect.uninterruptibleMask(restore =>
  Effect.flatMap(Effect.exit(restore(program)), exit => {
    if (Exit.isSuccess(exit)) return exit;
    const cause = exit.cause, reason = cause.reasons[0];
    return cause.reasons.length === 1 && reason._tag === "Fail" && reason.error?._tag === tag
      ? restore(Effect.suspend(() => handler(reason.error))) : Effect.failCause(cause);
  }));
