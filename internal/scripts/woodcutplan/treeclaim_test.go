package woodcutplan

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// camp builds n candidates in a row, nearest first, the shape of a real tree camp.
func camp(n int) []TreeCandidate {
	out := make([]TreeCandidate, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, TreeCandidate{At: at(2730+i, 3486), Distance: i + 1})
	}
	return out
}

// TestNAccountsNeverDoubleBookATree is the owner's requirement, stated as a test: more accounts
// than trees, every account choosing from the same camp, and no two accounts ever holding the
// same tree.
func TestNAccountsNeverDoubleBookATree(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	const trees, accounts = 12, 94
	claims := NewTreeClaims()
	cands := camp(trees)

	holders := map[tile.Tile]string{}
	shared := 0
	for i := 0; i < accounts; i++ {
		me := fmt.Sprintf("woodcutter%02d", i)
		pick, ok := PickTreeFor(cands, nil, claims, me, now)
		if !ok {
			t.Fatalf("%s was given no tree at all from a camp of %d", me, trees)
		}
		if !pick.Mine {
			// Rule 2: it still got a tree to chop, it just does not hold it.
			if !pick.Shared {
				t.Errorf("%s holds no claim but the pick is not flagged Shared: %+v", me, pick)
			}
			shared++
			continue
		}
		if prev, taken := holders[pick.Tree.At]; taken {
			t.Fatalf("%s was handed %v, which %s already holds", me, pick.Tree.At, prev)
		}
		holders[pick.Tree.At] = me
	}
	if len(holders) != trees {
		t.Errorf("every tree in the camp should have found exactly one holder: %d of %d", len(holders), trees)
	}
	if shared != accounts-trees {
		t.Errorf("the other %d accounts should each have doubled up: got %d", accounts-trees, shared)
	}
	if got := claims.Len(now); got != trees {
		t.Errorf("registry holds %d claims, want %d", got, trees)
	}
}

// TestNobodyIsLeftIdleWhenEveryTreeIsTaken pins rule 2 on its own, because it is the rule most
// easily lost in a later refactor: a claim is a preference, never a permit.
func TestNobodyIsLeftIdleWhenEveryTreeIsTaken(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	claims := NewTreeClaims()
	cands := camp(3)
	for i, who := range []string{"a", "b", "c"} {
		if pick, ok := PickTreeFor(cands, nil, claims, who, now); !ok || !pick.Mine {
			t.Fatalf("%s (account %d) should have claimed a free tree: ok=%v pick=%+v", who, i, ok, pick)
		}
	}
	pick, ok := PickTreeFor(cands, nil, claims, "latecomer", now)
	if !ok {
		t.Fatal("an account with no free tree must still be told to chop something, not to wait")
	}
	if pick.Mine {
		t.Errorf("nothing was free, so the pick cannot be a claim: %+v", pick)
	}
	if !pick.Shared {
		t.Errorf("the pick should say it is a deliberate double-up: %+v", pick)
	}
	if pick.Tree.At == (tile.Tile{}) {
		t.Error("a shared pick still has to name a real tree")
	}
	if pick.Claimed != 3 {
		t.Errorf("Claimed should report all three: %+v", pick)
	}
}

// TestTheSpareAccountsSpreadRatherThanStack is rule 4: when every tree is claimed, the accounts
// that could not claim one must not all be sent to the same tile, which is the pathology this
// whole file exists to remove.
func TestTheSpareAccountsSpreadRatherThanStack(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	claims := NewTreeClaims()
	cands := camp(8)
	// Take every tree with accounts that are not among the spares below.
	for i := 0; i < 8; i++ {
		if _, ok := PickTreeFor(cands, nil, claims, fmt.Sprintf("holder%d", i), now); !ok {
			t.Fatal("setup: a holder failed to claim")
		}
	}
	seen := map[tile.Tile]int{}
	for i := 0; i < 40; i++ {
		pick, ok := PickTreeFor(cands, nil, claims, fmt.Sprintf("spare%02d", i), now)
		if !ok || !pick.Shared {
			t.Fatalf("spare%02d: ok=%v pick=%+v", i, ok, pick)
		}
		seen[pick.Tree.At]++
	}
	if len(seen) < 4 {
		t.Errorf("40 spare accounts landed on only %d of 8 trees; they are stacking, not spreading: %v", len(seen), seen)
	}
	lo, hi := 40, 0
	for _, n := range seen {
		lo, hi = min(lo, n), max(hi, n)
	}
	// Recorded rather than asserted: an even five-per-tree is not the requirement and pinning
	// it would only make the test brittle against a hash change. Not stacking is.
	t.Logf("40 spare accounts over 8 trees: %d trees used, %d to %d accounts each", len(seen), lo, hi)
	// And the choice is stable for one account, so it does not thrash tree to tree pass to pass.
	first, _ := PickTreeFor(cands, nil, claims, "spare07", now)
	second, _ := PickTreeFor(cands, nil, claims, "spare07", now.Add(time.Second))
	if first.Tree.At != second.Tree.At {
		t.Errorf("one account's shared pick moved between passes: %v then %v", first.Tree.At, second.Tree.At)
	}
}

// TestTheSpreadSurvivesTreesFallingAndComingBack is why the fallback is rendezvous-weighted
// rather than hash-modulo-count. At a normal-tree camp the candidate set changes every few
// seconds as trees fall and respawn; a choice that depended on the SET SIZE would send every
// spare account to a different tree each time any tree fell, which is exactly the extra walking
// the whole change exists to remove.
func TestTheSpreadSurvivesTreesFallingAndComingBack(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	claims := NewTreeClaims()
	full := camp(10)
	for i := range full {
		claims.Claim(full[i].At, fmt.Sprintf("holder%d", i), now)
	}

	before := map[string]tile.Tile{}
	for i := 0; i < 30; i++ {
		me := fmt.Sprintf("spare%02d", i)
		pick, _ := PickTreeFor(full, nil, claims, me, now)
		before[me] = pick.Tree.At
	}
	// One tree falls: drop it from the candidate set, exactly as locindex.Find would once the
	// loc change arrived.
	fallen := full[4].At
	short := append(append([]TreeCandidate{}, full[:4]...), full[5:]...)

	moved, shouldHaveMoved := 0, 0
	for i := 0; i < 30; i++ {
		me := fmt.Sprintf("spare%02d", i)
		pick, _ := PickTreeFor(short, nil, claims, me, now)
		if before[me] == fallen {
			shouldHaveMoved++
			continue
		}
		if pick.Tree.At != before[me] {
			moved++
		}
	}
	if moved != 0 {
		t.Errorf("%d accounts that were not pointed at the fallen tree moved anyway", moved)
	}
	if shouldHaveMoved == 0 {
		t.Log("note: no account happened to be pointed at the fallen tree, so only the stability half was exercised")
	}
}

// TestAClaimExpires is rule 1: an account that dies, disconnects or is teleported away stops
// refreshing, and its tree has to come back to the camp without anybody observing anything.
func TestAClaimExpires(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	claims := NewTreeClaims()
	tree := at(2730, 3486)

	if !claims.Claim(tree, "dies-in-a-minute", now) {
		t.Fatal("a free tree should be claimable")
	}
	if claims.Claim(tree, "somebody-else", now) {
		t.Error("a live claim must not be takeable by another account")
	}
	if got := claims.HolderOf(tree, now.Add(TreeClaimTTL-time.Second)); got != "dies-in-a-minute" {
		t.Errorf("still held one second before the TTL: got %q", got)
	}
	if got := claims.HolderOf(tree, now.Add(TreeClaimTTL)); got != "" {
		t.Errorf("the claim should have lapsed exactly at the TTL: still held by %q", got)
	}
	if !claims.Claim(tree, "somebody-else", now.Add(TreeClaimTTL)) {
		t.Error("an expired claim must be takeable by the next account along")
	}
	// A holder that keeps working keeps its tree: the refresh is what separates "still here"
	// from "gone", and it is why the TTL can be short enough to free a dead account quickly.
	refreshed := now.Add(TreeClaimTTL)
	for i := 0; i < 10; i++ {
		refreshed = refreshed.Add(TreeClaimRefresh)
		if !claims.Claim(tree, "somebody-else", refreshed) {
			t.Fatalf("a holder re-stamping its own claim must always succeed (pass %d)", i)
		}
	}
	if got := claims.HolderOf(tree, refreshed); got != "somebody-else" {
		t.Errorf("after ten refreshes the holder should still hold it: got %q", got)
	}
}

// TestOnlyTheHolderCanRelease stops a stray or replayed release freeing another account's tree.
func TestOnlyTheHolderCanRelease(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	claims := NewTreeClaims()
	tree := at(2730, 3486)
	claims.Claim(tree, "holder", now)

	if claims.Release(tree, "impostor") {
		t.Error("a release from a non-holder must be refused")
	}
	if claims.HolderOf(tree, now) != "holder" {
		t.Error("and it must not have freed the tree")
	}
	if !claims.Release(tree, "holder") {
		t.Error("the holder's own release should succeed")
	}
	if claims.HolderOf(tree, now) != "" {
		t.Error("the tree should be free after its holder released it")
	}
}

// TestOneAccountHoldsOneTree: picking a new tree gives up the old one, so an account walking
// across a camp cannot accumulate reservations it is not working.
func TestOneAccountHoldsOneTree(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	claims := NewTreeClaims()
	near, far := camp(2)[0], camp(2)[1]

	first, _ := PickTreeFor([]TreeCandidate{near, far}, nil, claims, "me", now)
	if first.Tree.At != near.At {
		t.Fatalf("nearest first: got %v", first.Tree.At)
	}
	// The nearest is now blocked for this account (say an Ent turned up), so it picks the far
	// one — and must let the near one go.
	var blocks TreeBlocks
	blocks.Block(near.At, now)
	second, _ := PickTreeFor([]TreeCandidate{near, far}, &blocks, claims, "me", now)
	if second.Tree.At != far.At {
		t.Fatalf("with the nearest blocked the pick should move on: got %v", second.Tree.At)
	}
	if got := claims.HolderOf(near.At, now); got != "" {
		t.Errorf("the abandoned tree is still claimed by %q", got)
	}
	if got := claims.Len(now); got != 1 {
		t.Errorf("one account should hold exactly one tree: %d", got)
	}
}

// TestBlockedTreesAreNeverPickedEvenWhenEverythingElseIsClaimed is the one place where "chop
// something rather than idle" must NOT win: a blocked tree is one where chopping breaks the axe
// or is known to do nothing, and the fallback must not reach for it.
func TestBlockedTreesAreNeverPickedEvenWhenEverythingElseIsClaimed(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	claims := NewTreeClaims()
	cands := camp(3)
	var blocks TreeBlocks
	blocks.Block(cands[0].At, now)
	// The other two are held by other accounts.
	claims.Claim(cands[1].At, "other-a", now)
	claims.Claim(cands[2].At, "other-b", now)

	pick, ok := PickTreeFor(cands, &blocks, claims, "me", now)
	if !ok {
		t.Fatal("two claimed trees are still choppable, so this must not refuse")
	}
	if pick.Tree.At == cands[0].At {
		t.Errorf("the fallback reached for a BLOCKED tree: %+v", pick)
	}
	// And with every unblocked tree gone as well, there really is nothing: ok is false, which
	// the caller already handles as "wait for one to grow back".
	blocks.Block(cands[1].At, now)
	blocks.Block(cands[2].At, now)
	if _, ok := PickTreeFor(cands, &blocks, claims, "me", now); ok {
		t.Error("with every tree blocked the answer has to be no tree at all")
	}
}

// TestTwoAccountsChoosingSimultaneouslyCannotBothWin runs the race for real under -race: many
// goroutines claiming out of one camp at the same instant, asserting afterwards that no tile was
// handed to two accounts and that the total handed out never exceeds the camp.
func TestTwoAccountsChoosingSimultaneouslyCannotBothWin(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	const trees, accounts, rounds = 6, 60, 20
	for r := 0; r < rounds; r++ {
		claims := NewTreeClaims()
		cands := camp(trees)
		var (
			mu      sync.Mutex
			holders = map[tile.Tile]string{}
			wins    int
		)
		var start, done sync.WaitGroup
		start.Add(1)
		done.Add(accounts)
		for i := 0; i < accounts; i++ {
			me := fmt.Sprintf("racer%02d", i)
			go func() {
				defer done.Done()
				start.Wait()
				// Each goroutine gets its own copy: PickTreeFor sorts in place, and two
				// accounts genuinely do hold separate candidate slices in the real runner.
				own := append([]TreeCandidate(nil), cands...)
				pick, ok := PickTreeFor(own, nil, claims, me, now)
				if !ok || !pick.Mine {
					return
				}
				mu.Lock()
				defer mu.Unlock()
				if prev, taken := holders[pick.Tree.At]; taken {
					t.Errorf("round %d: %v handed to both %s and %s", r, pick.Tree.At, prev, me)
				}
				holders[pick.Tree.At] = me
				wins++
			}()
		}
		start.Done()
		done.Wait()
		if wins != trees {
			t.Fatalf("round %d: %d accounts won claims out of a camp of %d", r, wins, trees)
		}
	}
}

// TestNoClaimsMeansTheOldBehaviour: a single-account harness, or an account whose own name has
// not arrived from the server yet, gets exactly the nearest-first pick this package had before.
func TestNoClaimsMeansTheOldBehaviour(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cands := camp(5)
	// Shuffled, to prove the ordering is this function's and not the caller's.
	shuffled := []TreeCandidate{cands[3], cands[0], cands[4], cands[1], cands[2]}

	pick, ok := PickTreeFor(shuffled, nil, nil, "", now)
	if !ok || pick.Tree.At != cands[0].At || pick.Mine || pick.Shared {
		t.Fatalf("a nil registry should give the plain nearest tree: %+v", pick)
	}
	// Same answer as the old entry point, which is still what the Ent rules are written
	// against.
	old, okOld := PickTree(shuffled, nil, now)
	if !okOld || old.At != pick.Tree.At {
		t.Errorf("PickTreeFor disagreed with PickTree: %v vs %v", pick.Tree.At, old.At)
	}
}

// TestEveryAccountRanksTheCampIdentically: "first free" is only a shared rule if the order is
// shared. Two accounts holding the same candidates in different orders must still produce the
// same ranking.
func TestEveryAccountRanksTheCampIdentically(t *testing.T) {
	// Two trees at the same distance, which is the case a distance-only sort leaves to
	// whatever order the loc index happened to return.
	a := TreeCandidate{At: at(2730, 3486), Distance: 4}
	b := TreeCandidate{At: at(2734, 3486), Distance: 4}
	now := time.Unix(1_700_000_000, 0)

	claims := NewTreeClaims()
	first, _ := PickTreeFor([]TreeCandidate{a, b}, nil, claims, "one", now)
	second, _ := PickTreeFor([]TreeCandidate{b, a}, nil, claims, "two", now)
	if first.Tree.At == second.Tree.At {
		t.Fatalf("both accounts took %v — the claim did not de-conflict them", first.Tree.At)
	}
	if first.Tree.At != a.At {
		t.Errorf("the first account should have taken the tie-break winner %v, got %v", a.At, first.Tree.At)
	}
	if second.Tree.At != b.At {
		t.Errorf("the second account should have fallen through to %v, got %v", b.At, second.Tree.At)
	}
}

// TestReleaseAllFreesTheCamp: what an account calls when it leaves for the bank.
func TestReleaseAllFreesTheCamp(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	claims := NewTreeClaims()
	claims.Claim(at(1, 1), "me", now)
	claims.Claim(at(2, 2), "other", now)
	// One account can only hold one tree through PickTreeFor, but Claim is the lower level and
	// a caller could hold several; ReleaseAll has to clear all of them regardless.
	claims.Claim(at(3, 3), "me", now)

	if n := claims.ReleaseAll("me"); n != 2 {
		t.Errorf("released %d, want 2", n)
	}
	if got := claims.HolderOf(at(2, 2), now); got != "other" {
		t.Errorf("another account's claim was collateral damage: %q", got)
	}
	if got := claims.Len(now); got != 1 {
		t.Errorf("one claim should remain: %d", got)
	}
}

// TestSweepDropsExpiredEntries: hygiene over a long run. The TTL already hides them from every
// reader, so this only proves the map does not grow forever.
func TestSweepDropsExpiredEntries(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	claims := NewTreeClaims()
	for i := 0; i < 50; i++ {
		claims.Claim(at(i, 0), fmt.Sprintf("gone%02d", i), now)
	}
	if n := claims.Sweep(now); n != 0 {
		t.Errorf("nothing has expired yet, swept %d", n)
	}
	if n := claims.Sweep(now.Add(TreeClaimTTL)); n != 50 {
		t.Errorf("swept %d, want 50", n)
	}
	if got := claims.Len(now.Add(TreeClaimTTL)); got != 0 {
		t.Errorf("registry should be empty: %d", got)
	}
}

// TestANilRegistryIsUsableAndSilent is the regression test for the crash that made this
// change's first live A/B unreadable.
//
// PickTreeFor already documented nil as "no de-confliction", so nil is a value callers hold —
// woodcutrunner's own kill switch produces one. Release did not tolerate it, the nil-pointer
// panic went into internal/bot/runner.go's recover(), and twelve accounts logged an ordinary
// "stopped" while the arm reported 25 cohort xp against the 3,624 a real build of origin/main
// produced on the same engine. Every method has to answer a nil receiver, not just the one that
// was called first.
func TestANilRegistryIsUsableAndSilent(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	var c *TreeClaims
	tree := at(2730, 3486)

	if c.Claim(tree, "me", now) {
		t.Error("a nil registry cannot grant a claim")
	}
	if c.Release(tree, "me") {
		t.Error("a nil registry holds nothing to release")
	}
	if n := c.ReleaseAll("me"); n != 0 {
		t.Errorf("ReleaseAll on nil released %d", n)
	}
	if h := c.HolderOf(tree, now); h != "" {
		t.Errorf("HolderOf on nil answered %q", h)
	}
	if n := c.Len(now); n != 0 {
		t.Errorf("Len on nil answered %d", n)
	}
	if n := c.Sweep(now); n != 0 {
		t.Errorf("Sweep on nil answered %d", n)
	}
	if _, free, ok := c.ClaimFirstFree(camp(3), "me", now); ok || free != 0 {
		t.Errorf("ClaimFirstFree on nil answered free=%d ok=%v", free, ok)
	}
	// And the whole decision still works through it, giving the nearest tree.
	pick, ok := PickTreeFor(camp(3), nil, c, "me", now)
	if !ok || pick.Mine || pick.Shared {
		t.Fatalf("a nil registry should still choose a tree, unclaimed: %+v", pick)
	}
	if pick.Tree.At != camp(3)[0].At {
		t.Errorf("and it should be the nearest: got %v", pick.Tree.At)
	}
}

// TestSharedRegistryIsOnePerProcess pins the singleton, because de-confliction silently fails
// if two callers end up holding different registries.
func TestSharedRegistryIsOnePerProcess(t *testing.T) {
	if SharedTreeClaims() != SharedTreeClaims() {
		t.Fatal("SharedTreeClaims must return the same registry every time")
	}
	if SharedTreeClaims() == NewTreeClaims() {
		t.Fatal("NewTreeClaims must return an isolated registry, not the shared one")
	}
}

// Two accounts on different worlds standing at the same tile are different players: they cannot
// see each other and cannot take each other's tree. The registry was keyed on tile alone, which
// was right while the fleet lived on one world and silently wrong the moment it did not — with
// three worlds it would hand out "every tree is taken" to a world that is empty, giving back the
// +29% this file was written to win.
func TestAWorldCannotBlockAnotherWorldsTree(t *testing.T) {
	at := tile.Tile{X: 2728, Z: 3482}
	now := time.Now()

	w1 := SharedTreeClaimsFor(1)
	w2 := SharedTreeClaimsFor(2)
	if w1 == w2 {
		t.Fatal("two worlds share one registry — a claim on one would block the other")
	}
	if !w1.Claim(at, "a", now) {
		t.Fatal("world 1 could not claim a free tree")
	}
	if !w2.Claim(at, "b", now) {
		t.Error("world 2 was refused a tree claimed on world 1 — accounts that cannot contend must not de-conflict")
	}
	// And within one world the claim still holds, which is the whole feature.
	if w1.Claim(at, "c", now) {
		t.Error("a second account on the SAME world took an already-claimed tree")
	}

	// World 0 is "the fleet's own world" and must be the registry a caller with no world
	// information gets, so a single-world fleet is unchanged.
	if SharedTreeClaimsFor(0) != SharedTreeClaims() {
		t.Error("SharedTreeClaims() must be the world-0 registry, or an unworlded caller silently gets its own")
	}
	if SharedTreeClaimsFor(-1) != SharedTreeClaimsFor(0) {
		t.Error("a negative world should fold into world 0 rather than creating a registry nobody else can name")
	}
}
