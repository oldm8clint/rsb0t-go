package runerunner

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/runerunnerplan"
)

// LIVE 12 October 2026: 13 of the runners' 28 coin asks (8,146,426 of 17,642,374 coins) never reached
// the mule. donorrunner.Refusals replays one account's "none in the bank" to every account for
// thirty minutes, and every real decline (the second order of an ask that had just emptied the mule)
// re-armed it for the whole fleet, while the mule took in 774,000 coins an hour. A coin ask asks
// anyway, and keeps what the mule says in a memory of its own.
func TestCoinAskIgnoresAndDoesNotFeedTheFleetsRefusals(t *testing.T) {
	const mule = "coinmule-topup-test"
	donorrunner.Refusals.Record(time.Now(), mule, CoinsName, "none in the bank", donorplan.RefusalCooldown)
	defer donorrunner.Refusals.Clear(mule, CoinsName)

	own := donorplan.NewRefusalMemory()
	opts := CoinAwaitOptions(mule, "acct73", 500000, mulebus.NewBus(), own)
	if !opts.AskAnyway || opts.Memory != own || opts.Memory == donorrunner.Refusals {
		t.Fatalf("options %+v: want AskAnyway and the ask's own memory", opts)
	}
	if opts.Item != CoinsName || opts.Qty != 500000 || opts.Mule != mule || opts.Me != "acct73" || opts.Bus == nil ||
		opts.Timeout != runerunnerplan.TopUpAskTimeout {
		t.Errorf("options %+v", opts)
	}

	// The shared memory says the mule has no coins; the ask goes on past that check. The empty Deps
	// have no object config, so it stops at the config lookup: not Declined, not the replayed answer.
	before := donorrunner.Refusals.Count(time.Now())
	res := donorrunner.Await(&donorrunner.Deps{}, opts)
	if res.Declined || !strings.Contains(res.Reason, "cannot identify") {
		t.Fatalf("the shared refusal was replayed to a coin ask: %+v", res)
	}
	if got := donorrunner.Refusals.Count(time.Now()); got != before {
		t.Errorf("the fleet's refusals went from %d to %d", before, got)
	}

	// Every other asker still gets the replay: the memory is intact for them.
	other := donorrunner.Await(&donorrunner.Deps{}, donorrunner.AwaitOptions{Mule: mule, Me: "acct9", Item: CoinsName, Qty: 100, Bus: mulebus.NewBus()})
	if !other.Declined || other.Reason != "none in the bank" {
		t.Errorf("another account's ask: %+v", other)
	}
}
