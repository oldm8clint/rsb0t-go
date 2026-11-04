package donorrunner

// The donor's side of a mule hand-OUT: asking for an item it is blocked without, and RECEIVING
// it. Everything else in this package runs the other way — donorplan.DecideTrade's own doc
// comment is explicit that "the donor only gives, and only to its mule," declining any screen
// where the partner offers anything — so a donor had no way at all to take something from the
// mule, and no way to ask for one.
//
// The asymmetry this closes, spelled out because it is the whole reason this file exists: today
// the mule only ever reciprocates a trade a donor started, and the donor only ever starts one.
// For a hand-out both reverse. The mule's half is mulerunner.Handout; this is the donor's. The
// mechanism itself is unchanged and needs no new primitive — the offer screen still opens only
// when BOTH sides have sent Trade.Request, which is exactly why this loop watches the chat log
// for the mule's own "<name>:tradereq:" line (mulerunner.TradeRequestFrom, reused rather than
// re-derived) and answers it.
//
// Bounded on purpose. Await returns within its own timeout whether the mule answers, declines,
// or says nothing at all, so a blocked script gets a clear answer and falls back to the
// recovery it would have run anyway (for ProgressiveFisher, PhaseFund's pickpocketing). Asking
// must never be able to make a stuck account MORE stuck than it already was.

import (
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/mulerunner"
	"rsb0t-go/internal/scripts/orderplan"
	"rsb0t-go/internal/scripts/tile"
)

// AwaitOptions is one ask.
type AwaitOptions struct {
	// Mule is who to ask; Me is this account's own name, which the bus carries as the
	// sender and which the mule matches against its allow list.
	Mule string
	Me   string
	// Item is the canonical item name and Qty how many. The mule may hand over fewer (its
	// own catalogue caps it); Await treats any of them arriving as success, since one net
	// unblocks a fisher just as well as three.
	Item string
	Qty  int
	// Bus is the shared wall bus. nil makes Await a no-op refusal rather than a panic — a
	// fleet configured without a bus simply has no hand-outs.
	Bus *mulebus.Bus
	// Timeout is the whole ask's budget, from publishing to holding the item.
	Timeout time.Duration
	// AskAnyway skips the remembered-refusal check below, for a caller that has a LIVE reading
	// of the other side's bank and therefore knows better than a remembered answer does.
	//
	// peerrunner.Ask is the case it exists for: it chooses a holder out of the peerbank
	// directory precisely because that holder published the item, and it has already walked to
	// it by the time Await runs. Every other caller asks blind and wants the memory.
	AskAnyway bool
	// Memory overrides the shared Refusals memory, for a test. nil uses Refusals.
	Memory *donorplan.RefusalMemory
}

// AwaitResult is what came of it.
type AwaitResult struct {
	// Got is true only once this account's own pack is confirmed to hold the item — never
	// merely because a trade closed.
	Got bool
	// Declined is true when the mule answered "no". Distinct from a silent timeout so a
	// caller can tell "there is no point asking again soon" from "nobody was listening".
	Declined bool
	Reason   string
	Stopped  bool
}

// AskRepeatEvery re-publishes the order while waiting. One publish is enough for a mule that
// is already running (mulebus.Watcher is checkpoint-based and never misses a message between
// two polls), so this exists purely for the case where the mule reconnects or starts up during
// the wait and its own watcher begins from a later checkpoint.
const AskRepeatEvery = 10 * time.Second

// DefaultAskTimeout is how long a blocked script waits for the mule before falling back to its
// own recovery. Comfortably longer than the mule's own handoutMeetTimeout plus a bank trip, and
// far shorter than the many minutes the alternative recovery (walking off to pickpocket a new
// tool) costs — so asking is close to free even when the answer never comes.
const DefaultAskTimeout = 90 * time.Second

// Refusals is the fleet's shared memory of what each mule has lately said it cannot or will not
// give. One per process, reached by every asking script, because a mule's stock is a fleet-wide
// fact rather than one account's discovery.
//
// THE FLEET OWNER'S REPORT, 2026-09-22: "accounts are still requesting items the mule does not
// have". Await is the single point every ask in this repo goes through — ProgressiveFisher,
// LootChestFisher, ScarabSwarm, ProgressiveWoodcutter and peerrunner all call it — so the memory
// belongs here rather than in each of them.
//
// WHAT THIS DOES AND DOES NOT CHANGE for a caller that has not been touched, because every one
// of them is somebody else's: a suppressed ask returns EXACTLY the answer the mule itself last
// gave — Declined, with the mule's own words, verbatim and unreworded — only without the bus
// round trip, the trade wait and the up-to-90-second budget. Every caller already handles
// Declined, and every caller that parses a reason (lootchestfisherplan.ParseBankedCount,
// woodcutplan.MuleDeclineIsEmptyStock) still parses the same string. What is removed is the
// asking, not the answer.
//
// It is bounded in both directions: an entry lapses after donorplan.RefusalCooldown, and any
// actual delivery of that item by that mule clears it immediately (see the Got path below), so
// a restocked mule is never shut out by an answer it gave half an hour ago.
var Refusals = donorplan.NewRefusalMemory()

// nonNotedID resolves a canonical item name to the ordinary (un-noted) object id — donorrunner's
// own copy of mulerunner.nonNotedID, duplicated per this project's established per-package
// convention (see normaliseTargetName's own doc comment for the same choice made for the same
// reason). Both sides must agree on which id an item name means or the receipt check below would
// pass on a bank note the account cannot fish with.
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

func countOfID(items []gamestate.SlotItem, id int32) int {
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

// unnotedIDs is every un-noted object id a name resolves to. Usually one; "Coins" names two in
// this config (617 and 995), and counting only the first missed every coin a mule handed over —
// found live 2026-09-19 with the opt-in Scarab kit catalogue: the donor never saw its coins
// arrive, kept re-publishing the order every AskRepeatEvery, and the mule filled it twice.
// receiptIDs is every id the item can ARRIVE as: the unnoted forms and the noted ones. Used
// only to answer "did it turn up", never to decide what to ask for.
func receiptIDs(cfg *cachecfg.Config, name string) []int32 {
	if cfg == nil {
		return nil
	}
	var out []int32
	for _, id := range cfg.ObjIDs(name) {
		out = append(out, int32(id))
	}
	return out
}

func unnotedIDs(cfg *cachecfg.Config, name string) []int32 {
	if cfg == nil {
		return nil
	}
	var out []int32
	for _, id := range cfg.ObjIDs(name) {
		if obj, ok := cfg.Obj(id); ok && !obj.Noted() {
			out = append(out, int32(id))
		}
	}
	return out
}

func countOfIDs(items []gamestate.SlotItem, ids []int32) int {
	n := 0
	for _, id := range ids {
		n += countOfID(items, id)
	}
	return n
}

// PlayerPack is the persistent backpack component id, the same constant every sibling runner
// package already establishes (fisherrunner/crafterrunner/mulerunner/vialrunner all declare
// their own copy, per this project's convention of small per-package duplication).
//
// Why the receipt reads THIS and not actions.Equipment.Inventory(): Equipment resolves the
// backpack through sidebar tab 3, and content takes that tab away in several ordinary events.
// `~macro_events_set_tabs` (macro_events.rs2:218) is `if_settab(null, ...)` for tabs 0-6, and so
// are `~set_sail` (interface_boat/scripts/sail.rs2) and `~set_sail_cairn`/captain_shanks — so
// every ship crossing a donor makes blinds that view too, not only a mime. A crossing gives the
// tab back at `~initalltabs` before the arrival telejump, so its window is the route's own
// `p_delay` rather than the whole voyage; that does not help here, because a receipt is one read
// at one moment and a window of a few ticks is enough to make it wrong. The server sends
// IF_SETICON 65535, gamestate
// stores -1, and Equipment.Inventory() then returns nil: not "the pack is empty" but "this
// client cannot see the pack".
//
// Since actions.Equipment now falls back to this same fixed component for reads (see
// actions.PlayerPack), Equipment.Inventory() no longer returns nil in that state and the two
// agree. The read stays as it is: it is the direct expression of what the receipt needs, and
// it holds whether or not internal/actions keeps that fallback.
//
// The fixed component keeps working throughout, and that is a property of the engine rather
// than an accident: Player.setTab (engine/src/engine/entity/Player.ts:2121) only reassigns
// this.tabs[tab] and writes IF_SETTAB. It never calls invStopListenOnCom, so the listener
// `inv_transmit(inv, inventory:inv)` registered in ~initalltabs stays registered and
// UPDATE_INV keeps flowing to 3214 for the whole event.
//
// This matters here more than anywhere else in the package because heldOf is the RECEIPT.
// Await's whole contract is that only a real gain in this account's own pack proves a
// hand-out landed; a blind read that returns 0 breaks that in both directions — a baseline
// of 0 taken while blinded, compared against a real count once the tabs come back, passes
// waitForHeld without the mule ever handing anything over.
const PlayerPack = 3214

// packItems is the account's own pack through the fixed component, nil-safe.
func packItems(d *Deps) []gamestate.SlotItem {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return nil
	}
	return inv.NonEmpty()
}

// heldOf counts every id of the item in the account's own pack.
func heldOf(d *Deps, ids []int32) int {
	return countOfIDs(packItems(d), ids)
}

// askedNames says which asked line a trade-screen obj answers. A plain asked name is matched the way
// it always was, by the obj's display name, so this holds nothing for it. A QUALIFIED asked name
// ("Yew shortbow id68", cachecfg.QualifiedName) names one obj and its bank note out of several that
// share a display name (a strung bow and its unstrung form carry one): the trade screen names every
// one of them "Yew shortbow", so a row is matched to the line by OBJ ID instead, and read as that
// line. Without this the screen of a kit that was asked for the unstrung bows and was handed exactly
// them read "offered something I did not ask for", and was declined.
type askedNames map[int32]string

// newAskedNames reads the qualified names among asked into the obj ids they stand for.
func newAskedNames(cfg *cachecfg.Config, asked ...string) askedNames {
	if cfg == nil {
		return nil
	}
	var out askedNames
	for _, name := range asked {
		if _, _, qualified := cachecfg.SplitQualified(name); !qualified {
			continue
		}
		for _, id := range cfg.ObjIDs(name) {
			if out == nil {
				out = askedNames{}
			}
			out[int32(id)] = strings.TrimSpace(name)
		}
	}
	return out
}

// handoutRows turns trade-screen slots into the named rows orderplan reasons about — the
// name/id boundary this package already owns (see Deps' own doc comment: the caller resolves
// names, the pure layer never sees an id). An obj that answers a qualified asked name (names)
// carries that spelling; every other is named by its display name, as ever.
func handoutRows(d *Deps, items []gamestate.SlotItem, names askedNames) []orderplan.ItemCount {
	out := make([]orderplan.ItemCount, 0, len(items))
	for _, it := range items {
		name := ""
		if obj, ok := d.Config.Obj(int(it.ID)); ok {
			name = obj.Name
		}
		if asked, ok := names[it.ID]; ok {
			name = asked
		}
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		out = append(out, orderplan.ItemCount{Name: name, Count: c})
	}
	return out
}

// Await publishes an order for o.Item and waits for the mule to hand it over, returning as soon
// as the item is genuinely in the pack, the mule declines, or the budget runs out.
func Await(d *Deps, o AwaitOptions) AwaitResult {
	if o.Bus == nil {
		return AwaitResult{Reason: "no wall bus configured"}
	}
	if o.Mule == "" || o.Me == "" || o.Item == "" {
		return AwaitResult{Reason: "nothing to ask for"}
	}
	qty := o.Qty
	if qty < 1 {
		qty = 1
	}
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = DefaultAskTimeout
	}
	// DO NOT ASK FOR WHAT THIS MULE HAS JUST SAID IT DOES NOT HAVE. See Refusals above for the
	// owner's report and for why replaying the mule's own answer is not a behaviour change.
	//
	// Checked FIRST, before the object-config lookup below, because it is the cheapest check
	// here (one map read) and because it needs nothing from the session at all — which is also
	// what makes it directly testable without a cache archive or a live client.
	mem := o.Memory
	if mem == nil {
		mem = Refusals
	}
	if !o.AskAnyway {
		if reason, age, refused := mem.Refused(time.Now(), o.Mule, o.Item); refused {
			d.logf("donor: not asking %q for %s — it said %q %s ago, and that answer does not depend on how many I ask for",
				o.Mule, o.Item, reason, age.Round(time.Second))
			return AwaitResult{Declined: true, Reason: reason}
		}
	}

	if _, ok := nonNotedID(d.Config, o.Item); !ok {
		return AwaitResult{Reason: "I cannot identify " + o.Item}
	}
	// THE RECEIPT MUST COUNT NOTES TOO, or a hand-out big enough to travel as notes can never
	// be seen to arrive.
	//
	// unnotedIDs exists for a real reason (its own comment): Coins have two unnoted ids and
	// counting the wrong one made the donor re-publish and the mule fill the order twice. But
	// using it for the RECEIPT means the donor is blind to exactly the hand-outs the mule
	// decides to note — mulerunner.mustUseNotes sends anything over 28 of a non-stackable item
	// as bank notes, which is most real food orders.
	//
	// Live, 2026-09-22, with the mule holding 91,380 cakes:
	//   [mule] hand-out: 50 Cake is more than a pack holds — drawing it as bank notes
	//   [cake]   donor: the hand-out trade closed but nothing arrived in my pack
	// The cakes did arrive. The donor was counting a form they could not be in. This is the
	// same defect COLLAB records for the Crystal keys, where every order above 28 came back as
	// "the trade closed with nothing arriving" while the notes sat in the pack.
	ids := receiptIDs(d.Config, o.Item)
	held := heldOf(d, ids)

	// ASK FOR NO MORE THAN THIS PACK CAN RECEIVE. See orderplan.FitAskToPack: content refuses a
	// trade whose offer does not fit the RECEIVER's free slots and closes both screens, and only
	// this side can see its own pack. Checked before publishing, so a full pack costs the mule
	// nothing -- no bank trip, no trade, no "closed before the confirm screen".
	if fit, readable := fitAsk(d, o.Item, qty); readable {
		if fit < 1 {
			d.logf("donor: not asking %q for %d %s: no room in my pack for it", o.Mule, qty, o.Item)
			return AwaitResult{Reason: noRoomReason}
		}
		if fit < qty {
			d.logf("donor: asking %q for %d %s, not %d: that is all my pack has room for", o.Mule, fit, o.Item, qty)
			qty = fit
		}
	}

	// Watch before publishing, or a mule quick enough to decline within the same moment
	// could answer into a window nothing is listening to.
	watcher := mulebus.NewWatcher()
	text := orderplan.Format(o.Item, qty)
	publish := func() {
		if here, located := currentTile(d); located {
			o.Bus.Publish(mulebus.HereMessage(o.Me, tile.Tile{X: here.X, Z: here.Z, Level: here.Level}))
		}
		o.Bus.Publish(mulebus.OrderMessage(o.Me, text, o.Mule))
	}
	publish()
	d.logf("donor: asked %q over the wall bus for %s", o.Mule, text)

	trade := actions.NewTrade(d.Session)
	want := orderplan.Order{Item: o.Item, Qty: qty}
	names := newAskedNames(d.Config, o.Item)
	deadline := time.Now().Add(timeout)
	nextPublish := time.Now().Add(AskRepeatEvery)
	chatCursor := 0
	if d.Chat != nil {
		chatCursor = d.Chat.Total()
	}
	for time.Now().Before(deadline) {
		// A declined order ends the wait immediately: the whole point of the mule
		// answering rather than staying silent is that a blocked script gets on with its
		// own recovery instead of standing still for the full budget.
		for _, msg := range watcher.Poll(o.Bus) {
			if msg.Kind != mulebus.KindDeclined || !mulebus.NamesMatch(msg.From, o.Mule) {
				continue
			}
			if !mulebus.AddressedTo(msg, o.Me) {
				continue
			}
			d.logf("donor: %q declined the order: %s", o.Mule, msg.Reason)
			// Remembered only when the reason is about the ITEM — a positional refusal or
			// this donor's own per-window cap says nothing about the bank, and
			// donorplan.ClassifyRefusal is what tells them apart.
			if mem.Record(time.Now(), o.Mule, o.Item, msg.Reason, donorplan.RefusalCooldown) {
				d.logf("donor: remembering that %q has no %s for the next %s",
					o.Mule, o.Item, donorplan.RefusalCooldown)
			}
			return AwaitResult{Declined: true, Reason: msg.Reason}
		}

		if trade.Active() {
			res := receive(d, trade, o.Mule, want, names, ids, held)
			if res.Stopped || res.Got || res.Declined {
				if res.Got {
					// Evidence beats memory: a mule that has just handed one over
					// plainly has them, whatever it said earlier.
					mem.Clear(o.Mule, o.Item)
				}
				return res
			}
			// A refused or broken screen is not the end of the ask — the mule retries
			// its own request, and the budget above is what actually ends this.
			continue
		}

		// The offer screen opens only once both sides have asked. Answer the mule's own
		// request rather than requesting blindly, so this can never open a trade with a
		// mule that is busy with somebody else.
		if d.Chat != nil {
			lines, next := d.Chat.ReadAfter(chatCursor, time.Time{})
			chatCursor = next
			for _, line := range lines {
				name, isReq := mulerunner.TradeRequestFrom(line)
				if !isReq || !orderplan.NamesMatch(name, o.Mule) {
					continue
				}
				if slot, found := findMuleSlot(d, o.Mule); found {
					d.logf("donor: %q is opening a trade — answering", o.Mule)
					if err := trade.Request(slot); err != nil {
						d.logf("donor: answering %q's trade request: %v", o.Mule, err)
					}
				} else {
					d.logf("donor: %q asked to trade but is not resolvable to a player index yet", o.Mule)
				}
			}
		}

		if time.Now().After(nextPublish) {
			publish()
			nextPublish = time.Now().Add(AskRepeatEvery)
		}
		if !d.Delay(400 * time.Millisecond) {
			return AwaitResult{Stopped: true, Reason: "stopped while waiting for the mule"}
		}
	}
	return AwaitResult{Reason: "the mule never handed it over"}
}

// noRoomReason is Await's reason when this account's own pack cannot receive what it would ask
// for. Never published and never remembered as the mule's answer: it is about this pack.
const noRoomReason = "no room in my pack"

// fitAsk sizes an ask of qty item to this account's free pack slots (orderplan.FitAskToPack),
// reading the pack through the fixed PlayerPack component. readable is false when the pack has
// not been read at all, and then the ask is left alone: an unreadable pack is not an empty one.
func fitAsk(d *Deps, item string, qty int) (fit int, readable bool) {
	if d.Session == nil || d.Config == nil {
		return qty, false
	}
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return qty, false
	}
	stackable := false
	if id, ok := nonNotedID(d.Config, item); ok {
		if obj, found := d.Config.Obj(int(id)); found {
			stackable = obj.Stackable
		}
	}
	var noted []int32
	for _, id := range receiptIDs(d.Config, item) {
		if obj, ok := d.Config.Obj(int(id)); ok && obj.Noted() {
			noted = append(noted, id)
		}
	}
	return fitFromPack(inv.NonEmpty(), qty, stackable, unnotedIDs(d.Config, item), noted), true
}

// fitFromPack is fitAsk's pure core over a pack that was read: its free slots, and whether it
// already holds the item (unnoted ids) or its note (noted ids).
func fitFromPack(pack []gamestate.SlotItem, qty int, stackable bool, unnoted, noted []int32) int {
	free := actions.BackpackCapacity - len(pack)
	return orderplan.FitAskToPack(qty, stackable, countOfIDs(pack, unnoted) > 0, countOfIDs(pack, noted) > 0, free)
}

// receive drives an already-open hand-out trade screen through orderplan.DecideReceive until it
// closes, and then proves the item actually arrived. heldBefore is what the pack held before the
// ask, so the check is a real delta rather than "the account has one now" (which would pass
// spuriously if the item had been there all along).
func receive(d *Deps, trade *actions.Trade, mule string, want orderplan.Order, names askedNames, ids []int32, heldBefore int) AwaitResult {
	deadline := time.Now().Add(60 * time.Second)
	chatFrom := 0
	if d.Chat != nil {
		chatFrom = d.Chat.Total()
	}
	accepted := false
	// confirmed is what the confirm screen showed crossing; the receipt waits for all of it
	// (500 coins, not merely one more than before).
	confirmed := 0
	lastScreen := orderplan.ScreenNone
	for {
		if !trade.Active() {
			closed, stopped := stillClosed(d, trade)
			if stopped {
				return AwaitResult{Stopped: true}
			}
			if !closed {
				continue
			}
			break
		}
		if randomevents.Busy(d.Session) {
			_ = trade.Decline(3 * time.Second)
			return AwaitResult{Reason: "a random event interrupted the hand-out"}
		}
		if time.Now().After(deadline) {
			d.logf("donor: the hand-out trade with %q ran too long — declining", mule)
			_ = trade.Decline(3 * time.Second)
			return AwaitResult{Reason: "the hand-out trade timed out"}
		}
		screen := orderplan.ScreenOffer
		if trade.OnConfirmScreen() {
			screen = orderplan.ScreenConfirm
		}
		if screen != lastScreen {
			// Accepting is per-screen: the server clears our acceptance on every screen
			// change, so carrying it across one would leave this waiting forever on a
			// confirm screen it never actually clicked. Same rule donorrunner's own
			// giving-side loop already follows.
			accepted = false
			lastScreen = screen
		}
		view := orderplan.ReceiveView{
			Screen:   screen,
			Partner:  trade.Partner(),
			Mule:     mule,
			Want:     want,
			Theirs:   handoutRows(d, trade.TheirOffer(), names),
			Mine:     handoutRows(d, trade.MyOffer(), names),
			Accepted: accepted,
		}
		if screen == orderplan.ScreenConfirm {
			mine, theirs := trade.ConfirmOffers()
			view.Mine, view.Theirs = handoutRows(d, mine, names), handoutRows(d, theirs, names)
			confirmed = 0
			for _, r := range view.Theirs {
				if strings.EqualFold(strings.TrimSpace(r.Name), strings.TrimSpace(want.Item)) {
					confirmed += r.Count
				}
			}
		}
		action, reason := orderplan.DecideReceive(view)
		switch action {
		case orderplan.ReceiveDecline:
			d.logf("donor: declining the hand-out screen with %q: %s", trade.Partner(), reason)
			_ = trade.Decline(3 * time.Second)
			return AwaitResult{Reason: reason}
		case orderplan.ReceiveAccept, orderplan.ReceiveConfirm:
			if err := trade.Accept(); err != nil {
				d.logf("donor: accepting the hand-out %v: %v", action, err)
			} else {
				accepted = true
			}
		case orderplan.ReceiveClosed:
			// Active() said otherwise a moment ago; let the loop re-read.
		}
		if !d.Delay(300 * time.Millisecond) {
			return AwaitResult{Stopped: true}
		}
	}

	// The receipt rule, the same one mulerunner.completeTrade applies in the other direction:
	// a cancelled confirm screen also closes, so a closed trade is not evidence of anything.
	// Only a real gain in this account's own pack is.
	if confirmed < 1 {
		confirmed = 1
	}
	// Content says so when it closed the screens because THIS pack cannot hold the offer, and
	// then nothing is coming: answer at once instead of spending the receipt wait on it.
	if iLackRoom(d, chatFrom) {
		d.logf("donor: the trade screen refused the hand-out from %q: my pack has no room for it", mule)
		return AwaitResult{Reason: noRoomReason}
	}
	if !waitForHeld(d, ids, heldBefore+confirmed, 8*time.Second) {
		d.logf("donor: the hand-out trade with %q closed but nothing arrived in my pack", mule)
		return AwaitResult{Reason: "the trade closed with nothing arriving"}
	}
	d.logf("donor: received %s from %q", want.Item, mule)
	return AwaitResult{Got: true}
}

// iLackRoom reports whether this account's game messages since chatFrom include content's
// "You don't have enough inventory space for this trade" (orderplan.TradeSpaceRefusal).
func iLackRoom(d *Deps, chatFrom int) bool {
	if d.Chat == nil {
		return false
	}
	lines, _ := d.Chat.ReadAfter(chatFrom, time.Time{})
	for _, line := range lines {
		if _, me := orderplan.TradeSpaceRefusal(line); me {
			return true
		}
	}
	return false
}

func waitForHeld(d *Deps, ids []int32, want int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if heldOf(d, ids) >= want {
			return true
		}
		if !d.Delay(250 * time.Millisecond) {
			return false
		}
	}
	return false
}

// DescribeAsk is the status line a script logs while asking.
func DescribeAsk(mule, item string) string {
	return "asking " + strings.TrimSpace(mule) + " for a " + strings.TrimSpace(item)
}

// RecentlyRefused reports whether mule has lately said it cannot or will not give item, and with
// what words. Await consults this itself, so a caller never HAS to — it exists for the decision
// Await cannot make on a caller's behalf: whether to make the TRIP.
//
// That is where the real cost is. The loop the fleet owner reported on 2026-09-22 was not a
// wasted 90-second wait, it was an 83-tile walk from the Seers bank to Catherby to ask four
// questions about axes that are not in the bank, repeated every two minutes at zero experience.
// Await can only decline the ask once the account is already standing there; only the calling
// script knows it is about to set off.
//
// Answers about ONE mule and ONE item, and only for the reasons that are about the item —
// positional refusals and this donor's own per-window cap are never remembered. See
// donorplan.ClassifyRefusal.
func RecentlyRefused(mule, item string) (reason string, refused bool) {
	r, _, ok := Refusals.Refused(time.Now(), mule, item)
	return r, ok
}
