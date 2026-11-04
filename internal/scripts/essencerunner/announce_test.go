package essencerunner

import "testing"

// The mine's portal puts the account down beside the NPC that sent it in, and PortalOut does not know which, so it announces every
// gateway's doorstep to the Guardian. The live landings of 14 Oct 2026 (build d5362bb2) that read "UNEXPLAINED" were all in reach: the
// Distentor's portal set accounts down on 2590,3086, 2592,3087, 2589,3088 and 2589,3087.
func TestTheMinePortalExpectsEveryGatewaysDoorstep(t *testing.T) {
	landings := exitLandings()
	for _, c := range []struct {
		name string
		x, z int
	}{
		{"Distentor (the live landings)", 2590, 3086},
		{"Distentor", 2592, 3087},
		{"Distentor", 2589, 3088},
		{"Distentor", 2589, 3087},
		{"Aubury (the walk graph's return)", 3253, 3401},
		{"Cromperty (the walk graph's return)", 2684, 3322},
	} {
		hit := false
		for _, l := range landings {
			hit = hit || l.Contains(c.x, c.z)
		}
		if !hit {
			t.Errorf("%s: %d,%d is not an expected doorstep of the portal", c.name, c.x, c.z)
		}
	}
	// The mine itself is not a doorstep, nor is the Lumbridge spawn tile.
	for _, c := range [][2]int{{2897, 4808}, {3222, 3218}} {
		for _, l := range landings {
			if l.Contains(c[0], c[1]) {
				t.Errorf("%d,%d is an expected doorstep of the portal", c[0], c[1])
			}
		}
	}
}
