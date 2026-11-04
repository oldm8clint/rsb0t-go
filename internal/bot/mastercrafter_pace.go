package bot

import (
	"fmt"
	"time"
)

// masterPaceEvery is how many loads a round's pace line covers: one log line every so many loads,
// not one a load (a quick load is about four seconds).
const masterPaceEvery = 25

// masterPace is a load-by-load round's pace, for the log: how long the bank visits and the work took,
// how many visits were quick, and the XP an hour that came to. The timing users asked for: where
// a load's time goes.
type masterPace struct {
	loads, quick, fallbacks int
	// fast is the loads made by six-tick runs (counted in loads too), desyncs the runs that fell
	// out of step.
	fast, desyncs int
	bank, work    time.Duration
	ticks, xp     int
	start         time.Time
	said          int
}

// add records one load: whether its bank visit was quick, the visit's and the work's time, the
// server ticks the work spanned and the XP it made.
func (p *masterPace) add(quick bool, bank, work time.Duration, ticks, xp int) {
	if p.start.IsZero() {
		p.start = time.Now().Add(-bank - work)
	}
	p.loads++
	if quick {
		p.quick++
	}
	p.bank += bank
	p.work += work
	p.ticks += ticks
	p.xp += xp
}

// addFast records a six-tick run: its whole loads, its time (bank and work are one stream, so it
// all counts as work), its ticks and XP.
func (p *masterPace) addFast(loads int, took time.Duration, ticks, xp int) {
	if p.start.IsZero() {
		p.start = time.Now().Add(-took)
	}
	p.loads += loads
	p.fast += loads
	p.work += took
	p.ticks += ticks
	p.xp += xp
}

// due reports whether a pace line is due, and if so counts it said.
func (p *masterPace) due() bool {
	if p.loads-p.said < masterPaceEvery {
		return false
	}
	p.said = p.loads
	return true
}

// say is the pace line.
func (p *masterPace) say() string {
	if p.loads == 0 {
		return "no loads yet"
	}
	n := time.Duration(p.loads)
	wall := time.Since(p.start)
	perHour := 0
	if wall > 0 {
		perHour = int(float64(p.xp) / wall.Hours())
	}
	return fmt.Sprintf("%d load(s) (%d six-tick, %d quick, %d fell back, %d desync(s)) in %s: a load %.1fs (bank %.1fs, work %.1fs over %.1f ticks), +%d XP, %d XP/h",
		p.loads, p.fast, p.quick, p.fallbacks, p.desyncs, wall.Round(time.Second), (wall / n).Seconds(), (p.bank / n).Seconds(), (p.work / n).Seconds(),
		float64(p.ticks)/float64(p.loads), p.xp, perHour)
}
