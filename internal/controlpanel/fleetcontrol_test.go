package controlpanel

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestFleetStopAll checks the whole-fleet Stop endpoint: it answers before triggering
// Config.OnStopAll (the response must reach the client before any shutdown starts — see
// handleFleetStopAll's own doc comment), refuses GET, and answers 501 when a build has
// not wired OnStopAll up at all (e.g. every other harness in this package).
func TestFleetStopAll(t *testing.T) {
	called := make(chan struct{}, 1)
	cfg := Config{OnStopAll: func() { called <- struct{}{} }}
	h := newHarness(t, cfg, testOptions(), &staticSource{})

	res := h.request(http.MethodPost, "/api/fleet/stop-all", nil, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d, body %s", res.StatusCode, bodyText(t, res))
	}
	var body fleetActionBody
	decode(t, res, &body)
	if !body.OK {
		t.Errorf("ok=false in body: %+v", body)
	}
	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("OnStopAll was not called")
	}

	if res := h.get("/api/fleet/stop-all"); res.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET /api/fleet/stop-all: %d, want 405", res.StatusCode)
	}

	h2 := newHarness(t, Config{}, testOptions(), &staticSource{})
	res = h2.request(http.MethodPost, "/api/fleet/stop-all", nil, nil)
	if res.StatusCode != http.StatusNotImplemented {
		t.Errorf("no OnStopAll wired: %d, want 501", res.StatusCode)
	}
}

// TestFleetRestartAll checks the whole-fleet Restart endpoint: a successful launch
// answers 200 and calls OnRestartAll, a launch failure surfaces the error as 502 rather
// than silently succeeding (see Config.OnRestartAll's own doc comment on why the fleet
// must NOT be shut down when the relaunch could not even be triggered), and a build with
// no OnRestartAll wired answers 501.
func TestFleetRestartAll(t *testing.T) {
	var launched int32
	cfg := Config{OnRestartAll: func() error { atomic.AddInt32(&launched, 1); return nil }}
	h := newHarness(t, cfg, testOptions(), &staticSource{})

	res := h.request(http.MethodPost, "/api/fleet/restart-all", nil, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d, body %s", res.StatusCode, bodyText(t, res))
	}
	var body fleetActionBody
	decode(t, res, &body)
	if !body.OK {
		t.Errorf("ok=false in body: %+v", body)
	}
	if atomic.LoadInt32(&launched) != 1 {
		t.Errorf("OnRestartAll called %d times, want 1", launched)
	}

	if res := h.get("/api/fleet/restart-all"); res.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET /api/fleet/restart-all: %d, want 405", res.StatusCode)
	}

	cfg2 := Config{OnRestartAll: func() error { return fmt.Errorf("restart-fleet.bat not found at C:\\fake\\restart-fleet.bat") }}
	h2 := newHarness(t, cfg2, testOptions(), &staticSource{})
	res = h2.request(http.MethodPost, "/api/fleet/restart-all", nil, nil)
	if res.StatusCode != http.StatusBadGateway {
		t.Errorf("failed launch: %d, want 502", res.StatusCode)
	}
	if got := bodyText(t, res); !strings.Contains(got, "restart-fleet.bat not found") {
		t.Errorf("body %q does not carry the launch error", got)
	}

	h3 := newHarness(t, Config{}, testOptions(), &staticSource{})
	res = h3.request(http.MethodPost, "/api/fleet/restart-all", nil, nil)
	if res.StatusCode != http.StatusNotImplemented {
		t.Errorf("no OnRestartAll wired: %d, want 501", res.StatusCode)
	}
}

// TestMetaReportsFleetControl checks /api/meta's fleetStop/fleetRestart flags — the
// header buttons enable only once these report true, the same gating pattern
// TestMetaReportsBotControlAndScripts already covers for BotControl.
func TestMetaReportsFleetControl(t *testing.T) {
	h := newHarness(t, Config{}, testOptions(), &staticSource{})
	var m metaBody
	decode(t, h.get("/api/meta"), &m)
	if m.FleetStop || m.FleetRestart {
		t.Error("meta reports fleet control for a build that has neither wired")
	}

	h2 := newHarness(t, Config{OnStopAll: func() {}, OnRestartAll: func() error { return nil }}, testOptions(), &staticSource{})
	decode(t, h2.get("/api/meta"), &m)
	if !m.FleetStop || !m.FleetRestart {
		t.Error("meta does not report fleet control for a build with both wired")
	}
}
