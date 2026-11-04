package webwalk

import (
	"testing"

	"rsb0t-go/internal/randomevents"
)

// TestVarrockSewerCavernHasAWalkOutAndIsNoGuardianPocket is the walker's half of the 14 Oct 2026 finding (build d5362bb2, 04:07-04:45): the
// "underground pocket below Varrock" the Guardian held three accounts in was the sewers' north cavern, 217 tiles that the walker crosses on
// its own, door 1530 at 3241,9910 on one side and the web 733 at 3210,9898 on the other, on the way to the moss giants (the account that
// resumed at 3202,9909 had a 123-waypoint route out the moment it logged in; it was the Guardian's void hops that kept it there).
//
// On the real 289 pack and the Default graph: from the tiles of the log a route exists to Peksa in Varrock and to the moss giants, it
// leaves the cavern by the door or the web and never by the north ladder (whose top, 3230,3505, is a 33-tile pocket walled by the unnamed
// loc 1902), and none of the tiles is one the Guardian owns or the walker must plan from with the zones off.
//
// Needs RSB0T_COLLISION_PACK; without it this skips.
func TestVarrockSewerCavernHasAWalkOutAndIsNoGuardianPocket(t *testing.T) {
	p := realPack(t)
	from := []Point{{X: 3202, Z: 9909}, {X: 3203, Z: 9910}, {X: 3204, Z: 9909}, {X: 3206, Z: 9908}, {X: 3230, Z: 9905}, {X: 3237, Z: 9907}, {X: 3239, Z: 9907}}
	for _, f := range from {
		if name, pocket := randomevents.PocketAt(f.X, f.Z, f.Level); pocket {
			t.Errorf("%s is a pocket (%s): a walk from it would plan with the zones off", f, name)
		}
		if name, owned := randomevents.StrandOwner(f.X, f.Z, f.Level); owned {
			t.Errorf("%s is owned by the Guardian's ladder (%s)", f, name)
		}
		if name, planned := randomevents.StrandPlanned(f.X, f.Z, f.Level); planned {
			t.Errorf("%s is planned by the Guardian (%s): the walker would refuse to start from it", f, name)
		}

		// Out to the surface, Peksa's shop (the log's destination).
		r := p.Plan(f, Point{X: 3076, Z: 3429}, Options{})
		if !r.OK {
			t.Errorf("no route from %s to Peksa: %s", f, r.Reason)
			continue
		}
		leaves := false
		for _, h := range r.Hops() {
			if h.LocID == 1530 && h.LocX == 3241 && h.LocZ == 9910 || h.LocID == 733 && h.LocX == 3210 && h.LocZ == 9898 {
				leaves = true
			}
			if h.LocID == 1755 && h.LocX == 3230 && h.LocZ == 9904 {
				t.Errorf("the route from %s to Peksa takes the north ladder, which comes up in a walled 33-tile pocket:\n%s", f, r.String())
			}
		}
		if !leaves {
			t.Errorf("the route from %s to Peksa leaves the cavern by neither its door nor its web:\n%s", f, r.String())
		}

		// On to the moss giants, through the web.
		m := p.Plan(f, Point{X: 3159, Z: 9900}, Options{})
		if !m.OK {
			t.Errorf("no route from %s to the moss giants: %s", f, m.Reason)
		}
	}
}

// routeTiles is every tile a route stands on: the tiles of its walks, and the stand and landing of each hop.
func routeTiles(r Route) []Point {
	var out []Point
	for _, l := range r.Legs() {
		if l.Hop != nil {
			out = append(out, l.Hop.From, l.Hop.To)
			continue
		}
		out = append(out, expand(l.Tiles)...)
	}
	return out
}

// TestTheMossGiantsRouteNeverWalksOnAStrandSite is the review of the first fix, on the real pack: the carve-out had taken the 217-tile cavern
// out of the pocket and left the tiles 3210,9895-9897 in it, the first the route reaches after the web, so the Guardian took every account
// that finished the cavern on its next poll (a hop to the PvP world within three seconds; two of the three live accounts). Whatever the tile,
// no tile of the moss giants' route, walked or landed on, from the door, from the cavern, from the manhole ladder's foot or from the log's
// tiles, is a strand site, a pocket, a tile the Guardian owns or one it plans; and nothing walks into the summon's ground from outside, so an
// account the Guardian handed back cannot walk back into the pocket either.
//
// Needs RSB0T_COLLISION_PACK; without it this skips.
func TestTheMossGiantsRouteNeverWalksOnAStrandSite(t *testing.T) {
	p := realPack(t)
	starts := []Point{
		{X: 3241, Z: 9911},                                         // the door's far side, where the route enters the cavern
		{X: 3241, Z: 9909}, {X: 3210, Z: 9899}, {X: 3230, Z: 9905}, // the cavern's door, web and ladder ends
		{X: 3237, Z: 9859},                                                             // the foot of the manhole ladder
		{X: 3202, Z: 9909}, {X: 3203, Z: 9910}, {X: 3206, Z: 9908}, {X: 3239, Z: 9907}, // the log's tiles
	}
	moss := Point{X: 3159, Z: 9900}
	peksa := Point{X: 3076, Z: 3429}
	passedWeb := false
	for _, s := range starts {
		for _, dest := range []Point{moss, peksa} {
			r := p.Plan(s, dest, Options{})
			if !r.OK {
				t.Errorf("no route from %s to %s: %s", s, dest, r.Reason)
				continue
			}
			for _, h := range r.Hops() {
				if h.LocID == 733 && h.LocX == 3210 && h.LocZ == 9898 {
					passedWeb = true
				}
			}
			for _, q := range routeTiles(r) {
				if site, ok := randomevents.StrandSiteAt(q.X, q.Z, q.Level); ok {
					t.Errorf("%s -> %s walks %s, in the strand site %q:\n%s", s, dest, q, site.Name, r.String())
					break
				}
				if name, ok := randomevents.PocketAt(q.X, q.Z, q.Level); ok {
					t.Errorf("%s -> %s walks %s, a pocket (%q):\n%s", s, dest, q, name, r.String())
					break
				}
				if name, ok := randomevents.StrandOwner(q.X, q.Z, q.Level); ok {
					t.Errorf("%s -> %s walks %s, owned by the Guardian's ladder (%q):\n%s", s, dest, q, name, r.String())
					break
				}
				if name, ok := randomevents.StrandPlanned(q.X, q.Z, q.Level); ok {
					t.Errorf("%s -> %s walks %s, planned by the Guardian (%q):\n%s", s, dest, q, name, r.String())
					break
				}
			}
		}
	}
	if !passedWeb {
		t.Error("no route to the moss giants went through the web at 3210,9898: the test did not walk the tiles beyond it")
	}

	// The tiles the review named, walked by the route beyond the web, are on it.
	r := p.Plan(Point{X: 3241, Z: 9911}, moss, Options{})
	got := map[Point]bool{}
	for _, q := range routeTiles(r) {
		got[q] = true
	}
	for _, z := range []int{9897, 9896, 9895} {
		if !got[Point{X: 3210, Z: z}] {
			t.Errorf("the route from the door to the moss giants does not walk 3210,%d, the tile the first fix left in the pocket:\n%s", z, r.String())
		}
	}

	// Nothing walks in or out between the sewers and the summon's ground, either way: the rally (3092,10044) is out of reach of the door, and
	// the moss giants out of reach of the arrival at 3217,9910, so the only way into the pocket is the summon.
	if r := p.Plan(Point{X: 3241, Z: 9911}, Point{X: 3092, Z: 10044}, Options{}); r.OK {
		t.Errorf("a route from the sewers' door to the PvP rally exists:\n%s", r.String())
	}
	if r := p.Plan(Point{X: 3217, Z: 9910}, moss, Options{}); r.OK {
		t.Errorf("a route from the summon's arrival to the moss giants exists:\n%s", r.String())
	}
	if r := p.Plan(Point{X: 3217, Z: 9910}, peksa, Options{}); r.OK {
		t.Errorf("a route from the summon's arrival to Peksa exists:\n%s", r.String())
	}
}
