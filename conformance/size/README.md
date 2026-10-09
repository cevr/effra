# Size conformance

Public programs and controls for the [small executable contract](../../docs/specs/binary-reachability.md). Every number they produce is a raw measurement. None is a performance claim or a size budget.

| Path | Contents |
| --- | --- |
| `fixtures/minimal.ef` | Pure minimal entry. |
| `fixtures/minimal-unused.ef` | The same entry plus HTTP, codec, provider, console/env and bundled declarations it never reaches. |
| `fixtures/managed.ef` | Managed effect without platform I/O: owned children, join, interruption, a deadline and typed recovery. |
| `fixtures/codec.ef` | Codec-only consumer of one derived JSON codec. |
| `fixtures/http.ef` | HTTP application. It is built and measured but not run. |
| `fixtures/direct.ef`, `fixtures/pipe.ef` | The same program written with direct calls and with the pipe. |
| `controls/go/minimal`, `controls/go/managed` | Matched idiomatic Go controls that keep the fixture's contract with the standard library only. The minimal control cancels on a termination signal through `signal.NotifyContext` and runs its work in an owning scope. |
| `controls/go/minimal-floor` | Unmatched floor: the minimal result printed with no cancellation or scope. It bounds the minimal row from below and is never compared as an equivalent program. |
| `controls/ts/{minimal,managed}.ts` | Matched TypeScript controls on Effect 4.0.1. `host.d.ts` declares the `process` surface they use. |
| `matrix/` | The explicit matrix, a Go program over `internal/receipt`, the same measurement package `ef build --receipt` uses. |
| `receipts/*.json` | Recorded matrix runs. Each one is bound to its commit, dirty state, toolchain and fixture/control/runtime hashes. |

## Commands

```sh
# Deterministic retention and receipt checks. These run in the gate as part of go test ./...
go test ./cmd/ef -run 'TestSizeFixtures|TestBuildReceipt|TestJSReceipt'
go test ./internal/receipt

# One application's receipt
ef build conformance/size/fixtures/managed.ef -o dist/managed --receipt dist/managed.receipt.json
ef build conformance/size/fixtures/managed.ef --target js --entry -o dist/managed.mjs --receipt dist/managed.js.receipt.json

# The explicit matrix. It needs go, bun and node; tsc is used when it is on PATH
go run ./conformance/size/matrix -out /tmp/effra-size [-record conformance/size/receipts/DATE.json]
```

`--receipt` refuses a path that names the build's source or one of its artifacts, including aliases through symbolic or hard links and spellings where `..` follows a link, and any path inside `dist/go/apps`. It publishes the receipt atomically inside the directory it admitted.

The matrix builds with `CGO_ENABLED=0`, an empty `GOFLAGS` and `-trimpath -mod=readonly -buildvcs=false`, and builds a `-ldflags=-s -w` companion for every unstripped binary. Effra rows are the `ef build --receipt` receipts; controls and counterfactuals are measured with the same `internal/receipt` functions. The matrix fails if any program's output differs from the expected output. For JavaScript it reports separately:

- the emitted module;
- the minified application bundle, with `effect` external;
- the minified deployment bundle, with `effect` inlined;
- the deployment delta between the two.

The all-source counterfactual rebuilds the minimal, managed and codec generations with every runtime source the compiler distributes. It measures what source selection removes that the Go linker's own dead-code elimination does not.

The codec and HTTP rows have no matched controls yet; see the remaining work in the spec.
