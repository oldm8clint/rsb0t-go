package randomevents

import (
	"sync"
	"time"

	"rsb0t-go/internal/actions"
)

// WHAT A MEASURING WINDOW NEEDS TO KNOW ABOUT RANDOM EVENTS.
//
// Anything that measures the pack across an interval -- the Loot panel's kill, pickup and gathering
// windows -- is wrong if a random event pays out inside it, because the reward lands in the same
// pack and is indistinguishable from what the window was watching for. Live on 5 Oct 2026 (build
// 73ff4228) the lifetime Cow card carried Feather x5,014, Casket x70, Mithril ore x59, Coal x265 and
// Strange fruit x17, and the Chicken and Goblin cards carried the same, plus Lamps: cows drop none
// of it. Those are the Maze, Mysterious Old Man and Strange plant rewards, and a ScarabSwarm
// account's take of a bone is exactly such a window -- the pack is read before the walk to the
// bone, the Guardian takes the account's action lock and handles an event for anything from
// seconds (a Genie) to minutes (the Maze), the script resumes, takes the bone, and the "gain" it
// reads back includes the whole event.
//
// Busy cannot answer that question: it says whether an event is being handled NOW, and the one
// that matters is usually over by the time the window closes. The Guardian already knows, so it
// keeps a counter and a window asks "has it moved since I opened".
//
// The counter, Guardian.eventSeq, is a seqlock: odd while a handler runs, even between handlers, and
// it only ever rises. A reading is therefore clean only when the counter is even and a later
// reading finds the same value -- no handler began or ended in between -- and a handler that ended
// only moments before the first reading can still have inventory updates in flight, so a reading
// taken inside eventSettle of the last one is not clean either.

// eventSettle is how long after a handler finishes its rewards can still be arriving in the pack.
// internal/fleet's RandomEventLoot waits 1.2s for the same thing; this is a little longer because
// a window that opens just inside it would be wrong for a whole take, not for one reading.
const eventSettle = 2 * time.Second

// Activity is a reading of how much random-event handling one account's Guardian has done, taken
// when a measuring window opens. Its zero value, and the reading of a session with no Guardian,
// is never disturbed.
type Activity struct {
	g         *Guardian
	seq       uint64
	unsettled bool
}

// ActivityOf reads session's Guardian's event counter. Take it BEFORE the pack reading it goes
// with: an event that began between the two would otherwise not be seen, and its reward arrives
// seconds later, inside the window.
func ActivityOf(session actions.Session) Activity {
	registryMu.Lock()
	g := registry[session]
	registryMu.Unlock()
	if g == nil {
		return Activity{}
	}
	return g.Activity()
}

// Activity is ActivityOf for a Guardian already in hand.
func (g *Guardian) Activity() Activity {
	if g == nil {
		return Activity{}
	}
	seq := g.eventSeq.Load()
	last := g.eventEnd.Load()
	return Activity{
		g:         g,
		seq:       seq,
		unsettled: seq%2 == 1 || (last != 0 && time.Since(time.Unix(0, last)) < eventSettle),
	}
}

// Disturbed reports whether the Guardian handled a random event at any point since a was read: one
// was running when it was read, one had only just finished, or one began or ended since. A window
// that comes back disturbed measured something that is not its own, and should be thrown away
// rather than attributed.
func (a Activity) Disturbed() bool {
	return a.g != nil && (a.unsettled || a.g.eventSeq.Load() != a.seq)
}

// BeginEvent marks the start of one stretch of random-event handling (or of a trapped event's
// exit) and returns the func that marks its end, which is safe to call more than once. Guardian's
// own kick calls it around every handler; it is exported so a test in another package can stand in
// for an event without running a handler.
func (g *Guardian) BeginEvent() (end func()) {
	g.eventSeq.Add(1)
	var once sync.Once
	return func() {
		once.Do(func() {
			g.eventEnd.Store(time.Now().UnixNano())
			g.eventSeq.Add(1)
		})
	}
}
