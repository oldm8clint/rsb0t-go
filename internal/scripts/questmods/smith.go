package questmods

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// Smithing at an anvil, from content 289's skill_smithing:
//
//   - A bar used on an anvil (loc 2783, `[oplocu,_anvil]`) transmits five column inventories
//     for that metal and opens interface 994 (`smithing`). A hammer must be held.
//   - INV_BUTTON1/2/3 on a column component makes 1/5/10 of the product in that slot
//     (`[inv_button1..3,smithing:columnN]` -> @attempt_smithing), checking the bar count and
//     level. Column 5 (component 1123) holds, for steel, dart tips, arrowheads, knives, NAILS
//     (slot 3, two per bar, Smithing 34) and studs (smithing.inv:101-107).
//   - Column 1 (component 1119) slot 0 is the dagger: one bar, Smithing 1/15/30 for
//     bronze/iron/steel, the cheapest product per bar for training.
const (
	ifSmithing      = 994
	comSmithColumn1 = 1119
	comSmithColumn5 = 1123

	ObjHammer    = 2347
	ObjBronzeBar = 2349
	ObjIronBar   = 2351
	ObjSteelBar  = 2353
	ObjNails     = 1539

	objBronzeDagger = 1205
	objIronDagger   = 1203
	objSteelDagger  = 1207
)

// Anvils near banks, nearest-first by the caller. Seers' three anvils (2712-2714,3492-3495)
// are next to the ladder's Seers hub; Varrock west's are beside the west bank.
var Anvils = []tile.Tile{
	{X: 2713, Z: 3492}, {X: 2712, Z: 3495}, {X: 2714, Z: 3495},
	{X: 3188, Z: 3424}, {X: 3188, Z: 3426}, {X: 3188, Z: 3421},
}

// smithTier is one metal's training product.
type smithTier struct {
	Bar, Dagger, Level int
	Name               string
}

var smithTiers = []smithTier{
	{Bar: ObjSteelBar, Dagger: objSteelDagger, Level: 30, Name: "Steel bar"},
	{Bar: ObjIronBar, Dagger: objIronDagger, Level: 15, Name: "Iron bar"},
	{Bar: ObjBronzeBar, Dagger: objBronzeDagger, Level: 1, Name: "Bronze bar"},
}

// TierFor is the best bar a Smithing level can work into a dagger.
func tierFor(level int) smithTier {
	for _, t := range smithTiers {
		if level >= t.Level {
			return t
		}
	}
	return smithTiers[len(smithTiers)-1]
}

func nearestAnvil(here tile.Tile) tile.Tile {
	best := Anvils[0]
	for _, a := range Anvils[1:] {
		if tile.Chebyshev(a, here) < tile.Chebyshev(best, here) {
			best = a
		}
	}
	return best
}

// SmithAtAnvil works bars of barID into the product in (com, slot) at the anvil at, or the
// nearest known one when at is zero, using up to bars of them, and returns how many it used.
func SmithAtAnvil(d *questrunner.Deps, at tile.Tile, barID, productID, com, slot, bars int) int {
	if !held(d, ObjHammer) {
		logf(d, "smithing needs a hammer in the pack")
		return 0
	}
	start := questrunner.HeldID(d, barID)
	if start == 0 || bars <= 0 {
		return 0
	}
	if bars > start {
		bars = start
	}
	if at == (tile.Tile{}) {
		at = nearestAnvil(questrunner.CurrentTile(d))
	}
	if !questrunner.Travel(d, at, 3) {
		return 0
	}
	m, ok := questrunner.FindLoc(d, 0, "Anvil", "", at, 6)
	if !ok {
		logf(d, "no anvil loaded at %d,%d", at.X, at.Z)
		return 0
	}
	used := func() int { return start - questrunner.HeldID(d, barID) }
	packBefore := questrunner.LootCounts(d)
	// Whatever the anvil made, onto the Loot tab's Smithing card, one event a bar worked (owner, 7
	// Oct: every item made inside a quest stage counts under the activity that made it).
	defer func() {
		if n := used(); n > 0 {
			questrunner.ReportMade(d, loot.Forging(barID, questrunner.ObjName(d, barID)), n, packBefore, barID, ObjHammer)
		}
	}()
	for tries := 0; used() < bars && tries < 2*bars+4; tries++ {
		if err := questrunner.UseItemOnLoc(d, barID, m); err != nil {
			logf(d, "bar on the anvil: %v", err)
			break
		}
		if !waitUntil(d, 6*time.Second, func() bool { main, _, _ := d.Session.State().Modals(); return main == ifSmithing }) {
			logf(d, "the anvil opened no smithing interface")
			continue
		}
		left := bars - used()
		option, n := 1, 1
		switch {
		case left >= 10:
			option, n = 3, 10
		case left >= 5:
			option, n = 2, 5
		}
		before := used()
		if err := actions.InvButton(d.Session, option, int32(productID), slot, com); err != nil {
			logf(d, "smithing button: %v", err)
			break
		}
		// One bar per five ticks (the lab's 200 ms tick makes it a second); a level refusal or a
		// missing hammer opens a message box instead, and the bar count does not move.
		waitUntil(d, time.Duration(n)*4*time.Second+3*time.Second, func() bool { return used() >= before+n })
		if used() == before {
			logf(d, "the anvil made nothing (level, bars or hammer)")
			questrunner.ClearLeftoverModal(d)
			break
		}
	}
	return used()
}

// smithNails is the provisioner's skill source for nails: two per steel bar at Smithing 34.
// The steel bars (and the hammer) are the kit's own lines, so they come from the bank or the
// ONE bundled mule ask; this only works them at the anvil.
func smithNails(d *questrunner.Deps, task questplan.SourceTask) bool {
	need := (task.Qty + 1) / 2
	if need <= 0 {
		need = 1
	}
	return SmithAtAnvil(d, task.Source.Tile, ObjSteelBar, ObjNails, comSmithColumn5, 3, need) > 0
}

// SmithingRung trains Smithing to level on daggers: the best bar the level can work, drawn
// from the bank, or else from the mule (one ask per 27 bars), with a hammer. Daggers are
// banked between trips (they are the fleet's to sell; nothing is dropped). id is the ladder's
// rung id (smithing34).
func SmithingRung(id string, level int) questrunner.Quest {
	const custom = "smith-daggers"
	decide := func(s questplan.Snapshot) questplan.Step {
		if s.Level("smithing") >= level {
			return questplan.Step{Kind: questplan.StepDone}
		}
		t := tierFor(s.Level("smithing"))
		bars := s.HeldID(t.Bar)
		if s.HeldID(ObjHammer) == 0 || bars == 0 {
			kit := fmt.Sprintf("smith-%d", t.Bar)
			if s.Failed["provision:"+kit] {
				return questplan.Step{Kind: questplan.StepWait, Reason: "no " + t.Name + "s or no hammer from the bank or the mule"}
			}
			return questplan.Step{Kind: questplan.StepProvision, KitName: kit, Bank: questplan.SeersBank, BankSet: true,
				KeepIDs: []int{ObjHammer, t.Bar, questplan.CoinsID},
				Kit: []questplan.KitLine{
					{ID: ObjHammer, Name: "Hammer", Qty: 1},
					{ID: t.Bar, Name: t.Name, Qty: 27},
				}, Reason: "bars and a hammer for Smithing"}
		}
		return questplan.Step{Kind: questplan.StepCustom, Custom: custom, Reason: fmt.Sprintf("%ss into daggers to Smithing %d", t.Name, level)}
	}
	return questrunner.Quest{
		Module: questplan.Module{Record: questplan.Record{ID: id, Name: fmt.Sprintf("Smithing %d", level)},
			Anchor: questplan.SeersBank, Decide: decide},
		Customs: map[string]questrunner.CustomFunc{custom: func(e *questrunner.Engine, _ questplan.Step) bool {
			d := e.Deps()
			t := tierFor(questrunner.BaseLevels(d)["smithing"])
			return SmithAtAnvil(d, tile.Tile{}, t.Bar, t.Dagger, comSmithColumn1, 0, questrunner.HeldID(d, t.Bar)) > 0
		}},
		Done: func(e *questrunner.Engine) bool { return questrunner.BaseLevels(e.Deps())["smithing"] >= level },
	}
}
