package fleet

import (
	"testing"
	"time"

	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/muleledger"
)

// TestLootRecorderTalliesPerAccount proves the bot.Context.Loot hook StartBot installs lands in
// that account's own tally, and that LootTotals leaves out accounts that never looted.
func TestLootRecorderTalliesPerAccount(t *testing.T) {
	m := New(Config{})
	casket := loot.Openable("Casket", 405)
	rec := m.lootRecorder("fisher1")
	rec(loot.Drop{Source: casket, Events: 3, Items: []loot.Item{{ID: 995, Name: "Coins", Count: 60}}})
	rec(loot.Drop{Source: casket, Events: 1, Items: []loot.Item{{ID: 995, Name: "Coins", Count: 20}}})
	m.lootRecorder("idle1") // a hook that never fires

	all := m.LootTotals()
	if len(all) != 1 || all[0].Account != "fisher1" {
		t.Fatalf("LootTotals = %+v, want only fisher1", all)
	}
	src := all[0].Sources
	if len(src) != 1 || src[0].Events != 4 || src[0].Items[0].Count != 80 {
		t.Fatalf("fisher1 totals = %+v", src)
	}
	// The same account's hook, taken again (a script restart), keeps adding to one tally.
	m.lootRecorder("fisher1")(loot.Drop{Source: casket, Events: 1})
	if got := m.LootTotals()[0].Sources[0].Events; got != 5 {
		t.Fatalf("after a second hook the casket count is %d, want 5", got)
	}
}

// TestTradeRecorderWritesToTheAttachedLedger proves the bot.Context.TradeLog hook StartBot
// installs writes to whatever ledger is attached when the trade happens, and records nothing
// (without failing) while none is.
func TestTradeRecorderWritesToTheAttachedLedger(t *testing.T) {
	m := New(Config{})
	rec := m.tradeRecorder()
	rec(muleledger.Entry{Mule: "mule", Partner: "early", Kind: muleledger.KindReceive, Outcome: muleledger.OutcomeAccepted})
	l := muleledger.Open("", 0)
	m.SetMuleLedger(l)
	rec(muleledger.Entry{Mule: "mule", Partner: "Fisher1", Kind: muleledger.KindReceive, Outcome: muleledger.OutcomeAccepted,
		In: []muleledger.Item{{ID: 995, Name: "Coins", Count: 500}}})
	got := m.MuleLedger().Entries(muleledger.Query{})
	if len(got) != 1 || got[0].Partner != "Fisher1" || got[0].In[0].Count != 500 || got[0].AtMs == 0 {
		t.Fatalf("ledger holds %+v, want only Fisher1's stamped trade", got)
	}
}

// TestMuleOutReaderReadsTheAttachedLedger proves the bot.Context.MuleOut hook StartBot installs
// answers from whatever ledger is attached when it is asked: nothing and no horizon while none is,
// then the items the mule handed out since the time given and how far back the record reaches.
func TestMuleOutReaderReadsTheAttachedLedger(t *testing.T) {
	m := New(Config{})
	read := m.muleOutReader()
	if got, from := read("mule", time.Time{}); len(got) != 0 || !from.IsZero() {
		t.Fatalf("no ledger attached: %v %v, want nothing", got, from)
	}
	m.SetMuleLedger(muleledger.Open("", 0))
	rec := m.tradeRecorder()
	old, recent := time.Now().Add(-48*time.Hour), time.Now().Add(-time.Hour)
	rec(muleledger.Entry{AtMs: old.UnixMilli(), Mule: "mule", Partner: "Acct1", Kind: muleledger.KindHandout, Outcome: muleledger.OutcomeAccepted,
		Out: []muleledger.Item{{ID: -1, Name: "Rune axe", Count: 1}}})
	rec(muleledger.Entry{AtMs: recent.UnixMilli(), Mule: "mule", Partner: "Acct2", Kind: muleledger.KindHandout, Outcome: muleledger.OutcomeAccepted,
		Out: []muleledger.Item{{ID: -1, Name: "Shark", Count: 12}}})
	got, from := read("mule", time.Now().Add(-24*time.Hour))
	if len(got) != 1 || got["shark"] != 12 {
		t.Fatalf("handed out in the last day: %v, want only the 12 Sharks", got)
	}
	if from.UnixMilli() != old.UnixMilli() {
		t.Fatalf("the record reaches back to %v, want its oldest entry %v", from, old)
	}
}
