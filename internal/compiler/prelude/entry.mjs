// Program entry: SIGINT/SIGTERM interrupt main, as the Go entry's signal
// context does. A failed exit writes the entry failure report, the same text
// as runtime/effra/report.go (docs/design.md, "Failure is more than a Result").
const __ef_reportString = text => {
  let out = '"';
  for (const character of text) {
    const code = character.codePointAt(0);
    if (character === '"') out += '\\"';
    else if (character === '\\') out += '\\\\';
    else if (character === '\n') out += '\\n';
    else if (character === '\r') out += '\\r';
    else if (character === '\t') out += '\\t';
    else if (code < 0x20 || code === 0x7f) out += '\\u' + code.toString(16).padStart(4, '0');
    else if (code >= 0xd800 && code <= 0xdfff) out += '�';
    else out += character;
  }
  return out + '"';
};
const __ef_reportPlain = value => {
  const prototype = Object.getPrototypeOf(value);
  return prototype === Object.prototype || prototype === null;
};
// Fields are listed in byte order of their source names: an object literal
// keeps construction order, not declaration order.
const __ef_reportFields = value => {
  const names = Object.keys(value).filter(name => name !== '_tag').sort((a, b) => a < b ? -1 : a > b ? 1 : 0);
  if (names.length === 0) return '';
  return '{ ' + names.map(name => name + ': ' + __ef_reportValue(value[name])).join(', ') + ' }';
};
const __ef_reportValue = value => {
  switch (typeof value) {
    case 'string': return __ef_reportString(value);
    case 'bigint': return value.toString();
    case 'boolean': return String(value);
    case 'undefined': return 'void';
    case 'function': return '<fn>';
  }
  if (value instanceof Uint8Array) return '<bytes len=' + value.length + '>';
  if (value === null || typeof value !== 'object' || !__ef_reportPlain(value)) return '<opaque>';
  const fields = __ef_reportFields(value);
  if (typeof value._tag !== 'string') return fields === '' ? '{}' : fields;
  const variant = value._tag.slice(value._tag.lastIndexOf('.') + 1);
  return fields === '' ? variant : variant + ' ' + fields;
};
const __ef_reportReason = reason => {
  if (reason._tag === 'Interrupt') return 'interrupt';
  if (reason._tag === 'Die') {
    const defect = reason.defect;
    const message = defect instanceof Error ? defect.message : typeof defect === 'string' ? defect : __ef_reportValue(defect);
    return 'defect: ' + __ef_reportString(message);
  }
  const error = reason.error;
  if (error === null || typeof error !== 'object' || typeof error._tag !== 'string') return 'failure: ' + __ef_reportValue(error);
  const fields = __ef_reportFields(error);
  return 'failure: ' + error._tag + (fields === '' ? '' : ' ' + fields);
};
const __ef_runEntry = program => {
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
    process.stderr.write(reasons.map(reason => __ef_reportReason(reason) + '\n').join(''));
    process.exitCode = reasons.length > 0 && reasons.every(reason => reason._tag === 'Interrupt') ? 130 : 1;
  }).finally(() => {
    process.off('SIGINT', stop);
    process.off('SIGTERM', stop);
  });
};
