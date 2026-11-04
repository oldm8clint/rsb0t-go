package questrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// Druidic Ritual (W22, 18 Oct 2026) runs under a fleet-wide slot of four and Jungle Potion under one of three: the ritual's animals
// stand on a handful of spawns and Wydin's shelf keeps one raw beef and one raw chicken, and the herbs are four shared locs that stand
// empty for a hundred ticks after each pick. The fifth (fourth) account waits and a release lets it in.
func TestDruidicRitualAndJunglePotionRunUnderTheirSlots(t *testing.T) {
	for _, c := range []struct {
		id  string
		cap int
	}{{questplan.DruidicRitualID, 4}, {questplan.JunglePotionID, 3}} {
		if !exclusiveQuests[c.id] || capacityOf(c.id) != c.cap {
			t.Errorf("%s: exclusive %v capacity %d, want true and %d", c.id, exclusiveQuests[c.id], capacityOf(c.id), c.cap)
		}
		now := time.Now()
		ds := make([]*Deps, c.cap+2)
		for i := range ds {
			ds[i] = &Deps{}
		}
		for i := 0; i < c.cap; i++ {
			if !tryHoldQuest(c.id, ds[i], now) {
				t.Fatalf("%s: account %d of %d was refused", c.id, i+1, c.cap)
			}
		}
		if tryHoldQuest(c.id, ds[c.cap], now) {
			t.Errorf("%s: an account over the capacity was admitted", c.id)
		}
		releaseQuest(c.id, ds[0])
		if !tryHoldQuest(c.id, ds[c.cap], now) {
			t.Errorf("%s: a released slot was not given to the waiting account", c.id)
		}
		for _, d := range ds {
			releaseQuest(c.id, d)
		}
		if _, held := slots[c.id]; held {
			t.Errorf("%s: slots left behind: %v", c.id, slots[c.id])
		}
	}
}

// The ritual gives its slot up once every meat is in the pack (DruidPastTheHunt) and does not remember it for good: the account
// that loses the meats is among the animals again. Jungle Potion names no ExclusiveDone and holds its slot to the end of the run.
func TestDruidicRitualFreesItsSlotPastTheHuntAndNeverRemembersIt(t *testing.T) {
	if !slotRepeats[questplan.DruidicRitualID] || slotRepeats[questplan.JunglePotionID] {
		t.Errorf("slotRepeats: druid %v junglepotion %v", slotRepeats[questplan.DruidicRitualID], slotRepeats[questplan.JunglePotionID])
	}
	if questplan.DruidicRitual.ExclusiveDone == nil || questplan.JunglePotion.ExclusiveDone != nil {
		t.Error("ExclusiveDone: the ritual names the end of its hunts, the jungle none")
	}
	e := fakeEngine(&logSink{})
	e.NarrowSlots = true
	q := Quest{Module: questplan.DruidicRitual}
	e.quest = q
	d := e.D
	if !tryHoldQuest(questplan.DruidicRitualID, d, time.Now()) {
		t.Fatal("the account was refused the slot")
	}
	pastHunt := questplan.Snapshot{Journal: questplan.StatusInProgress, Stage: questplan.DruidGivenIngredients, StageKnown: true}
	if !e.freeSlotIfPast(pastHunt) {
		t.Fatal("the slot was not given up past the hunt")
	}
	if r := e.Store.Quests[questplan.DruidicRitualID]; r != nil && r.Hints[HintSlotDone] {
		t.Error("the slot-done hint was remembered for a quest whose hunt can come round again")
	}
	// And with the account past it, four others fill the slots (the capacity is four and the account holds none now).
	others := []*Deps{{}, {}, {}, {}}
	for i, o := range others {
		if !tryHoldQuest(questplan.DruidicRitualID, o, time.Now()) {
			t.Errorf("account %d of 4 was refused after the first gave its slot up", i+1)
		}
	}
	for _, o := range others {
		releaseQuest(questplan.DruidicRitualID, o)
	}
	releaseQuest(questplan.DruidicRitualID, d)
}
