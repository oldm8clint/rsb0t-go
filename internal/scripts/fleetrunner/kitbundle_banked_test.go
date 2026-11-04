package fleetrunner

import "testing"

// A 10,000-line of raw fish crosses as ONE noted slot; UnnoteKit banks it and draws back only the pack's room, so
// a delivery is about twenty in the pack (KitResult.Got) and the rest in the bank. The bank's share is reported
// (KitResult.Banked) so the caller can count what was DELIVERED instead of re-asking.
func TestKitBundleReportsBankedQuantities(t *testing.T) {
	if got := bankedOf(nil); got != nil {
		t.Errorf("nothing banked: %v", got)
	}
	if got := bankedOf(map[string]int{"Raw cod": 0}); got != nil {
		t.Errorf("a zero is not banked: %v", got)
	}
	got := bankedOf(map[string]int{"Raw bass": 9627, "Raw cod": 0, "Raw lobster": 187})
	if len(got) != 2 || got["Raw bass"] != 9627 || got["Raw lobster"] != 187 {
		t.Errorf("banked %v", got)
	}
	// It is a copy: a caller that merges into it does not write back into the bundle's info.
	src := map[string]int{"Raw bass": 5}
	out := bankedOf(src)
	out["Raw bass"] = 99
	if src["Raw bass"] != 5 {
		t.Error("bankedOf returned the info's own map")
	}
	// Delivered is the pack and the bank together.
	r := KitResult{Got: map[string]int{"Raw cod": 22}, Banked: map[string]int{"Raw cod": 184, "Raw bass": 9627}}
	delivered := map[string]int{}
	for k, n := range r.Got {
		delivered[k] += n
	}
	for k, n := range r.Banked {
		delivered[k] += n
	}
	if delivered["Raw cod"] != 206 || delivered["Raw bass"] != 9627 {
		t.Errorf("delivered %v", delivered)
	}
}
