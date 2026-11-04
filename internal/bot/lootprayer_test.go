package bot

import (
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/protocol/serverprot"
)

type buried struct{ id, xp int }

func testBuryWatch() (*buryWatch, *[]buried, *time.Time) {
	var got []buried
	clock := time.Unix(1000, 0)
	w := newBuryWatch(func(id, xp int) { got = append(got, buried{id, xp}) })
	w.now = func() time.Time { return clock }
	return w, &got, &clock
}

func boneLeaves(slot, id int) gamestate.Event {
	return gamestate.Event{Kind: gamestate.EventInventoryChanged, Comp: lootPackComponent, Slot: slot,
		ItemID: 0, PreviousID: int32(id), Count: 0, PreviousCount: 1}
}

func prayerXP(delta int) gamestate.Event {
	return gamestate.Event{Kind: gamestate.EventSkillXP, Skill: loot.PrayerStat, XPDelta: delta, XP: 100 + delta}
}

func tickEvent(n int) gamestate.Event { return gamestate.Event{Kind: gamestate.EventTick, Tick: n} }

// TestBuryWatchPairsABoneWithItsPrayerXP: content deletes the bone and advances Prayer in one
// script step, so the two halves arrive in the same tick, in either order.
func TestBuryWatchPairsABoneWithItsPrayerXP(t *testing.T) {
	w, got, _ := testBuryWatch()
	w.onEvent(tickEvent(10))
	w.onEvent(boneLeaves(3, 526))
	w.onEvent(prayerXP(4))
	w.onEvent(tickEvent(11))
	w.onEvent(prayerXP(5)) // the other order
	w.onEvent(boneLeaves(4, 532))
	if len(*got) != 2 || (*got)[0] != (buried{526, 4}) || (*got)[1] != (buried{532, 5}) {
		t.Fatalf("recorded %+v, want Bones for 4 xp then Big bones for 5", *got)
	}
}

// TestBuryWatchIgnoresEitherHalfAlone: banking or dropping a bone takes it out of the pack with
// no experience; a quest's Prayer experience arrives with no bone leaving. Neither is a burial.
func TestBuryWatchIgnoresEitherHalfAlone(t *testing.T) {
	w, got, _ := testBuryWatch()
	w.onEvent(tickEvent(10))
	w.onEvent(boneLeaves(0, 526)) // deposited
	w.onEvent(boneLeaves(1, 526))
	w.onEvent(tickEvent(11))
	w.onEvent(tickEvent(12))
	w.onEvent(tickEvent(13))
	w.onEvent(prayerXP(1125)) // The Restless Ghost, three ticks later
	if len(*got) != 0 {
		t.Fatalf("recorded %+v from a deposit and a quest reward, want nothing", *got)
	}
	// A bone leaving the BANK (another component) is not the pack.
	w.onEvent(gamestate.Event{Kind: gamestate.EventInventoryChanged, Comp: 5382, PreviousID: 526, PreviousCount: 1})
	w.onEvent(prayerXP(4))
	if len(*got) != 0 {
		t.Fatalf("a bank change paired with Prayer xp: %+v", *got)
	}
	// Nor is an item that is not a bone, whatever the timing.
	w.onEvent(tickEvent(20))
	w.onEvent(boneLeaves(5, 1891)) // a cake eaten
	w.onEvent(prayerXP(4))
	if len(*got) != 0 {
		t.Fatalf("a cake paired with Prayer xp: %+v", *got)
	}
}

// TestBuryWatchDoesNotPairAcrossTicks: a stale half from an earlier tick never claims a later one.
func TestBuryWatchDoesNotPairAcrossTicks(t *testing.T) {
	w, got, clock := testBuryWatch()
	w.onEvent(tickEvent(10))
	w.onEvent(boneLeaves(0, 526)) // banked, no xp
	w.onEvent(tickEvent(14))
	w.onEvent(prayerXP(4)) // an unrelated gain four ticks later
	if len(*got) != 0 {
		t.Fatalf("paired halves four ticks apart: %+v", *got)
	}
	// And a half left pending when ticks stop arriving expires on the clock.
	w.onEvent(tickEvent(20))
	w.onEvent(boneLeaves(1, 526))
	*clock = clock.Add(10 * time.Second)
	w.onEvent(prayerXP(4))
	if len(*got) != 0 {
		t.Fatalf("paired halves ten seconds apart: %+v", *got)
	}
}

// buryPackets is what the server sends for one burial of the bone in slot, content's
// inv_delslot then stat_advance: an UPDATE_INV_PARTIAL emptying the slot and an UPDATE_STAT with
// Prayer at xp.
func buryPackets(st *gamestate.State, slot, xp int) {
	st.Handle(serverprot.UPDATE_INV_PARTIAL, []byte{byte(lootPackComponent >> 8), byte(lootPackComponent & 0xff), byte(slot), 0, 0, 0})
	st.Handle(serverprot.UPDATE_STAT, []byte{byte(loot.PrayerStat), byte(xp >> 24), byte(xp >> 16), byte(xp >> 8), byte(xp), 1})
}

// TestWatchBurialsRecordsOntoThePrayerCard wires the watch to a real gamestate.State the way the
// Runner does, drives it with the server's own packets, and checks the Drop that reaches the sink.
func TestWatchBurialsRecordsOntoThePrayerCard(t *testing.T) {
	var drops []loot.Drop
	sess := newAPISession()
	st := sess.st
	// The login sync: Prayer's first UPDATE_STAT (which fires nothing) and two Bones in the pack.
	st.Handle(serverprot.UPDATE_STAT, []byte{byte(loot.PrayerStat), 0, 0, 0, 100, 1})
	st.Handle(serverprot.UPDATE_INV_PARTIAL, []byte{byte(lootPackComponent >> 8), byte(lootPackComponent & 0xff), 2, 0x02, 0x0f, 1, 3, 0x02, 0x0f, 1})

	ctx := &Context{Session: sess, Loot: func(d loot.Drop) { drops = append(drops, d) }}
	ctx.watchBurials()
	defer ctx.disposeSubscriptions()
	buryPackets(st, 2, 145)
	if len(drops) != 1 {
		t.Fatalf("recorded %d drops, want one burial", len(drops))
	}
	d := drops[0]
	if d.Source.GroupKey() != "production|prayer" || d.Source.ID != 526 || d.Events != 1 || d.XP != 45 || len(d.Items) != 0 {
		t.Fatalf("burial recorded as %+v", d)
	}
	// No loot sink: nothing subscribes, nothing records.
	quiet := &Context{Session: sess}
	quiet.watchBurials()
	buryPackets(st, 3, 190)
	if len(drops) != 2 || drops[1].XP != 45 {
		t.Fatalf("drops = %+v; the wired context should have seen the second burial and only once", drops)
	}
}
