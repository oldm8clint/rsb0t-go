package wcfletchplan

// Where the first rung chops: the normal-tree PATCHES around Seers, and the board that spreads
// the cohort across them.
//
// # The regression this exists for (live build 3dfb543a, 2026-09-23)
//
// ProgressiveWCFletch's median combined Woodcutting+Fletching XP/h fell to 1,177 (p10 342,
// n=84) from about 7,320. Nothing broke. The build before latched fletching off for the 37
// accounts that lost the race for one of the world's four knife spawns, and a knifeless account
// climbs on Woodcutting, so those accounts sat at willows. With the latch gone every account
// keeps its knife, TierIndex holds everything below Fletching 20 on rung 1 whatever its
// Woodcutting level, and 55 of 84 accounts (Woodcutting 42-69) were queueing at the ONE Seers
// normal-tree camp: 12 trees inside its leash, 26 inside reach. They cut 27 logs per
// account-hour against 201 for the willow group — 730 XP/h against about 22,000.
//
// THE CAMP IS SUPPLY-BOUND, and that is content, not code. woodcut.rs2's get_logs forces a
// normal tree's deplete chance to certainty and gives it a 120-199 tick stump (capacity.go in
// woodcutplan has the lines), so a normal tree yields ONE log per ~96 seconds at a production
// tick however many accounts stand at it. 55 accounts x 27 logs/h over 26 trees is 57 logs per
// tree-hour, which is that respawn at a busy world's playercount scale — the camp was giving
// everything it had. No choice of which tree to click there can buy more; only more trees can.
//
// # Why more normal trees, rather than a better tree
//
// Every alternative was priced against the same content numbers before this was written:
//
//   - WILLOWS for everyone who can chop them: the Seers willows are 4-6 trees on a 30-tick
//     stump with the 1-in-8 deplete roll, which caps the whole patch at about 8,800 logs/h. The
//     5b1adfb4 baseline ran 81 accounts there and measured 108 logs per account-hour — exactly
//     that cap. Putting the 55 back on willows returns the fleet to that baseline (about 712k
//     XP/h cohort-wide) and pulls the 27 willow fletchers from ~22k down to ~12k each.
//   - MAPLES for Woodcutting 60+ (10 of the 55): 100 xp a log and nearly unused, so worth ~19k
//     each — but it stops their Fletching for good, crowds the one maple fletcher (the owner's
//     own account), and overrides the owner's ladder ("staying at normal trees until we unlock
//     cutting oak logs into shortbows"). Recorded in COLLAB as the owner's call, not built.
//   - More NORMAL trees: arrow shafts stack, so rung 1 never banks (the pack never fills), and a
//     patch 70 tiles from the bank costs ONE walk, not one per load. There are 123 more normal
//     trees inside the Seers radius, safe and reachable, in the eight patches below. At the
//     same 41-57 logs per tree-hour that is 5-7k logs/h of supply the fleet was not touching.
//
// So the owner's ladder stands exactly as it is — rung 1 is still normal trees and arrow shafts
// until Fletching 20 — and the only change is WHICH normal trees.
//
// # Every patch below was chosen, not guessed
//
// Each is a cluster of normal-tree-table locs (content/scripts/skill_woodcutting/configs/
// trees.dbrow `[normal_tree_table]`: `tree`, `tree2`, `deadtree2` — one log, 25 xp, `Logs`, the
// same 120-199 tick stump), found by clustering the static loc index around the Seers bank. Of
// about twenty clusters inside the Seers radius, eight are kept, each only if:
//
//   - every tree it counts has a stand tile the collision pack can route to FROM THE SEERS BANK
//     with no door on the way (pathfind.FindPath, which knows no doors);
//   - it keeps clear of every hunting NPC near Seers, against a table of those spawns read out
//     of the content maps' NPC sections with each npc's wanderrange and huntrange
//     (patches_data_test.go): every counted stand tile at least 3 tiles outside a hunter's
//     reach, and at least 2 for the door-free route from the bank AND for the walk from the
//     anchor to every tree the account could be handed there. `cowardly` hunts anyone under
//     twice its level, which is every rung-1 account here;
//   - no tree it could hand an account is a DETOUR away: the walk from the anchor to it is at
//     most twice the straight-line distance plus 30 tiles. A tree whose only route goes round a
//     wall is a tree that walks the account somewhere nobody chose.
//
// What those rules threw out, because each is a way this change could have killed accounts:
//   - the clusters in and around McGrubor's Wood, including one beside the Ranging Guild whose
//     trees are walked to one tile outside a level 44 guard dog's reach;
//   - the northern dead-tree fields, whose only door-free route passes the two level-5
//     HIGHWAYMEN on the road north of Seers (2735,3521 and 2749,3521, wanderrange 8, huntrange
//     5). Those two are in no webwalk danger zone, and a lab account walking to a northern patch
//     was attacked at 2734,3525 and ended up across the map;
//   - both evergreen stands east of Seers: some of their trees sit against Camelot's walls, and
//     the only route to them goes round the castle to the EAST, through White Wolf Mountain's
//     southern foot — below the danger zone's z 3468 — where a lab account walked into the white
//     wolves (2842,3450 and 2846-2848,3472-3477);
//   - a seventeen-tree field at 2723,3423, two tiles from the giant bats' danger rectangle;
//   - anything outside woodcutplan.SeersRadius of the bank, so DecideWoodPhase keeps treating the
//     account as "at Seers" and never sends it to the fare/ship road.
//
// TestPatchesAgreeWithTheLocIndex re-derives every Trees count and every reachability claim
// from the real loc index and pack when RSB0T_LOC_INDEX, RSB0T_CONFIG_JAG and
// RSB0T_COLLISION_PACK are set, so a content or pack change fails a test instead of quietly
// sending accounts to a field of stumps.

import (
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/woodcutplan"
)

// Patch is one place rung 1 can chop plain logs.
type Patch struct {
	// Name is what the log line and the panel call it.
	Name string
	// Loc is the display name woodcutrunner.Chop searches for: "Tree" or "Dead tree", both in
	// `[normal_tree_table]` (so is "Evergreen", but no evergreen stand near Seers is safe).
	Loc string
	// Anchor is a walkable tile the pack can route to from the Seers bank, chosen as the centre
	// that puts every counted tree closest.
	Anchor tile.Tile
	// Leash is how far from Anchor a counted tree stands at most.
	Leash int
	// Trees is how many Loc locs stand within Leash of Anchor with a stand tile routable from the
	// Seers bank. It is the patch's share of supply, and the board fills patches by it.
	Trees int
	// Home marks the ladder's own Seers camp, woodcutplan.TIERS[0] unchanged.
	Home bool
}

// Tier is the patch as a woodcutplan.Tier, so woodcutrunner.Chop, WalkNear and AtCamp run on it
// unchanged. Everything but the place and the loc name is rung 1's own tier.
func (p Patch) Tier() woodcutplan.Tier {
	t := Ladder[0].Tier
	t.Loc, t.Anchor, t.Leash = p.Loc, p.Anchor, p.Leash
	return t
}

// homeTier is rung 1's own camp, read rather than restated.
var homeTier = Ladder[0].Tier

// NormalPatches is every patch rung 1 may be sent to. The first is the ladder's own camp,
// exactly as woodcutplan.TIERS has it, so an account seated there behaves precisely as before.
//
// The distance in each comment is Chebyshev from the Seers bank (2725,3493).
var NormalPatches = []Patch{
	{Name: "Seers bank", Loc: homeTier.Loc, Anchor: homeTier.Anchor, Leash: homeTier.Leash, Trees: 12, Home: true}, // the ladder's own camp
	{Name: "south field", Loc: "Tree", Anchor: tile.Tile{X: 2718, Z: 3442}, Leash: 8, Trees: 24},                   // 51
	{Name: "Catherby road", Loc: "Tree", Anchor: tile.Tile{X: 2765, Z: 3463}, Leash: 12, Trees: 27},                // 40
	{Name: "Ranging Guild east", Loc: "Tree", Anchor: tile.Tile{X: 2692, Z: 3442}, Leash: 9, Trees: 17},            // 51
	{Name: "Catherby west", Loc: "Tree", Anchor: tile.Tile{X: 2776, Z: 3442}, Leash: 8, Trees: 15},                 // 51
	{Name: "south-west", Loc: "Tree", Anchor: tile.Tile{X: 2699, Z: 3426}, Leash: 9, Trees: 16},                    // 67
	{Name: "Catherby north", Loc: "Tree", Anchor: tile.Tile{X: 2792, Z: 3459}, Leash: 8, Trees: 11},                // 67
	{Name: "Seers south dead trees", Loc: "Dead tree", Anchor: tile.Tile{X: 2709, Z: 3462}, Leash: 5, Trees: 7},    // 31
	{Name: "Ranging Guild gate", Loc: "Tree", Anchor: tile.Tile{X: 2675, Z: 3445}, Leash: 6, Trees: 6},             // 50
}

// UsesPatches reports whether an account on rung idx making product is spread over
// NormalPatches rather than held at the ladder's own camp.
//
// Only rung 1, and only while its logs become something that STACKS. That is what makes a
// remote patch cost one walk: fifteen arrow shafts land in one slot, so the pack never fills and
// the account never has to carry anything back. A bow is one log to one unstackable item, so
// under the "bows" plain-log policy the pack fills every load and every load would be a walk of
// up to seventy tiles to the bank — those accounts stay at the camp beside it.
func UsesPatches(idx int, product Product, canFletch bool) bool {
	return idx == 0 && canFletch && product.PerLog > 1
}

// ---- the board ----

// PatchSeatTTL is how long a seat stands without a refresh. An account refreshes it every Loop
// pass while it is on rung 1; one that died, disconnected, was moved to another script or spent
// longer than this on a mule trip simply loses it and is seated afresh when it comes back.
const PatchSeatTTL = 5 * time.Minute

// PatchRebalanceAfter is how long an account keeps its patch before the board will consider
// moving it. Accounts leave rung 1 as they reach Fletching 20 and the fleet restarts many times a
// day, so a seating that was even when it was made drifts; this is how it evens out again
// without accounts walking back and forth.
const PatchRebalanceAfter = 10 * time.Minute

// PatchRebalanceGain is how much better another patch must be before an account walks to it: at
// least half as many trees again per account. A smaller margin moves accounts for a difference
// the walk would eat.
const PatchRebalanceGain = 1.5

// patchNearTie is how close two patches' shares must be to count as equal, so the nearer one is
// preferred. Without it the fill order is decided by one tree's difference and the first dozen
// accounts of a restart all walk to the far end of the map.
const patchNearTie = 0.85

// PatchBoard is the process-wide seating: which account chops at which patch.
//
// It is the same shape as woodcutplan.TreeClaims and for the same reason: every account the
// fleet runs lives in one process, so an atomic, lock-guarded map is an exact view of the
// cohort, where anything eventually consistent (mulebus) would let a restart's first burst of
// logins all read an empty board and pick the same patch. It imports nothing but sync, time and
// tile, so it is still a pure plan package.
//
// A nil *PatchBoard is usable and seats every account at the ladder's own camp, which is the
// behaviour before this file existed.
type PatchBoard struct {
	patches []Patch
	mu      sync.Mutex
	seats   map[string]patchSeat
}

type patchSeat struct {
	patch int
	since time.Time
	seen  time.Time
}

// NewPatchBoard returns an empty board over patches (NormalPatches when nil).
func NewPatchBoard(patches []Patch) *PatchBoard {
	if patches == nil {
		patches = NormalPatches
	}
	return &PatchBoard{patches: patches, seats: map[string]patchSeat{}}
}

// Seating is what Seat decided.
type Seating struct {
	// Patch is the patch to chop at, an index into the board's patches.
	Patch int
	// Moved is true when this call placed the account somewhere it was not seated before: a
	// first seating, a lapsed seat, or a rebalance.
	Moved bool
	// Occupants is how many accounts, this one included, are seated at Patch after the call.
	Occupants int
}

// Seat returns where who should chop, seating it if it has no live seat and refreshing the seat
// if it has.
//
// A new seat goes to the patch with the most trees per account once this one is counted
// (Trees / (occupants + 1)): water-filling, so the cohort ends up with as even a share of trees
// as the patches allow. Among patches within patchNearTie of the best share, the one nearest to
// from wins, then the lower index, so the answer is deterministic.
//
// An existing seat is kept. Once it is PatchRebalanceAfter old, it moves only if another patch
// would give this account at least PatchRebalanceGain times the trees its current one does.
func (b *PatchBoard) Seat(who string, from tile.Tile, now time.Time) Seating {
	if b == nil || len(b.patches) == 0 {
		return Seating{Patch: 0, Occupants: 1}
	}
	key := normWho(who)
	// A rung-1 account standing beside the church yews is ours, not a stranger there (campcrowd.go).
	noteOwnKey(key)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweepLocked(now)

	occ := make([]int, len(b.patches))
	for k, s := range b.seats {
		if k != key && s.patch >= 0 && s.patch < len(occ) {
			occ[s.patch]++
		}
	}

	if s, ok := b.seats[key]; ok && s.patch >= 0 && s.patch < len(b.patches) {
		s.seen = now
		if now.Sub(s.since) >= PatchRebalanceAfter {
			here := share(b.patches[s.patch].Trees, occ[s.patch])
			best := b.bestLocked(occ, from, s.patch)
			if best != s.patch && share(b.patches[best].Trees, occ[best]) >= PatchRebalanceGain*here {
				s = patchSeat{patch: best, since: now, seen: now}
				b.seats[key] = s
				return Seating{Patch: best, Moved: true, Occupants: occ[best] + 1}
			}
			// Considered and kept: start the clock again, so a crowded board is not re-scored on
			// every single pass.
			s.since = now
		}
		b.seats[key] = s
		return Seating{Patch: s.patch, Occupants: occ[s.patch] + 1}
	}

	best := b.bestLocked(occ, from, -1)
	b.seats[key] = patchSeat{patch: best, since: now, seen: now}
	return Seating{Patch: best, Moved: true, Occupants: occ[best] + 1}
}

// share is how many trees one more account at a patch would have to itself.
func share(trees, others int) float64 {
	if trees <= 0 {
		return 0
	}
	return float64(trees) / float64(others+1)
}

// bestLocked is the patch with the most trees per account for one more account, near-ties
// broken by distance from `from` and then by index. `except` is skipped (the caller's current
// patch when rebalancing); -1 skips nothing.
func (b *PatchBoard) bestLocked(occ []int, from tile.Tile, except int) int {
	top := 0.0
	for i, p := range b.patches {
		if i == except {
			continue
		}
		if s := share(p.Trees, occ[i]); s > top {
			top = s
		}
	}
	best, bestDist := -1, 0
	for i, p := range b.patches {
		if i == except || share(p.Trees, occ[i]) < top*patchNearTie {
			continue
		}
		d := tile.Chebyshev(from, p.Anchor)
		if from.Level != p.Anchor.Level {
			d += 1 << 20
		}
		if best < 0 || d < bestDist {
			best, bestDist = i, d
		}
	}
	if best < 0 {
		return 0
	}
	return best
}

// Leave releases who's seat. Called when the account leaves rung 1 or stops.
func (b *PatchBoard) Leave(who string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	delete(b.seats, normWho(who))
	b.mu.Unlock()
}

// Occupancy is how many live seats each patch holds, for a log line and for tests.
func (b *PatchBoard) Occupancy(now time.Time) []int {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweepLocked(now)
	out := make([]int, len(b.patches))
	for _, s := range b.seats {
		if s.patch >= 0 && s.patch < len(out) {
			out[s.patch]++
		}
	}
	return out
}

// Patches is the board's patch list.
func (b *PatchBoard) Patches() []Patch {
	if b == nil {
		return nil
	}
	return b.patches
}

func (b *PatchBoard) sweepLocked(now time.Time) {
	for k, s := range b.seats {
		if now.Sub(s.seen) > PatchSeatTTL {
			delete(b.seats, k)
		}
	}
}

// normWho keys a seat the way the game keys an account: case- and space-insensitive, so
// "Acct_181" and "acct 181" are one seat.
func normWho(who string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(who), "_", " "))
}

// One board per WORLD, for the reason woodcutplan.SharedTreeClaimsFor gives: two accounts on
// different worlds at the same patch are not contending for anything, so seating them against
// each other would send one of them away from an empty patch.
var (
	patchBoardsMu sync.Mutex
	patchBoards   = map[int]*PatchBoard{}
)

// SharedPatchBoardFor is the process-wide board for one world. World 0 is "the fleet's own
// world", which is what a caller with no world information has.
func SharedPatchBoardFor(world int) *PatchBoard {
	if world < 0 {
		world = 0
	}
	patchBoardsMu.Lock()
	defer patchBoardsMu.Unlock()
	b := patchBoards[world]
	if b == nil {
		b = NewPatchBoard(nil)
		patchBoards[world] = b
	}
	return b
}

// DescribeOccupancy is the board as one log line: "Seers bank 3/12, south field 4/24, ...",
// patches with nobody seated left out.
func DescribeOccupancy(patches []Patch, occ []int) string {
	type row struct {
		name  string
		n, of int
	}
	var rows []row
	for i, p := range patches {
		if i < len(occ) && occ[i] > 0 {
			rows = append(rows, row{p.Name, occ[i], p.Trees})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].n > rows[j].n })
	parts := make([]string, 0, len(rows))
	for _, r := range rows {
		parts = append(parts, r.name+" "+strconv.Itoa(r.n)+"/"+strconv.Itoa(r.of))
	}
	if len(parts) == 0 {
		return "nobody seated"
	}
	return strings.Join(parts, ", ")
}
