// Runtime for the evidence boundary projection. Evidence is erased: a
// SortedBy value is the carried Three itself.
import { User, three, sort3 } from '../b-witness/lib.mjs';
export { User, three } from '../b-witness/lib.mjs';

const ordered = (xs, compare) => compare(xs.a, xs.b) <= 0n && compare(xs.b, xs.c) <= 0n;
export const sortUsers = (xs) => sort3(xs, User.compare);
export const checkSortedUsers = (xs) => (ordered(xs, User.compare) ? xs : undefined);
export const sort = (xs, compare) => sort3(xs, compare);
export const sortTwice = (xs, compare) => sort(sort(xs, compare), compare);
export const smallest = (xs) => xs.a;
