package wcfletchplan

// The hand-off trip: what keeps an account on the mule's world across the script restart a hop to it
// causes, whatever world the account calls home.
//
// # The loss this exists for (live build 8c77500e, 12 October 2026)
//
// A hand-off is a trade, and a trade needs both accounts on one world. The mule is on world 1, so for
// an account that lives anywhere else donorrunner.Run hops to world 1 first, and the fleet restarts the
// script there. That restarted script reads its home again and, finding itself away from it, hops home
// before the trade: the notes it drew stay in its pack, the next bank visit puts them back, the next
// hand-off falls due, and the loop runs until the fleet's hop limit.
//
// The yew world spread (yewworlds.go) carried a trip of its own on its registry, so a yew account
// survived the hop. But a trip only stood for an account the registry held, and the twelve accounts
// switched to magic that day were in no registry: each lived on the world its homeWorld setting
// named (1, 2 or 3), and the four on each of worlds 2 and 3 could never have handed anything over.
//
// So the trip is its own registry, held for every account that sets out, and nothing else: the
// script marks it before donorrunner.Run, the restarted script on the mule's world sees it, stays
// (homeworld.go's hop is held while it stands) and resumes the hand-off, and the end of the hand-off
// ends it. TripTTL bounds a trip that never ends.

import (
	"sync"
	"time"
)

// HandoffTrips is the process-wide record of the accounts on a hand-off trip. The zero value is not
// usable; build one with NewHandoffTrips.
type HandoffTrips struct {
	mu    sync.Mutex
	began map[string]time.Time
}

// NewHandoffTrips returns an empty record.
func NewHandoffTrips() *HandoffTrips { return &HandoffTrips{began: map[string]time.Time{}} }

var sharedHandoffTrips = NewHandoffTrips()

// SharedHandoffTrips is the record the fleet's accounts share: one process holds every account, so a
// script restart (a hop) still finds its trip.
func SharedHandoffTrips() *HandoffTrips { return sharedHandoffTrips }

// Begin records that who set out to hand its stock to the mule at now. While it stands (TripTTL)
// the account is held on whatever world it is on; End ends it.
func (h *HandoffTrips) Begin(who string, now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.began[normWho(who)] = now
}

// End ends who's trip: the hand-off was made, given up, or never needed.
func (h *HandoffTrips) End(who string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.began, normWho(who))
}

// On reports whether who has a trip under way at now. A trip older than TripTTL is over.
func (h *HandoffTrips) On(who string, now time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	key := normWho(who)
	at, ok := h.began[key]
	if !ok {
		return false
	}
	if now.Sub(at) > TripTTL {
		delete(h.began, key)
		return false
	}
	return true
}
