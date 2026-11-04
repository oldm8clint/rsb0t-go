package mulerunner

// SEVERAL KITS IN ONE BANK VISIT (a user request: "the mule is overloaded ... withdraw multiple kits, take
// multiple trades at once between banking cycles"). HandoutKit made one bank visit per kit: open the bank, bank the
// pack, draw, close, trade. With a queue of kits the visit is most of the work, and the mule served one donor at a
// time while a line of them stood at its post. HandoutKits takes the oldest kits of the queue together:
//
//  1. every donor is looked up first; one out of sight is refused on the spot and costs the others nothing;
//  2. ONE bank visit: the pack is banked once, each kit is decided against the bank as it opened (the kits before
//     it already counted against the stock and the ledger's window caps), and orderplan.KitBatch fits as many as
//     the pack holds, up to the caller's maximum -- the first kit that does not fit ends the batch and is deferred;
//  3. the kits' lines are drawn together: a line two kits share (the same noted sharks) is one withdrawal split
//     between them afterwards;
//  4. each kit is announced, then traded out one after the other with deliverKitWith, the pack holding the kits
//     still to come; and a hand-over a donor puts on its side of ITS trade lands in the pack beside them;
//  5. the pack is banked once at the end, if it holds anything (a donor that never opened its trade, hand-overs).
//
// A batch of one is exactly HandoutKit, blind draw and all. Every trade is deliverKitWith's, whose screen checks
// are the single-kit ones plus "nothing of mine on the screen but this kit".

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/orderplan"
)

// HandoutKits fulfils up to maxKits of reqs (oldest first) in one bank visit and returns one result per request,
// in order. A result with Deferred set was not taken: the caller keeps it queued. Nothing is published here;
// each request's Announce and the results carry every answer.
func HandoutKits(d *Deps, reqs []KitRequest, maxKits int) []KitHandoutResult {
	out := make([]KitHandoutResult, len(reqs))
	if len(reqs) == 0 {
		return out
	}
	deferFrom := func(from int, why string) {
		for i := from; i < len(reqs); i++ {
			out[i] = KitHandoutResult{Deferred: true, Reason: why}
		}
	}
	if len(reqs) == 1 || maxKits < 2 {
		out[0] = HandoutKit(d, reqs[0])
		deferFrom(1, "")
		return out
	}
	announce := func(i int, lines []orderplan.KitLineDecision, reason string) {
		if reqs[i].Announce != nil {
			reqs[i].Announce(lines, reason)
		}
	}
	refuse := func(i int, lines []orderplan.KitLineDecision, reason string) {
		announce(i, lines, reason)
		out[i] = KitHandoutResult{Lines: lines, Reason: reason, Declined: true}
	}
	if randomevents.Busy(d.Session) {
		deferFrom(0, "busy with a random event")
		return out
	}

	// 1. Who is in sight.
	slots := make([]int32, len(reqs))
	var live []int
	for i, r := range reqs {
		slot, ok := ResolvePlayerSlot(d, r.To)
		if !ok {
			refuse(i, nil, orderplan.DeclineNotInSight)
			continue
		}
		slots[i] = slot
		live = append(live, i)
	}
	switch len(live) {
	case 0:
		return out
	case 1:
		out[live[0]] = HandoutKit(d, reqs[live[0]])
		return out
	}

	// 2. One bank visit.
	bank, _, opened := openNearestBankBoothWith(d, nil)
	if !opened {
		for _, i := range live {
			out[i] = KitHandoutResult{Reason: "could not open a bank"}
		}
		return out
	}
	d.bankRead(bank.Items())
	if bank.NoteMode() {
		_ = bank.SetNoteMode(false, handoutWaiter{d})
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
	type admitted struct {
		i     int
		lines []orderplan.KitLineDecision
	}
	var batch []admitted
	fit := orderplan.NewKitBatch(orderplan.PackSlots, maxKits)
	planned := map[string]int{}
	now := time.Now()
	scratch := reqs[live[0]].Ledger.Clone()
	for n, i := range live {
		r := reqs[i]
		left := func(item string) int { return max(0, stock(item)-planned[strings.ToLower(strings.TrimSpace(item))]) }
		dec := orderplan.DecideKit(now, r.To, r.Lines, r.Policy, scratch, left, facts, r.Room)
		if dec.Serving() == 0 {
			refuse(i, dec.Lines, kitWholeReason(dec.Lines))
			continue
		}
		giveSlots := 0
		if len(r.Give) > 0 {
			giveSlots = orderplan.GiveSlotsFor(r.Give, r.GiveSlots, facts)
		}
		if !fit.Add(dec.Lines, facts, giveSlots) {
			// The first kit that does not fit ends the batch: a big kit at the head is never overtaken.
			for _, j := range live[n:] {
				out[j] = KitHandoutResult{Deferred: true}
			}
			break
		}
		var crossed []orderplan.ItemCount
		for _, l := range dec.Lines {
			if l.Qty > 0 {
				planned[strings.ToLower(strings.TrimSpace(l.Item))] += l.Qty
				crossed = append(crossed, orderplan.ItemCount{Name: l.Item, Count: l.Qty})
			}
		}
		scratch.RecordKit(now, r.To, r.ID, crossed)
		batch = append(batch, admitted{i: i, lines: dec.Lines})
	}
	if len(batch) == 0 {
		_ = bank.Close()
		return out
	}
	var names []string
	for _, a := range batch {
		names = append(names, reqs[a.i].To)
	}
	d.logf("hand-out: %d kit(s) in one bank visit (%d of %d pack slot(s)): %s", len(batch), fit.Slots(), orderplan.PackSlots, strings.Join(names, ", "))

	// 3. Draw them together: a stack two kits share is one withdrawal.
	type contrib struct{ k, l, qty int }
	var merged []orderplan.KitLineDecision
	var mergedFrom [][]contrib
	byKey := map[string]int{}
	items := bank.Items()
	for k, a := range batch {
		for l, line := range a.lines {
			if line.Qty <= 0 {
				continue
			}
			key := carryKey(d, items, line)
			if m, ok := byKey[key]; ok {
				merged[m].Qty += line.Qty
				mergedFrom[m] = append(mergedFrom[m], contrib{k, l, line.Qty})
				continue
			}
			byKey[key] = len(merged)
			line.Reason = ""
			merged = append(merged, line)
			mergedFrom = append(mergedFrom, []contrib{{k, l, line.Qty}})
		}
	}
	drawn, offers, stopped := drawKit(d, bank, "the batch", merged)
	if stopped {
		_ = bank.Close()
		for _, a := range batch {
			out[a.i] = KitHandoutResult{Stopped: true}
		}
		return out
	}
	_ = bank.Close()

	// Split what came out between the kits that asked for it, the earlier kits first.
	perLines := make([][]orderplan.KitLineDecision, len(batch))
	perOffers := make([][]kitOffer, len(batch))
	for k, a := range batch {
		perLines[k] = append([]orderplan.KitLineDecision(nil), a.lines...)
		perOffers[k] = make([]kitOffer, len(a.lines))
	}
	for m := range merged {
		avail := drawn[m].Qty
		for _, c := range mergedFrom[m] {
			n := min(c.qty, avail)
			avail -= n
			line := &perLines[c.k][c.l]
			line.Qty = n
			switch {
			case n >= c.qty:
			case n <= 0:
				line.Reason = drawn[m].Reason
				if line.Reason == "" {
					line.Reason = "could not draw it out of the bank"
				}
			default:
				line.Reason = "could only draw " + itoa(n)
			}
			if n > 0 {
				perOffers[c.k][c.l] = kitOffer{id: offers[m].id, qty: n}
			}
		}
	}
	// Every kit's lines must be in the pack before any is announced.
	if !waitFor(d, "the withdrawn kits to reach my pack", 6*time.Second, func() bool {
		for m := range merged {
			if offers[m].qty > 0 && packCountOf(d, offers[m].id) < offers[m].qty {
				return false
			}
		}
		return true
	}) {
		for k, a := range batch {
			announce(a.i, perLines[k], "the withdrawal never reached my pack")
			out[a.i] = KitHandoutResult{Lines: perLines[k], Reason: "the withdrawal never reached my pack"}
		}
		bankBatchLeftovers(d)
		return out
	}

	// 4. Announce, then trade them out one after the other.
	type delivery struct {
		k     int
		offer []kitOffer
	}
	var todo []delivery
	for k, a := range batch {
		var live []kitOffer
		for l, line := range perLines[k] {
			if line.Qty > 0 {
				live = append(live, perOffers[k][l])
			}
		}
		if len(live) == 0 {
			announce(a.i, perLines[k], "could not draw it out of the bank")
			out[a.i] = KitHandoutResult{Lines: perLines[k], Reason: "could not draw it out of the bank"}
			continue
		}
		d.logf("hand-out: %q's kit drawn — %s", reqs[a.i].To, describeKitLines(perLines[k]))
		announce(a.i, perLines[k], "")
		todo = append(todo, delivery{k: k, offer: live})
	}
	for n, t := range todo {
		a := batch[t.k]
		r := reqs[a.i]
		slot := slots[a.i]
		if n > 0 {
			// Looked up again: the donor has stood at the post through the trades before it.
			s, ok := ResolvePlayerSlot(d, r.To)
			if !ok {
				d.logf("hand-out: %q is no longer in sight — its kit goes back to the bank", r.To)
				out[a.i] = KitHandoutResult{Lines: perLines[t.k], Reason: orderplan.DeclineNotInSight, Declined: true}
				continue
			}
			slot = s
		}
		res := deliverKitWith(d, r.To, slot, t.offer, r.Give, r.GiveSlots)
		res.Lines = perLines[t.k]
		out[a.i] = res
		if res.Stopped {
			for _, rest := range todo[n+1:] {
				out[batch[rest.k].i] = KitHandoutResult{Stopped: true}
			}
			return out
		}
	}

	// 5. One bank for whatever is left in the pack.
	bankBatchLeftovers(d)
	return out
}

// carryKey names the pack stack a decided line arrives as: its obj id (the note's, for a noted line), so two
// kits' lines that end up in the same stack are drawn once. A line the bank cannot name keeps its own key.
func carryKey(d *Deps, bankItems []gamestate.SlotItem, line orderplan.KitLineDecision) string {
	id, found := bankedID(d.Config, bankItems, line.Item)
	if !found {
		return fmt.Sprintf("name:%s:%v", strings.ToLower(strings.TrimSpace(line.Item)), line.Noted)
	}
	if line.Noted {
		if nid, ok := noteFor(d.Config, id); ok {
			return fmt.Sprintf("id:%d", nid)
		}
		return fmt.Sprintf("nonote:%d", id)
	}
	return fmt.Sprintf("id:%d", id)
}

// bankBatchLeftovers banks what the pack holds once a batch is done: kits whose donor never opened its trade and
// whatever donors handed over. Quiet when the pack is empty.
func bankBatchLeftovers(d *Deps) {
	if len(packItems(d)) == 0 {
		return
	}
	n, ok := DepositAllInPack(d)
	d.logf("hand-out: banked %d kind(s) of item the batch left in the pack (ok=%v)", n, ok)
}
