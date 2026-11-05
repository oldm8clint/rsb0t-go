package mulerunner

// The mule's hand-OUT: withdrawing an item a blocked donor asked for over the wall bus and
// trading it TO them. Everything else in this package runs the other way — a donor opens the
// trade and gives, the mule reciprocates and receives — and internal/bot/mule.go's own doc
// comment states the gap plainly: it "never acts on a queued pickup — no travel, no trade
// initiated toward a donor."
//
// What is deliberately reused rather than duplicated from completeTrade, and what is not.
// Reused: the allow-list (orderplan.Policy.Allowed is the same names), the settle-before-
// accepting discipline, the transient-flicker debounce (stillClosed, shared verbatim), and —
// the important one — the receipt rule that "trade closed" is NOT proof anything transferred.
// completeTrade proves receipt by watching its own pack GAIN what was promised; the exact
// mirror here is watching its own pack LOSE what it offered, which is the only evidence the
// engine actually gives the giving side. Not reused: completeTrade's own body, because every
// decision inside it is about an offer arriving from someone else — whose offer to settle on,
// when to re-accept a changed one, what "mine must be empty" means. In this direction our own
// offer is the one under test and theirs must stay empty, which inverts every one of those
// checks. Sharing one function across both would have meant a flag on nearly every line.
//
// Honest scope limit, stated the way mule.go states its own: this serves a donor that is
// ALREADY visible to the mule. The incident it was built for is exactly that case — production
// account `spare28` sat blocked at the Catherby bank about two tiles from `mule` — and it
// declines cleanly (so the donor stops waiting and falls back to its own recovery) rather than
// travelling to a donor somewhere else on the map. Travel is the same still-unbuilt trip
// machinery mule.go's own doc comment defers, and this does not pretend to have built it.

import (
	"errors"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/orderplan"
)

// HandoutRequest is one order off the bus, plus the policy that governs it. The raw order
// text is carried through rather than a pre-parsed item so that orderplan — the tested pure
// layer — stays the only thing that ever decides what a text means.
type HandoutRequest struct {
	// To is the donor that placed the order.
	To string
	// Text is the order exactly as it arrived (mulebus.Message.Text).
	Text string
	// Policy and Ledger are the safety gate; see orderplan.Decide. Ledger may be nil, which
	// declines — failing closed, matching orderplan's own contract.
	//
	// Since 2026-09-22 the Policy's curated catalogue is no longer the only thing that can be
	// handed out: orderplan.OpenStock serves an explicit order for an uncatalogued item out of
	// real stock, on the fleet owner's instruction. Nothing in THIS file changed for it, and
	// that is worth saying rather than leaving to be rediscovered — the widening is entirely a
	// decision-layer change, and every mechanical step below (resolve the id the bank really
	// holds, draw exactly the decided quantity, notes only when a pack genuinely cannot carry
	// it, prove receipt by the pack falling) already worked for any item by name.
	Policy orderplan.Policy
	Ledger *orderplan.Ledger
}

// HandoutResult is what happened. Delivered is true only once the items are confirmed gone
// from this account's own pack after the trade closed — never merely because a trade closed.
type HandoutResult struct {
	Delivered bool
	// Item/Crossed are the canonical item name and how many actually left, set on Delivered.
	Item    string
	Crossed int
	// Reason is why nothing (or less) crossed, short enough for mulebus.DeclinedMessage.
	Reason string
	// Declined marks a refusal the donor should be told about so it stops waiting, as
	// opposed to a mechanical failure mid-trade (which it should also be told about, but
	// which is not a policy decision).
	Declined bool
	// Stopped is true when the caller's own Delay asked to stop mid hand-out.
	Stopped bool
}

func declinedResult(reason string) HandoutResult {
	return HandoutResult{Reason: reason, Declined: true}
}

// handoutRequestEvery spaces the outgoing trade requests while waiting for the donor to
// reciprocate — the same interval donorplan.RequestEvery uses in the other direction, for the
// same reason (a single request can be missed, and the server ignores stale ones).
const handoutRequestEvery = 3 * time.Second

// handoutMeetTimeout is how long the mule keeps asking a visible donor to open a trade before
// giving up on the order. Short on purpose: the donor is blocked and waiting on a bounded
// timer of its own, and a mule stuck re-requesting a donor that is not answering is a mule not
// taking the ordinary hand-OVER trades that are its actual job.
const handoutMeetTimeout = 25 * time.Second

// withdrawGuard bounds the per-item Withdraw-1 loop. Quantities here are tiny by policy
// (orderplan.DefaultCatalog is one of each), so this only ever exists to stop a loop that is
// not making progress, never to cap a legitimate withdrawal.
const withdrawGuard = 30

// nonNotedID resolves a canonical item name to the ordinary (un-noted) object id.
//
// Config.ObjIDs returns every id with that name, which for most tradeable items is the item
// AND its bank note — handing over a note instead of the tool would leave the donor just as
// blocked, holding something it cannot fish with. Picks the first id whose Obj is not a note,
// and reports false rather than guessing if the name resolves to nothing usable.
func nonNotedID(cfg *cachecfg.Config, name string) (int32, bool) {
	if cfg == nil {
		return 0, false
	}
	for _, id := range cfg.ObjIDs(name) {
		obj, ok := cfg.Obj(id)
		if !ok || obj.Noted() {
			continue
		}
		return int32(id), true
	}
	return 0, false
}

// noteFor is the BANK NOTE object id of obj id: the noted obj of the same name whose CertLink is id,
// the mirror of bankedID. Used only for an order too large to travel any other way (see notesAbove).
//
// BY ID, NOT BY NAME. Content 289 names the unstrung bow and the strung one alike ("Yew shortbow"
// is obj 66's and 857's name), so a name has a note for each, and only the one whose CertLink is the
// banked id is what a note-mode withdrawal of it yields. The first note of the name was the
// unstrung bow's: a kit line of 200 strung Yew shortbows drew notes the pack check never found, and
// every walk-alch kit came out "could not draw it out of the bank" (live 6 Oct 2026, the mule holding
// 130,712 of them).
func noteFor(cfg *cachecfg.Config, id int32) (int32, bool) {
	if cfg == nil {
		return 0, false
	}
	obj, ok := cfg.Obj(int(id))
	if !ok || obj.Noted() {
		return 0, false
	}
	for _, n := range cfg.ObjIDs(obj.Name) {
		if note, ok := cfg.Obj(n); ok && note.Noted() && note.CertLink == int(id) {
			return int32(n), true
		}
	}
	return 0, false
}

// notesAbove is the order size above which a hand-out must travel as bank notes: a full
// backpack. Strictly above, never at or below, and that boundary is load-bearing — it is what
// keeps nonNotedID's own rule intact. Every tool in the default catalogue is ordered one at a
// time, so a donor blocked without a net still receives a NET, not a note it cannot fish with.
// Only an order that genuinely cannot fit in a pack (500 crystal keys) ever becomes notes.
//
// The value is orderplan.NotesAbove, shared with the donor's own FitAskToPack so both sides of a
// hand-out agree on when an order travels as notes.
const notesAbove = orderplan.NotesAbove

// mustUseNotes is whether a hand-out of qty has to cross as BANK NOTES rather than as the
// ordinary item. Pure, so the rule can be tested without a bank, a session or a trade.
//
// Two conditions, and the second was missing until 2026-09-21. Notes are for an order that
// genuinely cannot fit in a backpack -- 500 crystal keys against 28 slots -- so an item that
// ALREADY STACKS never needs them, at any quantity, because it occupies exactly one slot however
// many there are.
//
// Testing quantity alone broke the one item the fleet most needs to move in bulk. Coins stack
// natively and therefore have no noted form at all, so a 150,000-coin order took the notes
// branch, failed to find a note id, and was declined outright -- which is every woodcutter's
// rune-axe money, live.
func mustUseNotes(qty int, stackable bool) bool { return orderplan.TravelsAsNotes(qty, stackable) }

// bankedID is nonNotedID, preferring the un-noted id the bank actually holds when a name
// resolves to more than one. Found live (2026-09-19, the opt-in Scarab kit catalogue): "Coins"
// names two un-noted objs in this config (617 and 995), nonNotedID picked the one no bank ever
// holds, and an order for coins was declined "none in the bank" by a mule holding 1000 of them.
// Every name with a single un-noted id — the whole default fishing-tool catalogue — resolves
// exactly as before.
func bankedID(cfg *cachecfg.Config, bankItems []gamestate.SlotItem, name string) (int32, bool) {
	if cfg == nil {
		return 0, false
	}
	return pickBankedID(cfg.ObjIDs(name), func(id int) bool {
		obj, ok := cfg.Obj(id)
		return !ok || obj.Noted()
	}, bankItems, func(id int) bool {
		obj, ok := cfg.Obj(id)
		return ok && Wearable(obj.IOps[:])
	})
}

// Wearable reports whether an item's inventory options include wielding or wearing it.
func Wearable(iops []string) bool {
	for _, op := range iops {
		switch strings.ToLower(strings.TrimSpace(op)) {
		case "wield", "wear":
			return true
		}
	}
	return false
}

// pickBankedID is bankedID's pure core: among ids, skipping unusable ones, the best the bank holds,
// else the first usable one. When a plain name matches several rows the wearable one wins: content
// 289 names the unstrung bow and the strung one alike ("Shortbow" is obj 50 and obj 841), and the
// first-by-id rule handed every asker the unstrung one it could not wield. An asker that wants one
// form exactly uses a qualified name (cachecfg.QualifiedName), which resolves to a single obj.
// wearable may be nil: nothing is preferred then.
func pickBankedID(ids []int, unusable func(int) bool, bankItems []gamestate.SlotItem, wearable func(int) bool) (int32, bool) {
	var first, firstHeld, firstWear int32
	found, held, wear := false, false, false
	for _, id := range ids {
		if unusable(id) {
			continue
		}
		isWear := wearable != nil && wearable(id)
		if !found {
			first, found = int32(id), true
		}
		if countByID(bankItems, int32(id)) > 0 {
			if isWear {
				return int32(id), true
			}
			if !held {
				firstHeld, held = int32(id), true
			}
		} else if isWear && !wear {
			firstWear, wear = int32(id), true
		}
	}
	switch {
	case held:
		return firstHeld, true
	case wear:
		return firstWear, true
	}
	return first, found
}

func countByID(items []gamestate.SlotItem, id int32) int {
	n := 0
	for _, it := range items {
		if it.ID != id {
			continue
		}
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		n += c
	}
	return n
}

// packCountOf reads this account's own pack via the fixed PlayerPack component — see that
// constant's own doc comment for the live-found reason every read in this package that
// happens after a bank open/close uses it rather than Equipment.Inventory().
func packCountOf(d *Deps, id int32) int { return countByID(packItems(d), id) }

// Handout fulfils one bus order end to end: screen it, open the bank, re-check it against real
// stock, withdraw exactly what was decided, open a trade with the donor, offer exactly that and
// nothing else, confirm, and verify the items actually left this account's pack. Any refusal —
// policy, stock, or a donor that never answers — comes back as a Reason the caller publishes as
// a mulebus.DeclinedMessage, so a waiting donor stops waiting rather than hanging.
//
// On a mechanical failure after the withdrawal, whatever was drawn stays in the pack; muleBot's
// own Loop already calls DepositAllInPack before every trade cycle, so it goes back on the next
// pass rather than needing its own unwind path here.
func Handout(d *Deps, req HandoutRequest) HandoutResult {
	now := time.Now()

	// Cheapest first: a name off the allow list, a malformed text or an item that is not on
	// the safety catalogue is refused from the message alone, without ever walking to a bank.
	if screened := orderplan.Prescreen(now, req.To, req.Text, req.Policy, req.Ledger); screened.Verdict != orderplan.Fulfil {
		return declinedResult(screened.Reason)
	}
	if randomevents.Busy(d.Session) {
		return HandoutResult{Reason: "busy with a random event"}
	}
	// Resolve the donor BEFORE drawing anything: a donor that is not visible cannot be
	// traded with, and finding that out after a withdrawal would leave items out of the bank
	// for nothing. See this file's own header for why an absent donor is declined rather
	// than travelled to.
	slot, ok := ResolvePlayerSlot(d, req.To)
	if !ok {
		// The exact string is orderplan.DeclineNotInSight so the asking side can tell a
		// POSITIONAL decline from a stock one without matching text it invented itself.
		return declinedResult(orderplan.DeclineNotInSight)
	}

	// THE BLIND DRAW (4 Oct, the speed of MasterCrafter's six-tick gem stream). The order is decided
	// from the bank as this mule last read it (every visit confirms with the bank open before it
	// closes, and nothing else touches this account's bank), so the draw rides behind the booth
	// click and is in the pack the tick after the bank opens. The bank as it really opened is
	// then the judge: a draw that is not exactly the order goes back, and the order is decided
	// again from the fresh bank by the paced path below, exactly as before.
	blind := planBlindHandout(d, req, now)
	var blindOps []actions.BankOp
	if blind != nil {
		blindOps = blind.ops
	}
	bank, open, opened := openNearestBankBoothWith(d, blindOps)
	if !opened {
		return HandoutResult{Reason: "could not open a bank"}
	}
	// Publish the stock BEFORE deciding, not after. A donor asking for something that is not
	// here is exactly the case this reading is for (the fleet owner's 2026-09-22 report), and
	// every path below this point can return early -- so a publish placed after the decision
	// would be skipped on precisely the declines that matter. See Deps.OnBankRead.
	d.bankRead(bank.Items())
	if blind != nil {
		got := countByID(bank.Inventory(), blind.carry) - blind.carryBefore
		if open.Batch.Err == nil && got == blind.decision.Qty && !bank.NoteMode() {
			if blind.decision.Clamped {
				d.logf("hand-out: %q asked for more than policy or stock allows — giving %d %s", req.To, blind.decision.Qty, blind.decision.Item)
			}
			_ = bank.Close()
			if !waitFor(d, "the withdrawn hand-out to reach my pack", 5*time.Second, func() bool {
				return packCountOf(d, blind.carry) >= blind.carryBefore+blind.decision.Qty
			}) {
				return HandoutResult{Reason: "the withdrawal never reached my pack"}
			}
			res := deliver(d, req.To, slot, blind.carry, blind.decision.Qty)
			res.Item = blind.decision.Item
			return res
		}
		if errors.Is(open.Batch.Err, actions.ErrBankStopped) {
			_ = bank.Close()
			return HandoutResult{Stopped: true}
		}
		d.logf("hand-out: the blind draw of %d %s did not come out exactly (%d drawn; %s) — banking it and deciding again from the bank as it opened", blind.decision.Qty, blind.decision.Item, got, open)
		bankPackAtOpenBank(d, bank)
		if bank.NoteMode() {
			_ = bank.SetNoteMode(false, handoutWaiter{d})
		}
		d.bankRead(bank.Items())
	}
	decision := orderplan.Decide(now, req.To, req.Text, req.Policy, req.Ledger, func(item string) int {
		id, found := bankedID(d.Config, bank.Items(), item)
		if !found {
			return 0
		}
		return countByID(bank.Items(), id)
	})
	if decision.Verdict != orderplan.Fulfil {
		_ = bank.Close()
		return declinedResult(decision.Reason)
	}
	id, found := bankedID(d.Config, bank.Items(), decision.Item)
	if !found {
		_ = bank.Close()
		return declinedResult("I cannot identify that item")
	}
	if decision.Clamped {
		d.logf("hand-out: %q asked for more than policy or stock allows — giving %d %s", req.To, decision.Qty, decision.Item)
	}

	// An order bigger than a backpack has to cross as bank notes, because it cannot cross any
	// other way: 500 crystal keys do not fit in 28 slots. Notes stack, so the whole order rides
	// in one slot, and they become ordinary items again the moment the receiver banks them,
	// because a bank always stores the un-noted form. Everything below notesAbove is untouched.
	//
	// UNLESS THE ITEM ALREADY STACKS. Quantity alone was the whole test until 2026-09-21, and it
	// asked the wrong question: the rule this comment states is "an order that genuinely cannot
	// fit in a pack", and a stackable item always fits, in exactly one slot, at any quantity. The
	// item that exposed it is COINS, which stack natively and therefore have no noted form at
	// all -- so notedID failed and the whole hand-out was declined. Live, every woodcutter asking
	// for the 150,000 coins a rune axe costs:
	//
	//     woodcutter: "mule" did not hand over coins
	//                 (that many will not fit in a pack and it has no bank note)
	//
	// That was the second layer of the same wall: the parser's QtyMax refused the order before
	// this could even be reached, and fixing that simply moved the refusal here.
	carry := id
	stacks := false
	if obj, ok := d.Config.Obj(int(id)); ok && obj.Stackable {
		stacks = true
	}
	notes := mustUseNotes(decision.Qty, stacks)
	if notes {
		noted, ok := noteFor(d.Config, id)
		if !ok {
			_ = bank.Close()
			return declinedResult("that many will not fit in a pack and it has no bank note")
		}
		carry = noted
		d.logf("hand-out: %d %s is more than a pack holds — drawing it as bank notes", decision.Qty, decision.Item)
	}

	// MAKE ROOM FIRST. Live 2026-09-23 09:05:49-55 (build c0da98ee) this hand-out opened the
	// bank with 28 raw sharks in the pack -- a cook round had just withdrawn them and yielded to
	// this very order -- clicked Withdraw-1 on a Crystal key into a full pack, drew nothing, and
	// declined the donor "could not draw it out of the bank". The mule's Loop deposits its pack
	// before every trade cycle, but the order had arrived INSIDE the cycle, so nothing between
	// the cook round and this withdrawal banked it. The bank is open here already, so this is a
	// few clicks, and only when the pack cannot take the order as it stands.
	makeRoom(d, bank, carry, decision.Qty, stacks || carry != id)
	held := packCountOf(d, carry)
	// THE FAST DRAW (4 Oct). Note mode on, the withdrawal (Withdraw-1/5/10, or Withdraw-X with its
	// count right behind the button) and note mode off go out in one burst and are confirmed once
	// against the open bank's side panel: one round trip where the old path took one for each
	// note switch, one for the count dialog and one for the items. Anything it falls short of is
	// drawn the old way below, which is unchanged.
	drawn, stopped := 0, false
	unnotedBefore, carryBefore := countByID(bank.Inventory(), id), countByID(bank.Inventory(), carry)
	var ops []actions.BankOp
	if notes {
		ops = append(ops, actions.NoteModeOp(true))
	}
	ops = append(ops, actions.WithdrawAsOp(id, int32(decision.Qty), carry))
	if notes {
		ops = append(ops, actions.NoteModeOp(false))
	}
	fast := bank.Batch(ops, handoutWaiter{d}, actions.BatchOptions{})
	switch {
	case errors.Is(fast.Err, actions.ErrBankStopped):
		stopped = true
	case fast.Err != nil:
		d.logf("hand-out: %s — drawing it the old way", fast)
	default:
		// What the side panel shows arrived, not what was asked for: a bank holding fewer than
		// the order is drawn dry and the rest is left to the old path's own verdict.
		drawn = min(max(countByID(fast.Pack, carry)-carryBefore, 0), decision.Qty)
		if !fast.OK() || drawn < decision.Qty {
			d.logf("hand-out: %s — drawing the rest the old way", fast)
		}
	}
	if !stopped && drawn < decision.Qty && (fast.Err != nil || countByID(bank.Items(), id) > 0) {
		if notes && countByID(bank.Inventory(), id) > unnotedBefore {
			// The note switch did not take and the draw came out as items: bank them and draw the
			// whole order again as notes.
			bankPackAtOpenBank(d, bank)
			held, drawn = 0, 0
		}
		if notes && !bank.NoteMode() {
			if err := bank.SetNoteMode(true, handoutWaiter{d}); err != nil {
				_ = bank.Close()
				return declinedResult("could not switch the bank to notes")
			}
		}
		var more int
		more, stopped = withdrawExactly(d, bank, id, carry, decision.Qty-drawn)
		drawn += more
	}
	if !stopped && drawn < decision.Qty && freeInBankView(bank) == 0 {
		// Something landed in the pack after the check above (a withdrawal still in flight).
		// Bank it and draw the rest once, rather than declining an order the bank can fill.
		d.logf("hand-out: the pack filled up while drawing %d %s — banking it and drawing the rest", decision.Qty, decision.Item)
		bankPackAtOpenBank(d, bank)
		// Everything drawn so far went back with it, so the pack now holds none of the order.
		// Not re-read from PlayerPack: that view does not follow the pack while the bank is open.
		held = 0
		var more int
		more, stopped = withdrawExactly(d, bank, id, carry, decision.Qty)
		drawn = more
	}
	if carry != id && bank.NoteMode() {
		// Put the bank back as it was found: this mule does every other withdrawal un-noted,
		// and a note mode left on would silently change all of them. The fast draw already
		// switched it off in its own burst; this is for the old path.
		if err := bank.SetNoteMode(false, handoutWaiter{d}); err != nil {
			d.logf("hand-out: could not switch the bank back off notes: %v", err)
		}
	}
	_ = bank.Close()
	if stopped {
		return HandoutResult{Stopped: true}
	}
	if drawn < decision.Qty {
		return HandoutResult{Reason: "could not draw it out of the bank"}
	}
	// Re-read outside the bank interface: the withdrawal is only real if the account's own
	// ordinary pack shows it, the same way every proven withdrawal in this codebase confirms.
	if !waitFor(d, "the withdrawn hand-out to reach my pack", 5*time.Second, func() bool {
		return packCountOf(d, carry) >= held+decision.Qty
	}) {
		return HandoutResult{Reason: "the withdrawal never reached my pack"}
	}

	res := deliver(d, req.To, slot, carry, decision.Qty)
	res.Item = decision.Item
	return res
}

// blindHandout is a hand-out decided before the bank opens: the decision, what is drawn and what
// it arrives as, and the ops that ride behind the booth click.
type blindHandout struct {
	decision    orderplan.Decision
	id, carry   int32
	carryBefore int
	ops         []actions.BankOp
}

// planBlindHandout decides req from the bank as this mule last read it and plans the draw: the
// pack banked first when it has no room for the order (makeRoom's rule), note mode on for an order
// that must cross as notes, the withdrawal, note mode off. nil when there is no earlier reading or
// the order is not a plain fulfil from it: the paced path then decides from the bank as it opens.
func planBlindHandout(d *Deps, req HandoutRequest, now time.Time) *blindHandout {
	last := d.Session.State().Inventory(actions.BankWithdraw)
	if last == nil || d.Config == nil {
		return nil
	}
	rows := last.NonEmpty()
	decision := orderplan.Decide(now, req.To, req.Text, req.Policy, req.Ledger, func(item string) int {
		id, found := bankedID(d.Config, rows, item)
		if !found {
			return 0
		}
		return countByID(rows, id)
	})
	if decision.Verdict != orderplan.Fulfil || decision.Qty <= 0 {
		return nil
	}
	id, found := bankedID(d.Config, rows, decision.Item)
	if !found {
		return nil
	}
	b := &blindHandout{decision: decision, id: id, carry: id}
	stacks := false
	if obj, ok := d.Config.Obj(int(id)); ok && obj.Stackable {
		stacks = true
	}
	notes := mustUseNotes(decision.Qty, stacks)
	if notes {
		noted, ok := noteFor(d.Config, id)
		if !ok {
			return nil
		}
		b.carry = noted
	}
	pack := packItems(d)
	oneStack := stacks || notes
	free := actions.BackpackCapacity - len(pack)
	if !(oneStack && countByID(pack, b.carry) > 0) && free < handoutSlots(decision.Qty, oneStack) {
		seen := map[int32]bool{}
		for _, it := range pack {
			if !seen[it.ID] {
				seen[it.ID] = true
				b.ops = append(b.ops, actions.DepositAllOp(it.ID))
			}
		}
	} else {
		b.carryBefore = countByID(pack, b.carry)
	}
	if notes {
		b.ops = append(b.ops, actions.NoteModeOp(true))
	}
	b.ops = append(b.ops, actions.WithdrawAsOp(id, int32(decision.Qty), b.carry))
	if notes {
		b.ops = append(b.ops, actions.NoteModeOp(false))
	}
	return b
}

// withdrawExactly draws exactly qty of id, waiting for the bank's own side-panel view to show it.
//
// A single unit — every order the default catalogue can produce (orderplan.DefaultCatalog is one
// of each) — is one Withdraw-1 click, exactly as before. Withdraw-All is never used: it would empty
// a bank row of however many the fleet has accumulated, and "never hand out an unbounded quantity"
// is the whole point of the decision layer above.
//
// More than one unit goes through Withdraw-X with the exact count (added 2026-09-19 for the
// opt-in Scarab Swarm kit catalogue: runes and coins come in hundreds, and the old Withdraw-1 loop
// was bounded at withdrawGuard=30 clicks, so any order above 30 failed with "could not draw it out
// of the bank"). The count dialog is answered with exactly qty, and the side panel must then show
// the full amount; anything short falls back to Withdraw-1 clicks for the remainder, still bounded
// by withdrawGuard. A mule whose catalogue caps every item at one unit (the live default) never
// reaches this branch.
// drawID is the row CLICKED in the bank; countID is what is expected to ARRIVE. They differ
// only when the bank is in note mode: a bank stores the un-noted form, so a noted hand-out
// still clicks the ordinary row and receives the note.
//
// Getting this wrong is silent, and it was: passing the NOTED id as the row to click made
// WithdrawX target a row the bank does not have, so nothing was drawn, the wait for it timed
// out, the single-click fallback failed the same way, and the order came back to the donor as
// "could not draw it out of the bank" (live 2026-09-21, a 400-key order).
func withdrawExactly(d *Deps, bank *lockedBank, drawID, countID int32, qty int) (drawn int, stopped bool) {
	start := countByID(bank.Inventory(), countID)
	if qty > 1 {
		if err := bank.WithdrawX(drawID, int32(qty), handoutWaiter{d}); err != nil {
			d.logf("hand-out: withdraw-x %d of id=%d: %v — falling back to single clicks", qty, drawID, err)
		}
		waitFor(d, "the bank to hand over the count", 5*time.Second, func() bool {
			return countByID(bank.Inventory(), countID) >= start+qty
		})
		drawn = countByID(bank.Inventory(), countID) - start
		if drawn < 0 {
			drawn = 0
		}
		if drawn >= qty {
			return drawn, false
		}
	}
	for guard := 0; guard < withdrawGuard && drawn < qty; guard++ {
		if err := bank.WithdrawOne(drawID); err != nil {
			d.logf("hand-out: withdraw id=%d: %v", drawID, err)
			return drawn, false
		}
		want := start + drawn + 1
		if !waitFor(d, "the bank to hand over one", 5*time.Second, func() bool {
			return countByID(bank.Inventory(), countID) >= want
		}) {
			return drawn, false
		}
		drawn++
		if !d.Delay(200 * time.Millisecond) {
			return drawn, true
		}
	}
	return drawn, false
}

// handoutSlots is how many of the mule's own pack slots a hand-out of qty occupies once drawn:
// one when it stacks (a stackable item, or bank notes), one per unit otherwise.
func handoutSlots(qty int, oneStack bool) int {
	if oneStack || qty < 1 {
		return 1
	}
	return qty
}

// freeInBankView is the pack's free slots as the open bank's side panel shows them — the view
// that follows the pack while the bank is open (see countByName's doc comment in cook.go).
func freeInBankView(bank *lockedBank) int {
	free := actions.BackpackCapacity - len(bank.Inventory())
	if free < 0 {
		return 0
	}
	return free
}

// makeRoom banks the pack through the open bank when it cannot take qty more of carry, and
// reports how many stacks went. A pack already holding carry as one stack (oneStack) needs no new
// slot for more of it; that case is left alone so a mule carrying its own coins keeps them.
func makeRoom(d *Deps, bank *lockedBank, carry int32, qty int, oneStack bool) int {
	if oneStack && countByID(bank.Inventory(), carry) > 0 {
		return 0
	}
	if freeInBankView(bank) >= handoutSlots(qty, oneStack) {
		return 0
	}
	n := bankPackAtOpenBank(d, bank)
	if n > 0 {
		d.logf("hand-out: banked %d stack(s) the pack was holding, to make room for the hand-out", n)
	}
	return n
}

// handoutWaiter adapts a mulerunner Deps' Delayer to actions.Waiter, for the Withdraw-X dialog.
type handoutWaiter struct{ d *Deps }

func (w handoutWaiter) Delay(t time.Duration) bool { return w.d.Delay(t) }
func (w handoutWaiter) DelayUntil(cond func() bool, timeout time.Duration) bool {
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
func (w handoutWaiter) DelayTicks(n int) bool {
	return w.d.Delay(time.Duration(n) * 600 * time.Millisecond)
}

// partnerLacksRoom reports whether this account's own game messages since chatFrom include
// content's "Other player doesn't have enough inventory space for this trade" — the refusal
// trade.rs2 prints to the GIVER when the receiver's pack cannot hold the offer, just before it
// closes both screens. See orderplan.TradeSpaceRefusal.
func partnerLacksRoom(d *Deps, chatFrom int) bool {
	if d.Chat == nil {
		return false
	}
	lines, _ := d.Chat.ReadAfter(chatFrom, time.Time{})
	for _, line := range lines {
		if partner, _ := orderplan.TradeSpaceRefusal(line); partner {
			return true
		}
	}
	return false
}

// deliver runs the trade itself, with the donor already resolved and the items already held.
func deliver(d *Deps, to string, slot int32, id int32, qty int) HandoutResult {
	trade := actions.NewTrade(d.Session)
	chatFrom := 0
	if d.Chat != nil {
		chatFrom = d.Chat.Total()
	}
	before := packCountOf(d, id)
	if before < qty {
		return HandoutResult{Reason: "I am not holding what I drew"}
	}

	// The offer screen never opens from one side alone — the same mechanism this package's
	// own doc comment describes for incoming requests, seen from the other end: we send
	// Trade.Request and the donor must send one back before either side sees a screen.
	// donorrunner.Await is the half that answers this.
	deadline := time.Now().Add(handoutMeetTimeout)
	nextRequest := time.Time{}
	for !trade.Active() {
		if time.Now().After(deadline) {
			return declinedResult("you never opened the trade")
		}
		if randomevents.Busy(d.Session) {
			return HandoutResult{Reason: "a random event interrupted the hand-out"}
		}
		if time.Now().After(nextRequest) {
			if err := trade.Request(slot); err != nil {
				d.logf("hand-out: requesting a trade with %q: %v", to, err)
			}
			nextRequest = time.Now().Add(handoutRequestEvery)
		}
		if !d.Delay(300 * time.Millisecond) {
			return HandoutResult{Stopped: true}
		}
	}

	// Same stale-header hazard completeTrade documents at length: Trade.Partner() reads a
	// component text nothing clears when the interface closes, so it can still name the
	// PREVIOUS partner for a moment. Wait for it to actually say who we asked before trusting
	// it, then refuse if it never does — in this direction a wrong partner means giving items
	// to the wrong account, which is strictly worse than a wasted round.
	waitFor(d, "the trade partner name", 3*time.Second, func() bool {
		return orderplan.NamesMatch(trade.Partner(), to)
	})
	if !orderplan.NamesMatch(trade.Partner(), to) {
		d.logf("hand-out: the trade screen says %q, not %q — declining", trade.Partner(), to)
		_ = trade.Decline(3 * time.Second)
		return HandoutResult{Reason: "the trade screen named someone else"}
	}

	if !waitFor(d, "the trade offer screen", 10*time.Second, trade.OnOfferScreen) {
		_ = trade.Decline(3 * time.Second)
		return HandoutResult{Reason: "the offer screen never opened"}
	}
	// Offer-All only when the pack holds exactly the hand-out; with more of a stackable (coins
	// the mule was already carrying) Offer-X puts exactly qty on the screen, since the confirm
	// check below and the donor both refuse any other amount.
	offer := func() error { return trade.OfferAll(id) }
	if packCountOf(d, id) > qty {
		offer = func() error { return trade.Offer(id, int32(qty), 3*time.Second) }
	}
	if err := offer(); err != nil {
		d.logf("hand-out: offering id=%d: %v", id, err)
		_ = trade.Decline(3 * time.Second)
		return HandoutResult{Reason: "could not place the item on the screen"}
	}
	if !waitFor(d, "my own offer to register", 10*time.Second, func() bool {
		return countByID(trade.MyOffer(), id) >= qty
	}) {
		_ = trade.Decline(3 * time.Second)
		return HandoutResult{Reason: "my offer never registered"}
	}
	// A hand-out is a pure give. Anything on the donor's side means this is not the trade
	// either of us intended, and the safe reading of "not what I intended" is to walk away.
	if len(trade.TheirOffer()) > 0 {
		d.logf("hand-out: %q put items on a hand-out screen — declining", to)
		_ = trade.Decline(3 * time.Second)
		return HandoutResult{Reason: "you offered me something on a hand-out"}
	}
	if err := trade.Accept(); err != nil {
		d.logf("hand-out: accept (offer screen): %v", err)
		return HandoutResult{Reason: "could not accept the offer screen"}
	}

	confirmDeadline := time.Now().Add(30 * time.Second)
	for !trade.OnConfirmScreen() {
		if randomevents.Busy(d.Session) {
			_ = trade.Decline(3 * time.Second)
			return HandoutResult{Reason: "a random event interrupted the hand-out"}
		}
		if !time.Now().Before(confirmDeadline) {
			_ = trade.Decline(3 * time.Second)
			return HandoutResult{Reason: "you never reached the confirm screen"}
		}
		if !d.Delay(250 * time.Millisecond) {
			return HandoutResult{Stopped: true}
		}
		if !trade.Active() && stillClosed(d, trade.Active) {
			// stillClosed is the same debounce completeTrade uses in the other
			// direction — see debounceWindow's own doc comment for the live evidence
			// that a single false reading here is usually an interface refresh, not a
			// real close, and that reacting to it threw away good trades.
			//
			// When content closed it because the DONOR's pack cannot hold the offer, say so:
			// that close used to read exactly like any other, and it was the most common one.
			if partnerLacksRoom(d, chatFrom) {
				d.logf("hand-out: %q has no room in its pack for %d x id=%d — the trade screen refused it", to, qty, id)
				return declinedResult(orderplan.DeclineNoRoom)
			}
			return HandoutResult{Reason: "the trade closed before the confirm screen"}
		}
	}

	mine, theirs := trade.ConfirmOffers()
	if countByID(mine, id) != qty || len(theirs) != 0 || !orderplan.NamesMatch(trade.Partner(), to) {
		d.logf("hand-out: the confirm screen does not match what I offered %q (mine=%d theirs=%d partner=%q) — declining",
			to, countByID(mine, id), len(theirs), trade.Partner())
		_ = trade.Decline(3 * time.Second)
		return HandoutResult{Reason: "the confirm screen did not match"}
	}
	if err := trade.Accept(); err != nil {
		d.logf("hand-out: accept (confirm screen): %v", err)
		return HandoutResult{Reason: "could not accept the confirm screen"}
	}
	if !waitFor(d, "the hand-out trade to close", 20*time.Second, func() bool { return !trade.Active() }) {
		return HandoutResult{Reason: "the trade never closed"}
	}
	// The receipt rule, mirrored. A cancelled confirm screen also closes, so the close proves
	// nothing on its own; the items having genuinely left this account's own pack does.
	if !waitFor(d, "the hand-out to leave my pack", 8*time.Second, func() bool {
		return packCountOf(d, id) <= before-qty
	}) {
		d.logf("hand-out: the trade with %q closed but the items are still in my pack", to)
		return HandoutResult{Reason: "the trade closed without the items leaving"}
	}
	d.logf("HAND-OUT DELIVERED: %d x id=%d to %q", qty, id, to)
	return HandoutResult{Delivered: true, Crossed: qty}
}
