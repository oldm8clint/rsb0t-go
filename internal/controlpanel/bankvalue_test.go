package controlpanel

import (
	"encoding/json"
	"net/http"
	"testing"

	"rsb0t-go/internal/bankxp"
)

// bankSource is a Source that reports fixed banks and fixed skill levels, which is all the
// valuation and banked-experience code reads. It deliberately implements BankAllSource so the
// tests take the same path the live fleet does.
type bankSource struct {
	staticSource
	banks []AccountBank
}

func (b *bankSource) Bank(account string) ([]Slot, int64, bool) {
	for _, bank := range b.banks {
		if bank.Account == account {
			return bank.Items, bank.UpdatedAtUnixMs, true
		}
	}
	return nil, 0, false
}

func (b *bankSource) AllBanks() []AccountBank { return b.banks }

// Real ids and names from the same content the route table is generated from, so the test is
// exercising the actual lookup rather than a private fiction.
const (
	rawLobsterID = 377
	logsID       = 1511
	coinsID2     = 995
)

func newBankHarness(t *testing.T, prices *PriceBook, accounts []AccountStatus, banks []AccountBank) *harness {
	t.Helper()
	// Levels travel ON the bank, the way FleetSource.AllBanks reports them, so the test takes
	// the production path rather than a second roster read the real endpoint no longer makes.
	for i := range banks {
		for _, a := range accounts {
			if a.Name != banks[i].Account || len(a.Skills) == 0 {
				continue
			}
			levels := map[string]int{}
			for name, c := range a.Skills {
				levels[name] = c.Level
			}
			banks[i].Levels = levels
		}
	}
	src := &bankSource{staticSource: staticSource{accounts: accounts}, banks: banks}
	return newHarness(t, Config{Prices: prices}, testOptions(), src)
}

func bankAllBody(t *testing.T, h *harness, query string) struct {
	Banks     []bankBody    `json:"banks"`
	Prices    []priceEntry  `json:"prices"`
	PriceBook priceBookInfo `json:"priceBook"`
	XP        bankXPBody    `json:"xp"`
} {
	t.Helper()
	var out struct {
		Banks     []bankBody    `json:"banks"`
		Prices    []priceEntry  `json:"prices"`
		PriceBook priceBookInfo `json:"priceBook"`
		XP        bankXPBody    `json:"xp"`
	}
	res := h.get("/api/bank-all" + query)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("/api/bank-all%s = %d", query, res.StatusCode)
	}
	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decoding /api/bank-all: %v", err)
	}
	return out
}

// TestBankAllPricesEveryDistinctItemOnce is the shape the panel depends on: one price row per
// DISTINCT item id, not one per slot and not one per account. At the live roster that is the
// difference between a few dozen rows and a few thousand.
func TestBankAllPricesEveryDistinctItemOnce(t *testing.T) {
	book := &PriceBook{prices: map[string]MarketPrice{
		"raw lobster": {Name: "Raw lobster", Coins: 200, Trades: 22, Units: 281692},
	}}
	h := newBankHarness(t, book,
		[]AccountStatus{{Name: "a"}, {Name: "b"}},
		[]AccountBank{
			{Account: "a", Items: []Slot{{Slot: 0, ID: rawLobsterID, Name: "Raw lobster", Count: 100}, {Slot: 1, ID: coinsID2, Name: "Coins", Count: 500}}},
			{Account: "b", Items: []Slot{{Slot: 0, ID: rawLobsterID, Name: "Raw lobster", Count: 50}}},
		})
	body := bankAllBody(t, h, "")

	if len(body.Prices) != 2 {
		t.Fatalf("priced %d rows, want 2 (one per distinct item, across three slots in two banks)", len(body.Prices))
	}
	by := map[int]priceEntry{}
	for _, p := range body.Prices {
		by[p.ID] = p
	}
	lob := by[rawLobsterID]
	if lob.Unit != 200 || lob.Source != string(ValueFromMarket) {
		t.Errorf("raw lobster = %d from %q, want 200 from market", lob.Unit, lob.Source)
	}
	if lob.Trades != 22 || lob.Units != 281692 {
		t.Errorf("the market sample must travel with the price so a thin one is visible: got %d trades / %d units", lob.Trades, lob.Units)
	}
	// A coin is a coin, and it must never fall to "unknown" — PriceBook.Value states this
	// explicitly because high alchemy of a 1-coin item rounds to zero.
	if by[coinsID2].Unit != 1 {
		t.Errorf("coins = %d, want 1", by[coinsID2].Unit)
	}
}

// TestBankAllReportsAnUnpricedItemAsUnknown: a missing price is UNKNOWN, never zero. Without a
// config archive there is no cache cost either, so an item the market has never seen has no
// value at all, and the response has to say so rather than quietly contributing nothing to a
// total that looks complete.
func TestBankAllReportsAnUnpricedItemAsUnknown(t *testing.T) {
	h := newBankHarness(t, &PriceBook{prices: map[string]MarketPrice{}},
		[]AccountStatus{{Name: "a"}},
		[]AccountBank{{Account: "a", Items: []Slot{{ID: rawLobsterID, Name: "Raw lobster", Count: 10}}}})
	body := bankAllBody(t, h, "")
	if len(body.Prices) != 1 {
		t.Fatalf("want one price row, got %d", len(body.Prices))
	}
	if body.Prices[0].Unit != 0 || body.Prices[0].Source != "none" {
		t.Errorf("an item with no market price and no cache cost = %d from %q, want 0 from \"none\"", body.Prices[0].Unit, body.Prices[0].Source)
	}
	if body.PriceBook.Cache {
		t.Error("this harness has no config archive; the response must admit there is no high-alch fallback")
	}
	if !body.PriceBook.Enabled {
		t.Error("a price book was configured, so the panel should be told the market lookup is on")
	}
}

// TestBankAllBankedXPUsesTheFleetRouteByDefault pins the defaults, which are the conservative
// ones: only routes a registered script here actually runs, and levels ignored.
func TestBankAllBankedXPUsesTheFleetRouteByDefault(t *testing.T) {
	h := newBankHarness(t, nil,
		[]AccountStatus{{Name: "a"}},
		[]AccountBank{{Account: "a", Items: []Slot{{ID: logsID, Name: "Logs", Count: 100}}}})
	body := bankAllBody(t, h, "")

	if body.XP.Policy != string(bankxp.PolicyFleet) || body.XP.Levels != "ignore" || body.XP.Rate != 1 {
		t.Errorf("defaults = %q/%q/%v, want fleet/ignore/1", body.XP.Policy, body.XP.Levels, body.XP.Rate)
	}
	// BankFletcher cuts Logs into Arrow shafts: 15 shafts at 5 tenths each, 7.5 XP a log.
	if body.XP.Combined.XP != 750 {
		t.Errorf("100 logs on the fleet's own route = %d XP, want 750", body.XP.Combined.XP)
	}
	if len(body.XP.Accounts) != 1 || body.XP.Accounts[0].XP != 750 {
		t.Errorf("per-account total = %+v, want one account with 750", body.XP.Accounts)
	}

	// Asking for the best route instead must pick burning (40 XP) and say which it took.
	best := bankAllBody(t, h, "?xp=best")
	if best.XP.Combined.XP != 4000 {
		t.Errorf("100 logs on the best route = %d XP, want 4000 (burning)", best.XP.Combined.XP)
	}
	if len(best.XP.Combined.Lines) != 1 || best.XP.Combined.Lines[0].RouteKey != "burn_logs" {
		t.Errorf("the chosen route must be named, got %+v", best.XP.Combined.Lines)
	}
}

// TestBankAllBankedXPGatesOnLevels covers the two honest answers the level gate has to give:
// nothing at all when the account cannot perform the route, and a reduced figure when it can
// but content's own roll says it will sometimes fail.
func TestBankAllBankedXPGatesOnLevels(t *testing.T) {
	low := AccountStatus{Name: "low", Skills: map[string]SkillCounter{"Cooking": {Level: 1}}}
	high := AccountStatus{Name: "high", Skills: map[string]SkillCounter{"Cooking": {Level: 99}}}
	banks := []AccountBank{
		{Account: "low", Items: []Slot{{ID: rawLobsterID, Name: "Raw lobster", Count: 1000}}},
		{Account: "high", Items: []Slot{{ID: rawLobsterID, Name: "Raw lobster", Count: 1000}}},
	}
	h := newBankHarness(t, nil, []AccountStatus{low, high}, banks)

	open := bankAllBody(t, h, "")
	if open.XP.Combined.XP != 2000*120 {
		t.Fatalf("ungated = %d XP, want 240000 (2,000 lobsters at 120)", open.XP.Combined.XP)
	}

	gated := bankAllBody(t, h, "?levels=current")
	byName := map[string]bankXPAccount{}
	for _, a := range gated.XP.Accounts {
		byName[a.Account] = a
	}
	if byName["low"].XP != 0 || byName["low"].LevelBlocked != 1000 {
		t.Errorf("a level-1 cook banks no lobster XP: %+v", byName["low"])
	}
	if byName["high"].XP <= 0 {
		t.Errorf("a level-99 cook should bank a real figure: %+v", byName["high"])
	}
	// The combined pool is gated at the BEST level in the fleet, and says which that was.
	if gated.XP.CombinedLevels["Cooking"] != 99 {
		t.Errorf("combined levels = %v, want Cooking 99 (the best-placed account)", gated.XP.CombinedLevels)
	}
	if gated.XP.Combined.XP > open.XP.Combined.XP {
		t.Error("gating must never RAISE the figure")
	}
	// They are equal here, and that is correct rather than a missing gate: content's own roll
	// for a lobster is successchance 38,332, and the engine's formula
	// (floor(38*(99-99)/98) + floor(332*98/98) + 1 = 333) exceeds the 256-sided roll, so a
	// level-99 cook never burns one. Asserting equality pins that clamp.
	if gated.XP.Combined.XP != open.XP.Combined.XP {
		t.Errorf("a level-99 cook never burns a lobster, so the gated figure should equal the ungated one: %d vs %d", gated.XP.Combined.XP, open.XP.Combined.XP)
	}
}

// TestBankAllLevelsUnknownIsNotZero: an account the fleet reports no skills for has to come
// back flagged, not silently valued at nothing.
func TestBankAllLevelsUnknownIsNotZero(t *testing.T) {
	h := newBankHarness(t, nil,
		[]AccountStatus{{Name: "quiet"}},
		[]AccountBank{{Account: "quiet", Items: []Slot{{ID: rawLobsterID, Name: "Raw lobster", Count: 10}}}})
	body := bankAllBody(t, h, "?levels=current")
	if len(body.XP.Accounts) != 1 {
		t.Fatalf("want one account, got %d", len(body.XP.Accounts))
	}
	if body.XP.Accounts[0].LevelsKnown {
		t.Error("no skills were reported for this account; LevelsKnown must be false")
	}
	if body.XP.Accounts[0].XP != 0 {
		t.Errorf("with no levels known nothing can be claimed, got %d", body.XP.Accounts[0].XP)
	}
}

func TestBankAllRateIsClampedAndApplied(t *testing.T) {
	h := newBankHarness(t, nil,
		[]AccountStatus{{Name: "a"}},
		[]AccountBank{{Account: "a", Items: []Slot{{ID: logsID, Name: "Logs", Count: 100}}}})
	for _, c := range []struct {
		query string
		want  float64
		xp    int64
	}{
		{"", 1, 750},
		{"?rate=5", 5, 3750},
		{"?rate=0", 1, 750},
		{"?rate=-3", 1, 750},
		{"?rate=abc", 1, 750},
		{"?rate=1000", 100, 75000},
		{"?rate=NaN", 1, 750},
		{"?rate=Inf", 100, 75000},
	} {
		body := bankAllBody(t, h, c.query)
		if body.XP.Rate != c.want {
			t.Errorf("%q: rate %v, want %v", c.query, body.XP.Rate, c.want)
		}
		if body.XP.Combined.XP != c.xp {
			t.Errorf("%q: %d XP, want %d", c.query, body.XP.Combined.XP, c.xp)
		}
	}
}

func TestItemRoutesEndpointServesTheGeneratedTable(t *testing.T) {
	h := newBankHarness(t, nil, []AccountStatus{{Name: "a"}}, nil)
	res := h.get("/api/item-routes")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("/api/item-routes = %d", res.StatusCode)
	}
	defer res.Body.Close()
	var body itemRoutesBody
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(body.Routes) != len(bankxp.Routes) {
		t.Errorf("served %d routes, the table holds %d", len(body.Routes), len(bankxp.Routes))
	}
	if body.XPScale != 10 {
		t.Errorf("XPScale = %d; XP10 is tenths of a point and the API has to say so", body.XPScale)
	}
	if body.Cache {
		t.Error("this harness has no config archive, so the table cannot have been checked against one")
	}
	if body.Mismatches != 0 {
		t.Errorf("with no cache to check against, nothing can be a mismatch: got %d", body.Mismatches)
	}
	if len(body.Skills) == 0 || len(body.Scripts) == 0 {
		t.Errorf("the legend needs both lists: skills=%v scripts=%v", body.Skills, body.Scripts)
	}
}
