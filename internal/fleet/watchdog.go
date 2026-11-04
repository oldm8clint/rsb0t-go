package fleet

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"sort"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/bot"
	"rsb0t-go/internal/client"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/stallwatch"
)

// The fleet's stall watchdog: the impure half of internal/stallwatch, which holds the decision
// and its reasoning (read its package doc first). Every WatchdogConfig.Every it reads each
// account's state -- cheaply, straight off the account and its session, NOT through Snapshot --
// asks stallwatch what to do, and does it:
//
//	nudge    press an open dialogue's continue, else close the open modal
//	restart  stop the bot, wait for the runner to finish (the first RestartWait, then up to
//	         RestartFinishWait more -- a slow bot is waited out, never abandoned), StartBot with
//	         the same script and the same settings -- skipped for a BLIND account
//	         (stallwatch.Obs.Blind), which a restart is proven not to help
//	relogin  Disconnect, wait for the session to close, settle, then ConnectExisting with the
//	         script marked to resume -- so the login goes through loginOnce's ordinary gate and
//	         route pacing (28 then a rest per route; responses 9 and 16 handled exactly as for any
//	         other login) and retryAccount restarts the script
//	give up  mark the account "needs_attention" in /api/status (AccountStatus.Watchdog) and stop
//
// Besides stalls it keeps one more promise (watchdog_orphan.go): an account in game is not left with
// its bot stopped when nobody meant that. The restart finishes its own job, and a safety net starts
// again a bot found stopped for OrphanAfter that an operator, the script itself or a hop did not stop.
//
// Every line it writes goes to the account's own activity log as "[acct/script] [watchdog] ...",
// through wlogf, which deliberately does NOT stamp the account's last-line time: the watchdog's
// own lines must never count as the account "saying so".

// WatchdogConfig is StartWatchdog's configuration: the decision's knobs plus the loop's own.
type WatchdogConfig struct {
	stallwatch.Config
	// Every is the sweep interval (default 30s).
	Every time.Duration
	// DumpDir, when set, receives one goroutine dump when a stall is first seen, at most one per
	// DumpEvery fleet-wide and MaxDumps per process. Relative to the working directory, which for
	// the live fleet is run/, so the default "logs" lands in the gitignored run/logs/.
	DumpDir   string
	DumpEvery time.Duration
	MaxDumps  int
	// SummaryEvery is how often a one-line fleet summary is written (default 30m).
	SummaryEvery time.Duration
	// RestartWait is how long a restart waits quietly for the old runner to finish (default 15s).
	RestartWait time.Duration
	// RestartFinishWait bounds how much longer it keeps waiting after that, to start the script
	// again once the old bot has ended (default 10m). A bot that takes minutes to wind down (a
	// ScarabSwarm prayer ladder took two) is waited out; a runner still alive after this is left
	// alone, and the safety net starts the script if it ever ends.
	RestartFinishWait time.Duration
	// ReloginWait bounds how long a relogin is counted as in flight (default 10m).
	ReloginWait time.Duration
}

// DefaultWatchdogConfig is the fleet default: ON in OBSERVE mode (detect and report, act on
// nothing) until an operator switches it -- with -watchdog, or at runtime with SetWatchdogMode
// (the control panel's POST /api/watchdog).
func DefaultWatchdogConfig() WatchdogConfig {
	return WatchdogConfig{
		Config:       stallwatch.DefaultConfig(),
		Every:        30 * time.Second,
		DumpDir:      "logs",
		DumpEvery:    10 * time.Minute,
		MaxDumps:     20,
		SummaryEvery: 30 * time.Minute,
		RestartWait:  15 * time.Second,
		ReloginWait:  10 * time.Minute,

		RestartFinishWait: 10 * time.Minute,
	}
}

// WatchdogStatus is one account's watchdog state for /api/status; nil (omitted) while there is
// nothing to say.
type WatchdogStatus struct {
	// State is "stalled" (detected; a ladder may be under way) or "needs_attention" (the ladder
	// gave up; an operator should look).
	State string `json:"state"`
	// Since is when this stall was first detected (RFC3339).
	Since string `json:"since,omitempty"`
	// Step is the last ladder step taken: nudge, restart, relogin or give up.
	Step string `json:"step,omitempty"`
	// Ladders is how many ladders this account has had inside the ladder window.
	Ladders int `json:"ladders,omitempty"`
	// Detail is the last thing the watchdog said about it.
	Detail string `json:"detail,omitempty"`
}

// watchdogFields is embedded in Manager, so the watchdog costs fleet.go one line.
type watchdogFields struct {
	watchMu sync.Mutex
	watch   *watchdog
	// watchCfg is the configuration StartWatchdog was given, kept even when it started in mode
	// off, so SetWatchdogMode can start the watchdog later without a fleet restart. Nil until
	// StartWatchdog runs (a Manager that never configured one, e.g. cmd/fleet).
	watchCfg *WatchdogConfig
}

type watchdog struct {
	m       *Manager
	cfg     WatchdogConfig
	started time.Time

	// The seams the tests use; nil or zero means the real thing (see guardianBusy, requeueLogin).
	busyFn    func(actions.Session) bool // randomevents.Busy
	connectFn func(string) error         // Manager.ConnectExisting
	settle    time.Duration              // after the old session is gone; hopSettle
	// connectAttempts/connectRetry bound how often a relogin re-queues its login when
	// ConnectExisting refuses (defaults 6 and 5s: the reconnect loop that the logout woke is
	// usually still winding down, "finishing disconnect; retry shortly").
	connectAttempts int
	connectRetry    time.Duration

	// startFn is Manager.StartBot, for a restart's and the safety net's start (watchdog_orphan.go).
	startFn func(username, script string, settings map[string]interface{}) error

	mu sync.Mutex
	// mode is the live mode, cfg.Mode at start and changed by SetWatchdogMode; read by every
	// sweep, so a switch takes effect on the next one.
	mode      stallwatch.Mode
	hist      map[string]*stallwatch.History
	status    map[string]WatchdogStatus
	inFlight  map[string]bool // an action is running for this account
	actions   []time.Time     // interventions, for the fleet-wide window
	relogins  int
	lastDump  time.Time
	dumps     int
	breakerAt time.Time
	// scriptBreakerAt rate-limits each script's own breaker line.
	scriptBreakerAt map[string]time.Time
	// orphans is the safety net's memory per account (watchdog_orphan.go).
	orphans   map[string]stallwatch.OrphanHistory
	summaryAt time.Time
	counts    watchdogCounts
}

type watchdogCounts struct {
	Detected, Nudged, Restarted, Relogged, GaveUp, Held, Busy int
	// Orphans is how many left-stopped bots the safety net has started again.
	Orphans   int
	Recovered map[string]int
}

// StartWatchdog starts the stall watchdog. Calling it more than once does nothing. In mode off it
// starts nothing but remembers cfg, so SetWatchdogMode can start it later. It stops with the
// Manager (CloseAll).
func (m *Manager) StartWatchdog(cfg WatchdogConfig) {
	cfg = normalizeWatchdogConfig(cfg)
	m.watchMu.Lock()
	defer m.watchMu.Unlock()
	if m.watch != nil || m.watchCfg != nil {
		return
	}
	kept := cfg
	m.watchCfg = &kept
	if cfg.Mode == stallwatch.ModeOff {
		m.logf("[watchdog] off (POST /api/watchdog with {\"mode\":\"observe\"} starts it without a restart)")
		return
	}
	m.startWatchdogLocked(cfg)
}

// normalizeWatchdogConfig fills every unset knob from DefaultWatchdogConfig.
func normalizeWatchdogConfig(cfg WatchdogConfig) WatchdogConfig {
	def := DefaultWatchdogConfig()
	if cfg.Mode == "" {
		cfg.Mode = def.Mode
	}
	if cfg.Every <= 0 {
		cfg.Every = def.Every
	}
	if cfg.SummaryEvery <= 0 {
		cfg.SummaryEvery = def.SummaryEvery
	}
	if cfg.RestartWait <= 0 {
		cfg.RestartWait = def.RestartWait
	}
	if cfg.ReloginWait <= 0 {
		cfg.ReloginWait = def.ReloginWait
	}
	if cfg.RestartFinishWait <= 0 {
		cfg.RestartFinishWait = def.RestartFinishWait
	}
	if cfg.DumpEvery <= 0 {
		cfg.DumpEvery = def.DumpEvery
	}
	if cfg.MaxDumps <= 0 {
		cfg.MaxDumps = def.MaxDumps
	}
	return cfg
}

// startWatchdogLocked creates and starts the watchdog; m.watchMu is held and m.watch is nil.
func (m *Manager) startWatchdogLocked(cfg WatchdogConfig) {
	w := &watchdog{m: m, cfg: cfg, mode: cfg.Mode, started: time.Now(), hist: map[string]*stallwatch.History{},
		status: map[string]WatchdogStatus{}, inFlight: map[string]bool{}, scriptBreakerAt: map[string]time.Time{},
		counts: watchdogCounts{Recovered: map[string]int{}}}
	w.summaryAt = w.started
	m.watch = w
	var exempt []string
	for k := range cfg.Exempt {
		exempt = append(exempt, k)
	}
	sort.Strings(exempt)
	m.logf("[watchdog] on, mode %s (%s): judges a running bot stalled when it is silent AND making no progress for %s (never one that is logging), or blind outside any event with no progress for %s (logging or not; 0s = off); ladder nudge -> restart -> relogin -> needs attention (a blind account skips the restart), %s between steps; at most %d ladders per account per %s, %d actions per %s fleet-wide, %d relogins at once; circuit breaker at max(%d, %.0f%%) stalled, given-up accounts not counted; grace %s after a start/login and %s after this start; exempt %v; safety net (act mode): a bot stopped for %s on an account in game that no operator, hop or script ending explains is started again, at most %d times per account per %s and %d per sweep (0s = off); the mode can be changed at runtime with POST /api/watchdog",
		cfg.Mode, describeMode(cfg.Mode), cfg.StallAfter, cfg.BlindAfter, cfg.RecheckAfter, cfg.MaxLadders, cfg.LadderWindow, cfg.FleetActions, cfg.FleetWindow, cfg.MaxRelogins,
		cfg.BreakerMin, cfg.BreakerFrac*100, cfg.StartGrace, cfg.FleetGrace, exempt, cfg.OrphanAfter, cfg.OrphanMaxStarts, cfg.LadderWindow, cfg.OrphanPerSweep)
	if min := stallwatch.MinStallAfter(); cfg.StallAfter < min {
		longest := stallwatch.LongestKnownSilentWait()
		m.logf("[watchdog] WARNING: the stall threshold %s is below %s, the longest known silent wait (%s, %s) plus a %s margin -- an account doing exactly that will be judged stalled",
			cfg.StallAfter, min, longest.What, longest.Max, stallwatch.SilenceMargin)
	}
	go w.loop()
}

// describeMode says in words what a mode lets the watchdog do.
func describeMode(mode stallwatch.Mode) string {
	switch mode {
	case stallwatch.ModeObserve:
		return "reports only, acts on nothing"
	case stallwatch.ModeNudge:
		return "presses a continue or closes a modal, then reports"
	case stallwatch.ModeAct:
		return "the whole ladder"
	}
	return "off"
}

// WatchdogInfo is the watchdog's own state, for GET /api/watchdog.
type WatchdogInfo struct {
	// Configured is false when this Manager never had StartWatchdog called (nothing to switch).
	Configured bool `json:"configured"`
	// Mode is the live mode: off, observe, nudge or act.
	Mode    string   `json:"mode"`
	Modes   []string `json:"modes"`
	Running bool     `json:"running"`
	// Since is when the watchdog started (RFC3339), empty while it has never run.
	Since      string `json:"since,omitempty"`
	StallAfter string `json:"stallAfter,omitempty"`
	BlindAfter string `json:"blindAfter,omitempty"`
	// The counts since the watchdog started, as its summary line prints them. In observe mode
	// Held is how many stalls it WOULD have acted on.
	Detected  int `json:"detected"`
	Nudged    int `json:"nudged"`
	Restarted int `json:"restarted"`
	Relogged  int `json:"relogged"`
	GaveUp    int `json:"gaveUp"`
	Held      int `json:"held"`
	Busy      int `json:"busyReports"`
	// OrphanStarts is how many bots left stopped in game the safety net has started again.
	OrphanStarts int `json:"orphanStarts"`
	// StalledNow and NeedsAttention count the accounts /api/status shows each way right now.
	StalledNow     int `json:"stalledNow"`
	NeedsAttention int `json:"needsAttention"`
}

// WatchdogInfo reports the watchdog's live mode and counts.
func (m *Manager) WatchdogInfo() WatchdogInfo {
	info := WatchdogInfo{}
	for _, md := range stallwatch.Modes() {
		info.Modes = append(info.Modes, string(md))
	}
	m.watchMu.Lock()
	w := m.watch
	var cfg WatchdogConfig
	configured := m.watchCfg != nil
	if configured {
		cfg = *m.watchCfg
	}
	m.watchMu.Unlock()
	if !configured {
		info.Mode = string(stallwatch.ModeOff)
		return info
	}
	info.Configured = true
	info.Mode = string(cfg.Mode)
	info.StallAfter, info.BlindAfter = cfg.StallAfter.String(), cfg.BlindAfter.String()
	if w == nil {
		return info
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	info.Mode = string(w.mode)
	info.Running = w.mode != stallwatch.ModeOff
	info.Since = w.started.Format(time.RFC3339)
	c := w.counts
	info.Detected, info.Nudged, info.Restarted, info.Relogged, info.GaveUp, info.Held, info.Busy =
		c.Detected, c.Nudged, c.Restarted, c.Relogged, c.GaveUp, c.Held, c.Busy
	info.OrphanStarts = c.Orphans
	for _, st := range w.status {
		if st.State == "needs_attention" {
			info.NeedsAttention++
		} else {
			info.StalledNow++
		}
	}
	return info
}

// SetWatchdogMode switches the watchdog's mode at runtime, effective from the next sweep, and
// returns the mode it replaced. Switching away from off starts the watchdog if it was never
// running (its fleet grace then counts from now); switching to off stops every sweep's work and
// clears the per-account statuses. Histories are kept, so a stall already seen in observe mode
// is acted on at the next sweep once the mode allows it. Nothing is persisted: the next process
// start uses -watchdog again.
func (m *Manager) SetWatchdogMode(mode string) (previous string, err error) {
	md, ok := stallwatch.ParseMode(mode)
	if !ok {
		return "", fmt.Errorf("fleet: the watchdog mode must be off, observe, nudge or act, not %q", mode)
	}
	m.watchMu.Lock()
	if m.watchCfg == nil {
		m.watchMu.Unlock()
		return "", fmt.Errorf("fleet: this fleet has no stall watchdog configured")
	}
	prev := m.watchCfg.Mode
	m.watchCfg.Mode = md
	w := m.watch
	if w == nil {
		if md != stallwatch.ModeOff {
			m.startWatchdogLocked(*m.watchCfg)
		}
		m.watchMu.Unlock()
		if md != prev {
			m.logf("[watchdog] mode changed from %s to %s at runtime (%s)", prev, md, describeMode(md))
		}
		return string(prev), nil
	}
	m.watchMu.Unlock()
	w.mu.Lock()
	prev = w.mode
	w.mode = md
	if md == stallwatch.ModeOff {
		w.status = map[string]WatchdogStatus{}
	}
	w.mu.Unlock()
	if md != prev {
		m.logf("[watchdog] mode changed from %s to %s at runtime (%s)", prev, md, describeMode(md))
	}
	return string(prev), nil
}

func (w *watchdog) loop() {
	t := time.NewTicker(w.cfg.Every)
	defer t.Stop()
	for {
		select {
		case <-w.m.stop:
			return
		case now := <-t.C:
			w.sweep(now)
		}
	}
}

// watchStatus returns an account's watchdog status for Snapshot, nil when there is none.
func (m *Manager) watchStatus(username string) *WatchdogStatus {
	m.watchMu.Lock()
	w := m.watch
	m.watchMu.Unlock()
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	st, ok := w.status[username]
	if !ok {
		return nil
	}
	return &st
}

// wlogf is alogf for the watchdog's own lines: recorded and printed like any other, but never
// stamped as the account's last line.
func (m *Manager) wlogf(a *account, format string, args ...any) {
	a.mu.RLock()
	prefix := fmt.Sprintf("[%s/%s] [watchdog] ", a.username, a.script)
	a.mu.RUnlock()
	msg := prefix + fmt.Sprintf(format, args...)
	if a.activity != nil {
		a.activity.Record(msg)
	}
	if m.Log != nil {
		m.Log(msg)
	}
}

// watchObs is one account's reading for one sweep.
type watchObs struct {
	a       *account
	obs     stallwatch.Obs
	session *client.Session
	diag    string
	// orphan, runner and script are the safety net's reading of the same account.
	orphan stallwatch.OrphanObs
	runner *bot.Runner
	script string
}

// observe reads one account without taking anything heavier than its own locks.
func (w *watchdog) observe(a *account) watchObs {
	a.mu.RLock()
	s := a.session
	o := stallwatch.Obs{
		Username:    a.username,
		Script:      a.script,
		InGame:      inGame(s != nil, a.connecting, a.reconnecting, a.hopping, a.deliberateDisconnect),
		ConnectedAt: a.connectedAt,
	}
	runner := a.runner
	world := a.world
	stopBy := a.stopBy
	a.mu.RUnlock()
	var rs bot.Status
	if runner != nil {
		rs = runner.Status()
		o.Running = rs.State == bot.StateRunning
		o.BotStartedAt = rs.StartedAt
	}
	// A bot that is not running because the watchdog itself stopped it: idle time in that state is
	// the watchdog's own doing, never "somebody took over" (stallwatch.Obs.OwnStop).
	o.OwnStop = !o.Running && stopBy == stopWatchdog
	if ns := a.lastLine.Load(); ns > 0 {
		o.LastLineAt = time.Unix(0, ns)
	}
	out := watchObs{a: a, obs: o, session: s, runner: runner, script: o.Script, orphan: orphanObs(o, o.Script, runner, rs, stopBy)}
	if s == nil || !o.Running {
		return out
	}
	st := s.State()
	fp := stallwatch.Fingerprint{World: world}
	fp.X, fp.Z, fp.Level, fp.TileOK = st.Tile()
	for _, sk := range st.Skills() {
		if sk.Generation > 0 {
			fp.TotalXP += sk.XP
		}
	}
	if inv := st.Inventory(actions.PlayerPack); inv != nil {
		fp.PackGen = inv.Generation
	}
	if inv := st.Inventory(actions.PlayerWorn); inv != nil {
		fp.WornGen = inv.Generation
	}
	if inv := st.Inventory(actions.BankWithdraw); inv != nil {
		fp.BankGen = inv.Generation
	}
	if hp, ok := st.HP(); ok {
		fp.HP = hp
	}
	out.obs.Print = fp
	main, side, chat := st.Modals()
	out.obs.ModalOpen = main != -1 || side != -1 || chat != -1
	text := ""
	if comps := w.m.cfg.Components; comps != nil {
		dlg := actions.NewChatDialog(s, comps)
		out.obs.CanContinue = dlg.CanContinue()
		if chat != -1 {
			text = strings.Join(dlg.Texts(), " ")
		}
	}
	readable := st.SideIcon(3) != -1 && st.SideIcon(4) != -1
	if comps := w.m.cfg.Components; comps != nil {
		readable = actions.NewEquipment(s, comps).Readable() // exactly what the scripts' own gates ask
	}
	guardianBusy := randomevents.Busy(s)
	out.obs.Blind = blindOutsideEvent(readable, fp.TileOK, fp.TileOK && randomevents.InTrapEventSquare(fp.X, fp.Z), guardianBusy)
	phase, _ := runner.Phase()
	out.diag = fmt.Sprintf("at %d,%d,%d; tabs 3/4 %d/%d (blind=%v); modals main/side/chat %d/%d/%d; continue=%v %q; guardian busy=%v; phase %q",
		fp.X, fp.Z, fp.Level, st.SideIcon(3), st.SideIcon(4), out.obs.Blind, main, side, chat, out.obs.CanContinue, text, guardianBusy, phase)
	return out
}

// inGame is stallwatch.Obs.InGame: a live session that is not logging in, reconnecting or
// hopping worlds, and was not deliberately disconnected. A hop logs the account out and back in
// on purpose (worldhop.go), so it is never a stall. Pure.
func inGame(hasSession, connecting, reconnecting, hopping, deliberate bool) bool {
	return hasSession && !connecting && !reconnecting && !hopping && !deliberate
}

// blindOutsideEvent is stallwatch.Obs.Blind: the backpack/worn panels unreadable with the tile
// known, outside the mime/maze squares and with the Guardian idle -- inside an event the tabs are
// the event's business and come back at its mapzoneexit. Pure.
func blindOutsideEvent(readable, tileOK, inTrapSquare, guardianBusy bool) bool {
	return !readable && tileOK && !inTrapSquare && !guardianBusy
}

// lastBotLine is the account's newest activity line that is not the watchdog's own.
func lastBotLine(a *account) string {
	if a.activity == nil {
		return ""
	}
	lines := a.activity.Recent(20)
	for i := len(lines) - 1; i >= 0; i-- {
		if !strings.Contains(lines[i], "] [watchdog] ") {
			return lines[i]
		}
	}
	return ""
}

func (w *watchdog) sweep(now time.Time) {
	w.m.mu.RLock()
	accounts := append([]*account(nil), w.m.accounts...)
	w.m.mu.RUnlock()

	type row struct {
		watchObs
		as stallwatch.Assessment
		h  stallwatch.History
	}
	w.mu.Lock()
	mode := w.mode
	if mode == stallwatch.ModeOff {
		w.status = map[string]WatchdogStatus{}
	}
	w.mu.Unlock()
	if mode == stallwatch.ModeOff {
		return // switched off at runtime (SetWatchdogMode); a switch back resumes on the next sweep
	}
	cfg := w.cfg.Config
	cfg.Mode = mode

	rows := make([]row, 0, len(accounts))
	var orphanCands []watchObs
	fleet := stallwatch.Fleet{StalledByScript: map[string]int{}, InGameByScript: map[string]int{}}
	for _, a := range accounts {
		o := w.observe(a)
		w.mu.Lock()
		prev := w.hist[o.obs.Username]
		busy := w.inFlight[o.obs.Username]
		w.mu.Unlock()
		if busy {
			continue // one action at a time per account; its own goroutine reports the outcome
		}
		var h stallwatch.History
		if prev != nil {
			h = *prev
		}
		as, h := stallwatch.Assess(now, o.obs, h, cfg, w.started)
		// Given-up accounts are left out of the breakers' counts (see stallwatch.Fleet.Count).
		fleet.Count(now, o.obs, as, h, cfg)
		rows = append(rows, row{o, as, h})
		if o.orphan.HasBot {
			orphanCands = append(orphanCands, o)
		}
	}

	w.mu.Lock()
	cut := now.Add(-w.cfg.FleetWindow)
	kept := w.actions[:0]
	for _, t := range w.actions {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	w.actions = kept
	fleet.ActionsInWindow, fleet.ReloginsInFlight = len(w.actions), w.relogins
	stalled, inGame, byScript, inGameByScript := fleet.Stalled, fleet.InGameBots, fleet.StalledByScript, fleet.InGameByScript
	breaker := stallwatch.BreakerOpen(fleet, cfg)
	logBreaker := breaker && now.Sub(w.breakerAt) >= w.cfg.FleetWindow
	if logBreaker {
		w.breakerAt = now
	}
	var scriptTrips []string
	if !breaker {
		for script := range byScript {
			if stallwatch.ScriptBreakerOpen(fleet, script, cfg) && now.Sub(w.scriptBreakerAt[script]) >= w.cfg.FleetWindow {
				w.scriptBreakerAt[script] = now
				scriptTrips = append(scriptTrips, script)
			}
		}
	}
	w.mu.Unlock()
	if logBreaker {
		w.m.logf("[watchdog] CIRCUIT BREAKER: %d of %d running bot accounts are stalled at once (%s) — suspecting a fleet-wide cause (server lag or outage, the network, or a bad build); acting on none of them until the count falls",
			stalled, inGame, describeCounts(byScript))
	}
	sort.Strings(scriptTrips)
	for _, script := range scriptTrips {
		w.m.logf("[watchdog] CIRCUIT BREAKER for %s: %d of its %d running accounts are stalled at once — suspecting that script (or something it depends on), not each account; not acting on %s accounts until the count falls, and still acting on every other script",
			script, byScript[script], inGameByScript[script], script)
	}

	for _, r := range rows {
		d, h := stallwatch.Plan(now, r.as, r.obs, r.h, cfg, fleet)
		name := r.obs.Username
		w.mu.Lock()
		hh := h
		w.hist[name] = &hh
		w.mu.Unlock()
		w.report(now, r.watchObs, r.as, d, h)
		switch d.Action {
		case stallwatch.ActNudge, stallwatch.ActRestart, stallwatch.ActRelogin:
			w.mu.Lock()
			w.actions = append(w.actions, now)
			fleet.ActionsInWindow++
			if d.Action == stallwatch.ActRelogin {
				w.relogins++
				fleet.ReloginsInFlight++
			}
			w.mu.Unlock()
		}
		switch d.Action {
		case stallwatch.ActNudge:
			w.nudge(r.a, r.session)
		case stallwatch.ActRestart:
			w.begin(name)
			go w.restart(r.a)
		case stallwatch.ActRelogin:
			w.begin(name)
			go w.relogin(r.a, d.Why)
		}
		w.setStatus(name, h, d)
	}
	// After the stall ladder, so an account the ladder just began a restart on is already in flight.
	w.sweepOrphans(now, mode, cfg, orphanCands)

	w.mu.Lock()
	summary := now.Sub(w.summaryAt) >= w.cfg.SummaryEvery
	if summary {
		w.summaryAt = now
	}
	c := w.counts
	w.mu.Unlock()
	if summary {
		w.m.logf("[watchdog] summary (mode %s): watching %d running bot accounts, %d stalled now and %d given up on; since start detected %d, nudged %d, restarted %d, relogged %d, gave up %d, held back %d, busy-without-progress reports %d, safety-net starts %d, recovered %s",
			mode, inGame, stalled, fleet.GivenUp, c.Detected, c.Nudged, c.Restarted, c.Relogged, c.GaveUp, c.Held, c.Busy, c.Orphans, describeCounts(c.Recovered))
	}
}

// report writes the lines one sweep owes an account: recoveries, a first sighting (with its
// diagnosis and, rate-limited, a goroutine dump), busy-without-progress, holds and give-ups.
func (w *watchdog) report(now time.Time, o watchObs, as stallwatch.Assessment, d stallwatch.Decision, h stallwatch.History) {
	a := o.a
	if as.Recovered {
		by := "on its own"
		switch as.RecoveredBy {
		case stallwatch.StepNudged, stallwatch.StepRestarted, stallwatch.StepRelogged:
			by = "after the " + as.RecoveredBy.String()
		case stallwatch.StepGaveUp:
			by = "on its own after the watchdog had given up"
		}
		w.m.wlogf(a, "recovered %s: progress again %s after the stall was first seen", by, as.RecoveredAfter.Round(time.Second))
		w.mu.Lock()
		w.counts.Recovered[by]++
		w.mu.Unlock()
	}
	if as.Aborted {
		w.m.wlogf(a, "ladder dropped: the account left the game or its bot stopped for more than %s (%s)", w.cfg.IdleAbort, abortedWhy(o.orphan))
	}
	if as.AbortedOwnStop {
		// Nobody took over: the bot is stopped because THIS watchdog stopped it, and its restart or
		// relogin has not started it again. The safety net does that (watchdog_orphan.go).
		w.m.wlogf(a, "ladder dropped: the bot this watchdog itself stopped (its own restart or relogin) has not been running again for more than %s; nobody else took over -- the safety net starts it again once the account has been in game for %s", w.cfg.IdleAbort, w.cfg.OrphanAfter)
	}
	if d.FirstSeen {
		dump := w.dump(now, o.obs.Username)
		if as.BlindStall {
			w.m.wlogf(a, "STALLED: BLIND for %s (backpack/worn panels unreadable outside any event) and no progress for %s, last line %s ago; last line: %q; %s%s",
				as.BlindFor.Round(time.Second), as.NoProgressFor.Round(time.Second), as.QuietFor.Round(time.Second), lastBotLine(a), o.diag, dump)
		} else {
			w.m.wlogf(a, "STALLED: running but silent for %s and no progress for %s; last line: %q; %s%s",
				as.QuietFor.Round(time.Second), as.NoProgressFor.Round(time.Second), lastBotLine(a), o.diag, dump)
		}
		w.mu.Lock()
		w.counts.Detected++
		w.mu.Unlock()
	}
	switch d.Action {
	case stallwatch.ActReport:
		if d.Why == "stalled" {
			w.m.wlogf(a, "not acting: %s", d.Held)
			w.mu.Lock()
			w.counts.Held++
			w.mu.Unlock()
			return
		}
		w.m.wlogf(a, "writing log lines but no progress for %s — leaving it alone (an account that says what it is waiting for is never restarted); last line: %q; %s",
			as.NoProgressFor.Round(time.Minute), lastBotLine(a), o.diag)
		w.mu.Lock()
		w.counts.Busy++
		w.mu.Unlock()
	case stallwatch.ActGiveUp:
		w.m.wlogf(a, "NEEDS ATTENTION: %s — leaving the account alone for %s; stalled since %s; %s",
			d.Why, w.cfg.GiveUpHold, h.StallFrom.Format("15:04:05"), o.diag)
		w.mu.Lock()
		w.counts.GaveUp++
		w.mu.Unlock()
	}
}

func (w *watchdog) setStatus(name string, h stallwatch.History, d stallwatch.Decision) {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch {
	case h.Step == stallwatch.StepGaveUp:
		detail := d.Why
		if prev, ok := w.status[name]; ok && detail == "" {
			detail = prev.Detail
		}
		w.status[name] = WatchdogStatus{State: "needs_attention", Since: fmtTime(h.StallFrom), Step: h.Step.String(), Ladders: len(h.Ladders), Detail: detail}
	case h.Detected:
		detail := d.Held
		if d.Action != stallwatch.ActNone && d.Action != stallwatch.ActReport {
			detail = d.Action.String() + " taken"
		} else if prev, ok := w.status[name]; ok && detail == "" {
			detail = prev.Detail
		}
		st := WatchdogStatus{State: "stalled", Since: fmtTime(h.StallFrom), Ladders: len(h.Ladders), Detail: detail}
		if h.Step != stallwatch.StepNone {
			st.Step = h.Step.String()
		}
		w.status[name] = st
	default:
		delete(w.status, name)
	}
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func (w *watchdog) begin(name string) {
	w.mu.Lock()
	w.inFlight[name] = true
	w.mu.Unlock()
}

// end closes an action. The next sweep re-baselines the account's fingerprint: a restart or a
// relogin is not progress, and a relogin's new session starts its inventory generation counters
// again, which compared against the old ones would read as a change that never happened.
func (w *watchdog) end(name string) {
	w.mu.Lock()
	delete(w.inFlight, name)
	if h := w.hist[name]; h != nil {
		h.Seen = false
	}
	w.mu.Unlock()
}

// nudge presses an open dialogue's continue, else closes whatever modal is open. It sends at
// most one packet and never walks, so it cannot make a stuck account worse.
func (w *watchdog) nudge(a *account, s *client.Session) {
	w.mu.Lock()
	w.counts.Nudged++
	w.mu.Unlock()
	if s == nil {
		w.m.wlogf(a, "nudge: no session any more; nothing sent")
		return
	}
	w.nudgeOn(a, s)
}

// nudgeOn is nudge on a live session. It sends nothing at all while the Guardian is handling a
// random event: a press or a close then could be the wrong answer to the event itself.
func (w *watchdog) nudgeOn(a *account, s actions.Session) {
	if w.guardianBusy(s) {
		w.m.wlogf(a, "nudge: the Guardian is handling a random event; nothing sent, the next step waits its recheck")
		return
	}
	st := s.State()
	if comps := w.m.cfg.Components; comps != nil {
		dlg := actions.NewChatDialog(s, comps)
		if dlg.CanContinue() {
			text := strings.Join(dlg.Texts(), " ")
			err := dlg.Continue()
			w.m.wlogf(a, "nudge: pressed continue on %q (err=%v); tabs 3/4 now %d/%d — rechecking in %s", text, err, st.SideIcon(3), st.SideIcon(4), w.cfg.RecheckAfter)
			return
		}
	}
	main, side, chat := st.Modals()
	if main != -1 || side != -1 || chat != -1 {
		err := actions.CloseModal(s)
		w.m.wlogf(a, "nudge: closed the open modal main/side/chat %d/%d/%d (err=%v) — rechecking in %s", main, side, chat, err, w.cfg.RecheckAfter)
		return
	}
	w.m.wlogf(a, "nudge: nothing was open to press or close by the time it ran — rechecking in %s", w.cfg.RecheckAfter)
}

// restart stops the bot, waits for its runner to finish, and starts the same script with the same
// settings. It finishes the job however long the old bot takes to wind down: after the first
// RestartWait it says so and keeps waiting, for up to RestartFinishWait more, because a restart that
// gives up there leaves the account with NO bot once the old one ends (18 October 2026: a ScarabSwarm
// bot took two minutes to stop, the restart abandoned it after 15 seconds, and the account stood in
// game with no bot for eight hours). It never starts a second copy beside a live one: it starts only
// after the old runner has finished, and only if no other bot has been started and no operator has
// stopped it meanwhile (startAgain). A runner still alive after both waits is left alone, and the
// safety net starts the script if that runner ever ends.
func (w *watchdog) restart(a *account) {
	defer w.end(a.username)
	w.mu.Lock()
	w.counts.Restarted++
	w.mu.Unlock()
	a.mu.RLock()
	script, runner := a.script, a.runner
	a.mu.RUnlock()
	if runner == nil || script == "" {
		w.m.wlogf(a, "restart: no bot to restart")
		return
	}
	settings := runner.Settings()
	w.m.wlogf(a, "restart: stopping %s and starting it again with the same settings", script)
	// The watchdog's own stop, not an operator's: it marks the account so that nothing mistakes this
	// stop for somebody's decision (account.stopBy; stallwatch.Obs.OwnStop).
	if err := w.m.stopBot(a.username, stopWatchdog); err != nil {
		w.m.wlogf(a, "restart: stop failed: %v", err)
		return
	}
	done := runner.Done()
	select {
	case <-done:
	case <-time.After(w.cfg.RestartWait):
		dump := w.dump(time.Now(), a.username)
		w.m.wlogf(a, "restart: the bot did not stop within %s — NOT starting a second copy beside it; waiting up to %s more for it to end, then starting %s again%s", w.cfg.RestartWait, w.cfg.RestartFinishWait, script, dump)
		waited := time.NewTimer(w.cfg.RestartFinishWait)
		defer waited.Stop()
		select {
		case <-done:
			w.m.wlogf(a, "restart: the old %s bot has ended", script)
		case <-waited.C:
			w.m.wlogf(a, "restart: the old %s bot has still not ended after %s more; not starting a second copy beside it -- the safety net starts %s if it ever ends", script, w.cfg.RestartFinishWait, script)
			return
		case <-w.m.stop:
			return
		}
	case <-w.m.stop:
		return
	}
	if w.startAgain(a, runner, script, settings, "restart") {
		w.m.wlogf(a, "restart: %s is running again — rechecking after %s of fresh silence", script, w.cfg.StallAfter)
	}
}

// relogin logs the account out and queues it back in through the ordinary login path, marked so
// the reconnect restarts its script. Counted as in flight until the account is back in game with
// its bot running, or ReloginWait passes; either way the reconnect loop owns the login itself.
func (w *watchdog) relogin(a *account, why string) {
	defer func() {
		w.mu.Lock()
		w.relogins--
		w.mu.Unlock()
		w.end(a.username)
	}()
	w.mu.Lock()
	w.counts.Relogged++
	w.mu.Unlock()
	a.mu.RLock()
	script := a.script
	a.mu.RUnlock()
	reason := ""
	if why != "" && why != "stalled" {
		reason = " (" + why + ")"
	}
	w.m.wlogf(a, "relogin%s: logging out and back in through the fleet's login pacing (a fresh login restores the sidebar and closes every server-side interface), then restarting %s", reason, script)
	started := time.Now()
	if err := w.m.disconnect(a.username, stopWatchdog); err != nil {
		w.m.wlogf(a, "relogin: logout failed: %v", err)
	}
	if !w.m.waitSessionGone(a, hopSessionGoneTimeout) {
		w.m.clearDeliberate(a)
		w.m.wlogf(a, "relogin: the old session did not close within %s; handed back to the ordinary reconnect path", hopSessionGoneTimeout)
		return
	}
	settle := w.settle
	if settle <= 0 {
		settle = hopSettle
	}
	if !waitLoginRetry(w.m.stop, settle) {
		// The fleet is shutting down; still, never leave the account parked as deliberately
		// disconnected by a watchdog that is no longer there to log it back in.
		w.m.clearDeliberate(a)
		return
	}
	if !w.requeueLogin(a, script) {
		return
	}
	deadline := time.Now().Add(w.cfg.ReloginWait)
	for time.Now().Before(deadline) {
		if !waitLoginRetry(w.m.stop, 2*time.Second) {
			return
		}
		a.mu.RLock()
		back := a.session != nil && a.runner != nil
		runner := a.runner
		a.mu.RUnlock()
		if back && runner.Status().State == bot.StateRunning && runner.Status().StartedAt.After(started) {
			w.m.wlogf(a, "relogin: back in game after %s with %s running again — rechecking after %s of fresh silence",
				time.Since(started).Round(time.Second), script, w.cfg.StallAfter)
			return
		}
	}
	w.m.wlogf(a, "relogin: not back in game with its bot running after %s; the reconnect loop keeps trying on its own schedule", w.cfg.ReloginWait)
}

// guardianBusy is randomevents.Busy, or the test's stand-in.
func (w *watchdog) guardianBusy(s actions.Session) bool {
	if w.busyFn != nil {
		return w.busyFn(s)
	}
	return randomevents.Busy(s)
}

// requeueLogin queues the relogin's login with the account marked to resume its script, retrying
// a refused ConnectExisting a bounded number of times: straight after the logout, the reconnect
// loop the dropped session woke is usually still winding down ("finishing disconnect; retry
// shortly"). If the login cannot be queued at all, the account is NOT left as it was -- logged
// out with deliberateDisconnect set, which nothing ever clears and which also hides it from the
// watchdog as "not in game", i.e. stranded offline and invisible. Instead the flag is cleared, the
// error is recorded as the account's last error, and the ladder is closed as "needs attention"
// so /api/status, the panel and the Discord alert all show it. Returns true once a login is
// queued (or another path already has the account connecting or connected).
func (w *watchdog) requeueLogin(a *account, script string) bool {
	connect := w.connectFn
	if connect == nil {
		connect = w.m.ConnectExisting
	}
	attempts, every := w.connectAttempts, w.connectRetry
	if attempts <= 0 {
		attempts = 6
	}
	if every <= 0 {
		every = 5 * time.Second
	}
	a.mu.Lock()
	a.resumeScript = script != ""
	a.mu.Unlock()
	var err error
	for i := 1; i <= attempts; i++ {
		if err = connect(a.username); err == nil {
			if i > 1 {
				w.m.wlogf(a, "relogin: login queued on attempt %d of %d", i, attempts)
			}
			return true
		}
		a.mu.RLock()
		elsewhere := a.session != nil || a.connecting
		a.mu.RUnlock()
		if elsewhere {
			w.m.wlogf(a, "relogin: another path is already logging it in (%v); leaving the login to it", err)
			return true
		}
		if i < attempts {
			w.m.wlogf(a, "relogin: could not queue the login (attempt %d of %d): %v -- retrying in %s", i, attempts, err, every)
			if !waitLoginRetry(w.m.stop, every) {
				w.m.clearDeliberate(a)
				return false
			}
		}
	}
	// Give the account back to every ordinary recovery path, and make the failure visible.
	a.mu.Lock()
	a.deliberateDisconnect = false
	a.lastErr = "watchdog relogin could not log it back in: " + err.Error()
	a.mu.Unlock()
	now := time.Now()
	why := fmt.Sprintf("the relogin logged it out but could not queue the login again after %d attempts (%v); it is logged out -- connect it from the panel", attempts, err)
	w.mu.Lock()
	if h := w.hist[a.username]; h != nil {
		h.Step, h.GaveUpAt, h.StepAt = stallwatch.StepGaveUp, now, now
		if h.StallFrom.IsZero() {
			h.StallFrom = now
		}
	}
	w.counts.GaveUp++
	w.status[a.username] = WatchdogStatus{State: "needs_attention", Since: fmtTime(now), Step: stallwatch.StepGaveUp.String(), Detail: why}
	w.mu.Unlock()
	w.m.wlogf(a, "NEEDS ATTENTION: %s", why)
	return false
}

// dump writes one goroutine dump, rate-limited fleet-wide, and returns a suffix naming the file.
func (w *watchdog) dump(now time.Time, username string) string {
	if w.cfg.DumpDir == "" {
		return ""
	}
	w.mu.Lock()
	if w.dumps >= w.cfg.MaxDumps || (!w.lastDump.IsZero() && now.Sub(w.lastDump) < w.cfg.DumpEvery) {
		w.mu.Unlock()
		return ""
	}
	w.lastDump = now
	w.dumps++
	w.mu.Unlock()
	if err := os.MkdirAll(w.cfg.DumpDir, 0o755); err != nil {
		return fmt.Sprintf("; goroutine dump failed: %v", err)
	}
	path := filepath.Join(w.cfg.DumpDir, fmt.Sprintf("watchdog-%s-%s.txt", now.Format("20060102-150405"), sanitizeFileName(username)))
	f, err := os.Create(path)
	if err != nil {
		return fmt.Sprintf("; goroutine dump failed: %v", err)
	}
	defer f.Close()
	if err := pprof.Lookup("goroutine").WriteTo(f, 2); err != nil {
		return fmt.Sprintf("; goroutine dump failed: %v", err)
	}
	return "; goroutine dump " + path
}

func sanitizeFileName(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, s)
}

func describeCounts(m map[string]int) string {
	if len(m) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		name := k
		if name == "" {
			name = "(no script)"
		}
		parts = append(parts, fmt.Sprintf("%s %d", name, m[k]))
	}
	return strings.Join(parts, ", ")
}
