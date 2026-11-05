package loot

import (
	"testing"
	"time"
)

// TestRunesAndBonesNoKillClaimedGoUnderTheNewestKill: a Death rune or Big bones pile no recent kill
// could claim is the account's newest kill's, with no event of its own; a Knife is still a pickup; a
// rune with no kill to go under is recorded nowhere (a user request: no rune or bone panel).
func TestRunesAndBonesNoKillClaimedGoUnderTheNewestKill(t *testing.T) {
	var kills KillLog
	ogre := NPC(nil, ActionKill, 114, "Ogre")
	cow := NPC(nil, ActionKill, 81, "Cow")
	long := time.Now().Add(-2 * time.Hour)
	kills.Note(ogre, 1, 1, 0, long.Add(-time.Hour))
	kills.Note(cow, 1, 1, 0, long)
	// Two hours is far outside KillWindow: SourceFor refuses it, Latest does not.
	if _, ok := kills.SourceFor(1, 1, 0, time.Now()); ok {
		t.Fatal("a kill two hours old claimed a drop")
	}
	for _, id := range []int{560 /* Death rune */, 532 /* Big bones */, 526 /* Bones */, 554} {
		src, events, ok := PickupSource(nil, &kills, id, "")
		if !ok || src.Name != "Cow" || events != 0 {
			t.Errorf("obj %d filed as %+v events %d ok %v, want the Cow kill with no event", id, src, events, ok)
		}
	}
	src, events, ok := PickupSource(nil, &kills, 946, "Knife")
	if !ok || src.Kind != KindPickup || events != 1 {
		t.Errorf("a knife filed as %+v events %d ok %v, want a pickup card", src, events, ok)
	}
	var none KillLog
	if _, _, ok := PickupSource(nil, &none, 560, ""); ok {
		t.Error("a rune with no kill to go under was recorded")
	}
	// A bone with no kill is still a pickup, as it always was (the BoneBurier's stray bones).
	if src, events, ok := PickupSource(nil, &none, 526, "Bones"); !ok || src.Kind != KindPickup || events != 1 {
		t.Errorf("bones with no kill filed as %+v events %d ok %v", src, events, ok)
	}
	if _, _, ok := PickupSource(nil, nil, 560, ""); ok {
		t.Error("a nil kill log recorded a rune")
	}
}

func TestAPileIsNeverCreditedToAKillOfAnNpcThatDropsNothing(t *testing.T) {
	now := time.Now()
	trooper := Source{Kind: KindNPC, Action: ActionKill, Name: "Khazard trooper", ID: 475}
	dwarf := Source{Kind: KindNPC, Action: ActionKill, Name: "Dwarf", ID: 118}
	if !DropsNothing(trooper) || DropsNothing(dwarf) || DropsNothing(Pickup(nil, 526, "Bones")) {
		t.Fatal("DropsNothing is wrong")
	}
	var k KillLog
	k.Note(dwarf, 3000, 9800, 0, now.Add(-time.Minute))
	k.Note(trooper, 2522, 3243, 0, now)
	// Beside the trooper's tile the dwarf's kill, not the newer trooper, is the only claimant left.
	if src, ok := k.SourceFor(2522, 3243, 0, now); !ok || src.Name != "Dwarf" {
		t.Errorf("SourceFor = %+v %v", src, ok)
	}
	if src, ok := k.Latest(); !ok || src.Name != "Dwarf" {
		t.Errorf("Latest = %+v %v", src, ok)
	}
	var only KillLog
	only.Note(trooper, 2522, 3243, 0, now)
	if _, ok := only.SourceFor(2522, 3243, 0, now); ok {
		t.Error("a trooper alone claimed a pile")
	}
	if _, ok := only.Latest(); ok {
		t.Error("a trooper alone is the latest kill")
	}
	// Runes then have no kill, bones get their own card.
	if _, _, ok := PickupSource(nil, &only, 562, "Chaos rune"); ok {
		t.Error("a rune was recorded under a trooper")
	}
	if src, _, ok := PickupSource(nil, &only, 526, "Bones"); !ok || src.Kind != KindPickup {
		t.Errorf("bones went to %+v %v", src, ok)
	}
}
