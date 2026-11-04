package waterfallplan

// The tomb trip is a bare-pack stretch (queststrip-1015). Glarial's tomb takes no rune and no jewellery, and the run staller
// carries both: its runes (554 fire, 556 air, 563 law), a Ring of dueling (2552) or a Games necklace (3853). The strip and
// every bank visit of the tomb trip must leave that kit in the bank, or the visit after the strip draws the ring back and
// TombSafe fails again: 238 wf-strip parks on 14-15 Oct 2026, no Waterfall completion.

import (
	"testing"

	"rsb0t-go/internal/scripts/questplan"
)

const (
	stallFire    = 554
	stallAir     = 556
	stallLaw     = 563
	stallDueling = 2552
	stallGames   = 3853
	ghostspeak   = 552
)

func TestTheRunStallersKitIsNotTombSafe(t *testing.T) {
	for _, id := range []int{stallFire, stallAir, stallLaw, stallDueling, stallGames} {
		if ok, _ := TombSafe(map[int]int{id: 1, ItemCoins: 40}, nil, IsFood); ok {
			t.Errorf("obj %d (the run staller's kit) is tomb-safe in the pack", id)
		}
	}
	if ok, why := TombSafe(map[int]int{ItemCoins: 40, FoodBread: 5}, nil, IsFood); !ok {
		t.Errorf("coins and bread are not tomb-safe: %s", why)
	}
	// Anything worn fails it, a Ghostspeak amulet included: the strip takes it off and the quest's Finish wears it again.
	if ok, _ := TombSafe(map[int]int{ItemCoins: 40}, map[int]int{ghostspeak: 1}, IsFood); ok {
		t.Error("a worn Ghostspeak amulet is tomb-safe")
	}
}

// A pack carrying the kit is stripped; a pack with the kit banked goes on to the tomb kit, and that kit's bank visit is bare.
func TestKitInThePackSendsTheAccountToStripAndABarePackToABareProvision(t *testing.T) {
	s := engineSnap(StageReadBook, ArdougneBank)
	s.InvIDs = map[int]int{stallFire: 40, stallLaw: 20, stallDueling: 1, stallGames: 1, ItemCoins: 90}
	step := DecideStep(s, DefaultSettings())
	if step.Kind != questplan.StepCustom || step.Custom != CustomStrip {
		t.Fatalf("with the staller's kit in the pack: %s, want the strip", step.Describe())
	}

	// Stripped: nothing but coins. The next bank visit is the tomb kit (the book is read, so the trip needs its food), and it is bare.
	s.InvIDs = map[int]int{ItemCoins: 90}
	step = DecideStep(s, DefaultSettings())
	if step.Kind != questplan.StepProvision || step.KitName != KitTomb {
		t.Fatalf("with a bare pack: %s, want the tomb kit", step.Describe())
	}
	if !step.Bare {
		t.Error("the tomb kit's bank visit is not bare: its deposit would keep the staller's kit and AtBank would draw the ring back")
	}
}

func TestEveryBankVisitOfTheTombTripIsBare(t *testing.T) {
	set := DefaultSettings()

	// The food top-up after the kit ran short of food, cakes and bread.
	s := engineSnap(StageReadBook, ArdougneBank)
	s.Ran["provision:"+KitTomb] = true
	s.BankKnown, s.BankIDs = true, map[int]int{}
	step := DecideStep(s, set)
	if step.Kind != questplan.StepProvision || step.KitName != KitTombFood+ShopSuffix {
		t.Fatalf("the top-up: %s", step.Describe())
	}
	if !step.Bare {
		t.Error("the tomb trip's food top-up (bread) is not bare")
	}
	s.HP, s.MaxHP = CakeStallMinHP, CakeStallMinHP
	step = DecideStep(s, set)
	if step.Kind != questplan.StepProvision || step.KitName != KitTombFood {
		t.Fatalf("the cake top-up: %s", step.Describe())
	}
	if !step.Bare {
		t.Error("the tomb trip's food top-up (cakes) is not bare")
	}

	// The surplus-food deposit of the tomb trip.
	ws := Snapshot{Inv: map[int]int{FoodCake: 40}, Worn: map[int]int{}, IsFood: IsFood, FreeSlots: 0, Settings: set}
	dep, ok := surplusFoodDeposit(TombLoadout(StageReadBook, set), ws)
	if !ok || dep.Kind != questplan.StepDeposit {
		t.Fatalf("the surplus-food deposit: %v %s", ok, dep.Describe())
	}
	if !dep.Bare {
		t.Error("the tomb trip's surplus-food deposit is not bare")
	}
}

// The dungeon trip carries runes of its own and may carry the staller's kit: its visits are ordinary, so the kit comes back at
// the first one after the tomb.
func TestTheDungeonTripsBankVisitsAreOrdinary(t *testing.T) {
	set := DefaultSettings()
	s := engineSnap(StageEnteredTomb, ArdougneBank)
	s.InvIDs = map[int]int{ItemAmulet: 1, ItemFullUrn: 1, ItemCoins: 300}
	s.BankKnown, s.BankIDs = true, map[int]int{}
	step := DecideStep(s, set)
	if step.Kind != questplan.StepProvision || step.KitName != KitDungeon {
		t.Fatalf("the dungeon kit: %s", step.Describe())
	}
	if step.Bare {
		t.Error("the dungeon kit's bank visit is bare: the staller's kit would never come back")
	}

	ws := Snapshot{Inv: map[int]int{FoodCake: 40}, Worn: map[int]int{}, IsFood: IsFood, FreeSlots: 0, Settings: set}
	dep, ok := surplusFoodDeposit(DungeonLoadout(StageEnteredTomb, set), ws)
	if !ok {
		t.Fatal("no surplus-food deposit for 40 cakes in a full pack")
	}
	if dep.Bare {
		t.Error("the dungeon trip's surplus-food deposit is bare")
	}
}
