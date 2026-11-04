package bot

import (
	"testing"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/loot"
)

type castRec struct {
	lost loot.Counts
	xp   int
}

func castFeed(w *castWatch, evs ...gamestate.Event) {
	for _, e := range evs {
		w.onEvent(e)
	}
}

func tickEv(n int) gamestate.Event { return gamestate.Event{Kind: gamestate.EventTick, Tick: n} }

func runeGone(id int32, had, now int32) gamestate.Event {
	item := id
	if now == 0 {
		item = 0
	}
	return gamestate.Event{Kind: gamestate.EventInventoryChanged, Comp: lootPackComponent, PreviousID: id, PreviousCount: had, ItemID: item, Count: now}
}

func magicXP(delta int) gamestate.Event {
	return gamestate.Event{Kind: gamestate.EventSkillXP, Skill: loot.MagicStat, XPDelta: delta}
}

// TestCastWatchPairsALawRuneLossWithMagicXP: the runes and the experience of one teleport arrive in
// either order inside a tick and make one cast with exactly the runes lost.
func TestCastWatchPairsALawRuneLossWithMagicXP(t *testing.T) {
	for name, order := range map[string][]gamestate.Event{
		"runes first": {runeGone(loot.AirRuneID, 100, 97), runeGone(loot.FireRuneID, 1, 0), runeGone(loot.LawRuneID, 50, 49), magicXP(35)},
		"xp first":    {magicXP(35), runeGone(loot.LawRuneID, 50, 49), runeGone(loot.AirRuneID, 100, 97), runeGone(loot.FireRuneID, 1, 0)},
	} {
		var got []castRec
		w := newCastWatch(func(lost loot.Counts, xp int) { got = append(got, castRec{lost, xp}) })
		castFeed(w, tickEv(10))
		castFeed(w, order...)
		castFeed(w, tickEv(11))
		if len(got) != 0 {
			t.Fatalf("%s: judged tick 10 before the next tick's packets could be in", name)
		}
		castFeed(w, tickEv(12))
		if len(got) != 1 || got[0].xp != 35 || got[0].lost[loot.LawRuneID] != 1 || got[0].lost[loot.AirRuneID] != 3 || got[0].lost[loot.FireRuneID] != 1 {
			t.Fatalf("%s: casts = %+v, want one Varrock cast", name, got)
		}
		if loot.TeleportName(got[0].lost, got[0].xp) != "Varrock Teleport" {
			t.Fatalf("%s: named %q", name, loot.TeleportName(got[0].lost, got[0].xp))
		}
	}
}

// TestCastWatchIgnoresWhatIsNotACast: a Law rune banked with no experience, an alchemy (Nature and
// Fire runes with experience) and a lone experience gain are none of them a teleport.
func TestCastWatchIgnoresWhatIsNotACast(t *testing.T) {
	var got []castRec
	w := newCastWatch(func(lost loot.Counts, xp int) { got = append(got, castRec{lost, xp}) })
	castFeed(w, tickEv(1), runeGone(loot.LawRuneID, 50, 0)) // deposited
	castFeed(w, tickEv(5), runeGone(561, 10, 9), runeGone(loot.FireRuneID, 20, 15), magicXP(65))
	castFeed(w, tickEv(7), magicXP(40))
	for tick := 4; tick < 12; tick++ {
		castFeed(w, tickEv(tick))
	}
	if len(got) != 0 {
		t.Fatalf("recorded %+v, want nothing", got)
	}
}

// TestCastWatchIgnoresRunesLeavingThroughAnOpenWindow: a bank deposit that lands next to a Magic gain
// is not a cast.
func TestCastWatchIgnoresRunesLeavingThroughAnOpenWindow(t *testing.T) {
	var got []castRec
	w := newCastWatch(func(lost loot.Counts, xp int) { got = append(got, castRec{lost, xp}) })
	open := true
	w.modal = func() bool { return open }
	castFeed(w, tickEv(1), runeGone(loot.LawRuneID, 50, 0), magicXP(65))
	for tick := 2; tick < 8; tick++ {
		castFeed(w, tickEv(tick))
	}
	if len(got) != 0 {
		t.Fatalf("recorded %+v for a deposit", got)
	}
	open = false
	castFeed(w, tickEv(8), runeGone(loot.LawRuneID, 50, 49), magicXP(61))
	for tick := 9; tick < 14; tick++ {
		castFeed(w, tickEv(tick))
	}
	if len(got) != 1 {
		t.Fatalf("recorded %+v, want the one real cast", got)
	}
}

// TestCastWatchTakesAnXPGainOnce: two casts a few ticks apart each take their own experience.
func TestCastWatchTakesAnXPGainOnce(t *testing.T) {
	var got []castRec
	w := newCastWatch(func(lost loot.Counts, xp int) { got = append(got, castRec{lost, xp}) })
	castFeed(w, tickEv(1), runeGone(loot.LawRuneID, 50, 49), runeGone(loot.AirRuneID, 100, 95), magicXP(55))
	castFeed(w, tickEv(2), tickEv(3))
	castFeed(w, tickEv(4), runeGone(loot.LawRuneID, 49, 48), runeGone(loot.AirRuneID, 95, 90), magicXP(55))
	for tick := 5; tick < 12; tick++ {
		castFeed(w, tickEv(tick))
	}
	if len(got) != 2 {
		t.Fatalf("casts = %+v, want two", got)
	}
}

// TestCastWatchWithoutSink records nothing and does not subscribe.
func TestWatchCastsNeedsALootSink(t *testing.T) {
	quiet := &Context{}
	quiet.watchCasts() // must not panic with no session or sink
}
