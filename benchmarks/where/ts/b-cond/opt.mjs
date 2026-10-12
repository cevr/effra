// Runtime for the conditional-method projection: each conditional method is
// a function of its method arguments returning the method (note §2.9).
export const Option = {
  some: (value) => Object.freeze({ _tag: "Some", value }),
  none: () => Object.freeze({ _tag: "None" }),
  compare: (T_compare) => (self, other) =>
    self._tag === "None" ? (other._tag === "None" ? 0n : -1n) : other._tag === "None" ? 1n : T_compare(self.value, other.value),
  equals: (T_equals) => (self, other) =>
    self._tag === other._tag && (self._tag === "None" || T_equals(self.value, other.value)),
  hash: (T_hash) => (self) => (self._tag === "None" ? 0n : 31n + T_hash(self.value)),
};
export const Pair = {
  make: (a, b) => Object.freeze({ a, b }),
  compare: (A_compare, B_compare) => (self, other) => {
    const first = A_compare(self.a, other.a);
    return first !== 0n ? first : B_compare(self.b, other.b);
  },
};
