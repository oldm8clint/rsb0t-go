package fisherrunner

import (
	"testing"
	"time"
)

// A two-minute recent-combat exclusion must not turn into 120 empty bank
// transactions. The same branch is used for whirlpools and failed banking.
func TestBlockedFishingDoesNotBankEmptyPack(t *testing.T) {
	bankCalls, waits := 0, 0
	d := &Deps{Delay: func(duration time.Duration) bool {
		if duration != time.Second {
			t.Fatalf("retry delay = %v", duration)
		}
		waits++
		return true
	}}
	bank := func() (int, bool) { bankCalls++; return 0, true }
	for i := 0; i < 120; i++ {
		if blockedFishingStep(d, 0, bank) {
			t.Fatal("ended rather than rechecking live safety")
		}
	}
	if bankCalls != 0 || waits != 120 {
		t.Fatalf("bank calls=%d waits=%d", bankCalls, waits)
	}
}

func TestBlockedFishingBanksCatchAndEndsSession(t *testing.T) {
	bankCalls := 0
	d := &Deps{Delay: func(time.Duration) bool { t.Fatal("waited after successful catch deposit"); return false }}
	if !blockedFishingStep(d, 14, func() (int, bool) { bankCalls++; return 14, true }) || bankCalls != 1 {
		t.Fatalf("bank calls=%d", bankCalls)
	}
}

func TestBlockedFishingCancellationAndFailedBank(t *testing.T) {
	for _, held := range []int{0, 3, 13, 14} {
		waits, bankCalls := 0, 0
		d := &Deps{Delay: func(time.Duration) bool { waits++; return false }}
		if !blockedFishingStep(d, held, func() (int, bool) { bankCalls++; return 0, false }) {
			t.Fatal("ignored cancelled wait")
		}
		wantCalls := 0
		if held >= 14 {
			wantCalls = 1
		}
		if waits != 1 || bankCalls != wantCalls {
			t.Fatalf("held=%d bank=%d waits=%d", held, bankCalls, waits)
		}
	}
}

func TestBlockedFishingWaitsForSmallCatchWithoutBankTrip(t *testing.T) {
	for held := 1; held < 14; held++ {
		waits := 0
		d := &Deps{Delay: func(time.Duration) bool { waits++; return true }}
		if blockedFishingStep(d, held, func() (int, bool) { t.Fatal("banked fewer than 14 fish"); return 0, false }) || waits != 1 {
			t.Fatalf("held=%d waits=%d", held, waits)
		}
	}
}
