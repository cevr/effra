package examples

import (
	"slices"

	"effra.local/prototype/prototypes/actorlib"
	"effra.local/prototype/runtime/rt"
)

// RoomFull lowers `type RoomFull struct{ Limit int64 }`.
type RoomFull struct{ Limit int64 }

func (RoomFull) Error() string { return "room full" }

// Room lowers the roster's state, `type Room struct{ limit int64; names []string }`.
type Room struct {
	limit int64
	names []string
}

// RosterMsg lowers `type RosterMsg interface{ Join | Leave | Members }`.
// Three replies of three types travel in one union.
type RosterMsg interface {
	isRosterMsg()
	Close()
}

// Join lowers `type Join struct{ Name string; Reply actor.Reply[int64, RoomFull] }`.
type Join struct {
	Name  string
	Reply actorlib.Reply[int64, RoomFull]
}

// Leave lowers `type Leave struct{ Name string; Reply actor.Reply[int64, actor.Never] }`.
type Leave struct {
	Name  string
	Reply actorlib.Reply[int64, actorlib.Never]
}

// Members lowers `type Members struct{ Reply actor.Reply[[]string, actor.Never] }`.
type Members struct {
	Reply actorlib.Reply[[]string, actorlib.Never]
}

func (Join) isRosterMsg()    {}
func (Leave) isRosterMsg()   {}
func (Members) isRosterMsg() {}

func (m Join) Close()    { m.Reply.Close() }
func (m Leave) Close()   { m.Reply.Close() }
func (m Members) Close() { m.Reply.Close() }

// rosterHandler lowers the sketch's `serveRoom`. Its paths call library code
// that can panic before the reply is consumed, so the lowering defers the
// release of m; after Send or Fail the release answers nothing.
func rosterHandler(_ *rt.Task, r Room, m RosterMsg) (actorlib.Next[Room], error) {
	defer m.Close()
	switch m := m.(type) {
	case Join:
		if int64(len(r.names)) >= r.limit {
			m.Reply.Fail(RoomFull{Limit: r.limit})
			return actorlib.Continue(r), nil
		}
		r.names = append(r.names, m.Name)
		m.Reply.Send(int64(len(r.names)))
		return actorlib.Continue(r), nil
	case Leave:
		if i := slices.Index(r.names, m.Name); i >= 0 {
			r.names = slices.Delete(r.names, i, i+1)
		}
		m.Reply.Send(int64(len(r.names)))
		return actorlib.Continue(r), nil
	case Members:
		// Value semantics: Reply.Send copies, so the reply never shares the
		// state's backing array (Q7).
		m.Reply.Send(r.names)
		return actorlib.Continue(r), nil
	}
	panic("unreachable: RosterMsg is sealed")
}

// StartRoom lowers `actor.Start(Room{limit: limit}, 64, serveRoom)`.
func StartRoom(s *rt.Scope, limit int64, capacity int) actorlib.Actor[RosterMsg] {
	return actorlib.Start(s, "room", Room{limit: limit}, capacity, rosterHandler)
}

// JoinRoom lowers the sketch's `JoinRoom`.
func JoinRoom(t *rt.Task, room actorlib.Address[RosterMsg], name string) (int64, error) {
	return actorlib.Ask(t, room, func(r actorlib.Reply[int64, RoomFull]) RosterMsg {
		return Join{Name: name, Reply: r}
	})
}

// LeaveRoom lowers the sketch's `LeaveRoom`.
func LeaveRoom(t *rt.Task, room actorlib.Address[RosterMsg], name string) (int64, error) {
	return actorlib.Ask(t, room, func(r actorlib.Reply[int64, actorlib.Never]) RosterMsg {
		return Leave{Name: name, Reply: r}
	})
}

// RoomMembers lowers the sketch's `RoomMembers`.
func RoomMembers(t *rt.Task, room actorlib.Address[RosterMsg]) ([]string, error) {
	return actorlib.Ask(t, room, func(r actorlib.Reply[[]string, actorlib.Never]) RosterMsg {
		return Members{Reply: r}
	})
}

// The agent form of the roster: roster-agent.go.sketch. The protocol is the
// methods of Room, which take a lent *Room receiver; there is no message
// union and no reply address.

// Join lowers `func (r *Room) Join(name string) (int64, error[RoomFull])`.
func (r *Room) Join(name string) (int64, error) {
	if int64(len(r.names)) >= r.limit {
		return 0, RoomFull{Limit: r.limit}
	}
	r.names = append(r.names, name)
	return int64(len(r.names)), nil
}

// Leave lowers `func (r *Room) Leave(name string) int64`.
func (r *Room) Leave(name string) int64 {
	if i := slices.Index(r.names, name); i >= 0 {
		r.names = slices.Delete(r.names, i, i+1)
	}
	return int64(len(r.names))
}

// Members lowers `func (r *Room) Members() []string`. The result leaves the
// agent through Call, which copies it.
func (r *Room) Members() []string { return r.names }

// StartRoomAgent lowers `actor.StartAgent(Room{limit: limit}, 64)`.
func StartRoomAgent(s *rt.Scope, limit int64, capacity int) actorlib.Agent[Room] {
	return actorlib.StartAgent(s, "room-agent", Room{limit: limit}, capacity)
}

// AgentJoin lowers `actor.Call(room, func(r *Room) (int64, error[RoomFull]) { return r.Join(name) })`.
func AgentJoin(t *rt.Task, room actorlib.AgentAddress[Room], name string) (int64, error) {
	return actorlib.Call(t, room, func(_ *rt.Task, r *Room) (int64, error) { return r.Join(name) })
}

// AgentLeave lowers `actor.Call(room, func(r *Room) int64 { return r.Leave(name) })`.
func AgentLeave(t *rt.Task, room actorlib.AgentAddress[Room], name string) (int64, error) {
	return actorlib.Call(t, room, func(_ *rt.Task, r *Room) (int64, error) { return r.Leave(name), nil })
}

// AgentMembers lowers `actor.Call(room, func(r *Room) []string { return r.Members() })`.
func AgentMembers(t *rt.Task, room actorlib.AgentAddress[Room]) ([]string, error) {
	return actorlib.Call(t, room, func(_ *rt.Task, r *Room) ([]string, error) { return r.Members(), nil })
}
