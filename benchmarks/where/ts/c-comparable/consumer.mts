// TypeScript caller of the (c) comparable projection. Strict, no casts, no any.
import { makeUser, makeUserId, three, sort3, sort3Func, empty, insert, lookup } from './lib.mjs';
import type { HashMap, User, UserId } from './lib.mjs';

const numbers = sort3(three(3n, 1n, 2n));
const words = sort3(three("b", "a", "c"));
// User is not Ordered: the caller falls back to an explicit comparison (a).
const users = sort3Func(three(makeUser("c", 30n), makeUser("a", 10n), makeUser("b", 20n)), (self: User, other: User) => self.age - other.age);

const key = makeUserId("acme", 7n);
let byId: HashMap<UserId, string> = empty<UserId, string>();
byId = insert(byId, makeUserId("acme", 7n), "ada");
byId = insert(byId, makeUserId("zeta", 7n), "bob");
byId = insert(byId, makeUserId("acme", 7n), "ada2");

// A generic TypeScript caller can forward the check only as Key<K>.

// Identity keys: what a native Map lowering would do with record keys.
const native = new Map<UserId, string>([[makeUserId("acme", 7n), "ada"]]);

console.log(
  `${numbers.a}${numbers.b}${numbers.c}`, words.a + words.b + words.c,
  users.a.name + users.b.name + users.c.name,
  [lookup(byId, key, "none"), lookup(byId, makeUserId("zeta", 7n), "none"), lookup(byId, makeUserId("x", 1n), "none")].join(","),
  lookup(insert(empty<boolean, string>(), true, "yes"), true, "no"),
  "native Map record key: " + (native.get(makeUserId("acme", 7n)) ?? "miss"),
);
