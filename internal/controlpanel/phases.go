package controlpanel

import (
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"rsb0t-go/internal/fleet"
	"rsb0t-go/internal/phaselog"
)

// "Where the time goes": the control panel samples every account's phase once a second into
// internal/phaselog's bounded per-account ring, and GET /api/phases answers the split of wall
// time between phases — fleet-wide, per script, per account, and one account's timeline.
//
// The phase comes from the script itself (bot.Context.SetPhase: ProgressiveFisher and
// MuleReceiver report theirs; a script that reports nothing shows as "unknown"), overridden by
// what the sampler knows better: "offline" when the account is not in game, "no script" when
// nothing runs, and "event" while the random-event guardian is handling something.
//
// The random-event overlay is a second, smaller ring per account holding which event the guardian
// was handling and when (its own phase names are the event names, with noEvent between them), so
// the timeline can label an "event" stretch "Genie" and the fleet view can count events by name.

// PhaseSource is optional. A Source that can report every account's current phase cheaply lets
// the control panel record where the time goes. The live fleet answers from
// fleet.Manager.Phases: each account's own lock once, no gamestate lock, no snapshot.
type PhaseSource interface {
	Phases(dst []AccountPhase) []AccountPhase
}

// AccountPhase is fleet.AccountPhase: one account's in-game state, script, reported phase and
// guardian activity.
type AccountPhase = fleet.AccountPhase

// phaseSampleInterval is how often every account's phase is sampled.
var phaseSampleInterval = time.Second

// phaseWindows are the windows /api/phases offers, and the longest is what the ring must hold.
var phaseWindows = map[string]time.Duration{"15m": 15 * time.Minute, "1h": time.Hour, "4h": 4 * time.Hour}

// eventCapacity is the event ring's segments per account: an event and the gap after it are two,
// so it keeps the last 32 events, about 3 hours of a busy account's.
const eventCapacity = 64

// noEvent is the event ring's phase between events.
const noEvent = "-"

type phaseRecorder struct {
	rec *phaselog.Recorder
	// events is the random-event overlay: event names and the gaps between them.
	events *phaselog.Recorder
	src    PhaseSource
	now    func() time.Time

	mu      sync.Mutex
	scripts map[string]string // last script seen per account, kept while offline

	stop chan struct{}
	wg   sync.WaitGroup
	once sync.Once
}

// newPhaseRecorder returns nil when the source cannot report phases.
func newPhaseRecorder(src Source, now func() time.Time) *phaseRecorder {
	ps, ok := src.(PhaseSource)
	if !ok {
		return nil
	}
	return &phaseRecorder{rec: phaselog.NewRecorder(phaselog.DefaultCapacity), events: phaselog.NewRecorder(eventCapacity), src: ps, now: now, scripts: map[string]string{}}
}

func (r *phaseRecorder) start() {
	if r == nil {
		return
	}
	r.stop = make(chan struct{})
	r.wg.Add(1)
	go r.run()
}

func (r *phaseRecorder) close() {
	if r == nil {
		return
	}
	r.once.Do(func() {
		if r.stop != nil {
			close(r.stop)
			r.wg.Wait()
		}
	})
}

func (r *phaseRecorder) run() {
	defer r.wg.Done()
	t := newTicker(phaseSampleInterval)
	defer t.Stop()
	var buf []AccountPhase
	for {
		select {
		case <-r.stop:
			return
		case <-t.C:
			buf = r.src.Phases(buf[:0])
			r.record(buf, r.now())
			if cap(buf) > 2*MaxAccounts {
				buf = nil
			}
		}
	}
}

// effectivePhase is the phase recorded for one sample, and when it began if known.
func effectivePhase(p AccountPhase) (string, time.Time) {
	switch {
	case !p.InGame:
		return phaselog.Offline, time.Time{}
	case p.Event != "":
		return phaselog.Event, p.EventSince
	case !p.Running:
		return phaselog.NoScript, time.Time{}
	case p.Phase == "":
		return phaselog.Unknown, time.Time{}
	default:
		return p.Phase, p.Since
	}
}

func (r *phaseRecorder) record(phases []AccountPhase, now time.Time) {
	r.mu.Lock()
	for _, p := range phases {
		if p.Script != "" && (p.Running || r.scripts[p.Username] == "") {
			r.scripts[p.Username] = p.Script
		}
	}
	r.mu.Unlock()
	for _, p := range phases {
		phase, since := effectivePhase(p)
		r.rec.Observe(p.Username, phase, now, since)
		if p.InGame && p.Event != "" {
			r.events.Observe(p.Username, p.Event, now, p.EventSince)
		} else {
			r.events.Observe(p.Username, noEvent, now, time.Time{})
		}
	}
}

// eventSegments is account's random events overlapping [from, to], clipped, oldest first.
func (r *phaseRecorder) eventSegments(account string, from, to time.Time) []phaselog.Segment {
	segs := r.events.Segments(account, from, to)
	out := segs[:0]
	for _, s := range segs {
		if s.Phase != noEvent {
			out = append(out, s)
		}
	}
	return out
}

func (r *phaseRecorder) script(account string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.scripts[account]
}

// phaseAccountJSON is one account's split as an array parallel to the response's phase list:
// 267 rows of maps would repeat every phase name 267 times.
type phaseAccountJSON struct {
	Name      string  `json:"name"`
	Script    string  `json:"script"`
	Phase     string  `json:"phase"`
	Since     int64   `json:"since"`
	Ms        []int64 `json:"ms"`
	CoveredMs int64   `json:"coveredMs"`
	// Events is how many random events the guardian handled for this account in the window.
	Events int `json:"events"`
}

// phaseEventJSON is one random event's fleet-wide tally over the window.
type phaseEventJSON struct {
	Name     string `json:"name"`
	Count    int    `json:"count"`
	Ms       int64  `json:"ms"`
	Accounts int    `json:"accounts"`
}

type phaseGroupJSON struct {
	Script   string  `json:"script"`
	Accounts int     `json:"accounts"`
	Ms       []int64 `json:"ms"`
}

type phaseTimelineJSON struct {
	Account  string             `json:"account"`
	FromMs   int64              `json:"from"`
	ToMs     int64              `json:"to"`
	Segments []phaselog.Segment `json:"segments"`
	// Events is the random-event overlay: which event the guardian was handling, when.
	Events []phaselog.Segment `json:"events"`
	// Totals is the account's time per phase since it was first seen, not bounded by the ring.
	Totals    map[string]int64 `json:"totals"`
	FirstSeen int64            `json:"firstSeen"`
}

type phasesJSON struct {
	NowMs    int64              `json:"now"`
	Window   string             `json:"window"`
	WindowMs int64              `json:"windowMs"`
	SampleMs int64              `json:"sampleMs"`
	Phases   []string           `json:"phases"`
	Fleet    phaseGroupJSON     `json:"fleet"`
	Scripts  []phaseGroupJSON   `json:"scripts"`
	Accounts []phaseAccountJSON `json:"accounts"`
	// Events is every random event handled in the window, by name, most frequent first.
	Events   []phaseEventJSON   `json:"events"`
	Timeline *phaseTimelineJSON `json:"timeline,omitempty"`
}

// maxTimelineMinutes bounds the Gantt strip; the ring holds about this much for a busy fisher.
const maxTimelineMinutes = 240

func (r *phaseRecorder) query(window string, account string, timelineMin int) phasesJSON {
	now := r.now()
	span := phaseWindows[window]
	from := now.Add(-span)
	out := phasesJSON{NowMs: now.UnixMilli(), Window: window, WindowMs: span.Milliseconds(), SampleMs: phaseSampleInterval.Milliseconds()}
	index := map[string]int{}
	col := func(phase string) int {
		i, ok := index[phase]
		if !ok {
			i = len(out.Phases)
			index[phase] = i
			out.Phases = append(out.Phases, phase)
		}
		return i
	}
	type row struct {
		acc phaseAccountJSON
		ms  map[string]int64
	}
	var rows []row
	events := map[string]*phaseEventJSON{}
	for _, name := range r.rec.Accounts() {
		s, _ := r.rec.Split(name, from, now)
		phase, since, _ := r.rec.Current(name)
		for p := range s.Ms {
			col(p)
		}
		evs := r.eventSegments(name, from, now)
		seen := map[string]bool{}
		for _, ev := range evs {
			e := events[ev.Phase]
			if e == nil {
				e = &phaseEventJSON{Name: ev.Phase}
				events[ev.Phase] = e
			}
			e.Count++
			e.Ms += ev.EndMs - ev.StartMs
			if !seen[ev.Phase] {
				seen[ev.Phase] = true
				e.Accounts++
			}
		}
		rows = append(rows, row{acc: phaseAccountJSON{Name: name, Script: r.script(name), Phase: phase, Since: since.UnixMilli(), CoveredMs: s.CoveredMs, Events: len(evs)}, ms: s.Ms})
	}
	out.Events = []phaseEventJSON{}
	for _, e := range events {
		out.Events = append(out.Events, *e)
	}
	sort.Slice(out.Events, func(i, j int) bool {
		if out.Events[i].Count != out.Events[j].Count {
			return out.Events[i].Count > out.Events[j].Count
		}
		return out.Events[i].Name < out.Events[j].Name
	})
	sort.Strings(out.Phases)
	for i, p := range out.Phases {
		index[p] = i
	}
	vec := func(m map[string]int64) []int64 {
		v := make([]int64, len(out.Phases))
		for p, ms := range m {
			v[index[p]] = ms
		}
		return v
	}
	add := func(dst []int64, src []int64) {
		for i := range src {
			dst[i] += src[i]
		}
	}
	out.Fleet = phaseGroupJSON{Script: "", Ms: make([]int64, len(out.Phases))}
	groups := map[string]*phaseGroupJSON{}
	for _, rw := range rows {
		rw.acc.Ms = vec(rw.ms)
		out.Accounts = append(out.Accounts, rw.acc)
		add(out.Fleet.Ms, rw.acc.Ms)
		out.Fleet.Accounts++
		key := rw.acc.Script
		if key == "" {
			key = "(no script)"
		}
		g := groups[key]
		if g == nil {
			g = &phaseGroupJSON{Script: key, Ms: make([]int64, len(out.Phases))}
			groups[key] = g
		}
		add(g.Ms, rw.acc.Ms)
		g.Accounts++
	}
	for _, g := range groups {
		out.Scripts = append(out.Scripts, *g)
	}
	sort.Slice(out.Scripts, func(i, j int) bool {
		if out.Scripts[i].Accounts != out.Scripts[j].Accounts {
			return out.Scripts[i].Accounts > out.Scripts[j].Accounts
		}
		return out.Scripts[i].Script < out.Scripts[j].Script
	})
	if account != "" {
		if timelineMin <= 0 {
			timelineMin = 30
		}
		tfrom := now.Add(-time.Duration(timelineMin) * time.Minute)
		tl := &phaseTimelineJSON{Account: account, FromMs: tfrom.UnixMilli(), ToMs: now.UnixMilli(), Segments: r.rec.Segments(account, tfrom, now), Events: r.eventSegments(account, tfrom, now)}
		if totals, first, ok := r.rec.Totals(account); ok {
			tl.Totals, tl.FirstSeen = totals, first.UnixMilli()
		}
		out.Timeline = tl
	}
	return out
}

// handlePhases answers GET /api/phases. Query parameters, all optional:
//
//	view      current: return only now and current account phases, without history
//	window    15m, 1h (default) or 4h: the span every split covers
//	account   an account name: adds that account's timeline (Gantt segments) and totals
//	minutes   the timeline's length in minutes, 1-240 (default 30)
func (s *Server) handlePhases(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	if s.phases == nil {
		writeError(w, http.StatusNotImplemented, "this fleet cannot report script phases")
		return
	}
	v := r.URL.Query()
	window := v.Get("window")
	if window == "" {
		window = "1h"
	}
	if _, ok := phaseWindows[window]; !ok {
		writeError(w, http.StatusBadRequest, "window must be 15m, 1h or 4h")
		return
	}
	account := v.Get("account")
	if len(account) > maxNameBytes {
		writeError(w, http.StatusBadRequest, "account name too long")
		return
	}
	minutes := 30
	if m := v.Get("minutes"); m != "" {
		n, err := strconv.Atoi(m)
		if err != nil || n < 1 || n > maxTimelineMinutes {
			writeError(w, http.StatusBadRequest, "minutes must be 1 to 240")
			return
		}
		minutes = n
	}
	if v.Get("view") == "current" {
		writeJSON(w, http.StatusOK, s.phases.current())
		return
	}
	writeJSON(w, http.StatusOK, s.phases.query(window, account, minutes))
}
