package questplan

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Dwarf Cannon (content id `mcannon`, varps %mcannon and the bit field %mcannonmulti, 1 quest point, 750 Crafting experience), ported
// from the TS def (defs/dwarfcannon/*.ts) and checked against content 289: quests/quest_mcannon (quest_mcannon.constant, npcs/lawgof2,
// nulodion and dwarfchildtw1, locs/railings, mcannon_ladders, mcannon_cave, mcannon_crate, mcannon_doors and mcannon_broken_cannon,
// objs/dwarf_remains, mcannon_journal.rs2) and general/scripts/ladders.rs2.
//
// THE QUEST. The Dwarf Commander of the Black Guard, north-west of the Fishing Guild, wants six broken railings in his camp's fence
// replaced (he hands over six railings; each Inspect of a broken one offers to replace it, a Crafting roll that on a failure cuts the
// account for up to 3 hitpoints), then the guard in the watchtower to the south looked for (a ladder up, a second ladder up, and the dwarf
// remains on the top floor, which are taken back to him), then the goblins' base found (the Cave Entrance beside the Fishing Guild's road)
// and Gilob's son Lollk freed from a crate at the far end of it; the mud pile at the cave's mouth is the way out. He then hands over a tool
// kit for the broken multicannon in the shed (four components to mend with Crafting or Smithing, a roll each, no hurt on a failure), is
// told, goes to see it, sends the account to Nulodion at the Black Guard camp south of Ice Mountain (a door that opens from stage 9) for the
// ammo mould and the notes, and takes both for the reward.
//
// THE STATE. %mcannon is 0 to 11 and the journal prints it, with two sub-states that decide a step and are not in the varp: stage 1 with all
// six railing bits set (the page then says to report back; McRailingsDone) and stage 2 with the remains in the PACK (the page says so;
// McTowerRemains). The second is not a convenience: the dwarf remains are obj id 0, which the client's inventory model reads as an empty slot
// (gamestate.readObjID: the wire's id 0 is empty and id 1 is obj 0), so no snapshot ever holds them and the journal is the only reader.
// Stages 6 and 7 read alike (the first Inspect flips 6 to 7, the next opens the repair menu) and the parser returns McFixCannon for both.
//
// WHAT CAN HURT. The Crafting roll on a railing (stat_random(crafting, 60, 150): 24% at Crafting 1, 38% at 40, 45% at 60) cuts the account
// for min(3, hitpoints / 10) on a failure, and health comes back one point in 100 ticks, so the railings are mended behind a hitpoint floor
// with food and are given as many tries as the run has time for; the bits stay set from run to run. The cannon's roll hurts nobody. The
// goblins of the camp, the tower and the cave (levels 2, 5 and 13) have no hunt mode and attack nobody who does not attack them (the account
// does not: auto-retaliate stays as it is and the walks run on); the hunters near the route are the camp's giant bats (level 27, huntrange 2,
// wander 8: the legs round the commander and the railings) and the cave's (the route passes four of them) and dungeon rats (level 12),
// cowardly hunters that skip an account of combat above twice their level, 54 for the bats. An account the bats hunt is priced against two of
// them in the shared fight model, on its hitpoints and its food (CannonHuntFit), and parked for 3 hours when the model loses.
//
// THE SHARED THINGS. The dwarf remains are one ground spawn on the tower's top floor, and the dwarf child is one npc that the crate adds for
// 120 ticks (a second search inside that time finds nothing). The quest is therefore a three-account slot, released once the child is out
// (Module.ExclusiveDone).

// DwarfCannonName is the quest tab's spelling (component 7356, `questlist:mcannon`).
const DwarfCannonName = "Dwarf Cannon"

// DwarfCannonID is the engine's id.
const DwarfCannonID = "mcannon"

// The stages the journal parser returns: %mcannon's own (quest_mcannon.constant), and two sub-states of stages 1 and 2.
const (
	McNotStarted   = 0
	McRailings     = 1
	McTower        = 2
	McGoblinCave   = 3
	McFindChild    = 4
	McChildRescued = 5
	McFixCannon    = 6 // 6 and 7 read alike
	McCannonFixed  = 8
	McSeeNulodion  = 9
	McReturnNotes  = 10
	McComplete     = 11
	// McRailingsDone is stage 1 with all six railing bits set: the page says to report back to the Commander.
	McRailingsDone = 21
	// McTowerRemains is stage 2 with the dwarf remains in the pack.
	McTowerRemains = 22
)

// Ids from content/pack (checked against the production config by TestDwarfCannonIDsMatchTheProductionConfig).
const (
	NpcCannonCommander = 208 // [lawgof2], Dwarf Commander
	NpcNulodion        = 209 // [nulodion]
	NpcCannonChild     = 207 // [dwarfchildtw1], added by the crate for 120 ticks
	NpcCannonGuard     = 206 // [mcannonguard], level 10, no hunt mode

	ObjCannonRemains = 0  // [mcannonremains]: obj id 0, which the client reads as an empty slot
	ObjCannonToolkit = 1  // [mcannontoolkit]
	ObjCannonNotes   = 3  // [nulodions_notes]
	ObjCannonMould   = 4  // [ammo_mould]
	ObjCannonRailing = 14 // [mcannonrailing1_obj]: the six railings the Commander gives, untradeable

	LocCannonCrate        = 1    // [mcannoncrateboy]
	LocCannonCave         = 2    // [mcannoncave], 4x2, op Enter
	LocCannonNulodionDoor = 3    // [mcannondoor], op Open (locked below stage 9)
	LocCannonShedDoor     = 4    // [mcannondoor1], op Open (locked below stage 6)
	LocCannonBroken       = 5    // [broken_multicannon], 3x3, op Inspect
	LocCannonMudPile      = 13   // [mcanmudpile], op Climb-over
	LocCannonRailingA     = 15   // [mcannonrailing1] .. [mcannonrailing6] are 15..20, op Inspect
	LocCannonLadderUp     = 1747 // [ladder], op Climb-up
	LocCannonLadderDown   = 1746 // [laddertop], op Climb-down
)

// CannonRailings is the number of broken railings.
const CannonRailings = 6

// Tiles from content maps and the loc index.
var (
	CannonCommanderAnchor = tile.Tile{X: 2571, Z: 3463}
	NulodionAnchor        = tile.Tile{X: 3011, Z: 3453}
	// CannonCrate is the crate Lollk is tied up in, in the cave; CannonCaveEntrance the Cave Entrance (4x2) beside the Fishing Guild's road,
	// whose stands include (2623,3391); CannonCaveArrive where the entrance's telejump lands, and CannonMudPile the mud pile at the
	// cave's mouth (stands 2620,9797 and 2620,9796); CannonCaveExit where its climb lands.
	CannonCrate        = tile.Tile{X: 2571, Z: 9850}
	CannonCaveEntrance = tile.Tile{X: 2622, Z: 3392}
	CannonCaveStand    = tile.Tile{X: 2623, Z: 3391}
	CannonCaveArrive   = tile.Tile{X: 2620, Z: 9797}
	CannonMudPile      = tile.Tile{X: 2621, Z: 9796}
	CannonCaveExit     = tile.Tile{X: 2623, Z: 3391}
	// The watchtower: the ground floor's ladder at 2570,3441, the first floor's down ladder at 2570,3441 and up ladder at 2570,3443, the
	// top floor's down ladder at 2570,3443. Every climb lands on the account's own tile one floor up or down (~climb_ladder with
	// movecoord(coord(), 0, 1, 0)), so one tile serves them all: 2570,3442, between the ladders, walkable on all three floors.
	CannonTowerStand   = tile.Tile{X: 2570, Z: 3442}
	CannonTowerStand1  = tile.Tile{X: 2570, Z: 3442, Level: 1}
	CannonTowerStand2  = tile.Tile{X: 2570, Z: 3442, Level: 2}
	CannonRemainsSpawn = tile.Tile{X: 2567, Z: 3444, Level: 2}
	// CannonShedStand is just inside the shed's door, beside the cannon (stands 2576,3461 and 2576,3462).
	CannonShedStand  = tile.Tile{X: 2576, Z: 3462}
	CannonShedCannon = tile.Tile{X: 2577, Z: 3461}
	// CannonRailingSpots are the six broken railings in order (loc 15 to 20), the engine's %mcannonmulti bit is the loc id minus 10.
	CannonRailingSpots = [CannonRailings]tile.Tile{
		{X: 2556, Z: 3475}, {X: 2558, Z: 3472}, {X: 2557, Z: 3464}, {X: 2559, Z: 3462}, {X: 2564, Z: 3460}, {X: 2572, Z: 3460},
	}
)

// CannonRailingLoc is the loc id of railing i (0 to 5).
func CannonRailingLoc(i int) int { return LocCannonRailingA + i }

// CannonParts are the repair menu's four components in the order the module mends them (%mcannonmulti bits 0 to 3).
var CannonParts = [4]string{"Pipe", "Barrel", "Axle", "Shaft"}

// InCannonCave reports a tile of the goblins' cave, the mapsquare m40_153 (every tile the planner reaches from its arrival lies inside).
func InCannonCave(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 2560 && t.X <= 2623 && t.Z >= 9792 && t.Z <= 9855
}

// Custom step names (internal/scripts/questmods) and kit names.
const (
	CustomCannonRailings = "cannon-railings"
	CustomCannonTower    = "cannon-tower"
	CustomCannonChild    = "cannon-child"
	CustomCannonOut      = "cannon-out"
	CustomCannonRepair   = "cannon-repair"
	CustomCannonReport   = "cannon-report"

	KitCannonFood = "cannon-food"
)

// Numbers.
const (
	// CannonMinHitpoints is the least maximum Hitpoints the quest starts at: the railings cut up to 3 and the bats hit up to 3.
	CannonMinHitpoints = 20
	// CannonStartRoom is the free pack slots wanted before the Commander is first asked: he hands over six railings (one slot each).
	CannonStartRoom = 8
	// CannonBatCeiling is the combat level above which the giant bats (level 27, cowardly) do not hunt the account: twice their level.
	CannonBatCeiling = 54
	// CannonBatsAtOnce is how many bats the fit prices: the camp's three within range of the commander and the cave's four on the route are
	// met one or two at a time.
	CannonBatsAtOnce = 2
	// CannonCutMax is the hurt of a failed railing: min(3, hitpoints / 10).
	CannonCutMax = 3

	// CannonLegPark is how long the quest skips an account whose leg failed (a tower without remains, a crate that gave no child, a shed
	// that would not open); the legs are bounded by minutes, so this is a failure and not a wait.
	CannonLegPark = 30 * time.Minute
	// CannonFitPark is how long it skips an account the bats' price is too high for.
	CannonFitPark = 3 * time.Hour
)

// CannonFoodIDs are the foods the quest eats, best first.
var CannonFoodIDs = []int{385, 373, 379, 365, 361, 329, 333, 2309, 1891, 1893, 1895}

var cannonSustain = Sustain{FoodIDs: CannonFoodIDs, EatAt: 12}

// cannonTools are the quest's own items, which every bank visit of the quest leaves in the pack: the six railings, the tool kit, the notes and
// the mould (all untradeable) and the food.
var cannonTools = []int{ObjCannonRailing, ObjCannonToolkit, ObjCannonNotes, ObjCannonMould}

// cannonKeep is the keep list of a bank visit.
func cannonKeep() []int { return append(append([]int(nil), cannonTools...), CannonFoodIDs...) }

// CannonRailingCut is the most a failed railing costs an account with maxHP hitpoints.
func CannonRailingCut(maxHP int) int { return min(CannonCutMax, maxHP/10) }

// CannonRailingFloor is the hitpoints the railings are tried at and above: enough that two cuts in a row leave the account standing with a
// bat's hit to spare, never under two fifths of the account's health, and above the engine's own eat line (cannonSustain.EatAt): a roll made
// at or under that line has the engine's eat loop clicking a bite while the offer's menu is open, and an inventory click closes the modal
// and drops the script. Never above the account's maximum.
func CannonRailingFloor(maxHP int) int {
	return min(max(2*CannonRailingCut(maxHP)+3, maxHP*2/5, cannonSustain.EatAt+1), maxHP)
}

// The stops. The talks with no menu carry a placeholder preference (a talk step must name one).
var (
	// CannonCommanderStop is every conversation with the Commander: the three accept lines never share a menu with each other
	// ("I'm sorry, I'm too busy mining." / "Yeah, I'd love to help." at the start; "Ok, I'll see what I can do." / "Sorry, I've done
	// enough for today." with the tool kit; "Sorry, I've really done enough." / "Ok then, just for you!" after the cannon), and every other
	// page has no menu.
	CannonCommanderStop = NpcStop{Npc: "Dwarf Commander", ID: NpcCannonCommander, Anchor: CannonCommanderAnchor, Leash: 8,
		Prefer: []string{"Yeah, I'd love to help.", "Ok, I'll see what I can do.", "Ok then, just for you!"}}
	// NulodionStop is Nulodion's talk (no menu at stage 9 or 10; the shop is op 3).
	NulodionStop = NpcStop{Npc: "Nulodion", ID: NpcNulodion, Anchor: NulodionAnchor, Leash: 8, Prefer: []string{"Hello."}}
)

// cannonMarkers are the journal's pages, newest stage first. mcannon_journal.rs2 keeps the earlier paragraphs on the page, so a later
// stage's needle is tried before an earlier one's.
var cannonMarkers = []struct {
	stage int
	text  string
}{
	{McComplete, "quest complete!"},
	{McReturnNotes, "gave me an ammo mould and notes"},
	{McSeeNulodion, "asked me to find nulodion"},
	{McCannonFixed, "fixed the broken multicannon"},
	{McFixCannon, "asked me to fix the multicannon"},
	{McChildRescued, "rescued the dwarf child"},
	{McFindChild, "next i need to find the"},
	{McGoblinCave, "sent me to find the goblin base"},
	{McTowerRemains, "i went to the watchtower"},
	{McTower, "check up on his guards"},
	{McRailingsDone, "repaired all the broken railings"},
	{McRailings, "my first task is to"},
	{McNotStarted, "i can start this quest"},
}

// ParseDwarfCannonJournal reads the stage from the rendered journal. ok is false for an empty or unrecognised page, which is unknown,
// never stage 0.
func ParseDwarfCannonJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	for _, m := range cannonMarkers {
		if strings.Contains(text, m.text) {
			return m.stage, true
		}
	}
	return 0, false
}

// CannonStageNumber is the %mcannon value a parsed stage stands for (the sub-states belong to 1 and 2).
func CannonStageNumber(stage int) int {
	switch stage {
	case McRailingsDone:
		return McRailings
	case McTowerRemains:
		return McTower
	}
	return stage
}

// CannonPastTheChild reports that the account is past the quest's one-at-a-time work (the tower's remains and the crate's child): the
// child is out, or the quest is done.
func CannonPastTheChild(s Snapshot) bool {
	return s.Journal == StatusComplete || (s.StageKnown && CannonStageNumber(s.Stage) >= McChildRescued)
}

// cannonBatFoes are the hunters the account is priced against.
func cannonBatFoes() []MeleeFoe {
	foes := make([]MeleeFoe, CannonBatsAtOnce)
	for i := range foes {
		foes[i] = FoeGiantBat
	}
	return foes
}

// CannonHuntFit is the gate before the camp and the cave: the hitpoints the railings and the bats need, and, for an account the bats
// hunt (combat 54 and under, or unread), the shared fight model's verdict on two of them with the food in the pack. An account above
// the bats' ceiling meets none of them and is not priced. An unread maximum or combat level passes, as every other gate here treats an
// unread stat.
func CannonHuntFit(s Snapshot) (bool, string) {
	if s.MaxHP > 0 && s.MaxHP < CannonMinHitpoints {
		return false, fmt.Sprintf("not doing Dwarf Cannon yet: a railing's failed roll cuts for up to %d and the giant bats near the camp and in the goblins' cave hit up to %d (this account has %d hitpoints, the gate is %d)",
			CannonCutMax, FoeGiantBat.PeakHit(), s.MaxHP, CannonMinHitpoints)
	}
	combat := DemonCombat(s)
	if combat != 0 && combat > CannonBatCeiling+demonCombatMargin {
		return true, ""
	}
	if odds := MeleeChain(s, cannonBatFoes(), cannonSustain); !odds.OK {
		return false, fmt.Sprintf("not going near the giant bats of the Black Guard camp and the goblins' cave with this pack (they hunt every account of combat %d and under, this one is %d): %s",
			CannonBatCeiling, combat, odds.Why)
	}
	return true, ""
}

// cannonFoodKit is the food the railings and the bats are met with: four sharks, from the bank or the mule, optional.
func cannonFoodKit() []KitLine {
	return []KitLine{{ID: ObjShark, Name: "Shark", Qty: 4, Optional: true}}
}

// cannonFoodHeld is the hitpoints of food the pack holds.
func cannonFoodHeld(s Snapshot) int { return FoodPoolHeal(s.InvIDs, CannonFoodIDs) }

// cannonFood is the once-a-run food round (bank, then mule), before anything that costs hitpoints.
func cannonFood(s Snapshot) (Step, bool) {
	if cannonFoodHeld(s) > 0 || s.Ran["provision:"+KitCannonFood] || s.Failed["provision:"+KitCannonFood] {
		return Step{}, false
	}
	return Step{Kind: StepProvision, KitName: KitCannonFood, Kit: cannonFoodKit(), KeepIDs: cannonKeep(), Bank: EastArdougneNorth, BankSet: true,
		Reason: "food for the railings' cuts and the giant bats"}, true
}

// cannonRoom is the deposit that makes room for what an NPC is about to hand over.
func cannonRoom(s Snapshot, need int, what string) (Step, bool) {
	if s.FreeSlots >= need {
		return Step{}, false
	}
	return Step{Kind: StepDeposit, KeepIDs: cannonKeep(), Bank: EastArdougneNorth, BankSet: true, Reason: "room for " + what}, true
}

// cannonFit is the park when the bats' price is too high.
func cannonFit(s Snapshot) (Step, bool) {
	if ok, why := CannonHuntFit(s); !ok {
		return Step{Kind: StepWait, ParkFor: CannonFitPark, Reason: why}, true
	}
	return Step{}, false
}

// cannonCustom is a custom step, or the park when it failed last time.
func cannonCustom(s Snapshot, name, reason, failed string) Step {
	if s.Failed[name] {
		return Step{Kind: StepWait, ParkFor: CannonLegPark, Reason: failed}
	}
	return Step{Kind: StepCustom, Custom: name, Reason: reason}
}

// DecideDwarfCannon is the whole quest, read from the journal's stage and the pack.
func DecideDwarfCannon(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return decideCannonStart(s)
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	talk := func(reason string) Step { return Step{Kind: StepTalk, Stop: CannonCommanderStop, Reason: reason} }
	switch s.Stage {
	case McNotStarted:
		return decideCannonStart(s)
	case McRailings:
		if st, ok := cannonFood(s); ok {
			return st
		}
		if st, ok := cannonFit(s); ok {
			return st
		}
		if s.HeldID(ObjCannonRailing) == 0 {
			return talk("the Commander gives another railing to a pack with none")
		}
		return cannonCustom(s, CustomCannonRailings, "replace the six broken railings",
			"the broken railings were not all replaced in the time the leg had (a roll that keeps failing, or hitpoints that will not come back without food)")
	case McRailingsDone:
		return talk("report the railings to the Commander")
	case McTower:
		return cannonCustom(s, CustomCannonTower, "take the dwarf remains from the watchtower's top floor",
			"no dwarf remains on the watchtower's top floor, or the ladders would not take the account up")
	case McTowerRemains:
		return talk("the dwarf remains to the Commander")
	case McGoblinCave, McFindChild:
		if st, ok := cannonFood(s); ok {
			return st
		}
		if st, ok := cannonFit(s); ok {
			return st
		}
		return cannonCustom(s, CustomCannonChild, "the goblins' cave, and Gilob's son in the crate at the far end",
			"the goblins' cave would not open, or the crate gave no dwarf child (another account freed him a minute ago, or the way in is shut)")
	case McChildRescued:
		if InCannonCave(s.Tile) {
			return Step{Kind: StepCustom, Custom: CustomCannonOut, Reason: "out of the goblins' cave by the mud pile"}
		}
		if st, ok := cannonRoom(s, 1, "the tool kit"); ok {
			return st
		}
		return talk("the tool kit for the multicannon")
	case McFixCannon:
		if s.HeldID(ObjCannonToolkit) == 0 {
			if st, ok := cannonRoom(s, 1, "the tool kit"); ok {
				return st
			}
			return talk("the Commander gives another tool kit to a pack with none")
		}
		return cannonCustom(s, CustomCannonRepair, "repair the broken multicannon in the shed",
			"the multicannon would not be repaired (the shed's door, or four rolls that keep failing)")
	case McCannonFixed:
		return cannonCustom(s, CustomCannonReport, "tell the Commander the cannon works and take his errand to Nulodion",
			"the Commander's talk about the repaired cannon did not run to its end")
	case McSeeNulodion:
		if st, ok := cannonRoom(s, 3, "Nulodion's notes and the ammo mould"); ok {
			return st
		}
		return Step{Kind: StepTalk, Stop: NulodionStop, Reason: "Nulodion's ammo mould and notes"}
	case McReturnNotes:
		if s.HeldID(ObjCannonNotes) > 0 && s.HeldID(ObjCannonMould) > 0 {
			return talk("the mould and the notes to the Commander")
		}
		if st, ok := cannonRoom(s, 3, "Nulodion's notes and the ammo mould"); ok {
			return st
		}
		return Step{Kind: StepTalk, Stop: NulodionStop, Reason: "Nulodion replaces a lost mould or notes"}
	case McComplete:
		return Step{Kind: StepDone}
	}
	return Step{Kind: StepWait, Reason: fmt.Sprintf("unrecognised Dwarf Cannon stage %d", s.Stage)}
}

// decideCannonStart is stage 0: the food, the gate, the room for the six railings, then the Commander.
func decideCannonStart(s Snapshot) Step {
	// The food first: a hunted account is priced on the bites it carries, and the bank and the mule are asked once a run before the price.
	if st, ok := cannonFood(s); ok {
		return st
	}
	if st, ok := cannonFit(s); ok {
		return st
	}
	if st, ok := cannonRoom(s, CannonStartRoom, "the six railings the Commander hands over"); ok {
		return st
	}
	return Step{Kind: StepTalk, Stop: CannonCommanderStop, Reason: "start Dwarf Cannon"}
}

// DwarfCannonRecord is the quest's identity. The server gates nothing; Hitpoints 20 is the module's own floor.
var DwarfCannonRecord = Record{ID: DwarfCannonID, Name: DwarfCannonName, QuestPoints: 1,
	Requirements: Requirements{Skills: []SkillReq{{Skill: "hitpoints", Level: CannonMinHitpoints}}}}

// DwarfCannon is the engine module. One death parks it for a day: the bats are priced, so a death means the price was wrong for this account.
var DwarfCannon = Module{
	Record: DwarfCannonRecord, Bank: EastArdougneNorth, BankSet: true, Anchor: CannonCommanderAnchor,
	ToolIDs:       cannonTools,
	ReadStage:     ParseDwarfCannonJournal,
	Sustain:       cannonSustain,
	MaxDeaths:     1,
	ExclusiveDone: CannonPastTheChild,
	Decide:        DecideDwarfCannon,
}
