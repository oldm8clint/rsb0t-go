// Package wire turns a tickmanip batch (a list of Steps) into the packets a real client would write, and
// writes them in ONE socket write.
//
// WHY ONE WRITE. The engine reads the packets a player has queued once per cycle, in order, in
// World.processClientsIn -> NetworkPlayer.decodeIn. A batch whose packets were written one socket
// write at a time can be split across two cycles by a connection that delivers them a few
// milliseconds apart, and the whole point of a manipulation is that its packets are read in the SAME
// cycle. Session.Send writes one WebSocket binary message per call, and an action that builds several
// packets in one call (internal/actions.OpLoc writes its anti-cheat side packets and then the op) is
// already one write; this package extends that to a whole tick's batch.
//
// It reuses the existing action functions (actions.OpHeldU, IfButton, OpLoc, OpNpc, Walk, OpHeld) so
// the bytes are the ones already proven against the TS reference vectors; the only new code is the
// collection of their builders into one Send.
package wire

import (
	"errors"
	"fmt"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/tickmanip"
)

// Batch collects the packet builders of several actions and writes them as one Send.
type Batch struct {
	real    actions.Session
	pending []func(w *packet.Packet)
	sess    batchSession
}

// NewBatch wraps a real session.
func NewBatch(real actions.Session) *Batch {
	b := &Batch{real: real}
	b.sess = batchSession{b: b}
	return b
}

// Session is an actions.Session whose Send queues instead of writing; State and Cheats are the real
// session's, so an action reads and counts exactly as it would unbatched.
func (b *Batch) Session() actions.Session { return &b.sess }

// Pending is how many queued Sends the next Flush will write (each may hold several packets).
func (b *Batch) Pending() int { return len(b.pending) }

// Flush writes everything queued as ONE real Send, in queue order, and empties the queue. The
// builders run inside the real session's send lock, so the ISAAC opcode stream stays in order.
func (b *Batch) Flush() error {
	if len(b.pending) == 0 {
		return nil
	}
	builders := b.pending
	b.pending = nil
	return b.real.Send(func(w *packet.Packet) {
		for _, build := range builders {
			build(w)
		}
	})
}

type batchSession struct{ b *Batch }

func (s *batchSession) Send(build func(w *packet.Packet)) error {
	s.b.pending = append(s.b.pending, build)
	return nil
}
func (s *batchSession) State() *gamestate.State     { return s.b.real.State() }
func (s *batchSession) Cheats() *anticheat.Counters { return s.b.real.Cheats() }

// Env is everything a Step needs to become a packet.
type Env struct {
	Batch *Batch
	// InvCom is the backpack's TYPE_INV component (actions.PlayerPack, 3214): the component every held-item
	// packet names, and the one the client's pack reading is keyed by.
	InvCom int
	// KnifeID and LogID are the object ids of the knife and of the log it is used on.
	KnifeID, LogID int32
	// MakeComp is the component of skill_multi3:make1_a, "Make 1" of the first product (15 arrow shafts).
	MakeComp int
	// HorseyID is a toy horsey's object id, or 0 when the lab did not give one.
	HorseyID int32
	// Route is the corner path a WalkClick sends (path[0] the player's own tile) and Run holds run on it.
	Route func() []pathfind.NavPoint
	Run   bool
	// AlchID is the item a cast targets, RingID a ring of dueling, NecklaceID a games necklace, BoneID a bone: the
	// backpack items the energy arms' steps name.
	AlchID, RingID, NecklaceID, BoneID int32
	// WieldIDs are two daggers; a Wield step wields whichever one is in the pack, so it alternates.
	WieldIDs [2]int32
	// RawID is the raw fish a CookUse step uses on the range, and UseOnRange sends the OPLOCU for it.
	RawID      int32
	UseOnRange func(s actions.Session, rawID int32, slot, com int) error
	// Leader, when set, is queued BEFORE the steps of every Write: the lab's ack packet (tickmanip.AckName), whose answer
	// tells the recorder which tick the server read the batch's first packet in. It leads, not trails, because the engine
	// reads at most five user events a tick: a batch that reaches the server late can be split, and the ack at its head
	// still names the tick its first packet was read, while the model (which counts the ack as a user event, tickmanip.Ack)
	// reproduces the split exactly.
	Leader func(s actions.Session) error
	// Click writes the skill's own first op (OPLOC1 on a tree, OPNPC1 or OPNPC3 on a spot) to the session it is
	// handed. It is a function because the target (a tile, an NPC index) changes between windows.
	Click func(s actions.Session) error
}

// ErrNotInPack is returned when a step needs an item the client does not see in its backpack.
var ErrNotInPack = errors.New("wire: the item is not in the client's backpack reading")

// slotOf finds the first backpack slot holding id, skipping slot `not` (-1 for none).
func (e *Env) slotOf(id int32, not int) (int, bool) {
	inv := e.Batch.real.State().Inventory(e.InvCom)
	if inv == nil {
		return 0, false
	}
	for slot, it := range inv.Slots {
		if it.ID == id && slot != not {
			return slot, true
		}
	}
	return 0, false
}

// packHolds is whether the backpack reading holds id at slot.
func (e *Env) packHolds(id int32, slot int) bool {
	inv := e.Batch.real.State().Inventory(e.InvCom)
	return inv != nil && slot >= 0 && slot < len(inv.Slots) && inv.Slots[slot].ID == id
}

// Queue appends one Step's packets to the batch without writing.
func (e *Env) Queue(st tickmanip.Step) error {
	sess := e.Batch.Session()
	switch st {
	case tickmanip.KnifeUse:
		// Same as actions.Equipment.UseOnHeld: the target is the first log slot, the used item the first
		// knife slot, both named with the backpack component.
		knife, ok := e.slotOf(e.KnifeID, -1)
		if !ok {
			return fmt.Errorf("%w: knife %d", ErrNotInPack, e.KnifeID)
		}
		log, ok := e.slotOf(e.LogID, knife)
		if !ok {
			return fmt.Errorf("%w: log %d", ErrNotInPack, e.LogID)
		}
		return actions.OpHeldU(sess, e.LogID, log, e.InvCom, e.KnifeID, knife, e.InvCom)
	case tickmanip.MakeShafts:
		return actions.IfButton(sess, e.MakeComp)
	case tickmanip.MoveOwn:
		x, z, level, ok := e.Batch.real.State().Tile()
		if !ok {
			return errors.New("wire: no position yet for a move on the own tile")
		}
		return actions.Walk(sess, []pathfind.NavPoint{{X: x, Z: z, Level: level}}, false)
	case tickmanip.Horsey:
		if e.HorseyID == 0 {
			return errors.New("wire: no toy horsey configured")
		}
		slot, ok := e.slotOf(e.HorseyID, -1)
		if !ok {
			return fmt.Errorf("%w: toy horsey %d", ErrNotInPack, e.HorseyID)
		}
		return actions.OpHeld(sess, 1, e.HorseyID, slot, e.InvCom)
	case tickmanip.Click:
		if e.Click == nil {
			return errors.New("wire: no click target configured")
		}
		return e.Click(sess)
	case tickmanip.WalkClick:
		if e.Route == nil {
			return errors.New("wire: no route configured")
		}
		return actions.Walk(sess, e.Route(), e.Run)
	case tickmanip.CastHigh, tickmanip.CastLow:
		slot, ok := e.slotOf(e.AlchID, -1)
		if !ok {
			return fmt.Errorf("%w: alchable item %d", ErrNotInPack, e.AlchID)
		}
		spell := "High Level Alchemy"
		if st == tickmanip.CastLow {
			spell = "Low Level Alchemy"
		}
		return actions.CastOnItem(sess, spell, e.AlchID, slot, e.InvCom)
	case tickmanip.RubRing:
		slot, ok := e.slotOf(e.RingID, -1)
		if !ok {
			return fmt.Errorf("%w: ring %d", ErrNotInPack, e.RingID)
		}
		return actions.OpHeld(sess, 4, e.RingID, slot, e.InvCom)
	case tickmanip.RubNecklace:
		slot, ok := e.slotOf(e.NecklaceID, -1)
		if !ok {
			return fmt.Errorf("%w: necklace %d", ErrNotInPack, e.NecklaceID)
		}
		return actions.OpHeld(sess, 4, e.NecklaceID, slot, e.InvCom)
	case tickmanip.CookUse:
		if e.UseOnRange == nil {
			return errors.New("wire: no range configured")
		}
		slot, ok := e.slotOf(e.RawID, -1)
		if !ok {
			return fmt.Errorf("%w: raw fish %d", ErrNotInPack, e.RawID)
		}
		return e.UseOnRange(sess, e.RawID, slot, e.InvCom)
	case tickmanip.Wield:
		for _, id := range e.WieldIDs {
			if id == 0 {
				continue
			}
			if slot, ok := e.slotOf(id, -1); ok {
				return actions.OpHeld(sess, 2, id, slot, e.InvCom)
			}
		}
		return fmt.Errorf("%w: neither wield item %v", ErrNotInPack, e.WieldIDs)
	case tickmanip.ClearMove:
		// The alchable at the slot after the one it is in, which holds something else (or nothing): the server finds no
		// such item there and clears moveClickRequest before it gives up.
		slot, ok := e.slotOf(e.AlchID, -1)
		if !ok {
			return fmt.Errorf("%w: alchable item %d", ErrNotInPack, e.AlchID)
		}
		wrong := (slot + 1) % 28
		for e.packHolds(e.AlchID, wrong) {
			wrong = (wrong + 1) % 28
		}
		return actions.OpHeldU(sess, e.AlchID, wrong, e.InvCom, e.AlchID, slot, e.InvCom)
	case tickmanip.Bury:
		slot, ok := e.slotOf(e.BoneID, -1)
		if !ok {
			return fmt.Errorf("%w: bone %d", ErrNotInPack, e.BoneID)
		}
		return actions.OpHeld(sess, 1, e.BoneID, slot, e.InvCom)
	}
	return fmt.Errorf("wire: unknown step %v", st)
}

// Write queues every step of a batch and writes them as ONE socket write. On an error nothing is written
// (the queue is dropped), so a half batch never reaches the server.
func (e *Env) Write(steps []tickmanip.Step) error {
	if len(steps) == 0 {
		return nil
	}
	if e.Leader != nil {
		if err := e.Leader(e.Batch.Session()); err != nil {
			e.Batch.pending = nil
			return err
		}
	}
	for _, st := range steps {
		if err := e.Queue(st); err != nil {
			e.Batch.pending = nil
			return err
		}
	}
	return e.Batch.Flush()
}
