// Option (c): Go-style structural comparable and Ordered. `comparable` is a
// predeclared constraint satisfied by primitives and by records and enums
// whose fields are all comparable; `Ordered` is i64, f64 and string. The
// projection carries both to the type level. TypeScript refuses
// `K extends Comparable<K>` as a circular constraint, so the check is a
// recursive conditional type applied where K is used: Key<K> is K when every
// field is comparable and never otherwise. A failed check leaves no usable
// key type; there is no wider type to fall back to.
declare const __ef_brand_User: unique symbol;
export interface User { readonly [__ef_brand_User]: "User"; readonly name: string; readonly age: bigint; }
declare const __ef_brand_UserId: unique symbol;
export interface UserId { readonly [__ef_brand_UserId]: "UserId"; readonly org: string; readonly n: bigint; }
export interface File { readonly _effraFile: "File" }
export declare const makeUser: (name: string, age: bigint) => User;
export declare const makeUserId: (org: string, n: bigint) => UserId;
export declare const openFile: (path: string) => File;

type Primitive = string | bigint | boolean | number;
export type IsComparable<K> =
  K extends Primitive ? true
  : K extends (...args: never[]) => unknown ? false
  : K extends File ? false
  : K extends object ? (false extends { [P in keyof K]-?: IsComparable<K[P]> }[keyof K] ? false : true)
  : false;
export type Key<K> = [IsComparable<K>] extends [true] ? K : never;
export type Ordered = bigint | number | string;

declare const __ef_brand_template_Three: unique symbol;
export type Three<T> = { readonly [__ef_brand_template_Three]: readonly [(value: T) => T]; readonly a: T; readonly b: T; readonly c: T; };
export declare const three: <T>(a: T, b: T, c: T) => Three<T>;
// fn sort3<T: Ordered>(xs: Three<T>) -> Three<T>   (cmp.Ordered, operator <)
export declare const sort3: <T extends Ordered>(xs: Three<T>) => Three<T>;
// User records are not Ordered: they need slices.SortFunc, which is option (a).
export declare const sort3Func: <T>(xs: Three<T>, compare: (self: T, other: T) => bigint) => Three<T>;

// map[K]V with K comparable. On JS the key is encoded structurally, because a
// native Map compares object keys by identity.
declare const __ef_brand_template_HashMap: unique symbol;
export type HashMap<K, V> = { readonly [__ef_brand_template_HashMap]: readonly [(value: K) => K, (value: V) => V]; };
export declare const empty: <K, V>() => HashMap<Key<K>, V>;
export declare const insert: <K, V>(map: HashMap<K, V>, key: Key<K>, value: V) => HashMap<K, V>;
export declare const lookup: <K, V>(map: HashMap<K, V>, key: Key<K>, fallback: V) => V;
export declare const keys: <K, V>(map: HashMap<K, V>) => readonly K[];
