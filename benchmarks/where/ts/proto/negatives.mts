// Misuse against the prototype's generated .d.mts is refused.
import { user, userId, three, sort3, empty, insert, User_compare, UserId_equals } from './out/sample.mjs';

const users = three(user("c", 30n), user("a", 10n), user("b", 20n));
// @ts-expect-error P1 missing witness
sort3(users);
// @ts-expect-error P2 another type's method as the witness
sort3(users, UserId_equals);
const byId = empty(userId("acme", 7n), "");
// @ts-expect-error P3 the compare method of another type as the hash witness
insert(byId, userId("acme", 7n), "ada", User_compare, UserId_equals);
