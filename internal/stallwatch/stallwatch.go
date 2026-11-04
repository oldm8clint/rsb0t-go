// Package stallwatch is the pure decision behind the fleet's stall watchdog
// (internal/fleet/watchdog.go): given what one account looked like on one sweep, and what the
// watchdog remembers about it, is the account stalled, and what -- if anything -- should be done
// about it now. No session, no clock, no I/O: the caller passes the time and the observation.
//
// WHY IT EXISTS. On 2026-09-23 Acct188 (ProgressiveWCFletch) sat for twenty minutes with
// botState "running", no XP, no movement and not one log line, and a bot restart did not help;
// across the archived logs the same silence happened 17 times to 14 accounts, ~22.6
// account-hours, and nothing noticed any of them. The root cause is fixed where it lives (the
// Guardian presses a trapped event's reward dialogue; the woodcutter's blind gate is bounded).
// This is the backstop for the next cause nobody has found yet.
//
// WHAT "STALLED" MEANS, and why it is this conservative. An account is stalled only when it is
// in game with its bot running and, for StallAfter (15 minutes by default: the longest known
// silent wait, KnownSilentWaits, plus SilenceMargin -- see MinStallAfter), BOTH:
//
//   - it has made no progress: no change of tile, total XP, pack/worn/bank contents or world,
//     and no rise in hitpoints (so an account resting to heal is progressing); and
//   - it has written no log line at all.
//
// ONE EXCEPTION, BLINDNESS (BlindAfter, 5 minutes by default): an account whose backpack and
// worn panels have been unreadable outside any event (Obs.Blind) for BlindAfter, with no
// progress for as long, is stalled even while it is still writing lines. Its lines are the
// woodcutter family's own blind gate saying it cannot see (internal/bot/loopgate.go), and no
// wait ends that: the tabs are the server's, and only a press or a relog brings them back (the
// main session's live finding, 2026-09-23 05:20: "after about 60 s ... request a RELOG").
//
// Otherwise, a bot that is waiting and SAYING SO is never acted on: "still waiting on a random event", a
// donor waiting at the meet, a mule queue wait, a throttled warning -- any line resets the
// silence clock. Measured over 27,637 archived account-hours, every harmless silence was 9.2
// minutes or less (donor meet waits up to 5.7, throttled warnings 5 or 10, resting up to 6.8,
// the fisher at most 9.2), while the Mime class ran 18.6 to 256 minutes. Those archives predate
// the donor's meet cap being raised to 10 minutes (donorplan.MeetTimeoutComing, 2026-09-23), and
// a donor logs nothing while it waits that out, so the default StallAfter is not "a bit over the
// measured 9.2" but a margin over every silent wait the code is KNOWN to allow (KnownSilentWaits);
// a test pins it against the donor constant so raising that cap again fails the build's tests
// rather than restarting healthy donors. An account that logs but makes no progress for
// BusyAfter is reported (a line, rate-limited) and never touched.
//
// The fleet default is ModeObserve: detect, diagnose and report, act on nothing, until an
// operator has watched a day of its reports and switches it (the control panel's
// POST /api/watchdog changes the mode at runtime, no restart).
//
// THE LADDER, one step per stall, each re-checked before the next:
//
//	nudge    press an open dialogue's continue, or close an open modal (skipped when nothing is
//	         open). The lab proved one continue clears the Mime class in 0.2s, and it costs nothing.
//	restart  stop the bot and start the same script with the same settings. SKIPPED for a
//	         BLIND account (backpack/worn panels unreadable outside any event, Obs.Blind): the
//	         tabs are the server's, and a restart is proven not to bring them back.
//	relogin  log out and back in through the fleet's ordinary login pacing; a fresh login runs
//	         ~initalltabs and closes every server-side interface. This is what finally freed
//	         Acct188, Acct206 and Acct216 live.
//	give up  mark the account "needs attention" (visible in /api/status and as its last error),
//	         alert, and leave it alone for GiveUpHold.
//
// A step is taken only when the account is still stalled by the SAME definition -- so after a
// restart, whose own start lines reset the silence clock, the next step needs another full
// StallAfter of silence. Rate limits: at most MaxLadders ladders per account per LadderWindow;
// fleet-wide at most FleetActions actions per FleetWindow and MaxRelogins relogins in flight; and
// two circuit breakers. One per script holds just that script when max(BreakerMin, BreakerFrac of
// its running accounts) are stalled at once -- a systemic script bug, which restarting its cohort
// one by one would not fix and would hide. One fleet-wide acts on nobody when two scripts trip at
// once or the stalls outside a tripped script reach max(BreakerMin, BreakerFrac of all running bot
// accounts), because that is a server, network or build problem that restarts would make worse.
// An account the watchdog has already GIVEN UP on (inside its GiveUpHold) does not count toward
// either breaker (Fleet.Count): it is an operator's problem now, and left in, a handful of them
// would hold every future stall fleet-wide until someone fixed them by hand.
//
// THE OTHER HALF, THE SAFETY NET (orphan.go): everything above judges a bot that is running. An
// account in game whose bot is NOT running, with nobody having meant that, is judged by OrphanPlan:
// it is started again with the script and settings it already had, after OrphanAfter, unless an
// operator stopped it or the script ended itself.
package stallwatch

import (
	"math"
	"strings"
	"time"
)

// Mode is how much the watchdog is allowed to do.
type Mode string

const (
	// ModeOff disables the watchdog entirely.
	ModeOff Mode = "off"
	// ModeObserve detects, diagnoses and reports, and acts on nothing. The default.
	ModeObserve Mode = "observe"
	// ModeNudge also presses a continue / closes a modal, and stops there (then reports).
	ModeNudge Mode = "nudge"
	// ModeAct runs the whole ladder.
	ModeAct Mode = "act"
)

// Modes lists every mode, for a flag's help or an API answer.
func Modes() []Mode { return []Mode{ModeOff, ModeObserve, ModeNudge, ModeAct} }

// SilentWait is one wait the code is known to allow that writes NO log line and makes no
// progress for its whole length -- exactly what the stall rule looks for -- and ends on its own.
type SilentWait struct {
	What string
	Max  time.Duration
}

// KnownSilentWaits are the longest such waits in the code today. Each is a bound some other
// package owns; the stall threshold must stay above every one of them by SilenceMargin, or the
// watchdog restarts accounts that are doing exactly what they should. The donor entry is pinned
// by a test against donorplan.MeetTimeoutComing itself, so raising that cap cannot quietly
// undercut the watchdog. Add an entry (and, where the bound is a constant in a pure package, a
// pin) whenever a new silent wait is found or an existing one is lengthened.
var KnownSilentWaits = []SilentWait{
	// donorplan.MeetTimeoutComing: a donor told "on its way" stands at the meet tile and logs
	// nothing until the mule arrives or this cap passes (raised 5 -> 10 minutes on 2026-09-23;
	// MeetWait is a bare delay). The review measured 2.3% of sweep phases restarting such a donor
	// at the old 10-minute threshold.
	{"a donor waiting at the mule's meet tile (donorplan.MeetTimeoutComing)", 10 * time.Minute},
	// The woodcutter family's throttled warnings (e.g. no axe at the mule) repeat every 10
	// minutes and nothing moves in between.
	{"a throttled 10-minute warning with nothing moving in between", 10 * time.Minute},
	// The longest harmless silence measured in 27,637 archived account-hours (a fisher).
	{"the longest harmless silence in the archived logs (a fisher)", 9*time.Minute + 12*time.Second},
}

// SilenceMargin is how far StallAfter must stay above the longest known silent wait: room for
// the loop pass that writes the line after a wait ends, a sweep's phase, and a slow server.
const SilenceMargin = 5 * time.Minute

// LongestKnownSilentWait is the longest entry of KnownSilentWaits.
func LongestKnownSilentWait() SilentWait {
	var out SilentWait
	for _, w := range KnownSilentWaits {
		if w.Max > out.Max {
			out = w
		}
	}
	return out
}

// MinStallAfter is the least StallAfter that cannot act on a known silent wait: the longest
// known one plus SilenceMargin. DefaultConfig uses exactly this; the fleet warns when a flag
// sets anything lower (a lab run may, on purpose).
func MinStallAfter() time.Duration { return LongestKnownSilentWait().Max + SilenceMargin }

// ParseMode reads a -watchdog flag value.
func ParseMode(s string) (Mode, bool) {
	switch m := Mode(strings.ToLower(strings.TrimSpace(s))); m {
	case ModeOff, ModeObserve, ModeNudge, ModeAct:
		return m, true
	}
	return "", false
}

// Config is every knob, with DefaultConfig's values documented there.
type Config struct {
	Mode       Mode
	StallAfter time.Duration
	// BlindAfter is how long an account may be blind (Obs.Blind) without progress before it is
	// stalled whatever it logs; 0 disables that path, leaving only StallAfter.
	BlindAfter   time.Duration
	RecheckAfter time.Duration
	StartGrace   time.Duration
	FleetGrace   time.Duration
	BusyAfter    time.Duration
	BusyEvery    time.Duration
	IdleAbort    time.Duration
	MaxLadders   int
	LadderWindow time.Duration
	GiveUpHold   time.Duration
	FleetActions int
	FleetWindow  time.Duration
	MaxRelogins  int
	BreakerMin   int
	BreakerFrac  float64
	// OrphanAfter is how long a bot nobody meant to stop may stay stopped on an account that is in
	// game before the safety net starts its script again (OrphanPlan, orphan.go); 0 turns the net
	// off. OrphanMaxStarts is the most starts per account inside LadderWindow and OrphanPerSweep the
	// most per sweep fleet-wide.
	OrphanAfter     time.Duration
	OrphanMaxStarts int
	OrphanPerSweep  int
	// Exempt holds script names and account names, lower-cased (see IsExempt).
	Exempt map[string]bool
}

// DefaultConfig is what the fleet runs with unless a flag says otherwise: OBSERVE, so a new
// build reports what it would have done and touches nothing until an operator switches it.
func DefaultConfig() Config {
	return Config{
		Mode:         ModeObserve,
		StallAfter:   MinStallAfter(), // 15m: the donor's 10-minute silent meet wait plus a 5-minute margin
		BlindAfter:   5 * time.Minute, // blind outside any event is never a wait that ends on its own
		RecheckAfter: 3 * time.Minute,
		StartGrace:   10 * time.Minute, // after a bot start, a login or a reconnect
		FleetGrace:   15 * time.Minute, // after the watchdog itself starts (a fleet start)
		BusyAfter:    30 * time.Minute,
		BusyEvery:    60 * time.Minute,
		IdleAbort:    15 * time.Minute,
		MaxLadders:   3,
		LadderWindow: 6 * time.Hour,
		GiveUpHold:   2 * time.Hour,
		FleetActions: 6,
		FleetWindow:  10 * time.Minute,
		MaxRelogins:  2,
		BreakerMin:   5,
		BreakerFrac:  0.03,
		// Long enough that a hop's or a relogin's own restart (a minute or two after the login) is
		// never raced, short enough that a bot left stopped is back within a few minutes.
		OrphanAfter:     5 * time.Minute,
		OrphanMaxStarts: 3,
		OrphanPerSweep:  3,
		// The mule's whole job is waiting at its post, and it had 0 silences in 27k account-hours.
		Exempt: map[string]bool{"mulereceiver": true},
	}
}

// ParseExempt turns a comma-separated list of script and account names into Config.Exempt.
func ParseExempt(s string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.Split(s, ",") {
		if p := strings.ToLower(strings.TrimSpace(part)); p != "" {
			out[p] = true
		}
	}
	return out
}

// IsExempt reports whether an account's script or name is on the exempt list.
func (c Config) IsExempt(username, script string) bool {
	return c.Exempt[strings.ToLower(script)] || c.Exempt[strings.ToLower(username)]
}

// Fingerprint is everything that counts as progress. Any change is progress, except that
// hitpoints count only when they rise (an account healing is working; one being hit is the
// Guardian's business, and its lines already reset the silence clock).
type Fingerprint struct {
	TileOK      bool
	X, Z, Level int
	TotalXP     int
	PackGen     int
	WornGen     int
	BankGen     int
	HP          int
	World       int
}

// progressed reports whether now differs from before in any way that counts.
func progressed(before, now Fingerprint) bool {
	if before.TileOK != now.TileOK || (now.TileOK && (before.X != now.X || before.Z != now.Z || before.Level != now.Level)) {
		return true
	}
	return before.TotalXP != now.TotalXP || before.PackGen != now.PackGen || before.WornGen != now.WornGen ||
		before.BankGen != now.BankGen || before.World != now.World || now.HP > before.HP
}

// Obs is one account on one sweep.
type Obs struct {
	Username string
	Script   string
	// InGame is a live session that is not logging in, reconnecting or hopping, and was not
	// deliberately disconnected. Running is the bot runner's own "running" state.
	InGame       bool
	Running      bool
	BotStartedAt time.Time
	ConnectedAt  time.Time
	// LastLineAt is the account's last log line that was not the watchdog's own.
	LastLineAt time.Time
	Print      Fingerprint
	// ModalOpen/CanContinue decide whether a nudge has anything to press.
	ModalOpen   bool
	CanContinue bool
	// OwnStop is the bot not running because the watchdog itself stopped it (a restart or a
	// relogin) and no later start has replaced it. Idle time in that state is the watchdog's own
	// doing: Assess never reports it as somebody else taking over (Assessment.AbortedOwnStop).
	OwnStop bool
	// Blind is the backpack and worn panels being unreadable (sidebar tabs 3/4 null) while no
	// random event owns the account: the caller leaves it false inside the mime/maze squares and
	// while the Guardian is busy. A blind account's ladder SKIPS THE RESTART: the tabs are the
	// server's, a new bot instance on the same session reaches the same refusal (Acct188 re-froze
	// straight after one at 03:20 on 2026-09-23), and only a fresh login -- whose ~initalltabs
	// sends them again -- or pressing whatever holds them back has ever restored them.
	Blind bool
}

// Step is how far up the ladder an account's current stall has gone.
type Step int

const (
	StepNone Step = iota
	StepNudged
	StepRestarted
	StepRelogged
	StepGaveUp
)

func (s Step) String() string {
	switch s {
	case StepNudged:
		return "nudge"
	case StepRestarted:
		return "restart"
	case StepRelogged:
		return "relogin"
	case StepGaveUp:
		return "give up"
	}
	return "none"
}

// History is what the watchdog remembers about one account between sweeps.
type History struct {
	Seen           bool
	Last           Fingerprint
	LastProgressAt time.Time
	Script         string
	IdleSince      time.Time
	// BlindSince is when the account's current unbroken run of Obs.Blind sweeps began (zero
	// while it can see, and reset whenever it leaves the game or its bot stops).
	BlindSince time.Time

	Step   Step
	StepAt time.Time
	// SkippedRestart is this ladder having gone straight to the relogin because the account was
	// blind (Obs.Blind), so a give-up says what was actually tried.
	SkippedRestart bool
	StallFrom      time.Time   // when the current stall was first detected
	Ladders        []time.Time // when each ladder started, pruned to LadderWindow
	GaveUpAt       time.Time

	BusyReportedAt time.Time
	Detected       bool // the current stall has been reported
}

// Status is what an account is, for the watchdog, on this sweep.
type Status int

const (
	StatusIdle Status = iota // not in game, or no bot running: nothing to watch
	StatusExempt
	StatusGrace
	StatusOK
	StatusBusy    // writing lines, but no progress for BusyAfter: reported, never touched
	StatusStalled // silent AND no progress for StallAfter
)

func (s Status) String() string {
	return [...]string{"idle", "exempt", "grace", "ok", "busy-no-progress", "stalled"}[s]
}

// Assessment is Assess's answer.
type Assessment struct {
	Status        Status
	QuietFor      time.Duration
	NoProgressFor time.Duration
	// BlindFor is how long the account has been blind without a break; BlindStall is this
	// stall being the blindness path (BlindAfter) rather than the silent one (StallAfter).
	BlindFor   time.Duration
	BlindStall bool
	// Recovered is set on the sweep an account with a ladder open made progress again.
	Recovered      bool
	RecoveredBy    Step
	RecoveredAfter time.Duration
	// Aborted is set when an open ladder was dropped because the account left the game or its
	// bot stopped for longer than IdleAbort, and the watchdog did not stop that bot itself
	// (an operator, a hop, a reconnect or the script ending took it out of the ladder).
	Aborted bool
	// AbortedOwnStop is set instead of Aborted when the bot was stopped by the watchdog's own
	// restart or relogin (Obs.OwnStop): nobody took over, the watchdog's own restart never
	// finished. The caller must not describe it as an operator or a hop.
	AbortedOwnStop bool
}

// Assess folds one observation into h and classifies the account. It never decides an action.
func Assess(now time.Time, o Obs, h History, cfg Config, watchStarted time.Time) (Assessment, History) {
	var a Assessment
	if h.Script != o.Script {
		// A different script is a different account as far as a ladder goes.
		h = History{Ladders: h.Ladders, GaveUpAt: h.GaveUpAt}
		h.Script = o.Script
	}
	if !o.InGame || !o.Running {
		if h.IdleSince.IsZero() {
			h.IdleSince = now
		}
		// Idle time is neither progress nor its absence: re-baseline when watching resumes.
		h.Seen = false
		h.BlindSince = time.Time{}
		if h.Step != StepNone && h.Step != StepGaveUp && now.Sub(h.IdleSince) > cfg.IdleAbort {
			h.Step, h.StepAt, h.Detected, h.SkippedRestart = StepNone, time.Time{}, false, false
			if o.OwnStop {
				a.AbortedOwnStop = true
			} else {
				a.Aborted = true
			}
		}
		a.Status = StatusIdle
		return a, h
	}
	h.IdleSince = time.Time{}
	switch {
	case !o.Blind:
		h.BlindSince = time.Time{}
	case h.BlindSince.IsZero():
		h.BlindSince = now
	}
	if !h.Seen {
		h.Seen, h.Last, h.LastProgressAt = true, o.Print, now
	} else if !progressed(h.Last, o.Print) {
		// A fall in hitpoints is not progress, but it lowers the bar a later heal must clear:
		// an account resting after a fight is working, however far below the baseline it fell.
		if o.Print.HP < h.Last.HP {
			h.Last.HP = o.Print.HP
		}
	} else {
		h.Last, h.LastProgressAt = o.Print, now
		if h.Step != StepNone {
			a.Recovered, a.RecoveredBy = true, h.Step
			if !h.StallFrom.IsZero() {
				a.RecoveredAfter = now.Sub(h.StallFrom)
			}
			h.Step, h.StepAt, h.StallFrom, h.Detected, h.SkippedRestart = StepNone, time.Time{}, time.Time{}, false, false
			if a.RecoveredBy == StepGaveUp {
				h.GaveUpAt = time.Time{}
			}
		} else if h.Detected {
			// Reported but not yet acted on (observe mode, or held back): it came back by itself.
			a.Recovered = true
			if !h.StallFrom.IsZero() {
				a.RecoveredAfter = now.Sub(h.StallFrom)
			}
			h.StallFrom, h.Detected = time.Time{}, false
		}
	}
	a.NoProgressFor = now.Sub(h.LastProgressAt)
	lastLine := o.LastLineAt
	if lastLine.IsZero() || lastLine.Before(o.BotStartedAt) {
		lastLine = o.BotStartedAt
	}
	a.QuietFor = now.Sub(lastLine)
	if !h.BlindSince.IsZero() {
		a.BlindFor = now.Sub(h.BlindSince)
	}
	blindStall := cfg.BlindAfter > 0 && o.Blind && a.BlindFor >= cfg.BlindAfter && a.NoProgressFor >= cfg.BlindAfter

	switch {
	case cfg.IsExempt(o.Username, o.Script):
		a.Status = StatusExempt
	case now.Sub(watchStarted) < cfg.FleetGrace, now.Sub(o.BotStartedAt) < cfg.StartGrace, now.Sub(o.ConnectedAt) < cfg.StartGrace:
		a.Status = StatusGrace
	case a.NoProgressFor >= cfg.StallAfter && a.QuietFor >= cfg.StallAfter:
		a.Status = StatusStalled
	case blindStall:
		// Blind and going nowhere: stalled even though it may still be logging (see the package doc).
		a.Status, a.BlindStall = StatusStalled, true
	case a.NoProgressFor >= cfg.BusyAfter:
		a.Status = StatusBusy
	default:
		a.Status = StatusOK
	}
	return a, h
}

// Action is one thing the watchdog does.
type Action int

const (
	ActNone   Action = iota
	ActReport        // a line only: a stall first seen (in observe mode, or held back), or busy-no-progress
	ActNudge
	ActRestart
	ActRelogin
	ActGiveUp
)

func (a Action) String() string {
	return [...]string{"none", "report", "nudge", "restart", "relogin", "give up"}[a]
}

// Fleet is the fleet-wide state one sweep decides against.
type Fleet struct {
	// Stalled and InGameBots are this sweep's counts, taken before any decision, and the same
	// two per script (by Obs.Script).
	Stalled         int
	InGameBots      int
	StalledByScript map[string]int
	InGameByScript  map[string]int
	// ActionsInWindow is how many actions were taken in the last FleetWindow;
	// ReloginsInFlight is how many relogins have not finished.
	ActionsInWindow  int
	ReloginsInFlight int
	// GivenUp is how many stalled accounts this sweep were left out of Stalled/StalledByScript
	// because the watchdog had already given up on them (see Count). Reported, never a breaker.
	GivenUp int
}

// Count adds one assessed account to this sweep's tallies: every in-game, running, non-exempt bot
// to InGameBots/InGameByScript, and every stalled one to Stalled/StalledByScript -- EXCEPT one
// the watchdog has already given up on and is still holding (h.Step == StepGaveUp inside
// GiveUpHold), which goes to GivenUp instead. Without that exception given-up accounts stayed
// StatusStalled on every sweep and counted toward the breakers indefinitely: five given-up
// ProgressiveWCFletch accounts held every new WCFletch stall, and nine spread over the fleet
// could hold every stall fleet-wide, until an operator fixed them by hand. h is the History that
// Assess returned for this sweep.
func (f *Fleet) Count(now time.Time, o Obs, a Assessment, h History, cfg Config) {
	if o.InGame && o.Running && a.Status != StatusExempt {
		f.InGameBots++
		if f.InGameByScript == nil {
			f.InGameByScript = map[string]int{}
		}
		f.InGameByScript[o.Script]++
	}
	if a.Status != StatusStalled {
		return
	}
	if h.Step == StepGaveUp && now.Sub(h.GaveUpAt) < cfg.GiveUpHold {
		f.GivenUp++
		return
	}
	f.Stalled++
	if f.StalledByScript == nil {
		f.StalledByScript = map[string]int{}
	}
	f.StalledByScript[o.Script]++
}

func breakerThreshold(inGame int, cfg Config) int {
	threshold := int(math.Ceil(cfg.BreakerFrac * float64(inGame)))
	if threshold < cfg.BreakerMin {
		threshold = cfg.BreakerMin
	}
	return threshold
}

// ScriptBreakerOpen reports whether so many of ONE script's accounts are stalled at once that the
// cause is that script (or what it depends on), not each account: max(BreakerMin, BreakerFrac of
// the script's running accounts). Restarting a script's whole cohort one by one would not fix a
// systemic bug and would hide it; holding only that script keeps the watchdog working for every
// other one. (Measured: 119 ScarabSwarm stage-2 silences in a handful of runs, not root-caused.)
func ScriptBreakerOpen(f Fleet, script string, cfg Config) bool {
	return f.StalledByScript[script] >= breakerThreshold(f.InGameByScript[script], cfg)
}

// BreakerOpen reports whether the stalls look fleet-wide -- a server, network or build problem --
// so the watchdog acts on nobody: either two or more scripts have tripped their own breaker, or
// the stalls outside any tripped script alone reach max(BreakerMin, BreakerFrac of all running
// bot accounts).
func BreakerOpen(f Fleet, cfg Config) bool {
	outside, tripped := f.Stalled, 0
	for script, n := range f.StalledByScript {
		if ScriptBreakerOpen(f, script, cfg) {
			tripped++
			outside -= n
		}
	}
	return tripped >= 2 || outside >= breakerThreshold(f.InGameBots, cfg)
}

// Decision is Plan's answer. Held says why an action that was due was not taken this sweep.
// FirstSeen is set on the sweep a stall is first detected, whatever the action, so the caller
// writes its diagnosis (and a goroutine dump) once per stall rather than once per step.
type Decision struct {
	Action    Action
	Why       string
	Held      string
	FirstSeen bool
}

// Plan decides what to do about one assessed account. It updates h for the action it returns,
// so the caller must record the returned History whether or not the action succeeds.
func Plan(now time.Time, a Assessment, o Obs, h History, cfg Config, f Fleet) (Decision, History) {
	h.Ladders = pruneLadders(h.Ladders, now, cfg.LadderWindow)
	if cfg.Mode == ModeOff {
		return Decision{}, h
	}
	switch a.Status {
	case StatusBusy:
		if h.BusyReportedAt.IsZero() || now.Sub(h.BusyReportedAt) >= cfg.BusyEvery {
			h.BusyReportedAt = now
			return Decision{Action: ActReport, Why: "writing log lines but no progress"}, h
		}
		return Decision{}, h
	case StatusStalled:
	default:
		return Decision{}, h
	}

	if h.StallFrom.IsZero() {
		h.StallFrom = now
	}
	if h.Step == StepGaveUp {
		if now.Sub(h.GaveUpAt) < cfg.GiveUpHold {
			return Decision{}, h
		}
		// The hold is over: a new ladder may start, if the per-account limit allows.
		h.Step, h.StepAt = StepNone, time.Time{}
	}
	if h.Step != StepNone && now.Sub(h.StepAt) < cfg.RecheckAfter {
		return Decision{}, h
	}

	firstSeen := !h.Detected
	h.Detected = true
	next := nextStep(h.Step, o, cfg)
	limited := false
	if h.Step == StepNone && next != ActGiveUp && len(h.Ladders) >= cfg.MaxLadders {
		next, limited = ActGiveUp, true
	}
	report := func(held string) (Decision, History) {
		if !firstSeen {
			return Decision{Held: held}, h
		}
		return Decision{Action: ActReport, Why: "stalled", Held: held, FirstSeen: true}, h
	}
	if cfg.Mode == ModeObserve {
		return report("observe mode")
	}
	if cfg.Mode == ModeNudge && (next == ActRestart || next == ActRelogin) {
		next = ActGiveUp
	}
	if next != ActGiveUp {
		// Giving up is a report, never an intervention, so neither limit applies to it.
		if BreakerOpen(f, cfg) {
			return report("circuit breaker: too many accounts stalled at once")
		}
		if ScriptBreakerOpen(f, o.Script, cfg) {
			return report("circuit breaker: too many " + o.Script + " accounts stalled at once")
		}
		if f.ActionsInWindow >= cfg.FleetActions {
			return report("fleet-wide action limit")
		}
		if next == ActRelogin && f.ReloginsInFlight >= cfg.MaxRelogins {
			return report("relogins already in flight")
		}
	}

	if h.Step == StepNone && next != ActGiveUp {
		h.Ladders = append(h.Ladders, now)
		h.SkippedRestart = false
	}
	h.StepAt = now
	switch next {
	case ActNudge:
		h.Step = StepNudged
		return Decision{Action: ActNudge, Why: "stalled", FirstSeen: firstSeen}, h
	case ActRestart:
		h.Step = StepRestarted
		return Decision{Action: ActRestart, Why: "stalled", FirstSeen: firstSeen}, h
	case ActRelogin:
		why := "stalled"
		if h.Step != StepRestarted {
			why = "stalled and blind: the backpack/worn panels are unreadable outside any event, which a bot restart cannot fix"
			h.SkippedRestart = true
		}
		h.Step = StepRelogged
		return Decision{Action: ActRelogin, Why: why, FirstSeen: firstSeen}, h
	default:
		h.Step, h.GaveUpAt = StepGaveUp, now
		why := "nudge, restart and relogin did not bring it back"
		if h.SkippedRestart {
			why = "the relogin did not bring it back (the restart was skipped: it was blind)"
		}
		switch {
		case limited:
			why = "it stalled again after too many ladders in the window"
		case cfg.Mode == ModeNudge:
			why = "nudge mode: the ladder stops at the nudge"
		}
		return Decision{Action: ActGiveUp, Why: why, FirstSeen: firstSeen}, h
	}
}

// nextStep is the rung above step, skipping a nudge that would have nothing to press and, for a
// blind account, the restart that cannot give it its panels back (see Obs.Blind).
func nextStep(step Step, o Obs, cfg Config) Action {
	switch step {
	case StepNone:
		if o.ModalOpen || o.CanContinue {
			return ActNudge
		}
		if o.Blind {
			return ActRelogin
		}
		return ActRestart
	case StepNudged:
		if o.Blind {
			return ActRelogin
		}
		return ActRestart
	case StepRestarted:
		return ActRelogin
	}
	return ActGiveUp
}

func pruneLadders(l []time.Time, now time.Time, window time.Duration) []time.Time {
	out := l[:0]
	for _, t := range l {
		if now.Sub(t) < window {
			out = append(out, t)
		}
	}
	return out
}
