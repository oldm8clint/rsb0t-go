package controlpanel

import (
	"fmt"
	"maps"
	"reflect"
	"testing"
)

func TestFlattenPreservesOrderedPairsAndSource(t *testing.T) {
	for _, tc := range []struct {
		name   string
		counts heatCounts
		want   []uint32
	}{
		{"nil", nil, []uint32{}},
		{"empty", heatCounts{}, []uint32{}},
		{"single", heatCounts{19: 7}, []uint32{19, 7}},
		{"unsorted-and-zero", heatCounts{17: 3, 0: 1, 5: 0, 0x3fffffff: ^uint32(0)}, []uint32{0, 1, 5, 0, 17, 3, 0x3fffffff, ^uint32(0)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := maps.Clone(tc.counts)
			for i := 0; i < 20; i++ {
				got := flatten(tc.counts)
				if !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
				if len(got) > 0 {
					got[0] = 999
					got[1] = 999
				}
				if !reflect.DeepEqual(tc.counts, before) {
					t.Fatal("flatten changed source")
				}
			}
		})
	}
}

func TestFlattenRoundTripAcrossManyKeys(t *testing.T) {
	counts := heatCounts{}
	for i := uint32(0); i < 4096; i++ {
		counts[(i*7919)%65536] = i + 1
	}
	flat := flatten(counts)
	for i := 2; i < len(flat); i += 2 {
		if flat[i] <= flat[i-2] {
			t.Fatal("keys are not strictly increasing")
		}
	}
	got, err := unflatten(flat)
	if err != nil || !reflect.DeepEqual(got, counts) {
		t.Fatalf("round trip changed counts: %v", err)
	}
}

var flattenedCounts []uint32

func BenchmarkFlattenHeatCounts(b *testing.B) {
	for _, size := range []int{0, 1, 100, 10000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			counts := make(heatCounts, size)
			for i := 0; i < size; i++ {
				counts[uint32(i)*7919] = uint32(i + 1)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				flattenedCounts = flatten(counts)
			}
		})
	}
}
