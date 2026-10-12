// Copied from benchmarks/where/ts/tools/count-any.cjs on wf10/spike-where (769e2dde) so the laws probe runs the same pinned TypeScript.
// Counts `any` type keywords (AST AnyKeyword nodes) and \bany\b word matches per file;
// exits 1 when any file has an `any` keyword.
const ts = require(require('path').resolve(__dirname, '../../../../conformance/framework-ports/ports/node_modules/typescript'));
const fs = require('fs');
for (const f of process.argv.slice(2)) {
  const text = fs.readFileSync(f, 'utf8');
  const sf = ts.createSourceFile(f, text, ts.ScriptTarget.ES2022, true);
  let keywords = 0;
  const lines = [];
  const visit = n => { if (n.kind === ts.SyntaxKind.AnyKeyword) { keywords++; lines.push(sf.getLineAndCharacterOfPosition(n.getStart()).line + 1); } ts.forEachChild(n, visit); };
  visit(sf);
  const words = (text.match(/\bany\b/g) || []).length;
  console.log(f + ': any keywords=' + keywords + (lines.length ? ' (lines ' + lines.join(',') + ')' : '') + ', word matches=' + words);
  if (keywords > 0) process.exitCode = 1;
}
