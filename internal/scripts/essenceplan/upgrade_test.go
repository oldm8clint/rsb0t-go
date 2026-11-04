package essenceplan

import (
	"strings"
	"testing"
	"time"
)

func upView(mod func(v *UpgradeView)) UpgradeView {
	v := UpgradeView{Now: t0, Selection: BestAvailable, MiningLevel: 1, Owned: []string{"Bronze pickaxe"}, Coins: 0}
	if mod != nil {
		mod(&v)
	}
	return v
}

func offered(d UpgradeDecision) []string {
	var out []string
	for _, o := range d.Offers {
		out = append(out, o.Item)
	}
	return out
}

// The upgrade ladder: a better tier than the best owned one that the level allows and the coins pay for (list price and a
// quarter on top), best first so a shelf that is out falls to the next.
func TestPickaxeUpgradeDecision(t *testing.T) {
	for _, c := range []struct {
		name    string
		mod     func(v *UpgradeView)
		buy     bool
		offers  string
		whyHas  string
		carry0  int // the first offer's carry
		gainsOK bool
	}{
		{"a fresh miner with no coins has nothing to upgrade to (iron needs Mining 1, 140 coins)", nil, false, "", "no affordable upgrade", 0, false},
		{"Mining 6 and 1,000 coins: steel, then iron", func(v *UpgradeView) { v.MiningLevel, v.Coins = 6, 1000 }, true, "Steel pickaxe,Iron pickaxe", "steel pickaxe", 625, true},
		{"Mining 6, 500 coins do not pay for steel with the margin, iron they do", func(v *UpgradeView) { v.MiningLevel, v.Coins = 6, 500 }, true, "Iron pickaxe", "iron pickaxe", 175, true},
		{"Mining 21, 1,600 coins: mithril (1,625 with the margin) is out of reach, steel is in", func(v *UpgradeView) { v.MiningLevel, v.Coins = 21, 1600 }, true, "Steel pickaxe,Iron pickaxe", "steel pickaxe", 625, true},
		{"Mining 21, 1,700: mithril", func(v *UpgradeView) { v.MiningLevel, v.Coins = 21, 1700 }, true, "Mithril pickaxe,Steel pickaxe,Iron pickaxe", "mithril pickaxe", 1625, true},
		{"Mining 41 with 40,000: rune first, all the way down", func(v *UpgradeView) { v.MiningLevel, v.Coins = 41, 40000 }, true, "Rune pickaxe,Adamant pickaxe,Mithril pickaxe,Steel pickaxe,Iron pickaxe", "rune pickaxe", 40000, true},
		{"Mining 41 with 5,000: adamant", func(v *UpgradeView) { v.MiningLevel, v.Coins = 41, 5000 }, true, "Adamant pickaxe,Mithril pickaxe,Steel pickaxe,Iron pickaxe", "adamant pickaxe", 4000, true},
		{"the best the level allows is already owned", func(v *UpgradeView) { v.MiningLevel, v.Coins, v.Owned = 41, 99999, []string{"Rune pickaxe"} }, false, "", "already owned", 0, false},
		{"a better one than owned only", func(v *UpgradeView) {
			v.MiningLevel, v.Coins, v.Owned = 41, 99999, []string{"Bronze pickaxe", "Mithril pickaxe"}
		}, true, "Rune pickaxe,Adamant pickaxe", "rune pickaxe", 40000, true},
		{"a pickaxe the level cannot use is not owned for this purpose", func(v *UpgradeView) {
			v.MiningLevel, v.Coins, v.Owned = 21, 99999, []string{"Rune pickaxe"}
		}, true, "Mithril pickaxe,Steel pickaxe,Iron pickaxe,Bronze pickaxe", "mithril pickaxe", 1625, false},
		{"no pickaxe at all: any tier is an upgrade, even the bronze one", func(v *UpgradeView) { v.MiningLevel, v.Coins, v.Owned = 1, 10, nil }, true, "Bronze pickaxe", "bronze pickaxe", 1, false},
		{"too poor for anything but bronze", func(v *UpgradeView) { v.MiningLevel, v.Coins, v.Owned = 41, 3, nil }, true, "Bronze pickaxe", "bronze pickaxe", 1, true},
		{"too poor for any upgrade", func(v *UpgradeView) { v.MiningLevel, v.Coins = 41, 100 }, false, "", "no affordable upgrade", 0, false},
		{"a named tier is the only one wanted", func(v *UpgradeView) { v.Selection, v.MiningLevel, v.Coins = "Mithril", 41, 99999 }, true, "Mithril pickaxe", "mithril pickaxe", 1625, true},
		{"a named tier the level cannot use", func(v *UpgradeView) { v.Selection, v.MiningLevel, v.Coins = "Rune", 20, 99999 }, false, "", "no pickaxe tier", 0, false},
		{"a failed trip waits", func(v *UpgradeView) { v.MiningLevel, v.Coins, v.NextTry = 41, 99999, t0.Add(10*time.Minute) }, false, "", "not before", 0, false},
	} {
		d := DecideUpgrade(upView(c.mod))
		if d.Buy != c.buy {
			t.Errorf("%s: buy %v (%s), want %v", c.name, d.Buy, d.Why, c.buy)
			continue
		}
		if got := strings.Join(offered(d), ","); got != c.offers {
			t.Errorf("%s: offers %q, want %q", c.name, got, c.offers)
		}
		if c.whyHas != "" && !strings.Contains(d.Why, c.whyHas) {
			t.Errorf("%s: why %q lacks %q", c.name, d.Why, c.whyHas)
		}
		if c.buy && len(d.Offers) > 0 && c.carry0 > 0 && d.Offers[0].Carry != c.carry0 {
			t.Errorf("%s: first offer carries %d, want %d", c.name, d.Offers[0].Carry, c.carry0)
		}
		if c.gainsOK && c.buy && len(d.Offers) > 0 && d.Offers[0].Rate <= d.OwnedRate {
			t.Errorf("%s: an upgrade of %.0f/h on %.0f/h", c.name, d.Offers[0].Rate, d.OwnedRate)
		}
	}
}

// The numbers behind the choice: a rune pickaxe is worth twice a bronze one and pays itself back in well under an hour of
// the essence's 100 coins in the stalls.
func TestPickaxeUpgradePaysBack(t *testing.T) {
	bronze, rune := EssencePerHour("Bronze pickaxe"), EssencePerHour("Rune pickaxe")
	if rune/bronze < 1.9 || rune/bronze > 2.3 {
		t.Errorf("rune/bronze = %.2f", rune/bronze)
	}
	cost, _ := PickaxeCost("Rune pickaxe")
	hours := float64(cost) / ((rune - bronze) * 100)
	if hours > 0.7 {
		t.Errorf("a rune pickaxe pays back in %.2f h of the extra essence at 100 coins", hours)
	}
	if UpgradeCoinMargin != 25 || UpgradeRetry != 30*time.Minute {
		t.Error("the margins")
	}
}
