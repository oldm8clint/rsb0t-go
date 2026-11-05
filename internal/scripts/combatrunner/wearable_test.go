package combatrunner

import "testing"

// A strung and an unstrung bow share a name: the wield step takes the one that offers Wield, whichever comes first in the pack.
func TestPickWearableIDPrefersTheFormThatOffersWield(t *testing.T) {
	wear := func(id int32) bool { return id == 841 }
	if id, ok := pickWearableID([]int32{50, 841}, wear); !ok || id != 841 {
		t.Errorf("unstrung first: %d %v, want the strung 841", id, ok)
	}
	if id, ok := pickWearableID([]int32{841, 50}, wear); !ok || id != 841 {
		t.Errorf("strung first: %d %v", id, ok)
	}
	// Nothing wearable: the first, as before (the caller's own wait then says why).
	if id, ok := pickWearableID([]int32{50, 52}, wear); !ok || id != 50 {
		t.Errorf("nothing wearable: %d %v, want 50", id, ok)
	}
	if _, ok := pickWearableID(nil, wear); ok {
		t.Error("no match must not resolve")
	}
	if id, ok := pickWearableID([]int32{7, 8}, nil); !ok || id != 7 {
		t.Errorf("nil predicate: %d %v", id, ok)
	}
}

func TestOffersWearReadsWieldAndWearOnly(t *testing.T) {
	if !offersWear([]string{"", "Wield", "", "", "Drop"}) || !offersWear([]string{"Wear", "", "", "", "Drop"}) {
		t.Error("Wield and Wear are wearable")
	}
	if offersWear([]string{"Use", "", "", "", "Drop"}) || offersWear(nil) {
		t.Error("Use is not wearable")
	}
}
