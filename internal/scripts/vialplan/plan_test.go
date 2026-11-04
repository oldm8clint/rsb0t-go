package vialplan

import "testing"

func TestIsShopRun(t *testing.T) {
	cases := []struct {
		name     string
		runs     int
		buyVials bool
		everyN   int
		want     bool
	}{
		{"disabled never runs", 5, false, 5, false},
		{"first trip never a shop run", 0, true, 5, false},
		{"every-N trip is due", 5, true, 5, true},
		{"off-cadence trip is not due", 4, true, 5, false},
		{"zero interval never runs", 10, true, 0, false},
		{"negative runs never runs", -1, true, 5, false},
		{"every trip when interval is 1", 3, true, 1, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsShopRun(c.runs, c.buyVials, c.everyN); got != c.want {
				t.Errorf("IsShopRun(%d,%v,%d) = %v, want %v", c.runs, c.buyVials, c.everyN, got, c.want)
			}
		})
	}
}

func TestVialsToBuy(t *testing.T) {
	cases := []struct {
		name            string
		freeSlots, want int
		expect          int
	}{
		{"bounded by free slots", 5, 27, 5},
		{"bounded by want", 28, 10, 10},
		{"zero free slots buys nothing", 0, 27, 0},
		{"negative free slots floors at zero", -3, 27, 0},
		{"negative want floors at zero", 10, -1, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := VialsToBuy(c.freeSlots, c.want); got != c.expect {
				t.Errorf("VialsToBuy(%d,%d) = %d, want %d", c.freeSlots, c.want, got, c.expect)
			}
		})
	}
}

func TestCoinsToWithdraw(t *testing.T) {
	cases := []struct {
		name              string
		bankCoins, target int
		expect            int
	}{
		{"bank covers target", 5000, 1000, 1000},
		{"bank short of target draws what's there", 400, 1000, 400},
		{"empty bank draws nothing", 0, 1000, 0},
		{"negative bank floors at zero", -5, 1000, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CoinsToWithdraw(c.bankCoins, c.target); got != c.expect {
				t.Errorf("CoinsToWithdraw(%d,%d) = %d, want %d", c.bankCoins, c.target, got, c.expect)
			}
		})
	}
}

func TestBankTile(t *testing.T) {
	if BankTile("") != FaladorWestBank {
		t.Error("blank bank setting should default to Falador West")
	}
	if BankTile("Falador West") != FaladorWestBank {
		t.Error("Falador West should resolve to the west bank")
	}
	if BankTile("falador east") != FaladorEastBank {
		t.Error("Falador East should resolve case-insensitively to the east bank")
	}
	if BankTile("nonsense") != FaladorWestBank {
		t.Error("an unknown bank setting should fall back to West")
	}
}

func TestVialSettingsNormalised(t *testing.T) {
	s := Settings{}.Normalised()
	if s.Bank != "Falador West" || s.BuyEveryRuns != 1 || s.BuyQty != 1 || s.CoinsPerTrip != 1 {
		t.Errorf("zero-value settings should clamp to sane minimums, got %+v", s)
	}
	over := Settings{BuyEveryRuns: 999, BuyQty: 999, CoinsPerTrip: 999_999}.Normalised()
	if over.BuyEveryRuns != 50 || over.BuyQty != PackSlots || over.CoinsPerTrip != 100_000 {
		t.Errorf("oversized settings should clamp to maximums, got %+v", over)
	}
}
