package questplan

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Plague City's decision, over simulated worlds: the quest tab's colour, the journal stage the parser reads, where the account stands
// (the mainland, the sewer, the city, the Rehnisons' upper floor, the cellar) and what the pack holds. Every step it returns is
// well formed, never a walk across the wall, and the quest is restartable from any state.

// plagueWorld is one simulated snapshot. The tiles are a representative one of each area.
type plagueWorld struct {
	stage  int // the journal's reading (3 for 3-6, 20 for 20-21, 24 for 24-25); -1 not started, -2 complete, -3 unknown tab
	area   PlagueArea
	inv    map[int]int
	worn   map[int]int
	ran    []string
	failed []string
	hp, mx int
	levels map[string]int
	free   int
}

var plagueTiles = map[PlagueArea]tile.Tile{
	PlagueEast:     {X: 2616, Z: 3332},
	PlagueSewer:    {X: 2562, Z: 9737},
	PlagueWest:     {X: 2529, Z: 3304},
	PlagueUpstairs: {X: 2530, Z: 3331, Level: 1},
	PlagueCellar:   {X: 2537, Z: 9670},
}

func (w plagueWorld) snap() Snapshot {
	s := Snapshot{TileKnown: true, Tile: plagueTiles[w.area], InvIDs: map[int]int{}, WornIDs: map[int]int{}, FreeSlots: 20,
		Ran: map[string]bool{}, Failed: map[string]bool{}, Blocked: map[string]bool{}, HP: 40, MaxHP: 40,
		Levels: map[string]int{"attack": 40, "strength": 40, "defence": 40, "hitpoints": 40, "prayer": 10, "magic": 20, "ranged": 1}}
	for id, n := range w.inv {
		s.InvIDs[id] = n
	}
	for id, n := range w.worn {
		s.WornIDs[id] = n
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

func inv(pairs ...int) map[int]int {
	m := map[int]int{}
	for i := 0; i+1 < len(pairs); i += 2 {
		m[pairs[i]] += pairs[i+1]
	}
	return m
}

// kitNames are the names of a provision step's lines, for the failure message.
func plagueKitIDs(st Step) map[int]int {
	out := map[int]int{}
	for _, l := range st.Kit {
		out[l.ID] = l.Qty
	}
	return out
}

func TestParsePlagueJournalNeedlesAreUnambiguousAndCoverTheGroups(t *testing.T) {
	// Each parsed stage is the lowest of the stages the journal renders alike (3-6, 20-21, 24-25); the rendering itself is checked
	// against the script in the content test.
	for _, c := range []struct {
		text string
		want int
	}{
		{"@dbl@I can start this quest by speaking to @dre@Edmond @dbl@who is in @dre@East|@dre@Ardougne|", PlagueNotStarted},
		{"@str@I've spoken to Edmond, he's asked me to help find his|@str@daughter Elena.||@dbl@I need to get some Dwellberries for @dre@Alrena", PlagueStarted},
		{"@dbl@I need to talk to @dre@Edmond @dbl@about getting into @dre@West Ardougne", PlagueGasMask},
		{"@dbl@I need to use @dre@Water @dbl@to soften the ground in Edmond's garden before I can dig into it.", PlagueWater},
		{"@str@I've softened the ground in Edmond's garden enough to dig.|@dbl@Now I need to dig a tunnel into @dre@West Ardougne", PlagueSoft},
		{"@str@I've dug a tunnel into the sewers.|@dbl@I need to find a way out of the sewers and into @dre@West|@dre@Ardougne", PlagueTunnel},
		{"@str@I've dug a tunnel into the sewers.|@dbl@I've tied a rope to the sewer grill but I need help to pull it off.", PlagueRope},
		{"@str@I've managed to clear the way into West Ardougne.|@dbl@Edmond told me to look out for an old family friend named @dre@Jethick.", PlaguePipeOpen},
		{"@str@I've managed to clear the way into West Ardougne.|@dbl@I've spoken to @dre@Jethick@dbl@, he thinks @dre@Elena @dbl@was staying with the @dre@Rehnison Family@dbl@, in a timber house", PlagueShown},
		{"@str@I've spoken to Jethick, he thinks Elena was staying with|@str@the Rehnison Family, in a timber house to the north of the|@str@city.|@dbl@The @dre@Rehnisons' daughter Milli @dbl@may have seen @dre@Elena.", PlagueSpokenParents},
		{"@str@I've spoken to Milli about Elena, she says Elena was taken|@str@into one of the Plague Houses.||@dbl@I need to get access to the @dre@Plague House. Milli @dbl@said it was to the south of the city.", PlagueSpokenMilli},
		{"@str@I've spoken to Milli about Elena|@dbl@I need clearance from either the @dre@Head Mourner @dbl@or @dre@Bravek|@dre@to get into the Plague House", PlagueNeedClearance},
		{"@str@Bravek might give me clearance if I make his Hangover|@str@Cure.||@dbl@I need to bring @dre@Bravek @dbl@the @dre@Hangover Cure @dbl@when I work it out.", PlagueSpokenBravek},
		{"@str@I've given Bravek the Hangover Cure and he has given me|@str@a warrant to enter the plague House.||@dbl@I need to get into the @dre@Plague House @dbl@and find @dre@Elena@dbl@.", PlagueCured},
		{"@str@I've freed Elena from the Plague House.||@dbl@I should go and talk to @dre@Edmond @dbl@again.", PlagueFreed},
		{"@str@I've spoken to Edmond and he thanked me for rescuing his|@str@daughter Elena.|@red@QUEST COMPLETE!", PlagueComplete},
	} {
		if got, ok := ParsePlagueJournal([]string{c.text}); !ok || got != c.want {
			t.Errorf("%q parsed %d (ok %v), want %d", c.text, got, ok, c.want)
		}
	}
	for _, empty := range [][]string{nil, {""}, {"  "}, {"@dbl@Nothing to do with it"}} {
		if got, ok := ParsePlagueJournal(empty); ok {
			t.Errorf("%q parsed as stage %d: an empty or foreign journal is unknown, never stage 0", empty, got)
		}
	}
	// The same text across two lines of the client's journal reads the same (the needles span a line break as a space).
	if got, ok := ParsePlagueJournal([]string{"@dbl@I need to use @dre@Water @dbl@to soften the ground in", "Edmond's garden before I can dig into it."}); !ok || got != PlagueWater {
		t.Errorf("a wrapped line parsed %d %v", got, ok)
	}
}

func TestPlagueAreasAreTheirBoxesAndTheMainlandIsTheRest(t *testing.T) {
	for _, c := range []struct {
		at   tile.Tile
		want PlagueArea
	}{
		{tile.Tile{X: 2616, Z: 3332}, PlagueEast},               // the north bank
		{tile.Tile{X: 2568, Z: 3334}, PlagueEast},               // Edmond
		{tile.Tile{X: 2566, Z: 3331}, PlagueEast},               // the garden: the mud pile's landing
		{tile.Tile{X: 2562, Z: 9737}, PlagueSewer},              // the dig's landing
		{tile.Tile{X: 2561, Z: 9737}, PlagueSewer},              // the mud pile
		{tile.Tile{X: 2530, Z: 9703}, PlagueSewer},              // the pipe's end
		{tile.Tile{X: 2535, Z: 9719}, PlagueSewer},              // Edmond's
		{tile.Tile{X: 2529, Z: 3304}, PlagueWest},               // the pipe's landing
		{tile.Tile{X: 2540, Z: 3305}, PlagueWest},               // Jethick
		{tile.Tile{X: 2533, Z: 3271}, PlagueWest},               // inside the plague house
		{tile.Tile{X: 2531, Z: 3331}, PlagueWest},               // the Rehnisons' ground floor
		{tile.Tile{X: 2531, Z: 3330, Level: 1}, PlagueUpstairs}, // Milli
		{tile.Tile{X: 2537, Z: 9670}, PlagueCellar},             // the stairs' landing
		{tile.Tile{X: 2541, Z: 9672}, PlagueCellar},             // Elena
		{tile.Tile{X: 2436, Z: 3315}, PlagueWest},               // the west gate's corridor
		{tile.Tile{X: 2480, Z: 3270}, PlagueEast},               // the mainland south of the city, which the east flood reaches on foot
		{tile.Tile{X: 2450, Z: 3290}, PlagueEast},               // the mainland west of the city
		{tile.Tile{X: 2448, Z: 3307}, PlagueWest},               // the corridor's south row, found by the real pack's flood
		{tile.Tile{X: 2461, Z: 3290}, PlagueWest},               // the wall's inner column, south of the gate
		{tile.Tile{X: 2461, Z: 3330}, PlagueEast},               // outside the gate, north of the wall
		{tile.Tile{X: 2445, Z: 3325}, PlagueEast},               // outside the corridor's north end
		{tile.Tile{X: 2557, Z: 3300}, PlagueEast},               // the wall's east side
		{tile.Tile{X: 3222, Z: 3218}, PlagueEast},               // Lumbridge
		{tile.Tile{X: 2531, Z: 3331, Level: 2}, PlagueEast},     // no floor above the Rehnisons'
		{tile.Tile{X: 3237, Z: 9859}, PlagueEast},               // some other dungeon
	} {
		if got := PlagueAreaOf(c.at); got != c.want {
			t.Errorf("%v is %v, want %v", c.at, got, c.want)
		}
		if InPlaguePocket(c.at) != (c.want != PlagueEast) {
			t.Errorf("%v: pocket %v for area %v", c.at, InPlaguePocket(c.at), c.want)
		}
	}
	// The plague house's ground floor: the doorstep belongs to the street, the step east of it to the house.
	for _, c := range []struct {
		at   tile.Tile
		want bool
	}{
		{tile.Tile{X: 2533, Z: 3271}, true}, {tile.Tile{X: 2533, Z: 3272}, false}, {tile.Tile{X: 2536, Z: 3272}, true},
		{tile.Tile{X: 2534, Z: 3268}, true}, {tile.Tile{X: 2542, Z: 3270}, false}, {tile.Tile{X: 2533, Z: 3271, Level: 1}, false},
	} {
		if InPlagueHouse(c.at) != c.want {
			t.Errorf("InPlagueHouse(%v) = %v", c.at, !c.want)
		}
	}
	for _, a := range []PlagueArea{PlagueEast, PlagueSewer, PlagueWest, PlagueUpstairs, PlagueCellar} {
		if a.String() == "" {
			t.Errorf("area %d has no name", a)
		}
	}
}

func TestPlagueCityHoldsOffUntilTheTabAndTheJournalAreRead(t *testing.T) {
	for _, c := range []struct {
		name string
		s    Snapshot
		want StepKind
	}{
		{"an uncoloured tab", plagueWorld{stage: -3}.snap(), StepWait},
		{"a complete quest", plagueWorld{stage: -2}.snap(), StepDone},
		{"in progress with the journal unread", func() Snapshot { s := plagueWorld{stage: PlagueWater}.snap(); s.StageKnown = false; return s }(), StepWait},
		{"no position yet", func() Snapshot { s := plagueWorld{stage: -1}.snap(); s.TileKnown = false; return s }(), StepWait},
	} {
		st := DecidePlagueCity(c.s)
		if st.Kind != c.want {
			t.Errorf("%s: %s %q", c.name, st.Kind, st.Describe())
		}
		if why := ValidateStep(st); why != "" {
			t.Errorf("%s: %s", c.name, why)
		}
	}
}

// custom is the step's custom name ("" when it is not a custom step).
func stepName(st Step) string {
	switch st.Kind {
	case StepCustom:
		return st.Custom
	case StepProvision:
		return "provision:" + st.KitName
	}
	return string(st.Kind)
}

func TestDecidePlagueCityEastOfTheWall(t *testing.T) {
	spade, rope, bucket, picture := ObjSpade, ObjRope, ObjBucket, PlagueObjPicture
	mask, cure := PlagueObjGasMask, PlagueObjHangoverCure
	fullBasics := inv(spade, 1, rope, 1, bucket, 1, picture, 1)
	for _, c := range []struct {
		name string
		w    plagueWorld
		want string
	}{
		{"not started", plagueWorld{stage: -1}, "plague-start"},
		{"stage 0 on the yellow tab", plagueWorld{stage: PlagueNotStarted}, "plague-start"},
		{"stage 1 with a berry", plagueWorld{stage: PlagueStarted, inv: inv(PlagueObjDwellberries, 1)}, "plague-alrena"},
		{"stage 1: the bank and the mule first", plagueWorld{stage: PlagueStarted}, "provision:plague-berries"},
		{"stage 1: then the wood", plagueWorld{stage: PlagueStarted, ran: []string{"provision:plague-berries"}}, "plague-berries"},
		{"stage 1: a failed wood waits", plagueWorld{stage: PlagueStarted, ran: []string{"provision:plague-berries"}, failed: []string{"plague-berries"}}, "wait"},
		{"stage 2", plagueWorld{stage: PlagueGasMask}, "plague-edmond"},
		{"stage 3: the errands first", plagueWorld{stage: PlagueWater}, "provision:plague-basics"},
		{"stage 3: the errands failed", plagueWorld{stage: PlagueWater, failed: []string{"provision:plague-basics"}}, "wait"},
		{"stage 3: a full pack makes room first", plagueWorld{stage: PlagueWater, free: 2}, "deposit"},
		{"stage 3 with the errands done", plagueWorld{stage: PlagueWater, inv: fullBasics}, "plague-water"},
		{"stage 7: the cure comes before the dig (the bank and the mule)", plagueWorld{stage: PlagueSoft, inv: fullBasics}, "provision:plague-cure-bank"},
		{"stage 7: then the shops", plagueWorld{stage: PlagueSoft, inv: fullBasics, ran: []string{"provision:plague-cure-bank"}}, "provision:plague-cure-shop"},
		{"stage 7 with the cure made", plagueWorld{stage: PlagueSoft, inv: inv(spade, 1, rope, 1, picture, 1, cure, 1)}, "plague-dig"},
		{"stage 8 in the east goes back down", plagueWorld{stage: PlagueTunnel, inv: inv(spade, 1, rope, 1, picture, 1, cure, 1)}, "plague-dig"},
		{"stage 8 without a rope buys one", plagueWorld{stage: PlagueTunnel, inv: inv(spade, 1, picture, 1, cure, 1)}, "provision:plague-basics"},
		{"stage 9", plagueWorld{stage: PlagueRope, inv: inv(spade, 1, picture, 1, cure, 1)}, "plague-dig"},
		{"stage 10 wants the mask first", plagueWorld{stage: PlaguePipeOpen, inv: inv(spade, 1, picture, 1, cure, 1)}, "provision:plague-mask"},
		{"stage 10: then the cupboard", plagueWorld{stage: PlaguePipeOpen, inv: inv(spade, 1, picture, 1, cure, 1), ran: []string{"provision:plague-mask"}}, "plague-cupboard"},
		{"stage 10 with the mask in the pack", plagueWorld{stage: PlaguePipeOpen, inv: inv(spade, 1, picture, 1, cure, 1, mask, 1)}, "plague-dig"},
		{"stage 10 with the mask worn", plagueWorld{stage: PlaguePipeOpen, inv: inv(spade, 1, picture, 1, cure, 1), worn: inv(mask, 1)}, "plague-dig"},
		{"stage 20 without the book: the bank first", plagueWorld{stage: PlagueShown, inv: inv(spade, 1, cure, 1, mask, 1)}, "provision:plague-book"},
		{"stage 20 with the book", plagueWorld{stage: PlagueShown, inv: inv(spade, 1, cure, 1, mask, 1, PlagueObjBook, 1)}, "plague-dig"},
		{"stage 20, the book banked and gone: the picture for Jethick", plagueWorld{stage: PlagueShown, inv: inv(spade, 1, cure, 1, mask, 1), ran: []string{"provision:plague-book"}}, "provision:plague-basics"},
		{"stage 22", plagueWorld{stage: PlagueSpokenParents, inv: inv(spade, 1, cure, 1, mask, 1)}, "plague-dig"},
		{"stage 26 without the cure makes one", plagueWorld{stage: PlagueSpokenBravek, inv: inv(spade, 1, mask, 1)}, "provision:plague-cure-bank"},
		{"stage 26 with the cure", plagueWorld{stage: PlagueSpokenBravek, inv: inv(spade, 1, mask, 1, cure, 1)}, "plague-dig"},
		{"stage 27 needs no cure", plagueWorld{stage: PlagueCured, inv: inv(spade, 1, mask, 1, PlagueObjWarrant, 1)}, "plague-dig"},
		{"stage 27 without a spade fetches it", plagueWorld{stage: PlagueCured, inv: inv(mask, 1)}, "provision:plague-basics"},
		{"stage 28 is Edmond's", plagueWorld{stage: PlagueFreed}, "plague-finish"},
	} {
		s := c.w.snap()
		st := DecidePlagueCity(s)
		if got := stepName(st); got != c.want {
			t.Errorf("%s: %s (%s), want %s", c.name, got, st.Describe(), c.want)
		}
		if why := ValidateStep(st); why != "" {
			t.Errorf("%s: %s", c.name, why)
		}
	}
}

func TestDecidePlagueCityBasicsKitSaysWhatTheStageNeeds(t *testing.T) {
	for _, c := range []struct {
		name  string
		stage int
		inv   map[int]int
		want  map[int]int // the kit's lines by id and quantity
	}{
		{"the water stage: all four", PlagueWater, nil, map[int]int{ObjSpade: 1, ObjRope: 1, ObjBucket: 4, PlagueObjPicture: 1}},
		{"the water stage holding a bucket of water is not short of a bucket", PlagueWater, inv(ObjBucketOfWater, 1), map[int]int{ObjSpade: 1, ObjRope: 1, PlagueObjPicture: 1}},
		{"the dig: no bucket", PlagueSoft, nil, map[int]int{ObjSpade: 1, ObjRope: 1, PlagueObjPicture: 1}},
		{"the grill: rope", PlagueTunnel, inv(ObjSpade, 1), map[int]int{ObjRope: 1, PlagueObjPicture: 1}},
		{"after the grill: no rope", PlagueRope, nil, map[int]int{ObjSpade: 1, PlagueObjPicture: 1}},
		{"the book stage with no book: the picture", PlagueShown, nil, map[int]int{ObjSpade: 1, PlagueObjPicture: 1}},
		{"the book stage with the book: no picture", PlagueShown, inv(PlagueObjBook, 1), map[int]int{ObjSpade: 1}},
		{"the Rehnisons: only a spade", PlagueSpokenParents, nil, map[int]int{ObjSpade: 1}},
		{"the warrant: only a spade", PlagueCured, nil, map[int]int{ObjSpade: 1}},
		{"the end: nothing", PlagueFreed, nil, map[int]int{}},
	} {
		s := plagueWorld{stage: c.stage, inv: c.inv}.snap()
		kit := plagueBasics(s, c.stage)
		got := map[int]int{}
		for _, l := range kit {
			got[l.ID] = l.Qty
			if why := ValidateKitLine(l); why != "" {
				t.Errorf("%s: %s", c.name, why)
			}
		}
		if fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("%s: kit %v, want %v", c.name, got, c.want)
		}
	}
	// The bucket line is flexible: one is enough to start, four are asked of the bank.
	for _, l := range plagueBasics(plagueWorld{stage: PlagueWater}.snap(), PlagueWater) {
		if l.ID == ObjBucket && (l.Min != 1 || l.Qty != 4) {
			t.Errorf("the bucket line %+v", l)
		}
		if l.ID == PlagueObjPicture && !l.Untradeable {
			t.Errorf("the picture is untradeable (never asked of the mule): %+v", l)
		}
	}
}

func TestDecidePlagueCityTheCureIsAssembledInOrder(t *testing.T) {
	// The east at stage 7 with spade, rope and bucket in hand: the walk through the cure.
	base := inv(ObjSpade, 1, ObjBucket, 1, ObjRope, 1, PlagueObjPicture, 1)
	bankPass := "provision:" + KitPlagueCureBank
	shops := "provision:" + KitPlagueCureShops
	steps := []struct {
		name string
		w    plagueWorld
		want string
	}{
		{"the bank and the mule first", plagueWorld{stage: PlagueSoft, inv: base}, bankPass},
		{"nothing banked: the shops' pestle and bar", plagueWorld{stage: PlagueSoft, inv: base, ran: []string{bankPass}}, shops},
		{"pestle and bar: mix", plagueWorld{stage: PlagueSoft, inv: inv(ObjSpade, 1, ObjRope, 1, PlagueObjPicture, 1, ObjBucket, 1, PlagueObjPestle, 1, PlagueObjChocolateBar, 1), ran: []string{bankPass}}, "plague-brew"},
		{"dust: the grass (a sound account goes to the trail)", plagueWorld{stage: PlagueSoft, inv: inv(ObjSpade, 1, ObjRope, 1, PlagueObjPicture, 1, ObjBucket, 1, PlagueObjChocolateDust, 1), ran: []string{bankPass}}, "plague-grass"},
		{"dust and grass: the milk", plagueWorld{stage: PlagueSoft, inv: inv(ObjSpade, 1, ObjRope, 1, PlagueObjPicture, 1, ObjBucket, 1, PlagueObjChocolateDust, 1, PlagueObjSnapeGrass, 1), ran: []string{bankPass}}, "plague-milk"},
		{"dust, grass and milk: mix the dust in", plagueWorld{stage: PlagueSoft, inv: inv(ObjSpade, 1, ObjRope, 1, PlagueObjPicture, 1, PlagueObjChocolateDust, 1, PlagueObjSnapeGrass, 1, ObjBucketOfMilk, 1), ran: []string{bankPass}}, "plague-brew"},
		{"chocolaty milk and grass: the last mix", plagueWorld{stage: PlagueSoft, inv: inv(ObjSpade, 1, ObjRope, 1, PlagueObjPicture, 1, PlagueObjChocolateMilk, 1, PlagueObjSnapeGrass, 1), ran: []string{bankPass}}, "plague-brew"},
		{"chocolaty milk: the grass", plagueWorld{stage: PlagueSoft, inv: inv(ObjSpade, 1, ObjRope, 1, PlagueObjPicture, 1, PlagueObjChocolateMilk, 1), ran: []string{bankPass}}, "plague-grass"},
		{"the cure: the dig", plagueWorld{stage: PlagueSoft, inv: inv(ObjSpade, 1, ObjRope, 1, PlagueObjPicture, 1, PlagueObjHangoverCure, 1)}, "plague-dig"},
		{"a failed brew is not repeated at once (and nothing else is to do)", plagueWorld{stage: PlagueSoft, inv: inv(ObjSpade, 1, ObjRope, 1, PlagueObjPicture, 1, PlagueObjPestle, 1, PlagueObjChocolateBar, 1, PlagueObjSnapeGrass, 1, ObjBucketOfMilk, 1), ran: []string{bankPass}, failed: []string{"plague-brew"}}, "wait"},
		{"a failed shop pass waits", plagueWorld{stage: PlagueSoft, inv: base, ran: []string{bankPass}, failed: []string{shops}}, "wait"},
		{"a failed grass leg waits", plagueWorld{stage: PlagueSoft, inv: inv(ObjSpade, 1, ObjRope, 1, PlagueObjPicture, 1, ObjBucket, 1, PlagueObjChocolateDust, 1), ran: []string{bankPass}, failed: []string{"plague-grass"}}, "wait"},
		{"a failed milking waits", plagueWorld{stage: PlagueSoft, inv: inv(ObjSpade, 1, ObjRope, 1, PlagueObjPicture, 1, ObjBucket, 1, PlagueObjChocolateDust, 1, PlagueObjSnapeGrass, 1), ran: []string{bankPass}, failed: []string{"plague-milk"}}, "wait"},
	}
	for _, c := range steps {
		st := DecidePlagueCity(c.w.snap())
		got := stepName(st)
		if got != c.want {
			// A failed leg is a wait; the wait names it.
			t.Errorf("%s: %s (%s), want %s", c.name, got, st.Describe(), c.want)
		}
	}
	// The shop kit asks only for what the recipe still lacks.
	for _, c := range []struct {
		name string
		inv  map[int]int
		want map[int]int
	}{
		{"nothing: the pestle, the bar and a bucket for the milk", nil, map[int]int{PlagueObjPestle: 1, PlagueObjChocolateBar: 1, ObjBucket: 1}},
		{"a bucket held", inv(ObjBucket, 1), map[int]int{PlagueObjPestle: 1, PlagueObjChocolateBar: 1}},
		{"milk held: no bucket", inv(ObjBucketOfMilk, 1), map[int]int{PlagueObjPestle: 1, PlagueObjChocolateBar: 1}},
		{"a pestle held", inv(ObjBucket, 1, PlagueObjPestle, 1), map[int]int{PlagueObjChocolateBar: 1}},
		{"a bar held", inv(ObjBucket, 1, PlagueObjChocolateBar, 1), map[int]int{PlagueObjPestle: 1}},
		{"dust held: neither", inv(ObjBucket, 1, PlagueObjChocolateDust, 1), map[int]int{}},
		{"chocolaty milk held: neither and no bucket", inv(PlagueObjChocolateMilk, 1), map[int]int{}},
	} {
		got := map[int]int{}
		for _, l := range plagueCureShopKit(plagueWorld{stage: PlagueSoft, inv: c.inv}.snap()) {
			got[l.ID] = l.Qty
			if why := ValidateKitLine(l); why != "" {
				t.Errorf("%s: %s", c.name, why)
			}
		}
		if fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("%s: shop kit %v, want %v", c.name, got, c.want)
		}
	}
	// The bank-and-mule kit is every line optional with no source of its own: it can block nothing.
	for _, l := range plagueCureBankKit() {
		if !l.Optional || len(l.Sources) != 0 {
			t.Errorf("the bank pass's line %+v must be optional and sourceless", l)
		}
		if why := ValidateKitLine(l); why != "" {
			t.Error(why)
		}
	}
}

func TestPlagueNextMixPrefersTheLatestMixAndNeverGrindsASpare(t *testing.T) {
	has := func(ids ...int) func(int) bool {
		set := map[int]bool{}
		for _, id := range ids {
			set[id] = true
		}
		return func(id int) bool { return set[id] }
	}
	for _, c := range []struct {
		name    string
		held    []int
		use, on int
		ok      bool
	}{
		{"nothing", nil, 0, 0, false},
		{"pestle and bar", []int{PlagueObjPestle, PlagueObjChocolateBar}, PlagueObjPestle, PlagueObjChocolateBar, true},
		{"pestle alone", []int{PlagueObjPestle}, 0, 0, false},
		{"dust and milk", []int{PlagueObjChocolateDust, ObjBucketOfMilk}, PlagueObjChocolateDust, ObjBucketOfMilk, true},
		{"dust, milk and a spare bar: the dust goes in first", []int{PlagueObjPestle, PlagueObjChocolateBar, PlagueObjChocolateDust, ObjBucketOfMilk}, PlagueObjChocolateDust, ObjBucketOfMilk, true},
		{"chocolaty milk and grass", []int{PlagueObjChocolateMilk, PlagueObjSnapeGrass}, PlagueObjSnapeGrass, PlagueObjChocolateMilk, true},
		{"chocolaty milk, grass and a spare bar: the cure", []int{PlagueObjChocolateMilk, PlagueObjSnapeGrass, PlagueObjPestle, PlagueObjChocolateBar}, PlagueObjSnapeGrass, PlagueObjChocolateMilk, true},
		{"a bar and a pestle with the dust already made: no second grind", []int{PlagueObjPestle, PlagueObjChocolateBar, PlagueObjChocolateDust}, 0, 0, false},
		{"grass and milk only", []int{PlagueObjSnapeGrass, ObjBucketOfMilk}, 0, 0, false},
	} {
		use, on, ok := PlagueNextMix(has(c.held...))
		if use != c.use || on != c.on || ok != c.ok {
			t.Errorf("%s: %d on %d (%v), want %d on %d (%v)", c.name, use, on, ok, c.use, c.on, c.ok)
		}
	}
}

func TestDecidePlagueCityTheSewerAndTheCity(t *testing.T) {
	mask, spade := PlagueObjGasMask, ObjSpade
	for _, c := range []struct {
		name string
		w    plagueWorld
		want string
	}{
		{"sewer, stage 8 with a rope", plagueWorld{stage: PlagueTunnel, area: PlagueSewer, inv: inv(ObjRope, 1)}, "plague-rope"},
		{"sewer, stage 8 without one goes home", plagueWorld{stage: PlagueTunnel, area: PlagueSewer}, "plague-home"},
		{"sewer, stage 9", plagueWorld{stage: PlagueRope, area: PlagueSewer}, "plague-grill"},
		{"sewer, stage 10 with the mask and the picture", plagueWorld{stage: PlaguePipeOpen, area: PlagueSewer, inv: inv(mask, 1, PlagueObjPicture, 1)}, "plague-cross"},
		{"sewer, stage 10 with the mask worn", plagueWorld{stage: PlaguePipeOpen, area: PlagueSewer, inv: inv(PlagueObjPicture, 1), worn: inv(mask, 1)}, "plague-cross"},
		{"sewer, stage 10 without the mask goes home", plagueWorld{stage: PlaguePipeOpen, area: PlagueSewer, inv: inv(PlagueObjPicture, 1)}, "plague-home"},
		{"sewer, stage 10 without the picture goes home", plagueWorld{stage: PlaguePipeOpen, area: PlagueSewer, inv: inv(mask, 1)}, "plague-home"},
		{"sewer, stage 26 without the cure goes home", plagueWorld{stage: PlagueSpokenBravek, area: PlagueSewer, inv: inv(mask, 1)}, "plague-home"},
		{"sewer, stage 26 with it crosses", plagueWorld{stage: PlagueSpokenBravek, area: PlagueSewer, inv: inv(mask, 1, PlagueObjHangoverCure, 1)}, "plague-cross"},
		{"sewer, stage 22 crosses", plagueWorld{stage: PlagueSpokenParents, area: PlagueSewer, inv: inv(mask, 1)}, "plague-cross"},
		{"sewer, stage 28 goes home", plagueWorld{stage: PlagueFreed, area: PlagueSewer}, "plague-home"},
		{"sewer, stage 3 goes home", plagueWorld{stage: PlagueWater, area: PlagueSewer}, "plague-home"},
		{"sewer, a failed way home parks", plagueWorld{stage: PlagueWater, area: PlagueSewer, failed: []string{"plague-home"}}, "wait"},
		{"west, stage 10 with the picture", plagueWorld{stage: PlaguePipeOpen, area: PlagueWest, inv: inv(PlagueObjPicture, 1)}, "plague-jethick"},
		{"west, stage 10 without it goes home", plagueWorld{stage: PlaguePipeOpen, area: PlagueWest}, "plague-home"},
		{"west, stage 20 with the book", plagueWorld{stage: PlagueShown, area: PlagueWest, inv: inv(PlagueObjBook, 1)}, "plague-book"},
		{"west, stage 20 without it asks the Rehnisons", plagueWorld{stage: PlagueShown, area: PlagueWest}, "plague-parents"},
		{"west, stage 20: the Rehnisons silent, the picture in hand: Jethick", plagueWorld{stage: PlagueShown, area: PlagueWest, inv: inv(PlagueObjPicture, 1), failed: []string{"plague-parents"}}, "plague-jethick"},
		{"west, stage 20: silent and no picture: home", plagueWorld{stage: PlagueShown, area: PlagueWest, failed: []string{"plague-parents"}}, "plague-home"},
		{"west, stage 20: the book handed in this run, an earlier silence forgotten: the Rehnisons again", plagueWorld{stage: PlagueShown, area: PlagueWest, inv: inv(PlagueObjPicture, 1),
			ran: []string{"plague-book"}, failed: []string{"plague-parents"}}, "plague-parents"},
		{"west, stage 20: the book handed in this run, no earlier silence", plagueWorld{stage: PlagueShown, area: PlagueWest, ran: []string{"plague-book"}}, "plague-parents"},
		{"west, stage 22", plagueWorld{stage: PlagueSpokenParents, area: PlagueWest}, "plague-milli"},
		{"west, stage 23", plagueWorld{stage: PlagueSpokenMilli, area: PlagueWest}, "plague-mourner"},
		{"west, stage 24", plagueWorld{stage: PlagueNeedClearance, area: PlagueWest}, "plague-audience"},
		{"west, stage 26 with the cure", plagueWorld{stage: PlagueSpokenBravek, area: PlagueWest, inv: inv(PlagueObjHangoverCure, 1)}, "plague-cure"},
		{"west, stage 26 without it goes home", plagueWorld{stage: PlagueSpokenBravek, area: PlagueWest}, "plague-home"},
		{"west, stage 27 with the warrant", plagueWorld{stage: PlagueCured, area: PlagueWest, inv: inv(PlagueObjWarrant, 1)}, "plague-house"},
		{"west, stage 27 without it asks Bravek again", plagueWorld{stage: PlagueCured, area: PlagueWest}, "plague-cure"},
		{"west, stage 28 goes home", plagueWorld{stage: PlagueFreed, area: PlagueWest}, "plague-home"},
		{"west, stage 8 goes home", plagueWorld{stage: PlagueTunnel, area: PlagueWest}, "plague-home"},
		{"upstairs at stage 22 asks Milli", plagueWorld{stage: PlagueSpokenParents, area: PlagueUpstairs}, "plague-milli"},
		{"upstairs after Milli comes down", plagueWorld{stage: PlagueSpokenMilli, area: PlagueUpstairs}, "plague-down"},
		{"cellar at stage 27 is Elena's", plagueWorld{stage: PlagueCured, area: PlagueCellar}, "plague-house"},
		{"cellar at stage 28 goes home", plagueWorld{stage: PlagueFreed, area: PlagueCellar}, "plague-home"},
		{"a failed leg is a wait", plagueWorld{stage: PlagueSpokenMilli, area: PlagueWest, failed: []string{"plague-mourner"}}, "wait"},
		{"a failed house leg is a wait", plagueWorld{stage: PlagueCured, area: PlagueWest, inv: inv(PlagueObjWarrant, 1), failed: []string{"plague-house"}}, "wait"},
	} {
		st := DecidePlagueCity(c.w.snap())
		if got := stepName(st); got != c.want {
			t.Errorf("%s: %s (%s), want %s", c.name, got, st.Describe(), c.want)
		}
		if why := ValidateStep(st); why != "" {
			t.Errorf("%s: %s", c.name, why)
		}
	}
	_ = spade
}

// A failed way home waits with a park of its own (no way out of the city is not a thing to try again in a minute).
func TestPlagueCityNoWayHomeParksForHalfAnHour(t *testing.T) {
	st := DecidePlagueCity(plagueWorld{stage: PlagueWater, area: PlagueWest, failed: []string{"plague-home"}}.snap())
	if st.Kind != StepWait || st.ParkFor != 30*time.Minute || !strings.Contains(st.Reason, "West Ardougne") {
		t.Errorf("%s ParkFor %s: %q", st.Kind, st.ParkFor, st.Reason)
	}
}

func TestDecidePlagueCityTheScrollIsReadOnceThenTheQuestIsDone(t *testing.T) {
	scroll := inv(PlagueObjScroll, 1)
	for _, c := range []struct {
		name string
		w    plagueWorld
		want string
	}{
		{"complete with the scroll on the mainland", plagueWorld{stage: -2, inv: scroll}, "plague-scroll"},
		{"complete, the scroll read", plagueWorld{stage: -2}, "done"},
		{"complete, the reading ran", plagueWorld{stage: -2, inv: scroll, ran: []string{"plague-scroll"}}, "done"},
		{"complete, the reading failed", plagueWorld{stage: -2, inv: scroll, failed: []string{"plague-scroll"}}, "done"},
		{"complete in a pocket is not walked out first", plagueWorld{stage: -2, inv: scroll, area: PlagueWest}, "done"},
	} {
		if got := stepName(DecidePlagueCity(c.w.snap())); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

// Nothing is entered that the model refuses: the hobgoblins' trail needs a combat above 56 or a body that survives it, the wood a
// body that survives a stray dog.
func TestPlagueGrassOddsPriceTheHobgoblinsByCombatAndHitpoints(t *testing.T) {
	lv := func(def, hp, att int) map[string]int {
		return map[string]int{"attack": att, "strength": att, "defence": def, "hitpoints": hp, "prayer": 1, "magic": 1, "ranged": 1}
	}
	for _, c := range []struct {
		name string
		s    Snapshot
		ok   bool
	}{
		{"hitpoints unread", Snapshot{}, true},
		{"a sound mid-level account", Snapshot{HP: 40, MaxHP: 40, Levels: lv(30, 40, 40)}, true},
		{"combat above 56 is not hunted at all", Snapshot{HP: 12, MaxHP: 12, Levels: lv(70, 70, 70)}, true}, // (HP unrealistic: the point is the combat)
		{"a weak hunted account", Snapshot{HP: 14, MaxHP: 14, Levels: lv(1, 14, 5)}, false},
		{"a weak hunted account with sharks to top up from", Snapshot{HP: 14, MaxHP: 40, Levels: lv(1, 40, 20), InvIDs: map[int]int{PlagueObjShark: 3}}, true},
		{"30 of 30 at Defence 1 just passes", Snapshot{HP: 30, MaxHP: 30, Levels: lv(1, 30, 20)}, true},
		{"22 of 30 fed up to 24 does not", Snapshot{HP: 22, MaxHP: 30, Levels: lv(1, 30, 20), InvIDs: map[int]int{PlagueObjShark: 3}}, false},
		{"a hunted account of 30 with nothing", Snapshot{HP: 30, MaxHP: 30, Levels: lv(10, 30, 20)}, true},
		{"a hunted account of 20 with nothing", Snapshot{HP: 20, MaxHP: 20, Levels: lv(1, 20, 10)}, false},
		{"a hurt hunted account", Snapshot{HP: 15, MaxHP: 40, Levels: lv(20, 40, 40)}, false},
		{"the same, fed", Snapshot{HP: 15, MaxHP: 40, Levels: lv(20, 40, 40), InvIDs: map[int]int{PlagueObjShark: 2}}, true},
	} {
		ok, why := PlagueGrassOdds(c.s)
		if ok != c.ok {
			t.Errorf("%s: odds %v (%s), want %v (combat %d)", c.name, ok, why, c.ok, PlagueCombat(c.s))
		}
		if !ok && why == "" {
			t.Errorf("%s: a refusal with no reason", c.name)
		}
	}
	if !PlagueGrassHunted(0) || !PlagueGrassHunted(56) || PlagueGrassHunted(57) {
		t.Error("the hobgoblins hunt up to combat 56 (twice level 28), and an unknown level counts as hunted")
	}
	if got := PlagueCombat(Snapshot{Levels: lv(1, 10, 1)}); got < 3 || got > 5 {
		t.Errorf("a level-3-ish account is combat %d", got)
	}
	if PlagueCombat(Snapshot{}) != 0 {
		t.Error("an unread account has no combat level")
	}
	// The hobgoblin is the content's: attack 22 and strength 24 make a maximum hit of 3.
	if h := FoePlagueHobgoblin.MaxHit(); h != 3 {
		t.Errorf("the hobgoblin's maximum hit is %d, want 3", h)
	}
	if h := FoePlagueGuardDog.MaxHit(); h != 5 {
		t.Errorf("the guard dog's maximum hit is %d, want 5", h)
	}
}

func TestDecidePlagueCityRefusesTheTrailAndTheWoodToAnAccountThatWouldDieThereAndParksTheRest(t *testing.T) {
	weak := map[string]int{"attack": 5, "strength": 5, "defence": 1, "hitpoints": 15, "prayer": 1, "magic": 1, "ranged": 1}
	// The cure at stage 7: the dust and a spade in hand, only the grass missing.
	w := plagueWorld{stage: PlagueSoft, inv: inv(ObjSpade, 1, ObjRope, 1, PlagueObjPicture, 1, ObjBucket, 1, PlagueObjChocolateDust, 1), ran: []string{"provision:" + KitPlagueCureBank}, levels: weak, hp: 15, mx: 15}
	st := DecidePlagueCity(w.snap())
	if stepName(st) != "provision:"+KitPlagueFood {
		t.Fatalf("a weak account is first offered food: %s (%s)", stepName(st), st.Describe())
	}
	if len(st.Kit) != 1 || st.Kit[0].ID != PlagueObjShark || !st.Kit[0].Optional {
		t.Errorf("the food kit %+v", st.Kit)
	}
	w.ran = append(w.ran, "provision:"+KitPlagueFood)
	st = DecidePlagueCity(w.snap())
	if st.Kind != StepWait || st.ParkFor != PlagueGrassPark || !strings.Contains(st.Reason, "hobgoblins") {
		t.Errorf("after the food it parks: %s ParkFor %s %q", st.Kind, st.ParkFor, st.Reason)
	}
	// A failed food pass is the same refusal (it does not loop through the kit).
	w.ran = []string{"provision:" + KitPlagueCureBank}
	w.failed = []string{"provision:" + KitPlagueFood}
	if st = DecidePlagueCity(w.snap()); st.Kind != StepWait || st.ParkFor != PlagueGrassPark {
		t.Errorf("a failed food pass: %s %q", st.Kind, st.Reason)
	}
	// With sharks in the pack, the same account would be allowed in only if the top-up reaches the bar; 15 maximum hitpoints never do.
	w.failed = nil
	w.ran = append(w.ran, "provision:"+KitPlagueFood)
	w.inv = inv(ObjSpade, 1, ObjRope, 1, PlagueObjPicture, 1, ObjBucket, 1, PlagueObjChocolateDust, 1, PlagueObjShark, 5)
	if st = DecidePlagueCity(w.snap()); st.Kind != StepWait {
		t.Errorf("fifteen hitpoints with five sharks: %s %q", st.Kind, st.Reason)
	}

	// The wood at stage 1: the same refusal pattern for an account that cannot take four blows of a dog.
	b := plagueWorld{stage: PlagueStarted, ran: []string{"provision:" + KitPlagueBerries}, levels: weak, hp: 15, mx: 15}
	if st = DecidePlagueCity(b.snap()); stepName(st) != "provision:"+KitPlagueFood {
		t.Fatalf("the wood: %s (%s)", stepName(st), st.Describe())
	}
	b.ran = append(b.ran, "provision:"+KitPlagueFood)
	if st = DecidePlagueCity(b.snap()); st.Kind != StepWait || st.ParkFor != PlagueGrassPark || !strings.Contains(st.Reason, "McGrubor") {
		t.Errorf("the wood after the food: %s %q", st.Kind, st.Reason)
	}
	b.mx, b.hp, b.levels = 40, 40, nil
	if st = DecidePlagueCity(b.snap()); stepName(st) != "plague-berries" {
		t.Errorf("a sound account goes into the wood: %s", stepName(st))
	}
}

func TestPlagueCityModuleIsWhatTheEngineRuns(t *testing.T) {
	m := PlagueCity
	if m.Record.ID != "elena" || m.Record.Name != "Plague City" || m.Record.QuestPoints != 1 {
		t.Errorf("record %+v", m.Record)
	}
	if m.Decide == nil || m.ReadStage == nil || !m.NoRetaliate || m.MaxDeaths != 2 || !m.BankSet {
		t.Errorf("module %+v", m)
	}
	if len(m.Record.Requirements.Skills) != 0 || m.Record.Requirements.MinQuestPoints != 0 || len(m.Record.Requirements.Quests) != 0 {
		t.Errorf("the server gates nothing: %+v", m.Record.Requirements)
	}
	for _, name := range []string{CustomPlagueGrass, CustomPlagueBerries} {
		c, ok := m.StepDeathCaps[name]
		if !ok || c.Max != 1 || c.Skip < 24*time.Hour {
			t.Errorf("one death on %s parks the quest for a day: %+v", name, c)
		}
	}
	// A quest tool is never food for the eat loop, and food is never a tool.
	tools := map[int]bool{}
	for _, id := range PlagueToolIDs() {
		tools[id] = true
	}
	for _, id := range PlagueFoodIDs() {
		if tools[id] {
			t.Errorf("food %d is also a quest tool", id)
		}
	}
	for _, id := range []int{PlagueObjChocolateBar, PlagueObjDwellberries, PlagueObjHangoverCure, PlagueObjGasMask, PlagueObjPicture, PlagueObjBook, PlagueObjKey,
		PlagueObjWarrant, PlagueObjNote, PlagueObjScroll, PlagueObjPestle, PlagueObjSnapeGrass, PlagueObjChocolateDust, PlagueObjChocolateMilk,
		ObjBucket, ObjBucketOfWater, ObjBucketOfMilk, ObjSpade, ObjRope, CoinsID} {
		if !tools[id] {
			t.Errorf("item %d is not on the quest's tool list: a bank visit would take it and the eat loop could eat it", id)
		}
	}
	keep := map[int]bool{}
	for _, id := range PlagueKeepIDs() {
		keep[id] = true
	}
	for _, id := range append(PlagueToolIDs(), PlagueFoodIDs()...) {
		if !keep[id] {
			t.Errorf("the keep list lacks %d", id)
		}
	}
	// Every custom step the module can name has a name and a distinct one.
	seen := map[string]bool{}
	for _, name := range PlagueCustomNames() {
		if name == "" || seen[name] || !strings.HasPrefix(name, "plague-") {
			t.Errorf("custom name %q", name)
		}
		seen[name] = true
	}
}

// A sweep over the whole space of states the decision can meet: every stage and place, with and without every item that matters, the
// run's memory of its passes and the hitpoints. Every state yields a well-formed step; the step suits the place (nothing that
// provisions or banks in the city or the sewer, nothing that talks to Edmond across the wall); and the engine is never asked for a
// custom step the quest did not name.
func TestDecidePlagueCitySweepNeverSurprises(t *testing.T) {
	rng := rand.New(rand.NewSource(18102026))
	names := map[string]bool{}
	for _, n := range PlagueCustomNames() {
		names[n] = true
	}
	items := []int{ObjSpade, ObjRope, ObjBucket, ObjBucketOfWater, ObjBucketOfMilk, PlagueObjDwellberries, PlagueObjGasMask, PlagueObjPicture, PlagueObjBook,
		PlagueObjNote, PlagueObjHangoverCure, PlagueObjWarrant, PlagueObjKey, PlagueObjScroll, PlagueObjPestle, PlagueObjSnapeGrass,
		PlagueObjChocolateBar, PlagueObjChocolateDust, PlagueObjChocolateMilk, PlagueObjShark}
	stages := []int{-3, -2, -1, PlagueNotStarted, PlagueStarted, PlagueGasMask, PlagueWater, PlagueSoft, PlagueTunnel, PlagueRope, PlaguePipeOpen,
		PlagueShown, PlagueSpokenParents, PlagueSpokenMilli, PlagueNeedClearance, PlagueSpokenBravek, PlagueCured, PlagueFreed}
	memory := []string{"provision:" + KitPlagueBerries, "provision:" + KitPlagueBasics, "provision:" + KitPlagueMask, "provision:" + KitPlagueBook,
		"provision:" + KitPlagueCureBank, "provision:" + KitPlagueCureShops, "provision:" + KitPlagueFood, "plague-brew", "plague-grass", "plague-milk",
		"plague-home", "plague-parents", "plague-dig", "plague-cross", "plague-house", "plague-berries", "plague-scroll", "plague-water"}
	count := 0
	for _, stage := range stages {
		for area := PlagueEast; area <= PlagueCellar; area++ {
			for i := 0; i < 600; i++ {
				w := plagueWorld{stage: stage, area: area, inv: map[int]int{}, worn: map[int]int{}, mx: 10 + rng.Intn(60)}
				w.hp = 1 + rng.Intn(w.mx)
				w.free = 1 + rng.Intn(28)
				for _, id := range items {
					if rng.Intn(3) == 0 {
						w.inv[id] = 1 + rng.Intn(3)
					}
				}
				if rng.Intn(6) == 0 {
					w.worn[PlagueObjGasMask] = 1
				}
				for _, m := range memory {
					switch rng.Intn(5) {
					case 0:
						w.ran = append(w.ran, m)
					case 1:
						w.failed = append(w.failed, m)
					}
				}
				w.levels = map[string]int{"attack": 1 + rng.Intn(60), "strength": 1 + rng.Intn(60), "defence": 1 + rng.Intn(60), "hitpoints": w.mx,
					"prayer": 1 + rng.Intn(30), "magic": 1 + rng.Intn(50), "ranged": 1 + rng.Intn(40)}
				s := w.snap()
				st := DecidePlagueCity(s)
				count++
				if why := ValidateStep(st); why != "" {
					t.Fatalf("stage %d %v %+v: %s (%s)", stage, area, w, why, st.Describe())
				}
				switch st.Kind {
				case StepCustom:
					if !names[st.Custom] {
						t.Fatalf("stage %d %v: the custom step %q is not one the module names", stage, area, st.Custom)
					}
				case StepProvision, StepDeposit, StepWait, StepDone:
				default:
					t.Fatalf("stage %d %v: a %s step, which the quest never asks for", stage, area, st.Kind)
				}
				// Provisioning and banking walk to shops and banks on the mainland: never from the other side of the wall.
				if area != PlagueEast && (st.Kind == StepProvision || st.Kind == StepDeposit) && stage != -2 {
					t.Fatalf("stage %d %v: %s %q provisions from the wrong side of the wall", stage, area, st.Kind, st.Describe())
				}
				// A step of the city's legs is never asked of an account on the mainland.
				if area == PlagueEast && st.Kind == StepCustom {
					switch st.Custom {
					case CustomPlagueRope, CustomPlagueGrill, CustomPlagueCross, CustomPlagueJethick, CustomPlagueBook, CustomPlagueParents, CustomPlagueMilli,
						CustomPlagueDown, CustomPlagueMourner, CustomPlagueAudience, CustomPlagueCure, CustomPlagueHouse:
						t.Fatalf("stage %d: %s asked of an account on the mainland", stage, st.Custom)
					}
				}
				// A step of the mainland's errands is never asked of an account on the far side.
				if area != PlagueEast && st.Kind == StepCustom {
					switch st.Custom {
					case CustomPlagueStart, CustomPlagueAlrena, CustomPlagueEdmond, CustomPlagueBerries, CustomPlagueWater, CustomPlagueDig,
						CustomPlagueFinish, CustomPlagueMilk, CustomPlagueGrass, CustomPlagueBrew, CustomPlagueCupboard:
						t.Fatalf("stage %d %v: %s asked of an account on the far side", stage, area, st.Custom)
					}
				}
				// The dig needs a spade, the pipe a mask, the grill a rope, the house a warrant.
				if st.Kind == StepCustom {
					need := map[string]int{CustomPlagueDig: ObjSpade, CustomPlagueRope: ObjRope, CustomPlagueHouse: PlagueObjWarrant}
					if id, ok := need[st.Custom]; ok && st.Custom != CustomPlagueHouse && s.HeldID(id) == 0 {
						t.Fatalf("stage %d %v: %s without item %d in the pack", stage, area, st.Custom, id)
					}
					if st.Custom == CustomPlagueHouse && area != PlagueCellar && s.HeldID(PlagueObjWarrant) == 0 {
						t.Fatalf("stage %d %v: the house without a warrant", stage, area)
					}
					if st.Custom == CustomPlagueCross && s.HeldID(PlagueObjGasMask) == 0 && s.WornIDs[PlagueObjGasMask] == 0 {
						t.Fatalf("stage %d: the pipe without a mask", stage)
					}
				}
			}
		}
	}
	if count < 50000 {
		t.Fatalf("only %d states swept", count)
	}
}
