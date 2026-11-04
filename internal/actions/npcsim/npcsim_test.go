package npcsim

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/tile"
)

func rig(t *testing.T) (*Engine, *actionsim.Session, *chatlog.Log) {
	t.Helper()
	pack, err := OpenPack([2]int{40, 48})
	if err != nil {
		t.Fatal(err)
	}
	sim := actionsim.New(actionsim.Objs{}, nil)
	chat := chatlog.New()
	e := New(sim, chat, pack, time.Millisecond)
	return e, sim, chat
}

func npcTile(t *testing.T, sim *actionsim.Session, index int) (tile.Tile, bool) {
	t.Helper()
	for _, n := range sim.State().NPCs() {
		if n.Index == index {
			return tile.Tile{X: n.X, Z: n.Z, Level: n.Level}, true
		}
	}
	return tile.Tile{}, false
}

// The NPC is in the client's view where it stands, and it keeps that tile across a rebuild (a walk or a teleport of the account).
func TestAnNpcIsSeenWhereItStandsAndSurvivesTheAccountMoving(t *testing.T) {
	e, sim, _ := rig(t)
	e.Stand(tile.Tile{X: 2596, Z: 3088})
	e.Spawn(7, 1234, tile.Tile{X: 2592, Z: 3091})
	if at, ok := npcTile(t, sim, 7); !ok || at != (tile.Tile{X: 2592, Z: 3091}) {
		t.Fatalf("npc at %v (seen %v)", at, ok)
	}
	e.Stand(tile.Tile{X: 2640, Z: 3130})
	e.Stand(tile.Tile{X: 2590, Z: 3090})
	if at, ok := npcTile(t, sim, 7); !ok || at != (tile.Tile{X: 2592, Z: 3091}) {
		t.Errorf("after the account moved the npc is at %v (seen %v)", at, ok)
	}
	e.MoveNPC(7, tile.Tile{X: 2590, Z: 3094})
	if at, _ := npcTile(t, sim, 7); at != (tile.Tile{X: 2590, Z: 3094}) {
		t.Errorf("after MoveNPC the npc is at %v", at)
	}
	e.Spawn(8, 99, tile.Tile{X: 2588, Z: 3090})
	e.MoveNPC(7, tile.Tile{X: 2591, Z: 3092})
	a, _ := npcTile(t, sim, 7)
	b, _ := npcTile(t, sim, 8)
	if a != (tile.Tile{X: 2591, Z: 3092}) || b != (tile.Tile{X: 2588, Z: 3090}) {
		t.Errorf("two npcs: %v and %v", a, b)
	}
}

// The rule the engine is for: a click is accepted from a tile that touches the NPC orthogonally, and from nowhere else -- not
// from two tiles away, not from a diagonal, not from its own tile.
func TestAnOptionIsAcceptedOnlyFromATouchingTile(t *testing.T) {
	cases := []struct {
		name string
		from tile.Tile
		want bool
	}{
		{"beside", tile.Tile{X: 2593, Z: 3091}, true},
		{"above", tile.Tile{X: 2592, Z: 3092}, true},
		{"two away", tile.Tile{X: 2594, Z: 3091}, false},
		{"diagonal", tile.Tile{X: 2593, Z: 3092}, false},
		{"under it", tile.Tile{X: 2592, Z: 3091}, false},
		{"far", tile.Tile{X: 2596, Z: 3088}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, sim, chat := rig(t)
			e.Stand(c.from)
			e.Spawn(7, 1234, tile.Tile{X: 2592, Z: 3091})
			var accepted []Click
			e.OnOp = func(cl Click) { accepted = append(accepted, cl) }
			if err := actions.OpNpc(sim, 3, 7); err != nil {
				t.Fatal(err)
			}
			e.Step()
			e.Step()
			clicks := e.Clicks()
			if len(clicks) != 1 || clicks[0].Reached != c.want || clicks[0].Option != 3 || clicks[0].Index != 7 {
				t.Fatalf("clicks %+v", clicks)
			}
			if got := len(accepted) == 1; got != c.want {
				t.Errorf("OnOp ran %d time(s), want accepted=%v", len(accepted), c.want)
			}
			said := strings.Contains(chat.Since(time.Time{}), CantReach)
			if said == c.want {
				t.Errorf("the refusal line was said=%v, want %v", said, !c.want)
			}
		})
	}
}

// A click on an NPC that is not in view is not a click.
func TestAClickOnAnNpcOutOfViewDoesNothing(t *testing.T) {
	e, sim, _ := rig(t)
	e.Stand(tile.Tile{X: 2593, Z: 3091})
	if err := actions.OpNpc(sim, 3, 55); err != nil {
		t.Fatal(err)
	}
	e.Step()
	if len(e.Clicks()) != 0 {
		t.Errorf("clicks %+v", e.Clicks())
	}
}

// A walk packet moves the account to its last waypoint, as many ticks later as the route is long, and the walk is dropped while
// the player is delayed.
func TestAWalkArrivesAfterTheRouteIsLongAndIsDroppedWhileDelayed(t *testing.T) {
	e, sim, _ := rig(t)
	e.Stand(tile.Tile{X: 2596, Z: 3088})
	send := func(to tile.Tile) {
		t.Helper()
		if err := actions.Walk(sim, []pathfind.NavPoint{{X: 2596, Z: 3088}, {X: to.X, Z: to.Z}}, false); err != nil {
			t.Fatal(err)
		}
	}
	send(tile.Tile{X: 2592, Z: 3090})
	e.Step() // read: the route is 4 long, arrival at tick 1+4
	for i := 0; i < 3; i++ {
		e.Step()
		if h := e.Here(); h != (tile.Tile{X: 2596, Z: 3088}) {
			t.Fatalf("tick %d: already at %v", e.Tick(), h)
		}
	}
	e.Step()
	if h := e.Here(); h != (tile.Tile{X: 2592, Z: 3090}) {
		t.Fatalf("not arrived: %v", h)
	}
	if x, z, _, ok := sim.State().Tile(); !ok || x != 2592 || z != 3090 {
		t.Errorf("the client sees %d,%d", x, z)
	}
	e.Delay(3)
	send(tile.Tile{X: 2595, Z: 3090})
	e.Step()
	if len(e.Walks()) != 1 {
		t.Errorf("a walk read in a delay was kept: %v", e.Walks())
	}
}

// A click read in a delay is dropped, and one scheduled with After runs on its tick.
func TestAClickInADelayIsDroppedAndEventsRunOnTheirTick(t *testing.T) {
	e, sim, _ := rig(t)
	e.Stand(tile.Tile{X: 2593, Z: 3091})
	e.Spawn(7, 1234, tile.Tile{X: 2592, Z: 3091})
	called := 0
	e.OnOp = func(Click) { called++ }
	e.Delay(2)
	if err := actions.OpNpc(sim, 3, 7); err != nil {
		t.Fatal(err)
	}
	e.Step()
	cl := e.Clicks()
	if len(cl) != 1 || !cl[0].Dropped || cl[0].Reached || called != 0 {
		t.Fatalf("clicks %+v, accepted %d", cl, called)
	}
	ran := 0
	e.After(2, func() { ran++ })
	e.Step()
	if ran != 0 {
		t.Fatal("ran early")
	}
	e.Step()
	if ran != 1 {
		t.Fatalf("ran %d times", ran)
	}
	e.Teleport(1, tile.Tile{X: 2891, Z: 4830})
	e.Step()
	e.Step()
	if x, z, _, _ := sim.State().Tile(); x != 2891 || z != 4830 {
		t.Errorf("teleported to %d,%d", x, z)
	}
}

// A solid tile cannot be stood on or stepped through, and a wall on a face stops the reach across it and the step across it, the
// way the real pack's walls do.
func TestTerrainCutsTheReachAndTheStep(t *testing.T) {
	npc := tile.Tile{X: 2592, Z: 3091}
	pack, err := OpenPackWith(Terrain{
		Solid: []tile.Tile{{X: 2592, Z: 3092}},
		Walls: []Wall{{At: npc, Face: 1}}, // a wall between the NPC and the tile east of him
	}, [2]int{40, 48})
	if err != nil {
		t.Fatal(err)
	}
	nav := func(t tile.Tile) pathfind.NavPoint { return pathfind.NavPoint{X: t.X, Z: t.Z, Level: t.Level} }
	if pack.Walkable(2592, 3092, 0) || !pack.Walkable(2592, 3090, 0) {
		t.Error("the solid tile is walkable, or its neighbour is not")
	}
	east, west, south := tile.Tile{X: 2593, Z: 3091}, tile.Tile{X: 2591, Z: 3091}, tile.Tile{X: 2592, Z: 3090}
	if pack.CanInteractFrom(nav(east), nav(npc)) {
		t.Error("a click is taken across a wall")
	}
	if !pack.CanInteractFrom(nav(west), nav(npc)) || !pack.CanInteractFrom(nav(south), nav(npc)) {
		t.Error("a click is refused from an open side")
	}
	// The wall also stops the step: east to west takes the long way round, not two tiles.
	res := pack.FindPath(nav(east), nav(west))
	if !res.OK || res.Steps() <= 2 {
		t.Errorf("east to west past the wall: ok=%v steps=%d", res.OK, res.Steps())
	}
	// Nothing walks onto the solid tile and the pack still holds the other mapsquares' ground the same.
	if res := pack.FindPath(nav(west), nav(tile.Tile{X: 2592, Z: 3092})); res.OK && res.Steps() == 0 {
		t.Errorf("a path to a solid tile: %+v", res)
	}
}

// A walled room cannot be entered across its corner: a diagonal step needs every cardinal step round the corner it cuts, as the
// engine's canTravel does (the baked pack clears the bit), so the room the tests wall off is sealed.
func TestAWalledRoomCannotBeEnteredAcrossItsCorner(t *testing.T) {
	centre := tile.Tile{X: 2592, Z: 3091}
	var walls []Wall
	for d := -1; d <= 1; d++ {
		walls = append(walls,
			Wall{At: tile.Tile{X: centre.X + d, Z: centre.Z + 1}, Face: 0}, Wall{At: tile.Tile{X: centre.X + d, Z: centre.Z - 1}, Face: 2},
			Wall{At: tile.Tile{X: centre.X + 1, Z: centre.Z + d}, Face: 1}, Wall{At: tile.Tile{X: centre.X - 1, Z: centre.Z + d}, Face: 3})
	}
	pack, err := OpenPackWith(Terrain{Walls: walls}, [2]int{40, 48})
	if err != nil {
		t.Fatal(err)
	}
	from := pathfind.NavPoint{X: 2596, Z: 3088}
	for x := centre.X - 1; x <= centre.X+1; x++ {
		for z := centre.Z - 1; z <= centre.Z+1; z++ {
			res := pack.FindPath(from, pathfind.NavPoint{X: x, Z: z})
			if res.OK && res.Waypoints[len(res.Waypoints)-1].X == x && res.Waypoints[len(res.Waypoints)-1].Z == z {
				t.Errorf("%d,%d inside the walled room can be walked to from outside: %+v", x, z, res)
			}
		}
	}
	// The room's own floor is one piece: inside, every tile reaches every other.
	in := pathfind.NavPoint{X: centre.X - 1, Z: centre.Z - 1}
	if res := pack.FindPath(in, pathfind.NavPoint{X: centre.X + 1, Z: centre.Z + 1}); !res.OK || res.Steps() != 2 {
		t.Errorf("across the room: %+v", res)
	}
}
