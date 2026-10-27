# 2026-10-27: actors as an ordinary library

Labels: **Proposed**. Source: the [#101](https://github.com/cevr/effra/issues/101) spike, [actors as an ordinary library](../research/actor-library.md). No `ef check` and no prototype compiler accepts these snippets yet. The Go they lower to is in [`prototypes/actorlib`](../../prototypes/actorlib/doc.go), where it builds and passes its tests under `-race`. It supersedes the `Actor<P>` projection and `actors.post` operation references of [docs/specs/actors.md](../specs/actors.md) (2026-10-06) and refines the `actor.Start(machine, capacity)` line of [the 2026-10-27 language preview](2026-10-27-go-sketch.md).

Owner, 2026-10-27: "Let's see if we can make actor a library mechanism."

## What changed

`actor` is an ordinary package. A message that wants an answer carries a typed reply address, and `actor.Ask` reads the reply type from the request function. There is no `actor` keyword, no `spawn` and no compiler-derived client.

## Proposed: a counter

```go
type CounterMsg interface{ Inc | Get }

type Inc struct{ By int64 }
type Get struct{ Reply actor.Reply[int64] }

func count(n int64, m CounterMsg) actor.Next[int64] {
	switch m := m.(type) {
	case Inc:
		return actor.Continue(n + m.By)
	case Get:
		m.Reply.Send(n) // consumes the reply; a second Send is a use after consume
		return actor.Continue(n)
	}
}

func Count(c actor.Address[CounterMsg]) (int64, error[actor.Stopped | actor.NoReply]) {
	return actor.Ask(c, func(r actor.Reply[int64]) CounterMsg { return Get{r} })
}
```

A `Get` built around an `actor.Reply[string]` is a type error. A handler that drops the reply releases it, and the release answers `NoReply`.

## Proposed: the same protocol as calls on a state value

```go
func (r *Room) Join(name string) (int64, error[RoomFull]) {
	if len(r.names) >= r.limit {
		return RoomFull{r.limit}
	}
	r.names = append(r.names, name)
	return len(r.names)
}

func JoinRoom(room actor.AgentAddress[Room], name string) (int64, error[RoomFull | actor.Stopped | actor.NoReply]) {
	return actor.Call(room, func(r *Room) (int64, error[RoomFull]) { return r.Join(name) })
}
```

The protocol is `Room`'s methods with a `*Room` receiver. `actor.Call` lends the state to the function literal for one call, and the borrow cannot escape it. The reply is the literal's return value, so there is no message union and no reply address. The literal's parameter types are spelled until [#214](https://github.com/cevr/effra/issues/214) infers them from `Call`'s expected type: `actor.Call(room, func(r) { r.Join(name) })`.

## Not claimed

- No checker accepts these snippets. The rules they rely on are spec Gaps 35 to 47, for #196 and #197.
- No actor syntax is proposed. The note's route-2 sketches exist only to measure size: route 1 costs 1.66x the tokens for an agent used inline (1.54x with #214), 2.65x to 3.38x for message-union handler actors and 1.06x for a machine.
- No performance parity: `Ask` and start/stop cost more than the hand-written Go actor (the note's Q8 table).
- No supervision, durable addresses or byte budgets.
