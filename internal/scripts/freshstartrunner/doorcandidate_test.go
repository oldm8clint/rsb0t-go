package freshstartrunner

import (
	"testing"

	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/tile"
)

// buildConnectedDoorPack is buildDoorPack's sibling (see doorthrough_test.go) for
// pickDoorCandidate's own tests, which — unlike pickFloodFillThrough's — need at least one
// candidate with a REAL FindPath route to dest, not just Walkable()/flood-fill checks.
// walkable marks every tile that exists at all; edges lists explicit bidirectional
// connections between two already-8-adjacent tiles, so a test builds exactly the corridor it
// needs with no incidental connectivity between tiles it never asked to be linked.
func buildConnectedDoorPack(t *testing.T, walkable [][3]int, edges [][2][3]int) *pathfind.Pack {
	t.Helper()
	const exitBytes, walkBytes = 4096, 512
	exit := make([]byte, exitBytes)
	walk := make([]byte, walkBytes)
	widx := func(x, z int) int { return (x&0x3f)*64 + (z & 0x3f) }
	for _, xz := range walkable {
		idx := widx(xz[0], xz[1])
		walk[idx>>3] |= 1 << uint(idx&7)
	}
	link := func(ax, az, bx, bz int) {
		dx, dz := bx-ax, bz-az
		for i := 0; i < 8; i++ {
			if componentDx[i] == dx && componentDz[i] == dz {
				exit[widx(ax, az)] |= 1 << uint(i)
				return
			}
		}
		t.Fatalf("buildConnectedDoorPack: %d,%d -> %d,%d is not one of the 8 adjacent directions", ax, az, bx, bz)
	}
	for _, e := range edges {
		link(e[0][0], e[0][1], e[1][0], e[1][1])
		link(e[1][0], e[1][1], e[0][0], e[0][1])
	}
	raw := append([]byte{'L', 'C', 'N', 'V', 1, 1, 0, 0, 1, 0}, []byte{0, 0, 0b0001}...)
	raw = append(raw, exit...)
	raw = append(raw, walk...)
	p, err := pathfind.Parse(raw)
	if err != nil {
		t.Fatalf("buildConnectedDoorPack: %v", err)
	}
	return p
}

// TestPickDoorCandidatePrefersConfirmedRouteOverNearestDeadEnd guards the real, live-found
// bug pickDoorCandidate was extracted to fix (2026-09-16, production account "spare 22", HCIM
// fleet, Fishing level 60 — ground-truthed offline via cmd/doordiag against the real collision
// pack; see pickDoorCandidate's own doc comment for the full incident). tryOpenDoor's old
// find()/findOpen() always grabbed whichever closed/open door was globally NEAREST to the
// account's own tile — in the real incident, a "Door" the account was literally standing on
// (distance 0) whose only unexplored side led into a small, separately sealed dead-end pocket,
// while a "Large door" about 5 tiles farther away led straight out toward the real destination
// (confirmed live via cmd/doordiag: FindPath from beside it reached Draynor in 5292 expanded
// nodes; FindPath from beside the near door failed as unreachable no matter which side was
// tried). This test reproduces that exact shape in miniature — a distance-1 door with no real
// route out on any side, and a distance-5 door whose one open side has a genuine, confirmed
// FindPath corridor all the way to dest — and checks the farther, genuinely useful door wins
// despite the nearer one being closer, which the old nearest-wins code could never do.
func TestPickDoorCandidatePrefersConfirmedRouteOverNearestDeadEnd(t *testing.T) {
	from := tile.Tile{X: 20, Z: 20, Level: 0}
	dest := tile.Tile{X: 30, Z: 20, Level: 0}

	pk := buildConnectedDoorPack(t,
		[][3]int{
			{20, 20, 0},                                                     // from itself — deliberately isolated, no exits at all
			{20, 18, 0},                                                     // the near door's one walkable side — also isolated, a genuine dead end
			{26, 20, 0}, {27, 20, 0}, {28, 20, 0}, {29, 20, 0}, {30, 20, 0}, // the far door's real corridor to dest
		},
		[][2][3]int{
			{{26, 20, 0}, {27, 20, 0}},
			{{27, 20, 0}, {28, 20, 0}},
			{{28, 20, 0}, {29, 20, 0}},
			{{29, 20, 0}, {30, 20, 0}},
		},
	)

	types := stairTypes{
		501: {ID: 501, Name: "Door", Width: 1, Length: 1, Ops: [5]string{"Open"}},
		502: {ID: 502, Name: "Door", Width: 1, Length: 1, Ops: [5]string{"Open"}},
	}
	ix, err := locindex.New([]locindex.Loc{
		{ID: 501, X: 20, Z: 19, Level: 0, Shape: 0}, // distance 1 from "from" — the dead end
		{ID: 502, X: 25, Z: 20, Level: 0, Shape: 0}, // distance 5 from "from" — the real exit
	})
	if err != nil {
		t.Fatal(err)
	}

	found, name, alreadyOpen, ok := pickDoorCandidate(pk, ix, types, nil, from, dest)
	if !ok {
		t.Fatal("pickDoorCandidate found no candidate")
	}
	if alreadyOpen {
		t.Error("neither candidate is already open")
	}
	if name != "Door" || found.ID != 502 || found.X != 25 || found.Z != 20 {
		t.Errorf("picked %+v (name %q) — want the farther door (id 502 at 25,20) with the confirmed route, not the nearer dead end (id 501)", found, name)
	}
}

// TestPickDoorCandidateFloodFillPrefersDestCloserCandidateAcrossDoors guards the same
// underlying fix from the other angle: the common multi-door-chain case where NO nearby door
// has a same-level FindPath route confirmed directly (dest is beyond more than one closed
// door), so tryOpenDoor's flood-fill exclusion strategy has to pick among several doors' own
// unreached sides. pickFloodFillThrough already broke ties toward whichever side of ONE door is
// closer to dest (see its own test); this checks that comparison now happens ACROSS every
// nearby door too, not just within the nearest one — a farther door whose own open side is
// closer to dest must win over a nearer door whose open side is not, matching the real
// Lumbridge castle incident's own chain-of-doors shape.
func TestPickDoorCandidateFloodFillPrefersDestCloserCandidateAcrossDoors(t *testing.T) {
	from := tile.Tile{X: 20, Z: 20, Level: 0}
	dest := tile.Tile{X: 40, Z: 20, Level: 0}

	pk := buildConnectedDoorPack(t,
		[][3]int{
			{20, 20, 0}, // from — isolated
			{20, 18, 0}, // near door's (id 601, at 20,19) one walkable side — isolated, far from dest
			{31, 20, 0}, // far door's (id 602, at 30,20) one walkable side — isolated, close to dest
		},
		nil, // no edges at all: nothing here has a confirmed FindPath route anywhere, so this
		// exercises strategy 2 (flood-fill exclusion) exclusively, never strategy 1.
	)

	types := stairTypes{
		601: {ID: 601, Name: "Door", Width: 1, Length: 1, Ops: [5]string{"Open"}},
		602: {ID: 602, Name: "Door", Width: 1, Length: 1, Ops: [5]string{"Open"}},
	}
	ix, err := locindex.New([]locindex.Loc{
		{ID: 601, X: 20, Z: 19, Level: 0, Shape: 0}, // distance 1 from "from"
		{ID: 602, X: 30, Z: 20, Level: 0, Shape: 0}, // distance 10 from "from", but its own side is much closer to dest
	})
	if err != nil {
		t.Fatal(err)
	}

	found, name, alreadyOpen, ok := pickDoorCandidate(pk, ix, types, nil, from, dest)
	if !ok {
		t.Fatal("pickDoorCandidate found no candidate")
	}
	if alreadyOpen {
		t.Error("neither candidate is already open")
	}
	if name != "Door" || found.ID != 602 {
		t.Errorf("picked %+v (name %q) — want the farther-from-`from` door (id 602) whose own side is closer to dest", found, name)
	}
}

// TestPickDoorCandidateStrategy1IgnoresUnreachableFarCandidate guards the second, deeper part
// of the real "lubmy basement" fix (2026-09-17, production accounts "acct"/"acct12"/
// "acct29"/"acct64", mapsquare 50,50 — reproduced live via
// cmd/fisherstrandtest -x 3209 -z 3217, confirmed offline via cmd/doordiag). An EARLIER
// version of this fix only gated Strategy 2 (the flood-fill-exclusion tie-break) on real
// room-boundary adjacency, leaving Strategy 1 (the confirmed-direct-route check) completely
// unguarded — and a live re-test after that first fix landed showed it wasn't enough:
// tryOpenDoor kept picking the exact same unreachable "Large door" forever, because Strategy
// 1 never checks whether the account can actually REACH a candidate's neighbour at all, only
// whether that neighbour has a real FindPath onward to dest. A candidate genuinely two rooms
// away can have a perfectly real corridor from ITS OWN far side to dest — that corridor is
// just as unreachable as if it didn't exist, since the account can't get to it without
// crossing at least one more closed door first — so Strategy 1 must be gated on room-boundary
// adjacency exactly the same way Strategy 2 now is.
//
// This test reproduces that exact shape: a real, explored multi-tile room (so the adjacency
// gate applies at all — see TestPickDoorCandidateFloodFillPrefersDestCloserCandidateAcrossDoors
// for why a degenerate 1-tile "room" must NOT gate), one real boundary door whose far side is
// a genuine dead end (no confirmed route — Strategy 2 must pick it), and one "Large door" that
// is NOT adjacent to the room at all but whose far side DOES have a confirmed corridor all the
// way to dest — checking the real boundary door wins despite having no confirmed route of its
// own, because the far door's confirmed route is fundamentally unreachable from here.
func TestPickDoorCandidateStrategy1IgnoresUnreachableFarCandidate(t *testing.T) {
	from := tile.Tile{X: 20, Z: 20, Level: 0}
	dest := tile.Tile{X: 30, Z: 20, Level: 0}

	pk := buildConnectedDoorPack(t,
		[][3]int{
			{20, 20, 0}, {21, 20, 0}, // the real room — two tiles, genuinely connected
			{20, 18, 0}, // the real boundary door's (id 701, at 20,19) far side — a dead end
			// the "Large door" (id 702, at 25,19) is NOT connected to the room at all, but its
			// own far side sits on a real, confirmed corridor all the way to dest:
			{25, 20, 0}, {26, 20, 0}, {27, 20, 0}, {28, 20, 0}, {29, 20, 0}, {30, 20, 0},
		},
		[][2][3]int{
			{{20, 20, 0}, {21, 20, 0}}, // the room's own only internal edge
			{{25, 20, 0}, {26, 20, 0}},
			{{26, 20, 0}, {27, 20, 0}},
			{{27, 20, 0}, {28, 20, 0}},
			{{28, 20, 0}, {29, 20, 0}},
			{{29, 20, 0}, {30, 20, 0}},
		},
	)

	types := stairTypes{
		701: {ID: 701, Name: "Door", Width: 1, Length: 1, Ops: [5]string{"Open"}},
		702: {ID: 702, Name: "Large door", Width: 1, Length: 1, Ops: [5]string{"Open"}},
	}
	ix, err := locindex.New([]locindex.Loc{
		{ID: 701, X: 20, Z: 19, Level: 0, Shape: 0}, // on the room's own boundary — a dead end beyond it
		{ID: 702, X: 25, Z: 19, Level: 0, Shape: 0}, // NOT on the room's boundary, but its far side reaches dest
	})
	if err != nil {
		t.Fatal(err)
	}

	found, name, alreadyOpen, ok := pickDoorCandidate(pk, ix, types, nil, from, dest)
	if !ok {
		t.Fatal("pickDoorCandidate found no candidate")
	}
	if alreadyOpen {
		t.Error("neither candidate is already open")
	}
	if name != "Door" || found.ID != 701 || found.X != 20 || found.Z != 19 {
		t.Errorf("picked %+v (name %q) — want the real boundary door (id 701 at 20,19), not the unreachable far \"Large door\" (id 702) whose confirmed route the account can't actually get to", found, name)
	}
}

// TestPickDoorCandidateNoneNearby checks the "nothing found at all" path callers rely on to
// treat as a plain recovery failure (tryOpenDoor returns false and lets walkNearCore's own
// STRANDED counter keep track, rather than panicking on a zero-value match).
func TestPickDoorCandidateNoneNearby(t *testing.T) {
	ix, err := locindex.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	pk := buildConnectedDoorPack(t, nil, nil)
	from := tile.Tile{X: 0, Z: 0, Level: 0}
	dest := tile.Tile{X: 10, Z: 10, Level: 0}
	if _, _, _, ok := pickDoorCandidate(pk, ix, stairTypes{}, nil, from, dest); ok {
		t.Fatal("expected no candidate when nothing is nearby")
	}
}
