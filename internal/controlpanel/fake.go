package controlpanel

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"time"

	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/muleledger"
)

// FakeSource is a fleet that does not exist: a handful of accounts that queue, log in, work, drop
// and reconnect on their own. It is for developing the page and for tests. It never touches the
// network.
type FakeSource struct {
	mu  sync.Mutex
	rng *rand.Rand
	// fishRNG is a SEPARATE stream for the simulated fishing loot (fakeloot.go). It has to be
	// separate: every simulated behaviour here draws from one seeded rng, so adding draws to
	// any one of them shifts the whole sequence and silently changes the others. Rolling the
	// fishing spots off f.rng moved the fake fleet's phase sequence far enough that an hour of
	// it stopped ever reaching "handoff", failing TestFakeFleetSimulatesPhasesAndAMule. Give a
	// new simulated behaviour its own stream rather than re-baselining everyone else's tests.
	fishRNG     *rand.Rand
	now         func() time.Time
	accounts    []AccountStatus
	bots        []int // index into fakeBots per account
	queue       []int
	inFlight    int
	sessions    int
	nextAttempt time.Time
	held        time.Time
	subs        map[int]chan<- Event
	nextSub     int
	dropped     uint64

	// runeRNG is one more separate stream, for the same reason: the simulated runecrafting and essence supply
	// (fakeloot.go). essenceHanded is the essence the fake miners have handed the fake mule, which its bank shows.
	runeRNG       *rand.Rand
	essenceHanded int64

	// handoffRequested/muleAtOverride are purely in-memory bookkeeping so the panel's
	// hand-off button and muleAt field are exercisable under -fake with zero game
	// connection — see HandoffControl's own doc comment. Neither is read back by
	// anything else in this fake (no fake bot actually performs a hand-over); recording
	// them is enough for a developer to confirm the request reached this Source at all.
	handoffRequested map[string]bool
	muleAtOverride   map[string]int

	// bank/bankAt back Bank (BankSource) — a separate store from AccountStatus, mirroring
	// how a real fleet keeps bank contents out of the per-tick status body (see BankSource's
	// own doc comment) and only reports them on request.
	bank   map[string][]Slot
	bankAt map[string]int64

	// assignments backs saved script assignments under -fake (fakeassign.go), so the rules
	// and a real process restart can be shown with no game connection. Nil: off.
	assignments *ScriptAssignments
	fakeRoster  map[string]string

	// loot backs LootTotals (fakeloot.go): each account's simulated drops.
	loot map[string]*loot.Tally
	// ledger backs MuleTrades (fakeloot.go): the simulated mule's finished trades.
	ledger *muleledger.Ledger
	// phaseSim backs Phases and MuleStatuses (fakephases.go): SYNTHETIC script phases and one
	// simulated mule, for the time-split and mule panels. Zero value ready; its own lock.
	phaseSim fakePhaseSim
}

// fakeChatLines is what a fake account "overhears" while StateIngame — plausible
// classic-RS chat/system lines (public chat, trade requests, PK warnings, GM messages),
// standing in for the real MESSAGE_GAME capture a live fleet's chat feed streams. Not
// meant to be exhaustive, just varied enough to demo the chat panel's account tagging,
// scrolling and (for the alerts digest) its "look for trouble in the chat" heuristics.
var fakeChatLines = []string{
	"Would you like to trade with Some_Guy99?",
	"Nice bot",
	"lol get pked much",
	"You have been granted a trade request from Zezima_Alt.",
	"Wow! You have just advanced a Fishing level!",
	"You have been assigned a new task by the Slayer master.",
	"Welcome to RuneScape.",
	"any1 wanna duel",
	"You are now member of the group 'iron btw'.",
	"Please avoid discussing sensitive political and religious subjects.",
	"free stuff giveaway world 2 falador park",
	"You have just been promoted to Iron.",
}

// fakeSpacing is the pause the fake fleet leaves between logins, as cmd/fleet's loginDelaySeconds does.
const fakeSpacing = time.Second

// fakeBackpack is the component id the fake fleet reports its carried items under.
const fakeBackpack = 3214

type fakeBot struct {
	short      string
	name       string
	activities []string
	counters   []string
	tool       int32
	product    int32
	home       Position
	// skill is the display name (matching gamestate.SkillNames/internal/bot.PrimarySkill)
	// this fake bot pretends to train, empty for one that doesn't (mirroring MuleReceiver
	// having no entry in the real bot.PrimarySkill map). Only real overlap with the actual
	// registry is "fisher"/ProgressiveFisher and "agility"/BarbarianAgility; the rest are
	// simulated scripts with no real internal/bot counterpart, given a plausible skill
	// anyway so the fake fleet exercises the same UI path a real crafter/thiever/etc. would.
	skill string
}

var fakeBots = []fakeBot{
	{"fisher", "ProgressiveFisher", []string{"Fishing shrimp at Draynor", "Walking to Draynor bank", "Banking raw shrimp"}, []string{"fish", "trips"}, 303, 317, Position{3087, 3228, 0}, "Fishing"},
	{"miner", "EssenceMiner", []string{"Mining rune essence", "Walking to Aubury", "Banking essence"}, []string{"essence", "trips"}, 1265, 1436, Position{3253, 3401, 0}, "Mining"},
	{"thief", "ProgressiveThiever", []string{"Pickpocketing men", "Eating trout", "Walking to Lumbridge"}, []string{"steals", "stuns"}, 333, 995, Position{3222, 3218, 0}, "Thieving"},
	{"cutter", "ProgressiveWoodcutter", []string{"Chopping willows", "Dropping logs", "Walking to Seers"}, []string{"logs", "nests"}, 1351, 1519, Position{2711, 3462, 0}, "Woodcutting"},
	{"crafter", "ProgressiveCrafter", []string{"Spinning flax at Seers", "Banking bow strings", "Walking to the flax field"}, []string{"bow strings", "trips"}, 1779, 1777, Position{2724, 3492, 0}, "Crafting"},
	{"agility", "BarbarianAgility", []string{"Crossing the log balance", "Climbing the net", "Eating swordfish"}, []string{"laps", "falls"}, 373, 373, Position{2552, 3556, 0}, "Agility"},
}

// fakeBankStock is one banked stack in the fake fleet: a REAL obj id with the REAL name
// content gives it, and a plausible count range.
//
// The ids and names are real on purpose. The previous fixture banked one tool plus up to six
// stacks of a single product, which made the Combined bank panel's own measurement useless (the
// GPU assessment in docs/control_panel_gpu.md had to be taken against the live fleet because
// "the -fake fleet was not representative ... its banks were nearly empty") and left the
// valuation and banked-XP work with nothing to show. A fake account now banks the spread of
// goods its script would really accumulate, so -fake exercises the pricing lookup, the
// processing-route table and the panel's own row count end to end.
//
// Names are set here because a fake fleet has no config archive to resolve them from — the real
// FleetSource fills Slot.Name from cachecfg (see fleetsource.bankFrom).
type fakeBankStock struct {
	id       int32
	name     string
	min, max int32
}

// fakeBankStocks is what each fake script's bank fills up with, keyed by fakeBot.short.
// Every id/name pair is taken from the same content checkout internal/bankxp's table is
// generated from.
var fakeBankStocks = map[string][]fakeBankStock{
	"fisher": {
		{303, "Small fishing net", 1, 1},
		{317, "Raw shrimps", 800, 6000},
		{321, "Raw anchovies", 200, 2500},
		{377, "Raw lobster", 100, 1400},
		{371, "Raw swordfish", 40, 700},
		{359, "Raw tuna", 60, 900},
		{995, "Coins", 500, 40000},
		{526, "Bones", 5, 120},
	},
	"miner": {
		{1265, "Bronze pickaxe", 1, 1},
		{1436, "Rune essence", 2000, 22000},
		{440, "Iron ore", 100, 1800},
		{453, "Coal", 50, 900},
		{995, "Coins", 200, 15000},
	},
	"thief": {
		{995, "Coins", 5000, 250000},
		{1623, "Uncut sapphire", 3, 60},
		{1621, "Uncut emerald", 1, 30},
		{1619, "Uncut ruby", 0, 12},
		{335, "Raw trout", 20, 200},
	},
	"cutter": {
		{1351, "Bronze axe", 1, 1},
		{1511, "Logs", 500, 9000},
		{1521, "Oak logs", 200, 4000},
		{1519, "Willow logs", 100, 3000},
		{995, "Coins", 100, 9000},
	},
	"crafter": {
		{1779, "Flax", 400, 7000},
		{1777, "Bow string", 200, 5000},
		{1737, "Wool", 50, 800},
		{995, "Coins", 100, 9000},
	},
	"agility": {
		{373, "Swordfish", 20, 300},
		{995, "Coins", 100, 9000},
		{532, "Big bones", 10, 200},
	},
}

// fakeBank builds one account's bank. Caller holds f.mu (it is called from the tick loop, the
// same place the old inline fixture was).
func (f *FakeSource) fakeBank(bot fakeBot) []Slot {
	stocks := fakeBankStocks[bot.short]
	if len(stocks) == 0 {
		stocks = []fakeBankStock{{bot.tool, "", 1, 1}, {bot.product, "", 50, 550}}
	}
	out := make([]Slot, 0, len(stocks))
	slot := 0
	for _, s := range stocks {
		span := int(s.max - s.min)
		if span < 0 {
			continue
		}
		count := s.min
		if span > 0 {
			count += int32(f.rng.Intn(span + 1))
		}
		if count <= 0 {
			continue
		}
		out = append(out, Slot{Slot: slot, ID: s.id, Name: s.name, Count: count})
		slot++
	}
	return out
}

// fakeLevelForXP is a rough, deliberately non-authoritative stand-in for the real XP curve
// (internal/xptable, if this repo has one, is not worth pulling in for demo data) — just
// enough to make a fake account's level visibly climb as its XP does.
func fakeLevelForXP(xp int64) int {
	lvl := 1 + int(xp/830)
	if lvl > 99 {
		lvl = 99
	}
	return lvl
}

// NewFakeSource makes n accounts (6 when n is not positive), all offline, with a seeded random walk.
func NewFakeSource(n int, seed int64) *FakeSource {
	if n <= 0 {
		n = 6
	}
	f := &FakeSource{rng: rand.New(rand.NewSource(seed)), fishRNG: rand.New(rand.NewSource(seed ^ 0x465153)), runeRNG: rand.New(rand.NewSource(seed ^ 0x52554e45)), now: time.Now, subs: make(map[int]chan<- Event), bank: make(map[string][]Slot), bankAt: make(map[string]int64)}
	now := f.now().UnixMilli()
	for i := 0; i < n; i++ {
		b := fakeBots[i%len(fakeBots)]
		counters := make([]Counter, len(b.counters))
		for j, name := range b.counters {
			counters[j] = Counter{Name: name}
		}
		name := fmt.Sprintf("%s%02d", b.short, i+1)
		f.accounts = append(f.accounts, AccountStatus{Name: name, State: StateOffline, SinceUnixMs: now, Bot: b.name, Counters: counters})
		f.bots = append(f.bots, i%len(fakeBots))
		// Seed a last-known bank straight away rather than waiting for the tick loop's 1-in-150
		// "opened the bank" roll. A real fleet does the same thing on startup — fleet's
		// bankstore restores each account's last contents before it is even logged in — and
		// without it the Combined bank panel is empty for the first several minutes of every
		// -fake run, which is what made this fleet useless for measuring that panel.
		f.bank[name] = f.fakeBank(b)
		f.bankAt[name] = now
	}
	return f
}

// Run steps the fleet every interval until ctx is done.
func (f *FakeSource) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			f.Step()
		}
	}
}

// Step moves every account on by one beat of simulated life.
func (f *FakeSource) Step() {
	f.mu.Lock()
	now := f.now()
	ms := now.UnixMilli()
	var out []Event
	say := func(a *AccountStatus, level Level, kind, text string) {
		out = append(out, Event{TimeUnixMs: ms, Account: a.Name, Level: level, Kind: kind, Text: text})
	}
	set := func(a *AccountStatus, s State) {
		a.State = s
		a.SinceUnixMs = ms
	}
	for i := range f.accounts {
		a := &f.accounts[i]
		bot := fakeBots[f.bots[i]]
		switch a.State {
		case StateOffline:
			if f.rng.Intn(3) == 0 {
				set(a, StateQueued)
				f.queue = append(f.queue, i)
				say(a, LevelInfo, "login", "joined the login queue")
			}
		case StateConnecting:
			f.inFlight--
			switch r := f.rng.Intn(20); {
			case r == 0:
				a.LastError = "login response 3: invalid username or password"
				set(a, StateError)
				say(a, LevelError, "login", a.LastError)
			case r < 3:
				a.Reconnects++
				backoff := 6 * time.Second << uint(a.Reconnects-1)
				if backoff > 5*time.Minute {
					backoff = 5 * time.Minute
				}
				a.NextRetryUnixMs = now.Add(backoff).UnixMilli()
				a.LastError = "login response 16: too many login attempts"
				set(a, StateReconnecting)
				f.held = now.Add(20 * time.Second)
				say(a, LevelWarn, "login", fmt.Sprintf("refused for logging in too fast; the queue waits 20s and this account retries in %s", backoff))
			default:
				f.sessions++
				set(a, StateIngame)
				p := bot.home
				a.Pos = &p
				a.Activity = bot.activities[0]
				a.Reconnects = 0
				a.NextRetryUnixMs = 0
				a.LastError = ""
				a.PrimarySkill = bot.skill
				say(a, LevelInfo, "login", "logged in")
			}
		case StateIngame:
			a.Packets += int64(20 + f.rng.Intn(40))
			a.Pos.X += f.rng.Intn(3) - 1
			a.Pos.Z += f.rng.Intn(3) - 1
			a.Counters[0].Value += int64(f.rng.Intn(3))
			if bot.skill != "" {
				if a.Skills == nil {
					a.Skills = map[string]SkillCounter{}
				}
				sk := a.Skills[bot.skill]
				sk.XPGained += int64(4 + f.rng.Intn(12))
				sk.XP = sk.XPGained
				sk.Level = fakeLevelForXP(sk.XP)
				a.Skills[bot.skill] = sk
			}
			f.stepLoot(a.Name, f.bots[i])

			// HP: wobble plausibly once known, so the alerts digest and any HP display
			// have something real to react to — including, rarely, an actual death, the
			// exact class of event the alerts panel exists to surface loudly.
			switch {
			case !a.HPKnown:
				a.MaxHP = 10 + f.rng.Intn(40)
				a.HP = a.MaxHP
				a.HPKnown = true
			case a.Dead:
				if f.rng.Intn(10) == 0 {
					a.Dead = false
					a.HP = a.MaxHP
					say(a, LevelInfo, "state", "respawned")
				}
			default:
				switch r := f.rng.Intn(60); {
				case r == 0:
					a.HP -= 1 + f.rng.Intn(a.MaxHP/2+1)
					if a.HP <= 0 {
						a.HP = 0
						a.Dead = true
						say(a, LevelError, "crash", "died")
					} else if a.HP*4 < a.MaxHP {
						say(a, LevelWarn, "state", fmt.Sprintf("HP low: %d/%d", a.HP, a.MaxHP))
					}
				case r < 4 && a.HP < a.MaxHP:
					a.HP++
				}
			}

			// Bank: occasionally "opens" the bank and refreshes its own last-known
			// contents, the same "not live every tick, just a snapshot from whenever it
			// was last read" behavior a real fleet's BankWithdraw capture has.
			if f.rng.Intn(150) == 0 {
				f.bank[a.Name] = f.fakeBank(bot)
				f.bankAt[a.Name] = ms
				say(a, LevelInfo, KindLog, "banked its supplies")
			}

			// Chat: plausible overheard lines, streamed as chat.message events the same
			// way a real fleet's raw MESSAGE_GAME capture is (see FleetSource.observe).
			if f.rng.Intn(12) == 0 {
				say(a, LevelInfo, KindChat, fakeChatLines[f.rng.Intn(len(fakeChatLines))])
			}
			// RSB0T_FAKE_STABLE=1 keeps ingame accounts ingame (no crash, no dropped connection), so a
			// browser fixture can watch one view for minutes. The rng is still drawn, so every other
			// simulated behaviour keeps its sequence.
			roll := f.rng.Intn(400)
			if stable := os.Getenv("RSB0T_FAKE_STABLE") == "1"; stable && roll < 8 {
				roll = 399
			}
			switch r := roll; {
			case r == 0:
				f.sessions--
				a.LastError = "bot crashed: no path to the bank"
				a.Pos = nil
				a.Activity = ""
				set(a, StateError)
				say(a, LevelError, "crash", a.LastError)
			case r < 8:
				f.sessions--
				a.Reconnects = 1
				a.NextRetryUnixMs = now.Add(6 * time.Second).UnixMilli()
				a.LastError = "connection closed: read timeout"
				a.Pos = nil
				set(a, StateReconnecting)
				say(a, LevelWarn, "state", "lost the connection, reconnecting in 6s")
			case r < 40:
				a.Activity = bot.activities[f.rng.Intn(len(bot.activities))]
				if len(a.Counters) > 1 {
					a.Counters[1].Value++
				}
				say(a, LevelInfo, KindLog, a.Activity)
			case r < 90:
				say(a, LevelDebug, KindLog, fmt.Sprintf("at %d, %d", a.Pos.X, a.Pos.Z))
			}
		case StateReconnecting:
			if ms >= a.NextRetryUnixMs {
				set(a, StateQueued)
				f.queue = append(f.queue, i)
			}
		case StateError:
			if f.rng.Intn(60) == 0 {
				set(a, StateStopped)
				say(a, LevelInfo, "state", "stopped")
			}
		case StateStopped:
			if f.rng.Intn(30) == 0 {
				a.LastError = ""
				set(a, StateOffline)
			}
		}
	}
	// One login at a time, spaced, and nothing while the queue is held after a refusal.
	if len(f.queue) > 0 && !now.Before(f.nextAttempt) && !now.Before(f.held) {
		i := f.queue[0]
		f.queue = append(f.queue[:0], f.queue[1:]...)
		f.inFlight++
		f.nextAttempt = now.Add(fakeSpacing)
		a := &f.accounts[i]
		set(a, StateConnecting)
		say(a, LevelInfo, "login", "logging in")
	}
	f.mu.Unlock()
	for _, ev := range out {
		f.Emit(ev)
	}
}

// Emit hands an event to every subscriber, dropping it for any whose channel is full.
func (f *FakeSource) Emit(ev Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, ch := range f.subs {
		select {
		case ch <- ev:
		default:
			f.dropped++
		}
	}
}

// Dropped is how many events a full subscriber channel refused.
func (f *FakeSource) Dropped() uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dropped
}

// Accounts implements Source with deep copies.
func (f *FakeSource) Accounts(dst []AccountStatus) []AccountStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.accounts {
		a.Counters = append([]Counter(nil), a.Counters...)
		if a.Pos != nil {
			p := *a.Pos
			a.Pos = &p
		}
		if a.Skills != nil {
			cp := make(map[string]SkillCounter, len(a.Skills))
			for k, v := range a.Skills {
				cp[k] = v
			}
			a.Skills = cp
		}
		if script, ok := f.assignments.scriptOf(a.Name); ok {
			a.AssignedScript, a.RosterScript = script, f.rosterScriptLocked(a.Name)
		}
		dst = append(dst, a)
	}
	return dst
}

// Logins implements Source.
func (f *FakeSource) Logins() LoginStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.now()
	l := LoginStatus{Queue: make([]string, len(f.queue)), InFlight: f.inFlight, Sessions: f.sessions, SpacingMs: fakeSpacing.Milliseconds()}
	for i, idx := range f.queue {
		l.Queue[i] = f.accounts[idx].Name
	}
	if len(f.queue) > 0 {
		next := f.nextAttempt
		if f.held.After(next) {
			next = f.held
		}
		if next.After(now) {
			l.NextAttemptUnixMs = next.UnixMilli()
		}
	}
	if f.held.After(now) {
		l.HeldUntilUnixMs = f.held.UnixMilli()
	}
	return l
}

// MemStats implements Source with this process's real numbers.
func (f *FakeSource) MemStats() MemStats { return RuntimeMemStats() }

// Inventories implements InventorySource: a logged-in account carries its tool and some product.
func (f *FakeSource) Inventories(account string) ([]Inventory, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, a := range f.accounts {
		if a.Name != account {
			continue
		}
		if a.State != StateIngame {
			return []Inventory{}, true
		}
		bot := fakeBots[f.bots[i]]
		slots := []Slot{{Slot: 0, ID: bot.tool, Count: 1}}
		for s := 1; s <= int(a.Counters[0].Value%28); s++ {
			slots = append(slots, Slot{Slot: s, ID: bot.product, Count: 1})
		}
		return []Inventory{{ID: fakeBackpack, Slots: slots}}, true
	}
	return nil, false
}

// Bank implements BankSource from the fake fleet's own bank/bankAt store (see the
// FakeSource field docs) — real per-account state, just fabricated instead of
// server-decoded, so the bank panel is exercisable end to end under -fake.
func (f *FakeSource) Bank(account string) ([]Slot, int64, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if account == fakeMuleName {
		// The fake mule is not one of the accounts; its bank holds the Rune essence the fake miners hand it, which is what
		// the Loot panel's Runecrafting section reads as the mule's stock (lootrunecraft.go).
		return []Slot{{Slot: 0, ID: loot.ObjRuneEssence, Name: loot.RuneEssenceName, Count: int32(4087 + f.essenceHanded)}}, f.now().UnixMilli(), true
	}
	for _, a := range f.accounts {
		if a.Name != account {
			continue
		}
		items := f.bank[account]
		out := make([]Slot, len(items))
		copy(out, items)
		return out, f.bankAt[account], true
	}
	return nil, 0, false
}

// fakeScripts is what BotScripts reports: the same names fakeBots simulates, so a customize
// dialog built against a fake fleet looks like the real script picker.
var fakeScripts = func() []string {
	names := make([]string, len(fakeBots))
	for i, b := range fakeBots {
		names[i] = b.name
	}
	return names
}()

// BotScripts implements BotControl.
func (f *FakeSource) BotScripts() []string {
	return append([]string(nil), fakeScripts...)
}

// StartBot implements BotControl by flipping the account's simulated bot state to "running" —
// there is no real internal/bot.Runner behind a fake fleet, just enough state for the panel's
// start/stop control to be exercised end to end without a game connection.
func (f *FakeSource) StartBot(account, scriptName string, settings map[string]interface{}) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.accounts {
		if f.accounts[i].Name != account {
			continue
		}
		if f.accounts[i].BotState == "running" {
			return fmt.Errorf("fake: %s already has a bot running (%s)", account, f.accounts[i].Bot)
		}
		supplied := settings
		settings = f.assignedStartSettings(i, scriptName, supplied)
		f.accounts[i].Bot = scriptName
		f.accounts[i].BotState = "running"
		f.accounts[i].BotError = ""
		f.accounts[i].Settings = settings
		f.noteAssignment(i, scriptName, supplied, settings)
		return nil
	}
	return fmt.Errorf("fake: no such account %q", account)
}

// StopBot implements BotControl.
func (f *FakeSource) StopBot(account string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.accounts {
		if f.accounts[i].Name != account {
			continue
		}
		if f.accounts[i].BotState != "running" {
			return fmt.Errorf("fake: %s has no bot to stop", account)
		}
		f.accounts[i].BotState = "stopped"
		return nil
	}
	return fmt.Errorf("fake: no such account %q", account)
}

// RequestHandoff implements HandoffControl purely in memory — see the FakeSource struct's
// own doc comment on handoffRequested.
func (f *FakeSource) RequestHandoff(account string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.accounts {
		if f.accounts[i].Name != account {
			continue
		}
		if f.accounts[i].BotState != "running" {
			return fmt.Errorf("fake: %s has no bot running", account)
		}
		if f.handoffRequested == nil {
			f.handoffRequested = map[string]bool{}
		}
		f.handoffRequested[account] = true
		return nil
	}
	return fmt.Errorf("fake: no such account %q", account)
}

// SetMuleAt implements HandoffControl purely in memory — see the FakeSource struct's own
// doc comment on muleAtOverride.
func (f *FakeSource) SetMuleAt(account string, n int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.accounts {
		if f.accounts[i].Name != account {
			continue
		}
		if f.accounts[i].BotState != "running" {
			return fmt.Errorf("fake: %s has no bot running", account)
		}
		if f.muleAtOverride == nil {
			f.muleAtOverride = map[string]int{}
		}
		if n <= 0 {
			delete(f.muleAtOverride, account)
		} else {
			f.muleAtOverride[account] = n
		}
		return nil
	}
	return fmt.Errorf("fake: no such account %q", account)
}

// Disconnect implements ConnectionControl by flipping a connected account to
// StateStopped, mirroring a real Session.Logout closely enough that the panel's
// disconnect/reconnect controls are exercisable under -fake with no game connection.
func (f *FakeSource) Disconnect(account string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.accounts {
		if f.accounts[i].Name != account {
			continue
		}
		switch f.accounts[i].State {
		case StateIngame, StateConnecting, StateReconnecting, StateQueued:
		default:
			return fmt.Errorf("fake: %s is not connected", account)
		}
		if f.accounts[i].State == StateIngame {
			f.sessions--
		}
		f.accounts[i].State = StateStopped
		f.accounts[i].SinceUnixMs = f.now().UnixMilli()
		f.accounts[i].Pos = nil
		f.accounts[i].Activity = ""
		f.accounts[i].BotState = ""
		f.accounts[i].ManuallyDisconnected = true
		return nil
	}
	return fmt.Errorf("fake: no such account %q", account)
}

// ConnectExisting implements ConnectionControl by sending a stopped/offline account back
// through the login queue, the same path a real account takes on the way to StateIngame.
func (f *FakeSource) ConnectExisting(account string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.accounts {
		if f.accounts[i].Name != account {
			continue
		}
		switch f.accounts[i].State {
		case StateIngame, StateConnecting, StateQueued:
			return fmt.Errorf("fake: %s is already connected", account)
		}
		f.accounts[i].State = StateQueued
		f.accounts[i].SinceUnixMs = f.now().UnixMilli()
		f.accounts[i].ManuallyDisconnected = false
		f.queue = append(f.queue, i)
		return nil
	}
	return fmt.Errorf("fake: no such account %q", account)
}

// AddAccount implements ConnectionControl by appending a brand-new simulated account and
// connecting it immediately — a fake fleet has no real login round-trip to wait out — and
// starting script on it (with settings) when one is given, so "add account" and
// "duplicate an existing account's settings" are both exercisable under -fake.
func (f *FakeSource) AddAccount(account, password, script string, settings map[string]interface{}) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.accounts {
		if f.accounts[i].Name == account {
			return fmt.Errorf("fake: %s is already in this fleet", account)
		}
	}
	idx := 0
	for i, b := range fakeBots {
		if b.name == script {
			idx = i
			break
		}
	}
	b := fakeBots[idx]
	now := f.now().UnixMilli()
	pos := b.home
	counters := make([]Counter, len(b.counters))
	for j, name := range b.counters {
		counters[j] = Counter{Name: name}
	}
	st := AccountStatus{Name: account, State: StateIngame, SinceUnixMs: now, Pos: &pos, Bot: script, Counters: counters}
	if script != "" {
		st.BotState = "running"
		st.Settings = settings
		st.Activity = b.activities[0]
	}
	f.accounts = append(f.accounts, st)
	f.bots = append(f.bots, idx)
	f.sessions++
	return nil
}

// Subscribe implements Source.
func (f *FakeSource) Subscribe(ch chan<- Event) func() {
	f.mu.Lock()
	id := f.nextSub
	f.nextSub++
	f.subs[id] = ch
	f.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			f.mu.Lock()
			delete(f.subs, id)
			f.mu.Unlock()
		})
	}
}
