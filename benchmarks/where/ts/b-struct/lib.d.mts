// Option (b), structural projection (rejected): the where clause projected as
// a TypeScript constraint on T, and the runtime dispatching through a method
// on the value (records become classes with prototype methods). This is what
// "Roc-style abilities in TypeScript" would look like if the constraint were
// carried to the type level. It is refuse-only: the constraint is the whole
// safety argument, and there is no wider type to fall back to, because the
// function cannot run without the method.
declare const __ef_brand_User: unique symbol;
export interface User { readonly [__ef_brand_User]: "User"; readonly name: string; readonly age: bigint; compare(other: User): bigint; }
export declare const User: { readonly make: (name: string, age: bigint) => User };
declare const __ef_brand_UserId: unique symbol;
export interface UserId { readonly [__ef_brand_UserId]: "UserId"; readonly org: string; readonly n: bigint; hash(): bigint; equals(other: UserId): boolean; }
export declare const UserId: { readonly make: (org: string, n: bigint) => UserId };

// where T.compare: fn(T, T) -> i64
export interface Comparable<T> { compare(other: T): bigint; }
// where K.hash: fn(K) -> i64, K.equals: fn(K, K) -> bool
export interface Hashable<K> { hash(): bigint; equals(other: K): boolean; }

declare const __ef_brand_template_Three: unique symbol;
export type Three<T> = { readonly [__ef_brand_template_Three]: readonly [(value: T) => T]; readonly a: T; readonly b: T; readonly c: T; };
export declare const three: <T>(a: T, b: T, c: T) => Three<T>;
export declare const sort3: <T extends Comparable<T>>(xs: Three<T>) => Three<T>;

declare const __ef_brand_template_HashMap: unique symbol;
export type HashMap<K, V> = { readonly [__ef_brand_template_HashMap]: readonly [(value: K) => K, (value: V) => V]; };
export declare const empty: <K extends Hashable<K>, V>() => HashMap<K, V>;
export declare const insert: <K extends Hashable<K>, V>(map: HashMap<K, V>, key: K, value: V) => HashMap<K, V>;
export declare const lookup: <K extends Hashable<K>, V>(map: HashMap<K, V>, key: K, fallback: V) => V;
export declare const keys: <K, V>(map: HashMap<K, V>) => readonly K[];
