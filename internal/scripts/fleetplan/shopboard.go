package fleetplan

import (
	"strings"
	"sync"
	"time"
)

// ShopBoard is safe for concurrent use by every session goroutine. The zero value is not
// usable; call NewShopBoard.
type ShopBoard struct {
	mu    sync.Mutex
	seen  map[string]shopSighting
	holds map[string]map[string]shopHold
}

type shopSighting struct {
	count int
	at    time.Time
	by    string
}

type shopHold struct {
	qty   int
	until time.Time
}

// NewShopBoard returns an empty board.
func NewShopBoard() *ShopBoard {
	return &ShopBoard{seen: map[string]shopSighting{}, holds: map[string]map[string]shopHold{}}
}

func shopLineKey(sh RuneShop, rune string) string {
	return strings.ToLower(strings.TrimSpace(sh.Keeper)) + "|" + strings.ToLower(strings.TrimSpace(rune))
}

func boardAccountKey(account string) string { return strings.ToLower(strings.TrimSpace(account)) }

// Observe records a stock count read off the shop's own panel at `at` by `by`. A reading OLDER
// than the one already held is ignored, so a restarted account replaying its checkpoint cannot
// overwrite a sibling's fresher look. Negative counts clamp to zero; a line the shop does not
// sell is ignored.
func (b *ShopBoard) Observe(sh RuneShop, rune string, count int, at time.Time, by string) {
	if !sh.Sells(rune) || at.IsZero() {
		return
	}
	if count < 0 {
		count = 0
	}
	k := shopLineKey(sh, rune)
	b.mu.Lock()
	defer b.mu.Unlock()
	if prev, ok := b.seen[k]; ok && prev.at.After(at) {
		return
	}
	b.seen[k] = shopSighting{count: count, at: at, by: strings.TrimSpace(by)}
}

// Sighting is the freshest reading held for a line, and who made it.
func (b *ShopBoard) Sighting(sh RuneShop, rune string) (count int, at time.Time, by string, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s, ok := b.seen[shopLineKey(sh, rune)]
	return s.count, s.at, s.by, ok
}

// Estimate is the freshest reading projected to now by the content's restock rule, capped at
// the line's base; the base itself when nobody has looked. Zero for a line the shop does not
// sell.
func (b *ShopBoard) Estimate(sh RuneShop, rune string, now time.Time, tick time.Duration) int {
	l, ok := sh.Line(rune)
	if !ok {
		return 0
	}
	b.mu.Lock()
	s, seen := b.seen[shopLineKey(sh, rune)]
	b.mu.Unlock()
	if !seen {
		return l.Base
	}
	return l.EstimateStock(s.count, s.at, now, tick)
}

// heldByOthersLocked totals the live holds on one line by every account except `account`,
// pruning the expired ones as it goes. Caller holds b.mu.
func (b *ShopBoard) heldByOthersLocked(k, account string, now time.Time) int {
	me := boardAccountKey(account)
	total := 0
	for who, h := range b.holds[k] {
		if !now.Before(h.until) {
			delete(b.holds[k], who)
			continue
		}
		if who == me {
			continue
		}
		total += h.qty
	}
	return total
}

// Available is what `account` may plan on buying from a line now: the estimate less every
// OTHER account's live hold, never below zero. The account's own hold is not subtracted --
// it is the account's own plan.
func (b *ShopBoard) Available(sh RuneShop, rune, account string, now time.Time, tick time.Duration) int {
	est := b.Estimate(sh, rune, now, tick)
	b.mu.Lock()
	held := b.heldByOthersLocked(shopLineKey(sh, rune), account, now)
	b.mu.Unlock()
	if est -= held; est < 0 {
		return 0
	}
	return est
}

// AvailableAll is Available for every line the shop sells, in the map shape PlanPurchase takes.
func (b *ShopBoard) AvailableAll(sh RuneShop, account string, now time.Time, tick time.Duration) map[string]int {
	out := make(map[string]int, len(sh.Lines))
	for _, l := range sh.Lines {
		out[l.Rune] = b.Available(sh, l.Rune, account, now, tick)
	}
	return out
}

// Reserve holds up to qty of a line for `account` until now+ttl and returns how many it was
// granted: min(qty, Available). A hold the same account already has on that line is REPLACED,
// never added to, so a re-plan cannot ratchet a hold up. qty <= 0 drops the account's hold on
// the line. The grant is what the caller should plan on; a zero grant means another account got
// there first and this one should choose a different source.
func (b *ShopBoard) Reserve(sh RuneShop, rune, account string, qty int, now time.Time, ttl, tick time.Duration) int {
	if !sh.Sells(rune) || boardAccountKey(account) == "" {
		return 0
	}
	k := shopLineKey(sh, rune)
	me := boardAccountKey(account)
	est := b.Estimate(sh, rune, now, tick)
	b.mu.Lock()
	defer b.mu.Unlock()
	if qty <= 0 || ttl <= 0 {
		if b.holds[k] != nil {
			delete(b.holds[k], me)
		}
		return 0
	}
	free := est - b.heldByOthersLocked(k, account, now)
	if free < 0 {
		free = 0
	}
	grant := qty
	if grant > free {
		grant = free
	}
	if b.holds[k] == nil {
		b.holds[k] = map[string]shopHold{}
	}
	if grant <= 0 {
		delete(b.holds[k], me)
		return 0
	}
	b.holds[k][me] = shopHold{qty: grant, until: now.Add(ttl)}
	return grant
}

// Held is the account's own live hold on a line (0 when none).
func (b *ShopBoard) Held(sh RuneShop, rune, account string, now time.Time) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	h, ok := b.holds[shopLineKey(sh, rune)][boardAccountKey(account)]
	if !ok || !now.Before(h.until) {
		return 0
	}
	return h.qty
}

// Release drops every hold `account` has at one shop. Called when a trip ends, bought or not;
// a hold that is never released simply lapses at its TTL.
func (b *ShopBoard) Release(sh RuneShop, account string) {
	me := boardAccountKey(account)
	prefix := strings.ToLower(strings.TrimSpace(sh.Keeper)) + "|"
	b.mu.Lock()
	defer b.mu.Unlock()
	for k, m := range b.holds {
		if strings.HasPrefix(k, prefix) {
			delete(m, me)
		}
	}
}

// ShopTripHoldTTL is how long a Lundail or Aubury hold lasts before it lapses by itself: the
// whole trip plus slack, so a hold never expires under an account that is still walking, while
// an account that died or was restarted mid-trip frees its hold within one trip's time. At the
// real 600 ms tick a Mage Bank round trip is ~10 minutes and a Varrock round trip ~17-20
// (docs/handoff_scarab_2026-09-22.md); both are shorter at the lab's 200 ms tick.
const (
	LundailHoldTTL = 15 * time.Minute
	AuburyHoldTTL  = 30 * time.Minute
)
