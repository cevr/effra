// Package examples is the hand lowering of the #101 sketch callers in
// docs/research/actor-library/examples: counter.go.sketch,
// roster.go.sketch, roster-agent.go.sketch and breaker.go.sketch. Each
// function names the sketch line it lowers.
package examples

import (
	"effra.local/prototype/prototypes/actorlib"
	"effra.local/prototype/runtime/rt"
)

// CounterMsg lowers `type CounterMsg interface{ Inc | Get }`. Get holds a
// resource, so the union is a resource (Gap A2) and gets a generated Close.
type CounterMsg interface {
	isCounterMsg()
	Close()
}

// Inc lowers `type Inc struct{ By int64 }`.
type Inc struct{ By int64 }

// Get lowers `type Get struct{ Reply actor.Reply[int64, actor.Never] }`.
type Get struct {
	Reply actorlib.Reply[int64, actorlib.Never]
}

func (Inc) isCounterMsg() {}
func (Get) isCounterMsg() {}

// Close: Inc holds no resource; Get's is the generated field release (R3).
func (Inc) Close()   {}
func (m Get) Close() { m.Reply.Close() }

// countHandler lowers the sketch's `count`. Neither path can panic or poll
// between receipt and consumption, so no deferred release is emitted: Inc
// owns nothing and Get consumes its reply.
func countHandler(_ *rt.Task, n int64, m CounterMsg) (actorlib.Next[int64], error) {
	switch m := m.(type) {
	case Inc:
		return actorlib.Continue(n + m.By), nil
	case Get:
		m.Reply.Send(n)
		return actorlib.Continue(n), nil
	}
	panic("unreachable: CounterMsg is sealed")
}

// StartCounter lowers `actor.Start(0, 64, count)`.
func StartCounter(s *rt.Scope, capacity int) actorlib.Actor[CounterMsg] {
	return actorlib.Start(s, "counter", int64(0), capacity, countHandler)
}

// Count lowers the sketch's `Count`: Ask with a request built from Get.
func Count(t *rt.Task, c actorlib.Address[CounterMsg]) (int64, error) {
	return actorlib.Ask(t, c, func(r actorlib.Reply[int64, actorlib.Never]) CounterMsg {
		return Get{Reply: r}
	})
}
