package bot

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/mastercraftrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runerunner"
	"rsb0t-go/internal/scripts/tile"
)

// RuneCarrier, runecraft P4: the share of a courier haul it keeps in its own bank, and the standby it tells the crafter.

// keepRig is a carrier at the post with a courier haul of `haul` Natures in its pack and `bank` in its own bank; the trade is a fake that
// takes the Natures and notes that are in the pack.
func keepRig(t *testing.T, haul, bank int, keep int) (*carrierRig, *int) {
	t.Helper()
	pack := []actionsim.Stack{{ID: coinsObj, Count: 4000}}
	if haul > 0 {
		pack = append(pack, actionsim.Stack{ID: natRune, Count: haul})
	}
	c := newCarrierRig(t, pack, nil)
	if bank > 0 {
		c.sim.OpenBank([]actionsim.Stack{{ID: natRune, Count: bank}})
	}
	c.b.set.KeepNatures = keep
	crossed := new(int)
	old := masterDeliver
	t.Cleanup(func() { masterDeliver = old })
	masterDeliver = func(*fleetrunner.Deps, *donorrunner.Deps, string, tile.Tile, int, int, []string) runerunner.Delivery {
		n := simCount(c.sim.Pack(), natRune)
		c.sim.Remove(natRune, n)
		*crossed += n
		return runerunner.Delivery{Result: donorrunner.Result{Traded: true, Slots: 1}, Delivered: map[string]int{"Nature rune": n}}
	}
	return c, crossed
}

// a user request: "keep some instead of trading all then asking back". The share stays in the carrier's own bank; the rest of the
// haul crosses; the carrier's own stock counts toward the share.
func TestCarrierKeepsItsShareOfTheHaul(t *testing.T) {
	for _, c := range []struct {
		name                   string
		haul, bank, keep       int
		wantCrossed, wantBank  int
		wantKeptLog, wantTrade bool
	}{
		{"the default share of 200 from a haul of 2,300", 2300, 0, 200, 2100, 200, true, true},
		{"the carrier already holds its share in the bank: all of the haul crosses", 2300, 500, 200, 2300, 500, true, true},
		{"a haul under the share: all of it is kept and there is nothing to trade", 150, 0, 200, 0, 150, true, false},
		{"a haul that tops the bank up to the share", 300, 120, 200, 220, 200, true, true},
		{"carrierKeepNatures 0 hands everything over and never opens the bank", 2300, 0, 0, 2300, 0, false, true},
	} {
		rig, crossed := keepRig(t, c.haul, c.bank, c.keep)
		opened := 0
		oldOpen := mastercraftrunner.OpenBank
		mastercraftrunner.OpenBank = func(d *fleetrunner.Deps, at tile.Tile) (*actions.Bank, bool) {
			opened++
			return oldOpen(d, at)
		}
		w := rig.world()
		// The courier's haul is what makes a carrier hold Natures at all: the shift's accounting is the world's.
		w.natures = c.haul
		res := w.deliver()
		mastercraftrunner.OpenBank = oldOpen
		if !res.OK {
			t.Errorf("%s: %+v\n%s", c.name, res, strings.Join(rig.logs, "\n"))
			continue
		}
		if *crossed != c.wantCrossed {
			t.Errorf("%s: %d Natures crossed, want %d", c.name, *crossed, c.wantCrossed)
		}
		if got := simCount(rig.sim.Bank(), natRune); got != c.wantBank {
			t.Errorf("%s: the bank holds %d Natures, want %d", c.name, got, c.wantBank)
		}
		if got := simCount(rig.sim.Pack(), natRune); got != 0 {
			t.Errorf("%s: %d Natures are left in the pack", c.name, got)
		}
		if c.keep == 0 && opened != 0 {
			t.Errorf("%s: the bank was opened with nothing to keep", c.name)
		}
		if (c.wantTrade) != (*crossed > 0) {
			t.Errorf("%s: trade %v, crossed %d", c.name, c.wantTrade, *crossed)
		}
		if c.wantKeptLog != rig.logged("keep: ") {
			t.Errorf("%s: the keep line: %v\n%s", c.name, rig.logged("keep: "), strings.Join(rig.logs, "\n"))
		}
		if simCount(rig.sim.Pack(), coinsObj) != 4000 {
			t.Errorf("%s: the coins stay with the account", c.name)
		}
	}
}

// A shift that ends holding a load nobody took (the gate closed on it) ends at the mule's post with 25 single items in the pack, which the
// mule's trade screen cannot take as it is (live 16 Oct 2026: two shifts ended "deliver failed 3 times in a row" with 25 essence in the
// pack). They go through the bank and come out as one stack of notes, which is what the mule is handed.
func TestCarrierTurnsALooseLoadIntoNotesBeforeHandingItOver(t *testing.T) {
	stackable(t, runecraftplan.NotedEssenceID)
	c := newCarrierRig(t, []actionsim.Stack{{ID: coinsObj, Count: 4000}}, nil)
	c.sim.Give(runecraftplan.EssenceID, 25)
	c.sim.OpenBank(nil)
	var seen map[string]int
	old := masterDeliver
	t.Cleanup(func() { masterDeliver = old })
	masterDeliver = func(_ *fleetrunner.Deps, _ *donorrunner.Deps, _ string, _ tile.Tile, _, slots int, _ []string) runerunner.Delivery {
		seen = map[string]int{"notes": simCount(c.sim.Pack(), runecraftplan.NotedEssenceID), "loose": simCount(c.sim.Pack(), runecraftplan.EssenceID), "slots": slots}
		n := seen["notes"]
		c.sim.Remove(runecraftplan.NotedEssenceID, n)
		return runerunner.Delivery{Result: donorrunner.Result{Traded: true, Slots: 1}, Delivered: map[string]int{"Rune essence": n}}
	}
	w := c.world()
	res := w.deliver()
	if !res.OK {
		t.Fatalf("%+v\n%s", res, strings.Join(c.logs, "\n"))
	}
	if seen["notes"] != 25 || seen["loose"] != 0 || seen["slots"] != 1 {
		t.Fatalf("the trade saw %v: want 25 notes, no loose essence, one slot", seen)
	}
	if w.delivered["Rune essence"] != 25 || simCount(c.sim.Bank(), runecraftplan.EssenceID) != 0 {
		t.Fatalf("delivered %v, bank %v", w.delivered, c.sim.Bank())
	}
	if !c.logged("the 25 loose essence the shift ended with went through the bank as 25 note(s)") {
		t.Fatalf("log:\n%s", strings.Join(c.logs, "\n"))
	}
	if simCount(c.sim.Pack(), coinsObj) != 4000 {
		t.Fatal("the coins stay with the account")
	}
	// A bank that will not open: the loose essence is offered as it is, and the log says why.
	c2 := newCarrierRig(t, []actionsim.Stack{{ID: coinsObj, Count: 4000}}, nil)
	c2.sim.Give(runecraftplan.EssenceID, 25)
	oldOpen := mastercraftrunner.OpenBank
	t.Cleanup(func() { mastercraftrunner.OpenBank = oldOpen })
	mastercraftrunner.OpenBank = func(*fleetrunner.Deps, tile.Tile) (*actions.Bank, bool) { return nil, false }
	masterDeliver = func(*fleetrunner.Deps, *donorrunner.Deps, string, tile.Tile, int, int, []string) runerunner.Delivery {
		return runerunner.Delivery{Result: donorrunner.Result{Traded: false, Reason: "none of it can go on a trade screen"}}
	}
	if res := c2.world().deliver(); res.OK {
		t.Fatalf("a delivery that took nothing should fail: %+v", res)
	}
	if !c2.logged("could not turn the 25 loose essence into notes (the bank at the mule's post would not open)") {
		t.Fatalf("log:\n%s", strings.Join(c2.logs, "\n"))
	}
}

// A bank that will not open: every Nature crosses, and the log says why the share was not kept.
func TestCarrierKeepWithABankThatWillNotOpen(t *testing.T) {
	rig, crossed := keepRig(t, 2300, 0, 200)
	old := mastercraftrunner.OpenBank
	t.Cleanup(func() { mastercraftrunner.OpenBank = old })
	mastercraftrunner.OpenBank = func(*fleetrunner.Deps, tile.Tile) (*actions.Bank, bool) { return nil, false }
	w := rig.world()
	if res := w.deliver(); !res.OK || *crossed != 2300 {
		t.Fatalf("%+v crossed %d", res, *crossed)
	}
	if !rig.logged("keep: could not bank the share of Natures (the bank at the mule's post would not open): all of them cross to the mule") {
		t.Fatalf("log:\n%s", strings.Join(rig.logs, "\n"))
	}
}

// The share is the leftover Natures only: a carrier with essence notes to hand over keeps its share of Natures and still trades the notes.
func TestCarrierKeepLeavesTheEssenceLeftoversToTheMule(t *testing.T) {
	rig, crossed := keepRig(t, 2300, 0, 200)
	rig.sim.Give(runecraftplan.NotedEssenceID, 50)
	var items []string
	old := masterDeliver
	t.Cleanup(func() { masterDeliver = old })
	masterDeliver = func(_ *fleetrunner.Deps, _ *donorrunner.Deps, _ string, _ tile.Tile, _, _ int, it []string) runerunner.Delivery {
		items = it
		n := simCount(rig.sim.Pack(), natRune)
		rig.sim.Remove(natRune, n)
		rig.sim.Remove(runecraftplan.NotedEssenceID, 50)
		*crossed += n
		return runerunner.Delivery{Result: donorrunner.Result{Traded: true}, Delivered: map[string]int{"Rune essence": 50, "Nature rune": n}}
	}
	w := rig.world()
	if res := w.deliver(); !res.OK || *crossed != 2100 || w.delivered["Rune essence"] != 50 || strings.Join(items, ",") != "Rune essence,Nature rune" {
		t.Fatalf("%+v crossed %d delivered %v items %v", res, *crossed, w.delivered, items)
	}
}

// ---- standby ----

func standbyRig(t *testing.T) *carrierRig {
	t.Helper()
	c := newCarrierRig(t, nil, nil)
	mindHoldStat(c.sim.State(), gamestate.HitpointsStat, levels.XPAtLevel(40), 40)
	return c
}

func TestCarrierSaysItStandsBy(t *testing.T) {
	c := standbyRig(t)
	now := time.Now()
	c.b.publishStandby(c.ctx, now)
	got := mulebus.ReadStandby(c.ctx.Bus, crafterName, now.Add(time.Millisecond), 0)
	if len(got) != 1 || got[0].Name != "Acct 7" || got[0].Role != mulebus.CarrierStandby {
		t.Fatalf("%+v", got)
	}
	// It is ready, not at work: the crafter's carriers at work do not count it.
	if work := mulebus.ReadCarriers(c.ctx.Bus, crafterName, now.Add(time.Millisecond), 0); len(work) != 0 {
		t.Fatalf("a standby is not at work: %+v", work)
	}
	// It is said on the check, and the world it is said on is the account's.
	c2 := standbyRig(t)
	c2.b.check(c2.ctx, time.Now())
	if len(mulebus.ReadStandby(c2.ctx.Bus, crafterName, time.Now().Add(time.Millisecond), 0)) != 1 {
		t.Fatal("a check publishes the standby")
	}
}

// Only an account that could start a shift stands by: the crafter set, a members world, hitpoints at the floor, no backoff.
func TestCarrierStandbyOnlyWhenReady(t *testing.T) {
	now := time.Now()
	for name, mod := range map[string]func(c *carrierRig){
		"no crafter named":    func(c *carrierRig) { c.b.set.Crafter = "" },
		"a free world":        func(c *carrierRig) { c.ctx.Pack.Members = false },
		"under the hitpoints": func(c *carrierRig) { c.b.set.MinHP = 60 },
		"backing off":         func(c *carrierRig) { c.b.backoffUntil = now.Add(10 * time.Minute) },
		"no bus":              func(c *carrierRig) { c.ctx.Bus = nil },
		"the crafter on another world": func(c *carrierRig) {
			c.ctx.WorldOf = func(account string) int {
				if strings.EqualFold(account, crafterName) {
					return 2
				}
				return 0
			}
		},
	} {
		c := standbyRig(t)
		bus := c.ctx.Bus
		mod(c)
		c.b.publishStandby(c.ctx, now)
		if len(mulebus.ReadStandby(bus, crafterName, now.Add(time.Millisecond), 0)) != 0 {
			t.Errorf("%s: stood by", name)
		}
	}
	// The crafter on the carrier's own world, or on a world nobody knows: ready.
	c0 := standbyRig(t)
	c0.ctx.WorldOf = func(account string) int { return 1 }
	c0.b.publishStandby(c0.ctx, now)
	if len(mulebus.ReadStandby(c0.ctx.Bus, crafterName, now.Add(time.Millisecond), 0)) != 1 {
		t.Error("the crafter on this world: ready")
	}
	c1 := standbyRig(t)
	c1.ctx.WorldOf = func(account string) int { return 0 }
	c1.b.publishStandby(c1.ctx, now)
	if len(mulebus.ReadStandby(c1.ctx.Bus, crafterName, now.Add(time.Millisecond), 0)) != 1 {
		t.Error("the crafter's world unknown: ready")
	}
	// An account whose hitpoints have not been read yet (a start, a hop) says nothing: it may be 12 hitpoints for all it knows.
	c := newCarrierRig(t, nil, nil)
	c.b.publishStandby(c.ctx, now)
	if len(mulebus.ReadStandby(c.ctx.Bus, crafterName, now.Add(time.Millisecond), 0)) != 0 {
		t.Error("stood by with the hitpoints unread")
	}
}
