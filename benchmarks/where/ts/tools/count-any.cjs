// Counts `any` type keywords (AST AnyKeyword nodes) and \bany\b word matches per file.
const ts = require('/home/exedev/Developer/personal/effect-oxlint/node_modules/typescript');
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
}
