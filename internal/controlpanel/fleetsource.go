package controlpanel

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/bot"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/fleet"
	"rsb0t-go/internal/gamestate"
)

// FleetSource is the Source for internal/fleet. It reads Manager.Snapshot and nothing else, so the
// fleet package needs no change to be watched.
//
// A snapshot says what each session is, not what happened to it, so events are derived by
// comparing each snapshot with the one before: every tick while a browser is open, and on a slow
// poll while the control panel is subscribed, so a login that failed overnight is in the feed when
// the page is opened in the morning.
type FleetSource struct {
	snapshot func() []fleet.AccountStatus
	roster   []string
	spacing  time.Duration
	// poll is how often snapshots are compared with no browser open; 0 turns the poll off.
	poll    time.Duration
	now     func() time.Time
	created int64
	// rosterSettings, when set, answers "what settings is this account configured with?" for
	// a StartBot call that supplied none. See StartBot and SetRosterSettings.
	rosterSettings func(account string) map[string]interface{}
	// assignments and rosterEntry back saved script assignments (assignments.go): a panel
	// start is recorded so the next process start replays it. Both nil (tests, cmd/fleet)
	// leaves StartBot exactly as it was before assignments existed.
	assignments *ScriptAssignments
	rosterEntry func(account string) (RosterEntry, bool)
	assignLogf  func(format string, args ...interface{})

	// manager backs BotControl (StartBot/StopBot/BotScripts). Nil for a FleetSource built
	// with newFleetSource (tests, a bare snapshot func) — BotControl's methods then report a
	// clear error instead of the panel's 501, since the type still satisfies the interface.
	manager *fleet.Manager

	// snapTTL/snapMu/snap/snapAt back cachedSnapshot — see snapshotcache.go for the
	// measured amplification this exists to remove. snapTTL 0 means the default window.
	// Its own mutex rather than f.mu below: Accounts holds f.mu across all of its
	// per-account bookkeeping, and a snapshot must not be serialized behind that.
	snapTTL time.Duration
	snapMu  sync.Mutex
	snap    []fleet.AccountStatus
	snapAt  time.Time

	// seen backs SeenPlayers (players.go): its own coalescing window over one
	// gamestate read per connected session, never a status snapshot.
	seen seenPlayersCache

	mu        sync.Mutex
	known     map[string]*tracked
	lastStart time.Time
	subs      map[int]chan<- Event
	nextSub   int
	stopPoll  chan struct{}
	// xpBaseline is the first-observed XP per account+skill this process has seen, so
	// Accounts can report "<skill> xp gained" — how much since this panel started
	// watching, not just the raw absolute total — without needing the browser open the
	// whole time the way cmd/fleet's own dashboard.html tracks it client-side. Set once
	// per account+skill on first sight, never updated after, so "gained" only ever grows.
	xpBaseline map[string]map[string]int
}

type tracked struct {
	state State
	since int64
	// chatSeen is the fleet.AccountStatus.ChatCount last turned into chat.message
	// events (see observe below) — lets observe emit only genuinely new lines instead
	// of replaying the whole RecentChat snapshot every poll. Set to the account's
	// current ChatCount the first time it's seen (no backfill of chat that predates
	// this control panel watching), the same "baseline on first sight" discipline
	// xpBaseline uses in Accounts below.
	chatSeen int
}

// NewFleetSource watches m. roster is every account the caller will connect, in order, so the ones
// not started yet show as queued; spacing is the pause the caller leaves between logins.
func NewFleetSource(m *fleet.Manager, roster []string, spacing time.Duration) *FleetSource {
	f := newFleetSource(m.Snapshot, roster, spacing)
	f.manager = m
	f.seen.read = m.SeenPlayers
	return f
}

func newFleetSource(snapshot func() []fleet.AccountStatus, roster []string, spacing time.Duration) *FleetSource {
	return &FleetSource{
		snapshot: snapshot,
		roster:   append([]string(nil), roster...),
		spacing:  spacing,
		poll:     5 * time.Second,
		now:      time.Now,
		created:  time.Now().UnixMilli(),
		known:    make(map[string]*tracked),
		subs:     make(map[int]chan<- Event),
	}
}

// LoginStarted records that the caller has just asked the fleet to log the next account in, which
// is what the next login in the queue is due after.
func (f *FleetSource) LoginStarted() {
	f.mu.Lock()
	f.lastStart = f.now()
	f.mu.Unlock()
}

// fleetState reads a state, and the words for having entered it, from one fleet account.
//
// Why a case for Reconnecting comes first: internal/fleet's Manager.reconnectLoop retries a
// dropped session with capped backoff (see fleet.go) rather than leaving it disconnected, so a
// session that is between attempts is genuinely "reconnecting", not "offline" — this used to be
// stale (an earlier note here said the fleet never reconnects; it does now).
//
// Why ManuallyDisconnected gets its own case ahead of the generic Disconnected ones: an
// account the operator deliberately logged out via ConnectionControl.Disconnect is
// "stopped" (StateStopped, "deliberately not running" per this package's own State doc),
// not "offline" the way an unexpected drop is — the panel offers a reconnect button either
// way, but the distinct state and message make it obvious at a glance which one this is,
// instead of showing whatever raw transport error closing the socket happened to produce.
func fleetState(a fleet.AccountStatus) (State, Level, string) {
	switch {
	case a.RouteWait:
		// Every proxy route (fleet/routes.go) is full or unhealthy: the account is queued and
		// sends nothing to the server until a slot frees.
		return StateQueued, LevelInfo, "waiting for a free route slot"
	case a.Reconnecting:
		return StateReconnecting, LevelWarn, fmt.Sprintf("reconnecting (attempt %d)", a.ReconnectAttempt)
	case a.Connected && !a.Disconnected:
		return StateIngame, LevelInfo, "logged in"
	case a.Connecting:
		return StateConnecting, LevelInfo, "logging in"
	case a.ManuallyDisconnected:
		return StateStopped, LevelInfo, "disconnected (requested)"
	case a.Error != "":
		return StateError, LevelError, "login failed: " + a.Error
	case a.Disconnected && a.DisconnectMsg != "":
		return StateOffline, LevelWarn, "disconnected: " + a.DisconnectMsg
	case a.Disconnected:
		return StateOffline, LevelWarn, "disconnected"
	}
	return StateConnecting, LevelInfo, "logging in"
}

// observe brings the tracked states up to date and returns the events the changes amount to.
// The caller holds f.mu.
func (f *FleetSource) observe(snap []fleet.AccountStatus, now time.Time) []Event {
	ms := now.UnixMilli()
	var out []Event
	for i := range snap {
		a := &snap[i]
		state, level, text := fleetState(*a)
		t := f.known[a.Username]
		firstSight := t == nil
		if firstSight {
			t = &tracked{state: StateQueued, since: ms, chatSeen: a.ChatCount}
			f.known[a.Username] = t
		}
		if t.state != state {
			t.state = state
			t.since = ms
			out = append(out, Event{TimeUnixMs: ms, Account: a.Username, Level: level, Kind: "state", Text: text})
		}
		// Chat: turn genuinely new lines (see fleet.AccountStatus.ChatCount's own doc
		// comment) into individual chat.message events, the same diff-against-last-
		// poll approach the state case above uses. Skipped on firstSight since
		// chatSeen was just baselined to the account's current count above — an
		// account already mid-session when this control panel starts watching should
		// not dump its whole chat history into the stream at once.
		if !firstSight && a.ChatCount > t.chatSeen {
			delta := a.ChatCount - t.chatSeen
			lines := a.RecentChat
			if delta < len(lines) {
				lines = lines[len(lines)-delta:]
			}
			for _, line := range lines {
				out = append(out, Event{TimeUnixMs: ms, Account: a.Username, Level: LevelInfo, Kind: KindChat, Text: line})
			}
			t.chatSeen = a.ChatCount
		}
	}
	return out
}

func (f *FleetSource) emit(events []Event) {
	if len(events) == 0 {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, ev := range events {
		for _, ch := range f.subs {
			select {
			case ch <- ev:
			default:
			}
		}
	}
}

// Accounts implements Source.
func (f *FleetSource) Accounts(dst []AccountStatus) []AccountStatus {
	snap := f.cachedSnapshot()
	now := f.now()
	f.mu.Lock()
	events := f.observe(snap, now)
	present := make(map[string]bool, len(snap))
	for i := range snap {
		a := &snap[i]
		present[a.Username] = true
		t := f.known[a.Username]
		st := AccountStatus{Name: a.Username, State: t.state, SinceUnixMs: t.since, Packets: int64(a.Packets), StaffLevel: a.StaffModLevel, Bot: a.Script, BotState: a.BotState, BotError: a.BotError, Reconnects: a.ReconnectAttempt, ManuallyDisconnected: a.ManuallyDisconnected, Settings: a.Settings, HPKnown: a.HPKnown, HP: a.HP, MaxHP: a.MaxHP, EnergyKnown: a.EnergyKnown, Energy: a.Energy, Dead: a.Dead, Route: a.Route, World: a.World, HomeWorld: a.HomeWorld, HopLog: a.HopLog}
		// A saved script assignment (assignments.go) is named beside the roster's script, so the
		// panel and fleet-switch.py can tell an override from the roster at a glance.
		if script, ok := f.assignments.scriptOf(a.Username); ok {
			st.AssignedScript, st.RosterScript = script, f.rosterScript(a.Username)
		}
		if t.state == StateIngame && a.ConnectedSecs > 0 {
			st.SinceUnixMs = now.Add(-time.Duration(a.ConnectedSecs * float64(time.Second))).UnixMilli()
		}
		if a.Error != "" {
			st.LastError = a.Error
		} else if a.DisconnectMsg != "" {
			st.LastError = a.DisconnectMsg
		} else if a.BotError != "" {
			st.LastError = a.BotError
		}
		st.Watchdog = a.Watchdog
		if st.LastError == "" && a.Watchdog != nil && a.Watchdog.State == "needs_attention" {
			st.LastError = "watchdog: stalled — needs attention (" + a.Watchdog.Detail + ")"
		}
		// Pos (fleet.AccountStatus, added alongside this change) is the account's last
		// known tile from gamestate.State.Tile — copied into controlpanel's own
		// Position type rather than reused directly so this package doesn't need to
		// import fleet.TilePos by name everywhere Position already appears (account.js
		// and the map panel already expect this exact {x,z,level} shape from
		// FakeSource). nil until the session has ever had a tile fix.
		if a.Pos != nil {
			st.Pos = &Position{X: a.Pos.X, Z: a.Pos.Z, Level: a.Pos.Level}
		}
		if a.SelfSlot >= 0 {
			st.Counters = append(st.Counters, Counter{Name: "player slot", Value: int64(a.SelfSlot)})
		}
		if a.MainModal != gamestate.NoModal {
			st.Counters = append(st.Counters, Counter{Name: "main modal", Value: int64(a.MainModal)})
		}
		if a.SideModal != gamestate.NoModal {
			st.Counters = append(st.Counters, Counter{Name: "side modal", Value: int64(a.SideModal)})
		}
		if len(a.Inventories) > 0 {
			st.Counters = append(st.Counters, Counter{Name: "inventories", Value: int64(len(a.Inventories))})
		}
		// PrimarySkill is set from the script name alone, independent of whether that
		// skill has reported any XP yet, so the panel can show "Agility — not yet
		// observed" for a bot that just started rather than nothing at all.
		if a.Script != "" {
			st.PrimarySkill, _ = bot.PrimarySkill(a.Script)
		}
		// Skills (internal/fleet.AccountStatus, added by the dashboardvis work) surfaces
		// here ONLY through the structured Skills map, deliberately NOT flattened into
		// Counters the way an earlier version of this code did. Counters is clamped to
		// MaxCounters (8) — with up to 21 skills at up to 3 entries each, flattening
		// them there meant only a handful ever survived the clamp, and because Go map
		// iteration order is random, a DIFFERENT handful survived on every single poll:
		// the exact "counters rotate for no reason" bug the account owner reported.
		// Skills has no such cap, and is iterated in sorted-name order below purely so
		// xpBaseline's own bookkeeping is deterministic, not because anything downstream
		// still depends on Counters for this data.
		if len(a.Skills) > 0 {
			names := make([]string, 0, len(a.Skills))
			for name := range a.Skills {
				names = append(names, name)
			}
			sort.Strings(names)
			st.Skills = make(map[string]SkillCounter, len(names))
			for _, name := range names {
				sk := a.Skills[name]
				if f.xpBaseline == nil {
					f.xpBaseline = make(map[string]map[string]int)
				}
				acctBaseline, ok := f.xpBaseline[a.Username]
				if !ok {
					acctBaseline = make(map[string]int)
					f.xpBaseline[a.Username] = acctBaseline
				}
				base, seen := acctBaseline[name]
				if !seen {
					acctBaseline[name] = sk.XP
					base = sk.XP
				}
				gained := sk.XP - base
				st.Skills[name] = SkillCounter{Level: sk.Level, XP: int64(sk.XP), XPGained: int64(gained)}
			}
		}
		if len(a.RandomEvents) > 0 {
			// "other" used to be everything that was not "cleared", which is the same
			// mistake stats.go's own RandomsFailed doc comment records and fixes: it sweeps
			// in "not_ours" (the guardian correctly declining somebody else's event, the
			// commonest outcome on a crowded fleet) and "retry" (an attempt, not a verdict).
			// Only "error" and "gave_up" are terminal failures. An unrecognised outcome is
			// counted nowhere rather than defaulting into the failure bucket — that default
			// is what made the number meaningless.
			cleared, other := 0, 0
			for _, ev := range a.RandomEvents {
				switch ev.Outcome {
				case "cleared":
					cleared++
				case "error", "gave_up":
					other++
				}
			}
			st.Counters = append(st.Counters, Counter{Name: "random events cleared", Value: int64(cleared)})
			if other > 0 {
				st.Counters = append(st.Counters, Counter{Name: "random events not cleared", Value: int64(other)})
			}
			// Structured copy alongside the lifetime Counters summary above — see
			// AccountStatus.RandomEvents's own doc comment for why the alerts digest
			// panel needs the actual newest-first list (specifically the most recent
			// entry's Outcome), not just a cleared/not-cleared count.
			st.RandomEvents = make([]RandomEventOutcome, len(a.RandomEvents))
			for i, ev := range a.RandomEvents {
				st.RandomEvents[i] = RandomEventOutcome{Name: ev.Name, Outcome: ev.Outcome, At: ev.At}
			}
		}
		st.Activity = activityOf(a.RecentLog)
		dst = append(dst, st)
	}
	for _, name := range f.roster {
		if !present[name] {
			dst = append(dst, AccountStatus{Name: name, State: StateQueued, SinceUnixMs: f.created})
		}
	}
	f.mu.Unlock()
	f.emit(events)
	return dst
}

// Logins implements Source. The fleet has no session cap and no queue hold of its own.
func (f *FleetSource) Logins() LoginStatus {
	snap := f.cachedSnapshot()
	f.mu.Lock()
	defer f.mu.Unlock()
	l := LoginStatus{Queue: []string{}, SpacingMs: f.spacing.Milliseconds()}
	present := make(map[string]bool, len(snap))
	for i := range snap {
		present[snap[i].Username] = true
		switch state, _, _ := fleetState(snap[i]); state {
		case StateIngame:
			l.Sessions++
		case StateConnecting:
			l.InFlight++
		}
	}
	for _, name := range f.roster {
		if !present[name] {
			l.Queue = append(l.Queue, name)
		}
	}
	if len(l.Queue) > 0 && !f.lastStart.IsZero() {
		l.NextAttemptUnixMs = f.lastStart.Add(f.spacing).UnixMilli()
	}
	if f.manager != nil {
		l.Routes = f.manager.Routes()
		l.Worlds = f.worldStatuses()
	}
	return l
}

// worldStatuses is one line per world the fleet has accounts on: the headcount, and whether
// that world last refused a login because it was full.
//
// Empty for a single-world fleet, deliberately: a panel should show a worlds table only when
// there is something to say. The "full" flag is the fleet's own shared memory of login
// response 7 — there is no way to ASK a world how many players it holds, only to be told it
// has no room for one more, so this is the best answer that exists.
func (f *FleetSource) worldStatuses() []WorldStatus {
	counts := f.manager.WorldCounts()
	if len(counts) == 0 {
		return nil
	}
	full := f.manager.FullWorlds()
	if len(counts) == 1 && len(full) == 0 {
		return nil
	}
	nums := make([]int, 0, len(counts))
	for w := range counts {
		nums = append(nums, w)
	}
	// Include a world that is known-full even with no accounts on it, so an operator can see
	// why nothing is moving there.
	for w := range full {
		if _, ok := counts[w]; !ok {
			nums = append(nums, w)
		}
	}
	sort.Ints(nums)
	out := make([]WorldStatus, 0, len(nums))
	for _, w := range nums {
		ws := WorldStatus{World: w, Accounts: counts[w]}
		if at, ok := full[w]; ok {
			ws.Full, ws.FullSinceUnixMs = true, at.UnixMilli()
		}
		out = append(out, ws)
	}
	return out
}

// MemStats implements Source: the fleet is this process.
func (f *FleetSource) MemStats() MemStats { return RuntimeMemStats() }

// Inventories implements InventorySource from the snapshot's known inventory components.
func (f *FleetSource) Inventories(account string) ([]Inventory, bool) {
	for _, a := range f.cachedSnapshot() {
		if a.Username != account {
			continue
		}
		ids := make([]int, 0, len(a.Inventories))
		for id := range a.Inventories {
			ids = append(ids, id)
		}
		sort.Ints(ids)
		var cache *cachecfg.Config
		if f.manager != nil {
			cache = f.manager.Cache()
		}
		out := make([]Inventory, 0, len(ids))
		for _, id := range ids {
			items := a.Inventories[id]
			slots := make([]Slot, len(items))
			for i, it := range items {
				slot := Slot{Slot: it.Slot, ID: it.ID, Count: it.Count}
				if cache != nil {
					if obj, ok := cache.Obj(int(it.ID)); ok {
						slot.Name = obj.Name
					}
				}
				slots[i] = slot
			}
			out = append(out, Inventory{ID: id, Slots: slots})
		}
		return out, true
	}
	for _, name := range f.roster {
		if name == account {
			return []Inventory{}, true
		}
	}
	return nil, false
}

// Bank implements BankSource from fleet.AccountStatus.BankItems/BankUpdatedAt — the
// same known-inventory snapshot Inventories reads above, just picked out for the bank
// component specifically (see fleet.AccountStatus.BankItems's own doc comment) and
// name-resolved the same way Inventories resolves names.
func (f *FleetSource) Bank(account string) ([]Slot, int64, bool) {
	snap := f.cachedSnapshot()
	cache := f.objCache()
	for i := range snap {
		if snap[i].Username != account {
			continue
		}
		slots, updated := bankFrom(&snap[i], cache)
		return slots, updated, true
	}
	for _, name := range f.roster {
		if name == account {
			return []Slot{}, 0, true
		}
	}
	return nil, 0, false
}

// AllBanks implements BankAllSource: every account's bank from ONE fleet snapshot.
//
// This exists because the obvious implementation of the combined-bank panel — ask for the
// account list, then call Bank(name) for each — is quadratic, and measurably so: at the
// live 266-account roster it took 267 whole-fleet snapshots per refresh, every five
// seconds, and starved /api/status of the very locks it needed (see snapshotcache.go and
// snapshotcost_test.go). The coalescing window alone does NOT fix that, because on a real
// fleet the per-account loop outruns the window; the algorithm had to change too.
func (f *FleetSource) AllBanks() []AccountBank {
	snap := f.cachedSnapshot()
	cache := f.objCache()
	out := make([]AccountBank, 0, len(snap))
	for i := range snap {
		slots, updated := bankFrom(&snap[i], cache)
		out = append(out, AccountBank{Account: snap[i].Username, Items: slots, UpdatedAtUnixMs: updated, Levels: levelsFrom(&snap[i])})
	}
	return out
}

// levelsFrom pulls one account's skill levels out of the SAME snapshot its bank came from, so
// the banked-experience level gate (see bankvalue.go) costs no extra pass over the fleet. Nil
// when the account has never reported a stat, which is a different thing from every level
// being 1 and is reported as such.
func levelsFrom(a *fleet.AccountStatus) map[string]int {
	if len(a.Skills) == 0 {
		return nil
	}
	out := make(map[string]int, len(a.Skills))
	for name, s := range a.Skills {
		out[name] = s.Level
	}
	return out
}

// objCache is the item-name cache, read once per request rather than once per account —
// Manager.Cache takes its own lock, and the combined-bank panel used to call it 266 times
// for a single refresh.
func (f *FleetSource) objCache() *cachecfg.Config {
	if f.manager == nil {
		return nil
	}
	return f.manager.Cache()
}

// bankFrom converts one account's snapshot bank into panel slots, resolving item names the
// same way Inventories does. Shared by Bank and AllBanks so the two can never drift.
func bankFrom(a *fleet.AccountStatus, cache *cachecfg.Config) ([]Slot, int64) {
	slots := make([]Slot, len(a.BankItems))
	for i, it := range a.BankItems {
		slot := Slot{Slot: it.Slot, ID: it.ID, Count: it.Count}
		if cache != nil {
			if obj, ok := cache.Obj(int(it.ID)); ok {
				slot.Name = obj.Name
			}
		}
		slots[i] = slot
	}
	var updated int64
	if a.BankUpdatedAt != "" {
		if t, err := time.Parse(time.RFC3339, a.BankUpdatedAt); err == nil {
			updated = t.UnixMilli()
		}
	}
	return slots, updated
}

// errNoManager is returned by BotScripts/StartBot/StopBot on a FleetSource that has no
// *fleet.Manager attached (built via the unexported newFleetSource, as tests do with a bare
// snapshot func) — the type still satisfies BotControl, so callers get this clear error
// instead of a type assertion silently failing.
var errNoManager = errors.New("controlpanel: this control panel has no fleet manager attached")

// BotScripts implements BotControl, listing every internal/bot script this build registered
// (see internal/bot.Names) — the same list cmd/fleet's dashboard offers as knownScripts.
func (f *FleetSource) BotScripts() []string {
	return bot.Names()
}

// StartBot implements BotControl by calling straight through to the same
// internal/fleet.Manager.StartBot that cmd/fleet's POST /api/start uses, so starting a script
// from this panel and from cmd/fleet's dashboard are the same operation on the same account.
func (f *FleetSource) StartBot(account, scriptName string, settings map[string]interface{}) error {
	if f.manager == nil {
		return errNoManager
	}
	defer f.invalidateSnapshot()

	// nil settings means the CALLER SUPPLIED NONE, which is different from an explicitly
	// empty object, and the two must not be treated alike. Without this fallback the bot
	// starts on the SCRIPT's own defaults -- and cheatSetup defaults to true in fisher.go,
	// barbarian.go and crafter.go, a flag those files' own doc comments label
	// LOCAL-ENGINE-ONLY. On production the bot then issues ::tele, the server refuses it,
	// and the script stops with "cheat setup: cheat commands disabled (production mode)".
	//
	// Demonstrated on a live account (spare30, 2026-09-21 07:55): a start with no settings
	// took a working production fisher to botState "error" doing nothing until it was
	// restarted with its roster settings. The panel's own Restart button sends
	// `a.settings || {}`, so any account whose settings happen to be empty walks into it
	// from one click.
	//
	// The roster is the right fallback, not the running bot's settings: fleet's own
	// AccountStatus.Settings comes from runner.Settings() and is therefore empty exactly
	// when it is most needed -- after a stop, when there is no runner left to ask.
	//
	// A SAVED ASSIGNMENT comes before the roster (see assignments.go): an account switched to
	// ScarabSwarm and stopped must restart on its saved Scarab settings, not the fisher's.
	return f.startWith(f.manager.StartBot, account, scriptName, settings)
}

// startWith is StartBot with the manager's start passed in, so a test can drive the real
// settings rule AND the assignment it records without a game connection.
func (f *FleetSource) startWith(start func(account, scriptName string, settings map[string]interface{}) error, account, scriptName string, supplied map[string]interface{}) error {
	resolved := f.resolveStartSettings(account, scriptName, supplied)
	if err := start(account, scriptName, resolved); err != nil {
		return err
	}
	f.noteAssignment(account, scriptName, supplied, resolved)
	return nil
}

// resolveStartSettings is that fallback, split out so a test can exercise the real rule
// rather than restate it: StartBot itself short-circuits on a nil manager, so a test driving
// it can never observe what settings would have been passed.
func (f *FleetSource) resolveStartSettings(account, scriptName string, supplied map[string]interface{}) map[string]interface{} {
	if saved, ok := f.assignments.StartSettings(account, scriptName, supplied, f.rosterScript(account)); ok {
		return saved
	}
	if supplied != nil || f.rosterSettings == nil {
		return supplied
	}
	return f.rosterSettings(account)
}

// SetScriptAssignments turns saved script assignments on (assignments.go). roster answers what
// the roster configures for an account, which decides whether a start is an override to save
// or a revert that removes one; logf reports each save and any failure (nil: log.Printf).
func (f *FleetSource) SetScriptAssignments(store *ScriptAssignments, roster func(account string) (RosterEntry, bool), logf func(format string, args ...interface{})) {
	f.assignments, f.rosterEntry, f.assignLogf = store, roster, logf
}

func (f *FleetSource) rosterScript(account string) string {
	if f.rosterEntry == nil {
		return ""
	}
	e, _ := f.rosterEntry(account)
	return e.Script
}

// noteAssignment records a successful start. A failed save does not undo the start -- the bot
// is running -- but it is logged and shown at /api/assignments, because it means the next
// restart will not replay this switch.
func (f *FleetSource) noteAssignment(account, scriptName string, supplied, resolved map[string]interface{}) {
	if !f.assignments.Enabled() {
		return
	}
	var e RosterEntry
	if f.rosterEntry != nil {
		e, _ = f.rosterEntry(account)
	}
	change, err := f.assignments.Note(account, scriptName, supplied, resolved, e.Script, e.Settings)
	logf := f.assignLogf
	if logf == nil {
		logf = log.Printf
	}
	switch {
	case err != nil:
		logf("controlpanel: [%s] %s is running, but saving its script assignment FAILED, so a restart will put it back on %q: %v", account, scriptName, e.Script, err)
	case change == AssignmentSaved:
		logf("controlpanel: [%s] script assignment saved: %s replaces the roster's %q at the next start (%s)", account, scriptName, e.Script, f.assignments.Path())
	case change == AssignmentRemoved:
		logf("controlpanel: [%s] script assignment removed: back on the roster's %s", account, scriptName)
	}
}

// ScriptAssignmentsView implements AssignmentControl.
func (f *FleetSource) ScriptAssignmentsView() AssignmentsView {
	return viewOf(f.assignments, f.rosterEntry)
}

// ClearScriptAssignment implements AssignmentControl. The running bot is not touched.
func (f *FleetSource) ClearScriptAssignment(account string) (bool, error) {
	if !f.assignments.Enabled() {
		return false, errAssignmentsOff
	}
	return f.assignments.Clear(account)
}

// SetRosterSettings gives StartBot somewhere to look when a caller supplies no settings at
// all: the account's own configured settings, as the roster file declares them. Optional --
// unset (tests, cmd/fleet) leaves the previous behaviour of passing nil straight through.
// cmd/controlpanel wires it from the same fleet.config.json entries it uses to auto-start
// each account after it connects, so "start this account's script" through the panel means
// the same thing as the process starting it itself.
func (f *FleetSource) SetRosterSettings(lookup func(account string) map[string]interface{}) {
	f.rosterSettings = lookup
}

// StopBot implements BotControl, delegating to internal/fleet.Manager.StopBot.
func (f *FleetSource) StopBot(account string) error {
	if f.manager == nil {
		return errNoManager
	}
	defer f.invalidateSnapshot()

	return f.manager.StopBot(account)
}

// RequestHandoff implements HandoffControl, delegating to
// internal/fleet.Manager.RequestHandoff.
func (f *FleetSource) RequestHandoff(account string) error {
	if f.manager == nil {
		return errNoManager
	}
	defer f.invalidateSnapshot()

	return f.manager.RequestHandoff(account)
}

// SetMuleAt implements HandoffControl, delegating to internal/fleet.Manager.SetMuleAt.
func (f *FleetSource) SetMuleAt(account string, n int) error {
	if f.manager == nil {
		return errNoManager
	}
	defer f.invalidateSnapshot()

	return f.manager.SetMuleAt(account, n)
}

// WatchdogInfo implements WatchdogControl, delegating to internal/fleet.Manager.WatchdogInfo.
func (f *FleetSource) WatchdogInfo() fleet.WatchdogInfo {
	if f.manager == nil {
		return fleet.WatchdogInfo{Mode: "off"}
	}
	return f.manager.WatchdogInfo()
}

// SetWatchdogMode implements WatchdogControl, delegating to internal/fleet.Manager.SetWatchdogMode.
func (f *FleetSource) SetWatchdogMode(mode string) (string, error) {
	if f.manager == nil {
		return "", errNoManager
	}
	defer f.invalidateSnapshot() // the per-account watchdog statuses change with the mode
	return f.manager.SetWatchdogMode(mode)
}

// Disconnect implements ConnectionControl, delegating to internal/fleet.Manager.Disconnect
// — a clean Session.Logout, not a bare Close, and marked so the fleet's own automatic
// reconnect does not immediately relog the account behind the operator's back.
func (f *FleetSource) Disconnect(account string) error {
	if f.manager == nil {
		return errNoManager
	}
	defer f.invalidateSnapshot()

	return f.manager.Disconnect(account)
}

// ConnectExisting implements ConnectionControl, delegating to internal/fleet.Manager.
// ConnectExisting.
func (f *FleetSource) ConnectExisting(account string) error {
	if f.manager == nil {
		return errNoManager
	}
	defer f.invalidateSnapshot()

	return f.manager.ConnectExisting(account)
}

// AddAccount implements ConnectionControl, delegating to internal/fleet.Manager.
// AddAccount. Not persisted to fleet.config.json — see that method's own doc comment.
func (f *FleetSource) AddAccount(account, password, script string, settings map[string]interface{}) error {
	if f.manager == nil {
		return errNoManager
	}
	defer f.invalidateSnapshot()

	return f.manager.AddAccount(account, password, script, settings)
}

// Subscribe implements Source. The first subscriber starts the slow poll and the last one stops it.
func (f *FleetSource) Subscribe(ch chan<- Event) func() {
	f.mu.Lock()
	id := f.nextSub
	f.nextSub++
	f.subs[id] = ch
	if f.stopPoll == nil && f.poll > 0 {
		stop := make(chan struct{})
		f.stopPoll = stop
		go f.watch(stop, f.poll)
	}
	f.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			f.mu.Lock()
			delete(f.subs, id)
			if len(f.subs) == 0 && f.stopPoll != nil {
				close(f.stopPoll)
				f.stopPoll = nil
			}
			f.mu.Unlock()
		})
	}
}

func (f *FleetSource) watch(stop <-chan struct{}, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			snap := f.cachedSnapshot()
			f.mu.Lock()
			events := f.observe(snap, f.now())
			f.mu.Unlock()
			f.emit(events)
		}
	}
}

// activityOf picks the Activity column's text from an account's recent log, newest first: the
// latest line that says what the account is doing, without the "[name/Script] " prefix (the
// panel already shows the account and its bot) and without the pathfinding diagnostics that
// dominated the column (owner 2026-09-19). A multi-line entry keeps its first line. When every
// recent line is a diagnostic, the newest one is shown rather than nothing.
func activityOf(recent []string) string {
	fallback := ""
	for i := len(recent) - 1; i >= 0; i-- {
		line := recent[i]
		if cut := strings.IndexByte(line, '\n'); cut >= 0 {
			line = line[:cut]
		}
		line = strings.TrimSpace(stripAccountPrefix(line))
		if line == "" {
			continue
		}
		if fallback == "" {
			fallback = line
		}
		if !isDiagnosticActivity(line) {
			return line
		}
	}
	return fallback
}

// stripAccountPrefix removes a leading "[name/Script]" or "[name]" tag.
func stripAccountPrefix(line string) string {
	if strings.HasPrefix(line, "[") {
		if end := strings.IndexByte(line, ']'); end > 0 && !strings.ContainsAny(line[1:end], " ") {
			return line[end+1:]
		}
	}
	return line
}

func isDiagnosticActivity(line string) bool {
	return strings.HasPrefix(line, "[diag]") ||
		(strings.HasPrefix(line, "route ") && strings.Contains(line, " -> ") && strings.Contains(line, "waypoints")) ||
		strings.HasPrefix(line, "id=")
}
