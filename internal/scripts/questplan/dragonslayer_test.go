package questplan

import "testing"

func TestParseDragonSlayerJournal(t *testing.T) {
	cases := []struct {
		lines []string
		stage int
		flags int
	}{
		{[]string{"@dbl@I can start this quest by speaking to the @dre@Guildmaster@dbl@ in"}, 0, 0},
		{[]string{"@str@The Guildmaster of the Champions' Guild said his friend", "@dbl@I should speak to @dre@Oziach@dbl@, who lives by the cliffs to the"}, DragonGuildmaster, 0},
		{[]string{"@dre@Oziach@dbl@ will sell me", "@dbl@I should return to @dre@Oziach@dbl@ for more detailed instructions."}, DragonOziach, DragonNeedsBriefing},
		{[]string{"@str@The Duke of Lumbridge gave me an anti-dragonbreath", "@dbl@I should see if there is a @dre@ship@dbl@ for sale in @dre@Port Sarim@dbl@."}, DragonOziach, DragonHasShield},
		{[]string{"@str@I bought a ship in Port Sarim called the Lady Lumbridge.", "@dbl@I still need to find a @dre@captain@dbl@ for my ship."}, DragonShip, 0},
		{[]string{"@str@I have repaired my ship using wooden planks and steel", "@dbl@I still need to find a @dre@captain@dbl@ for my ship."}, DragonRepaired, DragonShipRepaired},
		{[]string{"@str@ship to Crandor for me.", "@dbl@Now I should go to my ship in @dre@Port Sarim@dbl@ and set sail for"}, DragonNedMap, 0},
		{[]string{"@str@I have found the secret passage leading from", "@dbl@Now all I need to do is kill the @dre@dragon@dbl@!"}, DragonSailed, DragonSecretPassage},
	}
	for _, c := range cases {
		got, ok := ParseDragonSlayerJournal(c.lines)
		st, fl := DragonTracks(got)
		if !ok || st != c.stage || fl != c.flags {
			t.Errorf("%q: stage %d flags %b (ok %v), want %d %b", c.lines, st, fl, ok, c.stage, c.flags)
		}
	}
}

func TestDragonSlayerSteps(t *testing.T) {
	snap := func(stage, flags int) Snapshot {
		return Snapshot{Journal: StatusInProgress, Stage: stage + 100*flags, StageKnown: true, InvIDs: map[int]int{}, WornIDs: map[int]int{},
			FreeSlots: 20, BankKnown: true, BankIDs: map[int]int{}, Ran: map[string]bool{}, Failed: map[string]bool{},
			Tile: DraynorBankTile, TileKnown: true}
	}
	with := func(s Snapshot, ids ...int) Snapshot {
		for _, id := range ids {
			s.InvIDs[id] = 1
		}
		return s
	}
	ns := snap(0, 0)
	ns.Journal = StatusNotStarted
	rich := with(snap(DragonOziach, DragonHasShield), ObjCrandorMap, ObjMazeKey)
	rich.InvIDs[CoinsID] = 5000
	shipKit := with(snap(DragonShip, 0), ObjHammer)
	shipKit.InvIDs[ObjPlank], shipKit.InvIDs[ObjNails] = 3, 12
	cases := []struct {
		name  string
		snap  Snapshot
		kind  StepKind
		check func(Step) bool
	}{
		{"not started: the Guildmaster", ns, StepTalk, func(s Step) bool { return s.Stop.ID == NpcGuildmaster }},
		{"1: Oziach", snap(DragonGuildmaster, 0), StepCustom, func(s Step) bool { return s.Custom == CustomDragonOziach }},
		{"2 briefing owed: Oziach", snap(DragonOziach, DragonNeedsBriefing), StepCustom, func(s Step) bool { return s.Custom == CustomDragonOziach }},
		{"2 with the maze key: the maze", with(snap(DragonOziach, 0), ObjMazeKey), StepCustom, func(s Step) bool { return s.Custom == CustomDragonMaze }},
		{"2 melzar piece: the oracle kit", with(snap(DragonOziach, 0), ObjMazeKey, ObjMapMelzar), StepProvision, func(s Step) bool { return s.KitName == KitDragonOracle }},
		{"2 charms, not asked: the Oracle", with(snap(DragonOziach, 0), ObjMazeKey, ObjMapMelzar, ObjMindBomb, ObjUnfiredBowl, ObjLobsterPot, ObjSilk), StepTalk, func(s Step) bool { return s.Stop.ID == NpcOracle }},
		{"2 charms, asked: the chest", with(snap(DragonOziach, DragonAskedOracle), ObjMazeKey, ObjMapMelzar, ObjMindBomb, ObjUnfiredBowl, ObjLobsterPot, ObjSilk), StepCustom, func(s Step) bool { return s.Custom == CustomDragonOracle }},
		{"2 two pieces, poor: coins", with(snap(DragonOziach, 0), ObjMazeKey, ObjMapMelzar, ObjMapOracle), StepProvision, func(s Step) bool { return s.KitName == KitDragonCoins }},
		{"2 all pieces: combine", with(snap(DragonOziach, 0), ObjMazeKey, ObjMapMelzar, ObjMapOracle, ObjMapWormbrain), StepCustom, func(s Step) bool { return s.Custom == CustomDragonCombine }},
		{"2 map, no shield: the Duke", with(snap(DragonOziach, 0), ObjCrandorMap), StepTalk, func(s Step) bool { return s.Stop.ID == NpcDuke }},
		{"2 map, shield, coins: Klarense", rich, StepTalk, func(s Step) bool { return s.Stop.ID == NpcKlarense }},
		{"3 without the kit: the ship kit", snap(DragonShip, 0), StepProvision, func(s Step) bool { return s.KitName == KitDragonShip }},
		{"3 with the kit: repair", shipKit, StepCustom, func(s Step) bool { return s.Custom == CustomDragonRepair }},
		{"3 repaired: Ned", snap(DragonShip, DragonShipRepaired), StepTalk, func(s Step) bool { return s.Stop.ID == NpcNed }},
		{"8 no shield worn: the Duke", snap(DragonNedMap, 0), StepTalk, func(s Step) bool { return s.Stop.ID == NpcDuke }},
		{"9 passage shut: open it", with(snap(DragonSailed, 0), ObjAntiDragon), StepEquip, func(s Step) bool { return s.ItemID == ObjAntiDragon }},
	}
	for _, c := range cases {
		st := DecideDragonSlayer(c.snap)
		if why := ValidateStep(st); why != "" {
			t.Errorf("%s: malformed: %s", c.name, why)
		}
		if st.Kind != c.kind || !c.check(st) {
			t.Errorf("%s: got %s", c.name, st.Describe())
		}
	}
}

func TestMelzarsMazeRoute(t *testing.T) {
	if MazeLegs[0].KeyID != ObjMazeKey || MazeLegs[len(MazeLegs)-1].Kind != "chest" {
		t.Fatalf("the maze route must start at the front door and end at the chest")
	}
	keys := map[int]bool{}
	for _, l := range MazeLegs {
		if l.Kind == "kill" {
			keys[l.KeyID] = true
		}
		if l.Kind == "door" && l.KeyID != ObjMazeKey && !keys[l.KeyID] {
			t.Fatalf("%s is opened before its key is killed for", l.What)
		}
	}
}

func TestNailBarsCoverOnlyTheNailsStillShort(t *testing.T) {
	if got := NailBars(12, 0, 0); len(got) != 1 || got[0].ID != ObjSteelBar || got[0].Qty != 6 || !got[0].Optional {
		t.Fatalf("12 nails from nothing = %+v, want 6 optional steel bars", got)
	}
	if got := NailBars(12, 5, 1); len(got) != 1 || got[0].Qty != 3 {
		t.Fatalf("7 nails short with 1 bar = %+v, want 3 more bars", got)
	}
	if got := NailBars(12, 12, 0); got != nil {
		t.Fatalf("nails covered = %+v, want no line", got)
	}
	if got := NailBars(8, 0, 4); got != nil {
		t.Fatalf("bars cover it = %+v, want no line", got)
	}
}
