package bot

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/marketbulkplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/settings"
)

func TestMarketBulkIsRegistered(t *testing.T) {
	if _, ok := New("MarketBulkBuy"); !ok {
		t.Fatal("MarketBulkBuy is not registered")
	}
}

func TestMarketBulkSettingsReadTheOwnersTrade(t *testing.T) {
	s := marketBulkSettings(map[string]interface{}{"quantity": 52450.0, "maxUnitPrice": 95.0, "post": "2809,3441", "bank": "2725,3493"})
	if s.Quantity != 52450 || s.MaxUnitPrice != 95 || s.Unit() != 95 || s.Budget != fleetplan.DefaultMarketBudget ||
		s.Item != "Chaos rune" || s.Seller != "Seers Market" || s.TradeCoinLimit != 5_000_000 ||
		s.Post != (tile.Tile{X: 2809, Z: 3441}) || s.Bank != (tile.Tile{X: 2725, Z: 3493}) {
		t.Fatalf("%+v", s)
	}
}

// startBulk runs OnStart for settings and returns the refusal (or "" when it would go on to walk).
func startBulk(t *testing.T, set map[string]interface{}) string {
	t.Helper()
	b := &marketBulkBot{}
	var lines []string
	ctx := NewContext(nil, "mule", nil, nil, nil, nil, nil, nil, nil, set, func(l string) { lines = append(lines, l) })
	defer func() {
		// A request that passes every refusal reaches the walker, which needs a session; the
		// refusals below never get that far, which is the point.
		if r := recover(); r != nil && b.done == "" {
			b.done = "(passed the refusals)"
		}
	}()
	if err := b.OnStart(ctx); err != nil {
		t.Fatal(err)
	}
	return b.done
}

func TestMarketBulkRefusesWithoutAnExplicitQuantityOrCap(t *testing.T) {
	dir := t.TempDir()
	if r := startBulk(t, map[string]interface{}{"maxUnitPrice": 95.0, "checkpointDir": dir}); !strings.Contains(r, "REFUSED") || !strings.Contains(r, "quantity") {
		t.Fatalf("no quantity: %q", r)
	}
	if r := startBulk(t, map[string]interface{}{"quantity": 100.0, "checkpointDir": dir}); !strings.Contains(r, "REFUSED") || !strings.Contains(r, "maxUnitPrice") {
		t.Fatalf("no cap: %q", r)
	}
	if r := startBulk(t, map[string]interface{}{"quantity": 100.0, "maxUnitPrice": 95.0, "unitPrice": 100.0, "checkpointDir": dir}); !strings.Contains(r, "REFUSED") || !strings.Contains(r, "above") {
		t.Fatalf("unit over cap: %q", r)
	}
}

func saveLedger(t *testing.T, dir string, spent int) {
	t.Helper()
	bag := settings.NewBag()
	if err := bag.Encode(fleetplan.MarketLedgerState{Spent: spent, Buys: 9}); err != nil {
		t.Fatal(err)
	}
	if err := settings.NewStore(dir).Save(scarabMarketLedgerAccount, scarabMarketLedgerStore, bag); err != nil {
		t.Fatal(err)
	}
}

func TestMarketBulkRefusesWhenTheFleetBudgetIsSpent(t *testing.T) {
	dir := t.TempDir()
	// What the live ledger read before the bulk buy: 9 buys, 14,175 coins. The owner's full
	// trade (4,982,750) still fits in 5M; one coin more of prior spend and it would not.
	saveLedger(t, dir, 5_000_000-4_982_750+1)
	r := startBulk(t, map[string]interface{}{"quantity": 52450.0, "maxUnitPrice": 95.0, "checkpointDir": dir, "runId": "full-1"})
	if !strings.Contains(r, "REFUSED") || !strings.Contains(r, "budget") {
		t.Fatalf("spent budget: %q", r)
	}
}

func TestMarketBulkPassesTheRefusalsWithRoomInTheBudget(t *testing.T) {
	dir := t.TempDir()
	saveLedger(t, dir, 14_175)
	r := startBulk(t, map[string]interface{}{"quantity": 52450.0, "maxUnitPrice": 95.0, "checkpointDir": dir, "runId": "full-1"})
	if strings.Contains(r, "REFUSED") {
		t.Fatalf("a request that fits was refused: %q", r)
	}
}

func TestMarketBulkQuoteOnlyNeedsNoCapOrLedger(t *testing.T) {
	r := startBulk(t, map[string]interface{}{"quantity": 100.0, "quoteOnly": true, "checkpointDir": t.TempDir()})
	if strings.Contains(r, "REFUSED") {
		t.Fatalf("quote-only refused: %q", r)
	}
}

func TestMarketBulkUnreadableLedgerRefuses(t *testing.T) {
	dir := t.TempDir()
	bag := settings.NewBag()
	_ = bag.Encode(map[string]interface{}{"spent": "lots"})
	if err := settings.NewStore(dir).Save(scarabMarketLedgerAccount, scarabMarketLedgerStore, bag); err != nil {
		t.Fatal(err)
	}
	r := startBulk(t, map[string]interface{}{"quantity": 100.0, "maxUnitPrice": 95.0, "checkpointDir": dir, "runId": "r1"})
	if !strings.Contains(r, "REFUSED") || !strings.Contains(r, "ledger") {
		t.Fatalf("unreadable ledger: %q", r)
	}
}

// ONE-SHOT (review blocker 2): a reconnect or a panel restart re-runs OnStart with the same
// settings, and must never buy again. A real buy needs a runId; the second start of the same
// runId refuses; a request refused for another reason does not use its runId up.
func TestMarketBulkRunsOncePerRunID(t *testing.T) {
	dir := t.TempDir()
	saveLedger(t, dir, 14_175)
	set := func() map[string]interface{} {
		return map[string]interface{}{"quantity": 100.0, "maxUnitPrice": 95.0, "checkpointDir": dir}
	}
	if r := startBulk(t, set()); !strings.Contains(r, "REFUSED") || !strings.Contains(r, "runId") {
		t.Fatalf("no runId: %q", r)
	}
	over := set()
	over["runId"], over["unitPrice"] = "hundred-1", 100.0
	if r := startBulk(t, over); !strings.Contains(r, "REFUSED") || !strings.Contains(r, "above") {
		t.Fatalf("over the cap: %q", r)
	}
	first := set()
	first["runId"] = "hundred-1"
	if r := startBulk(t, first); strings.Contains(r, "REFUSED") {
		t.Fatalf("the first start of a runId a refusal did not use up was refused: %q", r)
	}
	again := set()
	again["runId"] = "hundred-1"
	if r := startBulk(t, again); !strings.Contains(r, "REFUSED") || !strings.Contains(r, "already started") {
		t.Fatalf("the same runId started twice: %q", r)
	}
	other := set()
	other["runId"] = "hundred-2"
	if r := startBulk(t, other); strings.Contains(r, "REFUSED") {
		t.Fatalf("a new runId was refused: %q", r)
	}
}

// quoteOnly "true" (a string) used to read as false through BoolSetting and turn a quote into a
// real buy; anything but a JSON boolean now refuses.
func TestMarketBulkQuoteOnlyMustBeABoolean(t *testing.T) {
	r := startBulk(t, map[string]interface{}{"quantity": 52450.0, "maxUnitPrice": 95.0, "quoteOnly": "true", "runId": "x", "checkpointDir": t.TempDir()})
	if !strings.Contains(r, "REFUSED") || !strings.Contains(r, "quoteOnly") {
		t.Fatalf(`quoteOnly "true": %q`, r)
	}
}

var _ = marketbulkplan.DefaultItem
