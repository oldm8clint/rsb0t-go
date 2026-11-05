package questplan

import (
	"slices"
	"testing"
)

// The kits through the real planner (PlanProvision) on the stock the fleet actually has: an account with an empty bank, the mule's published
// bank (the pie dishes, the redberries, the iron bars and the sharks it holds, none of the flour, the buckets or the pickaxes), the shops
// and the ground. What is asked of whom, in one bundle, and what the pack has room for.

// sqPublished is the mule's published bank as read on 20 Oct 2026, the items the quest names (counts rounded down).
func sqPublished() map[int]int {
	return map[int]int{ObjSqPieDish: 267, ObjSqRedberries: 77, ObjSqIronBar: 14135, ObjShark: 84783, ObjBass: 1325281, CoinsID: 123042169}
}

func TestThePieKitAsksTheMuleOnceForWhatItHoldsAndTheShopsForTheRest(t *testing.T) {
	s := Snapshot{InvIDs: map[int]int{CoinsID: 200}}
	kit := sqPieKitLines(s)
	plan := PlanProvision(kit, ProvisionState{Pack: map[int]int{CoinsID: 200}, Bank: map[int]int{}, BankKnown: true, Mule: sqPublished(), MuleKnown: true,
		FreeSlots: 27, Levels: map[string]int{}, Exhausted: map[string]bool{}})
	if plan.Phase != PhaseMule {
		t.Fatalf("the plan is %s: %s", plan.Phase, plan.Summary())
	}
	// One bundle: the dish and the berries, nothing the mule does not publish (flour, a bucket), and never more than a trade screen.
	ask := map[int]int{}
	total := 0
	for _, c := range plan.MuleAsk {
		ask[c.ID] = c.Count
		total += c.Count
	}
	if ask[ObjSqPieDish] != 1 || ask[ObjSqRedberries] != 3 || ask[ObjSqPotOfFlour] != 0 || ask[ObjSqBucket] != 0 || total > MaxBundleSlots {
		t.Errorf("the mule is asked for %v", plan.MuleAsk)
	}
	// What the mule does not hold comes from the shops and the ground in the item rule's order: the bucket from the palace floor first, the
	// flour from Wydin.
	var sources []string
	for _, task := range plan.Tasks {
		sources = append(sources, task.Line.Name+":"+string(task.Source.Kind))
	}
	if !slices.Contains(sources, "Pot of flour:shop") || !slices.Contains(sources, "Bucket:ground") {
		t.Errorf("the tasks are %v", sources)
	}
	for _, task := range plan.Tasks {
		if task.Line.ID == ObjSqPotOfFlour && task.Source.Shop.Npc != "Wydin" {
			t.Errorf("the flour comes from %q", task.Source.Shop.Npc)
		}
	}
	// A bucket of water already in the bank removes the bucket from the kit's needs (the line serves it, OnlyIfShort).
	kit = sqPieKitLines(Snapshot{InvIDs: map[int]int{ObjSqBucketWater: 1}})
	for _, l := range kit {
		if l.ID == ObjSqBucket || l.ID == ObjSqBucketWater {
			t.Errorf("a bucket of water held and the kit still lists %s", l.Name)
		}
	}
}

func TestTheOreKitAsksTheMuleOnceAndFitsThePack(t *testing.T) {
	// The dearest account: Defence 1 prices the visit at 230 hitpoints of food, twelve sharks.
	s := sqAccount(1, 10, 0)
	trip := SquireTripPrice(s)
	kit := sqOreKitLines(s, trip, true)
	plan := PlanProvision(kit, ProvisionState{Pack: map[int]int{CoinsID: 100}, Bank: map[int]int{}, BankKnown: true, Mule: sqPublished(), MuleKnown: true,
		FreeSlots: 27, Levels: map[string]int{"mining": 10}, Exhausted: map[string]bool{}})
	if plan.Phase != PhaseMule {
		t.Fatalf("the plan is %s: %s", plan.Phase, plan.Summary())
	}
	ask := map[int]int{}
	total := 0
	for _, c := range plan.MuleAsk {
		ask[c.ID] = c.Count
		total += c.Count
	}
	if ask[ObjSqIronBar] != 2 || ask[ObjShark] != min(ceilDiv(trip.FoodWanted, 20), SqMaxFoodItems) || total > MaxBundleSlots {
		t.Errorf("the mule is asked for %v (food wanted %d)", plan.MuleAsk, trip.FoodWanted)
	}
	// The pickaxe is the bronze one from the table at the mine or Bob's shop: the mule holds none.
	if len(plan.Tasks) != 1 || plan.Tasks[0].Line.ID != ObjBronzePickaxe {
		t.Errorf("the pickaxe's tasks are %v", plan.Tasks)
	}
	// The pack with everything the dungeon wants: two bars, a pickaxe, the sharks and the coins, and room for the ore.
	slots := 2 + 1 + min(ceilDiv(trip.FoodWanted, 20), SqMaxFoodItems) + 1 + 1
	if slots > 28 {
		t.Errorf("the dungeon's pack is %d slots", slots)
	}
	// The second kind of food is bass, in a kit of its own, and asks for nothing the first covered.
	covered := sqAccount(1, 10, 1265)
	covered.InvIDs[ObjShark] = 14
	if k := sqOreKitLines(covered, SquireTripPrice(covered), false); len(k) != 0 {
		t.Errorf("a pack that holds the food asks for more: %+v", k)
	}
	half := sqAccount(40, 10, 1265)
	half.InvIDs[ObjShark] = 3
	k := sqOreKitLines(half, SquireTripPrice(half), false)
	if len(k) != 1 || k[0].ID != ObjBass || !k[0].Optional || k[0].Qty > SqMaxFoodItems {
		t.Errorf("the second food kit is %+v", k)
	}
	// The first kit leaves out what is held: with the bars and the pickaxe in the pack it is the sharks alone.
	held := sqAccount(40, 10, 1265)
	held.InvIDs[ObjSqIronBar] = 2
	if k := sqOreKitLines(held, SquireTripPrice(held), true); len(k) != 1 || k[0].ID != ObjShark {
		t.Errorf("the kit with the bars and the pickaxe held is %+v", k)
	}
	// With the ore in hand the dungeon's kit is empty: nothing is fetched for a visit already made.
	done := sqAccount(40, 10, 0)
	done.InvIDs[ObjSqBluriteOre] = 1
	if k := sqOreKitLines(done, SquireTripPrice(done), true); len(k) != 1 || k[0].ID != ObjSqIronBar {
		t.Errorf("the kit with the ore held is %+v", k)
	}
}

// The keep list leaves every item of the chain in the pack through a bank visit and the floor bite leaves the pie and the sword alone.
func TestTheKeepListHoldsTheWholeChain(t *testing.T) {
	keep := SqKeepIDs()
	for _, id := range []int{ObjSqPortrait, ObjSqSword, ObjSqBluriteOre, ObjSqIronBar, ObjSqPieDish, ObjSqPieShell, ObjSqUncookedPie, ObjSqRedberryPie, ObjSqBurntPie,
		ObjSqPastryDough, ObjSqRedberries, ObjSqPotOfFlour, ObjSqEmptyPot, ObjSqBucket, ObjSqBucketWater, CoinsID, ObjShark, ObjBass} {
		if !slices.Contains(keep, id) {
			t.Errorf("item %d is not in the keep list", id)
		}
	}
	seen := map[int]bool{}
	for _, id := range keep {
		if seen[id] {
			t.Errorf("item %d is in the keep list twice", id)
		}
		seen[id] = true
	}
	// The foods are the sustain's; the tools are not food.
	if !slices.Equal(KnightsSword.Sustain.FoodIDs, SqFoodIDs()) || KnightsSword.Sustain.EatAt != SqEatAt {
		t.Errorf("the sustain is %+v", KnightsSword.Sustain)
	}
	if !slices.Equal(KnightsSword.ToolIDs, SqToolIDs()) {
		t.Errorf("the module's tools are not the plan's")
	}
}
