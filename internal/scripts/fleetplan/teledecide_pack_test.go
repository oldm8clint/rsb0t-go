package fleetplan

import (
	"testing"
	"time"

	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// packWalker prices walks as a ScarabSwarm account's Walker does: ships and NPC hops allowed with the fares in the pack, the wolf
// dens as no-go rectangles, the automatic danger zones resolved from the walk's own start.
type packWalker struct {
	t  *testing.T
	pl *webwalk.Planner
}

func (w *packWalker) route(from, to tile.Tile) webwalk.Route { return w.routeWith(from, to, true) }

// routeWith plans the walk; gated false leaves the account's world state out, which lets every gated edge through (the spirit
// trees and the quest shortcuts: the best an account that holds every quest could do).
func (w *packWalker) routeWith(from, to tile.Tile, gated bool) webwalk.Route {
	f, d := webwalk.Point{X: from.X, Z: from.Z, Level: from.Level}, webwalk.Point{X: to.X, Z: to.Z, Level: to.Level}
	var rects []webwalk.Rect
	for _, s := range randomevents.WolfTerritorySquares(2) {
		rects = append(rects, webwalk.Rect{MinX: s.MinX, MaxX: s.MaxX, MinZ: s.MinZ, MaxZ: s.MaxZ, Level: 0})
	}
	zones := webwalk.ResolveDangerZones(nil, rects, webwalk.ZoneContext{IncludeAutomatic: true, CombatLevel: 60, CombatKnown: true, Start: &f, Dest: &d})
	state := &webwalk.WorldState{Members: true, ItemCount: func(name string) int {
		if name == "Coins" {
			return 5000
		}
		return 0
	}}
	if !gated {
		state = nil
	}
	return w.pl.Plan(f, d, webwalk.Options{AvoidZones: zones, State: state, AllowNPC: true, AllowSpecial: true, MaxExpansions: 400000})
}

// facts reads a planned route as the decision prices it: hop costs split out of the tiles, ships counted.
func facts(r webwalk.Route) RouteFacts {
	if !r.OK {
		return RouteFacts{}
	}
	hops, ships := 0, 0
	for _, h := range r.Hops() {
		hops += h.Cost
		if h.Kind == webwalk.KindShip {
			ships++
		}
	}
	return RouteFacts{OK: true, TileUnits: max(r.Cost-hops, 0), HopUnits: hops, Ships: ships}
}

// decide runs DecideTeleport for a walk from here to dest, pricing here and every landing on the real pack.
func (w *packWalker) decide(here, dest tile.Tile, mod func(*TeleDecisionInput)) (TeleDecision, string, int) {
	in := TeleDecisionInput{
		Here: here, Dest: dest, HereRoute: facts(w.route(here, dest)),
		Magic: 99, Members: true, Staff: StaffOfAir,
		Pack:   RuneStock{"Law rune": 30, "Air rune": 150, "Fire rune": 30, "Water rune": 30, "Earth rune": 30},
		Energy: 100, EnergyKnown: true, KG: 10, Now: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC),
		MuleLaws: 33673, MuleKnown: true,
	}
	prices := 0
	in.LandCost = func(land tile.Tile) RouteFacts {
		prices++
		return facts(w.route(land, dest))
	}
	if mod != nil {
		mod(&in)
	}
	dec, why := DecideTeleport(TeleTravelSettings{Enabled: true, AnyWalk: true, AirRunes: true}, in)
	return dec, why, prices
}
