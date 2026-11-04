// Package updatewatcher checks a GitHub repo's commit history — scoped to individual
// scripts' own source paths, via the commits API's own path= filter — for whether newer
// code exists than what a running binary was built from.
//
// This package only ever ANSWERS that question. It cannot and does not apply anything it
// finds: see this project's own HOTRELOAD.md for exactly why a running process cannot
// swap a single script's compiled logic without a full restart (no Go `plugin` support on
// Windows; the proper fix — a separate "brain" process holding script logic, reconnecting
// over IPC — is real but far from finished). Picking up a newer commit always means
// rebuilding the binary and restarting the whole process; nothing in this package, or
// anything built on top of it, should ever be allowed to imply otherwise.
//
// Previously this package watched a single, unrelated PUBLIC repo
// (github.com/MaxBittker/rsb0t-scripts) for its single newest commit, with no per-script
// granularity — that pointed at the wrong repo entirely (this project's own, private
// oldm8clint/rsb0t-go is what actually matters) and could only ever say "the repo
// changed," never "which script's own code changed." This version replaces that with
// per-script, path-scoped checks against the real repo, requiring a token since the repo
// is private.
package updatewatcher

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultOwner/DefaultRepo are this project's own repo, confirmed private
// (2026-09-16, `gh api repos/oldm8clint/rsb0t-go --jq .private` -> true).
const (
	DefaultOwner = "oldm8clint"
	DefaultRepo  = "rsb0t-go"
)

// DefaultInterval is how often Manager.Run re-checks every configured script when its own
// Interval is unset — minutes, not seconds: this hits a real GitHub API with real rate
// limits (5000/hr authenticated), and nothing about an "update available" flag needs
// second-level freshness for a human operator deciding when to next restart the fleet.
const DefaultInterval = 5 * time.Minute

// Watcher asks one GitHub repo's commits API, scoped to one source path at a time via the
// API's own path= query parameter, for the newest commit strictly after a given time. A
// private repo (this project's own) needs Token; a public one does not.
type Watcher struct {
	Owner  string // DefaultOwner when empty
	Repo   string // DefaultRepo when empty
	Token  string // sent as "Authorization: Bearer <token>" when set; required to read a private repo
	Client *http.Client
	// URL, set only by tests, replaces the "https://api.github.com/repos/OWNER/REPO/commits"
	// base a real call would use — path/since/per_page are still appended as query
	// parameters the same way, so a test server sees the same query a real call would
	// (and can dispatch on r.URL.Query().Get("path") the way a Manager test with several
	// scripts' worth of paths needs to).
	URL string
}

// Update describes one commit GitHub reported.
type Update struct {
	SHA     string    `json:"sha"`
	Message string    `json:"message"` // first line only, see firstLine
	URL     string    `json:"url"`
	Time    time.Time `json:"time"`
}

type commit struct {
	SHA    string `json:"sha"`
	URL    string `json:"html_url"`
	Commit struct {
		Message string `json:"message"`
		Author  struct {
			Date time.Time `json:"date"`
		} `json:"author"`
	} `json:"commit"`
}

// OwnerRepo returns w's resolved owner/repo — Owner/Repo when set, DefaultOwner/
// DefaultRepo otherwise — so a caller building a display link (e.g. the running
// build's own commit URL, which the commits API never hands back since it isn't a
// commit lookup) can do it without duplicating this same default-filling logic.
func (w *Watcher) OwnerRepo() (owner, repo string) {
	owner, repo = w.Owner, w.Repo
	if owner == "" {
		owner = DefaultOwner
	}
	if repo == "" {
		repo = DefaultRepo
	}
	return owner, repo
}

func (w *Watcher) endpoint(path string, since time.Time) string {
	base := w.URL
	if base == "" {
		owner, repo := w.OwnerRepo()
		base = fmt.Sprintf("https://api.github.com/repos/%s/%s/commits", owner, repo)
	}
	q := url.Values{}
	q.Set("per_page", "1")
	if path != "" {
		q.Set("path", path)
	}
	if !since.IsZero() {
		q.Set("since", since.UTC().Format(time.RFC3339))
	}
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return base + sep + q.Encode()
}

// CheckPath asks for the newest commit touching path, strictly after since (the zero
// time means no lower bound at all — "the newest commit, period"). Returns nil, nil when
// there is none: nothing has ever touched path, or nothing has touched it since since.
func (w *Watcher) CheckPath(ctx context.Context, path string, since time.Time) (*Update, error) {
	client := w.Client
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, w.endpoint(path, since), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "rsb0t-go-updatewatcher")
	if w.Token != "" {
		req.Header.Set("Authorization", "Bearer "+w.Token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github commits: HTTP %s", resp.Status)
	}

	var commits []commit
	if err := json.NewDecoder(resp.Body).Decode(&commits); err != nil {
		return nil, err
	}
	if len(commits) == 0 {
		return nil, nil
	}
	c := commits[0]
	if c.SHA == "" {
		return nil, fmt.Errorf("github commits: empty response")
	}
	return &Update{SHA: c.SHA, Message: firstLine(c.Commit.Message), URL: c.URL, Time: c.Commit.Author.Date}, nil
}

func firstLine(s string) string {
	for i, r := range s {
		if r == '\n' {
			return s[:i]
		}
	}
	return s
}

// ScriptSource is one registered internal/bot script and the repo-relative paths its own
// code actually lives under — everything a check needs to scope itself to just that
// script, not the whole repo. Paths are files or directories (e.g.
// "internal/bot/fisher.go", "internal/scripts/fisherrunner"); each is checked separately
// since the commits API's own path filter takes exactly one path per request, and the
// newest result across all of a script's paths is what Manager reports for it.
type ScriptSource struct {
	Script string
	Paths  []string
}

// ScriptStatus is one script's most recently checked state, as served by
// internal/controlpanel's /api/scriptversions.
type ScriptStatus struct {
	Script string `json:"script"`
	// Update is nil when nothing newer than the running build was found on any of this
	// script's paths as of CheckedAt — "up to date," not "never checked" (see CheckedAt).
	Update          *Update `json:"update,omitempty"`
	CheckedAtUnixMs int64   `json:"checkedAt"`
	// Err is set when the most recent check itself failed (rate limit, network, a bad
	// token, ...); Update keeps whatever the last SUCCESSFUL check found rather than
	// blanking to "unknown" over one flaky poll.
	Err string `json:"error,omitempty"`
}

// Manager runs CheckAll on a ticker and keeps the latest ScriptStatus per script for a
// dashboard to read. Deliberately answer-only, per this package's own doc comment:
// nothing here can apply what it finds.
type Manager struct {
	Watcher   *Watcher
	BuildSHA  string    // the running binary's own commit SHA
	BuildTime time.Time // the running binary's own commit time — the "since" baseline for every check
	Scripts   []ScriptSource
	Interval  time.Duration // DefaultInterval when zero

	mu     sync.Mutex
	status map[string]ScriptStatus
}

// NewManager builds a Manager. Call Run to start its periodic polling loop, or CheckAll
// directly for a one-off check (as tests do).
func NewManager(w *Watcher, buildSHA string, buildTime time.Time, scripts []ScriptSource) *Manager {
	return &Manager{Watcher: w, BuildSHA: buildSHA, BuildTime: buildTime, Scripts: scripts, status: make(map[string]ScriptStatus, len(scripts))}
}

// checkOne finds the newest commit, across every one of src's paths, strictly after
// m.BuildTime — i.e. "does this script have code the running binary was not built from."
// The path with the latest such commit wins. A per-path error is surfaced only if every
// path for this script failed; one path's transient failure should not hide a real
// update another path already found.
func (m *Manager) checkOne(ctx context.Context, src ScriptSource) ScriptStatus {
	st := ScriptStatus{Script: src.Script, CheckedAtUnixMs: time.Now().UnixMilli()}
	var lastErr error
	for _, p := range src.Paths {
		u, err := m.Watcher.CheckPath(ctx, p, m.BuildTime)
		if err != nil {
			lastErr = err
			continue
		}
		if u == nil || u.SHA == m.BuildSHA {
			// u.SHA == m.BuildSHA is defensive: the since filter above should already
			// exclude the build's own commit, but never report a build as stale on
			// its own commit if a GitHub "since" boundary is ever inclusive.
			continue
		}
		if st.Update == nil || u.Time.After(st.Update.Time) {
			st.Update = u
		}
	}
	if st.Update == nil && lastErr != nil {
		st.Err = lastErr.Error()
	}
	return st
}

// CheckAll checks every configured script once, sequentially — the list is small
// (per-repo scripts, not accounts) and infrequent (Interval is minutes), so there is no
// need for concurrency here, and sequential keeps this predictable against GitHub's own
// rate limit. A per-script error leaves that script's previous good status in place.
func (m *Manager) CheckAll(ctx context.Context) {
	for _, src := range m.Scripts {
		st := m.checkOne(ctx, src)
		m.mu.Lock()
		if st.Err != "" {
			if prev, ok := m.status[src.Script]; ok {
				st.Update = prev.Update
			}
		}
		m.status[src.Script] = st
		m.mu.Unlock()
	}
}

// Snapshot returns every script's latest known status, sorted by script name.
func (m *Manager) Snapshot() []ScriptStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ScriptStatus, 0, len(m.status))
	for _, st := range m.status {
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Script < out[j].Script })
	return out
}

// Run checks every script immediately, then again every Interval, until ctx is done.
// Intended to run in its own goroutine for the lifetime of the caller (see
// internal/controlpanel/server.go's Start).
func (m *Manager) Run(ctx context.Context) {
	interval := m.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}
	m.CheckAll(ctx)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.CheckAll(ctx)
		}
	}
}
