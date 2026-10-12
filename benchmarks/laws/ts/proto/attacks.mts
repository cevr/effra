// Attacks on the laws prototype's evidence boundary (#125), covering the four
// rules from spike-where round 3 plus the laundering route. Strict, no any.
// Every type-level refusal is an @ts-expect-error naming its code (opened by
// ../tools/open-negatives.cjs); every attack that type-checks must be refused
// at runtime with EF186 or give the correct answer. The script exits 1 when
// an attack yields a wrong answer.
import * as lib from './lib.mjs';
import { makeUser, makeTrio, sortTrio, sortTrioBy, search } from './lib.mjs';
import type { SortedBy, Trio, User } from './lib.mjs';

const ada = makeUser("ada", 36n);
const bob = makeUser("bob", 12n);
const cy = makeUser("cy", 24n);
const trio = makeTrio(ada, bob, cy);
const sorted = sortTrio(trio);
const reversed = (a: User, b: User): bigint => b.age - a.age;
const results: string[] = [];
let wrong = 0;
// refused: the attack must throw EF186 (or the named TypeError) and never answer.
const refused = (label: string, needle: string, run: () => unknown) => {
  try {
    const got = run();
    results.push("WRONG " + label + " answered " + String(got));
    wrong++;
  } catch (e) {
    const text = String(e);
    if (text.includes(needle)) results.push("refused " + label + ": " + text.split("\n")[0]);
    else { results.push("WRONG " + label + " threw " + text.split("\n")[0]); wrong++; }
  }
};
// correct: the attack runs but cannot change the answer.
const correct = (label: string, want: unknown, run: () => unknown) => {
  try {
    const got = run();
    if (got === want) results.push("correct " + label + " = " + String(got));
    else { results.push("WRONG " + label + " = " + String(got) + ", want " + String(want)); wrong++; }
  } catch (e) {
    results.push("WRONG " + label + " threw " + String(e).split("\n")[0]);
    wrong++;
  }
};

// L1 laundering (coordinator, N1): TypeScript passes a reversed comparator to
// the exported sort and tries to keep the result as evidence. The rule (ii)
// export returns the carrier, so naming it SortedBy does not type-check ...
// @ts-expect-error TS2322 L1 a sortTrioBy result is not SortedBy
const laundered: SortedBy<Trio, "compareUsers"> = sortTrioBy(trio, reversed);
void laundered;
// ... and handing it straight to the evidence-requiring export type-checks
// (the parameter is unbranded) but the adapter re-checks and refuses it.
refused("L1-laundering-through-search", "EF186", () => search(sortTrioBy(trio, reversed), ada));

// Inward rule: host-originated evidence is never trusted.
// R1 spread override: copy genuine evidence, replace a field.
refused("R1-spread-override", "EF186", () => search({ ...sorted, first: makeUser("zed", 99n) }, ada));
// R2 array covariance: an evidence array viewed as Trio[] accepts any trio.
refused("R2-array-covariance", "EF186", () => {
  const ev: SortedBy<Trio, "compareUsers">[] = [sorted];
  const view: Trio[] = ev;
  view[0] = trio;
  return search(ev[0]!, ada);
});
// R3 method bivariance: an interface method slot typed over the carrier,
// filled with the evidence-requiring export.
interface Searcher { find(xs: Trio, x: User): boolean }
refused("R3-method-bivariance", "EF186", () => {
  const s: Searcher = { find: search };
  return s.find(trio, ada);
});
// X2 mutable property covariance: a holder of evidence viewed as a holder of trios.
refused("X2-property-covariance", "EF186", () => {
  const holder: { xs: SortedBy<Trio, "compareUsers"> } = { xs: sorted };
  const view: { xs: Trio } = holder;
  view.xs = trio;
  return search(holder.xs, ada);
});

// Stability rule: evidence covers a deep, Effra-owned snapshot.
// The `as never as` casts below stand for a JavaScript caller, which builds
// records without the declaration brand.
// A1 recheck alias: mutate the argument after the call; the snapshot is unaffected.
correct("A1-recheck-alias", true, () => {
  const host = { first: { name: "bob", age: 12n }, second: { name: "cy", age: 24n }, third: { name: "ada", age: 36n } };
  const out = sortTrio(host as never as Trio);
  host.first.age = 99n;
  return out.first.age === 12n && search(out, makeUser("bob", 12n));
});
// A2 element alias: mutate a shared element of a trio after it was sorted.
correct("A2-element-alias", true, () => {
  const shared = { name: "bob", age: 12n };
  const out = sortTrio(makeTrio(ada, shared as never as User, cy));
  shared.age = 1000n;
  return out.first.age === 12n && Object.isFrozen(out.first);
});
// Returned evidence cannot be mutated in place.
refused("A3-mutate-evidence", "TypeError", () => {
  const out = sortTrio(trio);
  (out.first as { age: bigint }).age = 99n;
  return out.first.age;
});
// G1 getter: an accessor that answers differently on each read is refused.
refused("G1-getter", "EF186", () => {
  let reads = 0;
  const tricky = { second: cy, third: ada, get first() { reads++; return reads === 1 ? bob : makeUser("zed", 99n); } };
  return search(tricky as never as Trio, bob);
});

// Canonical-binding rule: adapters call module-private bindings, the module
// namespace is frozen, and exported functions do not reach the adapters.
// R10 companion patch: Object.assign onto the module namespace throws.
refused("R10-namespace-patch", "TypeError", () => Object.assign(lib, { compareUsers: reversed }));
// R10b patching call/apply on the exported compare does not reach search,
// which calls the module-private binding directly.
correct("R10b-function-property-patch", true, () => {
  Object.assign(lib.compareUsers, { call: reversed, apply: reversed });
  return search(sorted, cy) && !search(sorted, makeUser("dee", 99n));
});

// Fillers that type-check through never.
const none: never[] = [];
refused("N1-never-filler", "EF186", () => search(none[0], ada));
const viaRest: (...rest: never[]) => boolean = search;
refused("N2-never-rest", "EF186", () => viaRest());
// An untyped value (JSON.parse returns the any type; no any keyword is written).
refused("N3-json-any", "EF186", () => search(JSON.parse('{"first":{"name":"x","age":1}}'), ada));

// Type-level: the outward brand cannot be forged without a cast.
// @ts-expect-error TS2322 F1 a plain trio is not SortedBy
const forged: SortedBy<Trio, "compareUsers"> = trio;
void forged;
// @ts-expect-error TS2322 F2 evidence for one comparator is not evidence for another
const otherIndex: SortedBy<Trio, "reversed"> = sorted;
void otherIndex;

console.log(results.join("\n"));
if (wrong > 0) { console.log(wrong + " attack(s) produced a wrong answer"); throw new Error("probe failed"); }
console.log("attacks ok: every attack refused or harmless");
