package fleet

import (
	"sort"
	"sync"
	"time"

	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/muleledger"
)

// lootBook keeps one loot.Tally per account for this process's lifetime: every Drop a running
// script reports through bot.Context.Loot (see StartBot), across script restarts and
// reconnects. It is deliberately NOT part of Snapshot/AccountStatus: the dashboard polls
// Snapshot every second and loot changes a few times a minute, so the control panel's stats
// aggregator reads LootTotals on its own slow cadence instead and persists lifetime totals
// itself (internal/controlpanel/lootstats.go). A restart starts every tally at zero, which is
// what that aggregator expects: it diffs against the last totals it saw in this process.
type lootBook struct {
	mu sync.Mutex
	by map[string]*loot.Tally
	// ledger is the mule trade record (SetMuleLedger); nil records nothing.
	ledger *muleledger.Ledger
}

func (b *lootBook) tally(username string) *loot.Tally {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.by == nil {
		b.by = map[string]*loot.Tally{}
	}
	t := b.by[username]
	if t == nil {
		t = loot.NewTally(nil)
		b.by[username] = t
	}
	return t
}

// lootRecorder is the bot.Context.Loot hook for one account. It logs nothing: a pickpocketing
// fleet reports a drop every few seconds per account, and the scripts already log what they took.
func (m *Manager) lootRecorder(username string) func(loot.Drop) {
	return m.loot.tally(username).Add
}

// AccountLoot is one account's loot totals, per source, since this process started.
type AccountLoot struct {
	Account string        `json:"account"`
	Epoch   int64         `json:"epoch"`
	Sources []loot.Totals `json:"sources"`
}

// LootTotals returns every account that has recorded any loot, sorted by name. Each tally is
// copied under its own lock; an account whose Epoch equals the caller's last reading can be
// skipped by the caller.
func (m *Manager) LootTotals() []AccountLoot {
	m.loot.mu.Lock()
	names := make([]string, 0, len(m.loot.by))
	tallies := make(map[string]*loot.Tally, len(m.loot.by))
	for name, t := range m.loot.by {
		names = append(names, name)
		tallies[name] = t
	}
	m.loot.mu.Unlock()
	sort.Strings(names)
	out := make([]AccountLoot, 0, len(names))
	for _, name := range names {
		t := tallies[name]
		epoch := t.Epoch()
		if epoch == 0 {
			continue
		}
		out = append(out, AccountLoot{Account: name, Epoch: epoch, Sources: t.Snapshot()})
	}
	return out
}

// SetMuleLedger attaches the persisted mule trade record every MuleReceiver writes its finished
// trades to (bot.Context.TradeLog). Call it before starting bots; nil detaches it.
func (m *Manager) SetMuleLedger(l *muleledger.Ledger) {
	m.loot.mu.Lock()
	m.loot.ledger = l
	m.loot.mu.Unlock()
}

// MuleLedger is the attached trade record, nil when none is.
func (m *Manager) MuleLedger() *muleledger.Ledger {
	m.loot.mu.Lock()
	defer m.loot.mu.Unlock()
	return m.loot.ledger
}

// tradeRecorder is the bot.Context.TradeLog hook: it writes to whatever ledger is attached at
// the time of the trade.
func (m *Manager) tradeRecorder() func(muleledger.Entry) {
	return func(e muleledger.Entry) { m.MuleLedger().Record(e) }
}

// muleOutReader is the bot.Context.MuleOut hook: what a mule has handed out since a time, read from
// whatever ledger is attached when it is asked (muleledger.Ledger.OutSince). With none attached it
// answers nothing and no horizon, which a reader takes as "not known" (MasterCrafter's storage round
// then moves nothing).
func (m *Manager) muleOutReader() func(mule string, since time.Time) (map[string]int64, time.Time) {
	return func(mule string, since time.Time) (map[string]int64, time.Time) {
		return m.MuleLedger().OutSince(mule, since)
	}
}
