package questplan

// The Tourist Trap's floors, as pure decisions: the rescue's replay chosen once and not for ever, the food an account crosses and goes on with, the
// pack room the camp's strip needs and the first kit leaves, the guards priced before the gate, and a park that does not leave the account in the
// desert.

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// gearWorn is n distinct pieces of weapon and armour worn (ids no outfit uses), over the kit's clothes.
func gearWorn(n int, base map[int]int) map[int]int {
	w := map[int]int{}
	for k, v := range base {
		w[k] = v
	}
	for i := 0; i < n; i++ {
		w[2000+i] = 1
	}
	return w
}

// ---------------------------------------------------------------------------------------------------------------
// The rescue's camp yard.
// ---------------------------------------------------------------------------------------------------------------

// The winch, the replay, the cart and the parks between them, from the failure flags and the pack. A failure flag lasts the whole run and the plan has
// only Failed and Ran to go on, so a replay is chosen on a failed winch only until it has run.
func TestTouristTrapRescueCampDecisions(t *testing.T) {
	inv, worn := ttKit()
	inv[ObjTtMetalKey] = 1
	delete(inv, ObjBronzeBar)
	delete(inv, ObjHammer)
	delete(inv, ObjTtFeather)
	withAna := map[int]int{}
	withBarrel := map[int]int{}
	for k, v := range inv {
		withAna[k], withBarrel[k] = v, v
	}
	withAna[ObjTtAnaBarrel] = 1
	withBarrel[ObjTtBarrel] = 1
	camp := func(inv map[int]int, ran, failed map[string]bool) Step {
		return ttIn{stage: TtRescue, at: ttAtCamp, inv: inv, worn: worn, ran: ran, failed: failed}.decide()
	}
	ttCust(CustomTtWinch, 0).check(t, "nothing yet: the winch", camp(inv, nil, nil))
	ttCust(CustomTtRecapture, 0).check(t, "the winch came up empty: the replay", camp(inv, nil, map[string]bool{CustomTtWinch: true}))
	// The cart took her barrel and did not go: no Ana in the pack and nothing to find at the winch, so the replay straight away.
	ttCust(CustomTtRecapture, 0).check(t, "the cart took her barrel: the replay", camp(inv, nil, map[string]bool{CustomTtEscape: true}))
	// The replay ends with the winch, so Ana is in the pack after it (and the replay forgets the winch's and the cart's flags): the cart.
	ttCust(CustomTtEscape, 0).check(t, "the replay done: the cart", camp(withAna, map[string]bool{CustomTtRecapture: true}, nil))
	// A winch flag with a replay behind it and Ana not in the pack is not another replay: the quest parks with the reason.
	st := camp(inv, map[string]bool{CustomTtRecapture: true}, map[string]bool{CustomTtWinch: true})
	ttExpect{kind: StepWait, park: true}.check(t, "the replay ran and Ana is lost again", st)
	if !strings.Contains(st.Reason, "lost again") {
		t.Errorf("the park does not say what happened: %s", st.Reason)
	}
	st = camp(inv, map[string]bool{CustomTtRecapture: true}, map[string]bool{CustomTtEscape: true})
	ttExpect{kind: StepWait, park: true}.check(t, "the replay ran and the ride took her barrel", st)
	// A replay that failed parks the quest too.
	st = camp(inv, nil, map[string]bool{CustomTtWinch: true, CustomTtRecapture: true})
	ttExpect{kind: StepWait, park: true}.check(t, "the replay failed", st)
	if !strings.Contains(st.Reason, "replay") {
		t.Errorf("the park does not name the replay: %s", st.Reason)
	}
	// The cart failed with Ana still in the pack: the replay cannot start with her, so the quest parks with the reason, not a replay that fails at once.
	st = camp(withAna, nil, map[string]bool{CustomTtEscape: true})
	ttExpect{kind: StepWait, park: true}.check(t, "the cart failed with Ana in the pack", st)
	// An empty barrel in the pack and no Ana: the replay (it takes the barrel down with it).
	ttCust(CustomTtRecapture, 0).check(t, "a barrel and no Ana", camp(withBarrel, nil, nil))
	// The parks are for hours, not minutes.
	for _, s := range []Step{
		camp(inv, map[string]bool{CustomTtRecapture: true}, map[string]bool{CustomTtWinch: true}),
		camp(inv, nil, map[string]bool{CustomTtWinch: true, CustomTtRecapture: true}),
		camp(withAna, nil, map[string]bool{CustomTtEscape: true}),
	} {
		if s.ParkFor < TtItemPark {
			t.Errorf("the park %q is for %s", s.Reason, s.ParkFor)
		}
	}
	// Driven as the engine drives it, the chain from a failed winch ends: no state in the chain asks for the replay twice.
	replays := 0
	ran, failed := map[string]bool{}, map[string]bool{CustomTtWinch: true}
	for i := 0; i < 6; i++ {
		step := camp(inv, ran, failed)
		if step.Kind == StepWait {
			break
		}
		if step.Kind == StepCustom && step.Custom == CustomTtRecapture {
			replays++
			ran[CustomTtRecapture] = true // the replay succeeds but leaves the world as it was (Ana still not in the pack)
		}
	}
	if replays != 1 {
		t.Errorf("a replay that succeeds without Ana was chosen %d times, want once", replays)
	}
}

// ---------------------------------------------------------------------------------------------------------------
// Food.
// ---------------------------------------------------------------------------------------------------------------

func TestTouristTrapFoodFloor(t *testing.T) {
	inv, worn := ttKit()
	ate := func(food map[int]int) map[int]int {
		m := map[int]int{}
		for k, v := range inv {
			m[k] = v
		}
		delete(m, 385)
		for k, v := range food {
			m[k] = v
		}
		return m
	}
	tried := map[string]bool{"provision:" + KitTtTrip: true}
	// At the shop: food under the entry floor asks for the kit first; once the kit has failed, the crossing parks with the food in the reason.
	for _, c := range []struct {
		name string
		food map[int]int
		step ttExpect
	}{
		{"3 sharks (60) cross", map[int]int{385: 3}, ttCust(CustomTtSouth, 0)},
		{"5 bass (65) cross", map[int]int{365: 5}, ttCust(CustomTtSouth, 0)},
		{"2 sharks and 2 bass (66) cross", map[int]int{385: 2, 365: 2}, ttCust(CustomTtSouth, 0)},
	} {
		c.step.check(t, c.name, ttIn{at: ttAtShop, notStarted: true, inv: ate(c.food), worn: worn}.decide())
	}
	for _, c := range []struct {
		name string
		food map[int]int
	}{
		{"none", nil}, {"2 sharks (40)", map[int]int{385: 2}}, {"4 bass (52)", map[int]int{365: 4}}, {"a shark and a bass (33)", map[int]int{385: 1, 365: 1}},
	} {
		ttExpect{kind: StepProvision, kit: KitTtTrip}.check(t, c.name+": the kit first", ttIn{at: ttAtShop, notStarted: true, inv: ate(c.food), worn: worn}.decide())
		st := ttIn{at: ttAtShop, notStarted: true, inv: ate(c.food), worn: worn, failed: tried}.decide()
		ttExpect{kind: StepWait, park: true}.check(t, c.name+": parked once the kit failed", st)
		if !strings.Contains(st.Reason, "no food") {
			t.Errorf("%s: the park does not name the food: %s", c.name, st.Reason)
		}
		if st.ParkFor < TtItemPark {
			t.Errorf("%s: parked for %s", c.name, st.ParkFor)
		}
	}
	// The kit's food lines are not optional (a kit no source can feed fails, so the crossing sees the food is missing), carry a minimum, and the bass
	// serves the sharks.
	kit := ttIn{at: ttAtShop, notStarted: true, inv: ate(nil), worn: worn}.decide().Kit
	var shark, bass *KitLine
	for i := range kit {
		switch kit[i].ID {
		case 385:
			shark = &kit[i]
		case 365:
			bass = &kit[i]
		}
	}
	if shark == nil || bass == nil || shark.Optional || bass.Optional || shark.Min != TtFoodMin || bass.OnlyIfShort != 385 {
		t.Fatalf("the food lines are %+v and %+v", shark, bass)
	}
	if got := bass.Min * FoodItemHeal(365); got < TtFoodEntryHeal {
		t.Errorf("the least bass the kit accepts restores %d, under the floor %d", got, TtFoodEntryHeal)
	}
	if got := shark.Min * FoodItemHeal(385); got < TtFoodEntryHeal {
		t.Errorf("the least sharks the kit accept restore %d, under the floor %d", got, TtFoodEntryHeal)
	}
	// With the sharks held, the bass line is not short, and a pack holding food enough is short of nothing.
	f := ttRead(ttIn{at: ttAtShop, notStarted: true, inv: ate(map[int]int{385: 3}), worn: worn}.snapshot())
	if _, all := ttShortLines(f, ttWants(f)); slices.Contains(all, "Shark") || slices.Contains(all, "Bass") {
		t.Errorf("a pack with three sharks is short of %v", all)
	}
	// South of the pass: under a shark's worth the account turns for the bank; with a shark's worth or Ana in the pack, or after the reward's talk, it does not.
	dry := ate(nil)
	dry[ObjTtMetalKey] = 1
	ttCust(CustomTtGateOut, 0).check(t, "no food in the camp", ttIn{stage: TtEnteredCmp, at: ttAtCamp, inv: dry, worn: worn}.decide())
	ttCust(CustomTtNorth, 0).check(t, "no food in the open desert", ttIn{stage: TtFindingPin, at: ttAtDesert, inv: dry, worn: worn}.decide())
	dry[385] = 1 // 20 hitpoints: exactly the restock floor
	ttCust(CustomTtSlave, 0).check(t, "a shark in the camp", ttIn{stage: TtEnteredCmp, at: ttAtCamp, inv: dry, worn: worn}.decide())
	delete(dry, 385)
	dry[ObjTtAnaBarrel] = 1
	ttExpect{kind: StepTalk, npc: NpcTtIrena}.check(t, "Ana and no food", ttIn{stage: TtRescue, at: tile.Tile{X: 3258, Z: 3029}, inv: dry, worn: worn}.decide())
	delete(dry, ObjTtAnaBarrel)
	ttExpect{kind: StepTalk, npc: NpcTtIrena}.check(t, "the reward's talk and no food", ttIn{stage: TtReward, at: ttAtIrena, inv: dry, worn: worn}.decide())
	// The reward's talk is a few tiles south of the pass: the food floor does not park a nearly finished quest.
	rewardInv := ate(nil)
	ttCust(CustomTtSouth, 0).check(t, "the reward, no food, the kit tried", ttIn{stage: TtReward, at: ttAtShop, inv: rewardInv, worn: worn, failed: tried}.decide())
	// Food is critical only south of the pass: at the shop the account is at the bank anyway.
	st := ttIn{stage: TtGotPlans, at: ttAtShop, inv: dry, worn: worn}.decide()
	if st.Kind == StepCustom && st.Custom == CustomTtNorth {
		t.Errorf("an account at the shop was sent through the pass for food: %s", st.Describe())
	}
}

// ---------------------------------------------------------------------------------------------------------------
// The guards.
// ---------------------------------------------------------------------------------------------------------------

func TestTouristTrapPricesTheCampBeforeTheGate(t *testing.T) {
	inv, worn := ttKit()
	inv[ObjTtMetalKey] = 1
	// A strong account with six sharks is priced for a rowdy guard and one with no food or too few levels is not.
	strong := map[string]int{"attack": 60, "strength": 60, "defence": 55, "hitpoints": 70}
	weak := map[string]int{"attack": 20, "strength": 20, "defence": 20, "hitpoints": 30}
	if ok, why := TtCampFit(ttIn{levels: strong, inv: inv}.snapshot()); !ok {
		t.Errorf("a combat 80 account with six sharks is refused: %s", why)
	}
	for name, in := range map[string]ttIn{
		"weak":    {levels: weak, maxHP: 30, hp: 30, inv: inv},
		"no food": {levels: strong, inv: map[int]int{}},
	} {
		if ok, _ := TtCampFit(in.snapshot()); ok {
			t.Errorf("a %s account is priced for a rowdy guard", name)
		}
	}
	if ok, _ := TtCampFit(Snapshot{InvIDs: map[int]int{}, WornIDs: map[int]int{}, Levels: map[string]int{}}); !ok {
		t.Error("an unread account is judged")
	}
	// At the gate, outside the camp: the account walks out through the pass (with the reason) before the park; at the shop it parks for hours.
	ttWeak := ttIn{stage: TtKilledCapt, at: ttAtGateOut, inv: inv, worn: worn, levels: weak, maxHP: 30, hp: 30}
	st := ttWeak.decide()
	ttCust(CustomTtNorth, 0).check(t, "unfit at the gate, in the desert", st)
	if !strings.Contains(st.Reason, "rowdy") && !strings.Contains(st.Reason, "mining camp") {
		t.Errorf("the walk out does not say why: %s", st.Reason)
	}
	ttWeak.at = ttAtShop
	st = ttWeak.decide()
	ttExpect{kind: StepWait, park: true}.check(t, "unfit at the shop", st)
	if !strings.Contains(st.Reason, "mining camp") || st.ParkFor < TtCaptainPark {
		t.Errorf("the park is %q for %s", st.Reason, st.ParkFor)
	}
	// Inside the camp at the mine doors: out through the gate first.
	slaveWorn := map[int]int{ObjTtSlaveShirt: 1, ObjTtSlaveRobe: 1, ObjTtSlaveBoots: 1}
	slaveInv := map[int]int{}
	for k, v := range inv {
		slaveInv[k] = v
	}
	st = ttIn{stage: TtTraded, at: ttAtCamp, inv: slaveInv, worn: slaveWorn, levels: weak, maxHP: 30, hp: 30}.decide()
	if st.Kind != StepCustom || (st.Custom != CustomTtDress && st.Custom != CustomTtGateOut) {
		t.Errorf("unfit at the mine doors: %s", st.Describe())
	}
	// Carrying Ana to Irena nothing is priced.
	annaInv := map[int]int{ObjTtAnaBarrel: 1, ObjTtSkin4: 3}
	ttExpect{kind: StepTalk, npc: NpcTtIrena}.check(t, "Ana in the desert, unfit", ttIn{stage: TtRescue, at: tile.Tile{X: 3258, Z: 3029}, inv: annaInv, worn: worn, levels: weak, maxHP: 30, hp: 30}.decide())
	// The crossing south prices the guards from the first trip (the captain is the harder fight: the weak account is parked for him before the guards).
	kitInv, kitWorn := ttKit()
	st = ttIn{stage: TtKilledCapt, at: ttAtShop, inv: kitInv, worn: kitWorn, levels: weak, maxHP: 30, hp: 30}.decide()
	ttExpect{kind: StepWait, park: true}.check(t, "unfit at the shop with the key lost", st)
}

// ---------------------------------------------------------------------------------------------------------------
// The pack's room.
// ---------------------------------------------------------------------------------------------------------------

// The kit's footprint against the room: for every number of pieces of gear worn and every stage, the food, the bars and the hammer the first kit asks
// for fit in what the pack has left after the gear and the slack, the least food is kept before the bars, and the papers come back as critical at the
// plans.
func TestTouristTrapKitLeavesRoomForTheStrip(t *testing.T) {
	_, kitWorn := ttKit()
	for gear := 0; gear <= 14; gear++ {
		for _, stage := range []int{TtNotStarted, TtStarted, TtApproached, TtKilledCapt} {
			f := ttRead(ttIn{stage: stage, at: ttAtShop, inv: map[int]int{CoinsID: 900}, worn: gearWorn(gear, kitWorn), notStarted: stage == TtNotStarted}.snapshot())
			if f.gear != gear {
				t.Fatalf("gear %d read as %d", gear, f.gear)
			}
			spare := ttSpareSlots(f)
			food, bars, hammer := 0, 0, 0
			for _, w := range ttWants(f) {
				switch w.line.ID {
				case 385:
					food = w.line.Qty
				case ObjBronzeBar:
					bars = w.line.Qty
					if w.line.Min < 1 || w.line.Min > bars {
						t.Errorf("gear %d stage %d: bars %d with a minimum of %d", gear, stage, bars, w.line.Min)
					}
				case ObjHammer:
					hammer = w.line.Qty
				}
			}
			if food+bars+hammer > max(spare, 0) {
				t.Errorf("gear %d stage %d: food %d + bars %d + hammer %d is more than the %d slots spare", gear, stage, food, bars, hammer, spare)
			}
			if spare >= TtFoodMin && food < TtFoodMin {
				t.Errorf("gear %d stage %d: %d spare slots and %d food", gear, stage, spare, food)
			}
			if spare >= TtFoodMin+1+TtBarMin && (bars < TtBarMin || hammer != 1) {
				t.Errorf("gear %d stage %d: room for the papers (%d spare) and bars %d hammer %d", gear, stage, spare, bars, hammer)
			}
			if spare < TtFoodMin+1+TtBarMin && (bars != 0 || hammer != 0) {
				t.Errorf("gear %d stage %d: the papers asked for with %d spare", gear, stage, spare)
			}
		}
	}
	// The slack and the fixed lines: no gear leaves 14 slots (six sharks, four bars and the hammer fit with room over); ten pieces leave 4.
	for gear, want := range map[int]int{0: 14, 3: 11, 8: 6, 10: 4} {
		f := ttRead(ttIn{at: ttAtShop, notStarted: true, inv: map[int]int{CoinsID: 900}, worn: gearWorn(gear, kitWorn)}.snapshot())
		if got := ttSpareSlots(f); got != want {
			t.Errorf("gear %d: %d spare slots, want %d", gear, got, want)
		}
	}
	// A bare account carries everything: six sharks, four bars, a hammer.
	f := ttRead(ttIn{at: ttAtShop, notStarted: true, inv: map[int]int{CoinsID: 900}, worn: kitWorn}.snapshot())
	got := map[int]int{}
	for _, w := range ttWants(f) {
		got[w.line.ID] = w.line.Qty
	}
	if got[385] != TtFoodWant || got[ObjBronzeBar] != TtBars || got[ObjHammer] != 1 {
		t.Errorf("a bare account's kit is %v", got)
	}
	// At the plans the papers are critical whatever the room: with ten pieces of gear worn (no room for them) the kit still asks for them.
	f = ttRead(ttIn{stage: TtGotPlans, at: ttAtShop, inv: map[int]int{CoinsID: 900, ObjTtPlans: 1}, worn: gearWorn(10, kitWorn)}.snapshot())
	crit := false
	for _, w := range ttWants(f) {
		if w.line.ID == ObjBronzeBar && w.critAt > 0 {
			crit = true
		}
	}
	if !crit {
		t.Error("the bars are not critical at the plans")
	}
}

// The camp's strip: the weapon and armour come off one slot each. A pack with the room (counting the junk it drops) strips; one without parks with the
// slots named (after walking out of the desert).
func TestTouristTrapStripNeedsItsRoom(t *testing.T) {
	_, kitWorn := ttKit()
	inv := map[int]int{CoinsID: 700, ObjTtMetalKey: 1, ObjTtSkin4: 3, 385: 6}
	geared := gearWorn(6, kitWorn)
	at := ttAtGateOut
	run := func(free int, extra map[int]int) Step {
		m := map[int]int{}
		for k, v := range inv {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return ttIn{stage: TtKilledCapt, at: at, inv: m, worn: geared, free: free}.decide()
	}
	ttExpect{kind: StepCustom, custom: CustomTtDress, qty: TtDressCamp}.check(t, "6 free slots for 6 pieces", run(6, nil))
	st := run(5, nil)
	ttCust(CustomTtNorth, 0).check(t, "5 free slots for 6 pieces: out of the desert first", st)
	if !strings.Contains(st.Reason, "strip") || !strings.Contains(st.Reason, "6 free pack slots") || !strings.Contains(st.Reason, "5 free") {
		t.Errorf("the walk out does not name the slots: %s", st.Reason)
	}
	// Junk the pack can drop counts: a disclaimer and an empty skin make the room.
	ttExpect{kind: StepCustom, custom: CustomTtDress, qty: TtDressCamp}.check(t, "5 free and a disclaimer", run(5, map[int]int{ObjTtDisclaimer: 1}))
	ttExpect{kind: StepCustom, custom: CustomTtDress, qty: TtDressCamp}.check(t, "4 free and junk for two", run(4, map[int]int{ObjTtDisclaimer: 1, ObjTtSkin0: 1}))
	// The stack of reward darts is one slot however many.
	st = run(4, map[int]int{ObjTtBronzeDart: 6})
	ttCust(CustomTtNorth, 0).check(t, "4 free and a stack of darts (one slot)", st)
	// On the shop side (nothing there asks for the strip, which is the camp's) the park is where it stands.
	f := ttRead(ttIn{stage: TtKilledCapt, at: ttAtShop, inv: inv, worn: geared, free: 2}.snapshot())
	st = ttDressCamp(f, "x")
	ttExpect{kind: StepWait, park: true}.check(t, "no room, at the shop", st)
	if st.ParkFor < TtItemPark || !strings.Contains(st.Reason, "6 free pack slots") {
		t.Errorf("parked for %s: %s", st.ParkFor, st.Reason)
	}
	// In the camp the same account walks out first (the gate).
	st = ttIn{stage: TtEnteredCmp, at: ttAtCamp, inv: inv, worn: geared, free: 2}.decide()
	if st.Kind != StepCustom || st.Custom != CustomTtGateOut {
		t.Errorf("no room in the camp: %s", st.Describe())
	}
	// An account wearing nothing but clothes strips nothing.
	ttCust(CustomTtGateIn, 0).check(t, "clothes only", ttIn{stage: TtKilledCapt, at: ttAtGateOut, inv: inv, worn: kitWorn, free: 0 + 1}.decide())
}

// A park from the open desert leaves first: every park the plan makes from south of the pass walks the account out through it, and the quest parks on
// the shop side.
func TestTouristTrapParksFromTheShopSide(t *testing.T) {
	inv, worn := ttKit()
	inv[ObjTtPlans] = 1
	for _, at := range []tile.Tile{ttAtDesert, ttAtBedabin, ttAtGateOut} {
		st := ttIn{stage: TtShownPlans, at: at, inv: inv, worn: worn, levels: map[string]int{"smithing": 12}}.decide()
		ttCust(CustomTtNorth, 0).check(t, fmt.Sprintf("a level park from %v", at), st)
	}
	st := ttIn{stage: TtShownPlans, at: ttAtShop, inv: inv, worn: worn, levels: map[string]int{"smithing": 12}}.decide()
	ttExpect{kind: StepWait, park: true}.check(t, "a level park at the shop", st)
	// With Ana the account parks where it stands: the walk out is not hers to make.
	annaInv := map[int]int{ObjTtAnaBarrel: 1, ObjTtSkin4: 3, 385: 6}
	if got := ttParkSafely(ttRead(ttIn{stage: TtRescue, at: ttAtDesert, inv: annaInv, worn: worn}.snapshot()), TtItemPark, "x"); got.Kind != StepWait || got.ParkFor != TtItemPark {
		t.Errorf("with Ana: %s", got.Describe())
	}
}

// Every state of a wider sweep (the failure and run flags the rescue uses, gear worn in numbers, little room, little food, low hitpoints) answers a
// well-formed step with a reason.
func TestTouristTrapFloorsSweep(t *testing.T) {
	_, kitWorn := ttKit()
	tiles := []tile.Tile{ttAtMain, ttAtShop, ttAtIrena, ttAtDesert, ttAtBedabin, ttAtGateOut, ttAtCamp, ttAtHall, ttAtLower, ttAtDeep, ttAtCell}
	invs := []map[int]int{
		{CoinsID: 900, ObjTtSkin4: 3, ObjTtMetalKey: 1},
		{CoinsID: 900, ObjTtSkin4: 3, ObjTtMetalKey: 1, 385: 1},
		{CoinsID: 900, ObjTtSkin4: 3, ObjTtMetalKey: 1, 385: 6, ObjTtPass: 1, ObjTtBarrel: 1},
		{CoinsID: 900, ObjTtSkin4: 3, ObjTtMetalKey: 1, 365: 2, ObjTtAnaBarrel: 1},
		{CoinsID: 900, ObjTtSkin4: 3, ObjTtMetalKey: 1, 385: 6, ObjTtShirt: 2, ObjTtRobe: 2, ObjTtBoots: 2, ObjTtPlans: 1},
	}
	flags := []map[string]bool{nil, {CustomTtWinch: true}, {CustomTtWinch: true, CustomTtRecapture: true}, {CustomTtEscape: true}, {CustomTtRecapture: true}}
	n := 0
	for _, stage := range []int{TtStarted, TtKilledCapt, TtTraded, TtGotPlans, TtGivenPine, TtRescue, TtReward} {
		for _, at := range tiles {
			for _, inv := range invs {
				for _, gear := range []int{0, 4, 10} {
					for _, free := range []int{1, 6, 28} {
						for _, failed := range flags {
							for _, ran := range []map[string]bool{nil, {CustomTtRecapture: true}} {
								st := ttIn{stage: stage, at: at, inv: inv, worn: gearWorn(gear, kitWorn), free: free, failed: failed, ran: ran, hp: 4, maxHP: 40,
									levels: map[string]int{"attack": 30, "strength": 30, "defence": 30, "hitpoints": 40}}.decide()
								n++
								if why := ValidateStep(st); why != "" {
									t.Fatalf("stage %d at %v inv %v gear %d free %d failed %v ran %v: %s (%s)", stage, at, inv, gear, free, failed, ran, why, st.Describe())
								}
								if st.Kind == StepWait && strings.TrimSpace(st.Reason) == "" {
									t.Fatalf("stage %d at %v: a wait with no reason", stage, at)
								}
								if st.Kind == StepWait && st.ParkFor > 0 && st.ParkFor < TtItemPark {
									t.Fatalf("stage %d at %v: a park of %s (%s)", stage, at, st.ParkFor, st.Reason)
								}
							}
						}
					}
				}
			}
		}
	}
	t.Logf("%d states", n)
}
