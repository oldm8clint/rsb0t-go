package mulebus

// Roster is who has announced themselves on the bus lately — ported from
// muleProtocol.ts's own Roster class. Entries expire, so a donor that logs out stops
// holding the mule at its post, and a mule that never hears anything still works: the
// roster only ever adds patience, never permission (the allow-list, not this, is what
// grants a trade).
//
// Go-idiom deviation, the same choice mulereceiverplan's own Collection/ActivityLog/
// PickupQueue already made: muleProtocol.ts's Roster defaults its clock to Date.now();
// this package must stay usable from a pure test without touching the wall clock, so
// every method takes the moment as an explicit time.Time parameter instead.

import (
	"strings"
	"time"
)

// Roster tracks the most recent time each name was heard from, expiring entries after
// ttl.
type Roster struct {
	ttl  time.Duration
	seen map[string]time.Time // lowercased name -> expires at
}

// NewRoster is an empty roster whose entries expire ttl after they were last marked.
func NewRoster(ttl time.Duration) *Roster {
	return &Roster{ttl: ttl, seen: map[string]time.Time{}}
}

func rosterKey(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// Mark records that name was heard from at now, refreshing its expiry.
func (r *Roster) Mark(now time.Time, name string) {
	r.seen[rosterKey(name)] = now.Add(r.ttl)
}

// Has reports whether name's last Mark hasn't yet expired as of now, sweeping it out if
// it has.
func (r *Roster) Has(now time.Time, name string) bool {
	key := rosterKey(name)
	expires, ok := r.seen[key]
	if !ok {
		return false
	}
	if !now.Before(expires) {
		delete(r.seen, key)
		return false
	}
	return true
}

// Forget removes name unconditionally.
func (r *Roster) Forget(name string) {
	delete(r.seen, rosterKey(name))
}

// Names is every live entry as of now, expired ones swept first. Order is unspecified
// (a Go map has none to begin with, unlike the TS Map's insertion order — see this
// package's doc comment on Go-idiom deviations).
func (r *Roster) Names(now time.Time) []string {
	out := make([]string, 0, len(r.seen))
	for key, expires := range r.seen {
		if now.Before(expires) {
			out = append(out, key)
		} else {
			delete(r.seen, key)
		}
	}
	return out
}

// Size is how many live entries remain as of now.
func (r *Roster) Size(now time.Time) int {
	return len(r.Names(now))
}
