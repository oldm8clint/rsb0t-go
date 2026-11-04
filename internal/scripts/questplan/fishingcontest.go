package questplan

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
)

// The Fishing Contest (content id `fishingcompo`, %fishingcompo 0 to 5, not transmitted), ported from the TS def
// (defs/fishingcontest/*.ts) and checked against content 289: quests/quest_fishingcompo/{configs,scripts}, areas/area_seers/scripts/
// hemenster/{bonzo,morris,sinister_stranger}.rs2, areas/area_seers/scripts/mcgrubors_wood.rs2, areas/area_white_wolf_mountain/
// scripts/mountain_dwarf.rs2, general_use/scripts/cupboards.rs2 and the maps (m41_53, m41_54, m44_54). One quest point, 2,437
// Fishing experience (stat_advance 24370 at 10x) and the dwarves' tunnel under White Wolf Mountain. Fishing 10 is the one gate, and
// it is enforced at the fishing spot, not at the start.
//
// The story, with the stage each step moves %fishingcompo to:
//
//	0   The Mountain Dwarf at the tunnel mouth west of White Wolf Mountain (2820,3487; a twin stands at the east mouth, 2876,3483,
//	    on the far side of the mountain): six menus deep, "I was just wondering what was down those stairs?", "Why not?", "If you were
//	    my friend I wouldn't mind.", "Well, let's be friends!", "And how am I meant to do that?", "Fortunately I'm alright at
//	    fishing!". He hands over a Fishing pass (1).
//	1   The Hemenster competition ground (the lake-shore yard north of the village, 184 tiles, fcCompoundRows): its gate (locs 47 and 48, the left and right halves)
//	    lets in a player who shows Morris the pass (three chat pages and an object box, then a jump onto the gate tile). Bonzo takes
//	    5 coins ("I'll enter the competition please.", comp stage 1) and seats the player at the willow tree (2) - or, if garlic has
//	    been put in the wall pipe (loc 41, "Use" garlic on any of the three pipes at 2636-2638,3446: %hemenster_pipe_stashed), at the
//	    pipes (3), because the Sinister Stranger (a vampire) cannot stand the smell and moves to the willow tree.
//	2   The willow spot (fishing spot 233) yields sardines whatever the bait, and the Stranger fishes the pipes: sardines lose.
//	3   The pipes spot (fishing spot 234) yields a raw giant carp for a Red vine worm (bait, obj 25) and a sardine for plain bait.
//	    Each click is one cast (four ticks) and one bait; the third catch (comp stage 4) makes Bonzo judge the pack: a giant carp
//	    among the three fish wins (the three fish are taken and the Fishing trophy given, 4); none loses (comp stage 0, back to 1).
//	4   The trophy back to the Mountain Dwarf: "Have you won yet?" and no menu (5, with the pass and the trophy both in the pack).
//
// What the content does that a script must plan for:
//   - Morris's gate is a conversation inbound and, in the middle of a contest, a prompt outbound (Bonzo: "So you're calling it quits
//     here for now?", "Yes I'll compete again another day." resets the contest to stage 1 and the entry fee to unpaid). The right half
//     of the gate asks it at stage 1 as well, so the left half is the one the quest leaves by. The compound is registered as a
//     pocket for every quest's walks, so that a walk from inside to anywhere outside leaves by the gate first.
//   - Stashing the garlic BEFORE paying is the order that retries cheaply. Stash after paying (the TS def's order) needs the
//     Stranger within eight tiles of the player at that moment, and a Stranger who has just been moved by another account's garlic
//     is not; paid and unseated, a player has only quitting to try again. Stashed first, the seating is Bonzo's script, which is
//     free to talk to again for as long as the comp stage says paid and the stage says started ("I'll enter the competition
//     please." is a line, not a menu, in that state): it moves the Stranger and seats the player at the pipes the moment he is near.
//   - One worm wins, and three are carried: the win test is "a raw giant carp in the pack" at the third catch. The pack needs room
//     for three fish (hemenster_catch checks one slot with more than one bait) and a free slot for the trophy.
//   - The red worms are dug from the vines in McGrubor's Wood (Check, with a spade; one worm a dig, stackable), the wood being a
//     fenced pocket whose one door is the loose railing at 2662,3500, with ten level 44 guard dogs in it. The dogs are "cowardly":
//     they hunt only accounts of combat 88 or less, and only within a tile of themselves, so the vines' stand (2632,3497) is three
//     tiles from the reach of every dog's wander box, but the winding way in passes within two tiles of the spawns of the dogs at
//     2669,3488 and 2645,3488 and cannot be walked round (the planner finds no route with even a one-tile box round each spawn). The
//     trip is priced by the hitpoints the account goes in with and the food it carries (FishingWoodFit, BKFit's shape), topped up
//     before the railing, walked with the hostile-breaking walk and auto-retaliate off, and capped at one death a day.
//   - The pass, the worms, the garlic and the trophy are quest items the bank must not keep: the dwarf re-issues the pass only when
//     neither the pack nor the bank holds one, so a banked pass is withdrawn and never asked for again.
//
// The item rule (provision.go): the rod from the bank, the mule (18 are published) or Harry's Fishing Shop in Catherby (5 coins);
// the garlic from the bank or the mule (9), then Morgan's cupboard in Draynor (vampire slayer's own custom, unlimited); the spade
// from the bank or the mule, then the nearest of the four spawns on the map; the worms dug; the coins from the bank or the mule.

// FishingContestName is the quest tab's spelling (component 7359, `questlist:fishingcompo`).
const FishingContestName = "Fishing Contest"

// FishingContestID is the engine's id.
const FishingContestID = "fishingcompo"

// The %fishingcompo stages (quest_fishingcompo.constant; ^fishingcompo_complete is 5 in general/configs/quest.constant).
const (
	FcNotStarted = 0
	FcStarted    = 1
	FcInComp     = 2
	FcGarlicComp = 3
	FcWonComp    = 4
	FcComplete   = 5
)

// Ids from content/pack (checked against the production config by TestTotemFishingIdsMatchTheProductionConfig). The garlic, 1550,
// is ObjGarlic and the spade, 952, ObjSpade.
const (
	NpcMountainDwarf    = 232 // [tunnel_dwarf], "Mountain Dwarf"
	NpcBonzo            = 225
	NpcMorris           = 227
	NpcSinisterStranger = 226
	NpcWillowSpot       = 233 // [0_41_53_compofishspot]: sardines whatever the bait
	NpcPipesSpot        = 234 // [0_41_53_sinisterfishspot]: giant carp for a red vine worm
	NpcHarry            = 576 // Harry's Fishing Shop, Catherby
	NpcGuardDog         = 99  // [guarddog], level 44

	ObjRedVineWorm   = 25
	ObjFishingTrophy = 26
	ObjFishingPass   = 27
	ObjFishingRod    = 307
	ObjFishingBait   = 313
	ObjRawSardine    = 327
	ObjRawGiantCarp  = 338

	LocWallPipe           = 41 // [garlicpipe], op 1 Check, "Use" garlic
	LocHemensterGateLeft  = 47 // [fishinggateclosedl]
	LocHemensterGateRight = 48 // [fishinggateclosedr]
	LocMcGruborRailing    = 51 // [mcgruborlooserailing], op 1 Squeeze-through
)

// FcRedVineLocs are the diggable vines of McGrubor's Wood: the locs of the `red_vine` category (mcgrubors_wood.loc), seven shapes that
// all show as "Vine" with the op Check. A corner vine (2013) shows the same and is not in the category.
var FcRedVineLocs = []int{58, 2989, 2990, 2991, 2992, 2993, 2994}

// Tiles from content maps and the loc index.
var (
	// FcDwarfAnchor is the dwarf at the tunnel mouth west of White Wolf Mountain (m44_54, 2820,3487; a twin beside him).
	FcDwarfAnchor = tile.Tile{X: 2820, Z: 3487}
	// FcGateOutside and FcGateInside are the two sides of the gate's left half (loc 47 at 2642,3442, a wall on the east edge of its tile).
	FcGateOutside = tile.Tile{X: 2643, Z: 3442}
	FcGateInside  = tile.Tile{X: 2642, Z: 3442}
	// FcBonzoAnchor is Bonzo (m41_53, 2641,3439); Morris stands at the gate (2643,3440).
	FcBonzoAnchor = tile.Tile{X: 2641, Z: 3439}
	// FcPipeStand is the easternmost wall pipe's own tile: a straight wall decoration is operated from its own tile and from no other.
	FcPipeStand = tile.Tile{X: 2638, Z: 3446}
	// FcPipesSpot is the pipes' fishing spot (m41_53, 2637,3444).
	FcPipesSpot = tile.Tile{X: 2637, Z: 3444}
	// FcVineStand is the middle of the red-worm patch (2627-2633, 3492-3499), vines on three sides, three tiles or more from every
	// dog's wander box.
	FcVineStand = tile.Tile{X: 2632, Z: 3497}
	// FcWoodOutside is the tile outside the loose railing (the graph's edge is 2661,3500 to 2662,3500).
	FcWoodOutside = tile.Tile{X: 2661, Z: 3500}
	// FcHarry is Harry (m44_53, 2834,3445), whose shop sells a fishing rod for 5 coins.
	FcHarry = ShopRef{Npc: "Harry", NpcID: NpcHarry, Anchor: tile.Tile{X: 2834, Z: 3445}}
	// FcSeersBank is the bank the quest's module uses: Seers' Village is nearest to the compound, the wood and the Hemenster road.
	FcSeersBank = tile.Tile{X: 2725, Z: 3491}
)

// FcWoodDogs are the guard dogs of McGrubor's Wood and the road about it (content m41_54, all npc 99, wanderrange 3, huntrange 1).
var FcWoodDogs = []tile.Tile{
	{X: 2625, Z: 3502}, {X: 2628, Z: 3484}, {X: 2631, Z: 3472}, {X: 2636, Z: 3503}, {X: 2645, Z: 3488},
	{X: 2659, Z: 3478}, {X: 2665, Z: 3471}, {X: 2666, Z: 3506}, {X: 2669, Z: 3488}, {X: 2674, Z: 3504},
}

// FcDogSafeGap is how many tiles a stand in the wood must be from the nearest tile a guard dog can stand on (its spawn and the three
// tiles about it): a dog hunts within a tile of itself, so two is out of its reach.
const FcDogSafeGap = 2

// FcSafeFromDogs reports whether a tile in McGrubor's Wood is out of the reach of every one of its guard dogs, wherever in its wander
// box each stands.
func FcSafeFromDogs(t tile.Tile) bool {
	for _, dog := range FcWoodDogs {
		dx, dz := t.X-dog.X, t.Z-dog.Z
		if dx < 0 {
			dx = -dx
		}
		if dz < 0 {
			dz = -dz
		}
		if max(max(dx-3, 0), max(dz-3, 0)) < FcDogSafeGap {
			return false
		}
	}
	return true
}

// FcSpadeSpawns are the spades lying on the map (obj 952, one each): East Ardougne (Edmond's house), Falador, beside the Draynor
// manor and on the first floor of Varrock's west house.
var FcSpadeSpawns = []tile.Tile{{X: 2574, Z: 3331}, {X: 2981, Z: 3369}, {X: 3120, Z: 3359}, {X: 3218, Z: 3412, Level: 1}}

// fcCompoundRows are the rows of the competition ground: for each z from 3413 up to 3446, the least and greatest x of the tiles the pack
// reaches on foot from Bonzo (184 of them; totemfish_pack_test.go proves the table against the pack). The yard is a lake shore, not a
// rectangle: broad at 3434 to 3440 and a narrow strip south of it, with the house east of the strip (entered from the road, by the door
// at 2641,3424) and the road beside the gate outside it.
var fcCompoundRows = [34][2]int{
	{2629, 2629}, {2628, 2630}, {2628, 2631}, {2629, 2632}, {2629, 2633}, {2630, 2633}, // 3413-3418
	{2630, 2635}, {2630, 2636}, {2631, 2637}, {2632, 2637}, {2633, 2636}, {2633, 2637}, // 3419-3424
	{2633, 2636}, {2633, 2636}, {2633, 2637}, {2633, 2636}, {2633, 2636}, {2633, 2636}, // 3425-3430
	{2633, 2636}, {2635, 2636}, {2632, 2636}, {2632, 2642}, {2631, 2642}, {2632, 2642}, // 3431-3436
	{2633, 2642}, {2633, 2642}, {2635, 2642}, {2637, 2642}, {2638, 2642}, {2638, 2642}, // 3437-3442
	{2638, 2642}, {2638, 2642}, {2638, 2641}, {2638, 2640}, // 3443-3446
}

// InFishingCompound reports a tile on the Hemenster competition ground (ground floor).
func InFishingCompound(t tile.Tile) bool {
	if t.Level != 0 || t.Z < 3413 || t.Z > 3446 {
		return false
	}
	row := fcCompoundRows[t.Z-3413]
	return t.X >= row[0] && t.X <= row[1]
}

// Custom step names (internal/scripts/questmods) and kit names.
const (
	CustomFishGarlic = "fishing-garlic"
	CustomFishDig    = "fishing-dig"
	CustomFishPay    = "fishing-pay"
	CustomFishStash  = "fishing-stash"
	CustomFishLeave  = "fishing-leave"
	CustomFishFish   = "fishing-fish"
	CustomFishSpare  = "fishing-spare"

	KitFishSupplies = "fishing-supplies"
	KitFishSpade    = "fishing-spade"
	KitFishFood     = "fishing-food"
)

// The numbers of the contest and of its worm trip.
const (
	// FcEntryFee is Bonzo's fee.
	FcEntryFee = 5
	// FcFishing is the level the fishing spots want (attempt_fish_hemenster: stat(fishing) < 10).
	FcFishing = 10
	// FcWormTarget is the worms carried into the contest: one wins, three are cast, and a cast the server refuses is spare.
	FcWormTarget = 3
	// FcCoinsQty and FcCoinsFloor are the supplies pass's draw and the least the pack may hold before the pass is made: the fee, a
	// rod, and the Catherby, Ardougne and Brimhaven ships (30 each).
	FcCoinsQty   = 200
	FcCoinsFloor = 100
	// FcMinFreeSlots is the pack room the contest wants when the account is about to enter: the pass, rod, spade, worms, garlic and
	// coins it carries and the three fish and the trophy it is handed.
	FcMinFreeSlots = 8
	// FcFishLevelPark is how long an account under Fishing 10 is left alone.
	FcFishLevelPark = 6 * time.Hour
	// FcRetryPark is how long a leg that failed on its last run waits before it is tried again.
	FcRetryPark = 30 * time.Minute
	// FcOddsPark is how long an account the wood's dogs are priced against is left alone: its hitpoints and Defence move slowly.
	FcOddsPark = 3 * time.Hour
)

// The wood's pricing (FishingWoodFit), in BKFit's shape. A guard dog is level 44 and "cowardly" (all.hunt: check_nottoostrong), so it
// hunts nothing above combat 88; it hits for up to 5 every four ticks. The way in passes the reach of two dogs, the way out the same
// two, and a dog that is hunting an account follows it at the account's pace: FcWoodAttacks is the attacks of one dog the trip is
// priced at (about fifty ticks of a dog at the account's heels) and FcWoodImmuneAttacks the few a dog the walk startles gets in
// before the account is above its ceiling. The hitpoints have a floor of their own, as the knights' do.
const (
	FcDogHuntCeiling      = 88
	FcWoodAttacks         = 14
	FcWoodImmuneAttacks   = 2
	FcWoodMinHitpoints    = 30
	FcWoodMinHitpointsImm = 15
)

// fishFoodIDs are the foods the quest eats and the pricing credits, best first.
var fishFoodIDs = []int{385, 373, 379, 365, 361, 329, 333, 2309, 1891, 1893, 1895}

// FishFoodIDs returns the food list (a copy).
func FishFoodIDs() []int { return append([]int(nil), fishFoodIDs...) }

// fishTools are the quest's own items, which the module protects from the floor bite and every bank visit leaves in the pack; fishKeep
// adds the food.
var (
	fishTools = []int{ObjFishingPass, ObjFishingTrophy, ObjRedVineWorm, ObjFishingBait, ObjFishingRod, ObjGarlic, ObjSpade, ObjRawGiantCarp, CoinsID}
	fishKeep  = append(append([]int(nil), fishTools...), fishFoodIDs...)
)

// FishKeepIDs returns the keep list (a copy).
func FishKeepIDs() []int { return append([]int(nil), fishKeep...) }

// The stops. The dwarf's start is a six-deep chain whose pages all repeat "grumpy little man", so each rung names a line of its own;
// the other talks have no menu in the state they are made in and carry a placeholder (a talk step must name one, and an unexpected
// menu is stopped at, not guessed at).
var (
	FcDwarfStartStop = NpcStop{Npc: "Mountain Dwarf", ID: NpcMountainDwarf, Anchor: FcDwarfAnchor, Leash: 8,
		Prefer: []string{
			"I was just wondering what was down those stairs?",
			"Why not?",
			"If you were my friend I wouldn't mind.",
			"Well, let's be friends!",
			"And how am I meant to do that?",
			"Fortunately I'm alright at fishing!",
		}}
	// FcDwarfPassStop asks for a spare pass (tunnel_dwarf_won: offered only while neither pack nor bank holds one).
	FcDwarfPassStop = NpcStop{Npc: "Mountain Dwarf", ID: NpcMountainDwarf, Anchor: FcDwarfAnchor, Leash: 8,
		Prefer: []string{"I need another competition pass."}}
	// FcDwarfTrophyStop hands the trophy over: no menu while the pack holds both the pass and the trophy.
	FcDwarfTrophyStop = FcDwarfPassStop
	// FcBonzoStop takes the fee and seats the player ("I'll enter the competition please."); a line, not a menu, once the fee is paid.
	FcBonzoStop = NpcStop{Npc: "Bonzo", ID: NpcBonzo, Anchor: FcBonzoAnchor, Leash: 8,
		Prefer: []string{"I'll enter the competition please."}}
	// FcBonzoSpareStop is Bonzo's "Hello champ!" and the spare trophy: no menu at all.
	FcBonzoSpareStop = FcBonzoStop
)

// FcQuitContest is the prompt's line that resets the contest (bonzo_quits).
const FcQuitContest = "Yes I'll compete again another day."

// fcJournalNeedles are ParseFishingContestJournal's phrases, newest first, written against NormaliseLines (fishingcompo_journal.rs2;
// the page is rebuilt at each stage but its first lines repeat from stage 1, so these are the lines that differ).
var fcJournalNeedles = []struct {
	stage  int
	needle string
}{
	{FcComplete, "quest complete"},
	{FcWonComp, "i easily won the contest"},
	{FcGarlicComp, "next to the pipes"},
	{FcInComp, "next to the willow tree"},
	{FcStarted, "fishing contest pass"},
	{FcNotStarted, "i can start this quest by speaking to"},
}

// ParseFishingContestJournal reads the stage from the rendered journal; ok is false for an empty or unrecognised journal, which is
// unknown, never stage 0.
func ParseFishingContestJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	if text == "" {
		return 0, false
	}
	for _, n := range fcJournalNeedles {
		if strings.Contains(text, n.needle) {
			return n.stage, true
		}
	}
	return 0, false
}

// FcPastStranger reports the account past the quest's one shared step, the Sinister Stranger (a single npc whom the garlic moves for
// every player, and whom the next account's seating needs back at his post): seated at the pipes or beyond.
func FcPastStranger(s Snapshot) bool {
	if s.Journal == StatusComplete {
		return true
	}
	return s.Journal == StatusInProgress && s.StageKnown && s.Stage >= FcGarlicComp
}

// fcFishingShort reports an account whose Fishing is read and under 10. A skill the snapshot does not name is unread (BaseLevels leaves
// out a stat no packet has set), and an unread level is not judged.
func fcFishingShort(s Snapshot) bool {
	lvl, read := s.Levels["fishing"]
	return read && lvl < FcFishing
}

// fcBank is the bank a visit uses: the one nearest the account, Seers' Village before a position has arrived.
func fcBank(s Snapshot) tile.Tile {
	if s.TileKnown {
		return NearestBank(s.Tile).Tile
	}
	return FcSeersBank
}

// fcWait is a wait that parks the quest for d with the reason.
func fcWait(d time.Duration, reason string) Step {
	return Step{Kind: StepWait, Reason: reason, ParkFor: d}
}

// fcRodSources are the shops that sell a fishing rod, the one nearer the account first: Harry's in Catherby and Gerrant's in Port
// Sarim (5 coins each, 5 on the shelf). Before a position has arrived the order is Harry's.
func fcRodSources(s Snapshot) []Source {
	shops := []ShopRef{FcHarry, FluffsSardineShop}
	if s.TileKnown {
		here := s.Tile
		sort.SliceStable(shops, func(i, j int) bool {
			fi, fj := freshstart.FareToCross(&here, &shops[i].Anchor), freshstart.FareToCross(&here, &shops[j].Anchor)
			if fi != fj {
				return fi < fj
			}
			return tile.Chebyshev(here, shops[i].Anchor) < tile.Chebyshev(here, shops[j].Anchor)
		})
	}
	out := make([]Source, 0, len(shops))
	for _, sh := range shops {
		out = append(out, Source{Kind: SourceShop, Shop: sh, Price: 5, Note: sh.Npc + "'s fishing shop keeps 5 rods"})
	}
	return out
}

// fcSuppliesKit is the rod, the garlic and the coins: the rod from the bank, the mule or a fishing shop; the garlic from the bank or
// the mule only (the cupboard in Draynor is its own step, CustomFishGarlic); the coins optional, because the ladder's travel hook
// draws the fares as well. The garlic is left out of a run that has stashed it already.
func fcSuppliesKit(s Snapshot) []KitLine {
	kit := []KitLine{{ID: ObjFishingRod, Name: "Fishing rod", Qty: 1, Sources: fcRodSources(s)}}
	if !s.Ran[CustomFishStash] {
		kit = append(kit, KitLine{ID: ObjGarlic, Name: "Garlic", Qty: 1, Optional: true})
	}
	// Plain bait is the other two casts of a contest that has one worm: the third catch is judged by "a giant carp among the fish".
	kit = append(kit, KitLine{ID: ObjFishingBait, Name: "Fishing bait", Qty: FcWormTarget, Stackable: true, Optional: true})
	return append(kit, KitLine{ID: CoinsID, Name: "Coins", Qty: FcCoinsQty, Stackable: true, Optional: true})
}

// FcBaitReady reports whether the pack holds the bait for the whole contest: three worms, or at least one worm (a giant carp wins
// the contest alone) and plain bait for the rest of the three casts.
func FcBaitReady(s Snapshot) bool {
	worms := s.HeldID(ObjRedVineWorm)
	return worms >= FcWormTarget || (worms >= 1 && worms+s.HeldID(ObjFishingBait) >= FcWormTarget)
}

// fcCanFish reports whether the pack can carry on a contest at the pipes: a worm for the first cast, or a giant carp already caught
// and plain bait for the next.
func fcCanFish(s Snapshot) bool {
	return s.HeldID(ObjRedVineWorm) >= 1 || (s.HeldID(ObjRawGiantCarp) >= 1 && s.HeldID(ObjFishingBait) >= 1)
}

// FcSpadeSources lists the spawns the spade can be fetched from, the nearest first to an account that stands somewhere: the spawns on
// the account's own side of the water before those across it, nearer before further. Before a position has arrived the list is
// the map's order.
func FcSpadeSources(s Snapshot) []Source {
	spawns := append([]tile.Tile(nil), FcSpadeSpawns...)
	if s.TileKnown {
		here := s.Tile
		sort.SliceStable(spawns, func(i, j int) bool {
			fi, fj := freshstart.FareToCross(&here, &spawns[i]), freshstart.FareToCross(&here, &spawns[j])
			if fi != fj {
				return fi < fj
			}
			return tile.Chebyshev(here, spawns[i]) < tile.Chebyshev(here, spawns[j])
		})
	}
	out := make([]Source, 0, len(spawns))
	for _, t := range spawns {
		out = append(out, Source{Kind: SourceGround, Tile: t, PerSpawn: 1, RespawnTicks: 100, Note: "a spade lying on the map"})
	}
	return out
}

// fcSpadeKit is the spade for the vines.
func fcSpadeKit(s Snapshot) []KitLine {
	return []KitLine{{ID: ObjSpade, Name: "Spade", Qty: 1, Sources: FcSpadeSources(s)}}
}

// fcFoodKit is food for the wood: the bank and the mule only.
func fcFoodKit() []KitLine {
	return []KitLine{{ID: 385, Name: "Shark", Qty: 4, Optional: true}, {ID: 373, Name: "Swordfish", Qty: 4, Optional: true},
		{ID: 379, Name: "Lobster", Qty: 4, Optional: true}, {ID: 365, Name: "Bass", Qty: 4, Optional: true},
		{ID: 361, Name: "Tuna", Qty: 4, Optional: true}}
}

// FcImmune reports whether the account's combat level is above the dogs' hunt ceiling: an unread level is not.
func FcImmune(s Snapshot) bool { return bkCombat(s.Levels) > FcDogHuntCeiling }

// FishingWoodDamage is the hitpoints the trip is priced to take from the dogs: the attacks above, each the model's expected damage on
// this account's Defence, with the fight model's quarter on top (FightIncomingScale).
func FishingWoodDamage(s Snapshot) float64 {
	attacks := FcWoodAttacks
	if FcImmune(s) {
		attacks = FcWoodImmuneAttacks
	}
	return float64(attacks) * FoePerAttack(FoeGuardDog, fighterOf(s)) * FightIncomingScale
}

// FishingWoodFit is the gate before the railing: the hitpoints, and the hitpoints above two dog hits plus the food in the pack covering
// twice what the trip is priced at. An unread maximum passes, as every other gate here treats an unread stat. Food can be eaten on
// the road (nothing on the trip is a chat), so all of the pack's counts.
func FishingWoodFit(s Snapshot) (bool, string) {
	if s.MaxHP <= 0 {
		return true, ""
	}
	need := FcWoodMinHitpoints
	if FcImmune(s) {
		need = FcWoodMinHitpointsImm
	}
	if s.MaxHP < need {
		return false, fmt.Sprintf("not going into McGrubor's Wood for the worms yet: ten level-44 guard dogs hunt there (a hit of up to %d) and the way to the vines passes the reach of two of them (this account has %d hitpoints at combat %d, the gate is %d)",
			FoeGuardDog.PeakHit(), s.MaxHP, bkCombat(s.Levels), need)
	}
	damage := FishingWoodDamage(s)
	room := float64(s.MaxHP - 2*FoeGuardDog.PeakHit())
	food := FoodPoolHeal(s.InvIDs, fishFoodIDs)
	if have := room + float64(food); have < 2*damage {
		return false, fmt.Sprintf("not going into McGrubor's Wood with this pack: the dogs are priced at %d hitpoints (combat %d, Defence %d) and %d hitpoints less two of their hits plus %d of food is %d, short of twice that",
			int(damage+0.5), bkCombat(s.Levels), s.Level("defence"), s.MaxHP, food, int(have))
	}
	return true, ""
}

// fcProvision is a provisioning step of the quest.
func fcProvision(s Snapshot, kit string, lines []KitLine, why string) Step {
	return Step{Kind: StepProvision, KitName: kit, Kit: lines, KeepIDs: fishKeep, Bank: fcBank(s), BankSet: true, Reason: why}
}

// fcStart is stage 0: the level, the fares, a free slot for the pass, and the dwarf.
func fcStart(s Snapshot) Step {
	if fcFishingShort(s) {
		return fcWait(FcFishLevelPark, "Fishing 10 is needed to fish the contest's spots (the account has "+strconv.Itoa(s.Level("fishing"))+")")
	}
	if s.HeldID(CoinsID) < FcCoinsFloor && !s.Ran["provision:"+KitFishSupplies] && !s.Failed["provision:"+KitFishSupplies] {
		return fcProvision(s, KitFishSupplies, fcSuppliesKit(s), "coins for the fee and the ships, a rod and the garlic")
	}
	if s.FreeSlots < 1 {
		return Step{Kind: StepDeposit, KeepIDs: fishKeep, Bank: fcBank(s), BankSet: true, Reason: "room for the competition pass"}
	}
	return Step{Kind: StepTalk, Stop: FcDwarfStartStop}
}

// fcPass makes the pack hold the pass: the bank's (read first: the dwarf gives a spare only to an account whose pack and bank hold
// none), else the dwarf's spare. The second result is false when the pass is held.
func fcPass(s Snapshot) (Step, bool) {
	if s.HeldID(ObjFishingPass) > 0 {
		return Step{}, false
	}
	if !s.BankKnown {
		return Step{Kind: StepScanBank, Bank: fcBank(s), BankSet: true, Reason: "the competition pass may be in the bank (the dwarf gives a spare only when it is in neither)"}, true
	}
	if n, _ := s.BankedID(ObjFishingPass); n > 0 {
		return Step{Kind: StepWithdraw, Items: []WithdrawItem{{Name: "Fishing pass", Qty: 1, ID: ObjFishingPass}}, Bank: fcBank(s), BankSet: true,
			Reason: "the competition pass is in the bank"}, true
	}
	return Step{Kind: StepTalk, Stop: FcDwarfPassStop, UntilItemID: ObjFishingPass}, true
}

// fcPrepare is stage 1: everything the contest takes into the compound, then Bonzo.
func fcPrepare(s Snapshot) Step {
	if st, ok := fcPass(s); ok {
		return st
	}
	held := s.HeldID
	// The rod, the garlic and the coins: one pass of the item rule. The pass is repeated while the rod is missing (a rod is the
	// one line that is not optional); the garlic and the coins are asked once, whatever the pass found.
	kit := "provision:" + KitFishSupplies
	needGarlic := !s.Ran[CustomFishStash] && held(ObjGarlic) == 0
	passDone := s.Ran[kit] || s.Failed[kit]
	if held(ObjFishingRod) == 0 || (!passDone && (needGarlic || held(CoinsID) < FcCoinsFloor)) {
		if s.Blocked[kit] && held(ObjFishingRod) == 0 {
			return fcWait(FcRetryPark, "no fishing rod from the bank, the mule or a fishing shop")
		}
		return fcProvision(s, KitFishSupplies, fcSuppliesKit(s), "a fishing rod, a clove of garlic and coins for the fee and the ships")
	}
	if needGarlic {
		if s.Failed[CustomFishGarlic] {
			return fcWait(FcRetryPark, "no garlic from the bank, the mule or Morgan's cupboard in Draynor")
		}
		return Step{Kind: StepCustom, Custom: CustomFishGarlic, Reason: "a clove of garlic from Morgan's cupboard in Draynor Village"}
	}
	// The worms.
	if !FcBaitReady(s) {
		if held(ObjSpade) == 0 {
			if s.Failed["provision:"+KitFishSpade] || s.Blocked["provision:"+KitFishSpade] {
				return fcWait(FcRetryPark, "no spade from the bank, the mule or the map's spawns")
			}
			return fcProvision(s, KitFishSpade, fcSpadeKit(s), "a spade for the red vines")
		}
		if ok, why := FishingWoodFit(s); !ok {
			if !s.Ran["provision:"+KitFishFood] && !s.Failed["provision:"+KitFishFood] {
				return fcProvision(s, KitFishFood, fcFoodKit(), "food for the guard dogs: "+why)
			}
			return fcWait(FcOddsPark, why)
		}
		if s.Failed[CustomFishDig] {
			return fcWait(FcRetryPark, "the red vines gave no worms on the last try")
		}
		return Step{Kind: StepCustom, Custom: CustomFishDig, Qty: FcWormTarget, Reason: "dig red vine worms in McGrubor's Wood"}
	}
	// Room, the fee, and Bonzo.
	if s.FreeSlots < FcMinFreeSlots {
		return Step{Kind: StepDeposit, KeepIDs: fishKeep, Bank: fcBank(s), BankSet: true, Reason: "room for the fish and the trophy"}
	}
	if held(CoinsID) < FcEntryFee {
		return fcWait(FcRetryPark, "no "+strconv.Itoa(FcEntryFee)+" coins for the contest fee from the bank or the mule")
	}
	// The garlic goes in the pipe BEFORE the fee (see the file comment): a step of its own, so that the engine's memory of it
	// (Snapshot.Ran) says the pipe is stashed whatever Bonzo does next, and no second clove is fetched for a seat that failed.
	if held(ObjGarlic) > 0 && !s.Ran[CustomFishStash] {
		if s.Failed[CustomFishStash] {
			return fcWait(FcRetryPark, "the garlic would not go in the wall pipe on the last try")
		}
		return Step{Kind: StepCustom, Custom: CustomFishStash, Reason: "into the competition ground and the garlic in the wall pipe, before the fee"}
	}
	if s.Failed[CustomFishPay] {
		return fcWait(FcRetryPark, "Bonzo did not seat the account on the last try")
	}
	return Step{Kind: StepCustom, Custom: CustomFishPay, Reason: "the fee to Bonzo, and the seat at the pipes"}
}

// fcInComp is stage 2: seated at the willow tree, which cannot win. Garlic in the pack goes in the pipe (the Stranger moves and the
// seat changes at once); none, and the account quits the contest to fetch some.
func fcInComp(s Snapshot) Step {
	if s.HeldID(ObjGarlic) > 0 && !s.Failed[CustomFishStash] {
		return Step{Kind: StepCustom, Custom: CustomFishStash, Reason: "garlic in the wall pipe, so that the Stranger moves to the willow tree"}
	}
	if s.Failed[CustomFishLeave] {
		return fcWait(FcRetryPark, "the account could not quit the contest on its last try")
	}
	return Step{Kind: StepCustom, Custom: CustomFishLeave, Reason: "the willow tree's spot cannot win and there is no garlic to move the Stranger: quit the contest"}
}

// fcContest is stage 3: seated at the pipes. A rod and a worm in the pack, and the account fishes; either missing, and it quits to
// get them (the entry is lost, the pipe stays stashed).
func fcContest(s Snapshot) Step {
	if s.HeldID(ObjFishingRod) == 0 || !fcCanFish(s) {
		if s.Failed[CustomFishLeave] {
			return fcWait(FcRetryPark, "the account could not quit the contest on its last try")
		}
		return Step{Kind: StepCustom, Custom: CustomFishLeave, Reason: "no rod or no worm left: quit the contest and fetch them"}
	}
	if st, ok := fcPass(s); ok && !(s.TileKnown && InFishingCompound(s.Tile)) {
		return st
	}
	if s.Failed[CustomFishFish] {
		return fcWait(FcRetryPark, "the pipes' fishing spot gave no catch on the last try")
	}
	return Step{Kind: StepCustom, Custom: CustomFishFish, Reason: "fish the pipes' spot with the red vine worms"}
}

// fcWon is stage 4: the trophy to the dwarf, from the pack, the bank or Bonzo's spare.
func fcWon(s Snapshot) Step {
	if s.HeldID(ObjFishingTrophy) > 0 {
		return Step{Kind: StepTalk, Stop: FcDwarfTrophyStop}
	}
	if !s.BankKnown {
		return Step{Kind: StepScanBank, Bank: fcBank(s), BankSet: true, Reason: "the trophy may be in the bank"}
	}
	if n, _ := s.BankedID(ObjFishingTrophy); n > 0 {
		return Step{Kind: StepWithdraw, Items: []WithdrawItem{{Name: "Fishing trophy", Qty: 1, ID: ObjFishingTrophy}}, Bank: fcBank(s), BankSet: true,
			Reason: "the trophy is in the bank"}
	}
	if st, ok := fcPass(s); ok && !(s.TileKnown && InFishingCompound(s.Tile)) {
		return st
	}
	if s.Failed[CustomFishSpare] {
		return fcWait(FcRetryPark, "Bonzo gave no spare trophy on the last try")
	}
	return Step{Kind: StepCustom, Custom: CustomFishSpare, Reason: "ask Bonzo for the spare trophy"}
}

// DecideFishingContest is the whole quest, read from the journal stage, the pack and where the account stands.
func DecideFishingContest(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return fcStart(s)
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	switch s.Stage {
	case FcNotStarted:
		return fcStart(s)
	case FcStarted:
		return fcPrepare(s)
	case FcInComp:
		return fcInComp(s)
	case FcGarlicComp:
		return fcContest(s)
	case FcWonComp:
		return fcWon(s)
	case FcComplete:
		return Step{Kind: StepWait, Reason: "the quest tab has not turned green yet"}
	}
	return Step{Kind: StepWait, Reason: "unrecognised Fishing Contest stage"}
}

// FishingContestRecord is the quest's identity. Fishing 10 is the content's own gate (the contest's spots refuse a lower level),
// stated in the record so that the ladder holds an account short of it from the start and does not walk it to the Catherby dwarf
// and Hemenster only to be refused at the first cast.
var FishingContestRecord = Record{ID: FishingContestID, Name: FishingContestName, QuestPoints: 1,
	Requirements: Requirements{Skills: []SkillReq{{Skill: "fishing", Level: FcFishing}}}}

// FishingContest is the engine module. It never fights (NoRetaliate: the wood's dogs hunt an account of combat 88 or less within a
// tile, and are walked away from), one death anywhere parks it for a day, and a death on the worm trip parks it for a day whatever
// the cap says. ExclusiveDone releases the fleet-wide slot once the account is seated at the pipes (the Sinister Stranger is the
// shared step).
var FishingContest = Module{
	Record: FishingContestRecord, Bank: FcSeersBank, BankSet: true, Anchor: FcBonzoAnchor,
	ToolIDs:     fishTools,
	ReadStage:   ParseFishingContestJournal,
	Sustain:     Sustain{FoodIDs: fishFoodIDs, EatAt: 12},
	NoRetaliate: true,
	MaxDeaths:   1,
	StepDeathCaps: map[string]StepDeathCap{
		CustomFishDig: {Max: 1, Window: 24 * time.Hour, Skip: 24 * time.Hour},
	},
	ExclusiveDone: FcPastStranger,
	Decide:        DecideFishingContest,
}
