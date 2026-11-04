package runecraftrunner

import (
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/locreach"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/tile"
)

// TemplePlan is the shortest walk inside a temple: from the arrival tile to a tile the engine accepts the
// altar's Craft-rune from, and from there to a tile it accepts the portal's Use from. The altar and the
// portal are each reached from their sides (internal/locreach), and which side matters: the account is held
// four ticks by the craft (p_delay(3)) and cannot walk meanwhile (a walk read in a delay is discarded), so
// the walk to the portal is the one that costs ticks after the hold. Air, Body and Nature are each three or
// four steps from the arrival to the altar and as many from the altar to the portal at best.
type TemplePlan struct {
	AltarStand, PortalStand tile.Tile
	// ToAltar and ToPortal are the steps of each walk, Steps their sum.
	ToAltar, ToPortal, Steps int
}

// standTiles are the walkable tiles the engine accepts a use of loc m from (its footprint, solid, left out).
func standTiles(pk *pathfind.Pack, cfg *cachecfg.Config, m locindex.Match) []tile.Tile {
	typ, ok := cfg.Loc(m.ID)
	if !ok {
		return nil
	}
	l := locreach.Loc{X: m.X, Z: m.Z, Level: m.Level, Width: typ.Width, Length: typ.Length, Angle: m.Angle, Shape: m.Shape, ForceApproach: typ.ForceApproach}
	fx, fz, fw, fh := l.Footprint()
	var out []tile.Tile
	for _, t := range locreach.Tiles(l) {
		if t.X >= fx && t.X < fx+fw && t.Z >= fz && t.Z < fz+fh {
			continue
		}
		if pk.Walkable(t.X, t.Z, t.Level) {
			out = append(out, t)
		}
	}
	return out
}

func stepsBetween(pk *pathfind.Pack, a, b tile.Tile) (int, bool) {
	if a == b {
		return 0, true
	}
	r := pk.FindPath(pathfind.NavPoint{X: a.X, Z: a.Z, Level: a.Level}, pathfind.NavPoint{X: b.X, Z: b.Z, Level: b.Level})
	if !r.OK {
		return 0, false
	}
	return r.Steps(), true
}

// PlanTemple picks the altar stand and the portal stand that make the whole inside walk shortest. ok is
// false when no pair is reachable (the caller falls back to walking to the nearest accepted tile).
func PlanTemple(pk *pathfind.Pack, cfg *cachecfg.Config, a runecraftplan.Altar, altar, portal locindex.Match) (TemplePlan, bool) {
	if pk == nil || cfg == nil {
		return TemplePlan{}, false
	}
	best, found := TemplePlan{}, false
	for _, as := range standTiles(pk, cfg, altar) {
		in, ok := stepsBetween(pk, a.Enter, as)
		if !ok {
			continue
		}
		for _, ps := range standTiles(pk, cfg, portal) {
			out, ok := stepsBetween(pk, as, ps)
			if !ok {
				continue
			}
			if !found || in+out < best.Steps || (in+out == best.Steps && out < best.ToPortal) {
				best, found = TemplePlan{AltarStand: as, PortalStand: ps, ToAltar: in, ToPortal: out, Steps: in + out}, true
			}
		}
	}
	return best, found
}
