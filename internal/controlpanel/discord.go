package controlpanel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// discordPayload is Discord's documented incoming-webhook request body — see
// https://discord.com/developers/docs/resources/webhook#execute-webhook. Only `content` (a
// plain message) is used; that is plenty for an operator alert and needs no extra dependency
// beyond net/http and encoding/json, both already imported all over this package.
type discordPayload struct {
	Content string `json:"content"`
}

// DiscordClient posts alert/summary text to one Discord incoming webhook.
//
// Optional, default-off, same as every other feature added tonight (see cmd/controlpanel's
// -pprof/-maptiles flags): a zero-value DiscordClient (WebhookURL empty) makes every method a
// safe no-op, so a caller can construct and use one unconditionally and nothing about a
// deployment that never configures a webhook changes at all. A broken or unreachable webhook
// is likewise never allowed to affect the fleet itself — send/alert log an error and move on,
// never return one to a caller that might treat it as fatal.
type DiscordClient struct {
	// WebhookURL is the operator-supplied Discord webhook URL (see cmd/controlpanel's
	// -discord-webhook-env flag). Empty disables this client entirely.
	WebhookURL string
	// HTTPClient, when nil, defaults to a short-timeout http.Client built on first use — a
	// webhook post must never be allowed to hang this process.
	HTTPClient *http.Client
	// Now, when nil, defaults to time.Now. Tests override it to control the dedup window in
	// alert without a real sleep.
	Now func() time.Time
	// pollInterval/summaryInterval, when zero, default to discordPollInterval and
	// discordSummaryInterval below. Unexported: only a test needs Watch to run faster than
	// once every 15s.
	pollInterval    time.Duration
	summaryInterval time.Duration

	mu       sync.Mutex
	lastSent map[string]time.Time
}

// Enabled reports whether d will actually post anything. A nil *DiscordClient is also
// disabled, so callers never need a separate nil check before using one.
func (d *DiscordClient) Enabled() bool {
	return d != nil && d.WebhookURL != ""
}

func (d *DiscordClient) httpClient() *http.Client {
	if d.HTTPClient != nil {
		return d.HTTPClient
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (d *DiscordClient) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// send posts content to the webhook right away. Best-effort: errors and non-2xx responses are
// logged, never returned — see the type's own doc comment on why a broken webhook must never
// propagate into the fleet's own control flow.
func (d *DiscordClient) send(content string) {
	if !d.Enabled() {
		return
	}
	body, err := json.Marshal(discordPayload{Content: content})
	if err != nil {
		log.Printf("controlpanel: discord webhook: encoding payload: %v", err)
		return
	}
	req, err := http.NewRequest(http.MethodPost, d.WebhookURL, bytes.NewReader(body))
	if err != nil {
		log.Printf("controlpanel: discord webhook: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.httpClient().Do(req)
	if err != nil {
		log.Printf("controlpanel: discord webhook: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		log.Printf("controlpanel: discord webhook: unexpected status %d", resp.StatusCode)
	}
}

// alert sends content under key, at most once every d in the window — so, e.g., one flaky
// account's STRANDED line repeating on every poll cannot flood the channel: the first one
// posts, the rest are dropped silently until the window passes.
func (d *DiscordClient) alert(key, content string, every time.Duration) {
	if !d.Enabled() {
		return
	}
	d.mu.Lock()
	if d.lastSent == nil {
		d.lastSent = make(map[string]time.Time)
	}
	now := d.now()
	if last, ok := d.lastSent[key]; ok && now.Sub(last) < every {
		d.mu.Unlock()
		return
	}
	d.lastSent[key] = now
	d.mu.Unlock()
	d.send(content)
}

// AlertPanic posts a best-effort notice that this process is about to crash. Intended to be
// called from a deferred recover() in cmd/controlpanel's main() immediately before
// re-panicking, so an operator watching Discord learns about a crash without needing to be
// staring at the console when it happens. Synchronous and best-effort by design (see send) —
// it does not retry and does not block process exit meaningfully longer than one HTTP POST.
func (d *DiscordClient) AlertPanic(recovered interface{}) {
	if !d.Enabled() {
		return
	}
	d.send(fmt.Sprintf(":rotating_light: controlpanel process is crashing: %v", recovered))
}

// Alert dedup windows. Unexported consts, not flags: an operator who wants this tuned can ask
// for a flag later, but these are reasonable enough that no deployment tonight needs to
// choose them.
const (
	discordAccountAlertWindow = 15 * time.Minute
	discordPollInterval       = 15 * time.Second
	discordSummaryInterval    = time.Hour
)

// Watch runs until ctx is done, polling src for account deaths and STRANDED escalations (see
// freshstartrunner's own "needs an operator" log lines, which surface here through
// AccountStatus.Activity — the most recent RecentLog line) and posting an hourly summary. A
// no-op that returns immediately when d is disabled, so a caller (cmd/controlpanel's main) can
// always start this in its own goroutine unconditionally, exactly like every other opt-in
// feature added tonight.
func (d *DiscordClient) Watch(ctx context.Context, src Source) {
	if !d.Enabled() {
		return
	}
	d.send(":green_circle: control panel started — Discord alerts are on")

	seen := make(map[string]discordAccountState)

	pollEvery := d.pollInterval
	if pollEvery <= 0 {
		pollEvery = discordPollInterval
	}
	summaryEvery := d.summaryInterval
	if summaryEvery <= 0 {
		summaryEvery = discordSummaryInterval
	}
	poll := time.NewTicker(pollEvery)
	defer poll.Stop()
	summary := time.NewTicker(summaryEvery)
	defer summary.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-poll.C:
			d.checkAccounts(src, seen)
		case <-summary.C:
			d.postSummary(src)
		}
	}
}

// discordAccountState is the per-account bit of state Watch diffs each poll against, to alert
// only on a genuine change (a death just observed, a new/different STRANDED line) rather than
// re-alerting every single poll for as long as the condition holds.
type discordAccountState struct {
	dead     bool
	activity string
	// needsAttention is the stall watchdog having given up on the account (fleet/watchdog.go).
	needsAttention bool
}

func (d *DiscordClient) checkAccounts(src Source, seen map[string]discordAccountState) {
	accounts := src.Accounts(nil)
	for _, a := range accounts {
		prev, ok := seen[a.Name]
		if a.Dead && (!ok || !prev.dead) {
			d.alert("death:"+a.Name, fmt.Sprintf(":skull: **%s** has died", a.Name), discordAccountAlertWindow)
		}
		if strings.Contains(a.Activity, "STRANDED") && a.Activity != prev.activity {
			d.alert("stranded:"+a.Name, fmt.Sprintf(":sos: **%s** needs an operator — %s", a.Name, a.Activity), discordAccountAlertWindow)
		}
		needs := a.Watchdog != nil && a.Watchdog.State == "needs_attention"
		if needs && (!ok || !prev.needsAttention) {
			d.alert("watchdog:"+a.Name, fmt.Sprintf(":warning: **%s** stalled and the watchdog gave up — needs an operator: %s", a.Name, a.Watchdog.Detail), discordAccountAlertWindow)
		}
		seen[a.Name] = discordAccountState{dead: a.Dead, activity: a.Activity, needsAttention: needs}
	}
}

func (d *DiscordClient) postSummary(src Source) {
	accounts := src.Accounts(nil)
	online := 0
	for _, a := range accounts {
		if a.State == StateIngame {
			online++
		}
	}
	d.send(fmt.Sprintf(":bar_chart: hourly summary — %d/%d accounts online", online, len(accounts)))
}
