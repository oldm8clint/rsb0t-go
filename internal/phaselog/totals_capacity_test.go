package phaselog

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

func totalsFixture(phases, used int) (*Recorder, []string) {
	r := NewRecorder(2)
	names := make([]string, phases)
	for i := range names {
		names[i] = fmt.Sprintf("fixture-phase-%d", i)
		r.Observe("catalogue", names[i], at(float64(i)), time.Time{})
	}
	for i := 0; i < used; i++ {
		name := names[phases-used+i]
		r.Observe("target", name, at(float64(i*2)), time.Time{})
		r.Observe("target", name, at(float64(i*2+1)), time.Time{})
	}
	return r, names
}

func TestSparseTotalsExcludeUnusedPhasesAndOwnTheirMap(t *testing.T) {
	r, names := totalsFixture(64, 2)
	want := map[string]int64{names[62]: 1000, names[63]: 2000}
	got, first, ok := r.Totals("target")
	if !ok || !first.Equal(at(0)) || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v/%v/%v, want %v", got, first, ok, want)
	}
	got[names[62]] = 999
	got["injected"] = 1
	again, _, _ := r.Totals("target")
	if !reflect.DeepEqual(again, want) {
		t.Fatalf("returned map aliases recorder: %v", again)
	}
	r.Observe("new", names[63], at(5), time.Time{})
	empty, first, ok := r.Totals("new")
	if !ok || empty == nil || len(empty) != 0 || !first.Equal(at(5)) {
		t.Fatalf("new account changed: %v/%v/%v", empty, first, ok)
	}
	missing, first, ok := r.Totals("missing")
	if ok || missing != nil || !first.IsZero() {
		t.Fatalf("missing account changed: %v/%v/%v", missing, first, ok)
	}
}

var phaseTotalsResult map[string]int64

func BenchmarkPhaseTotalsCapacity(b *testing.B) {
	for _, tc := range []struct{ phases, used int }{{2, 2}, {64, 2}, {255, 2}, {64, 64}} {
		b.Run(fmt.Sprintf("phases-%d-used-%d", tc.phases, tc.used), func(b *testing.B) {
			r, _ := totalsFixture(tc.phases, tc.used)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				phaseTotalsResult, _, _ = r.Totals("target")
			}
		})
	}
}
