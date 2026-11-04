package runecraftrunner

import (
	"sync"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/locreach"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/tile"
)

// THE GATE CYCLE'S TICK TRACE (CR-3 step 1, runecraftplan/legs.go has the why and the format).
//
// A LegTrace samples the crafter once per server tick while a cycle is under way: where he stood, whether the run setting was on and his
// run energy. It is the only way to know the tick a walk ENDED in: the walkers poll on the clock, so a position read at the end of a
// walk is a tick or two later than the arrival, and the legs this trace exists to measure are two to five ticks long.
//
// It only reads. The subscription is made once per gate round and the callback runs on the goroutine that feeds the game state, so it
// takes the tile, the varp and the energy (each a read lock of its own, taken after the state released its write lock: events.go) and
// appends to a slice under its own mutex, nothing else. Sampling is off between cycles and capped within one.

// legSource is the part of gamestate.State the trace reads (a test supplies its own).
type legSource interface {
	Subscribe(gamestate.Subscriber) (cancel func())
	Tick() int
	Tile() (x, z, level int, ok bool)
	RunEnabled() bool
	Energy() (energy int, ok bool)
}

// legTraceCap bounds the samples of one cycle: four minutes of ticks, far past any cycle (about 30 ticks) or even a lag-stretched one.
const legTraceCap = 400

// LegTrace is the sampler.
type LegTrace struct {
	src    legSource
	cancel func()

	mu      sync.Mutex
	armed   bool
	samples []runecraftplan.LegSample
}

// NewLegTrace subscribes to the session's ticks. A nil Deps or session gives a trace that records nothing.
func NewLegTrace(d *fleetrunner.Deps) *LegTrace {
	if d == nil || d.Session == nil {
		return &LegTrace{}
	}
	return newLegTrace(d.Session.State())
}

func newLegTrace(src legSource) *LegTrace {
	t := &LegTrace{src: src}
	t.cancel = src.Subscribe(func(e gamestate.Event) {
		if e.Kind == gamestate.EventTick {
			t.sample(e.Tick)
		}
	})
	return t
}

func (t *LegTrace) sample(tick int) {
	t.mu.Lock()
	armed := t.armed && len(t.samples) < legTraceCap
	t.mu.Unlock()
	if !armed {
		return
	}
	// A tick with no known tile (the account is not in the game) is not a sample: a zero tile would read as "outside the temple".
	x, z, level, ok := t.src.Tile()
	if !ok {
		return
	}
	s := runecraftplan.LegSample{Tick: tick, Energy: -1, At: tile.Tile{X: x, Z: z, Level: level}}
	s.Run = t.src.RunEnabled()
	if e, ok := t.src.Energy(); ok {
		s.Energy = e
	}
	t.mu.Lock()
	if t.armed && len(t.samples) < legTraceCap {
		t.samples = append(t.samples, s)
	}
	t.mu.Unlock()
}

// Arm starts a cycle's samples afresh, taking the first at once (the tick the source is at now) so a leg that begins before the next
// tick event has its starting position.
func (t *LegTrace) Arm() {
	if t == nil || t.src == nil {
		return
	}
	t.mu.Lock()
	t.armed = true
	t.samples = t.samples[:0]
	t.mu.Unlock()
	t.sample(t.src.Tick())
}

// Take stops the sampling and returns the cycle's samples.
func (t *LegTrace) Take() []runecraftplan.LegSample {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.armed = false
	out := make([]runecraftplan.LegSample, len(t.samples))
	copy(out, t.samples)
	t.samples = t.samples[:0]
	return out
}

// Armed reports whether a cycle is being sampled.
func (t *LegTrace) Armed() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.armed
}

// Close drops the subscription.
func (t *LegTrace) Close() {
	if t != nil && t.cancel != nil {
		t.cancel()
	}
}

// ReachesFrom is a test on a tile for "the engine accepts an option on the loc from here" (internal/locreach: the loc's footprint
// and the sides it can be reached from), the same rule Reached applies to the account's own tile. It is never true for a loc the
// config does not know or a shape locreach does not model.
func ReachesFrom(d *fleetrunner.Deps, m locindex.Match) func(tile.Tile) bool {
	if d == nil || d.Config == nil {
		return nil
	}
	typ, ok := d.Config.Loc(m.ID)
	if !ok {
		return nil
	}
	l := locreach.Loc{X: m.X, Z: m.Z, Level: m.Level, Width: typ.Width, Length: typ.Length, Angle: m.Angle, Shape: m.Shape, ForceApproach: typ.ForceApproach}
	return func(t tile.Tile) bool {
		if t.Level != m.Level {
			return false
		}
		reached, modelled := locreach.Reached(t.X, t.Z, l)
		return modelled && reached
	}
}
