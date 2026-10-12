// Direct misuse of the (a) dictionary projection is refused.
import { makeUser, makeUserId, three, sort3, empty, insert, i64Order, userOrder, userIdHash, order, hashing } from './lib.mjs';
import type { HashMap, Order, User, UserId } from './lib.mjs';

const users = three(makeUser("c", 30n), makeUser("a", 10n), makeUser("b", 20n));
// @ts-expect-error TS2554 N1 missing dictionary
sort3(users);
// @ts-expect-error TS2345 N2 a dictionary for a different type
sort3(users, i64Order);
// @ts-expect-error TS2345 N3 a hashing dictionary where an order is required
sort3(three(makeUserId("a", 1n), makeUserId("b", 2n), makeUserId("c", 3n)), userIdHash);
// @ts-expect-error TS2345 N4 elements of two record types with one type's dictionary
sort3(three<User | UserId>(makeUser("a", 1n), makeUserId("b", 2n), makeUser("c", 3n)), userOrder);
// @ts-expect-error TS2741 N5 a forged dictionary (the template brand is unexported)
const forged: Order<User> = { compare: (self: User, other: User) => self.age - other.age };
const byId: HashMap<UserId, string> = empty(userIdHash);
// @ts-expect-error TS2345 N6 a key of another type
insert(byId, makeUser("a", 1n), "v");
// @ts-expect-error TS2345 N7 a float ordering where the dictionary requires i64 (bigint)
sort3(three(1.5, Number.NaN, 0.5), order((self: number, other: number) => self - other));
// Accepted, not refused: anyone may build a dictionary for any type,
// including a callable or a handle. The degenerate dictionary is representable.
const fnKeys = hashing((f: () => number) => BigInt(f.length), (a: () => number, b: () => number) => a === b);
export { forged, fnKeys };
