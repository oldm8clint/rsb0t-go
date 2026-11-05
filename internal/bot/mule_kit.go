package bot

// The MuleReceiver's half of the BUNDLED KIT order (owner, 2026-09-23): one trade of at most 28
// trade-screen slots per kit, from the fleet's one mule. Kept out of mule.go's own flow the way
// muleledger.go is: mule.go only routes a KindKitOrder here (pollBus), counts queued kits as
// pending work (ordersPending), and offers serveKit a Loop call when no single-item order is
// waiting (serveOrder). The decision is orderplan.DecideKit, the mechanics mulerunner.HandoutKit.
//
// THE FISHING HAND-OFF COMES FIRST. A kit is a long bank visit (up to 28 withdrawals, a note-mode
// switch) and a long trade, on the account ~300 donors queue for, so a kit is served only while no
// donor is announced and no trade request is waiting -- or once it has waited kitMaxDefer, so a
// sustained fishing queue delays a kit and never starves it. Single-item orders keep the priority
// they always had: they are served first, as before.
//
// AND NEVER TWO OVERDUE KITS IN A ROW WHILE FISHERS WAIT. The first version made every kit past
// kitMaxDefer due, and Loop serves an order before RunCycle and returns, so a backlog of overdue
// kits was served back to back: 100 kits was ~50 minutes with no fishing hand-off at all (review
// of r4). After a kit served while fishers were waiting, the next kit waits for one RunCycle turn
// (kitTurnOwed), so the two queues alternate. Queued kits also count in the published queue depth,
// so donors deciding whether to draw a load see the work the mule really has.
//
// A KIT NOBODY IS WAITING FOR IS NEVER SERVED. The donor says how long it will wait (the order's
// EtaMs, donorrunner.DefaultKitTimeout by default) and cancels when it stops waiting; the mule
// drops a kit past its donor's wait, with an answer, and drops one its donor cancels.

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/muleledger"
	"rsb0t-go/internal/phaselog"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/mulereceiverplan"
	"rsb0t-go/internal/scripts/mulerunner"
	"rsb0t-go/internal/scripts/orderplan"
)

// kitMaxDefer is the longest a queued kit waits behind the fishing hand-off before it is served
// anyway. Shortened from two minutes with the donors' wait (muleAskWaitMinutes, default four): a kit waits at most
// kitMaxDefer behind the fishers and still leaves its donor kitMinLeft for the bank visit and the trade.
const kitMaxDefer = 90 * time.Second

// kitMinLeft is the least time a kit's donor must still be waiting for the mule to start its
// hand-out: a bank visit of up to 28 withdrawals and a trade take about a minute, and a donor that
// gives up mid-trade leaves the mule holding a drawn kit (revK, S2).
const kitMinLeft = 90 * time.Second

// kitServeGuess is what one kit hand-out is expected to cost, for the "queued" answer's estimate.
const kitServeGuess = 45 * time.Second

// kitRepeatWindow is how long after serving a kit its id is remembered, so a re-publish the
// donor made while it was being served is not queued and served again.
const kitRepeatWindow = 2 * time.Minute

type queuedKit struct {
	msg mulebus.Message
	at  time.Time
	// until is when the donor stops waiting for it.
	until time.Time
}

// kitDefaultWait is how long a donor waits for a kit when its order does not say: the donor
// side's own default.
const kitDefaultWait = donorrunner.DefaultKitTimeout

// dropDeadKits drops every queued kit its donor has stopped waiting for, answering each so a
// donor still listening knows.
func (b *muleBot) dropDeadKits(ctx *Context) {
	now := time.Now()
	for key, q := range b.kits {
		if now.Before(q.until) {
			continue
		}
		delete(b.kits, key)
		ctx.logf("wall bus: dropping %q's kit — it has waited %s, past the donor's own wait", q.msg.From, now.Sub(q.at).Round(time.Second))
		if ctx.Bus != nil {
			ctx.Bus.Publish(mulebus.KitAnswerMessage(ctx.Username, q.msg.From, q.msg.KitID, nil, "that kit waited too long; ask again", 0))
		}
	}
}

// cancelKit drops key's queued kit when its donor cancels it (mulebus.KitCancelMessage).
func (b *muleBot) cancelKit(ctx *Context, key, id string) {
	if q, ok := b.kits[key]; ok && q.msg.KitID == id {
		delete(b.kits, key)
		ctx.logf("wall bus: %q cancelled its kit", q.msg.From)
	}
}

type servedKit struct {
	id string
	at time.Time
}

// ordersPending is how many hand-out orders of any kind are waiting: single items and kits.
func (b *muleBot) ordersPending() int { return len(b.orders) + len(b.kits) }

func kitLinesFrom(msg mulebus.Message) []orderplan.KitLine {
	out := make([]orderplan.KitLine, 0, len(msg.Kit))
	for _, l := range msg.Kit {
		out = append(out, orderplan.KitLine{Item: l.Item, Qty: l.Qty, Noted: l.Noted})
	}
	return out
}

func busKitLines(lines []orderplan.KitLineDecision) []mulebus.KitLine {
	out := make([]mulebus.KitLine, 0, len(lines))
	for _, l := range lines {
		out = append(out, mulebus.KitLine{Item: l.Item, Qty: l.Qty, Noted: l.Noted, Reason: l.Reason})
	}
	return out
}

// takeKit screens a kit order off the bus: queued with a "queued" answer, or refused on the spot.
func (b *muleBot) takeKit(ctx *Context, msg mulebus.Message) {
	answer := func(reason string) {
		ctx.Bus.Publish(mulebus.KitAnswerMessage(ctx.Username, msg.From, msg.KitID, nil, reason, 0))
	}
	if !b.handout {
		answer("hand-outs are switched off")
		return
	}
	key := strings.ToLower(strings.TrimSpace(msg.From))
	if id, ok := b.servingKit[key]; ok && id == msg.KitID {
		return // a re-publish of the kit being served right now
	}
	if last, ok := b.servedKits[key]; ok && last.id == msg.KitID && time.Since(last.at) < kitRepeatWindow {
		return // a re-publish of the kit just served
	}
	if q, queued := b.kits[key]; queued {
		q.msg = msg // the donor's latest word, same place in the queue
		b.kits[key] = q
		return
	}
	// Two accounts on different worlds cannot trade; the bus is world-agnostic, so say so now
	// rather than walking to a booth for a donor nowhere to be seen.
	if mine := ctx.World(); mine > 0 && msg.World > 0 && msg.World != mine {
		answer(fmt.Sprintf("I am on world %d, not %d", mine, msg.World))
		return
	}
	// The donor-level half of the screen, decidable from the message alone: the item-level half
	// needs the bank and runs in HandoutKit.
	now := time.Now()
	switch {
	case !orderplan.AllowedFrom(b.handoutPolicy, msg.From):
		answer("not on the allow list")
		return
	case b.handoutPolicy.MaxOrdersPerDonor <= 0:
		answer("hand-outs are switched off")
		return
	case b.handoutLedger == nil:
		answer("no hand-out ledger")
		return
	case b.handoutLedger.OrdersTo(now, msg.From) >= b.handoutPolicy.MaxOrdersPerDonor:
		answer("already handed you one recently")
		return
	case len(msg.Kit) == 0:
		answer(orderplan.DeclineKitEmpty)
		return
	}
	if b.kits == nil {
		b.kits = map[string]queuedKit{}
	}
	wait := time.Duration(msg.EtaMs) * time.Millisecond
	if wait <= 0 {
		wait = kitDefaultWait
	}
	b.kits[key] = queuedKit{msg: msg, at: now, until: now.Add(wait)}
	ctx.logf("wall bus: %q asks for a kit of %d line(s) — queued", msg.From, len(msg.Kit))
	eta := time.Duration(len(b.kits)) * kitServeGuess
	if n := b.incomingPending(); n > 0 {
		eta += time.Duration(n) * b.cycle()
	}
	ctx.Bus.Publish(mulebus.KitAnswerMessage(ctx.Username, msg.From, msg.KitID, nil, "", eta))
}

// kitDue reports whether the oldest queued kit may be served now (see this file's doc comment).
func (b *muleBot) kitDue() (string, queuedKit, bool) {
	var key string
	var oldest queuedKit
	for k, q := range b.kits {
		if key == "" || q.at.Before(oldest.at) {
			key, oldest = k, q
		}
	}
	if key == "" {
		return "", queuedKit{}, false
	}
	return key, oldest, mulereceiverplan.KitDue(b.fishersQuiet(), b.kitTurnOwed, time.Since(oldest.at), kitMaxDefer)
}

// orderGate is the queue state mulereceiverplan.OrderServable reads, the same facts serveOrder acts on.
func (b *muleBot) orderGate() mulereceiverplan.OrderGate {
	g := mulereceiverplan.OrderGate{
		SingleOrders: len(b.orders), Kits: len(b.kits),
		KitTurnOwed: b.kitTurnOwed, FishersQuiet: b.fishersQuiet(), KitMaxDefer: kitMaxDefer,
	}
	for _, q := range b.kits {
		if age := time.Since(q.at); age > g.OldestKitAge {
			g.OldestKitAge = age
		}
	}
	return g
}

// donorPriorityWindow is how long after announcing a hand-off donor keeps kits waiting behind it. On 5 Nov 2026 donors
// still on their way held kits a whole idleTimeout at a time and left the mule idle 63% of the time.
const donorPriorityWindow = 20 * time.Second

// fishersQuiet is whether no trade request is waiting and no donor has just announced.
func (b *muleBot) fishersQuiet() bool {
	if b.watcher != nil && b.watcher.QueuedRequests() > 0 {
		return false
	}
	b.incomingPending()
	for _, at := range b.incoming {
		if time.Since(at) < donorPriorityWindow {
			return false
		}
	}
	return true
}

// keyedKit is a queued kit with its queue key.
type keyedKit struct {
	key string
	q   queuedKit
}

// kitBatchMax is how many kits one bank visit may draw (setting kitBatch; 1 serves them one at a time, as before).
func (b *muleBot) kitBatchMax() int {
	if b.kitBatch < 1 {
		return 1
	}
	return b.kitBatch
}

// kitCompanions are the queued kits that ride along with the oldest one in one bank visit: only while no fisher
// waits (a batch is several trades long), oldest first, and only those whose donor is still waiting for the
// whole batch to get to them (kitMinLeft, and a kitServeGuess for every trade before theirs).
func (b *muleBot) kitCompanions(first string) []keyedKit {
	if b.kitBatchMax() < 2 || !b.fishersQuiet() {
		return nil
	}
	var cands []keyedKit
	for k, q := range b.kits {
		if k != first {
			cands = append(cands, keyedKit{k, q})
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].q.at.Before(cands[j].q.at) })
	var out []keyedKit
	for _, c := range cands {
		if len(out)+1 >= b.kitBatchMax() {
			break
		}
		need := kitMinLeft + time.Duration(len(out)+1)*kitServeGuess
		if time.Until(c.q.until) < need {
			continue
		}
		out = append(out, c)
	}
	return out
}

// kitGiveFrom is the hand-over a kit order says the donor will put on its side of the trade: positive lines only,
// at most one trade screen's worth.
func kitGiveFrom(msg mulebus.Message) []orderplan.KitLine {
	var out []orderplan.KitLine
	for _, l := range msg.Give {
		if l.Qty > 0 && strings.TrimSpace(l.Item) != "" && len(out) < orderplan.KitLinesMax {
			out = append(out, orderplan.KitLine{Item: l.Item, Qty: l.Qty, Noted: l.Noted})
		}
	}
	return out
}

// serveKit fulfils the oldest queued kit if it is due -- and, while no fisher waits, up to kitBatchMax-1 more kits
// in the same bank visit (mulerunner.HandoutKits) -- and reports whether it spent this Loop call on it.
func (b *muleBot) serveKit(ctx *Context, d *mulerunner.Deps) bool {
	key, q, due := b.kitDue()
	if !due {
		return false
	}
	delete(b.kits, key)
	if left := time.Until(q.until); left < kitMinLeft {
		// Too late to finish before the donor gives up: answer instead of drawing a kit that
		// would be left in the mule's pack (revK, S2).
		ctx.logf("wall bus: not serving %q's kit — its donor stops waiting in %s", q.msg.From, left.Round(time.Second))
		if ctx.Bus != nil {
			ctx.Bus.Publish(mulebus.KitAnswerMessage(ctx.Username, q.msg.From, q.msg.KitID, nil, "that kit would not be ready before you stop waiting; ask again", 0))
		}
		return false
	}
	group := append([]keyedKit{{key, q}}, b.kitCompanions(key)...)
	for _, g := range group[1:] {
		delete(b.kits, g.key)
	}
	// EVERY served kit owes the fishing queue a turn. Deciding it from the queue before the kit
	// started missed a fisher announcing DURING the kit, and the next overdue kit ran straight
	// after (revK, S1). A queue still quiet at the next Loop ignores the debt (kitDue).
	b.kitTurnOwed = true
	ctx.SetPhase(phaselog.Handout)
	b.servingKit = servingKitOrder{}
	// finalSent is whether the donor has already been told the kit is NOT coming (a refusal from
	// HandoutKit itself); a plan announced before a trade that then failed is not final.
	finalSent := make([]bool, len(group))
	reqs := make([]mulerunner.KitRequest, len(group))
	for i, g := range group {
		i, msg := i, g.q.msg
		b.servingKit[g.key] = msg.KitID
		reqs[i] = mulerunner.KitRequest{
			To: msg.From, ID: msg.KitID, Lines: kitLinesFrom(msg), Room: msg.FreeSlots,
			Policy: b.handoutPolicy, Ledger: b.handoutLedger,
			Give: kitGiveFrom(msg), GiveSlots: msg.GiveSlots,
			Announce: func(lines []orderplan.KitLineDecision, reason string) {
				finalSent[i] = finalSent[i] || reason != ""
				ctx.Bus.Publish(mulebus.KitAnswerMessage(ctx.Username, msg.From, msg.KitID, busKitLines(lines), reason, 0))
			},
		}
	}
	if len(group) > 1 {
		ctx.logf("wall bus: serving %d kits in one bank visit", len(group))
	}
	results := mulerunner.HandoutKits(d, reqs, b.kitBatchMax())
	b.servingKit = nil
	if b.servedKits == nil {
		b.servedKits = map[string]servedKit{}
	}
	for k, v := range b.servedKits {
		if time.Since(v.at) > kitRepeatWindow {
			delete(b.servedKits, k)
		}
	}
	for i, g := range group {
		res := results[i]
		if res.Deferred {
			// Not taken into this visit: nothing was said to the donor, its place in the queue is as it was.
			b.kits[g.key] = g.q
			continue
		}
		b.servedKits[g.key] = servedKit{id: g.q.msg.KitID, at: time.Now()}
		b.load.NoteKitWait(time.Now(), time.Since(g.q.at))
		b.afterKit(ctx, d, g.q.msg, res, finalSent[i])
	}
	return true
}

// afterKit is everything the mule owes the record, the ledger and the donor once one kit's hand-out has ended.
func (b *muleBot) afterKit(ctx *Context, d *mulerunner.Deps, msg mulebus.Message, res mulerunner.KitHandoutResult, finalSent bool) {
	switch {
	case res.Stopped:
		return
	case res.Delivered:
		var crossed []orderplan.ItemCount
		for _, l := range res.Lines {
			if l.Qty > 0 {
				crossed = append(crossed, orderplan.ItemCount{Name: l.Item, Count: l.Qty})
			}
		}
		b.handoutLedger.RecordKit(time.Now(), msg.From, msg.KitID, crossed)
		b.noteHandoutTrade()
		if len(res.Took) > 0 {
			ctx.logf("handed a kit of %d line(s) to %q in one trade and took %d stack(s) in from it", len(crossed), msg.From, len(res.Took))
		} else {
			ctx.logf("handed a kit of %d line(s) to %q in one trade", len(crossed), msg.From)
		}
		ctx.Bus.Publish(mulebus.TradedMessage(ctx.Username, msg.From))
		recordKitHandout(ctx, msg.From, res)
	default:
		ctx.logf("could not hand %q its kit: %s", msg.From, res.Reason)
		// A failure after the plan went out (the donor never opened the trade, the screen
		// closed) still has to reach the donor, or it waits out its whole budget.
		if !finalSent {
			ctx.Bus.Publish(mulebus.KitAnswerMessage(ctx.Username, msg.From, msg.KitID, busKitLines(res.Lines), res.Reason, 0))
		}
		recordKitHandout(ctx, msg.From, res)
	}
	if dropped := b.watcher.DropRequestsFrom(d, msg.From); dropped > 0 {
		ctx.logf("wall bus: ignored %d leftover trade request line(s) from the kit hand-out with %q", dropped, msg.From)
	}
}

// servingKitOrder is the kit hand-outs in progress (queue key to kit id), empty between hand-outs.
type servingKitOrder map[string]string

// recordKitHandout writes one kit hand-out to the fleet's trade record, one Out item per line.
func recordKitHandout(ctx *Context, partner string, res mulerunner.KitHandoutResult) {
	if ctx.TradeLog == nil || res.Stopped {
		return
	}
	e := muleledger.Entry{Mule: ctx.Username, Partner: partner, Kind: muleledger.KindHandout, Outcome: muleledger.OutcomeFailed, Reason: res.Reason}
	if res.Delivered {
		e.Outcome, e.Reason = muleledger.OutcomeAccepted, ""
		for _, l := range res.Lines {
			if l.Qty > 0 {
				e.Out = append(e.Out, ledgerItem(ctx, -1, l.Item, int64(l.Qty)))
			}
		}
		for _, l := range res.Took {
			if l.Qty > 0 {
				e.In = append(e.In, ledgerItem(ctx, -1, l.Item, int64(l.Qty)))
			}
		}
	}
	ctx.TradeLog(e)
}
