package controlpanel

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/fleet"
)

// GET /api/watchdog reads the stall watchdog's live mode, POST /api/watchdog changes it without
// a fleet restart, a bad mode is a 400 that changes nothing, and a Source with no watchdog is a
// 501 -- the same shape as every other optional control on this API.
func TestWatchdogModeEndpoint(t *testing.T) {
	m := fleet.New(fleet.Config{WSURL: "ws://localhost/"})
	defer m.CloseAll()
	cfg := fleet.DefaultWatchdogConfig()
	cfg.DumpDir = ""
	m.StartWatchdog(cfg)
	h := newHarness(t, Config{}, testOptions(), NewFleetSource(m, []string{"a"}, time.Second))
	hdr := http.Header{"Content-Type": {"application/json"}}

	var info fleet.WatchdogInfo
	decode(t, h.get("/api/watchdog"), &info)
	if info.Mode != "observe" || !info.Running || !info.Configured || len(info.Modes) != 4 {
		t.Fatalf("GET = %+v, want observe (the default), running", info)
	}

	res := h.request(http.MethodPost, "/api/watchdog", strings.NewReader(`{"mode":"act"}`), hdr)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("POST act: status %d, body %s", res.StatusCode, bodyText(t, res))
	}
	var body watchdogModeBody
	decode(t, res, &body)
	if !body.OK || body.Previous != "observe" || body.Watchdog.Mode != "act" {
		t.Fatalf("POST act body = %+v", body)
	}
	decode(t, h.get("/api/watchdog"), &info)
	if info.Mode != "act" {
		t.Fatalf("GET after the switch = %q", info.Mode)
	}

	res = h.request(http.MethodPost, "/api/watchdog", strings.NewReader(`{"mode":"restart-everything"}`), hdr)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("a bad mode: status %d, want 400", res.StatusCode)
	}
	decode(t, h.get("/api/watchdog"), &info)
	if info.Mode != "act" {
		t.Fatalf("a refused switch changed the mode to %q", info.Mode)
	}
	if res := h.request(http.MethodPost, "/api/watchdog", strings.NewReader(`{"mode":"observe"}`), nil); res.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("POST without a JSON content type: status %d, want 415", res.StatusCode)
	}
	if res := h.request(http.MethodDelete, "/api/watchdog", nil, nil); res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE: status %d, want 405", res.StatusCode)
	}

	h2 := newHarness(t, Config{}, testOptions(), &staticSource{})
	if res := h2.get("/api/watchdog"); res.StatusCode != http.StatusNotImplemented {
		t.Fatalf("GET with no WatchdogControl: status %d, want 501", res.StatusCode)
	}
}

// A cross-origin POST is refused by the same guard every other write goes through.
func TestWatchdogModeEndpointRefusesCrossOrigin(t *testing.T) {
	m := fleet.New(fleet.Config{WSURL: "ws://localhost/"})
	defer m.CloseAll()
	cfg := fleet.DefaultWatchdogConfig()
	cfg.DumpDir = ""
	m.StartWatchdog(cfg)
	h := newHarness(t, Config{}, testOptions(), NewFleetSource(m, []string{"a"}, time.Second))
	res := h.request(http.MethodPost, "/api/watchdog", strings.NewReader(`{"mode":"act"}`),
		http.Header{"Content-Type": {"application/json"}, "Origin": {"https://evil.example"}})
	if res.StatusCode == http.StatusOK {
		t.Fatal("a cross-origin POST switched the watchdog")
	}
	if got := m.WatchdogInfo().Mode; got != "observe" {
		t.Fatalf("mode = %q after a refused cross-origin POST", got)
	}
}
