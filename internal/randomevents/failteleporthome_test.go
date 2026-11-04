package randomevents

import (
	"testing"

	"rsb0t-go/internal/pathfind"
)

// A combat account's own home, standing in for whatever a script sets on Guardian.SafeRef.
var trainerHome = pathfind.NavPoint{X: 3268, Z: 3167, Level: 0}

// TestFailTeleportHomeReturnsTheAccountToWhereItWas is the fix for the recovery that sent every
// account to the Catherby bank: the tile the account was standing on before the jump is the
// per-account right answer, and no script has to declare anything for it to be.
func TestFailTeleportHomeReturnsTheAccountToWhereItWas(t *testing.T) {
	// The Al Kharid warrior stand, an account that has never been near Catherby.
	home, why := failTeleportHome(nil, 3288, 3172, 0, trainerHome)
	if home.X != 3288 || home.Z != 3172 || home.Level != 0 {
		t.Fatalf("failTeleportHome = %d,%d,%d (%s), want the pre-jump tile 3288,3172,0", home.X, home.Z, home.Level, why)
	}
	if home == CatherbyBankRef {
		t.Fatal("the recovery still aims at the Catherby bank")
	}
}

// TestFailTeleportHomeFallsBackToTheScriptsOwnHome covers the case where there is no usable
// reading from before the jump. The fallback must be the account's own SafeRef, which is what
// safeRefFor hands in — Catherby only when a script never set one.
func TestFailTeleportHomeFallsBackToTheScriptsOwnHome(t *testing.T) {
	home, why := failTeleportHome(nil, 0, 0, 0, trainerHome)
	if home != trainerHome {
		t.Fatalf("failTeleportHome with no pre-jump tile = %d,%d,%d (%s), want the script's own home %d,%d",
			home.X, home.Z, home.Level, why, trainerHome.X, trainerHome.Z)
	}
}

// TestFailTeleportHomeRejectsAnUnstandableTile guards the one way the pre-jump reading can be
// worse than the fallback: a tile the collision pack says nothing can stand on leaves a walk
// with nowhere to aim.
func TestFailTeleportHomeRejectsAnUnstandableTile(t *testing.T) {
	pack := packWithNothingWalkable()
	home, why := failTeleportHome(pack, 3288, 3172, 0, trainerHome)
	if home != trainerHome {
		t.Fatalf("failTeleportHome over an unwalkable tile = %d,%d,%d (%s), want the script's own home", home.X, home.Z, home.Level, why)
	}
}

// packWithNothingWalkable is an empty loaded pack: every Walkable lookup answers false because
// no mapsquare is present, which is exactly the reading the guard above is about.
func packWithNothingWalkable() *pathfind.Pack {
	return &pathfind.Pack{Version: 2}
}
