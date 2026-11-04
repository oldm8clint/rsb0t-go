package marketbulkplan

import (
	"errors"
	"testing"
)

func valid() Settings {
	return Settings{Quantity: 52450, MaxUnitPrice: 95, Budget: 5_000_000}.Normalised()
}

func TestValidateRefusesWithoutQuantityOrCap(t *testing.T) {
	s := valid()
	s.Quantity = 0
	if err := s.Validate(); !errors.Is(err, ErrNoQuantity) {
		t.Fatalf("no quantity: %v", err)
	}
	s = valid()
	s.MaxUnitPrice = 0
	if err := s.Validate(); !errors.Is(err, ErrNoPriceCap) {
		t.Fatalf("no cap: %v", err)
	}
	s = valid()
	s.UnitPrice = 96
	if err := s.Validate(); !errors.Is(err, ErrOverCap) {
		t.Fatalf("unit over cap: %v", err)
	}
	s = valid()
	s.Budget = 0
	if err := s.Validate(); !errors.Is(err, ErrNoBudget) {
		t.Fatalf("no budget: %v", err)
	}
	if err := valid().Validate(); err != nil {
		t.Fatalf("a complete request refused: %v", err)
	}
}

func TestQuoteOnlyNeedsAQuantityButNoCap(t *testing.T) {
	s := Settings{Quantity: 100, QuoteOnly: true}.Normalised()
	if err := s.Validate(); err != nil {
		t.Fatalf("quote-only refused: %v", err)
	}
	s.Quantity = 0
	if err := s.Validate(); !errors.Is(err, ErrNoQuantity) {
		t.Fatalf("quote-only without a quantity: %v", err)
	}
}

func TestNormalisedDefaultsNeverThePriceOrQuantity(t *testing.T) {
	s := Settings{}.Normalised()
	if s.Quantity != 0 || s.MaxUnitPrice != 0 || s.UnitPrice != 0 {
		t.Fatalf("defaulted a price or quantity: %+v", s)
	}
	if s.Item != "Chaos rune" || s.Seller != "Seers Market" || s.TradeCoinLimit != 5_000_000 || s.Post != CatherbyPost || s.Bank != SeersBank {
		t.Fatalf("defaults: %+v", s)
	}
}

func TestUnitDefaultsToTheCap(t *testing.T) {
	if u := valid().Unit(); u != 95 {
		t.Fatalf("unit %d", u)
	}
	s := valid()
	s.UnitPrice = 90
	if u := s.Unit(); u != 90 {
		t.Fatalf("unit %d", u)
	}
}

func TestTheOwnersTradeIsOneTrade(t *testing.T) {
	trades, err := PlanTrades(52450, 95, DefaultTradeCoinLimit)
	if err != nil {
		t.Fatal(err)
	}
	if len(trades) != 1 || trades[0].Qty != 52450 || trades[0].Coins != 4_982_750 {
		t.Fatalf("got %+v", trades)
	}
}

func TestSmallestLiveBuy(t *testing.T) {
	trades, err := PlanTrades(100, 95, DefaultTradeCoinLimit)
	if err != nil || len(trades) != 1 || trades[0].Coins != 9500 {
		t.Fatalf("got %+v %v", trades, err)
	}
}

func TestSplitStaysUnderTheLimitAndSumsExactly(t *testing.T) {
	// 5,000,000 / 95 = 52,631 a trade at most.
	for _, qty := range []int{52631, 52632, 105262, 105263, 200000} {
		trades, err := PlanTrades(qty, 95, DefaultTradeCoinLimit)
		if err != nil {
			t.Fatalf("%d: %v", qty, err)
		}
		sum := 0
		for _, tr := range trades {
			if tr.Coins > DefaultTradeCoinLimit {
				t.Fatalf("%d: a trade of %d coins is over the limit", qty, tr.Coins)
			}
			if tr.Coins != tr.Qty*95 {
				t.Fatalf("%d: coins %d for %d", qty, tr.Coins, tr.Qty)
			}
			sum += tr.Qty
		}
		if sum != qty {
			t.Fatalf("%d: trades sum to %d", qty, sum)
		}
		want := (qty + 52630) / 52631
		if len(trades) != want {
			t.Fatalf("%d: %d trades, want %d", qty, len(trades), want)
		}
		if d := trades[0].Qty - trades[len(trades)-1].Qty; d > 1 {
			t.Fatalf("%d: uneven split %+v", qty, trades)
		}
	}
}

func TestPlanRefusals(t *testing.T) {
	if _, err := PlanTrades(0, 95, 0); !errors.Is(err, ErrNoQuantity) {
		t.Fatalf("zero qty: %v", err)
	}
	if _, err := PlanTrades(10, 0, 0); !errors.Is(err, ErrNoPriceCap) {
		t.Fatalf("zero unit: %v", err)
	}
	if _, err := PlanTrades(10, 6_000_000, 5_000_000); !errors.Is(err, ErrUnitOverLimit) {
		t.Fatalf("unit over limit: %v", err)
	}
	if _, err := PlanTrades(52631*MaxTrades+1, 95, 0); !errors.Is(err, ErrTooManyTrades) {
		t.Fatalf("too many trades: %v", err)
	}
}

func TestBudget(t *testing.T) {
	// The fleet ledger read 14,175 spent before the bulk buy: 4,985,825 left of 5M.
	if err := CheckBudget(4_982_750, 5_000_000-14_175); err != nil {
		t.Fatalf("the owner's trade fits: %v", err)
	}
	if err := CheckBudget(4_982_750, 4_982_749); !errors.Is(err, ErrBudgetSpent) {
		t.Fatalf("one coin short: %v", err)
	}
	if err := CheckBudget(1, 0); !errors.Is(err, ErrBudgetSpent) {
		t.Fatalf("spent: %v", err)
	}
}

func TestQuote(t *testing.T) {
	if err := CheckQuote(95, 95); err != nil {
		t.Fatal(err)
	}
	if err := CheckQuote(100, 95); !errors.Is(err, ErrQuoteOverCap) {
		t.Fatalf("over cap: %v", err)
	}
}

func TestJudgeTrade(t *testing.T) {
	tr := Trade{Qty: 100, Unit: 95, Coins: 9500}
	cases := []struct {
		got, paid int
		want      Outcome
	}{
		{100, 9500, Bought},
		{0, 0, Nothing},
		{0, 9500, Lost},
		{50, 9500, Partial},
		{100, 9600, Partial},
		{100, 0, Partial},
	}
	for _, c := range cases {
		if o := JudgeTrade(tr, c.got, c.paid); o != c.want {
			t.Fatalf("got %d paid %d: %v, want %v", c.got, c.paid, o, c.want)
		}
		if Continue(c.want) != (c.want == Bought) {
			t.Fatalf("continue after %v", c.want)
		}
	}
}
