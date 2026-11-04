package bot

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/lootchestfisherplan"
)

// Live 2026-09-26 06:40-06:43, spare104 at the mule post. After one good trade three runes
// (Cosmic 564, Nature 561, Law 563) "never reached the trade screen after 4 offers" and went into
// b.knownUntradable. The hand-over now skipped all three -- but the DECISION to hand over did not
// know that set existed: LootInPack counted the three slots, PhaseHandoff fired, donorrunner
// summoned the mule and declined an empty trade, and giveStep tried again every 5 s. The five-try
// give-up banked the runes, bankHaulRows counted them as mule haul, PhaseDrainBank drew them
// straight back out, and the loop started over until the owner restarted the account.
//
// The decision to GO and the decision to OFFER must read the same rule (handoverKeepIDs' own doc
// comment); the learned set is part of that rule.
var liveUntradeableRunes = map[int]bool{564: true, 561: true, 563: true}

func TestHandoverKeepIncludesItemsLearnedUntradeable(t *testing.T) {
	b := &lootChestBot{knownUntradable: map[int]bool{}}
	for id := range liveUntradeableRunes {
		b.knownUntradable[id] = true
	}
	keep := b.handoverKeepIDs(nil)
	for id := range liveUntradeableRunes {
		if !keep[id] {
			t.Errorf("id %d is known untradeable but still counts as haul worth a trip to the mule", id)
		}
	}
	// CONTROL: nothing learned, nothing extra kept.
	if (&lootChestBot{}).handoverKeepIDs(nil)[564] {
		t.Error("a Cosmic rune is kept although nothing was learned about it")
	}
}

// The bank side of the same loop: stowHaul/drainBankStep count mule haul in the bank by NAME
// (muleKeep), so a learned-untradeable row banked by the give-up was drawn straight back out.
func TestMuleKeepNamesItemsLearnedUntradeable(t *testing.T) {
	names := map[int]string{564: "Cosmic rune", 561: "Nature rune", 563: "Law rune"}
	keep := muleKeepWith(liveUntradeableRunes, func(id int) string { return names[id] })
	has := map[string]bool{}
	for _, n := range keep {
		has[n] = true
	}
	for _, n := range names {
		if !has[n] {
			t.Errorf("%q is known untradeable but the bank would still offer it to the mule (keep %v)", n, keep)
		}
	}
	for _, n := range fisherplan.NeverHandOver {
		if !has[n] {
			t.Errorf("NeverHandOver entry %q dropped from the keep list", n)
		}
	}
	// An id the config cannot name adds nothing rather than a blank that matches nothing.
	for _, n := range muleKeepWith(map[int]bool{9999: true}, func(int) string { return "" }) {
		if n == "" {
			t.Error("a blank name joined the keep list")
		}
	}
}

// Repeated failures back off by MINUTES, not the 5 s that let this loop fire more than ten times
// in three minutes, and each failure makes the next wait longer.
func TestHandoffBackoffIsMinutesAndGrows(t *testing.T) {
	prev := time.Duration(0)
	for fails := 1; fails <= 10; fails++ {
		w := lootchestfisherplan.HandoffBackoff(fails)
		if w < time.Minute {
			t.Fatalf("HandoffBackoff(%d) = %s, want at least a minute", fails, w)
		}
		if w < prev {
			t.Fatalf("HandoffBackoff(%d) = %s, shorter than the %s before it", fails, w, prev)
		}
		if w > time.Hour {
			t.Fatalf("HandoffBackoff(%d) = %s, want it capped at an hour", fails, w)
		}
		prev = w
	}
	if lootchestfisherplan.HandoffBackoff(3) <= lootchestfisherplan.HandoffBackoff(1) {
		t.Error("the backoff does not grow with repeated failures")
	}
	if lootchestfisherplan.HandoffBackoff(0) != 0 {
		t.Error("no failures should mean no wait")
	}
}

// While backing off, the account does not go back to the mule: the banked haul is not reported
// to the dispatcher (no PhaseDrainBank), so it goes back to work instead of drawing the same load
// and summoning the mule again. A success clears the hold.
func TestAFailedHandoffHoldsTheMuleOffForMinutes(t *testing.T) {
	now := time.Date(2026, 9, 26, 6, 40, 37, 0, time.UTC)
	b := &lootChestBot{bankHaulRows: 3}
	if got := b.bankHaulRowsFor(now); got != 3 {
		t.Fatalf("before any failure bankHaulRowsFor = %d, want the remembered 3", got)
	}
	first := b.noteHandoffFailure(now)
	if first < time.Minute {
		t.Fatalf("first failure holds for %s, want minutes", first)
	}
	if !b.handoffHeld(now.Add(5 * time.Second)) {
		t.Fatal("5 s after a failed hand-off the account is free to try again — the live loop")
	}
	if got := b.bankHaulRowsFor(now.Add(5 * time.Second)); got != 0 {
		t.Fatalf("while held bankHaulRowsFor = %d, want 0 so PhaseDrainBank is not chosen", got)
	}
	s := lootchestfisherplan.Snapshot{AtMule: true, MinKeys: 6, BankHaulRows: b.bankHaulRowsFor(now.Add(5 * time.Second))}
	if p := lootchestfisherplan.DecidePhase(s); p == lootchestfisherplan.PhaseDrainBank || p == lootchestfisherplan.PhaseHandoff {
		t.Fatalf("while held DecidePhase = %s, want the account back at work", p)
	}
	second := b.noteHandoffFailure(now.Add(first))
	if second <= first {
		t.Fatalf("second failure holds %s, not longer than the first %s", second, first)
	}
	if b.handoffHeld(now.Add(first + second + time.Second)) {
		t.Fatal("the hold never expires")
	}
	b.noteHandoffSuccess()
	if b.handoffHeld(now) || b.handoffFails != 0 {
		t.Fatal("a completed hand-off must clear the hold and the failure count")
	}
}
