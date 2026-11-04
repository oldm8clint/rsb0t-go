// Package peerbank is the fleet's shared, in-process answer to "who is holding what, and
// where are they standing?" — the directory that lets one account take a spare item from the
// NEAREST account that has one instead of every account walking to the single mule.
//
// WHY THIS IS NOT internal/controlpanel's AllBanks, which already answers the same question.
// It genuinely does, and better, for the panel: FleetSource.AllBanks reads EVERY account's
// bank out of ONE fleet snapshot, and its own doc comment records what the naive version cost
// (267 whole-fleet snapshots per refresh at the 266-account roster, starving /api/status of
// the gamestate locks it needed). Nothing here is a second attempt at that, and nothing here
// re-implements it.
//
// It is unreachable from a script, for a hard reason rather than a stylistic one: the import
// graph runs internal/controlpanel -> internal/fleet -> internal/bot, so a bot importing
// controlpanel (or fleet) is an import cycle the compiler refuses. A script cannot call
// AllBanks, today or after any amount of tidying, without inverting that graph.
//
// So this runs the other way round, and the inversion is the point rather than a workaround.
// AllBanks PULLS: something outside asks the fleet, and the fleet takes every session's lock
// to answer. This is PUSHED: each account calls Publish with its own bank at the one moment it
// already has that bank open and decoded, which every banking script does every trip anyway.
// The cost is therefore not "smaller than a fleet snapshot", it is ZERO extra reads — no
// snapshot, no session lock, nothing on /api/status's path. The directory is a map write per
// bank trip.
//
// The honest cost of pushing is STALENESS, and it is stated rather than hidden. An entry is a
// reading from the last time that account stood at a bank, not a live truth, so every entry
// carries UpdatedAt and every consumer is expected to age it out (peerplan.Reserve's own
// MaxAge). A directory row saying an account holds a bronze axe means "it did when it last
// looked", and the only thing that ever settles whether an item really moved is the ASKER's
// own pack rising — peerplan and peerrunner never treat a directory row as proof of anything.
//
// WHAT IS DELIBERATELY NOT HERE, on the same principle internal/mulebus states for itself:
// this package owns STORAGE and the one piece of bookkeeping that has to be atomic to work at
// all (Reserve/Release, below). It decides nothing. What counts as a spare, who is nearest,
// whether a trip is worth making, how long a turn lasts and what a loser does instead all live
// in internal/scripts/peerplan, where they are unit-tested without a session — exactly as
// mulebus leaves the trade policy to orderplan and the loot-claim policy to scarabplan.
//
// It carries no message kind and no wire shape of its own, and it is NOT a second bus. The
// request itself travels on internal/mulebus as an ordinary KindOrder addressed to the chosen
// peer instead of to a mule, which is the same message donorrunner.Await already publishes and
// mulerunner.Handout already serves. See peerplan's package doc for why that turned out to be
// the whole protocol.
package peerbank

import (
	"sort"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Holding is one named item and how many of it. Names are the game's own canonical object
// names, spelled as cachecfg spells them, because the whole point of a directory shared by
// scripts that never see each other's ids is that a name is the only identity both sides
// agree on. Count is units, with a non-stacking item counted once per slot.
type Holding struct {
	Item  string
	Count int
}

// Entry is one account's row in the directory.
type Entry struct {
	// Account is the account's own name, as the bus carries it.
	Account string
	// At is where the account was standing when it last published, and Located says whether
	// that reading is real. An account with no position fix is not a candidate for "nearest"
	// anything, and the two fields keep that different from "standing at 0,0".
	At      tile.Tile
	Located bool
	// Bank is what that account's bank held at UpdatedAt. Held is what was in its pack and
	// equipment. Both matter and they are NOT interchangeable: a spare is decided against
	// everything the account has (an axe in hand is still an axe it is using), while only the
	// banked copy can actually be withdrawn and handed over.
	Bank []Holding
	Held []Holding
	// UpdatedAt is when this row was published. Consumers age it out; this package does not.
	UpdatedAt time.Time
	// Working is the account's own statement that it is mid-task rather than at a bank or
	// idle, so a chooser can prefer interrupting somebody who is already standing still. It
	// is a hint, never a lock — Reserve below is the only thing that actually excludes.
	Working bool
}

// reservation is one holder's current turn: who has it and when it lapses.
type reservation struct {
	asker string
	until time.Time
}

// Directory is the shared board. One per fleet process, handed to every account's Context the
// same way internal/mulebus.Bus is, and safe for concurrent use by every session goroutine.
//
// The zero value is not usable; call New.
type Directory struct {
	mu       sync.Mutex
	entries  map[string]*Entry
	reserved map[string]reservation
}

// New returns an empty, ready-to-use Directory.
func New() *Directory {
	return &Directory{entries: make(map[string]*Entry), reserved: make(map[string]reservation)}
}

func key(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// Publish records one account's own reading of itself, replacing whatever it published before.
//
// Called by an account about ITSELF and never about anybody else: the directory has no
// authority of its own and no way to check a claim, so the only row an account may write is
// its own, and peerrunner.PublishSelf is the only caller in this repo. A row published about
// somebody else would be a lie nothing could detect — the same reasoning mulebus gives for
// leaving authentication to orderplan's allow list.
//
// bank and held are copied, so the caller may reuse its slices. An empty account name is
// ignored rather than creating an unaddressable row.
func (d *Directory) Publish(account string, at tile.Tile, located bool, bank, held []Holding, working bool) {
	k := key(account)
	if k == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.entries[k] = &Entry{
		Account:   strings.TrimSpace(account),
		At:        at,
		Located:   located,
		Bank:      append([]Holding(nil), bank...),
		Held:      append([]Holding(nil), held...),
		UpdatedAt: time.Now(),
		Working:   working,
	}
}

// Forget drops one account's row — called when a session goes away, so a logged-out account
// does not keep advertising a bronze axe nobody can walk to.
func (d *Directory) Forget(account string) {
	k := key(account)
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.entries, k)
	delete(d.reserved, k)
}

// Entries returns every row, sorted by account name so a caller's own tie-breaking is
// deterministic rather than dependent on Go's randomised map order. The slices inside each
// Entry are copies; the caller may keep them.
//
// busyAt is the moment reservations are judged against, so a caller reasons about one instant
// rather than a board that shifts under it mid-scan.
func (d *Directory) Entries(busyAt time.Time) []Entry {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]Entry, 0, len(d.entries))
	for k, e := range d.entries {
		cp := *e
		cp.Bank = append([]Holding(nil), e.Bank...)
		cp.Held = append([]Holding(nil), e.Held...)
		if r, ok := d.reserved[k]; ok && busyAt.Before(r.until) {
			cp.Working = true
		}
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Account < out[j].Account })
	return out
}

// Busy reports whether holder's turn is currently taken, and by whom.
func (d *Directory) Busy(holder string, now time.Time) (string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	r, ok := d.reserved[key(holder)]
	if !ok || !now.Before(r.until) {
		return "", false
	}
	return r.asker, true
}

// Reserve takes holder's turn for asker until now+ttl, and reports whether it got it.
//
// THIS IS THE WHOLE ANSWER TO CONTENTION, and it is here rather than in peerplan because it is
// the one part that cannot be pure: deciding "is this holder free?" and "then it is mine" has
// to be one atomic step, or sixty accounts all read "free" in the same instant and all walk at
// the same peer. That is not hypothetical — it is the bottleneck this whole feature exists to
// remove, rebuilt one layer down.
//
// An account serves ONE trade at a time, exactly as the mule does, so a turn is exclusive. A
// caller that does not get the turn is expected to go back to work and try again later
// (peerplan.BusyRetry), never to queue: queueing is what made the mule a 3.3x bottleneck, and
// the live Seers market stall already taught this fleet that per-stall turn-taking plus a short
// retry works where a queue does not.
//
// Re-reserving a turn the same asker already holds EXTENDS it, so an asker that is still
// walking keeps its turn alive the same way a mulebus claim is refreshed by re-publishing.
func (d *Directory) Reserve(holder, asker string, now time.Time, ttl time.Duration) bool {
	hk, ak := key(holder), key(asker)
	if hk == "" || ak == "" || hk == ak {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if r, ok := d.reserved[hk]; ok && now.Before(r.until) && key(r.asker) != ak {
		return false
	}
	d.reserved[hk] = reservation{asker: strings.TrimSpace(asker), until: now.Add(ttl)}
	return true
}

// Release gives holder's turn back, but only if asker is the one that holds it — a late
// arrival must not be able to free somebody else's turn out from under them. Mirrors
// mulebus.UnclaimMessage's own rule that only the original claimant's release counts.
func (d *Directory) Release(holder, asker string) {
	hk, ak := key(holder), key(asker)
	d.mu.Lock()
	defer d.mu.Unlock()
	if r, ok := d.reserved[hk]; ok && key(r.asker) == ak {
		delete(d.reserved, hk)
	}
}

// Count is how many rows the directory holds, for a status line.
func (d *Directory) Count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.entries)
}
