package bot

// The MuleReceiver's cooked-food BUFFER (a user request), the runner half of
// mulereceiverplan.CookBufferDue (the reasoning and the rule are there). OFF unless "cookBufferFood"
// is set above 0 AND "cookIdle" is on; with it off nothing here runs and the mule behaves as before.
//
//	cookBufferFood  int  default 0 (off) -- cooked food to build and hold in the bank; e.g. 2000
//
// A buffer round is the ordinary mulerunner.CookRound (one full pack, about 99 s), started in front of
// queued hand-out KITS only: never over a trade request, an announced donor or a single-item order.

import (
	"strings"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/phaselog"
	"rsb0t-go/internal/scripts/fighterfood"
	"rsb0t-go/internal/scripts/mulereceiverplan"
	"rsb0t-go/internal/scripts/mulerunner"
)

// noteBankFood records the cooked food and the raw fish a bank reading holds (a no-op when the buffer
// is off, so the live path costs nothing).
func (b *muleBot) noteBankFood(ctx *Context, items []gamestate.SlotItem) {
	if b.cookBuffer <= 0 || ctx.Config == nil || len(items) == 0 {
		return
	}
	cooked, raw := 0, 0
	for _, it := range items {
		if it.Count <= 0 {
			continue
		}
		o, ok := ctx.Config.Obj(int(it.ID))
		if !ok || o.Noted() {
			continue
		}
		name := strings.TrimSpace(o.Name)
		if rest, isRaw := strings.CutPrefix(strings.ToLower(name), "raw "); isRaw {
			if _, food := fighterfood.BaseFood(rest); food {
				raw += int(it.Count)
			}
			continue
		}
		if _, food := fighterfood.BaseFood(name); food {
			cooked += int(it.Count)
		}
	}
	b.cookedSeen, b.rawSeen, b.cookedKnown = cooked, raw, true
}

// minKitWait is the shortest time any queued kit's donor will still wait.
func (b *muleBot) minKitWait(now time.Time) time.Duration {
	least := time.Duration(0)
	first := true
	for _, q := range b.kits {
		if left := q.until.Sub(now); first || left < least {
			least, first = left, false
		}
	}
	return least
}

// cookBufferStep runs one buffer cook round when mulereceiverplan.CookBufferDue says so, and reports
// whether it spent this Loop call on it.
func (b *muleBot) cookBufferStep(ctx *Context, d *mulerunner.Deps) bool {
	if b.cookBuffer <= 0 || !b.cookIdle || !b.handout {
		return false
	}
	now := time.Now()
	queued := 0
	if b.watcher != nil {
		queued = b.watcher.QueuedRequests()
	}
	in := mulereceiverplan.CookBufferInput{
		Buffer: b.cookBuffer, Cooked: b.cookedSeen, CookedKnown: b.cookedKnown, Raw: b.rawSeen,
		QueuedRequests: queued, Incoming: b.incomingPending(), Orders: len(b.orders),
		Kits: len(b.kits), MinKitWait: b.minKitWait(now),
		SinceLastBufferRound: time.Since(b.lastBufferRound), OnTrip: b.trip != nil,
	}
	if b.lastBufferRound.IsZero() {
		in.SinceLastBufferRound = time.Hour
	}
	if !mulereceiverplan.CookBufferDue(in) {
		return false
	}
	b.lastBufferRound = now
	ctx.logf("cooking buffer: %d cooked in the bank of %d wanted, %d kit(s) queued with at least %s to wait: one cook round before the next kit",
		b.cookedSeen, b.cookBuffer, len(b.kits), in.MinKitWait.Round(time.Second))
	b.cookingBuffer = true
	ctx.SetPhase(phaselog.Cooking)
	if _, ok := mulerunner.CookRound(d); ok {
		ctx.logf("cooking buffer: round complete")
	}
	b.cookingBuffer = false
	ctx.SetPhase(phaselog.Idle)
	return true
}
