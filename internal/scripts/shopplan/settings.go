package shopplan

import "strings"

// Settings are ShopBuyout's settings. The JSON keys mirror the TS manifest's own
// SHOPBUYOUT_SETTINGS (shop, budgetGp, perTripGp, stopFloorGp, buyItems, recheckSeconds) plus
// the bank-booth and verbosity conveniences every other ported script in this codebase carries.
type Settings struct {
	// Shop is a preset label from ShopLabels(); blank falls back to the first preset.
	Shop string `json:"shop"`
	// BuyItems restricts the run to these stock names. Empty means every item the chosen
	// shop stocks, which is the TS default and the sense of "buy it out".
	BuyItems []string `json:"buyItems"`
	// BudgetGp is the whole session's spend; the run stops cleanly once it is gone.
	BudgetGp int32 `json:"budgetGp"`
	// PerTripGp is how many coins one bank trip draws.
	PerTripGp int32 `json:"perTripGp"`
	// StopFloorGp stops the run when the BANK's coins fall below it.
	StopFloorGp int32 `json:"stopFloorGp"`
	// RecheckSeconds is how long a pass waits at the keeper when the chosen stock is drained,
	// before looking again.
	RecheckSeconds int `json:"recheckSeconds"`
	// BankBooth is the bank loc's display name; blank falls back to the preset's.
	BankBooth string `json:"bankBooth"`
	LogDetail string `json:"logDetail"`
}

// Defaults, matching the TS manifest's own numbers.
const (
	DefaultBudgetGp       int32 = 250000
	DefaultPerTripGp      int32 = 100000
	DefaultStopFloorGp    int32 = 5000
	DefaultRecheckSeconds       = 60
)

// DefaultSettings are the manifest defaults.
func DefaultSettings() Settings {
	return Settings{
		Shop:           Shops[0].Label,
		BuyItems:       nil,
		BudgetGp:       DefaultBudgetGp,
		PerTripGp:      DefaultPerTripGp,
		StopFloorGp:    DefaultStopFloorGp,
		RecheckSeconds: DefaultRecheckSeconds,
		BankBooth:      BoothName,
		LogDetail:      "Normal",
	}
}

// Normalised resolves the shop label, drops item names the chosen shop does not stock, and
// applies the blank-falls-back-to-default rule every sibling script's Settings uses.
//
// An item name that names nothing in THIS shop is dropped rather than kept, since nothing
// downstream could act on it — but silently dropping every name would turn a typo into "buy
// the whole shop out", a much more expensive run than the operator asked for. Callers should
// report DroppedItems before starting.
func (s Settings) Normalised() Settings {
	shop, ok := ShopByLabel(s.Shop)
	if !ok {
		shop = Shops[0]
	}
	s.Shop = shop.Label

	out := make([]string, 0, len(s.BuyItems))
	seen := map[int32]bool{}
	for _, name := range s.BuyItems {
		it, ok := shop.ItemByName(name)
		if !ok || seen[it.ID] {
			continue
		}
		seen[it.ID] = true
		out = append(out, it.Name)
	}
	s.BuyItems = out

	if s.BudgetGp <= 0 {
		s.BudgetGp = DefaultBudgetGp
	}
	if s.PerTripGp <= 0 {
		s.PerTripGp = DefaultPerTripGp
	}
	if s.StopFloorGp < 0 {
		s.StopFloorGp = DefaultStopFloorGp
	}
	if s.RecheckSeconds <= 0 {
		s.RecheckSeconds = DefaultRecheckSeconds
	}
	s.BankBooth = strings.TrimSpace(s.BankBooth)
	if s.BankBooth == "" {
		s.BankBooth = shop.BoothName
	}
	return s
}

// ItemByName finds a stock row by display name within one shop, matching ignoring case. Safe
// here in a way a global name lookup would not be: a name is only ambiguous across the whole
// object table, and no single shop stocks two rows sharing one display name. Anything past
// this point uses the id.
func (s Shop) ItemByName(name string) (Item, bool) {
	want := strings.TrimSpace(name)
	for _, it := range s.Items {
		if strings.EqualFold(it.Name, want) {
			return it, true
		}
	}
	return Item{}, false
}

// DroppedItems lists the raw buyItems entries that name nothing in shop, so a run can warn
// about a typo instead of quietly widening itself to the whole shop.
func DroppedItems(shop Shop, raw []string) []string {
	var dropped []string
	for _, name := range raw {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if _, ok := shop.ItemByName(trimmed); !ok {
			dropped = append(dropped, trimmed)
		}
	}
	return dropped
}

// Chosen is the lowercased item-name set BuyoutPlan and MinUnitPrice take. Empty means every
// item in the shop.
func (s Settings) Chosen() map[string]bool {
	chosen := map[string]bool{}
	for _, name := range s.BuyItems {
		chosen[strings.ToLower(name)] = true
	}
	return chosen
}

// Verbose reports the Verbose log detail.
func (s Settings) Verbose() bool {
	return strings.EqualFold(strings.TrimSpace(s.LogDetail), "verbose")
}
