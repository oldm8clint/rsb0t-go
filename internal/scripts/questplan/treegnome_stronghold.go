package questplan

import (
	"fmt"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// The Khazard stronghold of Tree Gnome Village (stage 5, and stage 6 when the orb is lost), the pure half.
//
// WHAT HAPPENED (live 16 Oct 2026, 03:12-03:15 and 03:38-03:41, two accounts of 53 hitpoints, combat 62, an empty
// pack and 25 free slots; the engine's log, one line each):
//
//	tree-orb (over the wall, up the ladder, the chest) (journal inProgress, stage 5, hp 53/53, 25 free slots)
//	run staller: standing aside at 33/53 hitpoints so a bite can land
//	[guardian] displaced: 2504,3252,0 -> 2504,3252,1 (0 tiles in 600ms) -- an ordinary level change
//	Khazard commander is attacking while "tree-orb ..." fails: fighting back
//	fight: losing to Khazard commander (hitpoints 14/53 are at the eat line (14) with 0 food left)
//	walk to 2616,3332,0: baked walk failed: no path: unreachable   (and 2809,3440, 2474,3436, ...)
//
// Three things were wrong, and each is mended where it lives:
//
//  1. NO FOOD. The orb step had no kit at all. Climbing over the crumbled wall (quest_tree.rs2 khazzacklowwall) runs
//     ~npc_retaliate on the commander standing 1 tile from the landing, so the account arrived in the hall already
//     being hit (33/53 before the ladder), and the second commander upstairs finished it. Each commander is 22
//     hitpoints with attack 50, defence 50, strength 45 (FoeKhazardCommander): 20 to 25 hitpoints of damage each at
//     Defence 50, nothing a pack of food does not absorb, everything an empty pack does not. The step now provisions
//     food first (treeOrbKit), judges the pack against both commanders (TreeOrbReady) and parks for TreeOrbParkFor
//     when it cannot take them, and the step itself fights the commander at the landing and the one upstairs through
//     the shared fight step (food, the eat line from the foe, the race) instead of walking past them.
//  2. NO WAY DOWN. The walk graph had the tower's ladder but neither of its doors (internal/webwalk/khazard.go), so
//     the 37 tiles upstairs and the 28 of the ladder room were a sealed pocket for every walk: the retreat to the
//     bank, every later quest's first walk. The doors are in the graph now, and the stronghold is a questrunner
//     pocket too (questmods/treegnome.go): a walk that starts inside it leaves by the stronghold's own exit first,
//     whichever quest asked.
//  3. NO BRAKE. A step that kills an account twice in a day is not tried again that day (StepDeathCaps), as the
//     warlord's is.
//
// THE STRONGHOLD, FROM THE MAP (maps/m39_50.jm2) AND THE TS BOT (defs/treegnome/areas.ts), the same row tables, checked
// against the real collision pack tile by tile in treegnome_stronghold_pack_test.go:
//
//   - the HALL behind the crumbled wall (loc 2185 at 2509,3253, climbed over from the south, once the ballista has
//     fired, landing on 2509,3255), 49 tiles, with a commander on 2509,3254;
//   - the LADDER ROOM west of it, 28 tiles: the ladder up (1747 at 2503,3252) and the front door (2184 at 2502,3250,
//     opens from inside only), joined to the hall by the diagonal inner door (1530 at 2505,3256);
//   - the CHEST FLOOR above, 37 tiles, with the chest (2183/2182 at 2506,3259) and a commander on 2503,3255.
//
// All three are one Pocket for the quest runner: no walk goes from inside to outside without the way out above.

// The stronghold's tiles. The loc tiles that already have names (KhazardWallSouth, KhazardLadderTile, KhazardChestTile,
// KhazardDoorTile) are in treegnome.go.
var (
	// KhazardHallLanding is where the crumbled wall sets an account down.
	KhazardHallLanding = tile.Tile{X: 2509, Z: 3255}
	// KhazardInnerDoorStand is the hall's side of the inner door (TS INNER_DOOR_STAND).
	KhazardInnerDoorStand = tile.Tile{X: 2506, Z: 3256}
	// KhazardInnerDoorTile is the inner door's own tile (loc 1530).
	KhazardInnerDoorTile = tile.Tile{X: 2505, Z: 3256}
	// KhazardLadderStand is the ladder room's stand for the ladder up (TS LADDER_STAND).
	KhazardLadderStand = tile.Tile{X: 2503, Z: 3253}
	// KhazardLadderTopStand is the same stand on the chest floor, for the ladder down.
	KhazardLadderTopStand = tile.Tile{X: 2503, Z: 3253, Level: 1}
	// KhazardFrontDoorStand is the inside of the front door (TS FRONT_DOOR_STAND); the door sets the account down on
	// KhazardDoorTile.
	KhazardFrontDoorStand = tile.Tile{X: 2502, Z: 3251}
	// KhazardChestStand is the chest floor's stand for the chest (TS CHEST_STAND).
	KhazardChestStand = tile.Tile{X: 2506, Z: 3258, Level: 1}
)

// span is an inclusive x range of one row.
type span [2]int

// khazardHallRows and khazardLadderRoomRows are the two rooms by row (TS HALL_ROWS and LADDER_ROOM_ROWS); the rooms
// overlap in x and z bounds, so each is its own table. A row can name tiles that are not walkable (the ladder, a chair):
// the pack test pins that every WALKABLE tile of a row is in that room's flood and every tile of the flood is in its rows.
var (
	khazardHallRows = map[int]span{
		3254: {2508, 2511}, 3255: {2507, 2512}, 3256: {2506, 2512}, 3257: {2500, 2511}, 3258: {2502, 2512}, 3259: {2503, 2511},
	}
	khazardLadderRoomRows = map[int]span{
		3251: {2501, 2503}, 3252: {2500, 2504}, 3253: {2500, 2505}, 3254: {2500, 2506}, 3255: {2500, 2506}, 3256: {2500, 2504},
	}
)

func inRows(rows map[int]span, t tile.Tile) bool {
	r, ok := rows[t.Z]
	return t.Level == 0 && ok && t.X >= r[0] && t.X <= r[1]
}

// InKhazardHall is the hall behind the crumbled wall: the only way on is the inner door.
func InKhazardHall(t tile.Tile) bool { return inRows(khazardHallRows, t) }

// InKhazardLadderRoom is the west room, with the ladder up and the front door out.
func InKhazardLadderRoom(t tile.Tile) bool { return inRows(khazardLadderRoomRows, t) }

// InKhazardChestFloor is the floor above, where the chest is.
func InKhazardChestFloor(t tile.Tile) bool {
	return t.Level == 1 && t.X >= 2499 && t.X <= 2507 && t.Z >= 3250 && t.Z <= 3260
}

// InKhazardStronghold is any of the three: the places no walk leaves by itself.
func InKhazardStronghold(t tile.Tile) bool {
	return InKhazardHall(t) || InKhazardLadderRoom(t) || InKhazardChestFloor(t)
}

// Step death caps for the orb step: a step that kills an account twice in a day is not tried again that day.
const (
	TreeOrbDeathCap    = 2
	TreeOrbDeathWindow = 24 * time.Hour
	TreeOrbDeathSkip   = 24 * time.Hour
)

// TreeOrbParkFor is how long an account whose pack cannot take the Khazard commanders skips the quest: food
// from the bank or the baker is what mends it, and neither changes inside the hour.
const TreeOrbParkFor = 3 * time.Hour

// treeOrbGoodFood is how many sharks and lobsters (owned: pack, worn and the bank once it is read) are enough that the
// kit does not go shopping: the baker's cakes and bread are the fallback for an account whose bank has none, not a
// purchase made beside twenty sharks (a shop line is planned however full the bank is, and a coinless account's
// plan is BLOCKED on a line that was never needed).
const treeOrbGoodFood = 6

// treeOrbKit is the food the orb step carries into the stronghold: sharks and lobsters from the bank or the mule
// (never bought), and, when the account owns fewer than treeOrbGoodFood of them, what the East Ardougne baker sells
// (cakes, bread). Every line is optional: TreeOrbReady judges what the pack really holds. Two commanders cost 40 to
// 50 hitpoints between them at Defence 50 with a third of the bites spare, which is a few sharks or the baker's three
// cakes and a few loaves.
func treeOrbKit(s Snapshot) []KitLine {
	kit := []KitLine{
		{ID: 385, Name: "Shark", Qty: 8, Optional: true},
		{ID: 379, Name: "Lobster", Qty: 6, Optional: true},
	}
	if s.Owned(385)+s.Owned(379) < treeOrbGoodFood {
		kit = append(kit,
			KitLine{ID: 1891, Name: "Cake", Qty: 3, Optional: true, Sources: []Source{{Kind: SourceShop, Shop: ArdougneBaker, Price: 50}}},
			KitLine{ID: 2309, Name: "Bread", Qty: 8, Optional: true, Sources: []Source{{Kind: SourceShop, Shop: ArdougneBaker, Price: 12}}})
	}
	return kit
}

// TreeOrbVerdict is TreeOrbReady's answer.
type TreeOrbVerdict struct {
	OK  bool
	Why string
	// Foes is how many commanders were priced, Odds the fights played out.
	Foes int
	Odds FightOdds
}

// treeOrbFoesLeft is the commanders still between the account and the orb: both until the ladder room, the
// upstairs one from there on (the hall's was fought at the landing, or is not there).
func treeOrbFoesLeft(s Snapshot) []MeleeFoe {
	if s.TileKnown && (InKhazardLadderRoom(s.Tile) || InKhazardChestFloor(s.Tile)) {
		return []MeleeFoe{FoeKhazardCommander}
	}
	return []MeleeFoe{FoeKhazardCommander, FoeKhazardCommander}
}

// TreeOrbReady judges whether the pack can take the Khazard commanders: each is fought by the account's own swing
// (MeleeChain: the fight step melees) from the level a fight starts at (FightStartHP), eating by the foe's eat line,
// and the account must win with a tenth of its bites left for the way out and never fall under the foe's hardest hit.
// The reason names the account's levels and the food it holds, for the log.
func TreeOrbReady(s Snapshot) TreeOrbVerdict {
	foes := treeOrbFoesLeft(s)
	odds := MeleeChain(s, foes, treeSustain)
	v := TreeOrbVerdict{OK: odds.OK, Why: odds.Why, Foes: len(foes), Odds: odds}
	// An empty pack is the 16 Oct 2026 deaths; say so plainly, not as a bite count of 0 of 0.
	if len(FoodBites(s.InvIDs, treeFoods)) == 0 {
		v.OK, v.Why = false, fmt.Sprintf("no food in the pack for the Khazard commanders (%d hitpoints each, hits up to %d)",
			FoeKhazardCommander.Hitpoints, FoeKhazardCommander.MaxHit())
	}
	if !v.OK {
		v.Why = fmt.Sprintf("%s (%d Khazard commander(s) ahead; Attack %d, Strength %d, Defence %d, %d/%d hitpoints, %d bites of food)",
			v.Why, len(foes), s.Level("attack"), s.Level("strength"), s.Level("defence"), s.HP, s.MaxHP, len(FoodBites(s.InvIDs, treeFoods)))
	}
	return v
}

// treeOrbStep is the orb step behind its gates: the food first (once a run, outside the stronghold), then the
// odds. An account that fails them outside the stronghold parks for TreeOrbParkFor; one that fails them inside
// walks out of it (the pack it carries in, food spent, is what it has to leave with).
func treeOrbStep(s Snapshot, why string) Step {
	inside := s.TileKnown && InKhazardStronghold(s.Tile)
	if !inside && !s.Ran["provision:"+KitTreeOrb] && !s.Failed["provision:"+KitTreeOrb] {
		// The kit buys the baker's food only for an account whose bank has none: read the bank first.
		if !s.BankKnown {
			return Step{Kind: StepScanBank, Bank: EastArdougneNorth, BankSet: true}
		}
		return Step{Kind: StepProvision, KitName: KitTreeOrb, Kit: treeOrbKit(s), KeepIDs: append(append([]int(nil), treeTools...), treeFoods...),
			Bank: EastArdougneNorth, BankSet: true, Reason: "food for the two Khazard commanders (22 HP, max hit 5) in the tower"}
	}
	if v := TreeOrbReady(s); !v.OK {
		if inside {
			return Step{Kind: StepCustom, Custom: CustomTreeLeave, Reason: "the pack cannot take the Khazard commanders: " + v.Why}
		}
		return Step{Kind: StepWait, ParkFor: TreeOrbParkFor, Reason: "not going into the Khazard stronghold with this pack: " + v.Why}
	}
	return Step{Kind: StepCustom, Custom: CustomTreeOrb, Reason: why}
}
