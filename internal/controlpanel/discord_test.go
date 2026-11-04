package controlpanel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestDiscordSendPostsExpectedShape proves the request shape against a local httptest.Server
// standing in for Discord: POST, JSON content type, and Discord's documented
// {"content": "..."} incoming-webhook body — see discordPayload's own doc comment. This
// repo has no real Discord webhook to test against tonight, so this is the proof required
// instead.
func TestDiscordSendPostsExpectedShape(t *testing.T) {
	var got discordPayload
	var calls int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.Method != http.MethodPost {
			t.Errorf("method %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type %q, want application/json", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decoding body: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	d := &DiscordClient{WebhookURL: ts.URL}
	d.send("hello world")

	if got.Content != "hello world" {
		t.Errorf("content %q, want %q", got.Content, "hello world")
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Errorf("%d requests, want 1", n)
	}
}

// TestDiscordDisabledSendsNothing proves the default-off contract: a DiscordClient with no
// WebhookURL configured (the default for every deployment that never sets
// -discord-webhook-env) must never make an HTTP request, from send or alert or Watch.
func TestDiscordDisabledSendsNothing(t *testing.T) {
	called := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer ts.Close()

	var d *DiscordClient // nil client: must behave exactly like a disabled one
	if d.Enabled() {
		t.Fatal("nil client reports Enabled")
	}
	d.send("should not send")
	d.alert("k", "should not send", time.Minute)
	d.AlertPanic("should not send")

	enabled := &DiscordClient{} // zero value, no WebhookURL
	if enabled.Enabled() {
		t.Fatal("client with empty WebhookURL reports Enabled")
	}
	enabled.send("should not send either")

	if called {
		t.Error("a disabled DiscordClient made an HTTP request")
	}
}

// TestDiscordAlertDedupes proves the rate-limit/dedup behavior the task calls for: repeated
// alerts under the same key within the window collapse to one request, so e.g. one flaky
// account's STRANDED line repeating every poll cannot flood the channel.
func TestDiscordAlertDedupes(t *testing.T) {
	var calls int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	now := time.Now()
	d := &DiscordClient{WebhookURL: ts.URL, Now: func() time.Time { return now }}

	d.alert("death:foo", "died", time.Minute)
	d.alert("death:foo", "died again", time.Minute)
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Errorf("%d requests within the window, want 1 (deduped)", n)
	}

	// A different key is never deduped against the first.
	d.alert("death:bar", "a different account died", time.Minute)
	if n := atomic.LoadInt32(&calls); n != 2 {
		t.Errorf("%d requests, want 2 (distinct key)", n)
	}

	now = now.Add(2 * time.Minute)
	d.alert("death:foo", "died a third time", time.Minute)
	if n := atomic.LoadInt32(&calls); n != 3 {
		t.Errorf("%d requests after the window passed, want 3", n)
	}
}

// TestDiscordWatchAlertsOnDeathAndStranded exercises Watch end to end against a fakeSource
// (already used by this package's other tests), confirming it posts a startup notice, a death
// alert once a death is observed, and a STRANDED alert from AccountStatus.Activity — without
// re-alerting on an unchanged STRANDED line.
func TestDiscordWatchAlertsOnDeathAndStranded(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p discordPayload
		_ = json.NewDecoder(r.Body).Decode(&p)
		mu.Lock()
		bodies = append(bodies, p.Content)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	src := &discordFakeSource{accounts: []AccountStatus{
		{Name: "acct1", State: StateIngame, Dead: false, Activity: "chopping trees"},
	}}
	d := &DiscordClient{WebhookURL: ts.URL, pollInterval: 10 * time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		d.Watch(ctx, src)
	}()
	// Watch must end before this test's server closes, or a late post outlives the test.
	defer func() {
		cancel()
		<-done
	}()

	// Every step waits on what the watcher actually did, never on a fixed sleep: Watch's
	// first act is a synchronous webhook POST, and each alert is another, so under heavy CPU
	// load one poll can take longer than any fixed window this test could pick. The 30 ms
	// sleeps this replaced let the test cancel before a single poll had run.
	posted := func(substr string) func() bool {
		return func() bool {
			mu.Lock()
			defer mu.Unlock()
			for _, b := range bodies {
				if strings.Contains(b, substr) {
					return true
				}
			}
			return false
		}
	}
	// A poll that saw the account alive first, so the death below is a real alive-to-dead
	// transition rather than the first sighting of an already-dead account.
	waitFor(t, "a poll of the living account", func() bool { return src.polled() > 0 })
	src.set([]AccountStatus{{Name: "acct1", State: StateIngame, Dead: true, Activity: "chopping trees"}})
	waitFor(t, "the death alert", posted("has died"))
	src.set([]AccountStatus{{Name: "acct1", State: StateError, Dead: true, Activity: "STRANDED: no path found"}})
	waitFor(t, "the STRANDED alert", posted("STRANDED"))
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	var sawDeath, sawStranded bool
	for _, b := range bodies {
		if strings.Contains(b, "has died") {
			sawDeath = true
		}
		if strings.Contains(b, "STRANDED") {
			sawStranded = true
		}
	}
	if !sawDeath {
		t.Errorf("no death alert among %d messages: %v", len(bodies), bodies)
	}
	if !sawStranded {
		t.Errorf("no STRANDED alert among %d messages: %v", len(bodies), bodies)
	}
}

// discordFakeSource is a minimal Source (see source.go's interface) just for TestDiscordWatch*
// above — internal/controlpanel already has a richer FakeSource in fake.go, but that one
// simulates a whole live-feeling fleet on a timer; this test wants full, immediate control
// over exactly what one poll of Accounts() returns.
type discordFakeSource struct {
	mu       sync.Mutex
	accounts []AccountStatus
	polls    int
}

// polled reports how many times Accounts has been read.
func (s *discordFakeSource) polled() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.polls
}

func (s *discordFakeSource) set(a []AccountStatus) {
	s.mu.Lock()
	s.accounts = a
	s.mu.Unlock()
}

func (s *discordFakeSource) Accounts(dst []AccountStatus) []AccountStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.polls++
	return append(dst, s.accounts...)
}
func (s *discordFakeSource) Logins() LoginStatus              { return LoginStatus{} }
func (s *discordFakeSource) MemStats() MemStats               { return MemStats{} }
func (s *discordFakeSource) Subscribe(ch chan<- Event) func() { return func() {} }
