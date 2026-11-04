package questplan

import (
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// Witch's House (content id `ball`, %ballquest, not transmitted), from the quest-chain study
// (private evidence notes) and the TS def
// (rsb0t-main/src/bot/api/ai/quests/defs/witchshouse). Three things the study found in content
// and this port keeps: the door key is under the potted plant; reading the diary moves the stage
// from 3 to 5, after which the witch's catch no longer relocks the back door; and the shed's four
// forms have magic 1, so they are cast at, the glob from 2937,3466 (outside its maxrange) and
// the rest from the one-front tile 2936,3459 (where the size-2 bear freezes at 2936,3461).

// WitchsHouseName is the quest tab's spelling (component 7351).
const WitchsHouseName = "Witch's House"

// WitchsHouseID is the engine's id: the ladder's rung id (content's own is `ball`).
const WitchsHouseID = "witch"

// Stages (quest_ball.constant; 4 is unused, and 3 and 5 render the same journal page).
const (
	WitchStarted  = 1
	WitchMagnet   = 2
	WitchUnlocked = 3
	WitchDiary    = 5
	WitchDefeated = 6
	WitchComplete = 7
)

// Ids from content/pack.
const (
	NpcBallBoy      = 895
	NpcNoraHagg     = 896
	NpcWitchMouse   = 901
	ObjWitchBall    = 2407
	ObjWitchDiary   = 2408
	ObjWitchDoorKey = 2409
	ObjWitchMagnet  = 2410
	ObjWitchShedKey = 2411
	ObjLeatherGlove = 1059
	ObjCheese       = 1985
	LocWitchPot     = 2867
	LocWitchCupShut = 2868
	LocWitchCupOpen = 2869
	LocWitchHole    = 2870
	LocWitchShed    = 2863
	LocWitchFount   = 2864
)

// Forms of the shapeshifter, in the order witches_experiement.rs2 spawns them.
var WitchForms = []int{897, 898, 899, 900}

// Tiles (TS areas.ts WH_TILE, checked against the study).
var (
	BallBoyAnchor   = tile.Tile{X: 2928, Z: 3456}
	WitchPotStand   = tile.Tile{X: 2899, Z: 3474}
	WitchPotTile    = tile.Tile{X: 2900, Z: 3474}
	WitchCupStand   = tile.Tile{X: 2899, Z: 9873}
	WitchCupTile    = tile.Tile{X: 2898, Z: 9873}
	WitchPorch      = tile.Tile{X: 2902, Z: 3466}
	WitchHoleTile   = tile.Tile{X: 2903, Z: 3466}
	WitchDiaryStand = tile.Tile{X: 2901, Z: 3473, Level: 1}
	WitchFountStand = tile.Tile{X: 2911, Z: 3470}
	WitchFountTile  = tile.Tile{X: 2909, Z: 3470}
	WitchShedStand  = tile.Tile{X: 2933, Z: 3463}
	WitchShedDoor   = tile.Tile{X: 2934, Z: 3463}
	WitchBallStand  = tile.Tile{X: 2935, Z: 3461}
	// WitchGlobSpot is outside the glob's maxrange (2) from its spawn 2935,3462: it never chases.
	WitchGlobSpot = tile.Tile{X: 2937, Z: 3466}
	// WitchOneFront is reached only through the corridor tile 2936,3460; the bear cannot fit it.
	WitchOneFront = tile.Tile{X: 2936, Z: 3459}
	// FaladorWestBank is the TS def's bank; Taverley has none.
	FaladorWestBank = tile.Tile{X: 2946, Z: 3369}
	witchFoods      = []int{385, 373, 379, 365, 361, 329, 333, 2309, 2142, 1891, 1893, 1895}
	witchTools      = []int{ObjWitchDoorKey, ObjWitchShedKey, ObjWitchMagnet, ObjCheese, ObjWitchBall, ObjWitchDiary,
		ObjLeatherGlove, CoinsID, 554, 556, 558, 562, 1381}
)

// InWitchGarden reports the ring corridor and the fountain yard past the back door.
func InWitchGarden(t tile.Tile) bool {
	in := func(x0, x1, z0, z1 int) bool {
		return t.Level == 0 && t.X >= x0 && t.X <= x1 && t.Z >= z0 && t.Z <= z1
	}
	return in(2901, 2933, 3460, 3465) || in(2912, 2933, 3466, 3466) || in(2908, 2912, 3467, 3475)
}

// InWitchShed reports the shed, where the ball and the shapeshifter are.
func InWitchShed(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 2934 && t.X <= 2937 && t.Z >= 3459 && t.Z <= 3467
}

// Custom step names (internal/scripts/questmods).
const (
	CustomWitchPot      = "witch-pot"
	CustomWitchMagnet   = "witch-magnet"
	CustomWitchDropMag  = "witch-drop-magnet"
	CustomWitchMouse    = "witch-mouse"
	CustomWitchDiary    = "witch-diary"
	CustomWitchFountain = "witch-fountain"
	CustomWitchShed     = "witch-shed"
	CustomWitchBall     = "witch-ball"
	CustomWitchOut      = "witch-out"
	KitWitch            = "witch-kit"
)

var (
	BallBoyStop = NpcStop{Npc: "Boy", ID: NpcBallBoy, Anchor: BallBoyAnchor, Leash: 6,
		Prefer: []string{"What's the matter?", "Ok, I'll see what I can do."}}
	ThessaliaShop = ShopRef{Npc: "Thessalia", Anchor: tile.Tile{X: 3204, Z: 3417}}
)

var witchJournal = []struct {
	stage  int
	needle string
}{
	{WitchComplete, "quest complete"},
	{WitchDefeated, "now the shapeshifter is dead"},
	{WitchUnlocked, "i have worked out how to unlock the back door"},
	{WitchMagnet, "i have found a magnet in a cupboard"},
	{WitchStarted, "i should find a way into the garden"},
	{0, "i can start this quest"},
}

// ParseWitchsHouseJournal reads ball_journal.rs2's rendered stage, newest first (the page keeps
// earlier lines struck through, so only the order separates them). Stage 3 is also stage 5.
func ParseWitchsHouseJournal(lines []string) (int, bool) {
	text := NormaliseLines(lines)
	if text == "" {
		return 0, false
	}
	for _, n := range witchJournal {
		if strings.Contains(text, n.needle) {
			return n.stage, true
		}
	}
	return 0, false
}

// witchCasts covers the four forms' 144 hitpoints at ~3.3 a Fire Strike, with misses.
const witchCasts = 70

// The food the kit draws: ten pieces, not twenty. The kit step never trims what a pack already holds
// of a kit line, and a Stage 5 pack comes with the last quest's sharks, so twenty pieces of food left
// the pot, the diary and the fountain no slot to put their item in (34 of the 45 runs that parked on
// "witch-pot" had no free slot: the plant's key is added only when there is room). Six sharks and
// four lobsters are 168 hitpoints of food, which WitchShedReady judges against the fight's cost.
const (
	witchSharks   = 6
	witchLobsters = 4
)

// witchKit is the gloves, two cheese, and the casting kit: Wind Bolt (one chaos, two air) when
// Magic allows it, Fire Strike (one mind, three fire, two air) otherwise; a staff of air pays
// the air. Food is taken when owned: nothing near Taverley sells any.
func witchKit(s Snapshot, cheese int) []KitLine {
	betty := func(price int) []Source { return []Source{{Kind: SourceShop, Shop: BettyShop, Price: price}} }
	kit := []KitLine{
		{ID: CoinsID, Name: "Coins", Qty: 20, Stackable: true},
		{ID: ObjLeatherGlove, Name: "Leather gloves", Qty: 1, Worn: true, Sources: []Source{
			{Kind: SourceShop, Shop: ThessaliaShop, Price: 6},
		}},
		{ID: 385, Name: "Shark", Qty: witchSharks, Optional: true},
		{ID: 379, Name: "Lobster", Qty: witchLobsters, Optional: true},
	}
	if cheese > 0 {
		kit = append(kit, KitLine{ID: ObjCheese, Name: "Cheese", Qty: cheese, Sources: []Source{
			{Kind: SourceShop, Shop: WydinShop, Price: 4},
		}})
	}
	if s.Level("magic") >= 17 {
		kit = append(kit, KitLine{ID: 562, Name: "Chaos rune", Qty: witchCasts, Stackable: true, Sources: betty(15)})
	} else {
		kit = append(kit,
			KitLine{ID: 558, Name: "Mind rune", Qty: witchCasts, Stackable: true, Sources: betty(3)},
			KitLine{ID: 554, Name: "Fire rune", Qty: 3 * witchCasts, Stackable: true, Sources: betty(4)})
	}
	if ownsAirStaff(s) {
		kit = append(kit, KitLine{ID: 1381, Name: "Staff of air", Qty: 1, Optional: true, Worn: true})
	} else {
		kit = append(kit, KitLine{ID: 556, Name: "Air rune", Qty: 2 * witchCasts, Stackable: true, Sources: betty(4)})
	}
	return kit
}

// witchRoomKeep is what the quest keeps when it banks to make room: its tools (the module's) and the
// two fish the fight is eaten with. Cake, bread and the rest go to the bank, and the kit draws them
// back only if it wants them.
var witchRoomKeep = []int{385, 379}

// witchMinFree is the pack slots the quest's pickups need: the door key, the diary, the shed key and
// the ball (the magnet and the cheese come with the first two stages).
const witchMinFree = 4

// witchBankStep draws the quest items the bank already holds. The engine's cupboard ("You don't find
// anything interesting") and fountain ("There is nothing in the fountain.") count the bank, so an
// account whose pack lost the item to a deposit-all can never be given it again: 262 of 523 runs from
// 6 to 15 Oct 2026 ended at the fountain, on 13 accounts that each held the shed key in the bank, and
// 23 at the cupboard on one holding the magnet there. The plant's key has no bank check (the pot
// gives another every time, which is why those accounts bank 30 door keys and 30 diaries), but a key
// in the bank saves the walk to Taverley's plant. wantShed is whether the shed key is wanted now.
// The bank must have been read this run (BankedID), else nothing is known to be there.
func witchBankStep(s Snapshot, stage int, wantShed bool) (Step, bool) {
	var items []WithdrawItem
	banked := func(id int) bool {
		n, known := s.BankedID(id)
		return known && n > 0 && s.HeldID(id) == 0
	}
	if banked(ObjWitchDoorKey) {
		items = append(items, WithdrawItem{Name: "Door key", ID: ObjWitchDoorKey, Qty: 1})
	}
	if wantShed && banked(ObjWitchShedKey) {
		items = append(items, WithdrawItem{Name: "Key", ID: ObjWitchShedKey, Qty: 1})
	}
	if stage == WitchMagnet && banked(ObjWitchMagnet) {
		items = append(items, WithdrawItem{Name: "Magnet", ID: ObjWitchMagnet, Qty: 1})
	}
	if len(items) == 0 {
		return Step{}, false
	}
	return Step{Kind: StepWithdraw, Items: items, Bank: FaladorWestBank, BankSet: true, KeepIDs: witchRoomKeep,
		Reason: "quest items the bank holds"}, true
}

// witchRoomStep banks what the quest does not need when the pack has fewer than witchMinFree slots
// free and holds something it could bank; with nothing to bank it says nothing and the pickup fails
// with its own reason.
func witchRoomStep(s Snapshot) (Step, bool) {
	if s.FreeSlots >= witchMinFree {
		return Step{}, false
	}
	keep := map[int]bool{}
	for _, id := range witchRoomKeep {
		keep[id] = true
	}
	for _, id := range witchTools {
		keep[id] = true
	}
	spare := false
	for id := range s.InvIDs {
		if !keep[id] {
			spare = true
			break
		}
	}
	if !spare {
		return Step{}, false
	}
	return Step{Kind: StepDeposit, KeepIDs: witchRoomKeep, Bank: FaladorWestBank, BankSet: true,
		Reason: "room in the pack for the quest items"}, true
}

// DecideWitchsHouse is the whole quest.
func DecideWitchsHouse(s Snapshot) Step {
	switch s.Journal {
	case StatusComplete:
		return Step{Kind: StepDone}
	case StatusUnknown:
		return Step{Kind: StepWait, Reason: "the quest tab has not been coloured yet"}
	case StatusNotStarted:
		return Step{Kind: StepTalk, Stop: BallBoyStop}
	}
	if !s.StageKnown {
		return Step{Kind: StepWait, Reason: "the journal stage has not been read"}
	}
	held := func(id int) bool { return s.HeldID(id) > 0 }
	here := s.TileKnown
	keep := append(append([]int(nil), witchTools...), witchFoods...)
	stage := s.Stage
	if stage >= WitchDefeated {
		if held(ObjWitchBall) {
			if here && (InWitchGarden(s.Tile) || InWitchShed(s.Tile)) {
				return Step{Kind: StepCustom, Custom: CustomWitchOut, Reason: "back through the garden with the ball"}
			}
			return Step{Kind: StepTalk, Stop: BallBoyStop}
		}
		if !held(ObjWitchDoorKey) && !(here && (InWitchGarden(s.Tile) || InWitchShed(s.Tile))) {
			if st, ok := witchBankStep(s, stage, false); ok {
				return st
			}
			return Step{Kind: StepCustom, Custom: CustomWitchPot, Reason: "the door key, for the way back in"}
		}
		return Step{Kind: StepCustom, Custom: CustomWitchBall, Reason: "the ball from the shed"}
	}
	// The kit once per quest (the Ran/Failed marks reset on a death).
	kitKey := "provision:" + KitWitch
	if !s.Ran[kitKey] && !s.Failed[kitKey] && !(here && (InWitchGarden(s.Tile) || InWitchShed(s.Tile))) {
		cheese := 0
		if stage <= WitchMagnet {
			cheese = 2
		}
		return Step{Kind: StepProvision, KitName: KitWitch, Kit: witchKit(s, cheese), KeepIDs: keep,
			Bank: FaladorWestBank, BankSet: true, Reason: "gloves, cheese, runes and food"}
	}
	inYard := here && (InWitchGarden(s.Tile) || InWitchShed(s.Tile))
	// The shed is entered only with the runes and the food to finish it (witchshed.go). Judged before the
	// garden walk and before the shed key is drawn, so an account that is not ready does neither.
	fight := WitchShedVerdict{OK: true}
	if stage == WitchUnlocked || stage == WitchDiary {
		fight = WitchShedReady(s)
		if !fight.OK && !inYard {
			// Live 6 to 15 Oct 2026: five deaths in the shed, every one with a failed kit and the quest on
			// regardless. Nothing is fetched for the garden (the plant's key, the shed key, the walk past the
			// witch, whose catch costs the stage back to 1 before the diary is read) until the pack can finish
			// the shed.
			return Step{Kind: StepWait, Reason: "not going for the shed: " + fight.Why}
		}
	}
	if !inYard {
		// What the bank already holds is drawn, not looked for again: the engine's cupboard and fountain
		// answer "nothing" to an account that owns the item ANYWHERE (obj_gettotal counts the bank).
		if st, ok := witchBankStep(s, stage, stage == WitchUnlocked || stage == WitchDiary); ok {
			return st
		}
		if st, ok := witchRoomStep(s); ok {
			return st
		}
	}
	if !held(ObjWitchDoorKey) && !inYard {
		return Step{Kind: StepCustom, Custom: CustomWitchPot, Reason: "the door key under the potted plant"}
	}
	switch stage {
	case WitchStarted, WitchMagnet:
		if s.WornIDs[ObjLeatherGlove] == 0 {
			if held(ObjLeatherGlove) {
				return Step{Kind: StepEquip, ItemID: ObjLeatherGlove, Item: "Leather gloves"}
			}
			// A kit that failed before the gloves were bought leaves none to withdraw: the
			// questproof EMPTY lab run witch-empty-5 looped on "the bank holds no Leather gloves".
			if n, known := s.BankedID(ObjLeatherGlove); known && n == 0 {
				return ShopItemStep(s, "witch-gloves", ObjLeatherGlove, "Leather gloves", 1, ThessaliaShop, 6, keep)
			}
			return Step{Kind: StepWithdraw, Items: []WithdrawItem{{Name: "Leather gloves", ID: ObjLeatherGlove, Qty: 1}},
				Bank: FaladorWestBank, BankSet: true, KeepIDs: keep}
		}
		if stage == WitchStarted {
			// The cupboard gives a magnet only when none is owned anywhere, the bank included, and
			// a magnet from before stage 2 is "not the right magnet".
			if held(ObjWitchMagnet) {
				return Step{Kind: StepCustom, Custom: CustomWitchDropMag, Reason: "a magnet the cupboard will not replace"}
			}
			if n, _ := s.BankedID(ObjWitchMagnet); n > 0 {
				return Step{Kind: StepWithdraw, Items: []WithdrawItem{{Name: "Magnet", ID: ObjWitchMagnet, Qty: 1}},
					Bank: FaladorWestBank, BankSet: true, KeepIDs: keep, Reason: "a banked magnet, to drop"}
			}
			return Step{Kind: StepCustom, Custom: CustomWitchMagnet, Reason: "the magnet in the cellar cupboard"}
		}
		if !held(ObjWitchMagnet) {
			return Step{Kind: StepCustom, Custom: CustomWitchMagnet, Reason: "the magnet in the cellar cupboard"}
		}
		if !held(ObjCheese) {
			return ShopItemStep(s, "witch-cheese", ObjCheese, "Cheese", 2, WydinShop, 4, witchTools)
		}
		return Step{Kind: StepCustom, Custom: CustomWitchMouse, Reason: "the mouse, the cheese and the magnet"}
	case WitchUnlocked, WitchDiary:
		if held(ObjWitchShedKey) {
			return Step{Kind: StepCustom, Custom: CustomWitchShed, Reason: "the shed and its four forms"}
		}
		// Stages 3 and 5 look alike; reading is harmless at 5, so read once per attempt.
		if !s.Ran[CustomWitchDiary] && !s.Failed[CustomWitchDiary] && !(here && InWitchGarden(s.Tile)) {
			return Step{Kind: StepCustom, Custom: CustomWitchDiary, Reason: "the diary, so a catch cannot relock the back door"}
		}
		return Step{Kind: StepCustom, Custom: CustomWitchFountain, Reason: "the shed key in the fountain"}
	}
	return Step{Kind: StepWait, Reason: "witch's house: unrecognised stage"}
}

// WitchsHouseRecord is the quest's identity (no server gates).
var WitchsHouseRecord = Record{ID: WitchsHouseID, Name: WitchsHouseName, QuestPoints: 4}

// WitchsHouse is the engine module.
var WitchsHouse = Module{
	Record: WitchsHouseRecord, Bank: FaladorWestBank, BankSet: true, Anchor: BallBoyAnchor,
	ToolIDs:   witchTools,
	ReadStage: ParseWitchsHouseJournal,
	Sustain:   Sustain{FoodIDs: witchFoods, EatAt: 12},
	// Two deaths, and one in the shed skips the quest for a day: one account died four times (cap 3) and
	// others died there once, each with the same pack they would come back with.
	MaxDeaths: 2,
	StepDeathCaps: map[string]StepDeathCap{
		CustomWitchShed: {Max: WitchShedDeathCap, Window: WitchShedDeathWindow, Skip: WitchShedDeathSkip},
	},
	Decide: DecideWitchsHouse,
}

// ---------------------------------------------------------------------------
// The garden and the witch
// ---------------------------------------------------------------------------

// The garden is one ring corridor (the study's flood over the pack): down x=2901 from the back
// door, east along z=3460, up x=2933 past the shed door, west along z=3466, and into the
// fountain yard. The witch patrols the unreachable lane z=3463 between x 2904 and 2930 and
// catches a player on an exposed tile when her x is inside that tile's window.

// GardenGap is a run of exposed corridor tiles and the witch x window that catches them.
type GardenGap struct {
	Z, X0, X1 int
	W0, W1    int
}

// GardenGaps are the study's exposed tiles (witch.py), with their catch windows.
var GardenGaps = []GardenGap{
	{Z: 3460, X0: 2904, X1: 2906, W0: 2904, W1: 2907},
	{Z: 3460, X0: 2910, X1: 2914, W0: 2909, W1: 2915},
	{Z: 3460, X0: 2918, X1: 2922, W0: 2917, W1: 2923},
	{Z: 3460, X0: 2926, X1: 2928, W0: 2925, W1: 2929},
	{Z: 3466, X0: 2915, X1: 2918, W0: 2914, W1: 2919},
	{Z: 3466, X0: 2922, X1: 2925, W0: 2921, W1: 2926},
	{Z: 3466, X0: 2929, X1: 2931, W0: 2928, W1: 2930},
}

// Witch patrol ends on z=3463.
const (
	WitchPatrolWest = 2904
	WitchPatrolEast = 2930
)

// GardenRing is the corridor in walking order from the back door's garden side to the fountain.
func GardenRing() []tile.Tile {
	var ring []tile.Tile
	for z := 3465; z >= 3460; z-- {
		ring = append(ring, tile.Tile{X: 2901, Z: z})
	}
	for x := 2902; x <= 2933; x++ {
		ring = append(ring, tile.Tile{X: x, Z: 3460})
	}
	for z := 3461; z <= 3466; z++ {
		ring = append(ring, tile.Tile{X: 2933, Z: z})
	}
	for x := 2932; x >= 2912; x-- {
		ring = append(ring, tile.Tile{X: x, Z: 3466})
	}
	for z := 3467; z <= 3470; z++ {
		ring = append(ring, tile.Tile{X: 2912, Z: z})
	}
	return append(ring, WitchFountStand)
}

// RingIndex is t's position on the ring, or the nearest ring tile's when t is off it.
func RingIndex(t tile.Tile) int {
	best, bestD := 0, -1
	for i, r := range GardenRing() {
		d := tile.Chebyshev(r, t)
		if bestD < 0 || d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

// gapOf is the gap an on-ring tile is exposed in, or -1.
func gapOf(t tile.Tile) int {
	for i, g := range GardenGaps {
		if t.Z == g.Z && t.X >= g.X0 && t.X <= g.X1 && t.Level == 0 {
			return i
		}
	}
	return -1
}

// GardenHop is one leg of a garden walk: walk (safely) to Stand, then, when Gap >= 0, cross the
// gap to Cross once the witch is clear of it.
type GardenHop struct {
	Stand, Cross tile.Tile
	Gap          int
	// Tiles is how many tiles the crossing walks.
	Tiles int
}

// PlanGardenWalk splits the ring walk from one tile to another into safe legs and crossings.
func PlanGardenWalk(from, to tile.Tile) []GardenHop {
	ring := GardenRing()
	i, j := RingIndex(from), RingIndex(to)
	step := 1
	if j < i {
		step = -1
	}
	var hops []GardenHop
	for k := i; k != j; {
		// Walk along safe tiles until the next is exposed, or the end.
		n := k
		for n != j && gapOf(ring[n+step]) < 0 {
			n += step
		}
		if n == j {
			hops = append(hops, GardenHop{Stand: ring[j], Gap: -1})
			break
		}
		g := gapOf(ring[n+step])
		m := n + step
		for m != j && gapOf(ring[m]) == g {
			m += step
		}
		hops = append(hops, GardenHop{Stand: ring[n], Cross: ring[m], Gap: g, Tiles: abs(m - n)})
		k = m
		if m == j && gapOf(ring[m]) == g {
			break
		}
	}
	return hops
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// WitchClear reports whether crossing gap g, taking about ticks ticks, is safe with the witch
// at x moving dx (-1 west, +1 east, 0 unknown or pausing). She moves one tile a tick; a witch
// moving away is credited only with the ground to her patrol end and back.
func WitchClear(g GardenGap, x, dx, ticks int) bool {
	if x >= g.W0 && x <= g.W1 {
		return false
	}
	dist := g.W0 - x
	if x > g.W1 {
		dist = x - g.W1
	}
	away := (x < g.W0 && dx < 0) || (x > g.W1 && dx > 0)
	if away {
		end := WitchPatrolWest
		if dx > 0 {
			end = WitchPatrolEast
		}
		dist += 2 * abs(x-end)
	}
	return dist > ticks+2
}
