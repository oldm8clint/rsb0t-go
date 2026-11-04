package questplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// The accounts the Imp Catcher slot admits at once (questrunner.slotCapacity) spread over the farm's sites by how
// crowded each is per spawn, not onto the first: twelve over seventeen imps, six and six.
func TestPickImpSiteSpreadsTwelveAccountsAcrossTheSites(t *testing.T) {
	farmers := map[string]int{}
	for i := 0; i < 12; i++ {
		s := PickImpSite(farmers, tile.Tile{}, false)
		farmers[s.ID]++
	}
	if farmers[ImpSiteArdougne.ID] != 6 || farmers[ImpSiteYanille.ID] != 6 {
		t.Fatalf("twelve accounts split %v, want six and six", farmers)
	}
	// And the split is by crowding, not by turns: with the Ardougne strip already full of farmers the next ones go to Yanille.
	crowded := map[string]int{ImpSiteArdougne.ID: 9}
	for i := 0; i < 3; i++ {
		s := PickImpSite(crowded, tile.Tile{}, false)
		if s.ID != ImpSiteYanille.ID {
			t.Fatalf("farmer %d went to %s with nine already at Ardougne and %d at Yanille", i+1, s.ID, crowded[ImpSiteYanille.ID])
		}
		crowded[s.ID]++
	}
}

func TestPickImpSiteBreaksATieByDistance(t *testing.T) {
	// 8 farmers on nine spawns and 7 on eight leave one more account at 9/9 and 8/8 per spawn: a dead heat.
	tied := map[string]int{ImpSiteArdougne.ID: 8, ImpSiteYanille.ID: 7}
	if s := PickImpSite(tied, ImpSiteYanille.Stand, true); s.ID != ImpSiteYanille.ID {
		t.Errorf("from the Yanille field a dead heat went to %s", s.ID)
	}
	if s := PickImpSite(tied, ImpSiteArdougne.Stand, true); s.ID != ImpSiteArdougne.ID {
		t.Errorf("from the Ardougne field a dead heat went to %s", s.ID)
	}
	// An unread position falls to the table's order.
	if s := PickImpSite(tied, tile.Tile{}, false); s.ID != ImpSites[0].ID {
		t.Errorf("an unread position's dead heat went to %s, want the first site", s.ID)
	}
	// Nobody farming anywhere: the first site (nine spawns, the proven one), wherever the account stands.
	if s := PickImpSite(nil, ImpSiteYanille.Stand, true); s.ID != ImpSiteArdougne.ID {
		t.Errorf("an empty table sent a Yanille account to %s: crowding decides first, not distance", s.ID)
	}
}

func TestImpSiteByID(t *testing.T) {
	for _, s := range ImpSites {
		if got, ok := ImpSiteByID(s.ID); !ok || got.ID != s.ID {
			t.Errorf("%s: found %v, %v", s.ID, got.ID, ok)
		}
	}
	if _, ok := ImpSiteByID("karamja"); ok {
		t.Error("Karamja's volcano is a site")
	}
}

func TestImpSiteLeash(t *testing.T) {
	s := ImpSiteArdougne
	if !s.Has(s.Stand) || !s.Has(tile.Tile{X: s.Stand.X + s.Leash, Z: s.Stand.Z}) {
		t.Error("the stand and the leash's edge are the site's")
	}
	if s.Has(tile.Tile{X: s.Stand.X + s.Leash + 1, Z: s.Stand.Z}) {
		t.Error("an imp past the leash is the site's")
	}
	if s.Has(tile.Tile{X: s.Stand.X, Z: s.Stand.Z, Level: 1}) {
		t.Error("an imp on another level is the site's")
	}
}

func TestPickImpTakesTheNearestFreeImpOverOpenGround(t *testing.T) {
	s := ImpSiteArdougne
	at := func(dx int) tile.Tile { return tile.Tile{X: s.Stand.X + dx, Z: s.Stand.Z} }
	cases := []struct {
		name  string
		cands []ImpCandidate
		want  int // -1: none
	}{
		{"nothing in view", nil, -1},
		{"the nearest by steps, not by distance", []ImpCandidate{
			{At: at(2), Steps: 31}, // two tiles off behind a long wall
			{At: at(9), Steps: 10},
		}, 1},
		{"a free imp over a nearer one in a fight", []ImpCandidate{
			{At: at(1), Steps: 1, Busy: true},
			{At: at(12), Steps: 14},
		}, 1},
		{"none when every one in view is in a fight", []ImpCandidate{
			{At: at(3), Steps: 4, Busy: true},
			{At: at(2), Steps: 3, Busy: true},
		}, -1},
		{"none behind a door", []ImpCandidate{
			{At: at(-20), Steps: -1}, // the monastery's room
			{At: at(-18), Steps: -1},
		}, -1},
		{"none past the leash", []ImpCandidate{{At: at(s.Leash + 5), Steps: 8}}, -1},
		{"the unreachable are skipped, not the rest", []ImpCandidate{
			{At: at(-5), Steps: -1}, {At: at(6), Steps: 7},
		}, 1},
	}
	for _, c := range cases {
		got, ok := PickImp(s, c.cands)
		if c.want < 0 {
			if ok {
				t.Errorf("%s: chose %d, want none", c.name, got)
			}
			continue
		}
		if !ok || got != c.want {
			t.Errorf("%s: chose %d (%v), want %d", c.name, got, ok, c.want)
		}
	}
}

// The patrol visits every spawn once, starting with the nearest to the account and always moving to the nearest next.
func TestPatrolVisitsEverySpawnOnceNearestFirst(t *testing.T) {
	for _, s := range ImpSites {
		from := tile.Tile{X: s.Stand.X, Z: s.Stand.Z}
		route := s.Patrol(from)
		if len(route) != len(s.Spawns) {
			t.Fatalf("%s: %d stops for %d spawns", s.ID, len(route), len(s.Spawns))
		}
		seen := map[tile.Tile]bool{}
		at := from
		for i, stop := range route {
			if seen[stop] {
				t.Fatalf("%s: %v visited twice", s.ID, stop)
			}
			seen[stop] = true
			for _, other := range s.Spawns {
				if !seen[other] && tile.Chebyshev(at, other) < tile.Chebyshev(at, stop) {
					t.Errorf("%s: stop %d is %d away but %v is %d", s.ID, i, tile.Chebyshev(at, stop), other, tile.Chebyshev(at, other))
				}
			}
			at = stop
		}
	}
}
