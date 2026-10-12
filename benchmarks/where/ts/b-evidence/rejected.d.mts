// The projection the round-2 sketch implied, kept as the counterexample the
// boundary rule exists to forbid: an exported where-clause function that takes
// its method from the caller and still returns the evidence.
import type { Three } from '../b-witness/lib.mjs';
import type { SortedBy } from './lib.mjs';

export declare const sortMinting: <T, By>(xs: Three<T>, compare: (self: T, other: T) => bigint) => SortedBy<Three<T>, By>;
