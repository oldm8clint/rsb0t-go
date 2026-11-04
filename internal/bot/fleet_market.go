package bot

import (
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/settings"
)

// scarabMarketLedgerAccount/Store are where the ledger is saved in the checkpoint Store: a
// directory no account can own ("_fleet": RuneScape names cannot start with an underscore).
const (
	scarabMarketLedgerAccount = "_fleet"
	scarabMarketLedgerStore   = "ScarabMarketLedger"
)

var (
	scarabLedgersMu sync.Mutex
	scarabLedgers   = map[string]*scarabMarketLedger{}
)

// scarabMarketLedger is a ledger and the store it persists to.
type scarabMarketLedger struct {
	*fleetplan.MarketLedger
	store *settings.Store
	// saveMu keeps two commits' saves in order.
	saveMu sync.Mutex
	// loadErr is why the saved state could not be read. A ledger that could not read what was
	// spent refuses to spend (Remaining 0) rather than starting again from nothing.
	loadErr error
}

// scarabLedgerFor is the process's ledger for a checkpoint store, loaded on first use.
func scarabLedgerFor(store *settings.Store) *scarabMarketLedger {
	if store == nil {
		return nil
	}
	key := strings.ToLower(store.Dir)
	scarabLedgersMu.Lock()
	defer scarabLedgersMu.Unlock()
	if l, ok := scarabLedgers[key]; ok {
		return l
	}
	var st fleetplan.MarketLedgerState
	bag, err := store.Load(scarabMarketLedgerAccount, scarabMarketLedgerStore)
	if err == nil {
		err = bag.Decode(&st)
	}
	l := &scarabMarketLedger{MarketLedger: fleetplan.NewMarketLedger(st), store: store, loadErr: err}
	scarabLedgers[key] = l
	return l
}

// save writes the ledger's state.
func (l *scarabMarketLedger) save(st fleetplan.MarketLedgerState) error {
	l.saveMu.Lock()
	defer l.saveMu.Unlock()
	bag := settings.NewBag()
	if err := bag.Encode(st); err != nil {
		return err
	}
	return l.store.Save(scarabMarketLedgerAccount, scarabMarketLedgerStore, bag)
}

// questMarketRest keeps a stall that failed an account's purchase out of that account's choice
// for a while, so a provisioning round that retries every minute does not re-ask a stall that
// just refused (its "no essence" is also remembered fleet-wide by woodcutrunner's sold-out note).
const questMarketRest = 20 * time.Minute

var questMarketFailed = struct {
	sync.Mutex
	at map[string]time.Time // account + "\x00" + market
}{at: map[string]time.Time{}}

func questMarketKey(account, market string) string {
	return strings.ToLower(account) + "\x00" + strings.ToLower(market)
}

// questMarketResting is the stalls this account should not ask now.
func questMarketResting(account string, markets []string, now time.Time) map[string]bool {
	questMarketFailed.Lock()
	defer questMarketFailed.Unlock()
	out := map[string]bool{}
	for _, m := range markets {
		if at, ok := questMarketFailed.at[questMarketKey(account, m)]; ok && now.Sub(at) < questMarketRest {
			out[m] = true
		}
	}
	return out
}

func questMarketMarkFailed(account, market string, now time.Time) {
	questMarketFailed.Lock()
	questMarketFailed.at[questMarketKey(account, market)] = now
	questMarketFailed.Unlock()
}

// questMarketStocks turns the price page's rows into questplan's shape.
func questMarketStocks(rows []fleetplan.MarketListing) []questplan.MarketStock {
	out := make([]questplan.MarketStock, 0, len(rows))
	for _, r := range rows {
		out = append(out, questplan.MarketStock{Market: r.Market, Item: r.Item, Stock: r.Stock, LastSold: r.LastSold})
	}
	return out
}

// marketBusyRetries bounds how long an account that has walked to the stall with the coins waits
// for another account's turn to end (woodcutrunner's own turn wait is 3 minutes per attempt for a
// buyer with priority, which this is; axe upgrades yield to it -- see woodcutrunner/marketlease.go).
const marketBusyRetries = 4

// scarabSwarmMarketMaxAge bounds how often the public market page is fetched (shared cache).
const scarabSwarmMarketMaxAge = 15 * time.Minute
