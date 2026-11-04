package waterfallplan

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
)

var allLegs = []Leg{
	LegDone, LegWait, LegKitTomb, LegKitDungeon, LegAlmera, LegRaft, LegHudon, LegSwimOff,
	LegBookcase, LegReadBook, LegTGVDown, LegGolrieKey, LegGolrieGate, LegGolrie, LegTGVUp,
	LegTombstone, LegChest, LegCoffin, LegTombUp, LegRopeRock, LegRopeTree, LegBarrel,
	LegLedgeDoor, LegBaxKey, LegLaneIn, LegPuzzleDoor, LegPillars, LegStatue, LegFreeSlots,
	LegChalice, LegWashout, LegExitRaised, LegExitPuzzle, LegExitSouth, LegLaneOut, LegEntryDoor,
	LegStrandedBnk, LegMakeRoom, LegHeal,
}

// Every leg Decide can return becomes a well-formed engine step, and none falls through to the
// "no step for leg" wait.
func TestEveryLegMapsToAValidEngineStep(t *testing.T) {
	s := Snapshot{Inv: map[int]int{}, Worn: map[int]int{}, IsFood: IsFood, Settings: DefaultSettings(),
		StageKnown: true, Stage: StageReadBook, BankKnown: true, Bank: map[int]int{}}
	for _, l := range allLegs {
		step := StepFor(Decision{Leg: l, Why: "test"}, s)
		if why := questplan.ValidateStep(step); why != "" {
			t.Errorf("%s: %s", l, why)
		}
		if step.Kind == questplan.StepWait && strings.Contains(step.Reason, "no step for leg") {
			t.Errorf("%s has no step", l)
		}
	}
}

func engineSnap(stage Stage, at tile.Tile) questplan.Snapshot {
	return questplan.Snapshot{
		Journal: questplan.StatusInProgress, Stage: int(stage), StageKnown: true,
		Tile: at, TileKnown: true, InvIDs: map[int]int{}, WornIDs: map[int]int{},
		FreeSlots: 28, HP: 20, MaxHP: 20, Failed: map[string]bool{}, Ran: map[string]bool{},
	}
}

func TestAWornWeaponIsStrippedBeforeTheTombTrip(t *testing.T) {
	s := engineSnap(StageReadBook, ArdougneBank)
	s.WornIDs[1277] = 1 // bronze sword
	step := DecideStep(s, DefaultSettings())
	if step.Kind != questplan.StepCustom || step.Custom != CustomStrip {
		t.Fatalf("got %s", step.Describe())
	}
}

func TestTheTombKitProvisionsByTheItemRuleThenTopsFoodUp(t *testing.T) {
	s := engineSnap(StageReadBook, ArdougneBank)
	step := DecideStep(s, DefaultSettings())
	if step.Kind != questplan.StepProvision || step.KitName != KitTomb {
		t.Fatalf("first: %s", step.Describe())
	}
	// The tomb kit ran and still left the pack below the trip's food minimum: the item rule's
	// own-skill source next (cakes from the market stall), and only once that failed the shop.
	s.Ran["provision:"+KitTomb] = true
	s.BankKnown, s.BankIDs = true, map[int]int{}
	// At 20 Hitpoints the stall's guards are a death (the twelfth EMPTY lab run): the shop.
	step = DecideStep(s, DefaultSettings())
	if step.Kind != questplan.StepProvision || step.KitName != KitTombFood+ShopSuffix || step.Kit[0].ID != FoodBread {
		t.Fatalf("second at 20 hp: %s", step.Describe())
	}
	s.HP, s.MaxHP = CakeStallMinHP, CakeStallMinHP
	step = DecideStep(s, DefaultSettings())
	if step.Kind != questplan.StepProvision || step.KitName != KitTombFood || step.Kit[0].ID != FoodCake ||
		step.Kit[0].Sources[0].Kind != questplan.SourceSkill {
		t.Fatalf("second: %s", step.Describe())
	}
	// Sized by healing: the trip's food in units of a bread's 5 hitpoints, in cakes of 12.
	if want := ItemsFor(FoodCake, DefaultSettings().TombFood); step.Kit[0].Qty != want {
		t.Fatalf("cakes %d, want %d", step.Kit[0].Qty, want)
	}
	s.Failed["provision:"+KitTombFood] = true
	step = DecideStep(s, DefaultSettings())
	if step.Kind != questplan.StepProvision || step.KitName != KitTombFood+ShopSuffix || step.Kit[0].ID != FoodBread {
		t.Fatalf("third: %s", step.Describe())
	}
}

func TestFoodIsSizedByHealing(t *testing.T) {
	if u := FoodUnits(map[int]int{385: 2, 2309: 1, 1891: 1}, IsFood); u != (40+5+12)/FoodUnit {
		t.Fatalf("two sharks, a bread and a cake: %d units", u)
	}
	if n := ItemsFor(385, 18); n != 5 {
		t.Fatalf("18 units of shark: %d", n)
	}
	if n := ItemsFor(FoodBread, 18); n != 18 {
		t.Fatalf("18 units of bread: %d", n)
	}
}

func TestAShortKitWaitsAndThenParksInsteadOfSettingOff(t *testing.T) {
	s := engineSnap(StageReadBook, ArdougneBank)
	s.Failed["provision:"+KitTomb] = true
	step := DecideStep(s, DefaultSettings())
	if step.Kind != questplan.StepWait {
		t.Fatalf("got %s", step.Describe())
	}
}

// AIO-PLAN A8: every rung ships a test with an EMPTY published mule bank proving it picks a
// non-mule source. The dungeon kit's rope and runes fall to shops; the quest's own relics are
// never asked of the mule or a shop.
func TestTheDungeonKitWithAnEmptyMuleFallsToShops(t *testing.T) {
	ws := Snapshot{Inv: map[int]int{ItemAmulet: 1, ItemFullUrn: 1}, Worn: map[int]int{}, IsFood: IsFood,
		Settings: DefaultSettings(), BankKnown: true, Bank: map[int]int{ItemCoins: 500}}
	kit := loadoutKit(DungeonLoadout(StageEnteredTomb, DefaultSettings()), ws)
	plan := questplan.PlanProvision(kit, questplan.ProvisionState{
		Pack: ws.Inv, BankKnown: true, Bank: ws.Bank, MuleKnown: true, Mule: map[int]int{}, FreeSlots: 26,
	})
	if len(plan.MuleAsk) != 0 {
		t.Fatalf("asked an empty mule: %+v", plan.MuleAsk)
	}
	shops := map[int]string{}
	for _, task := range plan.Tasks {
		shops[task.Line.ID] = task.Source.Shop.Npc
	}
	if shops[ItemRope] != "Aemad" || shops[ItemAirRune] != "Betty" || shops[ItemEarthRune] != "Betty" || shops[ItemWaterRune] != "Betty" {
		t.Fatalf("shops %v (plan %s)", shops, plan.Summary())
	}
	if plan.Phase != questplan.PhaseBank {
		t.Fatalf("the shop coins come out of the bank first: %s", plan.Summary())
	}
}

func TestTheDungeonKitWithAStockedMuleIsOneAsk(t *testing.T) {
	ws := Snapshot{Inv: map[int]int{ItemAmulet: 1, ItemFullUrn: 1}, Worn: map[int]int{}, IsFood: IsFood,
		Settings: DefaultSettings(), BankKnown: true, Bank: map[int]int{}}
	kit := loadoutKit(DungeonLoadout(StageEnteredTomb, DefaultSettings()), ws)
	plan := questplan.PlanProvision(kit, questplan.ProvisionState{
		Pack: ws.Inv, BankKnown: true, Bank: ws.Bank, MuleKnown: true, FreeSlots: 26,
		Mule: map[int]int{ItemRope: 11, ItemAirRune: 48890, ItemEarthRune: 17249, ItemWaterRune: 5055, 385: 18541},
	})
	if plan.Phase != questplan.PhaseMule {
		t.Fatalf("plan %s", plan.Summary())
	}
	got := map[int]int{}
	for _, c := range plan.MuleAsk {
		got[c.ID] = c.Count
	}
	if got[ItemRope] != 1 || got[ItemAirRune] != 6 || got[385] != ItemsFor(385, DefaultSettings().DungeonFood) {
		t.Fatalf("mule ask %v", got)
	}
	for _, c := range plan.MuleAsk {
		if c.ID == ItemAmulet || c.ID == ItemFullUrn || c.ID == ItemBaxKey {
			t.Fatalf("asked the mule for a quest item: %+v", c)
		}
	}
}

func TestAWornAmuletIsTakenOffForTheDungeonTrip(t *testing.T) {
	s := engineSnap(StageEnteredTomb, ArdougneBank)
	s.WornIDs[ItemAmulet] = 1
	s.InvIDs[ItemFullUrn] = 1
	step := DecideStep(s, DefaultSettings())
	if step.Kind != questplan.StepCustom || step.Custom != CustomStrip {
		t.Fatalf("got %s", step.Describe())
	}
}

func TestInThePillarRoomTheStatueIsOnlyReachedThroughThePillarStep(t *testing.T) {
	s := engineSnap(StageEnteredPuzzle, tile.Tile{X: 2565, Z: 9910})
	s.InvIDs[ItemFullUrn], s.InvIDs[ItemAmulet], s.InvIDs[ItemBaxKey] = 1, 1, 1
	s.InvIDs[ItemAirRune], s.InvIDs[ItemEarthRune], s.InvIDs[ItemWaterRune] = 6, 6, 6
	step := DecideStep(s, DefaultSettings())
	if step.Kind != questplan.StepCustom || step.Custom != CustomPillars {
		t.Fatalf("got %s", step.Describe())
	}
	// A rune kind at zero is NOT a reason to leave on its own: every one of that kind may
	// already be placed (the ninth EMPTY lab run walked out at 17/18 on exactly that reading).
	// The executor re-checks each pair and is the one that knows.
	delete(s.InvIDs, ItemWaterRune)
	step = DecideStep(s, DefaultSettings())
	if step.Kind != questplan.StepCustom || step.Custom != CustomPillars {
		t.Fatalf("a spent rune kind left the room: %s", step.Describe())
	}
	// Once the executor has failed (a pair it could not place), the room is left, from its own
	// side of the door.
	s.Failed[CustomPillars] = true
	step = DecideStep(s, DefaultSettings())
	if step.Kind == questplan.StepCustom && step.Custom == CustomPillars {
		t.Fatalf("tried the pillars again after they failed: %s", step.Describe())
	}
	if step.Kind != questplan.StepUseOn || step.Anchor != PuzzleDoorNorth {
		t.Fatalf("left the room by %s (anchor %+v), want the key on the door from its north stand", step.Describe(), step.Anchor)
	}
}

func TestAKeyedDoorIsUsedFromTheAccountsOwnSide(t *testing.T) {
	s := engineSnap(StageEnteredPuzzle, tile.Tile{X: 2567, Z: 9897})
	s.InvIDs[ItemFullUrn], s.InvIDs[ItemAmulet], s.InvIDs[ItemBaxKey] = 1, 1, 1
	s.InvIDs[ItemAirRune], s.InvIDs[ItemEarthRune], s.InvIDs[ItemWaterRune] = 6, 6, 6
	step := DecideStep(s, DefaultSettings())
	if step.Kind != questplan.StepUseOn || step.Anchor != PuzzleDoorSouth {
		t.Fatalf("in the corridor: %s (anchor %+v), want the puzzle door from its south stand", step.Describe(), step.Anchor)
	}
}

func TestCompleteOnTheMainlandIsDone(t *testing.T) {
	s := engineSnap(StageComplete, ArdougneBank)
	s.Journal = questplan.StatusComplete
	if step := DecideStep(s, DefaultSettings()); step.Kind != questplan.StepDone {
		t.Fatalf("got %s", step.Describe())
	}
	// Complete but still in the raised room: walk out first (no washout after stage 10).
	s.Tile = RaisedLanding
	s.InvIDs[ItemBaxKey] = 1
	if step := DecideStep(s, DefaultSettings()); step.Kind == questplan.StepDone {
		t.Fatal("done while still in the dungeon")
	}
}

func TestTheModuleValidates(t *testing.T) {
	m := Module(DefaultSettings())
	if m.Record.ID != QuestID || m.ReadStage == nil || m.Decide == nil || !m.BankSet {
		t.Fatalf("module %+v", m.Record)
	}
	if st, ok := m.ReadStage([]string{"I spoke to Almera in a house close to the Baxtorian waterfall"}); !ok || st != int(StageStarted) {
		t.Fatalf("stage %d %v", st, ok)
	}
}

// A food top-up that could not buy the whole shelf is not a short kit once the pack holds the
// trip's floor (the seventh EMPTY lab run held 14 bread against a floor of 10 and parked).
func TestAFailedFoodTopUpAboveTheFloorIsNotShort(t *testing.T) {
	set := DefaultSettings()
	s := questplan.Snapshot{Journal: questplan.StatusInProgress, Stage: int(StageEnteredTomb), StageKnown: true,
		InvIDs: map[int]int{FoodBread: 14}, WornIDs: map[int]int{}, Failed: map[string]bool{"provision:" + KitDungeonFood: true}}
	if ws := FromEngine(s, set); ws.KitShort {
		t.Fatalf("14 bread against a floor of %d reads short", DungeonLoadout(StageEnteredTomb, set).FoodMin)
	}
	s.InvIDs[FoodBread] = 3
	if ws := FromEngine(s, set); ws.KitShort {
		t.Fatalf("3 bread with only the cake stall failed reads short: the baker is still to try")
	}
	s.Failed["provision:"+KitDungeonFood+ShopSuffix] = true
	if ws := FromEngine(s, set); !ws.KitShort {
		t.Fatalf("3 bread with the stall and the baker both failed must read short")
	}
}

// dungeonPack is what the live "acct" carried through the ledge door on 2026-09-26: the
// dungeon kit (rope, the eighteen runes, the amulet and the urn) and a pack filled to the last
// slot with the mule's cakes.
func dungeonPack() map[int]int {
	return map[int]int{ItemRope: 1, ItemAmulet: 1, ItemFullUrn: 1, ItemAirRune: 6, ItemEarthRune: 6, ItemWaterRune: 6,
		1891: 18, ItemCoins: 40}
}

func fullSnap(stage Stage, at tile.Tile, inv map[int]int) Snapshot {
	return Snapshot{Status: questplan.StatusInProgress, Stage: stage, StageKnown: true, Tile: at, TileKnown: true,
		Inv: inv, Worn: map[int]int{}, IsFood: IsFood, FreeSlots: 0, HP: 27, MaxHP: 27, Settings: DefaultSettings(),
		BankKnown: true, Bank: map[int]int{}}
}

// The live livelock: in the dungeon with no free slot for the key, the escape opened the entry
// door back to the ledge and the ledge opened it again, 158 times. With food in the pack a slot
// is made where the account stands; the step asks for exactly one.
func TestAFullPackInTheDungeonMakesRoomInsteadOfLeaving(t *testing.T) {
	s := fullSnap(StageEnteredWaterfall, DungeonEntry, dungeonPack())
	d := Decide(s)
	if d.Leg != LegMakeRoom {
		t.Fatalf("0 free slots and cakes at the dungeon entry: %s, want %s", d, LegMakeRoom)
	}
	st := StepFor(d, s)
	if st.Kind != questplan.StepCustom || st.Custom != CustomFreeSlots || st.Qty != 1 {
		t.Fatalf("step %+v, want the free-slots custom for one slot", st)
	}
	if why := questplan.ValidateStep(st); why != "" {
		t.Fatal(why)
	}
	s.FreeSlots = 1
	if d := Decide(s); d.Leg != LegBaxKey {
		t.Fatalf("with a slot made: %s, want the key crate", d)
	}
	// The chalice still asks for its five.
	if st := StepFor(Decision{Leg: LegFreeSlots}, s); st.Qty != RewardSlots {
		t.Fatalf("the reward's free slots: %d", st.Qty)
	}
}

// With no food to make a slot, the account must not go through the ledge door at all: the ledge
// takes the barrel home, and the mainland banks what the trip does not carry. Walked end to end,
// the ledge door is never chosen twice.
func TestAFullPackWithNoFoodBanksInsteadOfFlippingThroughTheLedgeDoor(t *testing.T) {
	inv := dungeonPack()
	delete(inv, 1891)
	inv[1511] = 18 // logs: something the trip does not carry
	s := fullSnap(StageEnteredWaterfall, DungeonEntry, inv)
	if d := Decide(s); d.Leg != LegEntryDoor {
		t.Fatalf("in the dungeon, no room and no food: %s, want the way out", d)
	}
	s.Tile = LedgeLanding
	if d := Decide(s); d.Leg != LegBarrel {
		t.Fatalf("on the ledge, no room and no food: %s, want the barrel, not the ledge door", d)
	}
	s.Tile = ArdougneBank
	d := Decide(s)
	if d.Leg != LegKitDungeon {
		t.Fatalf("on the mainland: %s, want the dungeon kit's bank visit", d)
	}
	st := StepFor(d, s)
	if st.Kind != questplan.StepDeposit {
		t.Fatalf("the bank visit: %s, want a deposit", st.Describe())
	}
	keep := map[int]bool{}
	for _, id := range st.KeepIDs {
		keep[id] = true
	}
	for _, id := range []int{ItemRope, ItemAmulet, ItemFullUrn, ItemAirRune, ItemEarthRune, ItemWaterRune, ItemCoins} {
		if !keep[id] {
			t.Errorf("the deposit would bank obj %d, which the trip carries", id)
		}
	}
	if keep[1511] {
		t.Error("the deposit keeps the logs that fill the pack")
	}
	// Room made: the kit's own provision (its food) again, no longer the deposit; and with the
	// food drawn, the river.
	s.FreeSlots = 10
	if st := StepFor(Decide(s), s); st.Kind != questplan.StepProvision {
		t.Fatalf("with room: %s, want the kit's provision", st.Describe())
	}
	s.Inv[385] = 10
	if d := Decide(s); d.Leg != LegRaft {
		t.Fatalf("with room and food: %s, want the raft", d)
	}
}

// The tomb and the Tree Gnome Village dungeon make a slot from food the same way, instead of
// climbing out and back in.
func TestAFullPackInTheTombOrTheGnomeDungeonMakesRoom(t *testing.T) {
	inv := map[int]int{1891: 20, ItemRope: 1, ItemCoins: 40}
	s := fullSnap(StageEnteredTomb, tile.Tile{X: 2540, Z: 9812}, inv)
	if AreaOf(s.Tile, true) != AreaTomb {
		t.Fatalf("fixture tile is %s", AreaOf(s.Tile, true))
	}
	if d := Decide(s); d.Leg != LegMakeRoom {
		t.Fatalf("tomb, no slot for the amulet: %s", d)
	}
	s = fullSnap(StageReadBook, tile.Tile{X: 2533, Z: 9556}, inv)
	if d := Decide(s); d.Leg != LegMakeRoom {
		t.Fatalf("gnome dungeon, no slot for Golrie's key: %s", d)
	}
}

// The slot food: a one-bite food or a slice before a 2/3 cake, a whole cake last.
func TestSlotFoodEatsWhatFreesASlotFirst(t *testing.T) {
	cases := []struct {
		inv  map[int]int
		want int
	}{
		{map[int]int{1891: 5, 1893: 1, 1895: 1}, 1895},
		{map[int]int{1891: 5, 1893: 1}, 1893},
		{map[int]int{1891: 5}, 1891},
		{map[int]int{1891: 5, 2309: 2}, 2309},
		{map[int]int{385: 1, 1895: 1}, 385},
		{map[int]int{ItemRope: 1}, 0},
	}
	for _, c := range cases {
		if got := SlotFood(c.inv); got != c.want {
			t.Errorf("SlotFood(%v) = %d, want %d", c.inv, got, c.want)
		}
	}
}

// The rope has three shops, so one keeper out of reach does not leave the dungeon kit short
// (Acct222 parked two hours on Aemad alone): with Aemad exhausted the plan goes to the next.
func TestTheRopeFallsToAnotherShopWhenAemadFails(t *testing.T) {
	ws := Snapshot{Inv: map[int]int{ItemAmulet: 1, ItemFullUrn: 1}, Worn: map[int]int{}, IsFood: IsFood,
		Settings: DefaultSettings(), BankKnown: true, Bank: map[int]int{ItemCoins: 500}}
	kit := loadoutKit(DungeonLoadout(StageEnteredTomb, DefaultSettings()), ws)
	var rope questplan.KitLine
	for _, l := range kit {
		if l.ID == ItemRope {
			rope = l
		}
	}
	if len(rope.Sources) < 2 {
		t.Fatalf("the rope has %d shop(s)", len(rope.Sources))
	}
	st := questplan.ProvisionState{Pack: map[int]int{ItemAmulet: 1, ItemFullUrn: 1, ItemCoins: 200}, BankKnown: true,
		Bank: map[int]int{}, MuleKnown: true, Mule: map[int]int{}, FreeSlots: 20,
		Exhausted: map[string]bool{questplan.SourceKey(rope, rope.Sources[0]): true}}
	plan := questplan.PlanProvision(kit, st)
	for _, task := range plan.Tasks {
		if task.Line.ID == ItemRope {
			if task.Source.Shop.Npc == "Aemad" {
				t.Fatal("the exhausted Aemad was planned again")
			}
			return
		}
	}
	t.Fatalf("no rope source after Aemad: %s", plan.Summary())
}

// The lab reproduction (wffull-empty-1): 22 cakes, the amulet and the urn, two free slots at the
// dungeon kit. The kit banks the food (keeping the trip's own items) and draws the trip's back
// from the bank, so the rope and three rune stacks fit with a slot left for the key.
func TestSurplusFoodIsBankedBeforeTheDungeonKit(t *testing.T) {
	s := engineSnap(StageEnteredTomb, ArdougneBank)
	s.InvIDs = map[int]int{ItemAmulet: 1, ItemFullUrn: 1, 1891: 22, ItemCoins: 300, ItemGolrieKey: 1, ItemBook: 1}
	s.FreeSlots = 2
	s.BankKnown, s.BankIDs = true, map[int]int{}
	step := DecideStep(s, DefaultSettings())
	if step.Kind != questplan.StepDeposit {
		t.Fatalf("22 cakes and 2 free slots at the dungeon kit: %s, want the food banked", step.Describe())
	}
	keep := map[int]bool{}
	for _, id := range step.KeepIDs {
		keep[id] = true
	}
	if keep[1891] || !keep[ItemAmulet] || !keep[ItemFullUrn] || !keep[ItemRope] || !keep[ItemAirRune] || !keep[ItemCoins] {
		t.Fatalf("the deposit keeps %v", step.KeepIDs)
	}
	// Banked: the kit draws the trip's food back, not all 22.
	s.InvIDs = map[int]int{ItemAmulet: 1, ItemFullUrn: 1, ItemCoins: 300}
	s.FreeSlots = 25
	s.BankIDs = map[int]int{1891: 22, ItemGolrieKey: 1, ItemBook: 1}
	step = DecideStep(s, DefaultSettings())
	if step.Kind != questplan.StepProvision || step.KitName != KitDungeon {
		t.Fatalf("after the deposit: %s, want the dungeon kit", step.Describe())
	}
	for _, l := range step.Kit {
		if l.ID == 1891 && (l.Qty < 1 || l.Qty > 12) {
			t.Fatalf("the kit draws %d cakes back, want the trip's food only", l.Qty)
		}
	}
	// A pack with only the trip's food (8 cakes is what the kit itself draws back) is never
	// sent back to the bank: that would be a deposit-withdraw loop.
	s.InvIDs = map[int]int{ItemAmulet: 1, ItemFullUrn: 1, 1891: 8, ItemCoins: 300}
	s.FreeSlots = 2
	if step := DecideStep(s, DefaultSettings()); step.Kind == questplan.StepDeposit {
		t.Fatalf("8 cakes (the trip's own food) were banked: %s", step.Describe())
	}
}
