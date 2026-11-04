package questrunner

import (
	"testing"
	"time"
)

// The legs that fetch a quest's kit break off from hostiles and flee an attacker; the legs a quest fights on do not.
func TestTravelOptionsAreCautiousOnlyWhenAsked(t *testing.T) {
	plain := travelOptions(4, time.Minute, false)
	if plain.AvoidHostiles || plain.Combat.FleeWhenAttacked || !plain.Combat.RunWhenAttacked || plain.Radius != 4 || plain.Timeout != time.Minute {
		t.Errorf("plain travel = %+v", plain)
	}
	careful := travelOptions(0, TravelTimeout, true)
	if !careful.AvoidHostiles || !careful.Combat.FleeWhenAttacked || !careful.Combat.RunWhenAttacked {
		t.Errorf("cautious travel = %+v", careful)
	}
}
