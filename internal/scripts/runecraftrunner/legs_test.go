package runecraftrunner

import (
	"sync"
	"testing"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
)

// fakeLegSource is a game state that announces ticks: advance moves the account and emits one EventTick, as a PLAYER_INFO does.
type fakeLegSource struct {
	mu         sync.Mutex
	subs       map[int]gamestate.Subscriber
	next       int
	tick, x, z int
	run        bool
	energy     int
	haveEnergy bool
	noTile     bool
}

func newFakeLegSource() *fakeLegSource {
	return &fakeLegSource{subs: map[int]gamestate.Subscriber{}, x: 2867, z: 3020, energy: 7, haveEnergy: true}
}

func (f *fakeLegSource) Subscribe(fn gamestate.Subscriber) func() {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := f.next
	f.next++
	f.subs[id] = fn
	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		delete(f.subs, id)
	}
}

func (f *fakeLegSource) Tick() int        { f.mu.Lock(); defer f.mu.Unlock(); return f.tick }
func (f *fakeLegSource) RunEnabled() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.run }
func (f *fakeLegSource) Energy() (int, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.energy, f.haveEnergy
}
func (f *fakeLegSource) Tile() (int, int, int, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.x, f.z, 0, !f.noTile
}
func (f *fakeLegSource) subscribers() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.subs) }
func (f *fakeLegSource) set(x, z int, run bool, e int) {
	f.mu.Lock()
	f.x, f.z, f.run, f.energy = x, z, run, e
	f.mu.Unlock()
}

func (f *fakeLegSource) advance() {
	f.mu.Lock()
	f.tick++
	n := f.tick
	subs := make([]gamestate.Subscriber, 0, len(f.subs))
	for _, s := range f.subs {
		subs = append(subs, s)
	}
	f.mu.Unlock()
	for _, s := range subs {
		s(gamestate.Event{Kind: gamestate.EventTick, Tick: n})
	}
}

// Nothing is sampled between cycles; an armed trace samples every tick with the position, the run flag and the energy; Take hands the
// cycle over and stops.
func TestLegTraceSamplesOnlyWhileArmed(t *testing.T) {
	src := newFakeLegSource()
	tr := newLegTrace(src)
	defer tr.Close()
	src.advance()
	src.advance()
	if got := tr.Take(); len(got) != 0 {
		t.Fatalf("%d samples before the cycle was armed", len(got))
	}
	src.set(2867, 3020, false, 7)
	tr.Arm()
	if !tr.Armed() {
		t.Fatal("not armed")
	}
	src.set(2400, 4835, true, 6)
	src.advance() // tick 3
	src.set(2400, 4837, true, 5)
	src.advance() // tick 4
	got := tr.Take()
	if tr.Armed() {
		t.Fatal("still armed after Take")
	}
	if len(got) != 3 {
		t.Fatalf("%d samples, want the arm-time one and two ticks: %+v", len(got), got)
	}
	// The sample taken at the arm is the position at that moment, at the tick the state had reached.
	if got[0].Tick != 2 || got[0].At.X != 2867 || got[0].Run || got[0].Energy != 7 {
		t.Errorf("arm-time sample %+v", got[0])
	}
	if got[1].Tick != 3 || got[1].At.Z != 4835 || !got[1].Run || got[1].Energy != 6 {
		t.Errorf("first tick %+v", got[1])
	}
	if got[2].Tick != 4 || got[2].At.Z != 4837 || got[2].Energy != 5 {
		t.Errorf("second tick %+v", got[2])
	}
	src.advance()
	if again := tr.Take(); len(again) != 0 {
		t.Errorf("sampled %d tick(s) after Take", len(again))
	}
}

// Arming again starts the next cycle clean.
func TestLegTraceArmStartsAfresh(t *testing.T) {
	src := newFakeLegSource()
	tr := newLegTrace(src)
	defer tr.Close()
	tr.Arm()
	src.advance()
	src.advance()
	tr.Arm()
	src.advance()
	if got := tr.Take(); len(got) != 2 || got[0].Tick != 2 || got[1].Tick != 3 {
		t.Fatalf("%+v", got)
	}
}

// A cycle that never ends cannot grow the trace without bound, and the energy the server has not said is -1.
func TestLegTraceIsCappedAndKnowsWhatItDoesNotKnow(t *testing.T) {
	src := newFakeLegSource()
	src.haveEnergy = false
	tr := newLegTrace(src)
	defer tr.Close()
	tr.Arm()
	for i := 0; i < legTraceCap+50; i++ {
		src.advance()
	}
	got := tr.Take()
	if len(got) != legTraceCap {
		t.Fatalf("%d samples, want the cap %d", len(got), legTraceCap)
	}
	if got[0].Energy != -1 {
		t.Errorf("energy %d when the server has not said", got[0].Energy)
	}
}

// Close drops the subscription; a nil trace and a trace with no source do nothing and do not panic.
func TestLegTraceCloseAndNil(t *testing.T) {
	src := newFakeLegSource()
	tr := newLegTrace(src)
	if src.subscribers() != 1 {
		t.Fatalf("%d subscribers", src.subscribers())
	}
	tr.Close()
	tr.Close()
	if src.subscribers() != 0 {
		t.Fatalf("%d subscribers after Close", src.subscribers())
	}
	var nilTrace *LegTrace
	nilTrace.Arm()
	nilTrace.Close()
	if nilTrace.Take() != nil || nilTrace.Armed() {
		t.Error("a nil trace recorded something")
	}
	empty := NewLegTrace(nil)
	empty.Arm()
	empty.Close()
	if len(empty.Take()) != 0 {
		t.Error("a trace with no session recorded something")
	}
}

// Only a tick event samples: a chat line or an inventory change is not a tick.
func TestLegTraceIgnoresEventsThatAreNotTicks(t *testing.T) {
	src := newFakeLegSource()
	tr := newLegTrace(src)
	defer tr.Close()
	tr.Arm()
	for _, s := range src.subs {
		s(gamestate.Event{Kind: gamestate.EventChatMessage})
		s(gamestate.Event{Kind: gamestate.EventInventoryChanged})
	}
	if got := tr.Take(); len(got) != 1 {
		t.Fatalf("%d samples, want only the arm-time one", len(got))
	}
}

// Without a config the reach test does not exist, and a caller that gets none leaves the leg unmeasured.
func TestReachesFromNeedsAConfig(t *testing.T) {
	if ReachesFrom(nil, locindex.Match{}) != nil {
		t.Error("a reach test from no deps")
	}
}

// A tick at which the account's tile is not known (it is not in the game) is no sample: a zero tile would read as outside the temple.
func TestLegTraceSkipsATickWithNoKnownTile(t *testing.T) {
	src := newFakeLegSource()
	src.noTile = true
	tr := newLegTrace(src)
	defer tr.Close()
	tr.Arm()
	src.advance()
	src.noTile = false
	src.advance()
	if got := tr.Take(); len(got) != 1 || got[0].Tick != 2 {
		t.Fatalf("%+v", got)
	}
}
