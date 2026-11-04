package runecraftrunner

// STRAY PACK ITEMS. The gate's pack is the Nature talisman, the Nature stack, the fare (coins), the essence a trade brings and, in road
// mode, a stack of noted bows to alch. The visit at the mule's post banked everything else, so anything else in the pack at the gate came
// from outside the round (a random event's reward) and takes one of the slots a trade of 25 essence needs.

import (
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
)

// Strays are the pack items the gate has no use for: everything but the talismans, the Nature runes, coins, essence (unnoted or noted) and
// the noted bows the alchemy holds.
func Strays(d *fleetrunner.Deps, bows []AlchBowIDs) []gamestate.SlotItem {
	var out []gamestate.SlotItem
	for _, it := range packItems(d) {
		id := int(it.ID)
		switch {
		case it.ID == NatureID, it.ID == CoinsID, id == runecraftplan.EssenceID, id == runecraftplan.NotedEssenceID:
			continue
		case isTalisman(id), isAlchNote(bows, it.ID):
			continue
		}
		out = append(out, it)
	}
	return out
}

func isTalisman(id int) bool {
	_, ok := runecraftplan.ByTalisman(id)
	return ok
}

func isAlchNote(bows []AlchBowIDs, id int32) bool {
	for _, b := range bows {
		if b.Noted == id {
			return true
		}
	}
	return false
}
