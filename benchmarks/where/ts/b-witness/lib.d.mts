// Option (b), proposed projection: where-clause dispatch lowered to witness
// parameters. A `where T.compare: fn(T, T) -> i64` constraint becomes one
// trailing parameter named after the method. The declarations claim nothing
// about T at the type level, so no TypeScript bypass can make them claim a
// fact the runtime does not hold. A type's owner-module methods are exported
// as a companion object under the type's name (a value merged with the
// interface), so a TypeScript caller writes `sort3(xs, User.compare)`.
declare const __ef_brand_User: unique symbol;
export interface User { readonly [__ef_brand_User]: "User"; readonly name: string; readonly age: bigint; }
export declare const User: {
  readonly make: (name: string, age: bigint) => User;
  readonly compare: (self: User, other: User) => bigint;
};

declare const __ef_brand_UserId: unique symbol;
export interface UserId { readonly [__ef_brand_UserId]: "UserId"; readonly org: string; readonly n: bigint; }
export declare const UserId: {
  readonly make: (org: string, n: bigint) => UserId;
  readonly hash: (self: UserId) => bigint;
  readonly equals: (self: UserId, other: UserId) => boolean;
};

// Predeclared types: methods come from their bundled owner module.
export declare const I64: {
  readonly compare: (self: bigint, other: bigint) => bigint;
  readonly hash: (self: bigint) => bigint;
  readonly equals: (self: bigint, other: bigint) => boolean;
};

declare const __ef_brand_template_Three: unique symbol;
export type Three<T> = { readonly [__ef_brand_template_Three]: readonly [(value: T) => T]; readonly a: T; readonly b: T; readonly c: T; };
export declare const three: <T>(a: T, b: T, c: T) => Three<T>;

// fn sort3<T: type>(xs: Three<T>) -> Three<T> where T.compare: fn(T, T) -> i64
export declare const sort3: <T>(xs: Three<T>, compare: (self: T, other: T) => bigint) => Three<T>;

// fn empty<K: type, V: type>() -> HashMap<K, V> where K.hash: fn(K) -> i64, K.equals: fn(K, K) -> bool
// The map captures its witnesses at construction, so later operations carry
// no where clause and a TypeScript caller cannot mix two hash functions in
// one map.
declare const __ef_brand_template_HashMap: unique symbol;
export type HashMap<K, V> = { readonly [__ef_brand_template_HashMap]: readonly [(value: K) => K, (value: V) => V]; };
export declare const empty: <K, V>(hash: (self: K) => bigint, equals: (self: K, other: K) => boolean) => HashMap<K, V>;
export declare const insert: <K, V>(map: HashMap<K, V>, key: K, value: V) => HashMap<K, V>;
export declare const lookup: <K, V>(map: HashMap<K, V>, key: K, fallback: V) => V;
export declare const keys: <K, V>(map: HashMap<K, V>) => readonly K[];
