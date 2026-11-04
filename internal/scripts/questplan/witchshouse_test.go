package questplan

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Pages as ball_journal.rs2 renders them, "|" breaks as separate lines, tags left in.
func TestParseWitchsHouseJournal(t *testing.T) {
	cases := []struct {
		lines []string
		want  int
	}{
		{[]string{"@dbl@I can start this quest by speaking to the @dre@little boy", "@dbl@standing by the long garden just @dre@north of taverley"}, 0},
		{[]string{"@dbl@I should find a way into the @dre@garden@dbl@ where the @dre@ball@dbl@ is"}, WitchStarted},
		{[]string{"@str@I should find a way into the garden where the ball is", "@dbl@I have found a @dre@magnet@dbl@ in a cupboard in the basement"}, WitchMagnet},
		{[]string{"@str@I have found a magnet in a cupboard in the basement", "@dbl@I have worked out how to unlock the back door to the", "@dre@garden"}, WitchUnlocked},
		{[]string{"@str@I have worked out how to unlock the back door to the", "@dbl@Now the @dre@shapeshifter@dbl@ is dead I should return the boy's @dre@ball"}, WitchDefeated},
		{[]string{"@str@... ball back.", "", "QUEST COMPLETE"}, WitchComplete},
	}
	for _, c := range cases {
		got, ok := ParseWitchsHouseJournal(c.lines)
		if !ok || got != c.want {
			t.Errorf("%q: got %d/%v, want %d", c.lines, got, ok, c.want)
		}
	}
}

func TestWitchsHouseSteps(t *testing.T) {
	outside := tile.Tile{X: 2930, Z: 3456}
	// A pack that can finish the shed: the kit's own 70 Wind Bolts and its food (witchshed_test.go judges the pack itself).
	snap := func(stage int) Snapshot {
		return Snapshot{Journal: StatusInProgress, Stage: stage, StageKnown: true,
			InvIDs:  map[int]int{ObjWitchDoorKey: 1, 562: witchCasts, 556: 2 * witchCasts, 385: witchSharks, 379: witchLobsters},
			WornIDs: map[int]int{ObjLeatherGlove: 1}, FreeSlots: 20, BankKnown: true, BankIDs: map[int]int{},
			Ran: map[string]bool{"provision:" + KitWitch: true}, Failed: map[string]bool{}, Tile: outside, TileKnown: true,
			Levels: map[string]int{"magic": 17, "defence": 20}, HP: 40, MaxHP: 40}
	}
	ns := snap(0)
	ns.Journal = StatusNotStarted
	kit := snap(1)
	kit.Ran = map[string]bool{}
	noKey := snap(1)
	delete(noKey.InvIDs, ObjWitchDoorKey)
	noGloves := snap(1)
	delete(noGloves.WornIDs, ObjLeatherGlove)
	noGloves.InvIDs[ObjLeatherGlove] = 1
	glovesNowhere := snap(1) // witch-empty-5: the kit failed before the gloves were bought
	delete(glovesNowhere.WornIDs, ObjLeatherGlove)
	glovesBanked := snap(1)
	delete(glovesBanked.WornIDs, ObjLeatherGlove)
	glovesBanked.BankIDs[ObjLeatherGlove] = 1
	staleMag := snap(1)
	staleMag.InvIDs[ObjWitchMagnet] = 1
	bankMag := snap(1)
	bankMag.BankIDs[ObjWitchMagnet] = 1
	mouse := snap(2)
	mouse.InvIDs[ObjWitchMagnet], mouse.InvIDs[ObjCheese] = 1, 2
	noCheese := snap(2)
	noCheese.InvIDs[ObjWitchMagnet] = 1
	diaryRead := snap(3)
	diaryRead.Ran[CustomWitchDiary] = true
	withKey := snap(5)
	withKey.InvIDs[ObjWitchShedKey] = 1
	ballInShed := snap(6)
	ballInShed.InvIDs[ObjWitchBall] = 1
	ballInShed.Tile = WitchBallStand
	ballOut := snap(6)
	ballOut.InvIDs[ObjWitchBall] = 1
	// The engine's fountain and cupboard count the bank (obj_gettotal): an item in the bank is withdrawn, not looked for.
	keyBanked := snap(3)
	keyBanked.Ran[CustomWitchDiary] = true
	keyBanked.BankIDs[ObjWitchShedKey] = 1
	keyAndDoorBanked := snap(3)
	delete(keyAndDoorBanked.InvIDs, ObjWitchDoorKey)
	keyAndDoorBanked.BankIDs[ObjWitchShedKey], keyAndDoorBanked.BankIDs[ObjWitchDoorKey] = 1, 30
	keyBankedUnread := snap(3) // a bank not read this run proves nothing is there
	keyBankedUnread.Ran[CustomWitchDiary] = true
	keyBankedUnread.BankKnown, keyBankedUnread.BankIDs = false, nil
	doorBanked := snap(1)
	delete(doorBanked.InvIDs, ObjWitchDoorKey)
	doorBanked.BankIDs[ObjWitchDoorKey] = 28
	ballDoorBanked := snap(6) // the way back in for the ball needs the door key too
	delete(ballDoorBanked.InvIDs, ObjWitchDoorKey)
	ballDoorBanked.BankIDs[ObjWitchDoorKey] = 28
	magBanked := snap(2)
	magBanked.BankIDs[ObjWitchMagnet] = 1
	noFood := snap(3)
	delete(noFood.InvIDs, 385)
	delete(noFood.InvIDs, 379)
	noFoodKeyBanked := snap(3)
	noFoodKeyBanked.Ran[CustomWitchDiary] = true
	noFoodKeyBanked.BankIDs[ObjWitchShedKey] = 1
	delete(noFoodKeyBanked.InvIDs, 385)
	delete(noFoodKeyBanked.InvIDs, 379)
	noFoodHolding := snap(5)
	noFoodHolding.InvIDs[ObjWitchShedKey] = 1
	delete(noFoodHolding.InvIDs, 385)
	delete(noFoodHolding.InvIDs, 379)
	noFoodInGarden := snap(3)
	noFoodInGarden.Ran[CustomWitchDiary] = true
	noFoodInGarden.Tile = GardenRing()[3]
	delete(noFoodInGarden.InvIDs, 385)
	delete(noFoodInGarden.InvIDs, 379)
	windStrikeOnly := snap(3) // the 9 Oct death: Magic 44, mind runes and a staff of air, no chaos
	delete(windStrikeOnly.InvIDs, 562)
	delete(windStrikeOnly.InvIDs, 556)
	windStrikeOnly.InvIDs[558] = 501
	windStrikeOnly.WornIDs[1381] = 1
	windStrikeOnly.Levels["magic"] = 44
	fullPack := snap(1)
	fullPack.FreeSlots = 1
	fullPack.InvIDs[1511] = 1 // logs, say: something the quest does not keep
	fullOfKeepers := snap(1)
	fullOfKeepers.FreeSlots = 1
	cases := []struct {
		name  string
		snap  Snapshot
		kind  StepKind
		check func(Step) bool
	}{
		{"not started: the boy", ns, StepTalk, func(s Step) bool { return s.Stop.ID == NpcBallBoy }},
		{"the kit first", kit, StepProvision, func(s Step) bool { return s.KitName == KitWitch }},
		{"no door key: the pot", noKey, StepCustom, func(s Step) bool { return s.Custom == CustomWitchPot }},
		{"gloves nowhere: buy them", glovesNowhere, StepProvision, func(s Step) bool {
			return len(s.Kit) == 1 && s.Kit[0].ID == ObjLeatherGlove && s.Kit[0].Sources[0].Shop.Npc == "Thessalia"
		}},
		{"gloves banked: withdraw them", glovesBanked, StepWithdraw, func(s Step) bool { return s.Items[0].ID == ObjLeatherGlove }},
		{"gloves held: wear them", noGloves, StepEquip, func(s Step) bool { return s.ItemID == ObjLeatherGlove }},
		{"stage 1: the cupboard", snap(1), StepCustom, func(s Step) bool { return s.Custom == CustomWitchMagnet }},
		{"stage 1 with a magnet: drop it", staleMag, StepCustom, func(s Step) bool { return s.Custom == CustomWitchDropMag }},
		{"stage 1 with a banked magnet: withdraw it to drop", bankMag, StepWithdraw, func(s Step) bool { return s.Items[0].ID == ObjWitchMagnet }},
		{"stage 2: the mouse", mouse, StepCustom, func(s Step) bool { return s.Custom == CustomWitchMouse }},
		{"stage 2 without cheese: Wydin, by the item rule", noCheese, StepProvision, func(s Step) bool { return len(s.Kit) == 1 && s.Kit[0].ID == ObjCheese }},
		{"stage 3: the diary first", snap(3), StepCustom, func(s Step) bool { return s.Custom == CustomWitchDiary }},
		{"stage 3 after the diary: the fountain", diaryRead, StepCustom, func(s Step) bool { return s.Custom == CustomWitchFountain }},
		{"stage 5 with the shed key: the shed", withKey, StepCustom, func(s Step) bool { return s.Custom == CustomWitchShed }},
		{"stage 6: the ball", snap(6), StepCustom, func(s Step) bool { return s.Custom == CustomWitchBall }},
		{"stage 6 with the ball in the shed: out", ballInShed, StepCustom, func(s Step) bool { return s.Custom == CustomWitchOut }},
		{"stage 6 with the ball outside: the boy", ballOut, StepTalk, func(s Step) bool { return s.Stop.ID == NpcBallBoy }},
		{"stage 3, the shed key banked: withdraw it", keyBanked, StepWithdraw, func(s Step) bool {
			return len(s.Items) == 1 && s.Items[0].ID == ObjWitchShedKey && s.Bank == FaladorWestBank
		}},
		{"stage 3, both keys banked: one withdrawal for both", keyAndDoorBanked, StepWithdraw, func(s Step) bool {
			return len(s.Items) == 2 && s.Items[0].ID == ObjWitchDoorKey && s.Items[1].ID == ObjWitchShedKey
		}},
		{"stage 3, a bank not read this run: the diary, then the fountain as before", keyBankedUnread, StepCustom, func(s Step) bool { return s.Custom == CustomWitchFountain }},
		{"stage 1, the door key banked: withdraw it, not the plant", doorBanked, StepWithdraw, func(s Step) bool {
			return len(s.Items) == 1 && s.Items[0].ID == ObjWitchDoorKey
		}},
		{"stage 6, the door key banked: withdraw it, not the plant", ballDoorBanked, StepWithdraw, func(s Step) bool {
			return len(s.Items) == 1 && s.Items[0].ID == ObjWitchDoorKey
		}},
		{"stage 2, the magnet banked: withdraw it, not the empty cupboard", magBanked, StepWithdraw, func(s Step) bool {
			return len(s.Items) == 1 && s.Items[0].ID == ObjWitchMagnet
		}},
		{"stage 3 with no food: wait, no garden", noFood, StepWait, func(s Step) bool { return strings.Contains(s.Reason, "not going for the shed") }},
		{"stage 3 with no food and the key banked: wait, the key stays banked", noFoodKeyBanked, StepWait, func(s Step) bool { return strings.Contains(s.Reason, "food") }},
		{"stage 5 holding the key with no food: wait outside", noFoodHolding, StepWait, func(s Step) bool { return strings.Contains(s.Reason, "not going for the shed") }},
		{"stage 3 already in the garden: the gate is not asked", noFoodInGarden, StepCustom, func(s Step) bool { return s.Custom == CustomWitchFountain }},
		{"stage 3 with Wind Strike only: wait", windStrikeOnly, StepWait, func(s Step) bool { return strings.Contains(s.Reason, "Wind Strike") }},
		{"a full pack with something to bank: bank it first", fullPack, StepDeposit, func(s Step) bool { return s.Bank == FaladorWestBank }},
		{"a full pack of keepers: the pickup tries and says why", fullOfKeepers, StepCustom, func(s Step) bool { return s.Custom == CustomWitchMagnet }},
	}
	for _, c := range cases {
		st := DecideWitchsHouse(c.snap)
		if why := ValidateStep(st); why != "" {
			t.Errorf("%s: malformed: %s", c.name, why)
		}
		if st.Kind != c.kind || !c.check(st) {
			t.Errorf("%s: got %s", c.name, st.Describe())
		}
	}
}

// An account that held the shed key in the bank (13 of them, 262 runs from 6 to 15 Oct 2026) was sent to the
// fountain, which answers "There is nothing in the fountain." to an owner of the key anywhere, and parked at it
// every run. The decision draws the key and the door key from the bank in one withdrawal and goes to the shed.
func TestABankedKeyAccountReachesTheShedWithoutTheFountain(t *testing.T) {
	s := Snapshot{Journal: StatusInProgress, Stage: WitchUnlocked, StageKnown: true,
		InvIDs: map[int]int{562: witchCasts, 556: 2 * witchCasts, 385: witchSharks, 379: witchLobsters}, WornIDs: map[int]int{ObjLeatherGlove: 1},
		FreeSlots: 14, BankKnown: true,
		BankIDs: map[int]int{ObjWitchShedKey: 1, ObjWitchDoorKey: 30, ObjWitchDiary: 30, CoinsID: 5000},
		Ran:     map[string]bool{"provision:" + KitWitch: true}, Failed: map[string]bool{},
		Tile: tile.Tile{X: 2946, Z: 3369}, TileKnown: true, Levels: map[string]int{"magic": 44, "defence": 20}, HP: 45, MaxHP: 45}
	var seen []string
	for i := 0; i < 6; i++ {
		st := DecideWitchsHouse(s)
		seen = append(seen, st.Describe())
		switch {
		case st.Kind == StepWithdraw:
			for _, it := range st.Items {
				s.BankIDs[it.ID] -= it.Qty
				s.InvIDs[it.ID] += it.Qty
			}
		case st.Kind == StepCustom && st.Custom == CustomWitchShed:
			if len(seen) != 2 {
				t.Fatalf("the shed after %v: want one withdrawal first", seen)
			}
			return
		default:
			t.Fatalf("step %d is %s (seen %v), want a withdrawal and then the shed", i, st.Describe(), seen)
		}
	}
	t.Fatalf("never reached the shed: %v", seen)
}

// The ring is one connected corridor in walking order, and every exposed tile the study found
// lies on it.
func TestGardenRingIsContiguousAndHoldsEveryGap(t *testing.T) {
	ring := GardenRing()
	for i := 1; i < len(ring); i++ {
		if tile.Chebyshev(ring[i-1], ring[i]) != 1 {
			t.Fatalf("ring breaks between %v and %v", ring[i-1], ring[i])
		}
	}
	for gi, g := range GardenGaps {
		for x := g.X0; x <= g.X1; x++ {
			if gapOf(tile.Tile{X: x, Z: g.Z}) != gi {
				t.Errorf("gap %d tile %d,%d not recognised", gi, x, g.Z)
			}
		}
	}
	for _, r := range ring {
		if !InWitchGarden(r) {
			t.Errorf("ring tile %v is outside the garden rectangle", r)
		}
	}
}

// From the back door to the fountain the walk crosses all seven gaps, each from the safe tile
// just before it to the safe tile just after; the reverse walk crosses them the other way.
func TestPlanGardenWalkCrossesEachGapOnce(t *testing.T) {
	hops := PlanGardenWalk(GardenRing()[0], WitchFountStand)
	var gaps []int
	for _, h := range hops {
		if h.Gap >= 0 {
			gaps = append(gaps, h.Gap)
			if gapOf(h.Stand) >= 0 || gapOf(h.Cross) >= 0 {
				t.Errorf("hop %+v stands or lands on an exposed tile", h)
			}
		}
	}
	if len(gaps) != 7 {
		t.Fatalf("crossed %v, want all seven", gaps)
	}
	last := hops[len(hops)-1]
	if (last.Gap < 0 && last.Stand != WitchFountStand) || (last.Gap >= 0 && last.Cross != WitchFountStand) {
		t.Fatalf("the walk ends at %+v, not the fountain", last)
	}
	// To the shed door only the four south gaps are crossed.
	n := 0
	for _, h := range PlanGardenWalk(GardenRing()[0], WitchShedStand) {
		if h.Gap >= 0 {
			n++
		}
	}
	if n != 4 {
		t.Fatalf("to the shed: %d gaps, want 4", n)
	}
	back := PlanGardenWalk(WitchShedStand, GardenRing()[0])
	if len(back) == 0 || back[0].Gap != 3 {
		t.Fatalf("the way back should cross the easternmost south gap first: %+v", back)
	}
}

func TestWitchClear(t *testing.T) {
	g := GardenGaps[1] // x 2910-2914, window 2909-2915
	cases := []struct {
		x, dx, ticks int
		want         bool
	}{
		{2912, 0, 3, false},  // inside the window
		{2920, 0, 3, false},  // five out is not more than the crossing's three ticks and two spare
		{2921, 0, 3, true},   // six out
		{2918, -1, 3, false}, // three out and coming
		{2918, 1, 3, true},   // walking away with room before her patrol end
		{2906, -1, 3, true},  // west of it, walking away from 2906: 3 + 2*2 = 7
		{2905, -1, 3, true},  // 4 + 2*1 = 6 > 5
		{2904, -1, 3, false}, // at her patrol end: 5 + 0, she turns straight back
	}
	for _, c := range cases {
		if got := WitchClear(g, c.x, c.dx, c.ticks); got != c.want {
			t.Errorf("witch x %d dx %d ticks %d: got %v, want %v", c.x, c.dx, c.ticks, got, c.want)
		}
	}
}

func TestWitchKitPicksWindBoltAtMagic17(t *testing.T) {
	s := Snapshot{Levels: map[string]int{"magic": 17}, WornIDs: map[int]int{}, InvIDs: map[int]int{}}
	has := func(kit []KitLine, id int) bool {
		for _, l := range kit {
			if l.ID == id {
				return true
			}
		}
		return false
	}
	if k := witchKit(s, 2); !has(k, 562) || has(k, 554) || !has(k, 556) || !has(k, ObjCheese) {
		t.Fatalf("magic 17, no staff: %+v", k)
	}
	s.Levels["magic"] = 13
	s.WornIDs[1381] = 1
	if k := witchKit(s, 0); has(k, 562) || !has(k, 554) || has(k, 556) || has(k, ObjCheese) {
		t.Fatalf("magic 13 with a staff, no cheese: %+v", k)
	}
	p := PlanProvision(witchKit(Snapshot{Levels: map[string]int{"magic": 17}}, 2), ProvisionState{
		BankKnown: true, Bank: map[int]int{CoinsID: 5000}, MuleKnown: true, FreeSlots: 20})
	if p.Phase != PhaseBank || len(p.Blocked) != 0 {
		t.Fatalf("empty mule: %s", p.Summary())
	}
}
