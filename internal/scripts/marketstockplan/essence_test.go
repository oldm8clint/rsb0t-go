package marketstockplan

import "testing"

// Rune essence is a market stock goal like a rune (a user request), with its own price ceiling.
func TestParseTargetsTakesRuneEssence(t *testing.T) {
	got, err := ParseTargets([]string{"Nature rune=20000", "rune essence=500000"})
	if err != nil || len(got) != 2 || got[1].Rune != "Rune essence" || got[1].Stock != 500000 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if HardCeilings["Rune essence"] <= 0 {
		t.Fatal("no price ceiling for Rune essence")
	}
}
