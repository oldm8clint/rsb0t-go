package bot

// MarketBulkBuy is one-shot per runId, across reconnects and restarts (review of claude/bulkbuy,
// blocker 2): the fleet restarts a running script by itself after a disconnect, and the control
// panel re-applies a saved assignment on every restart, so "the bot idles after DONE" alone would
// buy the whole quantity again. A real buy needs a runId, and the runId is written to disk BEFORE
// the first coin moves: a run that died halfway is not resumed or repeated automatically -- the
// operator reads the ledger and fires a new runId. Logging out for good (RequestFinish) is not the
// answer, because the account is the fleet's only mule.

import (
	"fmt"
	"sync"
	"time"

	"rsb0t-go/internal/settings"
)

const (
	marketBulkRunsAccount = "_fleet"
	marketBulkRunsScript  = "MarketBulkRuns"
)

// marketBulkRunsState is the persisted set of runIds already started, with when.
type marketBulkRunsState struct {
	Runs map[string]string `json:"runs"`
}

var marketBulkRunsMu sync.Mutex

// claimMarketBulkRun records runID as started, or reports when it already was. An unreadable
// file refuses rather than risk a second run.
func claimMarketBulkRun(store *settings.Store, runID string, now time.Time) (already string, err error) {
	marketBulkRunsMu.Lock()
	defer marketBulkRunsMu.Unlock()
	bag, err := store.Load(marketBulkRunsAccount, marketBulkRunsScript)
	if err != nil {
		return "", fmt.Errorf("the run record could not be read: %w", err)
	}
	var st marketBulkRunsState
	if err := bag.Decode(&st); err != nil {
		return "", fmt.Errorf("the run record could not be decoded: %w", err)
	}
	if at, ok := st.Runs[runID]; ok {
		return at, nil
	}
	if st.Runs == nil {
		st.Runs = map[string]string{}
	}
	st.Runs[runID] = now.UTC().Format(time.RFC3339)
	out := settings.NewBag()
	if err := out.Encode(st); err != nil {
		return "", err
	}
	if err := store.Save(marketBulkRunsAccount, marketBulkRunsScript, out); err != nil {
		return "", fmt.Errorf("the run record could not be written: %w", err)
	}
	return "", nil
}
