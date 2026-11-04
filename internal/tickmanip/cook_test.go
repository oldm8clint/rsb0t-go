package tickmanip

import (
	"reflect"
	"testing"
)

// TestReplayCookCeilingIsThreeTicks: spamming a use every tick is accepted only every third tick.
func TestReplayCookCeilingIsThreeTicks(t *testing.T) {
	tr := ReplayCook(EveryPlan(0, 1, CookUse), 13)
	if want := []int{0, 3, 6, 9, 12}; !reflect.DeepEqual(tr.Accepted, want) {
		t.Errorf("accepted = %v, want %v", tr.Accepted, want)
	}
	if want := []int{2, 5, 8, 11, 14}; !reflect.DeepEqual(tr.Done, want) {
		t.Errorf("done = %v, want %v", tr.Done, want)
	}
	if len(tr.Refused) != 8 {
		t.Errorf("refused = %v, want 8 refusals", tr.Refused)
	}
}

// TestReplayCookPeriods: a period of 2 is accepted every 4 ticks (every other use is refused, and the accepted one
// then waits out the delay again), 3 loses none, and 4 is no better than 2.
func TestReplayCookPeriods(t *testing.T) {
	for _, c := range []struct{ period, accepted int }{{2, 8}, {3, 10}, {4, 8}} {
		tr := ReplayCook(EveryPlan(0, c.period, CookUse), 30)
		if len(tr.Accepted) != c.accepted {
			t.Errorf("period %d: %d accepted, want %d (%v)", c.period, len(tr.Accepted), c.accepted, tr.Accepted)
		}
	}
}
