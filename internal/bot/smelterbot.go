package bot

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/scripts/smeltplan"
	"rsb0t-go/internal/scripts/smeltrunner"
	"rsb0t-go/internal/settings"
	"rsb0t-go/internal/webwalk"
	"rsb0t-go/internal/webwalk/hops"
	"rsb0t-go/internal/webwalk/live"
)

func init() {
	Register("SmelterBot", func() Bot { return &smelterBot{} })
}

// SmelterBotSettings is the script's declared settings schema, resolved through
// internal/settings so a value arriving as a bare string from a panel is typed, options are
// matched ignoring case, and a missing key takes the default.
//
// The TS manifest's bankStand/furnaceStand/furnaceName/bankBooth/obstacle/leashRadius tile and
// name knobs are deliberately NOT offered here — see smeltplan's own doc comment for why they
// are fixed constants in this port.
var SmelterBotSettings = settings.Schema{
	"bar": {
		Type:    settings.TypeString,
		Default: smeltplan.DefaultSettings().Bar,
		Options: smeltplan.BarNames(),
		Label:   "Bar to smelt",
		Help:    "the withdraw plan and the coal ratio are both derived from this",
	},
	"bankBooth": {
		Type:    settings.TypeString,
		Default: smeltplan.BoothName,
		Label:   "Bank booth",
		Help:    "the bank loc's display name",
	},
	"logDetail": {
		Type:    settings.TypeString,
		Default: "Normal",
		Options: []string{"Normal", "Verbose"},
		Label:   "Log detail",
	},
}

// resolveSmelterSettings turns a raw settings map into the script's own Settings, and also
// returns the bar name exactly as the caller wrote it so a typo can be reported.
//
// The raw value is preferred over the schema-resolved one whenever it names a real recipe.
// That is deliberate and not redundant: the schema's Options list exists so a control-panel
// dropdown offers content's own canonical spellings, but it also REJECTS anything not in that
// list and silently substitutes the default. `fleet.config.json` is hand-written JSON, not a
// dropdown, so a perfectly reasonable `"bar": "steel"` would otherwise become bronze without a
// word — the account would run all day smelting the wrong bar. smeltplan.RecipeFor accepts the
// short and casual spellings; anything it cannot resolve still falls back to the default and is
// reported by the caller through UnknownBar.
func resolveSmelterSettings(ctx *Context) (smeltplan.Settings, string) {
	bag := ctx.ResolveSettings(SmelterBotSettings)
	rawBar := ctx.SettingsBag().Str("bar", "")
	barName := bag.Str("bar", smeltplan.DefaultSettings().Bar)
	if _, ok := smeltplan.RecipeFor(rawBar); ok {
		barName = rawBar
	}
	return smeltplan.Settings{
		Bar:       barName,
		BankBooth: bag.Str("bankBooth", smeltplan.BoothName),
		LogDetail: bag.Str("logDetail", "Normal"),
	}.Normalised(), rawBar
}

// smelterBot ports rsb0t's public-catalogue SmelterBot.ts (src/bot/scripts/SmelterBot/) to this
// codebase's three-layer recipe — see docs/script_porting_framework.md. Every Loop call
// re-derives the stage from live state (smeltplan.DecideStage); the only things remembered
// across calls are what the TS class itself kept as fields — the resolved recipe, the run
// counters — plus the bank-refusal set every sibling port now carries.
//
// No mule: confirmed by reading the whole TS source — SmelterBot.ts imports no donorPlan or
// muleProtocol, and the bank is the only destination for every trip.
type smelterBot struct {
	settings smeltplan.Settings
	recipe   smeltplan.Recipe

	// walker takes the long way home when the same-level pathfinder cannot plan a route —
	// after a death or a fail-teleport. Nil if webwalk could not be built; the script then
	// degrades to same-level pathfinding rather than failing to start.
	walker *webwalk.Walker

	// bankRefused are ids the bank would not accept, remembered so every later trip stops
	// re-trying them and burning the deposit timeout (HerbCleaner's "Strange box" lesson).
	bankRefused map[int32]bool
	// exhausted latches once a bank trip proves the bank cannot fund one more bar.
	exhausted bool

	stage     string
	bars      int
	oreUsed   int
	trips     int
	xpAtStart int
	startedAt time.Time
	warnedAt  map[string]time.Time
}

func (b *smelterBot) deps(ctx *Context) *smeltrunner.Deps {
	return &smeltrunner.Deps{
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

func (b *smelterBot) OnStart(ctx *Context) error {
	if ctx.Pack == nil {
		return fmt.Errorf("smelter: no collision pack configured (fleet.config.json needs a packPath) — refusing to start rather than panic later inside pathfinding")
	}
	var rawBar string
	b.settings, rawBar = resolveSmelterSettings(ctx)
	b.recipe = b.settings.Recipe()

	// A typo'd bar would otherwise silently become bronze, so say so.
	if name, unknown := smeltplan.UnknownBar(rawBar); unknown {
		ctx.logf("smelter: WARNING: %q is not a bar this content build smelts — falling back to %s. Known bars: %v", name, b.settings.Bar, smeltplan.BarNames())
	}

	// The same construction gemcutter.go/barbarian.go/fisher.go already use. A failure here is
	// not fatal: the script still works between its two tiles, it just cannot walk home from
	// across the map.
	if w, err := live.NewWalker(live.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
	}); err != nil {
		ctx.logf("smelter: no webwalk (%v) — a long walk back to Al Kharid will be limited to same-level pathfinding", err)
	} else {
		if ctx.Components != nil && ctx.Config != nil {
			hops.Install(w, hops.New(hops.Deps{
				Client: w.Client, Session: ctx.Session, Config: ctx.Config,
				Components: ctx.Components, Log: ctx.Log,
			}))
		}
		b.walker = w
	}

	b.bankRefused = map[int32]bool{}
	b.warnedAt = map[string]time.Time{}
	b.startedAt = time.Now()
	b.xpAtStart = smeltrunner.SmithingXP(b.deps(ctx))

	if level := smeltrunner.SmithingLevel(b.deps(ctx)); level > 0 && level < b.recipe.Level {
		ctx.logf("smelter: WARNING: %s needs Smithing %d and this account has %d — the furnace will refuse every attempt until it levels up", b.recipe.Bar, b.recipe.Level, level)
	}
	ctx.logf("smelter: smelting %s (%s) at the Al Kharid furnace %d,%d, banking at %d,%d",
		b.recipe.Bar, describeRecipe(b.recipe),
		smeltplan.FurnaceStand.X, smeltplan.FurnaceStand.Z, smeltplan.BankStand.X, smeltplan.BankStand.Z)
	return nil
}

// describeRecipe renders the ore cost of one bar for the start-up log.
func describeRecipe(r smeltplan.Recipe) string {
	if !r.HasSecondary() {
		return fmt.Sprintf("%d %s", r.Primary.Per, r.Primary.Name)
	}
	return fmt.Sprintf("%d %s + %d %s", r.Primary.Per, r.Primary.Name, r.Secondary.Per, r.Secondary.Name)
}

func (b *smelterBot) vlog(ctx *Context, format string, args ...any) {
	if b.settings.Verbose() {
		ctx.logf("smelter: "+format, args...)
	}
}

func (b *smelterBot) warn(ctx *Context, key string, every time.Duration, format string, args ...any) {
	if time.Since(b.warnedAt[key]) < every {
		return
	}
	b.warnedAt[key] = time.Now()
	ctx.logf("smelter: WARNING: "+format, args...)
}

func (b *smelterBot) setStage(ctx *Context, stage smeltplan.Stage) {
	if string(stage) != b.stage {
		b.stage = string(stage)
		b.vlog(ctx, "-> %s", smeltplan.DescribeStage(stage))
	}
}

func (b *smelterBot) Loop(ctx *Context) error {
	d := b.deps(ctx)
	if !smeltrunner.WaitForGuardian(d) {
		return nil
	}
	// A death here drops the account near Lumbridge and the ordinary bank/travel dispatch walks
	// back, the same reasoning every sibling script's doc comment gives. Still clear the
	// one-way dead latch so a dashboard does not show dead=true forever.
	if ctx.Session.State().IsDead() {
		if hp, ok := ctx.Session.State().HP(); ok && hp > 0 {
			ctx.Session.State().ClearDead()
		}
	}
	// A level-up dialogue blocks everything, but the smelting menu is itself a chat modal that
	// reports CanContinue — so never "continue" past a make menu.
	dlg := actions.NewChatDialog(ctx.Session, ctx.Components)
	if dlg.CanContinue() && !dlg.IsMakeMenu() {
		_ = dlg.Continue()
		ctx.Delay(300 * time.Millisecond)
		return nil
	}

	stage := smeltplan.DecideStage(smeltplan.Snapshot{
		CanSmelt:      smeltrunner.CanSmelt(d, b.recipe),
		BankExhausted: b.exhausted,
	})
	b.setStage(ctx, stage)

	switch stage {
	case smeltplan.StageSmelt:
		b.smeltStep(ctx, d)
	case smeltplan.StageBank:
		b.bankStep(ctx, d)
	case smeltplan.StageDone:
		// SmelterBot.ts stops itself here too. Returning an error is how a Bot ends its own run
		// in this codebase; the Runner reports it as the stop reason.
		return fmt.Errorf("smelter: the bank cannot fund even one more %s", b.recipe.Bar)
	}
	return nil
}

func (b *smelterBot) smeltStep(ctx *Context, d *smeltrunner.Deps) {
	res := smeltrunner.Smelt(d, b.recipe)
	switch res.Reason {
	case "smelted":
		b.bars += res.Bars
		b.oreUsed += res.OreUsed
		// The Loot panel's Smithing card: one event an ore used (iron's failed smelts too, which
		// is what makes them a loss on the card), the bars made.
		lootMade(ctx, loot.Smithing(int(b.recipe.BarID), b.recipe.Primary.Name), res.OreUsed,
			loot.Product(int(b.recipe.BarID), b.recipe.Bar, res.Bars))
		// Iron legitimately loses about half its ore to content's "too impure" coin flip, so a
		// bar count below the ore count is not a fault worth warning about there.
		note := ""
		if b.recipe.FailsRandomly() && res.OreUsed > res.Bars {
			note = fmt.Sprintf(" (%d ore failed — content gives iron a 50%% chance without a ring of forging)", res.OreUsed-res.Bars)
		}
		b.vlog(ctx, "smelted %d %s from %d ore%s", res.Bars, b.recipe.Bar, res.OreUsed, note)
	case "no-furnace":
		b.warn(ctx, "no-furnace", 60*time.Second, "could not reach the furnace — retrying")
		ctx.Delay(3 * time.Second)
	case "no-menu":
		b.warn(ctx, "no-menu", 60*time.Second, "the smelting menu did not open or did not offer %s — retrying", b.recipe.Bar)
		ctx.Delay(2 * time.Second)
	case "stalled":
		b.warn(ctx, "stall", 60*time.Second, "the furnace consumed no ore — this is what a Smithing level below %d looks like", b.recipe.Level)
		ctx.Delay(3 * time.Second)
	case "no-ore", "stopped":
		// Nothing to do — the next Loop call re-derives the stage from live state.
	}
}

func (b *smelterBot) bankStep(ctx *Context, d *smeltrunner.Deps) {
	keep := map[int32]bool{}
	for _, in := range b.recipe.Ingredients() {
		keep[in.ID] = true
	}
	for id := range b.bankRefused {
		keep[id] = true
	}

	res, ok := smeltrunner.Restock(d, b.settings.BankBooth, b.recipe, keep)
	if !ok {
		b.warn(ctx, "bank-open", 60*time.Second, "could not reach or open the bank — retrying")
		ctx.Delay(3 * time.Second)
		return
	}
	for _, id := range res.Refused {
		if !b.bankRefused[id] {
			b.bankRefused[id] = true
			name := fmt.Sprintf("object %d", id)
			if ctx.Config != nil {
				if obj, found := ctx.Config.Obj(int(id)); found {
					name = obj.Name
				}
			}
			ctx.logf("smelter: the bank refused %s — it will be left in the pack from now on instead of re-trying every trip", name)
		}
	}
	b.trips++
	ctx.logf("smelter: bank trip %d — banked %d item kind(s), took %d ore", b.trips, res.Deposited, res.Withdrew)
	if res.Exhausted {
		b.exhausted = true
	}
}

func (b *smelterBot) OnStop(ctx *Context) {
	xp := smeltrunner.SmithingXP(b.deps(ctx)) - b.xpAtStart
	ctx.logf("smelter: stopped — %d %s from %d ore in %d bank trips, +%d Smithing XP in %s",
		b.bars, b.recipe.Bar, b.oreUsed, b.trips, xp, time.Since(b.startedAt).Round(time.Second))
}
