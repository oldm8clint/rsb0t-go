package freshstartrunner

import "testing"

// The grind this bounds, reproduced as arithmetic rather than as a live account.
//
// Two accounts were seen standing still, clicking, missing, and never getting anywhere: one
// logged 183 "timed out waiting for the pickpocket attempt to resolve" in 17 minutes at
// Thieving 3 holding 0 coins. Every existing counter kept resetting, because each of them
// escalates on a specific failure shape and nothing was failing per attempt — combatStuck in
// particular resets while the account is still beside its target, since an in-place miss is
// ordinary variance.
//
// So the bound has to be on the objective, not on the attempt. These cases pin the two
// properties that matter: a run of misses must not escalate on its own, and a run with no
// COINS must, even while Thieving XP is rising.
//
// These drive Thief.coinProgress, the method noteCoinProgress calls. They used to drive a local
// copy of its body, so they could not fail when the real rule changed.
func TestCoinProgressWindow(t *testing.T) {
	step := func(th *Thief, coins int) { th.coinProgress(coins) }

	t.Run("ordinary bad luck does not escalate", func(t *testing.T) {
		th := &Thief{}
		// A dozen misses, then a success. Well short of the bound.
		for i := 0; i < 12; i++ {
			step(th, 0)
		}
		step(th, 3)
		if th.noCoinGain != 0 {
			t.Errorf("noCoinGain = %d after a coin was gained, want 0 — a run of misses must not accumulate toward STRANDED once the account is earning", th.noCoinGain)
		}
	})

	t.Run("landing pockets without ever gaining coins still escalates", func(t *testing.T) {
		// The live shape: Thieving XP rose, so pockets were being landed, but the coin total
		// never moved. An XP-based check would never fire here.
		th := &Thief{}
		for i := 0; i < maxNoCoinGain; i++ {
			step(th, 0)
		}
		if th.noCoinGain < maxNoCoinGain {
			t.Fatalf("noCoinGain = %d after %d attempts with no coins, want at least %d", th.noCoinGain, maxNoCoinGain, maxNoCoinGain)
		}
	})

	// REWRITTEN 2026-09-25. This case used to be "coins going back down does not reset the
	// window" and asserted 5 here: spending what was earned must not look like progress, or an
	// account that funds a fare, is displaced, and funds it again forever would never be caught.
	// That reasoning cost more than it caught. Every drop this step can see is something outside
	// it (a fare paid, a death, a deposit, a hand-off), after which the account really is funding
	// again from a lower balance; keeping the old high-water mark meant the next funding run could
	// not beat it before the fare was covered, so it was called STRANDED while earning. Live on
	// 2026-09-25: 32 of these STRANDED lines from 20:16 to 22:40, 30 of them holding more coins than
	// a death leaves ("holding 57 of the 60 needed, 119 pockets landed"). A fund-and-lose loop is
	// not this counter's to catch: the sail and walk paths that displace an account have their own
	// STRANDED bounds (SailProgress, walkStuck), and a false STRANDED here hides the real ones.
	t.Run("a fare paid or a death starts a new funding window", func(t *testing.T) {
		th := &Thief{}
		for c := 3; c <= 62; c += 3 {
			step(th, c) // funded the fare to 62
		}
		step(th, 2) // paid 60 at the dock
		if th.noCoinGain != 0 || th.bestCoins != 2 {
			t.Fatalf("after paying the fare: noCoinGain %d, bestCoins %d; want 0 and 2", th.noCoinGain, th.bestCoins)
		}
		// Funding the next fare from 2, three misses to every pocket: about 80 attempts to get back
		// to 60, and 60 of them pass before the balance could beat the old 62. Never STRANDED.
		stranded := false
		for coins, i := 2, 0; coins < 60; i++ {
			if i%4 == 3 {
				coins += 3
			}
			stranded = step2(th, coins) || stranded
		}
		if stranded {
			t.Error("an account refunding its fare from a lower balance was called STRANDED while its coins rose")
		}
		// A death drops the coins to 0: same again.
		step(th, 0)
		if th.noCoinGain != 0 || th.bestCoins != 0 {
			t.Errorf("after a death: noCoinGain %d, bestCoins %d; want 0 and 0", th.noCoinGain, th.bestCoins)
		}
	})

	t.Run("a flat balance after a drop still escalates", func(t *testing.T) {
		// The negative control: the reset is one window, not a pass. An account that paid a fare
		// and then earns nothing at all is still caught.
		th := &Thief{}
		step(th, 62)
		step(th, 2)
		stranded := false
		for i := 0; i < maxNoCoinGain; i++ {
			stranded = step2(th, 2) || stranded
		}
		if !stranded {
			t.Errorf("%d attempts at a flat 2 coins after paying a fare did not escalate", maxNoCoinGain)
		}
	})
}

// The bound has to be far enough above a normal funding run that bad luck never trips it.
// A Man yields a few coins and a low-Thieving account lands roughly a third of its attempts,
// so a 30-coin fare is normally a couple of dozen attempts.
// step2 is one attempt that reports the STRANDED verdict.
func step2(th *Thief, coins int) bool { return th.coinProgress(coins) }

func TestMaxNoCoinGainLeavesRoomForANormalFundingRun(t *testing.T) {
	const typicalAttemptsToFundAFare = 30
	if maxNoCoinGain <= typicalAttemptsToFundAFare {
		t.Errorf("maxNoCoinGain = %d, which is within an ordinary funding run of about %d attempts — it would escalate on bad luck", maxNoCoinGain, typicalAttemptsToFundAFare)
	}
}
