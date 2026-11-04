package fleetplan

// The fleet-wide budget for runes bought from player markets (the Seers Market's chaos, chiefly),
// and the ledger that enforces it.
//
// WHY. The owner's ruling (COLLAB, "OWNER DECISIONS for ScarabSwarm and its supply", 2026-09-23):
// "Chaos from the Seers Market: up to 5,000,000 coins fleet-wide ... Log every purchase; enforce
// the cap fleet-wide, not per account." Round 1 of runesupply capped only the unit price (150 gp)
// and one purchase's size (150 chaos), so a cohort of 100 accounts, each re-buying whenever its
// casting stock ran low, had no ceiling at all: 100 x 150 x 150 gp is 2.25M a round.
//
// The ledger is one per fleet process (every account the cap covers lives in it), persisted by
// the bot (internal/bot/scarabswarm_marketledger.go) so a restart cannot reset what was spent.
// An account RESERVES its purchase's worst case before it trades and COMMITS what it actually
// paid afterwards, so two accounts at two stalls cannot both spend the last million.
//
// Pure in this package's sense: no client and no I/O; every time is passed in.

import (
	"strings"
	"sync"
	"time"
)

// MarketLedgerKeep is how many recent purchases the persisted ledger keeps for the record.
const MarketLedgerKeep = 200

// MarketLedger is safe for concurrent use by every session goroutine. Build it with
// NewMarketLedger.
type MarketLedger struct {
	mu    sync.Mutex
	st    MarketLedgerState
	holds map[string]marketHold
}

type marketHold struct {
	coins int
	until time.Time
}

// NewMarketLedger starts a ledger from a persisted state (the zero state for a first run).
func NewMarketLedger(st MarketLedgerState) *MarketLedger {
	if st.Spent < 0 {
		st.Spent = 0
	}
	return &MarketLedger{st: st, holds: map[string]marketHold{}}
}

func ledgerKey(account string) string { return strings.ToLower(strings.TrimSpace(account)) }

// heldByOthersLocked is every live reservation but account's own.
func (l *MarketLedger) heldByOthersLocked(account string, now time.Time) int {
	k, n := ledgerKey(account), 0
	for who, h := range l.holds {
		if !now.Before(h.until) {
			delete(l.holds, who)
			continue
		}
		if who != k {
			n += h.coins
		}
	}
	return n
}

// Remaining is what account may still spend under budget: the budget less everything committed
// and every OTHER account's live reservation. A budget of 0 or less means no market purchases.
func (l *MarketLedger) Remaining(budget int, account string, now time.Time) int {
	if l == nil || budget <= 0 {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	left := budget - l.st.Spent - l.heldByOthersLocked(account, now)
	if left < 0 {
		return 0
	}
	return left
}

// Reserve holds up to coins of the budget for account's next purchase and returns what it was
// granted (all of coins, part of it, or 0). A second Reserve by the same account replaces its
// hold rather than adding to it.
func (l *MarketLedger) Reserve(account string, coins, budget int, now time.Time, ttl time.Duration) int {
	if l == nil || coins <= 0 || budget <= 0 {
		return 0
	}
	if ttl <= 0 {
		ttl = MarketHoldTTL
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	left := budget - l.st.Spent - l.heldByOthersLocked(account, now)
	if left <= 0 {
		delete(l.holds, ledgerKey(account))
		return 0
	}
	if coins > left {
		coins = left
	}
	l.holds[ledgerKey(account)] = marketHold{coins: coins, until: now.Add(ttl)}
	return coins
}

// Release drops account's reservation without spending anything (a purchase that did not happen).
func (l *MarketLedger) Release(account string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	delete(l.holds, ledgerKey(account))
	l.mu.Unlock()
}

// Commit counts a purchase that happened: its coins are added to what the fleet has spent, the
// account's reservation is released, and the purchase is recorded. It returns the state to
// persist and the purchase as recorded (with SpentAfter filled in). A purchase with nothing paid
// only releases the hold.
func (l *MarketLedger) Commit(p MarketPurchase) (MarketLedgerState, MarketPurchase) {
	if l == nil {
		return MarketLedgerState{}, p
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.holds, ledgerKey(p.Account))
	if p.Paid > 0 {
		l.st.Spent += p.Paid
		l.st.Buys++
		p.SpentAfter = l.st.Spent
		l.st.Recent = append(l.st.Recent, p)
		if over := len(l.st.Recent) - MarketLedgerKeep; over > 0 {
			l.st.Recent = append([]MarketPurchase(nil), l.st.Recent[over:]...)
		}
	}
	return l.snapshotLocked(), p
}

// State is a copy of the ledger's persisted part.
func (l *MarketLedger) State() MarketLedgerState {
	if l == nil {
		return MarketLedgerState{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.snapshotLocked()
}

func (l *MarketLedger) snapshotLocked() MarketLedgerState {
	out := l.st
	out.Recent = append([]MarketPurchase(nil), l.st.Recent...)
	return out
}
