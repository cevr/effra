// Direct misuse of the (b) witness projection is refused. Each directive is
// opened by tools/open-negatives.cjs to show the diagnostic it suppresses.
import { User, UserId, I64, three, sort3, empty, insert } from './lib.mjs';
import type { HashMap } from './lib.mjs';

const users = three(User.make("c", 30n), User.make("a", 10n), User.make("b", 20n));
// @ts-expect-error TS2554 N1 missing witness
sort3(users);
// @ts-expect-error TS2345 N2 a witness for a different type
sort3(users, UserId.hash);
// @ts-expect-error TS2345 N3 a witness of the wrong shape (equals returns bool, compare returns i64)
sort3(three(UserId.make("a", 1n), UserId.make("b", 2n), UserId.make("c", 3n)), UserId.equals);
// @ts-expect-error TS2345 N4 elements of two record types with one type's method
sort3(three<User | UserId>(User.make("a", 1n), UserId.make("b", 2n), User.make("c", 3n)), User.compare);
// @ts-expect-error TS2741 N5 a forged record (the brand is an unexported unique symbol)
const forged: User = { name: "x", age: 1n };
const byId: HashMap<UserId, string> = empty(UserId.hash, UserId.equals);
// @ts-expect-error TS2345 N6 a key of another type
insert(byId, User.make("a", 1n), "v");
// @ts-expect-error TS2345 N7 a map whose witnesses disagree on the key type
const mixed: HashMap<UserId, string> = empty(UserId.hash, I64.equals);
// @ts-expect-error TS2345 N8 a float ordering where the witness requires i64 (bigint)
sort3(three(1.5, Number.NaN, 0.5), (self: number, other: number) => self - other);
export { forged, mixed };
