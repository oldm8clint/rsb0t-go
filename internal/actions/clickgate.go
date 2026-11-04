package actions

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"rsb0t-go/internal/gamestate"
)

// THE CLICK GATE: the one place that says how often a loop may click the same thing.
//
// WHY IT EXISTS (found in the lab 15 Oct 2026, claude/mineexit-1015; the audit is
// docs/click_flood_audit_2026-10-15.md). The trial EssenceMiner stood in the mine for 8-11 minutes with a full pack, re-sending
// its walk to the portal. Its hold loop returned "dialog" the moment a level-up chat showed, its caller called it again at once, and
// each call clicked the rock while the client still showed the chat (the server's IF_CLOSE is a tick away): about 8,400 OPLOCs a
// second. Engine-TS NetworkPlayer.decodeIn reads at most five USER_EVENT packets a player a tick and leaves the rest in the socket
// buffer, in order, so every Continue, walk and CLOSE_MODAL sent afterwards waited behind them (5,000 clicks = 1,000 ticks = ten
// minutes).
//
// The shape is any loop that sends a click and can come back round, or return to a caller that comes back round, without a tick
// having passed: a chat that is up (stale on the client for a tick after the click closed it), a full pack, a stall, a refused
// click. This gate bounds that shape at its source. A hold loop sends its click through GatedClick (or ClickLoc, ClickNpc,
// ClickObj), which waits until the same target was last clicked ClickGapTicks game ticks ago; the engine reads five a tick, so a
// click every other tick leaves its buffer empty and whatever the bot sends next is read in the tick after it is sent.
//
// It is keyed on the session's state, not on the caller's Deps (callers build a Deps per call), and on the target, so switching
// to another tree or another rock is never held back (the Ent rule: "as soon as an ent shows, swap to a tree without one").
//
// It is NOT in OpLoc, OpNpc and OpObj themselves: those primitives are also the batch senders' (SendBurst, the drop and equip
// batches, the chop and fish manipulations), which write several packets a tick on purpose and are paced by their own tick
// controllers. A gate there would break them; the loops that re-click are the ones that choose to use this.

// ClickGapTicks is the fewest game ticks between two gated clicks on the same target from one account.
const ClickGapTicks = 2

// clickGapWall is the same gap in wall time, for a client whose tick counter has stopped (no PLAYER_INFO arriving): the click may
// go when either has run out, so a stalled counter cannot hold a loop for ever and a fast one cannot let a hot loop through.
const clickGapWall = ClickGapTicks * sleepTick

// clickHeldLogEvery is how often a held click may call its held hook for one session.
const clickHeldLogEvery = 30 * time.Second

// clickPoll is how often a held click looks at the gap again.
const clickPoll = 100 * time.Millisecond

// clickKeysKept bounds the targets one session remembers; marks older than a few gaps are dropped first.
const clickKeysKept = 32

// clickSessionsKept bounds the sessions remembered (sessions that were closed leave their mark behind; losing marks only lets
// one click through).
const clickSessionsKept = 2048

// ErrClickStopped is returned by a gated click whose caller was told to stop while the click was held back.
var ErrClickStopped = errors.New("actions: stopped while a click was held for its gap")

type clickMark struct {
	tick int
	at   time.Time
}

type clickGate struct {
	last       map[string]clickMark
	heldLogged time.Time
}

var clickGates = struct {
	mu sync.Mutex
	m  map[*gamestate.State]*clickGate
}{m: map[*gamestate.State]*clickGate{}}

// gateOf is the session's gate; the caller holds clickGates.mu.
func gateOf(st *gamestate.State) *clickGate {
	if g, ok := clickGates.m[st]; ok {
		return g
	}
	if len(clickGates.m) >= clickSessionsKept {
		clickGates.m = map[*gamestate.State]*clickGate{}
	}
	g := &clickGate{last: map[string]clickMark{}}
	clickGates.m[st] = g
	return g
}

// LocKey names a loc as a click target for the gate.
func LocKey(x, z, locType int) string { return fmt.Sprintf("loc:%d,%d,%d", x, z, locType) }

// NpcKey names an NPC as a click target for the gate.
func NpcKey(index int) string { return fmt.Sprintf("npc:%d", index) }

// ObjKey names a ground item as a click target for the gate.
func ObjKey(x, z, itemType int) string { return fmt.Sprintf("obj:%d,%d,%d", x, z, itemType) }

// ClickWait is how much longer a click on key has to wait, in game ticks and in wall time; the click may go when either has run
// out (0, 0). A session that has never clicked key waits for nothing.
func ClickWait(s Session, key string) (ticks int, wall time.Duration) {
	st := s.State()
	if st == nil {
		return 0, 0
	}
	clickGates.mu.Lock()
	var mark clickMark
	ok := false
	if g, found := clickGates.m[st]; found {
		mark, ok = g.last[key]
	}
	clickGates.mu.Unlock()
	if !ok {
		return 0, 0
	}
	ticks = ClickGapTicks - (st.Tick() - mark.tick)
	wall = clickGapWall - time.Since(mark.at)
	if ticks <= 0 || wall <= 0 {
		return 0, 0
	}
	return ticks, wall
}

// MarkClick records a click on key now. GatedClick does it for the caller; a caller whose click went out some other way (a batch
// that carries the click, a tick-controlled manipulation) marks it itself so the next gated click is spaced from it.
func MarkClick(s Session, key string) {
	st := s.State()
	if st == nil {
		return
	}
	tick, now := st.Tick(), time.Now()
	clickGates.mu.Lock()
	defer clickGates.mu.Unlock()
	g := gateOf(st)
	if len(g.last) >= clickKeysKept {
		for k, m := range g.last {
			if now.Sub(m.at) > 4*clickGapWall {
				delete(g.last, k)
			}
		}
		if len(g.last) >= clickKeysKept { // every one recent: start over rather than grow
			g.last = map[string]clickMark{}
		}
	}
	g.last[key] = clickMark{tick: tick, at: now}
}

// heldDue says whether a held click may be reported now (once every clickHeldLogEvery per session).
func heldDue(st *gamestate.State) bool {
	clickGates.mu.Lock()
	defer clickGates.mu.Unlock()
	g := gateOf(st)
	if time.Since(g.heldLogged) < clickHeldLogEvery {
		return false
	}
	g.heldLogged = time.Now()
	return true
}

// WaitClickGap holds the caller until key may be clicked again, and reports false when w said stop meanwhile. held, when not nil,
// is called with the ticks still to wait when the click has to wait at all, at most once every 30 s per session: a hot loop is
// then one log line, not thousands.
func WaitClickGap(s Session, w Waiter, key string, held func(ticks int)) bool {
	ticks, _ := ClickWait(s, key)
	if ticks <= 0 {
		return true
	}
	if held != nil && heldDue(s.State()) {
		held(ticks)
	}
	w = waiterOr(w)
	for {
		if ticks, _ = ClickWait(s, key); ticks <= 0 {
			return true
		}
		if !w.Delay(clickPoll) {
			return false
		}
	}
}

// GatedClick waits until key may be clicked, runs send, and marks the click when send succeeded. It returns ErrClickStopped when w
// said stop while the click was held, and send's own error otherwise.
func GatedClick(s Session, w Waiter, key string, held func(ticks int), send func() error) error {
	if !WaitClickGap(s, w, key, held) {
		return ErrClickStopped
	}
	if err := send(); err != nil {
		return err
	}
	MarkClick(s, key)
	return nil
}

// ClickLoc is OpLoc through the gate, keyed on the loc.
func ClickLoc(s Session, w Waiter, option, x, z, locType int) error {
	return GatedClick(s, w, LocKey(x, z, locType), nil, func() error { return OpLoc(s, option, x, z, locType) })
}

// ClickNpc is OpNpc through the gate, keyed on the NPC.
func ClickNpc(s Session, w Waiter, option, npcIndex int) error {
	return GatedClick(s, w, NpcKey(npcIndex), nil, func() error { return OpNpc(s, option, npcIndex) })
}

// ClickObj is OpObj through the gate, keyed on the ground item.
func ClickObj(s Session, w Waiter, option, x, z, itemType int) error {
	return GatedClick(s, w, ObjKey(x, z, itemType), nil, func() error { return OpObj(s, option, x, z, itemType) })
}
