package bot

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/peerbank"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/marketstockplan"
	"rsb0t-go/internal/scripts/runerunnerplan"
	"rsb0t-go/internal/scripts/tile"
)

// marketRig is a rotation runner at the mule's post with the market station at the cursor and the
// stalls' page and the trade book replaced by what the test says.
func marketRig(t *testing.T, name string, extra map[string]interface{}, rows []fleetplan.MarketListing) *rotRig {
	t.Helper()
	r := newRotRig(t, name, 41, 2809, 3441, extra)
	r.ctx.Peers = peerbank.New()
	r.b.marketRowsSeam = func() []fleetplan.MarketListing { return rows }
	r.b.marketWalkSeam = func(string, tile.Tile) bool { return false }
	r.b.marketBookSeam = func(item string) (marketstockplan.Book, bool) {
		if strings.EqualFold(item, "Nature rune") {
			return marketstockplan.Book{Sell: 600, SellTrades: 11, Coherent: true}, true
		}
		return marketstockplan.Book{}, false
	}
	r.b.newRound(r.ctx, runerunnerplan.AreaOf(fleetrunner.Here(r.b.deps(r.ctx))), false)
	i := runerunnerplan.Index(r.b.st.Ring, "market@41")
	if i < 0 {
		t.Fatalf("no market station in %s", runerunnerplan.PlanString(r.b.st.Ring, 0))
	}
	r.b.st.JumpTo(i)
	r.b.resumed = true
	return r
}

func (r *rotRig) runMarket() runerunnerplan.StationResult {
	s, _ := r.b.st.Current()
	r.b.runStation(r.ctx, r.b.deps(r.ctx), s, fleetrunner.Here(r.b.deps(r.ctx)))
	return r.b.st.Done["market@41"]
}

var s15Nature = []fleetplan.MarketListing{{Market: "S15", Item: "Nature rune", Stock: 10000, LastSold: 600}}

// The ring carries the market station right after the draw, and an operator can leave it out.
func TestRuneRunnerRoundCarriesTheMarketStation(t *testing.T) {
	r := marketRig(t, "marketbuyer1", nil, s15Nature)
	defer releaseBoards("marketbuyer1")
	plan := runerunnerplan.PlanString(r.b.st.Ring, 0)
	if !strings.HasPrefix(plan, "[deliver@41 draw@41 market@41 hop") {
		t.Fatalf("round %s", plan)
	}
	off := newRotRig(t, "marketbuyer2", 41, 2809, 3441, map[string]interface{}{"marketBuy": false})
	defer releaseBoards("marketbuyer2")
	off.b.newRound(off.ctx, runerunnerplan.AreaOf(fleetrunner.Here(off.b.deps(off.ctx))), false)
	if strings.Contains(runerunnerplan.PlanString(off.b.st.Ring, 0), "market") {
		t.Fatalf("marketBuy false still plans the station: %s", runerunnerplan.PlanString(off.b.st.Ring, 0))
	}
}

// The station costs nothing when there is nothing to buy: it logs why, finishes as a skip, and
// does not look again until marketCheckMinutes have passed.
func TestRuneRunnerMarketStationNoBuyAndRateLimit(t *testing.T) {
	r := marketRig(t, "marketbuyer3", nil, []fleetplan.MarketListing{
		{Market: "Sil Stuff", Item: "Nature rune", Stock: 13},
		{Market: "Constructor", Item: "Nature rune", Stock: 4},
	})
	defer releaseBoards("marketbuyer3")
	publishRuneMule(r, map[string]int{"Nature rune": 109, "Death rune": 0, "Fire rune": 0, "Chaos rune": 542})
	got := r.runMarket()
	if got.Outcome != runerunnerplan.OutcomeSkipped || !strings.Contains(got.Reason, "market:") {
		t.Fatalf("outcome %+v\n%s", got, r.log())
	}
	if !strings.Contains(r.log(), "runerunner: market: no buy:") || !strings.Contains(r.log(), "Nature rune: short 19891") {
		t.Fatalf("no reason in the log\n%s", r.log())
	}
	if r.b.st.LastMarketCheck.IsZero() {
		t.Fatal("the look was not recorded")
	}
	// The next round, a minute later, does not look again.
	r.b.st.Done = map[string]runerunnerplan.StationResult{}
	r.b.st.JumpTo(runerunnerplan.Index(r.b.st.Ring, "market@41"))
	if got := r.runMarket(); !strings.Contains(got.Reason, "looked at the market") {
		t.Fatalf("second look: %+v", got)
	}
	// After marketCheckMinutes it looks again.
	r.b.st.Done = map[string]runerunnerplan.StationResult{}
	r.b.st.JumpTo(runerunnerplan.Index(r.b.st.Ring, "market@41"))
	r.b.st.LastMarketCheck = time.Now().Add(-16 * time.Minute)
	if got := r.runMarket(); strings.Contains(got.Reason, "looked at the market") {
		t.Fatalf("a look 16 minutes later was refused: %+v", got)
	}
}

func TestRuneRunnerMarketStationSkips(t *testing.T) {
	// The mule already holds its target: nothing is short.
	r := marketRig(t, "marketbuyer4", nil, s15Nature)
	defer releaseBoards("marketbuyer4")
	publishRuneMule(r, map[string]int{"Nature rune": 25000, "Death rune": 7000, "Fire rune": 12000, "Chaos rune": 9000})
	if got := r.runMarket(); got.Outcome != runerunnerplan.OutcomeSkipped || !strings.Contains(r.log(), "the mule holds 25000") {
		t.Fatalf("%+v\n%s", got, r.log())
	}
	// No bank published by the mule: nothing can be said to be short.
	r = marketRig(t, "marketbuyer5", nil, s15Nature)
	if got := r.runMarket(); got.Outcome != runerunnerplan.OutcomeSkipped || !strings.Contains(r.log(), "the mule has published no bank") {
		t.Fatalf("%+v\n%s", got, r.log())
	}
	// No coins at the last draw.
	r = marketRig(t, "marketbuyer6", nil, s15Nature)
	publishRuneMule(r, map[string]int{"Nature rune": 109})
	r.b.noCoins = true
	if got := r.runMarket(); got.Outcome != runerunnerplan.OutcomeSkipped || got.Reason != "no coins" {
		t.Fatalf("%+v", got)
	}
	// The page could not be read.
	r = marketRig(t, "marketbuyer7", nil, nil)
	publishRuneMule(r, map[string]int{"Nature rune": 109})
	if got := r.runMarket(); got.Outcome != runerunnerplan.OutcomeSkipped || !strings.Contains(got.Reason, "could not be read") {
		t.Fatalf("%+v", got)
	}
	// A station planned for another world does nothing here.
	r = marketRig(t, "marketbuyer8", nil, s15Nature)
	other := runerunnerplan.Station{Kind: runerunnerplan.KindMarket, World: 42}
	r.b.marketStation(r.ctx, r.b.deps(r.ctx), other)
	if got := r.b.st.Done["market@41"]; !strings.Contains(got.Reason, "the station is for world 42") {
		t.Fatalf("%+v", got)
	}
}

// A lot worth the trip goes as far as the walk to Seers (the rig has no walker, so it fails
// there): the plan is logged with its prices, nothing is bought, and the station ends as a walk
// failure the round retries, not as a purchase.
func TestRuneRunnerMarketStationPlansTheTripAndAsksWithinTheBank(t *testing.T) {
	r := marketRig(t, "marketbuyer9", nil, s15Nature)
	defer releaseBoards("marketbuyer9")
	publishRuneMule(r, map[string]int{"Nature rune": 109, "Death rune": 6000, "Fire rune": 10000, "Chaos rune": 8000})
	r.b.st.BankCoins, r.b.st.BankReadAt = 500000, time.Now()
	// The shops' float is its own test (runerunner_purse_test.go): here they need nothing.
	r.b.roundNeedSeam = func(int) runerunnerplan.RoundNeed { return runerunnerplan.RoundNeed{} }
	got := r.runMarket()
	log := r.log()
	if !strings.Contains(log, "market: worth a trip: 1333 Nature rune from S15 at 600 (cap 750, ceiling 900)") {
		t.Fatalf("no trip line\n%s", log)
	}
	// The bank holds 500,000 less the 10,000 reserve: the second plan buys what that pays.
	if r.b.marketSpendable() != 490000 {
		t.Fatalf("spendable %d", r.b.marketSpendable())
	}
	if got.Outcome != runerunnerplan.OutcomeWalkFailed || !strings.Contains(got.Reason, "could not reach the Seers bank") {
		t.Fatalf("the seam's failed walk should end the station as a walk failure the round retries: %+v\n%s", got, log)
	}
}

func TestRuneMarketHelpers(t *testing.T) {
	rows := marketStalls([]fleetplan.MarketListing{{Market: "S15", Item: "Nature rune", Stock: 5, LastSold: 600, LastBought: 280}})
	if len(rows) != 1 || rows[0].Ask != 600 || rows[0].Stock != 5 {
		t.Fatalf("%+v", rows)
	}
	now := time.Now()
	runeMarketRest("Zed Stall", "Nature rune", time.Minute, now)
	if !runeMarketResting(now)[marketstockplan.RestKey("zed stall", "nature rune")] {
		t.Fatal("not resting")
	}
	if runeMarketResting(now.Add(2 * time.Minute))[marketstockplan.RestKey("zed stall", "nature rune")] {
		t.Fatal("still resting after its time")
	}
	if got := describeTrades([]marketstockplan.Trade{{Market: "S15", Item: "Nature rune", Qty: 5, Unit: 600, UnitCap: 750, Ceiling: 900}}); got != "5 Nature rune from S15 at 600 (cap 750, ceiling 900)" {
		t.Fatal(got)
	}
	r := newRotRig(t, "marketbuyer10", 41, 2809, 3441, map[string]interface{}{"marketCoins": 200000.0, "coinReserve": 5000.0})
	defer releaseBoards("marketbuyer10")
	r.b.st.BankCoins = 1000000
	if r.b.marketSpendable() != 200000 {
		t.Fatalf("capped at marketCoins: %d", r.b.marketSpendable())
	}
	r.b.st.BankCoins = 3000
	if r.b.marketSpendable() != 0 {
		t.Fatalf("under the reserve: %d", r.b.marketSpendable())
	}
}
