// Direct misuse of the structural (b) projection is refused.
import { User, UserId, three, sort3, empty, insert } from './lib.mjs';
import type { HashMap } from './lib.mjs';

// @ts-expect-error TS2345 N1 a type without the method
sort3(three(UserId.make("a", 1n), UserId.make("b", 2n), UserId.make("c", 3n)));
// @ts-expect-error TS2345 N2 a predeclared type (bigint has no compare method)
sort3(three(3n, 1n, 2n));
// @ts-expect-error TS2345 N3 a callable element
sort3(three(() => 1, () => 2, () => 3));
// @ts-expect-error TS2322 N4 a function-keyed map
const fnKeys: HashMap<() => void, string> = empty();
const byId: HashMap<UserId, string> = empty();
// @ts-expect-error TS2345 N5 a key of another type
insert(byId, User.make("a", 1n), "v");
export { fnKeys };
