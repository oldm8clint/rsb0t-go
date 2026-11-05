package bot

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/phaselog"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/wcfletchplan"
	"rsb0t-go/internal/scripts/wcfletchrunner"
	"rsb0t-go/internal/scripts/woodcutplan"
	"rsb0t-go/internal/scripts/woodcutrunner"
)

func init() {
	Register("ProgressiveWCFletch", func() Bot { return &wcfletchBot{} })
}

// wcfletchBot is ProgressiveWoodcutter with a knife.
//
// Users asked for two versions of the woodcutting script — "one that does no fletching
// at all and one that does fletching" — so ProgressiveWoodcutter is left EXACTLY as it is and
// this is a second registration. Three things differ and nothing else does:
//
//  1. THE TREE IS CHOSEN BY FLETCHING LEVEL. ProgressiveWoodcutter climbs on Woodcutting; this
//     stays on a tree until it can cut the NEXT tree's logs into a shortbow, capped by what the
//     account's Woodcutting can actually chop. wcfletchplan.TierIndex is that pair.
//  2. LOGS ARE CUT BEFORE THEY ARE BANKED. Every load becomes the best unstrung bow the
//     Fletching level allows (arrow shafts at the first rung), so the pack that reaches the bank
//     holds bows.
//  3. THE MULE RECEIVES BOWS. wcfletchplan.MuleRows puts the fletched stock on the trade screen
//     first, "ready for an account to string them at a high rate of fletch exp per hour".
//
// # Why this embeds woodcutterBot instead of copying it
//
// Everything that is not fletching — the appearance screen, the tutorial skip, the starter-kit
// sale, the pickpocketed fare, the two ship crossings, the Seers bank, the axe ladder and its
// market/Bob's fallbacks, the axe-head and broken-axe events, the Ent exclusions, the Draynor
// start camp, the webwalk long legs — is ProgressiveWoodcutter's, already proven live, and MUST
// stay identical: the owner is going to run half the live woodcutters on each script and compare
// them, and two scripts that walk or bank differently are not a comparison. Embedding is what
// makes that structurally true rather than a promise. It also means a change the woodcutter's
// owner makes to any of those paths reaches this script for free, and a change that breaks the
// coupling fails the BUILD rather than quietly diverging.
//
// The four methods overridden below (OnStart, Loop, OnStop, plus this script's own bank and chop
// steps) are the entire diff. Nothing in internal/bot/woodcutter*.go,
// internal/scripts/woodcutplan/ or internal/scripts/woodcutrunner/ is modified.
//
// # Settings
//
// Every ProgressiveWoodcutter key, read unchanged (so an account can be moved between the two
// scripts without editing its settings), plus:
//
//	fletch          bool   default true      — the master switch. OFF makes this behave exactly
//	                                            as ProgressiveWoodcutter does, which is the safe
//	                                            way to pull an account back out of the trial.
//	normalLogs      string default "shafts"  — what plain logs become: "shafts" (the owner's own
//	                                            instruction) or "bows". See wcfletchplan's
//	                                            NormalPolicy for what the default costs — content
//	                                            does offer a plain-log longbow, worth a third
//	                                            more experience per log than shafts.
//	treeGate        string default "startAt" — which Woodcutting requirement holds the tree
//	                                            choice: "startAt" (what ProgressiveWoodcutter
//	                                            uses, so the two chop in the same places) or
//	                                            "content" (the server's own minimum).
//	muleAtFletched  int    default 1000      — FLETCHED UNITS (unstrung bows plus arrow shafts)
//	                                            in the bank and pack together that start a
//	                                            hand-over. Deliberately its own key: muleAt
//	                                            counts LOGS, and one log makes one bow but
//	                                            fifteen shafts.
//	spreadRungOne   bool   default true      — send rung-1 (arrow-shaft) accounts to the least
//	                                            crowded of wcfletchplan.NormalPatches instead of
//	                                            all to the one Seers camp. OFF is the behaviour
//	                                            before 2026-09-23.
//	spreadMaple     bool   default true      — seat maple-rung accounts over
//	                                            wcfletchplan.MapleCamps by the supply model instead
//	                                            of all at the one Seers maple camp. OFF is the
//	                                            behaviour before 2026-09-25.
//	homeWorld       int    default 0         — the world this account lives on: the control
//	                                            panel logs it in there, and the script hops there
//	                                            itself when it finds itself elsewhere
//	                                            (homeworld.go). 0 = the roster's.
//	mapleOverflow   bool   default true      — let that board send an account to the willow
//	                                            camps (willows cut into willow longbows) once the
//	                                            maple camps are crowded. OFF keeps every maple-rung
//	                                            account on maples.
//	chopManip       bool   default true      — write a knife-on-log batch so every swing of a tree
//	                                            comes 3 ticks after the last instead of 4 (woodcutrunner's
//	                                            chopmanip.go, chopmanipplan). Needs a knife and a log
//	                                            to carry in the pack. It measures how late its batches
//	                                            reach the engine and writes them that many ticks early;
//	                                            it never pauses or switches itself off. OFF is the plain
//	                                            chop.
//	spreadYew       bool   default true      — seat yew-rung accounts over wcfletchplan.YewCamps
//	                                            (the ladder's church camp, the Catherby yews and
//	                                            the lone road yew) by the supply model, counting
//	                                            the players they see at each camp who are not this
//	                                            cohort's, instead of all at the church camp's three
//	                                            yews. Yews never overflow to another rung. When
//	                                            every camp on the world is crowded for ten minutes
//	                                            the account logs which world would pay better
//	                                            (it never hops: homeWorld does that). OFF is the
//	                                            behaviour before 2026-10-05.
//	spreadYewWorlds bool   default true      — deal the yew-rung accounts over the worlds in yewWorlds, so each
//	                                            world's eleven yews feed a share of the cohort (wcfletch_world.go,
//	                                            wcfletchplan/yewworlds.go): live, 23 accounts on world 1's yews
//	                                            took 125 logs an hour each where one with a tree to itself takes
//	                                            about 170. The assigned world becomes the account's home, the
//	                                            script hops there (the homeWorld machinery), and a hand-off to the
//	                                            mule on world 1 is a hop there and a hop back. An account that
//	                                            loses 15% of a quarter hour to waiting for a yew moves to a
//	                                            lighter world, at most once an hour. OFF keeps every account on
//	                                            the world the homeWorld setting puts it on. Needs spreadYew.
//	yewWorlds       string default "1,2,3"   — the worlds the yew cohort is dealt over. Fewer than two spreads
//	                                            nothing; a world the deployment does not list is refused by the
//	                                            fleet and the account stays where it is.
//	spreadMagic     bool   default true      — seat magic-rung accounts over wcfletchplan.MagicCamps (the ladder's three
//	                                            trees west of the Seers bank and the four in the Sorcerers' Tower garden,
//	                                            both banking at Seers) by the supply model, counting the strangers at each
//	                                            camp as the yew board does, instead of all at the ladder's three trees
//	                                            (wcfletchplan/magiccamps.go). OFF is the behaviour before 2026-10-13.
type wcfletchBot struct {
	// woodcutterBot carries every field and every step this script does not change. Its own
	// `settings` field is still read by every inherited step, so OnStart keeps it in sync with
	// cfg.Wood below; nothing here writes it afterwards.
	woodcutterBot

	// gate looks at the mule's queue before a load is drawn (drawgate.go).
	gate drawGate

	// oaks is the 2-tick oak rule (wcfletch_oaks.go): twoTickOaks, level-driven by default.
	oaks wcfletchOaks

	// home is the homeWorld setting's state (homeworld.go).
	home homeWorld

	// cfg is this script's own settings. Named cfg rather than `settings` deliberately: an
	// outer field of the same name would shadow the embedded one, and every inherited step reads
	// the embedded one, so the shadowing would be silent and wrong.
	cfg wcfletchplan.Settings

	// manip is the account's chop manipulation (woodcutrunner/chopmanip.go): one controller for the
	// life of the run, so the measured delay of the link survives from tree to tree. Nil with chopManip off.
	manip *woodcutrunner.ManipState

	// stall is the run staller on the account's walks (walkstall.go): a carried Games necklace or Ring of dueling is rubbed every third
	// tick of a run, so the yew camp to Seers bank walks cost a third of the run energy. walkFletch cuts the pack's logs while the account
	// walks to the bank (wcfletchrunner/walkfletch.go) instead of before it starts.
	stall      *walkStall
	walkFletch *wcfletchrunner.WalkFletch

	// carrierTriedAt is when the account last cut a plain-tree log to carry for the manipulation (carrierTrip): it
	// is tried at most every carrierRetry, so a camp with no plain tree in reach costs one look and not a loop.
	carrierTriedAt time.Time

	// rung is the ladder position last chosen, so a change is logged once rather than every pass.
	rung     int
	haveRung bool

	// fletchOff turns fletching off once no knife can be got at all, and the account then runs
	// exactly as ProgressiveWoodcutter does — chopping and banking logs, which is a perfectly
	// good use of it. wcfletchplan.TierIndex reads the same flag and reverts to woodcutplan's
	// own Woodcutting climb, so a knifeless account is not pinned to normal trees.
	//
	// IT IS NO LONGER LATCHED FOR THE RUN, changed 2026-09-23. It was, on the reasoning that the
	// fallback is a decision taken once and logged once. At 84 accounts against the world's FOUR
	// knife ground spawns that reasoning inverted: 37 of the 84 latched off, 35 of them on
	// "no knife on the ground at the spawn — another account has it", and the fleet owner found
	// them standing at willows with Fletching 1 (Acct196: Woodcutting 51, Fletching 1). A
	// contested shared spawn is a race an account loses for a few minutes, not a permanent
	// property of the account — and while latched it would not even pick a knife out of its own
	// bank, because the bank-knife step was gated on fletching() too.
	//
	// So: retried on knifeRetry, and cleared the moment a knife is actually in hand.
	fletchOff   bool
	fletchOffAt time.Time
	knifeTries  int

	// idle is true when the last chop found no tree. The fleet owner's own ask — "while accounts
	// are waiting for trees to respawn ... they can cut their logs" — and the live fleet's
	// woodcutters spend real time in exactly this state.
	idle bool

	// patch is where this account chops while rung 1 is spread over wcfletchplan.NormalPatches,
	// an index into the world's board; havePatch is false while it is not seated (any other
	// rung, fletching off, or the setting off), which means the ladder's own Seers camp.
	patch     int
	havePatch bool

	// camp is where this account chops while the maple rung is spread over
	// wcfletchplan.MapleCamps, an index into the world's board; haveCamp is false while it is not
	// seated (any other rung, or spreadMaple off), which means the ladder's own maple camp.
	camp     int
	haveCamp bool

	// yew is this account's side of the yew board while the yew rung is spread over
	// wcfletchplan.YewCamps (wcfletch_yew.go): its camp, and the clocks on counting the players
	// round it and on advising another world. magic is the same for the magic rung over
	// wcfletchplan.MagicCamps.
	yew   yewSeat
	magic yewSeat

	// bank is the bank of the camp the account is seated at (wcfletchplan.Camp.Bank), set every pass;
	// the zero value, and every rung without a board, is the Seers bank.
	bank wcfletchplan.CampBank

	// yewW is the world spread's state for this run (wcfletch_world.go), idleClock the seconds spent waiting for a tree in
	// the current quarter hour, idleSeen the chopped and fletched counters at its last report and stumpLogAt when the
	// "every yew is a stump" line was last said.
	yewW       yewWorldState
	idleClock  wcfletchplan.IdleClock
	idleSeen   struct{ chopped, fletched int }
	stumpLogAt time.Time

	// What the last bank visit left of the rung's logs, for wcfletchplan.BulkDrawDue: the count
	// read after the draw, the rung it was read for, and how many visits in a row have failed to
	// draw logs the bank still showed.
	bankRungLogs  int
	bankRungIdx   int
	bankRungKnown bool
	drawFails     int

	// Counters for the stop line.
	fletchXPAtStart int
	fletched        int
	logsCut         int
	fletchFails     int
	bankStock       int
	bankDraws       int
}

// wcfletchSettings reads this script's own four keys. The woodcutting half is read by the
// embedded OnStart from the identical keys ProgressiveWoodcutter uses.
func wcfletchSettings(m map[string]interface{}) wcfletchplan.Settings {
	d := wcfletchplan.DefaultSettings()
	s := d
	s.Fletch = BoolSetting(m, "fletch", d.Fletch)
	s.NormalLogs = wcfletchplan.NormalPolicy(StringSetting(m, "normalLogs", string(d.NormalLogs)))
	s.TreeGate = wcfletchplan.TreeGate(StringSetting(m, "treeGate", string(d.TreeGate)))
	s.MuleAt = IntSetting(m, "muleAtFletched", d.MuleAt)
	s.SpreadRungOne = BoolSetting(m, "spreadRungOne", d.SpreadRungOne)
	s.SpreadMaple = BoolSetting(m, "spreadMaple", d.SpreadMaple)
	s.MapleOverflow = BoolSetting(m, "mapleOverflow", d.MapleOverflow)
	s.SpreadYew = BoolSetting(m, "spreadYew", d.SpreadYew)
	s.SpreadYewWorlds = BoolSetting(m, "spreadYewWorlds", d.SpreadYewWorlds)
	s.YewWorlds = StringSetting(m, "yewWorlds", d.YewWorlds)
	s.SpreadMagic = BoolSetting(m, "spreadMagic", d.SpreadMagic)
	s.ChopManip = BoolSetting(m, "chopManip", d.ChopManip)
	return s
}

func (b *wcfletchBot) OnStart(ctx *Context) error {
	// The embedded OnStart does the pack/locs/components preconditions, reads every woodcutting
	// setting from its own keys, applies the cohort jitter to muleAt, and builds the thief, the
	// sail progress, the brightness press and the webwalk walker. All of that is wanted here
	// unchanged.
	if err := b.woodcutterBot.OnStart(ctx); err != nil {
		return err
	}
	// ...but NOT the rune-axe tier exemption. wcfletchplan chooses the log/product pair whose
	// own woodcutplan.CurrentTierIndex lands exactly on the rung its fletching ladder picked,
	// and asserts that round trip in a test; re-gating the ladder underneath it would desync
	// the two. Users asked for this on ProgressiveWoodcutter, and that is where it stays.
	b.fastAxeTiers = false
	b.cfg = wcfletchSettings(ctx.Settings)
	b.oaks.configure(ctx.Settings)
	b.oaks.target = b.cfg.Wood.Target
	b.cfg.Wood = b.woodcutterBot.settings // already normalised and jittered by the embedded OnStart
	b.cfg = b.cfg.Normalised()
	// Spread this script's own hand-over threshold across the cohort too, for the same reason
	// the woodcutter jitters its own: a cohort started together climbs at the same rate and
	// would otherwise all arrive at a mule that trades with one partner at a time.
	b.cfg.MuleAt = jitterMuleAt(b.cfg.MuleAt, ctx.Username)
	b.woodcutterBot.settings = b.cfg.Wood
	// Recorded as one of this cohort from the first moment, on whatever rung it works, so the
	// players the yew board counts as strangers leave it out: a rung-1 account at the dead trees
	// beside the church yews is no stranger to the accounts chopping the yews there
	// (wcfletchplan/campcrowd.go).
	wcfletchplan.NoteOwn(ctx.Username)

	d := b.deps(ctx)
	b.fletchXPAtStart = wcfletchrunner.FletchingXP(d)
	wood, fletch := woodcutrunner.WoodcuttingLevel(d), wcfletchrunner.FletchingLevel(d)
	idx := wcfletchplan.TierIndex(b.cfg.LadderSnapshotFor(wood, fletch, true))
	product, ok := wcfletchplan.Choose(idx, fletch, b.cfg.NormalLogs)
	making := "nothing it can fletch yet"
	if ok {
		making = fmt.Sprintf("%s (Fletching %d, %.1f xp/log)", strings.ToLower(product.ItemName), product.Level, product.XP())
	}
	mule := "no mule"
	if b.cfg.Wood.Mule != "" {
		mule = fmt.Sprintf("%d fletched units to %s", b.cfg.MuleAt, b.cfg.Wood.Mule)
	}
	if !b.cfg.Fletch {
		ctx.logf("wcfletch: fletching is switched OFF in the settings — this run behaves exactly as ProgressiveWoodcutter does")
	}
	ctx.logf("wcfletch: Woodcutting %d / Fletching %d -> %s, making %s; target %s, %s, plain logs -> %s, tree gate %q",
		wood, fletch, wcfletchplan.Ladder[idx].Tier.Label, making, b.cfg.Wood.Target, mule, b.cfg.NormalLogs, b.cfg.TreeGate)
	if b.cfg.ChopManip {
		b.manip = woodcutrunner.NewManipState()
		ctx.logf("wcfletch: chop manipulation is ON — a knife-on-log batch read in the stale tick after each swing, written as many ticks early as the link is late (chopManip=false turns it off)")
	}
	b.stall = newWalkStall(ctx.Settings, "wcfletch")
	b.walkFletch = wcfletchrunner.NewWalkFletch(b.cfg.Fletch && BoolSetting(ctx.Settings, "walkFletch", true))
	b.stall.stall.Busy = func() bool { return b.walkFletch.Busy(b.deps(ctx)) }
	b.stall.install(ctx, b.walker, b.walkFletch.Prepare(func() *woodcutrunner.Deps { return b.deps(ctx) }))
	if b.stall.on() {
		ctx.logf("wcfletch: run staller is ON - a carried Games necklace or Ring of dueling is rubbed every 3rd tick of a walk to save run energy (walkRub=false turns it off)")
	}
	if b.walkFletch.On {
		ctx.logf("wcfletch: walk-fletch is ON - a full pack of logs is cut on the walk to the bank, 2 ticks a log, instead of standing at the trees first (walkFletch=false turns it off)")
	}
	b.home.start(ctx, "wcfletch")
	// The yew cohort's world, adopted straight after the settings' home so the first Loop hops to it (wcfletch_world.go).
	b.yewWorldStart(ctx, d)
	b.idleClock.Worked(time.Now())
	return nil
}

func (b *wcfletchBot) Loop(ctx *Context) error {
	d := b.deps(ctx)
	if !woodcutrunner.WaitForGuardian(d) {
		return nil
	}
	if ctx.Session.State().IsDead() {
		if hp, ok := ctx.Session.State().HP(); ok && hp > 0 {
			ctx.Session.State().ClearDead()
		}
	}
	// Both waits used to be silent and unbounded; the second is where Acct188 sat for twenty
	// minutes after a Mime (loopgate.go).
	if b.loopWaits.holdForTrade(ctx, "wcfletch") {
		return nil
	}
	// The 2-tick oaks, inside the day's allowance (wcfletch_oaks.go).
	if b.oaks.step(ctx, d) {
		return nil
	}
	// On another world than homeWorld: go home before anything that needs the mule (a hand-out
	// or a hand-off is a trade, and a trade needs both accounts on one world). See homeworld.go.
	if !b.yewWorldStep(ctx, d, time.Now()) {
		ctx.Delay(600 * time.Millisecond) // the Woodcutting level has not arrived: no hop on a guess (wcfletch_world.go)
		return nil
	}
	if b.goHome(ctx, time.Now()) {
		ctx.Delay(5 * time.Second)
		return nil
	}
	s, ok := b.snapshot(ctx, d)
	if !ok {
		b.loopWaits.refused(ctx, d, "wcfletch")
		return nil
	}
	b.loopWaits.readable(ctx, "wcfletch")
	b.reportYewIdle(ctx, d, time.Now())

	// ---- the ladder, and handing it to woodcutplan ----
	//
	// snapshot() filled s.Level with the WOODCUTTING level and s.Target with the settings'
	// target, which is what ProgressiveWoodcutter's phase machine wants. This script's rung is a
	// different question, so the pair is replaced here with the one that makes woodcutplan's own
	// CurrentTier land on the rung the fletching ladder chose — see
	// wcfletchplan.EffectiveWoodLevel, whose contract is asserted against woodcutplan's real
	// CurrentTierIndex by TestEffectivePairRoundTripsThroughWoodcutplan.
	//
	// Everything downstream (DecideWoodPhase's walk/chop branch, the Draynor camp's own oak
	// gate) then follows this script's rung with no forked copy of either function.
	wood, fletch := woodcutrunner.WoodcuttingLevel(d), wcfletchrunner.FletchingLevel(d)
	idx := wcfletchplan.TierIndex(b.cfg.LadderSnapshotFor(wood, fletch, b.fletching()))
	// WHERE the maple rung chops, decided before anything reads the rung, because a seat at a
	// willow camp (the overflow) makes this pass a WILLOW pass from here on: willow logs chopped,
	// cut into willow longbows and banked, exactly as the willow rung does. The ladder itself is
	// untouched -- TierIndex still says maple, and the account comes back to a maple camp the
	// moment the board has room. See wcfletchplan/maplecamps.go: the one Seers maple camp gives
	// about 1,700 logs an hour however many accounts wait at it, and live on e35420b6 70 of 79
	// maple-rung accounts were waiting at it.
	//
	// The yew rung is seated the same way over its own camps (wcfletchplan/yewcamps.go), and never
	// changes the rung: live on 73ff4228 all 23 yew-rung accounts were at the church camp's three
	// yews. So is the magic rung (wcfletchplan/magiccamps.go): live on 8c77500e four accounts to a
	// world stood at the ladder's three magic trees.
	ladderIdx := idx
	var camp wcfletchplan.Camp
	var seated bool
	camp, seated, idx = b.seatBoards(ctx, d, idx, fletch)
	// The bank this pass carries to and draws from: the seated camp's, else the Seers bank.
	b.bank = wcfletchplan.SeersBank
	if seated {
		b.bank = camp.BankOf()
	}
	rung := wcfletchplan.Ladder[idx]
	product, canFletch := wcfletchplan.Choose(idx, fletch, b.cfg.NormalLogs)
	if !b.haveRung || b.rung != idx {
		b.rung, b.haveRung = idx, true
		what := "banking the logs uncut"
		if canFletch {
			what = "cutting them into " + strings.ToLower(product.ItemName)
		}
		if idx != ladderIdx {
			what += fmt.Sprintf(" (the %s rung's overflow while its camps are crowded)", strings.ToLower(wcfletchplan.Ladder[ladderIdx].Tier.Label))
		}
		ctx.logf("wcfletch: rung %d/%d: %s (Woodcutting %d, Fletching %d) — %s",
			idx+1, len(wcfletchplan.Ladder), rung.Tier.Label, wood, fletch, what)
	}
	// WHERE rung 1 chops. The ladder's own camp beside the bank has 12 normal trees in its
	// leash, and a normal tree gives one log then stands as a stump for 120-199 ticks, so the
	// camp's supply is fixed however many accounts queue at it -- 55 of them, live on 3dfb543a,
	// cut 27 logs an account-hour between them. Arrow shafts stack, so rung 1 never banks and a
	// patch across the field costs one walk. See wcfletchplan/patches.go.
	//
	// Only with the knife IN THE PACK. Without one the logs are not cut, so they fill the pack
	// and every load would be a walk back from the patch -- and the Seers knife spawn is beside
	// the ladder's own camp, which is where an account that still needs one should be. The pack
	// is read from the inventory itself (woodcutrunner.HasKnife), which content's tab nulling
	// cannot blind, so a knife that reads absent is absent: after a death, for one.
	chopTier, spread := rung.Tier, false
	switch {
	case seated:
		// A maple- or yew-board camp: the same override as a patch, so "at camp" means at THAT camp.
		chopTier, spread = camp.Tier(), true
		b.leavePatch(ctx)
	case b.cfg.SpreadRungOne && b.fletching() && wcfletchrunner.HasKnife(d) && wcfletchplan.UsesPatches(idx, product, canFletch):
		chopTier, spread = b.seatPatch(ctx, d), true
	default:
		b.leavePatch(ctx)
	}
	s.Level, s.Target = wcfletchplan.EffectiveWoodLevel(idx), wcfletchplan.EffectiveTarget(idx)
	// ...but the start camp is judged on the REAL Woodcutting level, not the rung's.
	//
	// The replacement above is what makes woodcutplan's tier machinery land on this script's
	// rung, and that is right for chopping and walking. It is wrong for the Draynor camp, which
	// is a "this account is brand new" question, not a "which tree" one. An account held at
	// rung 0 by Fletching 1 presented as Woodcutting 1 to that gate however good its axe was:
	// live 2026-09-23 nineteen accounts of Woodcutting 28 to 61 sailed Catherby -> Ardougne ->
	// Brimhaven -> Musa -> Port Sarim -> Draynor, about seven minutes and 72 coins each, to a
	// camp their own config line says ends at Woodcutting 25 — from startup, after a give-up
	// AND after a clean hand-off, three separate entry paths, because the gate was never
	// really consulted. Twelve of them then deadlocked there, and the 60-coin fares they asked
	// the mule for are what starved it of pickup capacity.
	//
	// THE TARGET IS THE OTHER HALF, and a build that carried only the level proved it: AtOakGate
	// answers false for any target below oak whatever the level, and EffectiveTarget above is
	// the RUNG's label -- "Normal tree" at rung 0 -- so the same nineteen accounts went straight
	// back to the camp. The owner's own target is what the gate has to judge.
	s.CampLevel, s.CampTarget = wood, b.cfg.Wood.Target

	// The Draynor camp's ship fare, inherited unchanged — see woodcutter.go's own comment for
	// why it is funded before anything else is decided.
	if need, want := woodcutplan.NeedsCampFare(s); want && b.askMuleForAnAxeReady() {
		if b.askMuleForCoins(ctx, d, "start camp fare", need) {
			ctx.logf("wcfletch: funded the %d-coin fare for the Draynor camp", need)
			return nil
		}
	}

	phase := woodcutplan.DecideWoodPhase(s)
	// DecideWoodPhase judged Walk/Chop against the ladder's own camp. Two things override that,
	// both only ever between those two phases and the bank, so nothing else it decided moves:
	if phase == woodcutplan.PhaseWalk || phase == woodcutplan.PhaseChop {
		switch {
		case b.bulkDrawDue(d, idx, product, canFletch):
			// The bank still holds this rung's logs: stay on the bank phase, where the fletching
			// half cuts what was drawn and bankRung draws the next pack, until it is empty
			// (wcfletchplan/bulkdraw.go).
			phase = woodcutplan.PhaseBank
		case spread && s.Tile != nil:
			// Seated at a patch: "at camp" means at THAT patch.
			phase = woodcutplan.PhaseWalk
			if woodcutplan.AtCamp(*s.Tile, chopTier) {
				phase = woodcutplan.PhaseChop
			}
		}
	}
	if !b.havePhase || phase != b.phase {
		b.phase, b.havePhase = phase, true
		ctx.logf("wcfletch: phase: %s", woodcutplan.DescribeWoodPhase(phase))
	}
	if phase != woodcutplan.PhaseAccept && phase != woodcutplan.PhaseTutorial {
		if dlg := actions.NewChatDialog(ctx.Session, ctx.Components); dlg.CanContinue() {
			_ = dlg.Continue()
			ctx.Delay(600 * time.Millisecond)
			return nil
		}
	}
	if ev := woodcutplan.DecideWoodEvent(woodcutrunner.EventSnapshot(d, b.bankAxes)); ev != woodcutplan.EventNone {
		b.eventStep(ctx, d, ev)
		return nil
	}

	if manual := ctx.TakeHandoffRequest(); manual {
		b.handoffPending = true
		b.gate.force = true
	}
	if b.cfg.Donor().Enabled() && b.handoffPending {
		ctx.SetPhase(phaselog.Handoff)
		return b.handoffFletched(ctx, d)
	}

	// ---- the fletching half ----
	//
	// Only at the three phases where the account is at its trees or its bank. Fletching during
	// the tutorial, the starter-kit sale, a pickpocketing session, a ship crossing or an axe
	// order would at best waste a pass and at worst open a make menu into a dialogue another
	// step is driving.
	//
	// PhaseStart is deliberately NOT one of them: the Draynor start camp has its own fletching
	// (woodcutplan.DecideStartPhase's StartFletch -> woodcutrunner.FletchShafts), it is already
	// live-proven, and it makes exactly this script's rung-0 product. It is left to do its job.
	switch phase {
	case woodcutplan.PhaseChop, woodcutplan.PhaseWalk, woodcutplan.PhaseBank:
		fs := wcfletchrunner.FletchSnapshot(d, b.fletching(), rung, product, canFletch, b.idle, phase == woodcutplan.PhaseBank, b.reserve(d, rung))
		switch wcfletchplan.Decide(fs) {
		case wcfletchplan.ActKnife:
			ctx.SetPhase(phaselog.Setup)
			b.knifeStep(ctx, d)
			return nil
		case wcfletchplan.ActFletch:
			ctx.SetPhase(phaselog.Fletching)
			if phase == woodcutplan.PhaseBank && b.walkFletchToBank(ctx, d, rung, product) {
				return nil
			}
			b.fletchStep(ctx, d, rung, product)
			return nil
		}
	}

	ctx.SetPhase(woodcutterPhaseName(phase))

	fd := b.fresh(d)
	switch phase {
	case woodcutplan.PhaseAccept:
		freshstartrunner.AcceptAppearance(fd)
	case woodcutplan.PhaseTutorial:
		freshstartrunner.SkipTutorial(fd, b.cfg.Wood.GuideNPC, b.cfg.Wood.SkipOption)
	case woodcutplan.PhaseBright:
		freshstartrunner.BrightnessStep(fd, b.bright)
	case woodcutplan.PhaseWait:
		ctx.Delay(1200 * time.Millisecond)
	case woodcutplan.PhaseStore:
		b.storeStep(ctx, d)
	case woodcutplan.PhaseStart:
		// THE DRAYNOR START CAMP (Woodcutting under the oak gate) is tick-manipulated too (audit, 5 Nov 2026): d.Manip was set only by
		// chopRung, so every account below Woodcutting 25 chopped its dead trees on the plain 4-tick roll. The camp's chop is the same
		// Chop; its logs (kept until the shaft fletch) are the carrier the knife batch needs.
		if b.manip != nil {
			d.Manip = &woodcutrunner.ManipConfig{Enabled: true, State: b.manip}
		}
		b.startCampStep(ctx, d, s)
	case woodcutplan.PhaseFund:
		freshstartrunner.PickpocketStep(fd, b.thief, freshstartrunner.PickpocketOpts{
			Target: b.cfg.Wood.PickpocketTarget, Spot: b.cfg.Wood.PickpocketTile,
			RestBelow: b.cfg.Wood.RestBelow(), Need: freshstart.FareRemaining(s.Tile, b.cfg.Wood.Fare),
		})
	case woodcutplan.PhaseTravel:
		freshstartrunner.SailLeg(fd, woodcutplan.Seers, b.sail)
	case woodcutplan.PhaseBank:
		b.bankRung(ctx, d, rung, product)
	case woodcutplan.PhaseOrder:
		b.orderStep(ctx, d)
	case woodcutplan.PhaseWalk:
		radius := chopTier.Leash
		if radius > 3 {
			radius = 3
		}
		if !woodcutrunner.WalkNear(d, chopTier.Anchor, radius) {
			ctx.Delay(2 * time.Second)
		}
	case woodcutplan.PhaseChop:
		b.chopRung(ctx, d, chopTier)
	case woodcutplan.PhaseStuck:
		b.warn(ctx, "stuck", 120*time.Second, "no axe in the pack, worn or in the bank, and no mule to ask — put an axe in the Seers bank, or name a mule in the settings")
		ctx.Delay(3 * time.Second)
	}
	return nil
}

// fletching reports whether this run is fletching at all right now.
// knifeRetry is how long fletching stays off after a give-up before the account tries for a
// knife again. Ten minutes: long enough that 37 knifeless accounts are not all standing on the
// same four spawn tiles, short enough that an account is not wasting a whole run's fletching xp
// over a knife somebody else was holding for a minute.
const knifeRetry = 10 * time.Minute

// fletching reports whether this run is cutting logs right now.
//
// A knife in the pack overrides the give-up outright: whatever the account could not reach ten
// minutes ago, it demonstrably has one now — a bank withdrawal, the mule, or a spawn that
// respawned. See fletchOff for why this stopped being a latch.
func (b *wcfletchBot) fletching() bool {
	if !b.cfg.Fletch {
		return false
	}
	if !b.fletchOff {
		return true
	}
	return !b.fletchOffAt.IsZero() && time.Since(b.fletchOffAt) >= knifeRetry
}

// knifeBack clears a give-up because a knife is actually in hand. Called wherever one arrives.
func (b *wcfletchBot) knifeBack(ctx *Context) {
	if !b.fletchOff {
		return
	}
	b.fletchOff, b.fletchOffAt, b.knifeTries, b.fletchFails = false, time.Time{}, 0, 0
	ctx.logf("wcfletch: a knife turned up — fletching back ON")
}

// ---- the knife ----

// maxFletchKnifeTries is how many knife attempts this script gets before fletching is latched off
// for the run.
//
// Three, matching the start camp's own maxKnifeFails and for the same reason: "could not reach
// the spawn" is a walk that can succeed next time, while "no knife on the ground" means another
// account is holding the only one and content respawns it on its own timer. Past that, a loop
// would be the failure — and the fallback here is much softer than the camp's, because an account
// that cannot fletch is simply a ProgressiveWoodcutter.
const maxFletchKnifeTries = 3

func (b *wcfletchBot) knifeStep(ctx *Context, d *woodcutrunner.Deps) {
	res := wcfletchrunner.TakeKnife(d)
	if res.Got {
		if b.knifeTries > 0 {
			ctx.logf("wcfletch: knife collected — fletching again")
		}
		b.knifeTries = 0
		b.knifeBack(ctx)
		return
	}
	if res.Stopped {
		return
	}
	b.knifeTries++
	if b.knifeTries >= maxFletchKnifeTries {
		// Ask the mule before giving up, exactly as the start camp does. There are four knife
		// ground spawns in the world (woodcutplan.KnifeSpawns) against a cohort the owner sizes
		// at one woodcutter per rune axe on the Seers market, so nearly every account loses that
		// race at least once; the mule is the supply for a cohort and the spawn is the free
		// shortcut for a single account.
		if b.askMuleForKnife(ctx, d) {
			b.knifeTries = 0
			return
		}
		b.fletchOff, b.fletchOffAt = true, time.Now()
		ctx.logf("wcfletch: no knife anywhere (%s, %d attempts) and %q could not supply one — fletching off for now; this account chops and banks exactly as ProgressiveWoodcutter does, and tries for a knife again in %s",
			res.Reason, b.knifeTries, b.cfg.Wood.Mule, knifeRetry)
		return
	}
	b.warn(ctx, "fletch-knife", 30*time.Second, "could not get a knife: %s (attempt %d of %d)", res.Reason, b.knifeTries, maxFletchKnifeTries)
	ctx.Delay(3 * time.Second)
}

// ---- cutting ----

func (b *wcfletchBot) fletchStep(ctx *Context, d *woodcutrunner.Deps, rung wcfletchplan.Rung, p wcfletchplan.Product) {
	res := wcfletchrunner.Fletch(d, rung.LogsName, p, b.reserve(d, rung))
	if res.Reason == "reserve" {
		// Only the log kept for the chop manipulation's knife is left: nothing to cut, and no failure.
		b.idle = false
		return
	}
	if res.Made {
		b.fletched += res.Products
		b.logsCut += res.Logs
		b.fletchFails = 0
		// Cutting logs is exactly what clears the idle state: there is nothing left to do with
		// them, so the next pass goes back to the trees.
		b.idle = false
		return
	}
	if res.Reason == "stopped" {
		return
	}
	b.fletchFails++
	b.warn(ctx, "fletch", 30*time.Second, "cutting %s into %s made nothing (%s, %d in a row)",
		strings.ToLower(rung.LogsName), strings.ToLower(p.ItemName), res.Reason, b.fletchFails)
	// A knife that has gone (traded, died with it, a bug) is the one failure that is recoverable
	// by doing something else, so send the next pass to a spawn rather than back to the same
	// refused menu.
	if res.Reason == "no-knife" {
		return
	}
	// Anything else costs a pass. Delay so a refused use cannot spin: a use-failed branch with no
	// delay is what produced 26.9 million identical log lines in one live session.
	ctx.Delay(2 * time.Second)
	if b.fletchFails >= maxFletchFails {
		b.fletchOff, b.fletchOffAt = true, time.Now()
		ctx.logf("wcfletch: cutting %s failed %d times in a row (%s) — fletching off for now; this account chops and banks exactly as ProgressiveWoodcutter does, and tries again in %s",
			strings.ToLower(rung.LogsName), b.fletchFails, res.Reason, knifeRetry)
	}
}

// walkFletchToBank walks to the Seers bank with the walk's own aside cutting the pack's logs on the way (wcfletchrunner/
// walkfletch.go), in place of cutting them where the account stands and walking afterwards: 28 yew logs are 56 ticks of
// standing and the walk is 30 to 40 ticks that cut nothing. It reports whether it walked (the pass is then over); false leaves
// the standing cut to do what it always did: a short walk, a product that stacks (rung 1's shafts never bank), a pack with too
// few logs to be worth a batch, no knife, or the setting or the guard off.
//
// What the walk does not cut is cut at the bank by the next pass (the account is then within the minimum distance, so this
// returns false and fletchStep runs), before bankRung deposits anything: a log banked uncut would be a log not fletched.
func (b *wcfletchBot) walkFletchToBank(ctx *Context, d *woodcutrunner.Deps, rung wcfletchplan.Rung, p wcfletchplan.Product) bool {
	if b.walkFletch == nil || !b.walkFletch.Allowed(time.Now()) || p.PerLog > 1 || !wcfletchrunner.HasKnife(d) {
		return false
	}
	bank := b.campBank()
	if !wcfletchplan.WalkFletchWorth(tile.Chebyshev(currentTileOf(d), bank.Stand), true) {
		return false
	}
	reserve := b.reserve(d, rung)
	if wcfletchrunner.CountByName(d, rung.LogsName)-reserve < 2 {
		return false
	}
	b.walkFletch.Arm(rung.LogsName, p, reserve)
	ok := woodcutrunner.WalkNear(d, bank.Stand, 3)
	b.walkFletch.Disarm()
	if !ok {
		// The same wait OpenSeersBank's own failed walk takes before the next pass tries again.
		ctx.Delay(3 * time.Second)
	}
	return true
}

// maxFletchFails is how many consecutive fletch attempts that produce NOTHING are allowed before
// fletching is latched off.
//
// Six, deliberately more generous than the knife's three, because the failures it is counting are
// not all permanent: "no-menu" can be a make interface that a level-up dialogue got in front of,
// and "use-failed" can be the sidebar tabs the server has not assigned yet (which resolves on its
// own). What must not happen is an account standing at a tree cutting nothing for an hour, and
// six attempts at roughly two seconds apart plus the surrounding chop is a minute or two.
const maxFletchFails = 6

// chopRung is ProgressiveWoodcutter's chopStep with one addition: it remembers whether the chop
// found a tree.
//
// That is the fleet owner's "while accounts are waiting for trees to respawn ... they can cut
// their logs", and it is real recovered throughput rather than a tidiness: the live fleet's
// woodcutters spend measurable time on "no tree in sight". It is woodcutrunner.Chop's own "no-tree"
// (nothing of this species standing inside the leash) and nothing else.
//
// NOT "all-blocked". A tree that stands but is excluded (an Ent at it, no stand tile reachable, a wedge)
// is a tree the account is about to try again, and the a user request: "it should keep tick
// manipulating the tree until there are really none left ... if there isn't another tree it will start
// to fletch which it shouldn't". Cutting the pack's logs there pulled the account's attention off a yew
// that was still standing (acct149 read a slow yew as "gave nothing", excluded the only one in reach and
// cut its lone log into a longbow). The account waits for the exclusion to lift (woodcutter.go's
// waitOrReturn) and fletches only when there is nothing to chop.
//
// The tier is the rung's own, or the normal-tree patch this account is seated at (the same tier
// with another anchor, leash and loc name), so the chop, the Ent handling and the step-away all
// run against the place the account is actually standing.
func (b *wcfletchBot) chopRung(ctx *Context, d *woodcutrunner.Deps, tier woodcutplan.Tier) {
	if b.manip != nil {
		d.Manip = &woodcutrunner.ManipConfig{Enabled: true, State: b.manip}
		if b.carrierTrip(ctx, d) {
			return
		}
	}
	began := time.Now()
	res := woodcutrunner.Chop(d, tier)
	b.chopped += res.Gained
	b.idle = res.Reason == "no-tree"
	b.chopOutcome(ctx, d, tier, res)
	b.noteChopPass(ctx, d, tier, res.Reason, time.Since(began))
}

// carrierRetry is the least time between two trips for a plain-tree log.
const carrierRetry = 5 * time.Minute

// reserve is how many of the rung's logs the fletch step leaves in the pack: the one the chop manipulation's knife is
// used on (wcfletchplan.CarrierReserve). Zero with the manipulation off.
func (b *wcfletchBot) reserve(d *woodcutrunner.Deps, rung wcfletchplan.Rung) int {
	return wcfletchplan.CarrierReserve(b.manip != nil, wcfletchrunner.PackLogs(d), rung.LogsName, wcfletchrunner.FletchingLevel(d))
}

// drawCarrierAndLogs takes the chop manipulation's carrier log and the banked logs out of the open bank in ONE burst
// (twopertick-1012): the carrier used to be drawn and waited on, and only then were the logs drawn and waited on,
// a round trip each on every Seers visit. The two Withdraws are independent (the bank's list was fresh when it
// opened), so they go out together and are confirmed once, with the one-at-a-time path behind them. It reports
// whether the banked logs were drawn (the old `drew`), with the same bookkeeping.
func (b *wcfletchBot) drawCarrierAndLogs(ctx *Context, d *woodcutrunner.Deps, bank *actions.Bank, rung wcfletchplan.Rung) (drew bool) {
	var ops []actions.BankOp
	// The carrier: one log, taken when the pack holds none the manipulation's knife could be used on.
	var carrierID int32
	carrierName := ""
	if b.manip != nil && wcfletchrunner.HasKnife(d) {
		if name, ok := wcfletchplan.CarrierFromBank(true, wcfletchrunner.PackLogs(d), wcfletchrunner.BankLogs(d, bank), wcfletchrunner.FletchingLevel(d)); ok {
			if id, found := woodcutrunner.BankRowID(d, bank, name); found {
				carrierID, carrierName = id, name
			} else {
				b.warn(ctx, "carrier-withdraw", 5*time.Minute, "could not take a %s out of the bank for the chop manipulation", strings.ToLower(name))
			}
		}
	}
	// The banked logs: only with fletching on and a knife in hand, bounded by pack room (the carrier takes one).
	var logsID int32
	banked := 0
	if b.fletching() && wcfletchrunner.HasKnife(d) {
		if banked = woodcutrunner.BankTargetLogs(d, bank, rung.Tier.Logs); banked > 0 {
			free := woodcutrunner.FreeSlots(d)
			if carrierID != 0 {
				free--
			}
			if id, found := woodcutrunner.BankRowID(d, bank, rung.Tier.Logs); found && free > 1 {
				logsID = id
			} else {
				b.drawFails++
			}
		}
	}
	// Logs of the carrier's own kind: the Withdraw-All brings the carrier with it.
	if logsID != 0 && carrierID == logsID {
		carrierID = 0
	}
	if carrierID != 0 {
		ops = append(ops, actions.WithdrawOp(carrierID, 1))
	}
	if logsID != 0 {
		ops = append(ops, actions.WithdrawAllOp(logsID))
	}
	if len(ops) == 0 {
		return false
	}
	r, retried := bank.Visit(ops, woodcutrunner.Waiter(d), actions.BatchOptions{})
	ctx.logf("wcfletch: %s", actions.VisitLog("carrier and banked logs", len(ops), r, retried))
	if r.Err != nil {
		// The visit could not run (the bank closed, a stop): nothing is known to have been drawn.
		if logsID != 0 {
			b.drawFails++
		}
		return false
	}
	if carrierID != 0 {
		if _, short := r.ShortOf(actions.BankOpWithdraw, carrierID); short {
			b.warn(ctx, "carrier-withdraw", 5*time.Minute, "could not take a %s out of the bank for the chop manipulation", strings.ToLower(carrierName))
		} else {
			b.vlog(ctx, "took one %s out of the bank to tick-manipulate with", strings.ToLower(carrierName))
		}
	}
	if logsID != 0 {
		if _, short := r.ShortOf(actions.BankOpWithdraw, logsID); short {
			b.drawFails++
			return false
		}
		b.bankDraws++
		b.drawFails = 0
		ctx.logf("wcfletch: %d %s already banked — taking them out to fletch before chopping more",
			banked, strings.ToLower(rung.Tier.Logs))
		return true
	}
	return false
}

// carrierTrip cuts one log from a plain tree beside the account when it has none to put the knife on, so the first
// swing at a yew is already manipulated instead of the first dozen. It reports whether it did something (the caller's
// pass is then over). At the church yews the nearest plain trees are the dead trees beside them; at the oaks the
// ladder's own normal trees; at camps with no plain tree within wcfletchplan.CarrierPatchReach it does nothing and the
// account's own first log is the carrier. At most once every carrierRetry.
func (b *wcfletchBot) carrierTrip(ctx *Context, d *woodcutrunner.Deps) bool {
	if !wcfletchrunner.HasKnife(d) || !wcfletchplan.NeedsCarrier(true, wcfletchrunner.PackLogs(d), wcfletchrunner.FletchingLevel(d)) {
		return false
	}
	if time.Since(b.carrierTriedAt) < carrierRetry {
		return false
	}
	patch, ok := wcfletchplan.NearestPlainPatch(currentTileOf(d))
	if !ok {
		return false
	}
	b.carrierTriedAt = time.Now()
	ctx.logf("wcfletch: no log to tick-manipulate with — cutting one from the %s at %s first", strings.ToLower(patch.Loc), patch.Name)
	plain := *d
	plain.Manip = nil
	res := woodcutrunner.Chop(&plain, patch.Tier())
	b.chopped += res.Gained
	if res.Gained == 0 {
		b.vlog(ctx, "no log from the %s at %s (%s)", strings.ToLower(patch.Loc), patch.Name, res.Reason)
	}
	return true
}

// seatBoards seats the account on the board its rung has, if it has one, and gives back the seats on
// the others: the maple rung is spread over wcfletchplan.MapleCamps, the yew rung over
// wcfletchplan.YewCamps and the magic rung over wcfletchplan.MagicCamps, each by its own setting,
// and every other rung (or that setting off) is seated nowhere, which means the ladder's own camp.
// It returns the camp and whether the account is seated, and the rung it now works: the maple
// board's willow overflow is another rung's logs, a yew or magic camp is always its rung's own, so
// idx comes back the rung it went in as.
func (b *wcfletchBot) seatBoards(ctx *Context, d *woodcutrunner.Deps, idx, fletch int) (camp wcfletchplan.Camp, seated bool, rung int) {
	switch {
	case b.cfg.SpreadMaple && wcfletchplan.UsesMapleCamps(idx):
		camp = b.seatCamp(ctx, d, fletch)
		b.leaveYew(ctx)
		b.leaveMagic(ctx)
		return camp, true, camp.Rung
	case b.cfg.SpreadYew && wcfletchplan.UsesYewCamps(idx):
		// The church camp's three yews gave 23 accounts 45 bows an account an hour, 180 being what
		// one account takes from a tree to itself (wcfletchplan/yewcamps.go).
		camp = b.seatYew(ctx, d)
		b.leaveCamp(ctx)
		b.leaveMagic(ctx)
		return camp, true, idx
	case b.cfg.SpreadMagic && wcfletchplan.UsesMagicCamps(idx):
		// The ladder's three magic trees feed three accounts; a world's four waited a quarter to a
		// third of their time (wcfletchplan/magiccamps.go).
		camp = b.seatMagic(ctx, d)
		b.leaveCamp(ctx)
		b.leaveYew(ctx)
		return camp, true, idx
	}
	b.leaveCamp(ctx)
	b.leaveYew(ctx)
	b.leaveMagic(ctx)
	return wcfletchplan.Camp{}, false, idx
}

// campBank is the bank this pass carries to: the seated camp's (b.bank), the Seers bank when none.
func (b *wcfletchBot) campBank() wcfletchplan.CampBank {
	if b.bank.Name == "" {
		return wcfletchplan.SeersBank
	}
	return b.bank
}

// seatPatch takes or keeps this account's seat on the world's patch board and returns the
// patch's tier. A new seat is logged once, with the board's occupancy, so a live log can say
// where the cohort went.
func (b *wcfletchBot) seatPatch(ctx *Context, d *woodcutrunner.Deps) woodcutplan.Tier {
	board := wcfletchplan.SharedPatchBoardFor(ctx.World())
	here := woodcutplan.Seers
	if x, z, level, ok := d.Session.State().Tile(); ok {
		here = tile.Tile{X: x, Z: z, Level: level}
	}
	now := time.Now()
	st := board.Seat(ctx.Username, here, now)
	patches := board.Patches()
	p := patches[st.Patch]
	if st.Moved || !b.havePatch || b.patch != st.Patch {
		ctx.logf("wcfletch: rung 1 patch: %s (%d trees named %q, %d account(s) seated there) — board: %s",
			p.Name, p.Trees, p.Loc, st.Occupants, wcfletchplan.DescribeOccupancy(patches, board.Occupancy(now)))
	}
	b.patch, b.havePatch = st.Patch, true
	return p.Tier()
}

// leavePatch gives this account's seat back, so the board counts it only while it is on rung 1.
func (b *wcfletchBot) leavePatch(ctx *Context) {
	if !b.havePatch {
		return
	}
	wcfletchplan.SharedPatchBoardFor(ctx.World()).Leave(ctx.Username)
	b.havePatch = false
}

// seatCamp takes or keeps this account's seat on the world's maple board and returns the camp.
// A new seat is logged once, with what the supply model expects there and the board's occupancy,
// so a live log can say where the maple cohort went and why.
func (b *wcfletchBot) seatCamp(ctx *Context, d *woodcutrunner.Deps, fletch int) wcfletchplan.Camp {
	board := wcfletchplan.SharedCampBoardFor(ctx.World())
	here := woodcutplan.Seers
	if x, z, level, ok := d.Session.State().Tile(); ok {
		here = tile.Tile{X: x, Z: z, Level: level}
	}
	now := time.Now()
	st := board.Seat(ctx.Username, here, b.cfg.MapleOverflow, now)
	camps := board.Camps()
	c := camps[st.Camp]
	if st.Moved || !b.haveCamp || b.camp != st.Camp {
		why := ""
		if c.Overflow() {
			into := "banked uncut"
			if p, ok := wcfletchplan.Choose(c.Rung, fletch, b.cfg.NormalLogs); ok {
				into = "cut into " + strings.ToLower(p.ItemName) + "s"
			}
			why = fmt.Sprintf(" — the maple camps are crowded, so %s, %s, until one has room",
				strings.ToLower(wcfletchplan.Ladder[c.Rung].Tier.Logs), into)
		}
		ctx.logf("wcfletch: maple rung camp: %s (%d %s trees, %d account(s) seated there, about %.0f xp/h each by the supply model)%s — board: %s",
			c.Name, c.Trees, strings.ToLower(wcfletchplan.Ladder[c.Rung].Tier.Label), st.Occupants, st.Share, why,
			wcfletchplan.DescribeCamps(camps, board.Occupancy(now)))
	}
	b.camp, b.haveCamp = st.Camp, true
	return c
}

// leaveCamp gives this account's maple-board seat back, so the board counts it only while it is
// on the maple rung.
func (b *wcfletchBot) leaveCamp(ctx *Context) {
	if !b.haveCamp {
		return
	}
	wcfletchplan.SharedCampBoardFor(ctx.World()).Leave(ctx.Username)
	b.haveCamp = false
}

// bulkDrawDue is wcfletchplan.BulkDrawDue over live state.
func (b *wcfletchBot) bulkDrawDue(d *woodcutrunner.Deps, idx int, product wcfletchplan.Product, canFletch bool) bool {
	if !b.bankRungKnown {
		return false
	}
	near := false
	if x, z, level, ok := d.Session.State().Tile(); ok {
		near = level == 0 && tile.Chebyshev(tile.Tile{X: x, Z: z}, b.campBank().Stand) <= wcfletchplan.NearBankRadius
	}
	return wcfletchplan.BulkDrawDue(wcfletchplan.BulkSnapshot{
		Fletching:          b.fletching() && canFletch && wcfletchrunner.HasKnife(d),
		Stacking:           product.PerLog > 1,
		BankedLogs:         b.bankRungLogs,
		BankReadIsThisRung: b.bankRungIdx == idx,
		DrawFails:          b.drawFails,
		NearBank:           near,
	})
}

// ---- the bank (the Seers bank, or the seated camp's own) ----

// bankRung is ProgressiveWoodcutter's bankStep with the hand-over trigger changed.
//
// Everything it does to the bank is woodcutrunner's own exported helpers, called in the same
// order for the same reasons (deposit all but the best axe; read the bank's axes; take a better
// one out; then, having just emptied the pack and while standing where the market is, consider an
// axe upgrade). The ONE difference is what starts a hand-over: ProgressiveWoodcutter counts
// TARGET-SPECIES LOGS and this counts FLETCHED UNITS, because that is what this script's bank
// fills up with. Counting logs here would never fire at all on a run that is fletching properly —
// the logs have become bows by the time they reach the bank.
func (b *wcfletchBot) bankRung(ctx *Context, d *woodcutrunner.Deps, rung wcfletchplan.Rung, product wcfletchplan.Product) {
	at := b.campBank()
	bank, ok := woodcutrunner.OpenBankAt(d, at.Stand)
	if !ok {
		b.warn(ctx, "bank-open", 60*time.Second, "could not reach or open the %s bank — retrying", at.Name)
		ctx.Delay(3 * time.Second)
		return
	}
	keep := []string{}
	if a := woodcutplan.AxeToKeep(woodcutrunner.HeldAxes(d)); a != "" {
		keep = append(keep, a)
	}
	// The knife stays out of every deposit. It is not on woodcutplan's own keep list because
	// ProgressiveWoodcutter has no use for one, and banking it would cost this script a whole
	// round trip to a shared ground spawn to get it back.
	keep = append(keep, wcfletchplan.Knife)
	keep = append(keep, b.stall.keepNames()...)

	if deposited := woodcutrunner.DepositAllExcept(d, bank, keep); deposited > 0 {
		b.trips++
		b.vlog(ctx, "banked %d slots", deposited)
	}
	// The run staller's jewellery: drawn from the account's own bank when the pack holds none.
	b.stall.drawRub(ctx, bank, func(name string) bool { return woodcutrunner.WithdrawOneOf(d, bank, name) })

	b.bankAxes = woodcutrunner.BankAxes(d, bank)
	b.bankRead = true

	if want := woodcutplan.AxeToWithdraw(woodcutrunner.HeldAxes(d), b.bankAxes); want != "" {
		if woodcutrunner.WithdrawOneOf(d, bank, want) {
			ctx.logf("wcfletch: took the %s out of the bank", strings.ToLower(want))
			b.asks, b.orderedAt = 0, time.Time{}
		} else {
			b.warn(ctx, "axe-withdraw", 60*time.Second, "could not withdraw the %s — retrying", strings.ToLower(want))
		}
		b.bankAxes = woodcutrunner.BankAxes(d, bank)
	}

	// A knife that is in the BANK rather than the pack is the cheap recovery: take it back out
	// before walking to a ground spawn. Free while the bank is already open.
	//
	// NOT gated on fletching(). It used to be, which meant an account that had given up on the
	// contested ground spawn would stand at the bank holding a knife it had banked itself and
	// not take it out, because fletching was off -- and fletching stayed off because it had no
	// knife. The account's own bank is the one knife supply nobody else is racing it for.
	if b.cfg.Fletch && !wcfletchrunner.HasKnife(d) {
		if woodcutrunner.WithdrawOneOf(d, bank, wcfletchplan.Knife) {
			ctx.logf("wcfletch: took the knife out of the bank")
			b.knifeTries = 0
			b.knifeBack(ctx)
		}
	}

	// THE CARRIER FOR THE CHOP MANIPULATION, before anything else is drawn: the pack was just emptied of every log, the
	// carrier included, so one comes back out (a plain log if the bank has one, else the cheapest log the Fletching level
	// allows) and stays through the fletch step (it leaves one log of its kind uncut) and the walk to the trees.
	// (Drawn in the same burst as the banked logs below: drawCarrierAndLogs.)

	// FLETCH WHAT IS ALREADY BANKED BEFORE CHOPPING MORE. The fleet owner, 2026-09-23:
	// "progressivewcfletch should check the bank for any logs prior to going and chopping more
	// to fletch if the bank has logs, fletch them if not then chop".
	//
	// Banked logs are finished work waiting to become a product; chopping adds to the pile
	// instead of clearing it. Taking them out here costs nothing, because the bank is already
	// open on this trip and the walk to the trees has not started -- and once they are in the
	// pack, wcfletchplan.Decide's own ActFletch fires on the next Loop and cuts them exactly as
	// if they had just been chopped.
	//
	// Bounded by pack room so it can never fill the pack and leave the account nothing to chop
	// into: an account that has just deposited has a nearly empty pack, and whatever does not
	// fit stays banked for the next trip. Only when fletching is on and a knife is in hand --
	// without one this would draw logs it cannot cut and simply carry them around.
	//
	// ONE draw per visit, and on every rung but the first that is all it takes: a cut pack of
	// bows is a full pack, so the next Loop comes straight back here for the next draw. Arrow
	// shafts stack, so on rung 1 it is not, and Loop's wcfletchplan.BulkDrawDue check is what
	// holds the account here until the bank is empty (wcfletchplan/bulkdraw.go).
	drew := b.drawCarrierAndLogs(ctx, d, bank, rung)

	b.bankStock = wcfletchrunner.BankStock(d, bank)
	// Still read the logs, so a run whose fletching is off (or which banked logs before a knife
	// turned up) still hands them over at the ordinary threshold. Read AFTER the withdraw above
	// so it reflects what is genuinely left banked rather than what was there a moment ago.
	b.bankTargetLogs = woodcutrunner.BankTargetLogs(d, bank, rung.Tier.Logs)
	if idx, ok := wcfletchplan.RungIndexForLogs(rung.LogsID); ok {
		b.bankRungLogs, b.bankRungIdx, b.bankRungKnown = b.bankTargetLogs, idx, true
	}
	_ = bank.Close()

	// A visit that has just drawn logs to fletch -- or that is part way through emptying the bank
	// of them -- leaves the hand-off for later. A hand-off started now would bank the pack first
	// so the load can be drawn as notes, putting the logs just drawn straight back, and MuleRows
	// would then send every banked log to the mule uncut: the opposite of what the draw is for.
	// Bounded: the bank's logs only go down while this holds, and MaxBulkDrawFails failed draws
	// in a row end it, so the next visit that has nothing left to draw starts the hand-off.
	stillDrawing := drew
	if idx, ok := wcfletchplan.RungIndexForLogs(rung.LogsID); ok && !stillDrawing {
		stillDrawing = b.bulkDrawDue(d, idx, product, true)
	}
	if b.cfg.Donor().Enabled() && !stillDrawing {
		muleAt := b.cfg.MuleAt
		if v, ok := ctx.MuleAtOverride(); ok {
			muleAt = v
		}
		packStock := wcfletchrunner.PackStock(d)
		packLogs := wcfletchrunner.RungLogsHeld(d, rung)
		switch {
		case wcfletchplan.MuleDue(b.bankStock, packStock, muleAt, time.Now(), b.handoffNext):
			ctx.logf("wcfletch: %d unstrung bows/shafts held — handing them to %s to be strung",
				b.bankStock+packStock, b.cfg.Wood.Mule)
			b.handoffPending = true
		case woodcutplan.MuleDue(b.bankTargetLogs, packLogs, b.cfg.Wood.MuleAt, time.Now(), b.handoffNext):
			ctx.logf("wcfletch: %d %s held uncut — handing them over as logs",
				b.bankTargetLogs+packLogs, strings.ToLower(rung.Tier.Logs))
			b.handoffPending = true
		}
	}

	if b.cfg.Wood.BuysAxes() && !b.handoffPending && time.Now().After(b.axeNext) {
		b.axeStep(ctx, d)
	}
}

// ---- the mule ----

// handoffFletched is ProgressiveWoodcutter's handoffStep with wcfletchplan's rows: the unstrung
// bows and arrow shafts go on the trade screen FIRST, then any logs that were never cut, then the
// rest of what the bank gathered. The keep list adds the knife.
func (b *wcfletchBot) handoffFletched(ctx *Context, d *woodcutrunner.Deps) error {
	keepList := []string{wcfletchplan.Knife}
	if a := woodcutplan.AxeToKeep(woodcutrunner.HeldAxes(d)); a != "" {
		keepList = append(keepList, a)
	}
	keepList = append(keepList, splitKeep(b.cfg.Wood.MuleKeep)...)
	keepList = append(keepList, b.stall.keepNames()...)
	mule := b.cfg.Wood.Mule

	// A QUEUE TOO LONG TO JOIN IS WORKED THROUGH, NOT WALKED INTO. This is the 84-tile walk every
	// cap give-up on 263cceda paid twice (there and back) before it waited at all -- 96 of that
	// run's 105 hand-offs were this script's. See drawGate.
	if wait, why, hold := b.gate.check(ctx, mule); hold {
		b.handoffPending = false
		b.handoffNext = time.Now().Add(wait)
		ctx.logf("wcfletch: hand-off held back: %s — chopping on, asking again in %s", why, wait.Round(time.Second))
		return nil
	}

	at := b.campBank()
	bank, ok := woodcutrunner.OpenBankAt(d, at.Stand)
	if !ok {
		ctx.logf("wcfletch: hand-off: could not reach or open the %s bank — will retry", at.Name)
		ctx.Delay(5 * time.Second)
		return nil
	}
	// EMPTY THE PACK FIRST, so the load is drawn as notes into a pack that has room for it.
	//
	// bankRung's "fletch what is already banked" step withdraws up to a pack of UNNOTED logs,
	// and the hand-off trigger a few lines later fires on the same bank visit, so this used to
	// start with 26 of 28 slots already full of logs. Nothing could be drawn ("17 row(s) in
	// the bank, 26 pack slot(s) drawn, 17 row(s) left"), MuleHaul counted the 26 logs as the
	// load, and PlanLoad could not put a 26-slot group on a trade screen with 23 free slots —
	// so the donor walked to the mule with nothing it would offer and looped empty trades
	// until the meet cap. Live on 3dfb543a: ten donors, 144 empty trades in forty minutes.
	//
	// Depositing them puts the logs back where MuleRows already lists them ("then any logs
	// that were never cut"), so they cross as one noted stack like every other row. The keep
	// list is the same one the draw below uses, so the knife, the axe and the coins stay out.
	if deposited := woodcutrunner.DepositAllExcept(d, bank, keepList); deposited > 0 {
		ctx.logf("wcfletch: hand-off: banked %d pack stack(s) first, so the load can be drawn as notes", deposited)
	}
	rows := wcfletchplan.MuleRows(woodcutrunner.BankItemRows(d, bank), keepList)
	// THE WHOLE DRAW IS ONE BURST (twopertick-1012): note mode on, a Withdraw-All per row (a note takes one pack slot, so
	// as many rows as the pack has free slots) and note mode off go out together, five user events a tick, and are
	// confirmed once. It was a note-mode switch that waited on the varp, a Withdraw-All and a wait per row, and another
	// switch and wait.
	drawNames := make([]string, 0, len(rows))
	for _, row := range rows {
		drawNames = append(drawNames, row.Name)
	}
	noteDraw, asked := bank.DrawAllAsNotes(bank.RowIDsByName(d.Config, drawNames), actions.NoteResolver(d.Config), woodcutrunner.FreeSlots(d), woodcutrunner.Waiter(d))
	if asked > 0 {
		ctx.logf("wcfletch: hand-off: %s", actions.VisitLog("note draw", asked+2, noteDraw, 0))
	}
	b.bankAxes = woodcutrunner.BankAxes(d, bank)
	b.bankStock = wcfletchrunner.BankStock(d, bank)
	rowsLeft := len(wcfletchplan.MuleRows(woodcutrunner.BankItemRows(d, bank), keepList))
	_ = bank.Close()

	packRows := func() []donorplan.ItemRow { return woodcutrunner.PackSlotRows(d) }
	drawn := wcfletchplan.MuleHaul(packRows(), keepList)
	if drawn <= 0 {
		b.handoffPending = false
		b.handoffNext = time.Now().Add(donorplan.HandoffRetry)
		ctx.logf("wcfletch: hand-off: nothing drawn from the bank — resuming chopping")
		return nil
	}
	ctx.logf("wcfletch: hand-off to %q — %d row(s) in the bank, %d pack slot(s) drawn, %d row(s) left for another load",
		mule, len(rows), drawn, rowsLeft)

	rd := &donorrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
		Bus: ctx.Bus, Me: ctx.Username,
	}
	// Reach a mule on another world: hop there first and home afterwards, bounded by
	// worldhopplan; a one-world fleet no-ops. ProgressiveWoodcutter's own hand-off has always
	// done this and this one, written beside it, did not. See bot.WireDonorWorlds.
	WireDonorWorlds(ctx, rd)
	// An account that lives on another world than the mule's (dealt there, or by its homeWorld setting) hops to the
	// mule's and home again; the trip keeps the restarted script on the mule's world until the trade is made
	// (wcfletch_world.go).
	b.tripBegin(ctx)
	res := donorrunner.Run(rd, donorrunner.Options{
		Mule: mule, Stand: b.cfg.Wood.MuleStand,
		RowsInBank: len(rows), Drawn: drawn,
		Load: func() []donorplan.LoadGroup {
			return wcfletchplan.MuleLoad(packRows(), donorplan.DefaultMuleFreeSlots, keepList)
		},
		HaulLeft: func() int { return wcfletchplan.MuleHaul(packRows(), keepList) },
	})

	b.handoffPending = false
	b.tripEnd(ctx, res.Hopping, res.Traded, res.GaveUp)
	switch {
	case res.Hopping && !res.Traded && !res.GaveUp:
		ctx.logf("wcfletch: hand-off: %s - the script restarts on the other world and carries on from there", res.Reason)
	case res.Stopped:
		ctx.logf("wcfletch: hand-off interrupted by a stop request")
	case res.GaveUp:
		b.handoffNext = time.Now().Add(donorplan.HandoffRetry)
		ctx.logf("wcfletch: hand-off: %s — resuming chopping, next attempt no sooner than %s", res.Reason, b.handoffNext.Format(time.Kitchen))
	case res.Traded:
		b.handoffs++
		b.handedOver += res.Slots
		ctx.logf("wcfletch: hand-off #%d complete — %d trade(s), %d pack slot(s) crossed to %q", b.handoffs, res.Loads, res.Slots, mule)
		if rowsLeft > 0 {
			b.handoffPending = true
			ctx.logf("wcfletch: %d bank row(s) still for %q — drawing the next load", rowsLeft, mule)
		}
		if rowsLeft == 0 {
			// At the mule's post with an empty pack: one more trade asks for the walks' jewellery, and costs no walk.
			b.stall.askMuleRub(ctx, mule, 0)
		}
	default:
		ctx.logf("wcfletch: hand-off ended without trading (%s)", res.Reason)
	}
	return nil
}

func (b *wcfletchBot) OnStop(ctx *Context) {
	b.oaks.stop(ctx)
	d := b.deps(ctx)
	rung := "not started"
	if b.haveRung {
		rung = wcfletchplan.Ladder[b.rung].Tier.Label
	}
	// Appended AFTER the fields the fleet watchers already parse, never between them: tools read
	// the rung label and the numbers by position, so a new field in the middle would silently
	// shift every one of them.
	extra := fmt.Sprintf(", %d banked-log draw(s)", b.bankDraws)
	if b.havePatch {
		if ps := wcfletchplan.SharedPatchBoardFor(ctx.World()).Patches(); b.patch >= 0 && b.patch < len(ps) {
			extra += ", rung 1 at " + ps[b.patch].Name
		}
		b.leavePatch(ctx)
	}
	if b.haveCamp {
		if cs := wcfletchplan.SharedCampBoardFor(ctx.World()).Camps(); b.camp >= 0 && b.camp < len(cs) {
			extra += ", maple rung at " + cs[b.camp].Name
		}
		b.leaveCamp(ctx)
	}
	if b.yew.have {
		extra += b.yewStopNote(ctx)
		b.leaveYew(ctx)
	}
	if b.magic.have {
		extra += b.stopNote(ctx, magicSide)
		b.leaveMagic(ctx)
	}
	if b.yewW.assigned != 0 {
		extra += fmt.Sprintf(", yew world %d", b.yewW.assigned)
	}
	state := ""
	if !b.fletching() {
		state = " (fletching OFF — see the log line that says why)"
	}
	ctx.logf("wcfletch: stopped — %s, Woodcutting %d, Fletching %d (+%d xp), %d logs chopped, %d cut into %d unstrung bows/shafts, %d bank trips, %d slots to the mule in %d hand-over(s)%s%s",
		rung, woodcutrunner.WoodcuttingLevel(d), wcfletchrunner.FletchingLevel(d),
		wcfletchrunner.FletchingXP(d)-b.fletchXPAtStart,
		b.chopped, b.logsCut, b.fletched, b.trips, b.handedOver, b.handoffs, state, extra)
}
