// The four bypass routes against the conditional-method projection. Strict,
// no casts, no any. As for b-witness: nothing is narrowed, so a bypass can
// only drop or falsify a method argument, and the composed method fails on
// first use.
import { User, three, sort3 } from '../b-witness/lib.mjs';
import type { Three } from '../b-witness/lib.mjs';
import { Option } from './opt.mjs';

const attempt = (name: string, run: () => string) => {
  try { console.log(name + ": returned " + run()); } catch (e) { console.log(name + ": threw " + String(e).split("\n")[0]); }
};
const xs = three(Option.some(User.make("c", 30n)), Option.some(User.make("b", 20n)), Option.some(User.make("a", 10n)));
const first = (r: Three<Option<User>>) => (r.a._tag === "Some" && r.a.value ? r.a.value.name : "none");

// A1 is in consumer.mts (refused: Three<T> is invariant).

// A2 global Object augmentation supplies no method argument.
declare global { interface Object { compare(other: unknown): bigint } }
// @ts-expect-error TS2554 A2 augmentation does not supply the inner method argument
attempt("A2 augmentation", () => first(sort3(xs, Option.compare())));

// A3 a never[] rest drops the inner method argument.
const viaRest: (...rest: never[]) => (self: Option<User>, other: Option<User>) => bigint = Option.compare;
attempt("A3 never rest", () => first(sort3(xs, viaRest())));

// A4 a never-typed filler as the inner method argument.
const none: never[] = [];
attempt("A4 never filler", () => first(sort3(xs, Option.compare(none[0]))));
