// Review 2's laundering route, against the boundary-rule projection and
// against the rejected projection. Strict, no casts, no any. A directive marks
// a route the projection refuses; node still runs the line to show what the
// refusal prevents.
import { User, three, sort, sortUsers, smallest } from './lib.mjs';
import type { User_compare } from './lib.mjs';
import { sortMinting } from './rejected.mjs';

const attempt = (name: string, run: () => string) => {
  try { console.log(name + ": returned " + run()); } catch (e) { console.log(name + ": threw " + String(e).split("\n")[0]); }
};
const users = three(User.make("c", 30n), User.make("a", 10n), User.make("b", 20n));
const reversed = (self: User, other: User) => other.age - self.age;

// L1, boundary rule: the exported where-clause sort drops the evidence, so
// its result cannot reach the trusting consumer.
// @ts-expect-error L1 laundering through form (ii) is refused
attempt("L1 launder via form (ii)", () => smallest(sort(users, reversed)).name);

// L2, the rejected projection: the same route type-checks with no cast, and
// the trusting consumer returns the largest user as the smallest.
attempt("L2 launder via rejected minting form", () => smallest(sortMinting<User, User_compare>(users, reversed)).name);

// L3, the never-filler and never-rest routes against form (i) have no method
// argument to drop; the evidence is built from Effra's own method.
const none: never[] = [];
// @ts-expect-error L3 form (i) has no method parameter to fill
attempt("L3 filler into form (i)", () => smallest(sortUsers(users, none[0])).name);
