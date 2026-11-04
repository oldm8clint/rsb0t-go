package fleetplan

import (
	"testing"
	"time"
)

// The owner's ruling: 5,000,000 coins of Seers chaos fleet-wide, enforced fleet-wide.
func TestTheMarketLedgerEnforcesOneFleetWideBudget(t *testing.T) {
	now := time.Now()
	l := NewMarketLedger(MarketLedgerState{})
	const budget = 30_000
	// Two accounts at two stalls, each wanting a 150-chaos lot at a 150-gp ceiling (22,500).
	if got := l.Reserve("acct1", 22_500, budget, now, 0); got != 22_500 {
		t.Fatalf("first reservation granted %d, want 22500", got)
	}
	if left := l.Remaining(budget, "acct2", now); left != 7_500 {
		t.Fatalf("acct2 sees %d left while acct1 holds 22500 of 30000, want 7500", left)
	}
	if got := l.Reserve("acct2", 22_500, budget, now, 0); got != 7_500 {
		t.Fatalf("second reservation granted %d, want only the 7500 left", got)
	}
	// acct1 paid less than its ceiling: only what was paid is spent, and its hold is gone.
	st, p := l.Commit(MarketPurchase{Account: "acct1", Market: "Seers Market", Item: "Chaos rune", Qty: 150, Paid: 15_000, At: now})
	if st.Spent != 15_000 || st.Buys != 1 || p.SpentAfter != 15_000 || len(st.Recent) != 1 {
		t.Fatalf("after acct1's commit: %+v / %+v", st, p)
	}
	if left := l.Remaining(budget, "acct3", now); left != 30_000-15_000-7_500 {
		t.Fatalf("acct3 sees %d, want 7500 (budget less spent less acct2's hold)", left)
	}
	// acct2's purchase fell through: the release frees its hold without spending.
	l.Release("acct2")
	if left := l.Remaining(budget, "acct3", now); left != 15_000 {
		t.Fatalf("after acct2's release acct3 sees %d, want 15000", left)
	}
	// Spend the rest; then nothing is left for anyone and a reservation is refused.
	l.Reserve("acct3", 15_000, budget, now, 0)
	l.Commit(MarketPurchase{Account: "acct3", Paid: 15_000, At: now})
	if left := l.Remaining(budget, "acct4", now); left != 0 {
		t.Fatalf("spent out, acct4 still sees %d", left)
	}
	if got := l.Reserve("acct4", 100, budget, now, 0); got != 0 {
		t.Fatalf("a spent-out ledger granted %d", got)
	}
	// A restart resumes from the persisted state, not from zero.
	again := NewMarketLedger(l.State())
	if left := again.Remaining(budget, "acct4", now); left != 0 {
		t.Fatalf("a restarted ledger forgot the spend: %d left", left)
	}
	// Raising the budget (an owner decision) is the only way more is spent.
	if left := again.Remaining(budget+5_000, "acct4", now); left != 5_000 {
		t.Fatalf("raised budget: %d left, want 5000", left)
	}
	// A budget of 0 is "no market purchases".
	if again.Remaining(0, "acct4", now) != 0 || again.Reserve("acct4", 1, 0, now, 0) != 0 {
		t.Fatal("a zero budget allowed a purchase")
	}
}

func TestMarketHoldsLapseAndAreReplacedNotAdded(t *testing.T) {
	now := time.Now()
	l := NewMarketLedger(MarketLedgerState{})
	l.Reserve("a", 10_000, 50_000, now, time.Minute)
	l.Reserve("a", 12_000, 50_000, now, time.Minute) // a re-plan replaces the hold
	if left := l.Remaining(50_000, "b", now); left != 38_000 {
		t.Fatalf("after a re-plan b sees %d, want 38000", left)
	}
	if left := l.Remaining(50_000, "b", now.Add(2*time.Minute)); left != 50_000 {
		t.Fatalf("a lapsed hold still counts: %d", left)
	}
	// An account's own hold never counts against itself.
	l.Reserve("a", 12_000, 50_000, now, time.Minute)
	if left := l.Remaining(50_000, "A ", now); left != 50_000 {
		t.Fatalf("a's own hold counted against it (names compare case-blind): %d", left)
	}
	// A purchase with nothing paid only releases.
	st, _ := l.Commit(MarketPurchase{Account: "a", Paid: 0, At: now})
	if st.Spent != 0 || st.Buys != 0 || len(st.Recent) != 0 {
		t.Fatalf("an empty commit was recorded: %+v", st)
	}
	if left := l.Remaining(50_000, "b", now); left != 50_000 {
		t.Fatalf("an empty commit left the hold: %d", left)
	}
}

func TestTheLedgerKeepsOnlyRecentPurchases(t *testing.T) {
	l := NewMarketLedger(MarketLedgerState{})
	now := time.Now()
	for i := 0; i < MarketLedgerKeep+30; i++ {
		l.Commit(MarketPurchase{Account: "a", Paid: 10, At: now})
	}
	st := l.State()
	if len(st.Recent) != MarketLedgerKeep || st.Buys != MarketLedgerKeep+30 || st.Spent != 10*(MarketLedgerKeep+30) {
		t.Fatalf("recent %d buys %d spent %d", len(st.Recent), st.Buys, st.Spent)
	}
	if st.Recent[len(st.Recent)-1].SpentAfter != st.Spent {
		t.Fatalf("the last purchase's running total %d is not the ledger's %d", st.Recent[len(st.Recent)-1].SpentAfter, st.Spent)
	}
}
