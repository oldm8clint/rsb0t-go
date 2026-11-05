package runerunnerplan

import "testing"

func TestNextVialTrip(t *testing.T) {
	for _, c := range []struct {
		left, trips, max int
		want             bool
	}{
		{470, 1, 24, true},   // the first depletion of a 500 shelf
		{7, 5, 24, false},    // depleted
		{8, 5, 24, true},     // exactly a trip
		{300, 24, 24, false}, // the budget
		{300, 1, 0, false},   // off
	} {
		if got, why := NextVialTrip(c.left, c.trips, c.max); got != c.want {
			t.Errorf("NextVialTrip(%d,%d,%d) = %v (%s), want %v", c.left, c.trips, c.max, got, why, c.want)
		}
	}
}

func TestVialShuttleFirst(t *testing.T) {
	for _, c := range []struct {
		max    int
		wanted bool
		exp    int
		free   int
		want   bool
	}{
		{24, true, 498, 0, true},   // 5 Nov: the pack arrived full
		{24, true, 498, 26, false}, // room already
		{24, false, 498, 0, false},
		{24, true, 3, 0, false},
		{0, true, 498, 0, false},
	} {
		if got, why := VialShuttleFirst(c.max, c.wanted, c.exp, c.free); got != c.want {
			t.Errorf("VialShuttleFirst(%+v) = %v (%s), want %v", c, got, why, c.want)
		}
	}
}

func TestShuttleSiteIsAemadOnly(t *testing.T) {
	for _, k := range AmmoSiteKeys() {
		s, _ := AmmoSiteFor(k)
		if s.ShuttleSite() != (k == SiteAemad) {
			t.Errorf("%s ShuttleSite %v", k, s.ShuttleSite())
		}
	}
}
