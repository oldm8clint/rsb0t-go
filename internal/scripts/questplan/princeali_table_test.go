package questplan

import (
	"fmt"
	"testing"
)

// paWant names the step a row expects: its kind, and the one thing that tells it apart (the kit, the custom step or the npc).
type paWant struct {
	kind StepKind
	name string // a kit name, a custom name, or "npc:<id>" for a talk, "use:<item>:<target>" for a use-on
	qty  int    // the custom step's Qty (-1 for any)
}

func (w paWant) String() string { return fmt.Sprintf("%s %s", w.kind, w.name) }

func paGot(st Step) paWant {
	switch st.Kind {
	case StepProvision:
		return paWant{st.Kind, st.KitName, -1}
	case StepCustom:
		return paWant{st.Kind, st.Custom, st.Qty}
	case StepTalk:
		return paWant{st.Kind, fmt.Sprintf("npc:%d", st.Stop.ID), -1}
	case StepUseOn:
		return paWant{st.Kind, fmt.Sprintf("use:%d:%d", st.ItemID, st.TargetID), -1}
	case StepPickLoc:
		return paWant{st.Kind, fmt.Sprintf("loc:%d", st.LocID), -1}
	}
	return paWant{st.Kind, st.Reason, -1}
}

// The decision for each pack, one row at a time. The first table walks an account through stage 20 by giving it, one row after the
// other, what the step before asked for: every row is the next thing the quest wants of a pack that has just got what the row
// before wanted, so the order of the tour is the table's order.
func TestDecidePrinceAliWalkthroughOfStage20(t *testing.T) {
	inv := map[int]int{CoinsID: 300}
	for i, row := range []struct {
		give []int
		want paWant
	}{
		{nil, paWant{StepProvision, KitPrinceWater, -1}},
		{[]int{ObjJugOfWater, ObjJugOfWater}, paWant{StepProvision, KitPrinceFlour, -1}},
		{[]int{ObjPotOfFlour}, paWant{StepProvision, KitPrinceBerries, -1}},
		{[]int{ObjRedberries}, paWant{StepCustom, CustomPrinceBeerBuy, PrinceBeers}},
		{[]int{ObjBeer, ObjBeer, ObjBeer}, paWant{StepProvision, KitPrinceSkirt, -1}},
		{[]int{ObjPinkSkirt}, paWant{StepProvision, KitPrinceShears, -1}},
		{[]int{ObjShears}, paWant{StepCustom, CustomSheepWool, PrinceBalls}},
		{[]int{ObjBallOfWool, ObjBallOfWool, ObjBallOfWool}, paWant{StepProvision, KitPrincePickaxe, -1}},
		{[]int{ObjBronzePickaxe}, paWant{StepCustom, CustomPrinceClay, 1}},
		{[]int{ObjClay}, paWant{StepPickLoc, fmt.Sprintf("loc:%d", LocOnionPlant), -1}},
		{[]int{ObjOnion}, paWant{StepPickLoc, fmt.Sprintf("loc:%d", LocOnionPlant), -1}},
		{[]int{ObjOnion}, paWant{StepProvision, KitPrinceFire, -1}},
		{[]int{ObjLogs, ObjTinderbox}, paWant{StepCustom, CustomPrinceAshes, 0}},
		{[]int{ObjAshes}, paWant{StepTalk, fmt.Sprintf("npc:%d", NpcAggie), -1}}, // the yellow dye
		{[]int{ObjYellowDye}, paWant{StepTalk, fmt.Sprintf("npc:%d", NpcNed), -1}},
		{[]int{ObjPlainWig}, paWant{StepUseOn, fmt.Sprintf("use:%d:%d", ObjYellowDye, ObjPlainWig), -1}},
		{[]int{ObjBlondWig}, paWant{StepTalk, fmt.Sprintf("npc:%d", NpcAggie), -1}}, // the paste
		{[]int{ObjSkinPaste}, paWant{StepUseOn, fmt.Sprintf("use:%d:%d", ObjJugOfWater, ObjClay), -1}},
		{[]int{ObjSoftClay}, paWant{StepCustom, CustomPrincePrint, 0}}, // Keli's print: a priced visit with a top-up
		{[]int{ObjKeyPrint}, paWant{StepCustom, CustomPrinceRope, PrinceRopesBeforeTie}},
		{[]int{ObjRope, ObjRope}, paWant{StepProvision, KitPrinceBar, -1}},
		{[]int{ObjBronzeBar}, paWant{StepCustom, CustomPrinceKey, 1}},
		{[]int{ObjPrinceKey}, paWant{StepTalk, fmt.Sprintf("npc:%d", NpcLeela), -1}},
	} {
		// What the content removes as the row's item arrives: the onions the dye takes, the balls the wig takes ...
		for _, id := range row.give {
			inv[id]++
		}
		switch {
		case inv[ObjYellowDye] > 0 && inv[ObjOnion] > 0 && containsInt(row.give, ObjYellowDye):
			inv[ObjOnion] = 0
		case containsInt(row.give, ObjPlainWig):
			inv[ObjBallOfWool] = 0
		case containsInt(row.give, ObjBlondWig):
			inv[ObjPlainWig], inv[ObjYellowDye] = 0, 0
		case containsInt(row.give, ObjSkinPaste):
			inv[ObjPotOfFlour], inv[ObjRedberries], inv[ObjAshes], inv[ObjJugOfWater] = 0, 0, 0, inv[ObjJugOfWater]-1
		case containsInt(row.give, ObjSoftClay):
			inv[ObjClay], inv[ObjJugOfWater] = 0, inv[ObjJugOfWater]-1
		case containsInt(row.give, ObjKeyPrint):
			inv[ObjSoftClay] = 0
		case containsInt(row.give, ObjPrinceKey):
			inv[ObjKeyPrint], inv[ObjBronzeBar] = 0, 0
		}
		got := paGot(DecidePrinceAli(paSnap(PrinceSpokeOsman, inv, true)))
		if got.kind != row.want.kind || got.name != row.want.name || (row.want.qty >= 0 && got.qty != row.want.qty) {
			t.Errorf("row %d (+%v): the quest asks for %v (qty %d), want %v (qty %d); pack %v", i, row.give, got, got.qty, row.want, row.want.qty, inv)
		}
	}
}

// Rows for the other stages and for the packs that lost something on the way.
func TestDecidePrinceAliTable(t *testing.T) {
	disguise := map[int]int{CoinsID: 300, ObjPrinceKey: 1, ObjBlondWig: 1, ObjPinkSkirt: 1, ObjSkinPaste: 1}
	with := func(base map[int]int, ids ...int) map[int]int { return paIDs(base, ids...) }
	jail := with(disguise, ObjBeer, ObjBeer, ObjBeer, ObjRope, ObjRope)
	for _, row := range []struct {
		name  string
		stage int
		inv   map[int]int
		want  paWant
	}{
		{"not started", PrinceNotStarted, map[int]int{CoinsID: 300}, paWant{StepTalk, fmt.Sprintf("npc:%d", NpcHassan), -1}},
		{"stage 10", PrinceStarted, map[int]int{CoinsID: 300}, paWant{StepTalk, fmt.Sprintf("npc:%d", NpcOsman), -1}},
		{"stage 20 with the whole disguise and the key", PrinceSpokeOsman, jail, paWant{StepTalk, fmt.Sprintf("npc:%d", NpcLeela), -1}},
		{"stage 20, the key is the only thing short, with print and bar", PrinceSpokeOsman,
			with(map[int]int{CoinsID: 300, ObjBlondWig: 1, ObjPinkSkirt: 1, ObjSkinPaste: 1, ObjBeer: 3, ObjRope: 2, ObjKeyPrint: 1, ObjBronzeBar: 1}),
			paWant{StepCustom, CustomPrinceKey, 1}},
		{"stage 20, print but no bar: the bar before Osman", PrinceSpokeOsman,
			map[int]int{CoinsID: 300, ObjBlondWig: 1, ObjPinkSkirt: 1, ObjSkinPaste: 1, ObjBeer: 3, ObjRope: 2, ObjKeyPrint: 1},
			paWant{StepProvision, KitPrinceBar, -1}},
		{"stage 30 with the beers", PrinceHasEquipment, jail, paWant{StepCustom, CustomPrinceBeer, 0}},
		{"stage 30 short of a beer: the bartender", PrinceHasEquipment, with(disguise, ObjBeer, ObjBeer, ObjRope, ObjRope), paWant{StepCustom, CustomPrinceBeerBuy, PrinceBeers}},
		{"stage 30, the key lost: Leela's replacement", PrinceHasEquipment,
			map[int]int{CoinsID: 300, ObjBlondWig: 1, ObjPinkSkirt: 1, ObjSkinPaste: 1, ObjBeer: 3, ObjRope: 2}, paWant{StepCustom, CustomPrinceKey, 0}},
		{"stage 30, the wig lost: three balls of wool, none held", PrinceHasEquipment,
			map[int]int{CoinsID: 300, ObjPrinceKey: 1, ObjPinkSkirt: 1, ObjSkinPaste: 1, ObjBeer: 3, ObjRope: 2, ObjShears: 1}, paWant{StepCustom, CustomSheepWool, PrinceBalls}},
		{"stage 30, the skirt lost", PrinceHasEquipment,
			map[int]int{CoinsID: 300, ObjPrinceKey: 1, ObjBlondWig: 1, ObjSkinPaste: 1, ObjBeer: 3, ObjRope: 2}, paWant{StepProvision, KitPrinceSkirt, -1}},
		{"stage 30, the paste lost: water first", PrinceHasEquipment,
			map[int]int{CoinsID: 300, ObjPrinceKey: 1, ObjBlondWig: 1, ObjPinkSkirt: 1, ObjBeer: 3, ObjRope: 2}, paWant{StepProvision, KitPrinceWater, -1}},
		{"stage 40 with the rope", PrinceGuardDrunk, with(disguise, ObjRope, ObjRope), paWant{StepCustom, CustomPrinceBreakout, 0}},
		{"stage 40 with one rope: the second is bought", PrinceGuardDrunk, with(disguise, ObjRope), paWant{StepCustom, CustomPrinceRope, PrinceRopesBeforeTie}},
		{"stage 50 with one rope", PrinceTiedKeli, with(disguise, ObjRope), paWant{StepCustom, CustomPrinceBreakout, 1}},
		{"stage 50 without a rope: Ned", PrinceTiedKeli, disguise, paWant{StepCustom, CustomPrinceRope, PrinceRopesAfterTie}},
		{"stage 100", PrinceSaved, map[int]int{CoinsID: 5}, paWant{StepTalk, fmt.Sprintf("npc:%d", NpcHassan), -1}},
	} {
		got := paGot(DecidePrinceAli(paSnap(row.stage, row.inv, true)))
		if got.kind != row.want.kind || got.name != row.want.name || (row.want.qty >= 0 && got.qty != row.want.qty) {
			t.Errorf("%s: the quest asks for %v (qty %d), want %v (qty %d)", row.name, got, got.qty, row.want, row.want.qty)
		}
	}
}

// The three visits to the jail compound (the print at Keli, Joe's beers, the break-out) for an account the guards would kill: food
// once, then the park, never the visit and never a plain talk to Keli. The same packs for a sound account are the visit.
func TestDecidePrinceAliTheCompoundForAWeakAccount(t *testing.T) {
	disguise := map[int]int{CoinsID: 300, ObjPrinceKey: 1, ObjBlondWig: 1, ObjPinkSkirt: 1, ObjSkinPaste: 1}
	atKeli := paIDs(map[int]int{CoinsID: 300}, ObjBlondWig, ObjPinkSkirt, ObjSkinPaste, ObjBeer, ObjBeer, ObjBeer, ObjRope, ObjRope, ObjSoftClay)
	for _, row := range []struct {
		name  string
		stage int
		inv   map[int]int
		visit string
	}{
		{"the print at stage 20", PrinceSpokeOsman, atKeli, CustomPrincePrint},
		{"Joe's beers at stage 30", PrinceHasEquipment, paIDs(disguise, ObjBeer, ObjBeer, ObjBeer, ObjRope, ObjRope), CustomPrinceBeer},
		{"the break-out at stage 40", PrinceGuardDrunk, paIDs(disguise, ObjRope, ObjRope), CustomPrinceBreakout},
		{"the break-out at stage 50", PrinceTiedKeli, paIDs(disguise, ObjRope), CustomPrinceBreakout},
	} {
		// A sound account (Defence 40, 50 hitpoints, the stats paSnap gives) makes the visit.
		sound := paSnap(row.stage, row.inv, true)
		if got := paGot(DecidePrinceAli(sound)); got.kind != StepCustom || got.name != row.visit {
			t.Errorf("%s, a sound account: %v, want the %s visit", row.name, got, row.visit)
		}
		// A weak one (Defence 1, 30 hitpoints) fetches food once.
		weak := paSnap(row.stage, row.inv, true)
		weak.MaxHP, weak.HP = 30, 30
		weak.Levels["defence"] = 1
		if got := paGot(DecidePrinceAli(weak)); got.kind != StepProvision || got.name != KitPrinceFood {
			t.Errorf("%s, a weak account: %v, want the food kit", row.name, got)
		}
		// Then it parks, and the food it was given does not change that: nothing is eaten inside a chat.
		weak.Ran["provision:"+KitPrinceFood] = true
		weak.InvIDs[385] += 4
		st := DecidePrinceAli(weak)
		if st.Kind != StepWait || st.ParkFor != PrinceOddsPark {
			t.Errorf("%s, a weak account with food: %s (park %s), want the odds park", row.name, st.Describe(), st.ParkFor)
		}
		if st.Kind == StepTalk && st.Stop.ID == NpcLadyKeli {
			t.Errorf("%s: a refused account is sent to talk to Lady Keli", row.name)
		}
	}
}
