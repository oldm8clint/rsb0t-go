package mulereceiverplan

// What the mule logs about its own load (a user request: "we need to optimize that so the mule can get
// back to cooking"): how long kits waited, how much of the time it stood idle, and how many cook rounds an
// hour it got. A rolling window, so the number answers "is the crowd rule helping" an hour after a deploy.

import (
	"fmt"
	"sort"
	"time"
)

// LoadWindow is the span the figures cover.
const LoadWindow = time.Hour

type loadWait struct {
	at   time.Time
	wait time.Duration
}

// LoadStats is the rolling record. The zero value is ready.
type LoadStats struct {
	waits []loadWait
	cooks []time.Time
	idle  []loadWait // at = end of the idle stretch, wait = its length
	since time.Time
	kits  int
}

// NoteKitWait records a kit served after waiting wait in the queue.
func (s *LoadStats) NoteKitWait(now time.Time, wait time.Duration) {
	s.start(now)
	s.waits = append(s.waits, loadWait{now, wait})
	s.kits++
}

// NoteCookRound records a finished cook round.
func (s *LoadStats) NoteCookRound(now time.Time) {
	s.start(now)
	s.cooks = append(s.cooks, now)
}

// NoteIdle records a stretch the mule stood with nothing to do.
func (s *LoadStats) NoteIdle(now time.Time, d time.Duration) {
	s.start(now)
	if d > 0 {
		s.idle = append(s.idle, loadWait{now, d})
	}
}

func (s *LoadStats) start(now time.Time) {
	if s.since.IsZero() {
		s.since = now
	}
}

// Summary is the figures over the window ending now, and whether there is enough to say: the median and
// longest kit wait, the share of the window spent idle, cook rounds an hour.
func (s *LoadStats) Summary(now time.Time) (line string, ok bool) {
	cut := now.Add(-LoadWindow)
	var waits []time.Duration
	for _, w := range s.waits {
		if w.at.After(cut) {
			waits = append(waits, w.wait)
		}
	}
	s.waits = trimWaits(s.waits, cut)
	var idle time.Duration
	for _, i := range s.idle {
		if i.at.After(cut) {
			idle += i.wait
		}
	}
	s.idle = trimWaits(s.idle, cut)
	cooks := 0
	for _, c := range s.cooks {
		if c.After(cut) {
			cooks++
		}
	}
	span := min(LoadWindow, now.Sub(s.since))
	if span < 5*time.Minute {
		return "", false
	}
	sort.Slice(waits, func(i, j int) bool { return waits[i] < waits[j] })
	med, longest := time.Duration(0), time.Duration(0)
	if len(waits) > 0 {
		med, longest = waits[len(waits)/2], waits[len(waits)-1]
	}
	perHour := float64(cooks) * float64(time.Hour) / float64(span)
	return fmt.Sprintf("load over %s: %d kit(s) served, median wait %s, longest %s; idle %d%% of the time; %.1f cook round(s) an hour",
		span.Round(time.Minute), len(waits), med.Round(time.Second), longest.Round(time.Second),
		int(100*idle/max(span, time.Second)), perHour), true
}

func trimWaits(w []loadWait, cut time.Time) []loadWait {
	i := 0
	for i < len(w) && !w[i].at.After(cut) {
		i++
	}
	return w[i:]
}
