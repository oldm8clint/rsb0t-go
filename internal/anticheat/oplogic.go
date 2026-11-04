// Package anticheat ports the client's ANTICHEAT_OPLOGICn side-packets: monotonic
// per-session counters (reset once at login, Client.ts:2026-2034) that, once past a
// threshold, make the client send an extra packet alongside every further qualifying
// action. A real client emits these, so a headless client that wants to look like one
// must too. Only the counters Phase 2's actions actually touch are implemented; the
// rest belong to webwalking/combat (Phase 3+).
package anticheat

import (
	"math/rand/v2"
	"sync"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
)

// Thresholds and operands, read from Client.ts's doAction and verified by grep this
// session (see the plan file's Phase 2 notes): oplogic5 += a on OP_PLAYER4 /
// ACCEPT_TRADEREQ, fires ANTICHEAT_OPLOGIC5 + p1(154) at >= 66. oplogic6 increments
// on INV_BUTTON1 only when (a & 3) == 0, fires ANTICHEAT_OPLOGIC6 + p2(6118) at >= 133 —
// INV_BUTTON2..5 (the ones trade's Offer-All/Offer-X/Remove use) carry no counter.
const (
	opLogic5Threshold = 66
	opLogic5Payload   = 154
	opLogic6Threshold = 133
	opLogic6Payload   = 6118
	// oplogic9 increments on every OP_HELD4 and fires ANTICHEAT_OPLOGIC9 + p3(13018169) at
	// >= 116 (Client.ts:9168-9172).
	// OP_OBJ1/4/5 (Client.ts:8780-8820): oplogic7 +1 when the scene-local x is a multiple of 4,
	// fires p4(0) at >= 123; oplogic8 += scene-local z, fires p1(19) at >= 75; oplogic3 +=
	// mapBuildBaseZ, fires p4(0) at >= 118.
	opLogic7Threshold = 123
	opLogic8Threshold = 75
	opLogic8Payload   = 19
	opLogic3Threshold = 118
	opLogic9Threshold = 116
	// OP_LOC2 adds the scene-local z to oplogic1 and fires p4(0) at >= 139; OP_LOC3 adds one
	// to oplogic2 and fires p2(37954) at >= 124 (Client.ts:8972-8989).
	opLogic1Threshold = 139
	opLogic2Threshold = 124
	opLogic2Payload   = 37954
	// Every interactWithLoc (OPLOC1-5, OPLOCT, OPLOCU) adds one to cyclelogic2; past 1086 it
	// sends ANTICHEAT_CYCLELOGIC2 once and starts again from 0 (Client.ts:5718-5741).
	cycleLogic2Limit = 1086
	opLogic9Payload  = 13018169
)

type Counters struct {
	mu       sync.Mutex
	opLogic5 int32
	opLogic4 int32
	opLogic6 int32
	opLogic9 int32
	opLogic7 int32
	opLogic8 int32
	opLogic3 int32
	opLogic1 int32
	opLogic2 int32
	// cycleLogic2 is not zeroed by Reset: Client.ts never clears it at login.
	cycleLogic2 int32
	// rnd is CYCLELOGIC2's random source, rand.IntN when nil; a test sets it.
	rnd func(n int) int
	// off suppresses the side-packets while the counters still advance, so switching them
	// back on later sends exactly what a client that had sent all along would send.
	off bool
}

// SetEnabled switches the side-packets on (the zero value, as a real client) or off. Off is for
// accounts whose owner has no detection concern and wants only the packets an action needs.
func (c *Counters) SetEnabled(on bool) {
	c.mu.Lock()
	c.off = !on
	c.mu.Unlock()
}

// Enabled reports whether the side-packets are sent.
func (c *Counters) Enabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.off
}

// Reset mirrors the login-time zeroing.
func (c *Counters) Reset() {
	c.mu.Lock()
	c.opLogic5 = 0
	c.opLogic4 = 0
	c.opLogic6 = 0
	c.opLogic9 = 0
	c.opLogic7 = 0
	c.opLogic8 = 0
	c.opLogic3 = 0
	c.opLogic1 = 0
	c.opLogic2 = 0
	c.mu.Unlock()
}

// BeforeOpPlayer4 must be called with the outgoing packet writer right before the
// OPPLAYER4 opcode is written, with the same `a` (target player index) that packet
// carries: it advances the counter and, if past threshold, writes the extra anti-cheat
// packet first — exactly the order Client.ts uses.
func (c *Counters) BeforeOpPlayer4(w *packet.Packet, a int32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.opLogic5 += a
	if c.opLogic5 >= opLogic5Threshold && !c.off {
		w.P1Enc(clientprot.ANTICHEAT_OPLOGIC5)
		w.P1(opLogic5Payload)
	}
}

// BeforeOpPlayer1 matches Client.ts: increment, then p1(131) at 52 and later.
func (c *Counters) BeforeOpPlayer1(w *packet.Packet) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.opLogic4++
	if c.opLogic4 >= 52 && !c.off {
		w.P1Enc(clientprot.ANTICHEAT_OPLOGIC4)
		w.P1(131)
	}
}

// BeforeInvButton1 is the INV_BUTTON1 counterpart (unused by trade; kept for bank's
// Withdraw-1 which is inv option 1). Note the (a & 3) == 0 gate on the increment.
func (c *Counters) BeforeInvButton1(w *packet.Packet, a int32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if a&0x3 == 0 {
		c.opLogic6++
	}
	if c.opLogic6 >= opLogic6Threshold && !c.off {
		w.P1Enc(clientprot.ANTICHEAT_OPLOGIC6)
		w.P2(opLogic6Payload)
	}
}

// BeforeOpHeld4 is the OP_HELD4 counterpart (held option 4, e.g. "Rub"): an unconditional
// increment, then ANTICHEAT_OPLOGIC9 + p3(13018169) on every call at or past the threshold,
// written before the OPHELD4 opcode as Client.ts does.
func (c *Counters) BeforeOpHeld4(w *packet.Packet) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.opLogic9++
	if c.opLogic9 >= opLogic9Threshold && !c.off {
		w.P1Enc(clientprot.ANTICHEAT_OPLOGIC9)
		w.P3(opLogic9Payload)
	}
}

// BeforeOpObj1 goes before OPOBJ1, with the ground item's scene-local x (Client.ts's b).
func (c *Counters) BeforeOpObj1(w *packet.Packet, localX int32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if localX&0x3 == 0 {
		c.opLogic7++
	}
	if c.opLogic7 >= opLogic7Threshold && !c.off {
		w.P1Enc(clientprot.ANTICHEAT_OPLOGIC7)
		w.P4(0)
	}
}

// BeforeOpObj4 goes before OPOBJ4, with the ground item's scene-local z (Client.ts's c).
func (c *Counters) BeforeOpObj4(w *packet.Packet, localZ int32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.opLogic8 += localZ
	if c.opLogic8 >= opLogic8Threshold && !c.off {
		w.P1Enc(clientprot.ANTICHEAT_OPLOGIC8)
		w.P1(opLogic8Payload)
	}
}

// BeforeOpObj5 goes before OPOBJ5, with the build area's base z (Client.ts's mapBuildBaseZ).
func (c *Counters) BeforeOpObj5(w *packet.Packet, baseZ int32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.opLogic3 += baseZ
	if c.opLogic3 >= opLogic3Threshold && !c.off {
		w.P1Enc(clientprot.ANTICHEAT_OPLOGIC3)
		w.P4(0)
	}
}

// BeforeOpLoc2 goes before the OPLOC2 packet (and its CYCLELOGIC2), with the loc's scene-local z.
func (c *Counters) BeforeOpLoc2(w *packet.Packet, localZ int32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.opLogic1 += localZ
	if c.opLogic1 >= opLogic1Threshold && !c.off {
		w.P1Enc(clientprot.ANTICHEAT_OPLOGIC1)
		w.P4(0)
	}
}

// BeforeOpLoc3 goes before the OPLOC3 packet (and its CYCLELOGIC2).
func (c *Counters) BeforeOpLoc3(w *packet.Packet) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.opLogic2++
	if c.opLogic2 >= opLogic2Threshold && !c.off {
		w.P1Enc(clientprot.ANTICHEAT_OPLOGIC2)
		w.P2(opLogic2Payload)
	}
}

// BeforeInteractWithLoc goes before every OPLOC1-5, OPLOCT and OPLOCU opcode: interactWithLoc's
// counter and, once past its limit, the variable-length CYCLELOGIC2 packet with its random
// fields, byte for byte as Client.ts writes it.
func (c *Counters) BeforeInteractWithLoc(w *packet.Packet) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cycleLogic2++
	if c.cycleLogic2 <= cycleLogic2Limit {
		return
	}
	c.cycleLogic2 = 0
	if c.off {
		return
	}
	rnd := c.rnd
	if rnd == nil {
		rnd = rand.IntN
	}
	w.P1Enc(clientprot.ANTICHEAT_CYCLELOGIC2)
	w.P1(0)
	start := w.Pos
	if rnd(2) == 0 {
		w.P2(16791)
	}
	w.P1(254)
	w.P2(uint16(rnd(65536)))
	w.P2(16128)
	w.P2(52610)
	w.P2(uint16(rnd(65536)))
	w.P2(55420)
	if rnd(2) == 0 {
		w.P2(35025)
	}
	w.P2(46628)
	w.P1(byte(rnd(256)))
	w.PSize1(w.Pos - start)
}
