// Package phaselog records where each account's wall time goes: a bounded, run-length ring of
// phase segments per account ("fishing" from 12:00:03 to 12:02:41, then "walking", ...), plus
// cumulative per-phase totals since the account was first seen.
//
// A script reports its current phase cheaply (bot.Context.SetPhase stores a string); something
// outside the script samples every account about once a second and feeds the samples here
// (internal/controlpanel's phase recorder does). Consecutive samples of the same phase extend one
// segment, so a fisher that changes phase a couple of times a minute costs a couple of segments a
// minute, not sixty samples.
//
// Memory is bounded twice: each account keeps at most Capacity segments (the oldest is dropped),
// and phase names are interned into a table of at most MaxPhases entries, past which new names
// are recorded as Other. At the default 512 segments a segment is 16 bytes, so 267 accounts cost
// about 2.2 MB however long the process runs.
//
// No imports beyond the standard library and no I/O: the recorder is pure bookkeeping and is
// unit-tested without a session.
package phaselog

import (
	"sort"
	"sync"
	"time"
)

// Canonical phase names. A script may report any string; these are the ones the control panel
// colours consistently and the fisher/mule scripts use. The first group is what the sampler
// itself reports when it knows better than the script.
const (
	Offline    = "offline"   // not in game (logged out, queued, reconnecting)
	NoScript   = "no script" // in game with no script running
	Unknown    = "unknown"   // a script is running but has never reported a phase
	Event      = "event"     // the random-event guardian is handling something
	Other      = "other"     // a phase name past the MaxPhases interning cap
	Fishing    = "fishing"
	Banking    = "banking"
	Walking    = "walking"
	Handoff    = "handoff"
	Recovering = "recovering"
	Stranded   = "stranded"
	Shopping   = "shopping"
	Setup      = "setup"
	Idle       = "idle"
	Trading    = "trading"
	Cooking    = "cooking"
	Handout    = "handout"
	Looting    = "looting" // opening a loot source: the crystal chest, a casket
	// Woodcutting is chopping, as Fishing is fishing: the script's own productive work.
	// A script that never reports anything reads as 0% on EVERY phase, which makes the
	// panel's time split quietly wrong about the whole fleet rather than merely silent
	// about that script — found live 2026-09-21 with two ProgressiveWoodcutter accounts
	// producing levels while showing zero time in every column.
	Woodcutting = "woodcutting"
	// Fletching is cutting logs with a knife — ProgressiveWCFletch's own productive work, kept
	// separate from Woodcutting rather than folded into it because the whole reason that script
	// exists is to find out how its time splits between chopping and cutting. A run that
	// reported both as "woodcutting" could not answer the question it was started to answer.
	Fletching = "fletching"
)

const (
	// DefaultCapacity is how many segments each account keeps.
	DefaultCapacity = 512
	// MaxPhases caps the interning table; a runaway script inventing a phase per loop cannot
	// grow memory past it.
	MaxPhases = 256
	// maxGap is the longest silence between two samples that is still bridged. A longer one
	// (the sampler stalled, or the process slept) leaves a hole rather than inventing time.
	maxGap = 15 * time.Second
)

// Segment is one continuous stretch of one phase.
type Segment struct {
	Phase   string `json:"phase"`
	StartMs int64  `json:"start"`
	EndMs   int64  `json:"end"`
}

// seg is Segment in 16 bytes: the phase is an index into the recorder's table and the length
// is milliseconds (49 days fits in 32 bits).
type seg struct {
	start int64
	dur   uint32
	phase uint16
}

func (s seg) end() int64 { return s.start + int64(s.dur) }

type ring struct {
	segs   []seg // circular, len == capacity once full
	head   int   // index of the oldest segment once full
	n      int
	totals []int64 // ms per phase index since first seen
	first  int64
}

func (r *ring) last() *seg {
	if r.n == 0 {
		return nil
	}
	return &r.segs[(r.head+r.n-1)%len(r.segs)]
}

func (r *ring) push(s seg) {
	if r.n < len(r.segs) {
		r.segs[(r.head+r.n)%len(r.segs)] = s
		r.n++
		return
	}
	r.segs[r.head] = s
	r.head = (r.head + 1) % len(r.segs)
}

func (r *ring) each(fn func(seg)) {
	for i := 0; i < r.n; i++ {
		fn(r.segs[(r.head+i)%len(r.segs)])
	}
}

// Recorder holds every account's ring. Safe for concurrent use.
type Recorder struct {
	mu       sync.Mutex
	capacity int
	names    []string
	index    map[string]uint16
	accounts map[string]*ring
}

// NewRecorder returns a recorder keeping capacity segments per account (DefaultCapacity when
// capacity <= 0).
func NewRecorder(capacity int) *Recorder {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &Recorder{capacity: capacity, index: map[string]uint16{}, accounts: map[string]*ring{}}
}

func (r *Recorder) intern(name string) uint16 {
	if i, ok := r.index[name]; ok {
		return i
	}
	if len(r.names) >= MaxPhases-1 {
		if name != Other {
			return r.intern(Other)
		}
	}
	i := uint16(len(r.names))
	r.names = append(r.names, name)
	r.index[name] = i
	return i
}

// Observe records that account was in phase at time at. since, when non-zero, is when the
// reporter says the phase began; it only moves a new segment's start earlier, never before the
// previous segment's end, so a precise script timestamp tightens the boundary a 1 Hz sample
// would otherwise blur.
func (r *Recorder) Observe(account, phase string, at, since time.Time) {
	if phase == "" {
		phase = Unknown
	}
	atMs := at.UnixMilli()
	r.mu.Lock()
	defer r.mu.Unlock()
	rg := r.accounts[account]
	if rg == nil {
		rg = &ring{segs: make([]seg, r.capacity), first: atMs}
		r.accounts[account] = rg
	}
	p := r.intern(phase)
	for len(rg.totals) <= int(p) {
		rg.totals = append(rg.totals, 0)
	}
	last := rg.last()
	if last != nil && atMs <= last.end() {
		return // out of order or duplicate sample
	}
	bridged := last != nil && atMs-last.end() <= maxGap.Milliseconds()
	if last != nil && last.phase == p && bridged {
		d := atMs - last.end()
		if int64(last.dur)+d > int64(^uint32(0)) {
			// Past 49 days in one segment: start a fresh one rather than overflow.
			rg.push(seg{start: last.end(), dur: uint32(d), phase: p})
		} else {
			last.dur += uint32(d)
		}
		rg.totals[p] += d
		return
	}
	start := atMs
	if bridged {
		start = last.end()
		if s := since.UnixMilli(); !since.IsZero() && s > start && s < atMs {
			// The phase began between two samples: the old phase ran until then.
			d := s - last.end()
			last.dur += uint32(d)
			rg.totals[last.phase] += d
			start = s
		}
	}
	d := atMs - start
	rg.push(seg{start: start, dur: uint32(d), phase: p})
	rg.totals[p] += d
}

// Split is how an account's (or the fleet's) time in a window divides between phases.
type Split struct {
	// Ms is milliseconds per phase inside the window.
	Ms map[string]int64 `json:"ms"`
	// CoveredMs is the sum of Ms: how much of the window the ring actually covers (a ring that
	// has dropped older segments, or an account first seen mid-window, covers less).
	CoveredMs int64 `json:"coveredMs"`
}

func (r *Recorder) splitLocked(rg *ring, fromMs, toMs int64, into map[string]int64) int64 {
	var covered int64
	rg.each(func(s seg) {
		a, b := s.start, s.end()
		if a < fromMs {
			a = fromMs
		}
		if b > toMs {
			b = toMs
		}
		if b > a {
			into[r.names[s.phase]] += b - a
			covered += b - a
		}
	})
	return covered
}

// Split reports account's time between from and to. ok is false for an account never observed.
func (r *Recorder) Split(account string, from, to time.Time) (Split, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rg := r.accounts[account]
	if rg == nil {
		return Split{Ms: map[string]int64{}}, false
	}
	out := Split{Ms: map[string]int64{}}
	out.CoveredMs = r.splitLocked(rg, from.UnixMilli(), to.UnixMilli(), out.Ms)
	return out, true
}

// Totals is account's cumulative time per phase since it was first observed — not bounded by
// the ring, so it stays exact after old segments are dropped.
func (r *Recorder) Totals(account string) (map[string]int64, time.Time, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rg := r.accounts[account]
	if rg == nil {
		return nil, time.Time{}, false
	}
	// Phase indices are shared across accounts; most of these slots can be zero.
	count := 0
	for _, ms := range rg.totals {
		if ms > 0 {
			count++
		}
	}
	out := make(map[string]int64, count)
	for i, ms := range rg.totals {
		if ms > 0 {
			out[r.names[i]] = ms
		}
	}
	return out, time.UnixMilli(rg.first), true
}

// Segments lists account's segments overlapping [from, to], oldest first, clipped to the window.
func (r *Recorder) Segments(account string, from, to time.Time) []Segment {
	r.mu.Lock()
	defer r.mu.Unlock()
	rg := r.accounts[account]
	if rg == nil {
		return nil
	}
	fromMs, toMs := from.UnixMilli(), to.UnixMilli()
	segmentAt := func(i int) seg { return rg.segs[(rg.head+i)%len(rg.segs)] }
	// Both starts and ends are monotonic in chronological ring order. Find
	// the overlapping range without visiting all retained history.
	first := sort.Search(rg.n, func(i int) bool { return segmentAt(i).end() > fromMs })
	count := sort.Search(rg.n-first, func(i int) bool { return segmentAt(first+i).start >= toMs })
	if count == 0 {
		return nil
	}
	out := make([]Segment, count)
	for i := range out {
		s := segmentAt(first + i)
		a, b := s.start, s.end()
		if a < fromMs {
			a = fromMs
		}
		if b > toMs {
			b = toMs
		}
		out[i] = Segment{Phase: r.names[s.phase], StartMs: a, EndMs: b}
	}
	return out
}

// Current is account's latest phase and when its segment began.
func (r *Recorder) Current(account string) (phase string, since time.Time, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rg := r.accounts[account]
	if rg == nil || rg.n == 0 {
		return "", time.Time{}, false
	}
	last := rg.last()
	return r.names[last.phase], time.UnixMilli(last.start), true
}

// Accounts lists every observed account, sorted.
func (r *Recorder) Accounts() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.accounts))
	for name := range r.accounts {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Forget drops an account's ring and totals.
func (r *Recorder) Forget(account string) {
	r.mu.Lock()
	delete(r.accounts, account)
	r.mu.Unlock()
}

// SegmentBytes is the in-memory size of one stored segment, for the memory estimate in the
// package comment and its test.
const SegmentBytes = 16
