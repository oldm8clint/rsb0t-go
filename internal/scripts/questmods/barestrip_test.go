package questmods

// The quests' strips with the run staller's hooks set (queststrip-1015). Merlin's Wilderness altar trip, Entrana's monks and
// Thorvald's ladder each refuse the run staller's kit; ScarabSwarm's quest Deps keep that kit in the pack through every
// ordinary deposit (KeepInPack) and draw the ring of dueling back out of the bank behind it (AtBank), so the strips never
// ended bare: ten of ten Merlin runs parked in merlin-strip on 14-15 Oct 2026, and Grail's Entrana and Fremennik's pre-trial
// strips took the same path.

import (
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

const (
	kitFire     = 554
	kitAir      = 556
	kitLaw      = 563
	kitDueling  = 2552
	kitGames    = 3853
	kitShark    = 385
	kitCoins    = 995
	kitGhost    = 552
	kitAmuletAt = 2
)

// stripRig is an account standing at the bank tile at an open bank, wearing a Ghostspeak amulet, with the run staller's kit, a
// shark and coins in the pack. Its Deps keep every item by KeepInPack and draw the ring of dueling back in AtBank, as
// scarabrunner.Travel does; atBank counts the AtBank calls.
func stripRig(t *testing.T, at tile.Tile) (d *questrunner.Deps, atBank *int) {
	t.Helper()
	sim := actionsim.New(actionsim.Objs{}, []actionsim.Stack{
		{ID: kitFire, Count: 1}, {ID: kitAir, Count: 1}, {ID: kitLaw, Count: 1},
		{ID: kitDueling, Count: 1}, {ID: kitGames, Count: 1}, {ID: kitShark, Count: 1}, {ID: kitCoins, Count: 1},
	})
	sim.ShowTabs()
	sim.Wear(kitAmuletAt, kitGhost)
	sim.StandAt(at.X, at.Z)
	sim.OpenBank(nil)
	atBank = new(int)
	comps := actionsim.TabComponents()
	d = &questrunner.Deps{
		Session: sim, Components: comps, Log: func(string) {},
		Delay:      func(time.Duration) bool { time.Sleep(time.Millisecond); return true },
		KeepInPack: func(string) bool { return true },
		AtBank: func(b *actions.Bank, w actions.Waiter) {
			*atBank++
			_ = b.QuickWithdrawX(kitDueling, 1, w)
		},
	}
	return d, atBank
}

// wantFoodAndCoinsOnly fails the test unless the pack holds exactly the shark and the coins and nothing is worn.
func wantFoodAndCoinsOnly(t *testing.T, d *questrunner.Deps, atBank *int) {
	t.Helper()
	pack := questrunner.PackCounts(d)
	if len(pack) != 2 || pack[kitShark] != 1 || pack[kitCoins] != 1 {
		t.Errorf("the pack holds %v after the strip, want the shark and the coins alone", pack)
	}
	if worn := questrunner.WornCounts(d); len(worn) != 0 {
		t.Errorf("%v is still worn after the strip", worn)
	}
	if *atBank != 0 {
		t.Errorf("AtBank ran %d time(s) behind the strip, want none: it draws the ring of dueling straight back", *atBank)
	}
}

func TestMerlinStripBanksTheRunStallersKit(t *testing.T) {
	d, atBank := stripRig(t, questplan.CatherbyBank)
	strip := MerlinsCrystal().Customs[questplan.CustomMerlinStrip]
	if strip == nil {
		t.Fatal("Merlin's Crystal registers no merlin-strip executor")
	}
	if !strip(&questrunner.Engine{D: d}, questplan.Step{}) {
		t.Fatalf("merlin-strip failed with the pack at %v and %v worn", questrunner.PackCounts(d), questrunner.WornCounts(d))
	}
	wantFoodAndCoinsOnly(t, d, atBank)
	// What the plan asks of the strip: a pack of coins and food and nothing worn goes to the altar.
	s := questplan.Snapshot{InvIDs: questrunner.PackCounts(d), WornIDs: questrunner.WornCounts(d)}
	if !questplan.WildernessSafe(s) {
		t.Errorf("questplan.WildernessSafe refuses the stripped pack %v", s.InvIDs)
	}
}

func TestGrailStripBanksTheRunStallersKit(t *testing.T) {
	d, atBank := stripRig(t, questplan.FaladorEastBank)
	foods := questplan.HolyGrail.Sustain
	keep := func(id int) bool { return id == questplan.CoinsID || foods.IsFood(id) }
	if !StripAtBank(d, questplan.FaladorEastBank, keep) {
		t.Fatalf("the Entrana strip failed with the pack at %v and %v worn", questrunner.PackCounts(d), questrunner.WornCounts(d))
	}
	wantFoodAndCoinsOnly(t, d, atBank)
}

func TestThorvaldStripBanksTheRunStallersKit(t *testing.T) {
	d, atBank := stripRig(t, questplan.SeersBank)
	if !thorvaldStripWith(d, questplan.Sustain{FoodIDs: []int{kitShark}}) {
		t.Fatalf("Thorvald's strip failed with the pack at %v and %v worn", questrunner.PackCounts(d), questrunner.WornCounts(d))
	}
	// Thorvald's ladder takes food and nothing else: the coins go too.
	pack := questrunner.PackCounts(d)
	if len(pack) != 1 || pack[kitShark] != 1 {
		t.Errorf("the pack holds %v after the strip, want the shark alone", pack)
	}
	if worn := questrunner.WornCounts(d); len(worn) != 0 {
		t.Errorf("%v is still worn after the strip", worn)
	}
	if *atBank != 0 {
		t.Errorf("AtBank ran %d time(s) behind the strip, want none", *atBank)
	}
}
