package wcfletchplan

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/woodcutplan"
	"rsb0t-go/internal/webwalk"
)

// The first patch is the ladder's own camp, exactly — so an account seated there, and every
// account with spreadRungOne off, chops precisely where it did before this file existed.
func TestHomePatchIsTheLaddersOwnCamp(t *testing.T) {
	home := NormalPatches[0]
	want := woodcutplan.TIERS[0]
	if !home.Home || home.Anchor != want.Anchor || home.Leash != want.Leash || home.Loc != want.Loc {
		t.Fatalf("patch 0 = %+v, want the ladder's own camp %v leash %d loc %q", home, want.Anchor, want.Leash, want.Loc)
	}
	if home.Tier() != want {
		t.Errorf("patch 0's tier %+v differs from woodcutplan.TIERS[0] %+v", home.Tier(), want)
	}
	for i, p := range NormalPatches[1:] {
		if p.Home {
			t.Errorf("patch %d (%s) is marked Home too", i+1, p.Name)
		}
	}
}

// A patch is rung 1 somewhere else: same logs, same experience, same content requirement.
func TestPatchTierIsRungOneElsewhere(t *testing.T) {
	for _, p := range NormalPatches {
		got := p.Tier()
		r := Ladder[0].Tier
		if got.Label != r.Label || got.Logs != r.Logs || got.XP != r.XP || got.Level != r.Level || got.StartAt != r.StartAt {
			t.Errorf("%s: tier %+v is not rung 1's (%+v) apart from place and loc", p.Name, got, r)
		}
		if got.Loc != p.Loc || got.Anchor != p.Anchor || got.Leash != p.Leash {
			t.Errorf("%s: tier did not take the patch's own loc/anchor/leash: %+v", p.Name, got)
		}
	}
}

// Only the three loc names content's [normal_tree_table] uses around Seers. A "Dead tree" that
// is really deadtree_burnt would give charcoal; none stands within reach of these patches
// (checked against the loc index when this list was made), and the loc-index test re-checks it.
func TestPatchLocsAreNormalTrees(t *testing.T) {
	ok := map[string]bool{"Tree": true, "Dead tree": true, "Evergreen": true}
	seen := map[string]bool{}
	for _, p := range NormalPatches {
		if !ok[p.Loc] {
			t.Errorf("%s: loc %q is not a normal-tree-table name", p.Name, p.Loc)
		}
		if seen[p.Name] {
			t.Errorf("patch name %q is used twice", p.Name)
		}
		seen[p.Name] = true
		if p.Trees <= 0 || p.Leash <= 0 {
			t.Errorf("%s: trees %d leash %d", p.Name, p.Trees, p.Leash)
		}
		if p.Anchor.Level != 0 {
			t.Errorf("%s: anchor %v is not on the ground", p.Name, p.Anchor)
		}
	}
}

// The whole reach of a patch -- the account may stand Leash from the anchor and Chop searches
// Leash+2 around wherever it stands -- stays inside woodcutplan.SeersRadius, so DecideWoodPhase
// always takes its Seers branch there and never the fare/ship road.
func TestEveryPatchIsInsideTheSeersRadius(t *testing.T) {
	for _, p := range NormalPatches {
		reach := tile.Chebyshev(p.Anchor, woodcutplan.Seers) + p.Leash + 2
		if reach > woodcutplan.SeersRadius {
			t.Errorf("%s: anchor %v leash %d reaches %d tiles from the Seers bank, past SeersRadius %d",
				p.Name, p.Anchor, p.Leash, reach, woodcutplan.SeersRadius)
		}
	}
}

// No patch's reach touches any curated danger zone: White Wolf Mountain (the owner's ruling),
// the Seers-south giant bats, the Ardougne-road bears, the wilderness.
//
// The eight new patches are held to a generous box, twice the leash plus two. The Home patch is
// the ladder's own camp, which rung 1 chopped long before patches existed, and is held to what an
// account there can actually reach, its leash plus two: the generous box would reach z 3508 and
// clip the seers-north-highwaymen zone (from z 3506), which starts eight tiles past the camp's real
// reach. Every tree in that camp's leash is checked against the zone on the real pack by
// internal/webwalk's TestSeersHighwaymenLeaveTheFleetsRoutesAlone.
func TestNoPatchTouchesADangerZone(t *testing.T) {
	for _, p := range NormalPatches {
		r := 2*p.Leash + 2
		if p.Home {
			r = p.Leash + 2
		}
		box := webwalk.Rect{MinX: p.Anchor.X - r, MaxX: p.Anchor.X + r, MinZ: p.Anchor.Z - r, MaxZ: p.Anchor.Z + r, Level: 0}
		for _, z := range webwalk.KnownDangerZones {
			for _, zr := range z.Rects {
				if zr.Level != webwalk.AllLevels && zr.Level != 0 {
					continue
				}
				if box.MinX <= zr.MaxX && zr.MinX <= box.MaxX && box.MinZ <= zr.MaxZ && zr.MinZ <= box.MaxZ {
					t.Errorf("%s: reach %+v overlaps danger zone %q %+v", p.Name, box, z.ID, zr)
				}
			}
		}
	}
}

func TestUsesPatchesOnlyForStackingRungOne(t *testing.T) {
	shaft, _ := Choose(0, 1, NormalShafts)
	bow, _ := Choose(0, 10, NormalBows)
	oak, _ := Choose(1, 20, NormalShafts)
	cases := []struct {
		name      string
		idx       int
		p         Product
		canFletch bool
		want      bool
	}{
		{"rung 1 arrow shafts", 0, shaft, true, true},
		{"rung 1 plain longbows (the bows policy) bank every load", 0, bow, true, false},
		{"rung 2 oak bows", 1, oak, true, false},
		{"rung 1 but nothing can be cut", 0, shaft, false, false},
	}
	for _, c := range cases {
		if got := UsesPatches(c.idx, c.p, c.canFletch); got != c.want {
			t.Errorf("%s: UsesPatches = %v, want %v", c.name, got, c.want)
		}
	}
}

// ---- the board ----

var seersBank = woodcutplan.Seers

// THE COHORT SPREADS. 55 accounts seated one after another from the Seers bank -- the live
// 3dfb543a rung-1 cohort's size -- must use every patch, put no more on the ladder's own camp
// than its share of the trees, and leave no patch with twice the accounts per tree of another.
func TestBoardSpreadsTheLiveCohort(t *testing.T) {
	b := NewPatchBoard(nil)
	now := time.Unix(1_800_000_000, 0)
	for i := 0; i < 55; i++ {
		b.Seat(fmt.Sprintf("acct%d", i), seersBank, now)
	}
	occ := b.Occupancy(now)
	total, trees := 0, 0
	for i, p := range NormalPatches {
		total += occ[i]
		trees += p.Trees
		if occ[i] == 0 {
			t.Errorf("%s got nobody", p.Name)
		}
	}
	if total != 55 {
		t.Fatalf("occupancy sums to %d, want 55", total)
	}
	lo, hi := 1e9, 0.0
	for i, p := range NormalPatches {
		perTree := float64(occ[i]) / float64(p.Trees)
		if perTree < lo {
			lo = perTree
		}
		if perTree > hi {
			hi = perTree
		}
	}
	if hi > 2.2*lo {
		t.Errorf("accounts per tree range %.3f..%.3f: the fill is not even (%v)", lo, hi, occ)
	}
	fairHome := float64(55) * float64(NormalPatches[0].Trees) / float64(trees)
	if float64(occ[0]) > fairHome+1.5 {
		t.Errorf("the ladder's own camp holds %d accounts, over its fair share %.1f of %d trees", occ[0], fairHome, trees)
	}
	t.Logf("55 accounts over %d trees: %s", trees, DescribeOccupancy(NormalPatches, occ))
}

// On origin/main every one of those 55 stood at patch 0. That is the arm this board replaces,
// stated as a number so the difference is on the record: 12 trees for 55 accounts.
func TestTheOldSeatingIsOneCampForEveryone(t *testing.T) {
	if got := float64(NormalPatches[0].Trees) / 55; got > 0.25 {
		t.Errorf("the ladder's own camp would give 55 accounts %.2f trees each; the premise of this file is that it is far below one", got)
	}
}

func TestSeatIsStableAndRefreshes(t *testing.T) {
	b := NewPatchBoard(nil)
	now := time.Unix(1_800_000_000, 0)
	first := b.Seat("Acct181", seersBank, now)
	if !first.Moved {
		t.Fatal("a first seat should report Moved")
	}
	for i := 1; i <= 8; i++ {
		again := b.Seat("Acct181", seersBank, now.Add(time.Duration(i)*time.Minute))
		if again.Patch != first.Patch || again.Moved {
			t.Fatalf("pass %d: seat moved %d -> %d (Moved=%v) with nothing else on the board", i, first.Patch, again.Patch, again.Moved)
		}
	}
	// Refreshed every minute, so it has not lapsed even though the first seat is 8 minutes old.
	if occ := b.Occupancy(now.Add(8 * time.Minute)); occ[first.Patch] != 1 {
		t.Errorf("occupancy %v after steady refreshes", occ)
	}
}

func TestSeatKeyIgnoresCaseAndUnderscores(t *testing.T) {
	b := NewPatchBoard(nil)
	now := time.Unix(1_800_000_000, 0)
	a := b.Seat("Acct_181", seersBank, now)
	c := b.Seat("acct 181", seersBank, now)
	if a.Patch != c.Patch || c.Moved {
		t.Errorf("one account under two spellings got two seats: %+v %+v", a, c)
	}
	total := 0
	for _, n := range b.Occupancy(now) {
		total += n
	}
	if total != 1 {
		t.Errorf("occupancy total %d, want 1", total)
	}
}

func TestSeatLapsesWithoutARefresh(t *testing.T) {
	b := NewPatchBoard(nil)
	now := time.Unix(1_800_000_000, 0)
	s := b.Seat("gone", seersBank, now)
	if occ := b.Occupancy(now.Add(PatchSeatTTL)); occ[s.Patch] != 1 {
		t.Fatalf("seat gone at exactly the TTL: %v", occ)
	}
	if occ := b.Occupancy(now.Add(PatchSeatTTL + time.Second)); occ[s.Patch] != 0 {
		t.Errorf("seat still held past the TTL: %v", occ)
	}
	if again := b.Seat("gone", seersBank, now.Add(PatchSeatTTL+2*time.Second)); !again.Moved {
		t.Error("a lapsed seat's owner should be seated afresh (Moved)")
	}
}

func TestLeaveReleasesTheSeat(t *testing.T) {
	b := NewPatchBoard(nil)
	now := time.Unix(1_800_000_000, 0)
	s := b.Seat("graduate", seersBank, now)
	b.Leave("Graduate")
	if occ := b.Occupancy(now); occ[s.Patch] != 0 {
		t.Errorf("seat held after Leave: %v", occ)
	}
}

// Rebalancing: only after PatchRebalanceAfter, and only for at least PatchRebalanceGain times the
// trees per account. Two equal patches, four accounts, then the two on patch B leave (they
// reached Fletching 20): one of A's two moves to B; the other then has A to itself and stays.
func TestRebalanceMovesOnlyForAClearGain(t *testing.T) {
	two := []Patch{
		{Name: "A", Loc: "Tree", Anchor: tile.Tile{X: 2720, Z: 3480}, Leash: 8, Trees: 10},
		{Name: "B", Loc: "Tree", Anchor: tile.Tile{X: 2760, Z: 3460}, Leash: 8, Trees: 10},
	}
	b := NewPatchBoard(two)
	t0 := time.Unix(1_800_000_000, 0)
	seat := map[string]int{}
	for _, n := range []string{"a1", "a2", "a3", "a4"} {
		seat[n] = b.Seat(n, two[0].Anchor, t0).Patch
	}
	onA := []string{}
	for n, p := range seat {
		if p == 0 {
			onA = append(onA, n)
		} else {
			b.Leave(n)
		}
	}
	if len(onA) != 2 {
		t.Fatalf("expected an even 2/2 fill, got %v", seat)
	}
	// Too soon: nobody moves, however lopsided. Refreshed every minute, as a live account's
	// every-pass refresh keeps it well inside PatchSeatTTL.
	for m := time.Minute; m < PatchRebalanceAfter; m += time.Minute {
		for _, n := range onA {
			if s := b.Seat(n, two[0].Anchor, t0.Add(m)); s.Moved || s.Patch != 0 {
				t.Fatalf("%s moved %s in, before PatchRebalanceAfter: %+v", n, m, s)
			}
		}
	}
	later := t0.Add(PatchRebalanceAfter + time.Second)
	moved := 0
	for _, n := range onA {
		if s := b.Seat(n, two[0].Anchor, later); s.Moved {
			moved++
			if s.Patch != 1 {
				t.Errorf("%s rebalanced to %d, want B", n, s.Patch)
			}
		}
	}
	if moved != 1 {
		t.Errorf("%d accounts moved, want exactly 1 (the second then has A to itself)", moved)
	}
}

// A near-tie goes to the nearer patch, so a restart's first accounts do not all walk to the far
// edge of the map for one tree's difference.
func TestNearTieGoesToTheNearerPatch(t *testing.T) {
	two := []Patch{
		{Name: "far", Loc: "Tree", Anchor: tile.Tile{X: 2725, Z: 3420}, Leash: 8, Trees: 20},
		{Name: "near", Loc: "Tree", Anchor: tile.Tile{X: 2725, Z: 3480}, Leash: 8, Trees: 18},
	}
	b := NewPatchBoard(two)
	if s := b.Seat("x", seersBank, time.Unix(1_800_000_000, 0)); s.Patch != 1 {
		t.Errorf("seated at %s, want the nearer patch within the near-tie margin", two[s.Patch].Name)
	}
	far := []Patch{two[0], {Name: "near but small", Loc: "Tree", Anchor: tile.Tile{X: 2725, Z: 3480}, Leash: 8, Trees: 8}}
	b = NewPatchBoard(far)
	if s := b.Seat("x", seersBank, time.Unix(1_800_000_000, 0)); s.Patch != 0 {
		t.Errorf("seated at %s, but 8 trees is not a near-tie with 20", far[s.Patch].Name)
	}
}

func TestNilBoardSeatsAtTheLaddersCamp(t *testing.T) {
	var b *PatchBoard
	if s := b.Seat("x", seersBank, time.Now()); s.Patch != 0 {
		t.Errorf("nil board seated at %d", s.Patch)
	}
	b.Leave("x")
	if b.Occupancy(time.Now()) != nil {
		t.Error("nil board reported occupancy")
	}
}

func TestBoardsArePerWorld(t *testing.T) {
	if SharedPatchBoardFor(1) == SharedPatchBoardFor(2) {
		t.Error("worlds 1 and 2 share a board")
	}
	if SharedPatchBoardFor(-1) != SharedPatchBoardFor(0) {
		t.Error("a negative world should be world 0")
	}
}

// Every account in the fleet seats through one lock, so a restart's burst of logins cannot all
// read an empty board and pick the same patch.
func TestConcurrentSeatingIsAtomic(t *testing.T) {
	b := NewPatchBoard(nil)
	now := time.Unix(1_800_000_000, 0)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			b.Seat(fmt.Sprintf("burst%d", i), seersBank, now)
		}(i)
	}
	wg.Wait()
	occ := b.Occupancy(now)
	total, trees := 0, 0
	for i, n := range occ {
		total += n
		trees += NormalPatches[i].Trees
	}
	if total != 100 {
		t.Fatalf("occupancy total %d, want 100", total)
	}
	// Each patch within three accounts of its share of the trees: the same water-filling a
	// one-at-a-time seating gives, so no two simultaneous callers both took the emptiest patch.
	for i, n := range occ {
		fair := 100 * float64(NormalPatches[i].Trees) / float64(trees)
		if float64(n) > fair+3 {
			t.Errorf("%s holds %d of a burst of 100, over its share %.1f of %d trees: %v", NormalPatches[i].Name, n, fair, trees, occ)
		}
	}
}
