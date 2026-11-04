package combatplan

// HERBS FROM KILLS (a user request): "we want to be looting herbs especially ranarrs so we can
// use them for prayer potions".
//
// Every grimy herb in content 289 is named "Herb" (skill_herblore/configs/identifying/unids.obj,
// name=Herb, one obj per herb) and carries no cost, so oc_cost is the default and the name/value
// loot rules (WorthTrainLoot's TrainLootMinCost, a safespot's MinValue) never took one. They are
// told apart by OBJ ID only (pack/obj.pack, the unidentified_* rows; each id+1 is its bank note).
//
// HerbIDs ranks them by worth, best first: ranarr (prayer potions) leads, then the high herbs
// (snapdragon, torstol, lantadyme, dwarf weed, cadantine, kwuarm, avantoe), then the low ones.

// HerbIDs are the grimy herbs' obj ids, best first (content 289 pack/obj.pack).
var HerbIDs = []int{
	207,  // unidentified_ranarr: Prayer potion
	3051, // unidentified_snapdragon
	219,  // unidentified_torstol
	2485, // unidentified_lantadyme
	217,  // unidentified_dwarf_weed
	215,  // unidentified_cadantine
	213,  // unidentified_kwuarm
	211,  // unidentified_avantoe
	3049, // unidentified_toadflax
	209,  // unidentified_irit
	205,  // unidentified_harralander
	203,  // unidentified_tarromin
	201,  // unidentified_marentill
	199,  // unidentified_guam
}

// RanarrHerbID is the grimy ranarr (unidentified_ranarr).
const RanarrHerbID = 207

// HerbRank is a grimy herb's place in HerbIDs (0 is ranarr), and whether id is one at all.
func HerbRank(id int) (int, bool) {
	for i, h := range HerbIDs {
		if h == id {
			return i, true
		}
	}
	return 0, false
}

// HerbMinFree is the free pack slots a herb of rank needs before it is picked up, so a tight pack
// keeps its last slots for the best herbs: ranarr and the three rarest take the last free slot, the
// rest of the high herbs need two, and the low herbs (toadflax down to guam) four.
func HerbMinFree(rank int) int {
	switch {
	case rank <= 3:
		return 1
	case rank <= 7:
		return 2
	}
	return 4
}

// WorthHerb reports whether a ground item with obj id is a herb worth a slot with freeSlots free;
// isHerb is false for anything that is not a grimy herb.
func WorthHerb(id, freeSlots int) (worth, isHerb bool) {
	rank, ok := HerbRank(id)
	if !ok {
		return false, false
	}
	return freeSlots >= HerbMinFree(rank), true
}
