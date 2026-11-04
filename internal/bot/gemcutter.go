package bot

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/gemplan"
	"rsb0t-go/internal/scripts/gemrunner"
	"rsb0t-go/internal/settings"
	"rsb0t-go/internal/webwalk"
	"rsb0t-go/internal/webwalk/hops"
	"rsb0t-go/internal/webwalk/live"
)

func init() {
	Register("GemCutter", func() Bot { return &gemCutterBot{} })
}

// GemCutterSettings is the script's declared settings schema, resolved through
// internal/settings so a value arriving as a bare string from a panel is typed, options are
// matched ignoring case, and a missing key takes the default. The keys mirror the TS manifest's
// own settingsSchema (gems) plus this codebase's own conventions.
var GemCutterSettings = settings.Schema{
	"gems": {
		Type:    settings.TypeList,
		Default: []string{},
		Options: gemplan.GemNames(),
		Label:   "Gems to cut",
		Help:    "leave empty to cut every gem the account's Crafting level allows; naming gems restricts the run to those",
	},
	"bankBooth": {
		Type:    settings.TypeString,
		Default: gemplan.BoothName,
		Label:   "Bank booth",
		Help:    "the bank loc's display name",
	},
	"membersWorld": {
		Type:    settings.TypeBoolean,
		Default: true,
		Label:   "Members world",
		Help:    "opal, jade, red topaz and dragonstone are members-only objects the server refuses to cut on a free world; turning this off leaves them out of the run entirely",
	},
	"logDetail": {
		Type:    settings.TypeString,
		Default: "Normal",
		Options: []string{"Normal", "Verbose"},
		Label:   "Log detail",
	},
}

// gemCutterBot ports rsb0t's public-catalogue GemCutter.ts (src/bot/scripts/GemCutter/) to
// this codebase's three-layer recipe — see docs/script_porting_framework.md. Every Loop call
// re-derives the stage from live state (gemplan.DecideStage); the only things remembered
// across calls are what the TS class itself kept as fields: which gems have been denied by a
// refusal, which are confirmed empty in the bank, the level the eligible list was built for,
// and the run counters.
//
// No mule: confirmed by reading the whole TS source — GemCutter.ts imports no donorPlan or
// muleProtocol, and the bank is the only destination for every trip.
type gemCutterBot struct {
	settings     gemplan.Settings
	membersWorld bool

	// walker takes the long way home when the same-level pathfinder cannot plan a route —
	// after a death or a fail-teleport. Nil if webwalk could not be built; the script then
	// degrades to same-level pathfinding rather than failing to start.
	walker *webwalk.Walker

	// denied are gems a real refusal took off the table; confirmedEmpty are gems the bank was
	// observed to hold none of. Both are TS fields (deniedKeys, confirmedEmpty), ported 1:1.
	denied         map[string]bool
	confirmedEmpty map[string]bool
	eligible       []gemplan.Gem
	plannedLevel   int

	stage     string
	cuts      int
	crushed   int
	trips     int
	xpAtStart int
	startedAt time.Time
	warnedAt  map[string]time.Time
}

func (b *gemCutterBot) deps(ctx *Context) *gemrunner.Deps {
	return &gemrunner.Deps{
		Session:    ctx.Session,
		Pack:       ctx.Pack,
		Locs:       ctx.Locs,
		Config:     ctx.Config,
		Components: ctx.Components,
		Chat:       ctx.Chat,
		Delay:      ctx.Delay,
		Log:        ctx.Log,
		Walker:     b.walker,
	}
}

func (b *gemCutterBot) OnStart(ctx *Context) error {
	if ctx.Pack == nil {
		return fmt.Errorf("gemcutter: no collision pack configured (fleet.config.json needs a packPath) — refusing to start rather than panic later inside pathfinding")
	}
	bag := ctx.ResolveSettings(GemCutterSettings)
	b.settings = gemplan.Settings{
		Gems:      bag.List("gems", nil),
		BankBooth: bag.Str("bankBooth", gemplan.BoothName),
		LogDetail: bag.Str("logDetail", "Normal"),
	}.Normalised()
	b.membersWorld = bag.Bool("membersWorld", true)

	// A typo'd gem name would otherwise silently widen the run to every gem, so say so.
	if unknown := gemplan.UnknownGems(ctx.SettingsBag().List("gems", nil)); len(unknown) > 0 {
		ctx.logf("gemcutter: WARNING: ignoring unknown gem name(s) %s — known gems are %s", strings.Join(unknown, ", "), strings.Join(gemplan.GemNames(), ", "))
	}

	// The same construction barbarian.go/crafter.go/fisher.go already use. A failure here is
	// not fatal: the script still works standing at its bank, it just cannot walk home from
	// across the map.
	if w, err := live.NewWalker(live.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
	}); err != nil {
		ctx.logf("gemcutter: no webwalk (%v) — a long walk back to the bank will be limited to same-level pathfinding", err)
	} else {
		if ctx.Components != nil && ctx.Config != nil {
			hops.Install(w, hops.New(hops.Deps{
				Client: w.Client, Session: ctx.Session, Config: ctx.Config,
				Components: ctx.Components, Log: ctx.Log,
			}))
		}
		b.walker = w
	}

	b.denied = map[string]bool{}
	b.confirmedEmpty = map[string]bool{}
	b.warnedAt = map[string]time.Time{}
	b.plannedLevel = -1
	b.startedAt = time.Now()
	b.xpAtStart = gemrunner.CraftingXP(b.deps(ctx))

	which := "every gem the Crafting level allows"
	if len(b.settings.Gems) > 0 {
		which = strings.Join(b.settings.Gems, ", ")
	}
	ctx.logf("gemcutter: cutting %s at the bank at %d,%d (members world: %v)", which, gemplan.BankStand.X, gemplan.BankStand.Z, b.membersWorld)
	return nil
}

func (b *gemCutterBot) vlog(ctx *Context, format string, args ...any) {
	if b.settings.Verbose() {
		ctx.logf("gemcutter: "+format, args...)
	}
}

func (b *gemCutterBot) warn(ctx *Context, key string, every time.Duration, format string, args ...any) {
	if time.Since(b.warnedAt[key]) < every {
		return
	}
	b.warnedAt[key] = time.Now()
	ctx.logf("gemcutter: WARNING: "+format, args...)
}

func (b *gemCutterBot) setStage(ctx *Context, stage gemplan.Stage) {
	if string(stage) != b.stage {
		b.stage = string(stage)
		b.vlog(ctx, "-> %s", gemplan.DescribeStage(stage))
	}
}

// refreshEligible rebuilds the eligible list for the account's current Crafting level, the way
// GemCutter.ts's own refreshEligible does on every level change.
func (b *gemCutterBot) refreshEligible(ctx *Context, level int) {
	b.plannedLevel = level
	b.eligible = gemplan.Eligible(level, b.settings.Gems, b.denied, b.membersWorld)
	names := make([]string, 0, len(b.eligible))
	for _, g := range b.eligible {
		names = append(names, g.Name)
	}
	if len(names) == 0 {
		ctx.logf("gemcutter: Crafting %d allows none of the selected gems", level)
		return
	}
	b.vlog(ctx, "Crafting %d allows: %s", level, strings.Join(names, ", "))
}

func (b *gemCutterBot) Loop(ctx *Context) error {
	d := b.deps(ctx)
	if !gemrunner.WaitForGuardian(d) {
		return nil
	}
	// A death here just drops the account near Lumbridge and the ordinary bank/travel dispatch
	// walks back, the same reasoning every sibling script's doc comment gives. Still clear the
	// one-way dead latch so a dashboard does not show dead=true forever.
	if ctx.Session.State().IsDead() {
		if hp, ok := ctx.Session.State().HP(); ok && hp > 0 {
			ctx.Session.State().ClearDead()
		}
	}
	dlg := actions.NewChatDialog(ctx.Session, ctx.Components)
	if dlg.CanContinue() {
		_ = dlg.Continue()
		ctx.Delay(300 * time.Millisecond)
		return nil
	}

	if level := gemrunner.CraftingLevel(d); level != b.plannedLevel {
		b.refreshEligible(ctx, level)
	}
	targets := gemplan.Targets(b.eligible, b.confirmedEmpty)

	stage := gemplan.DecideStage(gemplan.Snapshot{
		HasChisel:         gemrunner.HasChisel(d),
		EligibleUncutHeld: gemrunner.EligibleUncutHeld(d, b.eligible),
		TargetsRemaining:  len(targets),
	})
	b.setStage(ctx, stage)

	switch stage {
	case gemplan.StageCut:
		b.cutStep(ctx, d)
	case gemplan.StageBank:
		return b.bankStep(ctx, d, targets)
	case gemplan.StageDone:
		// GemCutter.ts stops itself here too. Returning an error is how a Bot ends its own
		// run in this codebase; the Runner reports it as the stop reason.
		return fmt.Errorf("gemcutter: nothing left to cut — the bank holds none of the %d eligible gem kind(s)", len(b.eligible))
	}
	return nil
}

func (b *gemCutterBot) cutStep(ctx *Context, d *gemrunner.Deps) {
	res := gemrunner.Cut(d, b.eligible)
	switch res.Reason {
	case "cut":
		b.cuts += res.Cuts
		b.crushed += res.Crushed
		lootGems(ctx, res.Gem, res.Cuts, res.Crushed)
		b.vlog(ctx, "cut %d %s (+%d XP)%s", res.Cuts, res.Gem.Name, res.XPGained, crushedNote(res.Crushed))
	case "refused":
		// The account cannot cut this gem at its real level, whatever the level read said.
		b.denied[res.Gem.Key] = true
		ctx.logf("gemcutter: Crafting %d required for %s — skipping it for the rest of the run", res.RefusedLevel, res.Gem.Name)
		b.refreshEligible(ctx, gemrunner.CraftingLevel(d))
	case "members":
		b.denied[res.Gem.Key] = true
		ctx.logf("gemcutter: %s is members-only and this world refuses it — skipping it for the rest of the run", res.Gem.Name)
		b.refreshEligible(ctx, gemrunner.CraftingLevel(d))
	case "stalled":
		b.warn(ctx, "cut-stall", 60*time.Second, "chiselling %s produced nothing — retrying", res.Gem.Name)
		ctx.Delay(2 * time.Second)
	case "no-chisel", "no-gems", "stopped":
		// Nothing to do — the next Loop call re-derives the stage from live state.
	}
}

func crushedNote(crushed int) string {
	if crushed <= 0 {
		return ""
	}
	return fmt.Sprintf(", %d smashed", crushed)
}

func (b *gemCutterBot) bankStep(ctx *Context, d *gemrunner.Deps, targets []gemplan.Gem) error {
	bank, ok := gemrunner.OpenBank(d, b.settings.BankBooth)
	if !ok {
		b.warn(ctx, "bank-open", 60*time.Second, "could not reach or open the bank — retrying")
		ctx.Delay(3 * time.Second)
		return nil
	}
	defer func() { _ = bank.Close() }()

	deposited := gemrunner.DepositAllExceptIDs(d, bank, map[int32]bool{gemplan.ChiselID: true})
	if !gemrunner.HasChisel(d) {
		if !gemrunner.WithdrawChisel(d, bank) {
			return fmt.Errorf("gemcutter: no chisel in the pack or the bank — cannot cut anything")
		}
		b.vlog(ctx, "withdrew a chisel")
	}

	withdrew := 0
	for _, gem := range targets {
		free := gemrunner.FreeSlots(d)
		if free <= 0 {
			break
		}
		got := gemrunner.WithdrawUncut(d, bank, gem, free)
		if got == 0 {
			// The bank holds none: stop asking for it, exactly as the TS source's own
			// markEmpty does. Nothing re-checks this during the run.
			b.confirmedEmpty[gem.Key] = true
			b.vlog(ctx, "%s confirmed empty in the bank", gem.UncutName)
			continue
		}
		withdrew += got
		b.vlog(ctx, "withdrew %d %s", got, gem.UncutName)
	}

	if withdrew > 0 || deposited > 0 {
		b.trips++
		ctx.logf("gemcutter: bank trip %d — deposited %d item kind(s), took %d uncut gem(s)", b.trips, deposited, withdrew)
	}
	return nil
}

func (b *gemCutterBot) OnStop(ctx *Context) {
	xp := gemrunner.CraftingXP(b.deps(ctx)) - b.xpAtStart
	ctx.logf("gemcutter: stopped — %d gems cut, %d smashed, %d bank trips, +%d Crafting XP in %s",
		b.cuts, b.crushed, b.trips, xp, time.Since(b.startedAt).Round(time.Second))
}
