const __ef_catch = (program, tag, fallback) => __ef_recover(program, tag, () => Effect.sync(fallback));
