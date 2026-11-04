package actions

// SEVERAL OFFER LINES IN ONE TICK. Trade.Offer / Trade.OfferAll are one click, but every caller that offers a
// kit of several lines (the mule's hand-out, mulerunner/kit.go deliverKit) clicked one, waited for it to show
// on the screen, and clicked the next: a round trip (one to two ticks, three seconds when a count dialog is
// waited on) per line while the donor stood in front of the mule and the rest of the fleet queued behind.
//
// What content 289 allows (interface_trade/scripts/trade.rs2 [label,movetotrade], engine InvButtonHandler):
//
//   - [inv_button4,tradeside:inv] (Offer All) and [inv_button5,tradeside:inv] (Offer X) are plain inventory
//     buttons: no p_delay, no %action_delay. Offer-All runs to the end inside the handler and moves every one
//     of that object from the pack to the offer (inv_moveitem by object, not by slot).
//   - Offer-X suspends at p_countdialog, with protected access held until the answer, so nothing may be sent
//     between the button and its RESUME_P_COUNTDIALOG; the pair goes out back to back, as the fast bank's
//     Withdraw-X does (fastbank.go): the engine resumes the script in the same read.
//   - The pack does not close up when an offer leaves it, so a slot named from one reading of the side panel
//     still holds its object when the engine reaches it, provided no two lines name the same object.
//   - Every offer resets both players' accept flags (%tradestatus), which is harmless before the accept.
//
// OfferMany sends the lines five user events to a tick (an Offer-X is two), confirms once on the offer
// screen, and offers what did not show the old way, one line at a time.

import (
	"fmt"
	"time"
)

// tradeBatchConfirm is how long the one confirm waits for every line to show on the screen before the
// stragglers are offered one at a time.
const tradeBatchConfirm = 4 * time.Second

// TradeLine is one line of an offer: an object and how many of it.
type TradeLine struct {
	ID int32
	// Qty is the count the line must reach on the offer screen. With All it is the whole stack the pack holds
	// (what the offer screen is expected to show; 0 when unknown), else exactly this many are offered with Offer-X.
	Qty int32
	// All offers the object with Offer-All (one click, the whole stack) instead of Offer-X.
	All bool
}

// TradeBatch is what one OfferMany did.
type TradeBatch struct {
	// Wanted is the lines asked for; Sent those whose click went out in the burst.
	Wanted int
	Sent   []int32
	// Registered is the lines the offer screen then showed at their count; Retried the lines the one-at-a-time
	// offer landed afterwards; Left the lines that never showed.
	Registered, Retried, Left []int32
	// Missing is the lines the side panel does not hold: nothing was sent for them.
	Missing []int32
	// Events is the user events sent, Bursts the ticks they took (five events to a tick).
	Events, Bursts int
	Took           time.Duration
	Err            error
}

// OK reports whether every line is on the screen.
func (b TradeBatch) OK() bool { return b.Err == nil && len(b.Missing) == 0 && len(b.Left) == 0 }

// Log is the one-line account of the batch for a script's log.
func (b TradeBatch) Log() string {
	s := fmt.Sprintf("trade: offered %d of %d line(s) in %d tick(s) (%d click(s), %s)",
		len(b.Registered)+len(b.Retried), b.Wanted, b.Bursts, b.Events, b.Took.Round(time.Millisecond))
	if len(b.Retried) > 0 {
		s += fmt.Sprintf("; %d on the one-at-a-time retry", len(b.Retried))
	}
	if len(b.Left) > 0 {
		s += fmt.Sprintf("; %d never showed", len(b.Left))
	}
	if len(b.Missing) > 0 {
		s += fmt.Sprintf("; %d not in the pack", len(b.Missing))
	}
	return s
}

func offeredCount(t *Trade, id int32) int32 { return CountByID(t.MyOffer(), id) }

// OfferMany places every line on the offer screen in one burst. Two lines of the same object are merged into
// the first (an Offer-X per line would name the same slot twice). It needs the offer screen; it never accepts.
func (t *Trade) OfferMany(lines []TradeLine, w Waiter) (out TradeBatch) {
	w = waiterOr(w)
	start := time.Now()
	defer func() { out.Took = time.Since(start) }()
	var uniq []TradeLine
	for _, l := range lines {
		if l.ID == 0 || (l.Qty <= 0 && !l.All) {
			continue
		}
		dup := false
		for i := range uniq {
			if uniq[i].ID == l.ID {
				// A whole-stack line wins (its count is the stack, never summed twice); two exact counts add.
				if uniq[i].All || l.All {
					uniq[i].Qty = max(uniq[i].Qty, l.Qty)
					uniq[i].All = true
				} else {
					uniq[i].Qty += l.Qty
				}
				dup = true
			}
		}
		if !dup {
			uniq = append(uniq, l)
		}
	}
	out.Wanted = len(uniq)
	if len(uniq) == 0 {
		return out
	}
	if !t.OnOfferScreen() {
		out.Err = ErrNotOnScreen
		return out
	}
	pack := t.SidePack()
	var packets []BurstPacket
	var sent []TradeLine
	pairs := 0
	for _, l := range uniq {
		slot := -1
		for _, it := range pack {
			if it.ID == l.ID {
				slot = it.Slot
				break
			}
		}
		if slot < 0 {
			out.Missing = append(out.Missing, l.ID)
			continue
		}
		l, slot := l, slot
		sent = append(sent, l)
		out.Sent = append(out.Sent, l.ID)
		if l.All {
			out.Events++
			packets = append(packets, BurstPacket{Events: 1, Send: func() error { return InvButton(t.s, tradeOfferAllOption, l.ID, slot, TradeOfferInv) }})
			continue
		}
		pairs++
		out.Events += 2
		packets = append(packets, BurstPacket{Events: 2, Send: func() error {
			if err := InvButton(t.s, tradeOfferXOption, l.ID, slot, TradeOfferInv); err != nil {
				return err
			}
			return answerCountDialogBlind(t.s, l.Qty)
		}})
	}
	shown := func(l TradeLine) bool { return lineShown(t, l) }
	allShown := func() bool {
		for _, l := range sent {
			if !shown(l) {
				return false
			}
		}
		return true
	}
	res := SendBurst(t.s, w, BankMaxPerTick, allShown, packets)
	out.Bursts = res.Bursts
	if res.Err != nil {
		out.Err = res.Err
		return out
	}
	if len(packets) > 0 {
		w.DelayUntil(allShown, tradeBatchConfirm)
	}
	if pairs > 0 && t.s.State().CountDialogOpen() {
		// The P_COUNTDIALOG of an Offer-X answered blind arrives after the answer went out; the server's
		// dialog is already resumed, so the client's flag is stale (the fast bank does the same).
		t.s.State().ClearDialogInput()
	}
	for _, l := range sent {
		if shown(l) {
			out.Registered = append(out.Registered, l.ID)
			continue
		}
		if t.offerOne(l, w) {
			out.Retried = append(out.Retried, l.ID)
		} else {
			out.Left = append(out.Left, l.ID)
		}
	}
	return out
}

// offerOne is the old one-at-a-time offer of a line, waited on: Offer-All or Offer-X, then until it shows.
func (t *Trade) offerOne(l TradeLine, w Waiter) bool {
	if !t.OnOfferScreen() {
		return false
	}
	var err error
	if l.All {
		err = t.OfferAll(l.ID)
	} else {
		held := offeredCount(t, l.ID)
		err = t.Offer(l.ID, l.Qty-held, 3*time.Second)
	}
	if err != nil {
		return false
	}
	return w.DelayUntil(func() bool { return lineShown(t, l) }, tradeBatchConfirm)
}

// lineShown reports whether the offer screen holds the line: at least its count, or any at all for a whole-stack
// line whose count is not known.
func lineShown(t *Trade, l TradeLine) bool {
	have := offeredCount(t, l.ID)
	if l.Qty > 0 {
		return have >= l.Qty
	}
	return have > 0
}
