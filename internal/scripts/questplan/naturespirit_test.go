package questplan

import "testing"

func TestParseNatureSpiritJournal(t *testing.T) {
	cases := []struct {
		lines          []string
		stage          int
		nature, spirit bool
	}{
		{[]string{"@dbl@I can start this quest by speaking to @dre@Drezel@dbl@ in the"}, 0, false, false},
		{[]string{"@dbl@I need to look for @dre@Filliman Tarlock@dbl@ in the @dre@Swamps@dbl@ of Mort", "Myre."}, SpiritStarted, false, false},
		{[]string{"@dbl@I think I need to convince this poor fellow @dre@Tarlock@dbl@ that he's", "actually @dre@dead@dbl@!"}, SpiritSpoken, false, false},
		{[]string{"@dre@Filliman@dbl@ might need @dre@my help@dbl@ with his @dre@plan@dbl@."}, SpiritJournal, false, false},
		{[]string{"@dre@Filliman@dbl@ gave me a '@dre@bloom'@dbl@ spell but I need to be @dre@blessed@dbl@ at"}, SpiritSpell, false, false},
		{[]string{"@str@I've been blessed at the temple by Drezel.", "@dbl@I need to collect '@dre@something of nature.@dbl@'"}, SpiritBlessed, false, false},
		{[]string{"@str@I've cast the bloom spell in the swamp.", "@str@I collected a Mort Myre Fungi.",
			"@str@The Mort Myre Fungi was absorbed into the nature stone"}, SpiritFungi, true, false},
		{[]string{"@str@I collected a Mort Myre Fungi.", "@str@The Mort Myre Fungi was absorbed into the nature stone",
			"@str@The spell was absorbed into the spirit stone"}, SpiritFungi, true, true},
		{[]string{"@str@He says that he can cast the spell now"}, SpiritRitual, true, true},
		{[]string{"@str@I've killed three ghasts now."}, SpiritGhast3, true, true},
	}
	for _, c := range cases {
		got, ok := ParseNatureSpiritJournal(c.lines)
		st, n, sp := SpiritTracks(got)
		if !ok || st != c.stage || n != c.nature || sp != c.spirit {
			t.Errorf("%q: stage %d nature %v spirit %v (ok %v), want %d %v %v", c.lines, st, n, sp, ok, c.stage, c.nature, c.spirit)
		}
	}
}

func TestNatureSpiritSteps(t *testing.T) {
	snap := func(stage int) Snapshot {
		return Snapshot{Journal: StatusInProgress, Stage: stage, StageKnown: true, InvIDs: map[int]int{},
			WornIDs: map[int]int{ObjGhostspeak: 1}, FreeSlots: 20, BankKnown: true, BankIDs: map[int]int{},
			Ran: map[string]bool{}, Failed: map[string]bool{}, Tile: SpiritCamp, TileKnown: true}
	}
	with := func(s Snapshot, ids ...int) Snapshot {
		for _, id := range ids {
			s.InvIDs[id]++
		}
		return s
	}
	noAmulet := snap(SpiritSpoken)
	delete(noAmulet.WornIDs, ObjGhostspeak)
	amuletHeld := with(snap(SpiritSpoken), ObjGhostspeak)
	delete(amuletHeld.WornIDs, ObjGhostspeak)
	ns := snap(0)
	ns.Journal = StatusNotStarted
	inGrotto := snap(SpiritBlessed)
	inGrotto.Tile = GrottoInside
	harvested := with(snap(SpiritPicked), ObjSickleBlessed, ObjMortPear, ObjMortStem, ObjMortFungi)
	cases := []struct {
		name  string
		snap  Snapshot
		kind  StepKind
		check func(Step) bool
	}{
		{"no amulet anywhere: Urhney", noAmulet, StepTalk, func(s Step) bool { return s.Stop.ID == NpcUrhney }},
		{"amulet held: wear it", amuletHeld, StepEquip, func(s Step) bool { return s.ItemID == ObjGhostspeak }},
		{"not started: Drezel", ns, StepCustom, func(s Step) bool { return s.Custom == CustomSpiritDrezel }},
		{"5: find Filliman", snap(SpiritStarted), StepCustom, func(s Step) bool { return s.Custom == CustomSpiritTalk }},
		{"20: the mirror", snap(SpiritSpoken), StepCustom, func(s Step) bool { return s.Custom == CustomSpiritMirror }},
		{"25: the journal", snap(SpiritMirror), StepCustom, func(s Step) bool { return s.Custom == CustomSpiritJournal }},
		{"30: help", snap(SpiritJournal), StepCustom, func(s Step) bool { return s.Custom == CustomSpiritHelp }},
		{"35: blessing", snap(SpiritSpell), StepCustom, func(s Step) bool { return s.Custom == CustomSpiritDrezel }},
		{"40 with the scroll: bloom", with(snap(SpiritBlessed), ObjBloomSpell), StepCustom, func(s Step) bool { return s.Custom == CustomSpiritBloom }},
		{"40 without: another scroll", snap(SpiritBlessed), StepCustom, func(s Step) bool { return s.Custom == CustomSpiritScroll }},
		{"in the grotto at 40: out first", inGrotto, StepCustom, func(s Step) bool { return s.Custom == CustomSpiritLeave }},
		{"50 with fungus: stones", with(snap(SpiritFungi), ObjMortFungi, ObjUsedSpell), StepCustom, func(s Step) bool { return s.Custom == CustomSpiritStones }},
		{"55 both fed: the puzzle", snap(SpiritSpoken2 + spiritNatureBit + spiritSpiritBit), StepCustom, func(s Step) bool { return s.Custom == CustomSpiritPuzzle }},
		{"60: into the grotto", snap(SpiritRitual), StepCustom, func(s Step) bool { return s.Custom == CustomSpiritEnter }},
		{"70 without a sickle: the parts", snap(SpiritTransformed), StepProvision, func(s Step) bool { return s.KitName == KitSpiritParts }},
		{"70 with mould and ore: smelt", with(snap(SpiritTransformed), ObjSickleMould, ObjSilverOre), StepCustom, func(s Step) bool { return s.Custom == CustomSpiritSmelt }},
		{"70 with mould and bar: cast", with(snap(SpiritTransformed), ObjSickleMould, ObjSilverBar), StepCustom, func(s Step) bool { return s.Custom == CustomSpiritCast }},
		{"70 with the sickle: the spirit", with(snap(SpiritTransformed), ObjSilverSickle), StepCustom, func(s Step) bool { return s.Custom == CustomSpiritGrotto }},
		{"75 blessed: harvest", with(snap(SpiritSickleBless), ObjSickleBlessed), StepCustom, func(s Step) bool { return s.Custom == CustomSpiritHarvest }},
		{"85 three harvests: fill", harvested, StepCustom, func(s Step) bool { return s.Custom == CustomSpiritFill }},
		{"90 charged pouch: ghast", with(snap(SpiritPouch), ObjSickleBlessed, ObjPouch), StepCustom, func(s Step) bool { return s.Custom == CustomSpiritGhast }},
		{"105: the reward", snap(SpiritGhast3), StepCustom, func(s Step) bool { return s.Custom == CustomSpiritGrotto }},
	}
	for _, c := range cases {
		st := DecideNatureSpirit(c.snap)
		if why := ValidateStep(st); why != "" {
			t.Errorf("%s: malformed: %s", c.name, why)
		}
		if st.Kind != c.kind || !c.check(st) {
			t.Errorf("%s: got %s", c.name, st.Describe())
		}
	}
}

// spirit-empty-3: the sickle's silver needs a pickaxe, bought when none is owned.
func TestSpiritPartsKitCarriesAPickaxe(t *testing.T) {
	s := Snapshot{Journal: StatusInProgress, StageKnown: true, InvIDs: map[int]int{}, WornIDs: map[int]int{},
		BankKnown: true, BankIDs: map[int]int{}, Ran: map[string]bool{}, Failed: map[string]bool{}}
	st := sickleStep(s, nil)
	var pick *KitLine
	for i := range st.Kit {
		if st.Kit[i].ID == ObjBronzePickaxe {
			pick = &st.Kit[i]
		}
	}
	if pick == nil || len(pick.Sources) != 1 || pick.Sources[0].Shop.NpcID != BobsAxes.NpcID {
		t.Fatalf("no bought pickaxe in %+v", st.Kit)
	}
	s.BankIDs[1271] = 1 // an adamant pickaxe in the bank is drawn, never bought
	if l := pickaxeLine(s); l.ID != 1271 || len(l.Sources) != 0 {
		t.Fatalf("owned pickaxe: %+v", l)
	}
}
