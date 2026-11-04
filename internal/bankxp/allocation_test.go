package bankxp

import (
	"reflect"
	"testing"
)

func TestComputeIgnoresNonpositiveCounts(t *testing.T) {
	for _, have := range []map[int]int64{nil, {}, {999999: 0, 999998: -1}} {
		if got := Compute(have, nil, Options{}); !reflect.DeepEqual(got, Plan{}) {
			t.Fatalf("empty bank changed: %+v", got)
		}
	}
	have := map[int]int64{999999: 7, 999998: 0, 999997: -1}
	want := Compute(map[int]int64{999999: 7}, nil, Options{})
	if got := Compute(have, nil, Options{}); !reflect.DeepEqual(got, want) {
		t.Fatalf("nonpositive items changed plan: %+v vs %+v", got, want)
	}
	if !reflect.DeepEqual(have, map[int]int64{999999: 7, 999998: 0, 999997: -1}) {
		t.Fatal("input bank mutated")
	}
}

var allocationPlan Plan

func BenchmarkComputeBank(b *testing.B) {
	have := map[int]int64{}
	for _, route := range Routes {
		for _, in := range route.Inputs {
			have[in.ID] = 1000
		}
	}
	for id := 900000; len(have) < 300; id++ {
		have[id] = 1000
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		allocationPlan = Compute(have, nil, Options{Policy: PolicyFleet})
	}
}
