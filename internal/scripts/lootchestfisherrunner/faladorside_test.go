package lootchestfisherrunner

import (
	"testing"

	"rsb0t-go/internal/scripts/lootchestfisherplan"
	"rsb0t-go/internal/scripts/tile"
)

// TestOnFaladorSideCoversTheWholeFarEnd pins the box against the script's OWN named tiles, so it
// cannot drift away from the geography it is meant to describe.
//
// The bug it exists for (live 2026-09-20): "at Falador" was a 12-tile radius around the bank
// stand, but the chest is 82 tiles from it. Standing at the chest with five keys left, the
// script decided it was at neither end, tried to bank at the MULE'S bank in Catherby ~105 tiles
// away, and logged "could not reach the bank at 2809,3441" on a loop.
func TestOnFaladorSideCoversTheWholeFarEnd(t *testing.T) {
	in := []struct {
		name string
		at   tile.Tile
	}{
		{"the Falador west bank stand", lootchestfisherplan.FaladorBankStand},
		{"the chest itself", lootchestfisherplan.ChestTile},
		{"the only tile the chest can be used from", lootchestfisherplan.ChestStand},
		{"the chest room door, outside", lootchestfisherplan.HouseDoorOutside},
		{"the chest room door, inside", lootchestfisherplan.HouseDoorInside},
		{"the Taverley gate, east", lootchestfisherplan.TaverleyGateEast},
		{"the Taverley gate, west", lootchestfisherplan.TaverleyGateWest},
	}
	for _, c := range in {
		if !OnFaladorSide(c.at) {
			t.Errorf("%s (%d,%d) reads as NOT the Falador side", c.name, c.at.X, c.at.Z)
		}
	}
}

// TestOnFaladorSideExcludesTheMuleEnd is the separation that actually matters: the two ends of
// the expedition must never both look true, or the script would bank at the wrong one.
func TestOnFaladorSideExcludesTheMuleEnd(t *testing.T) {
	out := []struct {
		name string
		at   tile.Tile
	}{
		{"the Catherby mule stand", tile.Tile{X: 2809, Z: 3441, Level: 0}},
		{"the Catherby bank booths", tile.Tile{X: 2809, Z: 3442, Level: 0}},
		{"the Fishing Guild bank", tile.Tile{X: 2586, Z: 3419, Level: 0}},
		{"an upper floor above the bank", tile.Tile{X: 2946, Z: 3369, Level: 1}},
	}
	for _, c := range out {
		if OnFaladorSide(c.at) {
			t.Errorf("%s (%d,%d,%d) wrongly reads as the Falador side", c.name, c.at.X, c.at.Z, c.at.Level)
		}
	}
}

// TestOnFaladorSideCoversTheApproachFromTheShip pins the tiles the account was actually
// observed on. It walks in from the east off the ship chain, and at a 2960 edge 2963,3408 and
// 2964,3377 matched neither end -- the same "at neither end" shape the box was added to remove.
func TestOnFaladorSideCoversTheApproachFromTheShip(t *testing.T) {
	for _, at := range []tile.Tile{
		{X: 2963, Z: 3408, Level: 0},
		{X: 2964, Z: 3377, Level: 0},
		{X: 2949, Z: 3424, Level: 0},
	} {
		if !OnFaladorSide(at) {
			t.Errorf("observed transit tile %d,%d reads as NOT the Falador side", at.X, at.Z)
		}
	}
}

// TestOnFaladorSideStillExcludesFaladorEast keeps the widened edge honest: the east bank at
// 3010,3354 is 65 tiles further from the chest and belongs to no part of this run.
func TestOnFaladorSideStillExcludesFaladorEast(t *testing.T) {
	if OnFaladorSide(tile.Tile{X: 3010, Z: 3354, Level: 0}) {
		t.Fatal("the Falador EAST bank wrongly reads as this run's far end")
	}
}
