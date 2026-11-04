package phaselog

import (
	"fmt"
	"testing"
	"time"
	"unsafe"
)

var t0 = time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

func at(sec float64) time.Time { return t0.Add(time.Duration(sec * float64(time.Second))) }

func TestSegmentIsSixteenBytes(t *testing.T) {
	if got := unsafe.Sizeof(seg{}); got != SegmentBytes {
		t.Fatalf("seg is %d bytes, the package comment's memory estimate assumes %d", got, SegmentBytes)
	}
}

func TestConsecutiveSamplesExtendOneSegment(t *testing.T) {
	r := NewRecorder(8)
	for i := 0; i <= 10; i++ {
		r.Observe("a", Fishing, at(float64(i)), time.Time{})
	}
	segs := r.Segments("a", at(-100), at(100))
	if len(segs) != 1 || segs[0].Phase != Fishing || segs[0].EndMs-segs[0].StartMs != 10000 {
		t.Fatalf("want one 10s fishing segment, got %+v", segs)
	}
}

func TestPhaseChangeUsesReportedSinceBetweenSamples(t *testing.T) {
	r := NewRecorder(8)
	r.Observe("a", Fishing, at(0), time.Time{})
	r.Observe("a", Fishing, at(1), time.Time{})
	// The script switched to banking at 1.4s; the sampler only saw it at 2s.
	r.Observe("a", Banking, at(2), at(1.4))
	r.Observe("a", Banking, at(5), at(1.4))
	s, ok := r.Split("a", at(0), at(5))
	if !ok {
		t.Fatal("account not observed")
	}
	if s.Ms[Fishing] != 1400 || s.Ms[Banking] != 3600 || s.CoveredMs != 5000 {
		t.Fatalf("split = %+v, want fishing 1400 banking 3600 covered 5000", s)
	}
	// A since outside (lastEnd, at) is ignored: it cannot move time already attributed.
	r.Observe("a", Walking, at(6), at(0.5))
	s, _ = r.Split("a", at(5), at(6))
	if s.Ms[Walking] != 1000 {
		t.Fatalf("walking = %d, want the whole bridged second", s.Ms[Walking])
	}
}

func TestLongGapIsNotBridged(t *testing.T) {
	r := NewRecorder(8)
	r.Observe("a", Fishing, at(0), time.Time{})
	r.Observe("a", Fishing, at(100), time.Time{}) // sampler stalled 100s
	s, _ := r.Split("a", at(0), at(100))
	if s.CoveredMs != 0 {
		t.Fatalf("a stalled sampler invented %dms", s.CoveredMs)
	}
	r.Observe("a", Fishing, at(101), time.Time{})
	s, _ = r.Split("a", at(0), at(101))
	if s.Ms[Fishing] != 1000 {
		t.Fatalf("fishing = %d, want only the bridged second after the gap", s.Ms[Fishing])
	}
}

func TestRingIsBoundedButTotalsStayExact(t *testing.T) {
	r := NewRecorder(4)
	phases := []string{Fishing, Walking, Banking, Walking}
	sec := 0.0
	for i := 0; i < 40; i++ { // 40 segments of 10s into a 4-segment ring
		p := phases[i%len(phases)]
		for j := 0; j < 10; j++ {
			sec++
			r.Observe("a", p, at(sec), time.Time{})
		}
	}
	segs := r.Segments("a", at(-1), at(1000))
	if len(segs) != 4 {
		t.Fatalf("ring holds %d segments, want its capacity 4", len(segs))
	}
	totals, first, ok := r.Totals("a")
	if !ok || !first.Equal(at(1)) {
		t.Fatalf("first seen %v, want %v", first, at(1))
	}
	var sum int64
	for _, ms := range totals {
		sum += ms
	}
	if sum != 399000 {
		t.Fatalf("totals sum %dms, want 399000 (every bridged second since first seen)", sum)
	}
	if totals[Fishing] < 90000 {
		t.Fatalf("fishing total %d lost dropped segments", totals[Fishing])
	}
	// The window before the ring's oldest segment is honestly reported as uncovered.
	s, _ := r.Split("a", at(0), at(100))
	if s.CoveredMs != 0 {
		t.Fatalf("covered %dms of a window the ring no longer holds", s.CoveredMs)
	}
}

func TestSegmentsAreClippedToTheWindow(t *testing.T) {
	r := NewRecorder(8)
	for i := 0; i <= 60; i++ {
		p := Fishing
		if i >= 30 {
			p = Banking
		}
		r.Observe("a", p, at(float64(i)), time.Time{})
	}
	segs := r.Segments("a", at(20), at(40))
	if len(segs) != 2 || segs[0].StartMs != at(20).UnixMilli() || segs[1].EndMs != at(40).UnixMilli() {
		t.Fatalf("segments not clipped: %+v", segs)
	}
	if p, since, ok := r.Current("a"); !ok || p != Banking || !since.Equal(at(29)) {
		t.Fatalf("current = %s since %v", p, since)
	}
}

func TestInterningIsCapped(t *testing.T) {
	r := NewRecorder(4)
	for i := 0; i < MaxPhases+50; i++ {
		r.Observe("a", fmt.Sprintf("phase-%d", i), at(float64(i)), time.Time{})
	}
	if len(r.names) > MaxPhases {
		t.Fatalf("interned %d names, cap is %d", len(r.names), MaxPhases)
	}
	if p, _, _ := r.Current("a"); p != Other {
		t.Fatalf("a name past the cap recorded as %q, want %q", p, Other)
	}
}

func TestEmptyPhaseIsUnknownAndOutOfOrderIsIgnored(t *testing.T) {
	r := NewRecorder(4)
	r.Observe("a", "", at(5), time.Time{})
	r.Observe("a", Fishing, at(3), time.Time{})
	if p, _, _ := r.Current("a"); p != Unknown {
		t.Fatalf("current = %q", p)
	}
	if _, ok := r.Split("nobody", at(0), at(1)); ok {
		t.Fatal("an account never observed reported a split")
	}
}

func TestMemoryPerAccountAtDefaultCapacity(t *testing.T) {
	// 267 accounts at DefaultCapacity: the bound quoted in the package comment.
	perAccount := DefaultCapacity * SegmentBytes
	if total := 267 * perAccount; total > 2400*1024 {
		t.Fatalf("267 accounts cost %d bytes of segments, more than the documented ~2.2 MB", total)
	}
}
