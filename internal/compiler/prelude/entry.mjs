// Program entry: SIGINT/SIGTERM interrupt main, as the Go entry's signal
// context does. A failed exit writes the entry failure report, the same bytes
// as runtime/effra/report.go (docs/design.md, "Entry failure report"). The
// compiler's entry report plan gives every value's checked rendering, so no
// value is classified by its runtime shape.
const __ef_reportFields = (plan, value, fields) => {
  if (fields.length === 0) return '{}';
  return '{ ' + fields.map(field => field.name + ': ' + __ef_reportValue(plan, value[field.name], field.node)).join(', ') + ' }';
};
const __ef_reportValue = (plan, value, index) => {
  const node = plan.nodes[index];
  switch (node?.kind) {
    case 'string': if (typeof value === 'string') return __ef_quoteText(value); break;
    case 'i64': if (typeof value === 'bigint') return value.toString(); break;
    case 'bool': if (typeof value === 'boolean') return String(value); break;
    case 'void': return 'void';
    case 'bytes': if (value instanceof Uint8Array) return '<bytes len=' + value.length + '>'; break;
    case 'fn': return '<fn>';
    case 'record': if (value !== null && typeof value === 'object') return __ef_reportFields(plan, value, node.fields); break;
    case 'enum': {
      const variant = value !== null && typeof value === 'object' ? node.variants.find(candidate => candidate.tag === value._tag) : undefined;
      if (variant) return variant.fields.length === 0 ? variant.name : variant.name + ' ' + __ef_reportFields(plan, value, variant.fields);
      break;
    }
  }
  return '<opaque>';
};
const __ef_reportReason = (plan, reason) => {
  if (reason._tag === 'Interrupt') return 'interrupt';
  if (reason._tag === 'Die') {
    const defect = reason.defect;
    return 'defect: ' + __ef_quoteText(defect instanceof Error ? defect.message : String(defect));
  }
  const error = reason.error;
  const tag = error !== null && typeof error === 'object' && typeof error._tag === 'string' ? error._tag : undefined;
  if (tag === undefined) return 'failure: <opaque>';
  const failure = plan.failures.find(candidate => candidate.tag === tag);
  if (failure === undefined) return 'failure: ' + tag;
  if (failure.diagnostic) return typeof error.message === 'string' ? 'failure: ' + tag + ' { message: ' + __ef_quoteText(error.message) + ' }' : 'failure: ' + tag;
  return failure.fields.length === 0 ? 'failure: ' + tag : 'failure: ' + tag + ' ' + __ef_reportFields(plan, error, failure.fields);
};
const __ef_runEntry = (program, plan) => {
  const signal = new AbortController();
  const stop = () => signal.abort();
  process.on('SIGINT', stop);
  process.on('SIGTERM', stop);
  return Effect.runPromiseExit(program, { signal: signal.signal }).then(exit => {
    if (Exit.isSuccess(exit)) {
      if (exit.value !== undefined) console.log(typeof exit.value === 'bigint' ? exit.value.toString() : exit.value);
      return;
    }
    const reasons = exit.cause.reasons;
    process.stderr.write(reasons.map(reason => __ef_reportReason(plan, reason) + '\n').join(''));
    process.exitCode = reasons.length > 0 && reasons.every(reason => reason._tag === 'Interrupt') ? 130 : 1;
  }).finally(() => {
    process.off('SIGINT', stop);
    process.off('SIGTERM', stop);
  });
};
