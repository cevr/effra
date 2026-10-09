# Effra

**Go's directness and native binaries. Effect's typed failures, explicit dependencies and owned lifetimes. Algebraic data types with exhaustive matching. One small language.**

Effra is an experimental language for servers. You write `.ef` files and `ef` compiles them to a single static Go executable, or to JavaScript built on [Effect](https://effect.website). Every function signature states what it returns, every way it can fail and every service it needs, and the compiler checks all three.

> **Status: runnable prototype.** Everything in the first two sections compiles and runs today. The gate checks every Effra snippet that isn't labelled as a sketch. Syntax and APIs will still change.

## One program, three languages

Here is a checkout step written three ways: look up an order, ask a payment gateway to authorize it within 500 ms, and describe the result. The payment result is a closed sum type, and every failure is a named, typed error. All three versions are checked in and print the same thing.

### Effra

```rust
error OrderNotFound { id: string }
error GatewayDown

record Order {
    id: string
    total: i64
}

enum Payment {
    Pending
    Authorized { authId: string }
    Declined { reason: string }
}

service Orders {
    effect fn find(id: string) -> Order raises { OrderNotFound }
}

service Gateway {
    effect fn authorize(order: Order) -> Payment raises { GatewayDown }
}

effect fn checkout(id: string) -> string
    raises { OrderNotFound, GatewayDown, Timeout }
    uses { Orders, Gateway, Scheduler }
{
    let order = run Orders.find(id)
    let payment = run Gateway.authorize(order).timeout(500)
    match payment {
        Payment.Pending => "pending"
        Payment.Authorized { authId } => "paid " + authId
        Payment.Declined { reason } => "declined: " + reason
    }
}
```

The signature tells the whole story. `checkout` returns a `string`. It can fail with exactly `OrderNotFound`, `GatewayDown` or `Timeout`. It needs `Orders`, `Gateway` and `Scheduler`, the last because `.timeout` needs a clock to race against. The body is ordinary top-to-bottom code: `run` executes a deferred effect, and the match is exhaustive. Calling `checkout("42")` only builds a lazy program. The caller decides where it runs and what it's provided with ([full program](examples/checkout.ef)):

```rust
// From examples/checkout.ef
layer Live {
    Orders = MemoryOrders
    Gateway = FakeGateway("auth-7")
    Scheduler = LiveScheduler
}
```

### TypeScript with Effect 4

```ts
// From examples/compare/checkout.ts
import { Cause, Context, Data, Effect, Layer } from "effect"

class OrderNotFound extends Data.TaggedError("OrderNotFound")<{ readonly id: string }> {}
class GatewayDown extends Data.TaggedError("GatewayDown") {}

interface Order {
  readonly id: string
  readonly total: number
}

type Payment = Data.TaggedEnum<{
  Pending: {}
  Authorized: { readonly authId: string }
  Declined: { readonly reason: string }
}>
const Payment = Data.taggedEnum<Payment>()

class Orders extends Context.Service<Orders, {
  readonly find: (id: string) => Effect.Effect<Order, OrderNotFound>
}>()("Orders") {}

class Gateway extends Context.Service<Gateway, {
  readonly authorize: (order: Order) => Effect.Effect<Payment, GatewayDown>
}>()("Gateway") {}

// Inferred: Effect<string, OrderNotFound | GatewayDown | TimeoutError, Orders | Gateway>
const checkout = (id: string) =>
  Effect.gen(function* () {
    const orders = yield* Orders
    const gateway = yield* Gateway
    const order = yield* orders.find(id)
    const payment = yield* gateway.authorize(order).pipe(Effect.timeout("500 millis"))
    return Payment.$match(payment, {
      Pending: () => "pending",
      Authorized: ({ authId }) => `paid ${authId}`,
      Declined: ({ reason }) => `declined: ${reason}`
    })
  })
```

Effect gives you the same guarantees, and Effra borrows its model directly. The cost is in the encoding: service classes declared through a two-stage generic, generators with `yield*`, `pipe`, and a contract that is inferred rather than written down. You only see that contract by hovering.

### Go

```go
// From examples/compare/go/main.go
type OrderNotFoundError struct{ ID string }

func (e *OrderNotFoundError) Error() string { return "order not found: " + e.ID }

var ErrGatewayDown = errors.New("gateway down")

type Order struct {
	ID    string
	Total int64
}

// Payment is a closed set only by convention: any type with isPayment satisfies it.
type Payment interface{ isPayment() }

type Pending struct{}
type Authorized struct{ AuthID string }
type Declined struct{ Reason string }

func (Pending) isPayment()    {}
func (Authorized) isPayment() {}
func (Declined) isPayment()   {}

type Orders interface {
	Find(ctx context.Context, id string) (Order, error)
}

type Gateway interface {
	Authorize(ctx context.Context, order Order) (Payment, error)
}

// Checkout can fail with *OrderNotFoundError, ErrGatewayDown or context.DeadlineExceeded,
// but the signature only says error, and only this comment says which.
func Checkout(ctx context.Context, orders Orders, gateway Gateway, id string) (string, error) {
	order, err := orders.Find(ctx, id)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	payment, err := gateway.Authorize(ctx, order)
	if err != nil {
		return "", err
	}
	switch p := payment.(type) {
	case Pending:
		return "pending", nil
	case Authorized:
		return "paid " + p.AuthID, nil
	case Declined:
		return "declined: " + p.Reason, nil
	default: // the compiler cannot prove this unreachable; nil also lands here
		return "", fmt.Errorf("unknown payment %T", payment)
	}
}
```

Go's version is the most direct to read, and Effra keeps that directness: plain statements, no generators, no combinator chains. What Go can't tell you is which errors come back, whether the switch covers every case, or what a nil `Payment` would do.

### Same mistakes, three compilers

Each row makes one mistake in the program above and records what happens. The Effra column is the actual `ef diagnostics` output, verified by the gate ([`TestReadmeSmoke`](cmd/ef/readme_smoke_test.go)).

| Mistake | Go | TypeScript + Effect | Effra |
| --- | --- | --- | --- |
| Forget the `Declined` case | Compiles; falls into `default` at runtime | Type error | `EF117: missing match arm for Payment.Declined` |
| Add the timeout but don't declare it | Compiles; a new error appears at runtime | Compiles; the inferred error union silently widens | `EF107: undeclared failures: Timeout` |
| A caller forgets to handle `Timeout` | Compiles | Compiles unless a return type is written by hand | `EF107: undeclared failures: Timeout` |
| Forget to wire the `Gateway` implementation | Compiles if `nil` is passed or a struct field is left unset; panics when called | Type error at `runPromise` | `EF108: missing service requirements: Gateway` |
| Build an `Order` without its `total` | Compiles; `total` is silently `0` | Type error | `EF114: missing payload field total` |
| Typo a variant field (`auth` for `authId`) | Compile error | Type error | `EF114: unknown payload field auth` |

### At a glance

| | Go | TypeScript + Effect | Effra |
| --- | --- | --- | --- |
| Which failures can happen | `error`; read the body | Inferred `E` type parameter | Declared in `raises { … }` and checked |
| What a function needs | Parameters, struct fields, `context` | Inferred `R` type parameter | Declared in `uses { … }` and checked |
| Sum types | Interface plus marker method, open to anyone | `Data.TaggedEnum` plus `$match` | `enum` with payloads; `match` is exhaustive |
| Absence | `nil` pointers, interfaces, maps and slices | `null`/`undefined` (strict mode tracks them) | No null; absence is `Option<T>` |
| Control flow | Plain statements | Generators, `yield*`, `pipe` | Plain statements; `run` marks each effect |
| Concurrency | Goroutines plus `context`; you own cleanup | Fibers with structured interruption | `scope`, `fork`, `join`, `interrupt`; a scope waits for its children's cleanup |
| Dependency wiring | Constructors written by hand | `Layer` values | `layer` declarations the compiler checks and can graph |
| Output | Static native binary | JavaScript on Node, Bun or a browser | Static native Go binary, or JavaScript on Effect |

## A tour of what works today

### Data without null

Records, enums with payloads and generic types compose, and nothing is ever `nil`. The bundled `Data` module supplies `Option` and `Result`:

```rust
import Data "effra/data"

record User {
    name: string
}

fn label(user: Data.Option<User>) -> string {
    match user {
        Data.Option.None => "missing"
        Data.Option.Some { value: found } => found.name
    }
}

fn lookup(id: string) -> Data.Option<User> {
    if id == "42" {
        Data.Option.Some { value: User { name: "Ada" } }
    } else {
        Data.Option<User>.None {}
    }
}
```

### Matching several values at once

`match` takes several subjects and checks every combination of their variants. An arm can list alternatives with `|`, and a name bound in every alternative is bound once. Here a session's transitions are one table:

```rust
enum Session {
    Idle
    Active { key: string }
    Closed
}

enum Event {
    Open { key: string }
    Refresh { key: string }
    Close
}

enum Step {
    Go { next: Session }
    Stay
    Reject { reason: string }
}

fn step(state: Session, event: Event) -> Step {
    match state, event {
        Session.Idle, Event.Open { key } | Event.Refresh { key } => Step.Go { next: Session.Active { key } }
        Session.Active { key: current }, Event.Open { key } => if current == key { Step.Stay {} } else { Step.Reject { reason: "busy " + current } }
        Session.Active, Event.Refresh { key } => Step.Go { next: Session.Active { key } }
        Session.Idle | Session.Active, Event.Close => Step.Go { next: Session.Closed {} }
        Session.Closed, Event.Open | Event.Refresh | Event.Close => Step.Reject { reason: "closed" }
    }
}
```

Leave out a pair, for example by dropping `Session.Active` from the `Event.Close` arm, and the check fails with `EF117`, naming the missing combination.

### Errors with payloads, and recovery that removes them

Errors are nominal and can carry fields. `.catch<E>(fallback)` removes exactly `E` from the failure row and leaves the rest to the type checker:

```rust
// From examples/checkout.ef
effect fn report(id: string) -> void uses { Console } {
    let outcome = run checkout(id).provide(Live)
        .catch<OrderNotFound>("no such order")
        .catch<GatewayDown>("gateway down")
        .catch<Timeout>("gateway timed out")
    run Console.log(outcome)
}
```

`report` declares no failures, so deleting any one of those `.catch` lines is a compile error.

### Layers: dependency graphs the compiler can see

A `layer` binds services to implementations. Layers merge by identity, so a store shared by two domains is built once. A fixture swaps one node for the whole graph:

```rust
// From examples/layers.ef
layer Shared {
    Store = Memory("live")
}
layer Accounts {
    merge Shared;
    Account = AccountLive
}
layer Invoices {
    merge Shared;
    Invoice = InvoiceLive
}
layer App provides { Account, Invoice } {
    merge Accounts, Invoices
}
layer Fixture {
    merge App;
    replace Store = Memory("fixture")
}
```

`provides` hides `Store` from callers. `replace` happens before anything is constructed, and `ef graph` shows the selected graph.

### Structured concurrency

Children belong to a scope. Interrupting one waits for its cleanup to finish before the next line runs, on both targets:

```rust
// From examples/latest-task.ef
effect fn replacement() -> string uses { Clock } {
    scope {
        let previous = fork search("old")
        run previous.interrupt()
        let current = fork search("new")
        run current.join()
    }
}
```

### Go packages, without writing bindings

The compiler reads signatures straight from Go export data. Native calls are deferred, need an explicit `Foreign` capability, and keep Go's error as data until you choose to raise it:

```rust
import go strconv "strconv"

effect fn parse(text: string) -> bool raises { GoError } uses { Foreign } {
    run strconv.ParseBool(text).orFail()
}
```

### Tests are ordinary effects

Tests are effect functions whose names start with `test_`. A fixture is just another provider, and each test gets a fresh owning scope. `ef test` runs them on Go, and `ef test --target js` runs them on JavaScript:

```rust
// From examples/testing.ef
effect fn test_recovery() -> void raises { AssertionFailed } uses { Assert } {
    let actual = run greeting("unknown")
        .provide<Directory>(FixtureDirectory)
        .catch<Missing>("Unknown user")
    run Assert.equalText(actual, "Unknown user")
}
```

### One compiler model for people, editors and agents

`ef check`, `inspect`, `explain`, `graph`, `diagnostics`, `lint`, `lsp` and `mcp` all read the same checked model. `ef fmt` works from syntax alone, so it also formats code that does not type-check yet. Ask why `checkout` needs what it needs:

```sh
$ ef explain examples/checkout.ef checkout | jq -c '.symbol.contributions[] | {line: .span.line, kind, names}'
{"line":33,"kind":"failure","names":["OrderNotFound"]}
{"line":33,"kind":"requirement","names":["Orders"]}
{"line":34,"kind":"failure","names":["GatewayDown","Timeout"]}
{"line":34,"kind":"requirement","names":["Gateway","Scheduler"]}
```

Line 33 is `run Orders.find(id)`; line 34 is the gateway call with its timeout. The MCP server exposes the same facts to coding agents. Its semantic answers carry the source revision they were computed from, and a request that passes `expectedRevision` is refused if the source has changed since. Without it, the server answers from the current source, except that a type lookup by definition ID requires `expectedRevision` (an ID is only meaningful for the source it came from). Editors get the same model through `ef lsp`: diagnostics, hover and go-to-definition answer from the query behind `ef type --offset`, and document formatting is the `ef fmt` formatter. Range formatting, references and rename are not implemented yet.

## Where it's going

Each row states what has landed on this branch, what is in flight and what is only specified or designed. A feature ships only when it runs on its advertised targets, with diagnostics and tooling support.

| Capability | Status |
| --- | --- |
| Payload-aware recovery: `.recover<E>(handler)` passes the error's fields to an effectful handler | Landed on Go and JS ([`recovery-outcome.ef`](examples/recovery-outcome.ef), [`recovery-codec.ef`](examples/recovery-codec.ef)); reified `Exit` is not exposed |
| Layer runtime: concurrent shared acquisition, rollback and cleanup in reverse order | Shared build-owned acquisition with individually cancellable waiters landed on Go and JS; the remaining units are in progress |
| Codecs derived from records and enums, with explicit wire ↔ domain transformations | Structural JSON derivation through [`effra/json`](docs/bundled-modules.md) runs on Go and JS; transformations in progress |
| Graph views: `ef graph --kind layers --format mermaid\|dot`, plus MCP and editor views | In progress |
| HTTP server with bounded, owned shutdown (`Http.listen`) | Landed on Go and JS ([`http-transport.ef`](examples/http-transport.ef)); typed endpoints, streaming bodies and codecs are not built |
| Rich Go interop: host types, methods, `io.Reader`/`io.Writer`, `context` | Host types, methods, interface assignment, checked I/O and `context` forwarding landed; owned resources and callbacks in progress |
| Library modules: `pub` exports and module-qualified identity | Designed |
| State machines: ordinary step functions run by an owned runtime | [Specified](docs/specs/state-machines.md); not built |
| Actors with explicit mailbox budgets and owned behavior | [Specified](docs/specs/actors.md); not built |
| Evidence and proofs: values that carry what was checked | [Designed](docs/research/opaque-values-and-evidence.md) |

### Proofs you can't forget to check

Following *Ghosts of Departed Proofs* and Bend's laws-as-obligations, the plan is for the result of a check to become a value that only its owning module can construct. The sensitive operation then takes that value instead of a raw ID:

```rust
// Sketch: not implemented; syntax will change.
pub readonly record RefundGrant {
    order: OrderId
    actor: UserId
}

effect fn authorizeRefund(actor: User, order: Order) -> RefundGrant
    raises { Forbidden } uses { Policy }

effect fn refund(grant: RefundGrant) -> Receipt raises { GatewayDown } uses { Gateway }
```

With this in place:
- Skipping authorization becomes a type error.
- A grant names the exact order and actor that were approved, so it can't be swapped for another order.
- The evidence compiles down to the plain IDs it holds, with no proof objects at runtime.

Service laws, such as "decode after encode returns the input", become obligations that every implementation must pass. They are reported honestly as *tested*, never as proved.

### State machines from ordinary functions

The transition table in [the tour](#matching-several-values-at-once) already compiles: a multi-subject match whose every state and event pair is checked. Running it as a machine is specified but not built. In [the machine spec](docs/specs/state-machines.md), an owned runtime admits events, runs each step and owns the work each state starts, as one form of [actor](docs/specs/actors.md) behavior. A step can be a pure function or an effect with its own failures and services.

A machine will be a small `machine` declaration that names its initial state and its ordinary step functions. The declaration earns its place because the compiler checks something no library can: a `Stay` decision must keep the state's variant. Behavior stays in ordinary functions.

## Try it

You need Go 1.27+ and Bun; the full gate also needs Python 3.

```sh
git clone https://github.com/cevr/effra.git
cd effra
bun install --frozen-lockfile
go build -o bin/ef ./cmd/ef

./bin/ef run examples/checkout.ef               # paid auth-7 / no such order
./bin/ef run examples/checkout.ef --target js   # same output, on Effect
./bin/ef build examples/checkout.ef             # static executable in dist/
./bin/ef diagnostics examples/missing-service.ef  # expected failure: EF108
./bin/ef inspect examples/checkout.ef checkout  # the checked contract as JSON
./bin/ef graph examples/layers.ef               # services, providers and layers
./bin/ef test examples/testing.ef
./bin/ef mcp .                                  # compiler tools for agents over stdio
./bin/ef lsp                                    # diagnostics, hover, definition and formatting over LSP
```

Native builds lower checked source into typed Go closures and run `go build`. The executable needs no Effra, Bun, Node or Effect installation to run, and contains only the declarations and runtime modules its entry point reaches. The JavaScript target emits code on pinned Effect 4.0.1, and a JavaScript entry likewise carries only the declarations and prelude helpers it reaches.

## Runnable examples

| Example | What it demonstrates | Target |
| --- | --- | --- |
| [checkout.ef](examples/checkout.ef) | The program from the top of this page | Go / JS |
| [main.ef](examples/main.ef) | Nominal services, explicit provision and typed recovery | Go / JS |
| [workflow.ef](examples/workflow.ef) | Authorization, lookup and delivery with three service contracts | Go / JS |
| [data.ef](examples/data.ef) | Records, enums and exhaustive matching | Go / JS |
| [generic-users.ef](examples/generic-users.ef) | Generic records with `Option` and `Result` | Go / JS |
| [layers.ef](examples/layers.ef) | Shared construction, hidden dependencies and whole-graph fixture replacement | Go / JS |
| [layers-workflow.ef](examples/layers-workflow.ef) | Configured layer provision and retained operation failures | Go / JS |
| [latest-task.ef](examples/latest-task.ef) | Replace an owned child after interruption and cleanup finish | Go / JS |
| [recipes-retry.ef](examples/recipes-retry.ef) | Lazy typed recipes stored in generic records and Option; each retry runs again | Go / JS |
| [recipes-queue.ef](examples/recipes-queue.ef) | Recipe enum payloads, Result slots and recipe-returning callable fields | Go / JS |
| [recovery-outcome.ef](examples/recovery-outcome.ef) | Payload-binding recovery of a stored job recipe into an Outcome | Go / JS |
| [recovery-codec.ef](examples/recovery-codec.ef) | Effectful recovery handler converting a decode failure into a domain failure | Go / JS |
| [concurrency.ef](examples/concurrency.ef) | Child join/interrupt and deadline recovery | Go / JS |
| [causal.ef](examples/causal.ef) | Managed virtual time, shared latches and causal cleanup tests | Go / JS |
| [callables-service.ef](examples/callables-service.ef) | Callbacks that are generic over failure and service rows | Go / JS |
| [testing.ef](examples/testing.ef) | Fixture providers, typed recovery and owned children in tests | Go / JS |
| [imports.ef](examples/imports.ef) | Automatic native signatures, partial results and context forwarding | Go |
| [http-transport.ef](examples/http-transport.ef) | Bounded buffered HTTP/1.1 with explicit limits, typed handler failures and owned shutdown | Go / JS |
| [http.ef](examples/http.ef) | HTTP routes over `Http.listen`, SDK calls, file scopes and managed shutdown | Go |
| [lifecycle.ef](examples/lifecycle.ef) | Scoped files, cancellation and runtime snapshots | Go |
| [ownership.ef](examples/ownership.ef) | Borrowed outer handles and checked scoped file ownership | Go |

## Learn more

- [Implemented syntax and limits](docs/prototype.md)
- [Runtime contracts](docs/runtime.md)
- [Go interop](docs/interop.md)
- [Bundled modules](docs/bundled-modules.md)
- [Testing](docs/testing.md)
- Tooling: [CLI](docs/tooling.md), [MCP](docs/mcp.md), [LSP](docs/lsp.md)
- [Showcases](docs/showcases.md): real application patterns, with proposals clearly labelled
- [Design sketch](docs/design.md) and [contender roadmap](docs/contender-roadmap.md)
- [North star](NORTH_STAR.md), [prior art](PRIOR_ARTS.md) (Effect, Go, Gleam, ReScript, Elixir, Borgo, Bend and others) and [glossary](GLOSSARY.md)
- [Upstream conformance](docs/conformance.md): pinned Effect behavior mapped to Go/JS acceptance tests

Open design questions are tracked in the [Wayfinder map](https://github.com/cevr/effra/issues/1) on GitHub (`go run ./cmd/wayfinder frontier`; see [docs/wayfinder](docs/wayfinder/README.md)).

## Development

```sh
bun run gate          # formatting, vet, tests, real Go and JS programs, CLI/MCP/LSP parity, live HTTP
go test -race ./...
```

The gate also verifies the pinned Effect reference tests, which live in a submodule: run `scripts/init_upstream.sh` once per clone or worktree first.

Fast compilation is a design constraint; see the [measurement receipts](docs/prototype.md#baseline). Effra is a server language. Kernels, hard real-time and no-GC execution are out of scope.
