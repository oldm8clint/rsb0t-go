package mulebus

// The Nature gate's two messages (runecraft P3, docs/runecraft_p3_2026-10-14.md). The crafter (MasterCrafter, on his Nature
// round) stands outside the Nature ruins and crafts what the carriers (RuneCarrier accounts) bring him; the carriers need
// to know whether he is there, how long he is away and whether he has Natures to send home, and he needs to know who is
// working for him. Both are shapes and delivery, no policy: whether a carrier starts a shift on what it hears is
// runecraftplan.DecideCarrierShift's business.
//
//	KindRuneGate    crafter -> everyone: where he stands and in what state (opening, open, away, closed), the Natures he
//	                holds beyond his keep that a courier may take (Qty), how long a state lasts (EtaMs)
//	KindRuneCarrier carrier -> crafter: I am working for you (role: carrying, waiting at the gate, courier, going home) and
//	                what I hold (Qty), where I stand; or (role standby) I am ready on ScarabSwarm and a shift is possible, which is
//	                how the crafter counts the carriers he may start a campaign with before any gate is open (P4)
//
// Both ride the existing bus: the Bus is one ring buffer of 1,000 entries and Latest reads the newest of a kind from a name,
// so a heartbeat survives as long as fewer than a thousand messages follow it; the crafter publishes every GateEvery and a
// carrier every CarrierEvery, rates a few messages a second at most for the whole fleet.

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// The kinds.
const (
	KindRuneGate    Kind = "rune-gate"
	KindRuneCarrier Kind = "rune-carrier"
)

// GateState is what the crafter is doing, carried on a KindRuneGate message in Text.
type GateState string

// The gate's states.
const (
	// GateOpening: the crafter has started a Nature round and is on his way to the gate (EtaMs is a guess at when he is
	// there). A carrier may start its shift on this: the kit ask and the crossing take as long as his walk.
	GateOpening GateState = "opening"
	// GateOpen: he stands at the gate taking trades, or is in the middle of a craft (a few ticks away, which the carriers'
	// requests ride out).
	GateOpen GateState = "open"
	// GateAway: he is away for a while (a walk home, a hand-over): EtaMs says how long. Carriers hold what they have.
	GateAway GateState = "away"
	// GateClosed: the round is over. Carriers on shift go home.
	GateClosed GateState = "closed"
)

// Valid reports whether s is one of the four.
func (s GateState) Valid() bool {
	switch s {
	case GateOpening, GateOpen, GateAway, GateClosed:
		return true
	}
	return false
}

// CarrierRole is what a carrier is doing, carried on a KindRuneCarrier message in Text.
type CarrierRole string

// The carriers' roles.
const (
	// CarrierTransit: on the road with a kit (the crossing, the walks between the store and the gate).
	CarrierTransit CarrierRole = "transit"
	// CarrierWaiting: at the gate with a load, asking to trade.
	CarrierWaiting CarrierRole = "waiting"
	// CarrierCourier: at the gate with an empty kit, asking to take Natures home.
	CarrierCourier CarrierRole = "courier"
	// CarrierHome: going home with the kit spent.
	CarrierHome CarrierRole = "home"
	// CarrierStandby: not on a shift. The account is running ScarabSwarm, would start a shift if the gate opened (members
	// world, hitpoints above its floor, the crafter set) and says so every CarrierStandbyEvery. It is NOT at work: ReadCarriers
	// leaves it out, ReadStandby lists it.
	CarrierStandby CarrierRole = "standby"
)

// Valid reports whether r is one of the five.
func (r CarrierRole) Valid() bool {
	switch r {
	case CarrierTransit, CarrierWaiting, CarrierCourier, CarrierHome, CarrierStandby:
		return true
	}
	return false
}

// GateEvery is how often the crafter publishes his state; CarrierEvery how often a carrier does. GateTTL is how long a
// crafter's message is believed: three missed heartbeats.
const (
	GateEvery    = 5 * time.Second
	CarrierEvery = 15 * time.Second
	GateTTL      = 3 * GateEvery
	// CarrierTTL is how long a carrier counts as at work after its last message.
	CarrierTTL = 4 * CarrierEvery
	// CarrierStandbyEvery is how often a carrier between shifts says it is ready (its check on the bus is a minute apart), and
	// StandbyTTL how long it counts as ready after its last message: three missed checks.
	CarrierStandbyEvery = time.Minute
	StandbyTTL          = 3 * CarrierStandbyEvery
)

// GateMessage builds a KindRuneGate message from crafter `from`: at is the tile he stands on, state what he is doing, eta
// how long that lasts (0: unknown), surplus the Natures a courier may take now.
func GateMessage(from string, at tile.Tile, state GateState, eta time.Duration, surplus int) Message {
	ms := int(eta / time.Millisecond)
	if ms < 0 {
		ms = 0
	}
	if surplus < 0 {
		surplus = 0
	}
	return Message{Kind: KindRuneGate, From: clampName(from), Tile: at, Text: string(state), EtaMs: ms, Qty: surplus}
}

// WithCapacity returns m (a gate message) saying how many essence the crafter can take in one trade: his free pack slots besides the ones
// the essence would fill (Message.FreeSlots, which a gate message reuses). 0 is "not said": a full load of 25 (runecraftplan.CarrierOffer).
func (m Message) WithCapacity(n int) Message {
	if n < 0 {
		n = 0
	}
	m.FreeSlots = n
	return m
}

// CarrierMessage builds a KindRuneCarrier message from carrier `from` to the crafter `to`: role, the unnoted essence it
// holds (Qty) and where it stands.
func CarrierMessage(from, to string, at tile.Tile, role CarrierRole, held int) Message {
	if held < 0 {
		held = 0
	}
	return Message{Kind: KindRuneCarrier, From: clampName(from), To: clampName(to), Tile: at, Text: string(role), Qty: held}
}

// GateView is the crafter's latest KindRuneGate message, read.
type GateView struct {
	// Heard is set when a message of the crafter's was published within the TTL asked for.
	Heard bool
	// From is the crafter's name as published, Tile where he stood, World the world he is on (0: not carried).
	From  string
	Tile  tile.Tile
	World int
	State GateState
	// Eta is how long the state lasts, per the message; Surplus the Natures a courier may take; Age how old the message is. Capacity is
	// the essence he can take in one trade (0: not said, a full load).
	Eta      time.Duration
	Surplus  int
	Capacity int
	Age      time.Duration
}

// Usable reports whether a carrier may work for this gate: heard, and opening or open.
func (g GateView) Usable() bool { return g.Heard && (g.State == GateOpening || g.State == GateOpen) }

// ReadGate reads the newest KindRuneGate message from crafter published within ttl of now. A zero ttl is GateTTL.
func ReadGate(b *Bus, crafter string, now time.Time, ttl time.Duration) GateView {
	if b == nil || strings.TrimSpace(crafter) == "" {
		return GateView{}
	}
	if ttl <= 0 {
		ttl = GateTTL
	}
	// Latest wants the oldest moment worth looking at, and returns the message without its stamp, so the age is measured by
	// the newest stamped entry through Since.
	var last *Message
	var at time.Time
	for _, e := range b.stampedSince(now.Add(-ttl)) {
		if e.msg.Kind == KindRuneGate && NamesMatch(e.msg.From, clampName(crafter)) {
			m := e.msg
			last, at = &m, e.at
		}
	}
	if last == nil {
		return GateView{}
	}
	st := GateState(last.Text)
	if !st.Valid() {
		return GateView{}
	}
	return GateView{
		Heard: true, From: last.From, Tile: last.Tile, World: last.World, State: st,
		Eta: time.Duration(last.EtaMs) * time.Millisecond, Surplus: last.Qty, Capacity: last.FreeSlots, Age: now.Sub(at),
	}
}

// CarrierView is one carrier's latest message, read.
type CarrierView struct {
	Name string
	Role CarrierRole
	Held int
	Tile tile.Tile
	Age  time.Duration
}

// ReadCarriers lists the carriers at work for crafter: those whose newest KindRuneCarrier message to him, within ttl of now (a
// zero ttl is CarrierTTL), is of a role at work, newest message per carrier, in the order they first appear. A carrier whose
// newest message is its standby has gone back to ScarabSwarm and is not listed (ReadStandby lists those).
func ReadCarriers(b *Bus, crafter string, now time.Time, ttl time.Duration) []CarrierView {
	if ttl <= 0 {
		ttl = CarrierTTL
	}
	var out []CarrierView
	// The window is the longer of the two, so a standby newer than a message of work ends the work.
	for _, v := range latestCarriers(b, crafter, now, max(ttl, StandbyTTL)) {
		if v.Role != CarrierStandby && v.Age <= ttl {
			out = append(out, v)
		}
	}
	return out
}

// ReadStandby lists the carriers that stand by for crafter: those whose newest message to him, within ttl of now (a zero ttl is
// StandbyTTL), is a standby. A carrier at work (a message of another role newer than its standby) is not listed.
func ReadStandby(b *Bus, crafter string, now time.Time, ttl time.Duration) []CarrierView {
	if ttl <= 0 {
		ttl = StandbyTTL
	}
	var out []CarrierView
	for _, v := range latestCarriers(b, crafter, now, ttl) {
		if v.Role == CarrierStandby {
			out = append(out, v)
		}
	}
	return out
}

// latestCarriers is every carrier's newest message to crafter within window of now, in the order the carriers first appear.
func latestCarriers(b *Bus, crafter string, now time.Time, window time.Duration) []CarrierView {
	if b == nil || strings.TrimSpace(crafter) == "" {
		return nil
	}
	index := map[string]int{}
	var out []CarrierView
	for _, e := range b.stampedSince(now.Add(-window)) {
		m := e.msg
		if m.Kind != KindRuneCarrier || !NamesMatch(m.To, clampName(crafter)) {
			continue
		}
		role := CarrierRole(m.Text)
		if !role.Valid() {
			continue
		}
		v := CarrierView{Name: m.From, Role: role, Held: m.Qty, Tile: m.Tile, Age: now.Sub(e.at)}
		key := trimLower(m.From)
		if i, ok := index[key]; ok {
			out[i] = v
			continue
		}
		index[key] = len(out)
		out = append(out, v)
	}
	return out
}

// StandbyMessage builds the KindRuneCarrier message of a carrier that stands by: from -> crafter, at the tile it stands on.
func StandbyMessage(from, crafter string, at tile.Tile) Message {
	return CarrierMessage(from, crafter, at, CarrierStandby, 0)
}

// stampedSince is Since with the stamps, for a reader that wants a message's age.
func (b *Bus) stampedSince(t time.Time) []stamped {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []stamped
	for _, e := range b.entries {
		if !e.at.Before(t) {
			out = append(out, e)
		}
	}
	return out
}
