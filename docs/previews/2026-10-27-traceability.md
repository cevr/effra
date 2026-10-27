# 2026-10-27: why did this request time out? An agent reads a trace

Labels: **Checked (prototype)** and **Proposed**.

- **Checked (prototype)**: the program, the trace and the answer below were produced by the merged prototype compiler, from `prototypes/sketch9` at commit `ecc82507`. Commands: `go run ./cmd/sketchc check testdata/trace/timeout.sketch` exits 0; `go run ./cmd/sketchc build -rt kernel testdata/trace/timeout.sketch -out gen -o timeout` builds the program; `EFFRA_TRACE=timeout.jsonl ./timeout` prints `gateway timed out` and records the run; `go run ./cmd/sketchc trace testdata/trace/timeout.sketch timeout.jsonl` prints the answer shown (trailing spaces trimmed). The call spans vary by run; the cause trace has no times.
- **Proposed**: `ef trace` and MCP `code.trace`, designed in [docs/research/traceability.md](../research/traceability.md#query-surface). The sketch is not in `ef` until #203.

It does not claim a production tracer, an OpenTelemetry exporter, sampling for servers or values in events. The note lists those as follow-up units.

## The question

> A request for order 42 printed "gateway timed out". Why did it time out?

## The program (Checked (prototype))

`testdata/trace/timeout.sketch`, abridged to the parts the answer cites. The line numbers are the file's.

```go
31  func roundTrip(order Order) (Payment, error[GatewayDown]) uses Gateway, Scheduler {
32  	Scheduler.Sleep(2s)
33  	payment := try Gateway.Authorize(order)
34  	return payment
35  }
36
37  func checkout(id string) (string, error[OrderNotFound | GatewayDown | timeout.Expired]) uses Orders, Gateway, Scheduler {
38  	order := try Orders.Find(id)
39  	payment := try timeout.After(100ms) { roundTrip(order) }
    …
70  	outcome := with(Live) { checkout(id) } catch {
    …
75  	case timeout.Expired:
76  		"gateway timed out"
    …
81  func main() uses Console {
```

The source has no span, no logger and no trace call. The checker knows every definition, every capability call and every row, so the emitter names them.

## The recorded run and the answer (Checked (prototype))

The agent runs the query against the checked file. The query validates the trace first: it refuses a trace from another program or another revision of this one, and a trace whose tasks, parents or links break the runtime's rules. So the IDs below mean what the model says, and each "caught" is a link the runtime recorded.

```text
$ go run ./cmd/sketchc trace testdata/trace/timeout.sketch timeout.jsonl
trace of timeout.sketch at revision 3f777fe573d6 (recorder sketchc -rt kernel; schema effra-trace-3)
events: 16 kept of capacity 4096, 0 dropped. Order is partial: seq is the recorder's arrival order; id is the runtime's emission order, in which an event that happens before another has the smaller id, though a smaller id alone does not mean happens-before.

tasks
  #1 latency.main (func) at timeout.sketch:81:1, exit: success
    #2 latency.checkout/timeout.After#1 (timeout block) at timeout.sketch:39:17, exit: interrupted

capability calls (span: call to call-exit, on the recorder's clock)
  id 3    task #1   latency.Orders.Find at timeout.sketch:38:15 in latency.checkout: ok, 3.01µs
  id 6    task #2   host.Scheduler.Sleep at timeout.sketch:32:2 in latency.roundTrip: interrupted, 100.242099ms
  id 14   task #1   host.Console.Println at timeout.sketch:78:2 in latency.report: ok, 75.609µs

events
  seq  id   link  kind            task  def                                    site                   in
  0    1          start           #1    latency.main                           timeout.sketch:81:1
  1    2          construct       #1    latency.Live                           timeout.sketch:70:13   latency.report
  2    3          call            #1    latency.Orders.Find                    timeout.sketch:38:15   latency.checkout
  3    4    3     call-exit       #1    latency.Orders.Find                    timeout.sketch:38:15   latency.checkout
  4    5          start           #2    latency.checkout/timeout.After#1       timeout.sketch:39:17   latency.checkout
  5    6          call            #2    host.Scheduler.Sleep                   timeout.sketch:32:2    latency.roundTrip
  6    7          deadline        #2    latency.checkout/timeout.After#1       timeout.sketch:39:17   latency.checkout
  7    8          cancel-request  #2    latency.checkout/timeout.After#1       timeout.sketch:39:17   latency.checkout
  8    9    6     call-exit       #2    host.Scheduler.Sleep                   timeout.sketch:32:2    latency.roundTrip -> interrupted
  9    10         interrupt       #2    latency.roundTrip                      timeout.sketch:32:2    latency.roundTrip
  10   11         body-exit       #2    latency.checkout/timeout.After#1       timeout.sketch:39:17   latency.checkout -> interrupted (origins 8)
  11   12   11    observe         #2    latency.checkout/timeout.After#1       timeout.sketch:39:17   latency.checkout
  12   13   7     handler         #1    timeout.Expired                        timeout.sketch:75:2    latency.report
  13   14         call            #1    host.Console.Println                   timeout.sketch:78:2    latency.report
  14   15   14    call-exit       #1    host.Console.Println                   timeout.sketch:78:2    latency.report
  15   16         body-exit       #1    latency.main                           timeout.sketch:81:1

cause trace of the root exit (links recorded by the runtime; candidates are labelled)
  task #1 latency.main (func, timeout.sketch:81:1) exited with success
    handled timeout.Expired at timeout.sketch:75:2 in latency.report (task #1, seq 12)
      caught the deadline of task #2 latency.checkout/timeout.After#1 (seq 6): the timeout block timeout.After(100ms) at timeout.sketch:39:17 in latency.checkout, row error[latency.GatewayDown | timeout.Expired]
        call host.Scheduler.Sleep at timeout.sketch:32:2 was open when the deadline passed (call seq 5); it ended interrupted (call-exit seq 8)
        cancel-request of task #2 (seq 7)
        task #2 observed the stop in latency.roundTrip at timeout.sketch:32:2 (seq 9)
        task #2 exited with interrupted (seq 10)

omissions
  - values: argument, result and field values are not serialized (privacy default); failures and defects are named by type
  - functionCalls: direct calls between functions are not events; each site resolves to its enclosing definition
```

## The agent's answer, in the model's words

The request timed out because the deadline of the timeout block `latency.checkout/timeout.After#1` passed before its body finished (seq 6). The block is `timeout.After(100ms)` at `timeout.sketch:39:17`, inside `latency.checkout`. The handler at `timeout.sketch:75:2` recorded that deadline as the occurrence it caught (its `link` is the deadline's `id`, 7), so this is not a guess from the error's type.

- **Where the work was.** The block's task, #2, was in the capability call `host.Scheduler.Sleep` at `timeout.sketch:32:2`, inside `latency.roundTrip`: the call (id 6) started before the deadline (id 7) and its exit (id 9) came after. That line sleeps `2s`, twenty times the deadline; the call span shows it ended interrupted after about 100 ms.
- **How it stopped.** The runtime requested the stop of task #2 (seq 7, id 8). The task observed it at the same sleep (seq 9) and exited interrupted (seq 10); that exit names the request as the origin of its interruption (`origins 8`), a link the runtime recorded. No `call` event names `latency.Gateway.Authorize`, so the gateway was never called, and `latency.GatewayDown` appears nowhere: the gateway did not fail.
- **How it reached the output.** `timeout.After` then failed with `timeout.Expired`. That error is in the block's row `error[latency.GatewayDown | timeout.Expired]` and in the declared row of `latency.checkout`. The catch arm in `latency.report` at `timeout.sketch:75:2` handled it (seq 12) and printed "gateway timed out". The root task `latency.main` exited with success.
- **What to change.** Give the block a deadline above the round trip, or make `latency.roundTrip` faster. The trace does not show the gateway's own latency, because the call was never made.

Every name in this answer is a definition ID or a site from the checked model at revision `3f777fe573d6…`. The answer quotes no argument values, because the serialized trace holds none.

## What a trace from another program gets

```text
$ go run ./cmd/sketchc trace testdata/trace/checkout.sketch timeout.jsonl
timeout.jsonl: refused: trace revision 3f777fe573d6 does not match checkout.sketch revision 22c15ff8fb7b: the trace was recorded from another program or another version of this one
exit status 1
$ echo $?
1
```

## Proposed: the same question through `ef trace` and MCP `code.trace`

```sh
ef trace examples/checkout.ef run.jsonl --why timeout.Expired
```

```json
{"tool": "code.trace", "arguments": {"file": "examples/checkout.ef", "trace": "run.jsonl", "expectedRevision": "3f777fe5…", "why": "timeout.Expired"}}
```

Both answer with the `trace-evidence-1` envelope (`schema`, `producer`, `revision`, `budgets`, `omissions`, `fingerprint`, then the tasks, the calls, the events and the cause trace), the fields `sketchc trace -json` prints today. Neither exists yet.
