# 2026-10-28: Ergo with and without Go's syntax

Labels: **Checked**, **Proposed**. This preview supersedes no earlier preview. It extends [the Go-shaped language](2026-10-27-go-sketch.md) of 2026-10-27.

The owner asked, on 2026-10-28: "what could ergo look like if it didn't need to necessarily be constrained by the syntax of go but instead keep the philosophy", then "how could we simplify it further ?", then "yes write the preview and drop untyped constants". Later that day the owner decided most of the simplifications: "ok lets go with all your recos minus the one row for errors/services - i like the simpler types as well". A second round followed: "go eith your recos - how can we simolify, whilemkeepikg guarantees and speed? the goal is faster than hand optimized go code". A third round followed: "go with your recos - can we simplify further", and for bindings, "also let instead of var". A fourth round followed: "lets go with that - match instead of switch". A fifth round followed: "ok i like your recos except machine - i think the syntax earns its keep because we want something that can be both an actor and a fsm - id prefer consistency with oir types so find a way to make interface xonsistent with other types".

This preview shows the same programs in three columns. It measures the difference where it can. It defines how [#220](https://github.com/cevr/ergo/issues/220) stage 1b compares the mistake rates of agents on the two syntaxes. It does not decide whether Ergo leaves Go's syntax. It is an input to that decision.

## How to read this preview

The three columns:

- **Today**: the Go-shaped language of [`docs/spec/`](../spec/README.md) at commit `56ff92ff`.
- **Decided**: the Go-shaped language after the owner's decisions of 2026-10-28. The decisions are not in `docs/spec/` or the checker yet. Each one lands in a stage of [#196](https://github.com/cevr/ergo/issues/196), in the order 196u, 196p, 196r, 196m, 196v, 196t, 196g, 196c, 196d, 196e, 196s, 196l, 196x.
- **Non-Go**: a syntax that keeps Ergo's philosophy and its decisions, but not Go's syntax. It has no parser. Every Non-Go snippet is **Proposed**.

The labels follow [the preview rules](README.md#rules):

- **Checked**: the snippet is a whole file. `ergo check main.ef` exits 0 at commit `56ff92ff`, in a directory whose `ergo.json` is `{"edition": "2026"}`. `ergo run main.ef` prints the output the preview names.
- **Proposed**: no checker admits the snippet yet. A Today snippet that is Proposed is designed in `docs/spec/` and not built. A Decided snippet is Proposed until its stage lands in `docs/spec/` and the checker. Its rule is still decided. A Non-Go snippet is Proposed and undecided.

### What the owner decided on 2026-10-28

| Decision | Stage | Today | Decided and Non-Go |
| --- | --- | --- | --- |
| No untyped constants: a literal takes the type its context expects, else its default | 196u | Go's untyped constants | Decided (owner, 2026-10-28) |
| `if` and `match` are expressions everywhere, and a body yields its last expression | 196r | statements; a function ends in `return` | Decided (owner, 2026-10-28) |
| One pattern language for `match` cases, `catch` arms and binding destructuring | 196p | a case names a type | Decided (owner, 2026-10-28) |
| `match subject { case pattern: body }` replaces every `switch` form; `_` replaces `default`; several subjects form a tuple; `match` is total and an expression everywhere; `catch` arms use the same `case pattern:` form | 196p, amended ([#196 comment](https://github.com/cevr/ergo/issues/196#issuecomment-6460207338)) | type, value and tagless `switch` | Decided (owner, 2026-10-28: "match instead of switch") |
| One `for`: `for x in xs`, `for i, x in xs`, `for i in 0..n`, `for cond { }`, `for { }`. No C-style `for`, no `++` or `--`, no `range` | 196v, extended | Go's `for` forms | Decided (owner, 2026-10-28). `in` and `..` are decided by principle and open to the owner |
| `make`, `new`, `cap`, `copy`, `close`, `print` and `println` are removed; `xs.Append(x)`, `m.Delete(k)` and `slice.WithCapacity(n)` replace them; `[]T{}`, `map[K]V{}` and `len` stay | 196v, extended | Go's built-ins | Decided (owner, 2026-10-28) |
| Qualified imports only: no dot imports, and an alias only to resolve a name clash | 196v, extended | dot and aliased imports (designed) | Decided (owner, 2026-10-28) |
| One union declaration form: `type Payment = \| Pending{} \| Authorized{AuthID string}`; a bare name refers to an existing type | 196p | `type Payment interface{ A \| B }` and one struct per variant | Decided (owner, 2026-10-28) |
| No method receivers: `x.f(y)` resolves to `f(x, y)` from the package of `x`'s type | 196m | Go methods | Decided (owner, 2026-10-28) |
| `let x = e` binds a name that cannot be reassigned, and `let mut x = e` one that can; `:=` and `var` are removed. No shadowing. No fixed arrays `[N]T`. No labelled `break` or `continue` | 196v, amended ([#196 comment](https://github.com/cevr/ergo/issues/196#issuecomment-6460161923)) | `:=`, `var` and Go's rules | Decided (owner, 2026-10-28: "also let instead of var"). The marker `mut` is decided by principle and open to the owner |
| One result, plus tuples `(a, b)`. The failure row is not a tuple member | 196t | several results (designed) | Decided (owner, 2026-10-28) |
| A constraint is a method set, or `comparable`, `ordered` or `number`. No `~T` type sets | 196g | Go's constraints | Decided (owner, 2026-10-28) |
| No `go`, `chan`, `select`, `<-` or `defer`. `task.Go` starts a task; typed queues and `task.Race` replace channels and `select`; `scope.OnExit` runs other cleanup | 196c | Go's forms (designed) | Decided (owner, 2026-10-28) |
| No `reflect`, no `any` and no empty interface. Interface values exist only for services; generics cover open sets, and unions cover closed sets | 196d | Go's `reflect` and `any` | Decided (owner, 2026-10-28) |
| A function value does not escape by default; the `escaping` marker shows the rest | 196e | designed: non-escaping parameters | Decided (owner, 2026-10-28). The word `escaping` is recommended and open |
| String interpolation `"ok \(id)"` replaces format verbs | 196s | `+` and `fmt` | Decided (owner, 2026-10-28) |
| One shape for type declarations: `type Card { Number string }` is a record, `type Store { Find(id string) Order fails NotFound; law … }` an interface; the member shape decides the kind. No `struct` or `interface` keywords, no type aliases | 196p, extended | `struct` and `interface` | Decided (owner, 2026-10-28: "find a way to make interface xonsistent with other types") |
| `machine` keeps its declaration, in the same shape: `type Breaker machine[State, Signal] { initial …; step Step }`. One machine is both an FSM and an actor (`actor.StartMachine`) | #3, #57 | `type X machine[S, E] { … }` | Decided (owner, 2026-10-28: "the syntax earns its keep") |
| `fails` replaces `(T, error[E])`: `func F(…) T fails A \| B needs S`; function types `func(A) B fails E needs R` | 196t, beside tuples | `(T, error[E])` | Decided (owner, 2026-10-28) |
| No `package` line; one `import` per line, with an unquoted path | 196v, extended | `package` and quoted, grouped imports | Decided (owner, 2026-10-28) |
| `test Name { }` is an example and `test Name(x T) { }` a property; `assert` reports sub-expression values; `law` lives in operation types | 196l | `TestX`, `LawX` and `testing.T` | Decided (owner, 2026-10-28) |
| `extern go "pkg.Func" func F(…)` replaces `//ef:go`; `let W = wire { A: x }` makes a wire a value; `with wire { … } { … }` works inline | 196x | `//ef:go`, `wire W { A = x }` | Decided (owner, 2026-10-28) |
| A codec is derived from the type; one `Codec[T]` value per type gives a custom wire form | 196d, extended; #49 | `ParseX` and `Wire` by name | Decided (owner, 2026-10-28) |
| A resource satisfies a `Resource` operation type with a release law | 196c, extended | a type with `Close` | Decided (owner, 2026-10-28) |
| Function literals without parameter types: `func(x) { x * 2 }` | [#214](https://github.com/cevr/ergo/issues/214) | typed parameters | Decided (owner, 2026-10-28), tracked on #214 |
| One merged row for failures and services | none | separate rows | Considered; rejected by the owner, 2026-10-28. `fails` and `needs` stay separate |
| A surface without pointers | spike [#222](https://github.com/cevr/ergo/issues/222) | `*T` on parameters | Measure first; Proposed |
| Fewer number types: `int`, `float` and `byte` | measured on [#222](https://github.com/cevr/ergo/issues/222) | Go's numeric types (designed) | Measure first; Proposed |

Whether to adopt the full Non-Go syntax is not decided. The [#220 stage-1b arm](#the-220-stage-1b-arm) below is the test that decides it.

**The Decided column is now far from Go.** After these decisions it has no `package` line, no `:=`, no `var`, no `switch`, no `struct` or `interface` keyword, no `(T, error)` result, no `go`, no `defer`, no channels, no C-style `for`, no `make`, no receivers, no several results, no `TestX` functions and no untyped constants. What it keeps from Go is `func`, the `name Type` order of parameters and fields, its types and its lowering target. The Decided and Non-Go columns now share their semantics, their declarations, their binding words, `match`, `for`, `fails`, `with`, `test`, `law` and imports. They differ in three spellings: `func` against `fn`, `name Type` against `name: Type`, and a result after the parameters against a result after `->`. This preview compares that remaining difference.

## What stays

Every column keeps Ergo's philosophy and its guarantees. A signature lists every expected failure and every service the caller must supply. The compiler checks typed errors, declared dependencies, resource safety, structured concurrency with cancellation and every failure cause kept. Tracing and observability come from the checked model, with no span calls in the source. Laws state what must hold, and `ergo test` checks them. Constructs that the compiler must know get keywords, and a dot form is always library. Braces delimit blocks (owner, 2026-10-27: "No, it's fine. We can keep braces"). Names are exported by an upper-case first letter, so a name maps one to one to its Go name. Go is the only target. The Non-Go column still lowers to plain Go: rows are erased, `try` becomes `if err != nil`, a union becomes a Go interface or a tagged struct, and generics become Go code. The Non-Go column keeps Go's types (`string`, `int64`, `[]T`, `map[K]V`), the decided library calls (`len`, `xs.Append(x)`, `slice.WithCapacity(n)`) and Go's semicolon insertion. It changes only the surface.

Speed is the same in the Decided and Non-Go columns, because the syntax changes no lowering (inferred). The owner restated the goal on 2026-10-28, "faster than hand optimized go code" (north-star pillar 7). The levers toward it are listed on [#190](https://github.com/cevr/ergo/issues/190#issuecomment-6460103094): full monomorphization, wire devirtualization, unions by value, errors without allocation and others. Several decided stages give the compiler a fact that a lever uses: 196g and 196d allow specialization with no run-time dictionary, 196e allows inlined function values, and 196s lowers interpolation without boxing arguments into interfaces (inferred from the stage designs). Each lever needs its own benchmark against hand-written Go. This preview measures no speed.

## Side by side

### The README program

The program of the [README](../../README.md): a timeout around a resource, a law, a test wire and a test.

**Today.** Label: **Proposed**. The program is designed. With a `package pay` clause added, `ergo fmt --check` accepts it at `56ff92ff` (measured: exit 0). `ergo check` stops at S109 "this composite literal type is outside the implemented subset" (measured).

<!-- count: readme/today -->
```go
import (
	"testing"
	"timeout"
)

// A signature lists every expected failure and every service the caller must supply.
func Charge(card Card, amount int64) (Receipt, error[Declined | Frozen | timeout.Expired]) needs Accounts, Payments, Scheduler {
	return timeout.After(2s) { // the block runs as a child task; at 2s it is stopped and drained
		lock := try Accounts.Lock(card) // a resource: released on every exit, the timeout included
		Payments.Charge(lock, amount)   // lends lock for the call
	}
}

// A law. It must hold for every Payments provider, and ergo test checks it like a fuzz target.
func LawRefundNeverExceedsCharge(r Receipt) bool needs Payments {
	return Payments.Refundable(r) <= r.Amount
}

// A test with replaced providers and virtual time: no sleeps and no mock framework.
wire SlowWorld {
	Accounts  = MemoryAccounts{}
	Payments  = NewSlowGateway         // a constructor: it answers after 5s on this wire's Scheduler
	Scheduler = testing.VirtualClock{} // jumps to the next timer when every task waits
}

func TestSlowGatewayTimesOut() needs testing.T {
	r := with(SlowWorld) { Charge(Card{Number: "4242"}, 100) } catch {
	case timeout.Expired:
		Receipt{ID: "timed-out"} // reached at once, in virtual time
	case Declined, Frozen:
		Receipt{}
	}
	T.Equal(r.ID, "timed-out")
}
```

**Decided.** Label: **Proposed** (196r, 196v, `fails`, 196l, 196x, 196p as extended). One `import` per line, with an unquoted path. `fails` follows the result. The law moves into the operation type `Payments`, where it is checked against every implementation. A wire is a value bound with `let`. A test is a `test` declaration, and `assert` reports the values of its sub-expressions. `with` takes the wire without parentheses (inferred from `with wire { … } { … }`).

<!-- count: readme/decided -->
```go
import testing
import timeout

func Charge(card Card, amount int64) Receipt fails Declined | Frozen | timeout.Expired needs Accounts, Payments, Scheduler {
	timeout.After(2s) {
		let lock = try Accounts.Lock(card)
		Payments.Charge(lock, amount)
	}
}

type Payments {
	Charge(lock Lock, amount int64) Receipt fails Declined
	Refundable(r Receipt) int64

	law RefundNeverExceedsCharge(r Receipt) { Refundable(r) <= r.Amount }
}

let SlowWorld = wire {
	Accounts:  MemoryAccounts{}
	Payments:  NewSlowGateway
	Scheduler: testing.VirtualClock{}
}

test SlowGatewayTimesOut {
	let r = with SlowWorld { Charge(Card{Number: "4242"}, 100) } catch {
	case timeout.Expired:
		Receipt{ID: "timed-out"}
	case Declined, Frozen:
		Receipt{}
	}
	assert r.ID == "timed-out"
}
```

**Non-Go.** Label: **Proposed**. The same program. Parameters and fields read `name: Type`, `fn` replaces `func`, and the result follows `->`.

<!-- count: readme/nongo -->
```text
import testing
import timeout

fn Charge(card: Card, amount: int64) -> Receipt fails Declined | Frozen | timeout.Expired needs Accounts, Payments, Scheduler {
	timeout.After(2s) {
		let lock = try Accounts.Lock(card)
		Payments.Charge(lock, amount)
	}
}

type Payments {
	Charge(lock: Lock, amount: int64) -> Receipt fails Declined
	Refundable(r: Receipt) -> int64

	law RefundNeverExceedsCharge(r: Receipt) { Refundable(r) <= r.Amount }
}

let SlowWorld = wire {
	Accounts:  MemoryAccounts{}
	Payments:  NewSlowGateway
	Scheduler: testing.VirtualClock{}
}

test SlowGatewayTimesOut {
	let r = with SlowWorld { Charge(Card{Number: "4242"}, 100) } catch {
	case timeout.Expired:
		Receipt{ID: "timed-out"}
	case Declined, Frozen:
		Receipt{}
	}
	assert r.ID == "timed-out"
}
```

- **Tests and laws (196l).** `test Name { … }` is an example. `test Name(x T) { … }` is a property, run with generated inputs. `assert e` reports the values of the sub-expressions of `e` when it fails, so the test needs no `testing.T` service. A `law` is a member of an operation type and is checked against every implementation, which the closed world (196d) can list. These replace the `TestX` and `LawX` naming rules.
- **Count effect.** The Decided and Non-Go blocks declare `Payments`, because the law lives in it. Today's program does not declare `Payments`, so the two decided blocks carry two operation lines that Today's block leaves out.
- `with SlowWorld { … }` needs a rule that Go also has: a composite literal is not parsed in a `with`, `if`, `match` or `for` header unless it is in parentheses. Rust has the same rule for `if` and `match`.

### A union, destructuring and an exhaustive match

**Today.** Label: **Checked**. `ergo check main.ef` exits 0 at `56ff92ff`, and `ergo run main.ef` prints `ok auth-7` and `fraud: velocity` (measured).

<!-- count: union/today -->
```go
package pay

type Payment interface{ Pending | Authorized | Declined }

type Pending struct{}
type Authorized struct{ AuthID string }
type Declined struct{ Reason Reason }

type Reason interface{ Fraud | CardExpired }

type Fraud struct{ Rule string }
type CardExpired struct{}

func describe(pay Payment) string {
	switch p := pay.(type) {
	case Pending:
		return "waiting"
	case Authorized:
		return "ok " + p.AuthID
	case Declined:
		switch r := p.Reason.(type) {
		case Fraud:
			return "fraud: " + r.Rule
		case CardExpired:
			return "declined"
		}
	}
}

func main() needs Console {
	Console.Println(describe(Authorized{AuthID: "auth-7"}))
	Console.Println(describe(Declined{Reason: Fraud{Rule: "velocity"}}))
}
```

**Decided.** Label: **Proposed** (196p as amended and extended, 196r, 196s, 196v as extended). There is no `package` line: the directory names the package. A union lists its variants in one declaration. `Name{fields}` declares a variant struct, and a bare name refers to a type declared elsewhere. `match` replaces the type switch. A case pattern mirrors the composite literal that builds the value, and it nests. The `match` yields a value, and it must cover every variant.

<!-- count: union/decided -->
```go
type Payment =
	| Pending{}
	| Authorized{AuthID string}
	| Declined{Reason Reason}

type Reason =
	| Fraud{Rule string}
	| CardExpired{}

func describe(pay Payment) string {
	match pay {
	case Pending:
		"waiting"
	case Authorized{AuthID: id}:
		"ok \(id)"
	case Declined{Reason: Fraud{Rule: rule}}:
		"fraud: \(rule)"
	case Declined:
		"declined"
	}
}

func main() needs Console {
	Console.Println(describe(Authorized{AuthID: "auth-7"}))
	Console.Println(describe(Declined{Reason: Fraud{Rule: "velocity"}}))
}
```

**Non-Go.** Label: **Proposed**. The union form and the `match` are the decided ones, with `name: Type` fields.

<!-- count: union/nongo -->
```text
type Payment =
	| Pending{}
	| Authorized{AuthID: string}
	| Declined{Reason: Reason}

type Reason =
	| Fraud{Rule: string}
	| CardExpired{}

fn describe(pay: Payment) -> string {
	match pay {
	case Pending:
		"waiting"
	case Authorized{AuthID: id}:
		"ok \(id)"
	case Declined{Reason: Fraud{Rule: rule}}:
		"fraud: \(rule)"
	case Declined:
		"declined"
	}
}

fn main() needs Console {
	Console.Println(describe(Authorized{AuthID: "auth-7"}))
	Console.Println(describe(Declined{Reason: Fraud{Rule: "velocity"}}))
}
```

- The pattern `Declined{Reason: Fraud{Rule: rule}}` covers one variant of the nested union. The arm `Declined` covers the rest. The exhaustiveness check treats a variant pattern as a constructor, as Maranget's usefulness algorithm does (196p rule 3).
- In a pattern, a name that resolves to a type matches that type. Any other name binds. Rust resolves the same ambiguity the same way.
- `"ok \(id)"` interpolates `id` (196s). `\(` is not a valid escape in a Go string, so the form cannot be misread. Each interpolated value must have a generated text form (196d).
- The two decided bodies of `describe` are now the same text. The union declarations differ only in the colon of each field, and the signatures in `func` against `fn` and `-> string`.

### Type declarations: one shape

**Today.** Label: **Proposed**. A record, an interface and a provider law, as in [types.md](../spec/types.md#laws).

```go
type Card struct {
	Number string
	Expiry Date
}

type Store interface {
	Find(id string) (Order, error[NotFound])
	Put(id string, o Order)
}

func LawFindAfterPut(id string, v Order) bool needs Store {
	Store.Put(id, v)
	return Store.Find(id) == v
}
```

**Decided.** Label: **Proposed** (196p as extended, `fails`, 196l). A type lists what its values have, and the shape of its members decides its kind. Fields make a record. Operations make an interface, which may hold laws. The `struct` and `interface` keywords are removed. A type with fields and operations is S108. `=` means a union and nothing else, so there are no type aliases.

```go
type Card {
	Number string
	Expiry Date
}

type Store {
	Find(id string) Order fails NotFound
	Put(id string, o Order)

	law FindAfterPut(id string, o Order) { Put(id, o); try Find(id) == o }
}

type Celsius float64
```

**Non-Go.** Label: **Proposed**.

```text
type Card {
	Number: string
	Expiry: Date
}

type Store {
	Find(id: string) -> Order fails NotFound
	Put(id: string, o: Order)

	law FindAfterPut(id: string, o: Order) { Put(id, o); try Find(id) == o }
}

type Celsius float64
```

- The same shape appears four times: the declaration `type Card { … }`, a variant `Authorized{AuthID string}`, a literal `Card{Number: "4242"}` and a pattern `case Card{Number: n}:`.
- Use decides the role, as today ([types.md](../spec/types.md#capabilities), Capabilities rule 3). An operation type in `needs` or in a wire is a service. In a constraint position it is a constraint (196g). As a value type it is a provider value (196d).
- `type Celsius float64` is a defined type over another type. It is not an alias.
- Prior art: TypeScript object types put properties and methods in one literal form, and the member shape decides the kind. Rust `struct` and Gleam custom types put the fields directly after the name. Go chooses the kind by keyword.

### Machines: an FSM and an actor

The owner kept the `machine` declaration, because one machine serves both as a finite-state machine and as an actor (owner, 2026-10-28: "i think the syntax earns its keep because we want something that can be both an actor and a fsm"). It takes the same shape as the other types: the construct keyword, then members.

**Today.** Label: **Proposed**. The README's machine, designed ([runtime.md](../spec/runtime.md#machines-and-actors)).

```go
type State interface{ Closed | Open | HalfOpen }
type Signal interface{ Failure | Success | Retry }

func step(s State, sig Signal) (Decision[State, string], error[ProbeDown]) needs Probe {
	switch s, sig := s.(type), sig.(type) {
	case (Closed, Failure):
		return Enter{Open{sig.Endpoint}}
	case (HalfOpen, Success):
		return Enter{Closed{0}}
	case (_, _):
		return Ignore{}
	}
}

type Breaker machine[State, Signal] {
	initial Closed{0}
	step    step
}

func trip(endpoint string) error[actor.Stopped] needs Probe {
	b := actor.StartMachine(Breaker{}, 16)
	try b.Send(Failure{Endpoint: endpoint})
}
```

**Decided.** Label: **Proposed** (196p as amended and extended, `fails`, 196r, 196v). The step is a total `match` over the tuple `(State, Signal)`.

```go
type State = | Closed{Failures int64} | Open{Endpoint string} | HalfOpen{}
type Signal = | Failure{Endpoint string} | Success{} | Retry{}

func Step(s State, sig Signal) Decision[State, string] fails ProbeDown needs Probe {
	match (s, sig) {
	case (Closed, Failure{Endpoint: e}):
		Enter{Open{Endpoint: e}}
	case (HalfOpen, Success):
		Enter{Closed{Failures: 0}}
	case _:
		Ignore{}
	}
}

type Breaker machine[State, Signal] {
	initial Closed{Failures: 0}
	step    Step
}

func trip(endpoint string) fails actor.Stopped needs Probe {
	let b = actor.StartMachine(Breaker, 16)
	try b.Send(Failure{Endpoint: endpoint})
}
```

**Non-Go.** Label: **Proposed**.

```text
type State = | Closed{Failures: int64} | Open{Endpoint: string} | HalfOpen{}
type Signal = | Failure{Endpoint: string} | Success{} | Retry{}

fn Step(s: State, sig: Signal) -> Decision[State, string] fails ProbeDown needs Probe {
	match (s, sig) {
	case (Closed, Failure{Endpoint: e}):
		Enter{Open{Endpoint: e}}
	case (HalfOpen, Success):
		Enter{Closed{Failures: 0}}
	case _:
		Ignore{}
	}
}

type Breaker machine[State, Signal] {
	initial Closed{Failures: 0}
	step    Step
}

fn trip(endpoint: string) fails actor.Stopped needs Probe {
	let b = actor.StartMachine(Breaker, 16)
	try b.Send(Failure{Endpoint: endpoint})
}
```

- **As an FSM:** `Breaker.Step(s, sig)` is a pure, total step.
- **As an actor:** `actor.StartMachine(Breaker, 16)` runs it in the actor library, and the owner scope stops it.
- The laws of the `Machine` protocol, such as reachability and invariants, apply to every machine.
- `actor.StartMachine(Breaker, 16)` passes the machine type as a value, as the decision writes it; today's README passes `Breaker{}`. The stage decides which form is legal (inferred).

### Codecs and resources

These decisions have no side-by-side snippet here, because their surface is one value or one type.

- **Codecs (196d, extended; #49).** A codec is derived from the type by default. A custom wire form is the one `Codec[T]` value in the type's own package, found by type, not by name. The `RoundTrip` law on `Codec[T]` applies to every codec. This settles README Gap 20.
- **Resources as a protocol (196c, extended).** A resource satisfies a `Resource` operation type, with `Release` and the law "released exactly once on every exit". Users can write their own resources with the same guarantees.

### `catch` and `try`, total and partial

`try e catch { … }` handles the named failures and lets the rest escape into the caller's row. `e catch { … }` without `try` must handle every failure in the row.

**Today.** Label: **Checked**. `ergo check main.ef` exits 0 at `56ff92ff`, and `ergo run main.ef` prints `auth-42` and `missing` (measured).

```go
package checkout

type NotFound struct{ ID string }
type Declined struct{ Reason string }

type Order struct {
	ID    string
	Total int64
}

type Receipt struct{ ID string }

type Orders interface {
	Get(id string) (Order, error[NotFound])
}

type Payments interface {
	Authorize(order Order) (Receipt, error[Declined])
}

func Checkout(id string) (Receipt, error[NotFound | Declined]) needs Orders, Payments {
	order := try Orders.Get(id)
	return Payments.Authorize(order)
}

// Partial: Declined is handled here, and NotFound escapes into the row of Settle.
func Settle(id string) (string, error[NotFound]) needs Orders, Payments {
	receipt := try Checkout(id) catch {
	case Declined:
		Receipt{ID: "none"}
	}
	return receipt.ID
}

// Total: every failure is handled, so Quote has no error row.
func Quote(id string) string needs Orders, Payments {
	receipt := Checkout(id) catch {
	case NotFound:
		Receipt{ID: "missing"}
	case Declined:
		Receipt{ID: "declined"}
	}
	return receipt.ID
}

type MemoryOrders struct{}

func (MemoryOrders) Get(id string) (Order, error[NotFound]) {
	if id == "42" {
		return Order{ID: id, Total: 1999}
	}
	return NotFound{ID: id}
}

type FakePayments struct{}

func (FakePayments) Authorize(order Order) (Receipt, error[Declined]) {
	return Receipt{ID: "auth-" + order.ID}
}

wire Live {
	Orders   = MemoryOrders{}
	Payments = FakePayments{}
}

func main() needs Console {
	settled := with(Live) { Settle("42") } catch {
	case NotFound:
		"missing"
	}
	Console.Println(settled)
	Console.Println(with(Live) { Quote("7") })
}
```

**Decided.** Label: **Proposed** (196r, 196m, 196v, 196p as extended, `fails`, 196x). The whole file, so that it compares with the Checked one. There is no `package` line. Types take one shape: fields make a record, operations make an interface. `fails` follows the result. Final `return`s go, and `return` stays for the early exit. `let` binds. A provider method becomes a package function whose first parameter is the provider. A wire is a value, and `with` takes it without parentheses (inferred from `with wire { … } { … }`).

```go
type NotFound { ID string }
type Declined { Reason string }

type Order {
	ID    string
	Total int64
}

type Receipt { ID string }

type Orders {
	Get(id string) Order fails NotFound
}

type Payments {
	Authorize(order Order) Receipt fails Declined
}

func Checkout(id string) Receipt fails NotFound | Declined needs Orders, Payments {
	let order = try Orders.Get(id)
	Payments.Authorize(order)
}

// Partial: Declined is handled here, and NotFound escapes into the row of Settle.
func Settle(id string) string fails NotFound needs Orders, Payments {
	let receipt = try Checkout(id) catch {
	case Declined:
		Receipt{ID: "none"}
	}
	receipt.ID
}

// Total: every failure is handled, so Quote has no error row.
func Quote(id string) string needs Orders, Payments {
	let receipt = Checkout(id) catch {
	case NotFound:
		Receipt{ID: "missing"}
	case Declined:
		Receipt{ID: "declined"}
	}
	receipt.ID
}

type MemoryOrders {}

func Get(_ MemoryOrders, id string) Order fails NotFound {
	if id != "42" {
		return NotFound{ID: id}
	}
	Order{ID: id, Total: 1999}
}

type FakePayments {}

func Authorize(_ FakePayments, order Order) Receipt fails Declined {
	Receipt{ID: "auth-\(order.ID)"}
}

let Live = wire {
	Orders:   MemoryOrders{}
	Payments: FakePayments{}
}

func main() needs Console {
	let settled = with Live { Settle("42") } catch {
	case NotFound:
		"missing"
	}
	Console.Println(settled)
	Console.Println(with Live { Quote("7") })
}
```

**Non-Go.** Label: **Proposed**. The same file. Only the spelling of parameters, fields and results differs: `name: Type`, `fn`, and `->` before the result.

```text
type NotFound { ID: string }
type Declined { Reason: string }

type Order {
	ID: string
	Total: int64
}

type Receipt { ID: string }

type Orders {
	Get(id: string) -> Order fails NotFound
}

type Payments {
	Authorize(order: Order) -> Receipt fails Declined
}

fn Checkout(id: string) -> Receipt fails NotFound | Declined needs Orders, Payments {
	let order = try Orders.Get(id)
	Payments.Authorize(order)
}

// Partial: Declined is handled here, and NotFound escapes into the row of Settle.
fn Settle(id: string) -> string fails NotFound needs Orders, Payments {
	let receipt = try Checkout(id) catch {
	case Declined:
		Receipt{ID: "none"}
	}
	receipt.ID
}

// Total: every failure is handled, so Quote has no error row.
fn Quote(id: string) -> string needs Orders, Payments {
	let receipt = Checkout(id) catch {
	case NotFound:
		Receipt{ID: "missing"}
	case Declined:
		Receipt{ID: "declined"}
	}
	receipt.ID
}

type MemoryOrders {}

fn Get(_: MemoryOrders, id: string) -> Order fails NotFound {
	if id != "42" {
		return NotFound{ID: id}
	}
	Order{ID: id, Total: 1999}
}

type FakePayments {}

fn Authorize(_: FakePayments, order: Order) -> Receipt fails Declined {
	Receipt{ID: "auth-\(order.ID)"}
}

let Live = wire {
	Orders:   MemoryOrders{}
	Payments: FakePayments{}
}

fn main() needs Console {
	let settled = with Live { Settle("42") } catch {
	case NotFound:
		"missing"
	}
	Console.Println(settled)
	Console.Println(with Live { Quote("7") })
}
```

- The wire entries use `:`, as a composite literal does (inferred from the decision's `wire { A: x }`).

- `catch` stays postfix in every column. The prefix `catch f() { … }` is ranked mistake 2 in [the stage-1a ranking](../research/agent-mistakes.md#ranked-language-mistakes) (3 instances, measured there). The Non-Go syntax does not remove it.
- `try` stays a prefix keyword that binds looser than every operator. A Rust-style postfix `?` was not taken: ranked mistake 6, `try` over a selector or a comparison, is a Rust habit (2 instances, measured there).

### A signature with failures and services

**Today.** Label: **Proposed**. The first line has the shape of `Checkout` in the Checked file above, with a `card` parameter added. The generic line uses the provisional spelling of [types.md](../spec/types.md#generics).

```go
func Checkout(id string, card Card) (Receipt, error[NotFound | Declined]) needs Orders, Payments
func Settle(id string) error[NotFound] needs Orders
func Retry(n int64, body func() (T, error[E]) needs R) (T, error[E]) needs R
```

**Decided.** Label: **Proposed** (`fails`, 196t). `fails` follows the result, and `error[…]` leaves the syntax. A function has one result, which may be a tuple.

```go
func Checkout(id string, card Card) Receipt fails NotFound | Declined needs Orders, Payments
func Settle(id string) fails NotFound needs Orders
func Retry(n int64, body func() T fails E needs R) T fails E needs R
func Split(total int64, parts int64) (int64, int64) fails ZeroParts
```

**Non-Go, separate rows.** Label: **Proposed**. The decided rows, with `->` before the result.

```text
fn Checkout(id: string, card: Card) -> Receipt fails NotFound | Declined needs Orders, Payments
fn Settle(id: string) fails NotFound needs Orders
fn Retry(n: int64, body: fn() -> T fails E needs R) -> T fails E needs R
fn Split(total: int64, parts: int64) -> (int64, int64) fails ZeroParts
```

- Today, `(T, error[E])` looks like Go's two results, and after 196t it could be read as a tuple. `fails` removes that reading: `(int64, int64)` is a tuple, and the row follows `fails`. A Non-Go spelling `-> T ! E` was considered; the decided `fails` serves both columns, so only `->` remains Non-Go.
- Prior art for `fails`: Swift 6 typed throws `throws(E)`, Java `throws`, Zig `E!T`.

**Non-Go, one merged row (Koka and Unison style).** Label: **Proposed**. Considered; rejected by the owner, 2026-10-28.

```text
fn Checkout(id: string, card: Card) -> Receipt / {NotFound, Declined, Orders, Payments}
fn Settle(id: string) / {NotFound, Orders}
fn Retry(n: int64, body: fn() -> T / R) -> T / R
```

The trade-off:

- **For the merged row.** It is shorter: one list and one row variable (`R` in place of `E` and `R`). It is one concept, as in Koka (`<exn, console> int`), Unison (`->{IO, Exception} Nat`) and Effekt (`Int / { Exc }`).
- **Against it.** A reader cannot tell a failure from a service without the declaration of each name. `catch` removes failures and `with` removes services, so one row would have two eliminators. The [#135 record](../research/rows-and-constraints.md) already refused one combined row variable. The merged row also reads like a Koka effect row, where an empty row means pure. Ranked mistake 3 is exactly that reading of `needs` (2 instances, harm 3, measured there).
- **The separate rows keep** each kind visible at the call site and in `ergo inspect`, which reports `errors` and `needs` apart.

### Bindings

**Today.** Label: **Proposed**. `=` reassignment and local `var` are designed, not built.

```go
func total(order Order, fee int64) int64 {
	base := order.Total
	sum := base + fee // := declares a new local
	if sum > 1000 {
		sum = sum + 5 // = reassigns it
	}
	var limit int64 = 5000 // a typed local
	return sum + limit
}
```

**Decided.** Label: **Proposed** (196v as amended, 196p, 196r). `let` is the one binding form, and `let mut` marks a name that can be reassigned. `:=` and `var` are removed, locally and at package level. A pattern may bind after `let`, and it must be irrefutable (S103 otherwise, as in Rust's `let`). An inner scope may not redeclare a name of its function (S108).

```go
func total(order Order, fee int64) int64 {
	let Order{Total: base} = order // base cannot be reassigned
	let mut sum = base + fee        // sum can
	if sum > 1000 {
		sum = sum + 5
	}
	let limit int64 = 5000
	sum + limit
}
```

**Non-Go.** Label: **Proposed**. The same rules. The type follows a colon.

```text
fn total(order: Order, fee: int64) -> int64 {
	let Order{Total: base} = order
	let mut sum = base + fee
	if sum > 1000 {
		sum = sum + 5
	}
	let limit: int64 = 5000
	sum + limit
}
```

- After the amended 196v, the two decided columns use the same binding words. They differ only in the colon before a type.
- Go's `:=` and `=` differ by one character, and Go's "at least one new variable" rule lets `a, err := …` reuse `a`. `let` and `let mut` make the difference a word, and the rarer case, a mutable local, carries the marker.
- A `for` header binds as `let` does: `for i, x in xs` (196v, extended). `match` binds through its patterns. One Go header stays open (inferred from the decisions, which do not mention it): the init statement of `if x := f(); cond`. Neither sketch has one.

### Expression `if` and `match`

**Today.** Label: **Proposed**. `var` without a value is designed. A function with a result ends in `return`.

```go
func fee(gold bool, total int64) int64 {
	var rate int64
	if gold {
		rate = 0
	} else {
		rate = 3
	}
	return total * rate / 100
}

func label(pay Payment) string {
	switch p := pay.(type) {
	case Pending:
		return "waiting"
	case Authorized:
		return "ok " + p.AuthID
	case Declined:
		return "declined"
	}
}
```

**Decided.** Label: **Proposed** (196r, 196p as amended, 196u, 196v, 196s). `if` and `match` yield values in any position, and a body yields its last expression. A value `if` needs an `else`. A `match` must be total (S103). A constant pattern such as `case 404:` covers Go's value switch, and an `if`/`else` chain covers the tagless one.

```go
func fee(gold bool, total int64) int64 {
	let rate int64 = if gold { 0 } else { 3 }
	total * rate / 100
}

func label(pay Payment) string {
	match pay {
	case Pending:
		"waiting"
	case Authorized{AuthID: id}:
		"ok \(id)"
	case Declined:
		"declined"
	}
}

func log(pay Payment) needs Console {
	let text = match pay {
	case Pending:
		"waiting"
	case _:
		"settled"
	}
	Console.Println(text)
}
```

**Non-Go.** Label: **Proposed**.

```text
fn fee(gold: bool, total: int64) -> int64 {
	let rate: int64 = if gold { 0 } else { 3 }
	total * rate / 100
}

fn label(pay: Payment) -> string {
	match pay {
	case Pending:
		"waiting"
	case Authorized{AuthID: id}:
		"ok \(id)"
	case Declined:
		"declined"
	}
}

fn log(pay: Payment) needs Console {
	let text = match pay {
	case Pending:
		"waiting"
	case _:
		"settled"
	}
	Console.Println(text)
}
```

- The two decided bodies are the same text. Only the signatures and the colon in `let rate: int64` differ.
- `rate` needs its written type in both decided columns. Without it, `0` and `3` take the default `int`, and `total * rate` mixes `int64` and `int`, which is S108 under 196u rule 4.
- The stage-1a ranking records one Rust habit that this decision makes legal: a function literal with no `return` (ranked mistake 10, 1 instance, measured there).

### Generics and a constraint

**Today.** Label: **Proposed**. Type parameters are implicit: an otherwise unbound name of one upper-case letter is a type parameter, and its kind follows its position ([types.md](../spec/types.md#generics); [#135 record](../research/rows-and-constraints.md)). A constraint uses Go's bracket list.

```go
func Max[T cmp.Ordered](a, b T) T {
	if a > b {
		return a
	}
	return b
}

func Map(xs []A, f func(A) (B, error[E]) needs R) ([]B, error[E]) needs R {
	out := make([]B, 0, len(xs))
	for _, x := range xs {
		out = append(out, try f(x))
	}
	return out
}
```

**Decided.** Label: **Proposed** (196g, 196r, 196v as extended, 196e, `fails`). A constraint is a method set or one of the predeclared `comparable`, `ordered` and `number`. `out` is reassigned, so it is `let mut`. `for x in xs` replaces `for _, x := range xs`, and library calls replace `make` and `append`. `f` does not escape `Map`, so it needs no `escaping` marker.

```go
func Max[T ordered](a, b T) T {
	if a > b { a } else { b }
}

func Map(xs []A, f func(A) B fails E needs R) []B fails E needs R {
	let mut out = slice.WithCapacity[B](len(xs))
	for x in xs {
		out = out.Append(try f(x))
	}
	out
}
```

**Non-Go.** Label: **Proposed**. A constraint follows a colon, as in Rust and Swift.

```text
fn Max[T: ordered](a: T, b: T) -> T {
	if a > b { a } else { b }
}

fn Map(xs: []A, f: fn(A) -> B fails E needs R) -> []B fails E needs R {
	let mut out = slice.WithCapacity[B](len(xs))
	for x in xs {
		out = out.Append(try f(x))
	}
	out
}
```

- This preview assumes that `xs.Append(x)` returns the new slice, as Go's `append` does, and that `slice.WithCapacity` takes its element type as a type argument. The decision names the calls, not their signatures (inferred).

- The Non-Go column cannot group parameters (`a, b T`). Each parameter writes its type.
- A function value that escapes writes the marker in its type: `escaping func(T) U` in the Go-shaped column, `escaping fn(T) -> U` in the Non-Go column (196e). A field or a result of function type escapes by definition.
- A function literal may leave out its parameter types where the context gives them: `xs.Map(func(x) { x * 2 })` ([#214](https://github.com/cevr/ergo/issues/214)). The Non-Go spelling is `xs.Map(fn(x) { x * 2 })`.
- 196g also decides that generic code is specialized per instance in the lowering, so a constraint never needs a run-time dictionary. That is a speed lever of [#190](https://github.com/cevr/ergo/issues/190#issuecomment-6460103094); its gain is not measured yet (inferred).
- `for x in xs` is an old Ergo spelling that agents still wrote: the stage-1a corpus holds it in 7 programs (measured there, as an old spelling, not ranked). In both decided columns it is now the correct form, and `for … range` becomes the Go habit.

### Structured concurrency

A scope with two child tasks and a timeout. The `timeout.After` block is the scope. It owns both tasks. At 2s it stops them and waits for their cleanup. If `price` fails, the block exits with `NotFound`, and the scope stops and drains `rate`.

**Today.** Label: **Proposed**. `go` and `Task` are designed ([runtime.md](../spec/runtime.md#tasks)).

```go
func GetQuote(id string) (Quote, error[NotFound | timeout.Expired]) needs Catalog, Rates, Scheduler {
	return timeout.After(2s) {
		price := go Catalog.Price(id) // a Task[Price, NotFound], owned by the block
		rate := go Rates.Current()    // a Task[Rate] that cannot fail
		Quote{Price: try price.Wait(), Rate: rate.Wait()}
	}
}
```

**Decided.** Label: **Proposed** (196c, 196r, 196v, `fails`). `go` leaves the language. `task.Go` is a library function that starts a task and gives the same `Task[T, E]` resource. Its block needs the call's parentheses, as every trailing block does ([grammar.md](../spec/grammar.md#parser), Parser rule 7).

```go
func GetQuote(id string) Quote fails NotFound | timeout.Expired needs Catalog, Rates, Scheduler {
	timeout.After(2s) {
		let price = task.Go() { Catalog.Price(id) }
		let rate = task.Go() { Rates.Current() }
		Quote{Price: try price.Wait(), Rate: rate.Wait()}
	}
}
```

**Non-Go.** Label: **Proposed**.

```text
fn GetQuote(id: string) -> Quote fails NotFound | timeout.Expired needs Catalog, Rates, Scheduler {
	timeout.After(2s) {
		let price = task.Go() { Catalog.Price(id) }
		let rate = task.Go() { Rates.Current() }
		Quote{Price: try price.Wait(), Rate: rate.Wait()}
	}
}
```

- After 196c, concurrency is library code in both decided columns: `task.Go`, `timeout.After`, typed queues and `task.Race`. The two bodies are now the same text; only the signatures differ. Interruption points, masks and the scope rules do not change.
- 196c writes `task.Go { … }` without parentheses. Today's grammar refuses that form (S001), because a parser cannot tell it from the composite literal `task.Go{ … }`. This preview writes `task.Go() { … }` in both columns; the stage decides.
- Kotlin writes the same shape as `withTimeout(2000) { coroutineScope { async { … } } }`, and Swift as `withThrowingTaskGroup`. In both, the task functions are library code too.

### Methods and one function form

**Today.** Label: **Proposed**. A method call on a value is designed; the prototype refuses it with S109 ([types.md](../spec/types.md#methods)).

```go
func (o Order) Fee(rate int64) int64 {
	return o.Total * rate / 100
}

func (MemoryOrders) Get(id string) (Order, error[NotFound]) {
	return Order{ID: id, Total: 1999}
}

func total(order Order) int64 {
	return order.Fee(3)
}
```

**Decided.** Label: **Proposed** (196m, `fails`). There are no receivers. `x.f(a)` resolves to a field `f` first, then to a function `f` in the package that declares `x`'s type, whose first parameter takes `x`. Two candidates are S108. `MemoryOrders` satisfies `Orders` because its package declares `Get` with a `MemoryOrders` first parameter. The lowering emits plain Go functions, plus a Go method wrapper only where a value meets an interface.

```go
func Fee(o Order, rate int64) int64 {
	o.Total * rate / 100
}

func Get(_ MemoryOrders, id string) Order fails NotFound {
	Order{ID: id, Total: 1999}
}

func total(order Order) int64 {
	order.Fee(3) // Fee(order, 3)
}
```

**Non-Go.** Label: **Proposed**. The same rule, in the Non-Go spelling.

```text
fn Fee(o: Order, rate: int64) -> int64 {
	o.Total * rate / 100
}

fn Get(_: MemoryOrders, id: string) -> Order fails NotFound {
	Order{ID: id, Total: 1999}
}

fn total(order: Order) -> int64 {
	order.Fee(3)
}
```

- An open point for 196m (inferred from its text): Go forbids two package-level functions with one name. If two providers of `Orders` live in one package, both need a function `Get`. Then 196m needs a rule that lets functions share a name when their first parameter types differ. MoonBit allows "same-named methods on different types" in the owner package ([PRIOR_ART.md](../../PRIOR_ART.md), MoonBit). Without such a rule, each provider needs its own package.
- 196m resolves `x.f` only in the package of `x`'s type. A dot that reaches functions of other imported packages would work like a pipe. That is the separate 2026-10-08 pipe question.

### Constants and literals

The 196u rules are decided (owner, 2026-10-28) and hold in both decided columns. Ergo has no `const` keyword. Today a package-level `var` is immutable ([grammar.md](../spec/grammar.md#declarations), Declarations rule 6). After the amended 196v, a package-level `let` is immutable, and a package-level `let mut` is S108.

**Today.** Label: **Proposed**. Go's untyped-constant rules, which the checker of #196 followed until 196u. In v0, `int64` is the only integer type with checked meaning ([types.md](../spec/types.md#basic-types)); the full numeric set arrives with #196 and #81.

```go
var Limit = 10      // int
var Small int8 = 10 // int8

func scale() {
	var b float64 = 2 // accepted: the untyped 2 becomes float64
	var c int = 2.0   // accepted: 2.0 is representable as int
	d := 1 + 2.5      // accepted: float64 3.5
	e := 500ms        // duration
}
```

**Decided.** Label: **Proposed** (196u, 196v). A literal takes the type its context expects, else its default: `int`, `float64`, `rune`, `string` or `bool`. Both operands of a binary operator have one type. No name converts by itself.

```go
let Limit = 10      // int: the default
let Small int8 = 10 // int8: the written type; lowered to `const Small int8 = 10`

func scale() {
	let b float64 = 2  // accepted: an integer literal may take a float type
	let c int = 2.0    // S108: a float literal takes only a float type
	let d = 1 + 2.5    // S108: write 1.0 + 2.5
	let e = 500ms      // duration
	let f int8 = Limit // S108: Limit is int; write int8(Limit) or declare Limit int8
}
```

**Non-Go.** Label: **Proposed** (the 196u and 196v rules, decided; the spelling, undecided).

```text
let Limit = 10
let Small: int8 = 10

fn scale() {
	let b: float64 = 2
	let c: int = 2.0   // S108
	let d = 1 + 2.5    // S108
	let e = 500ms
	let f: int8 = Limit // S108
}
```

The literal rules and the binding words are the same in both decided columns. Only the colon before a type differs.

**Fewer number types.** Label: **Proposed** (measured first on [#222](https://github.com/cevr/ergo/issues/222), not decided). The surface keeps `int`, `float` and `byte`. Sized types stay at the `import go` boundary or in a `bits` library.

```go
let Limit = 10     // int
let Rate = 2.5     // float
let Mask byte = 255
```

With three number types, the 196u defaults become the only common types, and most literals need no written type (inferred).

### Pointers (spike #222)

Today a `*T` parameter is a mutable borrow: never nil, never stored, returned or captured ([types.md](../spec/types.md#borrows)). [#222](https://github.com/cevr/ergo/issues/222) asks whether the surface can drop `*T`. It measures first.

**Today.** Label: **Proposed** (designed, not built).

```go
type Cart struct{ Items []Item }

func Add(c *Cart, item Item) {
	c.Items = append(c.Items, item)
}

func fill(cart Cart, item Item) Cart {
	Add(&cart, item)
	return cart
}
```

**Without pointers.** Label: **Proposed** ([#222](https://github.com/cevr/ergo/issues/222), not decided). Values have value semantics. Move-on-last-use lets the lowering reuse `cart` in place ([types.md](../spec/types.md#structs), Structs rule 6).

```go
func Add(c Cart, item Item) Cart {
	Cart{Items: c.Items.Append(item)}
}

func fill(cart Cart, item Item) Cart {
	cart.Add(item)
}
```

#222 adopts this only if the value lowering stays within 10% of pointer passing on ns/op and allocs/op for every benchmark program. The question is the same in both syntaxes, so the Non-Go column takes the result of #222 as it is. In-place update of a value with one owner is also a speed lever of [#190](https://github.com/cevr/ergo/issues/190#issuecomment-6460103094) (Koka's Perceus, Roc); its gain is not measured (inferred).

## Counts (measured)

The counts cover the README program and the union example, the two blocks per column that carry a count marker in this file's source. Comments are stripped before counting. Every number in the first table is measured with the method in [Reproduction](#reproduction).

| Block | Column | Non-blank lines | Words (`wc -w`) | Lexical tokens | Distinct keywords | Keywords used |
| --- | --- | --- | --- | --- | --- | --- |
| README program | Today | 27 | 84 | 164 | 8 | `case catch func import needs return try wire` |
| README program | Decided | 27 | 95 | 164 | 14 | `assert case catch fails func import law let needs test try type wire with` |
| README program | Non-Go | 27 | 98 | 173 | 14 | `assert case catch fails fn import law let needs test try type wire with` |
| Union example | Today | 27 | 95 | 154 | 9 | `case func interface needs package return struct switch type` |
| Union example | Decided | 23 | 57 | 117 | 5 | `case func match needs type` |
| Union example | Non-Go | 23 | 58 | 122 | 5 | `case fn match needs type` |

What the counts say (inferred from the measured table):

- **The decisions do the shortening.** In the union example, the lexical tokens go from 154 (Today) to 117 (Decided): the union form, the patterns, `match`, the tail results, interpolation and the removed `package` line save 37 tokens. In the README program the totals are equal (164 and 164), but the programs are not: the Decided block also declares the `Payments` type with two operations, which Today's block leaves out (see the count-effect bullet of [the README program](#the-readme-program)). The `return`s, the grouped imports, the `bool` law function, the `testing.T` service and `T.Equal` go; `let`, `fails`, the wire entries and `assert` come in.
- **The Non-Go syntax adds a little.** In the union example it costs 5 tokens more than Decided (122 against 117): a colon in each of the three fields and in the parameter, and `->` before the result. In the README program it costs 9 tokens more (173 against 164): a colon in each of the six parameters and `->` before each of the three results. The lines are the same. It has more words in both programs (98 against 95, and 58 against 57), because each `->` is a separate word. Now that `fails`, the type shape, the imports and the wires are decided for both columns, the Non-Go syntax saves nothing in either program.
- **The keywords are the same.** The Non-Go blocks use `fn` for `func`. Every other keyword they use is a decided keyword.
- Token counts are not effort counts. A shorter program is not shown to be easier to write correctly. The [stage-1b arm](#the-220-stage-1b-arm) measures that.

### Keywords

Today has 28 keywords, the list in [grammar.md](../spec/grammar.md#keywords). Decided also has 28 (measured from the lists): 196c removes `go`, `chan`, `select` and `defer`; the amended 196v removes `var` and `range` and adds `let`, `mut` and `in`; the amended 196p removes `switch` and `default` and adds `match`; 196e adds `escaping` (the recommended spelling). The fifth round removes `struct`, `interface` and `package`, and adds `fails`, `test`, `law`, `assert` and `extern`. `with` becomes a keyword, because `with wire { … } { … }` takes a block (inferred; today `with` is a predeclared identifier). `go` stays a contextual word after `import` and `extern`. Non-Go has 28: the decided list with `fn` for `func`. The count is the same, but the words differ: 11 of Go's keywords leave, and 11 new ones arrive.

### Grammar rules

| Grammar | Syntax productions | How counted |
| --- | --- | --- |
| `docs/spec/grammar.md`, written EBNF | 48 | measured: productions in its `ebnf` blocks. They cover the prototype subset only, and the file defers to the Go spec for the rest |
| Go spec, go1.27 | 123 syntax and 43 lexical | measured: production names in the `ebnf` blocks of the Go spec |
| Today: the Go spec plus Ergo's forms | 133 | measured over a judged list: the 123 Go syntax productions, minus 6 that grammar.md removes (`ConstDecl`, `ConstSpec`, `GotoStmt`, `FallthroughStmt`, `AliasDecl`, `TypeAssertion`), plus 16 that Ergo adds (error row, `needs`, `provides`, wire forms, `catch`, trailing block, `try`, `machine`, tuple cases and the 196p patterns) |
| Decided Go-shaped, sketch at one granularity | 63 | measured: productions in [the first sketch](#decided-go-shaped-sketch) |
| Non-Go, sketch at the same granularity | 64 | measured: productions in [the second sketch](#non-go-sketch) |

The 133 and the two sketches are not comparable: the Go spec splits its productions more finely than the sketches do (inferred). The two sketches were written by one author at one granularity, so they compare with each other: 63 against 64 (measured). The decided stages removed every production by which the Non-Go sketch was shorter before: the three-clause `for`, the `range` clause, the switch guard, `default`, the `if` init statement, grouped imports, the `error[…]` result and the `struct` and `interface` forms. The two name lists now differ by one production (measured by comparing them): only the Non-Go sketch has `Rows` (`-> T fails E needs S`), which its function types, signatures and function literals share. The Decided sketch puts the same optional parts straight into `Signature` and `FuncLit`. Every other production has the same name on both sides, and most have the same body; the differences are `:` before a type and `fn` for `func`. Both inherit Go's lexical grammar, plus the `duration` literal of [grammar.md](../spec/grammar.md#literals).

## Costs

### What agents already know

Today's column borrows Go's prior knowledge, and it inherits Go's false friends. In the [stage-1a ranking](../research/agent-mistakes.md#ranked-language-mistakes), 6 of 11 classes and 10 of 19 instances have a Go habit as their inferred cause (measured count of the ledger; the causes are inferred there). They are duration arithmetic (ranked 1), an error bound as a value (4), a pointer as a value (5), a callback type without `needs` (7), a resource moved twice (8) and comma-ok (9). Rust habits account for 2 classes and 3 instances, Effect or TypeScript habits for 2 classes and 4 instances, and a Koka-style reading of `needs` for 1 class and 2 instances (measured counts, same source).

The decided changes already bring Rust, Swift and Kotlin spellings into both decided columns: `let mut`, `match`, `for x in xs` and `0..n`. The Non-Go signatures look more like Rust and Swift still, so an agent may reach further for `?`, `Result<T, E>`, `=>` arms, `impl` blocks, `self`, `throws`, `val` or `when` (guess; no data yet). Neither decided column has training data of its own. The stage-1a corpus gives two hints in opposite directions (inferred). When the syntax was open, agents wrote non-Go spellings such as `for x in xs` (7 programs) and `type X enum { … }` (12 programs; measured there as old spellings). After the Go-shaped specification landed, the leading mistakes were Go habits. Only the [stage-1b arm](#the-220-stage-1b-arm) can say which effect is larger.

Several ranked mistakes do not depend on the syntax: a prefix `catch` (2), `needs` read as the footprint (3), and `timeout.After` under `Clock` (11). The Non-Go syntax does not remove them (inferred from their causes).

The decided changes move the Go-shaped column far from Go (inferred from the decided stages). `package p`, a grouped and quoted import, `type T struct { … }`, `type I interface { … }`, a `(T, error)` result, `x := e`, `var x = e`, `switch`, `default`, `for i := 0; i < n; i++`, `for _, x := range xs`, `make`, `new`, `append`, `go f()`, `make(chan T)`, `select`, `defer`, a method receiver, a shadowed `err`, `func TestX(t *testing.T)`, a fixed array, a `~T` constraint, a dot import, `any`, `reflect` and `fmt.Sprintf` are all everyday Go. After the decided stages, each one is a mistake that needs a recognising diagnostic (#220 stage 2). So the Go-shaped column keeps `func`, `name Type` and Go's types, but most of Go's prior knowledge now produces mistakes in it. The honest comparison is no longer "Go's prior knowledge against none". It is "a familiar look with many false friends" against "an unfamiliar look with fewer of them" (inferred). The stage-1b arm measures which one agents handle better.

### Interop with Go packages

**Today.** Label: **Proposed** ([grammar.md](../spec/grammar.md#declarations), Declarations rule 3).

```go
import go "strconv" // every call through strconv charges go.Foreign

func parsePort(text string) (int64, error[go.Error]) needs go.Foreign {
	return strconv.ParseInt(text, 10, 64)
}
```

**Decided.** Label: **Proposed** (196v as extended, `fails`, 196x). The import path is unquoted. `extern go` binds one Go function with a stated signature; it replaces the bodiless declaration with a `//ef:go` comment.

```go
import go strconv

func parsePort(text string) int64 fails go.Error needs go.Foreign {
	strconv.ParseInt(text, 10, 64)
}

extern go "strings.ToUpper" func Upper(s string) string
```

**Non-Go.** Label: **Proposed**.

```text
import go strconv

fn parsePort(text: string) -> int64 fails go.Error needs go.Foreign {
	strconv.ParseInt(text, 10, 64)
}

extern go "strings.ToUpper" fn Upper(s: string) -> string
```

- **Signatures at the boundary.** Today, the Ergo view of `strconv.ParseInt` is Go's own signature with rows: `func ParseInt(s string, base int, bitSize int) (int64, error[go.Error]) needs go.Foreign`. After `fails`, the Decided view is `func ParseInt(s string, base int, bitSize int) int64 fails go.Error needs go.Foreign`, so the Decided column already translates Go's `(T, error)` result. The Non-Go column also translates every parameter list to `name: Type` and adds `->`. Both decided columns therefore show a Go declaration in a spelling other than Go's; the Non-Go one moves further (inferred).
- **Prior art.** Gleam's `@external(erlang, "module", "function")` and ReScript's `external name: type = "jsName"` bind host functions with a signature written in the source language's syntax. Both languages pay this translation in every binding, because their syntax differs from their host's. `extern go` is the same mechanism, in either column.
- **Pointers, nil and several results.** These are the same in both syntaxes. A Go `*T` parameter gets an adapter, a Go result that may be nil becomes `Option`, a Go `any` gets an adapter (196d), and a Go `(T, error)` becomes `T fails go.Error`. After 196t, several Go results become one tuple `(A, B)` in both decided columns. Go fixed arrays stay at the boundary only (196v).
- **Generated Go.** The lowered Go is the same. `//line` directives keep pprof, delve and panics on Ergo lines. In both decided columns, the generated Go looks further from its source than today, which matters when a person debugs it (inferred).
### The rework

A switch replaces the surface layer, and keeps the checker if the syntax tree keeps its shape (inferred). Sizes at `56ff92ff` (measured with `wc -l` and `grep -c`):

| Part | Size |
| --- | --- |
| Parser, `internal/sketch/syntax` | 3733 source lines, 776 test lines |
| Formatter, `internal/sketch/format` | 1765 source lines, 744 test lines |
| Prototype checker and bridge, `internal/sketch/proto` | 2742 source lines, 700 test lines; it reads the tree, so it changes only if the tree changes |
| Specification | `grammar.md` (206 lines) rewritten; 21 Go blocks in `docs/spec/`, 9 in the README and 10 in earlier previews to translate |
| Fixtures | 420 tracked `.sketch` files and 7 `.ef` files in 2026-edition modules to translate |
| #220 | the stage-1a ranking re-ranked, and the skill and cheatsheet of stages 4 and 5 written for the new syntax |

A mechanical translator, such as the one the stage-1b harness needs, could convert most fixtures (guess). The decided changes (196u to 196x, with the amendments) land in the Go-shaped language either way, so they are not part of the cost of a switch. They rewrite the same parser, formatter, specification and fixtures, so a switch made together with those stages would share part of that work (inferred).

### Pointers and value semantics

The pointer question is open and syntax-neutral. The surface without `*T` is [#222](https://github.com/cevr/ergo/issues/222). It decides by benchmarks: ns/op, B/op and allocs/op from `go test -bench -benchmem -count=10`, compared with `benchstat`, plus escape analysis. No number exists yet. The Non-Go column keeps `*T` and `&x` until #222 decides, so neither column carries a performance claim here.

## The #220 stage-1b arm

Stage 1b of [#220](https://github.com/cevr/ergo/issues/220) is the live multi-model eval. This arm adds one comparison to it: which syntax agents write correctly more often. The protocol is fixed before the run, so that the decision rule cannot move after the results.

**Arms.** Two syntaxes: Decided Go-shaped, and Non-Go. Both have the same semantics, every decided stage from 196u to 196x included. Run the arm after those stages land, so that `ergo check` judges the Go-shaped arm. 196c waits for the runtime lane. If it is late, run the arm after 196g, leave the concurrency tasks out of both arms, and record that.

**Tasks.** 24 tasks. Each task has a short statement in English, the declarations it needs (types and capabilities) written in the arm's syntax, an expected output or test, and a reference solution in each syntax. The reference solutions must check and run. The tasks cover every construct, and each one targets the stage-1a classes it can provoke:

| Construct | Tasks | Stage-1a classes it can provoke |
| --- | --- | --- |
| Error rows: `fails`, `try`, partial and total `catch`, `catch e` | 4 | prefix-catch, error-value, try-placement |
| Wires as values and `with`, a fallible constructor | 3 | needs-footprint |
| Unions, nested patterns, a tuple `match` | 3 | comma-ok |
| Expression `if` and `match` | 2 | func-literal-return |
| Generics: a constraint, a row-polymorphic callback | 2 | callback-needs |
| Concurrency: `timeout.After`, two children with `task.Go`, `tasks.Map` | 3 | duration, timeout-scheduler |
| Resources: a lend, a move | 2 | resource-move |
| Tests and laws: a `test` with `assert`, a `law` in an operation type | 2 | error-value |
| Go interop with `import go` and `extern go` | 1 | pointer-value |
| One function form: a provider, a dot call | 2 | pointer-value |

**Models.** At least two families, the newest release of each: the newest Claude and the newest Codex model at `high`. A third family may join if it costs nothing extra.

**Prompt.** A spec excerpt per syntax, plus the task. The two excerpts state the same rules with the same examples, translated, and their lengths match within 5% in one stated tokenizer (measured before the run). The prompt shows no code in the other syntax. The run is first-try only: no `ergo check` in the loop, because the Non-Go arm has no checker to loop on. Three samples per task, arm and model: 24 × 3 × 2 × 2 = 288 programs.

**Judging the Non-Go arm.** The Non-Go syntax has no checker. Two options:

1. **A written rubric** that graders apply to each program. It is cheap to build. But it judges the two arms by different means, and graders miss row and resource errors (inferred from the 3 undetected classes of stage 1a).
2. **A throwaway parser in the eval harness** (recommended). It parses the Non-Go sketch grammar of this preview and prints the same tree as Go-shaped text. Then `ergo check` judges both arms. A program that the parser refuses counts as a syntax mistake, the equivalent of S001. Before the run, the harness translates the 24 Non-Go reference solutions, and each translation must check. During the run, a person audits 20 translations chosen at random. A translator bug voids the runs it touched, and they run again.

The parser lives in the eval harness, not in the compiler, and it is deleted with the eval unless the decision adopts the syntax.

**Metrics,** per arm and model:

- the first-try pass rate: the program checks with no diagnostic and gives the expected output;
- mistakes per task and per 100 non-blank lines, each distinct diagnostic counted once;
- the mistake class of each, by the stage-1a classes, plus four new ones: Rust habit, Swift or Kotlin habit, a Go spelling inside the Non-Go syntax (`func`, `name Type` parameters, a result without `->`), and an error of the eval translator. Go habits that both decided columns refuse (`:=`, `switch`, `range`, `make`, `struct`, `(T, error)`, `package`) count in the stage-1a style in both arms.

**Decision rule.** The eval justifies a switch to the Non-Go syntax only if all of these hold:

1. On each model, the Non-Go first-try pass rate is higher by at least 15 percentage points. The threshold is a guess at what pays for the rework; fix it before the run.
2. The 95% bootstrap interval of the paired difference, resampled over tasks, excludes 0.
3. No stage-1a class of harm 3 occurs more often in the Non-Go arm.

Otherwise Ergo keeps the Go-shaped syntax with the decided changes. A tie keeps it too, because the Decided syntax needs no rework beyond the decided stages and keeps Go's parameter lists at the interop boundary. With 72 programs per arm and model, the eval can only detect a large difference (guess, no power calculation yet). The two decided columns now differ mostly in signatures, so a difference of 15 points is unlikely, and the expected outcome is to keep the Decided syntax (guess).

## Prior art

One line for each language: what it does, what this preview takes or refuses, and how it compiles to its host where it has one.

- **Gleam:** `fn f(x: T) -> R`, `let`, `case` as an expression with labelled-field patterns, no methods, qualified imports, and `@external` bindings with typed signatures. It compiles to Erlang source and to JavaScript, and lowers nested patterns to decision trees. The Non-Go column takes `name: Type`, `->` and `let`; Gleam's `Result` is a value, while Ergo's rows are erased.
- **Rust:** `fn`, `let` and `let mut`, tail expressions, `if` and `match` as expressions with `_` as the catch-all, struct patterns, `for x in xs` and `0..n`, and a postfix `?`. Native code through LLVM. 196r takes the tail rule, the amended 196p takes `match` and `_`, and the amended 196v takes `let`, `let mut`, `in` and `..`, without Rust's shadowing. The postfix `?` is refused because of ranked mistake 6.
- **Swift:** `let` and `var`, typed throws `throws(E)`, `if` and `switch` expressions (5.9), trailing closures, task groups, `\(…)` interpolation and non-escaping closures by default with `@escaping`. Swift 3 removed the C-style `for` and `++`/`--` (SE-0007, SE-0004). Native code through LLVM. Prior art for 196s, 196e and the one `for`; its `throws(E)` after the parameters is prior art for `fails`.
- **Kotlin:** `val` and `var`, the `when` expression, trailing lambdas, extension functions resolved statically, and `coroutineScope` with `withTimeout`. It compiles to JVM bytecode, JavaScript and native code. It is prior art for value `if` (196r) and for static dot calls (196m).
- **Koka:** one effect row merged with the result, `fun f(x : int) : <exn, console> int`, and dot selection, `x.f(y)` for `f(x, y)`. It compiles to C with Perceus reference counting, and to JavaScript. Its row is the merged-row option, which the owner rejected; its dot selection is prior art for 196m.
- **Unison:** abilities in one row on the arrow, `Text ->{IO, Exception} Nat`. It runs on its own runtime, not on a host language. Prior art for the merged row.
- **Effekt:** an effect set after the result, `def f(): Int / { Exc }`, in a brace syntax. It compiles to JavaScript, Chez Scheme and LLVM. The merged-row snippet uses its `/` spelling.
- **Roc:** static dispatch, where `x.f(y)` resolves `f` in the module of `x`'s type; `provides` and `requires` between an app and its platform; no pointers in the surface. It compiles to machine code. Prior art for 196m, for `needs` and `provides`, and for #222.
- **OCaml and ReScript:** `let`, `match … with` and `switch`, variant and record patterns, `external` bindings, and in ReScript the pipe `x->f(y)` for `f(x, y)`. OCaml compiles to native code and bytecode; ReScript compiles to readable JavaScript. The pipe is a syntactic rewrite with no type-directed lookup, unlike 196m.
- **Elixir:** keyed struct patterns `%Authorized{auth_id: id}`, and `|>` into the first argument. It compiles to BEAM through Erlang's abstract format. Prior art for the keyed patterns of 196p. Its `with` is a different construct with the same word.
- **MoonBit:** `fn f(x : Int) -> Int raise E` with checked errors, `let` and `let mut`, and `fn Type::method` with dot calls, where only the type's package defines methods. It compiles to Wasm, JavaScript, C and LLVM. Prior art for the owner-package rule of 196m and for same-named functions on different types. It reports a missing case as a warning, which Ergo refuses.
- **Zig:** error unions `E!T` with `try` and `catch`, `const` and `var` bindings, and the untyped `comptime_int`. It compiles through LLVM and its own backends, C included. Its error union in the result is prior art for `fails`, and its `test "name" { }` blocks with `std.testing` are prior art for `test` (196l); its `comptime_int` is the counter-example that 196u refuses.
- **Nim and D (UFCS):** `x.f(y)` is `f(x, y)` for any function in scope. Nim compiles to C, C++ and JavaScript; D compiles to native code. 196m narrows the lookup to the package of `x`'s type, so a dot never reaches an unrelated import.
- **Go:** the baseline of Today: `:=`, receivers, type switches, untyped constants, and `func` signatures whose last result is `error`. The Decided column keeps Go's declaration look, its types and its lowering target, and little of its statement syntax.

## What this preview does not claim

- No switch to the Non-Go syntax is decided. The stage-1b arm decides it.
- The Non-Go syntax has no parser, no formatter and no checker. Its snippets are unchecked, and so are both grammar sketches.
- The decided changes are not in `docs/spec/` or the checker yet. Their snippets are Proposed until their stages land.
- The counts are not a usability result. Fewer tokens are not shown to mean fewer mistakes.
- No speed claim is made. The speed levers are on #190, and the pointer and number-type questions wait for #222.

## Appendix: two grammar sketches

The two sketches cover the same forms at one granularity, so their production counts compare. Both include the decided stages: patterns, the union form, `match`, expression `if`, no receivers, `let` and `let mut`, one `for`, tuples, qualified imports with one unquoted path per line and no `package` line, no fixed arrays, no labels, no `++` or `--`, no `go`, `chan`, `select`, `<-` or `defer`, the `escaping` marker, and the fifth round: one `type` shape whose members (fields, or operations and laws) decide its kind, the `machine` declaration, `fails`, `test`, `assert`, `law`, `extern go`, wire values with `with`, and function literals with untyped parameters (#214). A function declaration needs a body; a bodiless Go binding is an `extern go` declaration. Wire entries use `:`, and `wire` may state `provides` and `needs` (both inferred). Both defer to Go's lexical grammar (identifiers, literals, `binary_op`, `unary_op`, `assign_op`). Both add the `duration` literal, the `..` range operator and `\( … )` interpolation inside an interpreted string (196s). Neither has an `if` init statement, the open point of the [bindings section](#bindings). Neither sketch is checked by a parser.

### Decided Go-shaped sketch

<!-- count: grammar/decided -->
```ebnf
SourceFile     = { ImportDecl ";" } { TopLevelDecl ";" } .
ImportDecl     = "import" [ "go" ] [ identifier ] import_path .
TopLevelDecl   = TypeDecl | FuncDecl | ExternDecl | TestDecl | LetDecl .
TypeDecl       = "type" identifier [ TypeParams ] ( Members | MachineType | "=" UnionType | Type ) .
TypeParams     = "[" TypeParam { "," TypeParam } "]" .
TypeParam      = identifier { "," identifier } [ TypeName ] .
Members        = "{" { Member ";" } "}" .
Member         = FieldDecl | identifier Signature | LawDecl .
FieldDecl      = identifier { "," identifier } Type .
LawDecl        = "law" identifier Params Block .
UnionType      = [ "|" ] Variant { "|" Variant } .
Variant        = TypeName [ TypeArgs ] [ "{" [ FieldDecl { ";" FieldDecl } ] "}" ] .
MachineType    = "machine" TypeArgs "{" { MachineEntry ";" } "}" .
MachineEntry   = identifier Expression .
Type           = TypeName [ TypeArgs ] | SliceType | MapType | FuncType | PointerType | "(" Type { "," Type } ")" .
TypeName       = identifier [ "." identifier ] .
TypeArgs       = "[" Type { "," Type } "]" .
SliceType      = "[" "]" Type .
MapType        = "map" "[" Type "]" Type .
PointerType    = "*" Type .
FuncType       = [ "escaping" ] "func" Signature .
FuncDecl       = "func" identifier [ TypeParams ] Signature Block .
ExternDecl     = "extern" "go" string_lit "func" identifier [ TypeParams ] Signature .
TestDecl       = "test" identifier [ Params ] Block .
Signature      = Params [ Type ] [ FailsClause ] [ NeedsClause ] .
Params         = "(" [ Param { "," Param } [ "," ] ] ")" .
Param          = [ identifier { "," identifier } ] [ "..." ] Type .
FailsClause    = "fails" TypeName [ TypeArgs ] { "|" TypeName [ TypeArgs ] } .
NeedsClause    = "needs" TypeName { "," TypeName } .
LetDecl        = "let" [ "mut" ] Pattern [ Type ] [ "=" Expression ] .
ExpressionList = Expression { "," Expression } .
ProvidesClause = "provides" TypeName { "," TypeName } .
WireBody       = "{" { WireEntry ";" } "}" .
WireEntry      = [ "replace" ] TypeName { "," TypeName } ":" Expression | TypeName .
Block          = "{" { Statement ";" } "}" .
Statement      = LetDecl | Expression | Assignment | ReturnStmt | "break" | "continue" | ForStmt | "assert" Expression .
Assignment     = Expression assign_op Expression .
ReturnStmt     = "return" [ Expression ] .
ForStmt        = "for" [ Expression | Pattern { "," Pattern } "in" Expression ] Block .
Expression     = [ "try" ] CatchExpr .
CatchExpr      = Binary [ "catch" [ identifier ] "{" { CaseClause } "}" ] .
Binary         = Unary { binary_op Unary } .
Unary          = { unary_op } Postfix .
Postfix        = Primary { Selector | Index | Slice | Call } .
Selector       = "." identifier .
Index          = "[" ExpressionList "]" .
Slice          = "[" [ Expression ] ":" [ Expression ] [ ":" Expression ] "]" .
Call           = "(" [ ExpressionList [ "..." ] [ "," ] ] ")" [ Block ] .
Primary        = Literal | identifier | CompositeLit | FuncLit | IfExpr | MatchExpr | WithExpr | WireLit | "(" ExpressionList ")" .
Literal        = int_lit | float_lit | rune_lit | string_lit | duration_lit .
CompositeLit   = LiteralType LiteralValue .
LiteralType    = TypeName [ TypeArgs ] | SliceType | MapType .
LiteralValue   = "{" [ Element { "," Element } [ "," ] ] "}" .
Element        = [ ( Expression | LiteralValue ) ":" ] ( Expression | LiteralValue ) .
FuncLit        = "func" "(" [ LitParam { "," LitParam } ] ")" [ Type ] [ FailsClause ] [ NeedsClause ] Block .
LitParam       = identifier [ Type ] .
IfExpr         = "if" Expression Block [ "else" ( IfExpr | Block ) ] .
MatchExpr      = "match" Expression "{" { CaseClause } "}" .
CaseClause     = "case" Pattern { "," Pattern } ":" { Statement ";" } .
WithExpr       = "with" ExpressionList Block .
WireLit        = "wire" [ ProvidesClause ] [ NeedsClause ] WireBody .
Pattern        = "_" | identifier | Literal | TypeName [ TypeArgs ] [ "{" [ FieldPattern { "," FieldPattern } ] "}" ] | "(" Pattern { "," Pattern } ")" .
FieldPattern   = identifier ":" Pattern .
```

### Non-Go sketch

<!-- count: grammar/nongo -->
```ebnf
SourceFile     = { ImportDecl ";" } { TopLevelDecl ";" } .
ImportDecl     = "import" [ "go" ] [ identifier ] import_path .
TopLevelDecl   = TypeDecl | FuncDecl | ExternDecl | TestDecl | LetDecl .
TypeDecl       = "type" identifier [ TypeParams ] ( Members | MachineType | "=" UnionType | Type ) .
TypeParams     = "[" TypeParam { "," TypeParam } "]" .
TypeParam      = identifier [ ":" TypeName ] .
Members        = "{" { Member ";" } "}" .
Member         = FieldDecl | identifier Signature | LawDecl .
FieldDecl      = identifier ":" Type .
LawDecl        = "law" identifier Params Block .
UnionType      = [ "|" ] Variant { "|" Variant } .
Variant        = TypeName [ TypeArgs ] [ "{" [ FieldDecl { "," FieldDecl } ] "}" ] .
MachineType    = "machine" TypeArgs "{" { MachineEntry ";" } "}" .
MachineEntry   = identifier Expression .
Type           = TypeName [ TypeArgs ] | SliceType | MapType | FuncType | PointerType | "(" Type { "," Type } ")" .
TypeName       = identifier [ "." identifier ] .
TypeArgs       = "[" Type { "," Type } "]" .
SliceType      = "[" "]" Type .
MapType        = "map" "[" Type "]" Type .
PointerType    = "*" Type .
FuncType       = [ "escaping" ] "fn" "(" [ Type { "," Type } ] ")" Rows .
FuncDecl       = "fn" identifier [ TypeParams ] Signature Block .
ExternDecl     = "extern" "go" string_lit "fn" identifier [ TypeParams ] Signature .
TestDecl       = "test" identifier [ Params ] Block .
Signature      = Params Rows .
Params         = "(" [ Param { "," Param } [ "," ] ] ")" .
Param          = ( identifier | "_" ) ":" [ "..." ] Type .
Rows           = [ "->" Type ] [ FailsClause ] [ NeedsClause ] .
FailsClause    = "fails" TypeName [ TypeArgs ] { "|" TypeName [ TypeArgs ] } .
NeedsClause    = "needs" TypeName { "," TypeName } .
LetDecl        = "let" [ "mut" ] Pattern [ ":" Type ] [ "=" Expression ] .
ExpressionList = Expression { "," Expression } .
ProvidesClause = "provides" TypeName { "," TypeName } .
WireBody       = "{" { WireEntry ";" } "}" .
WireEntry      = [ "replace" ] TypeName { "," TypeName } ":" Expression | TypeName .
Block          = "{" { Statement ";" } "}" .
Statement      = LetDecl | Expression | Assignment | ReturnStmt | "break" | "continue" | ForStmt | "assert" Expression .
Assignment     = Expression assign_op Expression .
ReturnStmt     = "return" [ Expression ] .
ForStmt        = "for" [ Expression | Pattern { "," Pattern } "in" Expression ] Block .
Expression     = [ "try" ] CatchExpr .
CatchExpr      = Binary [ "catch" [ identifier ] "{" { CaseClause } "}" ] .
Binary         = Unary { binary_op Unary } .
Unary          = { unary_op } Postfix .
Postfix        = Primary { Selector | Index | Slice | Call } .
Selector       = "." identifier .
Index          = "[" ExpressionList "]" .
Slice          = "[" [ Expression ] ":" [ Expression ] [ ":" Expression ] "]" .
Call           = "(" [ ExpressionList [ "..." ] [ "," ] ] ")" [ Block ] .
Primary        = Literal | identifier | CompositeLit | FuncLit | IfExpr | MatchExpr | WithExpr | WireLit | "(" ExpressionList ")" .
Literal        = int_lit | float_lit | rune_lit | string_lit | duration_lit .
CompositeLit   = LiteralType LiteralValue .
LiteralType    = TypeName [ TypeArgs ] | SliceType | MapType .
LiteralValue   = "{" [ Element { "," Element } [ "," ] ] "}" .
Element        = [ ( Expression | LiteralValue ) ":" ] ( Expression | LiteralValue ) .
FuncLit        = "fn" "(" [ LitParam { "," LitParam } ] ")" Rows Block .
LitParam       = identifier [ ":" Type ] .
IfExpr         = "if" Expression Block [ "else" ( IfExpr | Block ) ] .
MatchExpr      = "match" Expression "{" { CaseClause } "}" .
CaseClause     = "case" Pattern { "," Pattern } ":" { Statement ";" } .
WithExpr       = "with" ExpressionList Block .
WireLit        = "wire" [ ProvidesClause ] [ NeedsClause ] WireBody .
Pattern        = "_" | identifier | Literal | TypeName [ TypeArgs ] [ "{" [ FieldPattern { "," FieldPattern } ] "}" ] | "(" Pattern { "," Pattern } ")" .
FieldPattern   = identifier ":" Pattern .
```

Notes on both sketches:

- `for x in xs` and `for i, x in xs` iterate a slice or a map. `for i in 0..n` iterates a half-open `Range` value. There is no three-clause `for`, and no `++` or `--`.
- `match` has a subject; several subjects form a tuple. A tagless choice is an `if`/`else` chain.
- In both sketches, `with` takes wires and context values ([runtime.md](../spec/runtime.md#g1-context-through-with)), and its block is part of the construct. Today `with(…) { … }` is a call with a trailing block; 196x makes `with wire { … } { … }` a form of its own, so `with` drops its parentheses (inferred).
- One `TypeDecl` covers records, operation types, defined types, unions and machines. The members decide the kind: a type with fields is a record, and a type with operations is an operation type. A type that mixes fields and operations is S108, and a `law` is a member of an operation type only; the sketch does not express these rules. `=` always means a union, so there are no type aliases.
- A `law` member has parameters and a block that yields `bool`. A `test` with parameters is a property, run with generated inputs.

## Reproduction

- **Checked snippets.** At commit `56ff92ff`: `go build -o ergo ./cmd/ergo`. Put each Checked file in its own directory as `main.ef`, beside an `ergo.json` of `{"edition": "2026"}`. Then `ergo check main.ef` exits 0, `ergo fmt --check main.ef` exits 0, and `ergo run main.ef` prints the output named above (measured on 2026-10-28).
- **Code counts.** A short script reads every block that a `<!-- count: NAME -->` comment precedes in this file's source. It strips `//` comments outside string literals and drops blank lines. It then counts lines, words split on white space (as `wc -w` does), lexical tokens and distinct keywords. The lexical tokens are the matches of this regular expression: string literals, identifiers and keywords, a number with an optional unit, the operators `:= == != <= >= && || -> <- ... ++ --`, and single punctuation marks. A block named `…/today` uses today's 28 keywords, `…/decided` the Decided list and `…/nongo` the Non-Go list, as listed under [Keywords](#keywords).
- **Grammar counts.** In an `ebnf` block, a production is a line that starts with a name followed by `=`. For `docs/spec/grammar.md`, the same rule runs over its `ebnf` blocks. For the Go spec, the names come from its `ebnf` blocks at go1.27.
- **Sizes.** `wc -l` over the non-test and test `.go` files of each package; `grep -c '^```go'` over `docs/spec/*.md`, `README.md` and `docs/previews/*.md`; `git ls-files '*.sketch'`; and the `.ef` files beside an `ergo.json` that names the 2026 edition.
