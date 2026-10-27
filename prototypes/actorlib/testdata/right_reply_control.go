package examples

import (
	"ergo.local/prototype/prototypes/actorlib"
	"ergo.local/prototype/runtime/rt"
)

// Positive control for the negative cases: the same overlay, with the reply
// type that Get carries, compiles.
func countAgain(t *rt.Task, c actorlib.Address[CounterMsg]) (int64, error) {
	return actorlib.Ask(t, c, func(r actorlib.Reply[int64, actorlib.Never]) CounterMsg {
		return Get{Reply: r}
	})
}
