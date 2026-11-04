package fleetplan

import (
	"rsb0t-go/internal/scripts/thieverplan"
	"rsb0t-go/internal/scripts/tile"
)

// Labels of the two rungs.
const (
	ArdougneManLabel    = "Man (East Ardougne)"
	ArdougneFarmerLabel = "Farmer (Ardougne)"
)

// ArdougneMan is the Thieving 1 rung: the East Ardougne Men and Women, anchored between the two
// Men's spawns west of the market.
func ArdougneMan() thieverplan.Method {
	m, _ := thieverplan.MethodByLabel("Man")
	m.Label = ArdougneManLabel
	m.Anchor, m.Leash = tile.Tile{X: 2617, Z: 3306}, 16
	m.Region, m.Bank = thieverplan.RegionArdougne, thieverplan.ArdougneBank
	m.RespawnsOnTop, m.Verified = false, false
	m.Note = "two Men and two Women spawn in East Ardougne west of the market (content m40_51)"
	return m
}

// lowRungMargin is how far beyond a low rung's leash still counts as that rung's ground: the
// pickpocketer stands beside its target, and a target at the leash's edge puts it a tile or two out.
const lowRungMargin = 3

// ardougneFarm is the farm north of East Ardougne as HOME GROUND, which is wider than the Farmer
// rung's own leash on purpose: the farmhouse, the wheat field and the sheep pen all count, exactly
// as they did before the rung's anchor moved into the yard (15 tiles around 2648,3360, the old
// anchor plus its leash and lowRungMargin). An account the rung never sends there can still end up
// there, and calling it "elsewhere" would walk it ~80 tiles home instead of out of the gate.
var ardougneFarm = struct {
	centre tile.Tile
	reach  int
}{tile.Tile{X: 2648, Z: 3360}, 15}

// OnArdougneLowRung reports whether t is on the ground of the East Ardougne Man or the Farmer rung.
// Both lie outside ArdougneReach's 40 tiles of the market (the Man anchor is 44 out; the two
// northern Farmers stand beyond every pen), so without this Stage 3 called a Farmer pocket
// "elsewhere" and walked ~80 tiles home after it, and Stage 2 read a Man pocket as "not arrived"
// and sailed back to the bank (integration review of the capacity branch). Level 0 only.
func OnArdougneLowRung(t tile.Tile) bool {
	if t.Level != 0 {
		return false
	}
	at := tile.Tile{X: t.X, Z: t.Z}
	man := ArdougneMan()
	if tile.Chebyshev(at, tile.Tile{X: man.Anchor.X, Z: man.Anchor.Z}) <= man.Leash+lowRungMargin {
		return true
	}
	return tile.Chebyshev(at, ardougneFarm.centre) <= ardougneFarm.reach
}
