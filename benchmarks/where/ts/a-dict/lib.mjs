// Runtime for the (a) dictionary projection.
export const makeUser = (name, age) => Object.freeze({ name, age });
export const makeUserId = (org, n) => Object.freeze({ org, n });
export const order = (compare) => Object.freeze({ compare });
export const hashing = (hash, equals) => Object.freeze({ hash, equals });
export const mapInput = (o, f) => order((self, other) => o.compare(f(self), f(other)));
export const reverse = (o) => order((self, other) => o.compare(other, self));
export const i64Order = order((self, other) => self - other);
export const stringOrder = order((self, other) => (self < other ? -1n : self > other ? 1n : 0n));
export const userOrder = mapInput(i64Order, (u) => u.age);
export const userIdHash = hashing((id) => id.n, (a, b) => a.n === b.n && a.org === b.org);
export const three = (a, b, c) => Object.freeze({ a, b, c });
const sort2 = (x, y, o) => (o.compare(y, x) < 0n ? [y, x] : [x, y]);
export const sort3 = (xs, o) => {
  const [a1, b1] = sort2(xs.a, xs.b, o);
  const [b2, c2] = sort2(b1, xs.c, o);
  const [a3, b3] = sort2(a1, b2, o);
  return three(a3, b3, c2);
};
export const empty = (h) => Object.freeze({ h, buckets: new Map() });
export const insert = (map, key, value) => {
  const k = map.h.hash(key);
  const buckets = new Map(map.buckets);
  const bucket = (buckets.get(k) ?? []).filter(([old]) => !map.h.equals(old, key));
  buckets.set(k, [...bucket, [key, value]]);
  return Object.freeze({ h: map.h, buckets });
};
export const lookup = (map, key, fallback) => {
  const found = (map.buckets.get(map.h.hash(key)) ?? []).find(([k]) => map.h.equals(k, key));
  return found === undefined ? fallback : found[1];
};
export const keys = (map) => [...map.buckets.values()].flat().map(([k]) => k);
