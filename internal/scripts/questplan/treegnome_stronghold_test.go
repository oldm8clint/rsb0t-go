package questplan

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// orbSnap is a Stage 5 account at the foot of the wall with the orb step's kit already run: the levels of the
// fleet's median Stage 5 account (Attack 53, Strength 41, Defence 50, 53 hitpoints, Magic 61), the pack given.
func orbSnap(inv map[int]int) Snapshot {
	return Snapshot{Journal: StatusInProgress, Stage: 5, StageKnown: true, InvIDs: inv, WornIDs: map[int]int{},
		Levels: map[string]int{"attack": 53, "strength": 41, "defence": 50, "magic": 61, "hitpoints": 53}, HP: 53, MaxHP: 53,
		FreeSlots: 25, BankKnown: true, BankIDs: map[int]int{}, Ran: map[string]bool{"provision:" + KitTreeOrb: true}, Failed: map[string]bool{},
		Tile: tile.Tile{X: 2509, Z: 3250}, TileKnown: true}
}

func withLevels(s Snapshot, atk, str, def, hp int) Snapshot {
	s.Levels = map[string]int{"attack": atk, "strength": str, "defence": def, "magic": 61, "hitpoints": hp}
	s.HP, s.MaxHP = hp, hp
	return s
}

// THE TWO DEATHS' ACCOUNT: 53 hitpoints, combat 62, 25 free slots, nothing in the pack. The orb step used to go
// straight over the wall; it provisions food first, and with none held it parks for hours with the reason.
func TestTheOrbStepProvisionsFoodThenParksOnAnEmptyPack(t *testing.T) {
	s := orbSnap(map[int]int{})
	s.Ran = map[string]bool{}
	st := DecideTreeGnomeVillage(s)
	if st.Kind != StepProvision || st.KitName != KitTreeOrb {
		t.Fatalf("a fresh run: %s, want the orb kit", st.Describe())
	}
	if why := ValidateStep(st); why != "" {
		t.Fatalf("the kit step is malformed: %s", why)
	}
	for _, l := range st.Kit {
		if !l.Optional {
			t.Errorf("kit line %s is required: an account that cannot buy food must be judged by TreeOrbReady, not blocked in the provisioner", l.Name)
		}
	}
	// The kit ran and brought nothing (the bank held none, the baker was too dear).
	s.Ran["provision:"+KitTreeOrb] = true
	st = DecideTreeGnomeVillage(s)
	if st.Kind != StepWait || st.ParkFor != TreeOrbParkFor {
		t.Fatalf("an empty pack: %s, want a park for %s", st.Describe(), TreeOrbParkFor)
	}
	if !strings.Contains(st.Reason, "no food") || !strings.Contains(st.Reason, "Khazard commanders") {
		t.Errorf("the park does not say why: %q", st.Reason)
	}
	// A kit that failed on the way is not retried in a loop: the gate judges what the pack holds.
	s.Ran = map[string]bool{}
	s.Failed["provision:"+KitTreeOrb] = true
	if st = DecideTreeGnomeVillage(s); st.Kind != StepWait || st.ParkFor == 0 {
		t.Fatalf("a failed kit and an empty pack: %s", st.Describe())
	}
}

// With food the account goes in: the median account clears both commanders in the model, so two sharks (a bite to
// spend and a bite spare) are enough, and the weak ones need what the model says they do.
func TestTheOrbStepGoesInWithFoodTheOddsAccept(t *testing.T) {
	cases := []struct {
		name string
		s    Snapshot
		ok   bool
	}{
		{"median, two sharks", orbSnap(map[int]int{385: 2}), true},
		{"median, the baker's three cakes", orbSnap(map[int]int{1891: 3}), true},
		{"median, one shark: no bite spare", orbSnap(map[int]int{385: 1}), false},
		{"41 hitpoints, two sharks", withLevels(orbSnap(map[int]int{385: 2}), 53, 41, 50, 41), false},
		{"41 hitpoints, three sharks", withLevels(orbSnap(map[int]int{385: 3}), 53, 41, 50, 41), true},
		{"27 hitpoints, four sharks", withLevels(orbSnap(map[int]int{385: 4}), 44, 40, 44, 27), false},
		{"27 hitpoints, six lobsters", withLevels(orbSnap(map[int]int{379: 6}), 44, 40, 44, 27), true},
		{"Attack 20, eight sharks: it dies in the model", withLevels(orbSnap(map[int]int{385: 8}), 20, 20, 20, 30), false},
		{"a mage at Attack 1, sixteen bites of cake and bread", withLevels(orbSnap(map[int]int{1891: 3, 2309: 8}), 1, 1, 1, 20), false},
	}
	for _, c := range cases {
		st := DecideTreeGnomeVillage(c.s)
		if why := ValidateStep(st); why != "" {
			t.Errorf("%s: malformed: %s", c.name, why)
		}
		if c.ok && (st.Kind != StepCustom || st.Custom != CustomTreeOrb) {
			t.Errorf("%s: %s, want the orb step (%s)", c.name, st.Describe(), TreeOrbReady(c.s).Why)
		}
		if !c.ok && (st.Kind != StepWait || st.ParkFor != TreeOrbParkFor || !strings.Contains(st.Reason, "Khazard commander")) {
			t.Errorf("%s: %s, want a park with the reason", c.name, st.Describe())
		}
	}
}

// The gate reads the pack, not the bank: food that is only banked is food the account cannot eat in the tower.
func TestTheOrbGateIgnoresBankedFood(t *testing.T) {
	s := orbSnap(map[int]int{})
	s.BankIDs = map[int]int{385: 30}
	if st := DecideTreeGnomeVillage(s); st.Kind != StepWait {
		t.Fatalf("banked sharks only: %s", st.Describe())
	}
}

// The gate prices the melee swing and nothing else, as the fight step does: runes in the pack do not make a
// mage's account fit for two commanders it will swing at.
func TestTheOrbGateDoesNotCountRunesTheFightStepNeverCasts(t *testing.T) {
	s := withLevels(orbSnap(map[int]int{385: 2, 562: 300, 554: 900}), 1, 1, 1, 20)
	if v := TreeOrbReady(s); v.OK {
		t.Fatalf("a mage with chaos runes passed the melee gate: %+v", v.Odds.Sims)
	}
}

// Stage 6 with the orb lost sends the account back to the chest through the same gate.
func TestTheOrbIsGatedWhenItIsLostToo(t *testing.T) {
	s := orbSnap(map[int]int{})
	s.Stage = 6
	s.Ran = map[string]bool{}
	if st := DecideTreeGnomeVillage(s); st.Kind != StepProvision || st.KitName != KitTreeOrb {
		t.Fatalf("a lost orb, no food: %s, want the kit", st.Describe())
	}
	s.Ran["provision:"+KitTreeOrb] = true
	if st := DecideTreeGnomeVillage(s); st.Kind != StepWait || st.ParkFor == 0 {
		t.Fatalf("a lost orb, still no food: %s", st.Describe())
	}
	s.InvIDs = map[int]int{385: 3}
	if st := DecideTreeGnomeVillage(s); st.Kind != StepCustom || st.Custom != CustomTreeOrb {
		t.Fatalf("a lost orb, three sharks: %s", st.Describe())
	}
	// The orb in hand is Bolren's, wherever the account stands.
	s.InvIDs = map[int]int{ObjOrbOfProt: 1}
	if st := DecideTreeGnomeVillage(s); st.Kind != StepTalk || st.Stop.ID != NpcKingBolren {
		t.Fatalf("the orb held: %s", st.Describe())
	}
}

// INSIDE THE TOWER the same gate decides between the step and the way out, and the kit is not fetched from there.
func TestInsideTheTowerAnUnfitPackWalksOutAndAFitOneGoesOn(t *testing.T) {
	for _, at := range []tile.Tile{KhazardHallLanding, KhazardLadderStand, KhazardChestStand, {X: 2504, Z: 3252, Level: 1}} {
		s := orbSnap(map[int]int{})
		s.Ran = map[string]bool{} // the kit would be asked for outside; inside it never is
		s.Tile = at
		st := DecideTreeGnomeVillage(s)
		if st.Kind != StepCustom || st.Custom != CustomTreeLeave {
			t.Errorf("an empty pack at %v: %s, want the way out", at, st.Describe())
		}
		s.InvIDs = map[int]int{385: 6}
		if st = DecideTreeGnomeVillage(s); st.Kind != StepCustom || st.Custom != CustomTreeOrb {
			t.Errorf("six sharks at %v: %s, want the orb step", at, st.Describe())
		}
	}
}

// Past the hall only the upstairs commander is left, so a pack that cannot take two can still take one: the
// account that won the landing fight with half its food does not turn back from the ladder.
func TestTheLadderRoomPricesOneCommander(t *testing.T) {
	s := withLevels(orbSnap(map[int]int{385: 2}), 53, 41, 50, 41)
	if v := TreeOrbReady(s); v.OK || v.Foes != 2 {
		t.Fatalf("outside, 41 hitpoints, two sharks: ok %v, %d foes: %s", v.OK, v.Foes, v.Why)
	}
	s.Tile = KhazardLadderStand
	if v := TreeOrbReady(s); !v.OK || v.Foes != 1 {
		t.Fatalf("in the ladder room: ok %v, %d foes: %s", v.OK, v.Foes, v.Why)
	}
	s.Tile = KhazardChestStand
	if v := TreeOrbReady(s); v.Foes != 1 {
		t.Fatalf("on the chest floor: %d foes", v.Foes)
	}
	s.Tile = KhazardHallLanding
	if v := TreeOrbReady(s); v.Foes != 2 {
		t.Fatalf("in the hall: %d foes", v.Foes)
	}
}

// The commander is the content's, and what the model makes of one.
func TestTheKhazardCommanderIsPricedFromContent(t *testing.T) {
	f, ok := FoeByNPC(NpcKhazardCommander)
	if !ok || f != FoeKhazardCommander {
		t.Fatalf("FoeByNPC(%d) = %+v, %v", NpcKhazardCommander, f, ok)
	}
	if f.MaxHit() != 5 || f.PeakHit() != 5 || f.Hitpoints != 22 || f.AttackTicks != 4 {
		t.Errorf("commander %+v: max hit %d", f, f.MaxHit())
	}
	// The eat line the fight loops use against him: the module's own 14 stands (two of his hits is 10).
	if got := treeSustain.VsFoe(f, 53).EatAt; got != 14 {
		t.Errorf("eat line %d, want the module's 14", got)
	}
	// What one costs an account at Defence 50 on the model's scale: the 20 to 27 hitpoints the live deaths lost
	// to each (33/53 at the ladder, 14/53 after the second).
	p := MeleeFighter{Attack: 53, Strength: 41, Defence: 50, Magic: 61}
	m, ok := MeleePhase(p, f)
	if !ok {
		t.Fatal("no melee phase")
	}
	res := SimulateFight(FightSimInput{Foe: f, Fighter: p, HP: 53, MaxHP: 53, Phases: []FightPhase{m}, EatLine: 14})
	if !res.Won || res.MinHP < 20 || res.MinHP > 33 {
		t.Errorf("the median account vs one commander: won %v, lowest %.0f hitpoints", res.Won, res.MinHP)
	}
}

// THE POCKET: where the tower is, tile by tile (the real pack's flood has the rest, treegnome_stronghold_pack_test.go).
func TestTheKhazardStrongholdTiles(t *testing.T) {
	in := []tile.Tile{
		KhazardHallLanding, KhazardInnerDoorStand, {X: 2509, Z: 3254}, // the hall and its commander's spawn
		KhazardLadderStand, KhazardFrontDoorStand, {X: 2504, Z: 3255}, {X: 2506, Z: 3255}, // the ladder room
		KhazardChestStand, KhazardLadderTopStand, {X: 2504, Z: 3252, Level: 1}, {X: 2503, Z: 3255, Level: 1}, // the floor above
	}
	for _, at := range in {
		if !InKhazardStronghold(at) {
			t.Errorf("%v is in the stronghold", at)
		}
	}
	out := []tile.Tile{
		KhazardWallSouth, KhazardDoorTile, {X: 2502, Z: 3249}, {X: 2509, Z: 3260}, {X: 2512, Z: 3253}, // outside, round it
		EastArdougneNorth, BolrenAnchor, {X: 2506, Z: 3258, Level: 2}, {X: 2509, Z: 3254, Level: 1}, // another floor, another place
	}
	for _, at := range out {
		if InKhazardStronghold(at) {
			t.Errorf("%v is outside the stronghold", at)
		}
	}
	if !InKhazardHall(KhazardHallLanding) || InKhazardLadderRoom(KhazardHallLanding) || !InKhazardLadderRoom(KhazardLadderStand) || InKhazardHall(KhazardLadderStand) {
		t.Errorf("the hall and the ladder room are not told apart")
	}
	// The door's two sides: the stand inside is in, the tile it sets the account on is out.
	if !InKhazardStronghold(KhazardFrontDoorStand) || InKhazardStronghold(KhazardDoorTile) {
		t.Errorf("the front door's sides: stand in %v, out-tile in %v", InKhazardStronghold(KhazardFrontDoorStand), InKhazardStronghold(KhazardDoorTile))
	}
}

// The module carries the step's death cap and the food policy the gate prices with.
func TestTheTreeGnomeModuleCapsTheOrbStepsDeaths(t *testing.T) {
	c, ok := TreeGnomeVillage.StepDeathCaps[CustomTreeOrb]
	if !ok || c.Max != TreeOrbDeathCap || c.Window != TreeOrbDeathWindow || c.Skip != TreeOrbDeathSkip {
		t.Fatalf("orb step death cap %+v (%v)", c, ok)
	}
	if _, ok := TreeGnomeVillage.StepDeathCaps[CustomTreeWarlord]; !ok {
		t.Errorf("the warlord's cap is gone")
	}
	if got := TreeGnomeVillage.Sustain; got.EatAt != treeSustain.EatAt || len(got.FoodIDs) != len(treeSustain.FoodIDs) {
		t.Errorf("module sustain %+v, gate sustain %+v", got, treeSustain)
	}
}

// STAGE 8 WITHOUT THE ORBS. The four accounts the quest stores leave at tree-ceremony have parked 8, 73, 8 and 6 times:
// the step talked to King Bolren whatever the pack held, and without the orbs he only says "No, I'm afraid not."
// (king_bolren.rs2) and the step waited sixty seconds for a completion that cannot come. Without orbs in the pack the
// plan draws a banked pair, or kills the warlord again (the content pays orbs while the pack and the bank hold none).
func TestStageEightWithoutTheOrbsReclaimsThemInsteadOfTalkingToBolren(t *testing.T) {
	base := func() Snapshot {
		return Snapshot{Journal: StatusInProgress, Stage: 8, StageKnown: true, InvIDs: map[int]int{}, WornIDs: map[int]int{},
			Levels: map[string]int{"magic": 61}, FreeSlots: 23, BankKnown: false, BankIDs: map[int]int{},
			Ran: map[string]bool{}, Failed: map[string]bool{}, Tile: tile.Tile{X: 2616, Z: 3332}, TileKnown: true}
	}
	// With the orbs: Bolren's ceremony.
	s := base()
	s.InvIDs[ObjOrbsOfProt] = 1
	if st := DecideTreeGnomeVillage(s); st.Kind != StepCustom || st.Custom != CustomTreeCeremony {
		t.Fatalf("orbs held: %s", st.Describe())
	}
	// One single orb is not the pair: the quest wants the plural.
	s = base()
	s.InvIDs[ObjOrbOfProt] = 1
	if st := DecideTreeGnomeVillage(s); st.Kind == StepCustom && st.Custom == CustomTreeCeremony {
		t.Fatalf("the single orb sent the account to the ceremony: %s", st.Describe())
	}
	// Without them and without a bank reading: read the bank first.
	s = base()
	if st := DecideTreeGnomeVillage(s); st.Kind != StepScanBank {
		t.Fatalf("no orbs, bank unread: %s", st.Describe())
	}
	// A banked pair is drawn out.
	s = base()
	s.BankKnown, s.BankIDs[ObjOrbsOfProt] = true, 1
	if st := DecideTreeGnomeVillage(s); st.Kind != StepWithdraw || len(st.Items) != 1 || st.Items[0].ID != ObjOrbsOfProt {
		t.Fatalf("orbs banked: %s", st.Describe())
	}
	// None anywhere: the warlord leg, which provisions its kit once and then waits on runes it cannot find.
	s = base()
	s.BankKnown = true
	if st := DecideTreeGnomeVillage(s); st.Kind != StepProvision || st.KitName != KitTreeFight {
		t.Fatalf("no orbs anywhere: %s, want the warlord's kit", st.Describe())
	}
	s.Ran["provision:"+KitTreeFight] = true
	s.InvIDs = map[int]int{554: 2000, 562: 400, 556: 2000}
	if st := DecideTreeGnomeVillage(s); st.Kind != StepCustom || st.Custom != CustomTreeWarlord {
		t.Fatalf("no orbs anywhere, the runes in the pack: %s, want the warlord", st.Describe())
	}
	// Within the ceremony's range of Bolren, after the ceremony step ran, the missing orbs are the ceremony's own gap, a
	// tick before the quest completes. An account that only stands there is not waited for.
	s = base()
	s.BankKnown = true
	s.Tile = tile.Tile{X: 2545, Z: 3172}
	if st := DecideTreeGnomeVillage(s); st.Kind != StepProvision || st.KitName != KitTreeFight {
		t.Fatalf("beside Bolren, no ceremony run, no orbs: %s, want the warlord's kit", st.Describe())
	}
	s.Ran[CustomTreeCeremony] = true
	if st := DecideTreeGnomeVillage(s); st.Kind != StepWait || st.ParkFor != 0 || !strings.Contains(st.Reason, "ceremony") {
		t.Fatalf("beside Bolren after the ceremony step with no orbs: %s", st.Describe())
	}
	s.Tile = tile.Tile{X: 2616, Z: 3332}
	if st := DecideTreeGnomeVillage(s); st.Kind == StepWait {
		t.Fatalf("far from Bolren after the ceremony step with no orbs: %s", st.Describe())
	}
}

// THE KIT THROUGH THE PROVISIONER: banked sharks are drawn and nothing is bought, the baker is the fallback for an account
// with coins and no good food, and an account with neither is never BLOCKED at the bank (every line is optional): the gate,
// not the provisioner, decides whether the pack that results can go in.
func TestTheOrbKitThroughTheProvisioner(t *testing.T) {
	plan := func(bank map[int]int) (ProvisionPlan, []KitLine) {
		snap := Snapshot{InvIDs: map[int]int{}, WornIDs: map[int]int{}, BankKnown: true, BankIDs: bank}
		kit := treeOrbKit(snap)
		return PlanProvision(kit, ProvisionState{Pack: map[int]int{}, Worn: map[int]int{}, Bank: bank, BankKnown: true,
			MuleKnown: true, Mule: map[int]int{}, FreeSlots: 26, Levels: map[string]int{"magic": 61}}), kit
	}
	hasBaker := func(kit []KitLine) bool {
		for _, l := range kit {
			for _, src := range l.Sources {
				if src.Kind == SourceShop && src.Shop == ArdougneBaker {
					return true
				}
			}
		}
		return false
	}
	// Sharks in the bank are drawn out and nothing is bought: a coinless account with sharks banked is not blocked.
	for _, bank := range []map[int]int{{385: 20, CoinsID: 5000}, {385: 20}, {379: 6}, {385: 3, 379: 3}} {
		p, kit := plan(bank)
		if hasBaker(kit) || p.Phase == PhaseBlocked || len(p.Tasks) != 0 {
			t.Errorf("bank %v: baker lines %v, %s", bank, hasBaker(kit), p.Summary())
		}
		if len(p.Withdraw) == 0 {
			t.Errorf("bank %v: nothing withdrawn: %s", bank, p.Summary())
		}
	}
	// Too little good food and coins in hand: the baker's cakes and bread are planned beside what the bank has.
	p, kit := plan(map[int]int{385: 2, CoinsID: 5000})
	if !hasBaker(kit) || p.Phase == PhaseBlocked {
		t.Errorf("two sharks and coins: baker lines %v, %s", hasBaker(kit), p.Summary())
	}
	baker := 0
	for _, task := range p.Tasks {
		if task.Source.Kind == SourceShop && task.Source.Shop == ArdougneBaker {
			baker++
		}
	}
	if baker == 0 {
		t.Errorf("two sharks and coins, no baker task: %s", p.Summary())
	}
	// Nothing at all: the plan says so (BLOCKED on the baker's price), the engine marks the kit failed and the gate
	// parks the quest with the reason; it never loops on it.
	p, _ = plan(map[int]int{})
	if p.Phase != PhaseBlocked {
		t.Errorf("an empty bank and no coins: %s, want a blocked plan the engine fails and the gate parks", p.Summary())
	}
}

// With the bank unread the kit waits on a read, so the baker's lines are decided by what the bank holds.
func TestTheOrbKitReadsTheBankFirst(t *testing.T) {
	s := orbSnap(map[int]int{})
	s.Ran, s.BankKnown, s.BankIDs = map[string]bool{}, false, nil
	if st := DecideTreeGnomeVillage(s); st.Kind != StepScanBank {
		t.Fatalf("bank unread: %s, want a bank read first", st.Describe())
	}
	s.BankKnown, s.BankIDs = true, map[int]int{385: 12}
	st := DecideTreeGnomeVillage(s)
	if st.Kind != StepProvision || st.KitName != KitTreeOrb {
		t.Fatalf("bank read: %s", st.Describe())
	}
	for _, l := range st.Kit {
		if len(l.Sources) != 0 {
			t.Errorf("twelve sharks banked, yet %s has a shop source", l.Name)
		}
	}
}
