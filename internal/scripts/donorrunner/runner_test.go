package donorrunner

import (
	"reflect"
	"testing"

	"rsb0t-go/internal/scripts/donorplan"
)

func TestEffectiveLoadDropsCoinsAtOrBelowReserve(t *testing.T) {
	load := []donorplan.LoadGroup{{Name: "Raw shrimp", ID: 317, Slots: 5}, {Name: "Coins", ID: 995, Slots: 1}}
	o := Options{
		Load:      func() []donorplan.LoadGroup { return load },
		CoinsName: "Coins",
		Reserve:   500,
		CoinsHeld: func() int { return 400 }, // at or below the reserve — nothing to give
	}
	got := effectiveLoad(o)
	want := []donorplan.LoadGroup{{Name: "Raw shrimp", ID: 317, Slots: 5}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("effectiveLoad (below reserve) = %+v, want %+v", got, want)
	}

	o.CoinsHeld = func() int { return 1500 } // above the reserve — coins stay in the plan
	got = effectiveLoad(o)
	if !reflect.DeepEqual(got, load) {
		t.Errorf("effectiveLoad (above reserve) = %+v, want %+v", got, load)
	}
}

func TestEffectiveLoadPassesThroughWhenReserveDisabled(t *testing.T) {
	load := []donorplan.LoadGroup{{Name: "Coins", ID: 995, Slots: 1}}
	o := Options{Load: func() []donorplan.LoadGroup { return load }}
	if got := effectiveLoad(o); !reflect.DeepEqual(got, load) {
		t.Errorf("effectiveLoad with no reserve = %+v, want the load unchanged", got)
	}
}

func TestNamedRowsMatchesByID(t *testing.T) {
	plan := []donorplan.LoadGroup{{Name: "Raw shrimp", ID: 317}, {Name: "Coins", ID: 995}}
	rows := []donorplan.ItemRow{{ID: 995, Count: 1000}, {ID: 317, Count: 5}, {ID: 9999, Count: 1}}
	got := namedRows(rows, plan)
	want := []donorplan.ItemRow{{Name: "Coins", ID: 995, Count: 1000}, {Name: "Raw shrimp", ID: 317, Count: 5}, {Name: "", ID: 9999, Count: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("namedRows = %+v, want %+v", got, want)
	}
}

func TestWithoutSkippedDropsRefusedGroups(t *testing.T) {
	plan := []donorplan.LoadGroup{{Name: "Raw cod", ID: 342, Slots: 1}, {Name: "Lamp", ID: 2528, Slots: 1}, {Name: "Oyster", ID: 408, Slots: 1}}
	if got := withoutSkipped(plan, nil); !reflect.DeepEqual(got, plan) {
		t.Errorf("no skips = %+v, want the plan unchanged", got)
	}
	want := []donorplan.LoadGroup{{Name: "Raw cod", ID: 342, Slots: 1}, {Name: "Oyster", ID: 408, Slots: 1}}
	if got := withoutSkipped(plan, map[int]bool{2528: true}); !reflect.DeepEqual(got, want) {
		t.Errorf("lamp skipped = %+v, want %+v", got, want)
	}
}
