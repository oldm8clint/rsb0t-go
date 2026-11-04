package controlpanel

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"rsb0t-go/internal/muleledger"
)

// ledgerSource is a static Source that keeps a mule trade record.
type ledgerSource struct {
	*staticSource
	ledger *muleledger.Ledger
}

func (s ledgerSource) MuleTrades(q muleledger.Query) []muleledger.Entry { return s.ledger.Entries(q) }

func decodeLedger(t *testing.T, res *http.Response) MuleLedgerBody {
	t.Helper()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	var body MuleLedgerBody
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body
}

// TestMuleLedgerEndpoint proves GET /api/mule-ledger serves the attached record with its
// filters, rejects malformed ones, and answers 501 for a Source that keeps no record.
func TestMuleLedgerEndpoint(t *testing.T) {
	l := muleledger.Open("", 0)
	l.Record(muleledger.Entry{AtMs: 1000, Mule: "mule", Partner: "Fisher1", Kind: muleledger.KindReceive, Outcome: muleledger.OutcomeAccepted,
		In: []muleledger.Item{{ID: 995, Name: "Coins", Count: 300}}})
	l.Record(muleledger.Entry{AtMs: 2000, Mule: "mule", Partner: "Fisher2", Kind: muleledger.KindReceive, Outcome: muleledger.OutcomeFailed, Reason: "no stable offer"})
	l.Record(muleledger.Entry{AtMs: 3000, Mule: "mule2", Partner: "Acct1", Kind: muleledger.KindHandout, Outcome: muleledger.OutcomeAccepted,
		Out: []muleledger.Item{{ID: 301, Name: "Lobster pot", Count: 1}}})
	h := newHarness(t, Config{}, testOptions(), ledgerSource{&staticSource{}, l})

	all := decodeLedger(t, h.get("/api/mule-ledger"))
	if len(all.Entries) != 3 || all.Entries[0].In[0].Count != 300 || all.Entries[2].Out[0].Name != "Lobster pot" {
		t.Fatalf("unfiltered ledger: %+v", all.Entries)
	}
	some := decodeLedger(t, h.get("/api/mule-ledger?since=1500&mule=mule"))
	if len(some.Entries) != 1 || some.Entries[0].Reason != "no stable offer" {
		t.Fatalf("filtered ledger: %+v", some.Entries)
	}
	last := decodeLedger(t, h.get("/api/mule-ledger?limit=1"))
	if len(last.Entries) != 1 || last.Entries[0].AtMs != 3000 {
		t.Fatalf("limit=1 did not return the newest: %+v", last.Entries)
	}
	for _, bad := range []string{"?since=x", "?since=-1", "?limit=0", "?limit=abc"} {
		if res := h.get("/api/mule-ledger" + bad); res.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s answered %d, want 400", bad, res.StatusCode)
		}
	}
	if res := h.request(http.MethodPost, "/api/mule-ledger", nil, nil); res.StatusCode == http.StatusOK {
		t.Fatal("POST was accepted")
	}

	plain := newHarness(t, Config{}, testOptions(), &staticSource{})
	if res := plain.get("/api/mule-ledger"); res.StatusCode != http.StatusNotImplemented {
		t.Fatalf("a Source with no record answered %d, want 501", res.StatusCode)
	}
}

// TestFakeFleetServesLootAndMuleLedger runs the fake fleet the -fake panel uses through both
// endpoints: loot per source with items, and simulated mule trades.
func TestFakeFleetServesLootAndMuleLedger(t *testing.T) {
	f := NewFakeSource(12, 3)
	// The fake fleet logs in on its own clock; walk that clock a second per step so every
	// account reaches the game and works.
	clock := time.Now()
	f.mu.Lock()
	f.now = func() time.Time { return clock }
	f.mu.Unlock()
	for i := 0; i < 1500; i++ {
		clock = clock.Add(time.Second)
		f.Step()
	}
	h := newHarness(t, Config{}, testOptions(), f)
	h.srv.stats.poll()

	res := h.get("/api/loot")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("/api/loot status %d", res.StatusCode)
	}
	var loot LootBody
	if err := json.NewDecoder(res.Body).Decode(&loot); err != nil {
		t.Fatal(err)
	}
	if !loot.Available || len(loot.SinceRestart.Sources) == 0 || loot.SinceRestart.Items == 0 {
		t.Fatalf("/api/loot has no fake loot: available=%v sources=%d items=%d", loot.Available, len(loot.SinceRestart.Sources), loot.SinceRestart.Items)
	}
	kinds := map[string]bool{}
	for _, s := range loot.SinceRestart.Sources {
		kinds[s.Source.Action] = true
	}
	for _, want := range []string{"kill", "pickpocket", "open"} {
		if !kinds[want] {
			t.Fatalf("no %q source in the fake fleet's loot: %v", want, kinds)
		}
	}

	ledger := decodeLedger(t, h.get("/api/mule-ledger?limit=5000"))
	if len(ledger.Entries) == 0 {
		t.Fatal("the fake fleet recorded no mule trades")
	}
	var accepted, failed int
	for _, e := range ledger.Entries {
		if e.Mule != fakeMule || e.AtMs <= 0 || e.AtMs > clock.UnixMilli() {
			t.Fatalf("implausible fake trade %+v", e)
		}
		switch e.Outcome {
		case muleledger.OutcomeAccepted:
			accepted++
		case muleledger.OutcomeFailed:
			failed++
			if e.Reason == "" || len(e.In) != 0 {
				t.Fatalf("a failed fake trade has no reason or moved items: %+v", e)
			}
		}
	}
	if accepted == 0 {
		t.Fatalf("no accepted fake trades among %d", len(ledger.Entries))
	}
}
