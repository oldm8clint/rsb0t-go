package woodcutplan

import (
	"math"
	"testing"
	"time"
)

// The normal tree is the whole problem, and this pins the arithmetic behind that claim: one log
// per tree per ~96 production seconds, so a camp of N trees supports N * 0.63 logs/min shared
// between everyone standing in it.
func TestCampSupplyNormalTree(t *testing.T) {
	got := CampSupply(1, "Normal tree", ProductionTick, 0)
	want := 1.0 / (159.5 * 0.6 / 60)
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("CampSupply(1 normal tree) = %.4f logs/min, want %.4f", got, want)
	}
	if got > 0.7 || got < 0.6 {
		t.Errorf("one normal tree should be about 0.63 logs/min, got %.3f", got)
	}
	// Ten trees is ten times the supply: nothing in the model is per-account.
	if ten := CampSupply(10, "Normal tree", ProductionTick, 0); math.Abs(ten-10*got) > 1e-9 {
		t.Errorf("supply should scale with the tree count: %.4f vs %.4f", ten, 10*got)
	}
}

// An oak is worth dozens of normal trees, which is the argument for spreading a cohort up the
// ladder rather than only capping it at one camp.
func TestOakIsFarRicherThanANormalTree(t *testing.T) {
	normal := CampSupply(1, "Normal tree", ProductionTick, 0)
	oak := CampSupply(1, "Oak", ProductionTick, 0)
	if oak <= normal*20 {
		t.Errorf("one oak (%.2f logs/min) should be far richer than one normal tree (%.2f)", oak, normal)
	}
	if want := 8.0 / (30 * 0.6 / 60); math.Abs(oak-want) > 1e-9 {
		t.Errorf("oak supply = %.3f, want %.3f", oak, want)
	}
}

func TestCampSupplyRefusesWhatItDoesNotKnow(t *testing.T) {
	if got := CampSupply(10, "Banana tree", ProductionTick, 0); got != 0 {
		t.Errorf("an unknown tier must be 0, not a guess: got %.3f", got)
	}
	if got := CampSupply(0, "Oak", ProductionTick, 0); got != 0 {
		t.Errorf("no trees must be 0 supply, got %.3f", got)
	}
	if got := CampSupply(10, "Oak", 0, 0); got != 0 {
		t.Errorf("no tick must be 0 supply, got %.3f", got)
	}
}

// scale_by_playercount is modelled because it is real, and tested because it is small: a busy
// world helps by single-digit percent and must never be quoted as a fix.
func TestPlayerCountScaleIsASmallEffect(t *testing.T) {
	if s := PlayerCountScale(0); s != 1 {
		t.Errorf("an empty world should not scale the respawn: %.4f", s)
	}
	if s := PlayerCountScale(269); s < 0.92 || s > 0.94 {
		t.Errorf("269 players should shorten the respawn by about 6.7 percent, got scale %.4f", s)
	}
	if s := PlayerCountScale(5000); s != 0.5 {
		t.Errorf("the proc caps at 2000 players, so the floor is 0.5: got %.4f", s)
	}
}

func TestCampAccounts(t *testing.T) {
	// The measured single-account rate: 309 xp/min on a 200ms lab tick is 103 xp/min at the
	// production tick, which on normal logs (25 xp) is about 4.1 logs/min.
	perAccount := LogsPerMinute(309/3.0, TIERS[0])
	if perAccount < 4.0 || perAccount > 4.2 {
		t.Fatalf("expected about 4.1 logs/min per account, got %.2f", perAccount)
	}
	supply := CampSupply(20, "Normal tree", ProductionTick, 0)
	n := CampAccounts(supply, perAccount)
	// 20 trees * 0.627 = 12.5 logs/min against 4.1 each: about 3 accounts.
	if n < 2.5 || n > 3.5 {
		t.Errorf("20 normal trees should feed about 3 accounts at full speed, got %.2f", n)
	}
	if CampAccounts(0, perAccount) != 0 || CampAccounts(supply, 0) != 0 {
		t.Error("an unknown input must answer 0, never a guess")
	}
}

// Every tier on the ladder needs a supply row, or a camp census silently reports 0 for it.
func TestEverySupplyTierIsOnTheLadder(t *testing.T) {
	for _, tier := range TIERS {
		if _, ok := TierSupply[tier.Label]; !ok {
			t.Errorf("no TierSupply row for ladder tier %q", tier.Label)
		}
	}
	// The Draynor start camp is deliberately in TierSupply and deliberately NOT on the ladder:
	// it is the script's own alternative place to begin, and a census that leaves it out
	// cannot answer "start somewhere else" — which is the obvious response to a 2-account
	// camp. Anything ELSE in the table is a typo.
	known := map[string]bool{DraynorTier().Label: true}
	for _, tier := range TIERS {
		known[tier.Label] = true
	}
	for label := range TierSupply {
		if !known[label] {
			t.Errorf("TierSupply has %q, which is neither a ladder tier nor the Draynor start camp", label)
		}
	}
	if _, ok := TierSupply[DraynorTier().Label]; !ok {
		t.Error("the Draynor start camp needs a supply row, or a census silently reports 0 for it")
	}
	// Its dead trees live in the same normal.loc as ordinary trees, so they must run on the
	// normal tree's rule — one log, a 120-199 tick stump — and not look richer by accident.
	if TierSupply[DraynorTier().Label] != TierSupply["Normal tree"] {
		t.Errorf("Draynor's dead trees are in normal.loc, so their supply rule must match the normal tree's: %+v vs %+v",
			TierSupply[DraynorTier().Label], TierSupply["Normal tree"])
	}
}

func TestProductionTickIsSixHundredMs(t *testing.T) {
	if ProductionTick != 600*time.Millisecond {
		t.Errorf("ProductionTick = %s", ProductionTick)
	}
}
