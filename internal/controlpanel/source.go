package controlpanel

import (
	"runtime"

	"rsb0t-go/internal/fleet"
)

// State is where an account is in its connection life.
type State string

// The states an account reports. Counts in the status body are keyed by these.
const (
	StateOffline      State = "offline"
	StateQueued       State = "queued"
	StateConnecting   State = "connecting"
	StateIngame       State = "ingame"
	StateReconnecting State = "reconnecting"
	StateStopped      State = "stopped"
	StateError        State = "error"
)

var States = []State{StateIngame, StateConnecting, StateQueued, StateReconnecting, StateOffline, StateStopped, StateError}

type Level string

const (
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

var Levels = []Level{LevelDebug, LevelInfo, LevelWarn, LevelError}

const KindLog = "log"
const KindChat = "chat"

type Position struct {
	X     int `json:"x"`
	Z     int `json:"z"`
	Level int `json:"level"`
}

type Counter struct {
	Name  string `json:"name"`
	Value int64  `json:"value"`
}

type AccountStatus struct {
	Name                 string                  `json:"name"`
	State                State                   `json:"state"`
	SinceUnixMs          int64                   `json:"since"`
	Bot                  string                  `json:"bot"`
	Activity             string                  `json:"activity"`
	Pos                  *Position               `json:"pos"`
	Packets              int64                   `json:"packets"`
	StaffLevel           int                     `json:"staffLevel"`
	Reconnects           int                     `json:"reconnects"`
	NextRetryUnixMs      int64                   `json:"nextRetry"`
	LastError            string                  `json:"lastError"`
	Counters             []Counter               `json:"counters"`
	BotState             string                  `json:"botState,omitempty"`
	BotError             string                  `json:"botError,omitempty"`
	PrimarySkill         string                  `json:"primarySkill,omitempty"`
	Skills               map[string]SkillCounter `json:"skills,omitempty"`
	ManuallyDisconnected bool                    `json:"manuallyDisconnected,omitempty"`
	Settings             map[string]interface{}  `json:"settings,omitempty"`
	HPKnown              bool                    `json:"hpKnown,omitempty"`
	HP                   int                     `json:"hp,omitempty"`
	MaxHP                int                     `json:"maxHp,omitempty"`
	// EnergyKnown/Energy are run energy on the game's own 0-100 scale. EnergyKnown is false
	// until an UPDATE_RUNENERGY has been seen, so "0%, running has just been switched off"
	// is distinguishable from "not told yet" — see fleet.AccountStatus.EnergyKnown.
	EnergyKnown  bool                 `json:"energyKnown,omitempty"`
	Energy       int                  `json:"energy,omitempty"`
	Dead         bool                 `json:"dead,omitempty"`
	RandomEvents []RandomEventOutcome `json:"randomEvents,omitempty"`
	// Route is the proxy route id the account logs in through; empty with no proxy pool.
	Route string `json:"route,omitempty"`
	// World is the game world this account is logged in to, and HomeWorld the one its
	// roster entry gave it. Always a real world number, never 0 — an account that named no
	// world reports the fleet's own — so a panel can group or colour by world without
	// special-casing the unset case.
	//
	// THE TRAP ANY MAP OR HEATMAP MUST HANDLE. The worlds share ONE coordinate space: two
	// accounts on different worlds standing on the same tile are different players who
	// cannot see, trade with, or contend for resources with each other. So a combined view
	// (which is what users asked for: "keep both worlds combined in viewing") must
	// still distinguish them, or it will show a crowd where there is none. Anything that
	// reasons about proximity ACROSS accounts — not just drawing — has to key on
	// (world, tile) rather than tile alone.
	World     int `json:"world,omitempty"`
	HomeWorld int `json:"homeWorld,omitempty"`
	// HopLog is the most recent world-hop or full-world-failover line for this account,
	// empty for one that has never moved.
	HopLog string `json:"hopLog,omitempty"`
	// AssignedScript is set when a saved script assignment (assignments.go) decides what this
	// account starts on after a process restart, instead of its roster entry; RosterScript is
	// then the roster's own script ("" when the roster starts none). Both are absent for an
	// account that runs from the roster. Bot is still what is running right now.
	AssignedScript string `json:"assignedScript,omitempty"`
	RosterScript   string `json:"rosterScript,omitempty"`
	// Watchdog is the fleet stall watchdog's view of this account (internal/fleet/watchdog.go):
	// omitted while it has nothing to say, "stalled" while a stall is being worked, and
	// "needs_attention" once its ladder has given up -- which also fills LastError when nothing
	// else has, so the existing error column shows it without a panel change.
	Watchdog *fleet.WatchdogStatus `json:"watchdog,omitempty"`
}

type RandomEventOutcome struct {
	Name    string `json:"name"`
	Outcome string `json:"outcome"`
	At      string `json:"at"`
}

type SkillCounter struct {
	Level    int   `json:"level"`
	XP       int64 `json:"xp"`
	XPGained int64 `json:"xpGained"`
}

type LoginStatus struct {
	Queue             []string `json:"queue"`
	InFlight          int      `json:"inFlight"`
	Sessions          int      `json:"sessions"`
	MaxSessions       int      `json:"maxSessions"`
	SpacingMs         int64    `json:"spacingMs"`
	NextAttemptUnixMs int64    `json:"nextAttempt"`
	HeldUntilUnixMs   int64    `json:"heldUntil"`
	// Routes are the proxy pool's per-route counters (fleet/routes.go): id, kind, host label
	// and numbers, never an address beyond the host or a credential. Absent with no pool.
	Routes []RouteStatus `json:"routes,omitempty"`
	// Worlds is one entry per game world the fleet has accounts on, ascending: how many
	// accounts it holds and whether the world last answered "full" (login response 7).
	// Absent on a single-world fleet, so a panel that has never heard of worlds is not
	// handed an empty table to render.
	Worlds []WorldStatus `json:"worlds,omitempty"`
}

// WorldStatus is one game world's line for the panel.
type WorldStatus struct {
	World int `json:"world"`
	// Accounts is how many of this fleet's accounts are assigned to this world.
	Accounts int `json:"accounts"`
	// Full is true when this world recently refused a login with response 7 ("This world
	// is full"). It is the fleet's own shared memory, not a query — there is no way to ask
	// a world its population, only to be told when it has no room.
	Full bool `json:"full,omitempty"`
	// FullSinceUnixMs is when it last said so, 0 when it has not.
	FullSinceUnixMs int64 `json:"fullSince,omitempty"`
}

// RouteStatus is fleet.RouteStatus (fleet/routes.go): one proxy route's counters, carrying the
// route id and host label only, never a credential or a full address.
type RouteStatus = fleet.RouteStatus

// MemStats is the process-level runtime data exposed by the control-panel memory endpoint.
// HeapObjects is the live object count reported by runtime.MemStats; unlike HeapAlloc it helps
// distinguish many-small-allocation pressure from fewer-large allocations.
//
// RSSBytes/CPUPercent are a DIFFERENT, OS-level view from everything else in this struct —
// live-found 2026-09-16: the account owner saw Task Manager report ~1GB for this process
// while this dashboard's own numbers (Go-heap-only) looked nothing like that, and reasonably
// asked why the dashboard didn't just show the real, comparable figure. The ~1GB turned out
// to be Task Manager's PEAK working set from a transient login-storm spike, not the current
// cost — but the deeper gap was real: nothing here reported CURRENT OS-level memory or any
// CPU% at all. RSSBytes is this process's current resident memory (Task Manager's default
// "Memory" column, current not peak — see procstats_windows.go); CPUPercent is CPU used
// since the previous sample, as a percentage of TOTAL machine capacity (100% = every core
// saturated, matching Task Manager's own convention). Both are 0 on a platform
// procstats_other.go's stub covers (i.e. anywhere but Windows) or if the underlying syscall
// fails — treat 0 as "unavailable," not "actually zero," the same as this struct already
// does implicitly for a fresh process with no GC pauses yet.
type MemStats struct {
	HeapAlloc    uint64  `json:"heapAlloc"`
	HeapInuse    uint64  `json:"heapInuse"`
	HeapSys      uint64  `json:"heapSys"`
	HeapObjects  uint64  `json:"heapObjects"`
	StackInuse   uint64  `json:"stackInuse"`
	Sys          uint64  `json:"sys"`
	NumGC        uint32  `json:"numGC"`
	PauseTotalNs uint64  `json:"pauseTotalNs"`
	LastPauseNs  uint64  `json:"lastPauseNs"`
	Goroutines   int     `json:"goroutines"`
	RSSBytes     uint64  `json:"rssBytes"`
	CPUPercent   float64 `json:"cpuPercent"`
}

func RuntimeMemStats() MemStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	rss, cpuPct := realProcessStats()
	return MemStats{
		HeapAlloc:    m.HeapAlloc,
		HeapInuse:    m.HeapInuse,
		HeapSys:      m.HeapSys,
		HeapObjects:  m.HeapObjects,
		StackInuse:   m.StackInuse,
		Sys:          m.Sys,
		NumGC:        m.NumGC,
		PauseTotalNs: m.PauseTotalNs,
		LastPauseNs:  m.PauseNs[(m.NumGC+255)%256],
		Goroutines:   runtime.NumGoroutine(),
		RSSBytes:     rss,
		CPUPercent:   cpuPct,
	}
}

type Event struct {
	Seq        uint64 `json:"seq"`
	TimeUnixMs int64  `json:"t"`
	Account    string `json:"account"`
	Level      Level  `json:"level"`
	Kind       string `json:"kind"`
	Text       string `json:"text"`
}

// Source is the read-only view of the fleet the control panel needs.
type Source interface {
	Accounts(dst []AccountStatus) []AccountStatus
	Logins() LoginStatus
	MemStats() MemStats
	Subscribe(ch chan<- Event) (cancel func())
}

// InventorySource is optional. A Source that implements it lets the account detail panel show
// what an account is carrying; it is read on request, one account at a time, never on the tick.
type InventorySource interface {
	Inventories(account string) (inventories []Inventory, ok bool)
}

type Inventory struct {
	ID    int    `json:"id"`
	Slots []Slot `json:"slots"`
}

type BankSource interface {
	Bank(account string) (items []Slot, updatedAtUnixMs int64, ok bool)
}

// BankAllSource is optional, and exists for one measured reason: answering the combined-bank
// panel by calling BankSource.Bank once per account is quadratic in the roster. At the live
// 266-account roster that cost 267 whole-fleet snapshots per refresh, every five seconds,
// and starved /api/status of the same locks (see snapshotcache.go). A Source that can
// answer every account's bank from one pass should implement this; handleBankAll falls back
// to the per-account loop for one that cannot.
type BankAllSource interface {
	AllBanks() []AccountBank
}

// AccountBank is one account's bank as BankAllSource reports it.
type AccountBank struct {
	Account         string
	Items           []Slot
	UpdatedAtUnixMs int64
	// Levels is the account's current skill levels by display name, carried HERE rather than
	// read back through Accounts() for the same reason AllBanks exists at all: the
	// banked-experience figures need levels to gate a route, and asking the Source for the
	// roster again would be a SECOND whole-fleet snapshot on every five-second refresh —
	// precisely the starvation cfbdf17 removed (snapshotcost_test.go asserts the call count,
	// and caught this while it was being written). Nil when the Source has never seen a stat
	// update for the account, which the panel is told about rather than reading as level 1.
	Levels map[string]int
}

type BotControl interface {
	BotScripts() []string
	StartBot(account, scriptName string, settings map[string]interface{}) error
	StopBot(account string) error
}

type HandoffControl interface {
	RequestHandoff(account string) error
	SetMuleAt(account string, n int) error
}

// WatchdogControl is the fleet stall watchdog's runtime switch (internal/fleet/watchdog.go):
// GET /api/watchdog reads its mode and counts, POST /api/watchdog {"mode": "..."} changes the
// mode without a restart. Optional; a Source without it answers 501.
type WatchdogControl interface {
	WatchdogInfo() fleet.WatchdogInfo
	SetWatchdogMode(mode string) (previous string, err error)
}

type ConnectionControl interface {
	Disconnect(account string) error
	ConnectExisting(account string) error
	AddAccount(account, password, script string, settings map[string]interface{}) error
}

type Slot struct {
	Slot  int    `json:"slot"`
	ID    int32  `json:"id"`
	Name  string `json:"name,omitempty"`
	Count int32  `json:"count"`
}
