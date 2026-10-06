# Minimal executable retention baseline

Measured 2026-10-06 at integration `ff4b487166a7bc4c8cd5b55d6e87f9c638a8022b`. This establishes an artifact-size and unused-dependency baseline, not a speed result or a completed size optimization.

Both builds used Go1.27.0, linux/amd64, `CGO_ENABLED=0` and `go build -trimpath`, with debug information retained. Both resulting executables ran successfully. The Effra source was `effect fn main() -> () { () }`; the Go size control was `func main() {}`.

| Artifact | Bytes | SHA-256 |
| --- | ---: | --- |
| Effra empty entry | 5,532,694 | `7670757f904d5d84a47677d6a9bc5eef69f33c9ebba0328d3e2a0e89a6f64317` |
| Go empty entry | 1,891,532 | `7f100a882b7f4dc25066af5d3e04278e41de9255f9dee241aa071dd0326e626b` |

The bare Go control supplies no managed ownership/cancellation runtime. The size difference therefore cannot be attributed entirely to unnecessary code. The decisive reachability evidence is more specific: although this Effra source uses no HTTP facility, the generated module includes `runtime/http.go`, `go list -deps` includes `net/http`, and the linked executable contains these symbols:

```text
510760 T crypto/tls.init
528200 T net/http.init
528d80 T net/http.init.0
528e00 T net/http.init.1
```

No `ServeHTTP` function appeared in the selected linked-symbol output. Removing an unreachable function is therefore insufficient to remove this package's initialization roots. The current emitter copies `effect.go`, `fiber.go`, `http.go`, `scope.go` and `stdlib.go` for the minimal source; its module selection boundary needs to change.

Reproduction artifacts and full raw command/stdout/dependency receipt are preserved at `/tmp/effra-size-baseline-2026-10-06/`: `probe.py`, `main.ef`, `go-control/{main.go,go.mod}`, generated `dist/go/`, both binaries and `receipt.json`. The probe builds an exact-head compiler, invokes the public build command, records `go list -deps`, `go tool nm`, artifact hashes and exact bytes. It performs no timing or load scoring. Future implementation must check in a portable fixture and preserve this before-repair evidence.

The [binary reachability contract](../specs/binary-reachability.md) owns the repair: select needed modules, avoid unused initialization, preserve callback and provider roots, then extend the same evidence to codecs, HTTP and any admitted fluent API. Neither binary stripping nor weakening runtime guarantees satisfies it.

Emission review adds a rebuild requirement: `WriteRuntime` currently writes changed files but never reconciles removed files, and `buildGoSource` places multiple generated application directories beside one shared `dist/go/runtime`. Selecting fewer files alone would leave prior HTTP source behind or make one application's selected runtime replace another's. Reachable emission needs coherent artifact ownership and a fresh-versus-reused output fixture. This is a source-derived implementation constraint, not a new measured size result.
