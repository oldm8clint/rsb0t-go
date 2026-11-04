package questplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

func merlinSnap(stage int) Snapshot {
	return Snapshot{Journal: StatusInProgress, Stage: stage, StageKnown: true, InvIDs: map[int]int{}, WornIDs: map[int]int{},
		FreeSlots: 20, BankKnown: true, BankIDs: map[int]int{}, Ran: map[string]bool{}, Failed: map[string]bool{},
		Tile: CatherbyBank, TileKnown: true}
}

func TestMerlinsCrystalSteps(t *testing.T) {
	check := func(name string, s Snapshot, kind StepKind, want string) {
		t.Helper()
		st := DecideMerlinsCrystal(s)
		if why := ValidateStep(st); why != "" {
			t.Errorf("%s: malformed %s: %s", name, st.Describe(), why)
		}
		got := st.Custom
		if st.Kind == StepTalk {
			got = st.Stop.Npc
		}
		if st.Kind == StepProvision {
			got = st.KitName
		}
		if st.Kind != kind || (want != "" && got != want) {
			t.Errorf("%s: got %s %q (%s)", name, st.Kind, got, st.Describe())
		}
	}
	s := merlinSnap(0)
	s.Journal = StatusNotStarted
	check("not started", s, StepTalk, "King Arthur")
	check("stage 1", merlinSnap(MerlinStarted), StepTalk, "Sir Gawain")
	check("stage 2", merlinSnap(MerlinGawain), StepTalk, "Sir Lancelot")
	check("stage 3: fight food first", merlinSnap(MerlinLancelot), StepProvision, KitMerlinFight)
	s = merlinSnap(MerlinLancelot)
	s.Ran["provision:"+KitMerlinFight] = true
	check("stage 3: the keep", s, StepCustom, CustomMerlinKeep)
	// merlin-control-1: the kit failed and the pack holds no food, so no fight (it parks).
	s = merlinSnap(MerlinLancelot)
	s.Failed["provision:"+KitMerlinFight] = true
	check("stage 3: failed kit, no food: wait, never the keep", s, StepWait, "")
	s.InvIDs[1891] = 2
	check("stage 3: failed kit but cakes held: the keep", s, StepCustom, CustomMerlinKeep)
	s = merlinSnap(MerlinLancelot)
	s.Failed["provision:"+KitMerlinFight] = true
	s.Tile = tile.Tile{X: 2769, Z: 3403, Level: 2}
	check("stage 3: already inside the keep: carry on", s, StepCustom, CustomMerlinKeep)
	s = merlinSnap(MerlinMorgan)
	s.Tile = tile.Tile{X: 2769, Z: 3403, Level: 2}
	check("stage 4 still in the keep: leave", s, StepCustom, CustomMerlinLeaveKeep)
	s = merlinSnap(MerlinMorgan)
	check("stage 4: the altar words first, carrying nothing", s, StepCustom, CustomMerlinAltar)
	s.Stage = MerlinWordsKnown
	check("words known: the kit", s, StepProvision, KitMerlin)
	s.Ran["provision:"+KitMerlin] = true
	check("no bones: a bat", s, StepCustom, CustomMerlinBat)
	s.InvIDs[ObjBatBones] = 1
	s.InvIDs[ObjInsectRepel], s.InvIDs[ObjBucket] = 1, 1
	check("wax", s, StepCustom, CustomMerlinWax)
	delete(s.InvIDs, ObjBucket)
	s.InvIDs[ObjBucketOfWax] = 1
	check("the candle maker", s, StepTalk, "Candle maker")
	delete(s.InvIDs, ObjBucketOfWax)
	s.InvIDs[ObjBlackCandle], s.InvIDs[ObjTinderbox] = 1, 1
	check("light it", s, StepUseOn, "")
	delete(s.InvIDs, ObjBlackCandle)
	s.InvIDs[ObjLitBlackCandle] = 1
	check("no bread: the baker, by the item rule", s, StepProvision, "")
	s.InvIDs[ObjBread] = 1
	check("Excalibur", s, StepCustom, CustomMerlinExcalibur)
	s.InvIDs[ObjExcalibur] = 1
	check("summon", s, StepCustom, CustomMerlinSummon)
	s.Stage = MerlinBound
	check("the crystal", s, StepCustom, CustomMerlinCrystal)
	delete(s.InvIDs, ObjExcalibur)
	s.BankIDs[ObjExcalibur] = 1
	check("Excalibur banked: withdraw", s, StepWithdraw, "")
	check("stage 6: Arthur", merlinSnap(MerlinCrystalGone), StepTalk, "King Arthur")
}

func TestParseMerlinsCrystalJournalWords(t *testing.T) {
	base := "@dbl@To bind Thrantax to my will I will need the following:|"
	if st, ok := ParseMerlinsCrystalJournal([]string{base, "@dre@Some magic words from a Chaos Altar"}); !ok || st != MerlinMorgan {
		t.Fatalf("stage %d %v", st, ok)
	}
	if st, ok := ParseMerlinsCrystalJournal([]string{base, "@str@I have the magic words from the Chaos Altar memorised"}); !ok || st != MerlinWordsKnown {
		t.Fatalf("stage %d %v", st, ok)
	}
}

// A8: an EMPTY mule sends the Merlin kit to ground spawns and Arhein's store.
func TestMerlinKitWithAnEmptyMule(t *testing.T) {
	p := PlanProvision(merlinKit(Snapshot{}), ProvisionState{BankKnown: true, MuleKnown: true, FreeSlots: 20, Pack: map[int]int{CoinsID: 100}})
	if len(p.MuleAsk) != 0 || p.Phase != PhaseSource {
		t.Fatalf("plan %s", p.Summary())
	}
	kinds := map[int]SourceKind{}
	for _, task := range p.Tasks {
		kinds[task.Line.ID] = task.Source.Kind
	}
	if kinds[ObjInsectRepel] != SourceGround || kinds[ObjBucket] != SourceGround || kinds[ObjTinderbox] != SourceShop || kinds[ObjBread] != SourceShop {
		t.Fatalf("sources %v", kinds)
	}
}

func TestMerlinGoesIntoTheWildernessWithOnlyCoinsAndFood(t *testing.T) {
	s := Snapshot{Journal: StatusInProgress, StageKnown: true, Stage: MerlinMorgan, InvIDs: map[int]int{CoinsID: 50, 385: 4},
		WornIDs: map[int]int{}, Failed: map[string]bool{}, Ran: map[string]bool{}}
	if st := DecideMerlinsCrystal(s); st.Kind != StepCustom || st.Custom != CustomMerlinAltar {
		t.Fatalf("coins and sharks only: %s, want the altar", st.Describe())
	}
	s.InvIDs[ObjExcalibur] = 1
	if st := DecideMerlinsCrystal(s); st.Custom != CustomMerlinStrip {
		t.Fatalf("holding Excalibur: %s, want the strip", st.Describe())
	}
	delete(s.InvIDs, ObjExcalibur)
	s.WornIDs[1277] = 1
	if st := DecideMerlinsCrystal(s); st.Custom != CustomMerlinStrip {
		t.Fatalf("wearing a sword: %s, want the strip", st.Describe())
	}
}

// Sir Mordred is fought by magic: the fight kit buys Wind Bolt's chaos runes at Magic 17 (air
// runes too only without a worn air staff), Fire Strike's at 13-16, nothing below, and food the
// account can buy (the baker's cakes) beside the bank-or-mule sharks. With an empty bank and an
// empty mule the plan goes to Betty and the baker.
func TestMerlinFightKitIsCastingRunesAndFood(t *testing.T) {
	ids := func(kit []KitLine) map[int]KitLine {
		out := map[int]KitLine{}
		for _, l := range kit {
			out[l.ID] = l
		}
		return out
	}
	s := merlinSnap(MerlinLancelot)
	s.Levels = map[string]int{"magic": 17}
	s.WornIDs[1381] = 1
	k := ids(merlinFightKit(s))
	if k[objChaosRune].Qty != merlinCasts || k[objChaosRune].Optional {
		t.Fatalf("magic 17 with a staff of air: chaos line %+v", k[objChaosRune])
	}
	if _, ok := k[objAirRune]; ok {
		t.Fatal("air runes bought although a staff of air is worn")
	}
	if c := k[1891]; !c.Optional || len(c.Sources) != 1 || c.Sources[0].Kind != SourceShop {
		t.Fatalf("cake line %+v", c)
	}
	delete(s.WornIDs, 1381)
	if k = ids(merlinFightKit(s)); k[objAirRune].Qty != 2*merlinCasts {
		t.Fatalf("no staff worn: air line %+v", k[objAirRune])
	}
	s.Levels["magic"] = 13
	k = ids(merlinFightKit(s))
	if k[objMindRune].Qty != merlinCasts || k[objFireRune].Qty != 3*merlinCasts || k[objChaosRune].Qty != 0 {
		t.Fatalf("magic 13: %v", k)
	}
	s.Levels["magic"] = 5
	for _, id := range merlinFightRunes {
		if _, ok := ids(merlinFightKit(s))[id]; ok {
			t.Fatalf("magic 5 bought rune %d", id)
		}
	}

	// The whole step: an empty bank and an empty mule send the runes to Betty and the cakes to
	// the baker, and the provision keeps the runes through its bank visit.
	s.Levels["magic"] = 17
	s.WornIDs[1381] = 1
	st := DecideMerlinsCrystal(s)
	if st.Kind != StepProvision || st.KitName != KitMerlinFight {
		t.Fatalf("stage 3: %s", st.Describe())
	}
	keep := map[int]bool{}
	for _, id := range st.KeepIDs {
		keep[id] = true
	}
	if !keep[objChaosRune] {
		t.Fatal("the fight provision would deposit the chaos runes it bought")
	}
	p := PlanProvision(st.Kit, ProvisionState{BankKnown: true, MuleKnown: true, FreeSlots: 20, Pack: map[int]int{CoinsID: 2000}})
	if p.Phase != PhaseSource || len(p.MuleAsk) != 0 {
		t.Fatalf("plan %s", p.Summary())
	}
	shops := map[int]string{}
	for _, task := range p.Tasks {
		shops[task.Line.ID] = task.Source.Shop.Npc
	}
	if shops[objChaosRune] != "Betty" || shops[1891] != "Baker" {
		t.Fatalf("tasks %v", shops)
	}
}

// The bat is cast at with what the strip before the Wilderness banked: the stage-4 kit draws the
// Mordred kit's leftover runes and the air staff back from the bank, optionally, and never buys
// them.
func TestMerlinKitDrawsTheBatRunesBackFromTheBank(t *testing.T) {
	s := merlinSnap(MerlinWordsKnown)
	s.Levels = map[string]int{"magic": 17}
	s.BankIDs[objChaosRune] = 2 * merlinBatCasts
	s.BankIDs[1381] = 1
	st := DecideMerlinsCrystal(s)
	if st.Kind != StepProvision || st.KitName != KitMerlin {
		t.Fatalf("words known: %s", st.Describe())
	}
	for _, l := range st.Kit {
		if (l.ID == objChaosRune || l.ID == 1381 || l.ID == objAirRune) && (!l.Optional || len(l.Sources) != 0) {
			t.Fatalf("bat line %+v is bought or required", l)
		}
		if l.ID == objAirRune {
			t.Fatal("air runes drawn although the banked staff of air pays them")
		}
	}
	keep := map[int]bool{}
	for _, id := range st.KeepIDs {
		keep[id] = true
	}
	if !keep[objChaosRune] || !keep[1381] {
		t.Fatalf("the kit's bank visit would deposit the bat's runes or staff: keep %v", st.KeepIDs)
	}
	p := PlanProvision(st.Kit, ProvisionState{BankKnown: true, Bank: s.BankIDs, MuleKnown: true, FreeSlots: 20, Pack: map[int]int{CoinsID: 100}})
	got := map[int]int{}
	for _, w := range p.Withdraw {
		got[w.ID] = w.Count
	}
	if got[objChaosRune] != merlinBatCasts || got[1381] != 1 {
		t.Fatalf("withdrawals %v (%s)", got, p.Summary())
	}
}
