package questrunner

import "testing"

// Live 2 Oct 11:26 acct158 held 97 coins and could not buy a 1 gp Hammer: the fare reserve (100)
// stopped the purchase outright, eight Vampire Slayer parks; the same account held 51, 37, 46 and
// 14 coins at its later tries (the reserve was a wall at every one). A cheap purchase is never held
// back by the reserve; an expensive one still is.
func TestAPoorAccountCanStillBuyAOneCoinHammer(t *testing.T) {
	for _, coins := range []int{97, 51, 46, 37, 14, 1} {
		if got := ShopChunk(coins, 0, 1, 1, 0); got != 1 {
			t.Errorf("the first Hammer (base price 1) with %d coins held: got %d, want 1", coins, got)
		}
		if got := ShopChunk(coins, 1, 1, 1, 1); got != 1 {
			t.Errorf("a learned 1 gp price with %d coins held: got %d, want 1", coins, got)
		}
	}
	// Nothing left to buy, or no coins: nothing.
	if got := ShopChunk(96, 1, 1, 0, 1); got != 0 {
		t.Errorf("nothing left to buy: got %d, want 0", got)
	}
	if got := ShopChunk(0, 0, 1, 1, 0); got != 0 {
		t.Errorf("no coins: got %d, want 0", got)
	}
}

// The reserve still holds for anything that is not small.
func TestTheFareReserveStillHoldsForAnythingBig(t *testing.T) {
	cases := []struct {
		name                           string
		coins, unit, hint, left, spent int
		want                           int
	}{
		{"a 4 gp rune, 97 coins: only what the cheap allowance covers", 97, 4, 4, 6, 0, 2},
		{"a 4 gp rune, the allowance already spent", 97, 4, 4, 6, 10, 0},
		{"a 4 gp rune, 97 coins, 8 coins spent already: 2 left of the allowance, none fits", 97, 4, 4, 6, 8, 0},
		{"the first purchase of a 200 gp item with 97 coins is refused unseen", 97, 0, 200, 1, 0, 0},
		{"a 12 gp item is not cheap", 97, 0, 12, 1, 0, 0},
		{"a 10 gp item is", 97, 0, 10, 1, 0, 1},
		{"an unknown price at or under the reserve is not tried", 97, 0, 0, 1, 0, 0},
		{"an unknown price above the reserve is tried", 101, 0, 0, 1, 0, 1},
		{"plenty of coins: chunks of ten", 500, 4, 4, 30, 0, 10},
		{"plenty of coins, a smaller want", 500, 4, 4, 3, 0, 3},
		{"room is what is above the reserve", 130, 4, 4, 30, 0, 7}, // 30 / 4
		{"a little above the reserve", 112, 4, 4, 30, 0, 3},        // 12 / 4, the cheap allowance (2) is less
		{"empty pockets", 0, 1, 1, 5, 0, 0},
		{"a want of zero", 500, 1, 1, 0, 0, 0},
	}
	for _, c := range cases {
		if got := ShopChunk(c.coins, c.unit, c.hint, c.left, c.spent); got != c.want {
			t.Errorf("%s: ShopChunk(%d,%d,%d,%d,%d) = %d, want %d", c.name, c.coins, c.unit, c.hint, c.left, c.spent, got, c.want)
		}
	}
}

// A buying loop driven only by ShopChunk, as BuyFromShopAt drives it, never spends more than
// CheapSpend out of the reserve in one visit, whatever the price, the want and the coins held.
func TestAShopLoopSpendsAtMostTheCheapAllowanceOutOfTheReserve(t *testing.T) {
	for _, unit := range []int{1, 2, 3, 4, 7, 10, 18, 50} {
		for coins := 0; coins <= 300; coins += 7 {
			held, left, spent := coins, 30, 0
			first := true
			for i := 0; i < 200 && left > 0; i++ {
				u := unit
				if first {
					u = 0
				}
				n := ShopChunk(held, u, unit, left, spent)
				if n <= 0 {
					break
				}
				first = false
				held -= n * unit
				spent += n * unit
				left -= n
				if held < 0 {
					t.Fatalf("unit %d, %d coins: spent more than it had", unit, coins)
				}
			}
			floor := FareReserve
			if coins < floor {
				floor = coins
			}
			if held < floor-CheapSpend {
				t.Errorf("unit %d, %d coins: spent %d and holds %d, more than %d below the reserve", unit, coins, spent, held, CheapSpend)
			}
		}
	}
}
