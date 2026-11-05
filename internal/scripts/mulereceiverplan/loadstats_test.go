package mulereceiverplan

import (
	"strings"
	"testing"
	"time"
)

func TestLoadStatsSummary(t *testing.T) {
	var s LoadStats
	t0 := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	s.NoteIdle(t0, time.Second)
	if _, ok := s.Summary(t0.Add(time.Minute)); ok {
		t.Fatal("under five minutes of record says nothing")
	}
	for i, w := range []time.Duration{10 * time.Second, 50 * time.Second, 300 * time.Second} {
		s.NoteKitWait(t0.Add(time.Duration(i+1)*time.Minute), w)
	}
	s.NoteCookRound(t0.Add(10 * time.Minute))
	s.NoteCookRound(t0.Add(20 * time.Minute))
	s.NoteIdle(t0.Add(25*time.Minute), 6*time.Minute)
	line, ok := s.Summary(t0.Add(30 * time.Minute))
	if !ok {
		t.Fatal("30 minutes of record should summarise")
	}
	for _, want := range []string{"3 kit(s)", "median wait 50s", "longest 5m0s", "idle 20%", "4.0 cook round(s) an hour"} {
		if !strings.Contains(line, want) {
			t.Errorf("%q not in %q", want, line)
		}
	}
	// Old figures leave the window.
	if line, _ := s.Summary(t0.Add(3 * time.Hour)); !strings.Contains(line, "0 kit(s)") {
		t.Errorf("window not rolling: %q", line)
	}
}
