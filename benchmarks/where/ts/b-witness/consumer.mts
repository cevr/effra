// TypeScript caller of the (b) witness projection. Strict, no casts, no any.
import { User, UserId, I64, three, sort3, empty, insert, lookup } from './lib.mjs';
import type { HashMap } from './lib.mjs';

const users = sort3(three(User.make("c", 30n), User.make("a", 10n), User.make("b", 20n)), User.compare);
const numbers = sort3(three(3n, 1n, 2n), I64.compare);

// A map keyed by a user record.
const key = UserId.make("acme", 7n);
let byId: HashMap<UserId, string> = empty(UserId.hash, UserId.equals);
byId = insert(byId, UserId.make("acme", 7n), "ada");
byId = insert(byId, UserId.make("zeta", 7n), "bob");
byId = insert(byId, UserId.make("acme", 7n), "ada2");

// A caller-defined ordering is an ordinary function of the right type.
const byName = (self: User, other: User): bigint => (self.name < other.name ? -1n : self.name > other.name ? 1n : 0n);
const named = sort3(three(User.make("c", 1n), User.make("a", 3n), User.make("b", 2n)), byName);

console.log(
  users.a.name + users.b.name + users.c.name,
  `${numbers.a}${numbers.b}${numbers.c}`,
  [lookup(byId, key, "none"), lookup(byId, UserId.make("zeta", 7n), "none"), lookup(byId, UserId.make("x", 1n), "none")].join(","),
  named.a.name + named.b.name + named.c.name,
);
