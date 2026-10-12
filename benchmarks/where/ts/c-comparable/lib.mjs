// Runtime for the (c) comparable projection. The structural key encoder is
// what a JS lowering of Go-style map keys needs; it refuses a callable at
// run time, as Go panics on an interface key holding an uncomparable value.
export const makeUser = (name, age) => Object.freeze({ name, age });
export const makeUserId = (org, n) => Object.freeze({ org, n });
export const openFile = (path) => Object.freeze({ path, fd: Symbol(path) });
export const three = (a, b, c) => Object.freeze({ a, b, c });
const sort2 = (x, y, lt) => (lt(y, x) ? [y, x] : [x, y]);
const network = (xs, lt) => {
  const [a1, b1] = sort2(xs.a, xs.b, lt);
  const [b2, c2] = sort2(b1, xs.c, lt);
  const [a3, b3] = sort2(a1, b2, lt);
  return three(a3, b3, c2);
};
export const sort3 = (xs) => network(xs, (x, y) => x < y);
export const sort3Func = (xs, compare) => network(xs, (x, y) => compare(x, y) < 0n);
const encode = (k) => {
  switch (typeof k) {
    case "string": return "s" + JSON.stringify(k);
    case "bigint": return "i" + k;
    case "number": return "f" + k;
    case "boolean": return "b" + k;
    case "object":
      if (k !== null) return "{" + Object.keys(k).sort().map((p) => JSON.stringify(p) + ":" + encode(k[p])).join(",") + "}";
  }
  throw new TypeError("runtime error: hash of unhashable type " + typeof k);
};
export const empty = () => Object.freeze({ entries: new Map() });
export const insert = (map, key, value) => {
  const entries = new Map(map.entries);
  entries.set(encode(key), [key, value]);
  return Object.freeze({ entries });
};
export const lookup = (map, key, fallback) => {
  const found = map.entries.get(encode(key));
  return found === undefined ? fallback : found[1];
};
export const keys = (map) => [...map.entries.values()].map(([k]) => k);
