package swarmrally

import (
	"fmt"
	"sync"
	"time"
)

// Waves is a rally that RE-ARMS: a sequence of generations, each an ordinary Rally.
//
// A Rally latches once and stays latched for the life of the process, which is right for one
// gather and wrong for a swarm that dies and comes back. Stage 6's cycle is dying, and with a
// single Shared rally only the first gather ever happened: every later life, and every account
// that arrived after the latch, crossed alone the moment it landed — which is exactly the lone
// crossing the gather exists to replace (lab 2026-09-23: 0 of 39 lone lives reached her).
//
// So each life JOINS the current generation. While that generation is gathering, every account
// that joins it counts toward its quorum. Once it latches — engaged or aborted — the next account
// to join opens generation+1, with a fresh MaxWait, and that is the next wave. An account keeps the
// generation it joined for the rest of its life; it never moves to a later one mid-crossing, which
// is what keeps "who crossed with whom" a fact rather than a guess.
type Waves struct {
	p   Policy
	now func() time.Time

	mu  sync.Mutex
	gen int
	cur *Rally
	// recent holds the last WavesKept generations, oldest first, so a reader can ask about a
	// generation after the next has opened (Gen). Bounded, because a swarm that dies and comes
	// back opens a generation every few minutes for as long as the process lives.
	recent []*Rally
	// latched counts the generations that have latched, by verdict, for a report.
	latched map[Verdict]int
}

// WavesKept is how many recent generations Gen can still answer for.
const WavesKept = 32

// NewWaves opens a wave sequence; now may be nil for time.Now. The first generation opens on the
// first Join, not here, so its MaxWait is counted from the first account that actually arrives.
func NewWaves(p Policy, now func() time.Time) (*Waves, error) {
	if err := p.Valid(); err != nil {
		return nil, err
	}
	if now == nil {
		now = time.Now
	}
	return &Waves{p: p, now: now, latched: map[Verdict]int{}}, nil
}

// Join returns the generation an arriving account belongs to: the current one while it is still
// gathering, else a newly opened next one. Call it ONCE per life, when the account reaches the
// muster, and keep the answer until the life ends.
func (w *Waves) Join() (*Rally, int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cur != nil {
		if d := w.cur.Decide(); !d.Verdict.Terminal() {
			return w.cur, w.gen
		}
	}
	r, err := New(w.p, w.now)
	if err != nil {
		// Unreachable: the policy was validated by NewWaves and New checks nothing else.
		panic(fmt.Sprintf("swarmrally: a validated policy was refused: %v", err))
	}
	w.gen++
	w.cur = r
	w.recent = append(w.recent, r)
	if len(w.recent) > WavesKept {
		w.recent = w.recent[len(w.recent)-WavesKept:]
	}
	return r, w.gen
}

// Gen is generation n, or nil when it has not opened or is older than the last WavesKept. A
// reader that watches one wave needs this rather than Current: the first account the latch leaves
// behind opens the next generation at once, and Current moves on to it.
func (w *Waves) Gen(n int) *Rally {
	w.mu.Lock()
	defer w.mu.Unlock()
	i := len(w.recent) - 1 - (w.gen - n)
	if n <= 0 || n > w.gen || i < 0 {
		return nil
	}
	return w.recent[i]
}

// Current is the newest generation without opening one, or (nil, 0) before the first Join.
func (w *Waves) Current() (*Rally, int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.cur, w.gen
}

// NoteLatched records a generation's terminal verdict once, for Counts. A caller that saw the
// latch reports it; reporting the same generation twice is harmless only if it is done once, so
// the bot does it from the member that first sees it.
func (w *Waves) NoteLatched(gen int, v Verdict) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if gen == w.gen && v.Terminal() {
		w.latched[v]++
	}
}

// Counts is how many generations latched with each verdict, as reported through NoteLatched.
func (w *Waves) Counts() map[Verdict]int {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make(map[Verdict]int, len(w.latched))
	for k, v := range w.latched {
		out[k] = v
	}
	return out
}

// Policy is the rule every generation is opened with.
func (w *Waves) Policy() Policy { return w.p }

var (
	wavesMu sync.Mutex
	waves   = map[string]*Waves{}
)

// SharedWaves returns the wave sequence called name, creating it from p the first time; later
// callers get the same one and p is ignored, as with Shared. The bool says whether this call
// created it. Wave names and rally names are separate namespaces.
func SharedWaves(name string, p Policy) (*Waves, bool, error) {
	if name == "" {
		return nil, false, fmt.Errorf("a wave sequence needs a name; an empty one would pool every script in the process")
	}
	wavesMu.Lock()
	defer wavesMu.Unlock()
	if w, ok := waves[name]; ok {
		return w, false, nil
	}
	w, err := NewWaves(p, nil)
	if err != nil {
		return nil, false, err
	}
	waves[name] = w
	return w, true, nil
}

// LookupWaves returns an existing wave sequence, or nil. It never creates one.
func LookupWaves(name string) *Waves {
	wavesMu.Lock()
	defer wavesMu.Unlock()
	return waves[name]
}

// ForgetWaves drops a wave sequence. For tests and harnesses only, for the reason Forget gives.
func ForgetWaves(name string) {
	wavesMu.Lock()
	defer wavesMu.Unlock()
	delete(waves, name)
}
