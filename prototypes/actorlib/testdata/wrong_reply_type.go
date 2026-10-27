package examples

import (
	"effra.local/prototype/prototypes/actorlib"
	"effra.local/prototype/runtime/rt"
)

// Q1 negative case. Ask types the reply from the request's parameter alone.
// Get carries a Reply[int64, Never], so a request built around a
// Reply[string, Never] does not compile: no projection is needed to reject a
// wrong reply type.
func countAsString(t *rt.Task, c actorlib.Address[CounterMsg]) (string, error) {
	return actorlib.Ask(t, c, func(r actorlib.Reply[string, actorlib.Never]) CounterMsg {
		return Get{Reply: r}
	})
}
