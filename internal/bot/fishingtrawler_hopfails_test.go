package bot

import "testing"

// A crew member whose hops to the anchor's world are refused (a full world) is stranded after trawlerHopFails in a row, so its own
// script runs instead of the crew loop re-asking every 20 s.
func TestTrawlerCrewRefusedHopsStrand(t *testing.T) {
	b := &fishingTrawlerBot{}
	for i := 0; i < trawlerHopFails-1; i++ {
		b.hopFails++
		if b.Stranded() {
			t.Fatalf("stranded after %d refused hops", b.hopFails)
		}
	}
	b.hopFails++
	if !b.Stranded() {
		t.Fatalf("not stranded after %d refused hops", b.hopFails)
	}
}
