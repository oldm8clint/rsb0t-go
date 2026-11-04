package actions

import (
	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
)

// Coalescer collects the packets several actions would send and writes them as ONE Session.Send, one websocket
// message. The engine buffers a websocket message whole (web.ts client.buffer) and reads a player's packets in order
// once a cycle (World.processClientsIn), so packets that leave in one message are read in the same tick unless the
// five-user-events-a-tick limit splits them: that is what a manipulation that needs "knife on a log, Make 1 and the
// tree click in the same tick" relies on (internal/scripts/chopmanipplan), and three separate Sends would let the
// engine's cycle start between them.
//
// It reuses the ordinary action functions, so the bytes are exactly the ones those actions send alone:
//
//	b := actions.NewCoalescer(sess)
//	actions.OpHeldU(b.Session(), ...)
//	actions.IfButton(b.Session(), comp)
//	actions.OpLoc(b.Session(), op, x, z, id)
//	err := b.Flush()
//
// A Coalescer is not safe for concurrent use.
type Coalescer struct {
	real    Session
	pending []func(w *packet.Packet)
	sess    coalescedSession
}

// NewCoalescer wraps a real session.
func NewCoalescer(real Session) *Coalescer {
	c := &Coalescer{real: real}
	c.sess = coalescedSession{c: c}
	return c
}

// Session is the Session to hand to the actions: its Send queues, its State and Cheats are the real session's, so an
// action reads and counts exactly as it would unbatched.
func (c *Coalescer) Session() Session { return &c.sess }

// Pending is how many queued Sends the next Flush will write.
func (c *Coalescer) Pending() int { return len(c.pending) }

// Discard drops everything queued, for a batch that failed half way: nothing of it must reach the server.
func (c *Coalescer) Discard() { c.pending = nil }

// Flush writes everything queued as ONE real Send, in queue order, and empties the queue. The builders run inside the
// real session's send lock, so its opcode cipher stays in step. Nothing queued is a no-op.
func (c *Coalescer) Flush() error {
	if len(c.pending) == 0 {
		return nil
	}
	builders := c.pending
	c.pending = nil
	return c.real.Send(func(w *packet.Packet) {
		for _, build := range builders {
			build(w)
		}
	})
}

type coalescedSession struct{ c *Coalescer }

func (s *coalescedSession) Send(build func(w *packet.Packet)) error {
	s.c.pending = append(s.c.pending, build)
	return nil
}
func (s *coalescedSession) State() *gamestate.State     { return s.c.real.State() }
func (s *coalescedSession) Cheats() *anticheat.Counters { return s.c.real.Cheats() }

// MovementHeld reports whether another caller holds the session's movement (movehold.go). A Coalescer's session is a
// different value from the real one, so the hold, which is keyed on the session it was taken on, does not see it: a
// caller that coalesces a movement or loc click must ask here first, with the real session.
func MovementHeld(s Session) bool { return movementHeld(s) }

// InteractionHeld reports whether another goroutine holds the session's interaction clicks (HoldInput). Like
// MovementHeld, a caller that coalesces an OpNpc must ask here first, with the real session.
func InteractionHeld(s Session) bool { return interactionHeld(s) }
