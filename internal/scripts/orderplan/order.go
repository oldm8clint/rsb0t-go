// Package orderplan is the pure half of the mule hand-OUT: a donor script that is blocked
// on a missing item asks the mule for it over the wall bus (internal/mulebus's own
// KindOrder, which has existed unused since the bus was ported), and the mule decides
// whether to hand it over.
//
// Why this direction needed new code at all. Everything already built runs one way: a donor
// fills its pack, walks to the mule, opens the trade, and gives. internal/bot/mule.go's own
// doc comment states the gap plainly — it "never acts on a queued pickup — no travel, no
// trade initiated toward a donor" — and donorplan.DecideTrade is explicitly "the donor only
// gives, and only to its mule," declining any screen where the partner offers anything. So
// for a hand-out both halves reverse: the mule must initiate and give, the donor must accept
// and receive. Neither side had a decision function for that.
//
// The real incident this exists for (2026-09-18): production account `spare28` (Fishing 55, a
// genuinely valuable account) lost its Big fishing net with 0 coins and nothing sellable, so
// fisherplan.DecideFishPhase returned PhaseFund and it sat at the Catherby bank doing
// nothing for hours — while `mule` stood about two tiles away holding a bank full of both
// nets and coins. Two fixes that night (b306382, a53c927) let such an account walk off and
// pickpocket its way back to a net, which works but is slow and fragile. Asking the account
// standing next to it is the structural fix, and it generalises: any script blocked on any
// missing item can publish an order.
//
// Why its own package rather than a file in mulereceiverplan. The grammar is the one thing
// both sides must agree on byte for byte — the donor formats it, the mule parses it — so it
// cannot live in a package named for one side of the exchange. mulereceiverplan is already
// three unrelated concerns (teleport/trip planning, the received-items ledger, the settings
// form); a fourth that donorrunner and fisherplan would also have to import would make that
// worse, not better. Everything here is shared by both ends, which is exactly what earns a
// package of its own.
//
// It is pure: no client, actions, fleet or bot imports and no I/O, matching donorplan's and
// fisherplan's own package docs. Items are named by strings; resolving a name to an object id
// is the caller's job (see mulerunner/handout.go), deliberately, so the safety catalogue below
// can be reasoned about without a cache archive loaded.
package orderplan

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ---- the grammar ----

// Verb starts every order. A fixed leading keyword (rather than accepting bare "1 Harpoon")
// means an arbitrary chat-shaped string that happens to land on the bus can never be read as
// an order by accident.
const Verb = "need"

const (
	// QtyMax is the largest quantity the grammar itself will carry, before any catalogue or
	// per-window cap narrows it further. This is not the real limit — Handout.Max is — it
	// only stops a parse from producing an absurd number to reason about.
	//
	// It was 1000 until 2026-09-21, and that made it the real limit for exactly one item,
	// silently. Coins are handed out to fund a MARKET PURCHASE, and the purchase this fleet
	// actually makes is a rune axe at 150,000 -- so every coin order a woodcutter sent was
	// refused here, by the PARSER, with "quantity is too large", however high the catalogue's
	// own CoinsPerOrder was raised. Live evidence from the fleet log, with the mule announcing
	// a 170,000 per-order cap in the same run:
	//
	//     donor: asked "mule" over the wall bus for need 150000 Coins
	//     donor: "mule" declined the order: quantity is too large
	//
	// Raising it cannot over-hand anything: Prescreen CLAMPS an order to entry.Max and then to
	// the remaining window (qty, clamped = entry.Max, true), it does not fulfil what it was
	// asked for. So every item is still bounded by its own Handout.Max, and this bound goes
	// back to being what the comment above always said it was -- a guard against an absurd
	// number, not a policy. A million is roughly six rune axes: ample headroom for a purchase,
	// still an immediate refusal for a nonsense quantity.
	QtyMax = 1_000_000
	// ItemNameMax bounds a parsed item name. Real item names are far shorter; this exists so
	// a malformed order cannot carry a long string into a log line or a decline reason.
	ItemNameMax = 40
)

// Order is one parsed request: an item named exactly as the game names it, and how many.
type Order struct {
	Item string
	Qty  int
}

// Format renders an order for mulebus.OrderMessage's Text field. Parse(Format(item, qty))
// round-trips for every order Format accepts.
func Format(item string, qty int) string {
	return Verb + " " + strconv.Itoa(qty) + " " + strings.TrimSpace(item)
}

// ParseError is why an order text was rejected. Its message is deliberately short: it is
// published back to the donor as a mulebus.DeclinedMessage, whose Reason is truncated at
// mulebus.ReasonMax (80).
type ParseError string

func (e ParseError) Error() string { return string(e) }

// The parse failures, each its own value so a caller (and a test) can tell them apart rather
// than string-matching one generic error.
const (
	ErrEmpty       = ParseError("empty order")
	ErrNoVerb      = ParseError(`order must start with "` + Verb + `"`)
	ErrNoQty       = ParseError("order has no quantity")
	ErrBadQty      = ParseError("quantity is not a positive number")
	ErrQtyTooLarge = ParseError("quantity is too large")
	ErrNoItem      = ParseError("order names no item")
	ErrItemTooLong = ParseError("item name is too long")
	ErrItemChars   = ParseError("item name has characters an item name cannot have")
)

// fields splits on any run of spaces/tabs, tolerating the whitespace a hand-typed or
// truncated bus message might carry. Written out rather than using strings.Fields so the
// remainder (the item name) keeps its own single-spaced form.
func fields(s string) []string {
	var out []string
	for _, part := range strings.Split(strings.ReplaceAll(s, "\t", " "), " ") {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// itemNameOK reports whether every byte could appear in a real item name. Rejecting rather
// than sanitising is the point: the safety requirement is that an unknown or malformed name
// is declined, never guessed at, so anything outside this set fails the parse instead of
// being stripped down into something that might accidentally match the catalogue.
func itemNameOK(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		// Parentheses carry charges and doses in content's own names ("Games necklace(8)", "Ring of
		// dueling(3)", "Waterskin(4)"): the run staller's jewellery ask was declined for them (live
		// 6 Oct 2026, "Games necklace(8): item name has characters an item name cannot have").
		case c == ' ' || c == '\'' || c == '-' || c == '(' || c == ')':
		default:
			return false
		}
	}
	return true
}

// Parse reads an order text. It never guesses: an unparseable text is an error the caller
// declines with, not a best-effort reading.
func Parse(text string) (Order, error) {
	parts := fields(strings.TrimSpace(text))
	if len(parts) == 0 {
		return Order{}, ErrEmpty
	}
	if !strings.EqualFold(parts[0], Verb) {
		return Order{}, ErrNoVerb
	}
	if len(parts) < 2 {
		return Order{}, ErrNoQty
	}
	qty, err := strconv.Atoi(parts[1])
	if err != nil || qty <= 0 {
		return Order{}, ErrBadQty
	}
	if qty > QtyMax {
		return Order{}, ErrQtyTooLarge
	}
	if len(parts) < 3 {
		return Order{}, ErrNoItem
	}
	name := strings.Join(parts[2:], " ")
	if len(name) > ItemNameMax {
		return Order{}, ErrItemTooLong
	}
	if !itemNameOK(name) {
		return Order{}, ErrItemChars
	}
	return Order{Item: name, Qty: qty}, nil
}

// ---- why a decline happened ----

// DeclineNotInSight is the reason a mule gives when the donor is not visible to it.
//
// A hand-out is a TRADE, so the two accounts must be standing together; mulerunner.Handout
// resolves the donor's player slot before it draws anything, and declines outright when it
// cannot. The string lives here, in the package both sides already share, so the donor can
// recognise it by comparing against a constant instead of inventing a copy of the text.
const DeclineNotInSight = "you are not in sight of me"

// DeclineIsPositional reports whether a decline was about WHERE the donor was standing rather
// than about stock or policy.
//
// The distinction is not academic; it was measured on the production fleet 2026-09-21. A
// woodcutter asking from the Seers bank while the mule posts at Catherby, 83 tiles away, got
// "you are not in sight of me" for every tier in turn, and the asking script -- which steps
// down its axe ladder on any Declined -- read four positional failures as four scarcity
// answers and ended up asking for the WORST axe for a reason that had nothing to do with what
// the mule held (it held Mithril x6, Bronze x15, Iron x4). A positional decline says nothing
// about stock and must not be treated as if it does.
func DeclineIsPositional(reason string) bool {
	return strings.EqualFold(strings.TrimSpace(reason), DeclineNotInSight)
}

// ---- the safety catalogue ----

// Handout is one item the mule is willing to give away, and the most of it that may cross in
// a single order.
type Handout struct {
	// Item is the canonical spelling, as the game's own object config names it — what a
	// fulfilled decision reports, regardless of how the donor spelled it.
	Item string
	// Max is the per-request ceiling. An order above it is clamped down to it, never
	// refused outright (the donor is blocked and one net unblocks it just as well as five),
	// but it is never exceeded.
	Max int
	// WindowMax, when positive, replaces Policy.MaxUnitsPerItem for this item: the most of it
	// handed to everyone combined inside the ledger's window. Coins need it, since one order
	// is hundreds of units against a per-item cap sized for tools.
	WindowMax int
	// Floor, when positive, is how many of this item the mule must ALREADY hold before it
	// will hand any of it out at all. Below the floor every order for it is declined, with
	// the count said plainly, and the stock is left to build up.
	//
	// It exists for items whose value is in a BATCH rather than a unit. A crystal key is the
	// case it was added for (account owner, 2026-09-20): the account that spends them walks
	// Catherby -> Falador and back for one expedition, so going at all is only worth it when
	// there is a real stockpile to work through, and a trickle of two or three keys would
	// spend the whole round trip to open two or three chests. Unlike Max and WindowMax this
	// is not a safety bound — it does not limit what leaves the bank, it delays it.
	//
	// Deliberately enforced HERE, on the mule, rather than by the asking script: the mule is
	// the only side that can actually count its own bank, so a donor asking "how many have
	// you got?" would be answering from a reading it cannot take. A decline carrying the real
	// count is the same answer with no extra round trip and no window where the number is
	// stale.
	Floor int
	// FloorKeepsBack makes Floor a RESERVE as well as a gate: the mule keeps Floor of them
	// back on every order instead of only refusing below it.
	//
	// Off by default, because a gate is what Floor is documented above to be. Turn it on for
	// an item with many competing consumers, where the first asker through the gate would
	// otherwise drain the stock the gate exists to accumulate. Crystal keys are deliberately
	// NOT such an item: one account consumes them, and users want it to take the
	// whole stock and bring the proceeds back.
	FloorKeepsBack bool
	// FloorInclusive makes a gate open AT the floor: with it, a mule holding exactly Floor hands the item
	// out ("from 20,000 held"), where the older reading (crystal keys) declines at Floor and serves above it.
	// It means nothing with FloorKeepsBack, a reserve, which at the floor has nothing above it to give.
	FloorInclusive bool
}

// DefaultCatalog is what a mule hands out unless an operator configures otherwise: the
// fishing ladder's tools, one each.
//
// Deliberately narrow. The whole risk of this feature is that a message on a shared bus can
// move items out of a bank holding a ~255-account fleet's entire accumulated take, so the
// default is exactly the set that unblocks the incident this was built for and nothing else.
// Coins joined it on 2026-09-20, by the owner's decision, after 72 nets were lost to one
// whirlpool in five seconds and accounts short of the price sat at Harry's for minutes: at
// most CoinsPerOrder per order and CoinsPerWindow to everyone combined per window, so the
// worst case (a bug, or a compromised donor name) is bounded at CoinsPerWindow, not the bank.
func DefaultCatalog() []Handout {
	return append([]Handout{
		{Item: "Small fishing net", Max: 1},
		{Item: "Big fishing net", Max: 1},
		{Item: "Fishing rod", Max: 1},
		{Item: "Fly fishing rod", Max: 1},
		{Item: "Harpoon", Max: 1},
		{Item: "Lobster pot", Max: 1},
		// A knife is the fletching tool, not a weapon. Every new woodcutter needs one to turn
		// its logs into arrow shafts at the Draynor start camp, and until this was added the
		// only source in the whole script was two GROUND SPAWNS (woodcutplan.KnifeSpawns:
		// Lumbridge 3224,3202 and Seers 2704,3475). Two respawning items cannot supply a
		// cohort -- users asked for as many woodcutters as the Seers market has
		// rune axes, which is 94 -- so the mule hands them out like any other tool.
		//
		// One per account, like every other tool here: a knife is not consumed, so an account
		// that has one never needs a second, and a cap of 1 makes a runaway loop cost nothing.
		{Item: "Knife", Max: 1},
		{Item: "Coins", Max: CoinsPerOrder, WindowMax: CoinsPerWindow},
		{Item: "Crystal key", Max: KeysPerOrder, WindowMax: KeysPerWindow, Floor: KeysFloor},
	}, AxeHandouts()...)
}

// WithCoinLimits returns cat with the "Coins" entry's ceilings replaced, for a mule whose
// hand-out has to fund a PURCHASE rather than replace a lost tool.
//
// CoinsPerOrder's 500 is a ladder tool at stock price, and it is the right default: that
// constant's own comment records that the worst case from a bug or a compromised donor name is
// bounded at CoinsPerWindow rather than at the bank, and the production mule's bank holds about
// 29.6M coins. But the Seers market sells a rune axe for roughly 150,000, so against the default
// an order for it is clamped to 500 and the buyer walks to the market holding nothing — which is
// exactly what production woodcutters did on 2026-09-21, parking on "waiting for the mule to
// bring a rune axe" while settling for the mithril one the mule would give.
//
// So the ceiling moves per mule, from its own roster settings, rather than globally: a fleet that
// wants to fund market purchases says so in one visible place, and every other mule keeps the
// conservative bound. perOrder or perWindow <= 0 leaves that ceiling alone, so setting one of the
// pair without the other is meaningful.
//
// Returns a copy; cat is not modified. A catalogue with no "Coins" entry is returned unchanged
// rather than having one invented, because a mule deliberately configured not to hand out coins
// must not start doing so because somebody raised a limit.
func WithCoinLimits(cat []Handout, perOrder, perWindow int) []Handout {
	out := make([]Handout, len(cat))
	copy(out, cat)
	for i := range out {
		if !strings.EqualFold(out[i].Item, "Coins") {
			continue
		}
		if perOrder > 0 {
			out[i].Max = perOrder
		}
		if perWindow > 0 {
			out[i].WindowMax = perWindow
		}
	}
	return out
}

// AxeHandouts is the axe half of DefaultCatalog, named so a caller can ask "which axes will a
// mule part with, best first?" without the answer being hard-coded in two places.
//
// WHY IT EXISTS, measured on the PRODUCTION fleet 2026-09-21. Five accounts swapped from
// ProgressiveFisher to ProgressiveWoodcutter reached the Seers bank correctly and then parked on
// `waiting for the mule to bring a rune axe`: the only way that script could get an axe was the
// Seers PLAYER market, which says "buying 1 rune axe" out loud and waits for a human to sell it
// one, and `axePrice`/`axeBudget` both default to 0, where zero never buys. An unattended cohort
// could not start at all. The fleet already owned the axes — `mule`'s bank held Mithril x6,
// Bronze x15, Iron x4, Steel x1, Adamant x1 — there was simply no mechanism to move one.
//
// THIS IS A WEALTH-DISTRIBUTION CHANGE, not only a mechanism, so the choice is stated rather
// than slipped in. Bronze, iron, steel and mithril are here; **adamant and rune deliberately are
// NOT**, for the same shape of reason the crystal key's own note gives above:
//
//   - The fleet holds fifteen bronze, six mithril and four iron against ONE adamant and no rune
//     at all. Handing out a plentiful tier costs a tool; handing out the adamant costs the only
//     one there is.
//   - Leaving them out costs nothing in access. woodcutplan/axe.go's content research
//     established that axes have NO Woodcutting requirement on this server
//     (`woodcutting_axe_checker`'s level checks are commented out — "no wc req in 2004"), so
//     **any axe chops from Woodcutting 1**. A mithril axe puts a swapped account to work
//     immediately; the difference against rune is chopping speed, not whether it can start.
//   - An operator who wants the top two can add them. The reverse mistake cannot be undone.
//
// RUNE AND ADAMANT JOINED ON 2026-10-04, on the owner's word, and the first reason above is what
// changed: the mule now holds 45 Rune axes, bought for the woodcutters, and users asked for the
// ProgressiveWCFletch cohort to be supplied from them. They go first, so an axeless account asks
// for the best axe the mule holds; woodcutplan.MuleAxeLadder still drops any tier the mule's
// published bank shows none of, so a mule without them is never asked for one.
//
// No Floor, matching the fishing tools rather than the crystal key: an axe's value is in the
// unit, not the batch, and a floor would block exactly the case this exists for — a mule down to
// its last steel axe refusing the account that needs it. Max 1 because an account needs one axe,
// and Policy.MaxUnitsPerItem still bounds the window.
//
// Ordered BEST FIRST, which is the order a donor should ask in: it is the caller's fallback
// ladder, not just a set.
func AxeHandouts() []Handout {
	return []Handout{
		{Item: "Rune axe", Max: 1},
		{Item: "Adamant axe", Max: 1},
		{Item: "Mithril axe", Max: 1},
		{Item: "Steel axe", Max: 1},
		{Item: "Iron axe", Max: 1},
		{Item: "Bronze axe", Max: 1},
	}
}

// Crystal keys joined the catalogue on 2026-09-20, by the owner's decision, so the account
// running LootChestFisher can collect the keys the mule made out of the fleet's own key halves
// ("after getting the keys from mulereceiver and banking them for safe travel").
//
// Why this is a smaller exposure than it looks, stated plainly rather than assumed:
//   - A crystal key exists only because this mule made it (mulerunner.JoinKeyHalvesRound) out
//     of halves the fleet's own caskets produced. It is not part of the fleet's stored take the
//     way fish, ore or coins are; handing them all out and losing them costs the chest runs
//     they would have paid for and nothing else.
//   - The allow list still gates who may ask at all, exactly as it does for coins.
//   - KeysPerWindow bounds the worst case. At the drop table's own expected value (about 3,250
//     coins a key at high-alchemy prices, ~19,000 at the market's medians where they are not
//     one-trade outliers) that ceiling is roughly 180k-1.1M coins per window — comparable to
//     CoinsPerWindow, not to a bank.

// KeysFloor is how many crystal keys this mule must already hold before it hands any out.
// See Handout.Floor for why a batch floor exists at all; 100 is the account owner's own
// number (2026-09-20). Below it every key order is declined with the real count, the keys
// keep accumulating, and the asking account simply keeps fishing.
//
// It is a GATE, not a reserve (Handout.FloorKeepsBack is off for crystal keys): at or below
// 100 nothing crosses, and above it the WHOLE stock does, up to KeysPerOrder and the window
// cap. The owner's ruling of 2026-09-26, after the mule held 110 and handed over 10: "it should
// have taken 110 keys". There is one key consumer (spare104), so there is no later asker for a
// reserve to protect. lootchestfisherplan.Available mirrors this and must agree with it.
const KeysFloor = 100

// KeysPerOrder is the most crystal keys one order may take.
//
// This was 28 -- a full backpack -- because the hand-out could only pass UNNOTED keys, and 28
// is all a pack holds. Raised 2026-09-20 on the account owner's instruction, together with
// handing crystal keys over as BANK NOTES: notes stack, so a whole stock crosses in one slot
// and one trade carries what used to take fifteen. The keys become ordinary keys again simply
// by being banked, because a bank always stores the unnoted form.
//
// This IS a real increase in exposure, recorded rather than glossed over. The reasoning above
// still holds -- a crystal key exists only because this mule made it out of the fleet's own
// casket halves, so the worst case is the chest runs it would have paid for and not the
// fleet's stored take -- but that worst case is now ~18x larger. The allow list and KeysFloor
// are what keep it bounded, and KeysPerOrder/KeysPerWindow below are what cap the keys
// themselves -- QtyMax is only a guard against an absurd parse and does not narrow this.
const KeysPerOrder = 500

// KeysPerWindow is the most crystal keys handed to everyone combined per ledger window: two
// full orders. With notes that is 1000 rather than 56, which is far more than the fleet's
// casket-driven half supply produces in half an hour -- so in practice the real limit on a
// run stays the mule's actual stock, which is what KeysFloor gates.
const KeysPerWindow = 2 * KeysPerOrder

// CoinsPerOrder is the most coins one order may take: a ladder tool at any stock price.
const CoinsPerOrder = 500

// CoinsPerWindow is the most coins handed to everyone combined per ledger window: fifty
// orders, enough for a mass tool loss like 2026-09-20's, and a hard ceiling on the damage.
const CoinsPerWindow = 50 * CoinsPerOrder

// ---- handing out what the mule ACTUALLY holds ----

// OpenStock widens a mule past its curated Catalog: an order naming an item the catalogue never
// considered is served out of whatever the bank really holds, under this struct's own caps.
//
// THE FLEET OWNER'S REPORT, 2026-09-22, verbatim: "mulereciever wasnt updated to include handing
// out of all items in bank if requested from another account, accounts are still requesting items
// the mule does not have".
//
// MEASURED, not assumed — a read-only GET /api/bank?account=the mule against the live panel on
// 127.0.0.1:7300, 2026-09-22, while the fleet was running:
//
//   - That bank holds 226 distinct items. DefaultCatalog can hand out eleven of them.
//   - It holds exactly ONE chopping axe, an **Adamant axe x1** (obj 1357). AxeHandouts is
//     Mithril -> Steel -> Iron -> Bronze, so the single axe the fleet owns could not be handed to
//     anybody, however the ask was phrased, while all four rungs a woodcutter can ask for are
//     empty. That is the owner's sentence in one item.
//   - It also holds 13,317 Sharks, 73,272 Air runes and 35,069 Nature runes, and live the mule's
//     roster settings carry NO handoutItems list at all — so every Scarab kit ask for food or
//     runes is answered "not an item I hand out" today, by a mule standing on the stock.
//
// THE RECORDED ARGUMENT THIS OVERRIDES, stated plainly rather than deleted, because it was a
// deliberate decision and the owner's instruction now conflicts with it. AxeHandouts' own doc
// comment makes a WEALTH-DISTRIBUTION call — "adamant and rune deliberately are NOT [here] ...
// handing out a plentiful tier costs a tool; handing out the adamant costs the only one there
// is" — and DefaultCatalog calls itself "deliberately narrow ... exactly the set that unblocks
// the incident this was built for and nothing else". The owner has asked for the opposite and
// the owner wins. The argument is not discarded, though: it survives as the SHAPE of this
// struct, which is the compromise and not an accident.
//
//   - A CATALOGUE ENTRY ALWAYS WINS. Entitlement checks InCatalog first, so every hand-picked
//     Max, WindowMax, Floor and FloorKeepsBack still governs its item exactly as before, and the
//     curated ladder is still what a donor should ask for by default. Nothing here can route
//     around a bound somebody argued for.
//   - What changes is ONLY the answer to an explicit order for a named item the catalogue is
//     silent about. "Hand out anything" is not "hand out everything": Max, WindowMax, Never and
//     KeepBack below are what keep the second from following from the first, and Policy.Allowed
//     is still the only authentication the bus has.
type OpenStock struct {
	// Enabled turns the whole widening on. A zero-valued OpenStock is off, which is what keeps
	// every hand-built policy — peerrunner.onePolicy above all, which authorises exactly one
	// item for exactly one donor — behaving precisely as it did before this existed.
	Enabled bool
	// Max is the per-order ceiling for an uncatalogued item, the same kind of bound
	// Handout.Max is: an order above it is CLAMPED down, never refused, because a donor asking
	// for more than it may have is still a donor that needs some.
	Max int
	// WindowMax is the most of one uncatalogued item handed to everyone combined inside the
	// ledger's window. This is the bound that actually caps the damage from a bug or a
	// compromised donor name, exactly as CoinsPerWindow does for coins: the per-donor cap alone
	// means little across a ~269-account fleet.
	WindowMax int
	// KeepBack, when positive, is a RESERVE on every uncatalogued item: that many stay in the
	// bank and only the surplus above it may be handed out.
	//
	// It is applied through Handout.Floor with FloorKeepsBack set, so there is exactly one
	// reserve mechanism in this package rather than a second one with its own bugs — the class
	// of defect Floor itself already had once, documented as a gate and implemented as a
	// reserve (see Handout.Floor and Decide).
	//
	// Zero by default, and that is the owner's own case rather than an oversight: the live mule
	// holds ONE Adamant axe, and any reserve above zero would refuse the very order this
	// widening exists to serve. What bounds the exposure instead is Max and WindowMax, which
	// bound every item, including the ones there are thousands of.
	KeepBack int
	// Never are item names this path will NEVER serve, so an item whose limits were argued over
	// can only ever leave the bank under its own catalogue entry with those limits attached.
	//
	// This is not belt and braces. Catalog is replaceable wholesale (internal/bot/mule.go's
	// handoutItems setting builds a fresh one), and without this list an operator replacing the
	// catalogue would silently drop Coins and Crystal key out of it and straight into the open
	// path, where CoinsPerOrder and KeysFloor do not apply. See NeverOpen for what is on it.
	Never []string
}

// OpenMaxPerOrder is the default OpenStock.Max: the most of one uncatalogued item a single order
// may take.
//
// A thousand, which is neither arbitrary nor a guess at "a lot". It is what the largest real ask
// in this fleet already is: scarabrunner.defaultKitPerOrder is 1000, and its own comment records
// that a kit is "armour, food and runes in ordinary counts". Every other ask is far below it — a
// tool is one, a food load is a pack. Anything above a thousand is a script with a bug in it, and
// this is where that bug stops.
const OpenMaxPerOrder = 1000

// OpenPerWindow is the default OpenStock.WindowMax: ten full orders of any one uncatalogued item
// to everyone combined per window.
//
// Sized the same way CoinsPerWindow is (fifty orders of a ladder tool's price) — generous enough
// that the legitimate case never hits it, small enough that the worst case is bounded at ten
// thousand units of ONE item rather than at a bank holding 226 of them.
const OpenPerWindow = 10 * OpenMaxPerOrder

// DeclineKeptBack is the reason an order for an item on OpenStock.Never gets. Distinct from "not
// an item I hand out" on purpose: that one means "no such item in my policy", this one means "I
// have it and I am keeping it", and an asking script that cannot tell them apart would learn the
// wrong fact about the bank.
const DeclineKeptBack = "I keep that one back"

// NeverOpen is the default OpenStock.Never list: items that may only ever be handed out from an
// explicit catalogue entry, never by the open path.
//
// Each one is here because a specific, recorded bound would otherwise be routed around:
//
//   - "Coins" carries CoinsPerOrder/CoinsPerWindow, and a live mule raises them to 170,000 and
//     6,000,000 in its own roster entry (internal/bot/mule.go's coinsPerOrder). That is the
//     single largest exposure in this whole mechanism and it must stay a visible, configured
//     decision rather than something the open path grants by default at OpenMaxPerOrder.
//   - "Crystal key" carries KeysFloor, the batch gate users asked for by number
//     (2026-09-20). A key order that skipped it would hand over a stock that exists to be spent
//     in one trip.
//   - "Half of a key" is the mule's own raw material: mulerunner.JoinKeyHalvesRound turns the
//     fleet's casket halves into those crystal keys. Handing the halves out stops the mule
//     making keys at all, which no asking script wants and none would ever mean to do.
//
// Names are matched the same way every other name here is, case- and whitespace-insensitively.
func NeverOpen() []string {
	return []string{"Coins", "Crystal key", "Half of a key"}
}

// DefaultOpenStock is the widening as users asked for it on 2026-09-22: on, bounded per
// order and per window, no reserve, and with the three names above kept to their catalogue terms.
func DefaultOpenStock() OpenStock {
	return OpenStock{
		Enabled:   true,
		Max:       OpenMaxPerOrder,
		WindowMax: OpenPerWindow,
		Never:     NeverOpen(),
	}
}

// openEntry is the Handout an uncatalogued item would be served under, or false when the open
// path will not serve it at all. Never consulted for an item that IS catalogued — see
// Entitlement, which is the only caller.
//
// The returned Item is the DONOR'S spelling, trimmed, and that is the one real difference from a
// catalogue entry (whose Item is the canonical spelling the game's object config uses). There is
// nothing in this package that could canonicalise it: orderplan is pure and holds no cache, which
// is exactly the property that lets the catalogue be reasoned about without one. It costs
// nothing, because every consumer of Decision.Item already resolves the name case-insensitively —
// mulerunner.bankedID goes through cachecfg.ObjIDs, which ignores case, and the Ledger lower-cases
// every name it records.
func openEntry(pol Policy, item string) (Handout, bool) {
	name := strings.TrimSpace(item)
	if !pol.Open.Enabled || name == "" || pol.Open.Max <= 0 {
		return Handout{}, false
	}
	want := strings.ToLower(name)
	for _, n := range pol.Open.Never {
		if strings.ToLower(strings.TrimSpace(n)) == want {
			return Handout{}, false
		}
	}
	return Handout{
		Item:           name,
		Max:            pol.Open.Max,
		WindowMax:      pol.Open.WindowMax,
		Floor:          pol.Open.KeepBack,
		FloorKeepsBack: pol.Open.KeepBack > 0,
	}, true
}

// Entitlement is the one question "may this item leave the bank, and on what terms?", and it is
// what Prescreen and Decide both ask. The catalogue is consulted FIRST and always wins; only a
// name it says nothing about reaches the open path.
//
// InCatalog is deliberately left alone rather than widened, because several callers ask it a
// different question — "is this item on the curated list?" — and would be wrong if it started
// answering this one. restockplan.WindowFor derives a mule's restock floor from a catalogued
// WindowMax and must read 0 for an item that has no configured ceiling; internal/bot's startup
// checks use it to warn an operator that a script's item is not on the default list.
func Entitlement(pol Policy, item string) (Handout, bool) {
	if h, ok := InCatalog(pol, item); ok {
		return h, true
	}
	h, ok := openEntry(pol, item)
	if !ok {
		return h, false
	}
	// The pooled runes (runegate.go): a rune the policy gates is served on the gate's terms, not the open path's.
	return pol.Runes.narrow(h), true
}

// WillHandOut reports whether pol would consider an order for item at all, before any bank read,
// ledger or quantity. For a donor-side guard that wants to spare both sides a round trip.
func WillHandOut(pol Policy, item string) bool {
	h, ok := Entitlement(pol, item)
	return ok && h.Max > 0
}

// keptBack reports whether item is refused specifically because it is on the Never list, as
// opposed to the open path being switched off — the two need different reasons.
func keptBack(pol Policy, item string) bool {
	if !pol.Open.Enabled {
		return false
	}
	want := strings.ToLower(strings.TrimSpace(item))
	for _, n := range pol.Open.Never {
		if strings.ToLower(strings.TrimSpace(n)) == want {
			return true
		}
	}
	return false
}

// Policy is everything that decides whether an order is honoured, with no state of its own —
// the running totals live in a Ledger, which the caller owns.
type Policy struct {
	// Allowed is every donor name whose orders may be honoured. An order from a name not on
	// this list is declined, always: the bus carries no authentication of any kind, so this
	// is the only thing standing between a message and the bank.
	Allowed []string
	// Catalog is the curated set of items, each on its own hand-picked terms. It is what a
	// donor should ask for by default, and a catalogue entry always beats Open below.
	//
	// It was "the only set of items that may ever be handed out" until 2026-09-22, and that is
	// no longer true — see OpenStock for the fleet owner's instruction that changed it, and for
	// what still bounds the bank now that it is not the only gate.
	Catalog []Handout
	// Open decides what happens to an order for an item Catalog says nothing about: refused
	// outright (the zero value, and the behaviour of every policy built before this existed),
	// or served out of real stock under its own caps. See OpenStock.
	Open OpenStock
	// MaxOrdersPerDonor is how many orders one donor may have fulfilled inside the ledger's
	// window. Zero or negative disables hand-outs entirely (fail closed), which is what a
	// zero-valued Policy therefore does.
	MaxOrdersPerDonor int
	// MaxUnitsPerItem is how many units of one item may be handed out to everyone combined
	// inside the ledger's window — the cap that actually stops repeated orders emptying the
	// bank, since MaxOrdersPerDonor alone is per-donor and a fleet has hundreds of donors.
	MaxUnitsPerItem int
	// Runes is the pooled-rune rule (runegate.go): the mule hands a gated rune out only once it holds a stack of it,
	// and a thousand at a time. The zero value is off, which is every hand-built policy's behaviour.
	Runes RuneGate
}

// DefaultPolicy is DefaultCatalog with caps sized for the real fleet: a blocked account needs
// one tool to get going again, so one order per donor per window is enough to unblock it, and
// a fleet-wide ceiling of ten of any one item per window bounds the worst case (a bug, or a
// compromised donor name) at ten items rather than a bank.
//
// Open is DefaultOpenStock since 2026-09-22, on the fleet owner's instruction: an order for an
// item the catalogue is silent about is served from real stock rather than refused on sight. The
// curated catalogue above is unchanged and still governs every item on it.
func DefaultPolicy(allowed []string) Policy {
	return Policy{
		Allowed:           append([]string(nil), allowed...),
		Catalog:           DefaultCatalog(),
		Open:              DefaultOpenStock(),
		MaxOrdersPerDonor: 1,
		MaxUnitsPerItem:   10,
		Runes:             DefaultRuneGate(),
	}
}

// DefaultWindow is the period the caps above are measured over.
const DefaultWindow = 30 * time.Minute

// CatalogNames is every item pol will hand out, for a log line an operator can read at a
// glance — "which items can leave the bank" is the one thing worth stating out loud at startup.
func CatalogNames(pol Policy) []string {
	out := make([]string, 0, len(pol.Catalog))
	for _, h := range pol.Catalog {
		out = append(out, h.Item)
	}
	return out
}

// CatalogMax is the largest per-request cap anywhere in pol's catalogue.
func CatalogMax(pol Policy) int {
	max := 0
	for _, h := range pol.Catalog {
		if h.Max > max {
			max = h.Max
		}
	}
	return max
}

// normalise folds a name the identical way mulerunner.normalisePlayerName and
// donorrunner.normaliseTargetName already do (lowercase, underscore to space) — an account's
// login username uses an underscore where its in-game display name shows a space, and this
// package is compared against both. MUST stay identical to those two, not merely similar.
func normalise(name string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), "_", " ")
}

// AllowedFrom reports whether name may place orders under pol.
func AllowedFrom(pol Policy, name string) bool {
	n := normalise(name)
	if n == "" {
		return false
	}
	for _, a := range pol.Allowed {
		if normalise(a) == n {
			return true
		}
	}
	return false
}

// InCatalog resolves a requested name to its catalogue entry, matched case-insensitively and
// whitespace-trimmed. The returned Handout carries the CANONICAL spelling, which is what the
// caller must look up against the game's object config — never the donor's own spelling.
func InCatalog(pol Policy, item string) (Handout, bool) {
	want := strings.ToLower(strings.TrimSpace(item))
	for _, h := range pol.Catalog {
		if strings.ToLower(strings.TrimSpace(h.Item)) == want {
			return h, true
		}
	}
	return Handout{}, false
}

// ---- the ledger ----

type grant struct {
	at   time.Time
	to   string
	item string
	qty  int
	// kit groups the lines of one bundled kit (RecordKit), which count as ONE order; empty for
	// an ordinary single-item order.
	kit string
}

// Ledger is what has actually been handed out lately, so the per-window caps mean something
// across many separate decisions. Entries older than the window are swept on read.
//
// Go-idiom deviation, the same one mulereceiverplan's own Collection/ActivityLog/PickupQueue
// and mulebus.Roster already make and document: every method takes the moment as an explicit
// time.Time rather than reading the wall clock, so this stays usable from a pure test.
type Ledger struct {
	window time.Duration
	grants []grant
}

// NewLedger is an empty ledger whose entries age out after window.
func NewLedger(window time.Duration) *Ledger {
	if window <= 0 {
		window = DefaultWindow
	}
	return &Ledger{window: window}
}

// Window is the period this ledger measures over.
func (l *Ledger) Window() time.Duration { return l.window }

func (l *Ledger) sweep(now time.Time) {
	cutoff := now.Add(-l.window)
	kept := l.grants[:0]
	for _, g := range l.grants {
		if g.at.After(cutoff) {
			kept = append(kept, g)
		}
	}
	l.grants = kept
}

// Record notes that qty of item actually crossed to `to`. Call it only once the items are
// confirmed gone from the mule's own pack — a ledger of attempts rather than deliveries would
// let a run of failures lock out a donor that never received anything.
func (l *Ledger) Record(now time.Time, to, item string, qty int) {
	l.sweep(now)
	l.grants = append(l.grants, grant{at: now, to: normalise(to), item: strings.ToLower(strings.TrimSpace(item)), qty: qty})
}

// OrdersTo is how many orders `to` has had fulfilled inside the window. A bundled kit counts
// once however many lines it carried.
func (l *Ledger) OrdersTo(now time.Time, to string) int {
	l.sweep(now)
	want, n := normalise(to), 0
	kits := map[string]bool{}
	for _, g := range l.grants {
		if g.to != want {
			continue
		}
		if g.kit != "" {
			if kits[g.kit] {
				continue
			}
			kits[g.kit] = true
		}
		n++
	}
	return n
}

// UnitsOf is how many units of item have been handed to everyone combined inside the window.
func (l *Ledger) UnitsOf(now time.Time, item string) int {
	l.sweep(now)
	want, n := strings.ToLower(strings.TrimSpace(item)), 0
	for _, g := range l.grants {
		if g.item == want {
			n += g.qty
		}
	}
	return n
}

// ---- the decision ----

// Verdict is whether an order is honoured.
type Verdict string

// The verdicts.
const (
	Fulfil  Verdict = "fulfil"
	Decline Verdict = "decline"
)

// Decision is what to do about one order.
type Decision struct {
	Verdict Verdict
	// Item is the CANONICAL catalogue spelling, set only when Verdict is Fulfil.
	Item string
	// Qty is exactly how many to hand over, already capped. Never larger than the order
	// asked for, and never larger than the catalogue's own Max or what the window has left.
	Qty int
	// Clamped is true when Qty is less than the order asked for, so the mule can say so in
	// its log rather than appearing to quietly ignore half the request.
	Clamped bool
	// Reason is why it was declined (or, on a Fulfil, why the quantity was clamped). Short
	// enough for mulebus.DeclinedMessage's own 80-byte Reason.
	Reason string
}

func declined(reason string) Decision { return Decision{Verdict: Decline, Reason: reason} }

// Prescreen is everything Decide can settle without reading a bank: the sender's name, the
// grammar, the safety catalogue and both caps. Split out from Decide on purpose — the mule
// polls the bus far more often than it walks to a booth, and an unauthorised or malformed
// message must be refused instantly, from a log line alone, never at the cost of a bank trip.
// Callers that already have the bank open should use Decide instead and skip this.
//
// A Fulfil from Prescreen carries the quantity the policy allows; the caller MUST still narrow it
// to real stock (Decide's last step) before handing anything over.
func Prescreen(now time.Time, from, text string, pol Policy, led *Ledger) Decision {
	if !AllowedFrom(pol, from) {
		return declined("not on the allow list")
	}
	order, err := Parse(text)
	if err != nil {
		return declined(err.Error())
	}
	entry, ok := Entitlement(pol, order.Item)
	if !ok {
		if keptBack(pol, order.Item) {
			return declined(DeclineKeptBack)
		}
		return declined("not an item I hand out")
	}
	if pol.MaxOrdersPerDonor <= 0 {
		return declined("hand-outs are switched off")
	}
	if entry.Max <= 0 {
		return declined("not an item I hand out")
	}
	if led == nil {
		return declined("no hand-out ledger")
	}
	if led.OrdersTo(now, from) >= pol.MaxOrdersPerDonor {
		return declined("already handed you one recently")
	}
	qty, clamped := order.Qty, false
	if qty > entry.Max {
		qty, clamped = entry.Max, true
	}
	windowMax := pol.MaxUnitsPerItem
	if entry.WindowMax > 0 {
		windowMax = entry.WindowMax
	}
	if windowMax > 0 {
		left := windowMax - led.UnitsOf(now, entry.Item)
		if left <= 0 {
			return declined("hand-out limit for that item is spent")
		}
		if qty > left {
			qty, clamped = left, true
		}
	}
	return fulfil(entry.Item, qty, clamped)
}

func fulfil(item string, qty int, clamped bool) Decision {
	d := Decision{Verdict: Fulfil, Item: item, Qty: qty, Clamped: clamped}
	if clamped {
		d.Reason = "capped to " + strconv.Itoa(qty)
	}
	return d
}

// Decide is the whole gate between a bus message and the bank: Prescreen's checks, then real
// stock.
//
// stock reports how many of the CANONICAL item name the mule can actually draw (its bank plus
// whatever it already holds); nil means "unknown", which declines rather than assuming.
func Decide(now time.Time, from, text string, pol Policy, led *Ledger, stock func(item string) int) Decision {
	d := Prescreen(now, from, text, pol, led)
	if d.Verdict != Fulfil {
		return d
	}
	if stock == nil {
		return declined("cannot read my own stock")
	}
	held := stock(d.Item)
	if held <= 0 {
		return declined("none in the bank")
	}
	// The batch floor, if this item has one. Checked after the real stock read and before any
	// clamping, so the count in the reason is the true bank count rather than a clamped qty —
	// the asking script logs it, and "47 of 100" is what tells an operator the run is building
	// up rather than broken.
	//
	// Entitlement rather than InCatalog since 2026-09-22: an uncatalogued item served by the
	// open path carries OpenStock.KeepBack as a Floor with FloorKeepsBack set, and that reserve
	// has to be applied HERE or it would be documented and absent — the exact defect Floor
	// itself already had once (see FloorKeepsBack's own comment below).
	spendable := held
	if h, ok := Entitlement(pol, d.Item); ok && h.Floor > 0 {
		// held == Floor gives nothing away and must decline rather than fulfil an order of zero:
		// the reserve is what stays, so an order needs stock ABOVE it to be fillable at all.
		if held <= h.Floor && !(h.FloorInclusive && !h.FloorKeepsBack && held == h.Floor) {
			return declined(fmt.Sprintf("only %d %s banked, holding until %d", held, d.Item, h.Floor))
		}
		// HISTORY, kept because the reserve reading is still one opt-in away: from 2026-09-21
		// the floor was a RESERVE, after `mule` passed the gate with 418 crystal keys banked
		// and handed ALL 418 to the first asker. That reasoning assumed competing consumers. It
		// is SUPERSEDED for crystal keys by the fleet owner's rulings of 2026-09-22 and again
		// 2026-09-26 (below): there is ONE key consumer, spare104, and once the mule holds over
		// 100 it is meant to take all of them. The reserve now applies only to an item that
		// sets FloorKeepsBack, which today is the open path's OpenStock.KeepBack alone.
		spendable = held - h.Floor
		if !h.FloorKeepsBack {
			// A GATE, not a reserve -- which is what Handout.Floor's own doc says it is:
			// "how many of this item the mule must ALREADY hold before it hands any out".
			// The reserve above was added later and quietly contradicted that.
			//
			// The contradiction had a live cost. Users asked three times for a mule
			// holding over 100 keys to hand over THAT amount, and three times watched it
			// hand over the remainder instead: at 113 held it gave 13, and at 106 it would
			// give 6. A mule that must always keep 100 back can never satisfy "give me the
			// hundred you are holding".
			//
			// What the reserve was protecting is still protected, by the gate itself and by
			// Max/WindowMax. The 418-in-one-order incident it was written for was a donor
			// asking blind for 500; the crystal-key consumer now asks for what the mule
			// actually has, and there is exactly ONE such consumer in this fleet, whose whole
			// job is to spend them and bring the proceeds back. An item that genuinely needs
			// draining protection sets FloorKeepsBack and keeps the old behaviour.
			//
			// Restated by the owner on 2026-09-26 after a fourth sighting: the mule held 110 and
			// spare104 received 10 -- "it should have taken 110 keys". This side was already a
			// gate by then; the 10 came from lootchestfisherplan.Available still subtracting
			// the floor before it asked. Both sides now read Floor as a gate, and
			// TestDecideHandsOverAllHundredAndTenKeys pins the live number here.
			spendable = held
		}
	}
	qty, clamped := d.Qty, d.Clamped
	if qty > spendable {
		qty, clamped = spendable, true
	}
	return fulfil(d.Item, qty, clamped)
}

// ---- the donor's side of the trade screen ----

// ItemCount is one row of a trade screen, named rather than keyed by id so this stays pure.
type ItemCount struct {
	Name  string
	Count int
}

// Screen is which trade screen the donor is looking at.
type Screen int

// The screens.
const (
	ScreenNone Screen = iota
	ScreenOffer
	ScreenConfirm
)

// ReceiveView is what the donor reads while the mule hands it something. This is the exact
// mirror of donorplan.TradeView, and it exists separately rather than reusing it because
// donorplan.DecideTrade's whole contract is the opposite one: "a trade where the partner
// offers anything is declined: the donor only gives, and only to its mule." Both rules are
// right for their own direction, and conflating them would break the giving side.
type ReceiveView struct {
	Screen Screen
	// Partner is who the screen says is on the other side; Mule is who we asked.
	Partner string
	Mule    string
	// Want is the order we placed.
	Want Order
	// Theirs is the mule's side of the screen, Mine is ours.
	Theirs []ItemCount
	Mine   []ItemCount
	// Accepted is true once we have accepted the screen currently shown.
	Accepted bool
}

// ReceiveAction is what the donor does about the screen.
type ReceiveAction string

// The receive actions.
const (
	ReceiveClosed  ReceiveAction = "no trade open"
	ReceiveDecline ReceiveAction = "decline"
	ReceiveAccept  ReceiveAction = "accept the offer screen"
	ReceiveConfirm ReceiveAction = "accept the confirm screen"
	ReceiveWait    ReceiveAction = "wait for the mule"
)

// NamesMatch compares two player names the way the server identifies accounts:
// case-insensitively, with underscore and space the same. Blank names never match — the same
// rule mulebus.NamesMatch and donorplan.NamesMatch already state, restated here rather than
// imported so this package keeps no dependencies at all.
func NamesMatch(a, b string) bool {
	na, nb := normalise(a), normalise(b)
	return na != "" && na == nb
}

func totalOf(rows []ItemCount, name string) int {
	want, n := strings.ToLower(strings.TrimSpace(name)), 0
	for _, r := range rows {
		if strings.ToLower(strings.TrimSpace(r.Name)) == want {
			n += r.Count
		}
	}
	return n
}

func anyPositive(rows []ItemCount) bool {
	for _, r := range rows {
		if r.Count > 0 {
			return true
		}
	}
	return false
}

// DecideReceive decides the donor's side of a hand-out trade screen. It declines anything that
// is not precisely the trade it asked for: a different partner, a screen where we are being
// asked to give something away, or an offer carrying any item other than the one ordered.
// Returning the reason alongside the action so the caller can log WHY it walked away from a
// trade — the one thing the giving side's logs were missing when hand-overs were failing.
func DecideReceive(v ReceiveView) (ReceiveAction, string) {
	if v.Screen == ScreenNone {
		return ReceiveClosed, ""
	}
	if !NamesMatch(v.Partner, v.Mule) {
		return ReceiveDecline, "not the mule I asked"
	}
	if anyPositive(v.Mine) {
		// The donor asked for something; it is not giving anything back in the same
		// screen. A non-empty own side means either a misclick of ours or a screen that
		// is not the one we think it is, and both are reasons to walk away.
		return ReceiveDecline, "I am being asked to give something"
	}
	got := totalOf(v.Theirs, v.Want.Item)
	for _, r := range v.Theirs {
		if r.Count > 0 && !strings.EqualFold(strings.TrimSpace(r.Name), strings.TrimSpace(v.Want.Item)) {
			return ReceiveDecline, "offered something I did not ask for"
		}
	}
	if got <= 0 {
		// Nothing on the screen yet. The mule places its offer a moment after the screen
		// opens, exactly as a donor does in the other direction (see
		// mulerunner.WaitForStableOffer's own doc comment on why accepting the first
		// snapshot is a real, already-fixed bug), so waiting is right and the caller's own
		// timeout is what ends it.
		return ReceiveWait, "waiting for the mule to place its offer"
	}
	if got > v.Want.Qty {
		return ReceiveDecline, "offered more than I asked for"
	}
	if v.Accepted {
		return ReceiveWait, "already accepted this screen"
	}
	if v.Screen == ScreenConfirm {
		return ReceiveConfirm, ""
	}
	return ReceiveAccept, ""
}
