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
- r1: the worktree had no `conformance/framework-ports/ports/node_modules`. A hard-link copy from the warm source fixed that (the lockfile is identical).
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
