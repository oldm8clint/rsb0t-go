package mastercraftplan

import (
	"strconv"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/scripts/herbplan"
)

// THE HERB ROUND (a user request: "once the mule has 1000 grimy guams ... mastercraft should
// request all herbs in the mules bank that it can clean. clean all the herbs as fast as possible
// ... then once all herbs are clean they can be handed over to the mule"). the main account is the only
// account that has done Druidic Ritual.
//
// THE CONTENT (content/scripts, branch 289):
//
//   - Every grimy herb is named "Herb" with its own obj id (skill_herblore/configs/identifying/
//     unids.obj, iop1=Identify, members=yes); internal/scripts/herbplan.Herbs carries the 19 ids, the
//     clean ids and the levels.
//   - Cleaning ([opheld1,unidentified_*] in skill_herblore/scripts/identifying/identify.rs2 ->
//     [proc,attempt_identify_herb] -> herblore.rs2 [proc,identify_herb]): members world only, the
//     Herblore level is the clean herb's identified_herb_level param (default 3: guam; marrentill 5,
//     tarromin 11, harralander 20, ranarr 25, toadflax 30, irit 40, avantoe 48, kwuarm 54,
//     snapdragon 59, cadantine 65, lantadyme 67, dwarf weed 70, torstol 75: herbs.obj). The herb is
//     replaced IN ITS SLOT (inv_setslot), XP is the clean herb's identified_herb_exp, and there is NO
//     delay. No Druidic Ritual check in this build ("early osrs had no druidic ritual check").
//   - So five Identify clicks a tick (the engine's five user events) clean five herbs a tick, 27
//     (28) a load: the gem round's pace, one burst a server tick with one confirm at the end.
//
// THE NAME: the mule's published bank and the kit ask address each grimy herb by its qualified name
// ("Herb id199", cachecfg.QualifiedName), which peerrunner publishes for "Herb" and cachecfg.ObjIDs
// resolves to that one obj and its note.

// GuamID is the grimy guam's obj id (unidentified_guam).
const GuamID int32 = 199

// HerbName is a grimy herb's qualified name, as the mule publishes it and a kit asks for it.
func HerbName(h herbplan.Herb) string { return cachecfg.QualifiedName("Herb", int(h.UnidID)) }

// HerbsCleanable is every grimy herb Herblore allows, lowest level first (herbplan's order), less
// the Jungle Potion quest herbs.
func HerbsCleanable(herblore int) []herbplan.Herb {
	var out []herbplan.Herb
	for _, h := range herbplan.Eligible(herblore, nil, nil, true) {
		if !untradeableHerb[h.Key] {
			out = append(out, h)
		}
	}
	return out
}

// HerbTradeable reports whether a clean herb can go to the mule: every herb but the Jungle Potion
// ones (untradeableHerb).
func HerbTradeable(h herbplan.Herb) bool { return !untradeableHerb[h.Key] }

// untradeableHerb are the Jungle Potion herbs: herbs.obj gives every one tradeable=no, so a cleaned
// one can never go to the mule. Live 4 Oct the main account withdrew its Rogues purse every round and could
// not hand it over (owner: "its untradable and should be kept off the herb clean list").
var untradeableHerb = map[string]bool{
	"snake_weed": true, "ardrigal": true, "sito_foil": true, "volencia_moss": true, "rogues_purse": true,
}

// HerbsReady decides a herb round: the mule publishes at least set.HerbThreshold grimy guams, or
// this account's bank holds grimy herbs it can clean (a round that ended early resumes). own
// counts this account's bank by grimy herb id.
func HerbsReady(set Settings, herblore int, herbloreKnown bool, mule Stock, own map[int32]int) (ok, resume bool, why string) {
	switch {
	case !herbloreKnown:
		return false, false, "Herblore is not known yet"
	case !set.MembersWorld:
		return false, false, "cleaning herbs needs a members world (" + KeyMembersWorld + " is off)"
	}
	herbs := HerbsCleanable(herblore)
	if len(herbs) == 0 {
		return false, false, "Herblore " + strconv.Itoa(herblore) + " cleans no herb"
	}
	for _, h := range herbs {
		if own[h.UnidID] > 0 {
			return true, true, "this account's bank holds grimy " + h.Name + " to clean"
		}
	}
	if mule == nil {
		return false, false, "the mule has not published its bank"
	}
	guams := stockOf(mule, HerbName(herbplan.Herbs[0]))
	if guams < set.HerbThreshold {
		return false, false, "the mule holds " + strconv.Itoa(guams) + " grimy guams, under the threshold " + strconv.Itoa(set.HerbThreshold)
	}
	return true, false, "the mule holds " + strconv.Itoa(guams) + " grimy guams (threshold " + strconv.Itoa(set.HerbThreshold) + ")"
}

// PlanHerbAsk is the kit for a herb round: every grimy herb the mule publishes that Herblore can
// clean, by its qualified name, each line at most set.ArrowKit (the bulk line cap; the mule's own
// anyMaxPerOrder clamps it further, and the round asks again for what is left).
func PlanHerbAsk(set Settings, herblore int, mule Stock) []Line {
	var lines []Line
	for _, h := range HerbsCleanable(herblore) {
		if n := min(stockOf(mule, HerbName(h)), set.ArrowKit); n > 0 {
			lines = append(lines, Line{Item: HerbName(h), Qty: n})
		}
	}
	return lines
}
