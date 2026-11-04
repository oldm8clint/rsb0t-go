package waterfallrunner

// The tomb strip with the run staller's hooks set (queststrip-1015). ScarabSwarm's quest Deps carry KeepInPack (the jewellery,
// the walk-alch runes and the trip's laws stay in the pack through every deposit) and AtBank (the ring of dueling is drawn
// back out of the bank after every deposit); the strip used to go through both, and waterfallplan.TombSafe failed for ever.

import (
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	wf "rsb0t-go/internal/scripts/waterfallplan"
)

const (
	stallFire    = 554
	stallAir     = 556
	stallLaw     = 563
	stallDueling = 2552
	stallGames   = 3853
	ghostspeak   = 552
	amuletSlot   = 2
)

// stripRig is an account standing at an open bank wearing a Ghostspeak amulet, with the run staller's whole kit, a bread and
// coins in the pack. Its Deps keep every item in the pack by KeepInPack and draw the ring of dueling back out of the bank in
// AtBank, as scarabrunner.Travel does; atBank counts the AtBank calls.
func stripRig(t *testing.T) (d *questrunner.Deps, bank *actions.Bank, sim *actionsim.Session, atBank *int) {
	t.Helper()
	sim = actionsim.New(actionsim.Objs{}, []actionsim.Stack{
		{ID: stallFire, Count: 1}, {ID: stallAir, Count: 1}, {ID: stallLaw, Count: 1},
		{ID: stallDueling, Count: 1}, {ID: stallGames, Count: 1}, {ID: wf.FoodBread, Count: 1}, {ID: wf.ItemCoins, Count: 1},
	})
	sim.ShowTabs()
	sim.Wear(amuletSlot, ghostspeak)
	sim.OpenBank(nil)
	atBank = new(int)
	comps := actionsim.TabComponents()
	d = &questrunner.Deps{
		Session: sim, Components: comps, Log: func(string) {},
		Delay:      func(time.Duration) bool { time.Sleep(time.Millisecond); return true },
		KeepInPack: func(string) bool { return true },
		AtBank: func(b *actions.Bank, w actions.Waiter) {
			*atBank++
			_ = b.QuickWithdrawX(stallDueling, 1, w)
		},
	}
	return d, actions.NewBank(sim, comps), sim, atBank
}

// The strip ends tomb-safe whatever the hooks keep and draw: the kit, the amulet taken off and everything else is in the bank,
// the bread and the coins stay, and AtBank never ran.
func TestStripLeavesATombSafePackWhenTheRunStallerKeepsItsKit(t *testing.T) {
	d, bank, sim, atBank := stripRig(t)
	e := &questrunner.Engine{D: d, Store: questplan.NewStore()}

	rememberWorn(e, []int{ghostspeak})
	questrunner.StripWorn(d)
	if got := questrunner.WornCounts(d); len(got) != 0 {
		t.Fatalf("the strip left %v worn", got)
	}
	if !stripKit(d, bank) {
		t.Fatalf("stripKit reported a pack that is not tomb-safe: %v", questrunner.PackCounts(d))
	}

	pack := questrunner.PackCounts(d)
	if ok, why := wf.TombSafe(pack, questrunner.WornCounts(d), wf.IsFood); !ok {
		t.Errorf("the pack %v is not tomb-safe after the strip: %s", pack, why)
	}
	if len(pack) != 2 || pack[wf.FoodBread] != 1 || pack[wf.ItemCoins] != 1 {
		t.Errorf("the pack holds %v, want the bread and the coins", pack)
	}
	if *atBank != 0 {
		t.Errorf("AtBank ran %d time(s) behind the strip, want none: it draws the ring of dueling straight back", *atBank)
	}
	banked := map[int]int{}
	for _, r := range sim.Bank() {
		banked[int(r.ID)] += r.Count
	}
	for _, id := range []int{stallFire, stallAir, stallLaw, stallDueling, stallGames, ghostspeak} {
		if banked[id] == 0 {
			t.Errorf("object %d is not in the bank after the strip: %v", id, banked)
		}
	}
}

// The Ghostspeak amulet the strip took off is in the quest's store, so Finish wears it again (and Nature Spirit would draw and
// wear it itself at stage 70 should the quest never finish).
func TestStripRemembersTheWornGhostspeakAmulet(t *testing.T) {
	d, _, _, _ := stripRig(t)
	e := &questrunner.Engine{D: d, Store: questplan.NewStore()}

	rememberWorn(e, []int{ghostspeak, 1171})
	if got := wornIDs(e.Store.Record(wf.QuestID).Hints); len(got) != 2 || got[0] != ghostspeak || got[1] != 1171 {
		t.Errorf("the store remembers %v worn, want [%d 1171]", got, ghostspeak)
	}

	// Nothing worn records nothing, and a nil store is no panic.
	rememberWorn(e, nil)
	rememberWorn(&questrunner.Engine{D: d}, []int{ghostspeak})
}

// The case the survey named: nothing but a rune stack and a Ring of dueling in the pack, KeepInPack claiming both. The strip
// ends with an empty pack that TombSafe passes, and the next bank visit of the tomb trip (a bare provision) is the one that
// draws the trip's food.
func TestStripOfARuneAndARingEndsEmptyAndTombSafe(t *testing.T) {
	sim := actionsim.New(actionsim.Objs{}, []actionsim.Stack{{ID: stallFire, Count: 1}, {ID: stallDueling, Count: 1}})
	sim.OpenBank(nil)
	var atBank int
	d := &questrunner.Deps{
		Session: sim, Components: actionsim.Components(), Log: func(string) {},
		Delay:      func(time.Duration) bool { time.Sleep(time.Millisecond); return true },
		KeepInPack: func(name string) bool { return name == "obj 554" || name == "obj 2552" },
		AtBank:     func(*actions.Bank, actions.Waiter) { atBank++ },
	}
	if !stripKit(d, actions.NewBank(sim, d.Components)) {
		t.Fatalf("stripKit failed with the pack at %v", questrunner.PackCounts(d))
	}
	pack := questrunner.PackCounts(d)
	if len(pack) != 0 {
		t.Errorf("the pack holds %v, want it empty", pack)
	}
	if ok, why := wf.TombSafe(pack, questrunner.WornCounts(d), wf.IsFood); !ok {
		t.Errorf("TombSafe refuses the stripped pack: %s", why)
	}
	if atBank != 0 {
		t.Errorf("AtBank ran %d time(s), want none", atBank)
	}
}
