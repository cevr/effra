// A TypeScript caller of conditional methods (note §2.6). The caller composes
// what Effra's checker resolves: Option<User>.compare is
// Option.compare(User.compare). Adapted from the review-1 probe.
import { User, UserId, I64, three, sort3, empty, insert, lookup } from '../b-witness/lib.mjs';
import type { Three } from '../b-witness/lib.mjs';
import { Option, Pair } from './opt.mjs';

const name = (o: Option<User>) => (o._tag === "Some" && o.value ? o.value.name : "none");

// Option<User>
const xs = three(Option.some(User.make("c", 30n)), Option.none<User>(), Option.some(User.make("a", 10n)));
const sorted = sort3(xs, Option.compare(User.compare));
// Option<Option<User>>: resolution recurses once per type constructor.
const nested = three(Option.some(Option.some(User.make("b", 20n))), Option.some(Option.none<User>()), Option.none<Option<User>>());
const sortedNested = sort3(nested, Option.compare(Option.compare(User.compare)));
// Pair<i64, User>: one method argument per constrained parameter.
const pairs = three(Pair.make(2n, User.make("x", 1n)), Pair.make(1n, User.make("z", 3n)), Pair.make(1n, User.make("y", 2n)));
const sortedPairs = sort3(pairs, Pair.compare(I64.compare, User.compare));
// A map keyed by Option<UserId>: the composed methods are captured at construction.
let byId = empty<Option<UserId>, string>(Option.hash(UserId.hash), Option.equals(UserId.equals));
byId = insert(byId, Option.some(UserId.make("acme", 7n)), "ada");
byId = insert(byId, Option.none<UserId>(), "nobody");
console.log([
  [sorted.a, sorted.b, sorted.c].map(name).join(","),
  [sortedNested.a, sortedNested.b, sortedNested.c].map(o => (o._tag === "Some" && o.value ? name(o.value) : "none")).join(","),
  [sortedPairs.a, sortedPairs.b, sortedPairs.c].map(p => p.b.name).join(","),
  lookup(byId, Option.some(UserId.make("acme", 7n)), "miss") + "," + lookup(byId, Option.none<UserId>(), "miss"),
].join(" "));

// @ts-expect-error C1 the inner method of another type
sort3(xs, Option.compare(UserId.hash));
// @ts-expect-error C2 the outer method without its method argument
sort3(xs, Option.compare);
// @ts-expect-error C3 Option<User> compare on Option<UserId>
sort3(three(Option.some(UserId.make("a", 1n)), Option.none<UserId>(), Option.none<UserId>()), Option.compare(User.compare));
// @ts-expect-error C4 one level too few for Option<Option<User>>
sort3(nested, Option.compare(User.compare));
// @ts-expect-error C5 Pair's method arguments swapped
sort3(pairs, Pair.compare(User.compare, I64.compare));
// @ts-expect-error C6 a compare where the map needs a hash
empty<Option<UserId>, string>(Option.compare(UserId.equals), Option.equals(UserId.equals));

// A1 method bivariance on the conditional slot: refused, Three<T> is invariant.
interface L { s(xs: Three<Option<User | UserId>>, c: (a: Option<User>, b: Option<User>) => bigint): Three<Option<User | UserId>> }
// @ts-expect-error A1 invariant Three<T>
const l: L = { s: sort3 };
export { l };
