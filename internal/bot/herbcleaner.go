package bot

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/herbplan"
	"rsb0t-go/internal/scripts/herbrunner"
	"rsb0t-go/internal/settings"
	"rsb0t-go/internal/webwalk"
	"rsb0t-go/internal/webwalk/hops"
	"rsb0t-go/internal/webwalk/live"
)

func init() {
	Register("HerbCleaner", func() Bot { return &herbCleanerBot{} })
}

// HerbCleanerSettings is the script's declared settings schema, resolved through
// internal/settings so a value arriving as a bare string from a panel is typed, options are
// matched ignoring case, and a missing key takes the default. The keys mirror the TS manifest's
// own settingsSchema (herbs) plus this codebase's own conventions.
var HerbCleanerSettings = settings.Schema{
	"herbs": {
		Type:    settings.TypeList,
		Default: []string{},
		Options: herbplan.HerbNames(),
		Label:   "Herbs to clean",
		Help:    "leave empty to clean every herb the account's Herblore level allows; naming herbs restricts the run to those. The pack is emptied into the bank every cycle, including non-herbs, so start with nothing valuable carried",
	},
	"bankBooth": {
		Type:    settings.TypeString,
		Default: herbplan.BoothName,
		Label:   "Bank booth",
		Help:    "the bank loc's display name",
	},
	"membersWorld": {
		Type:    settings.TypeBoolean,
		Default: true,
		Label:   "Members world",
		Help:    "every herb is a members object and content refuses to identify any of them on a free world, before it even checks the level; turning this off leaves the run with nothing to do, which is the honest answer rather than a loop that spins",
	},
	"logDetail": {
		Type:    settings.TypeString,
		Default: "Normal",
		Options: []string{"Normal", "Verbose"},
		Label:   "Log detail",
	},
}

// herbCleanerBot ports rsb0t's public-catalogue HerbCleaner.ts (src/bot/scripts/HerbCleaner/)
// to this codebase's three-layer recipe — see docs/script_porting_framework.md. Every Loop call
// re-derives the stage from live state (herbplan.DecideStage); the only things remembered across
// calls are what the TS class itself kept as fields: which herbs have been denied by a refusal,
// which are confirmed empty in the bank, the level the eligible list was built for, and the run
// counters.
//
// No mule: confirmed by reading the whole TS source — HerbCleaner.ts imports no donorPlan or
// muleProtocol, and the bank is the only destination for every trip.
type herbCleanerBot struct {
	settings     herbplan.Settings
	membersWorld bool

	// walker takes the long way home when the same-level pathfinder cannot plan a route — after
	// a death or a fail-teleport. Nil if webwalk could not be built; the script then degrades to
	// same-level pathfinding rather than failing to start.
	walker *webwalk.Walker

	// denied are herbs a real refusal took off the table; confirmedEmpty are herbs the bank was
	// observed to hold none of. Both are TS fields (deniedKeys, confirmedEmpty), ported 1:1.
	denied         map[string]bool
	confirmedEmpty map[string]bool
	// undepositable are object ids the bank refused to take, remembered so a bank trip stops
	// re-trying them. Not a TS field: it exists because a live run collected eight "Strange box"
	// random-event items (object 3062, content debugname `macro_cube`) that cost a 5s timeout on
	// EVERY trip and held eight pack slots hostage, cutting each load from 28 herbs to 20. Owned
	// here rather than in the runner so it survives across bank trips, the same way donorrunner's
	// KnownUntradable is owned by its bot.
	undepositable map[int32]bool
	eligible      []herbplan.Herb
	plannedLevel  int

	stage     string
	cleaned   int
	trips     int
	xpAtStart int
	startedAt time.Time
	warnedAt  map[string]time.Time
}

func (b *herbCleanerBot) deps(ctx *Context) *herbrunner.Deps {
	return &herbrunner.Deps{
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

// droppedHerbNames lists the raw "herbs" setting entries that did not survive into the resolved
// run, so OnStart can report them. See the call site for the two distinct ways an entry is lost.
func droppedHerbNames(raw, kept []string) []string {
	keptSet := map[string]bool{}
	for _, name := range kept {
		keptSet[strings.ToLower(name)] = true
	}
	var dropped []string
	for _, name := range raw {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if h, ok := herbplan.HerbByName(trimmed); ok && keptSet[strings.ToLower(h.Name)] {
			continue
		}
		dropped = append(dropped, trimmed)
	}
	return dropped
}

func (b *herbCleanerBot) OnStart(ctx *Context) error {
	if ctx.Pack == nil {
		return fmt.Errorf("herbcleaner: no collision pack configured (fleet.config.json needs a packPath) — refusing to start rather than panic later inside pathfinding")
	}
	bag := ctx.ResolveSettings(HerbCleanerSettings)
	b.settings = herbplan.Settings{
		Herbs:     bag.List("herbs", nil),
		BankBooth: bag.Str("bankBooth", herbplan.BoothName),
		LogDetail: bag.Str("logDetail", "Normal"),
	}.Normalised()
	b.membersWorld = bag.Bool("membersWorld", true)

	// A herb name the run did not actually take is worth saying out loud, because the silent
	// outcome is a run quietly WIDER than the operator asked for: an empty list means "clean
	// everything". There are two ways a name gets dropped and this catches both — a plain typo,
	// and a bare table key like "ranarr" instead of the display name "Ranarr weed", which the
	// settings schema discards upstream of this script because it matches list values against
	// its own Options (the display names). UnknownHerbs alone would miss the second case, since
	// HerbByName accepts keys that the schema has already thrown away.
	if dropped := droppedHerbNames(ctx.SettingsBag().List("herbs", nil), b.settings.Herbs); len(dropped) > 0 {
		ctx.logf("herbcleaner: WARNING: ignoring herb name(s) %s — spell them exactly as one of: %s", strings.Join(dropped, ", "), strings.Join(herbplan.HerbNames(), ", "))
	}
	if !b.membersWorld {
		return fmt.Errorf("herbcleaner: every herb is a members object and content refuses to identify any of them on a free world — nothing this script does is possible with membersWorld false")
	}

	// The same construction barbarian.go/crafter.go/gemcutter.go already use. A failure here is
	// not fatal: the script still works standing at its bank, it just cannot walk home from
	// across the map.
	if w, err := live.NewWalker(live.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
	}); err != nil {
		ctx.logf("herbcleaner: no webwalk (%v) — a long walk back to the bank will be limited to same-level pathfinding", err)
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
	b.undepositable = map[int32]bool{}
	b.warnedAt = map[string]time.Time{}
	b.plannedLevel = -1
	b.startedAt = time.Now()
	b.xpAtStart = herbrunner.HerbloreXP(b.deps(ctx))

	which := "every herb the Herblore level allows"
	if len(b.settings.Herbs) > 0 {
		which = strings.Join(b.settings.Herbs, ", ")
	}
	ctx.logf("herbcleaner: cleaning %s at the bank at %d,%d", which, herbplan.BankStand.X, herbplan.BankStand.Z)
	return nil
}

func (b *herbCleanerBot) vlog(ctx *Context, format string, args ...any) {
	if b.settings.Verbose() {
		ctx.logf("herbcleaner: "+format, args...)
	}
}

func (b *herbCleanerBot) warn(ctx *Context, key string, every time.Duration, format string, args ...any) {
	if time.Since(b.warnedAt[key]) < every {
		return
	}
	b.warnedAt[key] = time.Now()
	ctx.logf("herbcleaner: WARNING: "+format, args...)
}

func (b *herbCleanerBot) setStage(ctx *Context, stage herbplan.Stage) {
	if string(stage) != b.stage {
		b.stage = string(stage)
		b.vlog(ctx, "-> %s", herbplan.DescribeStage(stage))
	}
}

// refreshEligible rebuilds the eligible list for the account's current Herblore level, the way
// HerbCleaner.ts's own refreshEligible does on every level change. This matters more here than
// for most scripts: cleaning herbs levels Herblore quickly at low levels, so a run that froze its
// list at start would never pick up the next herb it unlocked.
func (b *herbCleanerBot) refreshEligible(ctx *Context, level int) {
	b.plannedLevel = level
	b.eligible = herbplan.Eligible(level, b.settings.Herbs, b.denied, b.membersWorld)
	names := make([]string, 0, len(b.eligible))
	for _, h := range b.eligible {
		names = append(names, h.Name)
	}
	if len(names) == 0 {
		ctx.logf("herbcleaner: Herblore %d allows none of the selected herbs", level)
		return
	}
	b.vlog(ctx, "Herblore %d allows: %s", level, strings.Join(names, ", "))
}

func (b *herbCleanerBot) Loop(ctx *Context) error {
	d := b.deps(ctx)
	if !herbrunner.WaitForGuardian(d) {
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

	if level := herbrunner.HerbloreLevel(d); level != b.plannedLevel {
		b.refreshEligible(ctx, level)
	}
	targets := herbplan.Targets(b.eligible, b.confirmedEmpty)

	stage := herbplan.DecideStage(herbplan.Snapshot{
		EligibleUnidsHeld: herbrunner.EligibleUnidsHeld(d, b.eligible),
		TargetsRemaining:  len(targets),
	})
	b.setStage(ctx, stage)

	switch stage {
	case herbplan.StageClean:
		b.cleanStep(ctx, d)
	case herbplan.StageBank:
		return b.bankStep(ctx, d, targets)
	case herbplan.StageDone:
		// HerbCleaner.ts stops itself here too ("every selected herb is empty in the bank").
		// Returning an error is how a Bot ends its own run in this codebase; the Runner reports
		// it as the stop reason.
		return fmt.Errorf("herbcleaner: nothing left to clean — the bank holds none of the %d eligible herb kind(s)", len(b.eligible))
	}
	return nil
}

func (b *herbCleanerBot) cleanStep(ctx *Context, d *herbrunner.Deps) {
	res := herbrunner.Clean(d, b.eligible)
	switch res.Reason {
	case "cleaned":
		b.cleaned += res.Cleaned
		// The XP figure is reported, never relied on: the server floors a possibly fractional
		// award, so it can read 2 or 3 for the same herb. See herbplan's package doc comment.
		b.vlog(ctx, "identified %d %s (Herblore XP +%d)", res.Cleaned, res.Herb.Name, res.XPGained)
	case "refused":
		// The account cannot identify this herb at its real level, whatever the level read said.
		// Content prints no required level, so the herb that was clicked is what gets denied.
		b.denied[res.Herb.Key] = true
		ctx.logf("herbcleaner: Herblore %d required for %s — skipping it for the rest of the run", res.Herb.Level, res.Herb.Name)
		b.refreshEligible(ctx, herbrunner.HerbloreLevel(d))
	case "members":
		b.denied[res.Herb.Key] = true
		ctx.logf("herbcleaner: %s cannot be identified on a free world — skipping it for the rest of the run", res.Herb.Name)
		b.refreshEligible(ctx, herbrunner.HerbloreLevel(d))
	case "stalled":
		b.warn(ctx, "clean-stall", 60*time.Second, "identifying %s produced nothing — retrying", res.Herb.Name)
		ctx.Delay(2 * time.Second)
	case "no-herbs", "stopped":
		// Nothing to do — the next Loop call re-derives the stage from live state.
	}
}

func (b *herbCleanerBot) bankStep(ctx *Context, d *herbrunner.Deps, targets []herbplan.Herb) error {
	bank, ok := herbrunner.OpenBank(d, b.settings.BankBooth)
	if !ok {
		b.warn(ctx, "bank-open", 60*time.Second, "could not reach or open the bank — retrying")
		ctx.Delay(3 * time.Second)
		return nil
	}
	defer func() { _ = bank.Close() }()

	// Nothing is kept by design: identifying needs no tool, which is why this script has no
	// equivalent of GemCutter's chisel, and HerbCleaner.ts deposits the whole pack the same way.
	// The one exception is built from live experience rather than intent — see below.
	deposited, stuck := herbrunner.DepositAllExceptIDs(d, bank, b.undepositable)
	for _, id := range stuck {
		if b.undepositable[id] {
			continue
		}
		b.undepositable[id] = true
		name := fmt.Sprintf("object %d", id)
		if obj, ok := ctx.Config.Obj(int(id)); ok && obj.Name != "" {
			name = fmt.Sprintf("%q (object %d)", obj.Name, id)
		}
		ctx.logf("herbcleaner: %s will not leave the pack — not retrying it on later trips", name)
	}

	withdrew := 0
	for _, herb := range targets {
		free := herbrunner.FreeSlots(d)
		if free <= 0 {
			break
		}
		got := herbrunner.WithdrawUnid(d, bank, herb, free)
		if got == 0 {
			// The bank holds none: stop asking for it, exactly as the TS source's own markEmpty
			// does. Nothing re-checks this during the run — ported as-is.
			b.confirmedEmpty[herb.Key] = true
			b.vlog(ctx, "grimy %s confirmed empty in the bank", herb.Name)
			continue
		}
		withdrew += got
		b.vlog(ctx, "withdrew %d grimy %s", got, herb.Name)
	}

	if withdrew > 0 || deposited > 0 {
		b.trips++
		ctx.logf("herbcleaner: bank trip %d — deposited %d item kind(s), took %d grimy herb(s)", b.trips, deposited, withdrew)
	}
	return nil
}

func (b *herbCleanerBot) OnStop(ctx *Context) {
	xp := herbrunner.HerbloreXP(b.deps(ctx)) - b.xpAtStart
	ctx.logf("herbcleaner: stopped — %d herbs identified, %d bank trips, +%d Herblore XP in %s",
		b.cleaned, b.trips, xp, time.Since(b.startedAt).Round(time.Second))
}
