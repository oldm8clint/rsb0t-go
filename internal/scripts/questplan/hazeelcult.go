package questplan

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Hazeel Cult (content id `hazeelcult`, %hazeelcultquest, not transmitted, 1 quest point), ported from the TS def (defs/hazeelcult.ts)
// and checked against content 289: quests/quest_hazeelcult/scripts/{quest_hazeelcult,ceril_carnillean,clivet,alomone,claus_the_chef,
// butler_jones,hazeel_cultist,carnillean_guard,philipe_carnillean,hazeelcult_journal}.rs2, its configs (the constants, locs, npcs, objs)
// and the maps m40_50, m40_51 and m40_151. The reward is 1,500 Thieving experience (stat_advance(thieving, 15000)), 2,000 coins and the
// quest point; no level or item is asked for.
//
// WHICH SIDE. The quest forks at Clivet, the cult's man at the cave under the forest south of Ardougne, and the fork is for good
// (%hazeelcult_side). The TS def runs the Carnillean side. This port runs the CULT side, because the cult side has no fight at all and
// the Carnillean side has one:
//
//	Carnillean side  Ceril, Clivet ("You're crazy"), the valves, the raft, then KILL Alomone (level 13, 25 hitpoints, a hit of up to 2) for
//	                 the armour he drops (and drops only while `~obj_gettotal(carnillean_armour)` counts none in the pack, the bank or
//	                 worn), the raft out, Ceril, the butler's cupboard upstairs. One fight, and an armour the bank would block.
//	Cult side        Ceril, Clivet ("Ok, count me in": a vial of poison), the poison on the mansion's range, the valves, the raft, a
//	                 TALK with Alomone, the scroll from the chest in the mansion's attic (a key from a crate in the kitchen cellar, a
//	                 secret wall on the first floor), the raft again and Alomone's ritual. No monster is attacked, hunts or is hunted:
//	                 the cult's npcs (Clivet, Alomone, the cultists) have no hunt mode and are only ever talked to, the rats of the sewer
//	                 are level 1 and the mansion's guard, butler, chef and family are peaceful. Every item is recoverable: the
//	                 poison is handed out again by Clivet, the key by the crate and the scroll by the chest, none of them checks the
//	                 bank.
//
// The cost of no fight is more legs, which is the right trade for an unproven filler: a leg can fail and be tried again, a lost fight
// cannot. An account that took the Carnillean side (the journal tells: ParseHazeelJournal returns HzGoodSide plus the stage) is not
// finished by this module and is parked for a day with the reason said; nothing here can send an account down that road, since the only
// menu answers it gives are the cult's.
//
// THE QUEST, stage by stage (%hazeelcultquest, the cult side):
//
//	0 Sir Ceril Carnillean, in his mansion west of Ardougne's zoo, wants his stolen armour back: "What's wrong?" and
//	  "Yes, of course, I'd be happy to help." (stage 2).
//	2/3 Clivet stands at the sewer landing under the cave in the forest south of the mansion. Talking to him sets stage 3 before his menus:
//	  "What do you mean?", then "So what would I have to do?", then "Ok, count me in." (a poison, %hazeelcult_side = cult, stage 4). The
//	  two other ways out of each menu ("I've heard enough of your rubbish.", "You're crazy, I'd never help you.", "No. I won't do it.")
//	  are the Carnillean side's; Dialogue.Drive never guesses, so a menu without the answer stops the talk.
//	4 The poison goes into the mansion's soup: it is used on the range in the kitchen cellar (stage 5). Clivet gives another when the
//	  pack holds none.
//	5 Alomone, the cult's leader, sits in the hideout at the far end of the sewer. The hideout is reached by the raft at the landing, and
//	  the raft moves only when the five sewer valves in the forest are all set: `count_correct_valves` counts the bits that are set from
//	  the first valve on, and stops at the first that is not, so each wrong valve stops the raft one island short (0: it will not
//	  move). The bit is set by Turn-left on the third valve and by Turn-right on the other four, and the bits are perm: once the
//	  five are set every ride goes through. Clivet's amulet (given at stage 5) only says this; the raft does not ask for it. Alomone's
//	  talk is seven pages with no menu (stage 6).
//	6 The scroll of Hazeel is in the mansion: a rusty key from the crate in the kitchen cellar, the chest in the attic above the first
//	  floor, reached by the secret wall in the first-floor bedroom ("Knock-at", stage 6 and 7 only). The chest is opened with the key (it
//	  shuts itself 300 ticks later), the chest is searched, and the scroll comes (stage 7).
//	7 The scroll to Alomone: his ritual raises Hazeel, a strong queue closes the chat and finishes the quest (2,000 coins, the
//	  experience, the quest point).
//
// WHAT A SCRIPT MUST PLAN FOR.
//
//   - The sewer is a pocket. The landing (the cave's side, with Clivet), the hideout and the four islands the raft stops at when the
//     valves are wrong are separate pieces of ground that no walk joins: the cave and the stairs up to the forest, the raft, are
//     teleports. They are one registered pocket whose way out is the raft back to the landing and then the stairs. The mansion's
//     kitchen cellar is NOT part of it (its ladder joins the ground floor, and the walk graph knows that ladder).
//   - The attic and the ladder room under it are a second pocket. The first-floor ladder room (two tiles) is walled off from the floor
//     by the secret wall, which is a teleport through a wall loc (`open_and_close_door2`), and the attic is the ladder's other end. The
//     walk graph knows the ladder, not the wall.
//   - Four scripts raise a ~mesbox BEFORE they change the world: the crate ("Under various food package you find an old rusty key.")
//     adds the key and the chest ("Inside the chest you find the Scroll of Hazeel.") adds the scroll only when the box is continued,
//     so those two boxes are pressed on, never closed. The valves and the range raise theirs after the change.
//   - The raft's answer is the only oracle for the valves (the varp is not transmitted): the module rides first and sets the valves
//     only when the raft refused or stopped short, so an account whose valves are set from an earlier run is not walked round the
//     forest again.
//   - Several scripts end the conversation with the quest's own queue, not with the account's last click, so every talk is driven
//     until the journal says it is over, not until the chat shuts.

// HazeelCultName is the quest tab's spelling (component 7362, `questlist:hazeelcult`) and HazeelCultID the engine's id, content's own.
const (
	HazeelCultName = "Hazeel Cult"
	HazeelCultID   = "hazeelcult"
)

// The %hazeelcultquest values the journal tells apart (quest_hazeelcult.varp, ^hazeelcult_complete = 9 in quest.constant; 1 and 8 are
// unused), and HzGoodSide, which ParseHazeelJournal adds to the stage of an account on the Carnillean side.
const (
	HzNotStarted     = 0
	HzStarted        = 2
	HzSpokenClivet   = 3
	HzClivetDecision = 4 // the cult side: the poison is in hand
	HzPoured         = 5
	HzFinishedSide   = 6 // Alomone spoke: the scroll is in the mansion
	HzScroll         = 7 // the scroll is found
	HzComplete       = 9
	HzGoodSide       = 100
)

// Ids from content/pack (checked against the production config by TestHazeelIDsMatchTheProductionConfig in questmods). The poison is
// Ernest the Chicken's ObjPoison (273).
const (
	NpcHzCeril   = 885 // [sir_ceril_carnillean]
	NpcHzClaus   = 886 // [claus_carnillean], the chef
	NpcHzJones   = 890 // [butler_jones_hazeel_cultist]
	NpcHzAlomone = 891 // [alomone_hazeel_cultist], level 13: never attacked
	NpcHzHazeel  = 892 // [hazeel], raised by the ritual
	NpcHzClivet  = 893 // [clivet_hazeel_cultist]
	NpcHzCultist = 894 // [hazeel_cultist]

	ObjHzScroll = 2403 // [hazeel_scroll], untradeable
	ObjHzKey    = 2404 // [carnilleanchestkey], untradeable
	ObjHzArmour = 2405 // [carnillean_armour]: the other side's
	ObjHzMark   = 2406 // [mark_of_hazeel], Clivet's amulet

	LocHzValve1        = 2844 // [sewervalve1]..[sewervalve5]: Turn-left, Turn-right
	LocHzValve2        = 2845
	LocHzValve3        = 2846 // set by Turn-left; the others are set by Turn-right
	LocHzValve4        = 2847
	LocHzValve5        = 2848
	LocHzRaft          = 2849 // [hazeelsewerraft], Board
	LocHzCave          = 2852 // [hazeelcultcave], Enter: a 4 by 4 mouth in the forest
	LocHzStairs        = 2853 // [hazeelcultstairs], Climb-up
	LocHzSecretWall    = 2854 // [loc_2854], Knock-at: the first floor's wall to the ladder room
	LocHzChestShut     = 2856 // [carnilleanshutchest], Open (the key opens it)
	LocHzChestOpen     = 2857 // [carnilleanopenchest], Search, shut again after 300 ticks
	LocHzCrate         = 2858 // [carnilleancrate], Search: the rusty key
	LocHzRange         = 2859 // [carnilleanrange]: the poison goes onto it
	LocHzKitchenLadder = 1755 // the cellar's ladder up (the walk graph knows it)
	LocHzAtticLadderUp = 1747 // the ladder room's ladder to the attic
	LocHzAtticLadderDn = 1746 // the attic's ladder back down
)

// Tiles from content maps, the loc index and the collision pack.
var (
	// HzCerilAnchor is Ceril's spawn in the mansion's west room (m40_51 "0 5 7: 885"; a second Ceril, with a second butler, stands on the first
	// floor at "1 13 4": the talk looks for the nearest of the id).
	HzCerilAnchor = tile.Tile{X: 2565, Z: 3271}
	// HzClivetAnchor is Clivet's spawn at the landing (m40_151 "0 6 19: 893"), HzAlomoneAnchor Alomone's in the hideout ("0 49 6: 891").
	HzClivetAnchor  = tile.Tile{X: 2566, Z: 9683}
	HzAlomoneAnchor = tile.Tile{X: 2609, Z: 9670}
	// HzCaveAt is the cave's mouth in the forest (loc 2852, 4 by 4), HzCaveStand a tile beside it and HzLanding where the cave sets the
	// account down (p_teleport(0_40_151_10_18)).
	HzCaveAt    = tile.Tile{X: 2585, Z: 3233}
	HzCaveStand = tile.Tile{X: 2584, Z: 3233}
	HzLanding   = tile.Tile{X: 2570, Z: 9682}
	// HzStairsAt is the stairs up at the landing (loc 2853, 2 by 3) and HzStairsTop where they set the account down
	// (p_teleport(0_40_50_27_37)), beside the cave.
	HzStairsAt  = tile.Tile{X: 2570, Z: 9683}
	HzStairsTop = tile.Tile{X: 2587, Z: 3237}
	// HzRaftMouth is the raft at the landing (loc 2849 at 2567,9679; the only one the content does not send back), HzRaftMouthStand the
	// tile it is boarded from, HzHideoutRaft the hideout's (2606,9693) and HzHideoutStand the tile it is boarded from and the raft
	// from the landing sets the account on (p_teleport(0_40_151_46_28)).
	HzRaftMouth      = tile.Tile{X: 2567, Z: 9679}
	HzRaftMouthStand = tile.Tile{X: 2567, Z: 9680}
	HzHideoutRaft    = tile.Tile{X: 2606, Z: 9693}
	HzHideoutStand   = tile.Tile{X: 2606, Z: 9692}
	// HzRangeAt is the range in the kitchen cellar (loc 2859, 1 by 2) and HzRangeStand a tile it is operated from; HzCrateAt the crate
	// (loc 2858) and HzCrateStand its only stand.
	HzRangeAt    = tile.Tile{X: 2564, Z: 9672}
	HzRangeStand = tile.Tile{X: 2565, Z: 9671}
	HzCrateAt    = tile.Tile{X: 2571, Z: 9669}
	HzCrateStand = tile.Tile{X: 2570, Z: 9669}
	// HzWallAt is the secret wall (loc 2854, on the north edge of 2572,3270 on the first floor); HzWallSouth is the bedroom side, from
	// which the wall is knocked to reach the ladder room, and HzWallNorth the ladder room's.
	HzWallAt    = tile.Tile{X: 2572, Z: 3270, Level: 1}
	HzWallSouth = tile.Tile{X: 2572, Z: 3270, Level: 1}
	HzWallNorth = tile.Tile{X: 2572, Z: 3271, Level: 1}
	// HzAtticLadder is the ladder room's ladder (loc 1747 at 2573,3271) and HzAtticStand the tile the chest is operated from
	// (forceapproach north at angle 1: its east side).
	HzAtticLadder = tile.Tile{X: 2573, Z: 3271, Level: 1}
	HzChestAt     = tile.Tile{X: 2571, Z: 3269, Level: 2}
	HzChestStand  = tile.Tile{X: 2572, Z: 3269, Level: 2}
)

// HzValve is one of the five sewer valves in the forest: the loc, the op that SETS its bit and the tile it stands on.
type HzValve struct {
	Loc int
	Op  string
	At  tile.Tile
}

// HzValves are the five valves, in the order of `count_correct_valves`'s bits (m40_50 "0 2 47: 2844", "0 12 63: 2845", "0 25 45: 2846",
// "0 37 63: 2847", "0 49 43: 2848"). The third is turned left, the others right (oploc1/oploc2 of _hazeelcult_valve).
var HzValves = []HzValve{
	{LocHzValve1, "Turn-right", tile.Tile{X: 2562, Z: 3247}},
	{LocHzValve2, "Turn-right", tile.Tile{X: 2572, Z: 3263}},
	{LocHzValve3, "Turn-left", tile.Tile{X: 2585, Z: 3245}},
	{LocHzValve4, "Turn-right", tile.Tile{X: 2597, Z: 3263}},
	{LocHzValve5, "Turn-right", tile.Tile{X: 2609, Z: 3243}},
}

// The sewer's pieces of ground, the bounding boxes of the walkable components on the real collision pack (all level 0): the landing
// (34 tiles), the hideout (157), and the islands the raft stops at after one, two, three and four valves (36, 12, 18 and 23).
var (
	hzLandingBox = Rect{MinX: 2565, MaxX: 2571, MinZ: 9680, MaxZ: 9685}
	hzHideoutBox = Rect{MinX: 2601, MaxX: 2612, MinZ: 9666, MaxZ: 9693}
	hzIslandBoxs = []Rect{
		{MinX: 2573, MaxX: 2580, MinZ: 9686, MaxZ: 9693},
		{MinX: 2590, MaxX: 2593, MinZ: 9690, MaxZ: 9695},
		{MinX: 2599, MaxX: 2603, MinZ: 9708, MaxZ: 9712},
		{MinX: 2615, MaxX: 2619, MinZ: 9720, MaxZ: 9726},
	}
	// hzKitchenBox is the mansion's kitchen cellar (20 tiles), joined to the ground floor by a ladder.
	hzKitchenBox = Rect{MinX: 2564, MaxX: 2570, MinZ: 9668, MaxZ: 9672}
)

// InHzLanding reports a tile of the landing under the cave.
func InHzLanding(t tile.Tile) bool { return hzLandingBox.Contains(t) }

// InHzHideout reports a tile of the cult's hideout.
func InHzHideout(t tile.Tile) bool { return hzHideoutBox.Contains(t) }

// HzIslandOf is the island (1 to 4) a tile is on, 0 for none.
func HzIslandOf(t tile.Tile) int {
	for i, r := range hzIslandBoxs {
		if r.Contains(t) {
			return i + 1
		}
	}
	return 0
}

// InHzSewer reports a tile of the cult's sewer: the landing, the hideout or an island. It is a pocket: no walk leaves it.
func InHzSewer(t tile.Tile) bool { return InHzLanding(t) || InHzHideout(t) || HzIslandOf(t) != 0 }

// InHzKitchen reports a tile of the kitchen cellar under the mansion.
func InHzKitchen(t tile.Tile) bool { return hzKitchenBox.Contains(t) }

// InHzAttic reports a tile of the ladder room on the first floor (two tiles) or the attic above it: the pocket the secret wall closes.
func InHzAttic(t tile.Tile) bool {
	switch t.Level {
	case 1:
		return t.Z == 3271 && t.X >= 2571 && t.X <= 2572
	case 2:
		return t.X >= 2571 && t.X <= 2573 && t.Z >= 3269 && t.Z <= 3271
	}
	return false
}

// HzHops are the two level changes the walk graph lacks: the cave into the sewer and the stairs back up.
var HzHops = []LadderHop{
	{Stand: HzCaveStand, LocName: "Cave entrance", Op: "Enter", Arrive: HzLanding},
	{Stand: tile.Tile{X: 2569, Z: 9684}, LocName: "Stairs", Op: "Climb-up", Arrive: HzStairsTop},
}

// Custom step names (internal/scripts/questmods).
const (
	CustomHzDown    = "hz-down"    // the cave into the sewer's landing
	CustomHzPour    = "hz-pour"    // the poison on the range
	CustomHzAlomone = "hz-alomone" // the raft to the hideout and Alomone's first talk
	CustomHzKey     = "hz-key"     // the rusty key from the crate
	CustomHzChest   = "hz-chest"   // the secret wall, the attic and the chest: the scroll
	CustomHzRitual  = "hz-ritual"  // the raft to the hideout and the scroll to Alomone
)

// HzParkFor is how long the quest skips an account that cannot go on and has no way to mend it by trying again: one that took the
// Carnillean side, or whose raft will not take it to the hideout.
const HzParkFor = 24 * time.Hour

// hzTools are the quest's own items: what the floor bite never eats and every bank visit leaves in the pack.
var hzTools = []int{ObjPoison, ObjHzKey, ObjHzScroll, ObjHzMark, ObjHzArmour, CoinsID}

// hzKeepNoScroll is the keep list of the deposit that banks the scroll: the quest's tools but the scroll.
var hzKeepNoScroll = func() []int {
	var keep []int
	for _, id := range hzTools {
		if id != ObjHzScroll {
			keep = append(keep, id)
		}
	}
	return keep
}()

// HzKeepIDs returns the keep list (a copy).
func HzKeepIDs() []int { return append([]int(nil), hzTools...) }

// The stops. Talks with no menu carry a placeholder preference (a talk step must name one).
var (
	// HzCerilStop starts the quest: "What's wrong?" among three, then "Yes, of course, I'd be happy to help." among two.
	HzCerilStop = NpcStop{Npc: "Ceril Carnillean", ID: NpcHzCeril, Anchor: HzCerilAnchor, Leash: 6,
		Prefer: []string{"What's wrong?", "Yes, of course"}}
	// HzClivetStop is every talk with Clivet: at stage 2 and 3 three menus ("What do you mean?" / "I've heard enough of your rubbish.",
	// then "You're crazy, I'd never help you." / "So what would I have to do?", then "No. I won't do it." / "Ok, count me in."), at stage 4
	// none (a new vial when the pack holds none).
	HzClivetStop = NpcStop{Npc: "Clivet", ID: NpcHzClivet, Anchor: HzClivetAnchor, Leash: 10,
		Prefer: []string{"What do you mean?", "So what would I have to do?", "Ok, count me in."}}
	// HzAlomoneStop is Alomone, in the hideout: no menu at stage 5 and 7.
	HzAlomoneStop = NpcStop{Npc: "Alomone", ID: NpcHzAlomone, Anchor: HzAlomoneAnchor, Leash: 10, Prefer: []string{"Hello."}}
)

// hzMarkers are the journal's markers, newest stage first: hazeelcult_journal.rs2 keeps every earlier paragraph on the page, so the newest
// one decides. The Carnillean side's three (the raft the cultist took, the hideout, the armour returned) come first: their pages carry
// "he told me a pack of lies" too, which is also stage 3's own page. Struck-through lines read like the others (colour tags are removed).
var hzMarkers = []struct {
	stage int
	text  string
}{
	{HzComplete, "quest complete!"},
	{HzGoodSide + HzScroll, "i returned the armour, but ceril"},
	{HzGoodSide + HzFinishedSide, "i managed to enter the hideout, kill the cult leader"},
	{HzGoodSide + HzClivetDecision, "he jumped onto a raft and headed into the"},
	{HzScroll, "i found the scroll needed to revive hazeel"},
	{HzFinishedSide, "told me to find a spell scroll"},
	{HzPoured, "having decided to assist the cult"},
	{HzClivetDecision, "told me the truth about the carnilleans"},
	{HzSpokenClivet, "he told me a pack of lies about the carnilleans"},
	{HzStarted, "who were responsible live in a cave"},
	{HzNotStarted, "i can start this quest by talking to"},
}

// ParseHazeelJournal reads %hazeelcultquest from the rendered journal, plus HzGoodSide when the page is the Carnillean side's. ok is
// false for an empty or unrecognised page, which is unknown, never stage 0.
func ParseHazeelJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	for _, m := range hzMarkers {
		if strings.Contains(text, m.text) {
			return m.stage, true
		}
	}
	return 0, false
}

// hzNeedsRoom reports a step that is about to be handed an item (the vial, the key, the scroll) by a script that adds it without
// checking for room: `inv_add` into a full pack drops it on the floor.
func hzNeedsRoom(s Snapshot) bool { return s.FreeSlots < 1 }

// hzDeposit makes room, keeping the quest's items.
func hzDeposit(reason string) Step {
	return Step{Kind: StepDeposit, KeepIDs: hzTools, Bank: EastArdougneNorth, BankSet: true, Reason: reason}
}

func hzTalk(stop NpcStop, reason string) Step {
	return Step{Kind: StepTalk, Stop: stop, Reason: reason}
}

func hzCustom(name, reason string) Step { return Step{Kind: StepCustom, Custom: name, Reason: reason} }

// hzFindScroll is the step that gets the scroll of Hazeel: the key from the crate first, then the chest.
func hzFindScroll(s Snapshot) Step {
	if s.HeldID(ObjHzKey) == 0 {
		if hzNeedsRoom(s) {
			return hzDeposit("room for the key the crate gives")
		}
		return hzCustom(CustomHzKey, "the rusty key from the crate in the kitchen cellar")
	}
	if hzNeedsRoom(s) {
		return hzDeposit("room for the scroll the chest gives")
	}
	return hzCustom(CustomHzChest, "the scroll of Hazeel from the chest in the attic")
}

// DecideHazeelCult is the whole quest, read from the journal's stage, the account's tile and the pack.
func DecideHazeelCult(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return hzTalk(HzCerilStop, "start Hazeel Cult")
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	if !s.TileKnown {
		return Step{Kind: StepWait, Reason: "the account's position has not been read"}
	}
	if s.Stage >= HzGoodSide {
		return Step{Kind: StepWait, ParkFor: HzParkFor,
			Reason: fmt.Sprintf("this account took the Carnillean side (stage %d): this module finishes the cult's side only", s.Stage-HzGoodSide)}
	}
	switch s.Stage {
	case HzNotStarted:
		return Step{Kind: StepWait, Reason: "the journal and the quest tab disagree about Hazeel Cult"}
	case HzStarted, HzSpokenClivet:
		if hzNeedsRoom(s) {
			return hzDeposit("room for the vial of poison Clivet gives")
		}
		return hzClivet(s, "Clivet, at the cave under the forest: join the cult")
	case HzClivetDecision:
		if s.HeldID(ObjPoison) == 0 {
			if hzNeedsRoom(s) {
				return hzDeposit("room for the new vial of poison Clivet gives")
			}
			return hzClivet(s, "Clivet's vial of poison, lost")
		}
		return hzCustom(CustomHzPour, "the poison on the range in the mansion's kitchen cellar")
	case HzPoured:
		return hzCustom(CustomHzAlomone, "the valves, the raft and Alomone, in the hideout")
	case HzFinishedSide:
		// The chest gives the scroll, and moves the quest to stage 7, only to an account that holds none ("The chest is empty." otherwise,
		// whatever the stage): a scroll in the pack at stage 6 (found early, by hand) is banked first, and Alomone takes it at stage 7 only.
		if s.HeldID(ObjHzScroll) > 0 {
			return Step{Kind: StepDeposit, KeepIDs: hzKeepNoScroll, Bank: EastArdougneNorth, BankSet: true,
				Reason: "the scroll found before Alomone asked for it: the chest moves the quest on only for an account that holds none"}
		}
		return hzFindScroll(s)
	case HzScroll:
		if s.HeldID(ObjHzScroll) > 0 {
			return hzCustom(CustomHzRitual, "the scroll to Alomone, in the hideout")
		}
		return hzFindScroll(s)
	}
	return Step{Kind: StepWait, Reason: fmt.Sprintf("unrecognised Hazeel Cult stage %d", s.Stage)}
}

// hzClivet is a talk with Clivet, from the landing: the cave first when the account stands anywhere else (the walk graph has no edge
// for it).
func hzClivet(s Snapshot, reason string) Step {
	if !InHzLanding(s.Tile) {
		return hzCustom(CustomHzDown, "down the cave to Clivet")
	}
	return hzTalk(HzClivetStop, reason)
}

// HazeelCultRecord is the quest's identity. The content asks for nothing.
var HazeelCultRecord = Record{ID: HazeelCultID, Name: HazeelCultName, QuestPoints: 1}

// HazeelCult is the engine module. Nothing in it can hurt an account (no monster is fought or hunts), so it does not retaliate against
// the sewer's rats, and the first death on its walks parks it for a day.
var HazeelCult = Module{
	Record: HazeelCultRecord, Bank: EastArdougneNorth, BankSet: true, Anchor: HzCerilAnchor,
	Hops:        HzHops,
	ToolIDs:     hzTools,
	ReadStage:   ParseHazeelJournal,
	NoRetaliate: true,
	MaxDeaths:   1,
	Decide:      DecideHazeelCult,
}
