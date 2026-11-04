package crafterplan

import (
	"testing"

	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
)

func TestChooseSheepSkipsShornFarAndSkipped(t *testing.T) {
	me := tile.Tile{X: 3200, Z: 3265}
	sheep := []Sheep{
		{Index: 1, Type: SheepShorn, Tile: tile.Tile{X: 3200, Z: 3266}},  // nearest, but shorn
		{Index: 2, Type: SheepWoolly, Tile: tile.Tile{X: 3212, Z: 3265}}, // 12 away, out of range
		{Index: 3, Type: SheepWoolly, Tile: tile.Tile{X: 3203, Z: 3265}}, // skipped
		{Index: 4, Type: SheepWoolly, Tile: tile.Tile{X: 3205, Z: 3268}},
		{Index: 5, Type: SheepWoolly, Tile: tile.Tile{X: 3201, Z: 3265, Level: 1}},
	}
	skipped := func(index int) bool { return index == 3 }
	if got := ChooseSheep(sheep, me, skipped); got != 3 {
		t.Errorf("ChooseSheep = %d, want index 3 (sheep 4)", got)
	}
	if got := ChooseSheep(sheep[:2], me, nil); got != -1 {
		t.Errorf("ChooseSheep with only shorn/far sheep = %d, want -1", got)
	}
}

func TestWantsShears(t *testing.T) {
	if !WantsShears(false, 1) || WantsShears(true, 1) || WantsShears(false, BowStringLevel) {
		t.Error("shears are wanted only while missing and below crafting 10")
	}
}

func TestDecideWool(t *testing.T) {
	cases := []struct {
		wool int
		full bool
		want WoolAction
	}{{5, true, WoolSpin}, {0, true, WoolMakeRoom}, {5, false, WoolShear}, {0, false, WoolShear}}
	for _, c := range cases {
		if got := DecideWool(c.wool, c.full); got != c.want {
			t.Errorf("DecideWool(%d, %v) = %s, want %s", c.wool, c.full, got, c.want)
		}
	}
}

func TestJunk(t *testing.T) {
	pack := []freshstart.PackItem{{Name: "Coins", Count: 5}, {Name: "Shears", Count: 1}, {Name: "Wool", Count: 1}, {Name: "Bronze dagger", Count: 1}}
	if got := Junk(pack); got != 3 {
		t.Errorf("Junk = %d, want 3", got)
	}
	if got := Junk(pack[:3]); got != -1 {
		t.Errorf("Junk of kept things = %d, want -1", got)
	}
}

func TestLumbridgeRoutes(t *testing.T) {
	l := Lumbridge()
	if !l.InPen(tile.Tile{X: 3200, Z: 3265}) || l.InPen(l.PenOutside) || l.InPen(tile.Tile{X: 3200, Z: 3265, Level: 1}) {
		t.Error("InPen is the pen's ground-floor box")
	}
	far := tile.Tile{X: 3222, Z: 3218}
	beside := tile.Tile{X: 3206, Z: 3209}
	if got := RouteUpstairs(far, l.Staircase, 1); got != MoveWalk {
		t.Errorf("RouteUpstairs(far) = %s", got)
	}
	if got := RouteUpstairs(beside, l.Staircase, 1); got != MoveClimbUp {
		t.Errorf("RouteUpstairs(beside) = %s", got)
	}
	if got := RouteUpstairs(tile.Tile{X: 3205, Z: 3209, Level: 2}, l.Staircase, 1); got != MoveClimbDown {
		t.Errorf("RouteUpstairs(level 2) = %s", got)
	}
	if got := RouteUpstairs(l.Wheel, l.Staircase, 1); got != MoveHere {
		t.Errorf("RouteUpstairs(level 1) = %s", got)
	}
	if got := RouteDownstairs(l.Wheel, l.Staircase); got != MoveWalk {
		t.Errorf("RouteDownstairs(at the wheel) = %s", got)
	}
	if got := RouteDownstairs(tile.Tile{X: 3205, Z: 3208, Level: 1}, l.Staircase); got != MoveClimbDown {
		t.Errorf("RouteDownstairs(beside, level 1) = %s", got)
	}
	if got := RouteDownstairs(far, l.Staircase); got != MoveHere {
		t.Errorf("RouteDownstairs(ground) = %s", got)
	}
}
