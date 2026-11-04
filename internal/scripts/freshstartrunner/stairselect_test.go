package freshstartrunner

import (
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/tile"
)

// stairTypes stands in for a decoded config archive, the same shape locindex's own tests
// use (locindex_test.go's fakeTypes), duplicated here since that one is unexported in a
// different package.
type stairTypes map[int]cachecfg.Loc

func (f stairTypes) Loc(id int) (cachecfg.Loc, bool) {
	l, ok := f[id]
	return l, ok
}

// TestPickDirectionalMatchPrefersDirectionOverDistance guards the real, live-found bug
// this function was extracted to fix (2026-09-16, production accounts "oi" and "acct17",
// independently): both got permanently stuck at 2699,3406, level 2, needing to reach level
// 0 (i.e. go DOWN), one tile from a "Ladder" whose only option is "Climb-up" and about six
// tiles from a SEPARATE "Ladder" id whose only option is "Climb-down" — tryChangeLevel's own
// doc comment has the full incident. The old flattened search (every name x every op, keep
// whichever single match is globally nearest) picked the closer Climb-up ladder even though
// stairOpsDown lists "Climb-up" only as a last-resort fallback, since raw distance decided
// the winner with no regard for which op tier actually matched. This test reproduces that
// exact shape — a near wrong-direction fixture and a far right-direction one — and checks
// the direction-preferred op tier wins regardless of which one is physically closer.
func TestPickDirectionalMatchPrefersDirectionOverDistance(t *testing.T) {
	types := stairTypes{
		1747: {ID: 1747, Name: "Ladder", Width: 1, Length: 1, Ops: [5]string{"Climb-up"}},
		1746: {ID: 1746, Name: "Ladder", Width: 1, Length: 1, Ops: [5]string{"Climb-down"}},
	}
	ix, err := locindex.New([]locindex.Loc{
		{ID: 1747, X: 2699, Z: 3405, Level: 2, Shape: 0}, // 1 tile from "from" — Climb-up only
		{ID: 1746, X: 2704, Z: 3403, Level: 2, Shape: 0}, // ~6 tiles from "from" — Climb-down
	})
	if err != nil {
		t.Fatal(err)
	}
	from := tile.Tile{X: 2699, Z: 3406, Level: 2}

	// Descending (dest.Level 0 < from.Level 2): stairOpsDown must pick the FAR Climb-down
	// ladder over the NEAR Climb-up one.
	found, name := pickDirectionalMatch(ix, types, nil, from, stairNames, stairOpsDown, 25)
	if found == nil {
		t.Fatal("expected a match, got none")
	}
	if name != "Ladder" || found.ID != 1746 || found.X != 2704 || found.Z != 3403 {
		t.Errorf("descending picked %+v (name %q) — want the far Climb-down ladder id 1746 at 2704,3403", found, name)
	}

	// Ascending (dest.Level > from.Level): stairOpsUp must pick the NEAR Climb-up ladder —
	// this direction genuinely prefers the closer fixture, since it's also the correct one.
	found, name = pickDirectionalMatch(ix, types, nil, from, stairNames, stairOpsUp, 25)
	if found == nil {
		t.Fatal("expected a match, got none")
	}
	if name != "Ladder" || found.ID != 1747 || found.X != 2699 || found.Z != 3405 {
		t.Errorf("ascending picked %+v (name %q) — want the near Climb-up ladder id 1747 at 2699,3405", found, name)
	}
}

// TestPickDirectionalMatchStillPrefersNearestNameWithinATier guards the earlier, already-
// fixed bug tryChangeLevel's own doc comment describes (2026-09-15, cyclev2_9): within a
// single op tier, the search must still consider every name and take the nearest one, not
// just the first name in stairNames that happens to match anything at all.
func TestPickDirectionalMatchStillPrefersNearestNameWithinATier(t *testing.T) {
	types := stairTypes{
		2000: {ID: 2000, Name: "Staircase", Width: 1, Length: 1, Ops: [5]string{"Climb-down"}},
		2001: {ID: 2001, Name: "Ladder", Width: 1, Length: 1, Ops: [5]string{"Climb-down"}},
	}
	ix, err := locindex.New([]locindex.Loc{
		{ID: 2000, X: 2725, Z: 3491, Level: 0, Shape: 0}, // far Staircase — sorts first in stairNames
		{ID: 2001, X: 2711, Z: 3471, Level: 0, Shape: 0}, // near Ladder
	})
	if err != nil {
		t.Fatal(err)
	}
	from := tile.Tile{X: 2711, Z: 3472, Level: 0}
	found, name := pickDirectionalMatch(ix, types, nil, from, stairNames, stairOpsDown, 25)
	if found == nil {
		t.Fatal("expected a match, got none")
	}
	if name != "Ladder" || found.ID != 2001 {
		t.Errorf("picked %+v (name %q) — want the nearer Ladder, id 2001, despite Staircase sorting first", found, name)
	}
}

// TestPickDirectionalMatchFallsBackWhenPreferredTierHasNothing checks that a lower-priority
// op tier is only ever used when every higher-priority tier found nothing at all — not
// merely "something farther away."
func TestPickDirectionalMatchFallsBackWhenPreferredTierHasNothing(t *testing.T) {
	types := stairTypes{
		3000: {ID: 3000, Name: "Ladder", Width: 1, Length: 1, Ops: [5]string{"Climb-up"}},
	}
	ix, err := locindex.New([]locindex.Loc{
		{ID: 3000, X: 2700, Z: 3400, Level: 2, Shape: 0}, // Climb-up only, no Climb-down anywhere
	})
	if err != nil {
		t.Fatal(err)
	}
	from := tile.Tile{X: 2700, Z: 3401, Level: 2}
	found, name := pickDirectionalMatch(ix, types, nil, from, stairNames, stairOpsDown, 25)
	if found == nil {
		t.Fatal("expected the fallback Climb-up ladder to be picked, got none")
	}
	if name != "Ladder" || found.ID != 3000 {
		t.Errorf("picked %+v (name %q) — want the only available ladder as a last-resort fallback", found, name)
	}
}

// TestLeftPocket guards tryEscapeKnownPocket's own success check. Originally this checked
// ONLY a mapsquare change (a z+6400-offset pocket shares its surface counterpart's own level
// NUMBER — see tryEscapeKnownPocket's own doc comment) — exercised below by the real
// lubmy-basement incident's own numbers (mapsquare 50,150 -> 50,50, a 6400-tile jump in z).
// Real, live-found gap in that original version (2026-09-17, account "acct", Ardougne
// Monastery ground floor — see leftPocket's own doc comment for the full incident): an
// ORDINARY surface building's staircase to the room directly above changes LEVEL, not
// mapsquare, and the mapsquare-only check misreported that real, successful climb as a
// failure. The last two cases below lock that fix in: a level change alone must now count as
// an escape too, while ordinary same-level in-room movement still must not.
func TestLeftPocket(t *testing.T) {
	before := tile.Tile{X: 3212, Z: 9615, Level: 0}
	cases := []struct {
		name  string
		after tile.Tile
		want  bool
	}{
		{"the real lubmy-basement-to-surface jump", tile.Tile{X: 3218, Z: 3218, Level: 0}, true},
		{"ordinary in-room movement, same mapsquare", tile.Tile{X: 3215, Z: 9618, Level: 0}, false},
		{"the exact same tile", before, false},
		{"a different mapsquare in x only", tile.Tile{X: 3300, Z: 9615, Level: 0}, true},
		{"the real jagex incident: an ordinary staircase climb, same mapsquare, level 0->1", tile.Tile{X: 3212, Z: 9615, Level: 1}, true},
		{"same mapsquare and level, only x/z jitter (not a real climb)", tile.Tile{X: 3213, Z: 9616, Level: 0}, false},
	}
	for _, tc := range cases {
		if got := leftPocket(before, tc.after); got != tc.want {
			t.Errorf("%s: leftPocket(%+v, %+v) = %v, want %v", tc.name, before, tc.after, got, tc.want)
		}
	}
}

// TestPickDirectionalMatchNoneQualify checks the "found nothing at all" path callers rely on
// to move on to their own next fallback (e.g. tryChangeLevel's gangplank search).
func TestPickDirectionalMatchNoneQualify(t *testing.T) {
	ix, err := locindex.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	from := tile.Tile{X: 0, Z: 0, Level: 0}
	found, name := pickDirectionalMatch(ix, stairTypes{}, nil, from, stairNames, stairOpsDown, 25)
	if found != nil || name != "" {
		t.Errorf("expected no match, got %+v (name %q)", found, name)
	}
}
