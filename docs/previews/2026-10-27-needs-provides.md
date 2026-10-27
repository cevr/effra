# 2026-10-27: `needs` and `provides`

Labels: **Tested candidate** and **Proposed**. Supersedes the `uses` spelling of [the Go-shaped language preview](2026-10-27-go-sketch.md), which stays unchanged as the record of that day. Ticket: [#217](https://github.com/cevr/ergo/issues/217). Normative text: [grammar.md, Keywords](../spec/grammar.md#keywords) and [rows.md, Wire](../spec/rows.md#wire) rule 8.

- **Tested candidate** snippets depend on branch `wf10/sweep` at commit `71fc9ab4`, which implements the parser, the formatter and the bridge refusals. Commands, run on that commit in a scratch directory with the manifest `{"edition": "2026"}`: `ef check`, `ef fmt --check` and `ef run`. Each snippet states its result. After the Ergo rename on the same branch ([2026-10-27-ergo.md](2026-10-27-ergo.md)), the binary is `ergo` and the manifest is `ergo.json`.
- **Proposed** means the checking of a wire header, which [#196](https://github.com/cevr/ergo/issues/196) owns.

## What changed

The owner, 2026-10-27: "maybe we can change [uses] to needs. So, provides and needs kind of go hand in hand".

| Before (2026-10-27, go-sketch preview) | Now |
| --- | --- |
| `func main() uses Console` | `func main() needs Console` |
| `func f(g func() T uses (A, B))` | `func f(g func() T needs (A, B))` |
| `wire App uses Account, Invoice { … }` (meant: exposes) | `wire App provides Account, Invoice { … }` |
| (could not be said) | `wire App provides Account, Invoice needs Config { … }` |

One word now has one meaning. `uses` meant requirements on a signature and exposure on a wire header (spec README, Gap 33). `needs` is always requirements; `provides` is always exposure. The keyword count goes from 27 to 28.

## Tested candidate: a function that needs a capability

`ef check` exits 0, `ef fmt --check` reports "already formatted", and `ef run` prints `o-1 paid`.

```go
package checkout

type OrderNotFound struct{ ID string }

type Order struct {
	ID     string
	Status string
}

type Orders interface {
	Find(id string) (Order, error[OrderNotFound])
}

type MemoryOrders struct{}

func (m MemoryOrders) Find(id string) (Order, error[OrderNotFound]) {
	return Order{ID: id, Status: "paid"}
}

func status(id string) (string, error[OrderNotFound]) needs Orders {
	order := try Orders.Find(id)
	return order.Status
}

func report(id string) needs Orders, Console {
	line := status(id) catch {
	case OrderNotFound:
		"no order"
	}
	Console.Println(id + " " + line)
}

func main() needs Console {
	with(wire{ Orders = MemoryOrders{} }) {
		report("o-1")
	}
}
```

## Tested candidate: a wire header, parsed and formatted

`ef fmt` rewrites `wire App   provides Account,Invoice    needs Config,Clock{` to the header below, and `ef fmt --check` then reports "already formatted". `ef check` refuses both clauses with S109, "a wire's provides clause (checked by #196) is outside the implemented subset" at 3:10 and the same for `needs` at 3:36.

```go
package billing

wire App provides Account, Invoice needs Config, Clock {
	Store = MemoryStore{}
}
```

## Proposed: what a wire header means

Designed in [rows.md](../spec/rows.md#wire) rule 8; checked by #196.

- `provides Account, Invoice`: a `with(App) { … }` discharges `Account` and `Invoice`. `Store` serves them inside the build and stays hidden from the body.
- `needs Config, Clock`: the providers require `Config` and `Clock` from the `with` site, so they join that site's row.
- Without `provides`, every entry is exposed, as today.

## Tested candidate: the old spelling gets a repair

`ef check` and `ef fmt` both report S001 at 3:13, spanning the `uses` token: "`uses` is now `needs`". `ef fmt` exits 2 and leaves the file unchanged.

```text
package checkout

func main() uses Console {
	Console.Println("hi")
}
```

The fixtures `uses-hint-*` in `internal/sketch/sketchtest/fixtures.go` cover a declaration, a result, a function type in a list, an interface method, a wire header, a wire header after `provides`, and `uses` as a name.

## Prior art

- **Roc**: an application `provides` its entry points to a platform, which `requires` them.
- **Effect**: `Layer<ROut, E, RIn>` keeps the provided services (`ROut`) apart from the required ones (`RIn`).
- **OSGi**: bundle manifests declare `Provide-Capability` and `Require-Capability`.
- **Dagger and Guice**: `@Provides` marks what a module exposes.
- **Koka and Unison**: required effects are part of a function's type, as a `needs` row is.

## What this preview does not claim

- No checker or emitter behavior for `provides` or a wire-level `needs`; #196 owns it.
- The legacy front end (`effect fn … uses { … }`) keeps its `uses`; [#205](https://github.com/cevr/ergo/issues/205) retires it.
- Historical research notes keep the spelling they were written with.
