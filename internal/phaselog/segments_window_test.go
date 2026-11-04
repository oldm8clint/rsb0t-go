package phaselog

import (
	"reflect"
	"testing"
	"time"
)

func TestSegmentWindowsAfterWrapAndGap(t *testing.T) {
	r := NewRecorder(4)
	r.Observe("target", "a", at(0), time.Time{})
	r.Observe("target", "a", at(1), time.Time{})
	for i, p := range []string{"b", "c", "d", "e"} {
		r.Observe("target", p, at(float64(i+2)), time.Time{})
	}
	r.Observe("target", "f", at(30), time.Time{})
	segment := func(p string, start, end float64) Segment {
		return Segment{Phase: p, StartMs: at(start).UnixMilli(), EndMs: at(end).UnixMilli()}
	}
	for _, tc := range []struct {
		name     string
		from, to float64
		want     []Segment
	}{
		{"all", -1, 40, []Segment{segment("c", 2, 3), segment("d", 3, 4), segment("e", 4, 5), segment("f", 30, 30)}},
		{"clipped", 2.5, 4.5, []Segment{segment("c", 2.5, 3), segment("d", 3, 4), segment("e", 4, 4.5)}},
		{"exact-boundary", 3, 4, []Segment{segment("d", 3, 4)}},
		{"gap", 5, 30, nil},
		{"zero-length-segment", 29, 31, []Segment{segment("f", 30, 30)}},
		{"dropped", -20, 1, nil},
		{"future", 40, 50, nil},
		// Preserve existing clipping behavior even for degenerate caller windows.
		{"zero-window", 3.5, 3.5, []Segment{segment("d", 3.5, 3.5)}},
		{"reversed-window", 3.8, 3.2, []Segment{segment("d", 3.8, 3.2)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := r.Segments("target", at(tc.from), at(tc.to))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			if len(got) > 0 {
				got[0].Phase = "caller mutation"
				if again := r.Segments("target", at(tc.from), at(tc.to)); !reflect.DeepEqual(again, tc.want) {
					t.Fatal("returned result aliases recorder")
				}
			}
		})
	}
	if got := r.Segments("missing", at(0), at(40)); got != nil {
		t.Fatalf("missing account: %v", got)
	}
}

var phaseSegmentsResult []Segment

func BenchmarkPhaseSegmentWindow(b *testing.B) {
	r := NewRecorder(DefaultCapacity)
	for i := 0; i <= DefaultCapacity; i++ {
		name := Fishing
		if i%2 != 0 {
			name = Banking
		}
		r.Observe("target", name, at(float64(i)), time.Time{})
	}
	for _, tc := range []struct {
		name     string
		from, to float64
	}{
		{"full", -1, 513}, {"last-two", 510, 513}, {"future", 600, 610}, {"before-history", -10, -5},
	} {
		b.Run(tc.name, func(b *testing.B) {
			from, to := at(tc.from), at(tc.to)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				phaseSegmentsResult = r.Segments("target", from, to)
			}
		})
	}
}
