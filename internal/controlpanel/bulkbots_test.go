package controlpanel

import (
	"fmt"
	"net/http"
	"testing"
)

func TestBulkScriptsUsesEntireFleetAndPreservesSettings(t *testing.T) {
	src := &staticSource{}
	for i := 0; i < 266; i++ {
		src.accounts = append(src.accounts, AccountStatus{Name: fmt.Sprint(i), State: StateIngame, Bot: "ProgressiveFisher", BotState: "stopped", Settings: map[string]interface{}{"marker": i}})
	}
	src.accounts = append(src.accounts, AccountStatus{Name: "offline", State: StateReconnecting, Bot: "ProgressiveFisher"})
	h := newHarness(t, Config{}, testOptions(), botControlSource{src})
	var body bulkBotBody
	decode(t, h.request(http.MethodPost, "/api/bot/start-all", nil, nil), &body)
	if !body.OK || body.Changed != 266 || body.Skipped != 1 || len(src.starts) != 266 {
		t.Fatalf("result %+v, starts %d", body, len(src.starts))
	}
	for i, call := range src.starts {
		if call.Script != "ProgressiveFisher" || call.Settings["marker"] != i {
			t.Fatalf("start %d lost settings: %+v", i, call)
		}
		src.accounts[i].BotState = "running"
	}
	decode(t, h.request(http.MethodPost, "/api/bot/start-all", nil, nil), &body)
	if body.Changed != 0 || len(src.starts) != 266 {
		t.Fatal("started a second script on running accounts")
	}
	decode(t, h.request(http.MethodPost, "/api/bot/stop-all", nil, nil), &body)
	if body.Changed != 266 || len(src.stops) != 266 {
		t.Fatalf("stop result %+v", body)
	}
	if r := h.get("/api/bot/stop-all"); r.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET: %d", r.StatusCode)
	}
}

func TestBulkScriptsReportsFailures(t *testing.T) {
	src := &staticSource{accounts: []AccountStatus{{Name: "failed", State: StateIngame, Bot: "ProgressiveFisher", BotState: "error"}}, startErr: fmt.Errorf("fixture failure")}
	h := newHarness(t, Config{}, testOptions(), botControlSource{src})
	var body bulkBotBody
	decode(t, h.request(http.MethodPost, "/api/bot/start-all", nil, nil), &body)
	if body.OK || body.Errors["failed"] != "fixture failure" {
		t.Fatalf("result %+v", body)
	}
}
