package bot

// Keeping the mule's published bank current (2026-09-23, for ScarabSwarm's single mule-ask entry
// point, which filters every ask by what the mule published BEFORE walking to it).
//
// The mule published its bank (bot.PublishBank, one write into the shared peerbank directory)
// only after a received haul and inside a hand-out. So after a restart, until its first trade,
// there was no published bank at all, and every Scarab account asked blind -- a walk to Catherby
// for a mule that might hold nothing it wants. And a row ages out after peerplan.DefaultMaxAge, so
// a mule idle for ten minutes went unpublished again.
//
// Now: every bank the mule opens publishes, as it opens and as it closes (mulerunner's
// Deps.OnBankOpen), at most once every bankPublishEvery; and a quiet Loop that has not published
// yet, or not for bankRefreshEvery, opens the bank beside its post just to publish. The explicit
// publishes after a haul and in a hand-out are unchanged and never throttled.

import (
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/mulerunner"
	"rsb0t-go/internal/scripts/peerplan"
)

// bankPublishEvery rate-limits the publish on an ordinary bank open, so a cook round's open and
// close, or a run of deposits, is one directory write rather than several.
const bankPublishEvery = 5 * time.Second

// bankRefreshEvery is how stale the published bank may get on a quiet mule before it opens its
// bank just to publish: half the age at which peerplan stops believing a row.
const bankRefreshEvery = peerplan.DefaultMaxAge / 2

// publishBankThrottled is Deps.OnBankOpen: publish unless one went out in the last
// bankPublishEvery.
func (b *muleBot) publishBankThrottled(ctx *Context, items []gamestate.SlotItem, now time.Time) bool {
	b.noteBankFood(ctx, items)
	if ctx.Peers == nil || len(items) == 0 {
		return false
	}
	if !b.bankPublishedAt.IsZero() && now.Sub(b.bankPublishedAt) < bankPublishEvery {
		return false
	}
	PublishBank(ctx, items, false)
	b.bankPublishedAt = now
	return true
}

// publishBankNow is Deps.OnBankClose: publish whatever the throttle says.
func (b *muleBot) publishBankNow(ctx *Context, items []gamestate.SlotItem, now time.Time) bool {
	b.noteBankFood(ctx, items)
	if ctx.Peers == nil || len(items) == 0 {
		return false
	}
	PublishBank(ctx, items, false)
	b.bankPublishedAt = now
	return true
}

// bankOpenBackoff is how long a quiet mule waits before trying again when its booth would not
// open for a refresh: the booth is busy or out of reach, and retrying sooner costs a walk and a
// click on every Loop for nothing.
const bankOpenBackoff = time.Minute

// bankRefreshDue reports whether a quiet Loop should open the bank just to publish it.
func (b *muleBot) bankRefreshDue(ctx *Context, now time.Time) bool {
	if ctx.Peers == nil || !b.bank {
		return false
	}
	return b.bankPublishedAt.IsZero() || now.Sub(b.bankPublishedAt) >= bankRefreshEvery
}

// refreshAllowed is refreshPublishedBank's gate: due, and nothing waiting on the mule -- no trip,
// no queued trade request, no announced donor, no hand-out order.
func (b *muleBot) refreshAllowed(ctx *Context, now time.Time) bool {
	if !b.bankRefreshDue(ctx, now) || b.trip != nil || b.incomingPending() > 0 || len(b.orders) > 0 {
		return false
	}
	return b.watcher == nil || b.watcher.QueuedRequests() == 0
}

// refreshPublishedBank opens the bank beside the post and publishes it, when due and nothing is
// waiting on the mule. Reports whether it opened the bank.
func (b *muleBot) refreshPublishedBank(ctx *Context, d *mulerunner.Deps) bool {
	if !b.refreshAllowed(ctx, time.Now()) {
		return false
	}
	first := b.bankPublishedAt.IsZero()
	if _, ok := mulerunner.ReadBankTotals(d); !ok {
		// Try again after bankOpenBackoff rather than every Loop: the booth may be busy. (The
		// first version retried after the 5 s publish throttle -- review of
		// claude/handoff-muleseen.)
		b.bankPublishedAt = time.Now().Add(bankOpenBackoff - bankRefreshEvery)
		return true
	}
	if first && !b.bankPublishedAt.IsZero() {
		ctx.logf("published this mule's bank for the fleet (first reading since it started)")
	}
	// An empty bank publishes nothing; do not reopen it every Loop for that.
	if now := time.Now(); b.bankPublishedAt.Before(now.Add(-bankPublishEvery)) {
		b.bankPublishedAt = now
	}
	return true
}
