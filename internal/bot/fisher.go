package bot

import (
	"fmt"
	"hash/fnv"
	"math/rand"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/ml/observation"
	"rsb0t-go/internal/ml/policy"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/phaselog"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/fisherrunner"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/mulereceiverplan"
	"rsb0t-go/internal/scripts/mulerunner"
	"rsb0t-go/internal/scripts/tile"
	settingsbag "rsb0t-go/internal/settings"
	"rsb0t-go/internal/webwalk"
	"rsb0t-go/internal/webwalk/hops"
	"rsb0t-go/internal/webwalk/live"
)

func init() {
	Register("ProgressiveFisher", func() Bot { return &fisherBot{} })
}

// fisherBot ports ProgressiveFisher.ts's own loop() 1:1: every Loop call re-derives the
// account's phase from live state alone (fisherplan.DecideFishPhase) and does one
// phase-appropriate action, rather than the runtime's first two cuts (a fixed rung with a
// bare fish->bank loop, then a live-progression fish->bank loop that never left Catherby).
// A relog, a stop, or — the point of this pass — a death anywhere on the map resumes in the
// right place next Loop call, because nothing here is remembered across calls except the
// bank's own last-read contents (bankRead, mirroring ProgressiveFisher.ts's own `this.bank`)
// and the pickpocket/hand-off state that must survive one call to the next.
//
// Death recovery is NOT separate code: ProgressiveFisher.ts has none, because a death drops
// the account near Lumbridge with too little for the boat fare — exactly the state
// DecideFishPhase's own PhaseFund/PhaseTravel already detect and resolve on their own (see
// internal/scripts/freshstartrunner's doc comment for the full reasoning). Wiring those two
// phases live is this pass's real death-recovery fix, not a new stage machine.
//
// Selling fish for a short tool (PhaseSell) is ported: fish in the pack go to Harry and then
// Arhein, and with none in the pack the most valuable banked fish are drawn out first (see
// sellStep). The brightness step (PhaseBright) runs before a new account's first store trip.
//
// The hand-over load is drawn as notes, as ProgressiveFisher.ts's loadHandoff does, and a bank
// holding more rows than one pack of notes is handed over in further loads.
//
// The mule hand-off IS wired (2026-09-14, the account owner's own explicit spec that
// night): once the fishing target rung is actually reached and the bank holds muleAt fish
// (or the control panel's manual trigger fires — see Context.RequestHandoff/SetMuleAt),
// Loop intercepts before the ordinary phase dispatch, opens the Catherby bank, withdraws
// everything the mule should get (fisherplan.MuleRows — the fish, then anything else not
// kept; the ladder tools currently in play are never handed over, whatever muleKeep says),
// walks to the mule's post (mulereceiverplan.SeersStand — every script hands off at the
// same spot) and drives internal/scripts/donorrunner.Run, which requests a trade, offers
// the load, accepts, confirms, and gives the mule up after donorplan.MeetTimeout (10
// minutes — the TS original's own ASK_TIMEOUT_MS, confirmed against
// reference/rsb0t-scripts/donorPlan.ts; HANDOFF_RETRY_MS/donorplan.HandoffRetry, 5
// minutes, is the cooldown before trying again after a failed/refused hand-over, not the
// give-up timeout itself — the two are easy to conflate but the TS source is unambiguous).
// Coins are a deliberate exception to the whole-row keep/hand-over model: the account
// owner's own number (2026-09-14) is "withdraw everything except tools and 500gp for
// travel," so donorrunner splits coins at the trade screen (donorplan.DefaultTravelReserve)
// rather than an all-or-nothing keep-list entry — see handoffStep's own comment on why that
// 500 ends up in the donor's pocket, not sitting in the Catherby bank. Once the hand-over
// ends (traded, or gave up), the donor walks back to the Catherby bank and the very next
// Loop call resumes the ordinary phase machine from whatever the live state actually is —
// no separate "resume fishing" step, the same way death recovery already works here (see
// this file's own doc comment above).
//
// Settings, all optional (read via IntSetting/BoolSetting/StringSetting/FloatSetting, so
// both Go values and the float64/bool/string a JSON-decoded fleet.config.json roster entry
// produces work):
//
//	cheatSetup       bool    default false       — ::tele to Harry's stand + ::give enough
//	                                                coins to buy up the ladder to target, then
//	                                                let live progression buy whatever the
//	                                                level allows. LOCAL-ENGINE-ONLY.
//	cheatLevel       int     default 0            — when cheatSetup is on and this is > 0,
//	                                                ::setstat fishing to this level first.
//	target           string  default "Big net"    — how far up fisherplan.Rungs to climb.
//	autoSharks       bool    default true         — false keeps target even at Fishing 90+,
//	                                                where the ladder otherwise harpoons sharks
//	                                                (fisherplan.PinTarget). For big-net gem
//	                                                fishing.
//	homeWorld        int     default 0            — the world this account lives on: the
//	                                                control panel logs it in there, and the
//	                                                script hops there itself when it finds
//	                                                itself elsewhere (homeworld.go). 0 = the
//	                                                roster's.
//	targetFish       int     default 5            — stop one fishing session once this many
//	                                                raw fish have been caught since it started.
//	fishTimeoutSec   int     default 180          — give up a fishing session after this long.
//	maxCycles        int     default 0            — stop after this many complete bank trips;
//	                                                0 means run until Stop() is called.
//	fare             int     default 60           — coins wanted for the two boat fares
//	                                                (freshstart.DefaultFare).
//	pickpocketTarget string  default "Man"        — who pays the fare.
//	restBelowHp      int     default 40           — with no food, pickpocketing pauses at this
//	                                                HP% and resumes at 90%.
//	guideNpc         string  default "RuneScape Guide"
//	skipOption       string  default "Yes please"
//	mule             string  default "mule"     — the MuleReceiver account hand-overs go
//	                                                to; blank never hands over (see doc
//	                                                comment above). Live-overridable per
//	                                                account from the control panel's manual
//	                                                trigger via Context.RequestHandoff.
//	muleAt           int     default fisherplan.MuleFishAt (4,000) — fish banked, once the
//	                                                target rung is reached, that starts a
//	                                                hand-over. Live-overridable from the
//	                                                control panel via Context.SetMuleAt.
//	muleKeep         string  default fisherplan.DefaultMuleKeep — bank rows that never go
//	                                                to the mule, beyond the ladder tools
//	                                                (always kept) and coins (always split at
//	                                                the fixed 500gp travel reserve, never
//	                                                governed by this list — see doc comment
//	                                                above).
//	traceDecisions   bool    default false         — the real-observation bridge from
//	                                                docs/ml_lab_real_observation_bridge.md:
//	                                                emit one trace.Decision JSONL record
//	                                                per ordinary phase dispatch (see Loop's
//	                                                own comment for exactly which ones and
//	                                                why the mule hand-off is excluded).
//	                                                Off by default, purely additive, and
//	                                                never blocks a live decision on disk
//	                                                I/O — see fisherrunner.TraceRecorder's
//	                                                own doc comment.
//	tracePath        string  default "traces/<username-or-fisher>.jsonl" — where those
//	                                                records are appended.
//	traceExperimentId string default "progressive-fisher" — trace.Decision's own
//	                                                ExperimentID field; bump this alongside
//	                                                fisherplan.PhaseReward if that reward
//	                                                mapping ever changes, per the doc's own
//	                                                "Reward semantics" section.
//	eatFood          bool    default false         — carry food, eat it before HP is critical,
//	                                                and run to the Catherby bank when critical
//	                                                under attack, or at the first hit once out
//	                                                of food (see fisherplan.DecideSurvival).
//	                                                Defaults off (account owner's own call,
//	                                                2026-09-16) — opt in per account/fleet.
//	food             string  default "" (any)      — comma-separated foods to eat and withdraw
//	                                                (fighterfood.FoodOptions names).
//	foodCount        int     default 3             — bites each bank trip leaves in the pack, 0-14.
//	eatBelowHp       number  default 50            — eat at or below this HP%, 10-90.
//	fleeBelowHp      number  default 30            — critical at or below this HP%, 5-80; a
//	                                                critical account under attack flees.
//	maxHit           int     default 4             — the largest hit expected; eating starts at
//	                                                maxHit+1 HP and fleeing at maxHit HP whatever
//	                                                the percentages say, 0-30.
//	fishingGuild     bool    default true          — the Fishing Guild (readFisherGuild): at live Fishing
//	                                                68+ with a rung the Guild's spots offer, bank
//	                                                and fish there, hand off at 10,000 banked fish
//	                                                (unless muleAt is set) by walking the notes to
//	                                                the mule's post and back. See fisherguild.go.
//
// Every wait this bot makes goes through pause, which returns early the moment eating or fleeing
// is due, so a 180 s fishing session or a two-minute walk is cut short by a low HP instead of
// running on past it. The survival step runs before WaitForGuardian: Guardian's own in-combat flee
// holds the account busy for 36 s, and a fisher that cannot eat through that dies in it.
type fisherBot struct {
	layout fisherplan.Layout
	target string

	// keepCrystalKeys keeps crystal keys out of the mule hand-off, for the LootChestFisher
	// delegate ONLY. That script fishes as a plain ProgressiveFisher while it waits, holding
	// keys the mule gave it, and handing those back would trade the same keys in a circle.
	// An ordinary fisher leaves this false and hands over the keys it joined itself, which is
	// the whole point of joining them -- see fisherplan.NeverHandOver for the live incident.
	keepCrystalKeys bool

	// bassOyster is the BassOysterCaskets script (bassoyster.go): the Big net at the Fishing Guild and
	// the drop cycle (caskets and oysters opened, key halves joined, pearls cut, junk dropped). False is
	// ProgressiveFisher exactly. dropCycle is that cycle's settings and running totals, shared by every
	// Deps the bot builds; dropCycleRetryAt backs off a cycle that did nothing.
	bassOyster       bool
	dropCycle        *fisherrunner.Cycle
	dropCycleRetryAt time.Time

	// stall is the run staller (fisherstall.go, fisherrunner/stall.go), for both scripts: a carried
	// Games necklace or Ring of dueling rubbed every third tick of a run.
	stall *fisherrunner.Stall
	// manipOn is the fishManip setting and manip the manipulation's state, kept across every Deps the bot builds
	// (fishermanip.go).
	manipOn bool
	manip   *fisherrunner.ManipState
	// lastBankAt is when the last bankStep ended; bankRepeatWait spaces two of them.
	lastBankAt time.Time
	// manipKitAt is when the next kit trip may be made (fishermanip_kit.go).
	manipKitAt time.Time
	// fletch is the Fletching training and kitAskAt/chiselBuyAt space the kit ask and the chisel
	// purchase on the mule hand-off (bassoyster_fletch.go), for BassOysterCaskets.
	fletch      fletchTrain
	kitAskAt    time.Time
	chiselBuyAt time.Time

	maxCycles   int
	cyclesDone  int
	targetFish  int
	fishTimeout time.Duration

	fare             int
	pickpocketTarget string
	pickpocketSpot   tile.Tile
	// driftHome is handed to fisherrunner.Deps each Loop — see where it is set in Loop.
	driftHome  *pathfind.NavPoint
	restBelow  float64
	guideNpc   string
	skipOption string

	// mule/muleAt/muleKeep are this account's own hand-off settings — see the type's own
	// doc comment for exactly how they're used.
	mule     string
	muleAt   int
	muleKeep string

	// handoffPending latches a due hand-off (threshold met or a manual control-panel
	// trigger) across Loop calls that can't act on it immediately (e.g. the Catherby bank
	// wasn't reachable this call) so a transient failure doesn't silently drop a manual
	// request. handoffNextAllowed is donorplan.NextAllowed's own cooldown after a hand-over
	// that gave up. handoffsDone is purely a log/OnStop counter.
	handoffPending     bool
	handoffNextAllowed time.Time
	handoffRetryCycle  int
	handoffsDone       int

	// handoffGate looks at the mule's queue before a load is drawn (drawgate.go).
	handoffGate drawGate

	// knownUntradable is this account's own memory of item ids the trade screen has already
	// refused once (see donorrunner.Options.KnownUntradable's own doc comment) — lazily
	// allocated on first use, matching thief/sail's own long-lived-per-account pattern.
	knownUntradable map[int]bool

	// muleStand is where the hand-over trade happens (muleAtTile).
	muleStand tile.Tile

	// ask is this account's own memory of asking the mule to hand OUT a tool it is blocked
	// without — all of that logic lives in fisherhandout.go, which owns this field.
	ask *muleAsk

	// recover is the lost-tool recovery memory (Harry's restock, caskets, mule coins); see
	// fisherrecovery.go, which owns it.
	recover fisherRecovery

	// donor is this account's own side of the mule RESTOCK — answering the mule when IT runs
	// out of coins. All of that logic lives in fisherdonate.go, which owns this field. Nil
	// unless "donateCoins" is on.
	donor *fisherDonor

	phase     fisherplan.FishPhase
	havePhase bool
	bankRead  *fisherrunner.BankRead
	thief     *freshstartrunner.Thief
	sail      *freshstartrunner.SailProgress
	bright    *freshstart.Brightness
	refused   map[string]bool

	// walker plans freshstartrunner's own fare-recovery legs over webwalk's door/gate/stair/
	// ship-fare graph instead of plain same-level pathfind — see freshDeps and
	// freshstartrunner.walkWithWalker's own doc comment for the real Brimhaven-stranding gap
	// this closes. nil (falling back to today's plain-pathfind behavior) if live.NewWalker
	// fails at OnStart; every other bot already follows this same optional pattern.
	walker *webwalk.Walker
	// unsold is, per Catherby shop, the fish names that shop would not buy this run (lower-case),
	// ported from ProgressiveFisher.ts's own unsold map.
	unsold    map[string]map[string]bool
	caught    int
	starterGP int
	fishGP    int
	// boughtTools holds each tool bought ONCE, and toolBuys counts every purchase including
	// the repeats. They were one growing slice until 2026-09-20: a Big fish random event takes
	// the tool out of the pack and the account re-buys it, so a long run appended the same
	// name over and over (live 2026-09-19: 72 nets deleted across the fleet in five seconds),
	// and OnStop then printed the whole list. Nothing ever scanned it, so this was a log and
	// memory fault rather than a wedge — but it is the ever-growing-list shape, and the fleet
	// runs for hours.
	boughtTools []string
	toolBuys    int

	// trace/traceEpisode/lastObs are the opt-in real-observation bridge — see
	// traceDecisions's own doc comment above and Loop's for where these are used. trace
	// is nil (a safe no-op — see fisherrunner.TraceRecorder) unless traceDecisions was
	// on at OnStart.
	trace        *fisherrunner.TraceRecorder
	traceEpisode int
	lastObs      observation.State

	// surv is the resolved eat/flee settings; resting is the last pass's fisherplan Rest, for its
	// hysteresis. eatRetryAt spaces bites and backs off a failed one; restockRetryAt stops a
	// resting account reopening an empty bank every pass.
	surv    fisherplan.Survival
	resting bool

	// guild is the opt-in Fishing Guild state (fishingGuild); see fisherguild.go.
	guild fisherGuild
	// home is the homeWorld setting's state (homeworld.go).
	home           homeWorld
	eatRetryAt     time.Time
	restockRetryAt time.Time
	lastRestLog    time.Time
	bitesEaten     int
	flees          int
}

// pickpocketCombatRadius is how close to its pickpocket spot an account must be before the
// damage it takes is assumed to be its own failed pickpocket rather than a real attacker.
// See the CombatExpected assignment in Loop for the death that forced this to be a radius
// rather than the whole phase.
const pickpocketCombatRadius = 20

func (b *fisherBot) deps(ctx *Context) *fisherrunner.Deps {
	d := b.baseDeps(ctx)
	d.Stall = b.stall         // the run staller, for both scripts (fisherstall.go)
	d.Manip = b.manipConfig() // the fishing tick manipulation, for both scripts (fishermanip.go)
	if b.bassOyster {
		d.Junk = fisherplan.IsBassOysterJunk
		d.Cycle = b.dropCycle
	}
	return d
}

func (b *fisherBot) baseDeps(ctx *Context) *fisherrunner.Deps {
	return &fisherrunner.Deps{
		Session:    ctx.Session,
		Pack:       ctx.Pack,
		Locs:       ctx.Locs,
		Config:     ctx.Config,
		Components: ctx.Components,
		Chat:       ctx.Chat,
		Delay:      b.guardedDelay(ctx),
		Log:        ctx.Log,
		DriftHome:  b.driftHome,
		// Same live.NewWalker construction OnStart already builds for freshDeps below --
		// see fisherrunner.WalkNear's own doc comment for the live gap wiring it through
		// here too closes (an established account's ordinary bank-trip navigation had no
		// long-distance routing at all before this).
		Walker: b.walker,
		Loot:   ctx.Loot,
		Phase:  func(p string) func() { return fisherSubPhase(ctx, p) },
	}
}

func (b *fisherBot) freshDeps(ctx *Context) *freshstartrunner.Deps {
	return &freshstartrunner.Deps{
		Session:    ctx.Session,
		Pack:       ctx.Pack,
		Locs:       ctx.Locs,
		Config:     ctx.Config,
		Components: ctx.Components,
		Chat:       ctx.Chat,
		Delay:      b.guardedDelay(ctx),
		Log:        ctx.Log,
		Walker:     b.walker,
		Loot:       ctx.Loot,
	}
}

// defaultFisherMule is the mule setting's value when the key is missing altogether.
const defaultFisherMule = "mule"

// fisherMuleSetting reads the mule setting. Unlike StringSetting, an explicit blank is kept
// rather than replaced by the default: ProgressiveFisher.ts reads the mule setting with a blank
// default and documents "Blank = never hand anything over", and a roster entry with "mule": "" means
// exactly that. Found live: StringSetting turned "" into "mule", so an account configured
// never to hand over still walked to the mule once muleAt fish were banked. Only a missing or
// non-string key falls back to defaultFisherMule.
func fisherMuleSetting(settings map[string]interface{}) string {
	if v, ok := settings["mule"].(string); ok {
		return strings.TrimSpace(v)
	}
	return defaultFisherMule
}

// fisherTargetSetting reads target, pinned against the shark climb when autoSharks is false.
//
// Without the pin an account at Fishing 90+ harpoons sharks whatever its target says
// (fisherplan.SharkAutoLevel), and a shark harpoon catches no gems, caskets or oysters, which is
// what a big-net account is for. A pinned account also counts as at its target for the hand-off
// (fisherplan.AtTarget), which an unpinned 90+ big-net account never does.
func fisherTargetSetting(settings map[string]interface{}) string {
	target := StringSetting(settings, "target", fisherplan.DefaultTarget)
	if !BoolSetting(settings, "autoSharks", true) {
		target = fisherplan.PinTarget(target)
	}
	return target
}

// fisherTileSetting reads a tile setting in any form settings.Bag.Tile accepts ("x,z[,level]",
// {"x","z","level"} or [x, z, level]), falling back to def when missing or unreadable.
func fisherTileSetting(values map[string]interface{}, key string, def tile.Tile) tile.Tile {
	return settingsbag.NewBagFrom(values).Tile(key, def)
}

func hasToolName(owned []string, tool string) bool {
	for _, o := range owned {
		if strings.EqualFold(o, tool) {
			return true
		}
	}
	return false
}

func (b *fisherBot) OnStart(ctx *Context) error {
	if ctx.Pack == nil {
		return fmt.Errorf("fisher: no collision pack configured (fleet.config.json needs a packPath) — refusing to start rather than panic later inside pathfinding")
	}
	b.layout = fisherplan.Catherby()
	b.target = fisherTargetSetting(ctx.Settings)
	b.maxCycles = IntSetting(ctx.Settings, "maxCycles", 0)
	b.targetFish = IntSetting(ctx.Settings, "targetFish", 5)
	b.fishTimeout = time.Duration(IntSetting(ctx.Settings, "fishTimeoutSec", 180)) * time.Second

	b.fare = IntSetting(ctx.Settings, "fare", freshstart.DefaultFare)
	b.pickpocketTarget = StringSetting(ctx.Settings, "pickpocketTarget", freshstart.DefaultPickpocketTarget)
	b.pickpocketSpot = fisherTileSetting(ctx.Settings, "pickpocketTile", freshstart.PickpocketSpot)
	restHp := FloatSetting(ctx.Settings, "restBelowHp", 40)
	if restHp < 5 {
		restHp = 5
	} else if restHp > 90 {
		restHp = 90
	}
	b.restBelow = restHp / 100
	b.guideNpc = StringSetting(ctx.Settings, "guideNpc", freshstart.DefaultGuideNPC)
	b.skipOption = StringSetting(ctx.Settings, "skipOption", freshstart.DefaultSkipOption)

	b.mule = fisherMuleSetting(ctx.Settings)
	b.muleAt = jitterMuleAt(IntSetting(ctx.Settings, "muleAt", fisherplan.MuleFishAt), ctx.Username)
	b.muleKeep = StringSetting(ctx.Settings, "muleKeep", fisherplan.DefaultMuleKeep)
	b.muleStand = fisherTileSetting(ctx.Settings, "muleAtTile", mulereceiverplan.SeersStand)
	b.surv = readFisherSurvival(ctx.Settings)
	b.guild = readFisherGuild(ctx.Settings, ctx.Username)
	if b.bassOyster {
		b.applyBassOysterSettings(ctx) // the Big net, pinned, at the Guild: bassoyster.go
	}
	// The fishing tick manipulation, for ProgressiveFisher and BassOysterCaskets alike (fishermanip.go).
	b.manipOn = readFisherManip(ctx.Settings)
	ctx.logf("fisher: fishing tick manipulation %v (fishManip) — a knife and a plain log in the pack make a net, big net, lobster pot or harpoon spot roll every 3 ticks instead of 6; asked of the mule on the hand-off when the pack and bank hold none", b.manipOn)
	// The run staller, for ProgressiveFisher and BassOysterCaskets alike (fisherstall.go).
	b.stall = readFisherStall(ctx.Settings)
	ctx.logf("fisher: run staller %v — a carried Games necklace or Ring of dueling is rubbed every third tick of a run on a walk (walkRub), drawn from the bank or asked of the mule when the pack holds none", b.stall.On)

	b.thief = freshstartrunner.NewThief()
	b.sail = freshstartrunner.NewSailProgress()
	b.knownUntradable = map[int]bool{}
	b.bright = &freshstart.Brightness{}
	b.refused = map[string]bool{}
	b.unsold = map[string]map[string]bool{}
	b.havePhase = false
	if w, err := live.NewWalker(live.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: b.guardedDelay(ctx), Log: ctx.Log,
	}); err != nil {
		ctx.logf("fisher: no webwalk (%v) — fare-recovery legs use the plain walker", err)
	} else {
		// Ship fares and the Brimhaven customs officer are NPC hops the Walker only plans
		// through with a handler installed. Without one, the only way a fisher on the
		// Taverley or Falador side could plan back to Catherby was over White Wolf Mountain,
		// which webwalk's automatic "white-wolf-dens" zone now forbids; the ship chain is the
		// wolf-free way. Same wiring as MuleReceiver's and BarbarianAgility's walkers.
		if ctx.Components != nil && ctx.Config != nil {
			hops.Install(w, hops.New(hops.Deps{
				Client: w.Client, Session: ctx.Session, Config: ctx.Config,
				Components: ctx.Components, Log: ctx.Log,
			}))
		}
		b.walker = w
	}

	// DEFAULT FALSE, AND THAT IS A SAFETY PROPERTY, NOT A PREFERENCE.
	//
	// This sends ::tele and ::give. It is LOCAL-ENGINE-ONLY, and it used to default to TRUE --
	// so every production account was safe only because the roster remembered to say
	// "cheatSetup": false. It remembered 202 times out of 203.
	//
	// The 203rd was spare104, live on 2026-09-22. It had been switched to LootChestFisher and
	// its settings rewritten for THAT schema (mule, muleAtTile, muleKeep, target), which
	// dropped the flag -- and LootChestFisher delegates to this very bot, reading the same
	// ctx.Settings. So a production account sat at "::tele to Harry's stand" every 30 seconds
	// for the whole run, never handing anything over, while the fleet owner watched it fail.
	//
	// harness.Cheats did its job and refused rather than silently sending nothing, which is
	// the only reason this was a visible stall instead of a real teleport on a live account.
	// That is the failure docs/trustworthy_harnesses.md exists for, and it caught this one.
	//
	// Flipping the default costs nothing: EVERY caller that wants cheats already passes
	// "cheatSetup": true explicitly (cmd/barbmultilaptest, cmd/eventstrandtest,
	// cmd/fisherladdertest, cmd/fisherfoodtest). Nothing relied on the default being true.
	if BoolSetting(ctx.Settings, "cheatSetup", false) {
		d := b.deps(ctx)
		cheatLevel := IntSetting(ctx.Settings, "cheatLevel", 0)
		topIdx := fisherplan.TargetIndex(b.target)
		coins := 0
		for i := 0; i <= topIdx; i++ {
			coins += fisherplan.Rungs[i].ToolCost
		}
		coins *= 5
		if coins < 50 {
			coins = 50
		}
		if err := fisherrunner.CheatSetup(d, b.layout, cheatLevel, coins); err != nil {
			return fmt.Errorf("fisher: cheat setup: %w", err)
		}
	}
	if BoolSetting(ctx.Settings, "traceDecisions", false) {
		defaultPath := "traces/fisher.jsonl"
		if ctx.Username != "" {
			defaultPath = fmt.Sprintf("traces/%s.jsonl", ctx.Username)
		}
		tracePath := StringSetting(ctx.Settings, "tracePath", defaultPath)
		experimentID := StringSetting(ctx.Settings, "traceExperimentId", "progressive-fisher")
		rec, err := fisherrunner.NewTraceRecorder(tracePath, experimentID)
		if err != nil {
			// Never fail the whole script over an experimental, opt-in feature — log
			// and continue exactly as if traceDecisions had been left off.
			ctx.logf("fisher: traceDecisions requested but could not start (%v) — continuing without it", err)
		} else {
			b.trace = rec
			b.traceEpisode = int(time.Now().Unix())
			ctx.logf("fisher: recording live decisions to %q (experiment %q, episode %d)", tracePath, experimentID, b.traceEpisode)
		}
	}

	ctx.logf("fisher: target %q, mule %q, hand off at %d fish banked (muleKeep %q)", b.target, b.mule, b.muleAt, b.muleKeep)
	if b.guild.enabled {
		at := b.guild.muleAt
		if b.guild.muleAtExplicit {
			at = b.muleAt
		}
		ctx.logf("fisher: fishingGuild is on — at Fishing %d+ with a Guild rung this account fishes the Fishing Guild and hands off at %d fish banked", fisherplan.FishingGuildEntryLevel, at)
	}
	if b.surv.Enabled {
		foods := "any food"
		if len(b.surv.Foods) > 0 {
			foods = strings.Join(b.surv.Foods, ", ")
		}
		ctx.logf("fisher: eating %s, %d kept, eat at %.0f%% and flee at %.0f%% with a %d HP hit margin", foods, b.surv.FoodCount, b.surv.EatBelow*100, b.surv.FleeBelow*100, b.surv.MaxHit)
	} else {
		ctx.logf("fisher: eatFood is off — no food is carried and nothing flees before a hit lands")
	}
	// Off unless the roster asks for it; see fisherdonate.go.
	b.setUpDonating(ctx)
	b.home.start(ctx, "fisher")
	return nil
}

func bankCoinsOf(s fisherplan.FishSnapshot) int {
	if s.BankCoins != nil {
		return *s.BankCoins
	}
	return 0
}

func bankFishOf(s fisherplan.FishSnapshot) int {
	if s.FishBanked != nil {
		return *s.FishBanked
	}
	return 0
}

// snapshot builds the FishSnapshot fisherplan.DecideFishPhase reads, from the live session
// and the last bank read cached on b — ported from ProgressiveFisher.ts's own snapshot().
//
// One deliberate simplification, documented rather than hidden: BrightPending is always
// false, since this client has no way to switch the options side-tab yet (a new, tiny
// primitive with zero gameplay impact — see freshstartrunner's own doc comment on why the
// brightness step is out of scope for this pass). PhaseBright is therefore never chosen;
// a brand-new account simply never turns its brightness up, which changes nothing about
// fishing, banking, or the fund/travel recovery this pass is actually about.
func (b *fisherBot) snapshot(ctx *Context, d *fisherrunner.Deps) fisherplan.FishSnapshot {
	main, _, _ := ctx.Session.State().Modals()
	s := fisherplan.FishSnapshot{
		MainModal:     main,
		Level:         fisherrunner.FishingLevel(d),
		Target:        b.target,
		Coins:         fisherrunner.Coins(d),
		HeldTools:     fisherrunner.OwnedTools(d),
		FishHeld:      fisherrunner.CurrentRawFish(d),
		PackFull:      fisherrunner.PackFull(d) || fisherrunner.KeptFull(d),
		Fare:          b.fare,
		BrightPending: freshstartrunner.BrightPending(b.freshDeps(ctx), b.bright),
	}
	if x, z, level, ok := ctx.Session.State().Tile(); ok {
		t := tile.Tile{X: x, Z: z, Level: level}
		s.Tile = &t
	}
	if b.bankRead != nil {
		coins := b.bankRead.Coins
		fish := b.bankRead.Fish
		s.BankCoins = &coins
		s.FishBanked = &fish
		// DecideFishPhase's own sentinel for "the bank has never been read" is a nil
		// BankTools slice — a real read whose bank genuinely holds no ladder tool must
		// therefore come through as a non-nil EMPTY slice, not nil, or every such read
		// looks exactly like "never read" again and PhaseBank never lets go. Found live:
		// a fresh cheat-set account's first bank open (Big net rung, no tools bought
		// yet) produced BankRead{Tools: nil} and the bot spun on "bank phase done: 0
		// fish, 0 coins, tools [] in the bank" forever, one call after another with no
		// delay of its own (the same busy-loop shape BarbarianAgility hit tonight).
		s.BankTools = append([]string{}, b.bankRead.Tools...)
	}
	pack := fisherrunner.PackItems(d)
	s.Sellable = len(freshstart.SellQueue(pack, fisherplan.StarterKeep(), b.refused))
	b.recover.fill(b, ctx, &s)
	return s
}

// ---- main loop ----

func (b *fisherBot) Loop(ctx *Context) error {
	if b.guild.enabled && b.phase != fisherplan.PhaseFund {
		// Measure drift from the nearer home before the Guardian wait below: the Guild bank is
		// 223 tiles from Catherby's, past the 150-tile drift limit. See fisherguild.go.
		b.driftHome = b.guildDriftRef(ctx)
		b.applyGuildRefs(ctx, b.driftHome)
	}
	d := b.deps(ctx)
	if b.survivalStep(ctx) {
		return nil
	}
	if !fisherrunner.WaitForGuardian(d) {
		return nil
	}
	// ProgressiveFisher has no dedicated death-recovery code (unlike BarbarianAgility's own
	// recover(), which explicitly waits for the respawn teleport then calls ClearDead) — a
	// death here just drops the account near Lumbridge with little money, a state
	// PhaseFund/PhaseTravel already handle for an unrelated reason (see this file's own
	// package doc comment), so gameplay recovers on its own with no extra code needed.
	// gamestate.State.Dead itself, though, is a one-way latch only ClearDead ever resets
	// (see its own doc comment) — since nothing here ever called it, every fisher account
	// that has died even once shows dead=true on the dashboard for the rest of the run
	// regardless of how long it's been back to normal, live-found 2026-09-15 (a 200-account
	// soak: 33/200 accounts flagged dead, the great majority of them actively fishing/
	// walking/pickpocketing normally with full HP). Clear it as soon as HP is known and
	// positive — the account is unambiguously alive by then, whether or not this Loop call
	// happens to be mid-death-animation-turned-real-recovery or just a stale flag from
	// several minutes ago.
	if ctx.Session.State().IsDead() {
		if hp, ok := ctx.Session.State().HP(); ok && hp > 0 {
			ctx.Session.State().ClearDead()
		}
	}
	// A stray trade screen (this bot never opens one itself yet — the mule hand-off isn't
	// wired) is left alone rather than acted on blind.
	if actions.NewTrade(ctx.Session).Active() {
		ctx.SetPhase(phaselog.Idle)
		b.pause(ctx, 2*time.Second)
		return nil
	}
	// On another world than homeWorld (switched here at runtime, or a hop that did not land):
	// go home before drawing a load or asking the mule for anything. See homeworld.go.
	if b.home.step(ctx, "fisher", time.Now()) {
		b.pause(ctx, 5*time.Second)
		return nil
	}

	s := b.snapshot(ctx, d)
	// Catherby, or with fishingGuild on and the Guild selected, the Fishing Guild: sets b.layout
	// and s.Guild. A no-op beyond the Catherby layout with the setting off.
	b.selectFishingArea(ctx, &s)

	// BassOysterCaskets only: a pack nearly full with something to open, join, cut or drop gets a
	// drop cycle and goes on fishing rather than banking it as it is (bassoyster.go).
	if b.bassOyster && b.dropCycleStep(ctx, d, s) {
		return nil
	}

	// The mule hand-off is checked before the ordinary phase dispatch, not folded into
	// fisherplan.DecideFishPhase (which stays a 1:1 port of ProgressiveFisher.ts's own
	// phase list) — a cross-cutting concern the same way the dialogue-dismiss guard below
	// is, since a manual control-panel trigger can fire from any phase, not just PhaseBank.
	if manual := ctx.TakeHandoffRequest(); manual {
		b.handoffPending = true
		b.handoffGate.force = true
	}
	threshold := b.guildMuleAt(b.muleAt)
	if v, ok := ctx.MuleAtOverride(); ok {
		threshold = v
	}
	donorSettings := donorplan.NewSettings(b.mule, threshold, b.muleKeep)
	if donorSettings.Enabled() {
		// "Once the target is reached" (fisherplan.MuleFishAt's own doc comment): the
		// level has caught up to the target rung AND the account actually holds that
		// rung's tool — level alone can be true while PhaseBuy still hasn't landed the
		// tool yet.
		targetReached := fisherplan.AtTarget(s.Level, s.Target) && hasToolName(s.HeldTools, fisherplan.CurrentRung(s.Level, s.Target).Tool)
		autoDue := targetReached && b.cyclesDone >= b.handoffRetryCycle && b.bankRead != nil && donorplan.HandoffDue(bankFishOf(s), threshold, time.Now(), b.handoffNextAllowed)
		if b.handoffPending || autoDue {
			b.handoffPending = true
			ctx.SetPhase(phaselog.Handoff)
			return b.handoffStep(ctx, d, donorSettings)
		}
	}

	phase := fisherplan.DecideFishPhase(s)
	// Override PhaseFund when the account can already reach the bank by a plain walk —
	// found during a follow-up staging audit, same pattern as SailLeg's own fix (see its
	// doc comment) and travelViaFreshStart's identical fix: fisherplan.FishFundTarget
	// calls freshstart.FareRemaining, which shares freshstart.TravelLeg's own
	// ArdougneSideMaxX (x<2740) blind spot. Catherby's own fishing camp sits entirely
	// east of that threshold (x 2789-2865ish), so ANY position outside InCatherby's own
	// tight BankRadius(60) — easily reached by one ordinary combat flee (up to 40 tiles,
	// see fleeDistFor) from the edge of the fishing area — reads as "still needs full
	// boat fare," even though it is nothing more than a routine, no-boat walk back to the
	// bank. Left unchecked, a temporarily displaced, coin-light fisher would be sent
	// pickpocketing instead — and PickpocketSpotFor's own "away from Lumbridge"
	// classification always matches out here, so it would just wait forever for a Man
	// that will never wander by. Checking real pack connectivity to the bank first closes
	// this before it can ever misfire, exactly like SailLeg's own entry check.
	// ...but only for an account that is actually displaced. Live-found 2026-09-18:
	// fisherplan.DecideFishPhase now returns PhaseFund (rather than the old dead-end
	// PhaseStuck) for an account that has no usable tool, no coins and nothing to sell,
	// so it can pickpocket its way back. For an account standing AT the bank that is
	// exactly the right answer — but the bank is then trivially walkable from two tiles
	// away, so this override flipped it straight to PhaseTravel, SailLeg reported
	// "already on the destination side" and returned instantly, the phase was recomputed
	// to PhaseFund again, and the account span in a hot loop logging that line many times
	// a second. Production account "spare28" (Fishing 55) sat at 2811,3441 doing exactly that
	// at 0 XP/hour. The override is only ever meant to catch a fisher that has been pushed
	// out of the bank's own radius (one ordinary combat flee is up to 40 tiles) and needs
	// a plain walk home, so gate it on genuinely being outside that radius.
	if phase == fisherplan.PhaseFund && d.Pack != nil && s.Tile != nil {
		bank := b.layout.Bank
		from := pathfind.NavPoint{X: s.Tile.X, Z: s.Tile.Z, Level: s.Tile.Level}
		to := pathfind.NavPoint{X: bank.X, Z: bank.Z, Level: bank.Level}
		if fisherplan.FundIsReallyJustWalkHome(phase, s.Tile, d.Pack.FindPath(from, to).OK) {
			phase = fisherplan.PhaseTravel
		}
	}
	// Ask the mule before earning it the hard way. PhaseFund while already IN Catherby is
	// precisely DecideFishPhase's "no usable tool held or banked, not enough coins for one,
	// nothing to sell" branch — the displaced-but-reachable reading was redirected to
	// PhaseTravel just above — which is the exact state production account "spare28" sat in for
	// hours two tiles from a mule holding spares. Bounded and additive: askMuleForATool
	// returns false on any refusal, timeout or missing mule, and PhaseFund's ordinary
	// pickpocket recovery then runs exactly as it does today. See fisherhandout.go.
	_, guildTool := b.guildToolTarget(s) // the Guild's spawn is asked before the mule
	if phase == fisherplan.PhaseFund && s.Tile != nil && fisherplan.InCatherby(*s.Tile) && !guildTool {
		ctx.SetPhase(phaselog.Handoff)
		if b.askMuleForATool(ctx, s) {
			return nil // holding a tool again — let the next Loop call recompute the phase
		}
	}
	// Failing a pickpocket stuns and damages the account — confirmed live and reproduced
	// on the lab ("You fail to pick the man's pocket." -> "You've been stunned!" -> HP
	// dropping to 50%). That damage reads as InCombat, so the guardian's blind "in combat
	// with no tracked event" branch treats the account's own failed attempt as an
	// unexplained attack and flees it 20-40 tiles from the very Men it is pickpocketing;
	// it then walks back and repeats, never banking a coin. Production account
	// crafter1 did exactly that at the Lumbridge spot (3222,3218) at 0 XP/hour.
	//
	// Only the blind InCombat fallback is disabled, and only while actually funding:
	// genuine random events still detect and resolve normally through Detect/HandleEvade,
	// and every other phase keeps the protection. internal/bot/scarabswarm.go reached the
	// same conclusion independently for its own pickpocketing stage (see its
	// stage2CombatExpected setting).
	// ...but ONLY while actually standing at the spot attempting pickpockets, never while
	// travelling to it. Live-found within minutes of the first version of this: production
	// account "spare28" (combat level 3, no food) gave up pickpocketing at Catherby, set off
	// on the escape walk to the Lumbridge spot — a real 668-cost, 357-waypoint cross-map
	// route — and died 62 seconds in, with no flee logged the whole way because this flag
	// was suppressing the guardian for the entire fund phase, travel included. Self-
	// inflicted stun damage from a failed pickpocket is expected; being attacked halfway
	// across the map is not, and that is exactly when a defenceless account most needs the
	// blind flee.
	if ctx.Guardian != nil {
		atSpot := s.Tile != nil && tile.Chebyshev(*s.Tile, b.pickpocketSpot) <= pickpocketCombatRadius
		ctx.Guardian.CombatExpected = phase == fisherplan.PhaseFund && atSpot
	}
	// While funding, the account is SUPPOSED to be at the Lumbridge pickpocket spot, not at
	// the Catherby bank ~430 tiles away — so measure drift against where it is actually
	// meant to be. Live-found 2026-09-18: with the reference pinned to Catherby,
	// crafter1 logged "drifted far from home while waiting — recovering immediately"
	// on repeat while standing right beside the spot its own fund phase had sent it to,
	// with drift-recovery hauling it back toward Catherby the entire time. Re-pointing it
	// (rather than disabling the check) keeps the safety net live for a genuine
	// fail-teleport during the fund phase.
	b.driftHome = b.guildDriftRef(ctx) // nil unless fishingGuild is on and the Guild is nearer
	if phase == fisherplan.PhaseFund {
		spot := pathfind.NavPoint{X: b.pickpocketSpot.X, Z: b.pickpocketSpot.Z, Level: b.pickpocketSpot.Level}
		b.driftHome = &spot
	}
	if b.guild.enabled {
		d.DriftHome = b.driftHome
		b.applyGuildRefs(ctx, b.guildDriftRef(ctx))
	}
	if !b.havePhase || phase != b.phase {
		b.phase = phase
		b.havePhase = true
		ctx.logf("phase: %s", fisherplan.DescribeFishPhase(phase))
		if phase == fisherplan.PhaseFish {
			if active, ok := fisherplan.ActiveRung(s.Level, s.Target, s.HeldTools); ok {
				ctx.logf("active rung: %q (level %d, tool %q, op %q/pair %q)", active.Label, active.Level, active.Tool, active.Op, active.Pair)
			}
		}
	}

	// Dismiss a level-up (or similar) dialogue mid any phase but the two that expect one of
	// their own — matches ProgressiveFisher.ts's own guard in loop().
	if phase != fisherplan.PhaseAccept && phase != fisherplan.PhaseTutorial {
		dlg := actions.NewChatDialog(ctx.Session, ctx.Components)
		if dlg.CanContinue() {
			_ = dlg.Continue()
			return nil
		}
	}

	// fishingGuild only: a lost tool comes from its spawn inside the Guild entrance, not Harry's.
	if sp, ok := b.guildToolTarget(s); ok && phase != fisherplan.PhaseAccept && phase != fisherplan.PhaseTutorial && phase != fisherplan.PhaseWait {
		ctx.SetPhase(phaselog.Shopping)
		return b.guildToolStep(ctx, d, sp)
	}
	// fishingGuild only: the walk between Catherby and the Guild, through the Guild's doors.
	if dest, name, ok := b.guildTravelTarget(phase, s); ok {
		ctx.SetPhase(phaselog.Walking)
		return b.guildTravelStep(ctx, d, *s.Tile, dest, name)
	}
	// Every fisher: a knife and a plain log missing from the pack and the bank are fetched from the mule in one trip, once
	// (fishermanip_kit.go), so the fishing tick manipulation runs on every fishing account.
	if phase == fisherplan.PhaseFish && b.manipKitStep(ctx, d) {
		return nil
	}
	// BassOysterCaskets only: while Fletching is under 41 and there are logs and a knife, one training
	// round at the bank instead of fishing, so pearls can be cut into tips (bassoyster_fletch.go).
	if phase == fisherplan.PhaseFish && b.bassOyster && b.fletchTrainStep(ctx, d, s) {
		return nil
	}
	ctx.SetPhase(fisherPhaseName(phase))

	fd := b.freshDeps(ctx)

	// ---- ML trace bridge (opt-in, see traceDecisions's own doc comment above and
	// docs/ml_lab_real_observation_bridge.md) ----
	//
	// The observation is snapshotted BEFORE the phase acts — "a recorded decision must
	// describe the state actually available when the policy selected its action," per
	// that doc's decision-boundary contract — and the outcome/reward come from the
	// same counters this file already keeps for its own logging (b.caught,
	// b.cyclesDone, b.fishGP+b.starterGP, len(b.boughtTools)), compared before and
	// after the switch below runs. This covers every fisherplan.DecideFishPhase branch
	// this Loop call can dispatch to; the mule hand-off (handoffStep, above, which
	// returns before phase is ever computed) is a separate cross-cutting concern not
	// yet wired into this bridge — a known, documented scope limit, not an oversight.
	// The mule's own restock appeal, checked with the phase already known so "is this account
	// busy?" is answered from what it is actually about to do rather than guessed. A no-op
	// unless "donateCoins" is on, and it only ever acts while ordinary fishing — see
	// fisherdonate.go. Returning straight away when it did act: a donation is a whole trip,
	// not something to fold into the same Loop call as a fishing phase.
	if b.donor != nil {
		banked := 0
		if b.bankRead != nil {
			banked = b.bankRead.Coins
		}
		if b.donateStep(ctx, d, phase, banked) {
			return nil
		}
	}

	var obsBefore observation.State
	var caughtBefore, cyclesBefore, gpBefore, toolsBefore int
	if b.trace != nil {
		obsBefore = fisherrunner.ObservationFromSnapshot(d, b.layout, s)
		caughtBefore, cyclesBefore, gpBefore, toolsBefore = b.caught, b.cyclesDone, b.fishGP+b.starterGP, b.toolBuys
	}

	var stepErr error
	switch phase {
	case fisherplan.PhaseAccept:
		freshstartrunner.AcceptAppearance(fd)
	case fisherplan.PhaseTutorial:
		freshstartrunner.SkipTutorial(fd, b.guideNpc, b.skipOption)
	case fisherplan.PhaseBright:
		freshstartrunner.BrightnessStep(fd, b.bright)
	case fisherplan.PhaseWait:
		b.pause(ctx, 1200*time.Millisecond)
	case fisherplan.PhaseStore:
		sold, gp := freshstartrunner.SellStarterKit(fd, freshstart.StoreStand, fisherplan.StarterKeep(), b.refused)
		if sold > 0 {
			b.starterGP += gp
			ctx.logf("sold the starter kit: %d items for %d gp (%d coins)", sold, gp, fisherrunner.Coins(d))
		}
	case fisherplan.PhaseFund:
		freshstartrunner.PickpocketStep(fd, b.thief, freshstartrunner.PickpocketOpts{
			Target: b.pickpocketTarget, Spot: b.pickpocketSpot,
			RestBelow: b.restBelow, Need: fisherplan.FishFundTarget(s),
		})
	case fisherplan.PhaseTravel:
		freshstartrunner.SailLeg(fd, b.layout.Bank, b.sail)
	case fisherplan.PhaseBank:
		stepErr = b.bankStep(ctx, d, s)
		// A pearl cutter with no chisel anywhere buys one at Arhein's, a few tiles from the Catherby bank,
		// with the coins the bank step drew for it.
		if stepErr == nil && b.layout.Bank == fisherplan.Catherby().Bank {
			b.buyChisel(ctx, d)
		}
	case fisherplan.PhaseBuy:
		stepErr = b.buyStep(ctx, d, s)
	case fisherplan.PhaseSell:
		stepErr = b.sellStep(ctx, d, s)
	case fisherplan.PhaseOpen:
		stepErr = b.openStep(ctx, d, s)
	case fisherplan.PhaseAskCoins:
		stepErr = b.askCoinsStep(ctx, d, s)
	case fisherplan.PhaseRestock:
		stepErr = b.restockStep(ctx, d, s)
	case fisherplan.PhaseFish:
		stepErr = b.fishStep(ctx, d, s)
	case fisherplan.PhaseStuck:
		rung := fisherplan.CurrentRung(s.Level, s.Target)
		ctx.logf("fisher: stuck — no %s, %d coins and nothing to sell; hand this account a small fishing net or a few coins", rung.Tool, s.Coins)
		b.pause(ctx, 10*time.Second)
	}

	if b.trace != nil {
		progress := fisherplan.TraceProgress{
			FishCaught: b.caught - caughtBefore,
			BankTrip:   b.cyclesDone > cyclesBefore,
			ToolBought: b.toolBuys > toolsBefore,
			SoldGP:     (b.fishGP + b.starterGP) - gpBefore,
		}
		reward := fisherplan.PhaseReward(phase, progress)
		result := "ok"
		if stepErr != nil {
			result = stepErr.Error()
		}
		b.lastObs = obsBefore
		b.trace.Record(b.traceEpisode, obsBefore, policy.Action(phase), result, reward, false)
	}
	return stepErr
}

// ---- the Catherby bank ----

// bankStep deposits the catch and everything else but the tool(s) in play, tops up coins
// and the tool being fished with from the bank when buying is imminent, and refreshes
// b.bankRead — ported from ProgressiveFisher.ts's own bankStep. The mule hand-off itself is
// a separate step (handoffStep, below), checked in Loop before bankStep is ever reached —
// see this file's own doc comment.
func (b *fisherBot) bankStep(ctx *Context, d *fisherrunner.Deps, s fisherplan.FishSnapshot) error {
	// However the snapshot comes to ask for the bank, it is never asked for twice inside bankRepeatMin: a bank visit that did not
	// change what asked for it (a pack the deposit cannot empty) would otherwise repeat every second, as it did on 7 Oct 2026.
	if wait := bankRepeatWait(b.lastBankAt, time.Now()); wait > 0 {
		ctx.logf("fisher: banking again %s after the last bank visit because %s — waiting %s before the next one", time.Since(b.lastBankAt).Round(time.Second), bankWhy(s), wait.Round(time.Second))
		if !b.pause(ctx, wait) {
			return nil
		}
	}
	defer func() { b.lastBankAt = time.Now() }()
	ctx.logf("fisher: banking: %s (%d raw fish, %d free slots, %d coins in the pack)", fisherplan.BankReason(s), s.FishHeld, fisherrunner.FreeSlots(d), s.Coins)
	bank, ok := fisherrunner.OpenCatherbyBank(d, b.layout)
	if !ok {
		if b.survivalDue(ctx) {
			return nil // cut short to eat or flee, not a failure
		}
		ctx.logf("fisher: could not reach or open the Catherby bank — waiting before retrying")
		b.pause(ctx, 5*time.Second)
		return nil
	}
	defer bank.Close()

	rung := fisherplan.CurrentRung(s.Level, s.Target)
	owned := append(append([]string{}, s.HeldTools...), s.BankTools...)
	active, hasActive := fisherplan.ActiveRung(s.Level, s.Target, owned)
	// Not while Harry is out of stock: the coins then belong in the bank and the pack goes
	// fishing with the tool owned (fisherrecovery.go).
	buying := !hasToolName(owned, rung.Tool) && s.Coins+bankCoinsOf(s) >= rung.ToolCost && !s.ShopOut

	var keepRungs []*fisherplan.Rung
	keepRungs = append(keepRungs, &rung)
	if hasActive {
		keepRungs = append(keepRungs, &active)
	}
	keep := fisherBankKeep(fisherplan.BankKeep(buying, keepRungs...), b.surv, fisherrunner.EdibleFood(d))
	prevFish := bankFishOf(s)
	fisherrunner.DepositExcept(d, bank, keep)

	read := fisherrunner.ReadBank(d, bank)
	if buying && read.Coins > 0 {
		fisherrunner.WithdrawAllOf(d, bank, freshstart.Coins)
	}
	wanted := map[string]bool{rung.Tool: true}
	if hasActive {
		wanted[active.Tool] = true
	}
	for tool := range wanted {
		if tool == "" || hasToolName(fisherrunner.OwnedTools(d), tool) {
			continue
		}
		if hasToolName(read.Tools, tool) {
			fisherrunner.WithdrawOneOf(d, bank, tool)
		}
	}
	b.withdrawFood(ctx, d, bank)
	fisherrunner.DrawRubItem(d, bank)  // the run staller's jewellery, when the pack holds none and the bank does
	fisherrunner.DrawChisel(d, bank)   // the pearl cut's chisel, from Fletching 41, when the pack holds none and the bank does
	fisherrunner.DrawManipKit(d, bank) // the fishing tick manipulation's knife and plain logs, when the pack holds none and the bank does
	final := fisherrunner.ReadBank(d, bank)
	b.bankRead = &final
	ctx.logf("bank phase done: %d fish, %d coins, %d food, tools %v in the bank", final.Fish, final.Coins, final.Food, final.Tools)

	// A "cycle" (maxCycles/-maxcycles' own unit) is a real catch actually reaching the
	// bank, not just any deposit — a bank visit that only banks leftover coins after
	// buying a tool (DecideFishPhase sends the account back to the bank for exactly that
	// right after PhaseBuy) must NOT count, or maxCycles=1 stops the run before it ever
	// reaches PhaseFish once. Found live: the first full run after this pass's
	// phase-machine rewrite stopped after buying "Big fishing net" and banking the
	// change, having never fished at all, because the old "anything got deposited"
	// counter fired on that incidental coin deposit.
	if final.Fish > prevFish {
		b.cyclesDone++
	}
	if b.maxCycles > 0 && b.cyclesDone >= b.maxCycles {
		ctx.requestStop()
	}
	return nil
}

// ---- the mule hand-off ----

// stripCoinsFromKeep drops freshstart.Coins from a keep list: coins are never governed by
// the whole-row keep/hand-over choice muleKeep otherwise makes for everything else — see
// this file's own doc comment on why they're split at the trade screen instead
// (donorplan.DefaultTravelReserve).
func stripCoinsFromKeep(keep []string) []string {
	out := make([]string, 0, len(keep))
	for _, k := range keep {
		if !strings.EqualFold(strings.TrimSpace(k), freshstart.Coins) {
			out = append(out, k)
		}
	}
	return out
}

// haulAfterReserve is the pack slots still for the mule once the coin stack the travel reserve
// holds back is left out. donorrunner offers coins only above the reserve, so a stack at or below it
// never crosses; counted as haul it kept the donor meeting the mule and declining empty trades until
// the 10-minute give-up. Found live: a donor holding 1 coin traded 5 noted stacks, then requested
// and declined a trade about every 10 seconds.
func haulAfterReserve(haul, coins, reserve int) int {
	if coins > 0 && coins <= reserve && haul > 0 {
		return haul - 1
	}
	return haul
}

// joinHalvesBeforeHandoff turns this account's own banked key halves into crystal keys, so the
// haul crosses as keys rather than as pairs of dead rows.
//
// Reuses mulerunner.JoinKeyHalvesRound rather than growing a second copy, for the same reasons
// lootchestfisher does: that round resolves the half ids through ResolveKeyIDs instead of by
// name (985 and 987 BOTH display the identical "Half of a key", so a name lookup cannot tell
// them apart) and it counts a join by the crystal-key count rising rather than by clicks sent.
//
// Returns the number joined, or -1 when the round could not run at all, so the caller can tell
// "nothing to do" from "did not happen".
func (b *fisherBot) joinHalvesBeforeHandoff(ctx *Context, d *fisherrunner.Deps) int {
	if ctx.Config == nil || ctx.Components == nil {
		return -1
	}
	res, ok := mulerunner.JoinKeyHalvesRound(&mulerunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
	})
	if res.Stopped {
		return -1
	}
	if !ok {
		if res.Reason != "" {
			ctx.logf("fisher: hand-off: could not join key halves (%s) — handing over as they are", res.Reason)
		}
		return -1
	}
	if res.Joined > 0 {
		ctx.logf("fisher: hand-off: joined %d key half pair(s) into crystal key(s) before handing over", res.Joined)
	}
	return res.Joined
}

// handoffStep runs one full mule hand-over: open the Catherby bank, withdraw everything for
// the mule (fisherplan.MuleRows — the ladder tools currently in play always stay, whatever
// muleKeep says, via muleRules' own First/Keep rule; coins are withdrawn in full here and
// split at the trade screen instead, see stripCoinsFromKeep), walk to the mule's post and
// drive donorrunner.Run, then walk back to Catherby. Blocking, like bankStep/fishStep
// already are — one Loop call, cancellable throughout via ctx.Delay.
//
// Why 500gp ends up in the donor's pocket, not the Catherby bank: donorplan.Rules only
// knows "this whole row stays" or "this whole row goes," so getting an exact partial amount
// across means withdrawing every coin like any other row and having donorrunner hold back
// the reserve at the trade screen (Trade.Offer with an exact quantity) rather than never
// withdrawing it in the first place. Users asked for "500gp for travel" — money
// actually on the donor, not banked at Catherby where it does nothing for a donor that may
// end up walking from wherever this hand-over leaves it — so this reads that ask as "the
// donor keeps 500gp on hand," not "500gp specifically never leaves the bank."
func (b *fisherBot) handoffStep(ctx *Context, d *fisherrunner.Deps, settings donorplan.Settings) error {
	// A queue too long to join is fished through rather than stood in. See drawGate.
	if wait, why, hold := b.handoffGate.check(ctx, settings.Mule); hold {
		b.handoffPending = false
		b.handoffNextAllowed = time.Now().Add(wait)
		ctx.logf("fisher: hand-off held back: %s — fishing on, asking again in %s", why, wait.Round(time.Second))
		return nil
	}
	bank, ok := fisherrunner.OpenCatherbyBank(d, b.layout)
	if !ok {
		ctx.logf("fisher: hand-off: could not reach or open the Catherby bank — will retry")
		b.pause(ctx, 5*time.Second)
		return nil // b.handoffPending stays true; retried on the next Loop call.
	}

	// Read the bank before choosing the keep list: whether the coin row is handed over at all
	// depends on how much is banked (see fisherHandoffKeep).
	bankedCoins := fisherrunner.ReadBank(d, bank).Coins
	keep := fisherHandoffKeep(settings.Keep, b.surv, bankedCoins)
	keep = append(keep, b.kitKeepNames()...) // the run staller's jewellery and the pearl kit never cross to the mule
	if b.keepCrystalKeys {
		keep = append(keep, fisherplan.NeverHandOver...)
	}
	if bankedCoins <= fisherplan.DeathFloatCoins {
		ctx.logf("fisher: hand-off: keeping %d banked coin(s) as death-recovery money (float %d) — not handing them over",
			bankedCoins, fisherplan.DeathFloatCoins)
	}
	// The bank is readied for the mule in fisherplan.HandoffPrepOrder: banked caskets (and oysters)
	// opened, then key halves joined, then the trade below.
	for _, prep := range fisherplan.HandoffPrepOrder() {
		switch prep {
		case fisherplan.PrepOpenBanked:
			// Users want every casket looted and every oyster opened before the mule gets the
			// haul (2026-09-19), so what crosses the trade is the coins, gems and pearls inside.
			packKeep := append(append([]string{}, keep...), fisherplan.ToolNames()...)
			var opened int
			var ok bool
			bank, opened, ok = fisherrunner.OpenBankedOpenables(d, bank, func() (*actions.Bank, bool) {
				return fisherrunner.OpenCatherbyBank(d, b.layout)
			}, packKeep)
			if !ok {
				ctx.logf("fisher: hand-off: lost the bank while opening caskets/oysters — will retry")
				b.pause(ctx, 5*time.Second)
				return nil
			}
			if opened > 0 {
				ctx.logf("fisher: hand-off: opened %d casket(s)/oyster(s) before handing over", opened)
			}
		case fisherplan.PrepJoinKeyHalves:
			// JOIN KEY HALVES BEFORE THE HAUL CROSSES, at the fleet owner's request (2026-09-21).
			//
			// This is the right moment for it and not an arbitrary one: the casket pass
			// immediately before is what PRODUCES the halves, so they are in this account's own
			// bank seconds old. Two halves are two bank rows and two trade slots; one crystal key
			// is one of each and is immediately usable, so joining here costs a bank round and
			// saves the mule the same work on a queue it serves one donor at a time.
			//
			// Measured before building it rather than assumed: twelve accounts were holding
			// halves, most of them one or two, sitting unjoined.
			//
			// The round opens its own bank, so the hand-off's bank has to be re-taken afterwards.
			// A failure is not worth abandoning the hand-off for -- the halves keep, and the
			// mule's own idle joining still covers them.
			if joined := b.joinHalvesBeforeHandoff(ctx, d); joined >= 0 {
				var ok bool
				bank, ok = fisherrunner.OpenCatherbyBank(d, b.layout)
				if !ok {
					ctx.logf("fisher: hand-off: lost the bank after joining key halves — will retry")
					b.pause(ctx, 5*time.Second)
					return nil
				}
			}
		case fisherplan.PrepTrade:
			// Last in the order: everything below.
		}
	}
	rows := fisherplan.MuleRows(fisherrunner.BankItemRows(d, bank), keep)
	// Drawn as notes, as ProgressiveFisher.ts's loadHandoff does (Bank.setNoteMode(true), then
	// withdrawLoad per row, stopping when the pack is full). Found reading the port against the TS:
	// drawn as items, raw fish do not stack, so a bank at muleAt 2500 came out 27 fish at a time,
	// one Catherby-to-Seers round trip per 27.
	// THE WHOLE DRAW IS ONE BURST (twopertick-1012): note mode on, a Withdraw-All per row (a note takes one pack slot, so
	// as many rows as the pack has free slots) and note mode off go out together, five user events a tick, and are
	// confirmed once. It was a note-mode switch that waited on the varp, a Withdraw-All and a wait per row, and another
	// switch and wait.
	drawNames := make([]string, 0, len(rows))
	for _, row := range rows {
		drawNames = append(drawNames, row.Name)
	}
	noteDraw, asked := bank.DrawAllAsNotes(bank.RowIDsByName(d.Config, drawNames), actions.NoteResolver(d.Config), fisherrunner.FreeSlots(d), fisherrunner.Waiter(d))
	if asked > 0 {
		ctx.logf("fisher: hand-off: %s", actions.VisitLog("note draw", asked+2, noteDraw, 0))
	}
	final := fisherrunner.ReadBank(d, bank)
	b.bankRead = &final
	rowsLeft := len(fisherplan.MuleRows(fisherrunner.BankItemRows(d, bank), keep))
	_ = bank.Close()

	// One row per pack slot, as the TS hands Inventory.items() to muleHaul/muleLoad: merged rows
	// counted 27 unstackable fish as one slot.
	packRows := func() []donorplan.ItemRow { return fisherrunner.PackSlotRows(d) }
	drawn := fisherplan.MuleHaul(packRows(), keep)
	ctx.logf("fisher: hand-off to %q — %d row(s) in the bank, %d pack slot(s) drawn, %d row(s) left for another load, heading to %d,%d", settings.Mule, len(rows), drawn, rowsLeft, b.muleStand.X, b.muleStand.Z)

	// fishingGuild only: out through the Guild's doors to the post first. See fisherguild.go.
	b.guildHandoffWalk(ctx, d)

	// The currency 995, not the members obj 617 that also answers to "Coins": ObjIDs is in
	// id order, so the first-id form picks the wrong one. Nothing downstream reads CoinsID
	// today — donorrunner decides the split from CoinsHeld, which counts by name, and
	// offers each group's own pack id — so this was latent rather than live, but a wrong
	// id sitting in a field called CoinsID is a trap for whoever wires it up next. See
	// cachecfg.Config.CurrencyID.
	coinsID := int32(0)
	if id, ok := d.Config.CurrencyID(); ok {
		coinsID = int32(id)
	}
	rd := &donorrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
		Bus: ctx.Bus, Me: ctx.Username,
	}
	// Reach a mule that is logged in to a different world (worldhopplan bounds it; a
	// one-world fleet no-ops). See bot.WireDonorWorlds.
	WireDonorWorlds(ctx, rd)
	res := donorrunner.Run(rd, donorrunner.Options{
		Mule: settings.Mule, Stand: b.muleStand,
		RowsInBank: len(rows), Drawn: drawn,
		Load: func() []donorplan.LoadGroup {
			return fisherplan.MuleLoad(packRows(), donorplan.DefaultMuleFreeSlots, keep)
		},
		HaulLeft: func() int {
			return haulAfterReserve(fisherplan.MuleHaul(packRows(), keep), fisherrunner.Coins(d), donorplan.DefaultTravelReserve)
		},
		CoinsName: freshstart.Coins, CoinsID: coinsID,
		CoinsHeld:       func() int { return fisherrunner.Coins(d) },
		Reserve:         donorplan.DefaultTravelReserve,
		KnownUntradable: b.knownUntradable,
	})

	b.handoffPending = false
	switch {
	case res.Stopped:
		ctx.logf("fisher: hand-off interrupted by a stop request")
		return nil
	case res.GaveUp:
		// Widened from 1-10 to 2-19 rounds on 2026-09-19. The jitter's job is to stop a large
		// donor cohort converging on one mule together, and a lower bound of 1 meant a donor
		// that had just failed could rejoin the same queue after a single round — which is
		// what keeps a crowd at the bank self-sustaining. A minimum of 2 guarantees real
		// fishing happens between attempts, and the wider spread thins arrivals across a
		// longer window. Measured cost of the old pairing (4-minute wait, 1-10 rounds) on an
		// 85-donor live cohort: 72 hand-off attempts, 20 trades, 13 donors each burning a
		// full timeout — about 52 donor-minutes of fishing lost in twelve minutes.
		rounds := 2 + rand.Intn(18)
		b.handoffRetryCycle = b.cyclesDone + rounds
		b.handoffNextAllowed = time.Time{}
		ctx.logf("fisher: hand-off: %s — fishing %d more banked rounds before trying again", res.Reason, rounds)
	case res.Traded:
		b.handoffsDone++
		ctx.logf("fisher: hand-off #%d complete — %d trade(s), %d pack slot(s) crossed to %q", b.handoffsDone, res.Loads, res.Slots, settings.Mule)
		// Riders on this hand-off, at the mule's post: the kit (run staller jewellery, the Fletching climb's
		// knife and logs, a chisel) in one order, then a chisel from Arhein's when the mule had none.
		b.handoffKit(ctx, d)
		b.buyChisel(ctx, d)
		if rowsLeft > 0 {
			// More rows than one pack of notes: ProgressiveFisher.ts's loadHandoff goes round
			// again until the bank has none, so the next Loop call draws the next load.
			b.handoffPending = true
			ctx.logf("fisher: %d bank row(s) still for %q — drawing the next load", rowsLeft, settings.Mule)
		}
	default:
		ctx.logf("fisher: hand-off ended without trading (%s)", res.Reason)
	}

	if !fisherrunner.WalkNear(d, b.layout.Bank, 8) {
		ctx.logf("fisher: hand-off: could not walk back to the bank at %d,%d — the next Loop call will keep trying", b.layout.Bank.X, b.layout.Bank.Z)
	}
	return nil
}

// ---- the next tool ----

// sellStep ports ProgressiveFisher.ts's sellStep: raise the price of the next tool. Fish in the
// pack go to Harry, who pays the better price, and what Harry refuses goes to Arhein's general
// store; with no fish in the pack, the most valuable fish in the bank are drawn out first
// (drawFishToSell). When neither shop buys what the pack holds, the fish are banked and fishing
// goes on. It replaces the stand-in that fished (and, from 1e7e6f8, banked a full pack) instead: a
// fishing-16 account with no coins never raised the price of a big net that way.
func (b *fisherBot) sellStep(ctx *Context, d *fisherrunner.Deps, s fisherplan.FishSnapshot) error {
	rung := fisherplan.CurrentRung(s.Level, s.Target)
	if s.FishHeld == 0 {
		return b.drawFishToSell(ctx, d, s, rung)
	}
	shops := []struct {
		name  string
		stand tile.Tile
	}{{fisherplan.Harry, b.layout.HarryStand}, {fisherplan.Arhein, b.layout.ArheinStand}}
	for _, sh := range shops {
		refused := b.unsold[sh.name]
		if refused == nil {
			refused = map[string]bool{}
			b.unsold[sh.name] = refused
		}
		names := fisherplan.SellOrder(fisherrunner.PackItems(d), refused)
		if len(names) == 0 {
			continue
		}
		here := tile.Tile{}
		if x, z, level, ok := ctx.Session.State().Tile(); ok {
			here = tile.Tile{X: x, Z: z, Level: level}
		}
		if tile.Chebyshev(here, sh.stand) > 3 {
			if !fisherrunner.WalkNear(d, sh.stand, 2) {
				ctx.logf("fisher: could not reach %s's shop — retrying", sh.name)
				b.pause(ctx, 3*time.Second)
			}
			return nil
		}
		shop, ok := fisherrunner.OpenShopNear(d, sh.name, sh.stand)
		if !ok {
			ctx.logf("fisher: could not open %s's shop — retrying", sh.name)
			b.pause(ctx, 2*time.Second)
			return nil
		}
		for _, name := range names {
			coins := fisherrunner.Coins(d)
			if coins >= rung.ToolCost {
				break
			}
			count := fisherplan.SellCountFor(rung.ToolCost-coins, name, fisherrunner.PackCountByName(d, name))
			sold, gained, err := fisherrunner.SellToShop(d, shop, name, count)
			if err != nil || sold <= 0 || gained <= 0 {
				refused[strings.ToLower(name)] = true
				ctx.logf("fisher: %s would not buy %s", sh.name, name)
				continue
			}
			b.fishGP += gained
			ctx.logf("sold %d %s to %s for %d gp (%d/%d coins for a %s)", sold, strings.ToLower(name), sh.name, gained, fisherrunner.Coins(d), rung.ToolCost, strings.ToLower(rung.Tool))
		}
		_ = shop.Close()
		return nil
	}
	ctx.logf("fisher: neither Harry nor Arhein would buy the fish in the pack — banking them and fishing on")
	b.unsold = map[string]map[string]bool{}
	return b.bankStep(ctx, d, s)
}

// drawFishToSell ports drawFishToSell: at the Catherby bank, deposit everything but the coins and
// the tools in play, then withdraw, as items (a shop will not buy a note), the fish that raise the
// shortfall quickest (fisherplan.FishToSell, capped by the pack's free slots).
func (b *fisherBot) drawFishToSell(ctx *Context, d *fisherrunner.Deps, s fisherplan.FishSnapshot, rung fisherplan.Rung) error {
	bank, ok := fisherrunner.OpenCatherbyBank(d, b.layout)
	if !ok {
		if b.survivalDue(ctx) {
			return nil
		}
		ctx.logf("fisher: could not reach or open the Catherby bank to draw fish — waiting before retrying")
		b.pause(ctx, 5*time.Second)
		return nil
	}
	defer bank.Close()
	owned := append(append([]string{}, s.HeldTools...), s.BankTools...)
	keepRungs := []*fisherplan.Rung{&rung}
	if active, ok := fisherplan.ActiveRung(s.Level, s.Target, owned); ok {
		a := active
		keepRungs = append(keepRungs, &a)
	}
	// The coins stay out: they are the start of the money being raised.
	fisherrunner.DepositExcept(d, bank, fisherBankKeep(fisherplan.BankKeep(true, keepRungs...), b.surv, fisherrunner.EdibleFood(d)))
	need := rung.ToolCost - fisherrunner.Coins(d)
	plan := fisherplan.FishToSell(need, fisherrunner.BankStock(d, bank), fisherrunner.FreeSlots(d))
	if len(plan) == 0 {
		read := fisherrunner.ReadBank(d, bank)
		b.bankRead = &read
		ctx.logf("fisher: no fish in the bank to sell for a %s", strings.ToLower(rung.Tool))
		b.pause(ctx, 2*time.Second)
		return nil
	}
	if err := fisherrunner.SetNoteMode(d, bank, false); err != nil {
		ctx.logf("fisher: could not switch the bank to items: %v", err)
	}
	var drew []string
	for _, line := range plan {
		if err := fisherrunner.WithdrawXByName(d, bank, line.Name, line.Count); err != nil {
			ctx.logf("fisher: withdrawing %d %s: %v", line.Count, line.Name, err)
			continue
		}
		drew = append(drew, fmt.Sprintf("%d %s", line.Count, strings.ToLower(line.Name)))
	}
	read := fisherrunner.ReadBank(d, bank)
	b.bankRead = &read
	ctx.logf("drew %s to sell for a %s", strings.Join(drew, ", "), strings.ToLower(rung.Tool))
	return nil
}

// ---- fishing ----

func (b *fisherBot) fishStep(ctx *Context, d *fisherrunner.Deps, s fisherplan.FishSnapshot) error {
	active, ok := fisherplan.ActiveRung(s.Level, s.Target, s.HeldTools)
	if !ok {
		return nil
	}
	before := fisherrunner.CurrentRawFish(d)
	caught := fisherrunner.FishUntil(d, b.layout, active, before+b.targetFish, b.fishTimeout)
	// Raw fish dropped as junk (BassOysterCaskets) were caught all the same.
	gained := caught - before + d.RawDropped
	if gained > 0 {
		b.caught += gained
		ctx.logf("fishing session done: %d raw fish caught this session (rung %q, %d total)", gained, active.Label, caught)
	}
	return nil
}

func (b *fisherBot) OnStop(ctx *Context) {
	b.logDropCycleTotals(ctx)
	if b.stall != nil && b.stall.Rubs > 0 {
		ctx.logf("fisher: run staller totals — %d rub(s) on walks", b.stall.Rubs)
	}
	if b.fletch.rounds > 0 {
		ctx.logf("fisher: fletching training totals — %d round(s), %d logs cut into %d products, %d xp (off for the run: %v)", b.fletch.rounds, b.fletch.logsCut, b.fletch.productsMade, b.xpSinceTraining(ctx), b.fletch.off)
	}
	ctx.logf("fisher: stopped — fishing level %d, %d caught, %d bank trip(s), %d mule hand-off(s), bought %v across %d purchase(s), %d gp from the starter kit, %d pockets picked, %d bite(s) eaten, %d flee(s)",
		fisherrunner.FishingLevel(b.deps(ctx)), b.caught, b.cyclesDone, b.handoffsDone, b.boughtTools, b.toolBuys, b.starterGP, b.thief.Pockets, b.bitesEaten, b.flees)
	if b.guild.enabled {
		ctx.logf("fisher: fishingGuild: %d walk(s) between Catherby and the Guild", b.guild.walks)
		b.applyGuildRefs(ctx, nil)
	}

	if b.trace != nil {
		// The episode's terminal decision — this run stopped for whatever reason a
		// Runner decided to (an operator Stop(), maxCycles being reached via
		// ctx.requestStop() in bankStep, or an OnStart/Loop error). This bridge does
		// not yet distinguish those reasons from each other (a known, documented scope
		// limit — see docs/ml_lab_real_observation_bridge.md's list of terminal
		// reasons for what a fuller version would separate out).
		b.trace.Record(b.traceEpisode, b.lastObs, policy.Action("STOP"), "stopped", 0, true)
		if err := b.trace.Close(); err != nil {
			ctx.logf("fisher: closing the trace file: %v", err)
		}
		if dropped := b.trace.Dropped(); dropped > 0 {
			ctx.logf("fisher: trace recorder dropped %d decision(s) (queue was full)", dropped)
		}
		if writeErrs := b.trace.WriteErrors(); writeErrs > 0 {
			ctx.logf("fisher: trace recorder failed to write %d decision(s)", writeErrs)
		}
	}
}

// ---- staying alive ----

// bankRefugeRadius is how close to the Catherby bank stand counts as safe: the wolves and bats that
// killed accounts never come into the town centre.
const bankRefugeRadius = 6

// readFisherSurvival resolves the eat/flee settings. Defaults off 2026-09-16 (account
// owner's own call): a 200-account soak this session found the actual root cause of
// nearly every fishing-camp death was random-event mishandling (see randomevents' own
// combat-flee/tele fixes landed the same session), not unavoidable combat needing a food
// safety net — users want event handling itself fixed first and this layer opt-in
// until then, not silently defaulting every fleet to eating/fleeing behavior it didn't
// ask for.
func readFisherSurvival(m map[string]interface{}) fisherplan.Survival {
	return fisherplan.Survival{
		Enabled:   BoolSetting(m, "eatFood", false),
		Foods:     fisherplan.ParseFoodList(StringSetting(m, "food", "")),
		FoodCount: IntSetting(m, "foodCount", fisherplan.DefaultFoodCount),
		EatBelow:  FloatSetting(m, "eatBelowHp", fisherplan.DefaultEatBelowHP) / 100,
		FleeBelow: FloatSetting(m, "fleeBelowHp", fisherplan.DefaultFleeBelowHP) / 100,
		MaxHit:    IntSetting(m, "maxHit", fisherplan.DefaultMaxHit),
	}.Normalise()
}

// fisherBankKeep adds the food in the pack to a bank deposit's keep list, so a deposit-all never
// banks the food the run is carrying.
func fisherBankKeep(keep []string, surv fisherplan.Survival, packFood []freshstart.PackItem) []string {
	if !surv.Enabled {
		return keep
	}
	return append(append([]string{}, keep...), surv.FoodNames(packFood)...)
}

// fisherHandoffKeep is the mule hand-over's keep list: muleKeep plus every allowed food, so the
// food in the bank and the pack stays with the fisher.
//
// Coins are the one row decided here rather than by muleKeep, and the decision is bankCoins
// against fisherplan.DeathFloatCoins:
//
//   - At or below the float, the coin row is KEPT, so those coins are never withdrawn and stay
//     banked as the account's death-recovery money. This is the case that matters: an account
//     that dies while fishing loses its pack, and only banked coins can fund the walk to a
//     bank, the fare and a replacement tool.
//   - Above the float, coins are stripped from the keep list and withdrawn like any other row,
//     so the surplus reaches the mule. donorrunner still holds back its own reserve at the
//     trade screen, which is the donor's travel money for the trip home, and the next ordinary
//     bank visit deposits it (fisherplan.BankKeep banks coins unless they are about to be
//     spent).
//
// Whole-row rather than "withdraw all but 500" because the bank interface has no
// arbitrary-amount withdrawal — see DeathFloatCoins' own doc comment.
func fisherHandoffKeep(muleKeep []string, surv fisherplan.Survival, bankCoins int) []string {
	keep := muleKeep
	if bankCoins > fisherplan.DeathFloatCoins {
		keep = stripCoinsFromKeep(muleKeep)
	}
	return append(append([]string{}, keep...), surv.KeepNames()...)
}

// pause is ctx.Delay cut short when eating or fleeing is due; false means the caller should give up
// its current leg, as it would on a stop.
func (b *fisherBot) pause(ctx *Context, d time.Duration) bool {
	return b.guardedDelay(ctx)(d)
}

// guardedDelay is the Delayer every runner call gets: it sleeps in short slices and returns false
// as soon as fisherplan.DecideSurvival says to eat or flee.
func (b *fisherBot) guardedDelay(ctx *Context) func(time.Duration) bool {
	return func(d time.Duration) bool {
		const slice = 300 * time.Millisecond
		for {
			if b.survivalDue(ctx) {
				return false
			}
			step := d
			if step > slice {
				step = slice
			}
			if !ctx.Delay(step) {
				return false
			}
			d -= step
			if d <= 0 {
				return true
			}
		}
	}
}

// rawDeps are runner deps whose waits stop only for a stop request, for the survival steps
// themselves: a flee cut short by the very HP that started it would never arrive.
func (b *fisherBot) rawDeps(ctx *Context) *fisherrunner.Deps {
	d := b.deps(ctx)
	d.Delay = ctx.Delay
	return d
}

// survival reads live HP, food and place and decides.
func (b *fisherBot) survival(ctx *Context) (fisherplan.SurvivalAction, fisherplan.SurvivalInput) {
	st := ctx.Session.State()
	hp, hpOK := st.HP()
	maxHP, maxOK := st.MaxHP()
	in := fisherplan.SurvivalInput{HP: hp, MaxHP: maxHP, Known: hpOK && maxOK, InCombat: st.InCombat(), Resting: b.resting}
	in.Food = b.surv.FoodHeld(fisherrunner.EdibleFood(&fisherrunner.Deps{Session: ctx.Session, Config: ctx.Config}))
	if x, z, level, ok := st.Tile(); ok {
		here := tile.Tile{X: x, Z: z, Level: level}
		in.InCatherby = fisherplan.InCatherby(here) || (b.guild.active && fisherplan.InFishingGuild(here))
		in.AtBank = tile.Chebyshev(here, b.layout.Bank) <= bankRefugeRadius
	}
	return fisherplan.DecideSurvival(b.surv, in), in
}

// survivalDue reports whether a wait should end so the survival step can act.
func (b *fisherBot) survivalDue(ctx *Context) bool {
	if !b.surv.Enabled {
		return false
	}
	a, _ := b.survival(ctx)
	if a.Eat && time.Now().Before(b.eatRetryAt) {
		a.Eat = false
	}
	return a.Interrupts()
}

// survivalStep eats, flees or rests when fisherplan.DecideSurvival says so, and reports whether it
// took this Loop call.
func (b *fisherBot) survivalStep(ctx *Context) bool {
	if !b.surv.Enabled {
		return false
	}
	a, in := b.survival(ctx)
	b.resting = a.Rest
	if a.Flee || a.Rest {
		ctx.SetPhase(phaselog.Recovering)
	}
	ate := false
	if a.Eat && !time.Now().Before(b.eatRetryAt) {
		ate = true
		b.eat(ctx, in)
	}
	if a.Flee {
		b.flees++
		why := "no food left"
		if in.InCombat {
			why = "under attack"
		}
		ctx.logf("fisher: HP %d/%d, %s — fleeing to the Catherby bank (flee %d)", in.HP, in.MaxHP, why, b.flees)
		b.walkToRefuge(ctx)
		return true
	}
	if a.Rest {
		return b.restStep(ctx, in)
	}
	return ate
}

// eat takes one bite of the food fisherplan.Survival.PickFood chooses.
func (b *fisherBot) eat(ctx *Context, in fisherplan.SurvivalInput) bool {
	d := b.rawDeps(ctx)
	name, ok := b.surv.PickFood(fisherrunner.EdibleFood(d), in.HP, in.MaxHP)
	if !ok {
		b.eatRetryAt = time.Now().Add(3 * time.Second)
		return false
	}
	if fisherrunner.EatFood(d, name) {
		b.bitesEaten++
		hp, _ := ctx.Session.State().HP()
		ctx.logf("fisher: ate %s at HP %d/%d, now %d (%d food left)", strings.ToLower(name), in.HP, in.MaxHP, hp, in.Food-1)
		b.eatRetryAt = time.Now().Add(1200 * time.Millisecond)
		return true
	}
	ctx.logf("fisher: could not eat %s at HP %d/%d — trying again shortly", strings.ToLower(name), in.HP, in.MaxHP)
	b.eatRetryAt = time.Now().Add(3 * time.Second)
	ctx.Delay(600 * time.Millisecond)
	return false
}

// walkToRefuge runs to the Catherby bank, eating on the way whenever a bite is due: eating leaves
// the walk queue alone, so the two do not fight.
//
// Why it runs: a walking account cannot outpace a chasing White wolf (live, a foodless account
// walking to the bank died two tiles into the flee). Running was kept off in fisherrunner for the
// position drift that the posdrift fix e4e59a6 closed, and webwalk's combat handling already turns
// run on when attacked. Run is turned back off at the bank when this turned it on.
func (b *fisherBot) walkToRefuge(ctx *Context) {
	st := ctx.Session.State()
	if !st.RunEnabled() {
		if err := actions.SetRun(ctx.Session, true); err != nil {
			ctx.logf("fisher: could not turn run on to flee: %v", err)
		} else {
			defer func() {
				if x, z, level, ok := st.Tile(); ok && tile.Chebyshev(tile.Tile{X: x, Z: z, Level: level}, b.layout.Bank) <= bankRefugeRadius {
					_ = actions.SetRun(ctx.Session, false)
				}
			}()
		}
	}
	d := b.rawDeps(ctx)
	d.Delay = func(dur time.Duration) bool {
		if a, in := b.survival(ctx); a.Eat && !time.Now().Before(b.eatRetryAt) {
			b.eat(ctx, in)
		}
		return ctx.Delay(dur)
	}
	if !fisherrunner.WalkNear(d, b.layout.Bank, bankRefugeRadius-2) {
		ctx.logf("fisher: did not reach the Catherby bank while fleeing — trying again")
		ctx.Delay(600 * time.Millisecond)
	}
}

// restStep holds a foodless, hurt account at the bank: it restocks when the bank may hold food, and
// otherwise waits for HP to come back to fisherplan.ResumeAt.
func (b *fisherBot) restStep(ctx *Context, in fisherplan.SurvivalInput) bool {
	if !in.AtBank {
		b.walkToRefuge(ctx)
		return true
	}
	if b.surv.FoodCount > 0 && (b.bankRead == nil || b.bankRead.Food > 0) && !time.Now().Before(b.restockRetryAt) {
		b.restockRetryAt = time.Now().Add(time.Minute)
		d := b.rawDeps(ctx)
		if !fisherrunner.WaitForGuardian(d) {
			return true
		}
		ctx.logf("fisher: HP %d/%d with no food — restocking at the bank", in.HP, in.MaxHP)
		if err := b.bankStep(ctx, d, b.snapshot(ctx, d)); err != nil {
			ctx.logf("fisher: restock: %v", err)
		}
		return true
	}
	if time.Since(b.lastRestLog) >= 30*time.Second {
		b.lastRestLog = time.Now()
		ctx.logf("fisher: resting at the bank with no food — HP %d/%d, resuming at %d", in.HP, in.MaxHP, fisherplan.ResumeAt(in.MaxHP))
	}
	ctx.Delay(3 * time.Second)
	return true
}

// withdrawFood tops the pack's food up to foodCount from an open bank.
func (b *fisherBot) withdrawFood(ctx *Context, d *fisherrunner.Deps, bank *actions.Bank) {
	if !b.surv.Enabled || b.surv.FoodCount == 0 {
		return
	}
	held := b.surv.FoodHeld(fisherrunner.EdibleFood(d))
	maxHP, _ := ctx.Session.State().MaxHP()
	lines := b.surv.FoodToWithdraw(fisherrunner.BankStock(d, bank), held, maxHP)
	if len(lines) == 0 {
		return
	}
	if err := fisherrunner.SetNoteMode(d, bank, false); err != nil {
		ctx.logf("fisher: could not switch the bank to items for food: %v", err)
	}
	var drew []string
	for _, line := range lines {
		if err := fisherrunner.WithdrawXByName(d, bank, line.Name, line.Count); err != nil {
			ctx.logf("fisher: withdrawing %d %s: %v", line.Count, line.Name, err)
			continue
		}
		drew = append(drew, fmt.Sprintf("%d %s", line.Count, strings.ToLower(line.Name)))
	}
	if len(drew) > 0 {
		ctx.logf("fisher: withdrew %s to eat", strings.Join(drew, ", "))
	}
}

// muleAtJitterPercent is how far either side of the configured muleAt an account's own
// threshold may sit: 1000 becomes somewhere in 700..1300.
const muleAtJitterPercent = 30

// jitterMuleAt spreads hand-off thresholds across a cohort so donors do not all reach
// "time to hand over" at once.
//
// Measured live 2026-09-19 on an 87-account cohort sharing one mule: 80 distinct donors
// asked inside 25 minutes against a mule completing 1.43 trades/min at 80% busy, the median
// gap between banking one trade and the next request landing was 3s (a donor was essentially
// always already queued), and 45 donors gave up waiting. Every account in that roster
// carries the identical muleAt (MuleFishAt), so they converge on the mule together by construction
// -- the fleet's own configuration is part of why the queue is bursty rather than smooth.
//
// This does NOT close a demand/supply ratio, and is not pretending to: when demand genuinely
// exceeds one mule's throughput, spreading arrivals only smooths the queue. It is worth doing
// anyway because the accumulated first hand-off after a fleet restart IS a one-off backlog --
// every account in that cohort reported "0 row(s) left for another load", i.e. its whole bank
// fitted in a single trade -- and staggering thresholds stops the backlog re-forming as a
// synchronised wave each time.
//
// Derived from the account name, NOT rand: the threshold must be stable across restarts. A
// value that re-rolled on every launch would let an account sitting just under its own
// threshold flip to just over it (and back) between runs, handing off far more often than
// intended. FNV-1a is used only to spread names evenly; nothing here is security-sensitive.
func jitterMuleAt(base int, username string) int {
	if base <= 0 || username == "" {
		return base
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(username)))
	// Map the hash onto [-muleAtJitterPercent, +muleAtJitterPercent].
	span := 2*muleAtJitterPercent + 1
	offset := int(h.Sum32()%uint32(span)) - muleAtJitterPercent
	out := base + base*offset/100
	if out < 1 {
		return 1
	}
	return out
}

// fisherPhaseName maps a fisherplan phase to the time split's phase names (internal/phaselog):
// the few categories an operator reads XP/h losses from, not fisherplan's full list.
func fisherPhaseName(p fisherplan.FishPhase) string {
	switch p {
	case fisherplan.PhaseFish:
		return phaselog.Fishing
	case fisherplan.PhaseBank:
		return phaselog.Banking
	case fisherplan.PhaseBuy, fisherplan.PhaseSell:
		return phaselog.Shopping
	case fisherplan.PhaseTravel:
		return phaselog.Walking
	case fisherplan.PhaseFund:
		// Earning boat fare or a tool: the account is displaced or broke, not fishing.
		return phaselog.Recovering
	case fisherplan.PhaseStuck:
		return phaselog.Stranded
	case fisherplan.PhaseWait:
		return phaselog.Idle
	default: // accept, tutorial, brightness, starter store
		return phaselog.Setup
	}
}

// fisherSubPhase is fisherrunner.Deps.Phase: a walk inside fishing, banking or shopping reports
// as walking, so a long bank trip shows up as the walk it mostly is. A walk inside a hand-off,
// recovery or travel already reports as that step. Waiting on the guardian is always "event".
func fisherSubPhase(ctx *Context, p string) func() {
	if p == phaselog.Walking {
		switch cur, _ := ctx.Phase(); cur {
		case phaselog.Fishing, phaselog.Banking, phaselog.Shopping:
		default:
			return func() {}
		}
	}
	return ctx.PushPhase(p)
}
