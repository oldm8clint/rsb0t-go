package waterfallplan

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/questplan"
)

// tombSnap is an account about to use the pebble on Glarial's tombstone: kit satisfied (food
// worth the floor, nothing worn), the pebble held, on the mainland.
func tombSnap(hp, maxHP int, food map[int]int) Snapshot {
	inv := map[int]int{ItemPebble: 1}
	for id, n := range food {
		inv[id] = n
	}
	return Snapshot{
		Status: questplan.StatusInProgress, Stage: StageReadBook, StageKnown: true,
		Tile: ArdougneBank, TileKnown: true,
		Inv: inv, Worn: map[int]int{}, IsFood: IsFood, FreeSlots: 28 - len(inv),
		HP: hp, MaxHP: maxHP, Settings: DefaultSettings(),
		Failed: map[string]bool{}, Ran: map[string]bool{},
	}
}

// Live 5 Oct 2026: acct used the pebble at 21/31, took about 40 hitpoints in the tomb and died with
// 20 hitpoints of bread eaten; at 31 it would have had 51 against that 41. The decision now eats
// first.
func TestTheAccountEatsBeforeItUsesThePebble(t *testing.T) {
	s := tombSnap(21, 31, map[int]int{FoodBread: 14}) // the trip's full 14 units
	d := Decide(s)
	if d.Leg != LegHeal || d.Item != FoodBread {
		t.Fatalf("got %s (item %d), want eating bread", d, d.Item)
	}
	if !strings.Contains(d.Why, "21 of 31") {
		t.Errorf("the reason does not say why: %q", d.Why)
	}
	step := StepFor(d, s)
	if step.Kind != questplan.StepOpHeld || step.ItemID != FoodBread || step.Op != "Eat" || step.Item != "Bread" {
		t.Fatalf("%+v", step)
	}
	if why := questplan.ValidateStep(step); why != "" {
		t.Fatal(why)
	}
	// The same account eats until it is healthy and then goes: no more than three bites, and the
	// last decision is the tombstone.
	bites := 0
	for ; bites < 10; bites++ {
		d = Decide(s)
		if d.Leg != LegHeal {
			break
		}
		s.Inv[d.Item]--
		s.HP = min(s.HP+biteHeal(d.Item), s.MaxHP)
	}
	if d.Leg != LegTombstone || bites == 0 || bites > 3 || s.HP*100 < s.MaxHP*TombEntryHealthPct {
		t.Fatalf("after %d bites: %s at %d/%d", bites, d, s.HP, s.MaxHP)
	}
}

func TestAHealthyAccountGoesStraightIn(t *testing.T) {
	// 28 of 31 is 90.3%.
	if d := Decide(tombSnap(28, 31, map[int]int{FoodBread: 12})); d.Leg != LegTombstone {
		t.Fatalf("%s", d)
	}
	if d := Decide(tombSnap(31, 31, map[int]int{FoodBread: 12})); d.Leg != LegTombstone {
		t.Fatalf("%s", d)
	}
}

// No bite is eaten that would waste most of itself: a shark at 25/31 heals 20 for 6 missing.
func TestNoBiteThatWouldMostlyOverheal(t *testing.T) {
	s := tombSnap(25, 31, map[int]int{385: 4})
	if id := s.tombEntryBite(); id != 0 {
		t.Fatalf("ate obj %d for 6 missing hitpoints", id)
	}
	if d := Decide(s); d.Leg != LegTombstone {
		t.Fatalf("%s", d)
	}
}

// The biggest bite that fits what is missing: at 12/31 (19 missing) a lobster, not a bread and not a
// shark.
func TestTheBiggestBiteThatFits(t *testing.T) {
	s := tombSnap(12, 31, map[int]int{385: 3, 379: 3, FoodBread: 6})
	if id := s.tombEntryBite(); id != 379 {
		t.Fatalf("ate obj %d at 12/31, want a lobster (379)", id)
	}
	// A cake is bites of 4.
	cake := tombSnap(24, 31, map[int]int{1891: 4, 385: 2})
	if id := cake.tombEntryBite(); id != 1891 {
		t.Fatalf("ate obj %d at 24/31 with cakes and sharks, want a cake", id)
	}
	if biteHeal(1891) != 4 || biteHeal(1893) != 4 || biteHeal(1895) != 4 || biteHeal(385) != 20 || biteHeal(99999) != FoodUnit {
		t.Fatalf("bite figures: cake %d 2/3 %d slice %d shark %d unknown %d", biteHeal(1891), biteHeal(1893), biteHeal(1895), biteHeal(385), biteHeal(99999))
	}
}

// With no food and low health nothing is eaten (and the kit's floor, not this, keeps such an account
// out): the decision is not stuck on a heal it cannot do.
func TestNothingToEatIsNotAHealLeg(t *testing.T) {
	s := tombSnap(10, 31, map[int]int{})
	if s.tombEntryBite() != 0 {
		t.Fatal("a bite from an empty pack")
	}
	if step := StepFor(Decision{Leg: LegHeal}, s); step.Kind != questplan.StepWait {
		t.Fatalf("a heal leg with no item must wait, got %+v", step)
	}
	// Unread hitpoints are not judged.
	if (Snapshot{Inv: map[int]int{FoodBread: 5}, IsFood: IsFood}).tombEntryBite() != 0 {
		t.Fatal("an unread hitpoints bar asked for a bite")
	}
}

// Eating below the trip's food floor sends the account back to the kit, which restocks it; it does
// not loop, because the healed account needs no second bite.
func TestHealingBelowTheFloorRestocksThenGoesIn(t *testing.T) {
	s := tombSnap(21, 31, map[int]int{FoodBread: 11}) // exactly the floor
	d := Decide(s)
	if d.Leg != LegHeal {
		t.Fatalf("%s", d)
	}
	s.Inv[d.Item]--
	s.HP += biteHeal(d.Item)
	s.Inv[FoodBread]--
	s.HP += biteHeal(FoodBread)
	if d = Decide(s); d.Leg != LegKitTomb {
		t.Fatalf("below the floor after the bites: %s", d)
	}
	// The kit restocked to the full 14 units: healthy and fed, the tombstone.
	s.Inv[FoodBread] = 14
	if d = Decide(s); d.Leg != LegTombstone {
		t.Fatalf("restocked: %s", d)
	}
}

// The gate applies to the relic-recovery pass at a later stage as well: it shares tombTrip.
func TestRelicRecoveryEatsBeforeTheTombToo(t *testing.T) {
	s := tombSnap(15, 31, map[int]int{FoodBread: 12})
	s.Stage = StageEnteredTomb + 1 // a later stage with the relics lost
	if d := Decide(s); d.Leg != LegHeal {
		t.Fatalf("%s", d)
	}
}

// The trip's food: 14 units, a floor of 11, and an account holding less is sent back to the kit.
func TestTheTombTripCarriesMoreFood(t *testing.T) {
	set := DefaultSettings()
	if set.TombFood != 14 || set.TombFoodMin != 11 {
		t.Fatalf("tomb food %d floor %d", set.TombFood, set.TombFoodMin)
	}
	l := TombLoadout(StageReadBook, set)
	if l.Food != 14 || l.FoodMin != 11 {
		t.Fatalf("loadout food %d floor %d", l.Food, l.FoodMin)
	}
	// Four breads were enough before; they are not now.
	if ok, why := l.Ready(map[int]int{FoodBread: 4}, map[int]int{}, IsFood); ok || !strings.Contains(why, "food units") {
		t.Fatalf("%v %q", ok, why)
	}
	if ok, why := l.Ready(map[int]int{FoodBread: 11}, map[int]int{}, IsFood); !ok {
		t.Fatalf("eleven breads: %q", why)
	}
	// The earlier stages still draw food for the Tree Gnome Village but never wait for it.
	if early := TombLoadout(StageStarted, set); early.FoodMin != 0 {
		t.Fatalf("the early stages' floor is %d", early.FoodMin)
	}
	if got := set.Normalised(); got.TombFood != 14 || got.TombFoodMin != 11 {
		t.Fatalf("normalising changed the food: %d / %d", got.TombFood, got.TombFoodMin)
	}
}
