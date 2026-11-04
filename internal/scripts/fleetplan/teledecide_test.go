package fleetplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// teleRig is a decision input for an account at the Feldip ogre pens walking to the mule at Catherby with a full kit and a
// Staff of air worn, and the priced landings it asks for. Every walk is priced at no run energy, so a tile unit is a tick.
type teleRig struct {
	in     TeleDecisionInput
	prices map[tile.Tile]RouteFacts
	calls  []tile.Tile
}

// fullKit adds the elementals Varrock, Lumbridge and Falador cost.
func (r *teleRig) fullKit() *teleRig {
	r.in.Pack["Fire rune"], r.in.Pack["Water rune"], r.in.Pack["Earth rune"] = 10, 10, 10
	return r
}

func (r *teleRig) decide(s TeleTravelSettings) (TeleDecision, string) {
	s.Enabled = true
	return DecideTeleport(s, r.in)
}

// WalkTicks: the figures the design works out. 300 tile units at 10 kg cost 0.385% run energy a tile.
func TestWalkTicksEnergy(t *testing.T) {
	r := RouteFacts{OK: true, TileUnits: 300}
	for _, c := range []struct {
		name    string
		energy  int
		known   bool
		stalled bool
		want    int
	}{
		{"full energy runs most of it", 100, true, false, 170},
		{"ten percent runs a little", 10, true, false, 287},
		{"none walks every tile", 0, true, false, 300},
		{"a stalled walk drains a third as fast", 10, true, true, 263},
		// 50% / 0.385 = 129 run tiles: 64 ticks running + 171 walking
		{"unknown energy counts as half", 0, false, false, 129/2 + (300 - 129)},
	} {
		got := WalkTicks(r, c.energy, c.known, 10, c.stalled, 40)
		if got != c.want {
			t.Errorf("%s: %d ticks, want %d", c.name, got, c.want)
		}
	}
	if got := WalkTicks(RouteFacts{}, 100, true, 10, false, 40); got != UnwalkableTicks {
		t.Errorf("a walk with no route: %d ticks, want UnwalkableTicks", got)
	}
}

// Hop costs are inside the planner's route cost (Route.Cost): the caller splits them out, and WalkTicks counts them once, at
// two units a tick, with every ship's surcharge on top.
func TestWalkTicksSplitsHopsFromTiles(t *testing.T) {
	// A route of 100 walked tiles, a ship (36), a door (4) and a stair (6): cost 146.
	r := RouteFacts{OK: true, TileUnits: 100, HopUnits: 36 + 4 + 6, Ships: 1}
	got := WalkTicks(r, 0, true, 0, false, 40)
	if want := 100 + 46/2 + 40; got != want {
		t.Errorf("%d ticks, want %d (100 walked, 23 for the hops, 40 for the ship)", got, want)
	}
	// With the ship surcharge of none the ship is the planner's 18 ticks and nothing more.
	if got := WalkTicks(r, 0, true, 0, false, 0); got != 100+23 {
		t.Errorf("no surcharge: %d ticks, want 123", got)
	}
}

func withEnabled(s TeleTravelSettings, name string) TeleTravelSettings {
	s.Enabled = name != "teleport travel off"
	return s
}

func TestLawPriceTable(t *testing.T) {
	for _, c := range []struct {
		laws     int
		known    bool
		held     int
		mult     int
		muleOnly bool
		name     string
	}{
		{33673, true, 0, 1, false, "today's stock"},
		{20000, true, 0, 1, false, "20,000"},
		{19999, true, 0, 2, false, "19,999"},
		{8000, true, 0, 2, false, "8,000"},
		{5000, true, 0, 4, false, "5,000"},
		{3000, true, 0, 4, false, "3,000"},
		{2999, true, 0, 8, false, "2,999"},
		{1000, true, 0, 8, false, "1,000"},
		{999, true, 0, 1, true, "999: the mule walk only"},
		{500, true, 0, 1, true, "500"},
		{0, false, 5, 2, false, "unknown, laws held"},
		{0, false, 0, 1, true, "unknown, none held"},
	} {
		mult, only := LawPrice(c.laws, c.known, c.held)
		if mult != c.mult || only != c.muleOnly {
			t.Errorf("%s: x%d only=%v, want x%d only=%v", c.name, mult, only, c.mult, c.muleOnly)
		}
	}
}

// The settings fill their defaults, and the two caps take a negative as "none".
func TestTeleTravelSettingsTickDefaults(t *testing.T) {
	s := TeleTravelSettings{Enabled: true}.Normalised()
	if s.MinSaveTicks != 24 || s.CastTicks != 6 || s.SettleTicks != 5 || s.ShipTicks != 40 || s.MaxPerHour != 12 || s.FleetPerHour != 900 || s.MissedTicks != 900 {
		t.Errorf("defaults %+v", s)
	}
	s = TeleTravelSettings{FleetPerHour: -1, MaxPerHour: -1}.Normalised()
	if s.FleetPerHour != -1 || s.MaxPerHour != -1 {
		t.Errorf("a negative cap was overwritten: %+v", s)
	}
}
