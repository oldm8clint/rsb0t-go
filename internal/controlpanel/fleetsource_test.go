package controlpanel

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"rsb0t-go/internal/fleet"
	"rsb0t-go/internal/gamestate"
)

var (
	_ Source            = (*FleetSource)(nil)
	_ InventorySource   = (*FleetSource)(nil)
	_ BankSource        = (*FleetSource)(nil)
	_ BotControl        = (*FleetSource)(nil)
	_ ConnectionControl = (*FleetSource)(nil)
	_ Source            = (*FakeSource)(nil)
	_ InventorySource   = (*FakeSource)(nil)
	_ BankSource        = (*FakeSource)(nil)
	_ BotControl        = (*FakeSource)(nil)
	_ ConnectionControl = (*FakeSource)(nil)
)

func blank(name string) fleet.AccountStatus {
	return fleet.AccountStatus{Username: name, SelfSlot: -1, MainModal: gamestate.NoModal, SideModal: gamestate.NoModal}
}

func TestFleetSourceMapsTheSnapshot(t *testing.T) {
	now := time.Unix(1700000000, 0)
	online := blank("online")
	online.Connected = true
	online.StaffModLevel = 2
	online.SelfSlot = 7
	online.Packets = 120
	online.ConnectedSecs = 90
	online.Inventories = map[int][]fleet.SlotStatus{3214: {{Slot: 0, ID: 995, Count: 25}}, 93: {}}
	dropped := blank("dropped")
	dropped.Connected, dropped.Disconnected, dropped.DisconnectMsg = true, true, "read timeout"
	refused := blank("refused")
	refused.Error = "login response 3"
	snap := []fleet.AccountStatus{online, dropped, refused, blank("dialling")}

	src := newFleetSource(func() []fleet.AccountStatus { return snap }, []string{"online", "dropped", "refused", "dialling", "later1", "later2"}, 3*time.Second)
	src.now = func() time.Time { return now }
	src.poll = 0
	events := make(chan Event, 16)
	defer src.Subscribe(events)()

	got := map[string]AccountStatus{}
	for _, a := range src.Accounts(nil) {
		got[a.Name] = a
	}
	for name, want := range map[string]State{"online": StateIngame, "dropped": StateOffline, "refused": StateError, "dialling": StateConnecting, "later1": StateQueued, "later2": StateQueued} {
		if got[name].State != want {
			t.Errorf("%s is %q, want %q", name, got[name].State, want)
		}
	}
	o := got["online"]
	if o.Packets != 120 || o.StaffLevel != 2 || o.SinceUnixMs != now.Add(-90*time.Second).UnixMilli() {
		t.Errorf("online: %+v", o)
	}
	if len(o.Counters) != 2 || o.Counters[0] != (Counter{"player slot", 7}) || o.Counters[1] != (Counter{"inventories", 2}) {
		t.Errorf("online counters %+v", o.Counters)
	}
	if got["dropped"].LastError != "read timeout" || got["refused"].LastError != "login response 3" {
		t.Errorf("errors: %q, %q", got["dropped"].LastError, got["refused"].LastError)
	}

	if len(events) != 4 {
		t.Fatalf("%d events from the first snapshot, want one per started account", len(events))
	}
	levels := map[string]Level{}
	for len(events) > 0 {
		ev := <-events
		levels[ev.Account] = ev.Level
		if ev.Kind != "state" {
			t.Errorf("event kind %q", ev.Kind)
		}
	}
	if levels["online"] != LevelInfo || levels["dropped"] != LevelWarn || levels["refused"] != LevelError {
		t.Errorf("event levels %v", levels)
	}

	src.Accounts(nil)
	if len(events) != 0 {
		t.Fatalf("an unchanged snapshot made %d events", len(events))
	}
	snap[3].Connected = true
	src.Accounts(nil)
	if len(events) != 1 {
		t.Fatalf("one login made %d events", len(events))
	}
	if ev := <-events; ev.Account != "dialling" || ev.Text != "logged in" {
		t.Errorf("login event %+v", ev)
	}

	l := src.Logins()
	if len(l.Queue) != 2 || l.Queue[0] != "later1" || l.Sessions != 2 || l.InFlight != 0 || l.SpacingMs != 3000 || l.NextAttemptUnixMs != 0 {
		t.Errorf("logins before a login started: %+v", l)
	}
	src.LoginStarted()
	if l := src.Logins(); l.NextAttemptUnixMs != now.Add(3*time.Second).UnixMilli() {
		t.Errorf("next attempt %d", l.NextAttemptUnixMs)
	}

	inv, ok := src.Inventories("online")
	if !ok || len(inv) != 2 || inv[0].ID != 93 || inv[1].ID != 3214 || inv[1].Slots[0] != (Slot{Slot: 0, ID: 995, Count: 25}) {
		t.Errorf("online inventories %+v, %v", inv, ok)
	}
	if inv, ok := src.Inventories("later1"); !ok || len(inv) != 0 {
		t.Errorf("a queued account: %+v, %v", inv, ok)
	}
	if _, ok := src.Inventories("nobody"); ok {
		t.Error("an account the fleet does not have reported inventories")
	}
}

func TestFleetSourceMapsReconnectingAndBotFields(t *testing.T) {
	reconnecting := blank("reconnecting")
	reconnecting.Reconnecting = true
	reconnecting.ReconnectAttempt = 3
	botted := blank("botted")
	botted.Connected = true
	botted.Script = "BarbarianAgility"
	botted.BotState = "error"
	botted.BotError = "no path to the bank"
	snap := []fleet.AccountStatus{reconnecting, botted}

	src := newFleetSource(func() []fleet.AccountStatus { return snap }, []string{"reconnecting", "botted"}, time.Second)
	src.poll = 0
	got := map[string]AccountStatus{}
	for _, a := range src.Accounts(nil) {
		got[a.Name] = a
	}

	r := got["reconnecting"]
	if r.State != StateReconnecting {
		t.Errorf("reconnecting account is %q, want %q", r.State, StateReconnecting)
	}
	if r.Reconnects != 3 {
		t.Errorf("reconnects = %d, want 3", r.Reconnects)
	}

	b := got["botted"]
	if b.Bot != "BarbarianAgility" || b.BotState != "error" || b.BotError != "no path to the bank" {
		t.Errorf("botted: %+v", b)
	}
	// BotError should surface as LastError too, same as Error/DisconnectMsg do, since a bot
	// crash is exactly the sort of thing the accounts table's "last error" column is for.
	if b.LastError != "no path to the bank" {
		t.Errorf("botted LastError = %q, want the bot error", b.LastError)
	}
}

func TestFleetSourceBotControlNeedsAManager(t *testing.T) {
	src := newFleetSource(func() []fleet.AccountStatus { return nil }, nil, time.Second)
	if err := src.StartBot("a", "BarbarianAgility", nil); err == nil {
		t.Error("StartBot with no manager attached: want an error, got nil")
	}
	if err := src.StopBot("a"); err == nil {
		t.Error("StopBot with no manager attached: want an error, got nil")
	}
	// BotScripts does not need a manager — it just lists what this build registered.
	if src.BotScripts() == nil {
		t.Error("BotScripts returned nil, want a (possibly empty) slice")
	}
}

func TestFleetSourceBotControlOverARealManager(t *testing.T) {
	m := fleet.New(fleet.Config{})
	src := NewFleetSource(m, []string{"a"}, time.Second)
	if err := src.StartBot("a", "BarbarianAgility", nil); err == nil {
		t.Error("StartBot on an account that never connected: want an error, got nil")
	}
	if err := src.StopBot("a"); err == nil {
		t.Error("StopBot on an account with no bot: want an error, got nil")
	}
}

func TestFleetSourceConnectionControlNeedsAManager(t *testing.T) {
	src := newFleetSource(func() []fleet.AccountStatus { return nil }, nil, time.Second)
	if err := src.Disconnect("a"); err == nil {
		t.Error("Disconnect with no manager attached: want an error, got nil")
	}
	if err := src.ConnectExisting("a"); err == nil {
		t.Error("ConnectExisting with no manager attached: want an error, got nil")
	}
	if err := src.AddAccount("a", "pw", "", nil); err == nil {
		t.Error("AddAccount with no manager attached: want an error, got nil")
	}
}

func TestFleetSourceConnectionControlOverARealManager(t *testing.T) {
	m := fleet.New(fleet.Config{})
	src := NewFleetSource(m, []string{"a"}, time.Second)
	// None of these ever connected (no real network, and fleet.Config{} has no WSURL/
	// HTTPBase to dial anyway), so every call should surface the Manager's own clear
	// errors rather than panicking or silently no-op-ing.
	if err := src.Disconnect("a"); err == nil {
		t.Error("Disconnect on an account that never connected: want an error, got nil")
	}
	if err := src.ConnectExisting("nobody"); err == nil {
		t.Error("ConnectExisting on an unknown account: want an error, got nil")
	}
	// The first AddAccount call fails fast (no real engine to dial) but still records
	// the account (Manager.connectAccount appends before attempting login, same as
	// Connect always has) — so a second AddAccount for the same username hits the
	// dedupe check without ever attempting another dial, proving that check works
	// through the ConnectionControl surface and not just inside internal/fleet's own
	// white-box tests.
	if err := src.AddAccount("a", "pw", "", nil); err == nil {
		t.Error("AddAccount with no real engine to dial: want an error, got nil")
	}
	err := src.AddAccount("a", "pw", "", nil)
	if err == nil || !strings.Contains(err.Error(), "already in this fleet") {
		t.Errorf("AddAccount on a username the first call already recorded: got %v, want an 'already in this fleet' error", err)
	}
}

func TestFleetSourceMapsManuallyDisconnectedAndSettings(t *testing.T) {
	a := blank("stopped")
	a.Disconnected = true
	a.ManuallyDisconnected = true
	b := blank("configured")
	b.Connected = true
	b.Script = "BarbarianAgility"
	b.Settings = map[string]interface{}{"maxLaps": float64(5), "food": "Lobster"}
	snap := []fleet.AccountStatus{a, b}

	src := newFleetSource(func() []fleet.AccountStatus { return snap }, []string{"stopped", "configured"}, time.Second)
	src.poll = 0
	got := map[string]AccountStatus{}
	for _, one := range src.Accounts(nil) {
		got[one.Name] = one
	}

	s := got["stopped"]
	if s.State != StateStopped {
		t.Errorf("manually disconnected account state = %q, want %q", s.State, StateStopped)
	}
	if !s.ManuallyDisconnected {
		t.Error("ManuallyDisconnected not surfaced")
	}

	c := got["configured"]
	if v, _ := c.Settings["maxLaps"].(float64); v != 5 || c.Settings["food"] != "Lobster" {
		t.Errorf("settings not surfaced: %+v", c.Settings)
	}
}

func TestFleetSourceOverARealManager(t *testing.T) {
	m := fleet.New(fleet.Config{})
	src := NewFleetSource(m, []string{"a", "b"}, time.Second)
	accounts := src.Accounts(nil)
	if len(accounts) != 2 || accounts[0].State != StateQueued {
		t.Fatalf("accounts %+v", accounts)
	}
	if l := src.Logins(); len(l.Queue) != 2 {
		t.Errorf("queue %v", l.Queue)
	}
}

func TestFleetSourcePollsOnlyWhileSubscribed(t *testing.T) {
	var calls int64
	src := newFleetSource(func() []fleet.AccountStatus {
		atomic.AddInt64(&calls, 1)
		return nil
	}, nil, 0)
	src.poll = 5 * time.Millisecond
	time.Sleep(30 * time.Millisecond)
	if n := atomic.LoadInt64(&calls); n != 0 {
		t.Fatalf("polled %d times with no subscriber", n)
	}
	cancel := src.Subscribe(make(chan Event, 1))
	waitFor(t, "a poll", func() bool { return atomic.LoadInt64(&calls) > 0 })
	cancel()
	time.Sleep(30 * time.Millisecond)
	after := atomic.LoadInt64(&calls)
	time.Sleep(60 * time.Millisecond)
	if n := atomic.LoadInt64(&calls); n != after {
		t.Errorf("still polling after the last subscriber left: %d then %d", after, n)
	}
}

func TestFleetSourceFeedsTheHub(t *testing.T) {
	snap := []fleet.AccountStatus{blank("a")}
	var live atomic.Value
	live.Store(snap)
	src := newFleetSource(func() []fleet.AccountStatus { return live.Load().([]fleet.AccountStatus) }, []string{"a"}, 0)
	src.poll = 5 * time.Millisecond
	h := newHarness(t, Config{}, testOptions(), src)
	next := []fleet.AccountStatus{blank("a")}
	next[0].Error = "login response 5"
	live.Store(next)
	waitFor(t, "the poll to publish the login failure", func() bool {
		events, _ := h.srv.hub.eventsAfter(0, 10, kindEvents)
		for _, ev := range events {
			if ev.Account == "a" && ev.Level == LevelError {
				return true
			}
		}
		return false
	})
}

// TestFleetSourceMapsHPAndDead covers the alerts digest panel's own data source: HP/MaxHP/
// Dead need to reach AccountStatus unchanged from fleet.AccountStatus, since nothing here
// derives new state, only surfaces what internal/fleet already decoded.
func TestFleetSourceMapsHPAndDead(t *testing.T) {
	a := blank("hp1")
	a.Connected = true
	a.HPKnown = true
	a.HP = 5
	a.MaxHP = 40
	a.Dead = true
	snap := []fleet.AccountStatus{a}

	src := newFleetSource(func() []fleet.AccountStatus { return snap }, []string{"hp1"}, time.Second)
	src.poll = 0
	got := src.Accounts(nil)
	if len(got) != 1 {
		t.Fatalf("accounts %+v", got)
	}
	s := got[0]
	if !s.HPKnown || s.HP != 5 || s.MaxHP != 40 || !s.Dead {
		t.Errorf("HP/MaxHP/Dead not surfaced: %+v", s)
	}
}

// TestFleetSourceMapsRandomEventsStructured covers the alerts digest's other real
// dependency: the structured, newest-first RandomEvents list (not just the lifetime
// cleared/not-cleared Counters this package already added), since the digest needs to
// know whether the MOST RECENT event is still unhandled right now.
func TestFleetSourceMapsRandomEventsStructured(t *testing.T) {
	a := blank("re1")
	a.Connected = true
	a.RandomEvents = []fleet.RandomEventOutcome{
		{Name: "Genie", Outcome: "still_present", At: "2026-09-14T00:00:00Z"},
		{Name: "Maze", Outcome: "cleared", At: "2026-09-13T23:00:00Z"},
	}
	snap := []fleet.AccountStatus{a}

	src := newFleetSource(func() []fleet.AccountStatus { return snap }, []string{"re1"}, time.Second)
	src.poll = 0
	got := src.Accounts(nil)
	if len(got) != 1 || len(got[0].RandomEvents) != 2 {
		t.Fatalf("random events not surfaced: %+v", got)
	}
	if got[0].RandomEvents[0].Name != "Genie" || got[0].RandomEvents[0].Outcome != "still_present" {
		t.Errorf("newest-first random event wrong: %+v", got[0].RandomEvents[0])
	}
}

// TestFleetSourceEmitsOnlyNewChatLines covers the chat feed panel's live-streaming
// mechanism: observe() must turn fleet.AccountStatus.RecentChat/ChatCount into
// individual KindChat events, emitting only the lines that are genuinely new since the
// last poll — and, on the very first observation of an account, none at all (matching
// xpBaseline's own "baseline on first sight, no backfill" discipline), since an account
// already mid-session when the control panel starts watching should not dump its whole
// chat history into the stream at once.
func TestFleetSourceEmitsOnlyNewChatLines(t *testing.T) {
	a := blank("chatty")
	a.Connected = true
	a.RecentChat = []string{"line1", "line2"}
	a.ChatCount = 2
	snap := []fleet.AccountStatus{a}

	src := newFleetSource(func() []fleet.AccountStatus { return snap }, []string{"chatty"}, time.Second)
	src.poll = 0
	events := make(chan Event, 16)
	defer src.Subscribe(events)()

	// First observation legitimately emits a "state" event (queued -> ingame, since
	// a.Connected is true) — that's unrelated to this test, which only cares that no
	// KindChat event fires yet.
	src.Accounts(nil)
drainFirst:
	for {
		select {
		case ev := <-events:
			if ev.Kind == KindChat {
				t.Fatalf("unexpected chat event on first sight: %+v", ev)
			}
		default:
			break drainFirst
		}
	}

	a.RecentChat = []string{"line1", "line2", "line3", "line4"}
	a.ChatCount = 4
	snap[0] = a
	src.Accounts(nil)

	var got []string
drain:
	for {
		select {
		case ev := <-events:
			if ev.Kind != KindChat {
				t.Errorf("unexpected event kind %q for a chat diff", ev.Kind)
				continue
			}
			got = append(got, ev.Text)
		default:
			break drain
		}
	}
	if len(got) != 2 || got[0] != "line3" || got[1] != "line4" {
		t.Errorf("new chat lines = %v, want [line3 line4]", got)
	}
}

// TestFleetSourceBank covers BankSource: items/updatedAt pass through from fleet.
// AccountStatus.BankItems/BankUpdatedAt, a roster-only account (known but never banked)
// reports an empty-but-found bank rather than "not found", and a genuinely unknown
// account reports not found — mirroring Inventories' own three-way contract.
func TestFleetSourceBank(t *testing.T) {
	a := blank("banked")
	a.Connected = true
	a.BankItems = []fleet.SlotStatus{{Slot: 0, ID: 995, Count: 1000}}
	a.BankUpdatedAt = "2026-09-14T12:00:00Z"
	snap := []fleet.AccountStatus{a}

	src := newFleetSource(func() []fleet.AccountStatus { return snap }, []string{"banked", "neverbanked"}, time.Second)
	src.poll = 0

	items, updated, ok := src.Bank("banked")
	if !ok || len(items) != 1 || items[0].ID != 995 || items[0].Count != 1000 {
		t.Fatalf("Bank(banked) = %+v, %v, %v", items, updated, ok)
	}
	want, err := time.Parse(time.RFC3339, "2026-09-14T12:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if updated != want.UnixMilli() {
		t.Errorf("updatedAt = %d, want %d", updated, want.UnixMilli())
	}
	// newFleetSource (unlike NewFleetSource) attaches no *fleet.Manager, so there is no
	// cache to resolve a name from — Name should stay empty rather than panic.
	if items[0].Name != "" {
		t.Errorf("unexpected name resolution with no manager: %q", items[0].Name)
	}

	items2, updated2, ok2 := src.Bank("neverbanked")
	if !ok2 || len(items2) != 0 || updated2 != 0 {
		t.Errorf("Bank(neverbanked) = %+v, %v, %v, want empty/0/true", items2, updated2, ok2)
	}

	if _, _, ok3 := src.Bank("nobody"); ok3 {
		t.Error("Bank(nobody): want ok=false")
	}
}

// Run energy has to survive the hop from fleet.AccountStatus to controlpanel.AccountStatus,
// and the distinction between "0%" and "not told yet" has to survive with it. Both halves are
// asserted, because a mapping that copied Energy but dropped EnergyKnown would look correct in
// every case except the one the field exists for: an account whose energy is genuinely spent.
func TestFleetSourceMapsRunEnergyIncludingZero(t *testing.T) {
	spent := blank("spent")
	spent.Connected = true
	spent.EnergyKnown = true
	spent.Energy = 0

	fresh := blank("fresh")
	fresh.Connected = true
	fresh.EnergyKnown = true
	fresh.Energy = 100

	// Never sent an UPDATE_RUNENERGY: must NOT read as 0% energy.
	silent := blank("silent")
	silent.Connected = true

	snap := []fleet.AccountStatus{spent, fresh, silent}
	src := newFleetSource(func() []fleet.AccountStatus { return snap }, []string{"spent", "fresh", "silent"}, time.Second)
	src.poll = 0
	got := map[string]AccountStatus{}
	for _, a := range src.Accounts(nil) {
		got[a.Name] = a
	}

	if a := got["spent"]; !a.EnergyKnown || a.Energy != 0 {
		t.Errorf("spent: energyKnown=%v energy=%d, want known and 0 — a drained account is the case this field exists for", a.EnergyKnown, a.Energy)
	}
	if a := got["fresh"]; !a.EnergyKnown || a.Energy != 100 {
		t.Errorf("fresh: energyKnown=%v energy=%d, want known and 100", a.EnergyKnown, a.Energy)
	}
	if a := got["silent"]; a.EnergyKnown {
		t.Errorf("silent: energyKnown=%v, want false — nothing has reported energy for this account", a.EnergyKnown)
	}
}

// The "random events not cleared" counter must mean TERMINAL failures only. Counting every
// outcome that is not "cleared" is the mistake stats.go's own RandomsFailed doc comment
// records — it swept in the guardian correctly declining another player's event, and mere
// retries, and made the number meaningless. Measured live 2026-09-21 on a healthy fleet:
// 116 "not_ours" and 103 "retry" against a handful of real failures.
func TestFleetSourceCountsOnlyTerminalRandomEventFailures(t *testing.T) {
	a := blank("acct")
	a.Connected = true
	a.RandomEvents = []fleet.RandomEventOutcome{
		{Name: "Genie", Outcome: "cleared"},
		{Name: "Genie", Outcome: "not_ours"}, // correctly declined — somebody else's
		{Name: "whirlpool", Outcome: "retry"},
		{Name: "whirlpool", Outcome: "retry"},
		{Name: "Mime", Outcome: "error"},   // terminal
		{Name: "Maze", Outcome: "gave_up"}, // terminal
		{Name: "Genie", Outcome: "handling"},
		{Name: "Genie", Outcome: "something new"}, // unrecognised: counted nowhere
	}

	src := newFleetSource(func() []fleet.AccountStatus { return []fleet.AccountStatus{a} }, []string{"acct"}, time.Second)
	src.poll = 0
	var cleared, notCleared int64
	var sawCleared, sawNotCleared bool
	for _, got := range src.Accounts(nil) {
		for _, c := range got.Counters {
			switch c.Name {
			case "random events cleared":
				cleared, sawCleared = c.Value, true
			case "random events not cleared":
				notCleared, sawNotCleared = c.Value, true
			}
		}
	}
	if !sawCleared || cleared != 1 {
		t.Errorf("cleared = %d (present=%v), want 1", cleared, sawCleared)
	}
	if !sawNotCleared || notCleared != 2 {
		t.Errorf("not cleared = %d (present=%v), want 2 — only the error and the give-up, not the 1 not_ours, the 2 retries, the handling or the unrecognised outcome",
			notCleared, sawNotCleared)
	}
}

// And with nothing terminal, the counter must be absent entirely rather than reading zero
// next to a pile of routine outcomes.
func TestFleetSourceOmitsTheFailureCounterWhenNothingFailed(t *testing.T) {
	a := blank("quiet")
	a.Connected = true
	a.RandomEvents = []fleet.RandomEventOutcome{
		{Name: "Genie", Outcome: "not_ours"},
		{Name: "whirlpool", Outcome: "retry"},
		{Name: "Genie", Outcome: "cleared"},
	}
	src := newFleetSource(func() []fleet.AccountStatus { return []fleet.AccountStatus{a} }, []string{"quiet"}, time.Second)
	src.poll = 0
	for _, got := range src.Accounts(nil) {
		for _, c := range got.Counters {
			if c.Name == "random events not cleared" {
				t.Errorf("counter present with value %d, want it omitted — nothing terminal happened", c.Value)
			}
		}
	}
}

// A Start that supplies NO settings must fall back to the account's roster settings, not to
// the script's own defaults. cheatSetup defaults to true in fisher.go/barbarian.go/crafter.go
// -- a flag those files label LOCAL-ENGINE-ONLY -- so on production the script issues ::tele,
// the server refuses it, and the bot stops. That happened to a live account on 2026-09-21.
//
// An explicitly empty object is a different statement and must survive as one.
func TestStartBotFallsBackToRosterSettingsOnlyWhenNoneWereSupplied(t *testing.T) {
	roster := map[string]map[string]interface{}{
		"spare30": {"cheatSetup": false, "mule": "mule"},
	}
	for _, c := range []struct {
		name     string
		supplied map[string]interface{}
		want     map[string]interface{}
	}{
		{"absent falls back to the roster", nil, roster["spare30"]},
		{"explicitly empty stays empty", map[string]interface{}{}, map[string]interface{}{}},
		{"supplied settings win", map[string]interface{}{"cheatSetup": true}, map[string]interface{}{"cheatSetup": true}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFleetSource(func() []fleet.AccountStatus { return nil }, []string{"spare30"}, time.Second)
			f.SetRosterSettings(func(account string) map[string]interface{} { return roster[account] })
			// The REAL rule, not a restatement of it: StartBot short-circuits on a nil
			// manager, so it can never show what it would have passed.
			got := f.resolveStartSettings("spare30", "ProgressiveFisher", c.supplied)
			if (got == nil) != (c.want == nil) {
				t.Fatalf("resolved %v (nil=%v), want nil=%v", got, got == nil, c.want == nil)
			}
			if len(got) != len(c.want) {
				t.Fatalf("resolved %v, want %v", got, c.want)
			}
			for k, v := range c.want {
				if got[k] != v {
					t.Errorf("resolved[%q] = %v, want %v", k, got[k], v)
				}
			}
		})
	}
}

// And with no roster lookup wired at all (cmd/fleet, tests), nil must stay nil rather than
// becoming an empty map, so nothing about the existing behaviour changes.
func TestStartBotWithoutARosterLookupPassesNilThrough(t *testing.T) {
	f := newFleetSource(func() []fleet.AccountStatus { return nil }, []string{"spare30"}, time.Second)
	if f.rosterSettings != nil {
		t.Fatal("a FleetSource with no lookup wired should have none")
	}
	if err := f.StartBot("spare30", "ProgressiveFisher", nil); err != errNoManager {
		t.Fatalf("StartBot error = %v, want errNoManager", err)
	}
}
