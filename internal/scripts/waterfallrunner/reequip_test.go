package waterfallrunner

import (
	"reflect"
	"testing"
)

func TestWornHintsNameOnlyTheStrippedGear(t *testing.T) {
	got := wornIDs(map[string]bool{"worn:1277": true, "worn:1171": true, "worn:0": true, "worn:x": true,
		"worn:1": false, "keg-swapped": true})
	if want := []int{1171, 1277}; !reflect.DeepEqual(got, want) {
		t.Fatalf("worn ids %v, want %v", got, want)
	}
	if ids := wornIDs(nil); len(ids) != 0 {
		t.Fatalf("no hints: %v", ids)
	}
}
