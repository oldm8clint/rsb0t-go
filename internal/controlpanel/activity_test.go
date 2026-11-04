package controlpanel

import "testing"

func TestActivityDropsPrefixAndDiagnostics(t *testing.T) {
	for _, c := range []struct {
		recent []string
		want   string
	}{
		{[]string{"[acct67/ProgressiveFisher] caught one — 1 raw fish in pack now"}, "caught one — 1 raw fish in pack now"},
		{[]string{"[mule/MuleReceiver] cooking round complete", "[mule/MuleReceiver] [diag] FindPath 2816,3439,0 -> 2817,3444,0: 3 waypoints"}, "cooking round complete"},
		{[]string{"[sup/ProgressiveFisher] walking to the camp", "[sup/ProgressiveFisher] route 2590,3417,0 -> 2603,3420,0: cost 17, 11 waypoints, expanded 63\n  [x]"}, "walking to the camp"},
		{[]string{"[a/B] [diag] FindPath 1,2,0 -> 3,4,0: 2 waypoints"}, "[diag] FindPath 1,2,0 -> 3,4,0: 2 waypoints"},
		{[]string{"[bob] disconnected (requested)"}, "disconnected (requested)"},
		{nil, ""},
	} {
		if got := activityOf(c.recent); got != c.want {
			t.Errorf("activityOf(%q) = %q, want %q", c.recent, got, c.want)
		}
	}
}
