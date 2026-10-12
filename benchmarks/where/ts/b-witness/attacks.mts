// The four refusal bypasses (spike-rows round 4, M1) against the (b) witness
// projection. Strict, no casts, no any. The run shows, for each attack that
// type-checks, whether the call fails before producing a value or a value with
// a false static type escapes. A directive marks an attack the projection
// refuses; node still runs the line to show what refusal prevents.
import { User, UserId, three, sort3, empty, insert, lookup, keys } from './lib.mjs';
import type { Three, HashMap } from './lib.mjs';

const attempt = (name: string, run: () => string) => {
  try { console.log(name + ": returned " + run()); } catch (e) { console.log(name + ": threw " + String(e).split("\n")[0]); }
};
const mixed = three<User | UserId>(User.make("a", 1n), UserId.make("b", 2n), User.make("c", 3n));

// A1 method-parameter bivariance on the witness: refused, because the
// template brand makes Three<T> invariant, so neither direction of the
// bivariant parameter check holds.
interface SortLib { sortMixed(xs: Three<User | UserId>, compare: (self: User, other: User) => bigint): Three<User | UserId>; }
// @ts-expect-error A1 bivariant witness slot (Three<T> is invariant)
const lib: SortLib = { sortMixed: sort3 };
attempt("A1 method bivariance", () => { const r = lib.sortMixed(mixed, User.compare); return [r.a, r.b, r.c].map(v => "name" in v ? v.name : v.org).join(""); });

// A1b method-parameter bivariance on the key: type-checks. K is fixed by an
// instantiation expression, and the key parameter alone is compared
// bivariantly. This is TypeScript's container hole (baseline-effect shows the
// same on a native Map); no where constraint or witness is involved. With
// `sort3<User>` the A1 slot is still refused: Three<T> stays invariant.
interface MapLib { put(map: HashMap<UserId, string>, key: UserId | User, value: string): HashMap<UserId, string>; }
const mapLib: MapLib = { put: insert<UserId, string> };
attempt("A1b key bivariance", () => { const m = mapLib.put(empty(UserId.hash, UserId.equals), User.make("ada", 1n), "v"); const k: UserId = keys(m)[0]; return "UserId.org is " + typeof k.org; });

// A2 global Object augmentation: there is no structural constraint for it to
// satisfy. The witness parameter is still required and still typed.
declare global { interface Object { compare(other: unknown): bigint } }
// @ts-expect-error A2 augmentation does not supply a witness argument
attempt("A2 augmentation", () => sort3(three(() => 1, () => 2, () => 3)).a.toString());

// A3 a never[] rest drops the witness parameter.
const viaRest: (xs: Three<User>, ...rest: never[]) => Three<User> = sort3;
attempt("A3 never rest", () => viaRest(three(User.make("c", 3n), User.make("a", 1n), User.make("b", 2n))).a.name);

// A4 a never-typed filler in the witness position.
const none: never[] = [];
attempt("A4 never filler", () => sort3(three(User.make("c", 3n), User.make("a", 1n), User.make("b", 2n)), none[0]).a.name);
attempt("A4 never filler map", () => { const m: HashMap<UserId, string> = empty(none[0], none[0]); return lookup(insert(m, UserId.make("a", 1n), "v"), UserId.make("a", 1n), "none"); });

// A5 not a bypass, a TypeScript rule: a function with fewer parameters is
// assignable, so a hash (one argument) type-checks as a compare witness. It
// is type-honest (the result is a Three<UserId>), only meaningless. Effra
// resolves by method name and never makes this choice.
attempt("A5 shorter witness", () => { const r = sort3(three(UserId.make("c", 3n), UserId.make("a", 1n), UserId.make("b", 2n)), UserId.hash); return r.a.org + r.b.org + r.c.org; });
