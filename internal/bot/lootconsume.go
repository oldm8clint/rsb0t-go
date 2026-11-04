package bot

import (
	"regexp"
	"sync"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/loot"
)

// THE CONSUMABLES TAB: every food and drink any script eats or drinks, recorded without touching
// any script (a user request: "every food, potion, wine etc. consumed ... with counts and coin
// value. Consumed inputs are costs, never loot").
//
// Content 289 eats and drinks in one script step (scripts/player/scripts/consumption/effects/scripts/
// consume_effects.rs2): inv_delslot of the item, then the message "You eat the ..." or "You drink
// ...". So a consumption is an EDIBLE LEAVING ITS PACK SLOT in the same server tick as THAT LINE
// ARRIVING. Either half alone is ignored, and that matters: the same slot empties when the food is
// banked, traded, dropped or sold (no such line) and when the account dies and its whole pack goes at
// once, and none of those is food eaten. Same pairing as the Prayer card's bury watch (lootprayer.go).

// consumeLine is the first line every food and drink prints (consume_effects.rs2: "You eat the ...",
// "You drink the ...", and the cake, pie, pizza and potion variants beginning the same way).
var consumeLine = regexp.MustCompile(`^You (eat|drink)\b`)

// consumePairTicks is how many ticks apart the two halves may be seen and still pair. Both are written
// in one step, so 0 is the expectation; 1 allows for the tick counter advancing between them.
const consumePairTicks = 1

// consumeCap bounds the halves kept waiting for a partner.
const consumeCap = 64

type consumeHalf struct {
	id   int
	tick int
}

// consumeWatch pairs an edible leaving the pack with the line that says it was eaten. Pure apart from
// its callbacks, so it is tested by feeding it events.
type consumeWatch struct {
	mu     sync.Mutex
	tick   int
	losses []consumeHalf // edibles that left a slot, not yet paired
	lines  []consumeHalf // "You eat/drink" lines, not yet paired
	// edible reports whether an obj id is a food or drink (loot.ConsumableOp).
	edible func(id int) bool
	// modal reports whether a window that moves items out of the pack is open: a bank, a shop, a trade.
	// Nothing is eaten through one, so what leaves the pack then was deposited or sold.
	modal  func() bool
	record func(objID int)
}

func newConsumeWatch(edible func(id int) bool, record func(objID int)) *consumeWatch {
	return &consumeWatch{edible: edible, record: record}
}

// onEvent is a gamestate.Subscriber.
func (w *consumeWatch) onEvent(ev gamestate.Event) {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch ev.Kind {
	case gamestate.EventTick:
		w.tick = ev.Tick
	case gamestate.EventInventoryChanged:
		if ev.Comp != lootPackComponent {
			return
		}
		prev := int(ev.PreviousID)
		if prev <= 0 || w.edible == nil || !w.edible(prev) || (w.modal != nil && w.modal()) {
			return
		}
		left := int(ev.PreviousCount)
		if ev.ItemID == ev.PreviousID {
			left = int(ev.PreviousCount - ev.Count)
		}
		if left < 1 && ev.ItemID != ev.PreviousID {
			left = 1
		}
		for i := 0; i < left && i < 28; i++ {
			w.losses = append(w.losses, consumeHalf{id: prev, tick: w.tick})
		}
	case gamestate.EventChatMessage:
		if ev.ChatType != gamestate.ChatGame || !consumeLine.MatchString(ev.Text) {
			return
		}
		w.lines = append(w.lines, consumeHalf{tick: w.tick})
	default:
		return
	}
	w.expireLocked()
	w.pairLocked()
}

func (w *consumeWatch) fresh(h consumeHalf) bool { return w.tick-h.tick <= consumePairTicks }

func (w *consumeWatch) expireLocked() {
	keep := func(in []consumeHalf) []consumeHalf {
		out := in[:0]
		for _, h := range in {
			if w.fresh(h) {
				out = append(out, h)
			}
		}
		if len(out) > consumeCap {
			out = out[len(out)-consumeCap:]
		}
		return out
	}
	w.losses, w.lines = keep(w.losses), keep(w.lines)
}

// pairLocked matches the oldest pending loss with the oldest pending line, while both exist and were
// seen within consumePairTicks of each other.
func (w *consumeWatch) pairLocked() {
	for len(w.losses) > 0 && len(w.lines) > 0 {
		l, m := w.losses[0], w.lines[0]
		d := l.tick - m.tick
		if d < 0 {
			d = -d
		}
		if d > consumePairTicks {
			// The older of the two can never pair now: drop it and try again.
			if l.tick < m.tick {
				w.losses = w.losses[1:]
			} else {
				w.lines = w.lines[1:]
			}
			continue
		}
		w.losses, w.lines = w.losses[1:], w.lines[1:]
		if w.record != nil {
			w.record(l.id)
		}
	}
}

// consumeLogEvery spaces the log line: the first consumption of a run, then every this many.
const consumeLogEvery = 200

// watchConsumption starts the consume watch on this Context's session, for as long as the Runner
// runs. A no-op with no loot sink, no session or no cache (nothing says what is edible). The
// subscription is made through On, so the Runner's disposeSubscriptions cancels it with the others.
func (c *Context) watchConsumption() {
	if c == nil || c.Loot == nil || c.gameState() == nil || c.Config == nil {
		return
	}
	st := c.gameState()
	n := 0
	w := newConsumeWatch(func(id int) bool { _, ok := loot.ConsumableOp(c.Config, id); return ok }, func(id int) {
		d, ok := loot.Consumed(c.Config, id)
		if !ok {
			return
		}
		c.Loot(d)
		if n++; n == 1 || n%consumeLogEvery == 0 {
			c.logf("loot: consumed %s (%d this run)", d.Spent[0].Name, n)
		}
	})
	w.modal = func() bool { main, _, _ := st.Modals(); return main != gamestate.NoModal }
	c.On(w.onEvent, gamestate.EventTick, gamestate.EventInventoryChanged, gamestate.EventChatMessage)
}
