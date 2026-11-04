package anticheat

import (
	"testing"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
)

// TestOpLogic5FiresOnceThresholdCrossed checks the monotonic-counter behavior verified
// against Client.ts this session: reset to 0 at login, `+= a` per call, no extra packet
// below 66, and once at/above 66 the extra packet fires on every subsequent call too
// (not a one-shot, not a modulo cycle).
func TestOpLogic5FiresOnceThresholdCrossed(t *testing.T) {
	c := &Counters{}
	c.Reset()

	// a=10 per call: below threshold for the first 6 calls (60), crosses on the 7th (70).
	for i := 1; i <= 6; i++ {
		w := packet.NewWriter(8)
		c.BeforeOpPlayer4(w, 10)
		if w.Pos != 0 {
			t.Fatalf("call %d: expected no anti-cheat packet yet (opLogic5=%d), got %d bytes written", i, i*10, w.Pos)
		}
	}

	w := packet.NewWriter(8)
	c.BeforeOpPlayer4(w, 10)
	if w.Pos == 0 {
		t.Fatal("call 7 (opLogic5=70 >= 66): expected the anti-cheat packet, got none")
	}
	// ANTICHEAT_OPLOGIC5 opcode byte (ISAAC-obfuscated with no cipher attached here, so
	// raw) followed by p1(154).
	if w.Pos != 2 || w.Data[1] != 154 {
		t.Fatalf("got %v, want a 2-byte packet ending in 154", w.Data[:w.Pos])
	}

	// It keeps firing on every subsequent qualifying call, not just once.
	w2 := packet.NewWriter(8)
	c.BeforeOpPlayer4(w2, 10)
	if w2.Pos == 0 {
		t.Fatal("call 8: expected the anti-cheat packet to keep firing past threshold")
	}
}

// TestOpLogic6GatesOnLowBitsOfA checks the (a & 3) == 0 increment condition specifically,
// since it's easy to misport as an unconditional increment.
func TestOpLogic6GatesOnLowBitsOfA(t *testing.T) {
	c := &Counters{}
	c.Reset()

	// a=1 (a&3 != 0): never increments, so 200 calls never cross the threshold.
	for i := 0; i < 200; i++ {
		w := packet.NewWriter(8)
		c.BeforeInvButton1(w, 1)
		if w.Pos != 0 {
			t.Fatalf("call %d with a=1 (should never increment opLogic6): unexpected packet", i)
		}
	}

	// a=4 (a&3 == 0): increments every call, crosses 133 on the 133rd such call.
	c.Reset()
	for i := 1; i <= 132; i++ {
		w := packet.NewWriter(8)
		c.BeforeInvButton1(w, 4)
		if w.Pos != 0 {
			t.Fatalf("call %d: expected no packet yet (opLogic6=%d)", i, i)
		}
	}
	w := packet.NewWriter(8)
	c.BeforeInvButton1(w, 4)
	if w.Pos == 0 {
		t.Fatal("call 133: expected the anti-cheat packet")
	}
}

func written(fn func(w *packet.Packet)) int {
	w := packet.NewWriter(16)
	fn(w)
	return w.Pos
}

// TestSideChannelIsOnByDefaultAndSwitchable checks the off switch: a zero Counters sends as
// before, SetEnabled(false) writes nothing, and the counters keep advancing while off.
func TestSideChannelIsOnByDefaultAndSwitchable(t *testing.T) {
	var c Counters
	if !c.Enabled() {
		t.Fatal("a zero Counters must send side-packets, as before the switch existed")
	}
	// Past the OPLOGIC5 threshold in one step: the side-packet is opcode + p1.
	if n := written(func(w *packet.Packet) { c.BeforeOpPlayer4(w, opLogic5Threshold) }); n != 2 {
		t.Fatalf("enabled OPLOGIC5 wrote %d bytes, want 2", n)
	}

	c.SetEnabled(false)
	if c.Enabled() {
		t.Fatal("SetEnabled(false) left it enabled")
	}
	if n := written(func(w *packet.Packet) { c.BeforeOpPlayer4(w, 1) }); n != 0 {
		t.Fatalf("disabled OPLOGIC5 wrote %d bytes, want 0", n)
	}
	for i := 0; i < opLogic6Threshold; i++ {
		if n := written(func(w *packet.Packet) { c.BeforeInvButton1(w, 0) }); n != 0 {
			t.Fatalf("disabled OPLOGIC6 wrote %d bytes on call %d, want 0", n, i)
		}
	}

	// The counters kept advancing while off, so switching back on sends at once.
	c.SetEnabled(true)
	if n := written(func(w *packet.Packet) { c.BeforeInvButton1(w, 0) }); n != 3 {
		t.Fatalf("re-enabled OPLOGIC6 wrote %d bytes, want 3 (opcode + p2)", n)
	}
}

// TestOpLogic9FiresFromThe116thHeld4 ports Client.ts:9168-9172: +1 per OP_HELD4, nothing on
// the first 115, then opcode + p3(13018169) on the 116th and every one after it.
func TestOpLogic9FiresFromThe116thHeld4(t *testing.T) {
	var c Counters
	for i := 1; i < opLogic9Threshold; i++ {
		if n := written(func(w *packet.Packet) { c.BeforeOpHeld4(w) }); n != 0 {
			t.Fatalf("call %d: wrote %d bytes before the threshold", i, n)
		}
	}
	for call := opLogic9Threshold; call <= opLogic9Threshold+1; call++ {
		w := packet.NewWriter(8)
		c.BeforeOpHeld4(w)
		want := []byte{clientprot.ANTICHEAT_OPLOGIC9, 0xC6, 0xA4, 0x39} // 13018169 = 0xC6A439
		if w.Pos != 4 || string(w.Data[:4]) != string(want) {
			t.Fatalf("call %d: wrote %x, want %x", call, w.Data[:w.Pos], want)
		}
	}
	c.Reset()
	if n := written(func(w *packet.Packet) { c.BeforeOpHeld4(w) }); n != 0 {
		t.Fatalf("Reset must zero oplogic9: wrote %d bytes", n)
	}
	c.SetEnabled(false)
	for i := 0; i < opLogic9Threshold; i++ {
		if n := written(func(w *packet.Packet) { c.BeforeOpHeld4(w) }); n != 0 {
			t.Fatalf("disabled OPLOGIC9 wrote %d bytes", n)
		}
	}
}

// TestOpObjCountersFollowClientTs covers Client.ts:8780-8820: oplogic7 counts only x&3 == 0
// and fires p4(0) from the 123rd; oplogic8 sums z and fires p1(19) from 75; oplogic3 sums the
// build base z and fires p4(0) from 118.
func TestOpObjCountersFollowClientTs(t *testing.T) {
	var c Counters
	for i := 0; i < 300; i++ {
		if n := written(func(w *packet.Packet) { c.BeforeOpObj1(w, 1) }); n != 0 {
			t.Fatalf("OPLOGIC7 advanced on x&3 != 0")
		}
	}
	for i := 1; i < opLogic7Threshold; i++ {
		if n := written(func(w *packet.Packet) { c.BeforeOpObj1(w, 8) }); n != 0 {
			t.Fatalf("OPLOGIC7 fired early on call %d", i)
		}
	}
	w := packet.NewWriter(8)
	c.BeforeOpObj1(w, 8)
	if want := []byte{clientprot.ANTICHEAT_OPLOGIC7, 0, 0, 0, 0}; string(w.Data[:w.Pos]) != string(want) {
		t.Fatalf("OPLOGIC7 at threshold: %x, want %x", w.Data[:w.Pos], want)
	}

	if n := written(func(w *packet.Packet) { c.BeforeOpObj4(w, 74) }); n != 0 {
		t.Fatal("OPLOGIC8 fired at 74")
	}
	w = packet.NewWriter(8)
	c.BeforeOpObj4(w, 1)
	if want := []byte{clientprot.ANTICHEAT_OPLOGIC8, 19}; string(w.Data[:w.Pos]) != string(want) {
		t.Fatalf("OPLOGIC8 at 75: %x, want %x", w.Data[:w.Pos], want)
	}

	if n := written(func(w *packet.Packet) { c.BeforeOpObj5(w, 117) }); n != 0 {
		t.Fatal("OPLOGIC3 fired at 117")
	}
	w = packet.NewWriter(8)
	c.BeforeOpObj5(w, 1)
	if want := []byte{clientprot.ANTICHEAT_OPLOGIC3, 0, 0, 0, 0}; string(w.Data[:w.Pos]) != string(want) {
		t.Fatalf("OPLOGIC3 at 118: %x, want %x", w.Data[:w.Pos], want)
	}

	c.Reset()
	for _, f := range []func(*packet.Packet){
		func(w *packet.Packet) { c.BeforeOpObj1(w, 8) },
		func(w *packet.Packet) { c.BeforeOpObj4(w, 1) },
		func(w *packet.Packet) { c.BeforeOpObj5(w, 1) },
	} {
		if n := written(f); n != 0 {
			t.Fatal("Reset left an OP_OBJ counter past its threshold")
		}
	}
}

// TestCycleLogic2EveryInteractWithLoc covers Client.ts:5718-5741: nothing for 1086 loc
// interactions, the packet on the 1087th, then the count starts again from 0. With a random
// source that always answers 0 both optional words are written, so the payload is 18 bytes.
func TestCycleLogic2EveryInteractWithLoc(t *testing.T) {
	c := Counters{rnd: func(int) int { return 0 }}
	for i := 1; i <= cycleLogic2Limit; i++ {
		if n := written(func(w *packet.Packet) { c.BeforeInteractWithLoc(w) }); n != 0 {
			t.Fatalf("fired early on interaction %d", i)
		}
	}
	w := packet.NewWriter(32)
	c.BeforeInteractWithLoc(w)
	want := []byte{clientprot.ANTICHEAT_CYCLELOGIC2, 18,
		0x41, 0x97, // 16791
		254,
		0, 0, // random p2
		0x3F, 0x00, // 16128
		0xCD, 0x82, // 52610
		0, 0, // random p2
		0xD8, 0x7C, // 55420
		0x88, 0xD1, // 35025
		0xB6, 0x24, // 46628
		0, // random p1
	}
	if string(w.Data[:w.Pos]) != string(want) {
		t.Fatalf("got % x\nwant % x", w.Data[:w.Pos], want)
	}
	// Random source answering 1: both optional words are skipped and the length says 14.
	c.rnd = func(n int) int { return 1 }
	for i := 1; i <= cycleLogic2Limit; i++ {
		if n := written(func(w *packet.Packet) { c.BeforeInteractWithLoc(w) }); n != 0 {
			t.Fatalf("did not start again from 0 (fired on %d)", i)
		}
	}
	w = packet.NewWriter(32)
	c.BeforeInteractWithLoc(w)
	if w.Pos != 16 || w.Data[1] != 14 || w.Data[2] != 254 {
		t.Fatalf("without the optional words: % x", w.Data[:w.Pos])
	}
	// Reset (login) does not clear it, as Client.ts never does.
	for i := 0; i < 10; i++ {
		c.BeforeInteractWithLoc(packet.NewWriter(32))
	}
	c.Reset()
	if c.cycleLogic2 != 10 {
		t.Fatalf("Reset changed cyclelogic2 to %d", c.cycleLogic2)
	}
}

// TestOpLocCountersFollowClientTs: OPLOGIC1 sums the scene-local z and fires p4(0) at 139;
// OPLOGIC2 counts calls and fires p2(37954) at 124.
func TestOpLocCountersFollowClientTs(t *testing.T) {
	var c Counters
	if n := written(func(w *packet.Packet) { c.BeforeOpLoc2(w, 138) }); n != 0 {
		t.Fatal("OPLOGIC1 fired at 138")
	}
	w := packet.NewWriter(8)
	c.BeforeOpLoc2(w, 1)
	if want := []byte{clientprot.ANTICHEAT_OPLOGIC1, 0, 0, 0, 0}; string(w.Data[:w.Pos]) != string(want) {
		t.Fatalf("OPLOGIC1 at 139: % x", w.Data[:w.Pos])
	}
	for i := 1; i < opLogic2Threshold; i++ {
		if n := written(func(w *packet.Packet) { c.BeforeOpLoc3(w) }); n != 0 {
			t.Fatalf("OPLOGIC2 fired on call %d", i)
		}
	}
	w = packet.NewWriter(8)
	c.BeforeOpLoc3(w)
	if want := []byte{clientprot.ANTICHEAT_OPLOGIC2, 0x94, 0x42}; string(w.Data[:w.Pos]) != string(want) {
		t.Fatalf("OPLOGIC2 at 124: % x, want % x", w.Data[:w.Pos], want)
	}
}
