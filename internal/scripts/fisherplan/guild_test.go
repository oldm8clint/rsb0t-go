package fisherplan

import (
	"rsb0t-go/internal/scripts/tile"
	"testing"
)

func TestFishingGuildRouteRequiresOptInAndRealEntryLevel(t *testing.T) {
	receiver := tile.Tile{X: 2809, Z: 3441}
	for _, tc := range []struct {
		enabled bool
		level   int
		target  string
		guild   bool
	}{
		{false, 99, "Big net", false},
		{true, 67, "Big net", false},
		{true, 68, "Big net", true},
		{true, 68, "Small net", false},
		{true, 68, "Harpoon", true},
		{true, 68, "Lobster cage", true},
		{true, 76, "Harpoon sharks", true},
	} {
		route := PlanFishingRoute(tc.enabled, tc.level, CurrentRung(tc.level, tc.target), receiver)
		if (route.Area == AreaFishingGuild) != tc.guild {
			t.Fatalf("%+v: route=%+v", tc, route)
		}
		if route.Receiver != receiver || route.HandoffBank != Catherby().Bank {
			t.Fatalf("handoff left Catherby: %+v", route)
		}
		if tc.guild {
			if route.MuleAt != FishingGuildMuleAt || route.ReturnBank != FishingGuild().Bank {
				t.Fatalf("guild itinerary=%+v", route)
			}
		} else if route.Layout != Catherby() || route.MuleAt != MuleFishAt || route.ReturnBank != Catherby().Bank {
			t.Fatalf("Catherby defaults changed: %+v", route)
		}
	}
}

func TestGuildSelectionUsesActuallyOwnedTool(t *testing.T) {
	active, ok := ActiveRung(80, "Big net", []string{"Small fishing net"})
	if !ok {
		t.Fatal("small net should remain usable")
	}
	if r := PlanFishingRoute(true, 80, active, Catherby().Bank); r.Area != AreaCatherby {
		t.Fatalf("guild cannot supply Net/Bait for fallback small net: %+v", r)
	}
}

func TestGuildCampCoversEveryContentFishingSpot(t *testing.T) {
	// Coordinates from fishing_movement.enum, not modern OSRS map assumptions.
	spots := [][2]int{{52, 19}, {52, 20}, {52, 22}, {52, 23}, {42, 23}, {42, 22}, {42, 20}, {42, 19}, {48, 24}, {46, 24}, {45, 24}, {44, 25}, {43, 25}, {43, 27}, {39, 27}, {45, 28}, {45, 29}, {45, 32}, {45, 33}, {44, 34}, {42, 34}, {41, 30}, {47, 18}}
	l := FishingGuild()
	for _, s := range spots {
		p := tile.Tile{X: 40*64 + s[0], Z: 53*64 + s[1]}
		if tile.Chebyshev(p, l.Camp) > l.SpotRange {
			t.Fatalf("spot %+v outside camp %+v range%d", p, l.Camp, l.SpotRange)
		}
	}
}

func TestGuildRegionsAreDisjointAndOnTheMainland(t *testing.T) {
	g, c := FishingGuild(), Catherby()
	for _, p := range []tile.Tile{g.Bank, g.Camp, FishingGuildDoor} {
		if !InFishingGuild(p) || InCatherby(p) || !OnGuildMainland(p) || !NearerGuild(p) {
			t.Fatalf("%+v should be Guild-side mainland only", p)
		}
	}
	for _, p := range []tile.Tile{c.Bank, c.Camp, c.HarryStand, c.ArheinStand} {
		if InFishingGuild(p) || !InCatherby(p) || !OnGuildMainland(p) || NearerGuild(p) {
			t.Fatalf("%+v should be Catherby-side mainland only", p)
		}
	}
	// Seers' bank, the default mule post, sits between the two on the same walk.
	if seers := (tile.Tile{X: 2725, Z: 3491}); !OnGuildMainland(seers) || InFishingGuild(seers) || InCatherby(seers) {
		t.Fatalf("Seers should be on the mainland between the two homes")
	}
	for _, p := range []tile.Tile{{X: 3222, Z: 3218}, {X: 2772, Z: 3235}, {X: 2898, Z: 3423, Level: 1}, {X: 2603, Z: 3416, Level: 1}} {
		if OnGuildMainland(p) {
			t.Fatalf("%+v is not on the Guild's mainland", p)
		}
	}
}

func TestGuildIsHomeOnlyWhenSelected(t *testing.T) {
	bank := FishingGuild().Bank
	s := FishSnapshot{Tile: &bank, Level: 70, Target: "Big net", HeldTools: []string{"Big fishing net"}, BankTools: []string{}, Coins: 0}
	if InHome(s) {
		t.Fatal("the Guild must not be home with Guild unset")
	}
	if p := DecideFishPhase(s); p != PhaseTravel {
		t.Fatalf("Guild unset at the Guild bank = %s, want travel (the Catherby-only behaviour)", p)
	}
	s.Guild = true
	if !InHome(s) {
		t.Fatal("the Guild must be home once selected")
	}
	if p := DecideFishPhase(s); p != PhaseFish {
		t.Fatalf("Guild selected at the Guild bank = %s, want fish", p)
	}
	s.PackFull = true
	if p := DecideFishPhase(s); p != PhaseBank {
		t.Fatalf("Guild selected with a full pack = %s, want bank", p)
	}
	// Catherby stays home either way: the tool shops are there.
	c := Catherby().Bank
	s.Tile, s.PackFull = &c, false
	for _, g := range []bool{false, true} {
		s.Guild = g
		if !InHome(s) || DecideFishPhase(s) != PhaseFish {
			t.Fatalf("Catherby must stay home with Guild=%v", g)
		}
	}
}

func TestGuildToolSpawnsCoverEveryGuildRung(t *testing.T) {
	for _, r := range Rungs {
		if !SupportsGuildRung(r) {
			continue
		}
		sp, ok := GuildSpawnFor(r.Tool)
		if !ok || !InFishingGuild(sp.At) || tile.Chebyshev(sp.At, FishingGuildDoor) > 6 {
			t.Fatalf("rung %q: spawn %+v ok=%v", r.Label, sp, ok)
		}
	}
	if _, ok := GuildSpawnFor("Small fishing net"); ok {
		t.Fatal("the Guild spawns no small net")
	}
}

func TestGuildToolWanted(t *testing.T) {
	none := []string{}
	cases := []struct {
		level      int
		target     string
		held, bank []string
		tool       string
	}{
		{70, "Big net", nil, none, "Big fishing net"},
		{70, "Big net", []string{"Small fishing net"}, none, "Big fishing net"},
		{70, "Big net", []string{"Big fishing net"}, none, ""},
		{70, "Big net", nil, []string{"Big fishing net"}, ""}, // the bank has it: withdraw instead
		{70, "Big net", nil, nil, ""},                         // bank never read: read it first
		{67, "Big net", nil, none, ""},                        // cannot enter
		{70, "Harpoon", nil, none, "Harpoon"},
		{70, "Lobster cage", nil, none, "Lobster pot"},
		{80, "Harpoon sharks", nil, none, "Harpoon"},
		{70, "Small net", nil, none, ""}, // not a Guild rung
	}
	for _, c := range cases {
		sp, ok := GuildToolWanted(c.level, c.target, c.held, c.bank)
		if ok != (c.tool != "") || sp.Tool != c.tool {
			t.Fatalf("%+v: got %+v ok=%v", c, sp, ok)
		}
	}
	if !GuildToolRecoverable(70, "Big net", nil, nil) || GuildToolRecoverable(70, "Big net", []string{"Big fishing net"}, nil) || GuildToolRecoverable(67, "Big net", nil, nil) {
		t.Fatal("GuildToolRecoverable")
	}
}
