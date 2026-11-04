package restockrunner

// The donor's half of the conversation, bus-only: hear an appeal, decide whether to pledge,
// pledge, and recognise the grant that says "you, now". The actual walk, bank and trade is
// donate.go; this file touches no session, so its tests exercise the real message flow.

import (
	"fmt"
	"time"

	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/restockplan"
	"rsb0t-go/internal/scripts/tile"
)

// Answerer is one donor account's side of the restock conversation. Like Campaign it owns no
// lock and belongs to one bot's own Loop goroutine.
type Answerer struct {
	watcher *mulebus.Watcher
	// mule is the only account this donor will answer, matched case- and underscore-
	// insensitively. NOT optional and there is no "any mule" mode: an appeal is an instruction
	// to walk somewhere with money, and the bus carries no authentication of any kind (see
	// orderplan.Policy.Allowed's own doc comment for the same reasoning in the other
	// direction). A donor that would answer any name could be walked anywhere by anything that
	// can publish.
	mule string
	// lastDonated is fed back by the caller through Donated, so the cooldown counts from a
	// completed trip rather than from the moment it set out.
	lastDonated time.Time
	// pledged is the amount this account last pledged, kept for a log line.
	pledged int
	// pending is the most recent BROADCAST appeal from my mule, and pendingAt when it landed.
	// An appeal is a standing need for restockplan.AppealTTL, not an instant — see that
	// constant for the live failure that taught us so, where every appeal was consumed by a
	// poll that happened to land mid bank trip and an account with 57,995 spare coins answered
	// none of them. pledgedAt is when this account last answered; it re-screens the pending
	// appeal on every poll until it has answered THAT appeal or the appeal goes stale.
	pending              restockplan.Appeal
	pendingAt, pledgedAt time.Time

	pledges, grants int
}

// NewAnswerer starts one for a donor that reports to mule. A blank mule makes every Poll a
// no-op, which is what an account with no mule configured should do.
func NewAnswerer(mule string) *Answerer {
	return &Answerer{watcher: mulebus.NewWatcher(), mule: mule}
}

// Donated records that a donation trip finished, whatever came of it, starting the cooldown.
// Called on ANY terminal outcome: an account that walked to the mule and failed has still spent
// the trip, and letting it immediately try again is how a crowd at the post sustains itself
// (internal/bot/fisher.go's own hand-off jitter comment records that measurement).
func (a *Answerer) Donated(now time.Time) {
	a.lastDonated = now
	a.pledged = 0
	// Forget the appeal that sent us: whether it was met is the mule's business, and a donor
	// that kept re-screening the same one would pledge again the moment its cooldown lapsed
	// for a need that may be long over.
	a.pending, a.pendingAt = restockplan.Appeal{}, time.Time{}
}

// LastDonated is when the last trip finished; zero means never.
func (a *Answerer) LastDonated() time.Time { return a.lastDonated }

// Stats is pledges sent and grants received, for a log line.
func (a *Answerer) Stats() (pledges, grants int) { return a.pledges, a.grants }

// Call is a grant: the mule has told THIS account to come now.
type Call struct {
	// Mule is who called, Item and Qty what it expects, Post where it is standing.
	Mule string
	Item string
	Qty  int
	Post tile.Tile
}

// Poll drains the bus once and reports the one thing the caller has to act on: a grant.
//
// Everything else it handles itself. A broadcast appeal is screened against restockplan's own
// donor rule and answered with a pledge if this account genuinely has something spare — that
// needs no decision from the calling script and no interruption to it, which is the point: a
// pledge costs one bus message and commits the account to nothing.
//
// It returns at most one Call per poll. A second grant in the same poll would mean two mules
// called at once, and a donor cannot be in two places; the first is taken and the rest are
// ignored, which the mules' own grant TTLs clean up.
//
// state is this account's own current position — see restockplan.DonorState, and in particular
// Earmarked, which the caller MUST fill in honestly (a woodcutter holding the mule's own axe
// money is arithmetically the richest donor in the fleet).
func (a *Answerer) Poll(now time.Time, bus *mulebus.Bus, me string, state restockplan.DonorState) (Call, bool, string) {
	if bus == nil || me == "" || a.mule == "" {
		return Call{}, false, "no bus, name or mule configured"
	}
	// The cooldown is the answerer's own bookkeeping, not the caller's, so it is filled in
	// here once for BOTH branches below. Doing it only on the broadcast branch was a real bug
	// the tests caught: a grant issued from a pledge made before the last donation would then
	// be obeyed, and an account could be sent out twice back to back with the cooldown
	// apparently in force.
	if state.LastDonated.IsZero() {
		state.LastDonated = a.lastDonated
	}
	var (
		call   Call
		called bool
		why    string
	)
	for _, msg := range a.watcher.Poll(bus) {
		if msg.Kind != mulebus.KindAppeal {
			continue
		}
		if !mulebus.NamesMatch(msg.From, a.mule) {
			// Not my mule. Silently ignored rather than logged per message: on a fleet bus
			// this is the ordinary case, not an anomaly.
			continue
		}
		switch {
		case mulebus.IsGrant(msg, me):
			if called {
				continue
			}
			// A grant is checked, not obeyed. Two things can make one meaningless by the time
			// it lands: the appeal may be for an item this account has no rule for, and the
			// account may since have spent what it pledged. DecideAnswer re-runs the whole
			// donor rule against the CURRENT reading, so a grant can only ever send an account
			// that would pledge again right now.
			qty, ok, reason := restockplan.DecideAnswer(now, restockplan.Appeal{Item: msg.Item, Qty: msg.Qty}, state)
			if !ok {
				why = fmt.Sprintf("%q called me for %d %s but %s", msg.From, msg.Qty, msg.Item, reason)
				continue
			}
			call = Call{Mule: msg.From, Item: msg.Item, Qty: qty, Post: msg.Tile}
			called = true
			a.grants++
		case mulebus.IsBroadcastAppeal(msg):
			// Recorded, not decided on here. The decision runs once below, against the same
			// state, whether the appeal arrived on this poll or two polls ago.
			a.pending, a.pendingAt = restockplan.Appeal{Item: msg.Item, Qty: msg.Qty}, now
		}
	}
	if called {
		return call, true, why
	}
	// The standing appeal. Re-screened every poll until this account has answered THIS one or
	// it goes stale, so a script that was banking or walking when it arrived still gets to
	// answer it when it is free.
	if a.pendingAt.IsZero() || now.Sub(a.pendingAt) > restockplan.AppealTTL {
		return Call{}, false, why
	}
	if !a.pledgedAt.Before(a.pendingAt) {
		return Call{}, false, why // already answered this appeal
	}
	qty, ok, reason := restockplan.DecideAnswer(now, a.pending, state)
	if !ok {
		return Call{}, false, fmt.Sprintf("%q needs %d %s and I cannot help yet: %s", a.mule, a.pending.Qty, a.pending.Item, reason)
	}
	bus.Publish(mulebus.PledgeMessage(me, a.mule, a.pending.Item, qty))
	a.pledged, a.pledgedAt = qty, now
	a.pledges++
	return Call{}, false, fmt.Sprintf("pledged %d %s to %q", qty, a.pending.Item, a.mule)
}
