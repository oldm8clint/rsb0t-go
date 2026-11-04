package wcfletchplan

// Where the yew rung chops: the church camp the ladder has always sent it to, the yews on the
// Catherby side of Seers, and the board that seats the yew cohort over them.
//
// # The loss this exists for (live build 73ff4228, 4-5 October 2026)
//
// All 23 ProgressiveWCFletch accounts on the yew rung (Woodcutting 74-79, Fletching 66-70, rune
// axes: the "rung 5/6: Yew" line each logged at the 23:06 relaunch) stood at ONE camp, the
// ladder's own "three yews behind the church" (woodcutplan's Yew tier, anchor 2709,3462, leash 8).
// What that gave, from the run's log:
//
//   - 1,047 yew logs cut an hour (530, 464 and 576 "wcfletch: cut N yew logs" per half hour from
//     00:00 to 01:30), 45 an account, against 180 for an account that has a tree to itself;
//   - the owner's 15-minute sample at 00:20: 7,296 "no yew in sight -- waiting for one to grow
//     back" lines, about 580 "every yew within reach is excluded", 331 cut batches and 1,127 bows
//     an hour, 49 an account (37,723 and 1,878 of those two lines over the run's two hours and
//     eighteen minutes);
//   - the shape of one account's evening: a ring walk between the camp's four stand tiles
//     (2704,3464, 2713,3459, 2704,3459, 2705,3463), a log about once a minute, cut one at a time as
//     it arrives, and nothing else.
//
// The a user request: "accounts progressivewcfletch should also consider how many other
// players are also chopping the same tree and if they should hop or find another location to chop
// at." The first half of that is campcrowd.go. The second half is this file.
//
// THE CAMP IS SUPPLY-BOUND, and that is content. A yew is a 1-in-8 deplete on a 200-tick stump
// (trees.dbrow [yew_tree_table] respawnrate 200; woodcut.rs2 get_logs rolls the deplete per log),
// so a tree gives at most 8 logs per 112 seconds at the board's 270 players, 257 an hour, and
// three trees 772 an hour: the output of 4.3 accounts at 180 logs an hour, however many wait.
// (The same trees gave 36% MORE than that live. A busier world respawns faster, by
// scale_by_playercount's (4000-players)/4000, and a world of about 1,250 would give the measured
// logs; the board keeps the 270 the maple board uses, which makes its supply a floor. That moves
// every camp's supply together, so which camp the board picks hardly depends on it.)
//
// Each yew is 3x3 with one or two stand tiles (the loc index and pack: 2705,3459 has one,
// 2705,3464 two), but stand tiles are not the limit: players do not block players, and a stand
// tile another account is on is walked onto anyway (woodcutrunner.freeFirst). The limit is trees.
//
// # Every option, priced before choosing
//
// With CohortXP (chopmodel.go) over the live cohort's own 23 level pairs, rune axes, a world of
// 270, each arrangement being this board's own seating restricted to those camps
// (TestYewOptionsArePriced recomputes every row and fails if one moves by 3%):
//
//	arrangement                                      cohort XP/h   per account    logs/h
//	all 23 at the church yews (the live behaviour)       187k          8.1k          769
//	church + the lone road yew                           245k         10.7k        1,009
//	Catherby yews + the road yew (church left)           422k         18.3k        1,736
//	church + Catherby yews                               486k         21.1k        2,000
//	every camp (THE DEFAULT)                             510k         22.2k        2,098
//
// The default seats Catherby yews 13, church 8, road yew 2 (TestBoardSpreadsTheLiveYewCohort). The
// status-quo line is 27% UNDER the measurement (769 logs an hour against 1,047,
// TestYewStatusQuoAgainstTheMeasurement), so every other line is a floor too: nothing in this table
// has been measured on the new arrangement, in the lab or live. What each camp is made of:
//
//   - THE CHURCH CAMP is the ladder's own, Home, unchanged: three yews (2705,3459, 2705,3464,
//     2714,3459), 37 steps from the bank. One account alone there earns 38.9k xp/h.
//   - THE CATHERBY YEWS are seven yews 54-64 tiles south-east of the bank, below the road to
//     Catherby (2754,3433, 2755,3430, 2757,3433, 2759,3427, 2760,3431, 2765,3427, 2770,3437),
//     2.3 times the church camp's supply (1,802 logs an hour) for a walk of 79 steps instead of
//     37, which costs an account 9% of its rate: one account alone earns 38.0k xp/h, about the
//     same, and the camp feeds eleven. They are ONE camp: anchor 2762,3430, leash 8, the smallest
//     leash that counts all seven with a stand tile of each inside it (four anchors manage that,
//     and two of them keep the lone yew below out of the camp's 2*leash+2 reach), so every tree's
//     supply belongs to one camp and an account chopping any of them is "at the camp"
//     (woodcutplan.AtCamp). Split into a west block of five and an east pair it would have drawn
//     two camps inside each other's Chop reach (leash+2: the east pair's 2765,3427 is five tiles
//     from a west-block stand tile at 2760,3430).
//   - THE ROAD YEW is one yew at 2767,3452, eleven tiles south of the rung-1 "Catherby road" patch
//     (anchor 2767,3451, a stand tile, leash 2), 58 steps out: 257 logs an hour, 1.5 accounts'
//     worth, 30.4k xp/h to an account alone. It is worth a seat only when the others are full, and
//     the board gives it two (the default's +5% over church + Catherby yews).
//   - YEWS FURTHER OUT are not camps. Of the 31 yews within 400 tiles of the bank 11 are inside
//     SeersRadius (the three camps above) and 14 have a door-free route from it; the next three
//     are 157 and 160 steps out (2736,3334 and 2731,3331, among bears) and 369 (2496,3490): all
//     past SeersRadius 80, where woodcutplan.DecideWoodPhase stops treating an account as at Seers
//     and sends it down the fare road. TestNoOtherYewIsSafeAndInReach pins that no yew inside the
//     radius is left out.
//
// # What the owner decided, and what this does not change
//
// The ladder is the owner's ("stay at yews until we can cut magic shortbows"), and it stands:
// TierIndex still puts these accounts on the yew rung, a seat at a yew camp is that rung exactly
// (idx comes back the rung it went in), and the moment an account can cut magic shortbows it
// leaves the board. A yew camp changes WHERE an account chops and nothing it chops, cuts or banks,
// so there is no overflow: Camp.Overflow is false for every camp here. The setting spreadYew
// (default ON) turns the whole board off; off is the one-camp behaviour before this file.
//
// WHAT IT DOES NOT DO. It does not hop worlds (campcrowd.go says what it does instead). And it
// does not overflow to another rung. The maple board's own cohort is priced at 27k xp/h an
// account at its default seating, against this board's 22k, so yew accounts sent to the willows
// (a willow longbow, 109 xp a log, for a yew bow's 242) may well earn more in the model; that is
// the owner's ladder to change, and it is not priced here.
//
// # Every camp below was chosen, not guessed
//
// Each is a cluster of Yew locs found by clustering the loc index around the Seers bank, held to
// the maple camps' rules (maplecamps.go) and one more:
//
//   - every counted tree has a stand tile the collision pack routes to from the Seers bank with
//     no door on the way;
//   - every counted stand tile keeps 3 tiles outside every hunting NPC's reach (the giant bats at
//     2752,3409, 2753,3396 and 2759,3401 are the nearest to the Catherby yews: their south-west
//     stand tiles are 7 outside), and the route from the bank and every walk from the anchor to a
//     tree the account could be handed keep 2;
//   - no tree the account could be handed is a DETOUR (at most twice the straight line plus 30);
//   - no stand tile, route tile or in-camp walk tile lies in a webwalk danger zone that applies to
//     a combat-3 account;
//   - and, for these camps and not the maple ones, every counted tree has a stand tile within the
//     camp's Leash of its Anchor. A tree whose nearest stand tile is further out leaves an account
//     chopping it "not at the camp", so the next pass walks it back to the anchor and out to the
//     tree again each time a tree falls (three of the maple board's camps do this, by one tile
//     each; the shared check logs it for them).
//
// TestYewCampsAgreeWithTheLocIndex re-derives every Trees and Steps figure and every one of those
// rules from the real loc index and pack, the same way TestMapleCampsAgreeWithTheLocIndex does, so
// a content or pack change fails a test rather than quietly sending accounts to a camp that is not
// there.

import (
	"sync"

	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/woodcutplan"
)

// YewRung is the ladder position of the yew tier.
var YewRung = mustRung(YewLogsID)

// YewCamps is every camp the yew rung may be seated at: the ladder's own church camp first, then
// the Catherby side. Every camp is the yew rung's own (no overflow: Camp.Overflow is false for
// all of them), so a seat changes where an account chops and never what.
//
// The distance in each comment is the door-free walk from the Seers bank, Steps.
var YewCamps = []Camp{
	{Name: "Seers church yews", Rung: YewRung, Loc: "Yew", Anchor: Ladder[YewRung].Tier.Anchor, Leash: Ladder[YewRung].Tier.Leash, Trees: 3, Steps: 37, Home: true}, // the ladder's own camp
	{Name: "Catherby yews", Rung: YewRung, Loc: "Yew", Anchor: tile.Tile{X: 2762, Z: 3430}, Leash: 8, Trees: 7, Steps: 79},                                          // 79
	{Name: "Catherby road yew", Rung: YewRung, Loc: "Yew", Anchor: tile.Tile{X: 2767, Z: 3451}, Leash: 2, Trees: 1, Steps: 58},                                      // 58
}

// UsesYewCamps reports whether an account on rung idx is seated on the yew board.
func UsesYewCamps(idx int) bool { return idx == YewRung }

// ReferenceYewChopper is the account the yew board prices camps for: the middle of the live yew
// cohort (Woodcutting 74-79, Fletching 66-70 at the 2026-10-04 23:06 relaunch) with the rune axe.
var ReferenceYewChopper = Chopper{Wood: 75, Fletch: 67, Axe: woodcutplan.RuneAxe}

// One yew board per world, for the reason SharedPatchBoardFor gives.
var (
	yewBoardsMu sync.Mutex
	yewBoards   = map[int]*CampBoard{}
)

// SharedYewBoardFor is the process-wide yew board for one world (0 when unknown).
func SharedYewBoardFor(world int) *CampBoard {
	if world < 0 {
		world = 0
	}
	yewBoardsMu.Lock()
	defer yewBoardsMu.Unlock()
	b := yewBoards[world]
	if b == nil {
		b = NewCampBoardFor(YewCamps, ReferenceYewChopper, BoardPlayers)
		yewBoards[world] = b
	}
	return b
}
