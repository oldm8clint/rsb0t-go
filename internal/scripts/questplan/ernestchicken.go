package questplan

import (
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// Ernest the Chicken (content id `haunted`, %haunted, not transmitted), ported from defs/ernest/*.ts and checked against
// content 289 (quests/quest_haunted/scripts/{quest_haunted,haunted_journal}.rs2 and areas/area_draynor/scripts/
// {veronica,professor_oddenstein}.rs2). Four quest points, 300 coins, no experience, no requirement.
//
// The quest: Veronica, outside Draynor Manor, asks for her fiance (stage 1); Professor Oddenstein, on the manor's top
// floor, says Ernest is the chicken beside him and wants three parts the gremlins took (stage 2): a rubber tube, a
// pressure gauge and an oil can. Handed all three in the pack he turns Ernest back and the quest is done (stage 3). The
// three parts are in no order and no stage but 2 matters to the module.
//
// Where each part is, and what the content does that a script must plan for:
//
//   - The rubber tube lies on the floor of the closet (3111,3367), a sealed room of ten tiles behind a locked door. The
//     door opens only to the closet key used on it ([oplocu,closet_door]), which the compost heap west of the manor gives
//     to a spade used on it ([oplocu,hauntedcompostheap]; its Search is "I'm not looking through that with my hands!"), and
//     the same key used on the same door from inside is the way out: the door teleports the account through and shuts
//     behind it (open_and_close_door2), nothing else leaves the closet, and the key is never used up. A skeleton (level
//     25, 17 hitpoints, a hit of 3 at most, huntmode cowardly, huntrange 1) stands in the closet beside the tube; cowardly
//     hunting skips a player of combat level above twice its own, so it is the accounts of 50 and under it attacks.
//   - The oil can lies on the floor of the maze room 9 of the basement (3092,9755): the six levers and nine doors of
//     ernestmaze.go. The way in is a pocket of its own: the bookcase at (3097,3358) teleports an account standing east of
//     it into a sealed alcove of 36 tiles, whose ladder goes down to the maze and whose lever (the same loc type as the
//     Wilderness levers) teleports it back out.
//   - The pressure gauge is in the fountain south-west of the manor ([oploc1,hauntedfountain]). While %haunted_manor_
//     fountain_poisoned is 0 the piranhas bite for 1 ("Something in the water bites you."); a poisoned fish food (poison
//     used on fish food: opheldu) poured into the fountain sets it to 1 for ever, and after that the same Search hands the
//     gauge over. The varp is not transmitted and the gauge is no item to look for, so the module SEARCHES FIRST: an account
//     whose fountain an earlier run poisoned gets its gauge at the cost of nothing, the others pay one hitpoint to learn it
//     has to pour. The poison lies at (3097,3366), the fish food upstairs at (3108,3356,1); both respawn in 100 ticks.
//   - The spade lies at (3120,3359), beside the manor's back door; the mule holds spades.
//   - The manor's front doors (134 and 135, "The doors won't open." from inside) let an account in from the south only, so
//     the way out is the back door at (3123,3360) and the walker knows both. Fourteen "Tree" npcs (nasty_tree: aggressive_melee,
//     huntrange 1, no level cap, a hit of 3 at most), ten of them in the garden between Veronica and the front door, hit whoever
//     stands beside them; the garden has no way through that keeps a tile from each (the walker's planned routes brush between
//     one and five of them), and a walk that stops beside one is hit again, so the quest does not retaliate (ErnestChicken).
//
// A part, the closet key and the poisoned fish food are untradeable, so the trainer's bank visits between two passes put
// them in the bank, and the module's first pass of a run reads the bank for them (KitErnest) before it walks anywhere.

// ErnestChickenName is the quest tab's spelling (component 7339, `questlist:haunted`).
const ErnestChickenName = "Ernest the Chicken"

// ErnestChickenID is the engine's id, content's own.
const ErnestChickenID = "haunted"

// Ids from content/pack.
const (
	NpcVeronica      = 285 // [veronica], south of the manor's garden
	NpcOddenstein    = 286 // [professor_oddenstein], the manor's top floor
	NpcErnestMan     = 287 // [ernest], what the chicken turns into for 100 ticks
	NpcErnestChicken = 288 // [questchicken], named "Ernest"
	NpcManorSkeleton = 90  // [skeleton_unarmed], in the closet
	NpcManorTree     = 152 // [nasty_tree], the garden's twelve

	ObjPressureGauge    = 271
	ObjFishFood         = 272
	ObjPoison           = 273
	ObjPoisonedFishFood = 274
	ObjClosetKey        = 275 // named "Key", a name a dozen objects share: always by id
	ObjRubberTube       = 276
	ObjOilCan           = 277
	ObjSpade            = 952

	LocManorCompost     = 152 // [hauntedcompostheap]
	LocManorFountain    = 153 // [hauntedfountain]
	LocClosetDoor       = 131 // [closet_door]
	LocManorBookcaseL   = 155 // [hauntedbookcasel], teleports to (3096,3358)
	LocManorBookcaseR   = 156 // [hauntedbookcaser], teleports to (3096,3359)
	LocPuzzleLadderDown = 133 // [puzzle_ladder_top]: "Climb-down", in the alcove
	LocPuzzleLadderUp   = 132 // [puzzle_ladder]: "Climb-up", in the maze; resets the levers
	LocAlcoveLever      = 160 // [hauntedleverup]: "Pull", teleports out of the alcove
)

// Stages (%haunted; quest_haunted.constant and ^haunted_complete in general/configs/quest.constant).
const (
	ErnestNotStarted      = 0
	ErnestStarted         = 1
	ErnestSpokeOddenstein = 2
	ErnestComplete        = 3
)

// ErnestSkeletonLevel is the closet skeleton's vislevel: cowardly hunting leaves a player of more than twice this combat
// level alone (Npc.ts, HuntCheckNotTooStrong.OUTSIDE_WILDERNESS).
const ErnestSkeletonLevel = 25

// ErnestSkeletonHunts reports whether the closet skeleton goes for an account of this combat level (0 is not known, which
// counts as hunted).
func ErnestSkeletonHunts(combat int) bool { return combat <= 0 || combat <= 2*ErnestSkeletonLevel }

// ErnestSkeletonPeakHit is the skeleton's hardest hit: ((24+9)*64+320)/640.
const ErnestSkeletonPeakHit = 3

// ErnestClosetFloor is the hitpoints an account that the skeleton hunts goes into the closet with: three fifths of its maximum,
// never under four of the skeleton's hardest blows, never over the maximum. Under it the account eats first, and parks for a
// while if it has nothing to eat.
func ErnestClosetFloor(maxHP int) int {
	if maxHP <= 0 {
		return 0
	}
	return min(maxHP, max(maxHP*6/10, 4*ErnestSkeletonPeakHit))
}

// FoeManorSkeleton is the closet skeleton: quest_haunted's closet holds one [skeleton_unarmed] (maps/m48_52.jm2 "0 38 39:
// 90"): attack, strength and defence 24, 17 hitpoints, stab and slash defence 5, crush -5, no attackrate (4), no bonus.
var FoeManorSkeleton = MeleeFoe{Name: "Skeleton", Attack: 24, Strength: 24, Defence: 24, Magic: 1, Hitpoints: 17, AttackTicks: 4, DefenceBonus: -5}

// Tiles from the content's maps (m48_52 for the manor, m48_152 for the basement).
var (
	// VeronicaAnchor is Veronica, and OddensteinAnchor the Professor on the top floor.
	VeronicaAnchor   = tile.Tile{X: 3110, Z: 3330}
	OddensteinAnchor = tile.Tile{X: 3110, Z: 3367, Level: 2}
	// ErnestPoisonSpawn and ErnestFishFoodSpawn are the ground spawns of the two ingredients: the poison on the ground
	// floor's west side, the fish food upstairs. ErnestSpadeSpawn is beside the back door.
	ErnestPoisonSpawn   = tile.Tile{X: 3097, Z: 3366}
	ErnestFishFoodSpawn = tile.Tile{X: 3108, Z: 3356, Level: 1}
	ErnestSpadeSpawn    = tile.Tile{X: 3120, Z: 3359}
	// ErnestFountainStand is the tile north of the fountain (a 2x2 loc at 3087,3334) and ErnestCompostStand the tile south
	// of the compost heap (a 2x2 loc at 3084,3360).
	ErnestFountainStand = tile.Tile{X: 3087, Z: 3336}
	ErnestCompostStand  = tile.Tile{X: 3085, Z: 3359}
	// ErnestClosetOutside is the tile the closet door (a straight wall on its east edge at 3107,3367) is used from outside,
	// ErnestClosetInside the tile it lands on and is used from to leave, ErnestTubeSpawn the tube.
	ErnestClosetOutside = tile.Tile{X: 3107, Z: 3367}
	ErnestClosetInside  = tile.Tile{X: 3108, Z: 3367}
	ErnestTubeSpawn     = tile.Tile{X: 3111, Z: 3367}
	// ErnestBookcaseStand is east of the bookcases (they refuse an account west of them), ErnestAlcoveLever the lever's own tile
	// (a wall decoration is operated from it), ErnestLadderDownStand the tile north of the alcove's ladder.
	ErnestBookcaseStand   = tile.Tile{X: 3098, Z: 3358}
	ErnestAlcoveLever     = tile.Tile{X: 3096, Z: 3357}
	ErnestLadderDownStand = tile.Tile{X: 3092, Z: 3363}
	// ErnestBasementLanding is where the ladder down lands, ErnestLadderUpStand the tile the ladder up is climbed from, and
	// ErnestOilCanSpawn the oil can in room 9.
	ErnestBasementLanding = tile.Tile{X: 3116, Z: 9754}
	ErnestLadderUpStand   = tile.Tile{X: 3117, Z: 9755}
	ErnestOilCanSpawn     = tile.Tile{X: 3092, Z: 9755}
)

// The boxes of the manor's three sealed pockets, on level 0, from flooding the baked collision pack (the closet is ten
// tiles and the alcove thirty-six, TestThePocketsAreTheCollisionPacksComponents).
const (
	ernestClosetMinX, ernestClosetMaxX, ernestClosetMinZ, ernestClosetMaxZ = 3108, 3112, 3366, 3368
	ernestAlcoveMinX, ernestAlcoveMaxX, ernestAlcoveMinZ, ernestAlcoveMaxZ = 3091, 3096, 3354, 3363
)

// InErnestCloset reports a tile in the closet.
func InErnestCloset(t tile.Tile) bool {
	return t.Level == 0 && t.X >= ernestClosetMinX && t.X <= ernestClosetMaxX && t.Z >= ernestClosetMinZ && t.Z <= ernestClosetMaxZ
}

// InErnestAlcove reports a tile in the ladder alcove behind the bookcases.
func InErnestAlcove(t tile.Tile) bool {
	return t.Level == 0 && t.X >= ernestAlcoveMinX && t.X <= ernestAlcoveMaxX && t.Z >= ernestAlcoveMinZ && t.Z <= ernestAlcoveMaxZ
}

// Custom step names (internal/scripts/questmods) and the one kit.
const (
	CustomErnestFishFood = "ernest-fish-food"
	CustomErnestPoison   = "ernest-poison"
	CustomErnestSpade    = "ernest-spade"
	CustomErnestCan      = "ernest-can"
	CustomErnestProbe    = "ernest-probe"
	CustomErnestPour     = "ernest-pour"
	CustomErnestGauge    = "ernest-gauge"
	CustomErnestKey      = "ernest-key"
	CustomErnestTube     = "ernest-tube"
	CustomErnestHandIn   = "ernest-hand-in"

	KitErnest = "ernest"
)

var (
	// VeronicaStop starts the quest at her first menu ("No, I'm looking for something to kill." is the other); at stage 1 and 2 she has no menu.
	VeronicaStop = NpcStop{Npc: "Veronica", ID: NpcVeronica, Anchor: VeronicaAnchor, Leash: 8,
		Prefer: []string{"Aha, sounds like a quest. I'll help."}}
	// OddensteinStop is the Professor. At stage 1 his first menu is "I'm looking for a guy called Ernest." (the others only
	// ask about the machine and the house) and both answers of the second reach oddenstein_not_easy, which sets stage 2. At
	// stage 2 he has no menu: the hand-in is a conversation, then a scene with the dialogue shut (ernestHandIn drives it).
	OddensteinStop = NpcStop{Npc: "Professor Oddenstein", ID: NpcOddenstein, Anchor: OddensteinAnchor, Leash: 6,
		Prefer: []string{"I'm looking for a guy called Ernest.", "Change him back this instant!", "I'm glad Veronica didn't actually get engaged to a chicken."}}
)

// ernestJournalNeedles are ParseErnestChickenJournal's phrases, newest first, written against NormaliseLines
// (haunted_journal.rs2: finished lines are struck through but kept, so a later stage holds the earlier ones' words, bar the
// one line "He went into Draynor Manor and hasn't returned" that only stage 1 has).
var ernestJournalNeedles = []struct {
	stage  int
	needle string
}{
	{ErnestComplete, "we turned ernest back to normal"},
	{ErnestSpokeOddenstein, "discovered ernest is a"},
	{ErnestStarted, "he went into"},
	{ErnestNotStarted, "i can start this quest by speaking to"},
}

// ParseErnestChickenJournal reads the stage from the rendered journal. ok is false for an empty or unrecognised journal,
// which is unknown, never stage 0.
func ParseErnestChickenJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	if text == "" {
		return 0, false
	}
	for _, n := range ernestJournalNeedles {
		if strings.Contains(text, n.needle) {
			return n.stage, true
		}
	}
	return 0, false
}

// ernestFoods are the foods the account may carry and eat, best first. The trees hit for 3 and the skeleton for 3, so the
// food is for the walks and the random events more than for a fight.
var ernestFoods = []int{385, 373, 379, 365, 361, 329, 333, 2309, 2142, 1891, 1893, 1895, 1897, 1899, 1901}

// ernestTools are the quest's own items, kept by every bank visit of the quest and never eaten as food.
var ernestTools = []int{ObjSpade, ObjClosetKey, ObjRubberTube, ObjOilCan, ObjPressureGauge, ObjFishFood, ObjPoison, ObjPoisonedFishFood, CoinsID}

// ErnestToolIDs returns the tool list (a copy).
func ErnestToolIDs() []int { return append([]int(nil), ernestTools...) }

// ernestKeep is what a bank visit of this quest leaves in the pack: the tools and the food.
func ernestKeep() []int { return append(append([]int(nil), ernestTools...), ernestFoods...) }

// ErnestKeepIDs returns the keep list (a copy).
func ErnestKeepIDs() []int { return ernestKeep() }

// ernestBank is the bank a visit uses: the one nearest the account, except Al Kharid's behind its toll gate (Draynor
// then), and Draynor before a position packet has arrived.
func ernestBank(s Snapshot) tile.Tile { return cookSheepBank(s) }

// ernestKit is the one bundled pass of a run, bank and mule only: whatever the three parts, the key, the poisoned fish food
// and the spade and ingredients the bank or the mule holds. Every line is optional, because the ground and the quest's own
// steps supply what they cannot, and the manor's ground spawns are NOT listed as sources: the provisioner walks them with
// TravelCautious, whose flee squares round an attacker would close the garden's only corridor once a tree had hit the
// account, where the module's own gather steps walk it as the Vampire Slayer's proven legs do.
//
// A line that serves another is wanted only while that one is short (KitLine.OnlyIfShort): no spade for a tube the bank
// already holds, no poison for a gauge it holds. The parts, the key and the poisoned fish food are untradeable, so the mule
// is never asked for them, and they are asked of the bank first because the trainer banks them between two passes (the
// Murder Mystery and Romeo & Juliet lesson).
func ernestKit(s Snapshot) []KitLine {
	held := func(id int) bool { return s.HeldID(id) > 0 }
	var kit []KitLine
	if !held(ObjOilCan) {
		kit = append(kit, KitLine{ID: ObjOilCan, Name: "Oil can", Qty: 1, Optional: true, Untradeable: true})
	}
	if !held(ObjPressureGauge) {
		kit = append(kit,
			KitLine{ID: ObjPressureGauge, Name: "Pressure gauge", Qty: 1, Optional: true, Untradeable: true},
			KitLine{ID: ObjPoisonedFishFood, Name: "Poisoned fish food", Qty: 1, Optional: true, Untradeable: true, OnlyIfShort: ObjPressureGauge},
			KitLine{ID: ObjFishFood, Name: "Fish food", Qty: 1, Optional: true, OnlyIfShort: ObjPressureGauge},
			KitLine{ID: ObjPoison, Name: "Poison", Qty: 1, Optional: true, OnlyIfShort: ObjPressureGauge})
	}
	if !held(ObjRubberTube) {
		kit = append(kit,
			KitLine{ID: ObjRubberTube, Name: "Rubber tube", Qty: 1, Optional: true, Untradeable: true},
			KitLine{ID: ObjClosetKey, Name: "Key", Qty: 1, Optional: true, Untradeable: true, OnlyIfShort: ObjRubberTube},
			KitLine{ID: ObjSpade, Name: "Spade", Qty: 1, Optional: true, OnlyIfShort: ObjRubberTube})
	}
	return kit
}

// ernestLeg is a leg's custom step, or a wait once that step has failed in this run. A leg carries its own retries, so a failure is
// a verdict for the run: the wait ends in the engine's park, and the next run (the backoff later) starts the leg afresh, where a
// second call at once would walk the garden again for the same answer.
func ernestLeg(s Snapshot, custom, reason string) Step {
	if s.Failed[custom] {
		return Step{Kind: StepWait, Reason: "the " + custom + " leg failed this run: " + reason}
	}
	return Step{Kind: StepCustom, Custom: custom, Reason: reason}
}

// ernestRoom is the pack slots the next item needs: the parts and ingredients are one each, and the account may carry food
// and its run staller's kit.
const ernestRoom = 2

// ernestKitPass is the run's one bank-and-mule pass (KitErnest), asked for until it has run or failed once. It comes first,
// before Veronica is even spoken to: the account is wherever the quest before this one left it, which is often a bank's
// neighbourhood, where the same pass from the manor's top floor (the Professor's room) is two walks of three hundred tiles.
func ernestKitPass(s Snapshot) (Step, bool) {
	if s.Ran["provision:"+KitErnest] || s.Failed["provision:"+KitErnest] {
		return Step{}, false
	}
	return Step{Kind: StepProvision, KitName: KitErnest, Kit: ernestKit(s), KeepIDs: ernestKeep(),
		Bank: ernestBank(s), BankSet: true, Reason: "whatever the bank or the mule holds of the parts, the key and the ingredients"}, true
}

// DecideErnestChicken is the whole quest, read from the journal stage, the pack and where the account stands.
func DecideErnestChicken(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return ernestStart(s, VeronicaStop)
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	switch s.Stage {
	case ErnestNotStarted:
		return ernestStart(s, VeronicaStop)
	case ErnestStarted:
		return ernestStart(s, OddensteinStop)
	case ErnestSpokeOddenstein:
		return decideErnestParts(s)
	}
	return Step{Kind: StepWait, Reason: "unrecognised Ernest the Chicken stage"}
}

// ernestStart is a talk of the first two stages, after the run's pass.
func ernestStart(s Snapshot, stop NpcStop) Step {
	if st, ok := ernestKitPass(s); ok {
		return st
	}
	return Step{Kind: StepTalk, Stop: stop}
}

// decideErnestParts fetches the three parts and hands them in. The order is the one that crosses the manor's door fewest
// times: the gauge's ingredients and the can, which are inside, then the spade beside the back door, then out to the
// fountain and the compost heap, and back in by the front door for the tube.
func decideErnestParts(s Snapshot) Step {
	held := func(id int) bool { return s.HeldID(id) > 0 }
	tube, can, gauge := held(ObjRubberTube), held(ObjOilCan), held(ObjPressureGauge)

	// An account standing in one of the manor's sealed pockets (a relog, a restart or a park left it there) finishes that leg
	// first: nothing else can walk it out, and the leg knows the way.
	if s.TileKnown {
		switch {
		case InErnestCloset(s.Tile):
			return Step{Kind: StepCustom, Custom: CustomErnestTube, Reason: "in the closet: the tube if it is not in hand, then out by the key"}
		case InErnestAlcove(s.Tile) || InErnestBasement(s.Tile):
			return Step{Kind: StepCustom, Custom: CustomErnestCan, Reason: "in the manor's basement: the oil can if it is not in hand, then out by the ladder and the lever"}
		}
	}

	if tube && can && gauge {
		if s.Failed[CustomErnestHandIn] {
			return Step{Kind: StepTalk, Stop: OddensteinStop}
		}
		return Step{Kind: StepCustom, Custom: CustomErnestHandIn, Reason: "give the Professor the tube, the gauge and the can"}
	}

	if st, ok := ernestKitPass(s); ok {
		return st
	}
	if s.FreeSlots < ernestRoom {
		return Step{Kind: StepDeposit, KeepIDs: ernestKeep(), Bank: ernestBank(s), BankSet: true}
	}

	// 1. The gauge's ingredients, while the account is inside the manor and before it has any reason to go out: the fish food
	// upstairs, the poison on the ground floor, and the two used on each other. They are gathered before the fountain has been
	// searched (the common fountain bites and wants them), and only the pour consumes them.
	if !gauge && !s.Ran[CustomErnestPour] && !held(ObjPoisonedFishFood) {
		switch {
		case !held(ObjFishFood) && !s.Failed[CustomErnestFishFood]:
			return Step{Kind: StepCustom, Custom: CustomErnestFishFood, Reason: "fish food from the manor's top floor, for the fountain's piranhas"}
		case !held(ObjPoison) && !s.Failed[CustomErnestPoison]:
			return Step{Kind: StepCustom, Custom: CustomErnestPoison, Reason: "poison from the manor's west rooms, for the fountain's piranhas"}
		case held(ObjFishFood) && held(ObjPoison):
			return Step{Kind: StepUseOn, ItemID: ObjPoison, Item: "Poison", TargetKind: TargetItem, TargetID: ObjFishFood, Target: "Fish food"}
		}
	}

	// 2. The oil can, behind the bookcase.
	if !can {
		return ernestLeg(s, CustomErnestCan, "the oil can: the bookcase, the ladder, the maze's levers and doors, and out")
	}

	// 3. The spade for the compost heap, from beside the back door, on the way out to the fountain.
	if !tube && !held(ObjClosetKey) && !held(ObjSpade) && !s.Failed[CustomErnestSpade] {
		return Step{Kind: StepCustom, Custom: CustomErnestSpade, Reason: "a spade from beside the manor's back door, for the compost heap"}
	}

	// 4. The gauge, from the fountain outside.
	if !gauge {
		return decideErnestGauge(s)
	}

	// 5. The tube, from the closet: the key from the compost heap, then the door.
	if !held(ObjClosetKey) {
		if !held(ObjSpade) {
			return Step{Kind: StepWait, Reason: "no spade from the bank, the mule or the spawn beside the manor's back door"}
		}
		return ernestLeg(s, CustomErnestKey, "dig the compost heap with the spade for the closet key")
	}
	return ernestLeg(s, CustomErnestTube, "the closet: unlock the door, the tube, and out again")
}

// decideErnestGauge is the fountain: search it first, pour the poisoned fish food in if it bit, then search again.
func decideErnestGauge(s Snapshot) Step {
	held := func(id int) bool { return s.HeldID(id) > 0 }
	if !s.Ran[CustomErnestProbe] {
		return ernestLeg(s, CustomErnestProbe, "search the fountain: it hands the gauge over once it is poisoned, and bites for one hitpoint if not")
	}
	if !s.Ran[CustomErnestPour] {
		switch {
		case held(ObjPoisonedFishFood):
			return ernestLeg(s, CustomErnestPour, "the piranhas bit: pour the poisoned fish food into the fountain")
		case held(ObjFishFood) && held(ObjPoison):
			return Step{Kind: StepUseOn, ItemID: ObjPoison, Item: "Poison", TargetKind: TargetItem, TargetID: ObjFishFood, Target: "Fish food"}
		case !held(ObjFishFood) && !s.Failed[CustomErnestFishFood]:
			return Step{Kind: StepCustom, Custom: CustomErnestFishFood, Reason: "fish food from the manor's top floor, for the fountain's piranhas"}
		case !held(ObjPoison) && !s.Failed[CustomErnestPoison]:
			return Step{Kind: StepCustom, Custom: CustomErnestPoison, Reason: "poison from the manor's west rooms, for the fountain's piranhas"}
		}
		return Step{Kind: StepWait, Reason: "the fountain bites and no fish food and poison came from the bank, the mule or the manor's spawns"}
	}
	return ernestLeg(s, CustomErnestGauge, "search the poisoned fountain for the gauge")
}

// ErnestChickenRecord is the quest's identity. The server gates nothing.
var ErnestChickenRecord = Record{ID: ErnestChickenID, Name: ErnestChickenName, QuestPoints: 4}

// ErnestChicken is the engine module; its anchor (a flee reference) is Veronica's tile, outside the garden on open ground, not the
// Professor's room on the manor's top floor. Two deaths park it for a day: the only fight is the closet skeleton (a hit of 3), the
// garden's trees hit as hard, and an account that dies to them is not one to send back. NoRetaliate keeps the account
// walking when a tree hits it (a "Tree" has no Attack to retaliate with, and the answer to a hit would stop the walk at
// the tree's foot); the one fight, the skeleton's, is the module's own and clicks its attacks.
var ErnestChicken = Module{
	Record: ErnestChickenRecord, Bank: DraynorBank, BankSet: true, Anchor: VeronicaAnchor,
	ToolIDs:     ernestTools,
	ReadStage:   ParseErnestChickenJournal,
	Sustain:     Sustain{FoodIDs: ernestFoods, EatAt: 12},
	MaxDeaths:   2,
	NoRetaliate: true,
	Decide:      DecideErnestChicken,
}

// ErnestCustomNames are the custom steps the module can ask for, for the test that every one has an executor.
func ErnestCustomNames() []string {
	return []string{CustomErnestFishFood, CustomErnestPoison, CustomErnestSpade, CustomErnestCan, CustomErnestProbe, CustomErnestPour,
		CustomErnestGauge, CustomErnestKey, CustomErnestTube, CustomErnestHandIn}
}
