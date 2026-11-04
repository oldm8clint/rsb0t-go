package woodcutrunner

// The live-side half of cross-account tree de-confliction. The decision logic itself is pure
// and tested in internal/scripts/woodcutplan/treeclaim_test.go; what is testable here without a
// cache archive is the wiring: which registry an account uses, what it calls itself in that
// registry, and the claim the existing guarantee rests on — that a tree this account has SEEN
// fall is already gone from the candidate set before any claim is consulted.

import (
	"testing"
	"time"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/woodcutplan"
)

// TestTreeClaimsDefaultsToTheSharedRegistry pins the one thing about Deps.Claims that is easy
// to get wrong and impossible to notice: nil means the PROCESS-WIDE registry, not "off". A nil
// meaning "no de-confliction" would leave every existing caller — including the live fleet's —
// silently unchanged, which is the bug rather than a safe default.
func TestTreeClaimsDefaultsToTheSharedRegistry(t *testing.T) {
	d := &Deps{}
	if treeClaims(d) != woodcutplan.SharedTreeClaims() {
		t.Error("a Deps that never set Claims must de-conflict through the shared registry")
	}
	own := woodcutplan.NewTreeClaims()
	d.Claims = own
	if treeClaims(d) != own {
		t.Error("an explicit registry must be used instead of the shared one")
	}
	// The kill switch reaches even an explicitly-set registry, or an A/B arm would silently
	// measure the de-conflicted behaviour anyway.
	DeconflictTrees = false
	defer func() { DeconflictTrees = true }()
	if treeClaims(d) != nil {
		t.Error("with DeconflictTrees off there must be no registry at all")
	}
	if treeClaims(&Deps{}) != nil {
		t.Error("the kill switch must reach the shared-registry default too")
	}
}

// TestClaimantNamesTheAccount covers the identity this package claims trees under: the player
// id UPDATE_PID assigns in the engine's login sequence, which is unique per world and, unlike
// the display name, does not arrive part-way through the run and change the account's identity
// underneath its own live claim. Before it arrives the answer is "", and
// woodcutplan.PickTreeFor then falls back to plain nearest-first.
func TestClaimantNamesTheAccount(t *testing.T) {
	s := newFakeSession()
	d := &Deps{Session: s}

	if got := claimant(d); got != "" {
		t.Errorf("before login there is no identity to claim under: got %q", got)
	}
	setPID(s.state, 1234)
	if got, want := claimant(d), "pid#1234"; got != want {
		t.Errorf("after UPDATE_PID: got %q, want %q", got, want)
	}
	// Stable across calls. Deps is rebuilt on every pass by internal/bot/woodcutter.go, so an
	// identity derived from anything per-Deps would change under a live claim; this one is a
	// pure function of the session's UPDATE_PID.
	for i := 0; i < 5; i++ {
		if got := claimant(&Deps{Session: s}); got != "pid#1234" {
			t.Fatalf("identity moved between passes (call %d): %q", i, got)
		}
	}
	// Two accounts must never share a claimant string, or the registry would hand them the
	// same tree believing it was a refresh.
	other := newFakeSession()
	setPID(other.state, 1235)
	if a, b := claimant(d), claimant(&Deps{Session: other}); a == b {
		t.Errorf("two accounts produced the same claimant %q", a)
	}
	if got := claimant(&Deps{}); got != "" {
		t.Errorf("a Deps with no session must not panic or invent a name: got %q", got)
	}
}

// TestReleaseTreeOnlyGivesUpOurOwn: Chop releases unconditionally on its way out, including on
// a pick that deliberately doubled up on a tree somebody else holds. That has to be a no-op
// rather than a free gift of another account's reservation.
func TestReleaseTreeOnlyGivesUpOurOwn(t *testing.T) {
	now := time.Now()
	claims := woodcutplan.NewTreeClaims()
	s := newFakeSession()
	setPID(s.state, 77)
	d := &Deps{Session: s, Claims: claims}
	tree := tile.Tile{X: 2730, Z: 3486}

	claims.Claim(tree, "somebody-else", now)
	releaseTree(d, tree)
	if got := claims.HolderOf(tree, now); got != "somebody-else" {
		t.Errorf("releasing a tree we do not hold freed it: holder is now %q", got)
	}

	claims.Claim(tree, "pid#77", now.Add(woodcutplan.TreeClaimTTL))
	refreshTree(d, tree)
	if got := claims.HolderOf(tree, time.Now()); got != "pid#77" {
		t.Fatalf("setup: this account should hold the tree, holder is %q", got)
	}
	releaseTree(d, tree)
	if got := claims.HolderOf(tree, time.Now()); got != "" {
		t.Errorf("our own release should have freed the tree: holder is %q", got)
	}
}

// TestTheKillSwitchDoesNotCrash is the runner half of the same regression. Chop's deferred
// release runs on every exit path whether or not a claim was ever taken, so with
// DeconflictTrees off it runs against a nil registry — which panicked, and the panic was
// invisible because internal/bot/runner.go recovers it and the account just logs "stopped".
func TestTheKillSwitchDoesNotCrash(t *testing.T) {
	DeconflictTrees = false
	defer func() { DeconflictTrees = true }()
	s := newFakeSession()
	setPID(s.state, 42)
	d := &Deps{Session: s}
	tree := tile.Tile{X: 2730, Z: 3486}
	// A panic here fails the test outright; asserting no panic IS the assertion.
	releaseTree(d, tree)
	refreshTree(d, tree)
	// And with an explicit registry set as well, since the switch overrides that too.
	d.Claims = woodcutplan.NewTreeClaims()
	releaseTree(d, tree)
	refreshTree(d, tree)
	if got := d.Claims.Len(time.Now()); got != 0 {
		t.Errorf("with the switch off nothing may be written to a registry: %d claims", got)
	}
}

// fakeLocTypes stands in for the decoded config archive, which cannot be built synthetically.
// It satisfies exactly the locindex.Types interface pickTree passes *cachecfg.Config through.
type fakeLocTypes map[int]cachecfg.Loc

func (f fakeLocTypes) Loc(id int) (cachecfg.Loc, bool) { l, ok := f[id]; return l, ok }

// TestAFelledTreeLeavesTheCandidateSet is the "observed stumps matter" half of the requirement,
// and it asserts an EXISTING guarantee rather than anything added here — the reason no second
// stump mechanism was built.
//
// SCOPE, stated plainly: this exercises locindex.Find with the same query pickTree builds and
// the live loc changes pickTree feeds it. It does NOT run pickTree itself, which needs a real
// decoded *cachecfg.Config there is no synthetic constructor for. What it proves is the layer
// pickTree delegates the question to.
func TestAFelledTreeLeavesTheCandidateSet(t *testing.T) {
	const (
		treeID  = 1276 // a normal Tree
		stumpID = 1342 // what loc_change leaves behind
		shape   = 10   // centrepiece-straight: the layer a tree occupies
	)
	types := fakeLocTypes{
		treeID:  {ID: treeID, Name: "Tree", Width: 2, Length: 2, Ops: [5]string{"Chop down"}},
		stumpID: {ID: stumpID, Name: "Tree stump", Width: 2, Length: 2},
	}
	standing := locindex.Loc{ID: treeID, X: 2730, Z: 3486, Shape: shape}
	neighbour := locindex.Loc{ID: treeID, X: 2734, Z: 3486, Shape: shape}
	ix, err := locindex.New([]locindex.Loc{standing, neighbour})
	if err != nil {
		t.Fatalf("building the index: %v", err)
	}
	q := locindex.Query{X: 2730, Z: 3486, Radius: 10, Name: "Tree", Op: woodcutplan.ChopOp}

	if got := len(ix.Find(types, q, nil)); got != 2 {
		t.Fatalf("both trees should be candidates before either falls: got %d", got)
	}

	// The server fells the first one: LOC_ADD_CHANGE puts a stump on the same tile and layer.
	// This is exactly what locindex.ChangesFrom hands Find from gamestate's live loc records.
	felled := []locindex.Change{{X: 2730, Z: 3486, ID: stumpID, Shape: shape}}
	got := ix.Find(types, q, felled)
	if len(got) != 1 {
		t.Fatalf("the felled tree should be gone from the candidate set: got %d matches", len(got))
	}
	if got[0].X != neighbour.X {
		t.Errorf("the surviving candidate should be the neighbour at %d: got %d", neighbour.X, got[0].X)
	}

	// And a LOC_DEL tombstone (the layer emptied outright) has to do the same.
	deleted := []locindex.Change{{X: 2730, Z: 3486, ID: treeID, Shape: shape, Deleted: true}}
	if n := len(ix.Find(types, q, deleted)); n != 1 {
		t.Errorf("a deleted loc should leave one candidate: got %d", n)
	}
}

// setPID is UPDATE_PID, which the engine sends at login: the player id that identifies this
// account among everyone on the world.
func setPID(s *gamestate.State, pid int) {
	p := packet.NewWriter(8)
	p.P2(uint16(pid))
	p.P1(0)
	s.Handle(serverprot.UPDATE_PID, p.Data[:p.Pos])
}
