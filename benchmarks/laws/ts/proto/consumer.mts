// Positive TypeScript uses of the laws prototype's host boundary (#125).
// Strict, no casts, no any. Run by ../run.sh with tsc and node.
import { makeUser, makeTrio, sortTrio, sortTrioBy, search, oldest, sortedTrio } from './lib.mjs';
import type { SortedBy, Trio, User } from './lib.mjs';

const fail = (what: string): never => { throw new Error("consumer: " + what); };
const ada = makeUser("ada", 36n);
const bob = makeUser("bob", 12n);
const cy = makeUser("cy", 24n);
const trio = makeTrio(ada, bob, cy);

// Rule (i): the export binds compareUsers on the Effra side, so its result
// carries the evidence brand in the declarations.
const sorted: SortedBy<Trio, "compareUsers"> = sortTrio(trio);
if (sorted.first.name !== "bob" || sorted.third.name !== "ada") fail("sortTrio did not sort");

// Evidence is readable as the carrier and forgettable without a cast.
const plain: Trio = sorted;
if (oldest(plain) !== 36n) fail("oldest");

// The evidence parameter of search is the unbranded carrier (inward rule):
// a genuine evidence value and a host-sorted trio are both accepted, and the
// adapter re-establishes evidence on entry.
if (!search(sorted, cy)) fail("search on minted evidence");
if (!search(makeTrio(bob, cy, ada), ada)) fail("search on a host-sorted trio");
if (search(sorted, makeUser("dee", 99n))) fail("search found a missing user");

// An ordinary TypeScript wrapper keeps the evidence type through inference:
// wrapping loses nothing at the type level.
const sortAndLog = (t: Trio) => { const out = sortTrio(t); return out; };
const wrapped: SortedBy<Trio, "compareUsers"> = sortAndLog(trio);
if (!sortedTrio(wrapped)) fail("wrapped result is not sorted");

// Returned evidence is a deep-frozen snapshot.
if (!Object.isFrozen(sorted) || !Object.isFrozen(sorted.first)) fail("evidence result is not frozen");

// Rule (ii): a caller-supplied comparator mints nothing; the result is the carrier.
const byName = (a: User, b: User): bigint => (a.name < b.name ? -1n : a.name > b.name ? 1n : 0n);
const named: Trio = sortTrioBy(trio, byName);
if (named.first.name !== "ada") fail("sortTrioBy");

console.log("consumer ok");
