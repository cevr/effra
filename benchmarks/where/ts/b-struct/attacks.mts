// The four refusal bypasses (spike-rows round 4, M1) against the structural
// (b) projection. Strict, no casts, no any. The constraint is the only thing
// standing between a caller and a value without the method.
import { User, UserId, three, sort3, empty, insert, lookup, keys } from './lib.mjs';
import type { Three, HashMap } from './lib.mjs';

const attempt = (name: string, run: () => string) => {
  try { console.log(name + ": returned " + run()); } catch (e) { console.log(name + ": threw " + String(e).split("\n")[0]); }
};

// A1 method-parameter bivariance: a method slot that sorts UserIds.
interface SortLib { sortIds(xs: Three<UserId>): Three<UserId>; }
const lib: SortLib = { sortIds: sort3 };
attempt("A1 method bivariance", () => { const r = lib.sortIds(three(UserId.make("c", 3n), UserId.make("a", 1n), UserId.make("b", 2n))); return r.a.org; });

// A1b method bivariance inside the constraint: `compare(other: T)` is a method,
// so a union of two record types satisfies Comparable<User | Other> even
// though each compare accepts only its own type.
interface Other { readonly tag: "other"; readonly age: string; compare(other: Other): bigint; }
const other: Other = { tag: "other", age: "seven", compare: (o: Other) => BigInt(o.age.length) };
attempt("A1b constraint bivariance", () => { const r = sort3(three<User | Other>(User.make("a", 1n), other, User.make("c", 3n))); return typeof r.a; });

// A2 global Object augmentation: every type now satisfies Comparable and
// Hashable through its apparent type.
declare global { interface Object { compare(other: unknown): bigint; hash(): bigint; equals(other: unknown): boolean } }
attempt("A2 augmentation sort", () => sort3(three(() => 1, () => 2, () => 3)).a.toString());
attempt("A2 augmentation fn key", () => { const m: HashMap<() => void, string> = empty(); return lookup(insert(m, () => {}, "v"), () => {}, "none"); });
attempt("A2 augmentation bigint", () => String(sort3(three(3n, 1n, 2n)).a));

// A3 a never[] rest: there is no witness parameter to drop; the constraint
// is on T, so the rest form adds nothing beyond A1.
const viaRest: (xs: Three<UserId>, ...rest: never[]) => Three<UserId> = sort3;
attempt("A3 never rest", () => viaRest(three(UserId.make("c", 3n), UserId.make("a", 1n), UserId.make("b", 2n))).a.org);

// A4 a never-typed filler as an element.
const none: never[] = [];
attempt("A4 never filler", () => String(sort3(three(User.make("c", 3n), none[0], User.make("b", 2n))).a));
attempt("A4 never filler key", () => { const m: HashMap<UserId, string> = empty(); return String(keys(insert(m, none[0], "v")).length); });
