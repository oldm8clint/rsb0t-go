package fleetplan

// The one teleport decision (teleport-walks, a user request: "teleport runes should be considered every navigation
// trip and if it may be closer to teleport rather than run").
//
// ChooseTravelTeleport (teletravel.go) judged a walk by straight-line tiles plus a flat 300 for crossing the sea, before
// the Walker had planned anything: a Chebyshev estimate that over-priced the spirit-tree route from Ardougne to Varrock
// (real cost 191, the estimate 300+) and under-priced a walk that must go round a mountain. DecideTeleport judges in
// TICKS and from the planner's own routes: the Walker has already planned the walk it is about to follow (the cost of
// walking from here), and the I/O layer prices the walk from a candidate landing (LandCost). The cast is chosen when
// casting and then walking from the landing is at least a margin faster than walking from here, after the run energy,
// the weight and the ships on each route are taken into account (WalkTicks).
//
// Pure: no client, no actions, no webwalk. The Walker's route is passed in as RouteFacts, the landing's price as a func.
//
// What it will not do, in the order it checks (cheapest first, none of them plan):
//
//	off, no spell at the account's Magic level, mule-walk-only (the 6 Oct rollback), in combat, above Wilderness level 20
//	(teleport.rs2 refuses every spell there), in a pocket or strand site the Guardian owns, carrying something a
//	teleport destroys, a walk shorter than the cast plus the margin, the fleet-wide hourly cap.
//
// Laws are a finite hoard (the only shop selling them is Lundail's, 250 in stock, one restocked per 300 ticks), so the
// margin a cast must save grows as the mule's published stock falls (LawPrice): 24 ticks at 20,000 and over, x2 under
// 20,000, x4 under 8,000, x8 under 3,000, and under 1,000 only the walk to the mule may cast.

import (
	"sort"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Teleport-decision defaults (the teleTravel* settings of the same names, internal/bot/scarabswarm_travel.go).
const (
	// DefaultMinSaveTicks is the least ticks a cast must save after its own delay, at the cheapest law price.
	DefaultMinSaveTicks = 24
	// DefaultCastTicks is what the cast itself costs: the click, p_delay(1) and p_delay(0) and the animation reset.
	DefaultCastTicks = 6
	// DefaultSettleTicks is how long after its own alchemy cast or rub an account waits before the spell click: a
	// delayed player drops the click silently (engine Player.runScript returns -1 while delayed).
	DefaultSettleTicks = 5
	// DefaultShipTicks is the extra ticks one ship hop counts beyond the planner's own price (18): the dialogue and the
	// gangplank. The sail itself measured 8 s live (13 ticks).
	DefaultShipTicks = 40
	// DefaultMaxPerHour is the casts one account makes an hour that save under CheapCastTicks.
	DefaultMaxPerHour = 12
	// DefaultFleetPerHour is the casts the whole process makes an hour; the brake on a law-drain bug.
	DefaultFleetPerHour = 900
	// DefaultMissedTicks is the walking, in ticks, an account must have done that a cast it could not pay for would have
	// saved (inside MissedWindow) before it asks the mule for a kit of its own.
	DefaultMissedTicks = 900

	// CheapCastTicks is the saving from which a cast ignores the per-account hourly cap (never the fleet's).
	CheapCastTicks = 200
	// TeleportNearTiles is how close to a landing the account must not already be: a cast to a landing it stands beside
	// saves nothing it can price.
	TeleportNearTiles = 25
	// TeleportRepeatWindow is how long after landing on a tile the same landing is not chosen again: the loop guard.
	TeleportRepeatWindow = 3 * time.Minute
	// MaxLandingPrices bounds the planner runs one decision may spend on landings (branch and bound usually runs one).
	MaxLandingPrices = 2
	// MissedFloorTicks is the saving a spell the account could not pay for must have promised to be counted as missed.
	MissedFloorTicks = 150
	// MissedWindow is how long a missed saving counts toward MissedTicks.
	MissedWindow = 3 * time.Hour

	// UnwalkableTicks stands for a walk that has no route: bigger than any real one, small enough to add to.
	UnwalkableTicks = 1 << 20
	// stalledDrain is the share of the plain run drain a walk that stalls (a rub or an alchemy cast every few ticks) pays:
	// the tick lab measured 0.125-0.139 against 0.351-0.393 percent a tile (runstall.go).
	stalledDrain = 0.35
	// unknownEnergy stands for a run energy that has not been read.
	unknownEnergy = 50
)

// RouteFacts is what a planned walk is made of, in the planner's own units (webwalk.Route.Cost: run-tiles, two a tick).
type RouteFacts struct {
	// OK is false for a walk that has no route (the plan failed or ran out of budget): it is priced as infinite.
	OK bool
	// TileUnits is the cost of the walked tiles, HopUnits the cost of the hops (doors, stairs, ships), Ships how many of
	// those hops are ships.
	TileUnits, HopUnits, Ships int
}

// WalkTicks is how many ticks a walk of r takes an account with energy percent of run energy left (energyKnown false:
// half), carrying kg kilograms, whose walk stalls or not. The account runs while its energy lasts and walks after, at one
// tile a tick; the planner's hop costs are converted at two units a tick, and every ship adds shipTicks for the fare dialogue
// and the gangplank. Run energy regenerating on the way is ignored, which only makes a long walk look slower than it is.
//
// 300 tile units at full energy and 10 kg: 170 ticks; at 10%: 287; at none: 300.
func WalkTicks(r RouteFacts, energy int, energyKnown bool, kg int, stalled bool, shipTicks int) int {
	if !r.OK {
		return UnwalkableTicks
	}
	if !energyKnown {
		energy = unknownEnergy
	}
	energy = min(max(energy, 0), 100)
	drain := PlainDrainPerTile(kg)
	if stalled {
		drain *= stalledDrain
	}
	runTiles := r.TileUnits
	if drain > 0 {
		runTiles = min(r.TileUnits, int(float64(energy)/drain))
	}
	return runTiles/2 + (r.TileUnits - runTiles) + r.HopUnits/2 + r.Ships*max(shipTicks, 0)
}

// LawPrice is the multiplier on the least saving a cast must make, from the mule's published law stock, and whether the
// stock is so low that only the walk to the mule may cast. lawsHeld is the account's own laws: with no reading of the mule an
// account that holds some pays the middle price, one that holds none cannot cast anyway.
func LawPrice(muleLaws int, known bool, lawsHeld int) (mult int, muleWalkOnly bool) {
	if !known {
		if lawsHeld > 0 {
			return 2, false
		}
		return 1, true
	}
	switch {
	case muleLaws >= 20000:
		return 1, false
	case muleLaws >= 8000:
		return 2, false
	case muleLaws >= 3000:
		return 4, false
	case muleLaws >= 1000:
		return 8, false
	}
	return 1, true
}

// The reasons DecideTeleport declines, constant per cause so a log line can be counted by them (a number that varies is in
// TeleDecision.Detail). The first four are the settings' doing and need no remark in a log.
const (
	DeclineOff         = "teleport travel off"
	DeclineSwitchedOff = "casting is switched off"
	DeclineMagic       = "Magic below the lowest teleport"
	DeclineMuleOnly    = "only the walk to the mule may teleport"
	DeclineLowLaws     = "the mule's laws are low: only the walk to the mule may cast"
	DeclineCombat      = "in combat"
	DeclineWilderness  = "above Wilderness level 20"
	DeclinePocket      = "standing where the Guardian owns the way out"
	DeclineCasualty    = "carrying something a teleport destroys"
	DeclineShort       = "a short walk"
	DeclineFleetCap    = "fleet cap"
	DeclineHourlyCap   = "hourly cap"
	DeclineUnpayable   = "no payable teleport"
	DeclineUnpriced    = "no landing could be priced"
	DeclineSaving      = "saves too little"
	// DeclineEvent is the hook's (a random event is present), not DecideTeleport's.
	DeclineEvent = "a random event is present"
)

// QuietDecline reports a decline that is the settings' doing, which is no news in a log.
func QuietDecline(why string) bool {
	switch why {
	case DeclineOff, DeclineSwitchedOff, DeclineMagic, DeclineMuleOnly:
		return true
	}
	return false
}

// TeleCast is a cast that landed: the spell, where it set the account down and when.
type TeleCast struct {
	Spell   string
	Landing tile.Tile
	At      time.Time
}

// TeleDecisionInput is what one decision reads.
type TeleDecisionInput struct {
	Here, Dest tile.Tile
	// HereRoute is the walk the Walker has already planned from Here to Dest.
	HereRoute RouteFacts
	// LandCost prices the walk from a landing to Dest (one planner run); nil prices nothing, so nothing is chosen.
	LandCost func(land tile.Tile) RouteFacts

	Magic   int
	Members bool
	// Pack is the runes carried; Staff the staff WORN ("" for none: the spell needs it worn, not owned).
	Pack  RuneStock
	Staff string
	// QuestDone answers a quest gate; nil treats every quest as not done. Blocked is a spell the server refused lately.
	QuestDone func(quest string) bool
	Blocked   func(spell string) bool
	// Carrying names what the pack holds, for TeleportDestroys.
	Carrying []string

	InCombat bool
	// PocketOrStrand is the account standing in the Mage Bank pocket or a strand site the Guardian owns.
	PocketOrStrand bool
	// MuleWalk is the walk to the mule's post.
	MuleWalk bool

	Energy      int
	EnergyKnown bool
	KG          int
	Stalled     bool

	LastCast TeleCast
	Now      time.Time
	// CastsLastHour is this account's casts in the last hour, FleetCastsLastHour the whole process's.
	CastsLastHour, FleetCastsLastHour int
	// MuleLaws is the mule's published law stock, MuleKnown false with no fresh reading.
	MuleLaws  int
	MuleKnown bool
}

// TeleDecision is a chosen cast, or (with a declined reason from DecideTeleport) what was found while declining.
type TeleDecision struct {
	Teleport TravelTeleport
	// HereTicks is the walk from here, LandTicks the walk from the landing (without the cast), Save what casting saves.
	HereTicks, LandTicks, Save int
	// Price is the law-price multiplier that applied; Priced the planner runs spent on landings.
	Price, Priced int
	// Detail is the numbers behind a decline that has them ("saves 23 ticks, needs 24").
	Detail string
	// MissedSave is the largest saving, in ticks, that a spell the account could not pay for would have made at best (the
	// lower bound), 0 when none reached MissedFloorTicks. The caller counts it only when it declines.
	MissedSave int
}

// Landing is the chosen teleport's landing square.
func (d TeleDecision) Landing() tile.Tile {
	return tile.Tile{X: d.Teleport.DestX, Z: d.Teleport.DestZ}
}

// LowestTeleportLevel is the Magic level of the first teleport a walk may use.
func LowestTeleportLevel() int {
	lowest := 0
	for _, tp := range TravelTeleports() {
		if lowest == 0 || tp.Spell.Level < lowest {
			lowest = tp.Spell.Level
		}
	}
	return lowest
}

// DecideTeleport is the teleport, if any, to cast before following a walk from in.Here to in.Dest, and otherwise the reason
// there is none (a short phrase for the log, constant per cause so it can be counted). It plans nothing itself: a walk too
// short, a gate closed, a cap reached all answer before in.LandCost is ever called, and at most MaxLandingPrices landings are
// priced after that, the nearest to the destination first.
func DecideTeleport(s TeleTravelSettings, in TeleDecisionInput) (TeleDecision, string) {
	if !s.Enabled {
		return TeleDecision{}, DeclineOff
	}
	s = s.Normalised()
	if s.FleetPerHour < 0 {
		return TeleDecision{}, DeclineSwitchedOff
	}
	if in.Magic < LowestTeleportLevel() {
		return TeleDecision{}, DeclineMagic
	}
	if !s.AnyWalk && !in.MuleWalk {
		return TeleDecision{}, DeclineMuleOnly
	}
	price, muleOnly := LawPrice(in.MuleLaws, in.MuleKnown, in.Pack[RuneLaw])
	if muleOnly && !in.MuleWalk {
		return TeleDecision{Price: price}, DeclineLowLaws
	}
	if in.InCombat {
		return TeleDecision{}, DeclineCombat
	}
	if WildernessLevel(in.Here) > 20 {
		return TeleDecision{}, DeclineWilderness
	}
	if in.PocketOrStrand {
		return TeleDecision{}, DeclinePocket
	}
	if _, bad := carriesTeleportCasualty(in.Carrying); bad {
		return TeleDecision{}, DeclineCasualty
	}
	hereTicks := WalkTicks(in.HereRoute, in.Energy, in.EnergyKnown, in.KG, in.Stalled, s.ShipTicks)
	minSave := s.MinSaveTicks * price
	if in.HereRoute.OK && hereTicks < s.CastTicks+minSave {
		return TeleDecision{HereTicks: hereTicks, Price: price}, DeclineShort
	}
	if in.FleetCastsLastHour >= s.FleetPerHour {
		return TeleDecision{HereTicks: hereTicks, Price: price}, DeclineFleetCap
	}
	// The per-account cap only holds back casts that save little: the longest walks are the ones worth the laws.
	capped := in.CastsLastHour >= s.MaxPerHour
	need := minSave
	if capped {
		need = max(need, CheapCastTicks)
	}

	type cand struct {
		tp   TravelTeleport
		land tile.Tile
		lb   int
	}
	var cands []cand
	missed := 0
	for _, tp := range TravelTeleports() {
		if !TeleportAllowed(tp, in.Magic, in.Members, in.QuestDone, in.Blocked) {
			continue
		}
		land := tile.Tile{X: tp.DestX, Z: tp.DestZ}
		if tile.Chebyshev(tile.Tile{X: in.Here.X, Z: in.Here.Z}, land) < TeleportNearTiles {
			continue
		}
		if in.LastCast.Landing == land && !in.LastCast.At.IsZero() && in.Now.Sub(in.LastCast.At) < TeleportRepeatWindow {
			continue
		}
		// The least the walk from the landing can take: a straight run, two tiles a tick (an underground destination has no
		// straight line to a surface landing: its entrance can be anywhere, so only the cast itself is a lower bound).
		lb := s.CastTicks
		if in.Dest.Z < 6400 {
			lb += (tile.Chebyshev(land, tile.Tile{X: in.Dest.X, Z: in.Dest.Z}) + 1) / 2
		}
		if in.HereRoute.OK && lb+need >= hereTicks {
			continue
		}
		if !TeleportPayable(tp, in.Pack, in.Staff, s.LawReserve, s.AirRunes) {
			// The lower bound knows nothing of the sea, so only a landing on the destination's own side of it is a
			// believable measure of what the cast would have saved.
			if in.HereRoute.OK && hereTicks-lb >= MissedFloorTicks && TravelSide(land) == TravelSide(in.Dest) {
				missed = max(missed, hereTicks-lb)
			}
			continue
		}
		cands = append(cands, cand{tp: tp, land: land, lb: lb})
	}
	out := TeleDecision{HereTicks: hereTicks, Price: price, MissedSave: missed}
	if len(cands) == 0 {
		if capped {
			return out, DeclineHourlyCap
		}
		out.Detail = "none saves " + Itoa(need) + " ticks"
		return out, DeclineUnpayable
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].lb < cands[j].lb })

	// Branch and bound: the candidate with the smallest lower bound is priced first; the next only while its lower bound
	// could still beat the best real price by the margin.
	bestTotal := UnwalkableTicks
	var best cand
	found := false
	for _, c := range cands {
		if out.Priced >= MaxLandingPrices || in.LandCost == nil {
			break
		}
		if found && c.lb+need >= bestTotal {
			break
		}
		facts := in.LandCost(c.land)
		out.Priced++
		if !facts.OK {
			continue
		}
		land := WalkTicks(facts, in.Energy, in.EnergyKnown, in.KG, in.Stalled, s.ShipTicks)
		total := land + s.CastTicks
		better := !found || total < bestTotal ||
			(total == bestTotal && (LawCost(c.tp) < LawCost(best.tp) || (LawCost(c.tp) == LawCost(best.tp) && c.tp.Spell.Level < best.tp.Spell.Level)))
		if better {
			best, bestTotal, found = c, total, true
			out.LandTicks = land
		}
	}
	if !found {
		return out, DeclineUnpriced
	}
	out.Teleport = best.tp
	out.Save = hereTicks - bestTotal
	if out.Save < need {
		out.Teleport = TravelTeleport{}
		out.Detail = "saves " + Itoa(out.Save) + " ticks, needs " + Itoa(need)
		if capped && out.Save >= minSave {
			return out, DeclineHourlyCap
		}
		if price > 1 {
			return out, "price x" + Itoa(price)
		}
		return out, DeclineSaving
	}
	return out, ""
}

// SlidingCount counts events inside a trailing window: the casts an account made in the last hour. Not safe for
// concurrent use (an account's Travel is driven from one goroutine); the fleet-wide counter wraps it in a mutex.
type SlidingCount struct{ at []time.Time }

// Add records an event at now.
func (c *SlidingCount) Add(now time.Time) { c.at = append(c.at, now) }

// Count is the events within window of now; older ones are forgotten.
func (c *SlidingCount) Count(now time.Time, window time.Duration) int {
	keep := c.at[:0]
	for _, t := range c.at {
		if now.Sub(t) < window {
			keep = append(keep, t)
		}
	}
	c.at = keep
	return len(keep)
}

// MissedLedger totals the walking, in ticks, that casts the account could not pay for would have saved (the demand signal
// behind a dedicated kit ask): entries older than MissedWindow drop out.
type MissedLedger struct {
	at    []time.Time
	ticks []int
}

// Add records ticks of missed saving at now.
func (m *MissedLedger) Add(now time.Time, ticks int) {
	if ticks <= 0 {
		return
	}
	m.at = append(m.at, now)
	m.ticks = append(m.ticks, ticks)
}

// Total is the missed ticks inside MissedWindow of now.
func (m *MissedLedger) Total(now time.Time) int {
	keepAt, keepTicks, sum := m.at[:0], m.ticks[:0], 0
	for i, t := range m.at {
		if now.Sub(t) < MissedWindow {
			keepAt, keepTicks = append(keepAt, t), append(keepTicks, m.ticks[i])
			sum += m.ticks[i]
		}
	}
	m.at, m.ticks = keepAt, keepTicks
	return sum
}

// Clear forgets the ledger (a kit ask was made).
func (m *MissedLedger) Clear() { m.at, m.ticks = nil, nil }
