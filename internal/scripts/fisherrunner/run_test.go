package fisherrunner

import (
	"testing"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents"
)

// Covers wantRunWhileWalking, the pure decision run.go's maybeEnableRun is built on top
// of, mirroring randomevents/flee_walking_test.go's own coverage of wantRunForFlee.
// maybeEnableRun itself is not unit-tested here: like WalkNear itself, it needs a real
// actions.Session — proven live instead, see cmd/fisherruntest.

func TestWantRunWhileWalkingAlreadyOnDoesNothing(t *testing.T) {
	if wantRunWhileWalking(true, 99, true) {
		t.Fatal("wantRunWhileWalking(runOn=true, ...) must report false — nothing to do (and must not spam SetRun) if run is already on")
	}
}

func TestWantRunWhileWalkingEnoughEnergyTurnsRunOn(t *testing.T) {
	if !wantRunWhileWalking(false, RunEnergyThreshold, true) {
		t.Fatalf("wantRunWhileWalking(runOn=false, energy=%d) must report true — exactly at the threshold counts", RunEnergyThreshold)
	}
	if !wantRunWhileWalking(false, 100, true) {
		t.Fatal("wantRunWhileWalking(runOn=false, energy=100) must report true")
	}
}

func TestWantRunWhileWalkingBelowThresholdStaysWalking(t *testing.T) {
	if wantRunWhileWalking(false, RunEnergyThreshold-1, true) {
		t.Fatalf("wantRunWhileWalking(runOn=false, energy=%d) must report false — below the cruising floor", RunEnergyThreshold-1)
	}
	if wantRunWhileWalking(false, 0, true) {
		t.Fatal("wantRunWhileWalking(energy=0) must report false")
	}
}

func TestWantRunWhileWalkingUnknownEnergyStaysWalking(t *testing.T) {
	if wantRunWhileWalking(false, 100, false) {
		t.Fatal("wantRunWhileWalking(energyOK=false) must report false — energy unknown (no UPDATE_RUNENERGY observed yet), don't assume it's safe")
	}
}

// Live-found 2026-09-18: WaitForGuardian measured drift against randomevents.CatherbyBankRef
// unconditionally, so an account deliberately sent ~430 tiles away to the Lumbridge
// pickpocket spot to earn boat fare was continuously reported as "drifted far from home" and
// hauled back toward Catherby, fighting its own fund phase. crafter1 logged that on
// repeat while standing right next to the spot. The reference has to follow where the
// account is actually supposed to be.
func TestDriftHomeDefaultsToCatherbyAndFollowsTheOverride(t *testing.T) {
	if got := driftHome(&Deps{}); got != randomevents.CatherbyBankRef {
		t.Fatalf("an unset DriftHome = %v, want the CatherbyBankRef default %v", got, randomevents.CatherbyBankRef)
	}
	lumbridge := pathfind.NavPoint{X: 3222, Z: 3218, Level: 0}
	if got := driftHome(&Deps{DriftHome: &lumbridge}); got != lumbridge {
		t.Fatalf("DriftHome override = %v, want %v", got, lumbridge)
	}
	// The whole point: the two must be far enough apart that the drift threshold
	// genuinely distinguishes them, otherwise the override would be cosmetic.
	dx := lumbridge.X - randomevents.CatherbyBankRef.X
	if dx < 0 {
		dx = -dx
	}
	if dx <= emergencyFleeDriftTiles {
		t.Fatalf("Lumbridge is only %d tiles from Catherby against a %d-tile threshold — "+
			"this override would not change any decision", dx, emergencyFleeDriftTiles)
	}
}

// TestFishersRunFromTheFirstPointOfEnergy pins the owner's 2026-09-25 ruling: a fisher runs on
// any energy at all (it was 20%). Energy 19 used to walk; it must run now. Energy 0 still walks,
// because the server has switched running off and there is nothing to spend.
func TestFishersRunFromTheFirstPointOfEnergy(t *testing.T) {
	if RunEnergyThreshold != 1 {
		t.Fatalf("RunEnergyThreshold = %d, want 1 (owner's ruling 2026-09-25)", RunEnergyThreshold)
	}
	for _, e := range []int{1, 19} {
		if !wantRunWhileWalking(false, e, true) {
			t.Fatalf("energy %d%%: a fisher must run", e)
		}
	}
	if wantRunWhileWalking(false, 0, true) {
		t.Fatal("energy 0%: nothing to run on")
	}
}
