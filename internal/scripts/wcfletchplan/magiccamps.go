package wcfletchplan

// Where the magic rung chops: the ladder's own three magic trees west of the Seers bank, the four in
// the hedged garden behind the Sorcerers' Tower, the board that seats the magic cohort over them, and
// the registry that deals that cohort over the fleet's worlds.
//
// # The loss this exists for (live build 8c77500e, 12 and 13 October 2026)
//
// On 12 October twelve ProgressiveWCFletch accounts (Woodcutting 89-92, Fletching 81-83, rune axes) were
// switched to the target "Magic", four to a world on worlds 1-3 by their homeWorld settings, and every one
// went to the ladder's one magic camp: woodcutplan's Magic tier, three trees (2690,3427, 2691,3424,
// 2696,3423) round anchor 2693,3425. From their 15-minute idle reports, 20:25 to 20:58: 80, 86 and 109
// logs an account-hour on worlds 1, 2 and 3, with 33, 25 and 18% of the time spent waiting for a tree. The
// owner: "find other magic trees closer to a bank to spread accounts out further". On 13 October he moved
// all 23 accounts that can chop and fletch magic logs to the target "Magic" with homeWorld 1, 2 and 3 (8, 8
// and 7), so each world's three trees now carry about eight accounts.
//
// THE CAMP IS SUPPLY-BOUND. A magic tree is a 1-in-8 deplete on a 400-tick stump (trees.dbrow
// [magic_tree_table] respawnrate 400), so three trees give about 386 logs an hour at the board's 270
// players, and one account with a rune axe at Woodcutting 90 takes about 100 an hour when it never
// waits (a swing of 20 in 256 every 4 ticks, a pack of 26 carried 74 steps to the bank and back): the
// camp feeds three, and a world's eight wait.
//
// # Every magic tree, and why two camps
//
// Content 289 has thirteen magic trees (loc 1306; the loc index and the raw maps agree), all in members'
// areas, none in or near the Wilderness. Each was scanned against every bank on the collision pack and
// webwalk's graph on 12 October 2026 (offline; nothing was walked live):
//
//   - THE SEERS TREES, the ladder's own camp: three trees, 74 steps from the Seers bank. Kept, Home.
//   - THE SORCERERS' TOWER GARDEN: four trees (2698,3396, 2698,3398, 2705,3396, 2705,3398) inside the
//     hedge behind the tower, entered by the gap at 2701,3395, 28 tiles south of the Seers camp and 114
//     steps from the Seers bank with no door on the way, every stand tile and the whole walk well clear
//     of every hunting NPC (16 tiles outside the nearest's reach, the giant bats east of the Catherby
//     road). Its nearest SAFE bank is still Seers: Catherby's walk is 119 steps but crosses the giant
//     bats' zone (seers-south-bats), Ardougne north's is 132 (TestEachMagicCampBanksAtItsNearestSafeBank),
//     and the Legends' Guild bankers upstairs, 87 steps away, need the Legends' Quest. So it is not closer
//     to a bank than the Seers camp; it is the one other place the walker can feed from a bank at all.
//   - THE GNOME STRONGHOLD: three single trees (2432,3410, 2490,3414, 2371,3426). The south bank beside
//     them is upstairs and webwalk's graph has no Stronghold staircase, so the walk today is to the north
//     bank, 114 to 144 steps through the tree door and up a ladder. Each is ONE tree (an account alone
//     waits through every respawn, about half its time), 235 to 354 tiles from Seers (past
//     SeersSideRadius, where DecideWoodPhase sends an account down the fare road), behind the Stronghold
//     gate, and the hand-off walk to the mule is about 400 steps past the moss giants south-east of it,
//     which attack anything below combat 85. Not a camp.
//   - TIRANNWN: three trees behind the elf-lands quests, with no route at all. Not a camp.
//
// # Every option, priced before choosing
//
// With CohortXP (chopmodel.go) over one world's four accounts (the cohort of 12 October) and its eight (13
// October) at the cohort's levels, rune axes, a world of 270, each arrangement being this board's own seating
// restricted to those camps (TestMagicOptionsArePriced and TestMagicEightToAWorld recompute every row and
// fail if one moves by 3%):
//
//	arrangement                                          xp/h a world   each    logs/h a world
//	4 accounts, all at the Seers trees (12 Oct live)         82.7k     20.7k        248
//	4 accounts, all in the Sorcerers' Tower garden           89.3k     22.3k        268
//	4 accounts, both camps, two at each                     104.5k     26.1k        314
//	8 accounts, all at the Seers trees (13 Oct live)        112.6k     14.1k        338
//	8 accounts, both camps, four at each (THE DEFAULT)      173.1k     21.6k        520
//
// The status-quo row for four is 32% UNDER the measurement (248 logs an hour a world against 367:
// TestMagicStatusQuoAgainstTheMeasurement), as the yew board's is, so every row is a floor; nothing in the
// table has been measured on the new arrangement, in the lab or live.
//
// Two things about this board differ from the maple and yew boards:
//
//   - Its near tie is MagicNearTie, 0.95, not patchNearTie's 0.85. The two camps price within 2% of each
//     other for a lone account, so at 0.85 nearly every new seat is a "tie" broken toward the camp nearer
//     the account, which from the Seers bank is the Seers trees, and a world's four are seated 3 + 1
//     (about 98k a world); at 0.95 they are seated 2 + 2 (TestMagicNearTieSeatsTwoAndTwo).
//   - The seat's log line says the camp's Capacity (CampPrice.Capacity: supply over one account alone, 3
//     at the Seers trees and 5 in the garden). It refuses no seat: past it every account takes logs from
//     the others, and the board still seats there when nowhere else pays better.
//
// # What the owner decided, and what this does not change
//
// The ladder is the owner's and it stands: TierIndex puts an account on the magic rung, a seat at a
// magic camp is that rung exactly, and nothing it chops, cuts or banks changes (no overflow). Every camp
// names its bank (Camp.Bank; the data test holds it to the nearest safe one) and both are the Seers
// bank, so the bank, fletch, market and hand-off steps are the ones every other rung runs. spreadMagic
// (default ON) turns the board off, which is the one-camp behaviour before this file.
//
// THE WORLDS ARE NOT DEALT. Each account stays on the world its homeWorld setting names (1, 2 and 3, set by
// the owner), and the board is one per world (SharedMagicBoardFor), so a world's accounts are seated over
// that world's camps and no world's seating counts another's. The yew rung is dealt over worlds at run time
// (yewworlds.go); doing the same here would add a second registry of hops to the login path for no gain the
// owner's settings do not already give, and the yew deal's hop loop of 12 October (fixed in 1e34f613) is
// the cost of getting that wrong. A world that turns out too crowded is said in the log (campcrowd.go's
// advice), and moving an account is a homeWorld edit.

import (
	"sync"

	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/woodcutplan"
)

// MagicRung is the ladder position of the magic tier.
var MagicRung = mustRung(MagicLogsID)

// CampBank is the bank a camp carries its loads to: where the runner walks before it opens the
// nearest booth (Stand), and the floor tile in front of the booths that a camp's Steps and the data
// tests' routes are measured from (Floor).
type CampBank struct {
	Name  string
	Stand tile.Tile
	Floor tile.Tile
}

// SeersBank is the Seers' Village bank, every camp's bank unless it names another: the tile every
// woodcutting step walks to (woodcutplan.Seers) and the floor in front of its Use-quickly booths.
var SeersBank = CampBank{Name: "Seers", Stand: woodcutplan.Seers, Floor: tile.Tile{X: 2725, Z: 3491}}

// MagicCamps is every camp the magic rung may be seated at: the ladder's own three trees west of
// the Seers bank first, then the hedged garden behind the Sorcerers' Tower. Both bank at Seers,
// which is the nearest safe bank to each (magiccamps_data_test.go measures every candidate).
//
// The distance in each comment is the door-free walk from the Seers bank, Steps.
var MagicCamps = []Camp{
	{Name: "Seers magic trees", Rung: MagicRung, Loc: "Magic tree", Anchor: Ladder[MagicRung].Tier.Anchor, Leash: Ladder[MagicRung].Tier.Leash, Trees: 3, Steps: 74, Home: true, Bank: SeersBank}, // the ladder's own camp
	{Name: "Sorcerers' Tower garden", Rung: MagicRung, Loc: "Magic tree", Anchor: tile.Tile{X: 2702, Z: 3397}, Leash: 4, Trees: 4, Steps: 114, Bank: SeersBank},                                   // 114
}

// UsesMagicCamps reports whether an account on rung idx is seated on the magic board.
func UsesMagicCamps(idx int) bool { return idx == MagicRung }

// ReferenceMagicChopper is the account the magic board prices camps for: the middle of the live
// ProgressiveWCFletch cohort switched to magic on 12 October 2026 (Woodcutting 89-92, Fletching
// 81-83) with the rune axe.
var ReferenceMagicChopper = Chopper{Wood: 90, Fletch: 82, Axe: woodcutplan.RuneAxe}

// MagicNearTie is the magic board's near tie: two camps whose shares for a new seat are within 5% of
// each other count as equal, and the nearer one wins (the header says why it is not patchNearTie).
const MagicNearTie = 0.95

// One magic board per world, for the reason SharedPatchBoardFor gives.
var (
	magicBoardsMu sync.Mutex
	magicBoards   = map[int]*CampBoard{}
)

// NewMagicBoard returns an empty magic board, priced for ReferenceMagicChopper at BoardPlayers.
func NewMagicBoard() *CampBoard {
	return NewCampBoardFor(MagicCamps, ReferenceMagicChopper, BoardPlayers).withNearTie(MagicNearTie)
}

// SharedMagicBoardFor is the process-wide magic board for one world (0 when unknown).
func SharedMagicBoardFor(world int) *CampBoard {
	if world < 0 {
		world = 0
	}
	magicBoardsMu.Lock()
	defer magicBoardsMu.Unlock()
	b := magicBoards[world]
	if b == nil {
		b = NewMagicBoard()
		magicBoards[world] = b
	}
	return b
}
