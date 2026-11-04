package fisherrunner

import (
	"testing"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

// EatFood used to confirm a bite ONLY by waiting for HP to RISE. An account eats precisely when
// something is hitting it, and incoming damage can cancel the heal between samples, so HP often
// never exceeds its pre-eat value even though the server took the bite. The old check then timed
// out and reported failure while the food HAD been eaten -- which presents as an account starving
// with a full pack, not as an error.
//
// Live-found 2026-09-19 at the Baker's stall by the Scarab Swarm session (guard 3-a-tick against
// a cake's 4). The fisher hits the same case: its fund phase pickpockets at Lumbridge, which
// rouses guards. These pin the replacement signal -- the food leaving that slot -- since that is
// what stays true regardless of whether the heal out-ran the damage.

func setPackForEatTest(t *testing.T, f *fakeSession, items ...[2]int32) {
	t.Helper()
	f.state.Handle(serverprot.UPDATE_INV_FULL, func() []byte {
		p := packet.NewWriter(64)
		p.P2(uint16(PlayerPack))
		p.P2(uint16(len(items)))
		for _, it := range items {
			if it[0] != 0 {
				p.P2(uint16(it[0] + 1)) // the wire carries objId+1; 0 means an empty slot
			} else {
				p.P2(0)
			}
			p.P1(byte(it[1]))
		}
		return p.Data[:p.Pos]
	}())
}

// The food is consumed outright and the slot empties. This is the ordinary case for fish.
func TestSlotChangedWhenFoodLeavesThePack(t *testing.T) {
	f := newFakeSession()
	d := &Deps{Session: f}
	setPackForEatTest(t, f, [2]int32{333, 1}) // one trout in slot 0
	if slotChanged(d, 0, 333) {
		t.Fatal("slotChanged reported a bite before anything was eaten")
	}
	setPackForEatTest(t, f) // pack now empty
	if !slotChanged(d, 0, 333) {
		t.Fatal("the food left the pack but slotChanged did not report the bite — this is the starvation bug: EatFood would time out while the food HAD been eaten")
	}
}

// Food that TRANSFORMS rather than leaving: a whole Cake becomes a 2/3 cake in the SAME slot. A
// naive "is the slot empty now?" test misses this entirely and starves on cake.
func TestSlotChangedWhenFoodTransformsInPlace(t *testing.T) {
	f := newFakeSession()
	d := &Deps{Session: f}
	setPackForEatTest(t, f, [2]int32{1891, 1}) // Cake
	if slotChanged(d, 0, 1891) {
		t.Fatal("slotChanged reported a bite before anything was eaten")
	}
	setPackForEatTest(t, f, [2]int32{1893, 1}) // 2/3 cake, same slot, still occupied
	if !slotChanged(d, 0, 1891) {
		t.Fatal("the cake transformed in place but slotChanged did not report the bite — a slot-empty test would miss every partial-food item")
	}
}

// An unrelated slot changing must not read as a bite, or a bank/loot update during the confirm
// window would fake a successful eat.
func TestSlotChangedIgnoresOtherSlots(t *testing.T) {
	f := newFakeSession()
	d := &Deps{Session: f}
	setPackForEatTest(t, f, [2]int32{333, 1}, [2]int32{995, 100})
	setPackForEatTest(t, f, [2]int32{333, 1}, [2]int32{995, 250}) // coins changed, food did not
	if slotChanged(d, 0, 333) {
		t.Fatal("a change in an unrelated slot was reported as a bite")
	}
}

// A failed inventory read must report "not changed", never success: a read failure faking a
// confirmation would hide a genuinely failed eat, which is the opposite of the bug being fixed.
func TestSlotChangedTreatsAMissingInventoryAsNotChanged(t *testing.T) {
	f := newFakeSession()
	d := &Deps{Session: f}
	if slotChanged(d, 0, 333) {
		t.Fatal("a missing inventory was reported as a bite — a read failure must never fake a confirmation")
	}
}
