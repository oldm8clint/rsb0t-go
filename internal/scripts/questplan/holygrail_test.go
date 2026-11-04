package questplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// withTitanKit is the snapshot of an account whose fight kit has been drawn: Magic 60 and the whole
// rune kit (both spell families) in the pack, which the rune gate weighs.
func withTitanKit(s Snapshot) Snapshot {
	s.Levels = map[string]int{"magic": 60}
	if s.InvIDs == nil {
		s.InvIDs = map[int]int{}
	}
	for _, l := range TitanRuneLines() {
		s.InvIDs[l.ID] += l.Qty
	}
	return s
}

func TestHolyGrailSteps(t *testing.T) {
	snap := func(stage int) Snapshot {
		return Snapshot{Journal: StatusInProgress, Stage: stage, StageKnown: true, InvIDs: map[int]int{}, WornIDs: map[int]int{},
			FreeSlots: 20, BankKnown: true, BankIDs: map[int]int{}, Ran: map[string]bool{}, Failed: map[string]bool{},
			Tile: CatherbyBank, TileKnown: true}
	}
	want := func(name string, s Snapshot, kind StepKind, custom string) {
		t.Helper()
		st := DecideHolyGrail(s)
		if why := ValidateStep(st); why != "" {
			t.Errorf("%s: malformed %s", name, why)
		}
		if st.Kind != kind || (custom != "" && st.Custom != custom) {
			t.Errorf("%s: got %s", name, st.Describe())
		}
	}
	s := snap(0)
	s.Journal = StatusNotStarted
	want("not started", s, StepTalk, "")
	want("stage 2", snap(GrailStarted), StepCustom, CustomGrailMerlin)
	want("stage 3", snap(GrailSpokeMerlin), StepCustom, CustomGrailEntrana)
	s = snap(GrailSpokeCrone)
	s.Tile = tile.Tile{X: 2849, Z: 3352} // grail-empty-2: still on Entrana after the priest
	want("stage 4 on Entrana: sail back", s, StepCustom, CustomGrailLeaveEntrana)
	s = snap(GrailSpokeCrone)
	want("no napkin: Galahad", s, StepTalk, "")
	s.InvIDs[ObjNapkin] = 1
	// The kit comes before the whistles: the Catherby bank is 200 tiles from Galahad and 800 steps of
	// route from Draynor Manor.
	want("the fight kit", s, StepProvision, "")
	s.Ran["provision:"+KitGrailFight] = true
	s = withTitanKit(s)
	want("no whistles", s, StepCustom, CustomGrailWhistles)
	s.InvIDs[ObjMagicWhistle] = 2
	s.InvIDs[ObjExcalibur] = 1
	want("wear Excalibur", s, StepEquip, "")
	s.WornIDs[ObjExcalibur] = 1
	delete(s.InvIDs, ObjExcalibur)
	want("the realm", s, StepCustom, CustomGrailRealm)
	s = snap(GrailFindPercy)
	s.InvIDs[ObjMagicWhistle] = 1
	want("Percival", s, StepCustom, CustomGrailPercival)
	s = snap(GrailGaveWhistle)
	s.InvIDs[ObjMagicWhistle] = 1
	want("take the Grail", s, StepCustom, CustomGrailTake)
	s.InvIDs[ObjHolyGrail] = 1
	want("the Grail to Arthur", s, StepTalk, "")
	s.Tile = tile.Tile{X: 2649, Z: 4684, Level: 2}
	want("the Grail held but still in the realm: blow out", s, StepCustom, CustomGrailTake)
}

func TestParseHolyGrailJournal(t *testing.T) {
	if st, ok := ParseHolyGrailJournal([]string{"@dbl@The Fisher King is very sick..."}); !ok || st != GrailFindPercy {
		t.Fatalf("%d %v", st, ok)
	}
}

// Without Excalibur the quest acquires it itself: a banked one is withdrawn; owned nowhere, 500
// coins come from the bank by the item rule, then the Lady of the Lake sells it back. It never
// waits forever, and it never buys the coins.
func TestHolyGrailReacquiresExcalibur(t *testing.T) {
	s := Snapshot{Journal: StatusInProgress, Stage: GrailSpokeCrone, StageKnown: true,
		InvIDs: map[int]int{ObjNapkin: 1, ObjMagicWhistle: 2}, WornIDs: map[int]int{},
		FreeSlots: 20, BankKnown: true, BankIDs: map[int]int{ObjExcalibur: 1}, Ran: map[string]bool{"provision:" + KitGrailFight: true},
		Failed: map[string]bool{}, Blocked: map[string]bool{}, Tile: CatherbyBank, TileKnown: true}
	s = withTitanKit(s)
	if st := DecideHolyGrail(s); st.Kind != StepWithdraw {
		t.Fatalf("banked Excalibur: %s", st.Describe())
	}
	delete(s.BankIDs, ObjExcalibur)
	st := DecideHolyGrail(s)
	if st.Kind != StepProvision || st.KitName != KitGrailExcaliburCoins || len(st.Kit) != 1 || st.Kit[0].ID != CoinsID ||
		st.Kit[0].Qty != GrailExcaliburPrice || len(st.Kit[0].Sources) != 0 {
		t.Fatalf("owned nowhere, no coins: %s", st.Describe())
	}
	s.InvIDs[CoinsID] = 600
	st = DecideHolyGrail(s)
	if st.Kind != StepTalk || st.Stop.ID != NpcLadyOfTheLake || st.UntilItemID != ObjExcalibur {
		t.Fatalf("coins held: %s", st.Describe())
	}
	s.InvIDs[CoinsID] = 10
	s.Failed["provision:"+KitGrailExcaliburCoins] = true
	if st = DecideHolyGrail(s); st.Kind != StepWait {
		t.Fatalf("no coins anywhere: %s", st.Describe())
	}
}

// A kit that failed on the way (a six minute walk timing out 90 tiles short of the bank, live 13
// Oct 2026) is tried again; one whose sources are dead (Blocked) is not waited on.
func TestHolyGrailRetriesAKitThatOnlyFailedOnTheWay(t *testing.T) {
	k := "provision:" + KitGrailFight
	s := Snapshot{Journal: StatusInProgress, Stage: GrailSpokeCrone, StageKnown: true,
		InvIDs: map[int]int{ObjNapkin: 1}, WornIDs: map[int]int{}, FreeSlots: 20, BankKnown: true, BankIDs: map[int]int{},
		Ran: map[string]bool{}, Failed: map[string]bool{k: true}, Blocked: map[string]bool{}, Tile: CatherbyBank, TileKnown: true}
	s = withTitanKit(s)
	if st := DecideHolyGrail(s); st.Kind != StepProvision || st.KitName != KitGrailFight {
		t.Fatalf("a failed-on-the-way kit: %s", st.Describe())
	}
	s.Blocked[k] = true
	if st := DecideHolyGrail(s); st.Kind != StepCustom || st.Custom != CustomGrailWhistles {
		t.Fatalf("a blocked kit is not retried: %s", st.Describe())
	}
	s.Blocked[k] = false
	s.Failed[k] = false
	s.Ran[k] = true
	if st := DecideHolyGrail(s); st.Kind != StepCustom || st.Custom != CustomGrailWhistles {
		t.Fatalf("a provisioned kit: %s", st.Describe())
	}
}

// The kit's food and runes survive the bank visits the quest makes on the way to the realm.
func TestHolyGrailKeepsItsRunes(t *testing.T) {
	keep := map[int]bool{}
	for _, id := range HolyGrail.ToolIDs {
		keep[id] = true
	}
	for _, id := range append([]int{ObjExcalibur, ObjMagicWhistle, ObjNapkin, CoinsID}, TitanRuneIDs()...) {
		if !keep[id] {
			t.Errorf("object %d is not kept across a bank visit", id)
		}
	}
	for _, l := range grailFightKit() {
		if !keep[l.ID] && !contains(grailFoods, l.ID) {
			t.Errorf("kit line %s is neither a tool nor a food", l.Name)
		}
	}
}

func contains(ids []int, id int) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// Whatever the account carries, owns or has done, the step that blows it into the realm (the
// realm custom step, from outside the realm) is only ever chosen with Excalibur WORN and with runes
// that can kill him in the pack (TitanRunesEnough) at a Magic level that has been read, the whistles
// are not fetched on runes that cannot kill him, and an account standing in the arrival pocket
// without Excalibur is sent out, never to the Titan.
func TestHolyGrailNeverEntersTheRealmWithoutExcaliburOrRunes(t *testing.T) {
	outside := []tile.Tile{CatherbyBank, {X: 2612, Z: 3474}, SixHeads, {X: 3106, Z: 3359, Level: 2}, PortSarimMonks, FaladorEastBank}
	pocket := []tile.Tile{TitanArrival, TitanNaiveTrap, TitanMeleeApproach, {X: 2800, Z: 4725}}
	beyond := []tile.Tile{TitanPassTile, GrailFisherman, {X: 2770, Z: 4700}}
	tiles := append(append(append([]tile.Tile{}, outside...), pocket...), beyond...)
	// The pack's runes: none, the review's thin stock (58 mind, 70 chaos, plenty of air and fire),
	// only air and fire (no mind or chaos: an empty mule and an empty bank), and the whole kit.
	runeNames := []string{"none", "thin", "airfire", "full"}
	runeSets := map[string]map[int]int{
		"none":    {},
		"thin":    {ObjMindRune: 58, ObjChaosRune: 70, ObjAirRune: 1000, ObjFireRune: 1000},
		"airfire": {ObjAirRune: 1000, ObjFireRune: 1000},
		"full":    {},
	}
	for _, l := range TitanRuneLines() {
		runeSets["full"][l.ID] = l.Qty
	}
	n, entered := 0, 0
	for _, stage := range []int{GrailSpokeCrone, GrailFailedTitan} {
		for _, napkin := range []bool{false, true} {
			for _, whistles := range []int{0, 1, 2} {
				for _, where := range []string{"nowhere", "pack", "worn", "bank"} {
					for _, coins := range []int{0, 600} {
						for _, bankKnown := range []bool{false, true} {
							for _, kit := range []string{"fresh", "ran", "failed", "blocked"} {
								for _, runes := range runeNames {
									for _, magic := range []int{0, 60} {
										for _, at := range tiles {
											s := Snapshot{Journal: StatusInProgress, Stage: stage, StageKnown: true, Tile: at, TileKnown: true, FreeSlots: 20,
												InvIDs: map[int]int{}, WornIDs: map[int]int{}, BankIDs: map[int]int{}, BankKnown: bankKnown,
												Ran: map[string]bool{}, Failed: map[string]bool{}, Blocked: map[string]bool{}, Levels: map[string]int{}}
											if magic > 0 {
												s.Levels["magic"] = magic
											}
											for id, q := range runeSets[runes] {
												s.InvIDs[id] = q
											}
											if napkin {
												s.InvIDs[ObjNapkin] = 1
											}
											if whistles > 0 {
												s.InvIDs[ObjMagicWhistle] = whistles
											}
											if coins > 0 {
												s.InvIDs[CoinsID] = coins
											}
											switch where {
											case "pack":
												s.InvIDs[ObjExcalibur] = 1
											case "worn":
												s.WornIDs[ObjExcalibur] = 1
											case "bank":
												if bankKnown {
													s.BankIDs[ObjExcalibur] = 1
												}
											}
											k := "provision:" + KitGrailFight
											switch kit {
											case "ran":
												s.Ran[k] = true
											case "failed":
												s.Failed[k] = true
											case "blocked":
												s.Failed[k], s.Blocked[k] = true, true
											}
											st := DecideHolyGrail(s)
											n++
											worn := s.WornIDs[ObjExcalibur] > 0
											inRealm := InGrailRealm(at)
											canKill, why := TitanRunesEnough(s.Level("magic"), s.InvIDs, 0, 0)
											if st.Kind == StepCustom && st.Custom == CustomGrailRealm && !inRealm {
												entered++
												if !worn {
													t.Fatalf("stage %d at %v, Excalibur %s, kit %s: blows into the realm without Excalibur worn: %s", stage, at, where, kit, st.Describe())
												}
												if !canKill || magic == 0 {
													t.Fatalf("stage %d at %v, runes %s, Magic %d: blows into the realm on runes that cannot kill him (%s): %s", stage, at, runes, magic, why, st.Describe())
												}
											}
											if st.Kind == StepCustom && st.Custom == CustomGrailWhistles && !inRealm && (!canKill || magic == 0) {
												t.Fatalf("stage %d at %v, runes %s, Magic %d: fetches the whistles on runes that cannot kill him (%s): %s", stage, at, runes, magic, why, st.Describe())
											}
											if inRealm && OnTitanSide(at) && !worn && !(st.Kind == StepCustom && st.Custom == CustomGrailLeaveRealm) {
												t.Fatalf("stage %d in the arrival pocket at %v without Excalibur worn: %s", stage, at, st.Describe())
											}
											if inRealm && OnTitanSide(at) && worn && !(st.Kind == StepCustom && st.Custom == CustomGrailRealm) {
												t.Fatalf("stage %d in the arrival pocket with Excalibur worn: %s", stage, st.Describe())
											}
											if inRealm && !OnTitanSide(at) && !(st.Kind == StepCustom && st.Custom == CustomGrailRealm) {
												t.Fatalf("stage %d beyond the Titan at %v: %s", stage, at, st.Describe())
											}
											if why := ValidateStep(st); why != "" {
												t.Fatalf("malformed step %s: %s", st.Describe(), why)
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
	if n < 10000 {
		t.Fatalf("only %d states walked", n)
	}
	if entered == 0 {
		t.Fatal("no state ever chose the realm: the test walks nothing")
	}
}

// A grail account that owns no Excalibur anywhere gets the kit's food and runes first, then the
// Lady's Excalibur; the order never leaves it waiting on the kit for an item the kit cannot supply.
func TestHolyGrailKitThenLadyWhenExcaliburIsLost(t *testing.T) {
	k := "provision:" + KitGrailFight
	s := Snapshot{Journal: StatusInProgress, Stage: GrailSpokeCrone, StageKnown: true,
		InvIDs: map[int]int{ObjNapkin: 1, ObjMagicWhistle: 2, CoinsID: 600}, WornIDs: map[int]int{}, FreeSlots: 20,
		BankKnown: true, BankIDs: map[int]int{}, Ran: map[string]bool{}, Failed: map[string]bool{}, Blocked: map[string]bool{},
		Tile: CatherbyBank, TileKnown: true}
	s = withTitanKit(s)
	if st := DecideHolyGrail(s); st.Kind != StepProvision || st.KitName != KitGrailFight {
		t.Fatalf("the kit first: %s", st.Describe())
	}
	s.Ran[k] = true
	if st := DecideHolyGrail(s); st.Kind != StepTalk || st.Stop.ID != NpcLadyOfTheLake {
		t.Fatalf("then the Lady: %s", st.Describe())
	}
}

func TestGrailFightKitCarriesTheRunesAndNeverBlocksOnExcalibur(t *testing.T) {
	kit := grailFightKit()
	for _, l := range kit {
		if why := ValidateKitLine(l); why != "" {
			t.Error(why)
		}
		if l.ID == ObjExcalibur && (!l.Worn || !l.Optional || !l.Untradeable) {
			t.Errorf("Excalibur must count a worn copy and never block the kit: %+v", l)
		}
	}
	// Live 13 Oct 2026: Excalibur worn, food and runes in the bank.
	st := ProvisionState{Pack: map[int]int{}, Worn: map[int]int{ObjExcalibur: 1}, Bank: map[int]int{385: 20, ObjChaosRune: 250, ObjAirRune: 500}, BankKnown: true, FreeSlots: 20}
	plan := PlanProvision(kit, st)
	if len(plan.Blocked) != 0 || plan.Phase == PhaseBlocked {
		t.Fatalf("worn Excalibur blocked the kit: %s", plan.Summary())
	}
	if plan.Phase != PhaseBank {
		t.Fatalf("food and runes are banked: %s", plan.Summary())
	}
	for _, w := range plan.Withdraw {
		if w.ID == ObjExcalibur {
			t.Errorf("it withdrew the Excalibur it already wears: %s", plan.Summary())
		}
	}
	// Owned nowhere: the kit is satisfied by what it can get (nothing), not blocked; the Lady of the
	// Lake step, not the kit, gets Excalibur back.
	st = ProvisionState{Pack: map[int]int{}, Worn: map[int]int{}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 20}
	if plan = PlanProvision(kit, st); plan.Phase != PhaseSatisfied || len(plan.Blocked) != 0 {
		t.Fatalf("an empty bank and no Excalibur: %s", plan.Summary())
	}
}

// The Titan is a one-at-a-time step, so the module says when an account is past it.
func TestHolyGrailModuleFreesItsSlotPastTheTitan(t *testing.T) {
	if HolyGrail.ExclusiveDone == nil {
		t.Fatal("The Holy Grail's Titan is a one-at-a-time step")
	}
	s := Snapshot{Journal: StatusInProgress, Stage: GrailSpokeCrone, StageKnown: true, Tile: TitanArrival, TileKnown: true}
	if HolyGrail.ExclusiveDone(s) {
		t.Error("an account in the arrival pocket is not past the Titan")
	}
	s.Tile = TitanPassTile
	if !HolyGrail.ExclusiveDone(s) {
		t.Error("an account across the bridge is past the Titan")
	}
}
