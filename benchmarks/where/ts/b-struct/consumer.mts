// TypeScript caller of the structural (b) projection. Strict, no casts, no any.
import { User, UserId, three, sort3, empty, insert, lookup } from './lib.mjs';
import type { HashMap } from './lib.mjs';

const users = sort3(three(User.make("c", 30n), User.make("a", 10n), User.make("b", 20n)));
const key = UserId.make("acme", 7n);
let byId: HashMap<UserId, string> = empty();
byId = insert(byId, UserId.make("acme", 7n), "ada");
byId = insert(byId, UserId.make("zeta", 7n), "bob");
byId = insert(byId, UserId.make("acme", 7n), "ada2");
// No way to sort numbers or by another field: bigint has no compare method,
// and a second ordering of User needs a wrapper type.
console.log(
  users.a.name + users.b.name + users.c.name,
  [lookup(byId, key, "none"), lookup(byId, UserId.make("zeta", 7n), "none"), lookup(byId, UserId.make("x", 1n), "none")].join(","),
);
