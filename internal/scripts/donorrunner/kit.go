package donorrunner

// The donor's side of a BUNDLED KIT hand-out (owner, 2026-09-23): up to orderplan.KitLinesMax
// items asked for in one KindKitOrder and received in ONE trade. The mule's half is
// mulerunner.HandoutKit; the rules both sides share are orderplan's (DecideKit, DecideReceiveKit).
//
// The same contract as Await, extended to many lines. Bounded: AwaitKit returns within its own
// budget whether the mule answers, refuses or says nothing. The receipt is this pack's own gain
// per line, counting the noted forms, never a trade closing. And a mule that never answers a kit
// order at all -- an older build, or none listening -- comes back as Answered false within
// KitAnswerTimeout, so the caller can fall back to one Await per item, which is unchanged.
//
// Pack room is the caller's to make BEFORE asking: the kit is sized by the mule against the free
// slots reported here, and non-stackable lines it cannot fit unnoted arrive as bank notes, which
// the caller un-notes at its own bank (UnnoteKit). DepositForKit is the deposit-first helper.

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/mulerunner"
	"rsb0t-go/internal/scripts/orderplan"
	"rsb0t-go/internal/scripts/tile"
)

// KitAnswerTimeout is how long AwaitKit waits for the mule to answer a kit order at all (its
// "queued" acknowledgement comes from its next bus poll, within seconds) before reporting that
// this mule does not take kit orders.
const KitAnswerTimeout = 20 * time.Second

// KitPlanGrace is how long AwaitKit waits for the trade from the moment the mule's plan arrives,
// whatever is left of its own budget: the mule is drawing the kit by then.
const KitPlanGrace = 2 * time.Minute

// DefaultKitTimeout is a kit ask's whole budget. The mule holds a kit back while fishers are
// queued for up to two minutes (internal/bot/mule_kit.go kitMaxDefer), then needs a long bank
// visit and a trade.
const DefaultKitTimeout = 5 * time.Minute

// KitAsk is one kit order.
type KitAsk struct {
	Mule, Me string
	Lines    []orderplan.KitLine
	Bus      *mulebus.Bus
	Timeout  time.Duration
	// ID names the kit on the bus; empty generates one.
	ID string
	// Hold, when set, is called while the kit waits in the mule's line (answered and queued, no
	// plan yet, no trade open): a non-empty answer gives the wait up with that reason. Never called
	// once the mule has planned the kit or a trade is open, so a wait is never abandoned mid-draw.
	// A long wait (a world errand's) uses it to notice the mule leaving and to keep saying it is
	// waiting.
	Hold func() string
	// Republish, when positive, publishes the kit order again this often while it waits unplanned.
	// The mule keeps a queued kit's place on a re-publish and ignores one it is serving or has just
	// served, so this only puts back a kit the mule lost (its script restarted).
	Republish time.Duration
	// Give is what this account hands the mule in the kit's own trade (kitgive.go), already in the pack. It is
	// cut to what the pack holds, said on the order, and placed on this side of the kit's trade screen. Nil: the
	// kit gives nothing back, as before.
	Give []GiveLine
}

// KitAwaitResult is what came of it, judged per line by this pack's own gain.
type KitAwaitResult struct {
	// Answered is false when the mule never answered the kit order at all: fall back per item.
	Answered bool
	// Got is how many of each line's item arrived (unnoted and noted forms together), keyed by
	// the donor's own spelling of the line; Noted how many of those arrived as notes.
	Got   map[string]int
	Noted map[string]int
	// Declined is the mule's reason per line it did not hand over in full.
	Declined map[string]string
	Reason   string
	Stopped  bool
}

var kitSeq atomic.Int64

func newKitID(me string) string {
	return strings.ToLower(strings.TrimSpace(me)) + "-" + strconv.FormatInt(time.Now().UnixMilli()%1000000, 36) + strconv.FormatInt(kitSeq.Add(1), 36)
}

// freeSlots is this pack's free slots through the fixed component; readable false when unseen.
func freeSlots(d *Deps) (int, bool) {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return 0, false
	}
	return actions.BackpackCapacity - len(inv.NonEmpty()), true
}

// AwaitKit asks the mule for the whole kit in one KindKitOrder and receives it in one trade.
func AwaitKit(d *Deps, o KitAsk) KitAwaitResult {
	res := KitAwaitResult{Got: map[string]int{}, Noted: map[string]int{}, Declined: map[string]string{}}
	if o.Bus == nil {
		res.Reason = "no wall bus configured"
		return res
	}
	lines, bad := orderplan.NormaliseKit(o.Lines)
	if bad != "" || o.Mule == "" || o.Me == "" {
		if bad == "" {
			bad = "nothing to ask for"
		}
		res.Reason = bad
		return res
	}
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = DefaultKitTimeout
	}
	id := o.ID
	if id == "" {
		id = newKitID(o.Me)
	}
	// Every line's receipt ids and its baseline, before anything can arrive.
	type track struct {
		ids, noted []int32
		before     int
		noteBefore int
	}
	tracks := make([]track, len(lines))
	for i, l := range lines {
		ids := receiptIDs(d.Config, l.Item)
		var noted []int32
		for _, rid := range ids {
			if obj, ok := d.Config.Obj(int(rid)); ok && obj.Noted() {
				noted = append(noted, rid)
			}
		}
		tracks[i] = track{ids: ids, noted: noted, before: heldOf(d, ids), noteBefore: heldOf(d, noted)}
	}
	free, readable := freeSlots(d)
	give, giveLines, giveSlots := giveBus(d.Config, GiveHeld(packItems(d), o.Give))
	if readable && len(give) > 0 {
		// The hand-over leaves the pack as it is offered, before the kit arrives: its emptied slots are room.
		free += GiveFreed(packItems(d), give)
	}
	if readable && free == 0 {
		// A full pack reported as 0 free slots reads to the mule as "not reported" and it
		// would plan a whole trade screen; nothing can arrive, so ask nothing.
		d.logf("donor: not asking %q for a kit: no room in my pack", o.Mule)
		res.Reason = noRoomReason
		return res
	}
	if !readable {
		free = 0 // unknown: the mule plans for a trade screen
	}
	busLines := make([]mulebus.KitLine, 0, len(lines))
	for _, l := range lines {
		busLines = append(busLines, mulebus.KitLine{Item: l.Item, Qty: l.Qty, Noted: l.Noted})
	}
	start := time.Now()
	deadline := start.Add(timeout)
	watcher := mulebus.NewWatcher()
	publish := func() {
		if here, ok := currentTile(d); ok {
			o.Bus.Publish(mulebus.HereMessage(o.Me, tile.Tile{X: here.X, Z: here.Z, Level: here.Level}))
		}
		left := time.Until(deadline)
		o.Bus.Publish(mulebus.OnWorld(mulebus.WithGive(mulebus.KitOrderMessage(o.Me, o.Mule, id, busLines, free, left), giveLines, giveSlots), d.MyWorld))
	}
	// However this ends -- received, refused, timed out or stopped -- the mule is told to drop
	// the kit, so it never serves one nobody is waiting for. Harmless after a delivery: the mule
	// keeps only queued kits, and one just served is no longer queued.
	defer o.Bus.Publish(mulebus.KitCancelMessage(o.Me, o.Mule, id))
	publish()
	d.logf("donor: asked %q over the wall bus for a kit of %d line(s) (%d free slot(s)): %s", o.Mule, len(lines), free, describeKit(lines))
	if len(give) > 0 {
		d.logf("donor: handing %q %d stack(s) back in the kit's own trade (%d of its pack slots)", o.Mule, len(give), giveSlots)
	}

	settle := func() KitAwaitResult {
		for i, l := range lines {
			got := heldOf(d, tracks[i].ids) - tracks[i].before
			if got > 0 {
				res.Got[l.Item] = got
				if n := heldOf(d, tracks[i].noted) - tracks[i].noteBefore; n > 0 {
					res.Noted[l.Item] = n
				}
			}
			if got < l.Qty {
				if _, has := res.Declined[l.Item]; !has {
					res.Declined[l.Item] = "not handed over"
				}
			} else {
				delete(res.Declined, l.Item)
			}
		}
		return res
	}

	trade := actions.NewTrade(d.Session)
	want := lines // until the mule's plan arrives
	askedItems := make([]string, 0, len(lines))
	for _, l := range lines {
		askedItems = append(askedItems, l.Item)
	}
	names := newAskedNames(d.Config, askedItems...)
	planned := false
	nextPublish := start.Add(AskRepeatEvery)
	nextRepublish := start.Add(o.Republish)
	chatCursor := 0
	if d.Chat != nil {
		chatCursor = d.Chat.Total()
	}
	for time.Now().Before(deadline) {
		for _, msg := range watcher.Poll(o.Bus) {
			if msg.Kind != mulebus.KindKitAnswer || !mulebus.NamesMatch(msg.From, o.Mule) || !mulebus.AddressedTo(msg, o.Me) || msg.KitID != id {
				continue
			}
			res.Answered = true
			if mulebus.IsKitQueued(msg) {
				d.logf("donor: %q queued the kit (about %s)", o.Mule, (time.Duration(msg.EtaMs) * time.Millisecond).Round(time.Second))
				continue
			}
			var plan []orderplan.KitLine
			for _, l := range msg.Kit {
				if l.Qty > 0 {
					plan = append(plan, orderplan.KitLine{Item: l.Item, Qty: l.Qty})
				}
				if l.Reason != "" {
					res.Declined[donorSpelling(lines, l.Item)] = l.Reason
				}
			}
			if msg.Reason != "" || len(plan) == 0 {
				reason := msg.Reason
				if reason == "" {
					reason = "nothing in that kit can be handed out now"
				}
				d.logf("donor: %q declined the kit: %s", o.Mule, reason)
				res.Reason = reason
				return settle()
			}
			want, planned = plan, true
			// The mule is now drawing this kit for us: stay for the trade. An ask whose budget
			// ran out mid-draw left the mule holding a drawn kit and a trade nobody answered
			// (revK, S2). The mule itself will not start a kit with less than two minutes left.
			if until := time.Now().Add(KitPlanGrace); until.After(deadline) {
				deadline = until
			}
			d.logf("donor: %q is handing over %d line(s) of the kit", o.Mule, len(plan))
		}
		if !res.Answered && time.Since(start) > KitAnswerTimeout {
			res.Reason = "the mule did not answer the kit order"
			return res
		}

		if trade.Active() {
			r := receiveKit(d, trade, o.Mule, want, names, planned, give, func() bool {
				return kitArrived(d, lines, want, func(item string) (int, int) {
					for i, l := range lines {
						if strings.EqualFold(l.Item, item) {
							return heldOf(d, tracks[i].ids), tracks[i].before
						}
					}
					return 0, 0
				})
			})
			if r.stopped {
				res.Stopped = true
				return settle()
			}
			if r.received {
				d.logf("donor: received the kit from %q in one trade", o.Mule)
				return settle()
			}
			if r.reason != "" {
				d.logf("donor: the kit trade with %q ended without the kit: %s", o.Mule, r.reason)
				if r.final {
					res.Reason = r.reason
					return settle()
				}
			}
			continue
		}

		if d.Chat != nil {
			chatLines, next := d.Chat.ReadAfter(chatCursor, time.Time{})
			chatCursor = next
			for _, line := range chatLines {
				name, isReq := mulerunner.TradeRequestFrom(line)
				if !isReq || !orderplan.NamesMatch(name, o.Mule) {
					continue
				}
				if slot, found := findMuleSlot(d, o.Mule); found {
					d.logf("donor: %q is opening the kit trade — answering", o.Mule)
					if err := trade.Request(slot); err != nil {
						d.logf("donor: answering %q's trade request: %v", o.Mule, err)
					}
				}
			}
		}
		// Re-published until the mule has answered at all, for a mule that started late; and, with
		// Republish, every Republish while it waits in line unplanned.
		if !res.Answered && time.Now().After(nextPublish) {
			publish()
			nextPublish = time.Now().Add(AskRepeatEvery)
			if o.Republish > 0 {
				nextRepublish = time.Now().Add(o.Republish)
			}
		}
		if res.Answered && !planned {
			if o.Republish > 0 && time.Now().After(nextRepublish) {
				publish()
				nextRepublish = time.Now().Add(o.Republish)
			}
			if o.Hold != nil {
				if why := o.Hold(); why != "" {
					d.logf("donor: giving up the wait for %q's kit: %s", o.Mule, why)
					res.Reason = why
					return settle()
				}
			}
		}
		if !d.Delay(400 * time.Millisecond) {
			res.Stopped = true
			return settle()
		}
	}
	res.Reason = "the mule never handed the kit over"
	return settle()
}

// donorSpelling maps the mule's canonical item name back to the line the donor asked for.
func donorSpelling(lines []orderplan.KitLine, item string) string {
	for _, l := range lines {
		if strings.EqualFold(strings.TrimSpace(l.Item), strings.TrimSpace(item)) {
			return l.Item
		}
	}
	return item
}

// kitArrived reports whether every planned line has arrived in the pack in full.
func kitArrived(d *Deps, lines, want []orderplan.KitLine, held func(item string) (now, before int)) bool {
	for _, w := range want {
		now, before := held(donorSpelling(lines, w.Item))
		if now-before < w.Qty {
			return false
		}
	}
	return true
}

type kitReceipt struct {
	received bool
	stopped  bool
	final    bool
	reason   string
}

// receiveKit drives an open kit trade screen through orderplan.DecideReceiveKit until it closes,
// then waits for arrived() -- the whole planned kit in the pack.
//
// give, when set, is this account's hand-over riding the kit (kitgive.go): placed on this side of the offer
// screen once, and judged against what then shows there.
func receiveKit(d *Deps, trade *actions.Trade, mule string, want []orderplan.KitLine, names askedNames, planned bool, give []GiveLine, arrived func() bool) kitReceipt {
	deadline := time.Now().Add(90 * time.Second)
	chatFrom := 0
	if d.Chat != nil {
		chatFrom = d.Chat.Total()
	}
	accepted := false
	lastScreen := orderplan.ScreenNone
	lastOffer := ""
	placed := len(give) == 0
	var giveView []orderplan.ItemCount
	for {
		if !trade.Active() {
			closed, stopped := stillClosed(d, trade)
			if stopped {
				return kitReceipt{stopped: true}
			}
			if !closed {
				continue
			}
			break
		}
		if randomevents.Busy(d.Session) {
			_ = trade.Decline(3 * time.Second)
			return kitReceipt{reason: "a random event interrupted the hand-out"}
		}
		if time.Now().After(deadline) {
			_ = trade.Decline(3 * time.Second)
			return kitReceipt{reason: "the kit trade timed out"}
		}
		screen := orderplan.ScreenOffer
		if trade.OnConfirmScreen() {
			screen = orderplan.ScreenConfirm
		}
		if !placed && screen == orderplan.ScreenOffer && trade.OnOfferScreen() {
			placed = true
			out := trade.OfferMany(giveTradeLines(packItems(d), give), waiter{d})
			d.logf("donor: kit hand-over: %s", out.Log())
			giveView = handoutRows(d, giveSlotItems(give), names)
			if !out.OK() {
				// Judge the screen by what did go on it: a line that never showed is simply not handed over.
				giveView = handoutRows(d, trade.MyOffer(), names)
				d.logf("donor: kit hand-over: only part of it went on the screen (%d of %d line(s))", len(giveView), len(give))
			}
		}
		theirs, mine := handoutRows(d, trade.TheirOffer(), names), handoutRows(d, trade.MyOffer(), names)
		if screen == orderplan.ScreenConfirm {
			m, t := trade.ConfirmOffers()
			mine, theirs = handoutRows(d, m, names), handoutRows(d, t, names)
		}
		// The server clears both acceptances when a screen changes AND when an offer changes;
		// the mule places a kit one line at a time, so track both.
		sig := fmt.Sprint(screen, theirs, mine)
		if screen != lastScreen || sig != lastOffer {
			accepted = false
			lastScreen, lastOffer = screen, sig
		}
		action, reason := orderplan.DecideReceiveKit(orderplan.KitReceiveView{
			Screen: screen, Partner: trade.Partner(), Mule: mule, Want: want, Planned: planned,
			Theirs: theirs, Mine: mine, Accepted: accepted, Give: giveView,
		})
		switch action {
		case orderplan.ReceiveDecline:
			d.logf("donor: declining the kit screen with %q: %s", trade.Partner(), reason)
			_ = trade.Decline(3 * time.Second)
			return kitReceipt{reason: reason}
		case orderplan.ReceiveAccept, orderplan.ReceiveConfirm:
			if err := trade.Accept(); err != nil {
				d.logf("donor: accepting the kit %v: %v", action, err)
			} else {
				accepted = true
			}
		}
		if !d.Delay(300 * time.Millisecond) {
			return kitReceipt{stopped: true}
		}
	}
	if iLackRoom(d, chatFrom) {
		return kitReceipt{reason: noRoomReason, final: true}
	}
	deadlineHeld := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadlineHeld) {
		if arrived() {
			return kitReceipt{received: true}
		}
		if !d.Delay(250 * time.Millisecond) {
			return kitReceipt{stopped: true}
		}
	}
	// Some of it may have come; the caller's per-line receipt says what.
	return kitReceipt{reason: "the trade closed without the whole kit arriving", final: true}
}

func describeKit(lines []orderplan.KitLine) string {
	parts := make([]string, 0, len(lines))
	for _, l := range lines {
		parts = append(parts, strconv.Itoa(l.Qty)+" "+l.Item)
	}
	return strings.Join(parts, ", ")
}

// DepositForKit banks everything in the pack whose name is not in keep, through a bank the
// caller has open, so a kit ask reports -- and the mule can fill -- as many free slots as this
// account can spare. Returns the free slots afterwards (-1: the pack could not be read).
func DepositForKit(d *Deps, bank *actions.Bank, keep []string) int {
	keepSet := map[string]bool{}
	for _, k := range keep {
		keepSet[strings.ToLower(strings.TrimSpace(k))] = true
	}
	// A qualified name in keep ("Yew shortbow id857") keeps that obj and its note, and no other obj of
	// the same display name.
	keepIDs := map[int]bool{}
	for k := range keepSet {
		if _, _, qualified := cachecfg.SplitQualified(k); qualified && d.Config != nil {
			for _, id := range d.Config.ObjIDs(k) {
				keepIDs[id] = true
			}
		}
	}
	_ = bank.BatchDepositAllMatching(func(it gamestate.SlotItem) bool {
		if keepIDs[int(it.ID)] {
			return false
		}
		if obj, ok := d.Config.Obj(int(it.ID)); ok {
			return !keepSet[strings.ToLower(strings.TrimSpace(obj.Name))]
		}
		return false
	}, waiter{d})
	if f, ok := freeSlots(d); ok {
		return f
	}
	return -1
}

// UnnoteResult is what UnnoteKit did, per item.
type UnnoteResult struct {
	// Unnoted is how many were drawn back into the pack as the item.
	Unnoted map[string]int
	// Banked is how many of the notes stayed in the bank because the pack had no room: they are
	// NOT in the pack, and a caller counting its kit must not count them.
	Banked map[string]int
}

// UnnoteKit turns the bank notes a kit arrived as back into the items, through a bank the caller
// has open: every note of items is deposited (a bank stores the item), then the item is drawn
// back as far as the pack has room. What did not fit stays banked and is reported as such.
func UnnoteKit(d *Deps, bank *actions.Bank, items []string) UnnoteResult {
	res := UnnoteResult{Unnoted: map[string]int{}, Banked: map[string]int{}}
	out := res.Unnoted
	for _, item := range items {
		var noteIDs []int32
		for _, id := range receiptIDs(d.Config, item) {
			if obj, ok := d.Config.Obj(int(id)); ok && obj.Noted() {
				noteIDs = append(noteIDs, id)
			}
		}
		notes := 0
		for _, nid := range noteIDs {
			n := countOfID(bank.Inventory(), nid)
			if n <= 0 {
				continue
			}
			if err := bank.DepositAll(nid); err != nil {
				d.logf("donor: depositing %s notes: %v", item, err)
				continue
			}
			nid := nid
			waitForBankView(d, bank, func() bool { return countOfID(bank.Inventory(), nid) == 0 })
			notes += n
		}
		if notes == 0 {
			continue
		}
		// The item to draw back is the one the bank holds now the notes are in it: a name several objects share ("Bones") is
		// not the first of them, and drawing that one back found none and banked the whole kit.
		ids := unnotedIDs(d.Config, item)
		banked := func(id int32) bool { return countOfID(bank.Items(), id) > 0 }
		waitForBankView(d, bank, func() bool {
			for _, id := range ids {
				if banked(id) {
					return true
				}
			}
			return false
		})
		id, ok := pickBankHeldID(ids, banked)
		if !ok {
			continue
		}
		room := actions.BackpackCapacity - len(bank.Inventory())
		take := notes
		if obj, ok := d.Config.Obj(int(id)); !ok || !obj.Stackable {
			if take > room {
				take = room
			}
		}
		if take <= 0 {
			res.Banked[item] = notes
			d.logf("donor: no room to un-note any of %d %s — they stay in the bank", notes, item)
			continue
		}
		before := countOfID(bank.Inventory(), id)
		if err := bank.QuickWithdrawX(id, int32(take), waiter{d}); err != nil {
			d.logf("donor: drawing %d %s back unnoted: %v", take, item, err)
			res.Banked[item] = notes
			continue
		}
		waitForBankView(d, bank, func() bool { return countOfID(bank.Inventory(), id) >= before+take })
		out[item] = countOfID(bank.Inventory(), id) - before
		if left := notes - out[item]; left > 0 {
			res.Banked[item] = left
		}
		d.logf("donor: un-noted %d of %d %s at the bank (%d left banked)", out[item], notes, item, res.Banked[item])
	}
	return res
}

// pickBankHeldID is the un-noted object of a name that the bank holds: of ids (the name's un-noted objects, in id order)
// the first the bank has, else the first of them. A name several objects share ("Bones" is 280-283 before the 526 every bank
// holds) is the one actually banked once the notes are deposited, as the mule picks the object it draws (mulerunner.pickBankedID).
func pickBankHeldID(ids []int32, banked func(id int32) bool) (int32, bool) {
	for _, id := range ids {
		if banked(id) {
			return id, true
		}
	}
	if len(ids) > 0 {
		return ids[0], true
	}
	return 0, false
}

func waitForBankView(d *Deps, bank *actions.Bank, cond func() bool) bool {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		if !d.Delay(100 * time.Millisecond) {
			return false
		}
	}
	return cond()
}

// waiter adapts Deps' Delay to actions.Waiter.
type waiter struct{ d *Deps }

func (w waiter) Delay(t time.Duration) bool { return w.d.Delay(t) }
func (w waiter) DelayUntil(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		if !w.d.Delay(100 * time.Millisecond) {
			return false
		}
	}
}
func (w waiter) DelayTicks(n int) bool { return w.d.Delay(time.Duration(n) * 600 * time.Millisecond) }
