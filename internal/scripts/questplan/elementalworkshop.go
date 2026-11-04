package questplan

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// The Elemental Workshop (content id `elemental_workshop`, %elemental_workshop_bits, not transmitted, 1 quest point, Mining 20, Smithing 20
// and Crafting 20), ported from the TS def (defs/elementalworkshop/*.ts) and checked against content 289: quests/quest_elemental_workshop
// (quest_elemental_workshop.rs2, elemental_workshop_shield_book.rs2, elemental_workshop_journal.rs2, elemental_earth.rs2 and the configs)
// and maps/m42_154.jm2. The reward is 5,000 Crafting and 5,000 Smithing experience (stat_advance(crafting, 50000), 1x), 28 more Smithing
// for the bar and the shield, and the Elemental shield; no coins.
//
// THE QUEST. A book in a house in Seers' Village (the bookcase, loc 3389) starts it when it is read; a knife used on the book cuts a key
// out of its spine; the key opens the "Odd looking wall" at the back of the village smithy (a double door of wall locs, a p_teleport
// through them); behind it are the spiral stairs down into a workshop that is a pocket of its own. The workshop is a machine:
//
//   - the water wheel (northern chamber): the first valve (loc 3404, east) sets a bit only while the second (loc 3405, west) is off, the
//     second always toggles its own, and the lever (loc 3406) starts the wheel only when both bits are set, otherwise it clears them with
//     "You hear the sound of the flow gates resetting." So the order is first valve, second valve, lever, and a reset leaves the clean
//     state that order works from. Pulled again on a running wheel the lever stops it and the air with it.
//   - the bellows (eastern chamber): a hole in the leather (loc 3410, Fix) wants Crafting 20, a needle, a leather and a thread (the
//     leather and the thread are used up). The needle, the leather and the stone bowl come from three of the workshop's eight crates
//     (box 2, box 4 and box 1: locs 3395, 3397, 3394), a needle and a leather ONCE per account (a perm bit), the bowl whenever the
//     pack holds none; the thread must be brought.
//   - the furnace (loc 3413, south): a stone bowl filled at the lava trough (loc 3414) and emptied on the furnace lights it for good.
//   - the air lever (loc 3409) pumps the bellows, only when the wheel runs and the bellows are mended, and toggles.
//   - the Pile of Rock (28 locs, 3403, the western chamber): Mine, with Mining 20 and a pickaxe the level can use, makes a level 35 EARTH
//     ELEMENTAL appear ON THE ACCOUNT'S TILE (npc_add(coord, earth_elemental_rock_version)) and set on it. Killed (by the account that did
//     most of the damage) it drops an elemental ore (and swamp rocks). The ore needs nothing else done first.
//   - the furnace with the ore and four coal, the furnace lit and the air pumping: an elemental bar (8 Smithing XP).
//   - the workbench (loc 3402): the bar with a hammer, Smithing 20 and the book in the pack (the "instructions"): the shield, and the
//     quest (queue elemental_workshop_quest_complete).
//
// WHAT CAN HURT, and the fight. The workshop's four kinds of elemental (fire, earth, air, water: 21 of them in the maps) have no hunt mode
// and no wander away from their chambers; they are never attacked or woken. The ONE fight is the earth elemental of the rock: level 35,
// 35 hitpoints, Attack 20, Strength 35, Defence 35, a crush hit of up to 4 every SIX ticks (attackrate 6), no bonuses. It is priced before the
// account goes down and again before the rock: the same model as the Observatory's goblin guard (MeleeChain over the account's own swing
// and the food in its pack, a tenth of the bites left for the way out), parked for six hours when the model loses, one death at the rock
// parks the quest for a day (StepDeathCaps) and two anywhere do too. The module never retaliates against anything else.
//
// WHAT A SCRIPT MUST PLAN FOR.
//
//   - Three places that no walk joins: the secret room behind the smithy's wall (five tiles; the way out is a Push on the wall from inside),
//     the workshop (771 tiles under Seers', joined to the secret room by the stairs) and, on the first descent, nothing else. The pocket's
//     Leave climbs the stairs and pushes the wall.
//   - The wall opens from OUTSIDE only with the Battered key used on it (oploc1 "Push" refuses with "You see a small hole in the wall but no
//     way to open it." and opens from inside); the key is not used up, and a lost one is cut again from the book with the knife (the script
//     only checks that the pack holds none).
//   - The journal (the only place the quest's progress can be read) shows the water, the bellows and the furnace once the stairs have been
//     taken ("entered" bit 13) and the bar while one is in the pack. The air and the ore are not in it: the air is found out by the furnace
//     ("The furnace needs to be hotter to be of any use.") and the ore is in the pack or not.
//   - The levers are loc changes for three ticks after a pull ("lever_pulled"), so a second pull right after the first finds no Pull to
//     send; the legs wait for it.
//   - Items the workshop cannot give are brought: the hammer, the thread, a pickaxe, four coal and food are the kit (own bank, the mule,
//     then the shops), and the leg that is short of one goes up to fetch it.

// ElementalWorkshopName is the quest tab's spelling (component 7459, `questlist:elemental_workshop`) and ElementalWorkshopID the engine's id,
// content's own.
const (
	ElementalWorkshopName = "Elemental Workshop"
	ElementalWorkshopID   = "elemental_workshop"
)

// EwSkill is the level the quest wants of Mining (the rock), Smithing (the bench) and Crafting (the bellows): content's own,
// enforced at the leg that uses each, and stated by the Record so that the ladder does not start an account that has not got them.
const EwSkill = 20

// The stage ParseElementalWorkshopJournal returns: the part of the quest the book and the key have reached in the low bits (EwStageMask),
// and the machinery the journal's paragraphs show, once the stairs have been taken, in the high ones.
const (
	EwNotStarted = 0 // the book not read
	EwRead       = 1 // the book read: the key is not cut
	EwSlashed    = 2 // the key is cut, the stairs not taken
	EwEntered    = 3 // the stairs taken: the flags below say what is done
	EwComplete   = 4

	EwStageMask = 15
	EwWater     = 16  // the water wheel runs
	EwBellows   = 32  // the bellows are mended
	EwFurnace   = 64  // the furnace is lit
	EwBar       = 128 // the pack holds the bar ("I have made a bar from the elemental ore")
)

// EwStageOf is the book-and-key part of a stage.
func EwStageOf(stage int) int { return stage & EwStageMask }

// EwHas reports whether a stage carries a machinery flag.
func EwHas(stage, flag int) bool { return stage&flag != 0 }

// Ids from content/pack (checked against the production config by TestElementalWorkshopIDsMatchTheProductionConfig in questmods).
const (
	NpcEwEarthRock = 1023 // [earth_elemental_rock_version]: the fight; the standing earth elemental is [elemental_earth] 1020

	ObjEwBook     = 2886 // [elemental_workshop_shield_book], "Battered book", untradeable
	ObjEwKey      = 2887 // [elemental_workshop_key], "Battered key", untradeable
	ObjEwBowl     = 2888 // [elemental_workshop_lava_bowl], "A stone bowl"
	ObjEwBowlFull = 2889 // [elemental_workshop_lava_bowl_full], also "A stone bowl"
	ObjEwShield   = 2890 // [elemental_shield]
	ObjEwOre      = 2892 // [elemental_workshop_ore], "Elemental ore"
	ObjEwBar      = 2893 // [elemental_workshop_bar], "Elemental metal"
	ObjEwNeedle   = 1733
	ObjEwThread   = 1734
	ObjEwLeather  = 1741
	ObjEwCoal     = 453

	LocEwBookcase    = 3389 // [elemental_workshop_bookcase], Search: the book
	LocEwWallRight   = 3390 // [elemental_workshop_oddwall_r], Push (the key opens it): east half of the double door
	LocEwWallLeft    = 3391 // [elemental_workshop_oddwall_l]
	LocEwStairsDown  = 3415 // [elemental_workshop_spiralstairstop], Climb-down: the smithy's secret room into the workshop
	LocEwStairsUp    = 3416 // [elemental_workshop_spiralstairs], Climb-up
	LocEwBoxBowl     = 3394 // [elemental_workshop_box_1], Search: a stone bowl
	LocEwBoxNeedle   = 3395 // [elemental_workshop_box_2]: a needle, once per account
	LocEwBoxLeather  = 3397 // [elemental_workshop_box_4]: a leather, once per account
	LocEwWorkbench   = 3402 // [elemental_workshop_workbench]
	LocEwRock        = 3403 // [elemental_workshop_rock], Mine
	LocEwValveFirst  = 3404 // [elemental_workshop_valve_1], Turn: sets the "right" bit while the left is off
	LocEwValveSecond = 3405 // [elemental_workshop_valve_2], Turn: toggles the "left" bit
	LocEwWaterLever  = 3406 // [elemental_workshop_water_lever], Pull
	LocEwAirLever    = 3409 // [elemental_workshop_air_lever], Pull
	LocEwBellowsL    = 3410 // [elemental_workshop_bellows_l], Fix
	LocEwBellowsR    = 3411 // [elemental_workshop_bellows_r], Fix
	LocEwFurnace     = 3413 // [elemental_workshop_furnace]
	LocEwTrough      = 3414 // [elemental_workshop_trough]
)

// Tiles from content maps, the loc index and the collision pack.
var (
	// EwBookcaseAt is the Seers' Village bookcase that holds the book (loc 3389 at 2716,3481, angle 2); EwBookcaseStand a tile it is searched
	// from.
	EwBookcaseAt    = tile.Tile{X: 2716, Z: 3481}
	EwBookcaseStand = tile.Tile{X: 2715, Z: 3481}
	// EwWallAt is the east half of the smithy's double wall (loc 3390 at 2710,3495, angle 1); EwWallOutside is the tile the key is used from
	// (the wall's own row) and EwWallInside the secret room's tile it sets the account on (and the Push is made from).
	EwWallAt      = tile.Tile{X: 2710, Z: 3495}
	EwWallOutside = tile.Tile{X: 2710, Z: 3495}
	EwWallInside  = tile.Tile{X: 2710, Z: 3496}
	// EwStairsDownAt is the stairs' top (loc 3415 at 2710,3497); EwStairsDownStand its only stand. EwArrive is where they set the account down
	// (p_teleport(0_42_154_28_32)), EwStairsUpAt the stairs up (loc 3416 at 2714,9887) beside it, and EwSecretArrive the secret room's tile they
	// set the account on (p_teleport(0_42_54_21_41)).
	EwStairsDownAt    = tile.Tile{X: 2710, Z: 3497}
	EwStairsDownStand = tile.Tile{X: 2709, Z: 3498}
	EwArrive          = tile.Tile{X: 2716, Z: 9888}
	EwStairsUpAt      = tile.Tile{X: 2714, Z: 9887}
	EwSecretArrive    = tile.Tile{X: 2709, Z: 3497}
	// The workshop's machinery, each at its loc's tile: the valves and the lever in the northern chamber, the air lever and the bellows in
	// the eastern one, the furnace, the trough that is nearest to it and the workbench.
	EwValveFirstAt  = tile.Tile{X: 2726, Z: 9908}
	EwValveSecondAt = tile.Tile{X: 2713, Z: 9908}
	EwWaterLeverAt  = tile.Tile{X: 2722, Z: 9906}
	EwAirLeverAt    = tile.Tile{X: 2734, Z: 9887}
	EwBellowsAt     = tile.Tile{X: 2734, Z: 9882}
	EwFurnaceAt     = tile.Tile{X: 2724, Z: 9874}
	EwTroughAt      = tile.Tile{X: 2714, Z: 9871}
	EwWorkbenchAt   = tile.Tile{X: 2717, Z: 9887}
	// The three crates: the bowl's, the needle's and the leather's.
	EwBoxBowlAt    = tile.Tile{X: 2717, Z: 9894}
	EwBoxNeedleAt  = tile.Tile{X: 2700, Z: 9887}
	EwBoxLeatherAt = tile.Tile{X: 2724, Z: 9894}
	// EwRockAt is the Pile of Rock the quest mines (loc 3403 at 2706,9904, stands 2705,9904, 2707,9904 and 2706,9905), the nearest of the
	// western chamber's 28 to the stairs by a walk.
	EwRockAt = tile.Tile{X: 2706, Z: 9904}
)

// EwRegion is where an account stands relative to the workshop.
type EwRegion int

const (
	// EwSurface is anywhere the walker reaches.
	EwSurface EwRegion = iota
	// EwSecretRoom is the five tiles behind the smithy's wall.
	EwSecretRoom
	// EwWorkshop is the workshop under Seers'.
	EwWorkshop
)

var (
	ewWorkshopBox   = Rect{MinX: 2690, MaxX: 2739, MinZ: 9871, MaxZ: 9916}
	ewSecretRoomBox = Rect{MinX: 2709, MaxX: 2711, MinZ: 3496, MaxZ: 3498}
)

// EwRegionOf is the region a tile is in.
func EwRegionOf(t tile.Tile) EwRegion {
	switch {
	case ewWorkshopBox.Contains(t):
		return EwWorkshop
	case ewSecretRoomBox.Contains(t):
		return EwSecretRoom
	}
	return EwSurface
}

// InEwPocket reports a tile of the workshop or the secret room: no walk leaves them.
func InEwPocket(t tile.Tile) bool { return EwRegionOf(t) != EwSurface }

// Custom step names (internal/scripts/questmods) and kit names.
const (
	CustomEwBook     = "ew-book"     // the book from the bookcase
	CustomEwRead     = "ew-read"     // the book read: the quest begins
	CustomEwSlash    = "ew-slash"    // the knife on the book: the key
	CustomEwEnter    = "ew-enter"    // the key on the wall, the stairs down
	CustomEwLeave    = "ew-leave"    // the stairs up, the wall pushed
	CustomEwSupplies = "ew-supplies" // the crates: the bowl, the needle, the leather
	CustomEwWater    = "ew-water"    // the valves and the lever
	CustomEwBellows  = "ew-bellows"  // the leather stitched over the bellows
	CustomEwLava     = "ew-lava"     // the bowl filled at the trough, emptied on the furnace
	CustomEwOre      = "ew-ore"      // the rock, the earth elemental, the ore
	CustomEwSmelt    = "ew-smelt"    // the ore and four coal at the furnace
	CustomEwShield   = "ew-shield"   // the bar on the workbench

	KitEwKnife = "ew-knife"
	KitEwKit   = "ew-kit"
)

// EwCoalNeeded is the coal the furnace takes with the ore ("You need four heaps of coal to smelt elemental ore.").
const EwCoalNeeded = 4

// ewMinBites is the bites of food the earth elemental's fight is entered with at the least; ewFoodAsk is the sharks the kit asks of the bank
// and the mule when the pack holds fewer bites than that.
const (
	ewMinBites = 4
	ewFoodAsk  = 6
)

// EwItemParkFor is how long the quest skips an account whose bank, mule and shops cannot give an item (the coal, the food, the knife): the
// mule's stock changes by the hour, not the minute. EwFightParkFor is how long it skips one the model says loses to the earth elemental
// (its levels do not change in an hour), and EwBoxParkFor one that has used a crate's once-only gift and lost it.
const (
	EwItemParkFor  = 3 * time.Hour
	EwFightParkFor = 6 * time.Hour
	EwBoxParkFor   = 24 * time.Hour
)

// ewFoodIDs are the foods the quest eats (Module.Sustain; the eat loop's floor bite takes any food the pack carries).
var ewFoodIDs = []int{385, 373, 379, 365, 361, 329, 1891, 1893, 1895, 333, 2309, 2142}

// ewSustain is the quest's survival policy: the foods it carries and the floor its eat line never goes under (the elemental's hardest hit is
// 4, so FoeEatLine keeps the floor).
var ewSustain = Sustain{FoodIDs: ewFoodIDs, EatAt: 14}

// ewTools are the quest's own items, every pickaxe the account may use and its coins: what the floor bite never eats and every bank visit
// of the quest leaves in the pack.
var ewTools = func() []int {
	tools := []int{ObjEwBook, ObjEwKey, ObjEwBowl, ObjEwBowlFull, ObjEwOre, ObjEwBar, ObjEwShield, ObjKnife, ObjHammer, ObjEwNeedle, ObjEwThread,
		ObjEwLeather, ObjEwCoal, CoinsID}
	for _, p := range doricPickaxes {
		tools = append(tools, p.ID)
	}
	return tools
}()

// ewKeep is what every bank step of the quest leaves in the pack: the tools and the food (the food is no tool: the module's tools are
// protected from the floor bite, which is the rule that eats any food the pack carries once an account falls to half its hitpoints on a
// walk, and the sharks are exactly the food it should eat).
var ewKeep = append(append([]int(nil), ewTools...), ewFoodIDs...)

// EwKeepIDs returns the keep list (a copy).
func EwKeepIDs() []int { return append([]int(nil), ewKeep...) }

// EwToolIDs returns the module's tools (a copy).
func EwToolIDs() []int { return append([]int(nil), ewTools...) }

// FoeEarthElementalRock is quest_elemental_workshop.npc [earth_elemental_rock_version] (npc 1023): attack 20, defence 35, strength 35,
// magic 10, 35 hitpoints, attackrate 6, a crush hit, no bonus params (a hardest hit of 4); the standing [elemental_earth] has the same stats.
var FoeEarthElementalRock = MeleeFoe{Name: "Earth elemental", Attack: 20, Strength: 35, Defence: 35, Magic: 10, Hitpoints: 35, AttackTicks: 6}

// ewMarkers are the journal's markers for the part of the quest the book and the key have reached, newest first:
// elemental_workshop_journal.rs2 keeps every earlier paragraph on the page, so the newest one decides.
var ewMarkers = []struct {
	stage int
	text  string
}{
	{EwComplete, "quest complete!"},
	{EwEntered, "secret door in the seers village smithy"},
	{EwSlashed, "cutting open the spine of the book"},
	{EwRead, "where is this workshop and how do i get in"},
	{EwNotStarted, "i can start this quest by reading a"},
}

// ewFlagMarkers are the journal's paragraphs of the machinery, shown once the stairs have been taken.
var ewFlagMarkers = []struct {
	flag int
	text string
}{
	{EwWater, "waterwheel in the northern water"},
	{EwBellows, "bellows in the eastern air area are now fixed"},
	{EwFurnace, "lit the furnace with lava"},
	{EwBar, "made a bar from the elemental ore"},
}

// ParseElementalWorkshopJournal reads the quest's progress from the rendered journal: EwNotStarted to EwComplete, plus the machinery flags
// when the stairs have been taken. ok is false for an empty or unrecognised page, which is unknown, never stage 0.
func ParseElementalWorkshopJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	for _, m := range ewMarkers {
		if !strings.Contains(text, m.text) {
			continue
		}
		stage := m.stage
		if stage == EwEntered {
			for _, f := range ewFlagMarkers {
				if strings.Contains(text, f.text) {
					stage |= f.flag
				}
			}
		}
		return stage, true
	}
	return 0, false
}

// EwSkillShort says which of the quest's three skills is under its level (levels the snapshot has not read are not judged), "" for none.
func EwSkillShort(s Snapshot) string {
	var short []string
	for _, k := range []string{"mining", "smithing", "crafting"} {
		if lvl := s.Level(k); lvl > 0 && lvl < EwSkill {
			short = append(short, fmt.Sprintf("%s %d/%d", skillLabel(k), lvl, EwSkill))
		}
	}
	return strings.Join(short, ", ")
}

// EwFightVerdict is EwElementalReady's answer.
type EwFightVerdict struct {
	OK  bool
	Why string
}

// EwElementalReady judges whether the pack can take the rock's earth elemental: the account's own swing (MeleeChain: the shared fight step
// melees) from the level a fight starts at (FightStartHP), eating by the foe's eat line, and it must win with a tenth of its bites left for
// the way out and never fall under his hardest hit. The reason names the account's levels and the food it holds, for the log.
func EwElementalReady(s Snapshot) EwFightVerdict {
	bites := len(FoodBites(s.InvIDs, ewFoodIDs))
	if bites == 0 {
		return EwFightVerdict{Why: fmt.Sprintf("no food in the pack for the earth elemental (%d hitpoints, hits up to %d)",
			FoeEarthElementalRock.Hitpoints, FoeEarthElementalRock.MaxHit())}
	}
	odds := MeleeChain(s, []MeleeFoe{FoeEarthElementalRock}, ewSustain)
	if odds.OK {
		return EwFightVerdict{OK: true}
	}
	return EwFightVerdict{Why: fmt.Sprintf("%s (the earth elemental: Attack %d, Strength %d, Defence %d, %d/%d hitpoints, %d bites of food)",
		odds.Why, s.Level("attack"), s.Level("strength"), s.Level("defence"), s.HP, s.MaxHP, bites)}
}

// EwPickaxeFor is the id of the best pickaxe in the pack or worn that a Mining level can use (pickaxe_checker.rs2), 0 for none.
func EwPickaxeFor(pack, worn map[int]int, mining int) int {
	for _, p := range doricPickaxes {
		if mining >= p.Level && (pack[p.ID] > 0 || worn[p.ID] > 0) {
			return p.ID
		}
	}
	return 0
}

// ewHasBar reports the bar in the pack, or the journal saying so.
func ewHasBar(s Snapshot) bool { return s.HeldID(ObjEwBar) > 0 || EwHas(s.Stage, EwBar) }

// ewNeedsOre reports that the ore is still to be got: neither the ore nor the bar made of it is in hand.
func ewNeedsOre(s Snapshot) bool { return s.HeldID(ObjEwOre) == 0 && !ewHasBar(s) }

// ewKitLines is the kit the rest of the work wants in the pack, from where the work stands: the bench's hammer, a pickaxe the Mining level
// can use and food when the ore is still to be fought for, the coal when the bar is still to be smelted, the thread when the bellows are
// still to be mended (the needle and the leather are optional here: the workshop's crates give them once). The hammer, the pickaxe and the
// thread have shops; the coal and the food only the bank and the mule.
func ewKitLines(s Snapshot) []KitLine {
	var kit []KitLine
	if s.HeldID(ObjHammer) == 0 {
		kit = append(kit, KitLine{ID: ObjHammer, Name: "Hammer", Qty: 1, Sources: []Source{
			{Kind: SourceShop, Shop: ArheinStore, Price: 5, Note: "Arhein's store in Catherby keeps five"}}})
	}
	if ewNeedsOre(s) && DoricUsablePickaxe(s) == 0 {
		if id := DoricBankedPickaxe(s); id != 0 {
			for _, p := range doricPickaxes {
				if p.ID == id {
					kit = append(kit, KitLine{ID: p.ID, Name: p.Name, Qty: 1})
				}
			}
		} else {
			kit = append(kit, KitLine{ID: ObjBronzePickaxe, Name: "Bronze pickaxe", Qty: 1, Sources: []Source{
				{Kind: SourceShop, Shop: ArheinStore, Price: 1, Note: "Arhein's store in Catherby keeps two"}}})
		}
	}
	if !ewHasBar(s) && s.HeldID(ObjEwCoal) < EwCoalNeeded {
		kit = append(kit, KitLine{ID: ObjEwCoal, Name: "Coal", Qty: EwCoalNeeded})
	}
	if !EwHas(s.Stage, EwBellows) {
		if s.HeldID(ObjEwThread) == 0 {
			kit = append(kit, KitLine{ID: ObjEwThread, Name: "Thread", Qty: 1, Stackable: true, Sources: []Source{
				{Kind: SourceShop, Shop: DommikShop, Price: 1, Note: "Dommik's crafting store in Al Kharid keeps a hundred"}}})
		}
		if s.HeldID(ObjEwNeedle) == 0 {
			kit = append(kit, KitLine{ID: ObjEwNeedle, Name: "Needle", Qty: 1, Stackable: true, Optional: true})
		}
		if s.HeldID(ObjEwLeather) == 0 {
			kit = append(kit, KitLine{ID: ObjEwLeather, Name: "Leather", Qty: 1, Optional: true})
		}
	}
	if ewNeedsOre(s) && len(FoodBites(s.InvIDs, ewFoodIDs)) < ewMinBites {
		kit = append(kit, KitLine{ID: ObjShark, Name: "Shark", Qty: ewFoodAsk})
	}
	return kit
}

// ewRequiredShort lists the kit's required lines the pack is short of.
func ewRequiredShort(s Snapshot, kit []KitLine) []KitLine {
	var short []KitLine
	for _, l := range kit {
		if !l.Optional && l.Needs(s.InvIDs, s.WornIDs) > 0 {
			short = append(short, l)
		}
	}
	return short
}

// ewOptionalShort reports an optional kit line the pack is short of.
func ewOptionalShort(s Snapshot, kit []KitLine) bool {
	for _, l := range kit {
		if l.Optional && l.Needs(s.InvIDs, s.WornIDs) > 0 {
			return true
		}
	}
	return false
}

func ewNames(lines []KitLine) string {
	names := make([]string, 0, len(lines))
	for _, l := range lines {
		names = append(names, strings.ToLower(l.Name))
	}
	return strings.Join(names, ", ")
}

func ewCustom(name, reason string) Step { return Step{Kind: StepCustom, Custom: name, Reason: reason} }

func ewDeposit(reason string) Step {
	return Step{Kind: StepDeposit, KeepIDs: ewKeep, Bank: SeersBank, BankSet: true, Reason: reason}
}

// ewKitStep is the provisioning round the work still wants, if any: the required lines first (and a wait that parks the quest once every
// source of one is dead), then the optional ones once a run.
func ewKitStep(s Snapshot) (Step, bool) {
	kit := ewKitLines(s)
	short := ewRequiredShort(s, kit)
	if len(short) > 0 {
		if s.Blocked["provision:"+KitEwKit] {
			return Step{Kind: StepWait, ParkFor: EwItemParkFor,
				Reason: "no " + ewNames(short) + " from the bank, the mule or the shops: the workshop cannot be done without"}, true
		}
		return Step{Kind: StepProvision, KitName: KitEwKit, Kit: kit, KeepIDs: ewKeep, Bank: SeersBank, BankSet: true,
			Reason: "the workshop's kit (" + ewNames(short) + ")"}, true
	}
	if ewOptionalShort(s, kit) && !s.Ran["provision:"+KitEwKit] && !s.Failed["provision:"+KitEwKit] {
		return Step{Kind: StepProvision, KitName: KitEwKit, Kit: kit, KeepIDs: ewKeep, Bank: SeersBank, BankSet: true,
			Reason: "a needle and a leather, if the bank or the mule has them"}, true
	}
	return Step{}, false
}

// ewKeyStep cuts the key: the book (the bookcase), a knife (the bank, the mule) and the knife used on the book.
func ewKeyStep(s Snapshot) Step {
	if s.HeldID(ObjEwBook) == 0 {
		if s.FreeSlots < 1 {
			return ewDeposit("room for the book the bookcase gives")
		}
		return ewCustom(CustomEwBook, "the Battered book from the bookcase in Seers' Village")
	}
	if s.HeldID(ObjKnife) == 0 {
		if s.Blocked["provision:"+KitEwKnife] {
			return Step{Kind: StepWait, ParkFor: EwItemParkFor, Reason: "no knife from the bank or the mule to cut the book's spine"}
		}
		return Step{Kind: StepProvision, KitName: KitEwKnife, Kit: []KitLine{{ID: ObjKnife, Name: "Knife", Qty: 1}}, KeepIDs: ewKeep,
			Bank: SeersBank, BankSet: true, Reason: "a knife to cut the book's spine"}
	}
	return ewCustom(CustomEwSlash, "the knife through the book's spine: a key")
}

// ewOutside is the decision on the surface or in the secret room, stage 2 and later: the key, the book, the kit, the odds and the way in.
func ewOutside(s Snapshot, region EwRegion) Step {
	leave := func(reason string) Step { return ewCustom(CustomEwLeave, reason) }
	// The key opens the wall from outside only: an account already in the secret room goes on down without it.
	if s.HeldID(ObjEwKey) == 0 && region == EwSurface {
		return ewKeyStep(s)
	}
	if s.HeldID(ObjEwBook) == 0 {
		if region != EwSurface {
			return leave("out of the secret room for the book")
		}
		if s.FreeSlots < 1 {
			return ewDeposit("room for the book the bookcase gives")
		}
		return ewCustom(CustomEwBook, "the Battered book from the bookcase in Seers' Village (the bench wants it)")
	}
	if st, ok := ewKitStep(s); ok {
		if region != EwSurface && st.Kind != StepWait {
			return leave("out of the secret room for the workshop's kit")
		}
		return st
	}
	if ewNeedsOre(s) {
		if v := EwElementalReady(s); !v.OK {
			if region != EwSurface {
				return leave("out of the secret room: the pack cannot take the earth elemental")
			}
			return Step{Kind: StepWait, ParkFor: EwFightParkFor, Reason: "not going down to the rock's earth elemental with this pack: " + v.Why}
		}
	}
	return ewCustom(CustomEwEnter, "the key on the smithy's wall, the stairs down")
}

// ewInside is the decision in the workshop: the missing items send the account up, the machinery comes in an order that works, then the
// ore, the bar and the shield.
func ewInside(s Snapshot) Step {
	hasBar, hasOre := ewHasBar(s), s.HeldID(ObjEwOre) > 0
	if s.HeldID(ObjEwShield) > 0 {
		return Step{Kind: StepWait, Reason: "waiting for the Elemental Workshop's quest scroll"}
	}
	if s.HeldID(ObjEwBook) == 0 || s.HeldID(ObjHammer) == 0 {
		return ewCustom(CustomEwLeave, "up for the book or the hammer the bench wants")
	}
	if !EwHas(s.Stage, EwBellows) && s.HeldID(ObjEwThread) == 0 {
		return ewCustom(CustomEwLeave, "up for a thread to mend the bellows")
	}
	if !hasBar && s.HeldID(ObjEwCoal) < EwCoalNeeded {
		return ewCustom(CustomEwLeave, "up for the four coal the furnace takes")
	}
	if !hasBar && !hasOre && DoricUsablePickaxe(s) == 0 {
		return ewCustom(CustomEwLeave, "up for a pickaxe the Mining level can use")
	}
	if !EwHas(s.Stage, EwWater) {
		return ewCustom(CustomEwWater, "the two valves and the lever: the water wheel")
	}
	if !EwHas(s.Stage, EwBellows) {
		for _, c := range []struct {
			id   int
			what string
		}{{ObjEwNeedle, "a needle"}, {ObjEwLeather, "a leather"}} {
			if s.HeldID(c.id) > 0 {
				continue
			}
			if s.Failed[CustomEwSupplies] {
				return Step{Kind: StepWait, ParkFor: EwBoxParkFor,
					Reason: "the workshop's crates have no needle or leather left for this account (one each, ever) and it holds none"}
			}
			return Step{Kind: StepCustom, Custom: CustomEwSupplies, ItemID: c.id, Reason: c.what + " from the crates"}
		}
		return ewCustom(CustomEwBellows, "the leather stitched over the bellows")
	}
	if !EwHas(s.Stage, EwFurnace) {
		if s.HeldID(ObjEwBowl)+s.HeldID(ObjEwBowlFull) == 0 {
			return Step{Kind: StepCustom, Custom: CustomEwSupplies, ItemID: ObjEwBowl, Reason: "a stone bowl from the crate"}
		}
		return ewCustom(CustomEwLava, "lava from the trough onto the furnace")
	}
	if !hasBar && !hasOre {
		if v := EwElementalReady(s); !v.OK {
			return ewCustom(CustomEwLeave, "up: the pack cannot take the earth elemental: "+v.Why)
		}
		return ewCustom(CustomEwOre, "the rock, its earth elemental and the elemental ore")
	}
	if !hasBar {
		return ewCustom(CustomEwSmelt, "the ore and four coal at the furnace")
	}
	return ewCustom(CustomEwShield, "the bar on the workbench: the Elemental shield")
}

// DecideElementalWorkshop is the whole quest, read from the journal's stage, the account's tile and the pack.
func DecideElementalWorkshop(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	}
	if !s.TileKnown {
		return Step{Kind: StepWait, Reason: "the account's position has not been read"}
	}
	region := EwRegionOf(s.Tile)
	if why := EwSkillShort(s); why != "" {
		if region != EwSurface {
			return ewCustom(CustomEwLeave, "up: "+why)
		}
		return Step{Kind: StepWait, ParkFor: EwFightParkFor, Reason: "the Elemental Workshop wants Mining, Smithing and Crafting 20: " + why}
	}
	if s.Journal == StatusNotStarted {
		if region != EwSurface {
			return ewCustom(CustomEwLeave, "up from the workshop: the quest is not started")
		}
		if s.HeldID(ObjEwBook) == 0 {
			if s.FreeSlots < 1 {
				return ewDeposit("room for the book the bookcase gives")
			}
			return ewCustom(CustomEwBook, "the Battered book from the bookcase in Seers' Village")
		}
		return ewCustom(CustomEwRead, "read the Battered book: the quest begins")
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	stage := EwStageOf(s.Stage)
	switch {
	case stage == EwNotStarted || stage >= EwComplete:
		return Step{Kind: StepWait, Reason: "the journal and the quest tab disagree about the Elemental Workshop"}
	case stage == EwRead:
		if region != EwSurface {
			return ewCustom(CustomEwLeave, "up from the workshop: the key is not cut")
		}
		return ewKeyStep(s)
	}
	if region == EwWorkshop {
		return ewInside(s)
	}
	return ewOutside(s, region)
}

// ElementalWorkshopRecord is the quest's identity: the three skills at 20 are the content's own and the Record states them so the ladder
// does not start an account that has none of them.
var ElementalWorkshopRecord = Record{ID: ElementalWorkshopID, Name: ElementalWorkshopName, QuestPoints: 1,
	Requirements: Requirements{Skills: []SkillReq{{Skill: "mining", Level: EwSkill}, {Skill: "smithing", Level: EwSkill}, {Skill: "crafting", Level: EwSkill}}}}

// ElementalWorkshop is the engine module. The earth elemental is fought (a lost fight is a near-death); two deaths anywhere park it for a
// day and one at the rock does too (StepDeathCaps).
var ElementalWorkshop = Module{
	Record: ElementalWorkshopRecord, Bank: SeersBank, BankSet: true, Anchor: EwBookcaseAt,
	ToolIDs:   ewTools,
	ReadStage: ParseElementalWorkshopJournal,
	Sustain:   ewSustain,
	MaxDeaths: 2,
	StepDeathCaps: map[string]StepDeathCap{
		CustomEwOre: {Max: 1, Window: 24 * time.Hour, Skip: 24 * time.Hour},
	},
	Decide: DecideElementalWorkshop,
}
