package live

import (
	"os"
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/webwalk"
)

// Approaches on the production loc index and config: Draynor Manor's spooky staircases, where
// three Holy Grail accounts were trapped on level 1 on 2026-10-04 because Walk-down was sent
// from 3108,3367. The server (ReachStrategy.reachRectangle with the loc's forceapproach) only
// accepts the one row the content names, so those tiles must come first. Skips without
// RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG.
func TestApproachesAreTheTilesTheServerReaches(t *testing.T) {
	lp, cp := os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG")
	if lp == "" || cp == "" {
		t.Skip("RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG not set")
	}
	ix, err := locindex.Load(lp)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := cachecfg.LoadFile(cp)
	if err != nil {
		t.Fatal(err)
	}
	c := NewClient(Deps{Session: &fakeSession{st: gamestate.New()}, Locs: ix, Config: cfg})

	for _, tc := range []struct {
		name     string
		id, x, z int
		level    int
		op       string
		want     []webwalk.Point // the legal sides, in order, before the footprint
		refused  []webwalk.Point
	}{
		{"Draynor Manor down", 1731, 3108, 3364, 1, "Walk-down",
			[]webwalk.Point{{X: 3108, Z: 3366, Level: 1}, {X: 3109, Z: 3366, Level: 1}},
			[]webwalk.Point{{X: 3108, Z: 3367, Level: 1}, {X: 3107, Z: 3364, Level: 1}, {X: 3108, Z: 3363, Level: 1}}},
		{"Draynor Manor up", 1730, 3108, 3363, 0, "Walk-up",
			[]webwalk.Point{{X: 3108, Z: 3362, Level: 0}, {X: 3109, Z: 3362, Level: 0}},
			[]webwalk.Point{{X: 3107, Z: 3363, Level: 0}, {X: 3108, Z: 3360, Level: 0}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ms := c.FindLocs(webwalk.LocQuery{At: webwalk.Point{X: tc.x, Z: tc.z, Level: tc.level}, Radius: 1, IDs: []int{tc.id}, Op: tc.op})
			if len(ms) == 0 || ms[0].X != tc.x || ms[0].Z != tc.z {
				t.Fatalf("FindLocs = %+v", ms)
			}
			got := c.Approaches(ms[0])
			if len(got) < len(tc.want) {
				t.Fatalf("Approaches = %v", got)
			}
			for i, w := range tc.want {
				if got[i] != w {
					t.Errorf("Approaches[%d] = %s, want %s (all: %v)", i, got[i], w, got)
				}
			}
			for _, r := range tc.refused {
				for _, g := range got {
					if g == r {
						t.Errorf("%s is offered, but the loc's forceapproach refuses it", r)
					}
				}
			}
		})
	}

	if got := c.Approaches(webwalk.LocMatch{ID: 1731, X: 3108, Z: 3365, Level: 1}); got != nil {
		t.Errorf("a loc that is not at the tile has no approaches, got %v", got)
	}
}
