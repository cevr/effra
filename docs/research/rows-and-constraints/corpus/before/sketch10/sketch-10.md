# Sketch 10: Effra now, Effect for Go (2026-10-26)

Owner, 2026-10-26: "this will be effect for Go with this kind of new language as well. So we'll have a standard library and all the things that Effect does … more tailored to this new language and the new idioms."

**State today (measured):**
- `main` is at `d878b2ae`. Go is the only target. The JS emitter, framework-ports, the Effect submodule and the Node toolchain are gone.
- The sketch-9 core compiles in `prototypes/sketch9`: error rows, `uses`, `wire`/`with`, `try`/`catch`, unions, and `timeout.After`, all emitted as direct Go.
- Everything below that is not in that core is design: machines, actors, `tasks`, codecs, `http`, and the `html` package.

**Decisions taken today that shape these examples:**
- **#6:** `Close()` cannot fail with a typed error. A crash in `Close` joins the exit cause.
- **#9:** Go imports map automatically. Hand-written bindings are opt-in.
- **#102:** `Clock`, `Scheduler`, `Random` and `Console` are ordinary interfaces bound by `wire`.

---

## 1. A state machine, run as a supervised actor

```go
package breaker

import (
	"actor"
	"schedule"
	"tasks"
)

type ProbeDown struct{}

type Probe interface {
	Healthy(endpoint string) (bool, error[ProbeDown])
}

// States and signals are closed unions: the checker knows every case.
type State interface{ Closed | Open | HalfOpen }
type Closed struct{ Failures int64 }
type Open struct{ Endpoint string }
type HalfOpen struct{ Endpoint string }

type Signal interface{ Failure | Success | Retry }
type Failure struct{ Endpoint string }
type Success struct{}
type Retry struct{}

// One step: the state, a signal, and a Decision (Enter, Stay, Ignore, Reject, Done).
// The switch is checked for totality over State × Signal.
func step(s State, sig Signal) (Decision[State, string], error[ProbeDown]) uses Probe {
	switch s, sig := s.(type), sig.(type) {
	case (Closed, Failure):
		if s.Failures >= 2 {
			return Enter{Open{sig.Endpoint}}
		}
		return Stay{Closed{s.Failures + 1}}
	case (Open, Retry):
		if try Probe.Healthy(s.Endpoint) {
			return Enter{HalfOpen{s.Endpoint}}
		}
		return Stay{s}
	case (HalfOpen, Success):
		return Enter{Closed{0}}
	case (HalfOpen, Failure):
		return Enter{Open{sig.Endpoint}}
	case (_, _):
		return Ignore{}
	}
}

// machine is a keyword: an initial state and a step. `ef inspect` draws its graph
// from the step's cases, because a helper with an empty footprint is static.
type Breaker machine[State, Signal] {
	initial Closed{0}
	step    step
}

func run(signals chan Signal) uses Probe, Clock, Scheduler {
	// tasks.Set owns the actor and restarts it with backoff (Erlang one_for_one).
	set := tasks.Set(tasks.Restart(schedule.Exponential(1s).UpTo(5)))
	b := actor.Start(Breaker{}, 16) // a resource: Send, Ask, State, Stop
	set.Go(func() { actor.Pump(b, signals) }) // lends b

	d := try b.Ask(Retry{}) catch { case ProbeDown: Reject{} }
	log.Info("breaker", "decision", d, "state", b.State()) // State is a snapshot, never shared
} // b stopped, then set stopped (section 6)
```

What it replaces from Effect: `Machine`, `Actor`, `Supervisor`, `Schedule`. What it replaces from Go: a goroutine with a `select` loop, a mutex around the state, and hand-written restart logic.

---

## 2. Layers: `wire`, `with`, resources and fixtures

```go
package app

import (
	"retry"
	"sql"
)

type ConnectFailed struct{ URL string }
type OrderNotFound struct{ ID string }

type Config interface{ String(key string) string }
type Orders interface {
	Find(id string) (Order, error[OrderNotFound])
}

// Its fields are resources, so Postgres is one too, with a generated Close:
// refresh stopped, then pool closed (section 6). wire releases it, dependents
// first, to completion even under interruption (I2). Release cannot fail (#6).
type Postgres struct {
	pool    sql.Pool
	refresh Task[struct{}]
}

func (p Postgres) Find(id string) (Order, error[OrderNotFound]) { return p.pool.QueryOrder(id) }

// A constructor is an ordinary function. Its uses row is filled from the same wire
// graph; its error row becomes a startup error of every with() that builds it.
func OpenPostgres() (Postgres, error[ConnectFailed]) uses Config, Clock, Scheduler {
	url := Config.String("DATABASE_URL")
	pool := try retry.On[ConnectFailed](retry.Exponential(200ms).Times(5)) { sql.Open(url) }
	return Postgres{pool, go keepWarm(pool)} // both move into Postgres; nothing closes here
}

wire Live {
	Config    = EnvConfig{}
	Clock     = SystemClock{}
	Scheduler = LiveScheduler{}
	Orders    = OpenPostgres // built once per with(); closed when it ends
}

wire Test {
	Live
	replace Orders = MemoryOrders{}
	replace Clock  = testing.VirtualClock{} // #102: the clock is just an interface
}

func main() error[ConnectFailed] uses Console {
	return with(Live) {
		http.Serve(":8080", routes) // runs until stopped; Ctrl-C stops the task tree
	}
}
```

What it replaces from Effect: `Layer.succeed`, `Layer.effect`, `Layer.scoped`, `Layer.provide`, `Layer.merge`, `acquireRelease`, `TestClock`. What it replaces from Go: `context.Context` threading, `defer db.Close()` chains, and a hand-written dependency-injection `main`.

---

## 3. Codecs: parse, don't validate

```go
package orders

import "json"

type InvalidQuantity struct{ Units int64 }
type UnknownMember struct{ Handle string }

// A type's wire form is found by name, as Go finds TestX: ParseX decodes, x.Wire()
// encodes (Go's netip.ParseAddr / String pair).

// A refinement: the field is unexported, so ParseQuantity is the only way in.
type Quantity struct{ units int64 }

func ParseQuantity(n int64) (Quantity, error[InvalidQuantity]) {
	if n <= 0 {
		return InvalidQuantity{n}
	}
	return Quantity{n}
}
func (q Quantity) Wire() int64 { return q.units }

// Normalization: the legacy "enabled" spelling decodes as Active. Encoding writes
// the canonical spelling, because Status is a subset of StatusWire.
type Status interface{ Active | Suspended }
type StatusWire interface{ Active | Enabled | Suspended }

func ParseStatus(w StatusWire) Status {
	switch w.(type) {
	case Active, Enabled:
		return Active{}
	case Suspended:
		return Suspended{}
	}
}

// An effectful decode: the wire carries a handle, and decoding looks it up.
// The uses and error rows join every codec that contains a Member.
func ParseMember(handle string) (Member, error[UnknownMember]) uses Directory {
	return Directory.Find(handle)
}
func (m Member) Wire() string { return m.Handle }

type Order struct {
	Member   Member
	Quantity Quantity
	Status   Status
}

// The codec is derived, and its rows are the union of its fields' rows:
//   json.Decode[Order]: error[json.DecodeFailed | InvalidQuantity | UnknownMember] uses Directory
func importOrder(body []byte) (Order, error[json.DecodeFailed | InvalidQuantity | UnknownMember]) uses Directory {
	return json.Decode[Order](body)
}
```

What it replaces from Effect: `Schema.Struct`, `Schema.transform`, `Schema.transformOrFail`, `Schema.filter`, `Schema.brand`. What it replaces from Go: `json.Unmarshal` into a DTO, followed by a hand-written `Validate()`.

---

## 4. An HTML server with html functions (owner decision 2026-10-26: functions)

Handlers and routes are sketch 9 (C1). HTML is a standard-library package of ordinary functions, as in Gleam Lustre and Elm `Html`. There is no markup grammar. Owner, 2026-10-26: "Functions".

```go
package shop

import (
	. "html" // elements and attributes read as a tree: Tr, Td, Class, Text
	"http"
)

// A component is a function that returns html.Node. Text escapes its string;
// only an html.Node is inserted as markup, so escaping is decided by type.
func OrderRow(o Order) Node {
	return Tr(Class(statusClass(o.Status)),
		Td(Text(o.Member.Name)),
		Td(Int(o.Quantity.Wire())),
	)
}

func OrdersPage(orders []Order) Node {
	return Layout("Orders",
		Table(Each(orders, OrderRow)),
		When(len(orders) == 0, P(Text("No orders yet."))),
	)
}

func Layout(title string, children ...Node) Node {
	return Html(
		Head(Title(Text(title))),
		Body(children...),
	)
}

// A handler is an ordinary function. A catch without try must cover every error,
// so no failure leaves the handler without a status.
func listOrders(req http.Request) http.Response uses Orders {
	orders := Orders.List(req.Query("member")) catch e {
	case OrdersUnavailable:
		return http.HTML(503, P(Text("Try again soon.")))
	}
	return http.HTML(200, OrdersPage(orders))
}

var routes = http.Routes{
	"GET /orders":      listOrders,
	"POST /orders":     placeOrder, // JSON in, JSON out, via json.Decode[Order]
	"GET /orders/{id}": showOrder,
}
```

**Types do the checking that markup syntax would have done:**
- `Tr`, `Td` and the other elements take `...Child`, where `Child` is the closed union `Attr | Node`. A void element such as `Br` or `Img` takes only `...Attr`, so it cannot get children.
- `Href` and `Src` take a `url.Safe`, not a string. `Raw` takes an `html.Trusted`, which only `html.Trust` builds, so `ef inspect` can list every trust site.
- A component is a plain function, so Go's arity and type checks enforce its required arguments.

**Lowering:** `html.Node` is a render function that writes to a buffered writer, the representation templ uses. `Text` escapes at write time, so no intermediate string is built. A later optimization may pre-render static subtrees at compile time: a call with literal arguments only is constant, and the emitter can fold it into one `w.Raw`. Measure it before claiming it.

**Prior art:**
- **Gleam Lustre:** `html.td([], [text(name)])`. HTML is functions over an element type, and the same tree renders on the server to a string builder. Effra takes this surface.
- **Elm `Html`:** functions with separate attribute and child lists, and escaping by type. Effra merges the two lists into one variadic `...Child` union, which keeps Go's call shape.
- **templ (Go):** compiles to direct `io.Writer` calls. Effra takes its render-function representation, not its syntax.
- **Phoenix HEEx (Elixir):** compile-time HTML validation. Effra gets the useful part, closed element and attribute types, from the type checker instead of a parser.

---

## 5. Observability by construction (sketch 9, G1–G3)

```go
func placeOrder(req http.Request) http.Response uses Orders, Inventory {
	return with(trace.Span("place-order"), log.Annotate("member", req.Header("X-Member"))) {
		created(req) catch e { … }
	}
}
```

`ef ps <pid>` shows the live task tree: each task's state, what it waits on, its span and its restarts. A crash prints the chain of tasks and spans that led to it. Spans are keyed to checked definition IDs (#188), so a trace line links to the source definition and to `ef inspect`.

---

## 6. Automatic release (owner direction 2026-10-26, proposal for #65)

Owner, 2026-10-26: "How can we make it simpler so that defers are automatic … Kind of like acquire release".

**Rule.** A type with a `Close()` method is a resource. The block that owns a resource value closes it when the block ends, in reverse acquisition order, on every exit: return, error, panic and interruption. There is no `defer` to write and none to forget.

**Ownership moves in four ways only:**
1. `return` moves it to the caller.
2. A provider field moves it into the provider, which closes it when the `with` block that built the provider ends.
3. `go` moves a captured resource into the new task.
4. A channel send moves it to the receiver.

Passing a resource as an argument lends it. A lent resource cannot be stored, returned or sent, so a lend needs no lifetime annotations (as with Swift's non-escaping parameters).

**Early release.** Calling `x.Close()` (or `task.Wait()`, `b.Stop()`) consumes the value. After that call nothing runs at scope end, and a second use is a compile error.

```go
func copyFile(src, dst string) error[NotFound | WriteFailed] uses FS {
	in  := try FS.Open(src)
	out := try FS.Create(dst) // if this fails, `in` is still closed
	try io.Copy(out, in)
}                             // out closed, then in

func run(signals chan Signal) uses Probe, Clock, Scheduler {
	set := tasks.Set(tasks.Restart(schedule.Exponential(1s).UpTo(5)))
	b := actor.Start(Breaker{}, 16)
	set.Go(func() { actor.Pump(b, signals) }) // lends b
	d := try b.Ask(Retry{}) catch { case ProbeDown: Reject{} }
}                                             // b stopped, then set stopped

func importAll(paths []string) error[NotFound] uses FS {
	for _, p := range paths {
		f := try FS.Open(p)
		try load(f)
	} // f closed at the end of each iteration, not at function end (Go's defer-in-loop trap)
}

// A provider whose fields are resources gets a generated Close: fields close in
// reverse declaration order. Write Close only to add behaviour before that.
type Postgres struct {
	pool    sql.Pool
	refresh Task[struct{}]
}

func OpenPostgres() (Postgres, error[ConnectFailed]) uses Config, Clock, Scheduler {
	pool := try retry.On[ConnectFailed](retry.Exponential(200ms).Times(5)) {
		sql.Open(Config.String("DATABASE_URL"))
	}
	return Postgres{pool, go keepWarm(pool)} // both move into Postgres; nothing closes here
}
```

**Guarantees, as Effect's `acquireRelease` gives them:**
- Acquisition and registration of its release are one step, so interruption cannot fall between them (I2 masks).
- Release always runs and cannot fail (#6). A crash in `Close` joins the exit cause.
- A `Task` released at scope end is stopped and drained, as with `forkScoped`. Call `Wait` to get its result instead.

**Visibility.** The language server shows an inlay hint `closes: out, in` at each block end. `ef inspect` lists every inserted release with its source position.

**Lowering.** The emitted Go is the `defer` a careful Go programmer writes. A loop body becomes its own function, or gets an explicit close on each exit edge. Measure both before choosing.

**What it replaces:**
- **Effect:** `acquireRelease`, `Scope`, `addFinalizer`, `ensuring` and `forkScoped`.
- **Go:** `defer x.Close()`, `errdefer`-style cleanup when a later step fails, and the defer-in-loop trap.

**Prior art:**
- **Rust `Drop` with moves:** the model. Effra keeps moves and drops lifetimes, because a lent value cannot escape.
- **Swift `~Copyable`:** `consume`, `deinit`, and non-escaping borrowed parameters.
- **C# `using var` and Python `with`:** scoped release, with a marker at each binding. Effra infers it from the type.
- **Gleam `use`:** `use f <- with_file(path)` turns callback-scoped cleanup into flat code. It is explicit, and each resource needs a wrapper function.
- **Elixir/OTP:** a port or file belongs to its process and closes when the process exits. Effra's per-task scope gets the same "the owner's end releases it" rule.
- **Zig `defer` and `errdefer`:** explicit, and Zig does it on purpose to avoid hidden control flow. Effra answers that cost with inlay hints and `ef inspect`.
