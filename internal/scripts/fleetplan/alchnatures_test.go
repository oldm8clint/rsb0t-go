package fleetplan

import (
	"strings"
	"testing"
)

// isTestStock is the allow-list a test ask uses: the owner's default list, as the bot's predicate reads it.
func isTestStock(item string) bool {
	if i := strings.Index(item, " id"); i > 0 {
		item = item[:i] // "Yew shortbow id857" is the name it qualifies
	}
	for _, n := range DefaultAlchItems {
		if strings.EqualFold(n, item) {
			return true
		}
	}
	return false
}

// THE RULE (a user request: "if accounts dont have nature runes they shouldnt take things to alch"): the casts of
// stock a visit may carry are the visit's budget with a Nature rune in the pack, and none without.
func TestStockBudgetIsNothingWithoutNatures(t *testing.T) {
	for _, c := range []struct{ budget, natures, want int }{
		{120, 1, 120}, {120, 400, 120}, {60, 5, 60}, {200, 1, 200},
		{120, 0, 0}, {60, 0, 0}, {200, -3, 0},
		{0, 50, 0}, {-5, 50, 0}, {0, 0, 0},
	} {
		if got := StockBudget(c.budget, c.natures); got != c.want {
			t.Errorf("StockBudget(%d, %d) = %d, want %d", c.budget, c.natures, got, c.want)
		}
	}
}

// An account can cast stock when it holds natures in the pack or its own bank, or the ask brings some.
func TestCanCastStock(t *testing.T) {
	for _, c := range []struct {
		pack, bank int
		asked      bool
		want       bool
	}{
		{1, 0, false, true}, {0, 1, false, true}, {0, 0, true, true}, {5, 5, true, true},
		{0, 0, false, false}, {-1, 0, false, false},
	} {
		if got := CanCastStock(c.pack, c.bank, c.asked); got != c.want {
			t.Errorf("CanCastStock(%d, %d, %v) = %v, want %v", c.pack, c.bank, c.asked, got, c.want)
		}
	}
}

// The worth-asking test counts the own bank's natures too: a plan of stock alone is worth the walk for an account that
// draws its natures from its own bank at the next visit, and not for one that has none anywhere.
func TestWalkAlchKitWorthAskingCountsTheOwnBanksNatures(t *testing.T) {
	stock := []AskPlan{{Rungs: []ItemCount{{Item: "Yew longbow", Count: 100}}}}
	natures := []AskPlan{{Rungs: []ItemCount{{Item: "Nature rune", Count: 50}}}}
	if WalkAlchKitWorthAsking(WalkAlchAskInput{}, stock) {
		t.Error("stock alone for an account with natures nowhere is not worth a walk")
	}
	if !WalkAlchKitWorthAsking(WalkAlchAskInput{BankNatures: 40}, stock) {
		t.Error("stock alone for an account whose bank holds natures is")
	}
	if !WalkAlchKitWorthAsking(WalkAlchAskInput{}, append(append([]AskPlan(nil), stock...), natures...)) {
		t.Error("stock and natures together are")
	}
	if WalkAlchKitWorthAsking(WalkAlchAskInput{}, natures) {
		t.Error("natures with nothing to cast them on are not")
	}
	if !WalkAlchKitWorthAsking(WalkAlchAskInput{Natures: 5, Alchables: 5}, nil) {
		t.Error("an account that carries both needs no plan")
	}
}

func TestNoNaturesLineSaysWhy(t *testing.T) {
	held := NoNaturesLine("the Heroes loop", 67)
	for _, want := range []string{"0 of 67", "no Nature runes", "the Heroes loop", "stays banked"} {
		if !strings.Contains(held, want) {
			t.Errorf("held line %q lacks %q", held, want)
		}
	}
	drawn := NoNaturesLine("the walk kit", 0)
	for _, want := range []string{"drawing none", "no Nature runes", "pack or the bank", "stays banked"} {
		if !strings.Contains(drawn, want) {
			t.Errorf("draw line %q lacks %q", drawn, want)
		}
	}
}
