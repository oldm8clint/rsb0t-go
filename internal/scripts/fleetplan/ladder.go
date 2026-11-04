package fleetplan

import (
	"rsb0t-go/internal/scripts/thieverplan"
	"rsb0t-go/internal/scripts/tile"
)

// PickpocketCoins is each rung's mean coins per success, from pickpocket.dbrow.
var PickpocketCoins = map[string]float64{
	"Man": 3, "Farmer": 9, "Warrior woman": 18, "Guard": 30, "Knight of Ardougne": 50,
	"Watchman": 60, "Paladin": 80, "Gnome": 300 * 30.0 / 128, "Hero": 250,
}

// LadderCosts are the overheads the rate counts. Defaults are conservative estimates, not
// measurements: EatTicks is the content's eat delay, BankTicksPerTile a running walk (two tiles
// a tick) there and back, FoodPerTrip the bites one bank trip carries.
type LadderCosts struct {
	FoodHeal         int
	FoodPerTrip      int
	EatTicks         int
	BankTicksPerTile float64
	BankFixedTicks   int
}

// DefaultLadderCosts assume mule sharks (20 a bite), 20 per trip.
func DefaultLadderCosts() LadderCosts {
	return LadderCosts{FoodHeal: 20, FoodPerTrip: 20, EatTicks: 3, BankTicksPerTile: 1.0, BankFixedTicks: 10}
}

// RungRate is one rung's steady-state rate at a level.
type RungRate struct {
	Method       thieverplan.Method
	Success      float64
	XPPerTick    float64
	CoinsPerTick float64
}

// RateRung scores one rung at a level with the given costs.
func RateRung(m thieverplan.Method, level int, c LadderCosts) RungRate {
	p := SuccessChance(m, level)
	attempt := float64(thieverplan.AttemptTicks) + (1-p)*float64(m.StunTicks)
	dmg := (1 - p) * float64(m.StunDamage)
	heal := c.FoodHeal
	if heal < 1 {
		heal = 1
	}
	bites := dmg / float64(heal)
	perTrip := c.FoodPerTrip
	if perTrip < 1 {
		perTrip = 1
	}
	dist := float64(tile.Chebyshev(tile.Tile{X: m.Anchor.X, Z: m.Anchor.Z}, tile.Tile{X: m.Bank.X, Z: m.Bank.Z}))
	bankTrip := 2*dist*c.BankTicksPerTile + float64(c.BankFixedTicks)
	ticks := attempt + bites*float64(c.EatTicks) + bites/float64(perTrip)*bankTrip
	if ticks <= 0 {
		return RungRate{Method: m, Success: p}
	}
	return RungRate{Method: m, Success: p, XPPerTick: p * m.XP / ticks, CoinsPerTick: p * PickpocketCoins[m.Label] / ticks}
}

// LadderHome is the side of the two ships this ladder is climbed from, and the only side any
// caller of it ever stands on.
//
// THE RUNG HAS TO BE SOMEWHERE THE ACCOUNT CAN WALK TO. This ladder used to rank every rung in
// thieverplan.METHODS on XP per tick alone, and two of them — Man (Anchor: the Lumbridge castle
// courtyard, Bank: Varrock East) and Farmer (3227,3290, Bank: Varrock East) — are on the MAINLAND,
// the far side of the Brimhaven and Port Sarim ships. Every Stage 2 account stands in East
// Ardougne when the rung is chosen, because DecideStage2Phase refuses every working phase until
// InArdougneReach is true, so "Man" meant a 600-tile, two-ship-fare journey each way.
//
// Live, the whole 110-account ScarabSwarm cohort, 2026-09-22 (run/logs/fleet-20260922-101543-232-16204.log):
//
//	route 2652,3284,0 -> 3222,3218,0: cost 623, 306 waypoints, expanded 231567
//	walk to 3222,3218,0: baked walk failed: walk timed out
//	walk to 3222,3218,0 did not arrive (failed: timed out after 1m30s), stopped at 2801,3194,0
//	scarabswarm: stage 2 — could not reach Man from 2801,3194,0; retrying
//
// 31,310 walker lines for the Man anchor and 17,081 for the Farmer anchor; 1,473 "could not reach
// Man" and 978 "could not reach Farmer"; 707 accounts rode Captain Barnaby's ship out to Brimhaven
// on the way and NOT ONE of them ever took the Brimhaven->Ardougne ship back. scarabrunner.MoveTimeout
// is 90s and that route cannot be walked in 90s even once, so the walk could only ever fail, dump
// the account somewhere mid-ocean-crossing, and start again.
//
// Rungs are therefore filtered by thieverplan.Region, which the table already carries for exactly
// this distinction ("which side of the two ships a method lives on"). A caller that genuinely
// stands on the other side asks for it by name with BestPickpocketIn.
const LadderHome = thieverplan.RegionArdougne

// LadderRungs are the pickpocket rungs the ladder may use: the ones that can be walked to from
// LadderHome. See that constant for why locality is part of the choice and not an afterthought.
func LadderRungs() []thieverplan.Method { return LadderRungsIn(LadderHome) }

// LadderRungsIn are the pickpocket rungs in one region. An empty region means every region, which
// is the pre-2026-09-22 behaviour and is only correct for a caller that can reach both sides.
func LadderRungsIn(region thieverplan.Region) []thieverplan.Method {
	var out []thieverplan.Method
	for _, m := range thieverplan.METHODS {
		if m.Kind != thieverplan.KindPickpocket || m.Gate != "" {
			continue
		}
		if region != "" && m.Region != region {
			continue
		}
		if WildernessLevel(tile.Tile{X: m.Anchor.X, Z: m.Anchor.Z}) > 0 {
			continue
		}
		out = append(out, m)
	}
	return out
}
