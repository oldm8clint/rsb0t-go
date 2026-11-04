package mulestatus

import (
	"fmt"
	"testing"
	"time"

	"rsb0t-go/internal/muleledger"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time           { return c.t }
func (c *clock) advance(d time.Duration)  { c.t = c.t.Add(d) }
func newClock() *clock                    { return &clock{t: time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)} }
func (c *clock) ms(d time.Duration) int64 { return c.t.Add(d).UnixMilli() }

// One full engagement the way the mule bot reports it: reciprocated, offer, confirm, completed,
// banked.
func trade(tr *Tracker, c *clock, partner string, negotiate, bank time.Duration) {
	tr.Stage(partner, StageReciprocating)
	c.advance(negotiate / 2)
	tr.Stage(partner, StageOffer)
	c.advance(negotiate / 2)
	tr.Stage(partner, StageConfirm)
	tr.Completed(partner)
	c.advance(bank)
	tr.Banked()
}

func TestTrackerLiveStateFollowsAnAnnouncedDonorThroughItsTrade(t *testing.T) {
	c := newClock()
	tr := NewTracker("mule", c.now)
	tr.SetIncoming(map[string]time.Time{"Fisher_1": c.t, "fisher_2": c.t.Add(time.Second)})
	s := tr.Snapshot()
	if len(s.Incoming) != 2 || s.Incoming[0].Name != "Fisher_1" || s.DonorsHour != 2 {
		t.Fatalf("incoming = %+v donors=%d", s.Incoming, s.DonorsHour)
	}
	tr.Stage("Fisher 1", StageOffer)
	if s = tr.Snapshot(); s.Trade == nil || s.Trade.Partner != "Fisher 1" || s.Trade.Stage != StageOffer {
		t.Fatalf("trade = %+v", s.Trade)
	}
	c.advance(5 * time.Second)
	tr.Stage("Fisher 1", StageConfirm)
	if s = tr.Snapshot(); s.Trade.StageSinceMs != c.t.UnixMilli() || s.Trade.SinceMs != c.ms(-5*time.Second) {
		t.Fatalf("stage timing = %+v", s.Trade)
	}
	// The game reports "Fisher 1"; the bus announced "Fisher_1". Same player.
	tr.Completed("Fisher 1")
	s = tr.Snapshot()
	if s.Trade != nil || len(s.Incoming) != 1 || s.Incoming[0].Name != "fisher_2" {
		t.Fatalf("after the trade: trade=%+v incoming=%+v", s.Trade, s.Incoming)
	}
	if s.Timing.Cycles != 0 {
		t.Fatalf("a cycle was timed before its items were banked: %+v", s.Timing)
	}
	c.advance(10 * time.Second)
	tr.Banked()
	s = tr.Snapshot()
	if s.Timing.Cycles != 1 || s.Timing.NegotiateMedianMs != 5000 || s.Timing.BankMedianMs != 10000 || s.Timing.BusyMs != 15000 {
		t.Fatalf("timing = %+v", s.Timing)
	}
	var nilTracker *Tracker
	nilTracker.Stage("x", StageOffer) // must not panic
	nilTracker.Completed("x")
	nilTracker.Failed("x")
	nilTracker.Banked()
}

func TestTrackerTimingMediansBusyAndBounds(t *testing.T) {
	c := newClock()
	tr := NewTracker("mule", c.now)
	for i := 0; i < 6; i++ {
		trade(tr, c, fmt.Sprintf("d%d", i), 20*time.Second, 10*time.Second)
	}
	tr.Stage("d9", StageOffer)
	c.advance(20 * time.Second)
	tr.Failed("")
	s := tr.Snapshot()
	// 6 x 30s engagements + one 20s failure.
	if s.Timing.Cycles != 6 || s.Timing.NegotiateMedianMs != 20000 || s.Timing.BankMedianMs != 10000 || s.Timing.BusyMs != 200000 {
		t.Fatalf("timing = %+v", s.Timing)
	}
	if s.Trade != nil || s.DonorsHour != 7 {
		t.Fatalf("trade=%+v donors=%d", s.Trade, s.DonorsHour)
	}
	// Two hours on, only cycles inside the last hour count toward busy time.
	c.advance(2 * time.Hour)
	trade(tr, c, "late", 10*time.Second, 5*time.Second)
	if s = tr.Snapshot(); s.Timing.BusyMs != 15000 || s.DonorsHour != 1 || s.WindowMs != time.Hour.Milliseconds() {
		t.Fatalf("after two hours: timing=%+v donors=%d window=%d", s.Timing, s.DonorsHour, s.WindowMs)
	}
	for i := 0; i < 3*maxCycles; i++ {
		trade(tr, c, "x", time.Second, time.Second)
	}
	if len(tr.cycles) != maxCycles {
		t.Fatalf("cycle ring grew to %d", len(tr.cycles))
	}
	// A completed trade still being banked when the ring rolls keeps its own index.
	tr.Completed("y")
	for i := 0; i < 3; i++ {
		tr.Failed("z")
	}
	c.advance(7 * time.Second)
	tr.Banked()
	if got := tr.cycles[len(tr.cycles)-4]; got.banked == 0 || got.banked-got.accept != 7000 {
		t.Fatalf("banked the wrong cycle: %+v", got)
	}
}

func entry(at int64, partner, outcome, reason string, in ...muleledger.Item) muleledger.Entry {
	return muleledger.Entry{AtMs: at, Mule: "mule", Partner: partner, Kind: muleledger.KindReceive, Outcome: outcome, Reason: reason, In: in}
}

func TestSummarizeReadsTheLedger(t *testing.T) {
	c := newClock()
	started := c.ms(-30 * time.Minute)
	lob := muleledger.Item{ID: 377, Name: "Raw lobster", Count: 10}
	coins := muleledger.Item{ID: 995, Name: "Coins", Count: 100}
	var es []muleledger.Entry
	for i := 0; i < 6; i++ {
		es = append(es, entry(c.ms(-time.Duration(20-i)*time.Minute), fmt.Sprintf("d%d", i), muleledger.OutcomeAccepted, "", lob, coins))
	}
	es = append(es,
		entry(c.ms(-3*time.Minute), "d9", muleledger.OutcomeFailed, "no stable offer"),
		entry(c.ms(-2*time.Minute), "d8", muleledger.OutcomeFailed, "confirm screen timeout"),
		entry(c.ms(-1*time.Minute), "d7", muleledger.OutcomeFailed, ""),
		// Before this mule's run started: in the totals, not the window.
		entry(c.ms(-50*time.Minute), "old", muleledger.OutcomeAccepted, "", lob),
		muleledger.Entry{AtMs: c.ms(-4 * time.Minute), Mule: "mule", Partner: "acct4", Kind: muleledger.KindHandout, Outcome: muleledger.OutcomeAccepted, Out: []muleledger.Item{{ID: 995, Name: "Coins", Count: 500}}},
		muleledger.Entry{AtMs: c.ms(-4 * time.Minute), Mule: "mule", Partner: "acct5", Kind: muleledger.KindHandout, Outcome: muleledger.OutcomeFailed, Reason: "over cap"},
	)
	s := Summarize(es, c.t, started)
	if s.Completed != 7 || s.Failed != 3 || s.CompletedHour != 6 || s.FailedHour != 3 || s.StacksReceived != 13 {
		t.Fatalf("counts: %+v", s)
	}
	if s.SuccessRate != 6.0/9.0 || s.WindowMs != 30*time.Minute.Milliseconds() {
		t.Fatalf("success rate %v window %d", s.SuccessRate, s.WindowMs)
	}
	if len(s.Failures) != 3 || s.Failures[0].Partner != "d7" || s.Failures[0].Reason != "unknown" {
		t.Fatalf("failures, newest first, unknown reason named: %+v", s.Failures)
	}
	if len(s.Reasons) != 3 || s.Reasons[0] != (ReasonCount{Reason: "confirm screen timeout", Count: 1}) {
		t.Fatalf("reasons = %+v", s.Reasons)
	}
	if len(s.Received) != 2 || s.Received[0] != (ItemCount{Name: "Coins", Count: 600}) || s.Received[1] != (ItemCount{Name: "Raw lobster", Count: 60}) {
		t.Fatalf("received = %+v", s.Received)
	}
	if s.Partners != 9 || s.HandoutsTotal != 1 || s.HandoutsFailed != 1 || s.Handouts[0].To != "acct4" || s.Handouts[0].Count != 500 || s.Handouts[0].Item != "Coins" {
		t.Fatalf("partners=%d handouts=%+v failed=%d", s.Partners, s.Handouts, s.HandoutsFailed)
	}
	if s = Summarize(nil, c.t, 0); s.SuccessRate != -1 {
		t.Fatalf("no trades should read as unknown, got %v", s.SuccessRate)
	}
}

func TestSummarizeBounds(t *testing.T) {
	c := newClock()
	var many []muleledger.Entry
	for i := 0; i < 500; i++ {
		many = append(many, entry(int64(i), "x", muleledger.OutcomeFailed, "x"),
			muleledger.Entry{AtMs: int64(i), Kind: muleledger.KindHandout, Outcome: muleledger.OutcomeAccepted})
	}
	s := Summarize(many, c.t, 0)
	if len(s.Failures) != maxFailures || len(s.Handouts) != maxHandouts || s.Failed != 500 || s.HandoutsTotal != 500 {
		t.Fatalf("lists must be bounded, totals exact: failures=%d handouts=%d failed=%d total=%d", len(s.Failures), len(s.Handouts), s.Failed, s.HandoutsTotal)
	}
	var named []muleledger.Entry
	for i := 0; i < 3*maxItems; i++ {
		named = append(named, entry(c.t.UnixMilli(), "x", muleledger.OutcomeAccepted, "", muleledger.Item{Name: fmt.Sprintf("item%d", i%(2*maxItems)), Count: 1}))
	}
	if s = Summarize(named, c.t, 0); len(s.Received) != maxItems {
		t.Fatalf("received names = %d, want capped at %d", len(s.Received), maxItems)
	}
}

func TestDonorSeenIsBounded(t *testing.T) {
	c := newClock()
	tr := NewTracker("mule", c.now)
	for i := 0; i < maxDonorSeen+300; i++ {
		tr.SetIncoming(map[string]time.Time{fmt.Sprintf("d%d", i): c.t})
		c.advance(time.Second)
	}
	if len(tr.donorSeen) > maxDonorSeen {
		t.Fatalf("donorSeen grew to %d", len(tr.donorSeen))
	}
}

func TestClassifyStock(t *testing.T) {
	names := []string{"Raw lobster", "Lobster", "Coins", "Raw swordfish", "Air rune", "Big fishing net", "Raw shark", "Bones"}
	counts := []int64{120, 40, 90000, 30, 500, 3, 2, 7}
	st := ClassifyStock(names, counts, []string{"Big fishing net", "Air rune", "Staff of air", "Coins"}, time.UnixMilli(5))
	if st.Coins != 90000 {
		t.Fatalf("coins %d", st.Coins)
	}
	if len(st.Raw) != 3 || st.Raw[0].Name != "Raw lobster" || st.Raw[2].Name != "Raw shark" {
		t.Fatalf("raw = %+v", st.Raw)
	}
	if len(st.Cooked) != 1 || st.Cooked[0].Count != 40 {
		t.Fatalf("cooked = %+v", st.Cooked)
	}
	want := map[string]int64{"Big fishing net": 3, "Air rune": 500, "Staff of air": 0, "Coins": 90000}
	for _, c := range st.Catalog {
		if want[c.Name] != c.Count {
			t.Fatalf("catalog %s = %d, want %d", c.Name, c.Count, want[c.Name])
		}
	}
	if st.OtherStacks != 1 || st.UpdatedMs != 5 {
		t.Fatalf("other=%d updated=%d", st.OtherStacks, st.UpdatedMs)
	}
}
