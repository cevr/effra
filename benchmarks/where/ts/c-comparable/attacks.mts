// The four refusal bypasses (spike-rows round 4, M1) against the (c)
// comparable projection. Strict, no casts, no any. Key<K> is computed from
// the instantiated K, so most slot assignments are still checked; what gets
// through reaches the runtime key encoder, which must refuse again. A
// directive marks an attack the projection refuses; node still runs the line.
import { makeUserId, openFile, three, sort3, empty, insert, lookup, keys } from './lib.mjs';
import type { Three, HashMap, UserId } from './lib.mjs';

const attempt = (name: string, run: () => string) => {
  try { console.log(name + ": returned " + run()); } catch (e) { console.log(name + ": threw " + String(e).split("\n")[0]); }
};
const ids = (): HashMap<UserId, string> => empty<UserId, string>();

// A1 method-parameter bivariance.
// A1a a factory slot for a function-keyed map: refused, because Key<K> is in
// the instantiated return type.
interface MakeLib { make(): HashMap<() => void, string>; }
// @ts-expect-error A1a function-keyed factory slot
const maker: MakeLib = { make: empty };
attempt("A1a fn-keyed factory", () => String(keys(maker.make()).length));
// A1b a put slot whose key parameter is wider than the map's key: K comes from
// the map alone, so the key parameter is compared bivariantly and passes.
interface PutLib { put(map: HashMap<UserId, string>, key: UserId | (() => void), value: string): HashMap<UserId, string>; }
const putter: PutLib = { put: insert };
attempt("A1b key bivariance fn key", () => String(keys(putter.put(ids(), () => {}, "v")).length));
attempt("A1b key bivariance file key", () => { const p: { put(map: HashMap<UserId, string>, key: UserId | ReturnType<typeof openFile>, value: string): HashMap<UserId, string> } = { put: insert }; return String(keys(p.put(ids(), openFile("a"), "v")).length); });
// A1c a sort slot for a record: refused (T extends Ordered is checked).
interface SortLib { sortIds(xs: Three<UserId>): Three<UserId>; }
// @ts-expect-error A1c record sort slot
const sorter: SortLib = { sortIds: sort3 };
attempt("A1c record sort slot", () => sorter.sortIds(three(makeUserId("c", 3n), makeUserId("a", 1n), makeUserId("b", 2n))).a.org);

// A3 a never[] rest drops the key parameter.
const viaRest: (map: HashMap<UserId, string>, ...rest: never[]) => HashMap<UserId, string> = insert;
attempt("A3 never rest", () => String(keys(viaRest(ids())).length));

// A4 a never-typed filler as the key.
const none: never[] = [];
attempt("A4 never filler key", () => lookup(insert(ids(), none[0], "v"), makeUserId("a", 1n), "none"));

// Not a bypass: NaN is an Ordered f64, admitted by the projection.
attempt("NaN ordering", () => { const r = sort3(three(2, Number.NaN, 1)); return [r.a, r.b, r.c].join(","); });
attempt("NaN key", () => lookup(insert(empty<number, string>(), Number.NaN, "v"), Number.NaN, "none") + " (Go: a NaN key is never found)");
