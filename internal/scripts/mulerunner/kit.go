package mulerunner

// The mule's side of a BUNDLED KIT hand-out: up to orderplan.KitLinesMax items, drawn from the
// bank in one visit and handed over in ONE trade (owner, 2026-09-23: one mule for the whole
// fleet, and a Scarab kit ask is one trade of at most 28 unstacked items).
//
// Everything is drawn BEFORE the trade opens, as Handout does: a withdrawal mid-trade would mean
// a bank interface over a trade screen, and a mule walking to its booth cancels the trade it just
// opened. Every line crosses either as the item or, when the kit would otherwise need more slots
// than one trade screen or the donor's pack has, as bank notes, one slot a line. What is served,
// how many and which lines are noted is orderplan.DecideKit's; this file only carries it out.
//
// deliverKit is deliver's shape with several offers instead of one. It is a separate function on
// purpose: deliver serves every live single-item hand-out today, and threading a list through it
// would change the one path the fishing fleet already depends on.

import (
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/orderplan"
)

// KitRequest is one kit order off the bus.
type KitRequest struct {
	// To is the donor, ID the kit id it gave.
	To string
	ID string
	// Lines is the kit as asked; Room the donor's own free pack slots (0: not reported).
	Lines []orderplan.KitLine
	Room  int
	// Policy and Ledger are the same safety gate a single order passes; see orderplan.DecideKit.
	Policy orderplan.Policy
	Ledger *orderplan.Ledger
	// Announce, when set, is called once the kit is decided and drawn, BEFORE the trade opens,
	// with what is coming (or why nothing is): the donor judges the screen against it.
	Announce func(lines []orderplan.KitLineDecision, reason string)
	// Give is what the donor said it will hand this mule in the SAME trade (mulebus.Message.Give), GiveSlots the
	// pack slots that takes. The mule offers exactly the kit and accepts a donor side that is that hand-over and
	// nothing else (orderplan.JudgeGive); empty: a kit that takes nothing in, as before.
	Give      []orderplan.KitLine
	GiveSlots int
}

// KitHandoutResult is what happened. Delivered is true only once the items are confirmed gone
// from this account's pack after the trade closed; Lines then carries what crossed.
type KitHandoutResult struct {
	Delivered bool
	Lines     []orderplan.KitLineDecision
	Reason    string
	Declined  bool
	Stopped   bool
	// Took is what the donor handed the mule in the kit's own trade, read off the confirm screen both sides
	// accepted (empty for a kit with no hand-over).
	Took []orderplan.KitLine
	// Deferred is set by HandoutKits for a kit it did not take into its bank visit (the pack was full, a random
	// event, no bank): nothing was said to the donor, and the caller queues it again.
	Deferred bool
}

// kitOffer is one line as it sits in the mule's pack: the id it arrived as, and how many.
type kitOffer struct {
	id  int32
	qty int
}

// kitWholeReason is the one reason a kit that serves nothing gives: the reason every line shares,
// or a general one when they differ.
func kitWholeReason(lines []orderplan.KitLineDecision) string {
	reason := ""
	for _, l := range lines {
		if l.Reason == "" {
			continue
		}
		if reason == "" {
			reason = l.Reason
		} else if reason != l.Reason {
			return "nothing in that kit can be handed out now"
		}
	}
	if reason == "" {
		reason = "nothing in that kit can be handed out now"
	}
	return reason
}

// HandoutKit fulfils one kit order end to end. Nothing is published here; Announce and the
// result carry every answer to the caller, which owns the bus.
func HandoutKit(d *Deps, req KitRequest) KitHandoutResult {
	announce := func(lines []orderplan.KitLineDecision, reason string) {
		if req.Announce != nil {
			req.Announce(lines, reason)
		}
	}
	refuse := func(lines []orderplan.KitLineDecision, reason string) KitHandoutResult {
		announce(lines, reason)
		return KitHandoutResult{Lines: lines, Reason: reason, Declined: true}
	}
	if randomevents.Busy(d.Session) {
		return KitHandoutResult{Reason: "busy with a random event"}
	}
	slot, ok := ResolvePlayerSlot(d, req.To)
	if !ok {
		return refuse(nil, orderplan.DeclineNotInSight)
	}
	// THE BLIND DRAW (4 Oct): the kit is decided from the bank as this mule last read it and drawn
	// behind the booth click (the pack banked, every line, note mode on and off), in the pack the
	// ticks after the bank opens. The bank as it really opened is the judge: a draw that is not
	// exactly the kit goes back, and the kit is decided again from the fresh bank as before.
	blind := planBlindKit(d, req)
	var blindOps []actions.BankOp
	if blind != nil {
		blindOps = blind.ops
	}
	bank, open, opened := openNearestBankBoothWith(d, blindOps)
	if !opened {
		return KitHandoutResult{Reason: "could not open a bank"}
	}
	d.bankRead(bank.Items())
	if blind != nil {
		exact := open.Batch.Err == nil && !bank.NoteMode()
		// A line that came out short (the bank held less than this mule last read, 18 of 100 kits on 1 Nov 2026) is
		// handed out as drawn rather than banked and drawn again: the whole pack back in and every line out again cost
		// a second bank pass for a few items. Only a line that came out over (the pack held some before) or not at
		// all sends the draw back.
		got := make([]orderplan.KitLineDecision, len(blind.lines))
		served := 0
		for i, l := range blind.lines {
			got[i] = l
			if l.Qty <= 0 {
				continue
			}
			n := countByID(bank.Inventory(), blind.offers[i].id)
			if n <= 0 || n > l.Qty {
				exact = false
				break
			}
			got[i].Qty = n
			served++
		}
		if exact && served > 0 {
			_ = bank.Close()
			return finishKit(d, req, slot, got, blind.offers, announce)
		}
		if errors.Is(open.Batch.Err, actions.ErrBankStopped) {
			_ = bank.Close()
			return KitHandoutResult{Stopped: true}
		}
		d.logf("hand-out: the blind kit draw for %q did not come out exactly (%s) — banking it and deciding again from the bank as it opened", req.To, open)
		bankPackAtOpenBank(d, bank)
		if bank.NoteMode() {
			_ = bank.SetNoteMode(false, handoutWaiter{d})
		}
		d.bankRead(bank.Items())
	}
	stock := func(item string) int {
		id, found := bankedID(d.Config, bank.Items(), item)
		if !found {
			return 0
		}
		return countByID(bank.Items(), id)
	}
	facts := func(item string) (orderplan.KitFacts, bool) {
		id, found := bankedID(d.Config, bank.Items(), item)
		if !found {
			return orderplan.KitFacts{}, false
		}
		f := orderplan.KitFacts{}
		if obj, ok := d.Config.Obj(int(id)); ok {
			f.Stackable = obj.Stackable
		}
		_, f.HasNote = noteFor(d.Config, id)
		return f, true
	}
	dec := orderplan.DecideKit(time.Now(), req.To, req.Lines, req.Policy, req.Ledger, stock, facts, req.Room)
	if dec.Serving() == 0 {
		_ = bank.Close()
		return refuse(dec.Lines, kitWholeReason(dec.Lines))
	}

	lines, offers, stopped := drawKit(d, bank, req.To, dec.Lines)
	if stopped {
		_ = bank.Close()
		return KitHandoutResult{Stopped: true}
	}
	_ = bank.Close()
	return finishKit(d, req, slot, lines, offers, announce)
}

// finishKit is a hand-out kit's tail once the bank is closed: every line that came out is checked
// in the pack, announced, and delivered in one trade.
func finishKit(d *Deps, req KitRequest, slot int32, lines []orderplan.KitLineDecision, offers []kitOffer, announce func([]orderplan.KitLineDecision, string)) KitHandoutResult {
	var live []kitOffer
	for i, l := range lines {
		if l.Qty > 0 {
			live = append(live, offers[i])
		}
	}
	if len(live) == 0 {
		announce(lines, "could not draw it out of the bank")
		return KitHandoutResult{Lines: lines, Reason: "could not draw it out of the bank"}
	}
	if !waitFor(d, "the withdrawn kit to reach my pack", 5*time.Second, func() bool {
		for _, o := range live {
			if packCountOf(d, o.id) < o.qty {
				return false
			}
		}
		return true
	}) {
		announce(lines, "the withdrawal never reached my pack")
		return KitHandoutResult{Lines: lines, Reason: "the withdrawal never reached my pack"}
	}
	d.logf("hand-out: %q's kit drawn — %s", req.To, describeKitLines(lines))
	announce(lines, "")

	res := deliverKitWith(d, req.To, slot, live, req.Give, req.GiveSlots)
	res.Lines = lines
	return res
}

// blindKit is a kit decided before the bank opens: the lines, what each arrives as, and the ops
// that ride behind the booth click.
type blindKit struct {
	lines  []orderplan.KitLineDecision
	offers []kitOffer
	ops    []actions.BankOp
}

// planBlindKit decides req from the bank as this mule last read it and plans drawKit's burst: the
// whole pack banked, every unnoted line, note mode on, every noted line, note mode off. nil when
// there is no earlier reading or nothing would be served from it.
func planBlindKit(d *Deps, req KitRequest) *blindKit {
	last := d.Session.State().Inventory(actions.BankWithdraw)
	if last == nil || d.Config == nil {
		return nil
	}
	rows := last.NonEmpty()
	stock := func(item string) int {
		id, found := bankedID(d.Config, rows, item)
		if !found {
			return 0
		}
		return countByID(rows, id)
	}
	facts := func(item string) (orderplan.KitFacts, bool) {
		id, found := bankedID(d.Config, rows, item)
		if !found {
			return orderplan.KitFacts{}, false
		}
		f := orderplan.KitFacts{}
		if obj, ok := d.Config.Obj(int(id)); ok {
			f.Stackable = obj.Stackable
		}
		_, f.HasNote = noteFor(d.Config, id)
		return f, true
	}
	dec := orderplan.DecideKit(time.Now(), req.To, req.Lines, req.Policy, req.Ledger, stock, facts, req.Room)
	if dec.Serving() == 0 {
		return nil
	}
	b := &blindKit{lines: append([]orderplan.KitLineDecision(nil), dec.Lines...), offers: make([]kitOffer, len(dec.Lines))}
	seenCarry := map[int32]bool{}
	var unnoted, noted []actions.BankOp
	for i, l := range b.lines {
		if l.Qty <= 0 {
			continue
		}
		id, found := bankedID(d.Config, rows, l.Item)
		if !found {
			return nil
		}
		carry := id
		if l.Noted {
			nid, ok := noteFor(d.Config, id)
			if !ok {
				return nil
			}
			carry = nid
		}
		if seenCarry[carry] {
			return nil // two lines arriving as one object cannot be told apart in the pack
		}
		seenCarry[carry] = true
		b.offers[i] = kitOffer{id: carry, qty: l.Qty}
		op := actions.WithdrawAsOp(id, int32(l.Qty), carry)
		if l.Noted {
			noted = append(noted, op)
		} else {
			unnoted = append(unnoted, op)
		}
	}
	seen := map[int32]bool{}
	for _, it := range packItems(d) {
		if !seen[it.ID] {
			seen[it.ID] = true
			b.ops = append(b.ops, actions.DepositAllOp(it.ID))
		}
	}
	b.ops = append(b.ops, unnoted...)
	if len(noted) > 0 {
		b.ops = append(b.ops, actions.NoteModeOp(true))
		b.ops = append(b.ops, noted...)
		b.ops = append(b.ops, actions.NoteModeOp(false))
	}
	return b
}

// drawKit empties the pack through the open bank and draws every served line of lines: the
// unnoted lines first, then the noted ones in one note-mode pass, the bank left back on items.
// A line that draws short is cut to what arrived, with a reason. offers[i] is what line i sits in
// the pack as (zero for a line not drawn).
//
// THE FAST DRAW (4 Oct): the pack's Deposit-Alls, every unnoted line, note mode on, every noted
// line and note mode off all go out in one actions.Bank.Batch, five user events a tick, and are
// confirmed once against the open bank's side panel. Each line it falls short of is drawn the
// old way (withdrawExactly, one round trip per click), for what is still missing.
func drawKit(d *Deps, bank *lockedBank, to string, lines []orderplan.KitLineDecision) (out []orderplan.KitLineDecision, offers []kitOffer, stopped bool) {
	lines = append([]orderplan.KitLineDecision(nil), lines...)
	offers = make([]kitOffer, len(lines))
	type linePlan struct {
		id, carry int32
		ok        bool
	}
	plans := make([]linePlan, len(lines))
	resolve := func(i int) {
		l := &lines[i]
		if l.Qty <= 0 {
			return
		}
		id, found := bankedID(d.Config, bank.Items(), l.Item)
		if !found {
			l.Qty, l.Reason = 0, "I cannot identify that item"
			return
		}
		carry := id
		if l.Noted {
			nid, ok := noteFor(d.Config, id)
			if !ok {
				l.Qty, l.Reason = 0, orderplan.DeclineKitNoNote
				return
			}
			carry = nid
		}
		plans[i] = linePlan{id: id, carry: carry, ok: true}
	}
	for i := range lines {
		resolve(i)
	}
	// The pack must hold nothing but the kit: every offer below is Offer-All of what the pack
	// holds, and the kit alone can need all 28 slots.
	var ops []actions.BankOp
	seen := map[int32]bool{}
	for _, it := range bank.Inventory() {
		if !seen[it.ID] {
			seen[it.ID] = true
			ops = append(ops, actions.DepositAllOp(it.ID))
		}
	}
	banking := len(ops)
	anyNoted := false
	for i, p := range plans {
		if p.ok && !lines[i].Noted {
			ops = append(ops, actions.WithdrawAsOp(p.id, int32(lines[i].Qty), p.carry))
		}
		anyNoted = anyNoted || (p.ok && lines[i].Noted)
	}
	if anyNoted {
		ops = append(ops, actions.NoteModeOp(true))
		for i, p := range plans {
			if p.ok && lines[i].Noted {
				ops = append(ops, actions.WithdrawAsOp(p.id, int32(lines[i].Qty), p.carry))
			}
		}
		ops = append(ops, actions.NoteModeOp(false))
	}
	fast := bank.Batch(ops, handoutWaiter{d}, actions.BatchOptions{})
	if errors.Is(fast.Err, actions.ErrBankStopped) {
		return lines, offers, true
	}
	if !fast.OK() {
		d.logf("hand-out: %s — the rest the old way", fast)
	}
	if fast.Err != nil {
		// The burst could not run: the whole draw the old way.
		if n := bankPackAtOpenBank(d, bank); n > 0 {
			d.logf("hand-out: banked %d stack(s) the pack was holding before drawing %q's kit", n, to)
		}
	} else {
		if banking > 0 {
			d.logf("hand-out: banked %d stack(s) the pack was holding before drawing %q's kit", banking, to)
		}
		for _, sh := range fast.Short {
			if sh.Op.Kind == actions.BankOpDeposit {
				depositOneSlow(d, bank, sh.Op.ID)
			}
		}
	}
	// got is what the burst drew for line i: the pack was emptied first, so all of its carry.
	got := func(i int) int {
		if fast.Err != nil || !plans[i].ok {
			return 0
		}
		return min(countByID(fast.Pack, plans[i].carry), lines[i].Qty)
	}

	drawLine := func(i int, noted bool) bool {
		l := &lines[i]
		if l.Qty <= 0 || l.Noted != noted || !plans[i].ok {
			return true
		}
		id, carry := plans[i].id, plans[i].carry
		drawn := got(i)
		if drawn < l.Qty && (fast.Err != nil || countByID(bank.Items(), id) > 0) { // nothing to fall back on once the row is empty
			more, stopped := withdrawExactly(d, bank, id, carry, l.Qty-drawn)
			if stopped {
				return false
			}
			drawn += more
		}
		if drawn < l.Qty {
			if drawn <= 0 {
				l.Reason = "could not draw it out of the bank"
			} else {
				l.Reason = "could only draw " + itoa(drawn)
			}
			l.Qty = drawn
		}
		offers[i] = kitOffer{id: carry, qty: l.Qty}
		return true
	}
	for i := range lines {
		if !drawLine(i, false) {
			return lines, offers, true
		}
	}
	notedShort := false
	for i, l := range lines {
		notedShort = notedShort || (l.Noted && l.Qty > 0 && plans[i].ok && got(i) < l.Qty)
	}
	if !notedShort {
		// Every noted line came out of the burst (or there were none): record them, nothing to
		// switch.
		for i := range lines {
			if !drawLine(i, true) {
				return lines, offers, true
			}
		}
	} else {
		if err := bank.SetNoteMode(true, handoutWaiter{d}); err != nil {
			for i := range lines {
				if lines[i].Noted && lines[i].Qty > 0 {
					if g := got(i); g > 0 { // the burst drew some: hand those over
						lines[i].Qty, lines[i].Reason = g, "could only draw "+itoa(g)
						offers[i] = kitOffer{id: plans[i].carry, qty: g}
						continue
					}
					lines[i].Qty, lines[i].Reason = 0, "could not switch the bank to notes"
				}
			}
		} else {
			for i := range lines {
				if !drawLine(i, true) {
					_ = bank.SetNoteMode(false, handoutWaiter{d})
					return lines, offers, true
				}
			}
			if err := bank.SetNoteMode(false, handoutWaiter{d}); err != nil {
				d.logf("hand-out: could not switch the bank back off notes: %v", err)
			}
		}
	}
	return lines, offers, false
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// describeKitLines is the one-line log form of a decided kit.
func describeKitLines(lines []orderplan.KitLineDecision) string {
	parts := make([]string, 0, len(lines))
	for _, l := range lines {
		switch {
		case l.Qty <= 0:
			parts = append(parts, l.Item+" (not coming: "+l.Reason+")")
		case l.Noted:
			parts = append(parts, itoa(l.Qty)+" "+l.Item+" (noted)")
		default:
			parts = append(parts, itoa(l.Qty)+" "+l.Item)
		}
	}
	return strings.Join(parts, ", ")
}

// deliverKit runs the kit's one trade, with the donor already resolved and every line already
// held. See deliver for the reasoning behind each step; the differences are that several lines
// are offered together, in one burst and read once (actions.Trade.OfferMany), and that the confirm
// screen and the receipt check every line.
func deliverKit(d *Deps, to string, slot int32, offers []kitOffer) KitHandoutResult {
	return deliverKitWith(d, to, slot, offers, nil, 0)
}

// kitGiveWait is how long the mule waits, on the offer screen, for the donor to put its agreed hand-over on its
// side: the donor places it in one burst the moment the screen opens, so this is long only for a lagging client.
const kitGiveWait = 15 * time.Second

// deliverKitWith is deliverKit for a kit that may take a hand-over in: give is what the donor agreed to put on its
// side (giveSlots the pack slots that takes here). The rule that keeps it safe is the same on every read of the
// screen: this side holds exactly the kit, and the donor's side holds the agreed hand-over or part of it and
// nothing else (orderplan.JudgeGive); anything more and the trade is declined.
func deliverKitWith(d *Deps, to string, slot int32, offers []kitOffer, give []orderplan.KitLine, giveSlots int) KitHandoutResult {
	trade := actions.NewTrade(d.Session)
	chatFrom := 0
	if d.Chat != nil {
		chatFrom = d.Chat.Total()
	}
	before := map[int32]int{}
	for _, o := range offers {
		before[o.id] = packCountOf(d, o.id)
		if before[o.id] < o.qty {
			return KitHandoutResult{Reason: "I am not holding what I drew"}
		}
	}
	if len(give) > 0 {
		// The trade screen refuses a trade the receiving pack cannot hold, and offering moves the kit out of the
		// pack first: only the slots the kit empties count as room.
		free := orderplan.PackSlots - len(packItems(d))
		if !orderplan.GiveRoomOK(free, slotsLeaving(d, offers), orderplan.GiveSlotsFor(give, giveSlots, configFacts(d.Config))) {
			d.logf("hand-out: no room in my pack for %q's hand-over (%d free slot(s))", to, free)
			return KitHandoutResult{Reason: "no room in my pack for your hand-over", Declined: true}
		}
	}

	deadline := time.Now().Add(handoutMeetTimeout)
	nextRequest := time.Time{}
	for !trade.Active() {
		if time.Now().After(deadline) {
			return KitHandoutResult{Reason: "you never opened the trade", Declined: true}
		}
		if randomevents.Busy(d.Session) {
			return KitHandoutResult{Reason: "a random event interrupted the hand-out"}
		}
		if time.Now().After(nextRequest) {
			if err := trade.Request(slot); err != nil {
				d.logf("hand-out: requesting a trade with %q: %v", to, err)
			}
			nextRequest = time.Now().Add(handoutRequestEvery)
		}
		if !d.Delay(300 * time.Millisecond) {
			return KitHandoutResult{Stopped: true}
		}
	}
	waitFor(d, "the trade partner name", 3*time.Second, func() bool {
		return orderplan.NamesMatch(trade.Partner(), to)
	})
	if !orderplan.NamesMatch(trade.Partner(), to) {
		d.logf("hand-out: the trade screen says %q, not %q — declining", trade.Partner(), to)
		_ = trade.Decline(3 * time.Second)
		return KitHandoutResult{Reason: "the trade screen named someone else"}
	}
	if !waitFor(d, "the trade offer screen", 10*time.Second, trade.OnOfferScreen) {
		_ = trade.Decline(3 * time.Second)
		return KitHandoutResult{Reason: "the offer screen never opened"}
	}
	// EVERY LINE IN ONE BURST (twopertick-1012). Each line used to be clicked, then waited on until it showed on the
	// screen, before the next: a round trip a line (one to two ticks, and a count dialog's wait for an exact count)
	// while the donor stood in front of the mule and the rest of the fleet queued behind. Offer-All and Offer-X are
	// plain trade-side inventory buttons with no delay (interface_trade/scripts/trade.rs2), so the lines go out five
	// user events to a tick (an Offer-X is two, its count riding blind behind it) and the screen is read ONCE;
	// a line that did not show is offered again the old way.
	lines := make([]actions.TradeLine, 0, len(offers))
	for _, o := range offers {
		lines = append(lines, actions.TradeLine{ID: o.id, Qty: int32(o.qty), All: packCountOf(d, o.id) <= o.qty})
	}
	placed := trade.OfferMany(lines, handoutWaiter{d})
	d.logf("hand-out: %s", placed.Log())
	if placed.Err != nil || len(placed.Missing) > 0 || len(placed.Left) > 0 {
		d.logf("hand-out: could not place the kit on the screen (error %v, not held %v, never showed %v)", placed.Err, placed.Missing, placed.Left)
		_ = trade.Decline(3 * time.Second)
		return KitHandoutResult{Reason: "could not place the kit on the screen"}
	}
	for _, o := range offers {
		if !waitFor(d, "my own offer to register", 10*time.Second, func() bool {
			return countByID(trade.MyOffer(), o.id) >= o.qty
		}) {
			_ = trade.Decline(3 * time.Second)
			return KitHandoutResult{Reason: "my offer never registered"}
		}
	}
	// THE DONOR'S SIDE. With no hand-over agreed anything on it declines; with one, the mule waits for all of it to
	// show (the donor places it in one burst) and takes the screen as it stands after kitGiveWait.
	theirKit := func() []orderplan.KitLine { return kitRows(d.Config, trade.TheirOffer()) }
	giveSince := time.Now()
	for {
		state, why := orderplan.JudgeGive(give, theirKit())
		if state == orderplan.GiveBad {
			d.logf("hand-out: %q's side of the screen is not the hand-over agreed (%s) — declining", to, why)
			_ = trade.Decline(3 * time.Second)
			return KitHandoutResult{Reason: why}
		}
		if state == orderplan.GiveComplete {
			break
		}
		if time.Since(giveSince) >= kitGiveWait || !trade.OnOfferScreen() {
			d.logf("hand-out: only part of %q's hand-over showed in %s — going on without the rest", to, kitGiveWait)
			break
		}
		if !d.Delay(250 * time.Millisecond) {
			return KitHandoutResult{Stopped: true}
		}
	}
	seen := kitSig(theirKit())
	if err := trade.Accept(); err != nil {
		d.logf("hand-out: accept (offer screen): %v", err)
		return KitHandoutResult{Reason: "could not accept the offer screen"}
	}
	confirmDeadline := time.Now().Add(30 * time.Second)
	for !trade.OnConfirmScreen() {
		if randomevents.Busy(d.Session) {
			_ = trade.Decline(3 * time.Second)
			return KitHandoutResult{Reason: "a random event interrupted the hand-out"}
		}
		if !time.Now().Before(confirmDeadline) {
			_ = trade.Decline(3 * time.Second)
			return KitHandoutResult{Reason: "you never reached the confirm screen"}
		}
		if !d.Delay(250 * time.Millisecond) {
			return KitHandoutResult{Stopped: true}
		}
		if trade.OnOfferScreen() {
			// The donor changed its side after this side accepted: the server cleared both accepts. Judge the new
			// screen and accept it again, or decline.
			if now := theirKit(); kitSig(now) != seen {
				if state, why := orderplan.JudgeGive(give, now); state == orderplan.GiveBad {
					d.logf("hand-out: %q changed its side to something not agreed (%s) — declining", to, why)
					_ = trade.Decline(3 * time.Second)
					return KitHandoutResult{Reason: why}
				}
				seen = kitSig(now)
				if err := trade.Accept(); err != nil {
					d.logf("hand-out: accept (offer screen, again): %v", err)
				}
			}
		}
		if !trade.Active() && stillClosed(d, trade.Active) {
			if partnerLacksRoom(d, chatFrom) {
				d.logf("hand-out: %q has no room in its pack for the kit — the trade screen refused it", to)
				return KitHandoutResult{Reason: orderplan.DeclineNoRoom, Declined: true}
			}
			return KitHandoutResult{Reason: "the trade closed before the confirm screen"}
		}
	}
	mine, theirs := trade.ConfirmOffers()
	for _, o := range offers {
		if countByID(mine, o.id) != o.qty {
			d.logf("hand-out: the confirm screen does not match the kit I offered %q (id=%d: %d of %d) — declining", to, o.id, countByID(mine, o.id), o.qty)
			_ = trade.Decline(3 * time.Second)
			return KitHandoutResult{Reason: "the confirm screen did not match"}
		}
	}
	// Nothing of mine on the screen but the kit: with several kits drawn at once the pack holds other donors' items.
	if kinds := distinctIDs(mine); kinds != len(offers) {
		d.logf("hand-out: the confirm screen holds %d kind(s) of item on my side, the kit has %d — declining", kinds, len(offers))
		_ = trade.Decline(3 * time.Second)
		return KitHandoutResult{Reason: "the confirm screen did not match"}
	}
	took := kitRows(d.Config, theirs)
	if state, why := orderplan.JudgeGive(give, took); state == orderplan.GiveBad || !orderplan.NamesMatch(trade.Partner(), to) {
		d.logf("hand-out: the confirm screen's donor side is not the hand-over agreed (%q) — declining", why)
		_ = trade.Decline(3 * time.Second)
		return KitHandoutResult{Reason: "the confirm screen did not match"}
	}
	if err := trade.Accept(); err != nil {
		d.logf("hand-out: accept (confirm screen): %v", err)
		return KitHandoutResult{Reason: "could not accept the confirm screen"}
	}
	if !waitFor(d, "the hand-out trade to close", 20*time.Second, func() bool { return !trade.Active() }) {
		return KitHandoutResult{Reason: "the trade never closed"}
	}
	if !waitFor(d, "the kit to leave my pack", 8*time.Second, func() bool {
		for _, o := range offers {
			// What the donor handed back in the same trade may be the very item the kit carried (coins).
			if packCountOf(d, o.id) > before[o.id]-o.qty+countByID(theirs, o.id) {
				return false
			}
		}
		return true
	}) {
		d.logf("hand-out: the kit trade with %q closed but the items are still in my pack", to)
		return KitHandoutResult{Reason: "the trade closed without the items leaving"}
	}
	if len(took) > 0 {
		d.logf("HAND-OUT DELIVERED: a kit of %d line(s) to %q in one trade, and took %s in from it", len(offers), to, describeKitIn(took))
	} else {
		d.logf("HAND-OUT DELIVERED: a kit of %d line(s) to %q in one trade", len(offers), to)
	}
	return KitHandoutResult{Delivered: true, Took: took}
}

// kitRows is trade-screen rows as kit lines, named through the config (a bank note apart from its item). Without a
// config a row is named by its id.
func kitRows(cfg *cachecfg.Config, rows []gamestate.SlotItem) []orderplan.KitLine {
	out := make([]orderplan.KitLine, 0, len(rows))
	for _, r := range rows {
		n := int(r.Count)
		if n < 1 {
			n = 1
		}
		l := orderplan.KitLine{Item: itoa(int(r.ID)), Qty: n}
		if cfg != nil {
			if o, ok := cfg.Obj(int(r.ID)); ok {
				l.Item, l.Noted = o.Name, o.Noted()
			}
		}
		out = append(out, l)
	}
	return out
}

// kitSig is a comparable form of a screen side, for noticing that it changed.
func kitSig(rows []orderplan.KitLine) string {
	parts := make([]string, 0, len(rows))
	for _, r := range rows {
		parts = append(parts, strings.ToLower(r.Item)+"|"+itoa(r.Qty)+"|"+strconv.FormatBool(r.Noted))
	}
	sort.Strings(parts)
	return strings.Join(parts, ";")
}

func describeKitIn(rows []orderplan.KitLine) string {
	parts := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.Noted {
			parts = append(parts, itoa(r.Qty)+" "+r.Item+" (noted)")
		} else {
			parts = append(parts, itoa(r.Qty)+" "+r.Item)
		}
	}
	return strings.Join(parts, ", ")
}

func distinctIDs(rows []gamestate.SlotItem) int {
	seen := map[int32]bool{}
	for _, r := range rows {
		seen[r.ID] = true
	}
	return len(seen)
}

// slotsLeaving is how many pack slots offering offers empties: every slot of an id whose whole pack count is
// offered (an item offered in part, out of a stack other kits share, empties none).
func slotsLeaving(d *Deps, offers []kitOffer) int {
	pack := packItems(d)
	n := 0
	for _, o := range offers {
		if countByID(pack, o.id) > o.qty {
			continue
		}
		for _, it := range pack {
			if it.ID == o.id {
				n++
			}
		}
	}
	return n
}

// configFacts is orderplan's item facts read off the object config, by name: whether the unnoted item stacks.
func configFacts(cfg *cachecfg.Config) func(item string) (orderplan.KitFacts, bool) {
	return func(item string) (orderplan.KitFacts, bool) {
		if cfg == nil {
			return orderplan.KitFacts{}, false
		}
		// "Coins" names two objs (617 does not stack, 995 does): any plain obj of the name that stacks counts.
		f, found := orderplan.KitFacts{}, false
		for _, id := range cfg.ObjIDs(item) {
			if o, ok := cfg.Obj(id); ok && !o.Noted() {
				f.Stackable, found = f.Stackable || o.Stackable, true
			}
		}
		return f, found
	}
}
