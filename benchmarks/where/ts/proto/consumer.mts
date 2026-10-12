// TypeScript caller of the .d.mts the prototype generates (proto head
// 034df57d) for sample.ef. Strict, no casts, no any. The witness parameters
// are the trailing arguments; owner methods are exported as Type_method.
import { user, userId, three, sort3, empty, insert, lookup, User_compare, UserId_hash, UserId_equals } from './out/sample.mjs';

const users = sort3(three(user("c", 30n), user("a", 10n), user("b", 20n)), User_compare);
const key = userId("acme", 7n);
let byId = empty(key, "");
byId = insert(byId, userId("acme", 7n), "ada", UserId_hash, UserId_equals);
byId = insert(byId, userId("zeta", 7n), "bob", UserId_hash, UserId_equals);
byId = insert(byId, userId("acme", 7n), "ada2", UserId_hash, UserId_equals);
console.log(
  users.a.name + users.b.name + users.c.name,
  [lookup(byId, key, "none", UserId_hash, UserId_equals), lookup(byId, userId("zeta", 7n), "none", UserId_hash, UserId_equals)].join(","),
);
