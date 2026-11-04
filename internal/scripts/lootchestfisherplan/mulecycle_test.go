package lootchestfisherplan

import (
	"testing"
	"time"
)

// The fleet owner's cycle, 2026-09-21, tested as the owner stated it:
//
//  1. arrive at the mule with chest loot
//  2. hand the loot over AND ask how many keys the mule is holding
//  3. mule over 100: take them and go straight back to the chest, do not fish
//  4. mule at 100 or fewer: fish, and come back only when it has more
//  5. never travel to the mule "too early"
//  6. once the keys are spent, hand over the whole bank
//
// Every case below names the owner's own observation it protects.

var testNow = time.Date(2026, 9, 21, 20, 0, 0, 0, time.UTC)

// atMule is a snapshot of an account standing at the mule with nothing in its pack, no keys
// anywhere and nothing known about the mule. Each test overrides only what it is about.
func atMule() Snapshot {
	return Snapshot{AtMule: true, MinKeys: MinKeysDefault, Now: testNow, PackFree: PackSlots}
}

func fresh(banked int) MuleStock {
	return MuleStock{Banked: banked, At: testNow.Add(-time.Minute), Heard: true}
}

// "also no handover of loot from the chest when grabbing more keys"
//
// The regression: an account arriving at the mule holding the haul AND enough keys took the
// go-to-Falador branch and left with the loot still in its pack. Giving must win.
func TestLootIsHandedOverEvenWhenTheAccountAlreadyHasKeys(t *testing.T) {
	s := atMule()
	s.LootInPack = 9
	s.KeysInBank = 40
	s.KeysInPack = 3
	if got := DecidePhase(s); got != PhaseHandoff {
		t.Fatalf("arrived at the mule with a haul and keys: got %q, want %q — the haul must cross before anything else", got, PhaseHandoff)
	}
}

// The same account one pass later, haul gone: now the keys decide, and it leaves.
func TestOnceTheHaulIsGoneTheKeysSendItBackToFalador(t *testing.T) {
	s := atMule()
	s.KeysInPack = 3
	if got := DecidePhase(s); got != PhaseBankForTravel {
		t.Fatalf("keys in hand at the mule: got %q, want %q", got, PhaseBankForTravel)
	}
	s = atMule()
	s.KeysInBank = 40
	if got := DecidePhase(s); got != PhaseTravelToFalador {
		t.Fatalf("keys banked and none in hand: got %q, want %q", got, PhaseTravelToFalador)
	}
}

// "if the mule has over 100keys those should be traded in return for defi3lds bank"
func TestAMuleOverTheThresholdIsMet(t *testing.T) {
	s := atMule()
	s.Mule = fresh(MuleKeyThreshold + 1)
	if got := DecidePhase(s); got != PhaseHandoff {
		t.Fatalf("mule holding %d keys: got %q, want %q", MuleKeyThreshold+1, got, PhaseHandoff)
	}
}

// "they dont have over 100 keys then spare104 should go back to fishing"
func TestAMuleAtOrBelowTheThresholdSendsItFishing(t *testing.T) {
	for _, banked := range []int{0, 1, 57, MuleKeyThreshold} {
		s := atMule()
		s.Mule = fresh(banked)
		if got := DecidePhase(s); got != PhaseFish {
			t.Errorf("mule holding %d keys: got %q, want %q", banked, got, PhaseFish)
		}
	}
}

// "its trying to mule way too early" — the in-between default used to be PhaseTravelHome, so an
// account with no keys, nothing to give and nothing to collect walked the whole ship chain home
// to ask a question it already knew the answer to.
func TestNothingToDoAtTheMuleMeansFishNotWalkHome(t *testing.T) {
	s := Snapshot{MinKeys: MinKeysDefault, Now: testNow, PackFree: PackSlots}
	s.Mule = fresh(57)
	if got := DecidePhase(s); got != PhaseFish {
		t.Fatalf("in between with no errand: got %q, want %q", got, PhaseFish)
	}
}

// ... but a real errand still brings it home, from anywhere.
func TestAnErrandStillBringsItHome(t *testing.T) {
	base := Snapshot{MinKeys: MinKeysDefault, Now: testNow, PackFree: PackSlots}

	waiting := base
	waiting.Mule = fresh(MuleKeyThreshold + 50)
	if got := DecidePhase(waiting); got != PhaseTravelHome {
		t.Errorf("keys waiting at the mule: got %q, want %q", got, PhaseTravelHome)
	}

	toDrain := base
	toDrain.BankHaulRows = 12
	if got := DecidePhase(toDrain); got != PhaseTravelHome {
		t.Errorf("a bank full of loot for the mule: got %q, want %q", got, PhaseTravelHome)
	}

	// A DUE PROBE NO LONGER BRINGS IT HOME, changed 2026-09-23. A probe is a question, and it
	// is asked and answered over the WALL BUS -- the mule's own decline carries the count
	// ("only 69 Crystal key banked, holding until 100"), and the bus reaches it from anywhere.
	// The fleet owner watched this walk happen on four consecutive builds with an empty pack,
	// no keys and a mule below its reserve: 200 tiles out to learn a number, 200 tiles back.
	//
	// The exit from "never heard a count" is preserved, just moved: internal/bot/
	// lootchestfisher.go asks over the bus every Loop while a probe is due, bounded by its own
	// nextProbe clock. See InterruptsFishing.
	blind := base
	blind.Probe = true
	if got := DecidePhase(blind); got != PhaseFish {
		t.Errorf("a due probe alone should be asked over the bus, not walked to: got %q, want %q", got, PhaseFish)
	}
}

// Rule 4 (never drag a working fisher home) still holds for everything EXCEPT a real errand.
// This is the pair of cases that used to be one: the fisher was previously immune to being
// interrupted for any reason at all below the key threshold, which is why an account kept
// fishing past a mule holding 417 keys.
func TestAWorkingFisherIsLeftAloneUnlessThereIsSomethingToGoFor(t *testing.T) {
	s := Snapshot{Fishing: true, MinKeys: MinKeysDefault, Now: testNow, PackFree: PackSlots}
	s.Mule = fresh(57)
	if got := DecidePhase(s); got != PhaseFish {
		t.Fatalf("a fisher with nothing to collect: got %q, want %q", got, PhaseFish)
	}
	s.Mule = fresh(MuleKeyThreshold + 300)
	if got := DecidePhase(s); got == PhaseFish {
		t.Fatalf("a fisher with 400 keys waiting at the mule kept fishing")
	}
}

// "make sure it hands over everything in the bank besides 500gp and untradeables once it has
// looted all keys" — the guard is "once it has looted all keys", so a run with keys left is
// never interrupted to do bookkeeping.
func TestTheBankIsDrainedOnlyWhenTheKeysAreSpent(t *testing.T) {
	s := atMule()
	s.BankHaulRows = 12
	s.Mule = fresh(57)
	if got := DecidePhase(s); got != PhaseDrainBank {
		t.Fatalf("keys spent and a bank to empty: got %q, want %q", got, PhaseDrainBank)
	}

	s.KeysInBank = 3 // below MinKeys, but still keys: the run is not finished
	if got := DecidePhase(s); got == PhaseDrainBank {
		t.Fatalf("the bank was drained while the account still held keys")
	}
}

// The whole cycle in order, each step fed the state the previous one would have produced.
func TestTheOwnersCycleEndToEnd(t *testing.T) {
	// 1+2. Home with the haul, mule holding plenty: hand over.
	s := atMule()
	s.LootInPack = 14
	s.Mule = fresh(400)
	if got := DecidePhase(s); got != PhaseHandoff {
		t.Fatalf("step 1: got %q, want %q", got, PhaseHandoff)
	}

	// 3. Haul gone, keys collected as notes: bank them and go, never fish.
	s = atMule()
	s.NotedKeysInPack = 300
	s.Mule = fresh(MuleKeyThreshold)
	if got := DecidePhase(s); got != PhaseBankForTravel {
		t.Fatalf("step 3: got %q, want %q", got, PhaseBankForTravel)
	}

	// ... and from the bank, straight out to Falador.
	s = atMule()
	s.KeysInBank = 300
	s.Mule = fresh(MuleKeyThreshold)
	if got := DecidePhase(s); got != PhaseTravelToFalador {
		t.Fatalf("step 3b: got %q, want %q", got, PhaseTravelToFalador)
	}

	// 4. Back home with the haul banked at Falador, mule down to its reserve: give it the
	// bank, then fish.
	s = atMule()
	s.BankHaulRows = 20
	s.Mule = fresh(MuleKeyThreshold)
	if got := DecidePhase(s); got != PhaseDrainBank {
		t.Fatalf("step 4: got %q, want %q", got, PhaseDrainBank)
	}
	s.BankHaulRows = 0
	if got := DecidePhase(s); got != PhaseFish {
		t.Fatalf("step 4b: got %q, want %q", got, PhaseFish)
	}
}

// A snapshot with no Now at all must fail SAFE: every reading reads as stale, so the account
// fishes rather than travelling on a number nobody dated.
func TestAnUndatedSnapshotDoesNotTravelOnAStaleNumber(t *testing.T) {
	s := Snapshot{AtMule: true, MinKeys: MinKeysDefault, PackFree: PackSlots}
	s.Mule = MuleStock{Banked: 9999, Heard: true}
	if got := DecidePhase(s); got != PhaseFish {
		t.Fatalf("undated snapshot: got %q, want %q", got, PhaseFish)
	}
}
