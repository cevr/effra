// A2 against the (c) comparable projection, in its own file because the
// augmentation is global. Augmenting Object with the File brand makes every
// object a File, so a valid record key is refused: the check fails closed
// (over-refusal), never open. Augmentation cannot make a callable stop
// matching `(...args: never[]) => unknown`.
import { makeUserId, empty, insert, lookup } from './lib.mjs';
import type { HashMap, UserId } from './lib.mjs';

declare global { interface Object { readonly _effraFile: "File" } }
const attempt = (name: string, run: () => string) => {
  try { console.log(name + ": returned " + run()); } catch (e) { console.log(name + ": threw " + String(e).split("\n")[0]); }
};
attempt("A2 augmentation record key", () => {
  // @ts-expect-error A2 every record now looks like a File handle
  const m: HashMap<UserId, string> = empty<UserId, string>();
  // @ts-expect-error A2 the key type collapsed to never
  return lookup(insert(m, makeUserId("a", 1n), "v"), makeUserId("a", 1n), "none");
});
// @ts-expect-error A2 a callable key is still refused under the augmentation
attempt("A2 callable key", () => String(insert(empty<() => void, string>(), () => {}, "v")));
