// Corpus metrics for the #134 syntax spike (docs/research/primitives.md).
//
// Usage: bun benchmarks/syntax/measure.ts [--chosen H] [--json]
//
// Reads docs/research/primitives/corpus/<option>/*.ef for the baseline
// `today` and each option, and prints per option and per file:
//   tokens        lexical tokens after comments are removed
//   depth         maximum brace nesting depth
//   keywords      distinct keywords used
//   punct/line    punctuation tokens per non-blank code line
//   lookalikes    dot or call forms whose meaning has no general carrier, so
//                 the checker must recognise them by name: constructs in
//                 dot form (must be 0 for the chosen option)
//   awaiting      dot or call forms that become ordinary library only when a
//                 general feature lands, split by that feature
//   keyed today   dot forms that today's checker recognises by name
//
// Every option is measured against ONE classification (`forms` below), so no
// option can define its own lookalikes away. `library.ef` (the option's own
// library source) is reported separately and is not part of the totals.
//
// Exits non-zero when a file cannot be lexed, braces do not balance, an
// option is missing a corpus file, the chosen option has a lookalike, or an
// option that requires owned forks has a `fork` outside an owner block.

import { readdirSync, readFileSync, existsSync } from "node:fs";
import { join, dirname } from "node:path";

const root = join(dirname(new URL(import.meta.url).pathname), "..", "..");
const corpus = join(root, "docs", "research", "primitives", "corpus");
const baseline = "today";
const options = ["H", "P", "A", "B", "C", "D", "E"];

const args = process.argv.slice(2);
const chosenIndex = args.indexOf("--chosen");
const chosen = chosenIndex >= 0 ? args[chosenIndex + 1] : "H";
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
  // Main at ec4a5a9e reserves the machine declaration and its decision words
  // (internal/compiler/decisions.go). No file of the shared corpus uses them;
  // the corpus-main extension does (machine-breaker.ef).
  "machine", "decision", "enter", "stay", "ignore", "reject", "done",
]);
const optionKeywords: Record<string, string[]> = {
  today: [],
  H: ["provide", "race", "interrupt"],
  P: [],
  A: ["provide", "within", "otherwise"],
  B: ["provide", "within", "otherwise"],
  C: ["provide", "race", "or"],
  D: [],
  E: ["provide", "race", "after"],
};

// Options whose design requires every `fork` to sit inside an owner block in
// the same function (review 1, M6): `scope { }`, a `race { }` arm, or a
// `provide … { }` body, since every provide block owns its body (review 3,
// Minor 2). A `fork` entry in a `layer` is outside every owner block, so the
// split-out worker entry (U-workers) fails this check too.
const ownedFork = new Set(["H"]);

// The one classification of dot and call forms. `carrier` says what lets the
// form be an ordinary function:
//   none      no general type feature carries its checking; the checker must
//             recognise it by name, so in dot form it is a construct wearing
//             method syntax (review 1: B1 for fiber handles, M4 for a layer
//             argument, whose plan no row type carries).
//   q11       library once receiver methods (#127), whole-value type
//             parameters (#128 Q9) and the closed-at-the-edge recipe-parameter
//             contract (#128 Q11, not yet shown: 0 of 29) land, as ordinary
//             Ergo over a construct or as a declared-contract primitive.
//   rows      library once the rows units land (#126 F3b, #131 catch,
//             #132 provide, #133 recover), using the shown in-place half of
//             the recipe-parameter contract.
// `keyedToday` marks names that today's checker recognises (syntax.go postfix
// forms; the fiber methods in semantic.go). The script checks it against the
// compiler source (review 2, o3), so the column cannot drift.
// `surface` is "134" for the concurrency, lifetime, time and provision forms
// this ticket decides, and "host" for forms outside it (review 2, m5). Only a
// #134 lookalike fails the chosen option; the others are reported apart.
type Carrier = "none" | "q11" | "rows";
type Surface = "134" | "host";
type Form = { name: string; dot: boolean; next?: "(" | "<"; carrier: Carrier; keyedToday: boolean; surface?: Surface };
const forms: Form[] = [
  { name: "fork", dot: true, carrier: "none", keyedToday: false },
  { name: "join", dot: true, carrier: "none", keyedToday: true },
  { name: "interrupt", dot: true, carrier: "none", keyedToday: true },
  { name: "cancel", dot: true, carrier: "none", keyedToday: true },
  { name: "provide", dot: true, next: "(", carrier: "none", keyedToday: true },
  { name: "provide", dot: false, next: "(", carrier: "none", keyedToday: false },
  { name: "race", dot: true, carrier: "q11", keyedToday: false },
  { name: "scoped", dot: true, carrier: "q11", keyedToday: false },
  { name: "scoped", dot: false, carrier: "q11", keyedToday: false },
  { name: "timeout", dot: true, carrier: "q11", keyedToday: true },
  { name: "timeout", dot: false, carrier: "q11", keyedToday: false },
  { name: "timeoutOr", dot: true, carrier: "q11", keyedToday: false },
  { name: "timeoutOr", dot: false, carrier: "q11", keyedToday: false },
  { name: "timeoutOption", dot: true, carrier: "q11", keyedToday: false },
  { name: "timeoutTo", dot: true, carrier: "q11", keyedToday: false },
  { name: "provide", dot: true, next: "<", carrier: "rows", keyedToday: true },
  { name: "provideService", dot: true, carrier: "rows", keyedToday: false },
  { name: "provideService", dot: false, carrier: "rows", keyedToday: false },
  { name: "catch", dot: true, carrier: "rows", keyedToday: true },
  { name: "recover", dot: true, carrier: "rows", keyedToday: true },
  { name: "orFail", dot: true, carrier: "rows", keyedToday: true },
  // value.as<T>(): Go's v, ok := value.(T) for a native host value
  // (syntax.go hostAssert). No general feature carries it, so it is
  // compiler-known, but it is host interop, outside #134's surface.
  { name: "as", dot: true, next: "<", carrier: "none", keyedToday: true, surface: "host" },
];

function classify(name: string, dot: boolean, next: string): Form | undefined {
  return forms.find((f) => f.name === name && f.dot === dot && (f.next === undefined || f.next === next));
}

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
  otherKeyed: string[];
  awaitingQ11: string[];
  awaitingRows: string[];
  keyedToday: number;
  ownerless: string[];
};

function measure(option: string, file: string, base: string = corpus): Metrics {
  const path = join(base, option, file);
  const source = readFileSync(path, "utf8");
  const tokens = lex(source, `${option}/${file}`);
  const keywordSet = new Set([...baseKeywords, ...optionKeywords[option]]);
  const codeLines = new Set(tokens.map((t) => t.line));

  let depth = 0;
  let maxDepth = 0;
  const pairs: Record<string, string> = { "}": "{", ")": "(", "]": "[" };
  const stack: { text: string; line: number; owner: boolean }[] = [];
  const keywords = new Set<string>();
  const lookalikes: string[] = [];
  const otherKeyed: string[] = [];
  const awaitingQ11: string[] = [];
  const awaitingRows: string[] = [];
  const ownerless: string[] = [];
  let keyedToday = 0;
  let punct = 0;
  // A provide header (`provide App, S = p`) runs until its block opens at the
  // header's own nesting depth (a brace inside a provider's parentheses is
  // not the block).
  let provideHeader = false;
  let provideDepth = 0;

  tokens.forEach((t, index) => {
    const previous = tokens[index - 1];
    const next = tokens[index + 1];
    if (t.kind === "punct") {
      punct++;
      if (t.text === "{" || t.text === "(" || t.text === "[") {
        // An owner block is a `{` opened directly by `scope` or `race`, or
        // the block that ends a provide header.
        const opener = previous?.kind === "word" && (previous.text === "scope" || previous.text === "race");
        const block = provideHeader && stack.length === provideDepth;
        const owner = t.text === "{" && (opener || block);
        if (t.text === "{" && block) provideHeader = false;
        stack.push({ text: t.text, line: t.line, owner });
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
    if (called) {
      const form = classify(t.text, member, next.text);
      const site = `${t.line}:${member ? "." : ""}${t.text}${next.text}`;
      if (form) {
        if (form.carrier === "none" && form.surface === "host") otherKeyed.push(site);
        else if (form.carrier === "none") lookalikes.push(site);
        else if (form.carrier === "q11") awaitingQ11.push(site);
        else awaitingRows.push(site);
        if (form.keyedToday) keyedToday++;
      }
    }
    if (!member && t.text === "fork" && ownedFork.has(option) && !stack.some((s) => s.owner)) {
      ownerless.push(`${t.line}`);
    }
    if (!member && !called && t.text === "provide" && optionKeywords[option]?.includes("provide")) {
      provideHeader = true;
      provideDepth = stack.length;
    }
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
    otherKeyed,
    awaitingQ11,
    awaitingRows,
    keyedToday,
    ownerless,
  };
}

const failures: string[] = [];

// Review 2, o3: `keyedToday` must match the names today's compiler keys on:
// the postfix method names in syntax.go and the fiber operations in
// semantic.go's fiberCall.
function compilerKeyedNames(): Set<string> {
  const compiler = join(root, "internal", "compiler");
  const names = new Set<string>();
  const syntax = readFileSync(join(compiler, "syntax.go"), "utf8");
  for (const m of syntax.matchAll(/method\.text == "([A-Za-z]+)"/g)) names.add(m[1]);
  const semantic = readFileSync(join(compiler, "semantic.go"), "utf8");
  const start = semantic.indexOf("func (c *checker) fiberCall(");
  const end = semantic.indexOf("\nfunc ", start + 1);
  if (start < 0 || end < 0) failures.push("keyedToday: fiberCall not found in semantic.go");
  else for (const m of semantic.slice(start, end).matchAll(/case "([A-Za-z]+)"/g)) names.add(m[1]);
  return names;
}
{
  const keyed = compilerKeyedNames();
  const marked = new Set(forms.filter((f) => f.keyedToday).map((f) => f.name));
  for (const name of keyed) if (!marked.has(name)) failures.push(`keyedToday: the compiler keys .${name} but no form marks it`);
  for (const name of marked) if (!keyed.has(name)) failures.push(`keyedToday: .${name} is marked but the compiler does not key it`);
}
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
  const sum = (f: (m: Metrics) => number) => report.files.reduce((s, m) => s + f(m), 0);
  return {
    tokens: sum((m) => m.tokens),
    lines: sum((m) => m.lines),
    punct: sum((m) => m.punct),
    depth: Math.max(0, ...report.files.map((m) => m.depth)),
    keywords: [...new Set(report.files.flatMap((m) => m.keywords))].sort(),
    lookalikes: sum((m) => m.lookalikes.length),
    otherKeyed: sum((m) => m.otherKeyed.length),
    awaitingQ11: sum((m) => m.awaitingQ11.length),
    awaitingRows: sum((m) => m.awaitingRows.length),
    keyedToday: sum((m) => m.keyedToday),
  };
}

for (const report of reports) {
  for (const m of [...report.files, ...(report.library ? [report.library] : [])]) {
    for (const line of m.ownerless) {
      failures.push(`${report.option}: ${m.file}:${line}: fork outside an owner block (scope or race)`);
    }
  }
}
const chosenReport = reports.find((r) => r.option === chosen);
if (!chosenReport) {
  failures.push(`chosen option ${chosen} not measured`);
} else {
  for (const m of [...chosenReport.files, ...(chosenReport.library ? [chosenReport.library] : [])]) {
    for (const site of m.lookalikes) {
      failures.push(`chosen option ${chosen}: ${m.file}:${site} is a construct spelled like a call`);
    }
  }
}

// The corpus-main extension: examples that main added after the shared
// corpus was frozen (main at ec4a5a9e: machine decisions and scheduler
// quiescence; main at fb762763: latch handoff), rewritten for the baseline and the chosen option only. It is
// reported apart so the shared comparison stays like for like, and the
// chosen option's checks (no lookalike, every fork owned) apply to it.
const extension = join(root, "docs", "research", "primitives", "corpus-main");
const extensionReports: Report[] = [];
if (existsSync(extension)) {
  const extensionFiles = readdirSync(join(extension, baseline)).filter((f) => f.endsWith(".ef")).sort();
  for (const option of [baseline, chosen]) {
    const report: Report = { option, files: [] };
    for (const file of extensionFiles) {
      if (!existsSync(join(extension, option, file))) {
        failures.push(`corpus-main/${option}: missing ${file}`);
        continue;
      }
      try {
        const m = measure(option, file, extension);
        report.files.push(m);
        for (const line of m.ownerless) failures.push(`corpus-main/${option}: ${file}:${line}: fork outside an owner block`);
        if (option === chosen) for (const site of m.lookalikes) failures.push(`corpus-main/${chosen}: ${file}:${site} is a construct spelled like a call`);
      } catch (error) {
        failures.push((error as Error).message);
      }
    }
    extensionReports.push(report);
  }
}

if (asJson) {
  console.log(JSON.stringify({ chosen, forms, reports: reports.map((r) => ({ ...r, totals: totals(r) })), extension: extensionReports.map((r) => ({ ...r, totals: totals(r) })), failures }, null, 2));
} else {
  const fixed = (n: number) => n.toFixed(2);
  console.log(`# Syntax corpus metrics (chosen option: ${chosen})\n`);
  console.log("## Totals over the shared corpus (library.ef excluded)\n");
  console.log("| option | files | tokens | vs today | code lines | max depth | distinct keywords | punct/line | lookalikes (#134) | other compiler-known | awaiting Q11 | awaiting rows units | keyed today |");
  console.log("|---|---|---|---|---|---|---|---|---|---|---|---|---|");
  const base = reports.find((r) => r.option === baseline);
  const baseTokens = base ? totals(base).tokens : 0;
  for (const report of reports) {
    const t = totals(report);
    const delta = baseTokens === 0 ? "" : `${t.tokens - baseTokens >= 0 ? "+" : ""}${(((t.tokens - baseTokens) / baseTokens) * 100).toFixed(1)}%`;
    console.log(`| ${report.option} | ${report.files.length} | ${t.tokens} | ${delta} | ${t.lines} | ${t.depth} | ${t.keywords.length} | ${fixed(t.lines ? t.punct / t.lines : 0)} | ${t.lookalikes} | ${t.otherKeyed} | ${t.awaitingQ11} | ${t.awaitingRows} | ${t.keyedToday} |`);
  }
  if (extensionReports.length > 0) {
    console.log("\n## corpus-main extension (examples main added after the corpus froze; baseline and chosen option only)\n");
    console.log("| option | files | tokens | vs today | code lines | max depth | distinct keywords | punct/line | lookalikes (#134) | awaiting Q11 | awaiting rows units | keyed today |");
    console.log("|---|---|---|---|---|---|---|---|---|---|---|---|");
    const extensionBase = totals(extensionReports[0]).tokens;
    for (const report of extensionReports) {
      const t = totals(report);
      const delta = extensionBase === 0 ? "" : `${t.tokens - extensionBase >= 0 ? "+" : ""}${(((t.tokens - extensionBase) / extensionBase) * 100).toFixed(1)}%`;
      console.log(`| ${report.option} | ${report.files.length} | ${t.tokens} | ${delta} | ${t.lines} | ${t.depth} | ${t.keywords.length} | ${fixed(t.lines ? t.punct / t.lines : 0)} | ${t.lookalikes} | ${t.awaitingQ11} | ${t.awaitingRows} | ${t.keyedToday} |`);
    }
  }
  console.log("\n## Classification (one table for every option)\n");
  console.log("| form | carrier | surface | keyed today |");
  console.log("|---|---|---|---|");
  for (const f of forms) console.log(`| ${f.dot ? "." : ""}${f.name}${f.next ?? ""} | ${f.carrier} | ${f.surface ?? "134"} | ${f.keyedToday ? "yes" : "no"} |`);
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
    console.log(`- ${report.option}/library.ef: ${m.tokens} tokens, depth ${m.depth}, ${m.keywords.length} keywords, ${fixed(m.punctPerLine)} punct/line, ${m.lookalikes.length} lookalikes, ${m.awaitingQ11.length} awaiting Q11, ${m.awaitingRows.length} awaiting rows units`);
  }
  console.log("\n## Lookalike sites per option\n");
  for (const report of reports) {
    const sites = report.files.flatMap((m) => m.lookalikes.map((s) => `${m.file}:${s}`));
    const kinds = new Map<string, number>();
    for (const s of sites) {
      const kind = s.slice(s.lastIndexOf(":") + 1);
      kinds.set(kind, (kinds.get(kind) ?? 0) + 1);
    }
    const summary = [...kinds.entries()].map(([k, n]) => `${k} ${n}`).join(", ");
    console.log(`- ${report.option}: ${sites.length}${sites.length > 0 ? ` (${summary})` : ""}`);
  }
}

if (failures.length > 0) {
  console.error(`\nFAIL (${failures.length})`);
  for (const failure of failures) console.error(`- ${failure}`);
  process.exit(1);
}
console.error("\nOK");
