package locindex

import (
	"testing"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/pathfind"
)

// An opened door is the map's closed leaf gone and the open leaf added a tile along, turned a quarter: the base wall
// comes off its tile and the new one goes on its own.
func TestWallEditsRemoveTheClosedLeafAndAddTheOpenOne(t *testing.T) {
	ix, err := New([]Loc{
		{ID: 1530, X: 3082, Z: 3427, Level: 0, Shape: 0, Angle: 3}, // a shut door, blocks walking
		{ID: 77, X: 3082, Z: 3428, Level: 0, Shape: 10, Angle: 0},  // a table on the next tile: not a wall, left alone
		{ID: 1551, X: 3090, Z: 3427, Level: 0, Shape: 0, Angle: 1}, // a wall nobody touched
	})
	if err != nil {
		t.Fatal(err)
	}
	wallTypes := fakeTypes{1530: {BlockWalk: true}, 1531: {BlockWalk: true}, 77: {BlockWalk: true}, 1551: {BlockWalk: true}, 9: {}}
	got := WallEdits(ix, wallTypes, []gamestate.LocInstance{
		{Type: -1, Shape: 0, Rotate: 3, X: 3082, Z: 3427, Level: 0, Deleted: true},
		{Type: 1531, Shape: 0, Rotate: 0, X: 3082, Z: 3426, Level: 0},
	})
	want := []pathfind.WallEdit{
		{X: 3082, Z: 3427, Level: 0, Shape: 0, Angle: 3},
		{X: 3082, Z: 3426, Level: 0, Shape: 0, Angle: 0, Add: true},
	}
	if len(got) != len(want) {
		t.Fatalf("WallEdits = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("edit %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// Only the two straight wall shapes are modelled, and only what blocks walking.
func TestWallEditsIgnoreWhatThePackCannotModel(t *testing.T) {
	ix, err := New([]Loc{{ID: 1530, X: 10, Z: 10, Level: 0, Shape: 9, Angle: 1}}) // a diagonal door
	if err != nil {
		t.Fatal(err)
	}
	wallTypes := fakeTypes{1530: {BlockWalk: true}, 5: {BlockWalk: false}}
	got := WallEdits(ix, wallTypes, []gamestate.LocInstance{
		{Type: -1, Shape: 9, Rotate: 1, X: 10, Z: 10, Deleted: true}, // the diagonal leaf leaving
		{Type: 5, Shape: 0, Rotate: 0, X: 11, Z: 10},                 // a wall that does not block walking
		{Type: 1530, Shape: 22, Rotate: 0, X: 12, Z: 10},             // ground decoration
	})
	if len(got) != 0 {
		t.Errorf("WallEdits = %+v, want none", got)
	}
}
