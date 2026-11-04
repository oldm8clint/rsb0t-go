package questplan

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// grailSnap is an account at stage st with the fight kit drawn, a read bank holding bank and a pack holding inv.
func grailSnap(st int, inv, bank map[int]int) Snapshot {
	s := Snapshot{Journal: StatusInProgress, Stage: st, StageKnown: true, InvIDs: inv, WornIDs: map[int]int{}, FreeSlots: 20,
		BankKnown: true, BankIDs: bank, Ran: map[string]bool{"provision:" + KitGrailFight: true}, Failed: map[string]bool{},
		Blocked: map[string]bool{}, Tile: CatherbyBank, TileKnown: true}
	if s.InvIDs == nil {
		s.InvIDs = map[int]int{}
	}
	if s.BankIDs == nil {
		s.BankIDs = map[int]int{}
	}
	return withTitanKit(s)
}

func wantKind(t *testing.T, name string, st Step, kind StepKind) {
	t.Helper()
	if why := ValidateStep(st); why != "" {
		t.Errorf("%s: malformed step %s: %s", name, st.Describe(), why)
	}
	if st.Kind != kind {
		t.Fatalf("%s: got %s, want a %s step", name, st.Describe(), kind)
	}
}

// The napkin a bank holds is drawn, not asked for again: Galahad gives a second one at stage 4 (no bank check) and none at
// stage 7 (he counts the bank), and the bank is unread when a run begins. Live 16 Oct 2026: 23 banks held 441 napkins.
func TestTheNapkinComesFromTheBankBeforeGalahad(t *testing.T) {
	for _, stage := range []int{GrailSpokeCrone, GrailFailedTitan} {
		s := grailSnap(stage, nil, nil)
		s.BankKnown = false
		st := DecideHolyGrail(s)
		wantKind(t, "an unread bank", st, StepScanBank)
		if st.Bank != GrailNapkinBank || !strings.Contains(st.Describe(), "check the bank (") {
			t.Errorf("stage %d: the scan is %s at %v", stage, st.Describe(), st.Bank)
		}

		s = grailSnap(stage, nil, map[int]int{ObjNapkin: 19, ObjMagicWhistle: 1})
		st = DecideHolyGrail(s)
		wantKind(t, "a banked napkin", st, StepWithdraw)
		if len(st.Items) != 1 || st.Items[0].ID != ObjNapkin || st.Items[0].Qty != 1 || st.Bank != GrailNapkinBank {
			t.Errorf("stage %d: withdrew %+v at %v", stage, st.Items, st.Bank)
		}
		// What the room-making deposit of the withdrawal leaves in the pack: the tools, the runes, the food.
		keep := map[int]bool{}
		for _, id := range st.KeepIDs {
			keep[id] = true
		}
		for _, id := range append([]int{ObjMagicWhistle, ObjGrailBell, ObjExcalibur, CoinsID, 385, 379}, TitanRuneIDs()...) {
			if !keep[id] {
				t.Errorf("stage %d: the napkin withdrawal's deposit would bank object %d", stage, id)
			}
		}

		s = grailSnap(stage, nil, map[int]int{ObjMagicWhistle: 1})
		st = DecideHolyGrail(s)
		wantKind(t, "a read bank without a napkin", st, StepTalk)
		if st.Stop.ID != NpcGalahad || st.UntilItemID != ObjNapkin {
			t.Errorf("stage %d: talks to %s until item %d", stage, st.Stop.Npc, st.UntilItemID)
		}
	}
	// The napkin is only the whistle door's key: an account whose pack and bank hold both whistles never fetches one, from
	// Galahad or from the bank, and draws the whistles.
	s0 := grailSnap(GrailSpokeCrone, nil, map[int]int{ObjNapkin: 19, ObjMagicWhistle: 2})
	if st := DecideHolyGrail(s0); st.Kind != StepWithdraw || st.Items[0].ID != ObjMagicWhistle {
		t.Errorf("both whistles banked, no napkin held: %s", st.Describe())
	}
	s0 = grailSnap(GrailSpokeCrone, map[int]int{ObjMagicWhistle: 1}, map[int]int{ObjMagicWhistle: 1})
	if st := DecideHolyGrail(s0); st.Kind != StepWithdraw || st.Items[0].ID != ObjMagicWhistle || st.Items[0].Qty != 1 {
		t.Errorf("a whistle held and one banked, no napkin: %s", st.Describe())
	}
	// An account holding the napkin never goes near the bank or Galahad.
	s := grailSnap(GrailSpokeCrone, map[int]int{ObjNapkin: 1}, map[int]int{ObjNapkin: 19})
	s.BankKnown = false
	if st := DecideHolyGrail(s); st.Kind == StepScanBank && st.Bank == GrailNapkinBank || st.Kind == StepTalk && st.Stop.ID == NpcGalahad {
		t.Errorf("a held napkin: %s", st.Describe())
	}
}

// Galahad's napkin is inv_add on a pack with no room: it never arrives. Live 14-16 Oct 2026: five parks on "talk to Brother
// Galahad" ("no progress for 8 ticks"), every one of them with "0 free slots" on the line before.
func TestAFullPackBanksBeforeGalahadsNapkin(t *testing.T) {
	junk := map[int]int{1511: 1, 1205: 1, 590: 3}
	s := grailSnap(GrailSpokeCrone, map[int]int{1511: 1, 1205: 1, 590: 3}, nil)
	s.FreeSlots = 0
	st := DecideHolyGrail(s)
	wantKind(t, "a full pack with junk in it", st, StepDeposit)
	if st.Bank != GrailNapkinBank || !strings.Contains(st.Describe(), "free slot") {
		t.Errorf("the room trip is %s at %v", st.Describe(), st.Bank)
	}
	keep := map[int]bool{}
	for _, id := range st.KeepIDs {
		keep[id] = true
	}
	for id := range junk {
		if keep[id] {
			t.Errorf("object %d is kept by the room trip", id)
		}
	}
	for _, id := range append([]int{ObjMagicWhistle, ObjGrailBell, ObjExcalibur, CoinsID, 385}, TitanRuneIDs()...) {
		if !keep[id] {
			t.Errorf("the room trip would bank the quest's own object %d", id)
		}
	}

	// One free slot is enough for the napkin; a pack that is full of what the quest keeps has nothing to bank.
	s.FreeSlots = 1
	wantKind(t, "one free slot", DecideHolyGrail(s), StepTalk)
	kitOnly := grailSnap(GrailSpokeCrone, map[int]int{385: 12, 379: 4, CoinsID: 600}, nil)
	kitOnly.FreeSlots = 0
	wantKind(t, "a full pack of the quest's own items", DecideHolyGrail(kitOnly), StepTalk)
}

// The whistles the bank holds are drawn (the door at Draynor Manor is 800 steps from the Catherby bank, and gives two
// more to any account carrying fewer than two); the climb is for an account the bank cannot supply. Live 16 Oct 2026: 23
// banks held 405 whistles.
func TestTheWhistlesComeFromTheBankBeforeDraynorManor(t *testing.T) {
	base := map[int]int{ObjNapkin: 1}
	s := grailSnap(GrailSpokeCrone, base, nil)
	s.BankKnown = false
	st := DecideHolyGrail(s)
	wantKind(t, "an unread bank", st, StepScanBank)
	if st.Bank != CatherbyBank {
		t.Errorf("the whistle scan is at %v", st.Bank)
	}

	s = grailSnap(GrailSpokeCrone, base, map[int]int{ObjMagicWhistle: 17})
	st = DecideHolyGrail(s)
	wantKind(t, "banked whistles", st, StepWithdraw)
	if len(st.Items) != 1 || st.Items[0].ID != ObjMagicWhistle || st.Items[0].Qty != 2 {
		t.Errorf("two whistles wanted, withdrew %+v", st.Items)
	}

	// One in the pack and one in the bank: one is drawn. Two in the bank and none held: the trip needs both.
	s = grailSnap(GrailSpokeCrone, map[int]int{ObjNapkin: 1, ObjMagicWhistle: 1}, map[int]int{ObjMagicWhistle: 1})
	st = DecideHolyGrail(s)
	wantKind(t, "one held, one banked", st, StepWithdraw)
	if st.Items[0].Qty != 1 {
		t.Errorf("withdrew %d of a one whistle shortfall", st.Items[0].Qty)
	}
	// One banked and none held: the bank cannot supply two, so it supplies one and the next decision asks again, then the door.
	s = grailSnap(GrailSpokeCrone, base, map[int]int{ObjMagicWhistle: 1})
	if st = DecideHolyGrail(s); st.Kind != StepWithdraw || st.Items[0].Qty != 1 {
		t.Errorf("one banked: %s", st.Describe())
	}
	s = grailSnap(GrailSpokeCrone, map[int]int{ObjNapkin: 1, ObjMagicWhistle: 1}, nil)
	wantKind(t, "one held and none banked", DecideHolyGrail(s), StepCustom)
	if st = DecideHolyGrail(s); st.Custom != CustomGrailWhistles {
		t.Errorf("one held and none banked: %s", st.Describe())
	}
	// Nothing banked: Draynor Manor.
	s = grailSnap(GrailSpokeCrone, base, map[int]int{ObjNapkin: 4})
	if st = DecideHolyGrail(s); st.Kind != StepCustom || st.Custom != CustomGrailWhistles {
		t.Errorf("none banked: %s", st.Describe())
	}
	// The rune gate still comes first: no whistle is drawn on runes that cannot kill him.
	s = grailSnap(GrailSpokeCrone, base, map[int]int{ObjMagicWhistle: 17})
	s.InvIDs = map[int]int{ObjNapkin: 1}
	if st = DecideHolyGrail(s); st.Kind != StepWait || st.ParkFor != TitanParkKit {
		t.Errorf("no runes: %s", st.Describe())
	}
}

// The Fisherman's bell lies on the ground for 500 ticks; one the bank holds is drawn before the whistle, because nothing can
// be drawn once the account stands in the realm.
func TestTheBellTheBankHoldsIsDrawnBeforeTheRealm(t *testing.T) {
	inv := map[int]int{ObjNapkin: 1, ObjMagicWhistle: 2}
	s := grailSnap(GrailSpokeCrone, inv, map[int]int{ObjGrailBell: 2})
	s.WornIDs[ObjExcalibur] = 1
	st := DecideHolyGrail(s)
	wantKind(t, "a banked bell", st, StepWithdraw)
	if len(st.Items) != 1 || st.Items[0].ID != ObjGrailBell || st.Items[0].Qty != 1 {
		t.Errorf("withdrew %+v", st.Items)
	}
	s.InvIDs[ObjGrailBell] = 1
	if st = DecideHolyGrail(s); st.Kind != StepCustom || st.Custom != CustomGrailRealm {
		t.Errorf("a held bell: %s", st.Describe())
	}
	s = grailSnap(GrailSpokeCrone, inv, nil)
	s.WornIDs[ObjExcalibur] = 1
	if st = DecideHolyGrail(s); st.Kind != StepCustom || st.Custom != CustomGrailRealm {
		t.Errorf("no bell anywhere: %s", st.Describe())
	}
	// Whistles and a bell both banked: one withdrawal draws both.
	s = grailSnap(GrailSpokeCrone, map[int]int{ObjNapkin: 1}, map[int]int{ObjMagicWhistle: 9, ObjGrailBell: 1})
	st = DecideHolyGrail(s)
	wantKind(t, "whistles and a bell", st, StepWithdraw)
	if len(st.Items) != 2 || st.Items[0].ID != ObjMagicWhistle || st.Items[1].ID != ObjGrailBell {
		t.Errorf("withdrew %+v", st.Items)
	}
}

// After the Fisher King the whistle is drawn from the bank, and a Grail the bank holds is drawn before the renewed realm's
// table: `~obj_gettotal(holy_grail)` counts the bank.
func TestTheLateStagesDrawTheirWhistleAndGrailFromTheBank(t *testing.T) {
	s := grailSnap(GrailFindPercy, nil, map[int]int{ObjMagicWhistle: 5})
	st := DecideHolyGrail(s)
	wantKind(t, "Percival's whistle", st, StepWithdraw)
	if st.Items[0].ID != ObjMagicWhistle || st.Items[0].Qty != 1 {
		t.Errorf("withdrew %+v", st.Items)
	}
	s = grailSnap(GrailFindPercy, nil, nil)
	if st = DecideHolyGrail(s); st.Kind != StepCustom || st.Custom != CustomGrailWhistles {
		t.Errorf("no whistle anywhere: %s", st.Describe())
	}
	s.InvIDs[ObjMagicWhistle] = 1
	if st = DecideHolyGrail(s); st.Kind != StepCustom || st.Custom != CustomGrailPercival {
		t.Errorf("a whistle held: %s", st.Describe())
	}

	// Stage 9: the Grail in the bank is drawn; an unread bank is read first; in the realm nothing is drawn.
	s = grailSnap(GrailGaveWhistle, map[int]int{ObjMagicWhistle: 1}, map[int]int{ObjHolyGrail: 1})
	wantKind(t, "a banked Grail", DecideHolyGrail(s), StepWithdraw)
	s.BankKnown = false
	wantKind(t, "an unread bank", DecideHolyGrail(s), StepScanBank)
	s.Tile = tile.Tile{X: 2678, Z: 4715}
	if st = DecideHolyGrail(s); st.Kind != StepCustom || st.Custom != CustomGrailTake {
		t.Errorf("in the realm: %s", st.Describe())
	}
	s = grailSnap(GrailGaveWhistle, map[int]int{ObjMagicWhistle: 1}, nil)
	if st = DecideHolyGrail(s); st.Kind != StepCustom || st.Custom != CustomGrailTake {
		t.Errorf("a read bank without the Grail: %s", st.Describe())
	}
	s = grailSnap(GrailGaveWhistle, map[int]int{ObjMagicWhistle: 1, ObjHolyGrail: 1}, nil)
	if st = DecideHolyGrail(s); st.Kind != StepTalk || st.Stop.ID != NpcKingArthur {
		t.Errorf("the Grail held: %s", st.Describe())
	}
}

// The bell: a maiden within four tiles, the landing tile, and the castle's ground floor.
func TestTheBellsEarshotAndLanding(t *testing.T) {
	me := GrailBellRing
	for _, c := range []struct {
		m    tile.Tile
		want bool
	}{
		{tile.Tile{X: 2763, Z: 4690}, true},  // the spawn 3 tiles south
		{tile.Tile{X: 2759, Z: 4689}, true},  // 4 off on both axes
		{tile.Tile{X: 2760, Z: 4688}, false}, // 5 south
		{tile.Tile{X: 2769, Z: 4686}, false},
		{tile.Tile{X: 2763, Z: 4690, Level: 1}, false}, // upstairs does not hear it
	} {
		if got := GrailMaidenInEarshot(me, c.m); got != c.want {
			t.Errorf("a maiden on %v: %v, want %v", c.m, got, c.want)
		}
	}
	for _, c := range []struct {
		at   tile.Tile
		want bool
	}{
		{GrailCastleLanding, true},
		{tile.Tile{X: 2760, Z: 4691}, true},
		{GrailBellRing, false},
		{tile.Tile{X: 2762, Z: 4694}, false}, // where the bell lies
		{GrailFisherman, false},
		{tile.Tile{X: 2761, Z: 4692, Level: 1}, false},
	} {
		if got := GrailBellLanded(c.at); got != c.want {
			t.Errorf("landed at %v: %v, want %v", c.at, got, c.want)
		}
	}
	for _, c := range []struct {
		at   tile.Tile
		want bool
	}{
		{GrailCastleLanding, true},
		{GrailBellRing, false},
		{tile.Tile{X: 2762, Z: 4694}, false},
		{GrailFisherman, false},
		{TitanArrival, false},
		{tile.Tile{X: 2761, Z: 4692, Level: 1}, false},
		{tile.Tile{X: 2700, Z: 4692}, false},
	} {
		if got := InGrailCastle(c.at); got != c.want {
			t.Errorf("inside the castle at %v: %v, want %v", c.at, got, c.want)
		}
	}
	if !InGrailCastle(GrailStairStand) {
		t.Errorf("the stair stand %v is a tile of the castle's ground floor", GrailStairStand)
	}
}
