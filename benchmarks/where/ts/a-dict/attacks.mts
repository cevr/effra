// The four refusal bypasses (spike-rows round 4, M1) against the (a)
// dictionary projection. Strict, no casts, no any. A directive marks an attack
// the projection refuses; node still runs the line to show what refusal
// prevents.
import { makeUser, makeUserId, three, sort3, empty, insert, lookup, keys, userOrder, userIdHash } from './lib.mjs';
import type { Three, HashMap, Order, User, UserId } from './lib.mjs';

const attempt = (name: string, run: () => string) => {
  try { console.log(name + ": returned " + run()); } catch (e) { console.log(name + ": threw " + String(e).split("\n")[0]); }
};
const mixed = three<User | UserId>(makeUser("a", 1n), makeUserId("b", 2n), makeUser("c", 3n));

// A1 method-parameter bivariance on the dictionary: refused (Three<T> and
// Order<T> are invariant through their template brands).
interface SortLib { sortMixed(xs: Three<User | UserId>, order: Order<User>): Three<User | UserId>; }
// @ts-expect-error A1 bivariant dictionary slot (templates are invariant)
const lib: SortLib = { sortMixed: sort3 };
attempt("A1 method bivariance", () => { const r = lib.sortMixed(mixed, userOrder); return [r.a, r.b, r.c].map(v => "name" in v ? v.name : v.org).join(""); });

// A1b method-parameter bivariance on the key (TypeScript's container hole).
interface MapLib { put(map: HashMap<UserId, string>, key: UserId | User, value: string): HashMap<UserId, string>; }
const mapLib: MapLib = { put: insert<UserId, string> };
attempt("A1b key bivariance", () => { const m = mapLib.put(empty(userIdHash), makeUser("ada", 1n), "v"); const k: UserId = keys(m)[0]; return "UserId.org is " + typeof k.org; });

// A2 global Object augmentation: no structural constraint to satisfy.
declare global { interface Object { compare(other: unknown): bigint } }
// @ts-expect-error A2 augmentation does not supply a dictionary
attempt("A2 augmentation", () => sort3(three(() => 1, () => 2, () => 3)).a.toString());

// A3 a never[] rest drops the dictionary parameter.
const viaRest: (xs: Three<User>, ...rest: never[]) => Three<User> = sort3;
attempt("A3 never rest", () => viaRest(three(makeUser("c", 3n), makeUser("a", 1n), makeUser("b", 2n))).a.name);

// A4 a never-typed filler in the dictionary position.
const none: never[] = [];
attempt("A4 never filler", () => sort3(three(makeUser("c", 3n), makeUser("a", 1n), makeUser("b", 2n)), none[0]).a.name);
attempt("A4 never filler map", () => { const m: HashMap<UserId, string> = empty(none[0]); return lookup(insert(m, makeUserId("a", 1n), "v"), makeUserId("a", 1n), "none"); });
