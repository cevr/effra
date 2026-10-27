package examples

import (
	"ergo.local/prototype/prototypes/actorlib"
	"ergo.local/prototype/runtime/rt"
)

// Q1 negative case, handler side. Get's reply is a Reply[int64, Never], so a
// handler that answers it with a string does not compile.
func countWrongly(t *rt.Task, n int64, m CounterMsg) (actorlib.Next[int64], error) {
	if m, ok := m.(Get); ok {
		m.Reply.Send(t, "ten")
	}
	return actorlib.Continue(n), nil
}
