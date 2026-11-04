package loot

import (
	"testing"
	"time"
)

// TestBuriedBonesLandOnOnePrayerCardAndCostTheBone: every bone kind on one Prayer card, apart
// from Fletching, with one bone priced as the cost of each burial.
func TestBuriedBonesLandOnOnePrayerCardAndCostTheBone(t *testing.T) {
	bones := Bury(nil, 526, "")
	big := Bury(nil, 532, "")
	if bones.GroupKey() != "production|prayer" || big.GroupKey() != bones.GroupKey() {
		t.Fatalf("bury sources group on %q and %q, want production|prayer", bones.GroupKey(), big.GroupKey())
	}
	if bones.GroupKey() == Fletching(nil, 1511, "Logs").GroupKey() {
		t.Fatal("Prayer and Fletching share a card")
	}
	// Named from content's own route table when there is no cache.
	if bones.Name != "Bones" || big.Name != "Big bones" {
		t.Fatalf("names = %q / %q", bones.Name, big.Name)
	}
	if bones.Key() != "bury:bones:526" {
		t.Fatalf("key = %q", bones.Key())
	}
	c, ok := big.Consumes()
	if !ok || c.ID != 532 || c.Per != 1 {
		t.Fatalf("a burial consumes %+v (%v), want one Big bones", c, ok)
	}
}

// TestBuryableIsContentsOwnList: derived from bankxp's generated table, so it knows the bones a
// script might bury and nothing else.
func TestBuryableIsContentsOwnList(t *testing.T) {
	for _, id := range []int{526 /* Bones */, 532 /* Big bones */, 530 /* Bat bones */, 528 /* Burnt bones */} {
		if !IsBuryable(id) {
			t.Fatalf("obj %d should be buryable", id)
		}
	}
	for _, id := range []int{995, 1511, 1739 /* Cowhide */} {
		if IsBuryable(id) {
			t.Fatalf("obj %d is not a bone", id)
		}
	}
}

// TestTallyCarriesXP: a burial yields experience, not items, and that has to survive the tally.
// On the old code a Drop had no XP field at all.
func TestTallyCarriesXP(t *testing.T) {
	tally := NewTally(func() time.Time { return time.Unix(100, 0) })
	src := Bury(nil, 526, "")
	tally.Add(Drop{Source: src, Events: 1, XP: 4})
	tally.Add(Drop{Source: src, Events: 1, XP: 5})
	got := tally.Snapshot()
	if len(got) != 1 || got[0].Events != 2 || got[0].XP != 9 || len(got[0].Items) != 0 {
		t.Fatalf("snapshot = %+v, want 2 bones buried for 9 xp and no items", got)
	}
}

// TestKillLogAttributesADropToTheNearestRecentKill is the rule internal/bot's combat scripts
// used for their own bones, now shared so a runner package can use it too.
func TestKillLogAttributesADropToTheNearestRecentKill(t *testing.T) {
	now := time.Now()
	cow := NPC(nil, ActionKill, 81, "Cow")
	chicken := NPC(nil, ActionKill, 41, "Chicken")
	var k KillLog
	k.Note(cow, 3250, 3270, 0, now.Add(-40*time.Second))
	k.Note(chicken, 3235, 3295, 0, now.Add(-5*time.Second))
	if src, ok := k.SourceFor(3251, 3271, 0, now); !ok || src != cow {
		t.Fatalf("a drop beside the cow kill went to %+v", src)
	}
	if src, ok := k.SourceFor(3300, 3300, 0, now); !ok || src != chicken {
		t.Fatalf("an unmatched drop went to %+v, want the newest kill", src)
	}
	if _, ok := k.SourceFor(3251, 3271, 0, now.Add(4*time.Minute)); ok {
		t.Fatal("a kill older than the drop window claimed a drop")
	}
	var none *KillLog
	none.Note(cow, 0, 0, 0, now)
	if _, ok := none.SourceFor(0, 0, 0, now); ok {
		t.Fatal("a nil log attributed a drop")
	}
	for i := 0; i < 40; i++ {
		k.Note(chicken, 1, 1, 0, now)
	}
	if len(k.recent) != killLogCap {
		t.Fatalf("the log grew to %d, want it bounded at %d", len(k.recent), killLogCap)
	}
}

// TestPickupIsItsOwnCardPerItem: a map spawn is its own source, not some npc's.
func TestPickupIsItsOwnCardPerItem(t *testing.T) {
	cactus := Pickup(nil, 3138, "Potato cactus")
	knife := Pickup(nil, 946, "Knife")
	if cactus.Kind != KindPickup || cactus.Action != ActionTake || cactus.Model != ModelObj || cactus.Key() != "take:potato cactus:3138" {
		t.Fatalf("cactus = %+v", cactus)
	}
	if cactus.GroupKey() == knife.GroupKey() {
		t.Fatal("two different items share a pickup card")
	}
}
