// Copied from benchmarks/where/ts/tools/open-negatives.cjs on wf10/spike-where (769e2dde) so the laws probe runs the same pinned TypeScript.
// For each @ts-expect-error in the given files, reports the diagnostics TypeScript gives
// on the guarded statement once the directive is removed (in memory; files are not changed).
// Every directive declares the codes it expects before its label:
//   // @ts-expect-error TS2345 E2 laundering ...      or      // @ts-expect-error TS2589,TS2345 N6 ...
// The distinct codes the opened statement gives must equal the declared set, so a
// directive cannot pass by firing for a different reason than the one it states.
// Exits 1 when a directive suppresses nothing, declares no code, fires with a code set
// other than the declared one, or when the files hold no directive.
// Usage: node open-negatives.cjs <dir> <file>...
const ts = require(require('path').resolve(__dirname, '../../../../conformance/framework-ports/ports/node_modules/typescript'));
const path = require('path');
const fs = require('fs');
const dir = process.argv[2];
const files = process.argv.slice(3).map(f => path.resolve(dir, f));
const opened = new Map();
const labels = [];
for (const f of files) {
  const lines = fs.readFileSync(f, 'utf8').split('\n');
  lines.forEach((line, i) => {
    const m = line.match(/^(\s*)\/\/ @ts-expect-error (.*)$/);
    if (m) {
      const c = m[2].match(/^(TS\d+(?:,TS\d+)*) (.*)$/);
      labels.push({ file: f, line: i + 1, codes: c ? c[1].split(',') : [], label: c ? c[2] : m[2] });
      lines[i] = m[1] + '// (opened) ' + m[2];
    }
  });
  opened.set(f, lines.join('\n'));
}
const options = {
  strict: true, noEmit: true, module: ts.ModuleKind.NodeNext, moduleResolution: ts.ModuleResolutionKind.NodeNext,
  target: ts.ScriptTarget.ES2022, lib: ['lib.es2022.d.ts', 'lib.dom.d.ts', 'lib.esnext.disposable.d.ts'],
};
const host = ts.createCompilerHost(options);
const read = host.readFile.bind(host);
host.readFile = f => opened.has(path.resolve(f)) ? opened.get(path.resolve(f)) : read(f);
const getSourceFile = host.getSourceFile.bind(host);
host.getSourceFile = (f, lang, onError) => opened.has(path.resolve(f))
  ? ts.createSourceFile(f, opened.get(path.resolve(f)), lang, true) : getSourceFile(f, lang, onError);
const program = ts.createProgram(files, options, host);
const diags = ts.getPreEmitDiagnostics(program);
let missing = 0;
let wrong = 0;
for (const l of labels) {
  const target = l.line + 1;
  const hits = diags.filter(d => d.file && path.resolve(d.file.fileName) === l.file).filter(d => {
    const lc = d.file.getLineAndCharacterOfPosition(d.start).line + 1;
    return lc === target || (lc > target && lc <= target + 3 && /^\s/.test(opened.get(l.file).split('\n')[lc - 1]));
  });
  if (hits.length === 0) missing++;
  const got = [...new Set(hits.map(d => 'TS' + d.code))].sort();
  const want = [...new Set(l.codes)].sort();
  const codeOk = want.length > 0 && got.join(',') === want.join(',');
  if (hits.length > 0 && !codeOk) wrong++;
  console.log(path.basename(l.file) + ':' + target + '  ' + l.label + '  [expects ' + (want.join(',') || 'no code') + (hits.length > 0 && !codeOk ? '; CODE MISMATCH, got ' + got.join(',') : '') + ']');
  for (const d of hits) {
    const related = (d.relatedInformation || []).map(r => ts.flattenDiagnosticMessageText(r.messageText, ' ')).join(' | ');
    console.log('    TS' + d.code + ' ' + ts.flattenDiagnosticMessageText(d.messageText, ' ').slice(0, 260) + (related ? '  [' + related.slice(0, 160) + ']' : ''));
  }
}
console.log('directives=' + labels.length + ' without a diagnostic=' + missing + ' with a code mismatch=' + wrong);
if (missing > 0 || wrong > 0 || labels.length === 0) process.exitCode = 1;
