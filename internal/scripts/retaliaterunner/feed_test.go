package retaliaterunner

import (
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
)

// fakeSource is a feedSource the test drives by hand.
type fakeSource struct {
	mu      sync.Mutex
	subs    []gamestate.Subscriber
	tick    int
	anim    int
	animAt  int
	hitTick int
	hitSeen bool
}

func (f *fakeSource) Subscribe(fn gamestate.Subscriber) func() {
	f.mu.Lock()
	f.subs = append(f.subs, fn)
	f.mu.Unlock()
	return func() {}
}
func (f *fakeSource) Tick() int { return f.tick }
func (f *fakeSource) SelfAnim() (int, int) {
	return f.anim, f.animAt
}
func (f *fakeSource) LastSelfHit() (int, bool) { return f.hitTick, f.hitSeen }

func (f *fakeSource) emit(e gamestate.Event) {
	for _, s := range f.subs {
		s(e)
	}
}

// advance plays one PLAYER_INFO: the state is updated first, then the tick is announced, as gamestate does.
func (f *fakeSource) advance(anim int, hit bool) {
	f.tick++
	if anim >= 0 {
		f.anim, f.animAt = anim, f.tick
	}
	if hit {
		f.hitTick, f.hitSeen = f.tick, true
	}
	f.emit(gamestate.Event{Kind: gamestate.EventTick, Tick: f.tick})
}

func TestFeedReadsHitsAndChopAnimationsPerTick(t *testing.T) {
	src := &fakeSource{}
	f := newFeed(src)
	defer f.stop()
	src.advance(-1, false)  // 1: nothing
	src.advance(867, false) // 2: a rune axe swing
	src.advance(-1, true)   // 3: a hit
	src.advance(1248, true) // 4: some other animation (fletching) and a hit
	src.advance(-1, false)  // 5: the old hit and the old animation must not repeat

	want := []struct {
		n         int
		hit, roll bool
	}{{1, false, false}, {2, false, true}, {3, true, false}, {4, true, false}, {5, false, false}}
	for _, w := range want {
		o, ok := f.pop()
		if !ok || o.N != w.n || o.Hit != w.hit || o.Roll != w.roll {
			t.Fatalf("tick %d: got %+v ok %v, want hit %v roll %v", w.n, o, ok, w.hit, w.roll)
		}
	}
	if _, ok := f.pop(); ok {
		t.Fatal("extra observation")
	}
}

func TestFeedCountsLogsAndRefusedAttacks(t *testing.T) {
	src := &fakeSource{}
	f := newFeed(src)
	defer f.stop()
	game := func(s string) {
		src.emit(gamestate.Event{Kind: gamestate.EventChatMessage, ChatType: gamestate.ChatGame, Text: s})
	}
	game("You get some oak logs.")
	game("You get some oak logs.")
	game("There is no ammo left in your quiver.")
	game("You swing your axe at the tree.")
	src.emit(gamestate.Event{Kind: gamestate.EventChatMessage, ChatType: gamestate.ChatPublic, Text: "you get some oak logs"})
	if f.logs.Load() != 2 || f.refused.Load() != 1 {
		t.Fatalf("logs %d refused %d, want 2 and 1 (a public chat line is not a game line)", f.logs.Load(), f.refused.Load())
	}
}

func TestFeedNextWaitsForATickAndStops(t *testing.T) {
	src := &fakeSource{}
	f := newFeed(src)
	defer f.stop()
	go func() {
		time.Sleep(30 * time.Millisecond)
		src.advance(-1, true)
	}()
	o, ok, stopped := f.next(func(time.Duration) bool { return true }, time.Second)
	if !ok || stopped || o.N != 1 || !o.Hit {
		t.Fatalf("got %+v ok %v stopped %v", o, ok, stopped)
	}
	if _, ok, _ := f.next(func(time.Duration) bool { return true }, 20*time.Millisecond); ok {
		t.Fatal("a tick appeared from nowhere")
	}
	if _, _, stopped := f.next(func(time.Duration) bool { return false }, time.Second); !stopped {
		t.Fatal("a stop request was ignored")
	}
}

func TestFeedKeepsTheNewest64(t *testing.T) {
	src := &fakeSource{}
	f := newFeed(src)
	defer f.stop()
	for i := 0; i < 100; i++ {
		src.advance(-1, false)
	}
	o, _ := f.pop()
	if o.N != 37 {
		t.Fatalf("oldest kept tick %d, want 37", o.N)
	}
}
