package controlpanel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"rsb0t-go/internal/updatewatcher"
)

// TestMetaHidesUpdateChecksWithoutConfig covers the default, unconfigured case (no
// BuildSHA, no UpdateCheck.Token/Scripts — the state of a plain `-fake` run, or any
// build with no VCS stamp): /api/meta reports updateChecks:false and an empty buildSha,
// and /api/scriptversions answers available:false rather than an error.
func TestMetaHidesUpdateChecksWithoutConfig(t *testing.T) {
	h := newHarness(t, Config{}, testOptions(), &staticSource{})
	var m metaBody
	decode(t, h.get("/api/meta"), &m)
	if m.UpdateChecks {
		t.Error("meta reports updateChecks with no UpdateCheck configured")
	}
	if m.BuildSHA != "" {
		t.Errorf("buildSha = %q, want empty", m.BuildSHA)
	}

	var sv scriptVersionsBody
	decode(t, h.get("/api/scriptversions"), &sv)
	if sv.Available {
		t.Error("scriptversions reports available with no UpdateCheck configured")
	}
	if len(sv.Scripts) != 0 {
		t.Errorf("scripts = %v, want none", sv.Scripts)
	}
}

// TestMetaReportsBuildSHAAndUpdateChecksWhenConfigured covers a fully-configured build
// (BuildSHA/BuildCommitTime set, UpdateCheck.Token and Scripts set) — the state a real
// `go build` inside a git checkout produces.
func TestMetaReportsBuildSHAAndUpdateChecksWhenConfigured(t *testing.T) {
	cfg := Config{
		BuildSHA:        "deadbeef",
		BuildCommitTime: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		UpdateCheck: UpdateCheckConfig{
			Token:   "tok",
			Scripts: []updatewatcher.ScriptSource{{Script: "ProgressiveFisher", Paths: []string{"internal/bot/fisher.go"}}},
		},
	}
	h := newHarness(t, cfg, testOptions(), &staticSource{})
	var m metaBody
	decode(t, h.get("/api/meta"), &m)
	if !m.UpdateChecks {
		t.Error("meta does not report updateChecks for a fully-configured UpdateCheck")
	}
	if m.BuildSHA != "deadbeef" {
		t.Errorf("buildSha = %q, want deadbeef", m.BuildSHA)
	}
}

// TestScriptVersionsReportsUpdatesAndStaleCount exercises the full path end to end: a
// mocked GitHub commits endpoint (see updatewatcher's own tests for why a path-dispatch
// httptest server stands in for the real API), one script with a newer commit and one
// without, checked via the Server's own updatewatcher.Manager, then read back through
// GET /api/scriptversions the way the dashboard panel does.
func TestScriptVersionsReportsUpdatesAndStaleCount(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("path") {
		case "internal/bot/fisher.go":
			_, _ = w.Write([]byte(`[{"sha":"newsha","html_url":"https://github.com/oldm8clint/rsb0t-go/commit/newsha","commit":{"message":"fix fisher hand-off","author":{"date":"2026-09-15T00:00:00Z"}}}]`))
		default:
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	defer mock.Close()

	cfg := Config{
		BuildSHA:        "buildsha0",
		BuildCommitTime: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		UpdateCheck: UpdateCheckConfig{
			Token: "tok",
			Scripts: []updatewatcher.ScriptSource{
				{Script: "ProgressiveFisher", Paths: []string{"internal/bot/fisher.go"}},
				{Script: "BarbarianAgility", Paths: []string{"internal/bot/barbarian.go"}},
			},
		},
	}
	h := newHarness(t, cfg, testOptions(), &staticSource{})
	if h.srv.updates == nil {
		t.Fatal("Server.updates is nil for a fully-configured UpdateCheck")
	}
	// Redirect the manager's Watcher at the mock server instead of the real GitHub API,
	// then run one check synchronously (Run's own background loop was never started —
	// newHarness builds the Server via newServer directly, not Start — so there is no
	// race with this test's own direct CheckAll call).
	h.srv.updates.Watcher = &updatewatcher.Watcher{URL: mock.URL, Client: mock.Client()}
	h.srv.updates.CheckAll(context.Background())

	var sv scriptVersionsBody
	decode(t, h.get("/api/scriptversions"), &sv)
	if !sv.Available {
		t.Fatal("scriptversions reports unavailable for a fully-configured, just-checked UpdateCheck")
	}
	if sv.BuildSHA != "buildsha0" {
		t.Errorf("buildSha = %q", sv.BuildSHA)
	}
	if sv.StaleCount != 1 {
		t.Errorf("staleCount = %d, want 1 (only ProgressiveFisher has an update)", sv.StaleCount)
	}
	if len(sv.Scripts) != 2 {
		t.Fatalf("scripts = %+v, want 2", sv.Scripts)
	}
	byName := map[string]updatewatcher.ScriptStatus{}
	for _, s := range sv.Scripts {
		byName[s.Script] = s
	}
	fisher, ok := byName["ProgressiveFisher"]
	if !ok || fisher.Update == nil || fisher.Update.SHA != "newsha" {
		t.Errorf("ProgressiveFisher status = %+v, want an update pointing at newsha", fisher)
	}
	barbarian, ok := byName["BarbarianAgility"]
	if !ok || barbarian.Update != nil {
		t.Errorf("BarbarianAgility status = %+v, want up to date", barbarian)
	}
}

// TestScriptVersionsRefusesNonGET matches every other read-only endpoint in this
// package (see handlePOI/handleWalls's own tests) — POST is refused, not silently
// treated as GET.
func TestScriptVersionsRefusesNonGET(t *testing.T) {
	h := newHarness(t, Config{}, testOptions(), &staticSource{})
	res := h.request(http.MethodPost, "/api/scriptversions", nil, nil)
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /api/scriptversions: %d, want 405", res.StatusCode)
	}
}
