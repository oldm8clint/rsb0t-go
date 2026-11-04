package questrunner

import (
	"os"
	"testing"

	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
)

// Manni's firecracker goes into the Rellekka longhall's drain pipe (loc 4162, a shape-5 wall decoration at 2663,3674).
// The engine reaches a straight wall decoration from its own tile and no other (ReachStrategy.reachWallDecor1), but
// LocStands offered the collision pack's interact tiles, the tile and its walkable neighbours nearest first, so an account
// that came from the east stood beside the pipe, sent the item, and the server ran nothing: "the pipe took no firecracker"
// (90 log lines on four accounts, 14 Oct 2026). Skipped without RSB0T_COLLISION_PACK and RSB0T_LOC_INDEX.
func TestTheLonghallPipeIsStoodOnNotBeside(t *testing.T) {
	packPath, locPath := os.Getenv("RSB0T_COLLISION_PACK"), os.Getenv("RSB0T_LOC_INDEX")
	if packPath == "" || locPath == "" {
		t.Skip("RSB0T_COLLISION_PACK or RSB0T_LOC_INDEX not set")
	}
	pk, err := pathfind.Load(packPath)
	if err != nil {
		t.Fatal(err)
	}
	ix, err := locindex.Load(locPath)
	if err != nil {
		t.Fatal(err)
	}
	var pipe locindex.Loc
	found := false
	ix.Each(questplan.LonghallPipe.X, questplan.LonghallPipe.Z, questplan.LonghallPipe.X, questplan.LonghallPipe.Z, 0, func(l locindex.Loc) {
		if l.ID == questplan.LocLonghallPipe {
			pipe, found = l, true
		}
	})
	if !found {
		t.Fatalf("loc %d is not at %d,%d in the index", questplan.LocLonghallPipe, questplan.LonghallPipe.X, questplan.LonghallPipe.Z)
	}
	if pipe.Shape != 5 {
		t.Fatalf("the pipe's shape is %d, the test is about a shape-5 wall decoration", pipe.Shape)
	}
	own := tile.Tile{X: pipe.X, Z: pipe.Z, Level: pipe.Level}
	if !pk.Walkable(own.X, own.Z, own.Level) {
		t.Fatalf("the pipe's own tile %d,%d is not walkable on the pack: an account could never stand on it", own.X, own.Z)
	}
	// What the old stands were: more than the tile itself.
	old := pk.InteractTiles(pathfind.NavPoint{X: pipe.X, Z: pipe.Z, Level: pipe.Level})
	if len(old) < 2 {
		t.Fatalf("the pack offers %d interact tiles for the pipe, so the fixture no longer shows the old bug", len(old))
	}
	d := &Deps{Pack: pk}
	east := tile.Tile{X: pipe.X + 1, Z: pipe.Z}
	got := LocStands(d, locindex.Match{Loc: pipe}, east)
	if len(got) != 1 || got[0] != own {
		t.Fatalf("stands %v (the pack's interact tiles were %v), want only the pipe's own tile %v", got, old, own)
	}
}

// A walkable wall decoration whose own tile the pack does not walk leaves the caller the orthogonal fallback rather than
// no stand at all.
func TestAWallDecorationOnAnUnwalkableTileFallsBackToItsNeighbours(t *testing.T) {
	packPath := os.Getenv("RSB0T_COLLISION_PACK")
	if packPath == "" {
		t.Skip("RSB0T_COLLISION_PACK not set")
	}
	pk, err := pathfind.Load(packPath)
	if err != nil {
		t.Fatal(err)
	}
	// 0,0 is off the map: nothing there is walkable.
	d := &Deps{Pack: pk}
	got := LocStands(d, locindex.Match{Loc: locindex.Loc{ID: 4162, X: 1, Z: 1, Shape: 5}}, tile.Tile{X: 1, Z: 1})
	if len(got) != 4 {
		t.Fatalf("stands %v, want the four orthogonal neighbours as the last resort", got)
	}
}
