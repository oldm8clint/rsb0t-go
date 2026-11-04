// Package mulebus is the in-process replacement for MuleReceiver's own wall-bus protocol
// (rsb0t-scripts' muleProtocol.ts: a same-origin BroadcastChannel every bot tab in one
// Electron renderer shared). This project runs every account as a goroutine inside one
// process instead of a tab inside one renderer, so the transport collapses to a shared,
// thread-safe, in-memory pub-sub — "simpler than the original, not harder" per COLLAB.md's
// own PLAN.md sketch, but new design, not a line-for-line port: there is no wire format,
// no origin, and no cross-window delivery to reason about, just Go values passed between
// goroutines that all trust each other by construction (they're all this project's own
// bots).
//
// What this package deliberately does NOT include, and why: muleProtocol.ts also carries
// the trade-screen decision policy (decideOffer/decideGive/decideConfirm/decideOpenTrade)
// and the PM/order grammar (parseOrderItems/splitOrderPlace/parsePickupCall). Those are
// MuleReceiver's own trip-execution and PM-order logic — a much bigger, still-unbuilt
// integration (see internal/bot/mule.go's own doc comment for exactly what is and isn't
// wired) — not the transport this package replaces. Porting the message *shapes* and the
// presence bookkeeping (Roster) here, so a receiver can hear about a pickup and a future
// donor-side script has a real interface to publish to, is complete and useful on its own
// without those decision functions; adding them here just to say "ported" would be the
// "rushed, unverified" mistake this project's own overnight instructions warn against.
//
// WHY THE MULE'S OWN OUTGOING APPEAL LIVES HERE (KindAppeal/KindPledge, 2026-09-21). Every
// kind above this pair runs one way: a donor asks, a mule answers. KindOrder is the clearest
// case -- a blocked script publishes "need 1 Big fishing net" and the mule decides. So when
// the mule itself needed to ORIGINATE a request ("I am low on coins, who can spare some?"),
// the honest question was whether that belongs in this package at all, or beside it.
//
// It belongs here, for the same reason KindClaim/KindUnclaim do and by the same test: what
// is being added is a SHAPE and a delivery rule, and nothing else. An appeal is "who am I,
// what do I want, how much, where am I standing"; a pledge is "I can spare this much." Not
// one line of the policy is here -- how low is low, what a donor may safely part with, how
// many donors may be in flight at once, who wins when ninety answer -- all of that is
// internal/scripts/restockplan, exactly where orderplan holds the hand-OUT policy this
// package also refuses to hold. Putting the shapes somewhere else would have meant a second
// transport for one direction of the same conversation, and a donor bot importing two buses
// to hear two halves of it.
//
// The one delivery rule that IS here, because it is addressing and not policy: an appeal
// with an empty To is a BROADCAST ("anyone who can spare some"), and an appeal addressed To
// one account is a GRANT ("you specifically, bring it now"). That reuses AddressedTo rather
// than inventing a third kind for what is the same message with a recipient filled in, and
// IsGrant/IsBroadcastAppeal below name the two cases so no reader has to remember it.
//
// Every message kind from muleProtocol.ts's MuleMessage union is represented (Kind names
// match the TS string literals exactly, for anyone cross-referencing the two), even the
// ones internal/bot/mule.go's receiver doesn't act on yet (Ready, Coming, Declined,
// Traded) — a future donor-side script (ProgressiveFisher's/ProgressiveCrafter's own mule
// hand-off, explicitly out of this task's scope) can publish and read them without this
// package changing shape again.
package mulebus

import (
	"sync"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Kind names one of muleProtocol.ts's MuleMessage variants. String-valued (not iota) so a
// log line or a future JSON encoding reads the same name the TS source used.
type Kind string

// The message kinds, one per muleProtocol.ts MuleMessage union member.
const (
	KindMuleHere Kind = "mule-here" // the mule announcing position/state/free slots
	KindReady    Kind = "ready"     // a donor announcing it is full and walking over
	KindTraded   Kind = "traded"    // either side reporting a completed handover
	KindCollect  Kind = "collect"   // a donor asking the mule to come to it
	KindCancel   Kind = "cancel"    // a donor withdrawing its request
	KindOrder    Kind = "order"     // an item order placed over the bus (a script has no PM)
	KindHere     Kind = "here"      // any account reporting its own tile
	KindComing   Kind = "coming"    // the mule telling a donor it is on its way
	KindDeclined Kind = "declined"  // the mule telling a donor it will not come, and why

	// KindClaim and KindUnclaim are NOT from muleProtocol.ts — they are this project's own
	// addition, for a crowd of accounts standing on one patch of floor. A claim says "I am
	// walking to THIS pile, nobody else bother"; an unclaim says "I have it, or I gave up".
	// The POLICY — who wins a race, when a claim goes stale, whether to go at all — is
	// deliberately not here: it is the ScarabSwarm plan package's lootclaim.go, for the same
	// reason this package's doc comment gives for leaving the trade decisions out. This
	// package owns the shape and the delivery; it does not decide anything.
	KindClaim   Kind = "claim"   // an account reserving one ground pile before walking to it
	KindUnclaim Kind = "unclaim" // the claimant releasing it, taken or abandoned

	// KindAppeal and KindPledge are also NOT from muleProtocol.ts. They are the only pair on
	// this bus the MULE originates rather than answers -- see the package doc for why the
	// shapes belong here and the policy does not.
	//
	// KindAppeal with To == "" is a broadcast: "I am running low on <Item>, I want <Qty>, I
	// am standing at <Tile>." KindAppeal with To set is a grant to that one account: "you,
	// bring me <Qty> now." KindPledge is a donor's answer: "I can spare <Qty> of <Item>."
	KindAppeal Kind = "appeal" // the mule asking the fleet to restock it (broadcast or granted)
	KindPledge Kind = "pledge" // an account answering an appeal with what it can spare

	// KindReserve is NOT from muleProtocol.ts either: a donor telling its mule "I have drawn a
	// load for you and am on my way", sent once when the walk starts and ended by the same
	// KindCancel every hand-off already sends. It exists so the mule can count donors who are
	// still WALKING when it publishes how deep its queue is (MuleHere's Queue), which is what a
	// donor reads before it draws a load at all. It deliberately does not make the mule wait for
	// anybody: KindReady, sent within reach of the post, is still the only thing that does.
	KindReserve Kind = "reserve"

	// KindKitOrder and KindKitAnswer are NOT from muleProtocol.ts: a bundled kit (owner,
	// 2026-09-23), up to KitLinesMax different items handed over in ONE trade, because the fleet
	// has one mule and a kit asked for item by item is one bank trip and one trade per item.
	// The order carries its lines and the donor's free pack slots; the answer carries, per line,
	// how many are coming and whether as bank notes, or why not. An answer with no lines and no
	// Reason is the mule's "queued" acknowledgement, EtaMs its guess at the wait. What the mule
	// will serve is orderplan.DecideKit's business, not this package's.
	KindKitOrder  Kind = "kit-order"
	KindKitAnswer Kind = "kit-answer"
)

// KitLinesMax is the most lines a kit message carries: one trade screen of slots.
const KitLinesMax = 28

// KitLine is one line of a kit message. On an order Item and Qty are what the donor wants and
// Noted asks for the line as bank notes (orderplan.KitLine.Noted); on an answer Qty is what the
// mule will hand over (0: this line is not coming), Noted whether it crosses as bank notes, and
// Reason why a line is short or refused.
type KitLine struct {
	Item   string
	Qty    int
	Noted  bool
	Reason string
}

// MuleState is what the mule is doing, carried on a KindMuleHere heartbeat. Matches
// muleProtocol.ts's MuleState union exactly.
type MuleState string

const (
	StateHome      MuleState = "home"
	StateOutbound  MuleState = "outbound"
	StateMeeting   MuleState = "meeting"
	StateHomebound MuleState = "homebound"
)

// TravelMode is how the mule is getting to a pickup, carried on a KindComing message.
type TravelMode string

const (
	TravelWalk     TravelMode = "walk"
	TravelTeleport TravelMode = "teleport"
)

// Message is one bus message. Unlike muleProtocol.ts's tagged union (a different field
// set per Kind, enforced by the TypeScript compiler), this is one flat struct with every
// field any Kind might use — the same "Go idiom deviation" mulereceiverplan's own doc
// comments already call out for kitFromSettings' Record<string,number>: a Go value needs
// one concrete shape, and a flat struct reads at least as clearly as a second, parallel
// type per Kind would for a message set this small. Which fields are meaningful for a
// given Kind is documented per-field below and matches muleProtocol.ts's own field list
// for that variant exactly.
type Message struct {
	Kind Kind
	// From is required on every kind — the sender's account name.
	From string
	// To addresses one recipient by name; empty means "every mule/account listening"
	// (muleProtocol.ts's own optional `to` on Collect/Order/Coming/Declined).
	To string

	// Tile is the sender's position: MuleHere's own x/z/level, Collect's/Here's own
	// x/z/level.
	Tile tile.Tile
	// World is the game WORLD the sender is on — the other half of its address, once the
	// fleet can run accounts on more than one (w2.example.invalid opened 2026-09-22). Two
	// accounts on different worlds standing on the same Tile are different players who
	// cannot see or trade with each other, so a Tile alone stopped being a complete answer
	// to "where are you" on the day a second world opened.
	//
	// WHY THIS BELONGS HERE, against this package's own test (a SHAPE and a delivery rule,
	// nothing else): a world is address, not policy. Nothing here decides whether to hop,
	// how far is too far, or who should move — that is
	// internal/scripts/worldhopplan, exactly where the doc comment above puts orderplan and
	// restockplan. It also happens to be the ONE channel that can carry this: the bus is
	// in-process and world-agnostic, so a donor on world 2 hears a mule on world 1 through
	// it when there is no in-game way for them to hear each other at all.
	//
	// 0 MEANS "THE SENDER DID NOT SAY", never world zero (there is no world zero) and never
	// world 1. A reader that resolves 0 to 1 would make a donor hop on the strength of a
	// message that never mentioned a world. worldhopplan.MuleWorld enforces that and has
	// the test for it.
	World int
	// FreeSlots: MuleHere — pack slots the mule has left. A Nature gate message (KindRuneGate) reuses it for the essence the crafter takes
	// in one trade (Message.WithCapacity; 0 is "not said", a full load of 25).
	FreeSlots int
	// State: MuleHere only.
	State MuleState
	// Queue and CycleMs: MuleHere only, optional. Queue is how many donors this mule is
	// expecting (announced at its post, plus reserved and still walking) and CycleMs what it
	// believes one hand-off costs it, so a donor can judge the wait BEFORE drawing a load
	// rather than after walking it over. Zero CycleMs means "not carried": an older sender.
	Queue   int
	CycleMs int
	// Home: MuleHere only, optional — the mule's post, so a donor can walk there itself
	// instead of waiting. Nil means not carried (an older sender, in the TS source).
	Home *tile.Tile

	// With: Traded only — the other side of the completed handover.
	With string
	// Need: Ready only, optional — an exact count the donor wants handed back; nil means
	// "as usual" (matches muleProtocol.ts's `need?: number`).
	Need *int
	// Slots: Collect only — how many pack slots the donor wants to hand over.
	Slots int
	// Text: Order only — the order text, the same grammar a PM would carry.
	Text string
	// Via, EtaMs: Coming only.
	Via   TravelMode
	EtaMs int
	// Reason: Declined only.
	Reason string
	// Item: Claim, Unclaim, Appeal and Pledge — for Claim/Unclaim the item name on the
	// claimed pile, where the pair (Tile, Item) is the pile's identity (there is no server
	// object id here because a client sees ground items by name through the same decode path
	// as everything else); for Appeal/Pledge the canonical item name being asked for or
	// offered, spelled the way the game's own object config spells it.
	Item string
	// Kit and KitID: KitOrder and KitAnswer only — the kit's lines, and the id the donor gave
	// it, which the answer echoes so a donor never reads a stale answer as its own and the mule
	// can tell a re-publish of a kit from a new one.
	Kit   []KitLine
	KitID string
	// Give and GiveSlots: KitOrder only, optional -- what the donor hands the mule back in the SAME kit trade
	// (the kit ask's bank hand-over, a user request), by name and count as it will place it on its own side of
	// the screen, and the pack slots that takes on the mule's side. Empty: a kit that gives nothing back, which
	// the mule serves exactly as before (and refuses anything the donor puts on its side).
	Give      []KitLine
	GiveSlots int
	// Qty: Appeal and Pledge only — how many units. On a broadcast Appeal it is how much the
	// mule wants in total, which is a WANT and not an instruction to any one donor; on a
	// grant it is how much that one donor said it could spare; on a Pledge it is how much the
	// sender believes it can spare right now.
	//
	// A separate field rather than reusing Need, deliberately: Need is a *int whose nil means
	// "an older sender did not carry it" (muleProtocol.ts's `need?: number`), and an appeal
	// with no quantity is not a thing that can exist. A plain int that is always meaningful
	// for its two kinds reads better than a pointer that never legitimately is nil.
	//
	// And a structured field rather than the Text grammar KindOrder uses, which is the design
	// an appeal obviously wants and cannot have: orderplan.QtyMax bounds that parser at
	// 1,000,000, sized for what ONE donor may take in one hand-out, while an appeal's Qty is
	// what the whole fleet is being asked to make up — 15,000,000 on the live mule's own
	// configuration. See the closing note in internal/scripts/restockplan/restock.go.
	// A trawler KindHere message reuses it for the watch (WithWatch).
	Qty int
}

// Protocol constants, ported from muleProtocol.ts's own (values unchanged — these bound
// what a well-behaved publisher sends, not the transport itself, so there's no reason for
// the in-process version to disagree with the numbers already tuned against real trade
// timing).
const (
	// NameMax is the longest account name a message carries (a character name's own cap).
	NameMax = 12
	// ReasonMax is the longest Declined.Reason carried.
	ReasonMax = 80
	// OrderTextMax is the longest Order.Text carried.
	OrderTextMax = 120
	// CollectTTL: a queued Collect request older than this without a fresh heartbeat is
	// stale (mulereceiverplan.PickupQueue's own ttl parameter should be given this).
	CollectTTL = 20 * time.Second
	// HeartbeatInterval is how often a mule should publish KindMuleHere — muleProtocol.ts's
	// own ANNOUNCE_EVERY_MS.
	HeartbeatInterval = 3 * time.Second
	// CollectInterval is how often a donor should repeat KindCollect while queued —
	// muleProtocol.ts's own COLLECT_EVERY_MS.
	CollectInterval = 5 * time.Second
)

func clampName(s string) string {
	if len(s) > NameMax {
		return s[:NameMax]
	}
	return s
}

// Heartbeat builds a KindMuleHere message: the mule announcing where it is, what it's
// doing, and how much room is left. home is optional (nil omits it, matching an older
// sender in the TS source).
//
// It carries no World, so an existing caller keeps compiling and keeps meaning exactly what
// it meant. A mule that knows its world says so with OnWorld — see that function.
func Heartbeat(from string, at tile.Tile, freeSlots int, state MuleState, home *tile.Tile) Message {
	return Message{Kind: KindMuleHere, From: clampName(from), Tile: at, FreeSlots: freeSlots, State: state, Home: home}
}

// OnWorld stamps a message with the world its sender is on, returning the message so it
// reads as one expression at a publish site:
//
//	bus.Publish(mulebus.OnWorld(mulebus.Heartbeat(me, at, free, state, home), ctx.World()))
//
// A separate function rather than another parameter on every builder, deliberately: there
// are twelve builders in this file and a world is meaningful to all of them, so threading it
// through each signature would have churned every call site in the repository to add a field
// that most of them do not yet fill. world <= 0 leaves the message unstamped, which is the
// same as never calling this.
func OnWorld(msg Message, world int) Message {
	if world > 0 {
		msg.World = world
	}
	return msg
}

// CollectMessage builds a KindCollect message: a donor asking a mule (or every mule
// listening, if to is empty) to come to it. slots is clamped to 1-28, matching
// muleProtocol.ts's collectMessage builder exactly.
func CollectMessage(from string, at tile.Tile, slots int, to string) Message {
	if slots < 1 {
		slots = 1
	} else if slots > 28 {
		slots = 28
	}
	return Message{Kind: KindCollect, From: clampName(from), To: clampName(to), Tile: at, Slots: slots}
}

// CancelMessage builds a KindCancel message: a donor withdrawing its request.
func CancelMessage(from string) Message {
	return Message{Kind: KindCancel, From: clampName(from)}
}

// HereMessage builds a KindHere message: any account reporting its own tile, so a mule
// filling an order can find it.
func HereMessage(from string, at tile.Tile) Message {
	return Message{Kind: KindHere, From: clampName(from), Tile: at}
}

// OrderMessage builds a KindOrder message: an item order placed over the bus because a
// script has no way to type a private message. text is truncated to OrderTextMax,
// matching muleProtocol.ts's orderMessage builder.
func OrderMessage(from, text, to string) Message {
	if len(text) > OrderTextMax {
		text = text[:OrderTextMax]
	}
	return Message{Kind: KindOrder, From: clampName(from), To: clampName(to), Text: text}
}

// KitOrderMessage builds a KindKitOrder: donor `from` asking mule `to` for the kit `lines`
// (at most KitLinesMax; the rest are dropped), with freeSlots its own free pack slots (0: not
// reported) and wait how long it will wait for the kit from now (carried in EtaMs; 0: the mule's
// own default), so the mule never serves a kit its donor has already given up on.
func KitOrderMessage(from, to, id string, lines []KitLine, freeSlots int, wait time.Duration) Message {
	if len(lines) > KitLinesMax {
		lines = lines[:KitLinesMax]
	}
	kit := make([]KitLine, 0, len(lines))
	for _, l := range lines {
		kit = append(kit, KitLine{Item: clampItem(l.Item), Qty: l.Qty, Noted: l.Noted})
	}
	if freeSlots < 0 {
		freeSlots = 0
	}
	ms := int(wait / time.Millisecond)
	if ms < 0 {
		ms = 0
	}
	return Message{Kind: KindKitOrder, From: clampName(from), To: clampName(to), KitID: clampItem(id), Kit: kit, FreeSlots: freeSlots, EtaMs: ms}
}

// WithGive is a kit order that also hands the mule give (at most KitLinesMax lines) in the same trade, taking
// slots of the mule's pack; an empty give leaves the order as it was.
func WithGive(m Message, give []KitLine, slots int) Message {
	if len(give) > KitLinesMax {
		give = give[:KitLinesMax]
	}
	out := make([]KitLine, 0, len(give))
	for _, l := range give {
		if l.Qty > 0 {
			out = append(out, KitLine{Item: clampItem(l.Item), Qty: l.Qty, Noted: l.Noted})
		}
	}
	if len(out) == 0 {
		return m
	}
	m.Give = out
	m.GiveSlots = max(slots, 1)
	return m
}

// KitCancelMessage builds the KindCancel a donor sends when it stops waiting for kit id: a
// KindCancel carrying the KitID, which the mule reads as "drop that kit" and nothing else (a
// hand-off's own cancel carries no KitID).
func KitCancelMessage(from, to, id string) Message {
	return Message{Kind: KindCancel, From: clampName(from), To: clampName(to), KitID: clampItem(id)}
}

// KitAnswerMessage builds a KindKitAnswer from mule `from` to donor `to` about kit id: the plan
// (lines), a whole-kit refusal (reason, with or without lines), or with neither a "queued"
// acknowledgement carrying eta.
func KitAnswerMessage(from, to, id string, lines []KitLine, reason string, eta time.Duration) Message {
	if len(lines) > KitLinesMax {
		lines = lines[:KitLinesMax]
	}
	kit := make([]KitLine, 0, len(lines))
	for _, l := range lines {
		kit = append(kit, KitLine{Item: clampItem(l.Item), Qty: l.Qty, Noted: l.Noted, Reason: clampReason(l.Reason)})
	}
	ms := int(eta / time.Millisecond)
	if ms < 0 {
		ms = 0
	}
	return Message{Kind: KindKitAnswer, From: clampName(from), To: clampName(to), KitID: clampItem(id), Kit: kit, Reason: clampReason(reason), EtaMs: ms}
}

// IsKitQueued reports whether msg is a mule's "queued" acknowledgement of a kit rather than its
// answer.
func IsKitQueued(msg Message) bool {
	return msg.Kind == KindKitAnswer && len(msg.Kit) == 0 && msg.Reason == ""
}

func clampReason(s string) string {
	if len(s) > ReasonMax {
		return s[:ReasonMax]
	}
	return s
}

// ReadyMessage builds a KindReady message: a donor announcing it is full and walking
// over. need < 0 means "not carried" (muleProtocol.ts's `need?: number`).
func ReadyMessage(from string, need int) Message {
	msg := Message{Kind: KindReady, From: clampName(from)}
	if need >= 0 {
		n := need
		msg.Need = &n
	}
	return msg
}

// ReserveMessage builds a KindReserve message: a donor telling mule it has drawn a load for it
// and is walking over.
func ReserveMessage(from, to string) Message {
	return Message{Kind: KindReserve, From: clampName(from), To: clampName(to)}
}

// WithQueue stamps a KindMuleHere heartbeat with the mule's queue depth and its cycle estimate,
// returning the message so it reads as one expression at the publish site (the OnWorld shape).
// A negative queue or cycle is clamped to zero, which a reader takes as "not carried".
func WithQueue(msg Message, queue int, cycle time.Duration) Message {
	if queue < 0 {
		queue = 0
	}
	ms := int(cycle / time.Millisecond)
	if ms < 0 {
		ms = 0
	}
	msg.Queue, msg.CycleMs = queue, ms
	return msg
}

// TradedMessage builds a KindTraded message: either side reporting a completed handover.
func TradedMessage(from, with string) Message {
	return Message{Kind: KindTraded, From: clampName(from), With: clampName(with)}
}

// ComingMessage builds a KindComing message: the mule telling a donor it's on its way.
func ComingMessage(from, to string, via TravelMode, eta time.Duration) Message {
	ms := int(eta / time.Millisecond)
	if ms < 0 {
		ms = 0
	}
	return Message{Kind: KindComing, From: clampName(from), To: clampName(to), Via: via, EtaMs: ms}
}

// ClaimMessage builds a KindClaim message: an account reserving the pile of `item` standing
// at `at`, so the rest of a crowd does not all run at it. Re-publishing the same claim is a
// refresh and is how a claimant that is still walking keeps it alive — the expiry policy is
// scarabplan.ClaimTTL, not this package's business.
func ClaimMessage(from, item string, at tile.Tile) Message {
	return Message{Kind: KindClaim, From: clampName(from), Item: clampItem(item), Tile: at}
}

// UnclaimMessage builds a KindUnclaim message: the claimant releasing the pile, either because
// it now holds the item or because it has given up on it. Only the original claimant's release
// means anything; enforcing that is the reader's job (scarabplan.ClaimBoard.Release).
func UnclaimMessage(from, item string, at tile.Tile) Message {
	return Message{Kind: KindUnclaim, From: clampName(from), Item: clampItem(item), Tile: at}
}

// ItemMax is the longest item name a Claim/Unclaim carries.
const ItemMax = 40

func clampItem(s string) string {
	if len(s) > ItemMax {
		return s[:ItemMax]
	}
	return s
}

// AppealMessage builds a BROADCAST KindAppeal: the mule telling every account listening that
// it is running low on item and wants qty of it, and where it is standing so a donor can walk
// to it without waiting for a heartbeat.
//
// To is deliberately empty. An appeal that named one account would be a grant (see
// GrantMessage) and would have to be re-sent per donor; a broadcast is one message the whole
// fleet screens for itself, which is the point of having a bus at all.
//
// qty below 1 is clamped to 1: an appeal for nothing is a bug at the caller, and publishing it
// as "0" would read on the bus as a real message the fleet would waste answers on.
func AppealMessage(from, item string, qty int, at tile.Tile) Message {
	if qty < 1 {
		qty = 1
	}
	return Message{Kind: KindAppeal, From: clampName(from), Item: clampItem(item), Qty: qty, Tile: at}
}

// GrantMessage builds a KindAppeal addressed to ONE account: "you specifically, bring me qty of
// item, I am at Tile." The mule sends this in reply to a pledge it has chosen.
//
// This is what keeps ninety donors from all walking at once. A broadcast appeal asks; only a
// grant authorises, and the sender hands out a bounded number of them at a time
// (restockplan.Board). A donor that pledged and was not granted simply keeps working.
func GrantMessage(from, to, item string, qty int, at tile.Tile) Message {
	if qty < 1 {
		qty = 1
	}
	return Message{Kind: KindAppeal, From: clampName(from), To: clampName(to), Item: clampItem(item), Qty: qty, Tile: at}
}

// PledgeMessage builds a KindPledge: a donor answering an appeal with how much of item it
// believes it can spare. Addressed to the mule that appealed, because a fleet may run more
// than one mule and a pledge answers one appeal, not all of them.
//
// A pledge is a statement of intent, never a promise the coins exist: the donor computes it
// from its own last bank reading, and the real amount is whatever its bank actually holds when
// it gets there. Only the mule's own received count ever settles what arrived.
func PledgeMessage(from, to, item string, qty int) Message {
	if qty < 1 {
		qty = 1
	}
	return Message{Kind: KindPledge, From: clampName(from), To: clampName(to), Item: clampItem(item), Qty: qty}
}

// IsBroadcastAppeal reports whether msg is an appeal to the whole fleet rather than a grant to
// one account.
func IsBroadcastAppeal(msg Message) bool { return msg.Kind == KindAppeal && msg.To == "" }

// IsGrant reports whether msg is an appeal addressed specifically to me — the mule telling this
// account, and not the fleet at large, to come now. A blank me never matches, the same rule
// NamesMatch already states, so an account with no name cannot be granted anything.
func IsGrant(msg Message, me string) bool {
	return msg.Kind == KindAppeal && msg.To != "" && NamesMatch(msg.To, me)
}

// DeclinedMessage builds a KindDeclined message: the mule telling a donor it will not
// come, and why. reason is truncated to ReasonMax.
func DeclinedMessage(from, to, reason string) Message {
	if len(reason) > ReasonMax {
		reason = reason[:ReasonMax]
	}
	return Message{Kind: KindDeclined, From: clampName(from), To: clampName(to), Reason: reason}
}

// NamesMatch is muleProtocol.ts's own namesMatch: case-insensitive, trimmed, and false
// for two empty names (an unset "to" should never accidentally match an unset "from").
//
// It also reads _ as a space, which muleProtocol.ts did not need to: in this fleet a bus message
// carries an account's LOGIN name ("big_net") while everything read off the game carries its
// DISPLAY name ("Big net"), and the server treats the two as one account. Without the fold a mule
// answering a donor by the name it traded under addressed nobody.
func NamesMatch(a, b string) bool {
	a, b = trimLower(a), trimLower(b)
	return a != "" && a == b
}

// AddressedTo reports whether msg is meant for me: either unaddressed (To == "", meaning
// "every mule/account listening") or addressed to me by name.
func AddressedTo(msg Message, me string) bool {
	return msg.To == "" || NamesMatch(msg.To, me)
}

func trimLower(s string) string {
	out := make([]byte, 0, len(s))
	start, end := 0, len(s)
	for start < end && s[start] == ' ' {
		start++
	}
	for end > start && s[end-1] == ' ' {
		end--
	}
	for i := start; i < end; i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c == '_' {
			c = ' '
		}
		out = append(out, c)
	}
	return string(out)
}

// maxEntries bounds the bus's memory: every account in a fleet publishes onto the same
// Bus, so this is sized generously above chatlog.Log's own per-account 100-line cap
// (fine for one account's chat; a shared multi-account bus wants more headroom before a
// slow poller could miss something between two of its own Poll calls). 3,000 since 2 Nov 2026: the whole fleet at the
// Fishing Trawler publishes its presence (trawler.go) alongside the mule's traffic.
const maxEntries = 3000

type stamped struct {
	at  time.Time
	seq uint64
	msg Message
}

// Bus is a shared, thread-safe, in-process message board — every account's Context
// should point at the same *Bus (one per fleet process), the same way every browser tab
// in the TS original shared one BroadcastChannel. It follows chatlog.Log's own
// "bounded ring buffer, poll Since(checkpoint)" shape deliberately: internal/bot's Loop
// is called repeatedly rather than driven by its own goroutine reading a channel, so a
// polled buffer fits this codebase's existing convention better than a fan-out of Go
// channels would (and is simpler than the wall-bus needs to be — see the package doc).
type Bus struct {
	mu      sync.Mutex
	entries []stamped
	seq     uint64 // the last sequence number handed out; a Watcher's cursor
}

// NewBus returns an empty, ready-to-use Bus.
func NewBus() *Bus { return &Bus{} }

// Publish records msg, timestamped now, dropping the oldest entry past the buffer's cap.
// Never blocks and never fails — a full bus just loses its oldest history, the same
// trade-off chatlog.Log already makes for the same reason (a live bot must never stall on
// a log/bus write).
func (b *Bus) Publish(msg Message) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	b.entries = append(b.entries, stamped{at: time.Now(), seq: b.seq, msg: msg})
	if len(b.entries) > maxEntries {
		b.entries = b.entries[len(b.entries)-maxEntries:]
	}
}

// Since returns every message published at or after t, oldest first. Safe to call on
// every poll (mirrors chatlog.Log.Since) — a slow poller never misses a message that
// arrived between two calls, it just sees it on the next one.
func (b *Bus) Since(t time.Time) []Message {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []Message
	for _, e := range b.entries {
		if !e.at.Before(t) {
			out = append(out, e.msg)
		}
	}
	return out
}

// Latest returns the newest message of kind sent by from at or after since, if there is one. It
// is how a donor reads its mule's last heartbeat without keeping a Watcher of its own.
func (b *Bus) Latest(kind Kind, from string, since time.Time) (Message, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := len(b.entries) - 1; i >= 0; i-- {
		e := b.entries[i]
		if e.at.Before(since) {
			break
		}
		if e.msg.Kind == kind && NamesMatch(e.msg.From, from) {
			return e.msg, true
		}
	}
	return Message{}, false
}

// Watcher tracks how much of a Bus has already been polled, so the same message is never
// handled twice by a caller that polls repeatedly — internal/bot's Loop is called over
// and over by a Runner, the same reason mulerunner.RequestWatcher exists for the chat log.
type Watcher struct {
	checkpoint time.Time
	// seq is the sequence number of the last message this watcher returned. Poll used to advance
	// a wall-clock checkpoint AFTER reading, outside the bus's lock, so a message published in
	// between was stamped before the new checkpoint and never returned to anyone. A cursor taken
	// under the same lock the messages are numbered under cannot skip one.
	seq uint64
}

// NewWatcher starts watching from now — matches mulerunner.NewRequestWatcher's own
// behavior of ignoring whatever already happened before a script started.
func NewWatcher() *Watcher { return &Watcher{checkpoint: time.Now()} }

// Poll returns every message published since the last Poll call (oldest first),
// advancing the checkpoint so none of them are returned again.
func (w *Watcher) Poll(b *Bus) []Message {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []Message
	for _, e := range b.entries {
		if e.seq > w.seq && !e.at.Before(w.checkpoint) {
			out = append(out, e.msg)
		}
	}
	w.seq = b.seq
	return out
}
