package webwalk

import (
	"encoding/json"
	"os"
	"sync"
	"testing"

	"rsb0t-go/internal/pathfind"
)

// packEnv names the collision pack, as internal/pathfind's tests; without it these skip.
const packEnv = "RSB0T_COLLISION_PACK"

var (
	realOnce    sync.Once
	realPlanner *Planner
	realErr     error
)

func realPack(t testing.TB) *Planner {
	t.Helper()
	path := os.Getenv(packEnv)
	if path == "" {
		t.Skipf("%s not set", packEnv)
	}
	realOnce.Do(func() {
		pk, err := pathfind.Load(path)
		if err != nil {
			realErr = err
			return
		}
		g, err := Default()
		if err != nil {
			realErr = err
			return
		}
		realPlanner = NewPlanner(pk, g)
	})
	if realErr != nil {
		t.Fatal(realErr)
	}
	return realPlanner
}

// tsRoute is one route in test/fixtures/webwalk_routes.json.
type tsRoute struct {
	Name   string   `json:"name"`
	From   rawPoint `json:"from"`
	To     rawPoint `json:"to"`
	OK     bool     `json:"ok"`
	Reason string   `json:"reason"`
	Cost   int      `json:"cost"`
	Hops   []struct {
		Kind   string `json:"kind"`
		Action string `json:"action"`
	} `json:"hops"`
}

// TestRoutesMatchTheTSPlanner plans every route in test/fixtures/webwalk_routes.json (the
// TS PathFinder over the same pack and graph, no WorldState, no policy) and requires the
// same outcome and the same cost. Hop sequences are logged: equal-cost routes may differ.
func TestRoutesMatchTheTSPlanner(t *testing.T) {
	p := realPack(t)
	b, err := os.ReadFile("../../test/fixtures/webwalk_routes.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Routes []tsRoute `json:"routes"`
	}
	if err := json.Unmarshal(b, &fixture); err != nil {
		t.Fatal(err)
	}
	// The TS walker has no special crossing or NPC filter at plan time, and ignores the live
	// approach overrides, so compare with those off.
	g, _ := ParseGraph(defaultGraphJSON)
	pk := p.col
	ts := NewPlanner(pk, g)
	for _, want := range fixture.Routes {
		from := Point{X: want.From.X, Z: want.From.Z, Level: want.From.Level}
		to := Point{X: want.To.X, Z: want.To.Z, Level: want.To.Level}
		got := ts.Plan(from, to, Options{AllowSpecial: true, AllowNPC: true})
		if got.OK != want.OK || (got.OK && got.Cost != want.Cost) {
			t.Errorf("%s: go ok=%v cost=%d (%s), ts ok=%v cost=%d (%s)", want.Name, got.OK, got.Cost, got.Reason, want.OK, want.Cost, want.Reason)
			continue
		}
		var tsHops []string
		for _, h := range want.Hops {
			tsHops = append(tsHops, h.Kind+" "+h.Action)
		}
		var goHops []string
		for _, h := range got.Hops() {
			goHops = append(goHops, string(h.Kind)+" "+h.Action)
		}
		t.Logf("%s: ok=%v cost=%d expanded=%d\n  go hops %v\n  ts hops %v", want.Name, got.OK, got.Cost, got.Expanded, goHops, tsHops)
	}
}

func TestOverrideStandsSurviveThePack(t *testing.T) {
	p := realPack(t)
	for _, o := range ApproachOverrides {
		found := false
		for _, e := range p.EdgesFrom(o.From) {
			if e.LocID == o.LocID && e.Action == o.Action {
				found = true
			}
		}
		if !found {
			t.Errorf("override stand %s for loc %d %s is not walkable in the pack, or its edge was dropped", o.From, o.LocID, o.Action)
		}
	}
}

func hopWith(r *Route, locID int, action string) *Edge {
	for _, h := range r.Hops() {
		if h.LocID == locID && h.Action == action {
			return h
		}
	}
	return nil
}

// TestKnownScriptRoutes plans the routes the scripts need with a Walker's default options
// (no special crossings, no NPC hops, live approach overrides applied).
func TestKnownScriptRoutes(t *testing.T) {
	p := realPack(t)
	plan := func(name string, from, to Point, opt Options) Route {
		t.Helper()
		r := p.Plan(from, to, opt)
		if !r.OK {
			t.Errorf("%s: %s", name, r.Reason)
		} else {
			t.Logf("%s: %s", name, r.String())
		}
		return r
	}

	plan("catherby bank to fishing", Point{X: 2809, Z: 3441}, Point{X: 2837, Z: 3432}, Options{})
	plan("catherby fishing to bank", Point{X: 2845, Z: 3431}, Point{X: 2809, Z: 3441}, Options{})

	up := plan("seers field to wheel", Point{X: 2741, Z: 3444}, Point{X: 2711, Z: 3471, Level: 1}, Options{})
	if up.OK {
		if h := hopWith(&up, 1747, "Climb-up"); h == nil || h.From != (Point{X: 2714, Z: 3470}) {
			t.Errorf("seers field to wheel does not climb ladder 1747 from 2714,3470: %v", up.Hops())
		}
		if d := hopWith(&up, 1530, "Open"); d == nil || d.LocX != 2716 || d.LocZ != 3472 {
			t.Errorf("seers field to wheel does not open the wheelhouse door at 2716,3472: %v", up.Hops())
		}
	}
	down := plan("seers wheel to bank", Point{X: 2711, Z: 3471, Level: 1}, Point{X: 2725, Z: 3493}, Options{})
	if down.OK {
		if h := hopWith(&down, 1746, "Climb-down"); h == nil || h.From != (Point{X: 2714, Z: 3470, Level: 1}) {
			t.Errorf("seers wheel to bank does not climb down 1746 from 2714,3470,1: %v", down.Hops())
		}
	}
	plan("seers bank to field", Point{X: 2725, Z: 3493}, Point{X: 2741, Z: 3444}, Options{})
	plan("lumbridge to draynor", Point{X: 3222, Z: 3218}, Point{X: 3093, Z: 3244}, Options{})

	if r := p.Plan(Point{X: 3029, Z: 3217}, Point{X: 2924, Z: 3152}, Options{}); r.OK {
		t.Errorf("port sarim to karamja planned without NPC hops: %s", r.String())
	}
	ship := plan("port sarim to karamja", Point{X: 3029, Z: 3217}, Point{X: 2924, Z: 3152}, Options{AllowNPC: true, AllowSpecial: true})
	if ship.OK {
		hops := ship.Hops()
		if len(hops) < 2 || hops[0].Kind != KindShip || hopWith(&ship, 2082, "Cross") == nil {
			t.Errorf("port sarim to karamja hops = %v", hops)
		}
	}
}
