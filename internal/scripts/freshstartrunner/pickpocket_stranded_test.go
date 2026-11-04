package freshstartrunner

import (
	"testing"
	"time"
)

// What these bound, and the live readings that motivated them.
//
// Two separate faults, both measured on the live 110-account ScarabSwarm cohort on
// 2026-09-22 (run/logs/fleet-20260922-101543-232-16204.log), both in PickpocketStep:
//
//  1. An account short of a ship fare with no Man inside freshstart.PickpocketLeash had
//     exactly one escape — a walk to o.Spot — and that walk is itself priced in coins, so
//     for the account that needs it most it can never work:
//
//     18:05:27 [acct96]  no Man nearby and short of the fare (0 coins, need 60) — waiting
//     18:06:05 [acct96]  walk to 3222,3218,0: unreachable (unreachable from here (best 455 tiles))
//     18:06:05 [acct96]  could not reach the pickpocket spot at 3222,3218 — working where we stand
//
//     PickpocketOpts.Rescue is the way out: the fleet's mule was 43 tiles away holding 2.3M
//     coins. These cases pin WHEN it is asked and for HOW MUCH, which is the whole of this
//     package's share of that fix — the asking itself belongs to the caller that knows a
//     mule exists (internal/bot/scarabswarm.go).
//
//  2. The resolve wait charged the server's own approach walk to a flat 2500ms timeout, so
//     an ordinary click at 4+ tiles could not resolve inside its own budget. acct60 logged
//     562 "timed out waiting for the pickpocket attempt to resolve" and NOTHING else, having
//     gained 136 Thieving xp — roughly 17 landed pockets against 562 "timeouts".

func TestRescueIsAskedOnlyForCoinsTheAccountIsActuallyShort(t *testing.T) {
	cases := []struct {
		name string
		need int
		held int
		want int
	}{
		// acct96 and Acct222, live at 18:05:27 — the whole fare still to find.
		{"stranded with nothing", 60, 0, 60},
		// Acct218, live at 18:05:28: "short of the fare (17 coins, need 60)". Asking for
		// the whole 60 again would take coins off a mule that a ledger window caps, for no
		// gain — the shortfall is what unblocks the account.
		{"stranded part way", 60, 17, 43},
		// Stage 1 of ScarabSwarm runs this step with Need 0: it is grinding a Thieving
		// level, not a fare. A mule cannot hand over Thieving xp, so a trip to the post
		// would be pure loss. Live at 18:06:35: "no Man nearby (0 coins, fare needs 0)".
		{"grinding a level, not a fare", 0, 0, 0},
		// Already funded. PickpocketStep can still reach the stranded branch here (it is
		// short of a TARGET, not of coins), and must not ask for more.
		{"already funded", 60, 60, 0},
		{"more than funded", 60, 102, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rescueShortfall(tc.need, tc.held); got != tc.want {
				t.Errorf("rescueShortfall(need=%d, held=%d) = %d, want %d — Rescue must be asked for the shortfall only, and never at all when there is none",
					tc.need, tc.held, got, tc.want)
			}
		})
	}
}

func TestResolveBudgetCoversTheServerSideApproachWalk(t *testing.T) {
	// freshstart.ChooseTarget accepts anything inside its leash and PickpocketStep only
	// walks first beyond 8 tiles, so an ordinary click is aimed at an NPC up to EIGHT tiles
	// away. The engine routes the account there and runs the pickpocket on arrival, so the
	// budget has to pay for that walk or the wait expires before the attempt begins.
	adjacent := pickpocketResolveBudget(0)
	if adjacent != pickpocketResolveWait {
		t.Errorf("an adjacent click got %v, want the plain %v — nothing is being walked, so nothing extra should be charged",
			adjacent, pickpocketResolveWait)
	}
	// One game tick per tile is the conservative bound: a running account covers two, so
	// this over-estimates rather than expiring early.
	if got, want := pickpocketResolveBudget(8), pickpocketResolveWait+8*pickpocketApproachPerTile; got != want {
		t.Errorf("the farthest click PickpocketStep makes without walking first got %v, want %v", got, want)
	}
	// The measured failure this exists for: at 4 tiles the account is still walking when a
	// flat 2500ms would have expired, which is what acct60's 562 "timeouts" and the false
	// "interrupted before resolving" STRANDED escalation they fed were really made of.
	if got := pickpocketResolveBudget(4); got <= pickpocketResolveWait {
		t.Errorf("a 4-tile click got %v, which is no more than the old flat %v — the walk is unpaid for and the wait still expires mid-approach",
			got, pickpocketResolveWait)
	}
	// Monotonic, so a farther target is never given less time than a nearer one.
	for d := 1; d <= 8; d++ {
		if pickpocketResolveBudget(d) <= pickpocketResolveBudget(d-1) {
			t.Fatalf("budget(%d)=%v is not longer than budget(%d)=%v", d, pickpocketResolveBudget(d), d-1, pickpocketResolveBudget(d-1))
		}
	}
	// Distance comes from a live NPC scan, so guard the degenerate reading rather than
	// letting it produce a negative timeout that would expire instantly on every attempt.
	if got := pickpocketResolveBudget(-3); got != pickpocketResolveWait {
		t.Errorf("a negative distance got %v, want the adjacent budget %v — a bad reading must not make the wait expire instantly", got, pickpocketResolveWait)
	}
	// It must stay a bound, not an open-ended wait: PickpocketStep is a poll loop and the
	// caller's stage has its own budget.
	if got := pickpocketResolveBudget(8); got > 10*time.Second {
		t.Errorf("the worst-case budget is %v — too long for a step that runs on a poll loop", got)
	}
}
