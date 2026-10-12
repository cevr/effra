// Conditional methods (note §2.9), projected onto companion objects. Adapted
// from the review-1 probe. A receiver method on a generic type whose own
// where clause constrains the type's parameters,
//
//   fn (o: Option<T>) compare(other: Option<T>) -> i64 where T.compare: fn(T, T) -> i64
//
// lowers to a function of its method arguments that returns the method, so
// the resolved value has exactly the signature a `where` slot asks for. Method
// arguments come first, one per where entry in declaration order, named
// `<Param>_<method>`. Nothing about T is narrowed.
declare const __ef_brand_template_Option: unique symbol;
export type Option<T> = {
  readonly [__ef_brand_template_Option]: readonly [(value: T) => T];
  readonly _tag: "Some" | "None";
  readonly value?: T;
};
export declare const Option: {
  readonly some: <T>(value: T) => Option<T>;
  readonly none: <T>() => Option<T>;
  readonly compare: <T>(T_compare: (self: T, other: T) => bigint) => (self: Option<T>, other: Option<T>) => bigint;
  readonly equals: <T>(T_equals: (self: T, other: T) => boolean) => (self: Option<T>, other: Option<T>) => boolean;
  readonly hash: <T>(T_hash: (self: T) => bigint) => (self: Option<T>) => bigint;
};

// A generic record with derived methods (note §2.7): one method argument per
// constrained parameter.
declare const __ef_brand_template_Pair: unique symbol;
export type Pair<A, B> = {
  readonly [__ef_brand_template_Pair]: readonly [(value: A) => A, (value: B) => B];
  readonly a: A;
  readonly b: B;
};
export declare const Pair: {
  readonly make: <A, B>(a: A, b: B) => Pair<A, B>;
  readonly compare: <A, B>(A_compare: (self: A, other: A) => bigint, B_compare: (self: B, other: B) => bigint) => (self: Pair<A, B>, other: Pair<A, B>) => bigint;
};
