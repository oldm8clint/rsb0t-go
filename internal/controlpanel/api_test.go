package controlpanel

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestMetaReportsBotControlAndScripts(t *testing.T) {
	// No BotControl at all (plain FakeSource does implement it now, so use a plain staticSource).
	h := newHarness(t, Config{}, testOptions(), &staticSource{})
	var m metaBody
	decode(t, h.get("/api/meta"), &m)
	if m.BotControl {
		t.Error("meta reports botControl for a Source without it")
	}
	if len(m.Scripts) != 0 {
		t.Errorf("scripts %v, want none without BotControl", m.Scripts)
	}

	bc := botControlSource{&staticSource{scripts: []string{"BarbarianAgility", "ProgressiveFisher"}}}
	h2 := newHarness(t, Config{}, testOptions(), bc)
	decode(t, h2.get("/api/meta"), &m)
	if !m.BotControl {
		t.Error("meta does not report botControl for a Source that has it")
	}
	if len(m.Scripts) != 2 || m.Scripts[0] != "BarbarianAgility" {
		t.Errorf("scripts %v", m.Scripts)
	}
}

func TestBotStartAndStop(t *testing.T) {
	src := &staticSource{scripts: []string{"BarbarianAgility"}}
	bc := botControlSource{src}
	h := newHarness(t, Config{}, testOptions(), bc)

	res := h.request(http.MethodPost, "/api/bot/start", strings.NewReader(`{"account":"a","script":"BarbarianAgility","settings":{"maxLaps":5}}`), http.Header{"Content-Type": {"application/json"}})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("start: status %d, body %s", res.StatusCode, bodyText(t, res))
	}
	if len(src.starts) != 1 || src.starts[0].Account != "a" || src.starts[0].Script != "BarbarianAgility" {
		t.Fatalf("starts recorded: %+v", src.starts)
	}
	if v, _ := src.starts[0].Settings["maxLaps"].(float64); v != 5 {
		t.Errorf("settings not passed through: %+v", src.starts[0].Settings)
	}

	res = h.request(http.MethodPost, "/api/bot/stop", strings.NewReader(`{"account":"a"}`), http.Header{"Content-Type": {"application/json"}})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("stop: status %d, body %s", res.StatusCode, bodyText(t, res))
	}
	if len(src.stops) != 1 || src.stops[0] != "a" {
		t.Fatalf("stops recorded: %+v", src.stops)
	}

	// A GET is refused (these are actions, not reads), and so is one with no account.
	if res := h.get("/api/bot/start"); res.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET /api/bot/start: %d, want 405", res.StatusCode)
	}
	res = h.request(http.MethodPost, "/api/bot/start", strings.NewReader(`{"script":"BarbarianAgility"}`), http.Header{"Content-Type": {"application/json"}})
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("start with no account: %d, want 400", res.StatusCode)
	}

	// A Source without BotControl answers 501, same as /api/inventory does today.
	h2 := newHarness(t, Config{}, testOptions(), &staticSource{})
	res = h2.request(http.MethodPost, "/api/bot/start", strings.NewReader(`{"account":"a","script":"x"}`), http.Header{"Content-Type": {"application/json"}})
	if res.StatusCode != http.StatusNotImplemented {
		t.Errorf("start with no BotControl: %d, want 501", res.StatusCode)
	}
}

func TestBotStartSurfacesTheManagerError(t *testing.T) {
	src := &staticSource{startErr: fmt.Errorf("fleet: no such account %q", "ghost")}
	h := newHarness(t, Config{}, testOptions(), botControlSource{src})
	res := h.request(http.MethodPost, "/api/bot/start", strings.NewReader(`{"account":"ghost","script":"x"}`), http.Header{"Content-Type": {"application/json"}})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", res.StatusCode)
	}
	if body := bodyText(t, res); !strings.Contains(body, "no such account") {
		t.Errorf("body %q does not carry the manager's error", body)
	}
}

func TestMetaReportsConnectionControl(t *testing.T) {
	h := newHarness(t, Config{}, testOptions(), &staticSource{})
	var m metaBody
	decode(t, h.get("/api/meta"), &m)
	if m.ConnectionControl {
		t.Error("meta reports connectionControl for a Source without it")
	}

	h2 := newHarness(t, Config{}, testOptions(), connectionControlSource{&staticSource{}})
	decode(t, h2.get("/api/meta"), &m)
	if !m.ConnectionControl {
		t.Error("meta does not report connectionControl for a Source that has it")
	}
}

func TestFleetDisconnectAndConnect(t *testing.T) {
	src := &staticSource{}
	cc := connectionControlSource{src}
	h := newHarness(t, Config{}, testOptions(), cc)

	res := h.request(http.MethodPost, "/api/fleet/disconnect", strings.NewReader(`{"account":"a"}`), http.Header{"Content-Type": {"application/json"}})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("disconnect: status %d, body %s", res.StatusCode, bodyText(t, res))
	}
	if len(src.disconnects) != 1 || src.disconnects[0] != "a" {
		t.Fatalf("disconnects recorded: %+v", src.disconnects)
	}

	res = h.request(http.MethodPost, "/api/fleet/connect", strings.NewReader(`{"account":"a"}`), http.Header{"Content-Type": {"application/json"}})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("connect: status %d, body %s", res.StatusCode, bodyText(t, res))
	}
	if len(src.connects) != 1 || src.connects[0] != "a" {
		t.Fatalf("connects recorded: %+v", src.connects)
	}

	// GETs are refused (these are actions, not reads), and so is an empty account.
	if res := h.get("/api/fleet/disconnect"); res.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET /api/fleet/disconnect: %d, want 405", res.StatusCode)
	}
	res = h.request(http.MethodPost, "/api/fleet/connect", strings.NewReader(`{}`), http.Header{"Content-Type": {"application/json"}})
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("connect with no account: %d, want 400", res.StatusCode)
	}

	// A Source without ConnectionControl answers 501, same as /api/bot/start does.
	h2 := newHarness(t, Config{}, testOptions(), &staticSource{})
	res = h2.request(http.MethodPost, "/api/fleet/disconnect", strings.NewReader(`{"account":"a"}`), http.Header{"Content-Type": {"application/json"}})
	if res.StatusCode != http.StatusNotImplemented {
		t.Errorf("disconnect with no ConnectionControl: %d, want 501", res.StatusCode)
	}
	res = h2.request(http.MethodPost, "/api/fleet/connect", strings.NewReader(`{"account":"a"}`), http.Header{"Content-Type": {"application/json"}})
	if res.StatusCode != http.StatusNotImplemented {
		t.Errorf("connect with no ConnectionControl: %d, want 501", res.StatusCode)
	}
}

func TestFleetDisconnectSurfacesTheManagerError(t *testing.T) {
	src := &staticSource{disconnectErr: fmt.Errorf("fleet: no such account %q", "ghost")}
	h := newHarness(t, Config{}, testOptions(), connectionControlSource{src})
	res := h.request(http.MethodPost, "/api/fleet/disconnect", strings.NewReader(`{"account":"ghost"}`), http.Header{"Content-Type": {"application/json"}})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", res.StatusCode)
	}
	if body := bodyText(t, res); !strings.Contains(body, "no such account") {
		t.Errorf("body %q does not carry the manager's error", body)
	}
}

func TestFleetAddAccount(t *testing.T) {
	src := &staticSource{}
	h := newHarness(t, Config{}, testOptions(), connectionControlSource{src})

	res := h.request(http.MethodPost, "/api/fleet/add", strings.NewReader(`{"username":"newacct","password":"secret","script":"BarbarianAgility","settings":{"maxLaps":3}}`), http.Header{"Content-Type": {"application/json"}})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("add: status %d, body %s", res.StatusCode, bodyText(t, res))
	}
	if len(src.adds) != 1 {
		t.Fatalf("adds recorded: %+v", src.adds)
	}
	got := src.adds[0]
	if got.Account != "newacct" || got.Password != "secret" || got.Script != "BarbarianAgility" {
		t.Errorf("add call = %+v", got)
	}
	if v, _ := got.Settings["maxLaps"].(float64); v != 3 {
		t.Errorf("settings not passed through: %+v", got.Settings)
	}

	// Missing username/password are refused before ever reaching ConnectionControl.
	res = h.request(http.MethodPost, "/api/fleet/add", strings.NewReader(`{"username":"x"}`), http.Header{"Content-Type": {"application/json"}})
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("add with no password: %d, want 400", res.StatusCode)
	}
	res = h.request(http.MethodPost, "/api/fleet/add", strings.NewReader(`{"password":"x"}`), http.Header{"Content-Type": {"application/json"}})
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("add with no username: %d, want 400", res.StatusCode)
	}
	if len(src.adds) != 1 {
		t.Errorf("a rejected add call still reached ConnectionControl: %+v", src.adds)
	}

	// A Source without ConnectionControl answers 501.
	h2 := newHarness(t, Config{}, testOptions(), &staticSource{})
	res = h2.request(http.MethodPost, "/api/fleet/add", strings.NewReader(`{"username":"x","password":"y"}`), http.Header{"Content-Type": {"application/json"}})
	if res.StatusCode != http.StatusNotImplemented {
		t.Errorf("add with no ConnectionControl: %d, want 501", res.StatusCode)
	}
}

func TestFleetAddAccountSurfacesTheManagerError(t *testing.T) {
	src := &staticSource{addErr: fmt.Errorf("fleet: %s is already in this fleet", "dupe")}
	h := newHarness(t, Config{}, testOptions(), connectionControlSource{src})
	res := h.request(http.MethodPost, "/api/fleet/add", strings.NewReader(`{"username":"dupe","password":"x"}`), http.Header{"Content-Type": {"application/json"}})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", res.StatusCode)
	}
	if body := bodyText(t, res); !strings.Contains(body, "already in this fleet") {
		t.Errorf("body %q does not carry the manager's error", body)
	}
}

func TestStatusListsEveryAccount(t *testing.T) {
	src := NewFakeSource(6, 1)
	for i := 0; i < 40; i++ {
		src.Step()
	}
	h := newHarness(t, Config{}, testOptions(), src)
	res := h.get("/api/status")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	if cc := res.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control %q, want no-store", cc)
	}
	var body statusBody
	decode(t, res, &body)
	if len(body.Accounts) != 6 {
		t.Fatalf("%d accounts, want 6", len(body.Accounts))
	}
	total := 0
	for _, n := range body.Counts {
		total += n
	}
	if total != 6 {
		t.Errorf("counts add up to %d, want 6: %v", total, body.Counts)
	}
	for _, s := range States {
		if _, ok := body.Counts[s]; !ok {
			t.Errorf("counts have no %q", s)
		}
	}
	if body.Logins.SpacingMs != fakeSpacing.Milliseconds() || body.Logins.Queue == nil {
		t.Errorf("logins %+v", body.Logins)
	}
}

func TestStatusBoundsWhatItLists(t *testing.T) {
	src := &staticSource{}
	long := strings.Repeat("n", 100)
	for i := 0; i < MaxAccounts+44; i++ {
		counters := make([]Counter, 20)
		for j := range counters {
			counters[j] = Counter{Name: long, Value: int64(j)}
		}
		src.accounts = append(src.accounts, AccountStatus{Name: fmt.Sprintf("%s%d", long, i), State: StateIngame, Bot: long, Activity: long + long, LastError: long + long + long, Counters: counters})
	}
	src.logins.Queue = make([]string, MaxAccounts+10)
	h := newHarness(t, Config{}, testOptions(), src)
	var body statusBody
	decode(t, h.get("/api/status"), &body)
	if len(body.Accounts) != MaxAccounts || body.Truncated != 44 {
		t.Fatalf("%d listed and %d truncated, want %d and 44", len(body.Accounts), body.Truncated, MaxAccounts)
	}
	if body.Counts[StateIngame] != MaxAccounts+44 {
		t.Errorf("ingame count %d, want every account counted", body.Counts[StateIngame])
	}
	a := body.Accounts[0]
	if len(a.Name) > maxNameBytes || len(a.Bot) > maxBotBytes || len(a.Activity) > maxActivityBytes || len(a.LastError) > maxErrorBytes {
		t.Errorf("strings not cut: name %d, bot %d, activity %d, error %d bytes", len(a.Name), len(a.Bot), len(a.Activity), len(a.LastError))
	}
	if len(a.Counters) != MaxCounters || len(a.Counters[0].Name) > maxCounterNameBytes {
		t.Errorf("%d counters, first name %d bytes", len(a.Counters), len(a.Counters[0].Name))
	}
	if len(body.Logins.Queue) != MaxAccounts {
		t.Errorf("login queue lists %d, want %d", len(body.Logins.Queue), MaxAccounts)
	}
}

func TestMetaReportsLimits(t *testing.T) {
	h := newHarness(t, Config{}, testOptions(), nil)
	var m metaBody
	decode(t, h.get("/api/meta"), &m)
	if m.Version != APIVersion || m.Grid.Columns != GridColumns || m.Grid.MaxRows != GridMaxRows || m.Grid.MaxPanels != MaxPanels {
		t.Errorf("meta %+v", m)
	}
	if m.LayoutPersisted {
		t.Error("layout reported persisted with no file configured")
	}
	if !m.Inventories {
		t.Error("FakeSource reports inventories, but meta says it does not")
	}
	if m.EventHistory != DefaultEventHistory || m.LogHistory != DefaultLogHistory || m.MemoryHistory != DefaultMemoryHistory || m.TickMs != 20 {
		t.Errorf("history %d/%d/%d tick %d", m.EventHistory, m.LogHistory, m.MemoryHistory, m.TickMs)
	}
	if len(m.States) != len(States) || len(m.Levels) != len(Levels) {
		t.Errorf("states %v levels %v", m.States, m.Levels)
	}
}

func TestEventsSinceAndLimit(t *testing.T) {
	h := newHarness(t, Config{}, testOptions(), &staticSource{})
	for i := 1; i <= 10; i++ {
		h.srv.hub.publish(Event{Account: "fisher01", Text: fmt.Sprintf("line %d", i)})
	}
	var body eventsBody
	decode(t, h.get("/api/events?since=4&limit=3"), &body)
	if body.Last != 10 || len(body.Events) != 3 {
		t.Fatalf("last %d, %d events", body.Last, len(body.Events))
	}
	for i, want := range []uint64{8, 9, 10} {
		if body.Events[i].Seq != want {
			t.Errorf("event %d has seq %d, want %d", i, body.Events[i].Seq, want)
		}
	}
	for _, bad := range []string{"/api/events?since=x", "/api/events?limit=0", "/api/events?limit=-2", "/api/events?kind=chat"} {
		if res := h.get(bad); res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s answered %d, want 400", bad, res.StatusCode)
		}
	}
}

func TestEventHistoryKeepsTheNewest(t *testing.T) {
	h := newHarness(t, Config{EventHistory: MinHistory}, testOptions(), &staticSource{})
	for i := 0; i < 40; i++ {
		h.srv.hub.publish(Event{Text: "x"})
	}
	var body eventsBody
	decode(t, h.get("/api/events"), &body)
	if len(body.Events) != MinHistory || body.Events[0].Seq != 25 || body.Events[len(body.Events)-1].Seq != 40 {
		t.Fatalf("%d events from %d to %d", len(body.Events), body.Events[0].Seq, body.Events[len(body.Events)-1].Seq)
	}
}

func TestLogLinesCannotPushEventsOut(t *testing.T) {
	h := newHarness(t, Config{EventHistory: MinHistory, LogHistory: MinHistory}, testOptions(), &staticSource{})
	h.srv.hub.publish(Event{Kind: "login", Level: LevelError, Text: "login failed"})
	for i := 0; i < 100; i++ {
		h.srv.hub.publish(Event{Kind: KindLog, Text: "chatter"})
	}
	h.srv.hub.publish(Event{Kind: "state", Text: "logged in"})

	var all, events, logs eventsBody
	decode(t, h.get("/api/events"), &all)
	decode(t, h.get("/api/events?kind=event"), &events)
	decode(t, h.get("/api/events?kind=log"), &logs)
	if len(all.Events) != MinHistory+2 || len(events.Events) != 2 || len(logs.Events) != MinHistory {
		t.Fatalf("all %d, events %d, logs %d", len(all.Events), len(events.Events), len(logs.Events))
	}
	if events.Events[0].Text != "login failed" || events.Events[0].Seq != 1 {
		t.Errorf("the early event was lost: %+v", events.Events[0])
	}
	for i := 1; i < len(all.Events); i++ {
		if all.Events[i].Seq <= all.Events[i-1].Seq {
			t.Fatalf("merged history out of order at %d: %d then %d", i, all.Events[i-1].Seq, all.Events[i].Seq)
		}
	}
}

func TestEventsAreBoundedBeforeKept(t *testing.T) {
	h := newHarness(t, Config{}, testOptions(), &staticSource{})
	h.srv.hub.publish(Event{Seq: 999, Account: strings.Repeat("a", 90), Kind: strings.Repeat("k", 90), Level: "loud", Text: strings.Repeat("é", 1000)})
	events, _ := h.srv.hub.eventsAfter(0, 10, "")
	ev := events[0]
	if ev.Seq != 1 {
		t.Errorf("seq %d, want the hub's own 1", ev.Seq)
	}
	if len(ev.Text) > maxTextBytes || !utf8.ValidString(ev.Text) || len(ev.Account) > maxNameBytes || len(ev.Kind) > maxKindBytes {
		t.Errorf("text %d bytes (valid %v), account %d, kind %d", len(ev.Text), utf8.ValidString(ev.Text), len(ev.Account), len(ev.Kind))
	}
	if ev.Level != LevelInfo || ev.TimeUnixMs == 0 {
		t.Errorf("level %q time %d", ev.Level, ev.TimeUnixMs)
	}
}

func TestPumpTakesEventsFromTheSource(t *testing.T) {
	src := NewFakeSource(2, 1)
	h := newHarness(t, Config{}, testOptions(), src)
	for i := 0; i < 100; i++ {
		src.Emit(Event{Account: "fisher01", Text: "hi"})
	}
	waitFor(t, "the pump to take 100 events", func() bool {
		_, last := h.srv.hub.eventsAfter(0, 1, "")
		return last == 100
	})
}

func TestMemoryEndpoint(t *testing.T) {
	h := newHarness(t, Config{}, testOptions(), nil)
	h.srv.hub.sample(nil)
	h.srv.hub.sample(nil)
	var body memoryBody
	decode(t, h.get("/api/memory"), &body)
	if len(body.Samples) != 2 || body.IntervalMs != 20 {
		t.Fatalf("%d samples at %d ms", len(body.Samples), body.IntervalMs)
	}
	if body.Now.Sys == 0 || body.Now.Goroutines == 0 || body.Now.T == 0 {
		t.Errorf("now sample %+v", body.Now)
	}
}

func TestInventoryEndpoint(t *testing.T) {
	src := NewFakeSource(3, 1)
	for i := 0; i < 60; i++ {
		src.Step()
	}
	h := newHarness(t, Config{}, testOptions(), src)
	var body inventoryBody
	res := h.get("/api/inventory?account=fisher01")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("fisher01: %d", res.StatusCode)
	}
	decode(t, res, &body)
	if body.Account != "fisher01" || body.Inventories == nil {
		t.Errorf("body %+v", body)
	}
	for path, want := range map[string]int{
		"/api/inventory":                                    http.StatusBadRequest,
		"/api/inventory?account=nobody":                     http.StatusNotFound,
		"/api/inventory?account=" + strings.Repeat("x", 40): http.StatusBadRequest,
	} {
		if res := h.get(path); res.StatusCode != want {
			t.Errorf("%s: %d, want %d", path, res.StatusCode, want)
		}
	}
	bare := newHarness(t, Config{}, testOptions(), &staticSource{})
	if res := bare.get("/api/inventory?account=a"); res.StatusCode != http.StatusNotImplemented {
		t.Errorf("a source without inventories: %d, want 501", res.StatusCode)
	}
}

func TestUnknownEndpointsAndMethods(t *testing.T) {
	h := newHarness(t, Config{}, testOptions(), nil)
	if res := h.get("/api/nope"); res.StatusCode != http.StatusNotFound || !strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		t.Errorf("/api/nope: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	res := h.request(http.MethodPost, "/api/status", nil, nil)
	if res.StatusCode != http.StatusMethodNotAllowed || res.Header.Get("Allow") == "" {
		t.Errorf("POST /api/status: %d, Allow %q", res.StatusCode, res.Header.Get("Allow"))
	}
	if res := h.request(http.MethodDelete, "/api/layout", nil, nil); res.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("DELETE /api/layout: %d", res.StatusCode)
	}
	if res := h.request(http.MethodPost, "/", nil, nil); res.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /: %d", res.StatusCode)
	}
}

func TestAssetsAreServed(t *testing.T) {
	h := newHarness(t, Config{}, testOptions(), nil)
	cases := []struct{ path, ctype, contains string }{
		{"/", "text/html", "<title>"},
		{"/app.js", "text/javascript", "PANELS"},
		{"/panels/index.js", "text/javascript", "export const PANELS"},
		{"/style.css", "text/css", "--stone"},
	}
	for _, c := range cases {
		res := h.get(c.path)
		if res.StatusCode != http.StatusOK {
			t.Errorf("%s: %d", c.path, res.StatusCode)
			continue
		}
		if !strings.HasPrefix(res.Header.Get("Content-Type"), c.ctype) {
			t.Errorf("%s: Content-Type %q, want %s", c.path, res.Header.Get("Content-Type"), c.ctype)
		}
		if res.Header.Get("Content-Security-Policy") == "" || res.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: security headers missing", c.path)
		}
		if !strings.Contains(bodyText(t, res), c.contains) {
			t.Errorf("%s does not contain %q", c.path, c.contains)
		}
	}
}

func TestEveryRegisteredPanelIsEmbedded(t *testing.T) {
	index, err := webFiles.ReadFile("web/panels/index.js")
	if err != nil {
		t.Fatal(err)
	}
	imports := regexp.MustCompile(`import \w+ from '\./([\w-]+\.js)'`).FindAllSubmatch(index, -1)
	if len(imports) < 10 {
		t.Fatalf("index.js imports %d panel modules, want the six live panels and four placeholders", len(imports))
	}
	for _, m := range imports {
		if _, err := webFiles.ReadFile("web/panels/" + string(m[1])); err != nil {
			t.Errorf("panels/index.js imports %s, which is not embedded", m[1])
		}
	}
}

func TestNoAssetHoldsARawControlCharacter(t *testing.T) {
	entries := []string{"web/app.js", "web/customize.js", "web/layout.js", "web/settings.js", "web/store.js", "web/util.js", "web/index.html", "web/style.css"}
	panels, _ := webFiles.ReadDir("web/panels")
	for _, p := range panels {
		entries = append(entries, "web/panels/"+p.Name())
	}
	for _, name := range entries {
		data, err := webFiles.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for i, b := range data {
			if b < 0x20 && b != '\n' && b != '\r' && b != '\t' {
				t.Errorf("%s has control byte 0x%02x at offset %d", name, b, i)
				break
			}
		}
	}
}
