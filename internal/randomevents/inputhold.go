package randomevents

import (
	"fmt"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
)

// THE GUARDIAN HOLDS THE ACCOUNT'S INPUT WHILE IT HANDLES AN EVENT (plantfisher-1018, 18 Oct 2026).
//
// The Guardian has always asked scripts to stay out of its way: Busy() to wait on, Lock() to take. A script that does
// neither is a script that clicks over the Guardian's work, and every click is read by the engine as a new interaction
// that replaces the one before (MoveClickHandler and OpNpcHandler both run clearPendingAction). The fisher's bank code
// is one (OpenCatherbyBank clicked a booth, waited thirty seconds for the bank and walked to the next booth without
// asking either), and every script that walks, shops or banks has a stretch of its own like it. Live 20:01-20:03 on
// 18 Oct, six booth walks and four booth clicks went out between the Guardian's plant picks; and the booth click that
// went out at 20:03:04, with the plant already angry, was the account's last act before it was hit.
//
// So the protocol is enforced where the clicks are sent (actions.HoldInput): while the Guardian's poll goroutine runs
// a handler, a Walk, OpLoc, OpNpc, OpObj, OpHeld (not Eat or Drink) or use-on sent through the account's session by any
// other goroutine is dropped, as the engine drops the clicks of a delayed player, unless that goroutine holds the action
// lock (Lock, TryLock: the script that took the lock has agreed to the protocol, EmergencyFlee among them). The hold
// outlasts the handler by inputHoldTail when the event is still there, which covers the gap before the next try.

// inputHoldTail is how long the hold outlasts a handler that left its event standing: the retry window (2 poll
// intervals) plus the poll itself.
const inputHoldTail = 3 * pollInterval

// inputHeld is the hold's active(): a handler is running, or one has just ended with its event still standing.
func (g *Guardian) inputHeld() bool {
	now := time.Now().UnixNano()
	if g.handling.Load() && now-g.handlingSince.Load() < int64(inputHoldMax) {
		return true
	}
	return now < g.holdTailUntil.Load() || g.plantKeptOut()
}

// inputHoldMax is the longest one handler holds the account's input: the safety valve of the hold. The longest legitimate handler (the
// maze, a strand ladder rung) is a minute or two; a handler still running after this is stuck, and a stuck handler must not freeze the
// account's script for good.
const inputHoldMax = 3 * time.Minute

// plantKeptOut reports whether the account has walked out of an engaged plant's leash and the plant is still in its dangerous
// stretch: the account's script must not walk it back in before the plant has turned and gone, so its input stays held between the
// handler's runs too.
func (g *Guardian) plantKeptOut() bool {
	kept := false
	for _, index := range g.plants.indexes() {
		if !g.plants.hasLeft(index) {
			continue
		}
		age, ok := plantAgeTicks(g.session.State(), index)
		if !ok || age >= plantClearTicks {
			g.plants.forget(index)
			continue
		}
		kept = true
	}
	return kept
}

// holdsActionLock reports whether the calling goroutine holds the account's action lock through Lock or TryLock.
func (g *Guardian) holdsActionLock() bool {
	id := g.lockHolder.Load()
	return id != 0 && id == actions.GoroutineID()
}

// takeInput starts holding the account's input for the poll goroutine and returns the function that ends the handler's
// part of it. Called with the action lock held.
func (g *Guardian) takeInput() (release func()) {
	// The hold is registered before handling turns true: a click that sees the Guardian handling must find the hold there.
	h := actions.HoldInput(g.session, g.inputHeld, g.holdsActionLock)
	g.hold.Store(h)
	g.handlingSince.Store(time.Now().UnixNano())
	g.handling.Store(true)
	return func() {
		g.handling.Store(false)
		// Proof, in the log, that the hold did something: what the script sent over the handler is counted here.
		if n := h.Dropped(); n > 0 {
			g.log(fmt.Sprintf("held off %d click(s) the account's script sent while the Guardian handled the event", n))
		}
		if !g.inputHeld() {
			h.Release()
		}
	}
}

// keepInputFor extends the hold past the end of the running handler by d.
func (g *Guardian) keepInputFor(d time.Duration) {
	until := time.Now().Add(d).UnixNano()
	for {
		cur := g.holdTailUntil.Load()
		if until <= cur || g.holdTailUntil.CompareAndSwap(cur, until) {
			return
		}
	}
}

// plantWatch is one Strange plant the Guardian has engaged and not yet seen the end of.
type plantWatch struct {
	index int
	since time.Time
	// left is set when the account has walked out of the plant's leash to wait it out.
	left bool
}

// plantWatches is a Guardian's engaged plants, by NPC index.
type plantWatches struct {
	mu sync.Mutex
	m  map[int]*plantWatch
}

// engage notes that the plant npcIndex is this account's and being handled. It reports whether it is a new one.
func (p *plantWatches) engage(npcIndex int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.m == nil {
		p.m = make(map[int]*plantWatch)
	}
	if _, ok := p.m[npcIndex]; ok {
		return false
	}
	p.m[npcIndex] = &plantWatch{index: npcIndex, since: time.Now()}
	return true
}

func (p *plantWatches) left(npcIndex int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if w, ok := p.m[npcIndex]; ok {
		w.left = true
	}
}

func (p *plantWatches) hasLeft(npcIndex int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	w, ok := p.m[npcIndex]
	return ok && w.left
}

func (p *plantWatches) forget(npcIndex int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.m, npcIndex)
}

// indexes lists the engaged plants.
func (p *plantWatches) indexes() []int {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]int, 0, len(p.m))
	for i := range p.m {
		out = append(out, i)
	}
	return out
}

// plantBusy reports whether an engaged plant is in the stretch of its life that holds the account's script: from nine ticks
// before it can be picked until it is gone or well past the tick it turns. It goes by the NPC's index whatever its type is,
// so the change from the seed (407) to the angry plant (408) -- for which Detect has no event until the new type faces the
// account -- is not a gap in which a script is let go (live 20:03:04, the booth click two seconds before the evade began).
// An engaged plant that has left view, or outlived plantClearTicks, is forgotten.
func (g *Guardian) plantBusy() bool {
	busy := false
	for _, index := range g.plants.indexes() {
		age, ok := plantAgeTicks(g.session.State(), index)
		if !ok || age >= plantClearTicks {
			g.plants.forget(index)
			continue
		}
		if plantBusyWindow(age) {
			busy = true
		}
	}
	return busy
}

// plantsOf returns the registered Guardian's engaged plants for the session, nil when it has no Guardian.
func plantsOf(s actions.Session) *plantWatches {
	registryMu.Lock()
	g := registry[s]
	registryMu.Unlock()
	if g == nil {
		return nil
	}
	return &g.plants
}

// holdPastHandler keeps the account's input held for the retry gap when ev can hurt the account (a Strange plant or a
// Drunken Dwarf, which turn on their owner, and every event monster): between two tries at it, a script's click is the
// one that undoes the pick. The harmless events (a box, a genie) give the account back the moment the handler ends.
func (g *Guardian) holdPastHandler(ev *Event) {
	if ev != nil && (hostileCapableNames[ev.Name] || hostileKinds[ev.Kind]) {
		g.keepInputFor(inputHoldTail)
	}
}

// BusyForCaller is Busy for a caller that may itself hold the account's action lock (Lock, TryLock), as a bank sequence does:
// false then, because the Guardian cannot be handling anything while the caller holds the lock and a wait on the Guardian from
// inside the lock would never end. A script yields to the Guardian with this; it never holds the lock and waits on Busy.
func BusyForCaller(s actions.Session) bool {
	registryMu.Lock()
	g := registry[s]
	registryMu.Unlock()
	if g != nil && g.holdsActionLock() {
		return false
	}
	return Busy(s)
}
