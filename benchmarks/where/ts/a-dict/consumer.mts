// TypeScript caller of the (a) dictionary projection. Strict, no casts, no any.
import { makeUser, makeUserId, three, sort3, empty, insert, lookup, i64Order, stringOrder, userOrder, userIdHash, mapInput, reverse } from './lib.mjs';
import type { HashMap } from './lib.mjs';

const users = sort3(three(makeUser("c", 30n), makeUser("a", 10n), makeUser("b", 20n)), userOrder);
const numbers = sort3(three(3n, 1n, 2n), i64Order);

const key = makeUserId("acme", 7n);
let byId: HashMap<typeof key, string> = empty(userIdHash);
byId = insert(byId, makeUserId("acme", 7n), "ada");
byId = insert(byId, makeUserId("zeta", 7n), "bob");
byId = insert(byId, makeUserId("acme", 7n), "ada2");

// Dictionaries are values, so combinators build new orderings.
const byNameDesc = reverse(mapInput(stringOrder, (u: ReturnType<typeof makeUser>) => u.name));
const named = sort3(three(makeUser("c", 1n), makeUser("a", 3n), makeUser("b", 2n)), byNameDesc);

console.log(
  users.a.name + users.b.name + users.c.name,
  `${numbers.a}${numbers.b}${numbers.c}`,
  [lookup(byId, key, "none"), lookup(byId, makeUserId("zeta", 7n), "none"), lookup(byId, makeUserId("x", 1n), "none")].join(","),
  named.a.name + named.b.name + named.c.name,
);
