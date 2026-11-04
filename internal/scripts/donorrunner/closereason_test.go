package donorrunner

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/tile"
)

// Every case asserts on the distinguishing WORDS, not the whole sentence, so rewording the
// operator-facing text does not break the test while a branch collapsing into another one
// still does.
func TestCloseReasonNamesWhatThisSideSaw(t *testing.T) {
	cases := []struct {
		name     string
		screen   donorplan.Screen
		offered  int
		accepted bool
		plan     int
		skipped  int
		want     []string
		notWant  []string
	}{
		{
			name: "confirmed then closed", screen: donorplan.ScreenConfirm, offered: 13,
			accepted: true, plan: 13,
			want: []string{"confirmed", "13", "confirm screen"},
		},
		{
			name:   "accepted at the offer screen, confirm never opened",
			screen: donorplan.ScreenOffer, offered: 13, accepted: true, plan: 13,
			want:    []string{"accepted", "offer screen", "never opened"},
			notWant: []string{"declined"},
		},
		{
			name: "offered but nobody accepted", screen: donorplan.ScreenOffer, offered: 4, plan: 6,
			want:    []string{"4", "neither side accepted"},
			notWant: []string{"nothing"},
		},
		{
			// The empty-trade loop: the caller believed it had a load, the hand-over had
			// nothing to offer. Must be distinguishable from every mule-side failure.
			name: "nothing offerable at all", screen: donorplan.ScreenOffer, offered: 0, plan: 0,
			want:    []string{"nothing", "not offerable"},
			notWant: []string{"mule"},
		},
		{
			name: "closed before anything reached the box", screen: donorplan.ScreenNone, offered: 0, plan: 3,
			want: []string{"before", "3"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := closeReason(c.screen, c.offered, c.accepted, c.plan, c.skipped)
			if got == "" {
				t.Fatal("closeReason returned an empty string — the whole point is that the caller never prints ()")
			}
			for _, w := range c.want {
				if !strings.Contains(got, w) {
					t.Errorf("reason %q does not mention %q", got, w)
				}
			}
			for _, w := range c.notWant {
				if strings.Contains(got, w) {
					t.Errorf("reason %q should not mention %q", got, w)
				}
			}
		})
	}
}

func TestCloseReasonReportsSkippedGroups(t *testing.T) {
	with := closeReason(donorplan.ScreenOffer, 2, false, 5, 3)
	without := closeReason(donorplan.ScreenOffer, 2, false, 5, 0)
	if !strings.Contains(with, "3") || !strings.Contains(with, "untradeable") {
		t.Errorf("a skipped-group count must appear in the reason, got %q", with)
	}
	if strings.Contains(without, "untradeable") {
		t.Errorf("no groups were skipped, so the reason should not mention them: %q", without)
	}
}

// The branches must not all collapse into one sentence — an operator has to be able to tell
// a mule-side failure from a caller-side empty load.
func TestCloseReasonBranchesAreDistinct(t *testing.T) {
	seen := map[string]string{}
	for _, c := range []struct {
		label    string
		screen   donorplan.Screen
		offered  int
		accepted bool
		plan     int
	}{
		{"confirmed", donorplan.ScreenConfirm, 13, true, 13},
		{"accepted-at-offer", donorplan.ScreenOffer, 13, true, 13},
		{"offered-unaccepted", donorplan.ScreenOffer, 4, false, 6},
		{"nothing-offerable", donorplan.ScreenOffer, 0, false, 0},
		{"never-reached-box", donorplan.ScreenNone, 0, false, 3},
	} {
		got := closeReason(c.screen, c.offered, c.accepted, c.plan, 0)
		if prev, dup := seen[got]; dup {
			t.Errorf("%q and %q produce the same reason %q", prev, c.label, got)
		}
		seen[got] = c.label
	}
}

// The real post this was measured against: the mule's Catherby bank stand.
var testPost = tile.Tile{X: 2809, Z: 3441, Level: 0}

func TestAtPostAcceptsTheArrivalTolerance(t *testing.T) {
	// Every tile MeetWalk would already have accepted as "arrived" must count as at the
	// post, or the donor would arrive and then immediately decide it had not.
	for dx := -donorplan.StandRadius; dx <= donorplan.StandRadius; dx++ {
		for dz := -donorplan.StandRadius; dz <= donorplan.StandRadius; dz++ {
			here := tile.Tile{X: testPost.X + dx, Z: testPost.Z + dz, Level: 0}
			if !atPost(here, testPost) {
				t.Errorf("%d,%d is within StandRadius %d of the post but atPost said no", here.X, here.Z, donorplan.StandRadius)
			}
		}
	}
}

func TestAtPostRejectsTilesThatWouldStrandADonor(t *testing.T) {
	for _, c := range []struct {
		name string
		here tile.Tile
	}{
		{"one tile beyond the radius east", tile.Tile{X: testPost.X + donorplan.StandRadius + 1, Z: testPost.Z, Level: 0}},
		{"one tile beyond the radius north", tile.Tile{X: testPost.X, Z: testPost.Z + donorplan.StandRadius + 1, Level: 0}},
		// The tiles the live log showed the mule standing on while cooking: close, but
		// not the post. A donor that has drifted to one of these is NOT in position.
		{"the cooking-range side of the bank", tile.Tile{X: 2817, Z: 3443, Level: 0}},
		{"directly above the post", tile.Tile{X: testPost.X, Z: testPost.Z, Level: 1}},
		{"the fishing spot", tile.Tile{X: 2586, Z: 3419, Level: 0}},
	} {
		if atPost(c.here, testPost) {
			t.Errorf("%s (%d,%d,%d) must not count as at the post", c.name, c.here.X, c.here.Z, c.here.Level)
		}
	}
}
