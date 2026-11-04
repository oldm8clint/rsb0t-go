package fleetrunner

import (
	"os"
	"testing"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/thieverplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// TestTravelLandingsWalkHomeWithoutAShip checks teleport travel's side model on the real collision
// pack and transport graph, with Stage 3's own wolf avoids: from every landing ChooseTravelTeleport
// may pick for a ScarabSwarm destination on its side, the walk on is a land walk (no ship, no
// gangplank) and much shorter than walking there from the other side, which needs the ships.
// Skips without RSB0T_COLLISION_PACK.
func TestTravelLandingsWalkHomeWithoutAShip(t *testing.T) {
	path := os.Getenv("RSB0T_COLLISION_PACK")
	if path == "" {
		t.Skip("RSB0T_COLLISION_PACK not set")
	}
	pk, err := pathfind.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	g, err := webwalk.Default()
	if err != nil {
		t.Fatal(err)
	}
	pl := webwalk.NewPlanner(pk, g)
	opt := webwalk.Options{AvoidZones: AvoidRects()}
	pt := func(t tile.Tile) webwalk.Point { return webwalk.Point{X: t.X, Z: t.Z} }

	west := map[string]tile.Tile{
		"the Catherby mule post":  {X: 2809, Z: 3441},
		"the East Ardougne bank":  thieverplan.ArdougneBank,
		"the Seers' Village bank": {X: 2725, Z: 3492},
	}
	east := map[string]tile.Tile{"Zaff": fleetplan.ZaffStand}
	var camelot, ardougne, varrock fleetplan.TravelTeleport
	for _, tp := range fleetplan.TravelTeleports() {
		switch tp.Spell.Name {
		case "Camelot Teleport":
			camelot = tp
		case "Ardougne Teleport":
			ardougne = tp
		case "Varrock Teleport":
			varrock = tp
		}
	}
	landing := func(tp fleetplan.TravelTeleport) tile.Tile { return tile.Tile{X: tp.DestX, Z: tp.DestZ} }
	landWalk := func(name string, from, to tile.Tile) int {
		t.Helper()
		r := pl.Plan(pt(from), pt(to), opt)
		if !r.OK {
			t.Errorf("%s: no route from %v to %v (%s)", name, from, to, r.Reason)
			return 0
		}
		for _, s := range r.Waypoints {
			if s.Hop != nil && (s.Hop.Kind == webwalk.KindShip || s.Hop.Kind == webwalk.KindGangplank) {
				t.Errorf("%s: the walk from %v to %v takes a %s (%s)", name, from, to, s.Hop.Kind, s.Hop.LocName)
			}
		}
		return r.Cost
	}
	for name, dest := range west {
		for _, tp := range []fleetplan.TravelTeleport{camelot, ardougne} {
			from := landing(tp)
			cost := landWalk(tp.Spell.Name+" to "+name, from, dest)
			far := pl.Plan(pt(landing(varrock)), pt(dest), opt)
			if !far.OK {
				t.Errorf("%s from Varrock: no route (%s)", name, far.Reason)
				continue
			}
			t.Logf("%s: from %s costs %d, from the Varrock landing %d", name, tp.Spell.Name, cost, far.Cost)
			if far.Cost-cost < 150 {
				t.Errorf("%s: from %s costs %d, from Varrock %d: the teleport saves too little to be worth a law", name, tp.Spell.Name, cost, far.Cost)
			}
		}
	}
	for name, dest := range east {
		landWalk("Varrock Teleport to "+name, landing(varrock), dest)
	}
}
