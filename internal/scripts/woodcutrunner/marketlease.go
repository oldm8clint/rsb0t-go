package woodcutrunner

// The stall's turn, shared by every buyer in the fleet process, and the fleet's shared note of
// what the stall has said it has none of.
//
// WHY THE TURN HAS PRIORITIES NOW. Every market buyer in this repo goes through BuyAtMarket --
// ProgressiveWoodcutter/ProgressiveWCFletch axe upgrades, ScarabSwarm's chaos runes and staffs,
// the mule's MarketBulkBuy -- and until 2026-09-25 they all took one first-come turn. Measured over
// the 40.9 h before that evening's relaunch, axe trips held the Seers Market's turn for about
// 85% of wall-clock time, asking for rune and adamant axes the stall had none of, while the
// purchases that could succeed lost it: 269 lost turns for ScarabSwarm, and both of the mule's
// MarketBulkBuy runs (20:40, 20:43) ended "another account is buying from Seers Market right now"
// after a 106-tile walk and a coin withdrawal.
//
// An axe is an UPGRADE: the account is chopping with the axe it has the whole time. So an axe
// buyer is a BACKGROUND buyer (MarketBuyOptions.Background):
//
//   - it never takes a free turn while an ordinary buyer is waiting for it;
//   - it does not queue at all behind an ordinary buyer, and at most
//     marketBackgroundQueueCap background buyers queue at once -- the rest are told "busy"
//     immediately, before walking anywhere, and back off (woodcutplan.MarketBusyBackoff);
//   - while it HOLDS the turn it gives it up between asks the moment an ordinary buyer is waiting
//     (BuyAtMarket checks marketTurnGrant.priorityWaiting at the top of every ask).
//
// An ordinary buyer waits marketTurnWaitPriority for its turn rather than marketTurnWait, which
// covers one background ask cycle plus a trade already on the screen -- the longest a background
// holder can keep the turn once it has seen a priority buyer waiting.

import (
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/woodcutplan"
)

// marketTurnWait is how long a BACKGROUND buyer waits for the stall to be free before giving up
// this trip.
//
// Deliberately much shorter than a whole trip. Chopping is productive and the axe is an
// upgrade, not a blocker, so an account that cannot get a turn is better off cutting wood and
// coming back than standing in a queue.
const marketTurnWait = 30 * time.Second

// marketTurnWaitPriority is how long an ORDINARY buyer waits for its turn. See this file's header.
//
// Three minutes because of what it has to outlast. A background holder only checks for a waiting
// priority buyer between asks, and one ask at a silent stall is 12 s for a reply, 3 s watching for
// a decline, up to 30 s for the trade to open and, when it opens with nothing in it, about 80 s on
// the screen before it is declined -- a median 137 s per silent rune-axe trip was measured live.
// The old 30 s is why the mule lost the turn to an axe trip every time it arrived mid-ask.
const marketTurnWaitPriority = 3 * time.Minute

// marketBackgroundQueueCap is how many background buyers may stand waiting for one stall at once,
// fleet-wide. One: the next in line, and nobody behind it.
const marketBackgroundQueueCap = 1

// marketTurn is one stall's turn and its queue.
type marketTurn struct {
	mu   sync.Mutex
	held bool
	// gen increments on every acquisition, so a stale or doubled release cannot free a turn a
	// later buyer now holds.
	gen     uint64
	priWait int
	bgWait  int
}

// marketTurns holds one *marketTurn per stall name, shared by every account in this process.
var marketTurns sync.Map

func turnFor(seller string) *marketTurn {
	key := strings.ToLower(strings.TrimSpace(seller))
	v, _ := marketTurns.LoadOrStore(key, &marketTurn{})
	return v.(*marketTurn)
}

// takeLocked takes the turn when it is free and this buyer is allowed it. mu must be held.
func (t *marketTurn) takeLocked(background bool) (uint64, bool) {
	if t.held || (background && t.priWait > 0) {
		return 0, false
	}
	t.held = true
	t.gen++
	return t.gen, true
}

// mayQueue reports whether a buyer of this kind would be allowed to wait for the turn right now
// (true when the turn is free). An ordinary buyer always may.
func (t *marketTurn) mayQueue(background bool) bool {
	if !background {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.held && t.priWait == 0 {
		return true
	}
	return t.priWait == 0 && t.bgWait < marketBackgroundQueueCap
}

// marketTurnGrant is a held turn.
type marketTurnGrant struct {
	t    *marketTurn
	gen  uint64
	once sync.Once
}

// release gives the turn back. Safe to call more than once.
func (g *marketTurnGrant) release() {
	if g == nil {
		return
	}
	g.once.Do(func() {
		g.t.mu.Lock()
		if g.t.held && g.t.gen == g.gen {
			g.t.held = false
		}
		g.t.mu.Unlock()
	})
}

// priorityWaiting reports whether an ordinary buyer is waiting for this stall, which is the cue for
// a background holder to give the turn up.
func (g *marketTurnGrant) priorityWaiting() bool {
	if g == nil {
		return false
	}
	g.t.mu.Lock()
	defer g.t.mu.Unlock()
	return g.t.priWait > 0
}

// acquireMarketTurnAs takes this process's turn at a named stall. See this file's header for what
// background changes.
//
// A plain in-process lock is the right tool and not an under-engineered one: every account this
// serialises lives in the same fleet process, which is the entire design of this client. It is
// keyed by stall name so two different stalls never block each other.
func acquireMarketTurnAs(d *Deps, seller string, within time.Duration, background bool) (*marketTurnGrant, bool) {
	t := turnFor(seller)
	t.mu.Lock()
	if gen, ok := t.takeLocked(background); ok {
		t.mu.Unlock()
		return &marketTurnGrant{t: t, gen: gen}, true
	}
	if background && (t.priWait > 0 || t.bgWait >= marketBackgroundQueueCap) {
		// Not even a place in the queue: come back later rather than stand in line.
		t.mu.Unlock()
		return nil, false
	}
	if background {
		t.bgWait++
	} else {
		t.priWait++
	}
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		if background {
			t.bgWait--
		} else {
			t.priWait--
		}
		t.mu.Unlock()
	}()

	deadline := time.Now().Add(within)
	for {
		if !time.Now().Before(deadline) {
			return nil, false
		}
		if d == nil || d.Delay == nil || !d.Delay(500*time.Millisecond) {
			return nil, false
		}
		t.mu.Lock()
		gen, ok := t.takeLocked(background)
		yield := background && t.priWait > 0
		t.mu.Unlock()
		if ok {
			return &marketTurnGrant{t: t, gen: gen}, true
		}
		if yield {
			// An ordinary buyer arrived behind us: step out of its way.
			return nil, false
		}
	}
}

// acquireMarketTurn takes an ORDINARY buyer's turn at a named stall, returning a release func.
func acquireMarketTurn(d *Deps, seller string, within time.Duration) (release func(), ok bool) {
	g, ok := acquireMarketTurnAs(d, seller, within, false)
	if !ok {
		return nil, false
	}
	return g.release, true
}

// ---- what the stall has said it has none of ----

// fleetSoldOut is the whole fleet's note of what each stall has said it has none of. One per
// process, like marketTurns, because the stall's stock is one stock: its "I have no rune axe right
// now" is the same answer for every account that asks.
var fleetSoldOut woodcutplan.MarketSoldOut

// MarketSoldOutUntil reports whether seller has told any account in this fleet that it has no
// item, and until when the fleet is leaving that item alone.
func MarketSoldOutUntil(seller, item string, now time.Time) (time.Time, bool) {
	if strings.TrimSpace(seller) == "" {
		return time.Time{}, false
	}
	return fleetSoldOut.Until(seller, item, now)
}

// MarkMarketSoldOut records, for the whole fleet, that seller said it has no item, returning until
// when it is left alone. BuyAtMarket calls it on the stall's own "I have no ... right now"; it is
// exported for a caller that hears the same answer another way, and for tests.
func MarkMarketSoldOut(seller, item string, now time.Time) time.Time {
	if strings.TrimSpace(seller) == "" {
		return time.Time{}
	}
	return fleetSoldOut.Mark(seller, item, now)
}

// ClearMarketSoldOut forgets the fleet's note for seller and item.
func ClearMarketSoldOut(seller, item string) { fleetSoldOut.Clear(seller, item) }

// ---- the receipt ----

// objNames is the one thing the receipt needs from the cache config: an id's name and whether it
// is a note. *cachecfg.Config satisfies it.
type objNames interface {
	Obj(id int) (cachecfg.Obj, bool)
}

// marketReceiptCount counts name among items, NOTED OR NOT.
//
// This is the receipt's own count, and it must see a note, because the Seers stall hands an axe
// over AS a note (93a1337f). packCountByName stopped seeing notes on 2026-09-23 (02e7ca90, for the
// fletching deadlock its own comment records), and the market receipt, which still used it, then
// read every noted axe that arrived as "the trade closed without the item arriving" and asked the
// stall again -- and paid again. Measured on the live banks 2026-09-25: acct141 and Acct215 hold
// five Adamant axes each, acct153 and acct136 two, all bought one trip apart in the 02:46-03:16
// window when the stall had adamant axes, which is also why it has none now.
func marketReceiptCount(items []gamestate.SlotItem, name string, objs objNames) int {
	if objs == nil {
		return 0
	}
	want := strings.ToLower(strings.TrimSpace(name))
	n := 0
	for _, it := range items {
		if it.ID <= 0 {
			continue
		}
		obj, ok := objs.Obj(int(it.ID))
		if !ok || strings.ToLower(strings.TrimSpace(obj.Name)) != want {
			continue
		}
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		n += c
	}
	return n
}

// marketHeld is marketReceiptCount over this account's own pack.
func marketHeld(d *Deps, name string) int {
	if d == nil || d.Config == nil || d.Session == nil {
		return 0
	}
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return 0
	}
	return marketReceiptCount(inv.NonEmpty(), name, d.Config)
}
