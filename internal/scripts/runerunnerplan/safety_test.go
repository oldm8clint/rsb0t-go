package runerunnerplan

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Test 12: the Lundail death policy.
func TestLundailDeathPolicy(t *testing.T) {
	now := time.Unix(5_000_000, 0)
	if ok, _ := LundailAllowed(nil, now, 2); !ok {
		t.Error("no deaths: allowed")
	}
	one := []time.Time{now.Add(-time.Hour)}
	if ok, _ := LundailAllowed(one, now, 2); !ok {
		t.Error("one death: allowed")
	}
	two := []time.Time{now.Add(-20 * time.Hour), now.Add(-time.Hour)}
	ok, why := LundailAllowed(two, now, 2)
	if ok || !strings.Contains(why, "2 Lundail deaths") {
		t.Errorf("two deaths in 24h: %v %q", ok, why)
	}
	// Five hours on, the first has aged out.
	if ok, _ := LundailAllowed(two, now.Add(5*time.Hour), 2); !ok {
		t.Error("the oldest death aged out: allowed again")
	}
	st := NewState()
	st.NoteLundailDeath(now.Add(-25 * time.Hour))
	st.NoteLundailDeath(now)
	if len(st.LundailDeaths) != 1 {
		t.Errorf("a death older than the window is pruned: %v", st.LundailDeaths)
	}
}

// Test 13: no lever unless the kit is the blade alone, coins are planned and the purchase is
// worth the crossing.
func TestLeverGo(t *testing.T) {
	ok := LeverCheck{KitOK: true, PlanCoins: 1227, PlanRunes: 140, MinVisitRunes: 50, DeathsOK: true}
	if go_, why := LeverGo(ok); !go_ {
		t.Fatalf("a good kit refused: %s", why)
	}
	for _, c := range []struct {
		name string
		edit func(*LeverCheck)
		why  string
	}{
		{"kit", func(c *LeverCheck) { c.KitOK = false; c.KitWhy = "carrying 5 coins" }, "carrying 5 coins"},
		{"no coins", func(c *LeverCheck) { c.PlanCoins = 0 }, "no coins"},
		{"too few runes", func(c *LeverCheck) { c.PlanRunes = 49 }, "minVisitRunes"},
		{"deaths", func(c *LeverCheck) { c.DeathsOK = false; c.DeathsWhy = "2 Lundail deaths" }, "2 Lundail deaths"},
	} {
		in := ok
		c.edit(&in)
		if go_, why := LeverGo(in); go_ || !strings.Contains(why, c.why) {
			t.Errorf("%s: %v %q", c.name, go_, why)
		}
	}
}

// Test 14: each blocking condition alone stops a hop, with its reason.
func TestMayHop(t *testing.T) {
	now := time.Unix(6_000_000, 0)
	clear := HopCheck{HP: 10, MaxHP: 10, Now: now, LastHurt: now.Add(-time.Minute), Here: &tile.Tile{X: 3253, Z: 3402}}
	if ok, why := MayHop(clear); !ok {
		t.Fatalf("all clear refused: %s", why)
	}
	for _, c := range []struct {
		name string
		edit func(*HopCheck)
		why  string
	}{
		{"dead", func(c *HopCheck) { c.Dead = true }, "dead"},
		{"low hp", func(c *HopCheck) { c.HP = 4 }, "under half"},
		{"in combat", func(c *HopCheck) { c.InCombat = true }, "in combat"},
		{"hurt recently", func(c *HopCheck) { c.LastHurt = now.Add(-5 * time.Second) }, "hurt"},
		{"random event", func(c *HopCheck) { c.RandomEvent = true }, "random event"},
		{"wilderness", func(c *HopCheck) { c.Here = &tile.Tile{X: 3154, Z: 3924} }, "Wilderness"},
		{"mage bank", func(c *HopCheck) { c.Here = &tile.Tile{X: 2534, Z: 4714} }, "Mage Bank"},
		{"trip", func(c *HopCheck) { c.TripActive = true }, "trip"},
		{"login busy", func(c *HopCheck) { c.LoginBusy = true }, "login"},
		{"hop running", func(c *HopCheck) { c.HopInProgress = true }, "in progress"},
		{"no tile", func(c *HopCheck) { c.Here = nil }, "position"},
	} {
		in := clear
		c.edit(&in)
		if ok, why := MayHop(in); ok || !strings.Contains(why, c.why) {
			t.Errorf("%s: %v %q", c.name, ok, why)
		}
	}
	// Exactly half is enough.
	half := clear
	half.HP = 5
	if ok, _ := MayHop(half); !ok {
		t.Error("half hitpoints may hop")
	}
}

func TestStrikesTakeAPairOff(t *testing.T) {
	now := time.Unix(7_000_000, 0)
	st := NewState()
	if st.Strike("Betty@1", now) || st.Strike("Betty@1", now) {
		t.Fatal("two strikes do not take a pair off")
	}
	if !st.Strike("Betty@1", now) {
		t.Fatal("the third strike takes it off")
	}
	if off, _ := st.PairOff("Betty@1", now.Add(59*time.Minute)); !off {
		t.Error("off for an hour")
	}
	if off, _ := st.PairOff("Betty@1", now.Add(61*time.Minute)); off {
		t.Error("back after the hour")
	}
	st.Strike("Aubury@2", now)
	st.ClearStrikes("Aubury@2")
	if st.PairStrikes["Aubury@2"] != 0 {
		t.Error("a shop that opened clears its strikes")
	}
}

// Test 18 (state half): two mismatches disable the pair until the operator restarts.
func TestPriceMismatchDisablesPair(t *testing.T) {
	now := time.Unix(8_000_000, 0)
	st := NewState()
	if m, d := st.NotePrice("Betty@1", 1000, 1050); m || d {
		t.Fatal("within 10% is fine")
	}
	if m, d := st.NotePrice("Betty@1", 1000, 1500); !m || d {
		t.Fatal("one mismatch does not disable")
	}
	if m, d := st.NotePrice("Betty@1", 1000, 1500); !m || !d {
		t.Fatal("the second mismatch disables")
	}
	if off, why := st.PairOff("Betty@1", now.Add(1000*time.Hour)); !off || !strings.Contains(why, "restarted") {
		t.Errorf("disabled until restart: %v %q", off, why)
	}
	if cleared := st.ClearPriceChecks(); len(cleared) != 1 || cleared[0] != "Betty@1" {
		t.Errorf("an operator restart clears %v", cleared)
	}
	if off, _ := st.PairOff("Betty@1", now); off {
		t.Error("cleared")
	}
}

func TestFareShortTopsThePackUpToBothFaresEachWay(t *testing.T) {
	for _, c := range []struct{ held, want int }{{0, FareFloat}, {50, FareFloat - 50}, {FareFloat, 0}, {5000, 0}} {
		if got := FareShort(c.held); got != c.want {
			t.Errorf("FareShort(%d) = %d, want %d", c.held, got, c.want)
		}
	}
	if FareFloat != 120 {
		t.Errorf("FareFloat = %d, want 120 (four 30-coin fares)", FareFloat)
	}
}
