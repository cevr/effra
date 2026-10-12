// Runtime for the structural (b) projection: methods live on the values.
class UserRecord {
  constructor(name, age) { this.name = name; this.age = age; Object.freeze(this); }
  compare(other) { return this.age - other.age; }
}
class UserIdRecord {
  constructor(org, n) { this.org = org; this.n = n; Object.freeze(this); }
  hash() { return this.n; }
  equals(other) { return this.n === other.n && this.org === other.org; }
}
export const User = { make: (name, age) => new UserRecord(name, age) };
export const UserId = { make: (org, n) => new UserIdRecord(org, n) };
export const three = (a, b, c) => Object.freeze({ a, b, c });
const sort2 = (x, y) => (y.compare(x) < 0n ? [y, x] : [x, y]);
export const sort3 = (xs) => {
  const [a1, b1] = sort2(xs.a, xs.b);
  const [b2, c2] = sort2(b1, xs.c);
  const [a3, b3] = sort2(a1, b2);
  return three(a3, b3, c2);
};
export const empty = () => Object.freeze({ buckets: new Map() });
export const insert = (map, key, value) => {
  const h = key.hash();
  const buckets = new Map(map.buckets);
  const bucket = (buckets.get(h) ?? []).filter(([k]) => !k.equals(key));
  buckets.set(h, [...bucket, [key, value]]);
  return Object.freeze({ buckets });
};
export const lookup = (map, key, fallback) => {
  const found = (map.buckets.get(key.hash()) ?? []).find(([k]) => k.equals(key));
  return found === undefined ? fallback : found[1];
};
export const keys = (map) => [...map.buckets.values()].flat().map(([k]) => k);
