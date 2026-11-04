package woodcutplan

import "testing"

// Live 2026-09-22: acct134 sat at the Seers bank for 25 minutes at Woodcutting 1 holding a
// Knife, 150,000 coins and NO axe, asking a player market that never answered for a rune axe
// and then waiting out axeRetry, over and over. Two existing fallbacks both declined to help:
// the tier ladder only moves down on a STOCK refusal and "nobody answered" is not one, and
// Bob does not stock rune so the Bob fallback did not apply either.

func TestAxelessAccountIsOfferedBobsStockBestFirst(t *testing.T) {
	got := AxelessRescue(nil)
	want := []string{"Steel axe", "Iron axe", "Bronze axe"}
	if len(got) != len(want) {
		t.Fatalf("AxelessRescue(nil) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("AxelessRescue(nil) = %v, want %v (best first: any axe chops from "+
				"Woodcutting 1, so the best one Bob has is the one to buy)", got, want)
		}
	}
}

// The companion, and the one that stops this becoming a different bug. An account that already
// holds an axe is UPGRADING: it chops the whole time it waits, so buying a worse axe from Bob
// would be a downgrade paid for in coins and a cross-region walk.
func TestAnAccountHoldingAnAxeIsNotRescued(t *testing.T) {
	for _, held := range [][]string{
		{"Bronze axe"},
		{"Mithril axe"},
		{"Rune axe"},
		{"Bronze axe", "Steel axe"},
	} {
		if got := AxelessRescue(held); got != nil {
			t.Errorf("AxelessRescue(%v) = %v, want nil -- this account can already chop", held, got)
		}
	}
}

// HeldAxes builds its slice from live inventory rows, so a blank entry is the shape an empty
// slot arrives in. It must not read as an axe.
func TestABlankHeldEntryIsNotAnAxe(t *testing.T) {
	if got := AxelessRescue([]string{"", "   "}); len(got) != 3 {
		t.Fatalf("AxelessRescue([blank]) = %v, want the full ladder -- a blank slot is not an axe", got)
	}
}

// Everything it offers must be something Bob actually stocks, or the rescue walks the account
// across two regions to be told no.
func TestEveryRescueRungIsSomethingBobSells(t *testing.T) {
	for _, a := range AxelessRescue(nil) {
		if !BobSells(a) {
			t.Errorf("AxelessRescue offers %q, which Bob does not stock", a)
		}
	}
}
