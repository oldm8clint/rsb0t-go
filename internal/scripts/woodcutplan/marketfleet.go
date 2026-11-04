package woodcutplan

// The fleet's shared manners at a player market: what the stall has SAID it has none of, and how
// long an account that lost the stall's turn should stay away. Pure, like the rest of this
// package -- no session, no chat, no I/O. The one shared instance lives in woodcutrunner, beside
// the stall's turn, because that is the only place every buyer (ProgressiveWoodcutter,
// ProgressiveWCFletch, ScarabSwarm, MarketBulkBuy) already passes through.
//
// WHY THIS EXISTS, measured from run/fleet.log over the 40.9 h before the 2026-09-25 20:16
// relaunch (build before b5b87cc3):
//
//   - The Seers Market answered "I have no rune axe right now" 1,153 times, "I have no adamant axe
//     right now" 485 times and "I have no mithril axe right now" 33 times, and never once offered
//     a rune axe. Its own market page agreed (2026-09-25 12:46 UTC: no rune, adamant or mithril
//     axe listed at all). The stall's stock is ONE stock, so its answer is the same for every
//     account that asks -- yet each of 24 ProgressiveWCFletch accounts kept its own ten-minute
//     memory of it (AxeUnavailable), so the fleet as a whole asked again 40.9 times an hour.
//   - Those trips held the stall's one-buyer turn for about 85% of wall-clock time (a trip the
//     stall did not answer held it for a median 137 s), so the purchases that could actually
//     succeed -- ScarabSwarm's chaos runes, the mule's MarketBulkBuy -- lost the turn: 6,467
//     "the Seers market was busy" lines from the woodcutters, 269 lost turns for ScarabSwarm, and
//     both of the mule's MarketBulkBuy runs at 20:40 and 20:43 ended "nothing crossed (another
//     account is buying from Seers Market right now)".

import (
	"strings"
	"sync"
	"time"
)

// SoldOutFirst is how long the whole fleet leaves an item alone after a stall says it has none.
//
// HOURS, NOT MINUTES, because of what the answer is. The per-account AxeUnavailable memory is ten
// minutes, sized for a source that restocks from the fleet's own hand-overs; a player market
// restocks when the person who runs it restocks it, and over 40.9 hours that never happened for a
// rune axe. A lapsed note costs one ask by one account; a note that lapses every ten minutes cost
// forty asks an hour and most of the stall's time.
const SoldOutFirst = 2 * time.Hour

// SoldOutMax caps the escalation: each FRESH confirmation (an ask made after the previous note
// lapsed that gets the same "none") doubles the wait, up to this.
const SoldOutMax = 8 * time.Hour

// MarketSoldOut is the fleet's shared memory of what a player market has said it has none of,
// keyed by stall and item.
//
// Safe for concurrent use: every account in the fleet process reads and writes one instance.
// The zero value is ready to use.
type MarketSoldOut struct {
	mu      sync.Mutex
	entries map[string]soldOutEntry
}

type soldOutEntry struct {
	until time.Time
	// strikes counts consecutive fresh confirmations, for the escalation. A sale resets it.
	strikes int
}

func soldOutKey(seller, item string) string {
	return strings.ToLower(strings.TrimSpace(seller)) + "|" + strings.ToLower(strings.TrimSpace(item))
}

// Mark records that seller said it has no item, and returns until when the fleet will leave it
// alone.
//
// Several accounts hearing the same "none" while the note stands (they were already mid-trip when
// the first one heard it) are ONE answer, not several, so they do not escalate it. Only a fresh
// confirmation after the note has lapsed doubles the wait.
func (m *MarketSoldOut) Mark(seller, item string, now time.Time) time.Time {
	if m == nil {
		return time.Time{}
	}
	key := soldOutKey(seller, item)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.entries == nil {
		m.entries = map[string]soldOutEntry{}
	}
	e := m.entries[key]
	if e.strikes > 0 && now.Before(e.until) {
		return e.until
	}
	e.strikes++
	wait := SoldOutFirst
	for i := 1; i < e.strikes && wait < SoldOutMax; i++ {
		wait *= 2
	}
	if wait > SoldOutMax {
		wait = SoldOutMax
	}
	e.until = now.Add(wait)
	m.entries[key] = e
	return e.until
}

// Until reports whether seller is currently known to have no item, and until when.
func (m *MarketSoldOut) Until(seller, item string, now time.Time) (time.Time, bool) {
	if m == nil {
		return time.Time{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[soldOutKey(seller, item)]
	if !ok || !now.Before(e.until) {
		return time.Time{}, false
	}
	return e.until, true
}

// Clear forgets everything known about seller and item, escalation included. Call it when the
// stall says it HAS one, or a purchase lands: a stall that just sold one plainly restocked.
func (m *MarketSoldOut) Clear(seller, item string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, soldOutKey(seller, item))
}

// MarketBusyFirst is the wait after the first lost turn in a row -- the old fixed axeBusyRetry.
const MarketBusyFirst = 90 * time.Second

// MarketBusyMax caps the lost-turn backoff.
const MarketBusyMax = 20 * time.Minute

// MarketBusyBackoff is how long an account that lost the stall's turn `losses` times in a row
// should stay away, jittered by jitter (a number in [0,1), from the caller's own source).
//
// The old wait was a fixed 90 seconds, and a fixed wait is exactly what keeps a crowd in step:
// accounts that lost together came back together and lost together again, 158 times an hour. It
// now doubles per consecutive loss up to MarketBusyMax, and every wait is spread by +/-25% so a
// cohort that lost at the same moment does not return at the same moment. The first wait is
// unchanged, so an account that was merely second in line still gets its turn about a trip later.
func MarketBusyBackoff(losses int, jitter float64) time.Duration {
	if losses < 1 {
		losses = 1
	}
	wait := MarketBusyFirst
	for i := 1; i < losses && wait < MarketBusyMax; i++ {
		wait *= 2
	}
	if wait > MarketBusyMax {
		wait = MarketBusyMax
	}
	if jitter < 0 {
		jitter = 0
	}
	if jitter >= 1 {
		jitter = 0.999
	}
	// 0.75x .. 1.25x.
	return time.Duration(float64(wait) * (0.75 + 0.5*jitter))
}
