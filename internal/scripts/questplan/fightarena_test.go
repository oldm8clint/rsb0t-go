package questplan

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Journal pages as arena_journal.rs2 renders them: its "|" breaks are separate lines, and the
// colour tags stay in, as the interface text carries them.
func TestParseFightArenaJournal(t *testing.T) {
	cases := []struct {
		lines []string
		want  int
	}{
		{[]string{"@dbl@I can start this quest by speaking to @dre@Lady Servil@dbl@ just", "North-West@dbl@ of the @dre@Khazard Port."}, 0},
		{[]string{"@str@Khazard, and are being forced to fight in his Fight Arena.", "", "@dbl@I should go to the @dre@Fight Arena@dbl@ and try to find her family."}, 1},
		{[]string{"@dbl@Luckily I found some @dre@Khazard armour@dbl@ in a chest in a", "building on the north-east edge of the town."}, 2},
		{[]string{"@str@Luckily I found some Khazard armour in a chest in a", "@dbl@One of the guards told me that he really likes @dre@Khali Brew@dbl@."}, 3},
		{[]string{"@dbl@Now I've got the keys I should try to rescue @dre@Lady Servil's", "family@dbl@."}, 5},
		{[]string{"@dbl@I need to protect @dre@Jeremy and his father@dbl@ from a huge", "@dre@Ogre."}, 6},
		{[]string{"@str@I had to fight a large Ogre to stop it killing Sir Servil.", "", "@dbl@I should talk to @dre@Jeremy@dbl@ in the @dre@Fight Arena@dbl@!"}, 8},
		{[]string{"@str@I'd defeated it, General Khazard had me locked up.", "", "@dbl@I'm going to have to fight in the Arena!"}, 9},
		{[]string{"@dbl@After I'd defeated the Scorpion, @dre@General Khazard@dbl@ ordered", "me to fight his pet, @dre@Bouncer@dbl@."}, 10},
		{[]string{"@str@Fight Arena.", "", "@dre@General Khazard@dbl@ agreed to release the @dre@Servils@dbl@, but he", "wants to kill me himself! I can stay to fight him, or I can run", "@dbl@away."}, 11},
		{[]string{"@str@Fight Arena.", "", "@dre@General Khazard@dbl@ agreed to release the Servils. He", "challenged me to fight him, but I ran away.", "@dbl@I should go and tell @dre@Lady Servil@dbl@ the good news."}, 12},
		{[]string{"@str@General Khazard agreed to release the Servils. He", "@str@challenged me to fight him, but I ran away.", "", "@red@QUEST COMPLETE!"}, 14},
	}
	for _, c := range cases {
		got, ok := ParseFightArenaJournal(c.lines)
		if !ok || got != c.want {
			t.Errorf("%q: got %d/%v, want %d", c.lines, got, ok, c.want)
		}
	}
}

func TestFightArenaSteps(t *testing.T) {
	snap := func(stage int) Snapshot {
		return Snapshot{Journal: StatusInProgress, Stage: stage, StageKnown: true, InvIDs: map[int]int{}, WornIDs: map[int]int{},
			FreeSlots: 20, BankKnown: true, BankIDs: map[int]int{}, Ran: map[string]bool{}, Failed: map[string]bool{},
			Levels: map[string]int{"magic": 20}, Tile: tile.Tile{X: 2566, Z: 3190}, TileKnown: true}
	}
	clone := func(m map[int]int) map[int]int {
		c := map[int]int{}
		for k, v := range m {
			c[k] = v
		}
		return c
	}
	disguised := func(s Snapshot) Snapshot {
		s.WornIDs = clone(s.WornIDs)
		s.WornIDs[ObjKhazardHelmet], s.WornIDs[ObjKhazardArmour] = 1, 1
		return s
	}
	at := func(s Snapshot, t tile.Tile) Snapshot { s.Tile = t; return s }
	armed := func(s Snapshot) Snapshot { // 100 Wind Bolts and fifteen sharks
		s.InvIDs = clone(s.InvIDs)
		s.InvIDs[arenaChaosRune], s.InvIDs[arenaAirRune] = 100, 200
		s.InvIDs[385] = 15
		return s
	}
	blastPack := func(s Snapshot) Snapshot { // Fire Blast at Magic 66, fourteen sharks: wins the chain
		s.Levels = map[string]int{"magic": 66, "defence": 30}
		s.InvIDs = clone(s.InvIDs)
		s.InvIDs[arenaDeathRune], s.InvIDs[arenaFireRune], s.InvIDs[arenaAirRune], s.InvIDs[385] = 60, 300, 240, 14
		return s
	}
	weak := func(s Snapshot) Snapshot { // 28 hitpoints, as the accounts that died to Bouncer had
		s.MaxHP, s.HP = 28, 28
		return s
	}
	noFood := func(s Snapshot) Snapshot {
		s.InvIDs = clone(s.InvIDs)
		delete(s.InvIDs, 385)
		return s
	}
	ns := snap(0)
	ns.Journal = StatusNotStarted
	s3coins := disguised(snap(3))
	s3coins.InvIDs[CoinsID] = 20
	s3brew := disguised(snap(3))
	s3brew.InvIDs[ObjKhaliBrew] = 1
	s5 := disguised(snap(5))
	s5.InvIDs[ObjKhazardKeys] = 1
	s5kitShort := disguised(snap(5))
	s5kitShort.InvIDs[ObjKhazardKeys] = 1
	s5kitShort.Ran = map[string]bool{"provision:" + KitArena: true}
	s2held := snap(2)
	s2held.InvIDs[ObjKhazardHelmet], s2held.InvIDs[ObjKhazardArmour] = 1, 1
	s6failed := snap(6)
	s6failed.Failed = map[string]bool{"provision:" + KitArena: true}
	s6bothFailed := snap(6)
	s6bothFailed.Failed = map[string]bool{"provision:" + KitArena: true, "provision:" + KitArenaStrike: true}
	s6lowFailed := snap(6) // Magic 15: the kit was already Fire Strike, so no second try
	s6lowFailed.Levels = map[string]int{"magic": 15}
	s6lowFailed.Failed = map[string]bool{"provision:" + KitArena: true}
	strikeKit := func(s Step) bool {
		if s.KitName != KitArenaStrike {
			return false
		}
		mind, fire := false, false
		for _, l := range s.Kit {
			switch l.ID {
			case arenaChaosRune:
				return false
			case arenaMindRune:
				mind = true
			case arenaFireRune:
				fire = true
			}
		}
		return mind && fire
	}
	cases := []struct {
		name  string
		snap  Snapshot
		kind  StepKind
		check func(Step) bool
	}{
		{"not started: Lady Servil", ns, StepTalk, func(s Step) bool { return s.Stop.ID == NpcLadyServil }},
		{"stage 1: the chest", snap(1), StepCustom, func(s Step) bool { return s.Custom == CustomArenaChest }},
		{"stage 2 with the pieces held: wear them", s2held, StepEquip, func(s Step) bool { return s.ItemID == ObjKhazardHelmet }},
		{"stage 2 without them: the chest again", snap(2), StepCustom, func(s Step) bool { return s.Custom == CustomArenaChest }},
		{"stage 2 disguised: the drunk guard", disguised(snap(2)), StepCustom, func(s Step) bool { return s.Custom == CustomArenaGuard }},
		{"stage 3 without coins: the kit", disguised(snap(3)), StepProvision, func(s Step) bool { return s.KitName == KitArena }},
		{"stage 3 without coins, inside the building: leave first", at(disguised(snap(3)), DrunkGuardAnchor), StepCustom, func(s Step) bool { return s.Custom == CustomArenaLeave }},
		{"stage 3 with coins: the barman", s3coins, StepTalk, func(s Step) bool { return s.Stop.ID == NpcKhazardBarman && s.UntilItemID == ObjKhaliBrew }},
		{"stage 3 with the brew: the guard", s3brew, StepCustom, func(s Step) bool { return s.Custom == CustomArenaGuard }},
		{"stage 3 with the brew, in the guard's room: no leaving", at(s3brew, DrunkGuardAnchor), StepCustom, func(s Step) bool { return s.Custom == CustomArenaGuard }},
		{"stage 5 unarmed: the kit", s5, StepProvision, func(s Step) bool { return s.KitName == KitArena }},
		{"stage 5 unarmed, inside the building: leave for the kit", at(s5, tile.Tile{X: 2616, Z: 3168}), StepCustom, func(s Step) bool { return s.Custom == CustomArenaLeave }},
		{"stage 5 kit ran and still short: wait outside, never Jeremy", s5kitShort, StepWait, func(Step) bool { return true }},
		{"stage 5 armed: Jeremy's gate", armed(s5), StepCustom, func(s Step) bool { return s.Custom == CustomArenaJeremy }},
		{"stage 5 armed, inside the building: Jeremy's gate", at(armed(s5), DrunkGuardAnchor), StepCustom, func(s Step) bool { return s.Custom == CustomArenaJeremy }},
		{"stage 5 without keys: the guard's spare", disguised(snap(5)), StepCustom, func(s Step) bool { return s.Custom == CustomArenaGuard }},
		{"stage 5 without keys or disguise: the chest", snap(5), StepCustom, func(s Step) bool { return s.Custom == CustomArenaChest }},
		{"stage 6 outside unarmed: the kit before going back in", snap(6), StepProvision, func(s Step) bool { return s.KitName == KitArena }},
		{"stage 6 outside, Bolt kit failed: a Fire Strike kit from Betty", s6failed, StepProvision, strikeKit},
		{"stage 6 outside, both kits failed: wait outside", s6bothFailed, StepWait, func(Step) bool { return true }},
		{"stage 6 outside, Fire Strike kit failed: wait outside", s6lowFailed, StepWait, func(Step) bool { return true }},
		{"stage 6 outside armed, disguised: back in", disguised(armed(snap(6))), StepCustom, func(s Step) bool { return s.Custom == CustomArenaFight }},
		{"stage 6 outside armed, no disguise: the chest", armed(snap(6)), StepCustom, func(s Step) bool { return s.Custom == CustomArenaChest }},
		{"stage 6 on the floor with no runes (acct9): fight", at(snap(6), tile.Tile{X: 2606, Z: 3166}), StepCustom, func(s Step) bool { return s.Custom == CustomArenaFight }},
		{"stage 9 in the cell: Hengrad", at(snap(9), tile.Tile{X: 2600, Z: 3142}), StepCustom, func(s Step) bool { return s.Custom == CustomArenaFight }},
		{"stage 10 outside armed: door1 sends it in, no disguise needed", armed(snap(10)), StepCustom, func(s Step) bool { return s.Custom == CustomArenaFight }},
		{"stage 11 outside: nothing left to arm for", snap(11), StepCustom, func(s Step) bool { return s.Custom == CustomArenaFight }},
		{"stage 12 on the floor: flee", at(snap(12), ArenaSafespot), StepCustom, func(s Step) bool { return s.Custom == CustomArenaFlee }},
		{"stage 12 in the building: flee", at(snap(12), tile.Tile{X: 2607, Z: 3151}), StepCustom, func(s Step) bool { return s.Custom == CustomArenaFlee }},
		{"stage 12 outside: Lady Servil", snap(12), StepTalk, func(s Step) bool { return s.Stop.ID == NpcLadyServil }},
		// The entry gate (6-7 Oct 2026: eleven arena deaths at 27-28 hitpoints).
		{"not started, 28 hitpoints: not even Lady Servil", weak(ns), StepWait, func(s Step) bool { return s.ParkFor == ArenaParkFor }},
		{"stage 5 armed but 28 hitpoints: wait, parked for the gate's skip", weak(armed(s5)), StepWait, func(s Step) bool { return s.ParkFor == ArenaParkFor }},
		{"stage 6 outside armed, 28 hitpoints: wait, never back in", weak(disguised(armed(snap(6)))), StepWait, func(s Step) bool { return s.ParkFor == ArenaParkFor }},
		{"stage 10 outside armed, 28 hitpoints: wait, never back in", weak(armed(snap(10))), StepWait, func(s Step) bool { return s.ParkFor == ArenaParkFor }},
		{"stage 6 on the floor, 28 hitpoints: already trapped, fight", weak(at(snap(6), tile.Tile{X: 2606, Z: 3166})), StepCustom, func(s Step) bool { return s.Custom == CustomArenaFight }},
		{"stage 9 in the cell, 28 hitpoints: already trapped, Hengrad", weak(at(snap(9), tile.Tile{X: 2600, Z: 3142})), StepCustom, func(s Step) bool { return s.Custom == CustomArenaFight }},
		{"stage 5 with the runes but no food: the kit", noFood(armed(s5)), StepProvision, func(s Step) bool { return s.KitName == KitArena }},
		{"stage 5 with the runes, no food, kit already ran: wait", func() Snapshot {
			s := noFood(armed(s5))
			s.Ran = map[string]bool{"provision:" + KitArena: true}
			return s
		}(), StepWait, func(s Step) bool { return s.ParkFor == 0 }},
		{"stage 5 at the hitpoint gate, a pack that wins the chain: Jeremy's gate", func() Snapshot {
			s := blastPack(s5)
			s.MaxHP, s.HP = ArenaMinHitpoints, ArenaMinHitpoints
			return s
		}(), StepCustom, func(s Step) bool { return s.Custom == CustomArenaJeremy }},
		// The chain gate (16 Oct 2026): the rune and food counts pass, the simulated Bouncer does not. The kit is tried once
		// (more sharks may be in the bank), and then the quest waits for hours, not minutes.
		{"stage 5 at the gate, Wind Bolts and 15 sharks: the kit first", func() Snapshot {
			s := armed(s5)
			s.MaxHP, s.HP = ArenaMinHitpoints, ArenaMinHitpoints
			return s
		}(), StepProvision, func(s Step) bool { return s.KitName == KitArena }},
		{"stage 5 at the gate, Wind Bolts and 15 sharks, kit ran: wait, parked for hours (Bouncer)", func() Snapshot {
			s := armed(s5kitShort)
			s.MaxHP, s.HP = ArenaMinHitpoints, ArenaMinHitpoints
			return s
		}(), StepWait, func(s Step) bool {
			return s.ParkFor == ArenaOddsParkFor && strings.Contains(s.Reason, "Bouncer") && strings.Contains(s.Reason, "not going into the arena")
		}},
		{"stage 6 outside, disguised, 22 cakes and Fire Strike runes, kit ran: wait, never back in", func() Snapshot {
			s := disguised(snap(6))
			s.Ran = map[string]bool{"provision:" + KitArena: true}
			s.Levels = map[string]int{"magic": 55, "defence": 30}
			s.MaxHP, s.HP = 53, 53
			s.InvIDs = map[int]int{arenaMindRune: 200, arenaFireRune: 224, arenaAirRune: 400, 1891: 22}
			return s
		}(), StepWait, func(s Step) bool { return s.ParkFor == ArenaOddsParkFor }},
		{"stage 6 outside, a pack that wins what is left: back in", func() Snapshot {
			s := blastPack(disguised(snap(6)))
			s.MaxHP, s.HP = 60, 60
			return s
		}(), StepCustom, func(s Step) bool { return s.Custom == CustomArenaFight }},
	}
	for _, c := range cases {
		st := DecideFightArena(c.snap)
		if why := ValidateStep(st); why != "" {
			t.Errorf("%s: malformed: %s", c.name, why)
		}
		if st.Kind != c.kind || !c.check(st) {
			t.Errorf("%s: got %s", c.name, st.Describe())
		}
	}
}

// The live 4 Oct pilots, from the states run/fleet.log left them in. Each walk outside starts
// with door1, and nothing leaves them standing in the building with an outside walk to make.
func TestFightArenaResumesTheTrappedPilots(t *testing.T) {
	base := func(stage int, at tile.Tile) Snapshot {
		return Snapshot{Journal: StatusInProgress, Stage: stage, StageKnown: true, InvIDs: map[int]int{CoinsID: 40},
			WornIDs: map[int]int{ObjKhazardHelmet: 1, ObjKhazardArmour: 1}, FreeSlots: 14, Ran: map[string]bool{}, Failed: map[string]bool{},
			Levels: map[string]int{"magic": 33}, Tile: at, TileKnown: true}
	}
	// sike, emod, moderator: stage 3, no brew, coins, in the drunk guard's room.
	sike := base(3, tile.Tile{X: 2617, Z: 3141})
	st := DecideFightArena(sike)
	if st.Kind != StepCustom || st.Custom != CustomArenaLeave || !strings.Contains(st.Reason, "Khazard barman") {
		t.Fatalf("sike at 2617,3141: got %s, want door1 out for the barman", st.Describe())
	}
	// Out of the door (2617,3172): the barman, the brew, back to the guard.
	sike.Tile = tile.Tile{X: 2617, Z: 3172}
	if st := DecideFightArena(sike); st.Kind != StepTalk || st.Stop.ID != NpcKhazardBarman {
		t.Fatalf("sike outside: got %s", st.Describe())
	}
	sike.InvIDs[ObjKhaliBrew] = 1
	sike.Tile = KhazardBarAnchor
	if st := DecideFightArena(sike); st.Custom != CustomArenaGuard {
		t.Fatalf("sike with the brew: got %s", st.Describe())
	}
	// The guard trades the brew for the keys in his room: stage 5, unarmed, inside.
	sike.Stage = 5
	delete(sike.InvIDs, ObjKhaliBrew)
	sike.InvIDs[ObjKhazardKeys] = 1
	sike.Tile = DrunkGuardAnchor
	if st := DecideFightArena(sike); st.Custom != CustomArenaLeave {
		t.Fatalf("sike with the keys, unarmed, inside: got %s", st.Describe())
	}
	// acct9: stage 6 on the arena floor, no runes at all.
	acct9 := base(6, tile.Tile{X: 2606, Z: 3166})
	acct9.InvIDs = map[int]int{}
	acct9.Failed["provision:"+KitArena] = true
	if st := DecideFightArena(acct9); st.Custom != CustomArenaFight {
		t.Fatalf("acct9 on the floor: got %s", st.Describe())
	}
	// Dead and respawned with nothing: the kit, from outside, before door1 again.
	acct9.Tile = tile.Tile{X: 3222, Z: 3218}
	acct9.Failed = map[string]bool{}
	if st := DecideFightArena(acct9); st.Kind != StepProvision {
		t.Fatalf("acct9 respawned: got %s", st.Describe())
	}
}

// Every stage, at every place in and around the building: a step that walks outside is never
// handed to the engine from inside the building (the walker has no way out of it).
func TestFightArenaNeverWalksOutOfTheBuilding(t *testing.T) {
	places := []tile.Tile{DrunkGuardAnchor, {X: 2617, Z: 3171}, {X: 2585, Z: 3141}, {X: 2607, Z: 3151}, {X: 2614, Z: 3150}}
	for _, stage := range []int{1, 2, 3, 5, 6, 8, 9, 10, 11, 12, 13} {
		for _, p := range places {
			for _, coins := range []int{0, 20} {
				s := Snapshot{Journal: StatusInProgress, Stage: stage, StageKnown: true, InvIDs: map[int]int{CoinsID: coins, ObjKhazardKeys: 1},
					WornIDs: map[int]int{ObjKhazardHelmet: 1, ObjKhazardArmour: 1}, Ran: map[string]bool{}, Failed: map[string]bool{},
					Tile: p, TileKnown: true}
				if !InKhazardPocket(p) {
					t.Fatalf("%v is not in the pocket", p)
				}
				st := DecideFightArena(s)
				if arenaGoesOutside(st) {
					t.Errorf("stage %d at %v, %d coins: %s walks outside from inside the building", stage, p, coins, st.Describe())
				}
			}
		}
	}
}

func TestArenaPlaces(t *testing.T) {
	cases := []struct {
		t    tile.Tile
		want ArenaPlace
	}{
		{tile.Tile{X: 2617, Z: 3141}, ArenaBuilding}, // sike, the drunk guard's room
		{tile.Tile{X: 2618, Z: 3142}, ArenaBuilding},
		{tile.Tile{X: 2617, Z: 3171}, ArenaBuilding}, // north door1's inside tile
		{tile.Tile{X: 2617, Z: 3172}, ArenaOutside},  // and its outside
		{tile.Tile{X: 2585, Z: 3141}, ArenaBuilding}, // west door1's inside tile
		{tile.Tile{X: 2584, Z: 3141}, ArenaOutside},
		{tile.Tile{X: 2607, Z: 3152}, ArenaBuilding}, // door2's building side (arena_enter's landing)
		{tile.Tile{X: 2606, Z: 3166}, ArenaFloor},    // acct9
		{ArenaSafespot, ArenaFloor},
		{tile.Tile{X: 2600, Z: 3142}, ArenaCell}, // where the guards lock the account in
		{HengradAnchor, ArenaCell},
		{tile.Tile{X: 2617, Z: 3141, Level: 1}, ArenaOutside},
		{KhazardBarAnchor, ArenaOutside},
		{YanilleBank, ArenaOutside},
	}
	for _, c := range cases {
		if got := ArenaPlaceOf(c.t); got != c.want {
			t.Errorf("%v: got %d, want %d", c.t, got, c.want)
		}
	}
	if InArenaTrap(tile.Tile{X: 2617, Z: 3141}) || !InArenaTrap(tile.Tile{X: 2606, Z: 3166}) || !InArenaTrap(HengradAnchor) {
		t.Fatal("InArenaTrap is the floor and the cells only")
	}
}

// The go/no-go numbers: what each kit is worth against what is left to kill.
func TestArenaDamageAndKits(t *testing.T) {
	s := func(magic int, inv map[int]int, worn map[int]int) Snapshot {
		if worn == nil {
			worn = map[int]int{}
		}
		return Snapshot{Levels: map[string]int{"magic": magic}, InvIDs: inv, WornIDs: worn, BankIDs: map[int]int{}}
	}
	// 100 Fire Strikes: 100 x 8 / 2.6 = 307, enough for all three beasts (216).
	fire := s(13, map[int]int{arenaMindRune: 100, arenaFireRune: 300, arenaAirRune: 200}, nil)
	if got := ArenaDamage(fire); got != 307 || got < arenaBeastHP(6) {
		t.Fatalf("100 Fire Strikes: %d", got)
	}
	// At Magic 12 the same runes are only Wind Strikes: 100 x 2 / 2.6 = 76.
	if got := ArenaDamage(s(12, fire.InvIDs, nil)); got != 76 {
		t.Fatalf("Magic 12 with Fire Strike runes: %d", got)
	}
	// A staff of air pays the air: chaos alone is Wind Bolt at 17.
	bolt := s(17, map[int]int{arenaChaosRune: 40}, map[int]int{1381: 1})
	if got := ArenaDamage(bolt); got != 40*9*10/26 || got >= arenaBeastHP(6) || got < arenaBeastHP(10) {
		t.Fatalf("40 Wind Bolts with a staff: %d (Bouncer alone is %d)", got, arenaBeastHP(10))
	}
	if ArenaDamage(s(40, map[int]int{}, nil)) != 0 || arenaBeastHP(11) != 0 || arenaBeastHP(12) != 0 {
		t.Fatal("no runes are worth nothing, and from stage 11 nothing is left to kill")
	}
	// Each kit, once bought, is armed for the whole fight.
	for _, magic := range []int{1, 5, 9, 13, 17, 40} {
		for _, staff := range []bool{false, true} {
			base := s(magic, map[int]int{}, nil)
			if staff {
				base.WornIDs[1381] = 1
			}
			after := s(magic, map[int]int{}, base.WornIDs)
			for _, l := range arenaKit(base) {
				if l.ID == 556 && staff {
					t.Errorf("Magic %d: a staff of air still asks for air runes", magic)
				}
				if !l.Optional {
					after.InvIDs[l.ID] += l.Qty
				}
			}
			if got := ArenaDamage(after); got < arenaBeastHP(0) {
				t.Errorf("Magic %d (staff %v): the kit is worth %d, short of %d", magic, staff, got, arenaBeastHP(0))
			}
		}
	}
	if sp := arenaKitSpell(s(17, nil, nil)); sp.Name != "Wind Bolt" {
		t.Fatalf("Magic 17 kit: %s", sp.Name)
	}
	if sp := arenaKitSpell(Snapshot{}); sp.Name != "Fire Strike" {
		t.Fatalf("an unread Magic level: %s", sp.Name)
	}
}

// The study's tactic, as geometry: the safespot is inside the arena zone (the kill checks'
// zone), directly south of the static Jeremy, and not inside the prison cell.
func TestArenaSafespotGeometry(t *testing.T) {
	if !InArena(ArenaSafespot) || InArenaCell(ArenaSafespot) {
		t.Fatalf("safespot %v: arena %v cell %v", ArenaSafespot, InArena(ArenaSafespot), InArenaCell(ArenaSafespot))
	}
	jeremy := tile.Tile{X: 2602, Z: 3154}
	if ArenaSafespot.X != jeremy.X || ArenaSafespot.Z != jeremy.Z-1 {
		t.Fatalf("the safespot must be directly south of Jeremy")
	}
}

// The penned beasts are visible from the arena floor but no fight: a beast is loose only on the
// floor (acct9 clicked Attack on the penned ogre at 2607,3165 for two hours).
func TestArenaBeastLoose(t *testing.T) {
	for _, c := range []struct {
		at   tile.Tile
		want bool
	}{
		{tile.Tile{X: 2608, Z: 3165}, false}, // the ogre's map spawn
		{tile.Tile{X: 2607, Z: 3165}, false}, // the ogre's npc_add tile
		{tile.Tile{X: 2608, Z: 3159}, false}, // the scorpion's
		{tile.Tile{X: 2608, Z: 3162}, false}, // Bouncer's
		{tile.Tile{X: 2601, Z: 3168}, true},  // where the ogre attacks Justin
		{tile.Tile{X: 2602, Z: 3155}, true},  // frozen behind Jeremy
		{tile.Tile{X: 2606, Z: 3165}, true},  // through the open gate
		{tile.Tile{X: 2602, Z: 3155, Level: 1}, false},
	} {
		if got := ArenaBeastLoose(c.at); got != c.want {
			t.Errorf("%v: loose %v, want %v", c.at, got, c.want)
		}
	}
	if !InArenaPen(tile.Tile{X: 2607, Z: 3165}) || InArenaPen(ArenaSafespot) {
		t.Fatalf("pen geometry")
	}
}

// The empty-mule arm: the runes fall to Betty's shop, the coins come out of the bank on the
// same visit, and without a staff the air runes are on the list too.
func TestArenaKitWithAnEmptyMule(t *testing.T) {
	noStaff := Snapshot{InvIDs: map[int]int{}, WornIDs: map[int]int{}, BankIDs: map[int]int{}}
	p := PlanProvision(arenaKit(noStaff), ProvisionState{BankKnown: true, Bank: map[int]int{CoinsID: 5000}, MuleKnown: true, FreeSlots: 20})
	if p.Phase != PhaseBank || len(p.Blocked) != 0 || p.ShopCoins != 100*3+300*4+200*4+8*12 {
		t.Fatalf("plan %s (shop coins %d)", p.Summary(), p.ShopCoins)
	}
	staff := Snapshot{InvIDs: map[int]int{}, WornIDs: map[int]int{1381: 1}, BankIDs: map[int]int{}}
	for _, l := range arenaKit(staff) {
		if l.ID == 556 {
			t.Fatalf("a staff of air still asks for air runes")
		}
	}
}

// Inside the arena an unread journal stage is no reason to stop fighting (live 9 Oct: a failed journal read every
// other pass cost about ten seconds without a cast each, and accounts died with food carried).
func TestArenaFightsOnInsideWhileTheStageIsUnread(t *testing.T) {
	s := Snapshot{Journal: StatusInProgress, StageKnown: false, TileKnown: true, Tile: tile.Tile{X: 2602, Z: 3153}}
	if st := DecideFightArena(s); st.Kind != StepCustom || st.Custom != CustomArenaFight {
		t.Fatalf("on the floor with the stage unread: %+v, want the fight", st)
	}
	s.Tile = YanilleBank
	if st := DecideFightArena(s); st.Kind != StepWait {
		t.Fatalf("outside with the stage unread: %+v, want a wait", st)
	}
}
