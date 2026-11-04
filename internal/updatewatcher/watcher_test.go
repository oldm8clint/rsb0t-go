package updatewatcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func commitJSON(sha, path, msg string, date time.Time) string {
	c := commit{SHA: sha, URL: "https://github.com/example/repo/commit/" + sha}
	c.Commit.Message = msg
	c.Commit.Author.Date = date
	data, _ := json.Marshal(c)
	return string(data)
}

// TestEndpointBuildsPathAndSinceQuery is a direct unit test of the real (non-test-server)
// URL (*Watcher).endpoint builds against github.com — the query CheckPath actually sends
// on a real run, exercised here without a network call since the other CheckPath tests
// below all override Watcher.URL (which bypasses path/since entirely, by design, so a
// test server can serve a fixed response regardless of what would have been queried).
func TestEndpointBuildsPathAndSinceQuery(t *testing.T) {
	w := &Watcher{Owner: "oldm8clint", Repo: "rsb0t-go"}
	got := w.endpoint("internal/bot/fisher.go", time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC))
	want := "https://api.github.com/repos/oldm8clint/rsb0t-go/commits?path=internal%2Fbot%2Ffisher.go&per_page=1&since=2026-09-15T00%3A00%3A00Z"
	if got != want {
		t.Errorf("endpoint = %q, want %q", got, want)
	}
	// Defaults fill in when Owner/Repo are empty.
	def := (&Watcher{}).endpoint("", time.Time{})
	if def != "https://api.github.com/repos/oldm8clint/rsb0t-go/commits?per_page=1" {
		t.Errorf("default endpoint = %q", def)
	}
}

func TestCheckPathSendsAuthHeaderAndParsesResponse(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if got := r.Header.Get("User-Agent"); got == "" {
			t.Error("missing User-Agent")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[" + commitJSON("abc123", "internal/bot/fisher.go", "fix fisher\n\nlonger body", time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)) + "]"))
	}))
	defer server.Close()

	w := &Watcher{URL: server.URL, Token: "tok123", Client: server.Client()}
	u, err := w.CheckPath(context.Background(), "internal/bot/fisher.go", time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if u == nil || u.SHA != "abc123" {
		t.Fatalf("update = %#v", u)
	}
	if u.Message != "fix fisher" {
		t.Errorf("message = %q, want first line only", u.Message)
	}
	if gotAuth != "Bearer tok123" {
		t.Errorf("Authorization header = %q", gotAuth)
	}
}

func TestCheckPathNoAuthHeaderWithoutToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("unexpected Authorization header: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	w := &Watcher{URL: server.URL, Client: server.Client()}
	u, err := w.CheckPath(context.Background(), "some/path", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if u != nil {
		t.Fatalf("update = %#v, want nil for an empty commit list", u)
	}
}

func TestCheckPathRejectsBadStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"rate limited"}`))
	}))
	defer server.Close()

	_, err := (&Watcher{URL: server.URL, Client: server.Client()}).CheckPath(context.Background(), "x", time.Time{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCheckPathHonorsContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (&Watcher{URL: server.URL, Client: server.Client()}).CheckPath(ctx, "x", time.Time{})
	if err == nil {
		t.Fatal("expected cancellation error")
	}
}

// pathServer builds an httptest server that answers CheckPath calls per repo path from a
// fixed table, so a Manager test can simulate several scripts with several paths each
// without hitting the real network.
func pathServer(t *testing.T, byPath map[string]*Update) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Query().Get("path")
		u, ok := byPath[p]
		w.Header().Set("Content-Type", "application/json")
		if !ok || u == nil {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		_, _ = w.Write([]byte("[" + commitJSON(u.SHA, p, u.Message, u.Time) + "]"))
	}))
}

func TestManagerCheckAllPicksNewestAcrossPaths(t *testing.T) {
	older := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	server := pathServer(t, map[string]*Update{
		"internal/bot/fisher.go":           {SHA: "aaa", Message: "tweak fisher", Time: older},
		"internal/scripts/fisherrunner":    {SHA: "bbb", Message: "tweak fisherrunner", Time: newer},
		"internal/bot/barbarian.go":        nil, // up to date
		"internal/scripts/barbarianrunner": nil,
	})
	defer server.Close()

	w := &Watcher{URL: server.URL, Client: server.Client()}
	mgr := NewManager(w, "buildsha000", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), []ScriptSource{
		{Script: "ProgressiveFisher", Paths: []string{"internal/bot/fisher.go", "internal/scripts/fisherrunner"}},
		{Script: "BarbarianAgility", Paths: []string{"internal/bot/barbarian.go", "internal/scripts/barbarianrunner"}},
	})
	mgr.CheckAll(context.Background())

	snap := mgr.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("snapshot len = %d, want 2", len(snap))
	}
	// Sorted by script name: BarbarianAgility before ProgressiveFisher.
	if snap[0].Script != "BarbarianAgility" || snap[0].Update != nil {
		t.Errorf("BarbarianAgility status = %+v, want up to date", snap[0])
	}
	if snap[1].Script != "ProgressiveFisher" || snap[1].Update == nil || snap[1].Update.SHA != "bbb" {
		t.Errorf("ProgressiveFisher status = %+v, want newest (bbb) across its paths", snap[1])
	}
}

func TestManagerCheckAllKeepsPreviousGoodStatusOnError(t *testing.T) {
	newer := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	good := pathServer(t, map[string]*Update{"internal/bot/fisher.go": {SHA: "bbb", Message: "fix", Time: newer}})
	defer good.Close()

	w := &Watcher{URL: good.URL, Client: good.Client()}
	mgr := NewManager(w, "buildsha000", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), []ScriptSource{
		{Script: "ProgressiveFisher", Paths: []string{"internal/bot/fisher.go"}},
	})
	mgr.CheckAll(context.Background())
	first := mgr.Snapshot()
	if len(first) != 1 || first[0].Update == nil || first[0].Update.SHA != "bbb" {
		t.Fatalf("first check = %+v", first)
	}

	// Point the same manager's Watcher at a server that always errors, and check again —
	// the previously found update should survive, not vanish, since a live fleet
	// shouldn't have its dashboard flicker to "unknown" over one flaky GitHub poll.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()
	mgr.Watcher = &Watcher{URL: bad.URL, Client: bad.Client()}
	mgr.CheckAll(context.Background())

	second := mgr.Snapshot()
	if len(second) != 1 || second[0].Update == nil || second[0].Update.SHA != "bbb" {
		t.Fatalf("second check lost the previous update: %+v", second)
	}
	if second[0].Err == "" {
		t.Error("expected the error to be surfaced even though Update was kept")
	}
}

func TestManagerRunStopsOnContextCancellation(t *testing.T) {
	server := pathServer(t, map[string]*Update{"p": nil})
	defer server.Close()
	w := &Watcher{URL: server.URL, Client: server.Client()}
	mgr := NewManager(w, "sha", time.Now(), []ScriptSource{{Script: "S", Paths: []string{"p"}}})
	mgr.Interval = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { mgr.Run(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop after context cancellation")
	}
}
