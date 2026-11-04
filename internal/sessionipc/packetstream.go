package sessionipc

import "rsb0t-go/internal/gamestate"

// PacketFrame is one decoded server packet, wire-shaped identically to
// internal/client.Session's own Packet (Opcode/Payload) but declared independently so
// this package never needs to import internal/client — only a future session-holder
// ("host") process would. A host forwards one of these, in order, for every packet its
// ReadLoop already gets to observe (see client.Session.ReadLoop's own doc comment:
// "onPacket (optional) sees every packet after the state has applied it"), and
// StateMirror below feeds the same bytes into a second, independent gamestate.State on
// the brain side.
type PacketFrame struct {
	Opcode  int
	Payload []byte
}

// StateMirror rebuilds a gamestate.State purely from a stream of PacketFrames, with no
// session, socket or ISAAC cipher of its own. gamestate.State.Handle only ever
// interprets already-decrypted, already-length-framed payload bytes (see its own
// package doc comment — it exists precisely because a headless client has no live
// rendering client to introspect and rebuilds everything from the packet stream), so
// replaying the same (opcode, payload) sequence a real session observed reproduces the
// identical State a script reading ctx.Session.State() sees today, byte for byte.
//
// This is the concrete mechanism behind HOTRELOAD.md's claim that state *reads* need no
// per-action RPC at all: mirroring is "free" once packets are forwarded. A state
// *write* (any of the internal/actions functions bot scripts call — OpPlayer4,
// InvButton, ...) is the opposite case: it mutates the host's own ISAAC cipher state and
// anticheat.Counters, so it cannot be replayed locally and must stay a real call into
// the host process — see ActionCall/Dispatcher in action.go.
type StateMirror struct {
	state *gamestate.State
}

// NewStateMirror builds an empty mirror, matching a fresh session's own gamestate.New()
// at connect time.
func NewStateMirror() *StateMirror {
	return &StateMirror{state: gamestate.New()}
}

// Apply feeds one forwarded packet into the mirror, exactly like the host's own
// gamestate.State.Handle would for the real packet.
func (m *StateMirror) Apply(f PacketFrame) {
	m.state.Handle(f.Opcode, f.Payload)
}

// State returns the mirrored gamestate.State — safe to read concurrently with Apply
// (gamestate.State guards its own fields with an internal mutex), and safe to hand
// straight to any code written against ctx.Session.State() today, unchanged.
func (m *StateMirror) State() *gamestate.State {
	return m.state
}
