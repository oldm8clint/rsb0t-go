package fleetplan

import "testing"

// Food only counts once the account survives long enough to eat it, which is the whole reason the
// hitpoints-20 lives died with their food uneaten.
func TestFoodOnlyCountsAboveTheOneShotThreshold(t *testing.T) {
	below := CastsPerLife(20, 400)
	bare := CastsPerLife(20, 0)
	if below != bare {
		t.Errorf("food is being counted below the threshold: %v with 400 healing against %v with none", below, bare)
	}
	above := CastsPerLife(32, 400)
	if above <= CastsPerLife(32, 0) {
		t.Error("food is not being counted above the threshold")
	}
	// And the step is real: 32 hitpoints with food beats 31 hitpoints with the same food by a lot.
	if CastsPerLife(31, 400) >= above/2 {
		t.Errorf("one hitpoint either side of the threshold is not a step: %v against %v", CastsPerLife(31, 400), above)
	}
	if CastsPerLife(0, 100) != 0 {
		t.Error("a dead account casts")
	}
}

// The measured point the constant comes from: about five casts out of a 99-hitpoint life.
func TestCastsPerLifeMatchesTheMeasuredArenaLives(t *testing.T) {
	got := CastsPerLife(99, 0)
	if got < 4 || got > 6 {
		t.Errorf("a 99-hitpoint life is estimated at %.1f casts; the arena measured about 5", got)
	}
}
