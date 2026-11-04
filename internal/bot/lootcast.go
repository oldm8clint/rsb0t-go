package bot

import (
	"sort"
	"sync"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/loot"
)

// THE TELEPORTS TAB: every teleport any script casts, with the runes it cost, recorded without
// touching any script (a user request: "Law runes and other runes spent on teleports go in their
// own Teleports loot tab: cost per teleport cast, count, coin value").
//
// Content 289 deletes a teleport's runes and pays its Magic XP in one script step, so a cast is a
// LAW RUNE LEAVING THE PACK in the same server tick as MAGIC XP RISING. Banking, trading and
// dropping runes take them out with no experience at all, and every other Magic action either uses
// no Law rune (alchemy, superheat, a strike or a blast) or is one this tab is for (Telekinetic
// Grab). Either half alone is ignored, exactly as the Prayer card's bury watch (lootprayer.go)
// does it.
//
// The two halves arrive as separate packets (one inventory slot per rune, then the stat), in an
// order nobody guarantees, so nothing is decided on the tick a half arrives. Each tick keeps its
// own rune losses and Magic XP gains, and a tick is judged only once castSettleTicks later, when
// both neighbours' packets are certainly in. A rune loss with a Law rune and a gain within one tick
// of it is one cast; the loss is what the cast measurably spent (loot.TeleportName names the spell).

// castSettleTicks is how many ticks a tick waits before it is judged. The XP of a cast can land in
// the tick after its runes, so tick k is judged when tick k+2 begins.
const castSettleTicks = 2

type castTick struct {
	lost   loot.Counts
	gains  []int
	judged bool
}

// castWatch pairs rune losses with Magic XP gains per tick. Pure apart from its record callback, so
// it is tested by feeding it events.
type castWatch struct {
	mu     sync.Mutex
	tick   int
	ticks  map[int]*castTick
	record func(lost loot.Counts, xp int)
	// modal, when set, reports whether a window that takes items out of the pack (a bank, a shop, a
	// trade) is open: a rune that leaves the pack then was deposited or sold, not cast.
	modal func() bool
}

func newCastWatch(record func(lost loot.Counts, xp int)) *castWatch {
	return &castWatch{record: record, ticks: map[int]*castTick{}}
}

func (w *castWatch) bucketLocked(tick int) *castTick {
	b := w.ticks[tick]
	if b == nil {
		b = &castTick{lost: loot.Counts{}}
		w.ticks[tick] = b
	}
	return b
}

// onEvent is a gamestate.Subscriber.
func (w *castWatch) onEvent(ev gamestate.Event) {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch ev.Kind {
	case gamestate.EventTick:
		w.tick = ev.Tick
		w.settleLocked()
	case gamestate.EventInventoryChanged:
		if ev.Comp != lootPackComponent {
			return
		}
		prev := int(ev.PreviousID)
		if !loot.IsRuneID(prev) || (w.modal != nil && w.modal()) {
			return
		}
		left := int64(ev.PreviousCount)
		if ev.ItemID == ev.PreviousID {
			left = int64(ev.PreviousCount - ev.Count)
		}
		if left > 0 {
			w.bucketLocked(w.tick).lost[prev] += left
		}
	case gamestate.EventSkillXP:
		if ev.Skill != loot.MagicStat || ev.XPDelta <= 0 {
			return
		}
		b := w.bucketLocked(w.tick)
		b.gains = append(b.gains, ev.XPDelta)
	}
}

// settleLocked judges, oldest first, every tick old enough that its neighbours' packets are in, and
// forgets the ones nothing can pair with any more.
func (w *castWatch) settleLocked() {
	if len(w.ticks) == 0 {
		return
	}
	keys := make([]int, 0, len(w.ticks))
	for k := range w.ticks {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	for _, k := range keys {
		b := w.ticks[k]
		if k > w.tick-castSettleTicks {
			break
		}
		if !b.judged {
			b.judged = true
			if loot.IsLawCast(b.lost) {
				if xp, ok := w.takeGainLocked(k); ok && w.record != nil {
					w.record(b.lost, xp)
				}
			}
		}
		// Its gains are still wanted by the next tick's judgement until that tick has been judged.
		if k < w.tick-castSettleTicks-1 {
			delete(w.ticks, k)
		}
	}
}

// takeGainLocked takes one Magic XP gain from tick k or the tick after it, the same tick's first:
// content pays the experience in the script step that deletes the runes.
func (w *castWatch) takeGainLocked(k int) (xp int, ok bool) {
	for _, t := range []int{k, k + 1} {
		b := w.ticks[t]
		if b == nil || len(b.gains) == 0 {
			continue
		}
		xp = b.gains[0]
		b.gains = b.gains[1:]
		return xp, true
	}
	return 0, false
}

// watchCasts starts the cast watch on this Context's session, for as long as the Runner runs. A
// no-op with no loot sink or no session. The subscription is made through On, so the Runner's
// disposeSubscriptions cancels it with the others.
func (c *Context) watchCasts() {
	if c == nil || c.Loot == nil || c.gameState() == nil {
		return
	}
	st := c.gameState()
	w := newCastWatch(func(lost loot.Counts, xp int) {
		name := loot.TeleportName(lost, xp)
		c.Loot(loot.Drop{Source: loot.Teleport(name), Events: 1, Spent: loot.Items(c.Config, lost)})
		// The cast's Magic experience is the Teleports card's business, not the next Magic drop's.
		c.xp.discard(loot.MagicStat, xp)
		c.logf("loot: %s cast, runes spent: %s", name, loot.DescribeSpent(loot.Items(c.Config, lost)))
	})
	w.modal = func() bool { main, _, _ := st.Modals(); return main != gamestate.NoModal }
	c.On(w.onEvent, gamestate.EventTick, gamestate.EventInventoryChanged, gamestate.EventSkillXP)
}
