package runecraftplan

// The Nature gate (runecraft P3, docs/runecraft_p3_2026-10-14.md). The a user request: "runecraft with runners in the
// fleet, maxing nature runes; the main account gets the Runecraft levels and sits at the altar; other ScarabSwarm accounts run
// essence". MasterCrafter stands OUTSIDE the Nature ruins (the gate) and crafts what the carriers bring: the carrier trades
// him 25 unnoted essence, he uses the Nature talisman on the ruins, crafts inside, takes the portal out and is back at the
// gate for the next carrier. The gate needs ONE Nature talisman (the fleet has seven, all on the mule): a talisman is used on
// the ruins and nothing deletes it (runecraft.rs2:19-45), and the level is checked at the altar, not at the ruins.
//
// This file is the crafter's half, all pure: what he does next (DecideGateStage), whether a trade on the screen is taken
// (DecideGateTrade), when the round ends (DecideGateEnd), and the arithmetic the design was sized on (the cycle, the pack,
// the queue of carriers).
//
// WHY CARRIER-HOLDS-KIT. NaturePair had the MASTER hold the note stack and the coins and trade both ways. That caps the master at
// 24 free slots (talisman, notes, coins, the rune stack: trade.rs2 rejects an offer the other side has no room for) and costs a
// second offer in every trade. Here the CARRIER holds the kit (notes, coins, a rub item) and the crafter holds the talisman, the
// rune stack and the fare home, three slots and 25 free: one-way trades, 25 essence each.

import (
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// The gate's numbers.
const (
	// CarrierOffer is the unnoted essence a carrier brings to a trade: its pack is the notes, the coins, a rub item and this
	// many (28).
	CarrierOffer = 25
	// GateOfferMin is the fewest essence a trade is taken at once for (a carrier whose pack was a slot short); less is taken
	// only when the offer has settled and nothing better is coming (GateSettleMax).
	GateOfferMin = 20
	// GateSettle is how long the carrier's side must stay unchanged before the crafter accepts: Offer-All moves every essence
	// in one click, so the whole offer is on the screen in the tick after it, and a second tick makes sure.
	GateSettle = 1200 * time.Millisecond
	// GateSettleMax is how long a short offer is waited on before it is taken as it is.
	GateSettleMax = 4 * time.Second
	// GateTradeWait is how long an open screen with nothing offered is waited on before it is declined. (A carrier offers within a
	// tick of the screen opening and declines itself after three seconds without one, so this only ever ends a carrier that is stuck;
	// shortening it saves nothing that matters.)
	GateTradeWait = 25 * time.Second
	// GateRequestEvery is how soon a carrier asks again after a REFUSED request ("<name> is busy at the moment": the crafter was in
	// a trade, an open modal or the craft's hold): nothing was registered, so nothing can be undone by asking again.
	GateRequestEvery = 1200 * time.Millisecond
	// GateRequestPending is how soon it asks again when the request was neither executed nor refused (no "Sending trade offer..."
	// and no "busy" line yet): the asker is still walking up to the crafter, or the packet was dropped. Asking again is harmless
	// while no screen can be open, which is the case until a request has been EXECUTED, and six ticks is longer than the
	// execution plus the line's way back to the asker takes on the slowest route the fleet has, with a margin of two or three ticks
	// over the time the crafter's answer takes to show as a screen (the asker that asks again then closes it).
	GateRequestPending = 3600 * time.Millisecond
	// GateRequestQuiet is how long a carrier stays SILENT after "Sending trade offer..." (its request was executed: the crafter
	// has been sent ":tradereq:" and answers it). The screen opens when his answer is executed, one to four ticks later, and any
	// op on a player or walk packet either player sends once it is open closes it for BOTH (Player.clearPendingAction ->
	// closeModal -> [if_close,trademain], 16 Oct 2026: 17 of 85 handshakes were closed ~0.4 s after they opened by the carrier's
	// next timed request). Seven ticks is past the slowest handshake; only then is it asked again.
	GateRequestQuiet = 4200 * time.Millisecond
	// GateStandHold is the longest a carrier that waits beside the stand holds its first request for the crafter to reach the
	// stand: asked while he is still on the way, the engine walks the asker toward him (an op on a player runs on arrival), which
	// pulls the carriers off their tile and the crafter off the stand tile with them.
	GateStandHold = 1500 * time.Millisecond
	// GateNear is how close to the ruins' exit tile counts as at the gate.
	GateNear = 8
	// GateKeepSlots is the pack slots the crafter holds at the gate besides the essence: the talisman, the rune stack and the
	// fare home (coins). A trade can bring at most PackSlots minus this.
	GateKeepSlots = 3
	// GateFare is the coins the crafter carries for the ship home (30 each way, one way left to pay) with room for a second
	// attempt.
	GateFare = 100
	// CourierMin is the fewest Natures worth a courier's detour: the courier leaves the shelf, walks to the gate and back.
	CourierMin = 500
)

// The Nature gate's two tiles, beside the ruins (loc 2460 at 2868,3018, 3x3). Both are checked against the real collision pack and
// the locreach model by a test (runecraftrunner real-data test), which is the reason they are constants and not computed: the
// carrier and the crafter must agree on them without either asking the loc index.
var (
	// GateStandTile is where the crafter waits and the talisman is accepted from: a side of the ruins, two tiles from the portal's
	// exit tile. (2867,3019), the tile west of the ruins' middle, is NOT enterable (the collision pack: a blocked tile), which the
	// engine's own pathing treats as a wall: the walker's arrival rule stops a walk to it one tile short and the talisman is
	// then clicked from a tile that is not a stand (live 16 Oct 2026: 8 walk-to-ruins failures of 27 s each).
	GateStandTile = tile.Tile{X: 2867, Z: 3020}
	// GateWaitTile is where a carrier waits for the crafter: the tile ORTHOGONALLY beside the stand tile, so the engine's trade
	// op (which runs only from an orthogonally adjacent tile) executes the tick it is read with nobody walking to anybody.
	GateWaitTile = tile.Tile{X: 2867, Z: 3021}
)

// GateTradeMax is the most essence a trade can bring when the crafter's pack holds `occupied` slots besides it (the talisman,
// the rune stack, the fare), capped at a full pack. The NaturePair master holding notes, coins, talisman and runes (4 slots)
// takes 24; this shape (3) takes 25.
func GateTradeMax(occupied int) int { return max(0, PackSlots-occupied) }

// GateStage is the next thing the crafter does at the gate.
type GateStage string

// The stages.
const (
	// GateDone ends the round: the end rule holds and the pack holds no essence.
	GateDone GateStage = "done"
	// GateTrade stands at the gate and trades with a carrier (or waits for one).
	GateTrade GateStage = "trade"
	// GateWalkToRuins walks onto a tile the engine accepts the talisman on the ruins from.
	GateWalkToRuins GateStage = "walk-to-ruins"
	// GateUseTalisman uses the talisman on the ruins and waits to be seen inside the temple.
	GateUseTalisman GateStage = "use-talisman"
	// GateCraft binds the essence in the pack at the altar.
	GateCraft GateStage = "craft"
	// GatePortalOut leaves the temple.
	GatePortalOut GateStage = "portal-out"
	// GateStuck ends the round for a reason that will not pass at once.
	GateStuck GateStage = "stuck"
)

// GateSnapshot is what DecideGateStage reads.
type GateSnapshot struct {
	Level   int
	Members bool
	// Where is the crafter's region and Temple the key of the temple he stands in.
	Where  Region
	Temple string
	// AtRuins is set when he stands where the engine accepts a talisman on the Nature ruins.
	AtRuins bool
	// PackEssence is the unnoted essence in his pack; Talisman whether the Nature talisman is in it.
	PackEssence int
	Talisman    bool
	// End is non-empty when the round must end (DecideGateEnd): no new trade is taken, the essence held is crafted first.
	End string
}

// GateStep is DecideGateStage's answer.
type GateStep struct {
	Stage GateStage
	Why   string
}

// DecideGateStage chooses the crafter's next stage from where he stands and what he holds, afresh every time, so a restart
// anywhere (in the temple, on the doorstep with a load) carries on. Essence in the pack is always crafted before the round ends:
// a carrier has walked 120 tiles for it.
func DecideGateStage(s GateSnapshot) GateStep {
	switch s.Where {
	case RegionTemple:
		if s.Temple == KeyNature && s.PackEssence > 0 && s.Level >= Nature.Level && s.Members {
			return GateStep{GateCraft, strconv.Itoa(s.PackEssence) + " essence in the pack at the Nature altar"}
		}
		why := "no essence left in the pack"
		switch {
		case s.Temple != KeyNature:
			why = "standing in the " + s.Temple + " temple, which is not the gate's"
		case s.PackEssence > 0 && s.Level < Nature.Level:
			why = "Runecrafting " + strconv.Itoa(s.Level) + " is under the " + strconv.Itoa(Nature.Level) + " the Nature altar needs"
		case s.PackEssence > 0:
			why = "the Nature altar needs a members world"
		}
		return GateStep{GatePortalOut, why}
	case RegionMine:
		return GateStep{GateStuck, "standing in the Rune essence mine"}
	}
	if !s.Members {
		return GateStep{GateStuck, "this is not a members world: Nature runes are a members rune and the craft does nothing here"}
	}
	if s.Level < Nature.Level {
		return GateStep{GateStuck, "Runecrafting " + strconv.Itoa(s.Level) + " is under the " + strconv.Itoa(Nature.Level) + " the Nature altar needs"}
	}
	if s.PackEssence > 0 {
		if !s.Talisman {
			return GateStep{GateStuck, strconv.Itoa(s.PackEssence) + " essence in the pack and no Nature talisman to use on the ruins"}
		}
		if s.AtRuins {
			return GateStep{GateUseTalisman, strconv.Itoa(s.PackEssence) + " essence and the Nature talisman in hand, at the ruins"}
		}
		return GateStep{GateWalkToRuins, strconv.Itoa(s.PackEssence) + " essence and the Nature talisman in hand"}
	}
	if s.End != "" {
		return GateStep{GateDone, s.End}
	}
	if !s.Talisman {
		return GateStep{GateStuck, "no Nature talisman in the pack (the fleet has seven, all in the mule's bank)"}
	}
	return GateStep{GateTrade, "waiting for a carrier"}
}

// ---- the trade screen ----

// GateTradeKind is what the crafter does with the trade screen in front of him.
type GateTradeKind string

// The trade screen actions.
const (
	// GateTradeWaitFor leaves the screen as it is.
	GateTradeWaitFor GateTradeKind = "wait"
	// GateTradeAccept presses Accept (the offer screen, then the confirm screen).
	GateTradeAccept GateTradeKind = "accept"
	// GateTradeDecline closes the screen.
	GateTradeDecline GateTradeKind = "decline"
	// GateTradeGive puts Natures on his side of the screen for a courier.
	GateTradeGive GateTradeKind = "give"
)

// GateTradeAction is DecideGateTrade's answer.
type GateTradeAction struct {
	Kind GateTradeKind
	// Qty is the Natures a GateTradeGive puts up.
	Qty    int
	Reason string
}

// GateTradeView is the offer screen as the crafter sees it.
type GateTradeView struct {
	// Allowed says the partner on the screen is one of his carriers.
	Allowed bool
	Partner string
	// TheirEssence is the unnoted essence on their side; TheirOther the count of anything else.
	TheirEssence, TheirOther int
	// MyOffer is the items on his side; FreeSlots his free pack slots.
	MyOffer, FreeSlots int
	// Courier says the partner announced itself as a courier on the bus, and CourierQty is the Natures he may give it now.
	Courier    bool
	CourierQty int
	// Opened is how long the screen has been open; Settled how long their side has been unchanged.
	Opened, Settled time.Duration
	// GateEnding says the round is ending: a short offer is taken at once.
	GateEnding bool
}

// DecideGateTrade is the policy for the offer screen. The essence trade is one-way: the carrier's Offer-All is on the screen in
// one tick, and the crafter accepts a settled offer of GateOfferMin or more, or a shorter one once it has been waited on
// GateSettleMax. A courier trade is the other way: the partner offers nothing, and the crafter puts up the Natures.
func DecideGateTrade(v GateTradeView) GateTradeAction {
	if !v.Allowed {
		return GateTradeAction{GateTradeDecline, 0, strings.TrimSpace(v.Partner) + " is not one of the carriers"}
	}
	if v.TheirOther > 0 {
		return GateTradeAction{GateTradeDecline, 0, "the offer holds something that is not Rune essence"}
	}
	if v.TheirEssence > 0 {
		if v.TheirEssence > v.FreeSlots {
			return GateTradeAction{GateTradeDecline, 0, "offered " + strconv.Itoa(v.TheirEssence) + " essence and only " + strconv.Itoa(v.FreeSlots) + " slots are free"}
		}
		if v.MyOffer > 0 {
			return GateTradeAction{GateTradeDecline, 0, "my side of the screen is not empty"}
		}
		switch {
		case v.TheirEssence >= CarrierOffer:
			// A full load cannot grow (the carrier's pack is its notes, coins, ring and these 25): waiting out the settle would cost
			// two of the cycle's 24 to 28 ticks. The confirm screen is still checked against the offer.
			return GateTradeAction{GateTradeAccept, 0, "a full load of " + strconv.Itoa(v.TheirEssence) + " essence"}
		case v.FreeSlots >= GateOfferMin && v.TheirEssence >= v.FreeSlots:
			// ... and so is an offer of every slot he has free (a stack of bows or a random event's fruit takes one, and the carriers
			// are told to bring what he can take): it cannot grow either, he has no room for more.
			return GateTradeAction{GateTradeAccept, 0, "a full load of " + strconv.Itoa(v.TheirEssence) + " essence, all the room there is"}
		case v.TheirEssence >= GateOfferMin && v.Settled >= GateSettle:
			return GateTradeAction{GateTradeAccept, 0, "a settled offer of " + strconv.Itoa(v.TheirEssence) + " essence"}
		case v.Settled >= GateSettleMax || (v.GateEnding && v.Settled >= GateSettle):
			return GateTradeAction{GateTradeAccept, 0, "a short offer of " + strconv.Itoa(v.TheirEssence) + " essence, taken as it stands"}
		}
		return GateTradeAction{GateTradeWaitFor, 0, "the offer is still settling"}
	}
	// Nothing on their side.
	if v.Courier && v.CourierQty > 0 {
		if v.MyOffer == 0 {
			return GateTradeAction{GateTradeGive, v.CourierQty, "a courier is here for " + strconv.Itoa(v.CourierQty) + " Nature runes"}
		}
		if v.Settled >= GateSettle {
			return GateTradeAction{GateTradeAccept, 0, "the Natures are on my side"}
		}
		return GateTradeAction{GateTradeWaitFor, 0, "the courier has not accepted yet"}
	}
	if v.Opened >= GateTradeWait {
		return GateTradeAction{GateTradeDecline, 0, "nothing was offered in " + GateTradeWait.String()}
	}
	return GateTradeAction{GateTradeWaitFor, 0, "waiting for the offer"}
}

// CourierQuantity is the Natures a courier may take home: what the crafter holds beyond his keep, at most the courier's cap.
// Under the keep there is nothing to send.
func CourierQuantity(held, keep, max int) int {
	n := held - keep
	if n <= 0 || max <= 0 {
		return 0
	}
	if n > max {
		n = max
	}
	return n
}

// ---- the end of the round ----

// GateEndView is what DecideGateEnd reads.
type GateEndView struct {
	Now, Started time.Time
	// MaxRound is craftRuneMaxHours.
	MaxRound time.Duration
	// LastTrade is when essence last arrived (zero: none since Started); LastCarrier when a carrier was last seen on the bus
	// (zero: none since Started).
	LastTrade, LastCarrier time.Time
	// Carriers is how many are at work now.
	Carriers int
}

// The patience of the end rule.
const (
	// GateWarmUp is how long a round waits for its first carrier: the kit ask, the crossing and the corridor take most of ten
	// minutes.
	GateWarmUp = 12 * time.Minute
	// GateNoCarriers is how long the gate waits with no carrier on the bus before it closes.
	GateNoCarriers = 15 * time.Minute
	// GateDry is how long it waits with carriers about but no essence arriving.
	GateDry = 30 * time.Minute
)

// DecideGateEnd says whether the round is over and why: its time is up, no carrier has come for GateNoCarriers, or no essence
// has come for GateDry. The warm-up counts from the start of the round.
func DecideGateEnd(v GateEndView) (end bool, why string) {
	if v.MaxRound > 0 && v.Now.Sub(v.Started) >= v.MaxRound {
		return true, "the round's time (craftRuneMaxHours " + v.MaxRound.String() + ") is used up"
	}
	since := func(t time.Time) time.Duration {
		if t.Before(v.Started) {
			t = v.Started
		}
		return v.Now.Sub(t)
	}
	if v.Carriers == 0 && v.Now.Sub(v.Started) >= GateWarmUp && since(v.LastCarrier) >= GateNoCarriers {
		return true, "no carrier has been at work for " + since(v.LastCarrier).Round(time.Minute).String()
	}
	if v.Now.Sub(v.Started) >= GateWarmUp && since(v.LastTrade) >= GateDry {
		return true, "no essence has arrived for " + since(v.LastTrade).Round(time.Minute).String()
	}
	return false, ""
}

// ---- the cycle, the queue and the energy model the design was sized on ----

// GateCycle is the ticks of one trip of the crafter, from the trade to the next gate.
type GateCycle struct {
	// Trade is the trade screens (request, offer, two accepts, close); WalkOut the steps from the exit tile back to a tile the
	// ruins accept the talisman from; Talisman the cast and the jump (op, p_delay(1)); Inside the steps to the altar, the
	// craft's own tick and its four-tick hold; Portal the portal (no delay at all).
	Trade, WalkOut, Talisman, Inside, Portal int
}

// Ticks is the whole cycle.
func (c GateCycle) Ticks() int { return c.Trade + c.WalkOut + c.Talisman + c.Inside + c.Portal }

// The design's cycles: a fast one (trade 7, 2 steps, the cast 4, inside 9, portal 0 would be 22; the design's 24) and a slow one
// (28). A Nature cycle is 24 to 28 ticks for the 25 essence a trade brings.
var (
	GateCycleFast = GateCycle{Trade: 8, WalkOut: 2, Talisman: 4, Inside: 10, Portal: 0}
	GateCycleSlow = GateCycle{Trade: 10, WalkOut: 2, Talisman: 5, Inside: 11, Portal: 0}
)

// TicksPerHour is the engine's 6,000 ticks an hour.
const TicksPerHour = 6000

// GateRate is the essence an hour the crafter can take at a cycle for a trade of `perTrade` essence: the cap on the whole
// operation, whatever the carriers bring.
func GateRate(c GateCycle, perTrade int) int {
	if c.Ticks() <= 0 {
		return 0
	}
	return perTrade * TicksPerHour / c.Ticks()
}

// CarrierTrip is the ticks of one carrier trip: the walk to the gate and back, the shelf, the trade.
type CarrierTrip struct {
	// Tiles is the steps of the round trip between the store and the gate, Idle the ticks not moving (the shelf, the trade).
	Tiles, Idle int
}

// CarrierTripDefault is the design's: 244 tiles (122 each way) and about 30 ticks standing.
var CarrierTripDefault = CarrierTrip{Tiles: 244, Idle: 30}

// QueueRate is the essence an hour n carriers bring a crafter that can take `cap` an hour: each carrier brings `perTrade` per
// trip of `tripTicks`, and the gate takes no more than cap. It is the simple closed-queue bound (the carriers do not wait on one
// another until the gate is saturated), which is what the design's 2.7k, 4.0k, 5.4k and 6.0k an hour for 4, 6, 8 and 10 runners
// are.
func QueueRate(n, perTrade, tripTicks, cap int) int {
	if n <= 0 || tripTicks <= 0 {
		return 0
	}
	got := n * perTrade * TicksPerHour / tripTicks
	if cap > 0 && got > cap {
		return cap
	}
	return got
}

// CarriersFor is how many carriers saturate a gate that takes `cap` an hour, each bringing perTrade per tripTicks (rounded up).
func CarriersFor(perTrade, tripTicks, cap int) int {
	per := perTrade * TicksPerHour / max(1, tripTicks)
	if per <= 0 {
		return 0
	}
	return (cap + per - 1) / per
}

// GateExit and the stand rule: the crafter's wait tile is the tile beside the ruins the talisman is accepted from that is nearest
// the portal's exit, so the walk after every portal is as short as the map allows.
func GateExit() tile.Tile { return Nature.Exit }

// NearGate reports whether t is at the gate: on the surface within GateNear of the Nature ruins' exit tile.
func NearGate(t tile.Tile) bool {
	return t.Level == 0 && tile.Chebyshev(t, Nature.Exit) <= GateNear
}
