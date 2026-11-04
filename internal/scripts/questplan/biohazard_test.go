package questplan

import (
	"math/rand"
	"slices"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Biohazard's decision, over simulated worlds: the quest tab's colour, the journal stage the parser reads, where the account stands
// (the mainland, Varrock's east quarter, West Ardougne, the headquarters' two floors, the sewer) and what the pack, the worn items and
// the bank hold. Every step it returns is well formed, never a walk across the wall or through Varrock's gate, and the quest is
// restartable from any state.

// bioWorld is one simulated snapshot.
type bioWorld struct {
	stage  int // the journal's reading; -1 not started, -2 complete, -3 unknown tab
	at     tile.Tile
	inv    map[int]int
	worn   map[int]int
	bank   map[int]int
	ran    []string
	failed []string
	hp, mx int
	levels map[string]int
	free   int
	noRoom bool // no free slot at all
}

// Representative tiles.
var (
	bioEastTile    = tile.Tile{X: 2616, Z: 3332} // the north bank
	bioWestTile    = tile.Tile{X: 2556, Z: 3267} // the ladder's landing
	bioYardTile    = tile.Tile{X: 2543, Z: 3331} // the headquarters' yard
	bioHQTile      = tile.Tile{X: 2551, Z: 3321} // just inside the street door
	bioHQUpTile    = tile.Tile{X: 2550, Z: 3326, Level: 1}
	bioQuarterTile = tile.Tile{X: 3270, Z: 3390} // the Dancing Donkey
	bioSewerTile   = tile.Tile{X: 2562, Z: 9737}
	bioVarrockTile = tile.Tile{X: 3204, Z: 3417} // Thessalia
)

func (w bioWorld) snap() Snapshot {
	at := w.at
	if at == (tile.Tile{}) {
		at = bioEastTile
	}
	s := Snapshot{TileKnown: true, Tile: at, InvIDs: map[int]int{}, WornIDs: map[int]int{}, FreeSlots: 20,
		Ran: map[string]bool{}, Failed: map[string]bool{}, Blocked: map[string]bool{}, HP: 40, MaxHP: 40,
		Levels: map[string]int{"attack": 40, "strength": 40, "defence": 40, "hitpoints": 40, "prayer": 10, "magic": 20, "ranged": 1}}
	for id, n := range w.inv {
		s.InvIDs[id] = n
	}
	for id, n := range w.worn {
		s.WornIDs[id] = n
	}
	if w.bank != nil {
		s.BankKnown, s.BankIDs = true, w.bank
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
	if w.levels != nil {
		s.Levels = w.levels
	}
	if w.free != 0 {
		s.FreeSlots = w.free
	}
	if w.noRoom {
		s.FreeSlots = 0
	}
	switch w.stage {
	case -1:
		s.Journal = StatusNotStarted
	case -2:
		s.Journal = StatusComplete
	case -3:
		s.Journal = StatusUnknown
	default:
		s.Journal, s.Stage, s.StageKnown = StatusInProgress, w.stage, true
	}
	return s
}

func TestParseBiohazardJournalNeedlesAreUnambiguous(t *testing.T) {
	for _, c := range []struct {
		text string
		want int
	}{
		{"@dbl@I can start this quest by speaking to @dre@Elena @dbl@who is in @dre@East|Ardougne", BioNotStarted},
		{"@str@I've spoken to Elena, the Mourners stole her distillator|||@dbl@I need to talk to @dre@Jerico @dbl@about getting over the wall and|into @dre@West Ardougne", BioStarted},
		{"@str@I've spoken to Elena, the Mourners stole her distillator||@str@I've spoken to Jerico about getting into West Ardougne||@dre@Omart @dbl@will be able to get me over the wall|@dbl@He's waiting at the @dre@South @dbl@end of the wall", BioSpokenJerico},
		{"@str@I've spoken to Jerico about getting into West Ardougne||@dbl@I've chucked some birdfeed onto the Watch Tower", BioUsedBirdfeed},
		{"@str@I've spoken to Jerico about getting into West Ardougne||@dbl@The Watch Tower is now surrounded by flapping pigeons!|@dbl@Maybe I can sneak over the wall while the mourners are|distracted", BioReleasedPigeons},
		{"@str@I've crossed the wall into West Ardougne|@str@Omart and Kilron will stay to help me out again||@dbl@Somewhere in this city is @dre@Elena's distillator @dbl@- I must find|it and return it to her|", BioClimbedLadder},
		{"@str@I've crossed the wall into West Ardougne||@dbl@I have rather unkindly poisoned the mourners' stew by|putting rotten apples into it!|", BioPoisonedStew},
		{"@str@I've crossed the wall into West Ardougne|@str@I managed to find Elena's distillator|@dbl@Now I can return the @dre@distillator @dbl@to @dre@Elena", BioFoundDistillator},
		{"@str@I've found Elena's Distillator and returned it to her||@dbl@Elena's asked me to take the following items to @dre@Guidor @dbl@who|lives in @dre@Varrock", BioGivenDistillator},
		{"@str@Elena gave me some chemicals to take to Guidor||@dbl@The Varrock guards are out looking for someone carrying|suspicious materials. I must smuggle the items to @dre@Guidor|@dbl@secretly", BioSpokenChemist},
		{"@str@I've given all the items to Guidor||@dre@Guidor's @dbl@findings were very interesting|@dbl@Apparently there is no @dre@Plague@dbl@!|@dbl@I should see what Elena thinks of this", BioFoundSecret},
		{"@str@I've told Elena Guidor's findings, she was horrified!||@dbl@I need to confront the @dre@King of East Ardougne", BioReportedElena},
		{"@str@I've spoken to King Lathas, he admits the Plague was fake!||@red@QUEST COMPLETE!", BioComplete},
	} {
		if got, ok := ParseBiohazardJournal([]string{c.text}); !ok || got != c.want {
			t.Errorf("%q parsed %d (ok %v), want %d", c.text, got, ok, c.want)
		}
	}
	for _, empty := range [][]string{nil, {""}, {"  "}, {"@dbl@Nothing to do with it"}} {
		if got, ok := ParseBiohazardJournal(empty); ok {
			t.Errorf("%q parsed as stage %d: an empty or foreign journal is unknown, never stage 0", empty, got)
		}
	}
	// The same text across two lines of the client's journal reads the same.
	if got, ok := ParseBiohazardJournal([]string{"@dbl@I've chucked some birdfeed onto the", "Watch Tower"}); !ok || got != BioUsedBirdfeed {
		t.Errorf("a wrapped line parsed %d %v", got, ok)
	}
}

func TestBioAreasAreTheirBoxesAndTheMainlandIsTheRest(t *testing.T) {
	for _, c := range []struct {
		at   tile.Tile
		want BioArea
	}{
		{tile.Tile{X: 2616, Z: 3332}, BioEast},             // the north bank
		{tile.Tile{X: 2592, Z: 3336}, BioEast},             // Elena
		{tile.Tile{X: 2612, Z: 3324}, BioEast},             // Jerico
		{tile.Tile{X: 2559, Z: 3267}, BioEast},             // the east landing
		{tile.Tile{X: 2578, Z: 3293, Level: 1}, BioEast},   // King Lathas, upstairs in the castle
		{tile.Tile{X: 2556, Z: 3267}, BioWest},             // the west landing
		{tile.Tile{X: 2535, Z: 3333}, BioWest},             // the rotten apples
		{tile.Tile{X: 2518, Z: 3275}, BioWest},             // the nurse's hut
		{tile.Tile{X: 2542, Z: 3332}, BioWest},             // the yard beside the cauldron
		{tile.Tile{X: 2551, Z: 3320}, BioWest},             // the street door's own tile
		{tile.Tile{X: 2551, Z: 3321}, BioHQ},               // just inside it
		{tile.Tile{X: 2543, Z: 3325}, BioHQ},               // the room with the stairs
		{tile.Tile{X: 2550, Z: 3326, Level: 1}, BioHQUp},   // the sick mourners' room
		{tile.Tile{X: 2553, Z: 3326, Level: 1}, BioHQUp},   // behind the gate
		{tile.Tile{X: 2543, Z: 3326, Level: 1}, BioHQUp},   // the stairs' top
		{tile.Tile{X: 2551, Z: 3328}, BioWest},             // the back door's yard side
		{tile.Tile{X: 2562, Z: 9737}, BioPocket},           // the sewer
		{tile.Tile{X: 2530, Z: 3331, Level: 1}, BioPocket}, // the Rehnisons' upper floor
		{tile.Tile{X: 2537, Z: 9670}, BioPocket},           // the plague house's cellar
		{tile.Tile{X: 3270, Z: 3390}, BioQuarter},          // the Dancing Donkey
		{tile.Tile{X: 3264, Z: 3406}, BioQuarter},          // the gate's own tile
		{tile.Tile{X: 3285, Z: 3381}, BioQuarter},          // Guidor
		{tile.Tile{X: 3263, Z: 3406}, BioEast},             // outside the gate
		{tile.Tile{X: 3253, Z: 3420}, BioEast},             // the east bank
		{tile.Tile{X: 2934, Z: 3210}, BioEast},             // the Chemist
		{tile.Tile{X: 3270, Z: 3390, Level: 1}, BioEast},   // no floor above the inn is the quarter's
		{tile.Tile{X: 2551, Z: 3321, Level: 2}, BioEast},   // no third floor
		{tile.Tile{X: 2541, Z: 3321}, BioWest},             // west of the building
		{tile.Tile{X: 2556, Z: 3321}, BioWest},             // east of it
	} {
		if got := BioAreaOf(c.at); got != c.want {
			t.Errorf("%v is %v, want %v", c.at, got, c.want)
		}
	}
	for _, a := range []BioArea{BioEast, BioQuarter, BioWest, BioHQ, BioHQUp, BioPocket} {
		if a.String() == "" {
			t.Errorf("area %d has no name", a)
		}
	}
	// The headquarters is a pocket of its own on both floors, and nothing outside its box is.
	for _, c := range []struct {
		at   tile.Tile
		want bool
	}{
		{tile.Tile{X: 2551, Z: 3321}, true}, {tile.Tile{X: 2542, Z: 3324}, true}, {tile.Tile{X: 2555, Z: 3327, Level: 1}, true},
		{tile.Tile{X: 2551, Z: 3320}, false}, {tile.Tile{X: 2551, Z: 3328}, false}, {tile.Tile{X: 2541, Z: 3324}, false}, {tile.Tile{X: 2556, Z: 3324}, false},
	} {
		if InBioHQ(c.at) != c.want {
			t.Errorf("InBioHQ(%v) = %v", c.at, !c.want)
		}
	}
}

func TestBiohazardHoldsOffUntilTheTabAndTheJournalAreRead(t *testing.T) {
	for _, c := range []struct {
		name string
		s    Snapshot
		want StepKind
	}{
		{"an uncoloured tab", bioWorld{stage: -3}.snap(), StepWait},
		{"a complete quest", bioWorld{stage: -2}.snap(), StepDone},
		{"in progress with the journal unread", func() Snapshot { s := bioWorld{stage: BioStarted}.snap(); s.StageKnown = false; return s }(), StepWait},
		{"no position yet", func() Snapshot { s := bioWorld{stage: -1}.snap(); s.TileKnown = false; return s }(), StepWait},
		{"a stage past the end", bioWorld{stage: BioComplete}.snap(), StepDone},
		{"a stage the quest does not use", bioWorld{stage: 8}.snap(), StepWait},
	} {
		st := DecideBiohazard(c.s)
		if st.Kind != c.want {
			t.Errorf("%s: %s %q", c.name, st.Kind, st.Describe())
		}
		if why := ValidateStep(st); why != "" {
			t.Errorf("%s: %s", c.name, why)
		}
	}
}

var (
	bioFullSuit = inv(BioObjPriestGown, 1, BioObjPriestRobe, 1)
	bioAllVials = inv(BioObjBroline, 1, BioObjEthenea, 1, BioObjLiquidHoney, 1)
)

// bioMerge combines item maps.
func bioMerge(ms ...map[int]int) map[int]int {
	out := map[int]int{}
	for _, m := range ms {
		for id, n := range m {
			out[id] += n
		}
	}
	return out
}

func TestDecideBiohazardOnTheMainland(t *testing.T) {
	stash1, stash2, stash3 := "provision:"+KitBioStashEarly, "provision:"+KitBioStashHQ, "provision:"+KitBioStashVarrock
	food, coins, suit := "provision:"+KitBioFood, "provision:"+KitBioCoins, "provision:"+KitBioSuit
	weak := map[string]int{"attack": 5, "strength": 5, "defence": 5, "hitpoints": 10}
	for _, c := range []struct {
		name string
		w    bioWorld
		want string
	}{
		{"not started", bioWorld{stage: -1}, CustomBioStart},
		{"stage 0 on the yellow tab", bioWorld{stage: BioNotStarted}, CustomBioStart},
		{"stage 1", bioWorld{stage: BioStarted}, CustomBioJerico},
		{"stage 1: a failed talk waits", bioWorld{stage: BioStarted, failed: []string{CustomBioJerico}}, "wait"},
		{"stage 2: the bank first", bioWorld{stage: BioSpokenJerico}, stash1},
		{"stage 2: then the cupboard", bioWorld{stage: BioSpokenJerico, ran: []string{stash1}}, CustomBioBirdfeed},
		{"stage 2: a full pack makes room first", bioWorld{stage: BioSpokenJerico, ran: []string{stash1}, free: 2}, "deposit"},
		{"stage 2: a failed cupboard waits", bioWorld{stage: BioSpokenJerico, ran: []string{stash1}, failed: []string{CustomBioBirdfeed}}, "wait"},
		{"stage 2 with the bag", bioWorld{stage: BioSpokenJerico, inv: inv(BioObjBirdfeed, 1)}, CustomBioFeed},
		{"stage 3: the bank first", bioWorld{stage: BioUsedBirdfeed}, stash1},
		{"stage 3: then the spawn", bioWorld{stage: BioUsedBirdfeed, ran: []string{stash1}}, CustomBioPigeons},
		{"stage 3 with the cage", bioWorld{stage: BioUsedBirdfeed, inv: inv(BioObjPigeons, 1)}, CustomBioRelease},
		{"stage 3 with a failed release waits", bioWorld{stage: BioUsedBirdfeed, inv: inv(BioObjPigeons, 1), failed: []string{CustomBioRelease}}, "wait"},
		{"stage 4: across the wall", bioWorld{stage: BioReleasedPigeons}, CustomBioCross},
		{"stage 4 with a full pack", bioWorld{stage: BioReleasedPigeons, free: 3}, "deposit"},
		{"stage 4, a weak account: food from the bank and the mule", bioWorld{stage: BioReleasedPigeons, levels: weak, hp: 10, mx: 10}, food},
		{"stage 4, a weak account after the food: the quest is left alone", bioWorld{stage: BioReleasedPigeons, levels: weak, hp: 10, mx: 10, ran: []string{food}}, "wait"},
		{"stage 4, a weak account with the food kit failed: left alone", bioWorld{stage: BioReleasedPigeons, levels: weak, hp: 10, mx: 10, failed: []string{food}}, "wait"},
		{"stage 5: the bank first", bioWorld{stage: BioClimbedLadder}, stash2},
		{"stage 5: then across", bioWorld{stage: BioClimbedLadder, ran: []string{stash2}}, CustomBioCross},
		{"stage 6, key and gown in hand: across, no price to pay", bioWorld{stage: BioPoisonedStew, ran: []string{stash2}, inv: inv(BioObjKey, 1, BioObjDoctorGown, 1), levels: weak, hp: 10, mx: 10}, CustomBioCross},
		{"stage 7 with the distillator: Elena", bioWorld{stage: BioFoundDistillator, ran: []string{stash2}, inv: inv(BioObjDistillator, 1)}, CustomBioHandBack},
		{"stage 7 with the distillator and no room: the bank first", bioWorld{stage: BioFoundDistillator, ran: []string{stash2}, inv: inv(BioObjDistillator, 1), free: 4}, "deposit"},
		{"stage 7 with the distillator in the bank", bioWorld{stage: BioFoundDistillator}, stash2},
		{"stage 7 without it, key in hand: back across", bioWorld{stage: BioFoundDistillator, ran: []string{stash2}, inv: inv(BioObjKey, 1)}, CustomBioCross},
		{"stage 7 without it or the key, a sound account: across", bioWorld{stage: BioFoundDistillator, ran: []string{stash2}}, CustomBioCross},
		{"stage 10: a hand-back that came short", bioWorld{stage: BioGivenDistillator, ran: []string{stash3}, inv: inv(BioObjSample, 1)}, CustomBioReplace},
		{"stage 10: no sample", bioWorld{stage: BioGivenDistillator, ran: []string{stash3}, inv: bioAllVials}, CustomBioReplace},
		{"stage 10: the bank first", bioWorld{stage: BioGivenDistillator, inv: bioMerge(bioAllVials, inv(BioObjSample, 1))}, stash3},
		{"stage 10: coins first", bioWorld{stage: BioGivenDistillator, ran: []string{stash3}, inv: bioMerge(bioAllVials, inv(BioObjSample, 1))}, coins},
		{"stage 10: then the Chemist", bioWorld{stage: BioGivenDistillator, ran: []string{stash3}, inv: bioMerge(bioAllVials, inv(BioObjSample, 1, CoinsID, 300))}, CustomBioChemist},
		{"stage 10 with the coins kit failed: left alone", bioWorld{stage: BioGivenDistillator, ran: []string{stash3}, failed: []string{coins}, inv: bioMerge(bioAllVials, inv(BioObjSample, 1))}, "wait"},
		{"stage 12: the paper lost", bioWorld{stage: BioSpokenChemist, ran: []string{stash3}, inv: inv(CoinsID, 300, BioObjSample, 1)}, CustomBioChemist},
		{"stage 12: the sample lost", bioWorld{stage: BioSpokenChemist, ran: []string{stash3}, inv: inv(CoinsID, 300, BioObjTouchPaper, 1)}, CustomBioReplace},
		{"stage 12: the vials to the boys", bioWorld{stage: BioSpokenChemist, ran: []string{stash3}, inv: bioMerge(bioAllVials, inv(CoinsID, 300, BioObjSample, 1, BioObjTouchPaper, 1))}, CustomBioErrands},
		{"stage 12: one vial left", bioWorld{stage: BioSpokenChemist, ran: []string{stash3}, inv: inv(CoinsID, 300, BioObjSample, 1, BioObjTouchPaper, 1, BioObjEthenea, 1)}, CustomBioErrands},
		{"stage 12: the boys refused (a vial is still held): on to the suit", bioWorld{stage: BioSpokenChemist, ran: []string{stash3}, failed: []string{CustomBioErrands},
			inv: bioMerge(bioAllVials, inv(CoinsID, 300, BioObjSample, 1, BioObjTouchPaper, 1))}, suit},
		{"stage 12: no suit", bioWorld{stage: BioSpokenChemist, ran: []string{stash3}, inv: inv(CoinsID, 300, BioObjSample, 1, BioObjTouchPaper, 1)}, suit},
		{"stage 12: half a suit", bioWorld{stage: BioSpokenChemist, ran: []string{stash3}, inv: inv(CoinsID, 300, BioObjSample, 1, BioObjTouchPaper, 1, BioObjPriestGown, 1)}, suit},
		{"stage 12: the suit failed", bioWorld{stage: BioSpokenChemist, ran: []string{stash3}, failed: []string{suit}, inv: inv(CoinsID, 300, BioObjSample, 1, BioObjTouchPaper, 1)}, "wait"},
		{"stage 12: the suit worn counts", bioWorld{stage: BioSpokenChemist, ran: []string{stash3}, inv: inv(CoinsID, 300, BioObjSample, 1, BioObjTouchPaper, 1),
			worn: bioFullSuit}, CustomBioGateIn},
		{"stage 12: ready for the gate", bioWorld{stage: BioSpokenChemist, ran: []string{stash3}, inv: bioMerge(bioFullSuit, inv(CoinsID, 300, BioObjSample, 1, BioObjTouchPaper, 1))}, CustomBioGateIn},
		{"stage 12: a boy gave nothing back: Elena first", bioWorld{stage: BioSpokenChemist, ran: []string{stash3}, failed: []string{CustomBioCollect},
			inv: bioMerge(bioFullSuit, inv(CoinsID, 300, BioObjSample, 1, BioObjTouchPaper, 1))}, CustomBioReplace},
		{"stage 12: Elena has replaced them: the gate again", bioWorld{stage: BioSpokenChemist, ran: []string{stash3, CustomBioReplace}, failed: []string{CustomBioCollect},
			inv: bioMerge(bioFullSuit, inv(CoinsID, 300, BioObjSample, 1, BioObjTouchPaper, 1))}, CustomBioGateIn},
		{"stage 12, in Varrock: the suit is Thessalia's", bioWorld{stage: BioSpokenChemist, at: bioVarrockTile, ran: []string{stash3}, inv: inv(CoinsID, 300, BioObjSample, 1, BioObjTouchPaper, 1)}, suit},
		{"stage 14: Elena", bioWorld{stage: BioFoundSecret, inv: inv(CoinsID, 300)}, CustomBioReport},
		{"stage 14: coins for the ships home", bioWorld{stage: BioFoundSecret}, coins},
		{"stage 15: King Lathas", bioWorld{stage: BioReportedElena}, CustomBioLathas},
	} {
		s := c.w.snap()
		st := DecideBiohazard(s)
		if got := stepName(st); got != c.want {
			t.Errorf("%s: %s (%s), want %s", c.name, got, st.Describe(), c.want)
		}
		if why := ValidateStep(st); why != "" {
			t.Errorf("%s: %s", c.name, why)
		}
	}
}

func TestDecideBiohazardWestOfTheWall(t *testing.T) {
	food := "provision:" + KitBioFood
	weak := map[string]int{"attack": 5, "strength": 5, "defence": 5, "hitpoints": 10}
	for _, c := range []struct {
		name string
		w    bioWorld
		want string
	}{
		{"stage 5: the apples", bioWorld{stage: BioClimbedLadder, at: bioWestTile}, CustomBioApples},
		{"stage 5: the apples in hand", bioWorld{stage: BioClimbedLadder, at: bioWestTile, inv: inv(BioObjRottenApples, 1)}, CustomBioStew},
		{"stage 5, in the yard with the apples", bioWorld{stage: BioClimbedLadder, at: bioYardTile, inv: inv(BioObjRottenApples, 1)}, CustomBioStew},
		{"stage 5: a failed stew waits", bioWorld{stage: BioClimbedLadder, at: bioYardTile, inv: inv(BioObjRottenApples, 1), failed: []string{CustomBioStew}}, "wait"},
		{"stage 6: the gown", bioWorld{stage: BioPoisonedStew, at: bioYardTile}, CustomBioGown},
		{"stage 6: a failed cupboard waits, it does not cross the wall again", bioWorld{stage: BioPoisonedStew, at: bioWestTile, failed: []string{CustomBioGown}}, "wait"},
		{"stage 6: the gown in hand, in", bioWorld{stage: BioPoisonedStew, at: bioWestTile, inv: inv(BioObjDoctorGown, 1)}, CustomBioHQ},
		{"stage 6: the gown worn, in", bioWorld{stage: BioPoisonedStew, at: bioWestTile, worn: inv(BioObjDoctorGown, 1)}, CustomBioHQ},
		{"stage 6: a weak account: home for food", bioWorld{stage: BioPoisonedStew, at: bioWestTile, inv: inv(BioObjDoctorGown, 1), levels: weak, hp: 10, mx: 10}, CustomBioHome},
		{"stage 6: a weak account, the food already tried: left alone", bioWorld{stage: BioPoisonedStew, at: bioWestTile, inv: inv(BioObjDoctorGown, 1), levels: weak, hp: 10, mx: 10,
			ran: []string{food}}, "wait"},
		{"stage 6: a weak account with the key already: in", bioWorld{stage: BioPoisonedStew, at: bioWestTile, inv: inv(BioObjDoctorGown, 1, BioObjKey, 1), levels: weak, hp: 10, mx: 10}, CustomBioHQ},
		{"stage 6: no room for the key", bioWorld{stage: BioPoisonedStew, at: bioWestTile, inv: inv(BioObjDoctorGown, 1), noRoom: true}, CustomBioHome},
		{"stage 7 with the distillator: back over the wall", bioWorld{stage: BioFoundDistillator, at: bioWestTile, inv: inv(BioObjDistillator, 1)}, CustomBioHome},
		{"stage 7 without it: the crate again", bioWorld{stage: BioFoundDistillator, at: bioWestTile, inv: inv(BioObjDoctorGown, 1, BioObjKey, 1)}, CustomBioHQ},
		{"stage 4 in the city (a stranded Plague City account): home", bioWorld{stage: BioReleasedPigeons, at: bioWestTile}, CustomBioHome},
		{"stage 10 in the city: home", bioWorld{stage: BioGivenDistillator, at: bioWestTile}, CustomBioHome},
		{"stage 0 in the city: home", bioWorld{stage: -1, at: bioWestTile}, CustomBioHome},
		{"a failed way home waits", bioWorld{stage: BioGivenDistillator, at: bioWestTile, failed: []string{CustomBioHome}}, "wait"},
		{"the sewer: home", bioWorld{stage: BioClimbedLadder, at: bioSewerTile}, CustomBioHome},
		{"a plague house cellar: home", bioWorld{stage: BioPoisonedStew, at: tile.Tile{X: 2537, Z: 9670}}, CustomBioHome},
		{"the Rehnisons' upper floor: home", bioWorld{stage: BioPoisonedStew, at: tile.Tile{X: 2530, Z: 3331, Level: 1}}, CustomBioHome},
	} {
		s := c.w.snap()
		st := DecideBiohazard(s)
		if got := stepName(st); got != c.want {
			t.Errorf("%s: %s (%s), want %s", c.name, got, st.Describe(), c.want)
		}
		if why := ValidateStep(st); why != "" {
			t.Errorf("%s: %s", c.name, why)
		}
	}
}

func TestDecideBiohazardInsideTheHeadquarters(t *testing.T) {
	weak := map[string]int{"attack": 5, "strength": 5, "defence": 5, "hitpoints": 10}
	for _, c := range []struct {
		name string
		w    bioWorld
		want string
	}{
		{"downstairs at stage 6: up", bioWorld{stage: BioPoisonedStew, at: bioHQTile, worn: inv(BioObjDoctorGown, 1)}, CustomBioUp},
		{"downstairs with the key: up to the gate", bioWorld{stage: BioPoisonedStew, at: bioHQTile, worn: inv(BioObjDoctorGown, 1), inv: inv(BioObjKey, 1)}, CustomBioUp},
		{"downstairs, a weak account: out", bioWorld{stage: BioPoisonedStew, at: bioHQTile, worn: inv(BioObjDoctorGown, 1), levels: weak, hp: 10, mx: 10}, CustomBioLeaveHQ},
		{"downstairs with the distillator: out", bioWorld{stage: BioFoundDistillator, at: bioHQTile, worn: inv(BioObjDoctorGown, 1), inv: inv(BioObjDistillator, 1)}, CustomBioLeaveHQ},
		{"downstairs at another stage: out", bioWorld{stage: BioClimbedLadder, at: bioHQTile}, CustomBioLeaveHQ},
		{"downstairs, a failed way up waits", bioWorld{stage: BioPoisonedStew, at: bioHQTile, failed: []string{CustomBioUp}}, "wait"},
		{"a failed way out waits", bioWorld{stage: BioClimbedLadder, at: bioHQTile, failed: []string{CustomBioLeaveHQ}}, "wait"},
		{"upstairs: the mourner", bioWorld{stage: BioPoisonedStew, at: bioHQUpTile, worn: inv(BioObjDoctorGown, 1)}, CustomBioKey},
		{"upstairs with the key: the crate", bioWorld{stage: BioPoisonedStew, at: bioHQUpTile, worn: inv(BioObjDoctorGown, 1), inv: inv(BioObjKey, 1)}, CustomBioCrate},
		{"upstairs with the key at stage 7: the crate again", bioWorld{stage: BioFoundDistillator, at: bioHQUpTile, inv: inv(BioObjKey, 1)}, CustomBioCrate},
		{"upstairs with the distillator: out", bioWorld{stage: BioFoundDistillator, at: bioHQUpTile, inv: inv(BioObjKey, 1, BioObjDistillator, 1)}, CustomBioLeaveHQ},
		{"upstairs, a weak account: out", bioWorld{stage: BioPoisonedStew, at: bioHQUpTile, worn: inv(BioObjDoctorGown, 1), levels: weak, hp: 10, mx: 10}, CustomBioLeaveHQ},
		{"upstairs with no room for the key: out", bioWorld{stage: BioPoisonedStew, at: bioHQUpTile, worn: inv(BioObjDoctorGown, 1), noRoom: true}, CustomBioLeaveHQ},
		{"upstairs, a failed fight waits", bioWorld{stage: BioPoisonedStew, at: bioHQUpTile, worn: inv(BioObjDoctorGown, 1), failed: []string{CustomBioKey}}, "wait"},
		{"upstairs, a failed crate waits", bioWorld{stage: BioPoisonedStew, at: bioHQUpTile, inv: inv(BioObjKey, 1), failed: []string{CustomBioCrate}}, "wait"},
		{"upstairs at another stage: out", bioWorld{stage: BioGivenDistillator, at: bioHQUpTile}, CustomBioLeaveHQ},
	} {
		s := c.w.snap()
		st := DecideBiohazard(s)
		if got := stepName(st); got != c.want {
			t.Errorf("%s: %s (%s), want %s", c.name, got, st.Describe(), c.want)
		}
		if why := ValidateStep(st); why != "" {
			t.Errorf("%s: %s", c.name, why)
		}
	}
}

func TestDecideBiohazardInVarrocksEastQuarter(t *testing.T) {
	ready := bioMerge(bioFullSuit, bioAllVials, inv(BioObjSample, 1, BioObjTouchPaper, 1))
	for _, c := range []struct {
		name string
		w    bioWorld
		want string
	}{
		{"everything in hand: Guidor", bioWorld{stage: BioSpokenChemist, at: bioQuarterTile, inv: ready}, CustomBioGuidor},
		{"the vials with the boys: collect", bioWorld{stage: BioSpokenChemist, at: bioQuarterTile, inv: bioMerge(bioFullSuit, inv(BioObjSample, 1, BioObjTouchPaper, 1))}, CustomBioCollect},
		{"one vial short: collect", bioWorld{stage: BioSpokenChemist, at: bioQuarterTile, inv: bioMerge(bioFullSuit, inv(BioObjSample, 1, BioObjTouchPaper, 1, BioObjEthenea, 1, BioObjBroline, 1))}, CustomBioCollect},
		{"a boy gave nothing: out to Elena", bioWorld{stage: BioSpokenChemist, at: bioQuarterTile, failed: []string{CustomBioCollect},
			inv: bioMerge(bioFullSuit, inv(BioObjSample, 1, BioObjTouchPaper, 1))}, CustomBioGateOut},
		{"a boy gave nothing, Elena has replaced them: ask again (the leg waits)", bioWorld{stage: BioSpokenChemist, at: bioQuarterTile, failed: []string{CustomBioCollect},
			ran: []string{CustomBioReplace}, inv: bioMerge(bioFullSuit, inv(BioObjSample, 1, BioObjTouchPaper, 1))}, "wait"},
		{"the sample lost: out", bioWorld{stage: BioSpokenChemist, at: bioQuarterTile, inv: bioMerge(bioFullSuit, bioAllVials, inv(BioObjTouchPaper, 1))}, CustomBioGateOut},
		{"the paper lost: out", bioWorld{stage: BioSpokenChemist, at: bioQuarterTile, inv: bioMerge(bioFullSuit, bioAllVials, inv(BioObjSample, 1))}, CustomBioGateOut},
		{"no suit: out", bioWorld{stage: BioSpokenChemist, at: bioQuarterTile, inv: bioMerge(bioAllVials, inv(BioObjSample, 1, BioObjTouchPaper, 1))}, CustomBioGateOut},
		{"worn suit counts", bioWorld{stage: BioSpokenChemist, at: bioQuarterTile, inv: bioMerge(bioAllVials, inv(BioObjSample, 1, BioObjTouchPaper, 1)), worn: bioFullSuit}, CustomBioGuidor},
		{"a failed Guidor waits", bioWorld{stage: BioSpokenChemist, at: bioQuarterTile, inv: ready, failed: []string{CustomBioGuidor}}, "wait"},
		{"Guidor told: out", bioWorld{stage: BioFoundSecret, at: bioQuarterTile}, CustomBioGateOut},
		{"stage 10 in the quarter: out", bioWorld{stage: BioGivenDistillator, at: bioQuarterTile}, CustomBioGateOut},
		{"stage 15 in the quarter: out", bioWorld{stage: BioReportedElena, at: bioQuarterTile}, CustomBioGateOut},
		{"a failed gate waits", bioWorld{stage: BioFoundSecret, at: bioQuarterTile, failed: []string{CustomBioGateOut}}, "wait"},
	} {
		st := DecideBiohazard(c.w.snap())
		if got := stepName(st); got != c.want {
			t.Errorf("%s: %s (%s), want %s", c.name, got, st.Describe(), c.want)
		}
		if why := ValidateStep(st); why != "" {
			t.Errorf("%s: %s", c.name, why)
		}
	}
}

// The steps that wait for an hours-long reason park at once for that long, and the rest wait for the engine's own backoff.
func TestBiohazardParksCarryTheirTime(t *testing.T) {
	weak := map[string]int{"attack": 5, "strength": 5, "defence": 5, "hitpoints": 10}
	st := DecideBiohazard(bioWorld{stage: BioReleasedPigeons, levels: weak, hp: 10, mx: 10, ran: []string{"provision:" + KitBioFood}}.snap())
	if st.Kind != StepWait || st.ParkFor != BioKeyPark || !strings.Contains(st.Reason, "sick mourner") {
		t.Errorf("a refused fight: %+v", st)
	}
	st = DecideBiohazard(bioWorld{stage: BioGivenDistillator, ran: []string{"provision:" + KitBioStashVarrock}, failed: []string{"provision:" + KitBioCoins},
		inv: bioMerge(bioAllVials, inv(BioObjSample, 1))}.snap())
	if st.Kind != StepWait || st.ParkFor != BioSupplyPark || !strings.Contains(st.Reason, "coins") {
		t.Errorf("no coins: %+v", st)
	}
	st = DecideBiohazard(bioWorld{stage: BioSpokenChemist, at: bioQuarterTile, failed: []string{CustomBioGateOut}, inv: inv(BioObjSample, 1)}.snap())
	if st.Kind != StepWait || st.ParkFor != 30*time.Minute {
		t.Errorf("a gate that would not open: %+v", st)
	}
}

// Every kit and deposit the decision asks for keeps the quest's own items in the pack and the food the account fights on, and no kit
// line is malformed.
func TestBiohazardKitsAndKeepLists(t *testing.T) {
	keep := bioKeep()
	for _, id := range BioToolIDs() {
		if !slices.Contains(keep, id) {
			t.Errorf("the keep list lacks the quest item %d", id)
		}
	}
	for _, id := range BioFoodIDs() {
		if !slices.Contains(keep, id) {
			t.Errorf("the keep list lacks the food %d", id)
		}
	}
	// The rotten apples and the coins are the tools the bank visits and the eat loop must not touch: the apples have an Eat op.
	for _, id := range []int{BioObjRottenApples, CoinsID, BioObjKey, BioObjDoctorGown, BioObjDistillator, BioObjSample, BioObjTouchPaper} {
		if !slices.Contains(bioTools, id) {
			t.Errorf("the tool list lacks %d", id)
		}
		if !slices.Contains(Biohazard.ToolIDs, id) {
			t.Errorf("the module's tool list lacks %d", id)
		}
	}
	for name, kit := range map[string][]KitLine{
		KitBioStashEarly: bioStashEarly(), KitBioStashHQ: bioStashHQ(), KitBioStashVarrock: bioStashVarrock(), KitBioFood: bioFoodKit(), KitBioCoins: bioCoinsKit(),
		KitBioSuit: bioSuitKit(Snapshot{InvIDs: map[int]int{}, WornIDs: map[int]int{}}),
	} {
		if len(kit) == 0 {
			t.Errorf("kit %s is empty", name)
		}
		for _, l := range kit {
			if why := ValidateKitLine(l); why != "" {
				t.Errorf("kit %s: %s", name, why)
			}
			// Quest items are never asked of the mule (they are untradeable), and nothing in a stash blocks the quest.
			if strings.HasPrefix(name, "bio-stash") && !l.Optional {
				t.Errorf("stash kit %s has a line that blocks: %+v", name, l)
			}
		}
	}
	for _, l := range bioStashEarly() {
		if !l.Untradeable {
			t.Errorf("%+v is a quest item and must be untradeable", l)
		}
	}
	// The suit's lines are tradeable and come from Thessalia's shelf; a half already owned is not asked for.
	suit := bioSuitKit(Snapshot{InvIDs: map[int]int{}, WornIDs: map[int]int{}})
	if len(suit) != 2 || suit[0].ID != BioObjPriestGown || suit[1].ID != BioObjPriestRobe {
		t.Fatalf("the suit kit %+v", suit)
	}
	for _, l := range suit {
		if l.Untradeable || len(l.Sources) != 1 || l.Sources[0].Kind != SourceShop || l.Sources[0].Shop.NpcID != BioNpcThessalia {
			t.Errorf("suit line %+v", l)
		}
	}
	if one := bioSuitKit(Snapshot{InvIDs: map[int]int{BioObjPriestGown: 1}, WornIDs: map[int]int{BioObjPriestRobe: 1}}); len(one) != 0 {
		t.Errorf("a whole suit (one half worn) still asks for %+v", one)
	}
	if one := bioSuitKit(Snapshot{InvIDs: map[int]int{BioObjPriestRobe: 1}, WornIDs: map[int]int{}}); len(one) != 1 || one[0].ID != BioObjPriestGown {
		t.Errorf("half a suit asks for %+v", one)
	}
	// The coins line is flexible: the floor is what the trip is made on.
	c := bioCoinsKit()[0]
	if c.ID != CoinsID || c.Qty != BioCoinsQty || c.Min != BioCoinsMin || !c.Stackable || c.Optional {
		t.Errorf("the coins line %+v", c)
	}
	// Every provision and deposit step of the decision, over a sweep of worlds, carries the keep list.
	r := rand.New(rand.NewSource(7))
	for i := 0; i < 4000; i++ {
		st := DecideBiohazard(bioRandomWorld(r).snap())
		if st.Kind == StepProvision || st.Kind == StepDeposit {
			if !slices.Equal(st.KeepIDs, keep) {
				t.Fatalf("%s keeps %v, want the module's list", st.Describe(), st.KeepIDs)
			}
			if !st.BankSet {
				t.Fatalf("%s names no bank", st.Describe())
			}
		}
	}
}

// bioRandomWorld is a world of any stage at any place with any pack: the sweep property tests run over it.
func bioRandomWorld(r *rand.Rand) bioWorld {
	stages := []int{-1, 0, 1, 2, 3, 4, 5, 6, 7, 8, 10, 12, 14, 15}
	ats := []tile.Tile{bioEastTile, bioWestTile, bioYardTile, bioHQTile, bioHQUpTile, bioQuarterTile, bioSewerTile, bioVarrockTile,
		{X: 2530, Z: 3331, Level: 1}, {X: 2537, Z: 9670}, {X: 2934, Z: 3210}}
	items := []int{CoinsID, BioObjEthenea, BioObjLiquidHoney, BioObjBroline, BioObjSample, BioObjTouchPaper, BioObjDistillator, BioObjBirdfeed, BioObjKey, BioObjPigeons,
		BioObjEmptyCage, BioObjPriestGown, BioObjPriestRobe, BioObjDoctorGown, BioObjRottenApples, PlagueObjShark, 3000, 3001}
	w := bioWorld{stage: stages[r.Intn(len(stages))], at: ats[r.Intn(len(ats))], free: 1 + r.Intn(28)}
	w.inv, w.worn = map[int]int{}, map[int]int{}
	for _, id := range items {
		switch r.Intn(5) {
		case 0:
			w.inv[id] = 1 + r.Intn(3)
		case 1:
			if id == BioObjDoctorGown || id == BioObjPriestGown || id == BioObjPriestRobe {
				w.worn[id] = 1
			}
		}
	}
	if r.Intn(2) == 0 {
		w.bank = map[int]int{}
		for _, id := range items {
			if r.Intn(4) == 0 {
				w.bank[id] = 1 + r.Intn(3)
			}
		}
	}
	for _, k := range append(BiohazardCustomNames(), "provision:"+KitBioStashEarly, "provision:"+KitBioStashHQ, "provision:"+KitBioStashVarrock,
		"provision:"+KitBioFood, "provision:"+KitBioCoins, "provision:"+KitBioSuit) {
		switch r.Intn(8) {
		case 0:
			w.ran = append(w.ran, k)
		case 1:
			w.failed = append(w.failed, k)
		}
	}
	if r.Intn(3) == 0 {
		lv := 1 + r.Intn(70)
		w.levels = map[string]int{"attack": lv, "strength": lv, "defence": 1 + r.Intn(lv), "hitpoints": 10 + r.Intn(60), "prayer": 1, "magic": 1, "ranged": 1}
		w.mx = w.levels["hitpoints"]
		w.hp = 1 + r.Intn(w.mx)
	}
	return w
}

// A sweep of worlds: every step is well formed and is the kind of step the quest uses, a custom step is one the module has an executor
// for, and nothing ever walks across the wall or Varrock's gate (no walk step at all).
func TestDecideBiohazardSweepIsWellFormed(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	names := map[string]bool{}
	for _, n := range BiohazardCustomNames() {
		names[n] = true
	}
	kits := map[string]bool{KitBioStashEarly: true, KitBioStashHQ: true, KitBioStashVarrock: true, KitBioFood: true, KitBioCoins: true, KitBioSuit: true}
	for i := 0; i < 20000; i++ {
		w := bioRandomWorld(r)
		s := w.snap()
		st := DecideBiohazard(s)
		if why := ValidateStep(st); why != "" {
			t.Fatalf("world %+v: %s (%s)", w, why, st.Describe())
		}
		switch st.Kind {
		case StepCustom:
			if !names[st.Custom] {
				t.Fatalf("world %+v: an unknown custom step %q", w, st.Custom)
			}
		case StepProvision:
			if !kits[st.KitName] {
				t.Fatalf("world %+v: an unknown kit %q", w, st.KitName)
			}
		case StepWait, StepDone, StepDeposit:
		default:
			t.Fatalf("world %+v: a %s step, which no leg of Biohazard is (%s)", w, st.Kind, st.Describe())
		}
		// A leg that failed this run is never asked for again.
		if st.Kind == StepCustom && s.Failed[st.Custom] {
			t.Fatalf("world %+v: %s asked again after it failed", w, st.Custom)
		}
		if st.Kind == StepProvision {
			key := "provision:" + st.KitName
			// A kit that failed is never asked again in the run; the stashes, the coins and the food are once-a-run passes.
			if s.Failed[key] && st.KitName != KitBioStashEarly {
				t.Fatalf("world %+v: kit %s asked again after it failed", w, st.KitName)
			}
			if s.Ran[key] && st.KitName != KitBioSuit && !(st.KitName == KitBioStashEarly && false) {
				t.Fatalf("world %+v: kit %s asked again after it ran", w, st.KitName)
			}
		}
		// The talks carry their stops, and a deposit asks nothing else of the pack.
		if st.Kind == StepCustom && st.Stop.Npc != "" && st.Stop.ID == 0 {
			t.Fatalf("world %+v: a stop with no npc id: %+v", w, st.Stop)
		}
	}
}

// The quest is restartable from any state: whatever the pack and the place, there is a way forward that does not depend on the run's
// memory. A world that has run and failed nothing and holds what a stage needs gets a custom or provision step, never a wait.
func TestDecideBiohazardAHealthyAccountAlwaysHasAStep(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	for i := 0; i < 5000; i++ {
		w := bioRandomWorld(r)
		w.ran, w.failed = nil, nil
		if w.stage >= 8 && w.stage != 10 && w.stage != 12 && w.stage != 14 && w.stage != 15 {
			continue
		}
		w.levels, w.hp, w.mx = nil, 0, 0 // a sound account
		w.bank = nil
		st := DecideBiohazard(w.snap())
		if st.Kind == StepWait && !strings.Contains(st.Reason, "tab") {
			t.Fatalf("world %+v: waits with nothing failed or tried: %s", w, st.Reason)
		}
	}
}

func TestBioKeyOddsPricesTheSickMourner(t *testing.T) {
	mk := func(a, s, d, hp, sharks int) Snapshot {
		sn := Snapshot{HP: hp, MaxHP: hp, InvIDs: map[int]int{}, Levels: map[string]int{"attack": a, "strength": s, "defence": d, "hitpoints": hp}}
		if sharks > 0 {
			sn.InvIDs[PlagueObjShark] = sharks
		}
		return sn
	}
	for _, c := range []struct {
		name string
		s    Snapshot
		want bool
	}{
		{"a level one account", mk(1, 1, 1, 10, 0), false},
		{"a level one account with four sharks", mk(1, 1, 1, 10, 4), false},
		{"attack and strength 10, defence 1", mk(10, 10, 1, 10, 4), false},
		{"20/20/1 and 20 hitpoints, no food: too thin at the worst of the fight", mk(20, 20, 1, 20, 0), false},
		{"20/20/20 and 30 hitpoints, no food", mk(20, 20, 20, 30, 0), true},
		{"15/15/10 and 25 hitpoints, no food", mk(15, 15, 10, 25, 0), false},
		{"15/15/10 and 25 hitpoints, three sharks", mk(15, 15, 10, 25, 3), true},
		{"30/30/30", mk(30, 30, 30, 30, 0), true},
		{"60/60/60", mk(60, 60, 60, 60, 0), true},
		{"unread levels pass: nothing is judged on an unread stat", Snapshot{}, true},
		{"unread hitpoints pass", Snapshot{Levels: map[string]int{"attack": 1, "strength": 1}}, true},
	} {
		ok, why := BioKeyOdds(c.s)
		if ok != c.want {
			t.Errorf("%s: %v (%s), want %v", c.name, ok, why, c.want)
		}
		if !ok && why == "" {
			t.Errorf("%s: refused without a reason", c.name)
		}
	}
	// A hurt account is topped up from its food before the first blow, as the loop does.
	hurt := mk(15, 15, 10, 25, 3)
	hurt.HP = 8
	if ok, why := BioKeyOdds(hurt); !ok {
		t.Errorf("a hurt account with sharks was refused: %s", why)
	}
	hurt.InvIDs = map[int]int{}
	if ok, _ := BioKeyOdds(hurt); ok {
		t.Error("a hurt account with no food was passed")
	}
	// The foe's numbers: the maximum hit the messages quote.
	if FoeSickMourner.MaxHit() != 2 || FoeSickMourner.PeakHit() != 2 || FoeSickMourner.Hitpoints != 19 {
		t.Errorf("the sick mourner %+v hits for %d", FoeSickMourner, FoeSickMourner.MaxHit())
	}
	if f, ok := FoeByNPC(NpcSickMourner); !ok || f.Name != FoeSickMourner.Name {
		t.Errorf("the sick mourner is not in the fight tables: %+v %v", f, ok)
	}
}

func TestBiohazardModulePolicy(t *testing.T) {
	m := Biohazard
	if m.Record.ID != BiohazardID || m.Record.Name != "Biohazard" || m.Record.QuestPoints != 3 || m.MaxDeaths != 2 || !m.NoRetaliate || m.ReadStage == nil || m.Decide == nil ||
		m.ExclusiveDone == nil || m.Sustain.EatAt != 12 || !m.BankSet {
		t.Errorf("module %+v", m)
	}
	if len(m.Record.Requirements.Quests) != 1 || m.Record.Requirements.Quests[0] != PlagueCityID {
		t.Errorf("requirements %+v", m.Record.Requirements)
	}
	for _, leg := range []string{CustomBioKey, CustomBioGown} {
		c, ok := m.StepDeathCaps[leg]
		if !ok || c.Max != 1 || c.Window != 24*time.Hour || c.Skip != 24*time.Hour {
			t.Errorf("%s: death cap %+v (declared %v)", leg, c, ok)
		}
	}
	if len(m.StepDeathCaps) != 2 {
		t.Errorf("%d step death caps, want the two", len(m.StepDeathCaps))
	}
	// The slot is given up once Elena has the distillator, not before, and a finished quest has no slot to hold.
	past := func(w bioWorld) bool { return m.ExclusiveDone(w.snap()) }
	for stage, want := range map[int]bool{-1: false, 0: false, 1: false, 5: false, 6: false, 7: false, 10: true, 12: true, 14: true, 15: true} {
		if got := past(bioWorld{stage: stage}); got != want {
			t.Errorf("stage %d: past the headquarters %v, want %v", stage, got, want)
		}
	}
	if !past(bioWorld{stage: -2}) || past(bioWorld{stage: -3}) {
		t.Error("a complete quest is past the headquarters and an unread one is not")
	}
	// The custom names are distinct and every one has a stop-free or stop-carrying decision.
	seen := map[string]bool{}
	for _, n := range BiohazardCustomNames() {
		if seen[n] || n == "" {
			t.Errorf("custom name %q repeated or empty", n)
		}
		seen[n] = true
	}
	// The preferences: every conversation the plan drives has the stop's id, and its menu lines are not empty strings.
	for _, stop := range []NpcStop{BioElenaStart, BioElenaTalk, BioElenaReplace, BioJericoStop, BioOmartStop, BioKilronStop, BioMournerStop, BioChemistStop, BioGuidorStop, BioLathasStop} {
		if stop.ID == 0 || stop.Npc == "" || stop.Leash <= 0 {
			t.Errorf("stop %+v", stop)
		}
		for _, p := range stop.Prefer {
			if strings.TrimSpace(p) == "" {
				t.Errorf("stop %s has an empty preference", stop.Npc)
			}
		}
	}
	for _, er := range BioErrands {
		if er.Rim.ID == 0 || er.Var.ID == 0 || er.Rim.ID == er.Var.ID || len(er.Rim.Prefer) != 1 || !strings.Contains(er.Rim.Prefer[0], er.VialName) {
			t.Errorf("errand %+v", er)
		}
	}
}

// The three boys are paired with the one vial each that survives: Hops the broline, DeVinci the ethenea, Chancy the honey, and each
// vial once.
func TestBioErrandsPairEachBoyWithHisVial(t *testing.T) {
	want := map[string]int{"Hops": BioObjBroline, "DeVinci": BioObjEthenea, "Chancy": BioObjLiquidHoney}
	seen := map[int]bool{}
	for _, er := range BioErrands {
		if want[er.Name] != er.Vial || seen[er.Vial] {
			t.Errorf("%s carries vial %d", er.Name, er.Vial)
		}
		seen[er.Vial] = true
	}
	if len(seen) != 3 || !slices.Equal(BioVials(), []int{BioObjBroline, BioObjEthenea, BioObjLiquidHoney}) {
		t.Errorf("vials %v", BioVials())
	}
}

func TestBioHuntersBoxes(t *testing.T) {
	if len(BioWestHunters) != 7 {
		t.Fatalf("%d hunters", len(BioWestHunters))
	}
	for _, h := range BioWestHunters {
		b, r := h.Box(), h.Reach()
		if !b.Contains(h.Spawn) || !r.Contains(tile.Tile{X: h.Spawn.X + h.Wander + BioHuntRange, Z: h.Spawn.Z}) || r.Contains(tile.Tile{X: h.Spawn.X + h.Wander + BioHuntRange + 1, Z: h.Spawn.Z}) {
			t.Errorf("%s at %v: box %+v reach %+v", h.Name, h.Spawn, b, r)
		}
		// None reaches a place the quest works: the nurse's hut and its door, the street door, the apples, the ladder.
		for _, at := range []tile.Tile{BioNurseCupboard, {X: 2519, Z: 3275}, {X: 2518, Z: 3275}, BioHQDoorOutside, BioApples, BioWestLanding, BioKilronAnchor} {
			if r.Contains(at) {
				t.Errorf("%s at %v can reach %v", h.Name, h.Spawn, at)
			}
		}
	}
}
