package combatplan

import (
	"sort"
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// The shop-trip budget: how many cross-map buying journeys a band of training may spend, and
// which purchases share one journey.
//
// This file exists because of a measurement, not a hunch. Two arms of cmd/combattrainertest
// -mode marathon from Defence 1, identical but for buyGear, were run on 2026-09-19 (COLLAB,
// commit 0a4385f): the same Al Kharid band gained 68 XP/min with gear buying ON and 506 with it
// OFF, the travel-to-combat log-line ratio was 3.0:1 against 0.28:1, and 19 shop trips bought 4
// items. The fighting was identical in both arms — the buying arm's own in-rung heartbeats read
// 308 XP/min while its BAND average was 68 — so what the run lost was commuting, not killing.
//
// The mechanism is structural rather than a bug in any one step, and it is worth naming because
// each piece looks reasonable alone:
//
//  1. BuyPlan is ordered by MeleeSlots (weapon, body, legs, helm, shield), and those five
//     keepers stand in FIVE different places — Varrock, Varrock, Al Kharid, Barbarian Village,
//     Falador. Slot order is upgrade priority; it is not a route.
//  2. gearStep buys one slot per trip and then clears haveRung, so the account walks all the way
//     back to the rung after every single purchase.
//  3. mayShop re-arms after ONE kill, which at the Al Kharid rung is about twenty seconds.
//
// Together those turn five gear slots into five cross-map round trips, each paid for by one kill.
// The two answers here are the two halves of the cost: ClusterBySite stops the run making a
// separate journey per slot, and the budget stops it making an unbounded number of journeys per
// band.
//
// Pure, like the rest of this package: every input is a value the caller reads and passes in, so
// the rules below are unit-tested rather than only observed live.

// ShopStop is one journey to one keeper, and everything the plan wants to buy there.
type ShopStop struct {
	Site ShopSite
	// Buy is what to ask this keeper for, in the plan's own priority order.
	Buy []Purchase
}

// Slots names the slots this stop would fill, for a log line that can be audited.
func (s ShopStop) Slots() string {
	names := make([]string, 0, len(s.Buy))
	for _, p := range s.Buy {
		names = append(names, string(p.Slot))
	}
	return strings.Join(names, "+")
}

// ClusterBySite turns a slot-ordered buy plan into one stop per shop, so a keeper who can supply
// two of the things the plan wants is asked for both while the account is standing there.
//
// Measured limit, and it is worth stating because the first version of this file assumed
// otherwise and its own test caught it: on the MELEE ladder this saves nothing. At melee 30 the
// five slots resolve to five different shops — the Varrock sword shop and Horvik's armour shop
// are separate ShopSites even though both are in Varrock, and Falador's chainmail and shield
// shops likewise. So one stop per shop is one stop per slot there, and the saving comes entirely
// from the buying RUN below visiting them all on one journey rather than from batching.
//
// Where it does pay is the consumables: a rune shopping list asks Aubury for four kinds of rune
// at once, and a Ranged kit asks Lowe for a bow and its arrows. Those are one stop, not four.
//
// Stop order follows the plan's own order: the stop holding the highest-priority purchase comes
// first, because the weapon is what makes the next kill faster and a run that runs out of coins
// partway should have spent them on the front of the plan. Within a stop the purchases keep
// their plan order for the same reason.
func ClusterBySite(plan []Purchase) []ShopStop {
	var stops []ShopStop
	at := map[string]int{}
	for _, p := range plan {
		key := p.Site.Shop
		if i, seen := at[key]; seen {
			stops[i].Buy = append(stops[i].Buy, p)
			continue
		}
		at[key] = len(stops)
		stops = append(stops, ShopStop{Site: p.Site, Buy: []Purchase{p}})
	}
	return stops
}

// OrderStopsFrom sorts stops nearest-first from a tile, keeping the plan's order as the
// tie-break.
//
// Nearest-first rather than priority-first, and the reason is the thing this file is about: once
// a buying run is open the account is already away from its rung, so the question is no longer
// "what do I want most" but "what is cheapest to reach from here". A run that does Falador,
// then Al Kharid, then Barbarian Village crosses the map three times; the same three stops in
// distance order cross it once. Priority still decides what gets bought when the coins run out,
// because ClusterBySite has already put the best purchase at the front of each stop.
//
// Chebyshev over the keeper's spawn tile is deliberately a rough measure. It ignores doors,
// gates and the toll, so it is an ordering heuristic and never a route — the walker still plans
// the real path. A wrong ordering costs a longer walk, never a failed trip.
func OrderStopsFrom(stops []ShopStop, from tile.Tile) []ShopStop {
	out := make([]ShopStop, len(stops))
	copy(out, stops)
	order := map[string]int{}
	for i, s := range out {
		order[s.Site.Shop] = i
	}
	sort.SliceStable(out, func(i, j int) bool {
		di := tile.Chebyshev(from, out[i].Site.Stand)
		dj := tile.Chebyshev(from, out[j].Site.Stand)
		if di != dj {
			return di < dj
		}
		return order[out[i].Site.Shop] < order[out[j].Site.Shop]
	})
	return out
}

// TripBudget is how much commuting a run may spend on gear.
//
// The numbers are set against the measured band rather than chosen for roundness. The Al Kharid
// band is 10-25, about 6,700 XP on one stat at 76 XP a kill, so roughly 88 kills; arm A spent
// 17.2 minutes of it at 68 XP/min where arm B spent 6.5 at 506. One buying run per band with at
// most five stops covers every keeper the melee ladder uses exactly once, which is the most a
// band can possibly need — the plan cannot want a sixth slot it does not have.
type TripBudget struct {
	// RunsPerBand is how many separate buying journeys one band of training may start.
	RunsPerBand int
	// StopsPerRun caps one journey, so a run whose every stop fails still ends.
	StopsPerRun int
	// KillsPerRun is what a SECOND journey in the same band costs, in kills since the last one.
	// The first journey of a band is free: an account that has just walked into a harder rung
	// wearing the last one's gear should be allowed to fix that before it fights.
	KillsPerRun int
}

// DefaultTripBudget is one buying run per band, five stops, and a second run in the same band
// priced at forty kills.
//
// Five stops because the melee gear ladder resolves to five keepers, one per slot (see
// ClusterBySite for why they do not share). Forty kills because that is roughly half the Al
// Kharid band, so a band can afford at most two journeys however well it is killing — against
// the measured 19 trips, of which 15 bought nothing.
var DefaultTripBudget = TripBudget{RunsPerBand: 1, StopsPerRun: 5, KillsPerRun: 40}

// Normalise fills in anything a caller left at zero, so a settings bag that names none of these
// still gets the measured defaults rather than a budget of nothing. A budget of nothing would
// read as "never shop", which is a different feature.
func (b TripBudget) Normalise() TripBudget {
	if b.RunsPerBand <= 0 {
		b.RunsPerBand = DefaultTripBudget.RunsPerBand
	}
	if b.StopsPerRun <= 0 {
		b.StopsPerRun = DefaultTripBudget.StopsPerRun
	}
	if b.KillsPerRun <= 0 {
		b.KillsPerRun = DefaultTripBudget.KillsPerRun
	}
	return b
}

// TripRequest is what the run knows when it asks whether it may go shopping.
//
// Band is the rung key rather than a level, because that is what the budget resets against: a
// run that crosses from cows to alkharid has genuinely earned a fresh allowance, since the gear
// it wants is different on the other side of the boundary.
type TripRequest struct {
	Budget TripBudget
	// Band is the current rung key. A change resets RunsThisBand, which the caller does.
	Band string
	// RunsThisBand counts journeys already started in this band.
	RunsThisBand int
	// KillsSinceRun is kills since the last journey ended.
	KillsSinceRun int
	// TripsThisRun counts trips made in the whole run, so the very first one can be free.
	TripsThisRun int
	// WantToBuy is whether there is anything to buy at all. A budget question is meaningless
	// when the plan is empty, and answering "allowed" there would let the caller start an empty
	// journey.
	WantToBuy bool
}

// TripVerdict is the answer, carrying the reason so the log can be audited rather than trusted.
type TripVerdict struct {
	Allowed bool
	Why     string
}

// MayStartRun decides whether the account may leave its rung to go shopping.
//
// The first journey of the whole run is free, which is the one rule carried over unchanged from
// the mayShop this replaces: an account holding nothing worth swinging should arm itself before
// it fights anything. Everything after that is priced.
//
// What is deliberately NOT here: a wall-clock fallback. The rule this replaces let a run that
// could not kill anything shop every ten minutes regardless, so that a blocked rung could not
// freeze its gear for good. That is the right instinct and the wrong lever — a run that cannot
// kill anything has a rung problem, and ten minutes of walking to a shop does not fix it, it
// just hides it behind a number that looks like progress. A run whose rung is blocked now says
// so through the travel failure path instead.
func MayStartRun(r TripRequest) TripVerdict {
	b := r.Budget.Normalise()
	if !r.WantToBuy {
		return TripVerdict{false, "nothing the shops can still improve"}
	}
	if r.TripsThisRun == 0 {
		return TripVerdict{true, "the first buying run of the account is free, so it can arm itself before it fights"}
	}
	if r.RunsThisBand >= b.RunsPerBand {
		return TripVerdict{false, plural(r.RunsThisBand, "buying run") + " already spent in band " + quoted(r.Band) +
			", which is the budget — training on until the band changes"}
	}
	if r.KillsSinceRun < b.KillsPerRun {
		return TripVerdict{false, "a further buying run in band " + quoted(r.Band) + " costs " +
			itoa(b.KillsPerRun) + " kills and only " + itoa(r.KillsSinceRun) + " have been made since the last one"}
	}
	return TripVerdict{true, itoa(r.KillsSinceRun) + " kills since the last buying run pays for another in band " + quoted(r.Band)}
}

// MayMakeStop reports whether an OPEN buying run may make another stop.
//
// Separate from MayStartRun because the two costs are different, and conflating them is what
// made the old rule so expensive: the price of shopping is the commute out and back, which one
// journey pays once however many keepers it visits. So a stop inside an open run costs nothing
// extra and is capped only by StopsPerRun, which exists so that a journey whose every stop fails
// still ends rather than walking forever.
func MayMakeStop(b TripBudget, stopsThisRun int) bool {
	return stopsThisRun < b.Normalise().StopsPerRun
}

func quoted(s string) string { return "\"" + s + "\"" }

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return itoa(n) + " " + word + "s"
}
