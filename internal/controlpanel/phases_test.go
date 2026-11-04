package controlpanel

import (
	"net/http"
	"testing"
	"time"

	"rsb0t-go/internal/muleledger"
	"rsb0t-go/internal/mulestatus"
	"rsb0t-go/internal/phaselog"
)

// phaseStub is a Source with a scripted PhaseSource and MuleSource.
type phaseStub struct {
	staticSource
	phases []AccountPhase
	mules  []MuleStatus
	bank   []Slot
}

func (p *phaseStub) Phases(dst []AccountPhase) []AccountPhase { return append(dst[:0], p.phases...) }
func (p *phaseStub) MuleStatuses() []MuleStatus               { return p.mules }
func (p *phaseStub) Bank(account string) ([]Slot, int64, bool) {
	if account != "mule" {
		return nil, 0, false
	}
	return p.bank, 1000, true
}

func TestEffectivePhaseOverrides(t *testing.T) {
	since := time.Unix(100, 0)
	cases := []struct {
		p    AccountPhase
		want string
	}{
		{AccountPhase{InGame: false, Running: true, Phase: "fishing"}, phaselog.Offline},
		{AccountPhase{InGame: true, Running: true, Phase: "fishing", Event: "Genie", EventSince: since}, phaselog.Event},
		{AccountPhase{InGame: true, Running: false, Phase: "fishing"}, phaselog.NoScript},
		{AccountPhase{InGame: true, Running: true}, phaselog.Unknown},
		{AccountPhase{InGame: true, Running: true, Phase: "banking", Since: since}, "banking"},
	}
	for _, c := range cases {
		if got, _ := effectivePhase(c.p); got != c.want {
			t.Errorf("%+v -> %q, want %q", c.p, got, c.want)
		}
	}
}

func TestPhaseRecorderSplitsFleetScriptsAndAccounts(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	src := &phaseStub{}
	r := newPhaseRecorder(src, func() time.Time { return now })
	sample := func(sec int, phases ...AccountPhase) {
		for i := 0; i < sec; i++ {
			now = now.Add(time.Second)
			r.record(phases, now)
		}
	}
	fish := AccountPhase{Username: "f1", InGame: true, Script: "ProgressiveFisher", Running: true, Phase: "fishing"}
	bank := fish
	bank.Phase = "banking"
	mule := AccountPhase{Username: "mule", InGame: true, Script: "MuleReceiver", Running: true, Phase: "idle"}
	off := AccountPhase{Username: "f2", Script: "ProgressiveFisher"}
	sample(1, fish, mule, off)
	sample(60, fish, mule, off)
	sample(30, bank, mule, off)

	q := r.query("15m", "f1", 30)
	col := map[string]int{}
	for i, p := range q.Phases {
		col[p] = i
	}
	if q.Fleet.Accounts != 3 || len(q.Scripts) != 2 || q.Scripts[0].Script != "ProgressiveFisher" || q.Scripts[0].Accounts != 2 {
		t.Fatalf("groups: fleet=%+v scripts=%+v", q.Fleet, q.Scripts)
	}
	var f1 phaseAccountJSON
	for _, a := range q.Accounts {
		if a.Name == "f1" {
			f1 = a
		}
	}
	if f1.Ms[col["fishing"]] != 60000 || f1.Ms[col["banking"]] != 30000 || f1.Phase != "banking" {
		t.Fatalf("f1 split = %+v (phases %v)", f1, q.Phases)
	}
	if q.Fleet.Ms[col[phaselog.Offline]] != 90000 || q.Fleet.Ms[col["idle"]] != 90000 {
		t.Fatalf("fleet split = %+v (phases %v)", q.Fleet.Ms, q.Phases)
	}
	if q.Timeline == nil || len(q.Timeline.Segments) != 2 || q.Timeline.Totals["fishing"] != 60000 {
		t.Fatalf("timeline = %+v", q.Timeline)
	}
	// An offline account keeps the script it last ran.
	for _, a := range q.Accounts {
		if a.Name == "f2" && a.Script != "ProgressiveFisher" {
			t.Fatalf("offline f2 lost its script: %+v", a)
		}
	}
}

func TestPhasesAndMuleEndpoints(t *testing.T) {
	tr := mulestatus.NewTracker("mule", nil)
	tr.SetCatalog([]string{"Big fishing net"})
	tr.Stage("donor", mulestatus.StageOffer)
	src := &phaseStub{
		phases: []AccountPhase{{Username: "mule", InGame: true, Script: "MuleReceiver", Running: true, Phase: "trading"}},
		mules:  []MuleStatus{tr.Snapshot()},
		bank:   []Slot{{Name: "Raw lobster", Count: 50}, {Name: "Coins", Count: 900}, {Name: "Big fishing net", Count: 2}},
	}
	h := newHarness(t, Config{}, testOptions(), src)
	h.srv.phases.record(src.phases, time.Now())

	res := h.get("/api/phases?window=15m&account=mule&minutes=10")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("/api/phases = %d %s", res.StatusCode, bodyText(t, res))
	}
	var pj phasesJSON
	decode(t, res, &pj)
	if len(pj.Accounts) != 1 || pj.Timeline == nil || pj.Timeline.Account != "mule" {
		t.Fatalf("phases = %+v", pj)
	}
	for _, bad := range []string{"?window=2d", "?minutes=0", "?minutes=999"} {
		if res := h.get("/api/phases" + bad); res.StatusCode != http.StatusBadRequest {
			t.Errorf("/api/phases%s = %d, want 400", bad, res.StatusCode)
		}
	}

	res = h.get("/api/mule")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("/api/mule = %d", res.StatusCode)
	}
	var mj muleJSON
	decode(t, res, &mj)
	if len(mj.Mules) != 1 {
		t.Fatalf("mules = %+v", mj)
	}
	m := mj.Mules[0]
	if m.Trade == nil || m.Trade.Partner != "donor" || m.Phase != "trading" {
		t.Fatalf("mule trade/phase = %+v / %q", m.Trade, m.Phase)
	}
	if m.Stock == nil || m.Stock.Coins != 900 || len(m.Stock.Raw) != 1 || m.Stock.Catalog[0].Count != 2 {
		t.Fatalf("stock = %+v", m.Stock)
	}

	plain := newHarness(t, Config{}, testOptions(), &staticSource{})
	for _, path := range []string{"/api/phases", "/api/mule"} {
		if res := plain.get(path); res.StatusCode != http.StatusNotImplemented {
			t.Errorf("%s on a source without phases = %d, want 501", path, res.StatusCode)
		}
	}
}

func TestFakeFleetSimulatesPhasesAndAMule(t *testing.T) {
	f := NewFakeSource(14, 1)
	clock := time.Now()
	f.now = func() time.Time { return clock }
	seen := map[string]bool{}
	for i := 0; i < 3600; i++ {
		clock = clock.Add(time.Second)
		f.Step()
		for _, p := range f.Phases(nil) {
			ph, _ := effectivePhase(p)
			seen[ph] = true
		}
	}
	for _, want := range []string{phaselog.Fishing, phaselog.Walking, phaselog.Banking, phaselog.Event, phaselog.Handoff} {
		if !seen[want] {
			t.Errorf("an hour of the fake fleet never showed %q (saw %v)", want, seen)
		}
	}
	mules := f.MuleStatuses()
	if len(mules) != 1 || mules[0].Account != fakeMuleName || mules[0].Stock == nil {
		t.Fatalf("fake mule = %+v", mules)
	}
	sum := mulestatus.Summarize(f.MuleTrades(muleledger.Query{Mule: fakeMuleName, Limit: muleledger.MaxLimit}), clock, mules[0].StartedMs)
	if sum.Completed == 0 || sum.Failed == 0 || len(sum.Handouts) == 0 || len(sum.Reasons) == 0 || len(sum.Received) == 0 {
		t.Fatalf("an hour of the fake mule's ledger is too thin to demo: %+v", sum)
	}
	if mules[0].Timing.Cycles == 0 || mules[0].Timing.BusyMs == 0 {
		t.Fatalf("the fake mule's cycle timing is empty: %+v", mules[0].Timing)
	}
}

// phaseTradeStub adds the fleet's mule ledger, filtered the way muleledger.Ledger filters.
type phaseTradeStub struct {
	phaseStub
	ledger *muleledger.Ledger
}

func (p *phaseTradeStub) MuleTrades(q muleledger.Query) []muleledger.Entry {
	return p.ledger.Entries(q)
}

func TestMuleEndpointReadsHistoryOnlyFromTheTradeRecord(t *testing.T) {
	tr := mulestatus.NewTracker("mule", nil)
	now := time.Now().UnixMilli()
	live := &phaseStub{mules: []MuleStatus{tr.Snapshot()}}
	h := newHarness(t, Config{}, testOptions(), live)
	var mj muleJSON
	decode(t, h.get("/api/mule"), &mj)
	if mj.History || mj.Mules[0].History != nil {
		t.Fatalf("a source with no trade record reported history: %+v", mj)
	}
	ledger := muleledger.Open("", 0)
	lob := muleledger.Item{ID: 377, Name: "Raw lobster", Count: 5}
	ledger.Record(muleledger.Entry{AtMs: now, Mule: "mule", Partner: "a", Kind: muleledger.KindReceive, Outcome: muleledger.OutcomeAccepted, In: []muleledger.Item{lob, lob, lob, lob}})
	ledger.Record(muleledger.Entry{AtMs: now, Mule: "mule", Partner: "b", Kind: muleledger.KindReceive, Outcome: muleledger.OutcomeFailed, Reason: "no stable offer"})
	// Another mule's trade and one from two days ago are not this mule's history.
	ledger.Record(muleledger.Entry{AtMs: now, Mule: "othermule", Partner: "c", Kind: muleledger.KindReceive, Outcome: muleledger.OutcomeAccepted})
	ledger.Record(muleledger.Entry{AtMs: now - 48*3600*1000, Mule: "mule", Partner: "d", Kind: muleledger.KindReceive, Outcome: muleledger.OutcomeAccepted})
	withHistory := &phaseTradeStub{phaseStub: phaseStub{mules: []MuleStatus{tr.Snapshot()}}, ledger: ledger}
	h2 := newHarness(t, Config{}, testOptions(), withHistory)
	decode(t, h2.get("/api/mule"), &mj)
	hs := mj.Mules[0].History
	if !mj.History || hs == nil || hs.Completed != 1 || hs.Failed != 1 || hs.StacksReceived != 4 || len(hs.Failures) != 1 ||
		len(hs.Reasons) != 1 || hs.Reasons[0].Reason != "no stable offer" || len(hs.Received) != 1 || hs.Received[0].Count != 20 {
		t.Fatalf("history = %+v", hs)
	}
}

func TestRandomEventOverlayNamesEventsOnTheTimelineAndCountsThemFleetWide(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	r := newPhaseRecorder(&phaseStub{}, func() time.Time { return now })
	fish := func(name string) AccountPhase {
		return AccountPhase{Username: name, InGame: true, Script: "ProgressiveFisher", Running: true, Phase: "fishing"}
	}
	event := func(name, ev string, since time.Time) AccountPhase {
		p := fish(name)
		p.Event, p.EventSince = ev, since
		return p
	}
	step := func(sec int, phases func() []AccountPhase) {
		for i := 0; i < sec; i++ {
			now = now.Add(time.Second)
			r.record(phases(), now)
		}
	}
	step(30, func() []AccountPhase { return []AccountPhase{fish("f1"), fish("f2")} })
	// f1: a Genie that the guardian says began half a second before the sample saw it.
	genieAt := now.Add(500 * time.Millisecond)
	step(20, func() []AccountPhase { return []AccountPhase{event("f1", "Genie", genieAt), fish("f2")} })
	step(30, func() []AccountPhase { return []AccountPhase{fish("f1"), fish("f2")} })
	// Then both at once: f1 a second Genie, f2 a Sandwich lady.
	at := now
	step(10, func() []AccountPhase {
		return []AccountPhase{event("f1", "Genie", at), event("f2", "Sandwich lady", at)}
	})
	step(5, func() []AccountPhase { return []AccountPhase{fish("f1"), fish("f2")} })

	q := r.query("15m", "f1", 15)
	if len(q.Events) != 2 || q.Events[0].Name != "Genie" || q.Events[0].Count != 2 || q.Events[0].Accounts != 1 ||
		q.Events[1].Name != "Sandwich lady" || q.Events[1].Count != 1 {
		t.Fatalf("fleet events = %+v", q.Events)
	}
	if q.Events[0].Ms != 29500 {
		t.Fatalf("Genie time %dms, want 29500 (19.5s + 10s)", q.Events[0].Ms)
	}
	for _, a := range q.Accounts {
		want := map[string]int{"f1": 2, "f2": 1}[a.Name]
		if a.Events != want {
			t.Fatalf("%s events = %d, want %d", a.Name, a.Events, want)
		}
	}
	if q.Timeline == nil || len(q.Timeline.Events) != 2 || q.Timeline.Events[0].Phase != "Genie" || q.Timeline.Events[0].StartMs != genieAt.UnixMilli() {
		t.Fatalf("timeline events = %+v", q.Timeline)
	}
	// The overlay does not disturb the phase split: the event time is "event" there.
	col := map[string]int{}
	for i, p := range q.Phases {
		col[p] = i
	}
	for _, a := range q.Accounts {
		if a.Name == "f1" && a.Ms[col[phaselog.Event]] != 29500 {
			t.Fatalf("f1 event phase %dms, want 29500", a.Ms[col[phaselog.Event]])
		}
	}
}
