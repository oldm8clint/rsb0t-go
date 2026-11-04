package waterfallplan

import (
	"fmt"
	"sort"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
)

// The Waterfall Quest as a module on the ONE quest engine (questplan/questrunner), per
// AIO-PLAN C10: Decide's legs stay exactly as the waterfall agent derived them from content, and
// StepFor turns each leg into one of the engine's generic steps (talk, interactLoc, useOn,
// opHeld, walk, provision) or, where the leg needs a stand, a wait or a loop the generic steps
// do not have, a named custom step that waterfallrunner registers.
//
// What moved out of the old Snapshot: the pillar ledger (visit memory) now lives inside the
// pillars custom step, which uses the statue itself only when all 18 pairs were acknowledged in
// the same visit; and KitShort is now the engine's own record of a failed provision step
// (Snapshot.Failed["provision:<kit>"]), so a kit the item rule cannot fill waits and parks
// instead of setting off short.

// QuestID is the engine's id for this quest (the ladder's and the side store's key).
const QuestID = "waterfall"

// Custom step names, registered by waterfallrunner.
const (
	CustomStrip     = "wf-strip"
	CustomRaft      = "wf-raft"
	CustomHudon     = "wf-hudon"
	CustomSwimOff   = "wf-swim-off"
	CustomChest     = "wf-chest"
	CustomRopeRock  = "wf-rope-rock"
	CustomRopeTree  = "wf-rope-tree"
	CustomBarrel    = "wf-barrel"
	CustomLedgeDoor = "wf-ledge-door"
	CustomBaxCrate  = "wf-bax-crate"
	CustomLaneIn    = "wf-lane-in"
	CustomLaneOut   = "wf-lane-out"
	CustomPillars   = "wf-pillars"
	CustomFreeSlots = "wf-free-slots"
)

// Kit names, as provision steps carry them (Snapshot.Failed is keyed "provision:"+name).
const (
	KitTomb        = "waterfall-tomb"
	KitTombFood    = "waterfall-tomb-food"
	KitDungeon     = "waterfall-dungeon"
	KitDungeonFood = "waterfall-dungeon-food"
)

// Shops the kits fall back to, from content (npc spawns in maps/*.jm2, stock in the area .inv
// files, prices from the obj configs' cost).
var (
	// ShopAemad is the East Ardougne adventurers' shop: rope (stock 20, cost 18).
	ShopAemad = questplan.ShopRef{Npc: "Aemad", NpcID: 590, Anchor: tile.Tile{X: 2613, Z: 3294}}
	// ShopKhazard is Port Khazard's general store (khazardshop: rope stock 30), its shopkeeper
	// (npc 555) behind the counter at 2655,3151; and questplan.ArheinStore, Catherby's general
	// store (arheinstore: rope stock 2), is the rope's third shop. Aemad alone was a single point
	// of failure: on 2026-09-25 the live Acct222 could not reach him once ("the keeper is out of
	// reach"), the dungeon kit was short and the quest parked for two hours.
	ShopKhazard = questplan.ShopRef{Npc: "Shop keeper", NpcID: 555, Anchor: tile.Tile{X: 2655, Z: 3149}}
	// ShopBaker is the East Ardougne market baker: bread (stock 10, cost 12), cake (stock 3).
	ShopBaker = questplan.ShopRef{Npc: "Baker", NpcID: 571, Anchor: tile.Tile{X: 2669, Z: 3310}}
	// ShopBetty is Port Sarim's rune shop: air/earth/water runes (stock 1000 each).
	ShopBetty = questplan.ShopRef{Npc: "Betty", NpcID: 583, Anchor: tile.Tile{X: 3012, Z: 3259}}
	// ShopAubury is Varrock's rune shop: air/earth/water runes (stock 2000 each).
	ShopAubury = questplan.ShopRef{Npc: "Aubury", NpcID: 553, Anchor: tile.Tile{X: 3253, Z: 3402}}
)

// Foods the quest eats, best first: what a Stage 5 account or the mule usually holds, then what
// the fallback shop sells. Heal amounts are content's.
var Foods = []int{
	385,  // shark (20)
	373,  // swordfish (14)
	379,  // lobster (12)
	365,  // bass (13)
	361,  // tuna (10)
	329,  // salmon (9)
	1891, // cake (4 a bite, three bites)
	1893, // 2/3 cake
	1895, // slice of cake
	333,  // trout (7)
	2309, // bread (5)
	2142, // cooked meat (3)
}

// FoodBread is the fallback food the Baker sells.
const FoodBread = 2309

// RewardSlots is the free slots the chalice's reward needs.
const RewardSlots = 5

// SlotFood is the held food to eat or drop for a free slot: the one whose single bite frees its
// slot soonest. A slice of cake or any one-bite food first, then a 2/3 cake, a whole cake last,
// because a bite of cake leaves the rest of it in the same slot. 0 when no food is held.
func SlotFood(inv map[int]int) int {
	multi := map[int]bool{1891: true, 1893: true}
	for _, f := range Foods {
		if !multi[f] && inv[f] > 0 {
			return f
		}
	}
	for _, f := range []int{1893, 1891} {
		if inv[f] > 0 {
			return f
		}
	}
	return 0
}

// IsFood reports whether an id is one of Foods.
func IsFood(id int) bool {
	for _, f := range Foods {
		if f == id {
			return true
		}
	}
	return false
}

// Surface avoid list for every mainland walk: the Hemenster moss giants on the default
// washout-to-bank route (the waterfall study's planner probe).
var surfaceAvoid = []questplan.Rect{{
	MinX: HemensterMossGiants.MinX, MaxX: HemensterMossGiants.MaxX,
	MinZ: HemensterMossGiants.MinZ, MaxZ: HemensterMossGiants.MaxZ,
}}

// Record is the quest's identity. Hitpoints 20 is the OWNER's gate ("once we hit 20
// hitpoints"), not a server one: content has no requirement at all. Defence 20 is the fleet's
// safety floor: live 29 Sep, acct159 (04:12, on the walk in) and acct3 (04:57, walking out of
// the dungeon after completing) both died at Defence 1.
var Record = questplan.Record{
	ID: QuestID, Name: QuestName, QuestPoints: 1,
	Requirements: questplan.Requirements{Skills: []questplan.SkillReq{{Skill: "hitpoints", Level: 20}, {Skill: "defence", Level: 20}}},
	Items: []questplan.Item{
		{Name: "Rope", Qty: 1, Kind: questplan.Acquirable},
		{Name: "Air rune", Qty: 6, Kind: questplan.Acquirable},
		{Name: "Earth rune", Qty: 6, Kind: questplan.Acquirable},
		{Name: "Water rune", Qty: 6, Kind: questplan.Acquirable},
	},
}

// Module builds the engine module with the given settings.
func Module(set Settings) questplan.Module {
	set = set.Normalised()
	return questplan.Module{
		Record:  Record,
		Bank:    ArdougneBank,
		BankSet: true,
		Anchor:  ArdougneBank,
		// Not the book: once read it is only a pack slot the chalice's five reward slots need, and
		// the bookcase hands a new one to anyone who holds none.
		ToolIDs: []int{ItemGolrieKey, ItemPebble, ItemAmulet, ItemFullUrn, ItemEmptyUrn, ItemBaxKey, ItemRope},
		ReadStage: func(lines []string) (int, bool) {
			st, ok := ParseJournal(lines)
			return int(st), ok
		},
		Sustain:   questplan.Sustain{FoodIDs: Foods, EatAt: set.EatAtDungeon},
		MaxDeaths: set.MaxDeathsEngine(),
		// Every monster on the routes out-levels a combat-22 account and the lane's fire giants
		// hit 11: keep moving, never fight (the study's rule; auto-retaliate stood the third lab
		// run at Golrie's gate and the fourth in the spider room until they died).
		NoRetaliate: true,
		Decide:      func(s questplan.Snapshot) questplan.Step { return DecideStep(s, set) },
	}
}

// MaxDeathsEngine is the engine's park cap: MaxDeaths deaths park the quest (0 is no cap).
func (s Settings) MaxDeathsEngine() int { return s.MaxDeaths }

// FromEngine converts the engine's snapshot into this package's.
func FromEngine(s questplan.Snapshot, set Settings) Snapshot {
	ws := Snapshot{
		Status: s.Journal, Stage: Stage(s.Stage), StageKnown: s.StageKnown,
		Tile: s.Tile, TileKnown: s.TileKnown,
		Inv: s.InvIDs, Worn: s.WornIDs, Bank: s.BankIDs, BankKnown: s.BankKnown,
		IsFood: IsFood, FreeSlots: s.FreeSlots, HP: s.HP, MaxHP: s.MaxHP,
		Settings: set, Ran: s.Ran, PillarsFailed: s.Failed[CustomPillars],
	}
	// A failed food top-up is short only while the pack is still below the trip's floor: the
	// seventh EMPTY lab run bought the baker's whole shelf (10 of 14), held 14 against a floor of
	// 10, and still parked on "the dungeon kit is short".
	stage := StageNotStarted
	if ws.StageKnown {
		stage = ws.Stage
	}
	food := FoodUnits(ws.Inv, IsFood)
	// A top-up is short only when both its sources failed: the cake stall (own skill) and then
	// the baker (shop), the item rule's order.
	tombFoodShort := s.Failed["provision:"+KitTombFood] && s.Failed["provision:"+KitTombFood+ShopSuffix] &&
		food < TombLoadout(stage, set).FoodMin
	dungeonFoodShort := s.Failed["provision:"+KitDungeonFood] && s.Failed["provision:"+KitDungeonFood+ShopSuffix] &&
		food < DungeonLoadout(stage, set).FoodMin
	ws.Failed = s.Failed
	switch {
	case s.Failed["provision:"+KitTomb] || tombFoodShort:
		ws.KitShort, ws.KitShortTrip, ws.KitWhy = true, TripTomb, "the item rule could not fill the tomb kit"
	case s.Failed["provision:"+KitDungeon] || dungeonFoodShort:
		ws.KitShort, ws.KitShortTrip, ws.KitWhy = true, TripDungeon, "the item rule could not fill the dungeon kit"
	}
	return ws
}

// DecideStep is the module's decide(): the leg, as one engine step.
func DecideStep(s questplan.Snapshot, set Settings) questplan.Step {
	ws := FromEngine(s, set)
	return StepFor(Decide(ws), ws)
}

func custom(name, why string) questplan.Step {
	return questplan.Step{Kind: questplan.StepCustom, Custom: name, Reason: why}
}

func useLoc(item, loc int, locName string, anchor tile.Tile) questplan.Step {
	return questplan.Step{Kind: questplan.StepUseOn, ItemID: item, Item: ItemName(item),
		TargetKind: questplan.TargetLoc, TargetID: loc, Target: locName, Anchor: anchor}
}

func opLoc(loc int, name, op string, anchor tile.Tile, expect int) questplan.Step {
	return questplan.Step{Kind: questplan.StepInteractLoc, LocID: loc, Loc: name, Op: op, Anchor: anchor,
		ExpectItemID: expect, ExpectItem: expectName(expect), Avoid: surfaceAvoid}
}

// sideOf is the door stand on the account's side of a keyed door on the north edge of south.
func sideOf(s Snapshot, south, north tile.Tile) tile.Tile {
	if s.TileKnown && s.Tile.Z >= north.Z {
		return north
	}
	return south
}

func expectName(id int) string {
	if id == 0 {
		return ""
	}
	return ItemName(id)
}

// StepFor turns one leg into the engine step that performs it.
func StepFor(d Decision, s Snapshot) questplan.Step {
	switch d.Leg {
	case LegDone:
		return questplan.Step{Kind: questplan.StepDone}
	case LegWait:
		return questplan.Step{Kind: questplan.StepWait, Reason: "waterfall: " + d.Why}
	case LegStrandedBnk:
		return questplan.Step{Kind: questplan.StepWait, Reason: "waterfall: stranded on the tree bank without a rope"}

	case LegKitTomb:
		return kitStep(TripTomb, s, d.Why)
	case LegKitDungeon:
		return kitStep(TripDungeon, s, d.Why)

	case LegAlmera:
		return questplan.Step{Kind: questplan.StepTalk, Stop: questplan.NpcStop{
			Npc: "Almera", ID: NpcAlmera, Anchor: AlmeraAnchor, Leash: 8,
			Prefer: []string{"How can I help?"},
		}}
	case LegRaft:
		return custom(CustomRaft, d.Why)
	case LegHudon:
		return custom(CustomHudon, d.Why)
	case LegSwimOff:
		return custom(CustomSwimOff, d.Why)
	case LegBookcase:
		return opLoc(LocBookcase, "Bookcase", "Search", BookcaseTile, ItemBook)
	case LegReadBook:
		return questplan.Step{Kind: questplan.StepOpHeld, ItemID: ItemBook, Item: ItemName(ItemBook), Op: "Read"}

	case LegTGVDown:
		return questplan.Step{Kind: questplan.StepWalk, Anchor: TGVLanding, Radius: 3, Avoid: surfaceAvoid}
	case LegGolrieKey:
		return opLoc(LocGolrieCrate, "Crate", "Search", GolrieCrateTile, ItemGolrieKey)
	case LegGolrieGate:
		return useLoc(ItemGolrieKey, LocGolrieGate, "Door", GolrieGateTile)
	case LegGolrie:
		return questplan.Step{Kind: questplan.StepTalk, UntilItemID: ItemPebble, Stop: questplan.NpcStop{
			Npc: "Golrie", ID: NpcGolrie, Anchor: GolrieAnchor, Leash: 8,
			// Golrie offers no choices; a preference is still required by ValidateStep.
			Prefer: []string{"Could I take this old pebble?"},
		}}
	case LegTGVUp:
		return questplan.Step{Kind: questplan.StepWalk, Anchor: TGVLadderStand, Radius: 3}

	case LegHeal:
		if d.Item == 0 {
			return questplan.Step{Kind: questplan.StepWait, Reason: "waterfall: nothing to eat before the tomb"}
		}
		return questplan.Step{Kind: questplan.StepOpHeld, ItemID: d.Item, Item: foodName(d.Item), Op: "Eat", Reason: d.Why}
	case LegTombstone:
		return useLoc(ItemPebble, LocTombstone, "Tombstone of glarial", TombstoneTile)
	case LegChest:
		return custom(CustomChest, d.Why)
	case LegCoffin:
		return opLoc(LocCoffin, "Tomb of glarial", "Search", CoffinTile, ItemFullUrn)
	case LegTombUp:
		return opLoc(LocLadderUp, "Ladder", "Climb-up", TombLadderTile, 0)

	case LegRopeRock:
		return custom(CustomRopeRock, d.Why)
	case LegRopeTree:
		return custom(CustomRopeTree, d.Why)
	case LegBarrel:
		return custom(CustomBarrel, d.Why)
	case LegLedgeDoor:
		return custom(CustomLedgeDoor, d.Why)

	case LegBaxKey:
		// A custom, not a plain loc op: the crate must be searched from its west side
		// (BaxCrateStand), and a plain op lets the approach pick the south tile, which touches a
		// level-45 skeleton's spawn (the eighth EMPTY lab run died there).
		return custom(CustomBaxCrate, d.Why)
	case LegLaneIn:
		return custom(CustomLaneIn, d.Why)
	case LegLaneOut:
		return custom(CustomLaneOut, d.Why)
	// A keyed door is used from the side the account stands on: an anchor across it makes the
	// walk cross the door first (its Open walks through with the key held), and the ninth EMPTY
	// lab run then went back and forth through the pillar-room door 340 times.
	case LegPuzzleDoor, LegExitPuzzle:
		return useLoc(ItemBaxKey, LocKeyedDoor, "Door", sideOf(s, PuzzleDoorSouth, PuzzleDoorNorth))
	case LegExitSouth:
		return useLoc(ItemBaxKey, LocKeyedDoor, "Door", sideOf(s, SouthDoorSouth, SouthDoorNorth))
	case LegExitRaised:
		return useLoc(ItemBaxKey, LocKeyedDoor, "Door", RaisedDoorTile)
	case LegPillars, LegStatue:
		return custom(CustomPillars, d.Why)
	case LegFreeSlots:
		st := custom(CustomFreeSlots, d.Why)
		st.Qty = RewardSlots
		return st
	case LegMakeRoom:
		st := custom(CustomFreeSlots, d.Why)
		st.Qty = 1
		return st
	case LegChalice:
		return useLoc(ItemFullUrn, LocChalice, "Chalice of eternity", ChaliceTile)
	case LegWashout:
		return opLoc(LocChalice, "Chalice of eternity", "Take treasure", ChaliceTile, 0)
	case LegEntryDoor:
		return opLoc(LocEntryDoor, "Door", "Open", DungeonEntry, 0)
	}
	return questplan.Step{Kind: questplan.StepWait, Reason: fmt.Sprintf("waterfall: no step for leg %q", d.Leg)}
}

// kitStep is a trip's bank visit: strip what the trip may not carry, then provision the kit by
// the item rule, then top food up from the fallback shop.
func kitStep(trip Trip, s Snapshot, why string) questplan.Step {
	stage := StageNotStarted
	if s.StageKnown {
		stage = s.Stage
	}
	if trip == TripTomb {
		if ok, _ := TombSafe(s.Inv, s.Worn, s.IsFood); !ok {
			return custom(CustomStrip, "the tomb trip must be tomb-safe: "+why)
		}
		l := TombLoadout(stage, s.Settings)
		if dep, ok := surplusFoodDeposit(l, s); ok {
			return dep
		}
		kit := loadoutKit(l, s)
		if food := FoodUnits(s.Inv, s.IsFood); food < l.FoodMin && s.Ran["provision:"+KitTomb] {
			// Top up to the trip's full food, not its floor: the Tree Gnome Village dungeon's
			// hobgoblins killed a Defence-19 account holding four breads on the first lab run.
			return bare(foodTopUp(KitTombFood, l.Food-food, s))
		}
		return bare(provision(KitTomb, kit, why))
	}
	if s.Worn[ItemAmulet] > 0 {
		return custom(CustomStrip, "the amulet must travel in the pack for the statue")
	}
	l := DungeonLoadout(stage, s.Settings)
	if dep, ok := surplusFoodDeposit(l, s); ok {
		return dep
	}
	if s.FreeSlots < 1 && !s.has(ItemBaxKey) && !s.canMakeRoom() {
		// A full pack with no food: bank everything the trip does not carry, so the dungeon key
		// has a slot (the provision alone keeps a pack that already satisfies the kit).
		keep := []int{ItemCoins}
		for _, id := range sortedIDs(l.Required) {
			keep = append(keep, id)
		}
		for _, id := range sortedIDs(l.Optional) {
			keep = append(keep, id)
		}
		return questplan.Step{Kind: questplan.StepDeposit, KeepIDs: keep, Bank: ArdougneBank, BankSet: true,
			Reason: "a full pack with no food: room for the dungeon key"}
	}
	kit := loadoutKit(l, s)
	if food := FoodUnits(s.Inv, s.IsFood); food < l.FoodMin && KitSatisfiedButFood(l, s) && s.Ran["provision:"+KitDungeon] {
		return foodTopUp(KitDungeonFood, l.Food-food, s)
	}
	return provision(KitDungeon, kit, why)
}

// surplusFoodDeposit banks the pack's food, keeping the trip's own items, when the pack holds
// more food than the trip draws and too little room for the trip's missing items plus the one
// thing it picks up on the way (a key, the amulet, the urn). The provisioner keeps every food
// the pack carries, so a Stage 3 account's 22 mule cakes left the dungeon kit two slots for four
// lines (the lab's wffull-empty-1: "You don't have enough inventory space" at Betty's, then the
// kit short and the quest parked) and the live "acct" into the dungeon with none for the
// key. The kit then draws the trip's own food back from the bank, best first.
func surplusFoodDeposit(l Loadout, s Snapshot) (questplan.Step, bool) {
	// The margin is one food's worth (a shark is 4 units): the kit draws food back rounded up to
	// whole items, and a pack at that level must never be banked again (a deposit-withdraw loop).
	if FoodUnits(s.Inv, s.IsFood) <= l.Food+maxFoodUnits {
		return questplan.Step{}, false
	}
	need := 1
	for id, n := range l.Required {
		switch have := s.Inv[id]; {
		case have >= n:
		case stackable(id):
			if have == 0 {
				need++
			}
		default:
			need += n - have
		}
	}
	if s.FreeSlots >= need {
		return questplan.Step{}, false
	}
	keep := []int{ItemCoins}
	for _, id := range sortedIDs(l.Required) {
		keep = append(keep, id)
	}
	for _, id := range sortedIDs(l.Optional) {
		keep = append(keep, id)
	}
	return questplan.Step{Kind: questplan.StepDeposit, KeepIDs: keep, Bank: ArdougneBank, BankSet: true, Bare: l.TombSafe,
		Reason: fmt.Sprintf("%d free slot(s) for %d: the pack's %d food units are more than the %s's %d; bank them and draw the trip's back",
			s.FreeSlots, need, FoodUnits(s.Inv, s.IsFood), l.Trip, l.Food)}, true
}

// maxFoodUnits is the most FoodUnits one food item is worth (a shark's 20 hitpoints).
const maxFoodUnits = 20 / FoodUnit

// stackable reports the loadout items that share one slot however many are held.
func stackable(id int) bool {
	switch id {
	case ItemAirRune, ItemEarthRune, ItemWaterRune, ItemCoins:
		return true
	}
	return false
}

// KitSatisfiedButFood reports whether a loadout's required lines are all held.
func KitSatisfiedButFood(l Loadout, s Snapshot) bool {
	for id, n := range l.Required {
		if s.Inv[id] < n {
			return false
		}
	}
	return true
}

// bare marks st as a bare-pack step (questplan.Step.Bare): its bank visits leave the run staller's kit in the bank. The
// tomb trip's every bank visit is one, or the visit that follows the strip draws the staller's ring of dueling back and
// TombSafe fails again (the 238 wf-strip parks of 14-15 Oct 2026).
func bare(st questplan.Step) questplan.Step {
	st.Bare = true
	return st
}

func provision(name string, kit []questplan.KitLine, why string) questplan.Step {
	return questplan.Step{Kind: questplan.StepProvision, KitName: name, Kit: kit,
		KeepIDs: append(foodIDs(), ItemCoins), Bank: ArdougneBank, BankSet: true, Reason: why}
}

func foodIDs() []int { return append([]int(nil), Foods...) }

// ShopSuffix names a top-up's shop fallback kit.
const ShopSuffix = "-shop"

// FoodCake is the Baker's stall's cake; MethodThieveCake the skill source that steals it
// (Thieving 5, East Ardougne market).
const (
	FoodCake         = 1891
	MethodThieveCake = "thieve-cake"
)

// BakersStall is the East Ardougne market cake stall.
var BakersStall = tile.Tile{X: 2667, Z: 3310}

// CakeStallMinHP is the Hitpoints below which the cake stall is not an own-skill food source:
// a seen theft sets the market guards on the account, and they follow it to the baker next door.
// Lane R measured them killing low-hitpoint accounts there, and the twelfth EMPTY lab run died
// to one at 20 Hitpoints. Below it the top-up goes straight to the shop.
const CakeStallMinHP = 30

// foodTopUp tops the food up by units FoodUnits, by the item rule past the bank and the mule:
// own skills (cakes from the Baker's stall) before the shop (the baker's bread). Only when the
// stall's kit has failed is the bread kit offered.
func foodTopUp(name string, units int, s Snapshot) questplan.Step {
	if !s.Failed["provision:"+name] && s.MaxHP >= CakeStallMinHP {
		return provision(name, []questplan.KitLine{{
			ID: FoodCake, Name: "Cake", Qty: s.Inv[FoodCake] + ItemsFor(FoodCake, units),
			Sources: []questplan.Source{{Kind: questplan.SourceSkill, Skill: "thieving", Level: 5, Method: MethodThieveCake, Tile: BakersStall}},
		}}, "top the food up with cakes from the market stall")
	}
	return provision(name+ShopSuffix, []questplan.KitLine{{
		ID: FoodBread, Name: "Bread", Qty: s.Inv[FoodBread] + ItemsFor(FoodBread, units),
		Sources: []questplan.Source{{Kind: questplan.SourceShop, Shop: ShopBaker, Price: 12}},
	}}, "top the food up from the market baker")
}

// loadoutKit is a loadout as provision lines. The quest's own items are untradeable (only the
// quest gives them); rope and runes fall back to shops; food is optional per banked kind, best
// first, with sharks always offered to the mule, and a separate top-up step buys bread when the
// optional lines left the pack below the trip's minimum.
func loadoutKit(l Loadout, s Snapshot) []questplan.KitLine {
	var kit []questplan.KitLine
	for _, id := range sortedIDs(l.Required) {
		kit = append(kit, lineFor(id, l.Required[id], false))
	}
	for _, id := range sortedIDs(l.Optional) {
		if _, dup := l.Required[id]; dup {
			continue
		}
		if s.BankKnown && s.Bank[id] > 0 {
			kit = append(kit, lineFor(id, l.Optional[id], true))
		}
	}
	want := l.Food - FoodUnits(s.Inv, s.IsFood)
	if want <= 0 {
		return kit
	}
	// Banked food, best first.
	type have struct{ id, n int }
	var banked []have
	if s.BankKnown {
		for _, f := range Foods {
			if s.Bank[f] > 0 {
				banked = append(banked, have{f, s.Bank[f]})
			}
		}
	}
	sort.SliceStable(banked, func(i, j int) bool { return foodRank(banked[i].id) < foodRank(banked[j].id) })
	sharkListed := false
	for _, b := range banked {
		if want <= 0 {
			break
		}
		n := minInt(ItemsFor(b.id, want), b.n)
		kit = append(kit, questplan.KitLine{ID: b.id, Name: foodName(b.id), Qty: s.Inv[b.id] + n, Optional: true})
		if b.id == 385 {
			sharkListed = true
		}
		want -= n * FoodHeal[b.id] / FoodUnit
	}
	if want > 0 && !sharkListed {
		kit = append(kit, questplan.KitLine{ID: 385, Name: "Shark", Qty: s.Inv[385] + ItemsFor(385, want), Optional: true})
	}
	return kit
}

func foodRank(id int) int {
	for i, f := range Foods {
		if f == id {
			return i
		}
	}
	return len(Foods)
}

func foodName(id int) string {
	switch id {
	case 385:
		return "Shark"
	case 373:
		return "Swordfish"
	case 379:
		return "Lobster"
	case 365:
		return "Bass"
	case 361:
		return "Tuna"
	case 329:
		return "Salmon"
	case 1891:
		return "Cake"
	case 1893:
		return "2/3 cake"
	case 1895:
		return "Slice of cake"
	case 333:
		return "Trout"
	case 2309:
		return "Bread"
	case 2142:
		return "Cooked meat"
	}
	return fmt.Sprintf("obj %d", id)
}

// lineFor is one quest or supply item as a provision line.
func lineFor(id, qty int, optional bool) questplan.KitLine {
	l := questplan.KitLine{ID: id, Name: ItemName(id), Qty: qty, Optional: optional}
	switch id {
	case ItemRope:
		l.Sources = []questplan.Source{
			{Kind: questplan.SourceShop, Shop: ShopAemad, Price: 18},
			{Kind: questplan.SourceShop, Shop: ShopKhazard, Price: 18},
			{Kind: questplan.SourceShop, Shop: questplan.ArheinStore, Price: 18},
		}
	case ItemAirRune, ItemEarthRune, ItemWaterRune:
		l.Stackable = true
		l.Sources = []questplan.Source{
			{Kind: questplan.SourceShop, Shop: ShopBetty, Price: 4},
			{Kind: questplan.SourceShop, Shop: ShopAubury, Price: 4},
		}
	case ItemCoins:
		l.Stackable = true
	default:
		// Glarial's pebble, amulet and urn, the two keys and the book come only from the quest.
		l.Untradeable = true
	}
	return l
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
