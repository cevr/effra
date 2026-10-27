# Receipt: sketch9 prototype, round 1

Host: shared exe workbox, 16 cores, load average 11–14 during the runs; go1.27.0 linux/amd64.

## Commands and exit codes

| Command (from the worktree root) | Exit |
| --- | --- |
| `prototypes/sketch9/accept.sh` | 0 (`ACCEPT OK`) |
| `prototypes/sketch9/bench.sh` | 0 |
| `./scripts/gate.sh` | 1: only `framework-port references` fails, on the host (see below) |

The acceptance and gate logs are in `/home/exedev/.cache/baton/wf10/sketch-proto/accept-r1.log` and `gate-r1.log` to `gate-r3.log`.

The gate failure is independent of this change, which touches only `prototypes/sketch9/`:
- r1: the worktree had no `node_modules` copy in the then-present framework-port leaf (removed 2026-10-26). A hard-link copy from the warm source fixed that (the lockfile is identical).
- r3: `check-references.mjs` requires Bun 1.4.3, and the host now has 1.4.4.

The owner then decided to remove JS interop and that version requirement (plan.md, js-removal). This branch re-gates after that change reaches main.

## Build time of the emitted checkout program

`bench.sh`: one build with an empty `GOCACHE`, then three rebuilds, each after a one-literal edit of the sketch (`"paid "` becomes `"paid-<stamp>-<n> "`, never built before). `emit` is sketch to Go (parse, check, emit, write); `go build` is the emitted module.

| Run | emit (ms) | go build (ms) |
| --- | --- | --- |
| cold | 7 | 4325 |
| edit 1 | 4 | 234 |
| edit 2 | 5 | 234 |
| edit 3 | 4 | 260 |

These are single samples on a loaded shared host (measured). The cold build includes compiling `fmt`, `os`, `sync` and `time` from source. For scale only: compile-measure's direct shape (1024 operations, a different program) rebuilt in a 2.08 s median, and its recipe-closure shape in 48.13 s.

## Line counts

| Part | Lines |
| --- | --- |
| emitted `main.go` (checkout) | 172 |
| copied `rt/rt.go` | 144 |
| prototype Go, without tests (`sketch/*.go`, `sketch/rt/rt.go`, `cmd/sketchc`) | 2538 |
| tests (`sketch/sketch_test.go`, `sketch/rt/rt_test.go`) | 284 |
| total Go | 2822 |

The spec's target was about 2,500 lines with tests; this is 13% over. The extra is mostly in the checker, which adds S105–S110 to the four rules the spec named.

## Emitted shape (checkout, `//line` directives removed)

```go
func checkout(env_ *env, t_ *rt.Task, id string) (string, error) {
	if t_.Stopped() {
		return "", rt.Interrupted
	}
	v_1, e_2 := env_.Orders.Find(t_, id)
	if e_2 != nil {
		return "", e_2
	}
	order := v_1
	v_3, e_4 := rt.After(env_.Scheduler, t_, 500*time.Millisecond, func(t_ *rt.Task) (Payment, error) {
		if t_.Stopped() {
			return nil, rt.Interrupted
		}
		v_5, e_6 := env_.Gateway.Authorize(t_, order)
		return v_5, e_6
	})
	if e_4 != nil {
		return "", e_4
	}
	payment := v_3
	switch p := payment.(type) {
	case Pending:
		return "pending", nil
	case Authorized:
		return ("paid " + p.AuthID), nil
	case Declined:
		return ("declined: " + p.Reason), nil
	}
	panic("unreachable")
}

func report(env_ *env, t_ *rt.Task, id string) {
	var v_7 string
	var e_8 error
	{
		envc_9 := *env_
		envc_9.Orders = MemoryOrders{}
		envc_9.Gateway = FakeGateway{AuthID: "auth-7"}
		envc_9.Scheduler = rt.LiveScheduler{}
		v_10, e_11 := checkout(&envc_9, t_, id)
		v_7, e_8 = v_10, e_11
	}
	if e_8 != nil {
		switch e_8.(type) {
		case OrderNotFound:
			v_7 = "no such order"
		case GatewayDown:
			v_7 = "gateway down"
		case rt.Expired:
			v_7 = "gateway timed out"
		default:
			panic(e_8)
		}
	}
	outcome := v_7
	t_.Check()
	env_.Console.Println(t_, outcome)
}
```

## Spec points that were wrong or ambiguous

1. **Total catch.** The spec says a total catch has no default arm. With interruption carried as an error value, that would swallow `rt.Interrupted`. The emitter adds a default arm that passes it on.
2. **Interruption in infallible functions.** The spec writes `return zero, rt.Interrupted`. A function with no error row has nowhere to return it, so it calls `t.Check()`, which panics. The nearest task boundary recovers the panic: `rt.After`, or `rt.Recover` in `main`, which exits with code 130.
3. **Rules beyond S101–S104.** The subset needs S105–S110 to reject programs Go would reject or that would lose a failure. Without them, the emitter would produce Go that does not compile.
4. **Go version.** The worktree's `go.mod` now says `go 1.27`, as the spec does. An earlier draft said 1.26.

# Round 2: review round 1 repairs

Findings: `/home/exedev/.cache/baton/wf10/sketch-proto/round-1-findings.md` (14 Major, 2 Minor, 1 Optional, no blocker).

| Finding | Repair |
| --- | --- |
| M1 wire initializers drop rows | Inline `wire{}` providers are checked in the enclosing function before the block's discharge. A top-level `wire` must be closed: no capability from the caller, no escaping failure (S102 or S101 otherwise). |
| M2 cancellation drops defects | `rt.After` re-panics with the drained child's defect on the deadline and stop paths too. |
| M3 ancestor stop becomes success | `Stop` propagates down the task tree (children register with their parent). A stop during the body wins over the body's success. |
| M4 provider methods calling helpers | Methods pass an empty `env` to the functions they call (they require no capability, S102). |
| M5 no-value ABI | The timeout closure frame knows its Go result is `(struct{}, error)`. A fallible no-value function ends in `return nil`. |
| M6 unused `fmt` | Imports are collected from what is emitted. |
| M7 name collisions | Temporaries end in `_`, which no source name may. Function names are reserved like other names. A local that shadows a function makes the call S108. Fields named `Error` are reserved. |
| M8 unhandled failures in fields | S105 at every value position: struct fields, `if` conditions, switch subjects. |
| M9 provider parameter types | Parameter types are compared. Host capabilities take only host providers. |
| M10 integer types | One integer type (`int64`). Bindings are emitted as `var x T = v`, with the checked type. |
| M11 block shapes | `with` and `timeout.After` blocks must end with an expression, and `return` cannot leave them (S109). |
| M12 nondefaultable fields | A keyed struct literal must set every union, capability, or union-holding struct field. |
| M13 erased reads | An expression statement must be a call, `try` or `catch` (S110). |
| M14 short-circuit operators | `&&` and `||` are rejected (S109). `==` is basic types only; `+` is string or int64. |
| N1 prior art | Borgo's two `Result` paths, the XGo scope and ReScript's representations are corrected; Elixir `Task.shutdown` added. |
| N2 tests | The partial-catch tests use a producer with two errors. The parent-stop test requires `Interrupted`. |
| O1 provider specialization | Not done; a follow-up after correctness, needs a matched measurement. |

All 29 reviewer witnesses are regression fixtures in `testdata/probes/` (first line `// want CODE` or `// out: lines`), run by `TestProbes`, which also runs `go vet` on every built program. The reviewer's three runtime probes are in `sketch/rt/review_probe_test.go`. `go test -race ./...` passes.

Commands (round 2): `prototypes/sketch9/accept.sh` exit 0 (`accept-r2.log`); `bench.sh` exit 0 (`bench-r2.out`): cold 5458 ms, edits 300, 319 and 286 ms, emit 5–7 ms. The host was more loaded than in round 1, because another unit's gate was running. Lines: prototype Go without tests 2702, tests 401, total 3103.

# Round 3: review round 2 repairs

Findings: `/home/exedev/.cache/baton/wf10/sketch-proto/round-2-findings.md` (9 Major, 1 Minor, 1 Optional, no blocker).

| Finding | Repair |
| --- | --- |
| M1 names and binding identity | Go's predeclared identifiers (`string`, `error`, `panic`, …) and `Error` are reserved for every source name, methods included. Generated marker methods end in `_`, which no source name may. A named wire is emitted as its own function (`wireW_`), so a caller's locals cannot capture its initializers (`named-wire-shadow` prints `local provider`). |
| M2 empty wire | `with(wire{})` keeps the enclosing env; no copy. |
| M3 recursive layouts | A struct that contains itself by value is S108; recursion behind a union is fine. |
| M4 no-value values | An argument must have a value. A catch over a no-value expression must have no-value arms. |
| M5 prelude host types | Union variants, error types and method receivers must be structs declared in the file. |
| M6 duplicates | Duplicate struct fields, literal keys, catch alternatives, switch cases and union variants are S108. |
| M7 integer constants | Literals must fit int64, and constant `+` is folded with an overflow check, as Go does. Non-constant arithmetic wraps, as in Go. |
| M8 entry ABI | `main` takes no parameters and cannot be called. |
| M9 wire cycles | A wire that reaches `with` of itself, directly or through called functions, is S109. |
| N1 wording | "Closed" (no caller capability, no escaping failure) replaces "effect-free" in the docs and comments. |
| O1 | Still deferred. |

The 27 round-2 witnesses join `testdata/probes/` (56 in all), and the reviewer's task-tree stress test is `sketch/rt/task_tree_test.go`. `go test -race ./...` passes. `accept.sh` exit 0 (`accept-r3.log`). Lines: prototype Go without tests 2951, tests 487, total 3438.

# Round 4: review round 3 repairs

Findings: `/home/exedev/.cache/baton/wf10/sketch-proto/round-3-findings.md` (5 Major, 1 Minor, 1 Optional, no blocker).

| Finding | Repair |
| --- | --- |
| M1 generated scopes and name positions (third repeat) | Fixed by construction. Source names may not contain `__` (S109). Every generated name does: temporaries `v__1`, `env__`, `t__`, `Env__`, `e__`, `plain__`, `is__X`, `wire__W`, `envc__`, `main__`. Every source local, parameter and receiver is emitted as `name__l`, so it cannot shadow a type, a function or a generated name. `init` is reserved. Capability parameter names are checked. |
| M2 numeric literals | Literals are decoded once. A leading zero is S108 (Go reads it as octal). A duration must fit int64 nanoseconds. Integer literals are emitted as `int64(n)`, so a variadic argument stays int64 on every GOARCH (`portable-integer` builds for GOARCH=386). |
| M3 declaration namespaces | Duplicate functions, wires, methods and capability methods; a type and a function with one name; a field and a method with one name; repeated parameters and a parameter that repeats the receiver are all S108. |
| M4 wire cycle through methods | A capability call in a wire initializer follows every provider method of that name. This over-approximates and is documented in the code. |
| M5 capability method uses | A uses clause on a capability method is S109. |
| N1 exponential layout walk | The walk keeps on-path and finished states, so each type is walked once. |
| O1 | Still deferred. |

The 23 round-3 witnesses join `testdata/probes/` (78 in all). The reviewer's walk and Go-universe tests are `sketch/review_walk_test.go`. `go test -race ./...` passes. `accept.sh` exit 0 (`accept-r4.log`). Lines: prototype Go without tests 3051, tests 592, total 3643.

# Round 5: review round 4 repairs

Findings: `/home/exedev/.cache/baton/wf10/sketch-proto/round-4-findings.md` (5 Major, no blocker).

| Finding | Repair |
| --- | --- |
| M1 fields and union markers | Field names follow the generated-name rule too: no `__`, no trailing `_`, not `Error`. Go's predeclared names stay allowed as fields. |
| M2 a capability named `timeout` | The emitter lowers only the `timeout.After` calls the checker resolved (`Checked.Afters`). `timeout` is reserved as a source name. |
| M3 a type named `duration` | `duration` is reserved as a source name, so it cannot alias the intrinsic type. |
| M4 mixed keyed and positional fields | S001 at parse time. |
| M5 invalid string escapes | String literals are decoded with Go's rules (`strconv.Unquote`); an invalid one is S108. |

The 14 round-4 programs join `testdata/probes/` (92 in all). The reviewer's name-position matrix is `sketch/review_names_test.go`. `go test -race ./...` passes. `accept.sh` exit 0 (`accept-r5.log`). Lines: prototype Go without tests 3068, tests 662, total 3730.

# Round 6: review round 5 repairs

Findings: `/home/exedev/.cache/baton/wf10/sketch-proto/round-5-findings.md` (2 Major, no blocker).

| Finding | Repair |
| --- | --- |
| M1 duration argument drops a failure | The `timeout.After` duration goes through the value check (S105), like every other value position. |
| M2 string bytes Go source cannot hold | A string's value is what Go's rules decode from its spelling, and the emitter re-quotes that value (`strconv.Quote`). A raw byte order mark, NUL or invalid UTF-8 in a literal now builds and prints the same bytes as its escaped spelling. |

The 9 round-5 programs join `testdata/probes/` (101 in all); probe headers gain `// out-quoted: "..."` for output a header cannot spell. The reviewer's intrinsic-name tests are `sketch/review_intrinsics_test.go`. `go test -race ./...` passes. `accept.sh` exit 0 (`accept-r6.log`).

# Round 7: review round 6 repair

Findings: `/home/exedev/.cache/baton/wf10/sketch-proto/round-6-findings.md` (1 Major, no blocker).

| Finding | Repair |
| --- | --- |
| M1 identifier tested one byte at a time | The lexer decodes each code point (`utf8.DecodeRuneInString`) and applies Go's identifier rule to the rune: a letter or `_` to start, and a letter, digit or `_` after that. A non-identifier rune or invalid UTF-8 in identifier position is S001. Number suffixes are decoded the same way. String literals keep their own path. |

The 2 round-6 programs join `testdata/probes/` (103 in all): `nonletter-name` (`x⪪`, S001) and `letter-name` (`xª`, builds and prints `ok`). `sketch/lex_test.go` sweeps every non-ASCII code point, first and later in a name, against `go/token.IsIdentifier`, and rejects four invalid UTF-8 names with S001. These tests fail on the round-6 lexer. The sweep also shows that the old lexer rejected valid letters such as `À`. `accept.sh` exit 0 (`accept-r7.log`).

# trace-188: kernel mode and traces (#188)

Host: shared exe workbox, AMD EPYC 9554P, 16 vCPU, load average 6–13; go1.27.0 linux/amd64. Unit directory: `/home/exedev/.cache/baton/wf10/trace-188`. The research note is `docs/research/traceability.md`.

## Commands and exit codes

| Command (from the worktree root) | Exit |
| --- | --- |
| `bash /home/exedev/.cache/baton/wf10/trace-188/accept.sh` | 0 (`ACCEPT OK`, `accept-r1.log`) |
| `prototypes/sketch9/accept.sh` | 0 (`ACCEPT OK`) |
| `cd prototypes/sketch9 && go vet ./... && go test -count=1 ./...` | 0 |

## What changed

- `sketchc build -rt kernel` links `effra.local/prototype/runtime/rt`. `sketchc trace [-json]` reads a recorded run against the checked model (README, "Kernel mode and traces").
- `sketch/defs.go`: `NewModel`, the definition table and the trace points, derived from the checked declarations. `sketch/emit_kernel.go`: the kernel `main`, the constants, the site table and the point events. `sketch/trace.go`: the query and its refusals.
- `Scheduler.Sleep(d)` joins the prelude. Both runtimes lower it to `rt.Sleep`. `testdata/reject/sleep-not-duration.sketch` (S108) and `testdata/probes/sleep-timeout.sketch` (both outcomes of a timeout, embedded mode) cover it.
- Default mode: for the 40 accepted programs (`testdata/checkout.sketch` and the accepted probes), the base `854f21e7` `sketchc` and this branch emit byte-identical `main.go`. The embedded `rt/rt.go` gains only `Sleep`.

## The three traced examples (`examples-r1.txt`)

25 runs off and 25 on, interleaved; wall time includes process start.

| Example | Output | Events | Trace bytes | Wall median off / on (ms) | Binary, kernel with switch / without / embedded (bytes) |
| --- | --- | --- | --- | --- | --- |
| `testdata/trace/checkout.sketch` | `paid auth-7` | 9 | 1,269 | 3.29 / 2.75 | 2,649,099 / 2,586,999 / 2,548,752 |
| `testdata/trace/timeout.sketch` | `gateway timed out` | 13 | 1,818 | 105.41 / 104.73 | 2,650,345 / 2,588,197 / 2,549,423 |
| `testdata/trace/failure.sketch` | `no such order` | 9 | 1,297 | 3.13 / 2.95 | 2,644,650 / 2,582,278 / 2,544,050 |

The "without" binary is the same emitted program with the trace switch removed by a scratch script, to measure the switch alone: about 62 KB. A first writer that used `encoding/json` made the switch cost 1.48 MB.
