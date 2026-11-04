package bot

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/crafterplan"
	"rsb0t-go/internal/scripts/crafterrunner"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/mulereceiverplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
	"rsb0t-go/internal/webwalk/live"
)

func init() {
	Register("ProgressiveCrafter", func() Bot { return &crafterBot{} })
}

// crafterBot ports ProgressiveCrafter.ts's loop(): every Loop call re-derives the phase from live
// state (crafterplan.DecidePhase, freshStartPlan.ts's decidePhase over the Seers road) and does one
// step of it, so a stop, a relog or a death anywhere resumes in the right place. The phases:
//
//   - accept, tutorial: the appearance screen and the tutorial skip (freshstartrunner).
//   - bright: the brightness turned up to very bright, before the first store trip.
//   - drop: balls of wool (worthless at the store) dropped, and past crafting 10 raw wool too.
//   - store: the starter kit sold at the Lumbridge general store and shears bought, or with no coins
//     the free pair fetched from Fred's farm.
//   - wool: sheep shorn in the Lumbridge pen and the wool spun upstairs in the castle, to crafting 10.
//   - fund: Men pickpocketed for the boat fares and the skirt, resting below restBelowHp.
//   - skirt: a pink skirt bought from Thessalia in Varrock and worn.
//   - travel: Port Sarim -> Karamja -> Brimhaven -> Ardougne, and on to the Seers bank.
//   - arrived: the Seers flax loop — pick (or draw from the bank), climb to the wheel, spin bow
//     strings, bank — and, with a mule named, the hand-over once muleAt bow strings are banked.
//
// The hand-over draws the bow strings and everything else banked beyond muleKeep as notes, trades
// them to the mule at the Seers bank stand through donorrunner, and puts an unfinished load back.
//
// flaxSource "Mule, then pick" (a user request: flax from the mule, strung into bow strings):
// the bank is drawn first as with "Bank first, then pick"; once a visit counts no flax, the next
// step walks to the mule's post (its heartbeat's), asks for a kit of flaxKitSize flax as notes
// (scarabrunner.RequestKitBundle, the kit ask every other script uses) and banks it there. A kit
// that brings nothing backs off (flaxKitBackoffMinutes, doubling) and the loop picks meanwhile.
// In this mode the bow string hand-over meets the mule at its post as well, and an account
// standing at the post counts as the Seers loop (crafterplan.DecidePhaseWithMule), so a relog
// mid-trip walks back rather than taking the boat road. ROLLOUT GATES: the mule must list this
// account in allowedPartners, and its hand-out caps apply (open stock: anyMaxPerOrder per kit,
// anyPerWindow per window, handoutPerDonor kits per donor per window).
// The control panel's manual trigger (Context.RequestHandoff) and muleAt override work as they do
// for ProgressiveFisher.
//
// Differences from the TS, each on purpose:
//   - A store pass that could not buy shears (no coins, or none in stock) goes straight to the farm
//     shears on the next passes. The TS walks back to the store on every pass and then out to the
//     farm again, so an account with no coins never settles.
//   - The store's sellable count leaves out names the store already refused this run, as the fisher
//     does, so a pack of refused junk goes to the wool step's make-room drop instead of back to the
//     store.
//   - Shorn sheep are told apart by NPC type (42 shorn, 43 woolly), not only by the skip list.
//   - No phase is chosen until the crafting level and the tile have arrived, so a relogged Seers
//     account is never sent to the pen by a crafting level that reads 0.
//   - A trade someone else opens is left alone; the donor's own trades run inside donorrunner.
//   - TS LoopingBot's recoveryAnchor and the HUD/tally feeds have no Go runtime equivalent.
//
// Settings (the TS manifest's keys, plus the Go-only test and pacing knobs):
//
//	guideNpc, skipOption, fare, dropAt, pickpocketTarget, pickpocketTile, restBelowHp, skirtCost,
//	storeStand, penTile, wheelTile, flaxSource ("Pick it" | "Bank first, then pick" |
//	"Mule, then pick"), flaxKitSize (1000, 28-10000), flaxKitBackoffMinutes (15, 1-120), mule
//	(default mulecon; an explicit "" never hands over), muleAt (5000), muleKeep ("Coins, Shears"),
//	logDetail — see docs/bot_script_settings.md.
//	cheatSetup     bool default true — ::setstat crafting + ::tele to the flax field. LOCAL-ENGINE-ONLY.
//	craftingLevel  int  default 10   — the level cheatSetup sets.
//	pickBatch      int  default 30   — flax one pick step gathers before re-deciding.
//	stepTimeoutSec int  default 300  — give up one pick or spin step after this long.
//	maxCycles      int  default 0    — stop after this many bow-string bank trips; 0 runs until stopped.
//	spinManip      bool default true — spin the flax through the wheel's Spin menu, a batch of one flax every two ticks, instead of one
//	                                   use-on-wheel at a time (crafterplan/spinmanip.go); it measures itself and falls back by itself.
type crafterBot struct {
	settings      crafterplan.Settings
	craftingLevel int
	pickBatch     int
	stepTimeout   time.Duration
	cyclesDone    int
	maxCycles     int

	phase     freshstart.Phase
	havePhase bool

	thief     *freshstartrunner.Thief
	sail      *freshstartrunner.SailProgress
	bright    *freshstart.Brightness
	refused   map[string]bool
	sheepSkip map[int]time.Time
	// farmShears is set once the store could not sell shears, so the farm pair is fetched directly.
	farmShears bool

	// stringsInBank and flaxInBank are the Seers bank's counts at the last visit, nil until one.
	stringsInBank *int
	flaxInBank    *int

	handoffPending bool
	handoffNext    time.Time
	handoffs       int
	handedOver     int

	sheared, spun, dropped, drawn, soldGP int

	// flaxNext is when the next mule flax kit may be asked for; flaxFruitless counts kits in a row
	// that brought none; flaxKits and flaxFromMule are this run's totals.
	flaxNext      time.Time
	flaxFruitless int
	flaxKits      int
	flaxFromMule  int

	// walker walks the Lumbridge legs of the wool stage over webwalk's door and stair graph.
	walker *webwalk.Walker

	// spin is the account's flax-spinning manipulation guard (spinManip setting, crafterplan/spinmanip.go), kept for the run so
	// the measured plain baseline survives from pack to pack.
	spin *crafterplan.SpinGuard

	// stall is the run staller on every walk of the Seers loop (walkstall.go): the bank-to-wheel-and-back legs drain more run energy than
	// the account regains standing at the wheel, so without it the walks end up one tile a tick.
	stall *walkStall
}

func (b *crafterBot) deps(ctx *Context) *crafterrunner.Deps {
	return &crafterrunner.Deps{
		Session:    ctx.Session,
		Pack:       ctx.Pack,
		Locs:       ctx.Locs,
		Config:     ctx.Config,
		Components: ctx.Components,
		Chat:       ctx.Chat,
		Delay:      ctx.Delay,
		Log:        ctx.Log,
		Walker:     b.walker,
		SpinGuard:  b.spin,
		KeepExtra:  b.stall.keepNames(),
		AtBank:     b.drawRubAtBank(ctx),
	}
}

// crafterSettings reads ProgressiveCrafter's settings from a roster's settings object, with the TS
// manifest's defaults. As with the fisher, an explicit blank mule (or muleKeep) is kept rather than
// replaced by the default: blank mule means never hand anything over.
func crafterSettings(m map[string]interface{}) crafterplan.Settings {
	d := crafterplan.DefaultSettings()
	s := d
	s.GuideNPC = StringSetting(m, "guideNpc", d.GuideNPC)
	s.SkipOption = StringSetting(m, "skipOption", d.SkipOption)
	s.Fare = IntSetting(m, "fare", d.Fare)
	s.DropAt = IntSetting(m, "dropAt", d.DropAt)
	s.PickpocketTarget = StringSetting(m, "pickpocketTarget", d.PickpocketTarget)
	s.PickpocketTile = fisherTileSetting(m, "pickpocketTile", d.PickpocketTile)
	s.RestBelowHP = IntSetting(m, "restBelowHp", d.RestBelowHP)
	s.SkirtCost = IntSetting(m, "skirtCost", d.SkirtCost)
	s.StoreStand = fisherTileSetting(m, "storeStand", d.StoreStand)
	s.PenTile = fisherTileSetting(m, "penTile", d.PenTile)
	s.WheelTile = fisherTileSetting(m, "wheelTile", d.WheelTile)
	s.FlaxSource = StringSetting(m, "flaxSource", d.FlaxSource)
	s.FlaxKitSize = IntSetting(m, "flaxKitSize", d.FlaxKitSize)
	s.FlaxKitBackoffMinutes = IntSetting(m, "flaxKitBackoffMinutes", d.FlaxKitBackoffMinutes)
	if v, ok := m["mule"].(string); ok {
		s.Mule = strings.TrimSpace(v)
	}
	s.MuleAt = IntSetting(m, "muleAt", d.MuleAt)
	if v, ok := m["muleKeep"].(string); ok {
		s.MuleKeep = v
	}
	s.LogDetail = StringSetting(m, "logDetail", d.LogDetail)
	return s.Normalised()
}

func (b *crafterBot) OnStart(ctx *Context) error {
	if ctx.Pack == nil {
		return fmt.Errorf("crafter: no collision pack configured (fleet.config.json needs a packPath) — refusing to start rather than panic later inside pathfinding")
	}
	if ctx.Locs == nil {
		return fmt.Errorf("crafter: no loc index configured (fleet.config.json needs a locsPath) — refusing to start rather than panic later looking up the field/ladder/wheel/bank")
	}
	if ctx.Components == nil {
		return fmt.Errorf("crafter: no interface archive Components configured — refusing to start rather than panic later opening the bank")
	}

	b.settings = crafterSettings(ctx.Settings)
	b.craftingLevel = IntSetting(ctx.Settings, "craftingLevel", crafterplan.BowStringLevel)
	b.pickBatch = IntSetting(ctx.Settings, "pickBatch", crafterplan.PickBatch)
	b.stepTimeout = time.Duration(IntSetting(ctx.Settings, "stepTimeoutSec", 300)) * time.Second
	b.maxCycles = IntSetting(ctx.Settings, "maxCycles", 0)
	b.spin = crafterplan.NewSpinGuard(BoolSetting(ctx.Settings, "spinManip", true))
	b.thief = freshstartrunner.NewThief()
	b.sail = freshstartrunner.NewSailProgress()
	b.bright = &freshstart.Brightness{}
	b.refused = map[string]bool{}
	b.sheepSkip = map[int]time.Time{}
	b.havePhase = false
	if w, err := live.NewWalker(live.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
	}); err != nil {
		ctx.logf("crafter: no webwalk (%v) — the Lumbridge legs use the plain walker", err)
	} else {
		b.walker = w
	}
	b.stall = newWalkStall(ctx.Settings, "crafter")
	b.stall.install(ctx, b.walker)

	if BoolSetting(ctx.Settings, "cheatSetup", true) {
		if err := crafterrunner.CheatSetup(b.deps(ctx), b.craftingLevel); err != nil {
			return fmt.Errorf("crafter: cheat setup: %w", err)
		}
	}
	mule := "no mule"
	if b.settings.Mule != "" {
		mule = fmt.Sprintf("bow strings to %s at %d banked (keep %q)", b.settings.Mule, b.settings.MuleAt, b.settings.MuleKeep)
	}
	ctx.logf("crafter: fare %d, flax source %q, %s", b.settings.Fare, b.settings.FlaxSource, mule)
	if b.stall.on() {
		ctx.logf("crafter: run staller is ON - a carried Games necklace or Ring of dueling is rubbed every 3rd tick of a walk to save run energy (walkRub=false turns it off); the walks turn running on at 20%% energy either way")
	}
	if b.spin.On {
		ctx.logf("crafter: spin manipulation is ON - the wheel's Spin menu spins one flax every 2 ticks instead of one use every 3-4, measured against the plain use (spinManip=false turns it off)")
	}
	if crafterplan.AsksMuleForFlax(b.settings.FlaxSource) {
		if b.settings.Mule == "" {
			ctx.logf("crafter: flax source %q with no mule named — the bank is drawn, then flax is picked", b.settings.FlaxSource)
		} else {
			ctx.logf("crafter: flax from %s in kits of %d, backing off %dm (doubling) when a kit brings none; %s must list this account in allowedPartners",
				b.settings.Mule, b.settings.FlaxKitSize, b.settings.FlaxKitBackoffMinutes, b.settings.Mule)
		}
	}
	return nil
}

const craftingStat = crafterrunner.CraftingStat

// snapshot builds freshstart.Snapshot from the live session, ported from ProgressiveCrafter.ts's
// snapshot(). ok is false until the crafting level and the tile are known.
func (b *crafterBot) snapshot(ctx *Context, d *crafterrunner.Deps) (freshstart.Snapshot, bool) {
	st := ctx.Session.State()
	skill, known := st.Skill(craftingStat)
	x, z, level, tileOK := st.Tile()
	if !known || !tileOK || skill.Level <= 0 {
		return freshstart.Snapshot{}, false
	}
	main, _, _ := st.Modals()
	here := tile.Tile{X: x, Z: z, Level: level}
	eq := actions.NewEquipment(ctx.Session, ctx.Components)
	s := freshstart.Snapshot{
		MainModal:     main,
		Tile:          &here,
		Crafting:      skill.Level,
		Coins:         crafterrunner.CoinsHeld(d),
		HasShears:     crafterrunner.Count(d, freshstart.Shears) > 0,
		Wool:          crafterrunner.Count(d, freshstart.Wool),
		Balls:         crafterrunner.Count(d, freshstart.Ball),
		Sellable:      len(freshstart.SellQueue(crafterrunner.PackView(d), crafterplan.StoreKeep(), b.refused)),
		PackFull:      crafterrunner.PackFull(d),
		Fare:          b.settings.Fare,
		DropAt:        b.settings.DropAt,
		SkirtWorn:     eq.ContainsWorn(ctx.Config, freshstart.PinkSkirt),
		SkirtHeld:     crafterrunner.Count(d, freshstart.PinkSkirt) > 0,
		SkirtCost:     b.settings.SkirtCost,
		BrightPending: freshstartrunner.BrightPending(crafterrunner.Fresh(d), b.bright),
	}
	return s, true
}

func (b *crafterBot) Loop(ctx *Context) error {
	d := b.deps(ctx)
	if !crafterrunner.WaitForGuardian(d) {
		return nil
	}
	if actions.NewTrade(ctx.Session).Active() {
		ctx.Delay(2 * time.Second)
		return nil
	}
	s, ok := b.snapshot(ctx, d)
	if !ok {
		ctx.Delay(1200 * time.Millisecond)
		return nil
	}
	phase := crafterplan.DecidePhase(s)
	if phase != freshstart.PhaseArrived && b.mulePostMatters() {
		// Only read the bus when the answer can change the phase (crafterplan.DecidePhaseWithMule).
		phase = crafterplan.DecidePhaseWithMule(s, b.settings.FlaxSource, b.mulePost(ctx))
	}
	if !b.havePhase || phase != b.phase {
		b.phase, b.havePhase = phase, true
		ctx.logf("phase: %s", crafterplan.DescribePhase(phase))
	}
	// Level-ups and the like; the tutorial's own dialogue is the tutorial step's to answer.
	if phase != freshstart.PhaseAccept && phase != freshstart.PhaseTutorial {
		if dlg := actions.NewChatDialog(ctx.Session, ctx.Components); dlg.CanContinue() {
			_ = dlg.Continue()
			ctx.Delay(600 * time.Millisecond)
			return nil
		}
	}
	if ctx.TakeHandoffRequest() {
		b.handoffPending = true
	}
	if b.handoffPending {
		return b.handoffStep(ctx, d)
	}

	// Every phase but shearing works outside the sheep pen, whose gate the collision pack bakes shut.
	wantsPen := phase == freshstart.PhaseWool && crafterplan.DecideWool(s.Wool, s.PackFull) == crafterplan.WoolShear
	if !wantsPen && phase != freshstart.PhaseDrop && crafterplan.Lumbridge().InPen(*s.Tile) {
		if !crafterrunner.LeavePen(d) {
			ctx.Delay(3 * time.Second)
		}
		return nil
	}

	fd := crafterrunner.Fresh(d)
	switch phase {
	case freshstart.PhaseAccept:
		freshstartrunner.AcceptAppearance(fd)
	case freshstart.PhaseTutorial:
		freshstartrunner.SkipTutorial(fd, b.settings.GuideNPC, b.settings.SkipOption)
	case freshstart.PhaseBright:
		freshstartrunner.BrightnessStep(fd, b.bright)
	case freshstart.PhaseDrop:
		b.dropStep(ctx, d, s)
	case freshstart.PhaseStore:
		b.storeStep(ctx, d, s)
	case freshstart.PhaseWool:
		b.woolStep(ctx, d, s)
	case freshstart.PhaseFund:
		freshstartrunner.PickpocketStep(fd, b.thief, freshstartrunner.PickpocketOpts{
			Target: b.settings.PickpocketTarget, Spot: b.settings.PickpocketTile,
			RestBelow: b.settings.RestBelow(), Need: freshstart.FundTarget(s),
		})
	case freshstart.PhaseSkirt:
		// The long walk to Varrock goes through webwalk (doors, gates) when there is no skirt to wear
		// yet. Found live: an account that pickpocketed its way into Lumbridge castle's hall could not
		// route out with BuySkirtStep's same-level walker.
		if !s.SkirtHeld && b.walker != nil && tile.Chebyshev(*s.Tile, freshstart.ThessaliaStand) > 3 {
			if !crafterrunner.Walk(d, freshstart.ThessaliaStand, 2, 10*time.Minute) {
				ctx.Delay(3 * time.Second)
			}
			return nil
		}
		freshstartrunner.BuySkirtStep(fd)
	case freshstart.PhaseTravel:
		freshstartrunner.SailLeg(fd, crafterplan.Seers().Bank, b.sail)
	case freshstart.PhaseArrived:
		return b.seersStep(ctx, d)
	}
	return nil
}

// ---- wool ----

func (b *crafterBot) dropStep(ctx *Context, d *crafterrunner.Deps, s freshstart.Snapshot) {
	n := crafterrunner.Drop(d, crafterplan.DropNames(s.Crafting))
	b.dropped += n
	if n == 0 {
		ctx.logf("crafter: the balls would not drop — retrying")
		ctx.Delay(1200 * time.Millisecond)
		return
	}
	ctx.logf("dropped %d slots of wool", n)
}

func (b *crafterBot) storeStep(ctx *Context, d *crafterrunner.Deps, s freshstart.Snapshot) {
	want := crafterplan.WantsShears(s.HasShears, s.Crafting)
	if want && b.farmShears {
		if crafterrunner.GrabGroundShears(d) {
			b.farmShears = false
		}
		return
	}
	res := crafterrunner.StoreTrip(d, b.settings.StoreStand, crafterplan.StoreKeep(), b.refused, want)
	if !res.Opened {
		return
	}
	b.soldGP += res.GP
	ctx.logf("store trip: %d items sold for %d gp, %d coins", res.Sold, res.GP, crafterrunner.CoinsHeld(d))
	if res.BoughtShears {
		ctx.logf("bought %s (%d coins left)", freshstart.Shears, crafterrunner.CoinsHeld(d))
		return
	}
	if want {
		ctx.logf("crafter: the store sold no shears (no coins, or none in stock) — fetching the free pair from the farm instead")
		b.farmShears = true
	}
}

func (b *crafterBot) woolStep(ctx *Context, d *crafterrunner.Deps, s freshstart.Snapshot) {
	switch crafterplan.DecideWool(s.Wool, s.PackFull) {
	case crafterplan.WoolSpin:
		n := crafterrunner.SpinWool(d, b.settings.WheelTile)
		b.spun += n
		lootSpun(ctx, false, n)
		if n == 0 {
			ctx.Delay(2 * time.Second)
			return
		}
		ctx.logf("spun %d wool (%d this run)", n, b.spun)
	case crafterplan.WoolMakeRoom:
		ctx.logf("crafter: the pack is full of things the store will not take — dropping one to keep shearing")
		if !crafterrunner.DropJunk(d) {
			ctx.Delay(2 * time.Second)
		}
	default:
		pen := crafterplan.Lumbridge()
		if !pen.InPen(*s.Tile) {
			if !crafterrunner.GoToPen(d) {
				ctx.Delay(3 * time.Second)
			}
			return
		}
		switch crafterrunner.Shear(d, b.sheepSkip) {
		case crafterrunner.ShearWool:
			b.sheared++
		case crafterrunner.ShearNoSheep:
			if tile.Chebyshev(*s.Tile, b.settings.PenTile) > crafterplan.ArriveRadius {
				crafterrunner.Walk(d, b.settings.PenTile, 3, time.Minute)
				return
			}
			ctx.logf("crafter: every sheep in range is freshly shorn — waiting")
			ctx.Delay(3 * time.Second)
		case crafterrunner.ShearNoShears:
			ctx.Delay(time.Second)
		}
	}
}

// ---- Seers ----

// mulePostMatters reports whether the mule's post is part of this run's loop: flax comes from the
// mule and a mule is named.
func (b *crafterBot) mulePostMatters() bool {
	return crafterplan.AsksMuleForFlax(b.settings.FlaxSource) && b.settings.Mule != ""
}

// mulePost is where the mule stands: its heartbeat's post, else the fixed Catherby post.
func (b *crafterBot) mulePost(ctx *Context) tile.Tile {
	return scarabMulePresenceOf(ctx, b.settings.Mule).Post
}

func (b *crafterBot) setCounts(c crafterrunner.BankCounts) {
	strings, flax := c.Strings, c.Flax
	b.stringsInBank, b.flaxInBank = &strings, &flax
}

func (b *crafterBot) seersStep(ctx *Context, d *crafterrunner.Deps) error {
	view := crafterplan.SeersView{
		Pack:       crafterrunner.PackView(d),
		PackFull:   crafterrunner.PackFull(d),
		FlaxSource: b.settings.FlaxSource,
		FlaxInBank: b.flaxInBank,
		NotedFlax:  crafterrunner.NotedFlaxCount(d),
		MuleFlaxDue: b.mulePostMatters() &&
			crafterplan.MuleFlaxDue(b.settings.FlaxSource, time.Now(), b.flaxNext),
	}
	action := crafterplan.DecideSeers(view)
	ctx.logf("Seers step: %s", action)

	switch action {
	case crafterplan.SeersBankAll:
		deposited, counts, ok := crafterrunner.BankAllCounts(d, crafterplan.SeersKeep())
		if !ok {
			// A real delay, never an instant retry: a bank that cannot be reached otherwise spins.
			ctx.logf("crafter: could not reach or open the Seers bank — waiting before retrying")
			ctx.Delay(15 * time.Second)
			return nil
		}
		b.setCounts(counts)
		ctx.logf("banked everything at Seers (%d items)", deposited)

	case crafterplan.SeersSpin:
		spinBefore := crafterrunner.BowStringCount(d)
		spun := crafterrunner.SpinFlaxUntil(d, spinBefore+b.pickBatch, b.stepTimeout)
		lootSpun(ctx, true, spun)
		ctx.logf("spin step done: %d flax spun into bow strings", spun)
		if spun <= 0 {
			ctx.logf("crafter: could not spin any flax this attempt — waiting before retrying")
			ctx.Delay(10 * time.Second)
		}

	case crafterplan.SeersBankStrings:
		held := crafterrunner.BowStringCount(d)
		settings := b.settings
		if v, ok := ctx.MuleAtOverride(); ok {
			settings.MuleAt = v
		}
		// THE FLAX COMES OUT IN THE SAME VISIT (twopertick-1012). With the flax drawn from the bank, the pack is empty
		// after this deposit, so the very next pass was a second visit to the same booth: close, click, open wait,
		// deposit pass, draw. When the bank holds flax and no hand-over is due (it is tested on the strings the bank
		// WILL hold after this deposit), the Withdraw-All rides in the same burst as the Deposit-Alls.
		drawNow := func(opened crafterrunner.BankCounts) bool {
			return crafterplan.DrawFlaxWithStrings(settings, opened.Flax, opened.Strings, held, time.Now(), b.handoffNext, b.cyclesDone, b.maxCycles)
		}
		deposited, drew, counts, ok := crafterrunner.BankAllDrawFlax(d, crafterplan.SeersKeep(), drawNow)
		if !ok {
			ctx.logf("crafter: could not reach or open the Seers bank — waiting before retrying")
			ctx.Delay(15 * time.Second)
			return nil
		}
		b.setCounts(counts)
		ctx.logf("banked %d × %s (%d items deposited, %d in bank)", held, crafterplan.BowString, deposited, counts.Strings)
		if drew > 0 {
			b.drawn += drew
			ctx.logf("drew %d × %s from the bank in the same visit (%d left) — one bank visit a cycle instead of two", drew, crafterplan.Flax, counts.Flax)
		}
		b.cyclesDone++
		if crafterplan.HandoffAfterBanking(settings, b.stringsInBank, time.Now(), b.handoffNext) {
			ctx.logf("%d bow strings banked — handing the lot to %s", counts.Strings, settings.Mule)
			b.handoffPending = true
			return nil
		}
		if b.maxCycles > 0 && b.cyclesDone >= b.maxCycles {
			ctx.requestStop()
		}

	case crafterplan.SeersDrawFlax:
		got, counts, ok := crafterrunner.DrawFlax(d)
		if !ok {
			ctx.logf("crafter: could not reach or open the Seers bank — waiting before retrying")
			ctx.Delay(15 * time.Second)
			return nil
		}
		b.setCounts(counts)
		switch {
		case got > 0:
			b.drawn += got
			ctx.logf("drew %d × %s from the bank (%d left)", got, crafterplan.Flax, counts.Flax)
		case counts.Flax <= 0 && b.mulePostMatters():
			ctx.logf("the bank has no flax left — asking %s for more (or picking while that backs off)", b.settings.Mule)
		case counts.Flax <= 0:
			ctx.logf("the bank has no flax left — picking it again from here")
		default:
			// The count said there was flax and none arrived; say so rather than spin on the spot.
			ctx.logf("crafter: the bank would not hand over any flax — check the bank is not full of notes")
			ctx.Delay(10 * time.Second)
		}

	case crafterplan.SeersMuleFlax:
		b.muleFlaxStep(ctx, d)

	case crafterplan.SeersPick:
		before := crafterrunner.FlaxCount(d)
		got := crafterrunner.PickFlaxUntil(d, before+b.pickBatch, b.stepTimeout)
		ctx.logf("pick step done: %d flax in pack now", got)
		if got <= before {
			// Compared against the count before this call, not zero: an account holding some flax
			// already would otherwise spin at full CPU on a failed walk (production account, 2026-09-15).
			ctx.logf("crafter: could not reach the flax field this attempt — waiting before retrying")
			ctx.Delay(15 * time.Second)
		}
	}
	return nil
}

// ---- flax from the mule ----

// muleFlaxStep is one mule flax trip: check the mule is there and not swamped, walk to its post,
// ask for a kit of flax, bank it beside the post, and back off when nothing came.
func (b *crafterBot) muleFlaxStep(ctx *Context, d *crafterrunner.Deps) {
	mule := b.settings.Mule
	if ctx.Bus == nil || ctx.Username == "" {
		b.flaxKitFailed(ctx, crafterplan.FlaxKitDeferred, "no wall bus to ask the mule on")
		return
	}
	p := scarabMulePresenceOf(ctx, mule)
	if !p.Heard || !p.SameWorld {
		b.flaxKitFailed(ctx, crafterplan.FlaxKitDeferred, p.Why)
		return
	}
	if why, long := scarabMuleQueueTooLong(ctx, mule, donorrunner.DefaultKitTimeout); long {
		b.flaxKitFailed(ctx, crafterplan.FlaxKitDeferred, why)
		return
	}
	sd := &fleetrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Walker: b.walker,
		Log: ctx.Log, Loot: ctx.Loot, Me: ctx.Username, World: ctx.World(),
	}
	if !fleetrunner.WalkTo(sd, p.Post, 3, 5*time.Minute) {
		b.flaxKitFailed(ctx, crafterplan.FlaxKitDeferred, fmt.Sprintf("could not reach %s's post at %d,%d", mule, p.Post.X, p.Post.Z))
		return
	}
	b.flaxKits++
	ctx.logf("flax kit %d: asking %s for %d %s at %d,%d", b.flaxKits, mule, b.settings.FlaxKitSize, crafterplan.Flax, p.Post.X, p.Post.Z)
	kit := crafterrunner.FetchFlaxKit(sd, ctx.Bus, ctx.Username, mule, b.settings.FlaxKitSize, p.Post, ctx.CurrentWorld, donorrunner.DefaultKitTimeout, b.stall.keepNames()...)
	if kit.Stopped {
		return
	}
	switch {
	case kit.Banked:
		n := kit.BankFlax
		b.flaxInBank = &n
	case kit.Got > 0:
		b.flaxInBank = nil // notes still in the pack: the Seers bank banks and counts them
	}
	reason := kit.Declined
	if reason == "" && kit.Got <= 0 {
		reason = kit.Reason
	}
	out := crafterplan.ClassifyFlaxKit(max(kit.BankFlax, kit.Got), reason)
	if out != crafterplan.FlaxKitGained {
		b.flaxKitFailed(ctx, out, fmt.Sprintf("kit %s: %d arrived (%s)", out, kit.Got, reason))
		return
	}
	b.flaxFruitless = 0
	b.flaxFromMule += kit.Got
	ctx.logf("flax kit %d: %d %s from %s, bank holds %d — back to Seers to spin (%d from the mule this run)",
		b.flaxKits, kit.Got, crafterplan.Flax, mule, kit.BankFlax, b.flaxFromMule)
}

// flaxKitFailed backs the mule asks off; the loop picks flax until the back-off runs out.
func (b *crafterBot) flaxKitFailed(ctx *Context, out crafterplan.FlaxKitOutcome, why string) {
	wait, fruitless := crafterplan.AfterFlaxKit(out, b.flaxFruitless, time.Duration(b.settings.FlaxKitBackoffMinutes)*time.Minute)
	b.flaxFruitless = fruitless
	b.flaxNext = time.Now().Add(wait)
	ctx.logf("flax from %s not coming: %s — picking flax meanwhile, asking again in %s (%d fruitless in a row)",
		b.settings.Mule, why, wait, fruitless)
}

// ---- the mule ----

// handoffStep ports stepHandoff's load, wait and return: draw a load of notes at the Seers bank,
// trade it to the mule at the bank stand through donorrunner, and put an unfinished load back. With
// rows still in the bank after a traded load the next Loop call draws again, as loadHandoff goes round
// until the bank has none.
func (b *crafterBot) handoffStep(ctx *Context, d *crafterrunner.Deps) error {
	settings := b.settings
	if v, ok := ctx.MuleAtOverride(); ok {
		settings.MuleAt = v
	}
	donor := settings.Donor()
	if donor.Mule == "" {
		b.handoffPending = false
		return nil
	}
	muleKeep := crafterplan.MuleKeep(settings)
	packKeep := crafterplan.PackKeep(settings)
	// The run staller's jewellery stays in the bank (not drawn for the mule) and in the pack (not traded).
	muleKeep = append(muleKeep, b.stall.keepNames()...)
	packKeep = append(packKeep, b.stall.keepNames()...)
	load, ok := crafterrunner.LoadHandoff(d, muleKeep)
	if !ok {
		ctx.logf("crafter: hand-off: could not reach or open the Seers bank — will retry")
		ctx.Delay(5 * time.Second)
		return nil
	}
	b.setCounts(load.Counts)
	if load.RowsInBank == 0 {
		b.handoffPending = false
		ctx.logf("hand-over finished: %s", pluralDone(b.handedOver))
		return nil
	}
	if load.Drawn <= 0 {
		b.handoffPending = false
		b.handoffNext = time.Now().Add(donorplan.HandoffRetry)
		ctx.logf("hand-over paused: could not draw the hand-over from the bank — trying again in %s", donorplan.HandoffRetry)
		return nil
	}
	ctx.logf("drew %d slot(s) for %s (%d bank row(s), %d left for another load)", load.Drawn, donor.Mule, load.RowsInBank, load.RowsLeft)

	rd := &donorrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
		Bus: ctx.Bus, Me: ctx.Username,
	}
	// Reach a mule that is logged in to a different world (worldhopplan bounds it; a
	// one-world fleet no-ops). See bot.WireDonorWorlds.
	WireDonorWorlds(ctx, rd)
	stand := mulereceiverplan.SeersStand
	if b.mulePostMatters() {
		// Fed by the mule: it stands at its own post (Catherby for the mule), so meet it there. The
		// long leg goes through webwalk first; donorrunner's own walk is a plain same-level path.
		stand = crafterplan.HandoffStand(settings.FlaxSource, b.mulePost(ctx), mulereceiverplan.SeersStand)
		if !crafterrunner.Walk(d, stand, 3, 5*time.Minute) {
			ctx.logf("crafter: hand-off: the walk to %s's post at %d,%d did not arrive — trying donorrunner's own walk", donor.Mule, stand.X, stand.Z)
		}
	}
	res := donorrunner.Run(rd, donorrunner.Options{
		Mule: donor.Mule, Stand: stand,
		RowsInBank: load.RowsInBank, Drawn: load.Drawn,
		Load: func() []donorplan.LoadGroup {
			return crafterplan.MuleLoad(crafterrunner.PackSlotRows(d), donorplan.DefaultMuleFreeSlots, packKeep)
		},
		HaulLeft: func() int { return crafterplan.MuleHaul(crafterrunner.PackSlotRows(d), packKeep) },
	})
	switch {
	case res.Stopped:
		ctx.logf("crafter: hand-off interrupted by a stop request")
		return nil
	case res.Traded:
		b.handoffs++
		b.handedOver += res.Slots
		ctx.logf("handed %d slot(s) to %s in %d trade(s)", res.Slots, donor.Mule, res.Loads)
		if load.RowsLeft > 0 || crafterplan.MuleHaul(crafterrunner.PackSlotRows(d), packKeep) > 0 {
			return nil // handoffPending stays set: draw the next load.
		}
		b.handoffPending = false
		ctx.logf("hand-over finished: %s", pluralDone(b.handedOver))
		// At the mule's post with an empty pack: one more trade asks for the walks' jewellery, and costs no walk.
		b.stall.askMuleRub(ctx, donor.Mule, 0)
		return nil
	}
	ctx.logf("%s — putting the load back for now", res.Reason)
	if counts, ok := crafterrunner.ReturnHandoff(d); ok {
		b.setCounts(counts)
	}
	b.handoffPending = false
	b.handoffNext = time.Now().Add(donorplan.HandoffRetry)
	ctx.logf("hand-over paused: back to flax, asking again in %s", donorplan.HandoffRetry)
	return nil
}

func pluralDone(slots int) string {
	return fmt.Sprintf("%d slot(s) handed over this run", slots)
}

func (b *crafterBot) OnStop(ctx *Context) {
	ctx.logf("crafter: stopped — %d shorn, %d spun, %d dropped, %d pockets, %d gp sold, %d flax drawn, %d bow-string bank trips, %d slots to the mule in %d hand-over(s), %d flax from the mule in %d kit ask(s)",
		b.sheared, b.spun, b.dropped, b.thief.Pockets, b.soldGP, b.drawn, b.cyclesDone, b.handedOver, b.handoffs, b.flaxFromMule, b.flaxKits)
}

// drawRubAtBank is crafterBot's bank hook for the run staller's jewellery (see walkStall.drawRubAtBank).
func (b *crafterBot) drawRubAtBank(ctx *Context) func(bank *actions.Bank) {
	return b.stall.drawRubAtBank(ctx)
}
