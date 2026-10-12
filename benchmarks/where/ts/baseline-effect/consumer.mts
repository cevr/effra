// Baseline: Effect 4.0.1, the runtime Effra's JavaScript target emits. Effect
// is option (a) for ordering (Order values). Equality and hashing are a
// run-time mix of (c) and a protocol: Equal.equals compares primitives,
// arrays and plain objects structurally, defers to Equal.symbol / Hash.symbol
// methods when a value has them, and compares functions by reference. Nothing
// is refused at compile time. Strict, no casts, no any.
import { Array as Arr, Order, HashMap, Data, Option } from 'effect';

interface User { readonly name: string; readonly age: bigint }
const users: ReadonlyArray<User> = [{ name: "c", age: 30n }, { name: "a", age: 10n }, { name: "b", age: 20n }];
const byAge = Order.mapInput(Order.BigInt, (u: User) => u.age);
const sorted = Arr.sort(users, byAge).map(u => u.name).join("");

// A plain record key compares structurally (Effect 4 default), so it hits.
interface UserId { readonly org: string; readonly n: bigint }
const plain = HashMap.set(HashMap.empty<UserId, string>(), { org: "acme", n: 7n }, "ada");
const plainHit = Option.getOrElse(HashMap.get(plain, { org: "acme", n: 7n }), () => "miss");
// Data.Class installs Equal and Hash on the value, so the key hits.
class UserIdKey extends Data.Class<{ readonly org: string; readonly n: bigint }> {}
const structural = HashMap.set(HashMap.empty<UserIdKey, string>(), new UserIdKey({ org: "acme", n: 7n }), "ada");
const structuralHit = Option.getOrElse(HashMap.get(structural, new UserIdKey({ org: "acme", n: 7n })), () => "miss");

// Nothing refuses a function key or a handle key: both type-check.
const fnKey = (): number => 1;
const fnKeyed = HashMap.set(HashMap.empty<() => number, string>(), fnKey, "f");
const fnHit = Option.getOrElse(HashMap.get(fnKeyed, fnKey), () => "miss");

// NaN under Order.Number: Effect orders NaN before every number (a total order
// chosen by the library); nothing refuses it.
const floats = Arr.sort([2, Number.NaN, 1], Order.Number).join(",");

// TypeScript's container hole (A1b) on a native Map, without any library.
interface MapLib { put(map: Map<UserId, string>, key: UserId | User, value: string): void; }
const setId = (map: Map<UserId, string>, key: UserId, value: string): void => { map.set(key, value); };
const lib: MapLib = { put: setId };
const native = new Map<UserId, string>();
lib.put(native, { name: "ada", age: 1n }, "v");
const firstKey: UserId | undefined = [...native.keys()][0];

console.log(
  sorted, "plain:" + plainHit, "Data.Class:" + structuralHit, "fn key:" + fnHit, "NaN:" + floats,
  "native Map A1b: UserId.org is " + typeof firstKey?.org,
);
