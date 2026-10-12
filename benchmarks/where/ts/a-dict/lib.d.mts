// Option (a): explicit dictionary values (Effect-style Order / Equivalence +
// Hash), with derive producing the dictionary values. In Effra a dictionary is
// an ordinary record of functions, `record Order<T> { compare: fn(T, T) -> i64 }`,
// so it projects as a branded template record. Callers pass dictionaries
// explicitly; combinators build new ones.
declare const __ef_brand_User: unique symbol;
export interface User { readonly [__ef_brand_User]: "User"; readonly name: string; readonly age: bigint; }
declare const __ef_brand_UserId: unique symbol;
export interface UserId { readonly [__ef_brand_UserId]: "UserId"; readonly org: string; readonly n: bigint; }
export declare const makeUser: (name: string, age: bigint) => User;
export declare const makeUserId: (org: string, n: bigint) => UserId;

declare const __ef_brand_template_Order: unique symbol;
export type Order<T> = { readonly [__ef_brand_template_Order]: readonly [(value: T) => T]; readonly compare: (self: T, other: T) => bigint; };
declare const __ef_brand_template_Hash: unique symbol;
export type Hash<T> = { readonly [__ef_brand_template_Hash]: readonly [(value: T) => T]; readonly hash: (self: T) => bigint; readonly equals: (self: T, other: T) => boolean; };

// Dictionary constructors and combinators (a library, not compiler-known).
export declare const order: <T>(compare: (self: T, other: T) => bigint) => Order<T>;
export declare const hashing: <T>(hash: (self: T) => bigint, equals: (self: T, other: T) => boolean) => Hash<T>;
export declare const mapInput: <A, B>(order: Order<A>, f: (value: B) => A) => Order<B>;
export declare const reverse: <T>(order: Order<T>) => Order<T>;

// Derived or hand-written dictionary values.
export declare const i64Order: Order<bigint>;
export declare const stringOrder: Order<string>;
export declare const userOrder: Order<User>;
export declare const userIdHash: Hash<UserId>;

declare const __ef_brand_template_Three: unique symbol;
export type Three<T> = { readonly [__ef_brand_template_Three]: readonly [(value: T) => T]; readonly a: T; readonly b: T; readonly c: T; };
export declare const three: <T>(a: T, b: T, c: T) => Three<T>;

// fn sort3<T: type>(xs: Three<T>, order: Order<T>) -> Three<T>
export declare const sort3: <T>(xs: Three<T>, order: Order<T>) => Three<T>;

declare const __ef_brand_template_HashMap: unique symbol;
export type HashMap<K, V> = { readonly [__ef_brand_template_HashMap]: readonly [(value: K) => K, (value: V) => V]; };
export declare const empty: <K, V>(hash: Hash<K>) => HashMap<K, V>;
export declare const insert: <K, V>(map: HashMap<K, V>, key: K, value: V) => HashMap<K, V>;
export declare const lookup: <K, V>(map: HashMap<K, V>, key: K, fallback: V) => V;
export declare const keys: <K, V>(map: HashMap<K, V>) => readonly K[];
