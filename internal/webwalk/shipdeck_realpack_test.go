package webwalk

import (
	"testing"

	"rsb0t-go/internal/randomevents"
)

// TestEveryTileOfEveryShipDeckPlansOffByItsGangplank plans from each walkable tile of the three ship decks the strand
// catalogue holds (Ardougne, where the Brimhaven ferry lands; Brimhaven, where the Ardougne ferry does; Musa Point, where
// the Port Sarim one does) on the real 289 collision pack and the Default graph. The Walker stopped refusing walks from a
// deck on this proof: from every tile of every deck the planner finds the way off, and its first hop is the deck's own
// gangplank (Cross), so a script's walk needs no help from the Guardian to leave.
//
// Needs RSB0T_COLLISION_PACK; without it this skips.
func TestEveryTileOfEveryShipDeckPlansOffByItsGangplank(t *testing.T) {
	p := realPack(t)
	decks := []struct {
		name      string
		tile      Point // a tile of the deck, to find its catalogue entry
		gangplank int   // the loc the first hop must operate
		far       Point // a destination well away from the dock
	}{
		{"the Ardougne ship deck", Point{X: 2683, Z: 3268, Level: 1}, 2086, Point{X: 2655, Z: 3283}},
		{"the Brimhaven ship deck", Point{X: 2776, Z: 3235, Level: 1}, 2088, Point{X: 2956, Z: 3146}},
		{"the Musa Point ship deck", Point{X: 2956, Z: 3143, Level: 1}, 2082, Point{X: 2773, Z: 3234}},
	}
	for _, d := range decks {
		site, ok := randomevents.StrandSiteAt(d.tile.X, d.tile.Z, d.tile.Level)
		if !ok || site.Name != d.name || !site.Boarding {
			t.Fatalf("%s: the catalogue has %q (boarding %v) at %s", d.name, site.Name, site.Boarding, d.tile)
		}
		planned := 0
		for _, area := range site.Areas {
			for x := area.MinX; x <= area.MaxX; x++ {
				for z := area.MinZ; z <= area.MaxZ; z++ {
					from := Point{X: x, Z: z, Level: area.Level}
					if !p.col.Walkable(x, z, area.Level) {
						continue
					}
					planned++
					for _, to := range []Point{d.far} {
						r := p.Plan(from, to, Options{AllowNPC: true, AllowSpecial: true})
						if !r.OK {
							t.Errorf("%s: no route from %s to %s: %s", d.name, from, to, r.Reason)
							continue
						}
						hops := r.Hops()
						if len(hops) == 0 || hops[0].Kind != KindGangplank || hops[0].LocID != d.gangplank || hops[0].Action != "Cross" {
							t.Errorf("%s: the route from %s to %s starts %s, want the gangplank %d", d.name, from, to, r.String(), d.gangplank)
						}
					}
				}
			}
		}
		if planned == 0 {
			t.Errorf("%s: no walkable tile on the deck in the pack", d.name)
		}
		t.Logf("%s: %d walkable deck tiles all plan off by gangplank %d", d.name, planned, d.gangplank)
	}
}
