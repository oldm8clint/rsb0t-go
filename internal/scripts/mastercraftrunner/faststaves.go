package mastercraftrunner

// The Fire battlestaff round's fast run: the bow-stringing load (fastgems.go pairSpec) on a Battlestaff and a Fire orb. See
// mastercraftplan/staves.go for the content: no delay, Crafting 62, 125 XP a staff, members worlds only.

import (
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/bowstringrunner"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/gemrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
)

// StavesIDs are the three items of the load: the Battlestaff, the product and the Fire orb.
var StavesIDs = bowstringrunner.IDs{Unstrung: mastercraftplan.BattlestaffID, Strung: mastercraftplan.FireBattlestaffID, String: mastercraftplan.FireOrbID}

// StavesProducts is what the round hands the mule.
func StavesProducts() []Product {
	return []Product{{ID: mastercraftplan.FireBattlestaffID, Name: mastercraftplan.FireBattlestaff}}
}

// FastStaves attaches Fire orbs to Battlestaves the fast way, starting from a primed quick bank with the pack empty or holding a
// finished load (14 Fire battlestaves in slots 0..13): 14 staves a load in four ticks (see FastBows, whose stream and refusal-free
// check it shares; the Crafting level guard and the desync check read Crafting XP, 125 a staff). When the run cannot start the visit
// banks the pack and draws nothing (Load; Short when the bank holds less than a load).
func FastStaves(d *fleetrunner.Deps, q *QuickBank, o FastOptions) FastResult {
	spec := pairSpec(mastercraftplan.FireBattlestaff, StavesIDs, mastercraftplan.StavesXPTenths, gemrunner.CraftingStat, nil)
	r, qv := fastRun(d, q, spec, o)
	if qv != nil {
		r.Load = loadVisit(d, qv)
	}
	return r
}

// StavesOwn reads the Battlestaff and Fire orb a bank reading holds.
func StavesOwn(banked []gamestate.SlotItem) (staves, orbs int) {
	for _, it := range banked {
		switch it.ID {
		case mastercraftplan.BattlestaffID:
			staves += int(it.Count)
		case mastercraftplan.FireOrbID:
			orbs += int(it.Count)
		}
	}
	return staves, orbs
}
