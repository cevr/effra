// Runtime for the (b) witness projection. Same shape the prototype emits:
// the witness is an ordinary trailing argument.
export const User = {
  make: (name, age) => Object.freeze({ name, age }),
  compare: (self, other) => self.age - other.age,
};
export const UserId = {
  make: (org, n) => Object.freeze({ org, n }),
  hash: (self) => self.n,
  equals: (self, other) => self.n === other.n && self.org === other.org,
};
export const I64 = {
  compare: (self, other) => self - other,
  hash: (self) => self,
  equals: (self, other) => self === other,
};
export const three = (a, b, c) => Object.freeze({ a, b, c });
const sort2 = (x, y, compare) => (compare(y, x) < 0n ? [y, x] : [x, y]);
export const sort3 = (xs, compare) => {
  const [a1, b1] = sort2(xs.a, xs.b, compare);
  const [b2, c2] = sort2(b1, xs.c, compare);
  const [a3, b3] = sort2(a1, b2, compare);
  return three(a3, b3, c2);
};
export const empty = (hash, equals) => Object.freeze({ hash, equals, buckets: new Map() });
export const insert = (map, key, value) => {
  const h = map.hash(key);
  const buckets = new Map(map.buckets);
  const bucket = (buckets.get(h) ?? []).filter(([k]) => !map.equals(k, key));
  buckets.set(h, [...bucket, [key, value]]);
  return Object.freeze({ hash: map.hash, equals: map.equals, buckets });
};
export const lookup = (map, key, fallback) => {
  const found = (map.buckets.get(map.hash(key)) ?? []).find(([k]) => map.equals(k, key));
  return found === undefined ? fallback : found[1];
};
export const keys = (map) => [...map.buckets.values()].flat().map(([k]) => k);
