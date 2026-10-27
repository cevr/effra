package examples

import (
	"ergo.local/prototype/prototypes/actorlib"
	"ergo.local/prototype/runtime/rt"
)

// Q1 and Q3 negative case. Get's reply has an empty error row
// (Reply[int64, Never]), so a handler that fails it with RoomFull does not
// compile: the failure type travels in the reply type too.
func failCount(t *rt.Task, n int64, m CounterMsg) (actorlib.Next[int64], error) {
	if m, ok := m.(Get); ok {
		m.Reply.Fail(t, RoomFull{Limit: 1})
	}
	return actorlib.Continue(n), nil
}
