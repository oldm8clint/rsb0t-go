package fisherrunner

import (
	"testing"

	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
)

// Live 2026-09-19: a Mysterious Old Man fail-teleport dropped the mule `mule` at Brimhaven's
// northern dock (2772,3235) on Karamja. ReturnToCatherby's boat fallback only fired for
// freshstart.LegPier, but TravelLeg returns LegDestination for every Karamja tile — it was
// written for a fresh account's forward journey, where "on Karamja" means "the next leg heads
// for the destination". The fallback never fired, the mule retried four impossible walks
// forever, and the fleet completed zero trades for the whole run.

func TestAcrossWaterIncludesTheLiveMuleStrandingTile(t *testing.T) {
	brimhavenNorthDock := tile.Tile{X: 2772, Z: 3235}
	if freshstart.TravelLeg(brimhavenNorthDock) == freshstart.LegPier {
		t.Fatal("fixture assumption broken: TravelLeg now reports LegPier for Karamja, so the original bug no longer reproduces this way — re-check the fallback")
	}
	if !acrossWaterFromCatherby(brimhavenNorthDock) {
		t.Fatal("the tile the live mule was stranded on is not treated as across the water — ReturnToCatherby will never take the boat home")
	}
}

// Anywhere on Karamja, not just the one incident tile.
func TestAcrossWaterCoversKaramja(t *testing.T) {
	for _, k := range []tile.Tile{
		{X: 2772, Z: 3235}, // Brimhaven northern dock (the live incident)
		{X: 2760, Z: 3238}, // Brimhaven dock area
		{X: 2900, Z: 3150}, // central/south Karamja
	} {
		if !freshstart.OnKaramjaSide(k) {
			t.Fatalf("fixture tile %d,%d is not on Karamja per freshstart — pick a different fixture", k.X, k.Z)
		}
		if !acrossWaterFromCatherby(k) {
			t.Fatalf("Karamja tile %d,%d not treated as across the water", k.X, k.Z)
		}
	}
}

// The pre-existing LegPier case must keep working exactly as before.
func TestAcrossWaterKeepsThePierSideCase(t *testing.T) {
	portSarim := tile.Tile{X: 3029, Z: 3217}
	if freshstart.TravelLeg(portSarim) != freshstart.LegPier {
		t.Fatal("fixture assumption broken: Port Sarim is no longer LegPier")
	}
	if !acrossWaterFromCatherby(portSarim) {
		t.Fatal("the original LegPier boat fallback regressed")
	}
}

// Mainland west of the Ardougne line, off Karamja, can walk to Catherby: no boat.
func TestAcrossWaterExcludesTheArdougneMainland(t *testing.T) {
	ardougne := tile.Tile{X: 2662, Z: 3305}
	if freshstart.OnKaramjaSide(ardougne) {
		t.Fatal("fixture tile is on Karamja — pick a mainland tile")
	}
	if acrossWaterFromCatherby(ardougne) {
		t.Fatal("an Ardougne mainland tile was treated as needing a boat to reach Catherby")
	}
}
