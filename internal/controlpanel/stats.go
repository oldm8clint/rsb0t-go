package controlpanel

// Accumulated run statistics — the account owner's own ask: "recording data of
// everything earned from a run accumulating... split in individual and combined total
// view, total exp, total levels, total deaths, total randoms failed etc", followed up
// directly with "the control panel to always remember previous session data of things
// that are important" — making cross-restart persistence a hard requirement, not an MVP
// nicety.
//
// What counts as "a run": both. A persisted LIFETIME total (survives every restart, for
// as long as an operator keeps the same stats file around) AND an in-memory
// SINCE-RESTART view (zeroed each process start, for "what has this specific process
// instance done"), exactly the "both" option the account owner's own framing invited —
// this fleet gets restarted for deploys constantly (a dozen-plus times in one night,
// per HOTRELOAD.md's own account), so anything that only ever showed a since-restart
// figure would defeat the entire point of tracking it, while an operator mid-incident
// still wants to know "what has THIS process done" without mentally subtracting.
//
// Persistence follows internal/fleet/bankstore.go's own established pattern exactly,
// not a new mechanism: a JSON file (Config.StatsStateFile, empty disables it — same
// "empty disables the file, feature still works in a lesser, memory-only form"
// convention Config.LayoutFile and fleet.Config.BankStateFile both already use),
// loaded once at startup, saved with the same write-beside-then-rename technique so a
// crash mid-write never corrupts it, on a periodic timer plus once more on Shutdown so a
// clean stop never loses the last few seconds of updates.
//
// Where the data comes from: this aggregator holds its own Source reference and polls
// Source.Accounts on its own ticker — deliberately NOT hooked into hub.sample (which
// only runs while at least one browser stream is open, see hub.go's own stopTick
// bookkeeping), because "always remember" has to mean overnight-with-no-tab-open too, not
// just "while someone happens to be watching". This adds one more periodic
// Source.Accounts call, at a much slower cadence (statsInterval, below) than hub's own
// per-second sampling — the same order of magnitude as FleetSource's existing internal
// watch() poll (5s, always-on regardless of tabs), which already walks the same
// fleet.Manager.Snapshot cost this reuses, not a new expensive primitive.
//
// Everything here is derived from fields internal/fleet.AccountStatus /
// internal/controlpanel.AccountStatus already expose (Dead, RandomEvents, State,
// Skills, ConnectedSecs-implied Ingame state) — no new Event kind was needed, and
// internal/fleet is not touched at all, matching this feature's own "prefer deriving
// from what's already flowing over inventing a parallel tracking mechanism" brief.

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// statsInterval is how often the aggregator polls Source.Accounts — slow enough to be
// cheap on a 179-account fleet run continuously for a whole process lifetime, fast
// enough that "total uptime" and "time since a death" stay reasonably accurate. Not tied
// to hub's own 1s tick (see this file's own doc comment for why it deliberately isn't).
var statsInterval = 10 * time.Second

// statsSaveInterval is how often accumulated stats are flushed to StatsStateFile when
// dirty — independent of statsInterval so a fast poll cadence doesn't imply equally
// frequent disk writes on a long-running process.
var statsSaveInterval = 30 * time.Second

// acctStats is one account's persisted, monotonically-accumulating record. Every int
// counter here only ever increases (an edge-triggered count of distinct occurrences —
// "this account died", "this random event resolved" — never a live/current value, which
// is why none of this needs to be reconciled against AccountStatus's own current fields
// beyond detecting the next edge).
type acctStats struct {
	FirstSeenUnixMs int64 `json:"firstSeen"`
	LastSeenUnixMs  int64 `json:"lastSeen"`

	// LifetimeConnectedSecs accumulates statsInterval.Seconds() for every poll this
	// account was observed in the ingame state — an approximation sampled at
	// statsInterval, not a precise stopwatch, and documented as such wherever it's
	// surfaced (see StatsBody's own doc comment).
	LifetimeConnectedSecs float64 `json:"lifetimeConnectedSecs"`

	Deaths         int `json:"deaths"`
	RandomsCleared int `json:"randomsCleared"`
	// RandomsFailed counts only TERMINAL failures: a handler error, or the guardian giving
	// up after its maximum attempts. It used to count every outcome that was not "cleared",
	// which swept in two things that are not failures and made the dashboard report 17,802
	// failed random events against 6,963 cleared (2026-09-20):
	//
	//   * RandomsRetried — "still present after handling" is the guardian releasing the
	//     account to try the SAME event instance again, so one stubborn event logged it many
	//     times over. Measured in a single live run: one account's mime logged it 13 times
	//     and Strange plants 6-8 times each, against 93 cleared across the whole fleet. Those
	//     events still cleared on a later attempt.
	//   * RandomsNotOurs — the event belonged to another player and the guardian correctly
	//     declined it (internal/randomevents/speech_owner.go). That is a right decision, and
	//     it was previously not recorded at all.
	//
	// Counts written before that date carry the old meaning; see StatsBody.CountingSplit.
	RandomsFailed  int `json:"randomsFailed"`
	RandomsNotOurs int `json:"randomsNotOurs"`
	RandomsRetried int `json:"randomsRetried"`
	// Reconnects counts distinct reconnect EPISODES (an edge into StateReconnecting),
	// not raw retry attempts — internal/fleet.AccountStatus.ReconnectAttempt already
	// resets to 0 on a successful relogin, so counting attempts directly would
	// undercount an account that failed several times before finally reconnecting.
	Reconnects int `json:"reconnects"`

	// SkillXPBaseline/SkillLevelBaseline are the first-ever observed XP/level per skill
	// this account has shown since tracking began — persisted once, never updated after
	// — so "XP/levels gained" (current minus baseline, computed at query time from the
	// live snapshot, not stored here) survives every restart without re-baselining to
	// 0 and silently losing everything earned before that restart.
	SkillXPBaseline    map[string]int64 `json:"skillXPBaseline,omitempty"`
	SkillLevelBaseline map[string]int   `json:"skillLevelBaseline,omitempty"`

	// lastState/lastDead/lastRandomAt are edge-detection bookkeeping, persisted so a
	// restart doesn't re-count (or miss) an edge that straddles it — e.g. an account
	// that was already mid-"still present after handling" when the process stopped
	// should not have that same event re-counted as new the moment polling resumes.
	LastState    State  `json:"lastState,omitempty"`
	LastDead     bool   `json:"lastDead,omitempty"`
	LastRandomAt string `json:"lastRandomAt,omitempty"` // RFC3339, newest RandomEventOutcome.At already counted
}

// combinedTotals is acctStats summed across every tracked account, plus the live
// (not persisted — always read fresh from the latest snapshot) XP/level figures that
// only make sense combined across accounts at query time.
type combinedTotals struct {
	AccountsTracked int     `json:"accountsTracked"`
	XPGained        int64   `json:"xpGained"`
	LevelsGained    int     `json:"levelsGained"`
	CurrentLevelSum int     `json:"currentLevelSum"`
	Deaths          int     `json:"deaths"`
	RandomsCleared  int     `json:"randomsCleared"`
	RandomsFailed   int     `json:"randomsFailed"`
	RandomsNotOurs  int     `json:"randomsNotOurs"`
	RandomsRetried  int     `json:"randomsRetried"`
	Reconnects      int     `json:"reconnects"`
	UptimeSecs      float64 `json:"uptimeSecs"`
}

func (a combinedTotals) minus(b combinedTotals) combinedTotals {
	return combinedTotals{
		XPGained:       a.XPGained - b.XPGained,
		LevelsGained:   a.LevelsGained - b.LevelsGained,
		Deaths:         a.Deaths - b.Deaths,
		RandomsCleared: a.RandomsCleared - b.RandomsCleared,
		RandomsFailed:  a.RandomsFailed - b.RandomsFailed,
		RandomsNotOurs: a.RandomsNotOurs - b.RandomsNotOurs,
		RandomsRetried: a.RandomsRetried - b.RandomsRetried,
		Reconnects:     a.Reconnects - b.Reconnects,
		UptimeSecs:     a.UptimeSecs - b.UptimeSecs,
		// AccountsTracked/CurrentLevelSum are current-state snapshots, not
		// accumulating counters — "since restart" doesn't apply to them the same way,
		// so the caller uses the lifetime value for both rather than a meaningless
		// subtraction.
	}
}

// AccountStatsView is one account's individual stats, JSON-friendly — the "individual
// view" half of the account owner's ask.
type AccountStatsView struct {
	Username        string  `json:"username"`
	FirstSeenUnixMs int64   `json:"firstSeen"`
	LastSeenUnixMs  int64   `json:"lastSeen"`
	XPGained        int64   `json:"xpGained"`
	LevelsGained    int     `json:"levelsGained"`
	CurrentLevelSum int     `json:"currentLevelSum"`
	Deaths          int     `json:"deaths"`
	RandomsCleared  int     `json:"randomsCleared"`
	RandomsFailed   int     `json:"randomsFailed"`
	RandomsNotOurs  int     `json:"randomsNotOurs"`
	RandomsRetried  int     `json:"randomsRetried"`
	Reconnects      int     `json:"reconnects"`
	UptimeSecs      float64 `json:"uptimeSecs"`
}

// StatsBody answers GET /api/stats. Lifetime is persisted and survives every restart;
// SinceRestart is this process instance's own contribution, zeroed at startup — see this
// file's own doc comment for why both are shown rather than picking one. UptimeSecs
// figures throughout are sampled at statsInterval, not measured continuously — labeled
// "approximate" here once so individual fields don't need to repeat it.
type StatsBody struct {
	TrackingSinceUnixMs int64              `json:"trackingSince"`
	ProcessStartUnixMs  int64              `json:"processStart"`
	Approximate         bool               `json:"approximate"`
	Lifetime            combinedTotals     `json:"lifetime"`
	SinceRestart        combinedTotals     `json:"sinceRestart"`
	Accounts            []AccountStatsView `json:"accounts"`
}

// statsAggregator owns the persisted per-account records and the polling that keeps
// them current. Safe for concurrent use.
type statsAggregator struct {
	mu       sync.Mutex
	path     string // empty: memory only, same convention as fleet's bankStore
	src      Source
	now      func() time.Time
	interval time.Duration

	trackingSinceUnixMs int64
	processStartUnixMs  int64
	accounts            map[string]*acctStats
	// startBaseline is Lifetime's combinedTotals computed immediately after loading
	// persisted state, before this process's own poll has changed anything — the
	// "since restart" subtraction point. sessionXPBaseline/sessionLevelBaseline mirror
	// FleetSource's own xpBaseline pattern (see fleetsource.go): the first XP/level
	// THIS PROCESS observed per account+skill, used only for the SinceRestart view's
	// own gained figures, independent of the persisted lifetime baseline.
	startBaseline        combinedTotals
	sessionXPBaseline    map[string]map[string]int64
	sessionLevelBaseline map[string]map[string]int

	// latest is the most recent AccountStatus this aggregator has observed per
	// account, used to compute current-XP/level-relative figures at query time without
	// re-fetching — refreshed every poll, not persisted (always repopulated within one
	// interval of a restart).
	latest map[string]AccountStatus

	dirty bool
	stop  chan struct{}
	wg    sync.WaitGroup
	// saveMu serialises save. The periodic timer, Shutdown and a loot clear (lootclear.go) can all
	// call it, and two writes in flight could finish out of order and leave the OLDER snapshot on
	// disk, which after a clear would bring the cleared loot back on the next start.
	saveMu sync.Mutex

	// loot is the loot panel's aggregator (lootstats.go), persisted in the same file.
	loot *lootAggregator
}

// newStatsAggregator builds an aggregator over src, loading path if non-empty (best
// effort — a missing or corrupt file starts empty rather than failing construction,
// same as bankStore/layoutStore).
func newStatsAggregator(path string, src Source, now func() time.Time) *statsAggregator {
	if now == nil {
		now = time.Now
	}
	a := &statsAggregator{
		path:                 path,
		src:                  src,
		now:                  now,
		interval:             statsInterval,
		accounts:             make(map[string]*acctStats),
		sessionXPBaseline:    make(map[string]map[string]int64),
		sessionLevelBaseline: make(map[string]map[string]int),
		latest:               make(map[string]AccountStatus),
		processStartUnixMs:   now().UnixMilli(),
		loot:                 newLootAggregator(),
	}
	a.load()
	a.trackingSinceUnixMs = a.earliestFirstSeen()
	if a.trackingSinceUnixMs == 0 {
		a.trackingSinceUnixMs = a.processStartUnixMs
	}
	a.startBaseline = a.lifetimeLocked(nil)
	return a
}

func (a *statsAggregator) earliestFirstSeen() int64 {
	var earliest int64
	for _, rec := range a.accounts {
		if earliest == 0 || rec.FirstSeenUnixMs < earliest {
			earliest = rec.FirstSeenUnixMs
		}
	}
	return earliest
}

// start begins the polling loop in a new goroutine. Safe to call at most once.
func (a *statsAggregator) start() {
	a.stop = make(chan struct{})
	a.wg.Add(1)
	go a.run()
}

// stop ends the polling loop and flushes any pending changes to disk before returning —
// called from Server.Shutdown so a clean stop never loses the last interval's worth of
// updates the periodic save timer hasn't gotten to yet.
func (a *statsAggregator) close() {
	if a.stop != nil {
		close(a.stop)
		a.wg.Wait()
	}
	a.save()
}

func (a *statsAggregator) run() {
	defer a.wg.Done()
	poll := newTicker(a.interval)
	defer poll.Stop()
	save := newTicker(statsSaveInterval)
	defer save.Stop()
	a.poll()
	for {
		select {
		case <-a.stop:
			return
		case <-poll.C:
			a.poll()
		case <-save.C:
			a.save()
		}
	}
}

func (a *statsAggregator) poll() {
	accounts := a.src.Accounts(nil)
	now := a.now()
	a.recordSample(accounts, now)
	a.pollLoot(now)
}

// recordSample is the pure(ish) core: given one fresh snapshot, update every account's
// accumulating record and the latest-observed cache. Exported to this file's tests
// directly (no ticker/real-time wait needed to exercise it).
func (a *statsAggregator) recordSample(accounts []AccountStatus, now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ms := now.UnixMilli()
	changed := false
	for _, acc := range accounts {
		rec, ok := a.accounts[acc.Name]
		if !ok {
			rec = &acctStats{FirstSeenUnixMs: ms, LastState: acc.State}
			a.accounts[acc.Name] = rec
			if a.trackingSinceUnixMs == 0 || ms < a.trackingSinceUnixMs {
				a.trackingSinceUnixMs = ms
			}
			changed = true
		}
		rec.LastSeenUnixMs = ms

		if acc.State == StateIngame {
			rec.LifetimeConnectedSecs += a.interval.Seconds()
			changed = true
		}

		if acc.Dead && !rec.LastDead {
			rec.Deaths++
			changed = true
		}
		rec.LastDead = acc.Dead

		if rec.LastState.reconnectEdge(acc.State) {
			rec.Reconnects++
			changed = true
		}
		rec.LastState = acc.State

		if n := len(acc.RandomEvents); n > 0 {
			for i := n - 1; i >= 0; i-- { // oldest-of-the-window first, so LastRandomAt advances monotonically
				ev := acc.RandomEvents[i]
				if ev.At == "" || ev.At <= rec.LastRandomAt {
					continue
				}
				// Only the outcomes internal/fleet.parseRandomEvent actually emits, each
				// counted as what it is. An unrecognised outcome is deliberately counted
				// nowhere rather than defaulting to "failed" — that default is what made
				// this number meaningless in the first place.
				switch ev.Outcome {
				case "cleared":
					rec.RandomsCleared++
				case "not_ours":
					rec.RandomsNotOurs++
				case "retry":
					rec.RandomsRetried++
				case "error", "gave_up":
					rec.RandomsFailed++
				}
				rec.LastRandomAt = ev.At
				changed = true
			}
		}

		if len(acc.Skills) > 0 {
			if rec.SkillXPBaseline == nil {
				rec.SkillXPBaseline = make(map[string]int64)
			}
			if rec.SkillLevelBaseline == nil {
				rec.SkillLevelBaseline = make(map[string]int)
			}
			sessXP, ok := a.sessionXPBaseline[acc.Name]
			if !ok {
				sessXP = make(map[string]int64)
				a.sessionXPBaseline[acc.Name] = sessXP
			}
			sessLvl, ok := a.sessionLevelBaseline[acc.Name]
			if !ok {
				sessLvl = make(map[string]int)
				a.sessionLevelBaseline[acc.Name] = sessLvl
			}
			for name, sk := range acc.Skills {
				if _, seen := rec.SkillXPBaseline[name]; !seen {
					rec.SkillXPBaseline[name] = sk.XP
					rec.SkillLevelBaseline[name] = sk.Level
					changed = true
				}
				if _, seen := sessXP[name]; !seen {
					sessXP[name] = sk.XP
					sessLvl[name] = sk.Level
				}
			}
		}

		a.latest[acc.Name] = acc
	}
	if changed {
		a.dirty = true
	}
}

// reconnectEdge reports whether transitioning from s to next is the start of a new
// reconnect episode — used to count distinct reconnect EPISODES rather than raw retry
// attempts (see acctStats.Reconnects' own doc comment).
func (s State) reconnectEdge(next State) bool {
	return s != StateReconnecting && next == StateReconnecting
}

// lifetimeLocked computes combinedTotals across every persisted account record plus,
// when latest is non-nil, the live current-XP/level-relative figures from it. Caller
// holds a.mu (or calls this before any goroutine could observe a, as newStatsAggregator
// does for startBaseline).
func (a *statsAggregator) lifetimeLocked(latest map[string]AccountStatus) combinedTotals {
	var out combinedTotals
	out.AccountsTracked = len(a.accounts)
	for name, rec := range a.accounts {
		out.Deaths += rec.Deaths
		out.RandomsCleared += rec.RandomsCleared
		out.RandomsFailed += rec.RandomsFailed
		out.RandomsNotOurs += rec.RandomsNotOurs
		out.RandomsRetried += rec.RandomsRetried
		out.Reconnects += rec.Reconnects
		out.UptimeSecs += rec.LifetimeConnectedSecs
		if latest == nil {
			continue
		}
		acc, ok := latest[name]
		if !ok || len(acc.Skills) == 0 {
			continue
		}
		for skName, sk := range acc.Skills {
			out.CurrentLevelSum += sk.Level
			if base, ok := rec.SkillXPBaseline[skName]; ok {
				out.XPGained += sk.XP - base
			}
			if base, ok := rec.SkillLevelBaseline[skName]; ok {
				out.LevelsGained += sk.Level - base
			}
		}
	}
	return out
}

// sinceRestartLocked mirrors lifetimeLocked but relative to this process's own
// session-start XP/level baselines (see sessionXPBaseline's own doc comment) instead of
// the persisted lifetime ones, and subtracts a.startBaseline for the accumulating
// counters. Caller holds a.mu.
func (a *statsAggregator) sinceRestartLocked() combinedTotals {
	var raw combinedTotals
	for name, rec := range a.accounts {
		raw.Deaths += rec.Deaths
		raw.RandomsCleared += rec.RandomsCleared
		raw.RandomsFailed += rec.RandomsFailed
		raw.RandomsNotOurs += rec.RandomsNotOurs
		raw.RandomsRetried += rec.RandomsRetried
		raw.Reconnects += rec.Reconnects
		raw.UptimeSecs += rec.LifetimeConnectedSecs
		acc, ok := a.latest[name]
		if !ok || len(acc.Skills) == 0 {
			continue
		}
		sessXP := a.sessionXPBaseline[name]
		sessLvl := a.sessionLevelBaseline[name]
		for skName, sk := range acc.Skills {
			if base, ok := sessXP[skName]; ok {
				raw.XPGained += sk.XP - base
			}
			if base, ok := sessLvl[skName]; ok {
				raw.LevelsGained += sk.Level - base
			}
		}
	}
	delta := raw.minus(a.startBaseline)
	delta.AccountsTracked = len(a.accounts)
	return delta
}

// snapshot builds the full StatsBody a request handler serves.
func (a *statsAggregator) snapshot() StatsBody {
	a.mu.Lock()
	defer a.mu.Unlock()

	names := make([]string, 0, len(a.accounts))
	for name := range a.accounts {
		names = append(names, name)
	}
	sort.Strings(names)

	accountViews := make([]AccountStatsView, 0, len(names))
	for _, name := range names {
		rec := a.accounts[name]
		view := AccountStatsView{
			Username:        name,
			FirstSeenUnixMs: rec.FirstSeenUnixMs,
			LastSeenUnixMs:  rec.LastSeenUnixMs,
			Deaths:          rec.Deaths,
			RandomsCleared:  rec.RandomsCleared,
			RandomsFailed:   rec.RandomsFailed,
			RandomsNotOurs:  rec.RandomsNotOurs,
			RandomsRetried:  rec.RandomsRetried,
			Reconnects:      rec.Reconnects,
			UptimeSecs:      rec.LifetimeConnectedSecs,
		}
		if acc, ok := a.latest[name]; ok {
			for skName, sk := range acc.Skills {
				view.CurrentLevelSum += sk.Level
				if base, ok := rec.SkillXPBaseline[skName]; ok {
					view.XPGained += sk.XP - base
				}
				if base, ok := rec.SkillLevelBaseline[skName]; ok {
					view.LevelsGained += sk.Level - base
				}
			}
		}
		accountViews = append(accountViews, view)
	}

	return StatsBody{
		TrackingSinceUnixMs: a.trackingSinceUnixMs,
		ProcessStartUnixMs:  a.processStartUnixMs,
		Approximate:         true,
		Lifetime:            a.lifetimeLocked(a.latest),
		SinceRestart:        a.sinceRestartLocked(),
		Accounts:            accountViews,
	}
}

// ---- persistence: same write-beside-then-rename technique as fleet/bankstore.go and
// internal/controlpanel/layout.go's own layoutStore.save ----

type statsFile struct {
	TrackingSinceUnixMs int64                 `json:"trackingSince"`
	Accounts            map[string]*acctStats `json:"accounts"`
	// Loot is lootstats.go's persisted state; absent from a file written before it existed.
	Loot *lootFile `json:"loot,omitempty"`
}

func (a *statsAggregator) load() {
	if a.path == "" {
		return
	}
	f, err := os.Open(a.path)
	if err != nil {
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 32<<20))
	if err != nil {
		return
	}
	var loaded statsFile
	if err := json.Unmarshal(data, &loaded); err != nil {
		return
	}
	if loaded.Accounts != nil {
		a.accounts = loaded.Accounts
	}
	a.trackingSinceUnixMs = loaded.TrackingSinceUnixMs
	a.loot.restore(loaded.Loot)
}

// save persists the whole accounts map, best-effort — a write failure is silently
// dropped rather than propagated (there is no good way to surface it from a background
// ticker, and losing this write is never worse than this feature's pre-existing
// memory-only behavior), same convention as fleet.bankStore.set.
func (a *statsAggregator) save() {
	a.saveMu.Lock()
	defer a.saveMu.Unlock()
	a.mu.Lock()
	lootDirty := a.loot.takeDirty()
	if a.path == "" || (!a.dirty && !lootDirty) {
		a.mu.Unlock()
		return
	}
	snapshot := make(map[string]*acctStats, len(a.accounts))
	for k, v := range a.accounts {
		cp := *v
		snapshot[k] = &cp
	}
	trackingSince := a.trackingSinceUnixMs
	a.dirty = false
	a.mu.Unlock()

	data, err := json.MarshalIndent(statsFile{TrackingSinceUnixMs: trackingSince, Accounts: snapshot, Loot: a.loot.persisted()}, "", "  ")
	if err != nil {
		return
	}
	data = append(data, '\n')
	dir := filepath.Dir(a.path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return
		}
	}
	tmp, err := os.CreateTemp(dir, ".statestats-*.tmp")
	if err != nil {
		return
	}
	name := tmp.Name()
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, a.path)
	}
	if err != nil {
		_ = os.Remove(name)
	}
}
