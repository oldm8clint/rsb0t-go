package questplan

import (
	"math/rand"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Tai Bwo Wannai Trio's decision over simulated worlds: the tab's colour, the journal's state, where the account stands and what the pack
// and the bank hold. Every step it returns is well formed, a leg that failed parks the quest, and a quest short of its scarce supplies
// parks before the account has crossed the sea.

type tbWorld struct {
	st     TbwtState
	tab    Status // zero value: in progress
	tile   tile.Tile
	inv    map[int]int
	ran    []string
	failed []string
	hp, mx int
	levels map[string]int
	free   int
	noTile bool
}

var tbVillage = tile.Tile{X: 2800, Z: 3085}
var tbMainland = tile.Tile{X: 2655, Z: 3286}

// tbKit is the pack of an account that is ready for every leg: the tools, the runes, four doses, a spear and food.
func tbKit() map[int]int {
	return inv(CoinsID, 300, TbObjNet, 1, TbObjPestle, 1, ObjKnife, 1, ObjSeaweed, 1, ObjFireRune, 4, ObjAirRune, 12, ObjChaosRune, 6, TbObjNatureRune, 1,
		3032, 1, 1243, 1, ObjShark, 6)
}

func (w tbWorld) snap() Snapshot {
	s := Snapshot{Journal: StatusInProgress, Tile: tbVillage, TileKnown: true, InvIDs: map[int]int{}, WornIDs: map[int]int{}, FreeSlots: 12,
		Ran: map[string]bool{}, Failed: map[string]bool{}, Blocked: map[string]bool{}, HP: 54, MaxHP: 54,
		Levels: map[string]int{"attack": 53, "strength": 41, "defence": 50, "hitpoints": 54, "prayer": 32, "magic": 64, "ranged": 1, "cooking": 60,
			"agility": 30, "fishing": 70, "firemaking": 1}}
	if w.tab != "" {
		s.Journal = w.tab
	}
	if w.tile != (tile.Tile{}) {
		s.Tile = w.tile
	}
	s.TileKnown = !w.noTile
	s.Stage, s.StageKnown = w.st.Stage(), true
	for id, n := range w.inv {
		s.InvIDs[id] = n
	}
	for _, k := range w.ran {
		s.Ran[k] = true
	}
	for _, k := range w.failed {
		s.Failed[k] = true
	}
	if w.hp != 0 {
		s.HP = w.hp
	}
	if w.mx != 0 {
		s.MaxHP = w.mx
	}
	for k, v := range w.levels {
		s.Levels[k] = v
	}
	if w.free != 0 {
		s.FreeSlots = w.free
	}
	return s
}

// started is a state in the brothers' phase with the brothers' stages given.
func tbState(lubufu, tiadeche, tamayu, tinsay int) TbwtState {
	return TbwtState{Phase: TbPhaseStarted, Lubufu: lubufu, Tiadeche: tiadeche, Tamayu: tamayu, Tinsay: tinsay}
}

func tbWith(items map[int]int, extra map[int]int) map[int]int {
	out := map[int]int{}
	for k, v := range items {
		out[k] = v
	}
	for k, v := range extra {
		out[k] += v
	}
	return out
}

func tbWithout(items map[int]int, ids ...int) map[int]int {
	out := map[int]int{}
	for k, v := range items {
		out[k] = v
	}
	for _, id := range ids {
		delete(out, id)
	}
	return out
}

func TestDecideTaiBwoTrioTheLegsInOrder(t *testing.T) {
	kit := tbKit()
	for _, c := range []struct {
		name string
		w    tbWorld
		kind StepKind
		cust string
		qty  int
	}{
		{"the job: a red tab", tbWorld{tab: StatusNotStarted, inv: kit}, StepCustom, CustomTbStart, 0},
		{"the job: the pre-start page", tbWorld{st: TbwtState{Phase: TbPhasePre}, inv: kit}, StepCustom, CustomTbStart, 0},
		{"the report: the brothers are home", tbWorld{st: TbwtState{Phase: TbPhaseBrothers, Lubufu: TbLubufuDone, Tiadeche: TbTiadecheDone, Tinsay: 7, Tamayu: 4}, inv: kit}, StepCustom, CustomTbReport, 0},

		{"Lubufu: not met", tbWorld{st: tbState(TbLubufuNone, 0, 0, 0), inv: kit}, StepCustom, CustomTbLubufuMeet, 0},
		{"Lubufu: met", tbWorld{st: tbState(TbLubufuMet, 0, 0, 0), inv: kit}, StepCustom, CustomTbLubufuMeet, 0},
		{"Lubufu: the offer is made, not taken", tbWorld{st: tbState(TbLubufuOffered, 0, 0, 0), inv: kit}, StepCustom, CustomTbLubufuMeet, 0},
		{"Lubufu: the bait, none caught: fish what the room allows", tbWorld{st: tbState(TbLubufuBait, 0, 0, 0), inv: kit}, StepCustom, CustomTbFish, 10},
		{"Lubufu: the bait, a part load: fish the rest", tbWorld{st: tbState(TbLubufuBait, 0, 0, 0), inv: tbWith(kit, inv(TbObjRawKarambwanji, 4)), free: 6}, StepCustom, CustomTbFish, 8},
		{"Lubufu: the bait, the pack nearly full: hand over", tbWorld{st: tbState(TbLubufuBait, 0, 0, 0), inv: tbWith(kit, inv(TbObjRawKarambwanji, 9)), free: 2}, StepCustom, CustomTbLubufuGive, 0},
		{"Lubufu: the bait, twenty held: hand over", tbWorld{st: tbState(TbLubufuBait, 0, 0, 0), inv: tbWith(kit, inv(TbObjRawKarambwanji, 20)), free: 5}, StepCustom, CustomTbLubufuGive, 0},
		{"Lubufu: given, with Karambwanji to spare: drop one", tbWorld{st: tbState(TbLubufuGiven, 0, 0, 0), inv: tbWith(kit, inv(TbObjRawKarambwanji, 7))}, StepOpHeld, "", 0},
		{"Lubufu: given, three kept: the questions", tbWorld{st: tbState(TbLubufuGiven, 0, 0, 0), inv: tbWith(kit, inv(TbObjRawKarambwanji, 3))}, StepCustom, CustomTbLubufuAsk, 0},
		{"Lubufu: the apprenticeship offered", tbWorld{st: tbState(TbLubufuApprentice, 0, 0, 0), inv: kit}, StepCustom, CustomTbLubufuAsk, 0},

		{"Tiadeche: not met", tbWorld{st: tbState(TbLubufuDone, TbTiadecheNone, 0, 0), inv: kit}, StepCustom, CustomTbTiadecheMeet, 0},
		{"Tiadeche: waiting, no vessel", tbWorld{st: tbState(TbLubufuDone, TbTiadecheWaiting, 0, 0), inv: kit}, StepCustom, CustomTbVessel, 0},
		{"Tiadeche: waiting, a vessel and no bait: net one", tbWorld{st: tbState(TbLubufuDone, TbTiadecheWaiting, 0, 0), inv: tbWith(kit, inv(TbObjVessel, 1))}, StepCustom, CustomTbFish, 1},
		{"Tiadeche: waiting, a vessel and a Karambwanji: bait it", tbWorld{st: tbState(TbLubufuDone, TbTiadecheWaiting, 0, 0), inv: tbWith(kit, inv(TbObjVessel, 1, TbObjRawKarambwanji, 2))}, StepCustom, CustomTbBait, 0},
		{"Tiadeche: waiting, the vessel baited: the catch", tbWorld{st: tbState(TbLubufuDone, TbTiadecheWaiting, 0, 0), inv: tbWith(kit, inv(TbObjVesselLoaded, 1))}, StepCustom, CustomTbTiadecheCatch, 0},
		{"Tiadeche: caught, the request not made", tbWorld{st: tbState(TbLubufuDone, TbTiadecheCaught, 0, 0), inv: kit}, StepCustom, CustomTbTiadecheMeet, 0},

		{"Tamayu: not met", tbWorld{st: tbState(TbLubufuDone, TbTiadecheManual, TbTamayuNone, 0), inv: kit}, StepCustom, CustomTbTamayuMeet, 0},
		{"Tamayu: the vow made: the first hunt", tbWorld{st: tbState(TbLubufuDone, TbTiadecheManual, TbTamayuSlay, 0), inv: kit}, StepCustom, CustomTbTamayuHunt, 0},
		{"Tamayu: a hunt watched, a four-dose bottle: give it", tbWorld{st: tbState(TbLubufuDone, TbTiadecheManual, TbTamayuWatched, 0), inv: kit}, StepCustom, CustomTbTamayuGive, 3032},
		{"Tamayu: a hunt watched, two three-dose bottles: the first", tbWorld{st: tbState(TbLubufuDone, TbTiadecheManual, TbTamayuWatched, 0), inv: tbWith(tbWithout(kit, 3032), inv(3034, 2))}, StepCustom, CustomTbTamayuGive, 3034},

		{"Tinsay: not met", tbWorld{st: TbwtState{Phase: TbPhaseStarted, Lubufu: TbLubufuDone, Tiadeche: TbTiadecheManual, Tamayu: 4}, inv: kit}, StepCustom, CustomTbTinsayMeet, 0},
		{"Tinsay: the rum owed, nothing made: a banana first", tbWorld{st: TbwtState{Phase: TbPhaseStarted, Lubufu: TbLubufuDone, Tiadeche: TbTiadecheManual, Tamayu: 4, Tinsay: TbTinsayRum}, inv: kit}, StepCustom, CustomTbBanana, 0},
		{"Tinsay: the rum owed, a banana: slice it", tbWorld{st: TbwtState{Phase: TbPhaseStarted, Lubufu: TbLubufuDone, Tiadeche: TbTiadecheManual, Tamayu: 4, Tinsay: TbTinsayRum}, inv: tbWith(kit, inv(ObjBanana, 1))}, StepCustom, CustomTbSlice, 0},
		{"Tinsay: the rum owed, sliced: buy the rum", tbWorld{st: TbwtState{Phase: TbPhaseStarted, Lubufu: TbLubufuDone, Tiadeche: TbTiadecheManual, Tamayu: 4, Tinsay: TbTinsayRum}, inv: tbWith(kit, inv(TbObjSlicedBanana, 1))}, StepCustom, CustomTbRum, 0},
		{"Tinsay: the rum owed, sliced and a rum: mix", tbWorld{st: TbwtState{Phase: TbPhaseStarted, Lubufu: TbLubufuDone, Tiadeche: TbTiadecheManual, Tamayu: 4, Tinsay: TbTinsayRum}, inv: tbWith(kit, inv(TbObjSlicedBanana, 1, ObjKaramjaRum, 1))}, StepCustom, CustomTbMix, 0},
		{"Tinsay: the rum made: give it", tbWorld{st: TbwtState{Phase: TbPhaseStarted, Lubufu: TbLubufuDone, Tiadeche: TbTiadecheManual, Tamayu: 4, Tinsay: TbTinsayRum}, inv: tbWith(kit, inv(TbObjRumSlicedBanana, 1))}, StepCustom, CustomTbTinsayGive, TbObjRumSlicedBanana},
		{"Tinsay: the rum given: ask again", tbWorld{st: TbwtState{Phase: TbPhaseStarted, Lubufu: TbLubufuDone, Tiadeche: TbTiadecheManual, Tamayu: 4, Tinsay: TbTinsayGivenRum}, inv: kit}, StepCustom, CustomTbTinsayMeet, 0},
		{"Tinsay: the sandwich owed: a monkey", tbWorld{st: TbwtState{Phase: TbPhaseStarted, Lubufu: TbLubufuDone, Tiadeche: TbTiadecheManual, Tamayu: 4, Tinsay: TbTinsaySandwich}, inv: kit}, StepCustom, CustomTbMonkey, 0},
		{"Tinsay: the sandwich owed: a corpse to Tamayu", tbWorld{st: TbwtState{Phase: TbPhaseStarted, Lubufu: TbLubufuDone, Tiadeche: TbTiadecheManual, Tamayu: 4, Tinsay: TbTinsaySandwich}, inv: tbWith(kit, inv(TbObjMonkeyCorpse, 1))}, StepCustom, CustomTbSkin, 0},
		{"Tinsay: the sandwich owed: the skin and the seaweed", tbWorld{st: TbwtState{Phase: TbPhaseStarted, Lubufu: TbLubufuDone, Tiadeche: TbTiadecheManual, Tamayu: 4, Tinsay: TbTinsaySandwich}, inv: tbWith(kit, inv(TbObjMonkeySkin, 1))}, StepCustom, CustomTbSandwich, 0},
		{"Tinsay: the sandwich made: give it", tbWorld{st: TbwtState{Phase: TbPhaseStarted, Lubufu: TbLubufuDone, Tiadeche: TbTiadecheManual, Tamayu: 4, Tinsay: TbTinsaySandwich}, inv: tbWith(kit, inv(TbObjSandwich, 1))}, StepCustom, CustomTbTinsayGive, TbObjSandwich},
		{"Tinsay: the bones owed: a Jogre", tbWorld{st: TbwtState{Phase: TbPhaseStarted, Lubufu: TbLubufuDone, Tiadeche: TbTiadecheManual, Tamayu: 4, Tinsay: TbTinsayBones}, inv: kit}, StepCustom, CustomTbJogre, 0},
		{"Tinsay: the bones owed: bones in the pack: burn them", tbWorld{st: TbwtState{Phase: TbPhaseStarted, Lubufu: TbLubufuDone, Tiadeche: TbTiadecheManual, Tamayu: 4, Tinsay: TbTinsayBones}, inv: tbWith(kit, inv(TbObjJogreBones, 1))}, StepCustom, CustomTbBurn, 0},
		{"Tinsay: the bones owed: burnt, no paste, no Karambwanji: net one", tbWorld{st: TbwtState{Phase: TbPhaseStarted, Lubufu: TbLubufuDone, Tiadeche: TbTiadecheManual, Tamayu: 4, Tinsay: TbTinsayBones}, inv: tbWith(kit, inv(TbObjBurntJogreBones, 1))}, StepCustom, CustomTbFish, 1},
		{"Tinsay: the bones owed: burnt and a Karambwanji: grind it", tbWorld{st: TbwtState{Phase: TbPhaseStarted, Lubufu: TbLubufuDone, Tiadeche: TbTiadecheManual, Tamayu: 4, Tinsay: TbTinsayBones}, inv: tbWith(kit, inv(TbObjBurntJogreBones, 1, TbObjRawKarambwanji, 2))}, StepCustom, CustomTbGrind, TbObjRawKarambwanji},
		{"Tinsay: the bones owed: burnt and the paste: smother", tbWorld{st: TbwtState{Phase: TbPhaseStarted, Lubufu: TbLubufuDone, Tiadeche: TbTiadecheManual, Tamayu: 4, Tinsay: TbTinsayBones}, inv: tbWith(kit, inv(TbObjBurntJogreBones, 1, TbObjKarambwanjiPaste, 1))}, StepCustom, CustomTbPasteBones, 0},
		{"Tinsay: the bones owed: smothered: cook them", tbWorld{st: TbwtState{Phase: TbPhaseStarted, Lubufu: TbLubufuDone, Tiadeche: TbTiadecheManual, Tamayu: 4, Tinsay: TbTinsayBones}, inv: tbWith(kit, inv(TbObjBurntBonesInPaste, 1))}, StepCustom, CustomTbCook, TbObjBurntBonesInPaste},
		{"Tinsay: the bones owed: marinated: give them", tbWorld{st: TbwtState{Phase: TbPhaseStarted, Lubufu: TbLubufuDone, Tiadeche: TbTiadecheManual, Tamayu: 4, Tinsay: TbTinsayBones}, inv: tbWith(kit, inv(TbObjMarinatedBones, 1))}, StepCustom, CustomTbTinsayGive, TbObjMarinatedBones},

		{"the manual: held: to Tiadeche", tbWorld{st: TbwtState{Phase: TbPhaseStarted, Lubufu: TbLubufuDone, Tiadeche: TbTiadecheDone, Tamayu: 4, Tinsay: 7}, inv: tbWith(kit, inv(TbObjManual, 1))}, StepCustom, CustomTbManual, 0},
		{"the manual: asked for, a vessel in the pack: to Tinsay", tbWorld{st: TbwtState{Phase: TbPhaseStarted, Lubufu: TbLubufuDone, Tiadeche: TbTiadecheManual, Tamayu: 4, Tinsay: 7}, inv: tbWith(kit, inv(TbObjVessel, 1))}, StepCustom, CustomTbTinsayGive, TbObjVessel},
		{"the manual: asked for, no vessel: Lubufu's spare", tbWorld{st: TbwtState{Phase: TbPhaseStarted, Lubufu: TbLubufuDone, Tiadeche: TbTiadecheManual, Tamayu: 4, Tinsay: 7}, inv: kit}, StepCustom, CustomTbVessel, 0},
		{"the manual: the page reads 5 and the 6: talk to Tiadeche once", tbWorld{st: TbwtState{Phase: TbPhaseStarted, Lubufu: TbLubufuDone, Tiadeche: TbTiadecheDone, Tamayu: 4, Tinsay: 7}, inv: kit}, StepCustom, CustomTbTiadecheMeet, 0},
		{"the manual: talked to and not done: a vessel to Tinsay", tbWorld{st: TbwtState{Phase: TbPhaseStarted, Lubufu: TbLubufuDone, Tiadeche: TbTiadecheDone, Tamayu: 4, Tinsay: 7}, inv: tbWith(kit, inv(TbObjVesselLoaded, 1)), ran: []string{CustomTbTiadecheMeet}}, StepCustom, CustomTbTinsayGive, TbObjVesselLoaded},
	} {
		w := c.w
		if w.st.Phase == 0 && w.tab == "" {
			t.Fatalf("%s: no state", c.name)
		}
		got := DecideTaiBwoTrio(w.snap())
		if why := ValidateStep(got); why != "" {
			t.Errorf("%s: malformed step %+v: %s", c.name, got, why)
		}
		if got.Kind != c.kind || got.Custom != c.cust || (c.qty != 0 && got.Qty != c.qty) {
			t.Errorf("%s: %s %q qty %d (%s), want %s %q qty %d", c.name, got.Kind, got.Custom, got.Qty, got.Reason, c.kind, c.cust, c.qty)
		}
	}
}

// The spear: the Karambwan made into poison, in the order the content wants it, and where the chain has nothing to start from.
func TestDecideTaiBwoTrioTheSpearChain(t *testing.T) {
	kit := tbKit()
	base := func(items map[int]int, mods ...func(*tbWorld)) tbWorld {
		w := tbWorld{st: TbwtState{Phase: TbPhaseStarted, Lubufu: TbLubufuDone, Tiadeche: TbTiadecheManual, Tamayu: TbTamayuWatched, Agility: true}, inv: items}
		for _, m := range mods {
			m(&w)
		}
		return w
	}
	for _, c := range []struct {
		name string
		w    tbWorld
		kind StepKind
		cust string
		qty  int
	}{
		{"a poisoned spear: give it", base(tbWith(kit, inv(3173, 1))), StepCustom, CustomTbTamayuGive, 3173},
		{"the paste and a spear: smear it", base(tbWith(kit, inv(TbObjPoisonPaste, 1))), StepCustom, CustomTbPoison, 1243},
		{"the paste and no spear: wait", base(tbWith(tbWithout(kit, 1243), inv(TbObjPoisonPaste, 1))), StepWait, "", 0},
		{"a cooked Karambwan: grind it", base(tbWith(kit, inv(TbObjPoorKarambwan, 1))), StepCustom, CustomTbGrind, TbObjPoorKarambwan},
		{"a raw Karambwan: cook it", base(tbWith(kit, inv(TbObjRawKarambwan, 1))), StepCustom, CustomTbCook, TbObjRawKarambwan},
		{"a burnt Karambwan: drop it", base(tbWith(kit, inv(TbObjBurntKarambwan, 1))), StepOpHeld, "", 0},
		{"nothing to start from: Tiadeche's second gift", base(kit), StepCustom, CustomTbTiadecheGift, 0},
		{"the gift refused, Fishing 70, no vessel: a vessel", base(kit, func(w *tbWorld) { w.failed = []string{CustomTbTiadecheGift} }), StepCustom, CustomTbVessel, 0},
		{"the gift refused, a vessel and no bait: net one", base(tbWith(kit, inv(TbObjVessel, 1)), func(w *tbWorld) { w.failed = []string{CustomTbTiadecheGift} }), StepCustom, CustomTbFish, 2},
		{"the gift refused, a baited vessel: lower it", base(tbWith(kit, inv(TbObjVesselLoaded, 1)), func(w *tbWorld) { w.failed = []string{CustomTbTiadecheGift} }), StepCustom, CustomTbKarambwan, 0},
		{"the gift refused, a vessel and Karambwanji: lower it", base(tbWith(kit, inv(TbObjVessel, 1, TbObjRawKarambwanji, 2)), func(w *tbWorld) { w.failed = []string{CustomTbTiadecheGift} }), StepCustom, CustomTbKarambwan, 0},
		{"the gift refused, Fishing 60: wait", base(kit, func(w *tbWorld) { w.failed = []string{CustomTbTiadecheGift}; w.levels = map[string]int{"fishing": 60} }), StepWait, "", 0},
		{"the gift asked for this run: not asked again, Fishing 60: wait", base(kit, func(w *tbWorld) { w.ran = []string{CustomTbTiadecheGift}; w.levels = map[string]int{"fishing": 60} }), StepWait, "", 0},
		{"both given: the killing hunt", func() tbWorld {
			w := base(kit)
			w.st.Spear = true
			return w
		}(), StepCustom, CustomTbTamayuHunt, 1},
	} {
		got := DecideTaiBwoTrio(c.w.snap())
		if why := ValidateStep(got); why != "" {
			t.Errorf("%s: malformed step %+v: %s", c.name, got, why)
		}
		if got.Kind != c.kind || got.Custom != c.cust || (c.qty != 0 && got.Qty != c.qty) {
			t.Errorf("%s: %s %q qty %d (%s), want %s %q qty %d", c.name, got.Kind, got.Custom, got.Qty, got.Reason, c.kind, c.cust, c.qty)
		}
	}
	// Four doses short: Tamayu waits for the bank, with the number of doses in the reason.
	w := base(tbWithout(kit, 3032))
	w.st.Agility = false
	got := DecideTaiBwoTrio(w.snap())
	if got.Kind != StepWait || got.ParkFor != TbSupplyPark || !strings.Contains(got.Reason, "four doses") {
		t.Errorf("no bottle for Tamayu: %+v", got)
	}
}

// A leg that failed on its last try parks the quest for half an hour, with its name in the reason, and every leg is covered.
func TestDecideTaiBwoTrioAFailedLegParksTheQuest(t *testing.T) {
	kit := tbKit()
	worlds := map[string]tbWorld{
		CustomTbStart:         {st: TbwtState{Phase: TbPhasePre}, inv: kit},
		CustomTbReport:        {st: TbwtState{Phase: TbPhaseBrothers}, inv: kit},
		CustomTbLubufuMeet:    {st: tbState(TbLubufuNone, 0, 0, 0), inv: kit},
		CustomTbFish:          {st: tbState(TbLubufuBait, 0, 0, 0), inv: kit},
		CustomTbLubufuGive:    {st: tbState(TbLubufuBait, 0, 0, 0), inv: tbWith(kit, inv(TbObjRawKarambwanji, 20))},
		CustomTbLubufuAsk:     {st: tbState(TbLubufuGiven, 0, 0, 0), inv: kit},
		CustomTbVessel:        {st: tbState(TbLubufuDone, TbTiadecheWaiting, 0, 0), inv: kit},
		CustomTbBait:          {st: tbState(TbLubufuDone, TbTiadecheWaiting, 0, 0), inv: tbWith(kit, inv(TbObjVessel, 1, TbObjRawKarambwanji, 1))},
		CustomTbTiadecheMeet:  {st: tbState(TbLubufuDone, TbTiadecheNone, 0, 0), inv: kit},
		CustomTbTiadecheCatch: {st: tbState(TbLubufuDone, TbTiadecheWaiting, 0, 0), inv: tbWith(kit, inv(TbObjVesselLoaded, 1))},
		CustomTbTamayuMeet:    {st: tbState(TbLubufuDone, TbTiadecheManual, TbTamayuNone, 0), inv: kit},
		CustomTbTamayuHunt:    {st: tbState(TbLubufuDone, TbTiadecheManual, TbTamayuSlay, 0), inv: kit},
		CustomTbTamayuGive:    {st: tbState(TbLubufuDone, TbTiadecheManual, TbTamayuWatched, 0), inv: kit},
		CustomTbCook:          {st: TbwtState{Phase: TbPhaseStarted, Lubufu: 6, Tiadeche: 4, Tamayu: 3, Agility: true}, inv: tbWith(kit, inv(TbObjRawKarambwan, 1))},
		CustomTbGrind:         {st: TbwtState{Phase: TbPhaseStarted, Lubufu: 6, Tiadeche: 4, Tamayu: 3, Agility: true}, inv: tbWith(kit, inv(TbObjPoorKarambwan, 1))},
		CustomTbPoison:        {st: TbwtState{Phase: TbPhaseStarted, Lubufu: 6, Tiadeche: 4, Tamayu: 3, Agility: true}, inv: tbWith(kit, inv(TbObjPoisonPaste, 1))},
		CustomTbBanana:        {st: TbwtState{Phase: TbPhaseStarted, Lubufu: 6, Tiadeche: 4, Tamayu: 4, Tinsay: 2}, inv: kit},
		CustomTbSlice:         {st: TbwtState{Phase: TbPhaseStarted, Lubufu: 6, Tiadeche: 4, Tamayu: 4, Tinsay: 2}, inv: tbWith(kit, inv(ObjBanana, 1))},
		CustomTbRum:           {st: TbwtState{Phase: TbPhaseStarted, Lubufu: 6, Tiadeche: 4, Tamayu: 4, Tinsay: 2}, inv: tbWith(kit, inv(TbObjSlicedBanana, 1))},
		CustomTbMix:           {st: TbwtState{Phase: TbPhaseStarted, Lubufu: 6, Tiadeche: 4, Tamayu: 4, Tinsay: 2}, inv: tbWith(kit, inv(TbObjSlicedBanana, 1, ObjKaramjaRum, 1))},
		CustomTbMonkey:        {st: TbwtState{Phase: TbPhaseStarted, Lubufu: 6, Tiadeche: 4, Tamayu: 4, Tinsay: 4}, inv: kit},
		CustomTbSkin:          {st: TbwtState{Phase: TbPhaseStarted, Lubufu: 6, Tiadeche: 4, Tamayu: 4, Tinsay: 4}, inv: tbWith(kit, inv(TbObjMonkeyCorpse, 1))},
		CustomTbSandwich:      {st: TbwtState{Phase: TbPhaseStarted, Lubufu: 6, Tiadeche: 4, Tamayu: 4, Tinsay: 4}, inv: tbWith(kit, inv(TbObjMonkeySkin, 1))},
		CustomTbJogre:         {st: TbwtState{Phase: TbPhaseStarted, Lubufu: 6, Tiadeche: 4, Tamayu: 4, Tinsay: 6}, inv: kit},
		CustomTbBurn:          {st: TbwtState{Phase: TbPhaseStarted, Lubufu: 6, Tiadeche: 4, Tamayu: 4, Tinsay: 6}, inv: tbWith(kit, inv(TbObjJogreBones, 1))},
		CustomTbPasteBones:    {st: TbwtState{Phase: TbPhaseStarted, Lubufu: 6, Tiadeche: 4, Tamayu: 4, Tinsay: 6}, inv: tbWith(kit, inv(TbObjBurntJogreBones, 1, TbObjKarambwanjiPaste, 1))},
		CustomTbTinsayMeet:    {st: TbwtState{Phase: TbPhaseStarted, Lubufu: 6, Tiadeche: 4, Tamayu: 4, Tinsay: 0}, inv: kit},
		CustomTbTinsayGive:    {st: TbwtState{Phase: TbPhaseStarted, Lubufu: 6, Tiadeche: 4, Tamayu: 4, Tinsay: 2}, inv: tbWith(kit, inv(TbObjRumSlicedBanana, 1))},
		CustomTbManual:        {st: TbwtState{Phase: TbPhaseStarted, Lubufu: 6, Tiadeche: 5, Tamayu: 4, Tinsay: 7}, inv: tbWith(kit, inv(TbObjManual, 1))},
	}
	for cust, w := range worlds {
		before := DecideTaiBwoTrio(w.snap())
		if before.Kind != StepCustom || before.Custom != cust {
			t.Errorf("%s: the world asks for %s %q (%s)", cust, before.Kind, before.Custom, before.Reason)
			continue
		}
		w.failed = []string{cust}
		got := DecideTaiBwoTrio(w.snap())
		if got.Kind != StepWait || got.ParkFor != TbLegPark || !strings.Contains(got.Reason, cust) {
			t.Errorf("%s failed: %s %q park %s (%s), want a half hour's wait that names it", cust, got.Kind, got.Custom, got.ParkFor, got.Reason)
		}
	}
	// The ones the worlds above do not reach: the gift and the Karambwan are the spear chain's (tested there).
}

// The supplies: the scarce lines first, a park with the reason when the bank and the mule hold none, the rest after, and nothing asked
// of a quest whose leg is behind it.
func TestDecideTaiBwoTrioSupplies(t *testing.T) {
	start := tbWorld{st: TbwtState{Phase: TbPhasePre}, tile: tbMainland}
	// An empty pack on the mainland: room is not the problem (nothing to deposit), the scarce pass comes first.
	got := DecideTaiBwoTrio(start.snap())
	if got.Kind != StepProvision || got.KitName != KitTbScarce {
		t.Fatalf("an empty pack: %s %q %q", got.Kind, got.KitName, got.Reason)
	}
	ids := map[int]bool{}
	for _, l := range got.Kit {
		ids[l.ID] = true
	}
	for _, id := range []int{3032, 3034, 1239, 1241, 1243, TbObjNatureRune} {
		if !ids[id] {
			t.Errorf("the scarce pass does not ask for obj %d", id)
		}
	}
	for _, id := range []int{TbObjNet, TbObjPestle, ObjKnife, CoinsID, ObjShark} {
		if ids[id] {
			t.Errorf("the scarce pass asks for obj %d, which is the second pass's", id)
		}
	}
	// The scarce pass done, the potions and the spear still missing: the quest parks, with what is missing, before any tool is carried.
	start.ran = []string{"provision:" + KitTbScarce}
	start.inv = inv(TbObjNatureRune, 1)
	got = DecideTaiBwoTrio(start.snap())
	if got.Kind != StepWait || got.ParkFor != TbSupplyPark || !strings.Contains(got.Reason, "agility") || !strings.Contains(got.Reason, "spear") {
		t.Errorf("scarce supplies missing after the pass: %s %q park %s (%s)", got.Kind, got.KitName, got.ParkFor, got.Reason)
	}
	if strings.Contains(got.Reason, "nature") {
		t.Errorf("the nature rune is held and the reason names it: %s", got.Reason)
	}
	// The scarce things held, the tools missing: the second pass, with the coins and the food.
	start.ran = []string{"provision:" + KitTbScarce}
	start.inv = inv(TbObjNatureRune, 1, 3032, 1, 1243, 1)
	got = DecideTaiBwoTrio(start.snap())
	if got.Kind != StepProvision || got.KitName != KitTbTools {
		t.Fatalf("the scarce things held: %s %q (%s)", got.Kind, got.KitName, got.Reason)
	}
	ids = map[int]bool{}
	for _, l := range got.Kit {
		ids[l.ID] = true
	}
	for _, id := range []int{TbObjNet, TbObjPestle, ObjKnife, ObjSeaweed, ObjFireRune, ObjAirRune, ObjChaosRune, CoinsID, ObjShark} {
		if !ids[id] {
			t.Errorf("the tools pass does not ask for obj %d", id)
		}
	}
	// Both passes made and the pack still short: a park that names what, and it is a supply park, not a retry at once.
	start.ran = []string{"provision:" + KitTbScarce, "provision:" + KitTbTools}
	got = DecideTaiBwoTrio(start.snap())
	if got.Kind != StepWait || got.ParkFor != TbSupplyPark || !strings.Contains(got.Reason, "Pestle") {
		t.Errorf("both passes made: %s (%s)", got.Kind, got.Reason)
	}
	// A pass that failed on the way counts as made.
	start.ran, start.inv = nil, nil
	start.failed = []string{"provision:" + KitTbScarce}
	got = DecideTaiBwoTrio(start.snap())
	if got.Kind != StepWait || got.ParkFor != TbSupplyPark {
		t.Errorf("a failed scarce pass: %s %q (%s)", got.Kind, got.KitName, got.Reason)
	}
	// A full kit on the mainland goes; the first step is the job.
	full := tbWorld{st: TbwtState{Phase: TbPhasePre}, tile: tbMainland, inv: tbKit()}
	got = DecideTaiBwoTrio(full.snap())
	if got.Kind != StepCustom || got.Custom != CustomTbStart {
		t.Errorf("a full kit on the mainland: %s %q (%s)", got.Kind, got.Custom, got.Reason)
	}
	// Coins under the floor are drawn before the crossing, and without a position the account is taken to be at the bank's side.
	low := tbWorld{st: TbwtState{Phase: TbPhasePre}, tile: tbMainland, inv: tbWith(tbWithout(tbKit(), CoinsID), inv(CoinsID, 40))}
	got = DecideTaiBwoTrio(low.snap())
	if got.Kind != StepProvision || got.KitName != KitTbTools {
		t.Errorf("40 coins on the mainland: %s %q (%s)", got.Kind, got.KitName, got.Reason)
	}
	// The coins are not wanted on the island for a quest that does not need them: the account at Tiadeche with 40 goes on.
	island := tbWorld{st: tbState(TbLubufuDone, TbTiadecheWaiting, 0, 0), inv: tbWith(tbWithout(tbKit(), CoinsID), inv(CoinsID, 40, TbObjVessel, 1, TbObjRawKarambwanji, 1))}
	got = DecideTaiBwoTrio(island.snap())
	if got.Kind != StepCustom || got.Custom != CustomTbBait {
		t.Errorf("40 coins on the island: %s %q (%s)", got.Kind, got.Custom, got.Reason)
	}
	// A pack full of what the bank keeps frees nothing: no deposit is asked for, however little room it leaves (the watchdog would
	// wear the account down on it).
	packed := tbWorld{st: TbwtState{Phase: TbPhasePre}, tile: tbMainland, inv: tbKit(), free: 3}
	got = DecideTaiBwoTrio(packed.snap())
	if got.Kind == StepDeposit {
		t.Errorf("a deposit was asked of a pack that holds only what it keeps")
	}
	// With something to deposit, it is asked for first.
	junk := tbWorld{st: TbwtState{Phase: TbPhasePre}, tile: tbMainland, inv: tbWith(tbKit(), inv(1511, 4)), free: 3}
	got = DecideTaiBwoTrio(junk.snap())
	if got.Kind != StepDeposit {
		t.Errorf("a pack with logs in it and three slots free: %s (%s)", got.Kind, got.Reason)
	}
	// Past a leg, its supplies drop off the list: the last leg asks for none of the tools.
	last := tbWorld{st: TbwtState{Phase: TbPhaseStarted, Lubufu: TbLubufuDone, Tiadeche: TbTiadecheManual, Tamayu: 4, Tinsay: 7}, tile: tbMainland, inv: inv(CoinsID, 300, ObjShark, 6)}
	got = DecideTaiBwoTrio(last.snap())
	if got.Kind != StepCustom {
		t.Errorf("the last leg from the mainland with coins and food: %s %q (%s)", got.Kind, got.KitName, got.Reason)
	}
}

// The needs follow the legs: each drops off the list when its leg is behind the account.
func TestTbwtNeedsFollowTheLegs(t *testing.T) {
	names := func(s Snapshot, st TbwtState) map[int]bool {
		out := map[int]bool{}
		for _, n := range tbwtNeeds(s, st) {
			out[n.Line.ID] = true
		}
		return out
	}
	empty := tbWorld{st: TbwtState{Phase: TbPhaseStarted}, inv: map[int]int{}}
	got := names(empty.snap(), empty.st)
	for _, id := range []int{TbObjNet, TbObjPestle, ObjKnife, ObjSeaweed, ObjFireRune, ObjAirRune, ObjChaosRune, TbObjNatureRune, 3032, 1239} {
		if !got[id] {
			t.Errorf("a quest at its start does not need obj %d", id)
		}
	}
	// Tinsay done: nothing of the tinsay's tools remain; Tamayu done: no bottles and no spear.
	done := TbwtState{Phase: TbPhaseStarted, Lubufu: TbLubufuDone, Tiadeche: TbTiadecheManual, Tamayu: TbTamayuComplete, Tinsay: TbTinsayComplete, Agility: true, Spear: true}
	if got := names(tbWorld{st: done, inv: map[int]int{}}.snap(), done); len(got) != 0 {
		t.Errorf("the last leg needs %v", got)
	}
	// The sandwich made: no seaweed and no cast runes; the bones burnt: no Superheat runes.
	mid := TbwtState{Phase: TbPhaseStarted, Lubufu: TbLubufuDone, Tiadeche: TbTiadecheManual, Tamayu: TbTamayuComplete, Tinsay: TbTinsayBones, Agility: true, Spear: true}
	got = names(tbWorld{st: mid, inv: inv(TbObjBurntJogreBones, 1, TbObjKarambwanjiPaste, 1)}.snap(), mid)
	for _, id := range []int{ObjSeaweed, ObjAirRune, ObjChaosRune, ObjFireRune, TbObjNatureRune, TbObjPestle, ObjKnife, 3032, 1239} {
		if got[id] {
			t.Errorf("the marinade's last steps need obj %d", id)
		}
	}
	// Tamayu's bottles are partly poured: the doses held count, and four of them end the need.
	watched := TbwtState{Phase: TbPhaseStarted, Lubufu: TbLubufuDone, Tiadeche: TbTiadecheManual, Tamayu: TbTamayuWatched, Tinsay: TbTinsayComplete}
	if got := names(tbWorld{st: watched, inv: inv(3034, 1, 3038, 1)}.snap(), watched); got[3032] || got[3034] {
		t.Errorf("four doses held (3 and 1) and the bottles are still wanted: %v", got)
	}
	if got := names(tbWorld{st: watched, inv: inv(3034, 1)}.snap(), watched); !got[3032] {
		t.Errorf("three doses held and the bottles are not wanted")
	}
}

// The stats and the models: a level the module needs, a Jogre the account cannot beat and a Shaikahan visit it cannot survive each park
// the quest, with a reason, instead of sending the account anywhere.
func TestDecideTaiBwoTrioGatesAndPrices(t *testing.T) {
	kit := tbKit()
	at := tbState(TbLubufuNone, 0, 0, 0)
	for _, c := range []struct {
		name   string
		levels map[string]int
		want   string
	}{
		{"Magic 40", map[string]int{"magic": 40}, "magic 43"},
		{"Cooking 29", map[string]int{"cooking": 29}, "cooking 30"},
		{"Fishing 4", map[string]int{"fishing": 4}, "fishing 5"},
		{"Agility 14", map[string]int{"agility": 14}, "agility 15"},
		{"Hitpoints 30", map[string]int{"hitpoints": 30}, "hitpoints 35"},
	} {
		got := DecideTaiBwoTrio(tbWorld{st: at, inv: kit, levels: c.levels}.snap())
		if got.Kind != StepWait || got.ParkFor != TbStatsPark || !strings.Contains(got.Reason, c.want) {
			t.Errorf("%s: %s park %s (%s)", c.name, got.Kind, got.ParkFor, got.Reason)
		}
	}
	// An unread level is not judged.
	w := tbWorld{st: at, inv: kit}
	s := w.snap()
	delete(s.Levels, "magic")
	if got := DecideTaiBwoTrio(s); got.Kind != StepCustom {
		t.Errorf("an unread Magic: %s (%s)", got.Kind, got.Reason)
	}
	// The Jogre: an account of 20 hitpoints with two sharks does not go for the bones.
	weak := tbWorld{st: tbState(TbLubufuDone, TbTiadecheManual, 4, TbTinsayBones), inv: tbWith(tbWithout(kit, ObjShark), inv(ObjShark, 2)), hp: 20, mx: 20,
		levels: map[string]int{"hitpoints": 36, "attack": 30, "strength": 30, "defence": 30}}
	got := DecideTaiBwoTrio(weak.snap())
	if got.Kind != StepWait || got.ParkFor != TbOddsPark || !strings.Contains(got.Reason, "Jogre") {
		t.Errorf("a weak account at the Jogre: %s park %s (%s)", got.Kind, got.ParkFor, got.Reason)
	}
	// The same swing with the bones already burnt and the hitpoints of a sound account has no fight left to price.
	weak.hp, weak.mx = 54, 54
	weak.inv = tbWith(weak.inv, inv(TbObjBurntJogreBones, 1, TbObjKarambwanjiPaste, 1))
	if got := DecideTaiBwoTrio(weak.snap()); got.Kind != StepCustom || got.Custom != CustomTbPasteBones {
		t.Errorf("a weak account past the fight: %s %q (%s)", got.Kind, got.Custom, got.Reason)
	}
	// The beast: an account of 14 maximum hitpoints does not go to Tiadeche (the others legs are not priced by it).
	frail := tbWorld{st: tbState(TbLubufuDone, TbTiadecheWaiting, 0, 0), inv: tbWith(kit, inv(TbObjVesselLoaded, 1)), hp: 14, mx: 14, levels: map[string]int{"hitpoints": 36}}
	got = DecideTaiBwoTrio(frail.snap())
	if got.Kind != StepWait || !strings.Contains(got.Reason, "Shaikahan") {
		t.Errorf("a frail account at Tiadeche: %s (%s)", got.Kind, got.Reason)
	}
	// The odds of a sound account are fine, and the numbers are what the comments say.
	if ok, why := TbwtShaikahanOdds(tbWorld{inv: kit}.snap()); !ok {
		t.Errorf("a sound account is refused the Shaikahan's ground: %s", why)
	}
	if got := FoeShaikahan.MaxHit(); got != 3 {
		t.Errorf("the Shaikahan's maximum hit is %d, the comments say 3", got)
	}
	if odds := TbwtJogreOdds(tbWorld{inv: kit}.snap()); !odds.OK {
		t.Errorf("a sound account (53/41/50, 54 hitpoints, six sharks) is refused the Jogre: %s", odds.Why)
	}
}

// The cutscene is waited out where the account stands; a tab that is red, unknown or green is read before any stage; a stage that
// has not been read is waited for.
func TestDecideTaiBwoTrioTheOddSnapshots(t *testing.T) {
	kit := tbKit()
	hunting := tbWorld{st: tbState(TbLubufuDone, TbTiadecheManual, TbTamayuSlay, 0), inv: kit, tile: tile.Tile{X: 2520, Z: 4568}}
	if got := DecideTaiBwoTrio(hunting.snap()); got.Kind != StepWait || !strings.Contains(got.Reason, "hunt") {
		t.Errorf("in the cutscene: %s (%s)", got.Kind, got.Reason)
	}
	if got := DecideTaiBwoTrio(tbWorld{tab: StatusComplete}.snap()); got.Kind != StepDone {
		t.Errorf("a green tab: %s", got.Kind)
	}
	if got := DecideTaiBwoTrio(tbWorld{tab: StatusUnknown}.snap()); got.Kind != StepWait {
		t.Errorf("an unknown tab: %s", got.Kind)
	}
	s := tbWorld{st: tbState(TbLubufuNone, 0, 0, 0), inv: kit}.snap()
	s.StageKnown = false
	if got := DecideTaiBwoTrio(s); got.Kind != StepWait {
		t.Errorf("a stage not read: %s", got.Kind)
	}
	// A page that says the quest is over while the tab is still yellow: wait for the tab.
	done := tbWorld{st: TbwtState{Phase: TbPhaseComplete}, inv: kit}.snap()
	if got := DecideTaiBwoTrio(done); got.Kind != StepWait {
		t.Errorf("a complete page: %s", got.Kind)
	}
	// No position yet: a start is not judged away from the island.
	if got := DecideTaiBwoTrio(tbWorld{st: TbwtState{Phase: TbPhasePre}, inv: kit, noTile: true}.snap()); got.Kind != StepCustom {
		t.Errorf("no position: %s (%s)", got.Kind, got.Reason)
	}
}

// Whatever the world, the step is well formed, and a step that names a custom names one the plan declares; a random walk through
// stages and packs never panics and never asks for a step the engine would refuse.
func TestDecideTaiBwoTrioEveryStepIsWellFormed(t *testing.T) {
	customs := map[string]bool{}
	for _, c := range []string{CustomTbStart, CustomTbReport, CustomTbFish, CustomTbLubufuMeet, CustomTbLubufuGive, CustomTbLubufuAsk, CustomTbVessel, CustomTbBait,
		CustomTbTiadecheMeet, CustomTbTiadecheCatch, CustomTbTiadecheGift, CustomTbKarambwan, CustomTbTamayuMeet, CustomTbTamayuHunt, CustomTbTamayuGive,
		CustomTbCook, CustomTbGrind, CustomTbPoison, CustomTbRum, CustomTbBanana, CustomTbSlice, CustomTbMix, CustomTbMonkey, CustomTbSkin, CustomTbSandwich,
		CustomTbJogre, CustomTbBurn, CustomTbPasteBones, CustomTbTinsayMeet, CustomTbTinsayGive, CustomTbManual} {
		customs[c] = true
	}
	rng := rand.New(rand.NewSource(19))
	items := []int{CoinsID, TbObjNet, TbObjPestle, ObjKnife, ObjSeaweed, ObjFireRune, ObjAirRune, ObjChaosRune, TbObjNatureRune, 3032, 3034, 3038, 1239, 1243, 3171, 3173,
		ObjShark, ObjBanana, ObjKaramjaRum, TbObjSlicedBanana, TbObjRumSlicedBanana, TbObjVessel, TbObjVesselLoaded, TbObjManual, TbObjRawKarambwanji,
		TbObjKarambwanjiPaste, TbObjRawKarambwan, TbObjPoorKarambwan, TbObjBurntKarambwan, TbObjPoisonPaste, TbObjMonkeyCorpse, TbObjMonkeySkin, TbObjSandwich,
		TbObjJogreBones, TbObjBurntJogreBones, TbObjBurntBonesInPaste, TbObjMarinatedBones, 1511}
	for i := 0; i < 30000; i++ {
		w := tbWorld{st: TbwtState{Phase: 1 + rng.Intn(3), Lubufu: rng.Intn(7), Tiadeche: rng.Intn(6), Tinsay: rng.Intn(8), Tamayu: rng.Intn(5),
			Agility: rng.Intn(2) == 0, Spear: rng.Intn(2) == 0}, inv: map[int]int{}, free: 1 + rng.Intn(20)}
		for _, id := range items {
			if rng.Intn(3) == 0 {
				w.inv[id] = 1 + rng.Intn(5)
			}
		}
		if rng.Intn(3) == 0 {
			w.tile = tbMainland
		}
		for _, c := range []string{"provision:" + KitTbScarce, "provision:" + KitTbTools, CustomTbTiadecheGift, CustomTbTiadecheMeet} {
			switch rng.Intn(4) {
			case 0:
				w.ran = append(w.ran, c)
			case 1:
				w.failed = append(w.failed, c)
			}
		}
		if rng.Intn(5) == 0 {
			w.hp, w.mx = 5+rng.Intn(40), 10+rng.Intn(50)
			w.hp = min(w.hp, w.mx)
		}
		got := DecideTaiBwoTrio(w.snap())
		if why := ValidateStep(got); why != "" {
			t.Fatalf("%v inv %v: malformed step %+v: %s", w.st, w.inv, got, why)
		}
		if got.Kind == StepCustom && !customs[got.Custom] {
			t.Fatalf("%v: a step names the custom %q, which the plan does not declare", w.st, got.Custom)
		}
		if got.Kind == StepWait && got.ParkFor != 0 && got.ParkFor < time.Minute {
			t.Fatalf("%v: a park of %s", w.st, got.ParkFor)
		}
	}
}

// The keep list holds every item the legs make or use, and no tool is a food; the module's tools protect the quest's items from the
// eat loop and the bank visits.
func TestTbKeepIDsHoldEveryItemTheLegsMakeAndUse(t *testing.T) {
	keep := map[int]bool{}
	for _, id := range TbKeepIDs() {
		keep[id] = true
	}
	tools := map[int]bool{}
	for _, id := range TbToolIDs() {
		tools[id] = true
	}
	for _, id := range []int{CoinsID, TbObjNet, TbObjPestle, ObjKnife, ObjSeaweed, ObjFireRune, ObjAirRune, ObjChaosRune, TbObjNatureRune, ObjBanana, ObjKaramjaRum,
		TbObjSlicedBanana, TbObjRumSlicedBanana, TbObjVessel, TbObjVesselLoaded, TbObjManual, TbObjRawKarambwanji, TbObjKarambwanjiPaste, TbObjRawKarambwan,
		TbObjPoorKarambwan, TbObjBurntKarambwan, TbObjPoisonPaste, TbObjMonkeyCorpse, TbObjMonkeySkin, TbObjSandwich, TbObjJogreBones, TbObjBurntJogreBones,
		TbObjBurntBonesInPaste, TbObjMarinatedBones, 3032, 3034, 3036, 3038, 1239, 1247, 3171, 3175} {
		if !keep[id] || !tools[id] {
			t.Errorf("obj %d is not in the keep list (%v) and the tools (%v)", id, keep[id], tools[id])
		}
	}
	for _, id := range tbwtFoods {
		if !keep[id] {
			t.Errorf("food %d is not kept through a bank visit", id)
		}
		if tools[id] {
			t.Errorf("food %d is a tool: the eat loop would never bite it", id)
		}
	}
	if len(TaiBwoTrio.ToolIDs) != len(TbToolIDs()) {
		t.Errorf("the module's tools differ from TbToolIDs")
	}
}

// The module's own settings: a death parks it for a day, the account never fights back, the journal parser is the plan's.
func TestTaiBwoTrioModule(t *testing.T) {
	m := TaiBwoTrio
	if m.MaxDeaths != 1 || !m.NoRetaliate || m.ReadStage == nil || m.Decide == nil || m.Record.ID != TaiBwoID || m.Record.QuestPoints != 2 {
		t.Errorf("the module: %+v", m)
	}
	if got := m.Sustain; got.EatAt <= 0 || len(got.FoodIDs) == 0 {
		t.Errorf("the sustain: %+v", got)
	}
	want := map[string]int{"cooking": 30, "agility": 15, "fishing": 5, "magic": TbMinMagic, "hitpoints": TbMinHitpoints}
	for _, r := range m.Record.Requirements.Skills {
		if want[r.Skill] != r.Level {
			t.Errorf("the record asks %s %d, want %d", r.Skill, r.Level, want[r.Skill])
		}
		delete(want, r.Skill)
	}
	if len(want) != 0 || len(m.Record.Requirements.Quests) != 1 || m.Record.Requirements.Quests[0] != JunglePotionID {
		t.Errorf("the record's requirements: %+v (unseen %v)", m.Record.Requirements, want)
	}
}
