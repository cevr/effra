// Corpus metrics for the #134 syntax spike (docs/research/primitives.md).
//
// Usage: bun benchmarks/syntax/measure.ts [--chosen P] [--json]
//
// Reads docs/research/primitives/corpus/<option>/*.ef for the baseline
// `today` and each option, and prints per option and per file:
//   tokens        lexical tokens after comments are removed
//   depth         maximum brace nesting depth
//   keywords      distinct keywords used
//   punct/line    punctuation tokens per non-blank code line
//   lookalikes    compiler-known constructs spelled like a method or
//                 function call (must be 0 for the chosen option)
//
// `library.ef` (the option's own library source) is reported separately and
// is not part of the totals, because user programs do not contain it.
//
// Exits non-zero when a file cannot be lexed, braces do not balance, an
// option is missing a corpus file, or the chosen option has a lookalike.

import { readdirSync, readFileSync, existsSync } from "node:fs";
import { join, dirname } from "node:path";

const root = join(dirname(new URL(import.meta.url).pathname), "..", "..");
const corpus = join(root, "docs", "research", "primitives", "corpus");
const baseline = "today";
const options = ["P", "A", "B", "C", "D", "E"];

const args = process.argv.slice(2);
const chosenIndex = args.indexOf("--chosen");
const chosen = chosenIndex >= 0 ? args[chosenIndex + 1] : "P";
const asJson = args.includes("--json");

type Token = { kind: "word" | "number" | "string" | "punct"; text: string; line: number };

// Keywords of today's grammar (contextual in the parser, but reserved in
// practice) and the keywords each option adds. A word is counted as a keyword
// only when it is not a member name (`.provide`) and, for `provide`, not a
// called function (`provide(App)` in option D).
const baseKeywords = new Set([
  "import", "pub", "error", "record", "struct", "enum", "service", "impl", "for",
  "layer", "provides", "derive", "effect", "fn", "let", "run", "fail", "scope",
  "fork", "if", "else", "match", "uses", "raises", "host", "as", "true", "false",
  "void", "type", "with",
]);
const optionKeywords: Record<string, string[]> = {
  today: [],
  P: [],
  A: ["provide", "within", "otherwise"],
  B: ["provide", "within", "otherwise"],
  C: ["provide", "race", "or"],
  D: [],
  E: ["provide", "race", "after"],
};

// Constructs that are compiler-known but read like a method or call.
// today: every dot form the parser special-cases or the checker treats as a
//   construct (syntax.go postfix forms; fork handles' join/interrupt).
// A, B, E: `.timeout` and `.provide` no longer exist; any left is a miss.
// P: every dot form is an ordinary library function over the kernel
//   (`.fork`, `.scoped`, `.timeout*`, `.provide`); nothing is name-keyed.
// C: `.provide` no longer exists; `.timeout*` are ordinary library methods.
// D: provision stays compiler-known while spelled as calls (`.provide`,
//   `provide(L)`, `provideService<S>(p)`): the lang-item pattern.
const dotConstructs: Record<string, string[]> = {
  today: ["timeout", "provide", "catch", "recover", "orFail", "join", "interrupt", "cancel"],
  P: [],
  A: ["timeout", "provide"],
  B: ["timeout", "provide"],
  C: ["provide"],
  D: ["provide"],
  E: ["timeout", "provide"],
};
const callConstructs: Record<string, string[]> = {
  today: [],
  P: [],
  A: [],
  B: [],
  C: [],
  D: ["provide", "provideService"],
  E: [],
};
// Dot forms that are compiler-known today and become ordinary library only
// when the rows units land (#131 catch, #133 recover, fiber methods with a
// declared contract). Reported, not failed, so the dependency stays visible.
const pendingLibraryBase = ["catch", "recover", "orFail", "join", "interrupt", "cancel"];
// Library that additionally waits on receiver methods (#127), whole-value type
// parameters (#128) and row difference (F3b) before it can be written in .ef.
const pendingLibraryExtra: Record<string, string[]> = {
  P: ["fork", "scoped", "timeout", "timeoutOr", "provide"],
  C: ["timeout", "timeoutOr"],
  D: ["timeout", "timeoutOr"],
};
const pendingFor = (option: string) => [...pendingLibraryBase, ...(pendingLibraryExtra[option] ?? [])];

const multiPunct = ["->", "=>", "==", "!=", "<=", ">=", "&&", "||", "::", "..."];

function lex(source: string, file: string): Token[] {
  const tokens: Token[] = [];
  let i = 0;
  let line = 1;
  while (i < source.length) {
    const c = source[i];
    if (c === "\n") {
      line++;
      i++;
      continue;
    }
    if (c === " " || c === "\t" || c === "\r") {
      i++;
      continue;
    }
    if (c === "/" && source[i + 1] === "/") {
      while (i < source.length && source[i] !== "\n") i++;
      continue;
    }
    if (c === "/" && source[i + 1] === "*") {
      const end = source.indexOf("*/", i + 2);
      if (end < 0) throw new Error(`${file}:${line}: unterminated block comment`);
      for (let k = i; k < end; k++) if (source[k] === "\n") line++;
      i = end + 2;
      continue;
    }
    if (c === '"') {
      let j = i + 1;
      while (j < source.length && source[j] !== '"') {
        if (source[j] === "\\") j++;
        if (source[j] === "\n") throw new Error(`${file}:${line}: unterminated string`);
        j++;
      }
      if (j >= source.length) throw new Error(`${file}:${line}: unterminated string`);
      tokens.push({ kind: "string", text: source.slice(i, j + 1), line });
      i = j + 1;
      continue;
    }
    if (/[0-9]/.test(c)) {
      // Numbers, including duration literals such as 500ms or 2s.
      let j = i;
      while (j < source.length && /[0-9_.]/.test(source[j])) j++;
      while (j < source.length && /[a-z]/.test(source[j])) j++;
      tokens.push({ kind: "number", text: source.slice(i, j), line });
      i = j;
      continue;
    }
    if (/[A-Za-z_]/.test(c)) {
      let j = i;
      while (j < source.length && /[A-Za-z0-9_]/.test(source[j])) j++;
      tokens.push({ kind: "word", text: source.slice(i, j), line });
      i = j;
      continue;
    }
    const two = multiPunct.find((p) => source.startsWith(p, i));
    if (two) {
      tokens.push({ kind: "punct", text: two, line });
      i += two.length;
      continue;
    }
    if ("{}()[]<>.,:;=+-*/%!|&?@#$^~\\'`".includes(c)) {
      tokens.push({ kind: "punct", text: c, line });
      i++;
      continue;
    }
    throw new Error(`${file}:${line}: unexpected character ${JSON.stringify(c)}`);
  }
  return tokens;
}

type Metrics = {
  file: string;
  tokens: number;
  lines: number;
  depth: number;
  keywords: string[];
  punctPerLine: number;
  punct: number;
  lookalikes: string[];
  pending: number;
};

function measure(option: string, file: string): Metrics {
  const path = join(corpus, option, file);
  const source = readFileSync(path, "utf8");
  const tokens = lex(source, `${option}/${file}`);
  const keywordSet = new Set([...baseKeywords, ...optionKeywords[option]]);
  const codeLines = new Set(tokens.map((t) => t.line));

  let depth = 0;
  let maxDepth = 0;
  const pairs: Record<string, string> = { "}": "{", ")": "(", "]": "[" };
  const stack: { text: string; line: number }[] = [];
  const keywords = new Set<string>();
  const lookalikes: string[] = [];
  let pending = 0;
  let punct = 0;

  tokens.forEach((t, index) => {
    const previous = tokens[index - 1];
    const next = tokens[index + 1];
    if (t.kind === "punct") {
      punct++;
      if (t.text === "{" || t.text === "(" || t.text === "[") {
        stack.push(t);
        if (t.text === "{") maxDepth = Math.max(maxDepth, ++depth);
      } else if (t.text in pairs) {
        const open = stack.pop();
        if (!open || open.text !== pairs[t.text]) {
          throw new Error(`${option}/${file}:${t.line}: unbalanced ${t.text}`);
        }
        if (t.text === "}") depth--;
      }
      return;
    }
    if (t.kind !== "word") return;
    const member = previous?.kind === "punct" && previous.text === ".";
    const called = next?.kind === "punct" && (next.text === "(" || next.text === "<");
    if (member && called && dotConstructs[option].includes(t.text)) {
      lookalikes.push(`${t.line}:.${t.text}`);
    } else if (!member && called && callConstructs[option].includes(t.text)) {
      lookalikes.push(`${t.line}:${t.text}()`);
    }
    if (member && called && option !== baseline && pendingFor(option).includes(t.text)) pending++;
    if (member || !keywordSet.has(t.text)) return;
    if (t.text === "provide" && called) return;
    if ((t.text === "or" || t.text === "after") && !(previous?.text === "}")) return;
    keywords.add(t.text);
  });
  if (stack.length > 0) {
    const open = stack[stack.length - 1];
    throw new Error(`${option}/${file}:${open.line}: unclosed ${open.text}`);
  }
  return {
    file,
    tokens: tokens.length,
    lines: codeLines.size,
    depth: maxDepth,
    keywords: [...keywords].sort(),
    punctPerLine: codeLines.size === 0 ? 0 : punct / codeLines.size,
    punct,
    lookalikes,
    pending,
  };
}

const failures: string[] = [];
const baselineFiles = readdirSync(join(corpus, baseline))
  .filter((f) => f.endsWith(".ef"))
  .sort();

type Report = { option: string; files: Metrics[]; library?: Metrics };
const reports: Report[] = [];

for (const option of [baseline, ...options]) {
  const dir = join(corpus, option);
  if (!existsSync(dir)) {
    failures.push(`missing option directory ${option}`);
    continue;
  }
  const present = new Set(readdirSync(dir).filter((f) => f.endsWith(".ef")));
  const report: Report = { option, files: [] };
  for (const file of baselineFiles) {
    if (!present.has(file)) {
      failures.push(`${option}: missing corpus file ${file}`);
      continue;
    }
    try {
      report.files.push(measure(option, file));
    } catch (error) {
      failures.push((error as Error).message);
    }
  }
  for (const file of present) {
    if (file === "library.ef") {
      try {
        report.library = measure(option, file);
      } catch (error) {
        failures.push((error as Error).message);
      }
    } else if (!baselineFiles.includes(file)) {
      failures.push(`${option}: ${file} has no baseline in ${baseline}/`);
    }
  }
  reports.push(report);
}

function totals(report: Report) {
  const tokens = report.files.reduce((sum, m) => sum + m.tokens, 0);
  const lines = report.files.reduce((sum, m) => sum + m.lines, 0);
  const punct = report.files.reduce((sum, m) => sum + m.punct, 0);
  const depth = Math.max(0, ...report.files.map((m) => m.depth));
  const keywords = new Set(report.files.flatMap((m) => m.keywords));
  const lookalikes = report.files.reduce((sum, m) => sum + m.lookalikes.length, 0);
  const pending = report.files.reduce((sum, m) => sum + m.pending, 0);
  return { tokens, lines, punct, depth, keywords: [...keywords].sort(), lookalikes, pending };
}

const chosenReport = reports.find((r) => r.option === chosen);
if (!chosenReport) {
  failures.push(`chosen option ${chosen} not measured`);
} else {
  for (const m of chosenReport.files) {
    for (const site of m.lookalikes) {
      failures.push(`chosen option ${chosen}: ${m.file}:${site} is a construct spelled like a call`);
    }
  }
}

if (asJson) {
  console.log(JSON.stringify({ chosen, reports: reports.map((r) => ({ ...r, totals: totals(r) })), failures }, null, 2));
} else {
  const fixed = (n: number) => n.toFixed(2);
  console.log(`# Syntax corpus metrics (chosen option: ${chosen})\n`);
  console.log("## Totals over the shared corpus (library.ef excluded)\n");
  console.log("| option | files | tokens | vs today | code lines | max depth | distinct keywords | punct/line | lookalikes | pending-library dot forms |");
  console.log("|---|---|---|---|---|---|---|---|---|---|");
  const base = reports.find((r) => r.option === baseline);
  const baseTokens = base ? totals(base).tokens : 0;
  for (const report of reports) {
    const t = totals(report);
    const delta = baseTokens === 0 ? "" : `${t.tokens - baseTokens >= 0 ? "+" : ""}${(((t.tokens - baseTokens) / baseTokens) * 100).toFixed(1)}%`;
    console.log(`| ${report.option} | ${report.files.length} | ${t.tokens} | ${delta} | ${t.lines} | ${t.depth} | ${t.keywords.length} | ${fixed(t.lines ? t.punct / t.lines : 0)} | ${t.lookalikes} | ${report.option === baseline ? "n/a" : t.pending} |`);
  }
  console.log("\n## Keywords per option\n");
  for (const report of reports) console.log(`- ${report.option}: ${totals(report).keywords.join(", ")}`);
  console.log("\n## Per file\n");
  console.log(`| file | ${reports.map((r) => `${r.option} tok/depth/kw/punct/look`).join(" | ")} |`);
  console.log(`|---|${reports.map(() => "---").join("|")}|`);
  for (const file of baselineFiles) {
    const cells = reports.map((r) => {
      const m = r.files.find((x) => x.file === file);
      return m ? `${m.tokens}/${m.depth}/${m.keywords.length}/${fixed(m.punctPerLine)}/${m.lookalikes.length}` : "missing";
    });
    console.log(`| ${file} | ${cells.join(" | ")} |`);
  }
  console.log("\n## Option library source (not in totals)\n");
  for (const report of reports) {
    if (!report.library) continue;
    const m = report.library;
    console.log(`- ${report.option}/library.ef: ${m.tokens} tokens, depth ${m.depth}, ${m.keywords.length} keywords, ${fixed(m.punctPerLine)} punct/line, ${m.lookalikes.length} lookalikes`);
  }
  const listed = reports.filter((r) => r.option !== baseline && r.files.some((m) => m.lookalikes.length > 0));
  if (listed.length > 0) {
    console.log("\n## Lookalike sites in options\n");
    for (const report of listed) {
      const sites = report.files.flatMap((m) => m.lookalikes.map((s) => `${m.file}:${s}`));
      console.log(`- ${report.option}: ${sites.length} (${sites.slice(0, 8).join(", ")}${sites.length > 8 ? ", ..." : ""})`);
    }
  }
}

if (failures.length > 0) {
  console.error(`\nFAIL (${failures.length})`);
  for (const failure of failures) console.error(`- ${failure}`);
  process.exit(1);
}
console.error("\nOK");
