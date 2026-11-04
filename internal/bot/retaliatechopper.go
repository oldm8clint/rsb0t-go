package bot

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/retaliateplan"
	"rsb0t-go/internal/scripts/retaliaterunner"
	"rsb0t-go/internal/scripts/woodcutrunner"
	"rsb0t-go/internal/webwalk"
	"rsb0t-go/internal/webwalk/hops"
	"rsb0t-go/internal/webwalk/live"
)

func init() {
	Register("RetaliateChopper", func() Bot { return &retaliateChopperBot{} })
}

// retaliateChopperBot is the two-tick oak chopper of docs/two_tick_retaliate_research_2026-11-04.md: a shortbow worn with no
// ammo, auto-retaliate on, the Rapid style, and two Falador farm rats hitting the account exactly 2 ticks apart, so that a Clear
// then a tree Click written on alternate ticks make the oak roll every 2 ticks instead of 4. One account at a time: the site
// has two rats. Its own script, not a mode of another: it must never run beside chopManip (the knife trick and the flinch both
// write %action_delay), so it refuses to start when the settings carry chopManip.
//
// The decisions are in internal/scripts/retaliateplan, the walking, wielding, casting and the tick loop in
// internal/scripts/retaliaterunner. Settings (docs/bot_script_settings_additional.md, "RetaliateChopper"), all optional:
//
//	site          string default "falador-oaks"      the site table.
//	stand         string default ""                  stand tile "x,z"; empty picks one from the collision pack.
//	clear         string default "drop"              "drop" a log as the clear, or "button" (auto-retaliate On).
//	full          string default "drop"              pack full: "drop" the logs, or "bank" them at Falador east.
//	aggro         string default "confuse,weaken,curse"  spells that wake the rats, first affordable wins.
//	phaseTries    int    default 6                   re-phase tries before the fallback chop.
//	minGain       float  default 0.7                 share of 2-tick roll gaps to keep going.
//	lead          int    default 1                   starting write lead in ticks (0 to 2); it adapts.
//	stopHPPercent int    default 70                  stop below this share of max hitpoints.
//	fallback      string default "knife"             "knife" or "plain" chop when the rats cannot be phased.
//	fallbackMins  int    default 10                  minutes of fallback before trying again.
//	reportEvery   int    default 300                 ticks between rolling log lines.
//	logDetail     string default "Normal"            "Verbose" logs every setup decision.
type retaliateChopperBot struct {
	run      *retaliaterunner.Runner
	settings retaliateplan.Settings
	walker   *webwalk.Walker
	stopped  string
}

// retaliateChopperSettings reads the settings map with the defaults of retaliateplan.DefaultSettings.
func retaliateChopperSettings(m map[string]interface{}) retaliateplan.Settings {
	d := retaliateplan.DefaultSettings()
	return retaliateplan.Settings{
		Site:          StringSetting(m, "site", d.Site),
		Stand:         StringSetting(m, "stand", d.Stand),
		Clear:         StringSetting(m, "clear", d.Clear),
		Full:          StringSetting(m, "full", d.Full),
		Aggro:         StringSetting(m, "aggro", d.Aggro),
		PhaseTries:    IntSetting(m, "phaseTries", d.PhaseTries),
		MinGain:       FloatSetting(m, "minGain", d.MinGain),
		Lead:          IntSetting(m, "lead", d.Lead),
		StopHPPercent: IntSetting(m, "stopHPPercent", d.StopHPPercent),
		Fallback:      StringSetting(m, "fallback", d.Fallback),
		FallbackMins:  IntSetting(m, "fallbackMins", d.FallbackMins),
		ReportEvery:   IntSetting(m, "reportEvery", d.ReportEvery),
		LogDetail:     StringSetting(m, "logDetail", d.LogDetail),
	}.Normalised()
}

func (b *retaliateChopperBot) deps(ctx *Context) *woodcutrunner.Deps {
	return &woodcutrunner.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config, Components: ctx.Components,
		Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log, Loot: ctx.Loot, World: ctx.World(), Walker: b.walker,
	}
}

func (b *retaliateChopperBot) OnStart(ctx *Context) error {
	if BoolSetting(ctx.Settings, "chopManip", false) {
		b.stopped = "chopManip is set: the knife trick and the retaliate loop both write %action_delay and must never run on one account"
		ctx.logf("retaliate: STOPPED: %s", b.stopped)
		return nil
	}
	if ctx.Pack == nil || ctx.Locs == nil || ctx.Config == nil {
		b.stopped = "no collision pack, loc index or config: the stand tile cannot be chosen"
		ctx.logf("retaliate: STOPPED: %s", b.stopped)
		return nil
	}
	b.settings = retaliateChopperSettings(ctx.Settings)
	b.run = retaliaterunner.New(b.settings)
	// The rats put the account in combat all the time; without this the guardian flees them.
	if ctx.Guardian != nil {
		ctx.Guardian.CombatExpected = true
	}
	// The door-aware walker, built the way ProgressiveWoodcutter builds it (the Falador gate and the farm fences have doors).
	if w, err := live.NewWalker(live.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
	}); err != nil {
		ctx.logf("retaliate: no webwalk (%v): walks use the plain pathfinder", err)
	} else {
		if ctx.Components != nil && ctx.Config != nil {
			hops.Install(w, hops.New(hops.Deps{
				Client: w.Client, Session: ctx.Session, Config: ctx.Config,
				Components: ctx.Components, Log: ctx.Log,
			}))
		}
		b.walker = w
	}
	b.run.Start(b.deps(ctx))
	ctx.logf("retaliate: starting at the %s site: clear by %s, full pack: %s, aggro [%s], %d phase tries, fallback %s for %d min, stop below %d%% hitpoints",
		b.settings.Site, b.settings.Clear, b.settings.Full, strings.Join(b.settings.AggroList(), ", "), b.settings.PhaseTries, b.settings.Fallback, b.settings.FallbackMins, b.settings.StopHPPercent)
	return nil
}

func (b *retaliateChopperBot) Loop(ctx *Context) error {
	if b.stopped != "" || b.run == nil || b.run.Stopped() != "" {
		ctx.Delay(3 * time.Second)
		return nil
	}
	b.run.Step(b.deps(ctx))
	return nil
}

func (b *retaliateChopperBot) OnStop(ctx *Context) {
	if b.run == nil {
		ctx.logf("retaliate: stopped: %s", b.stopped)
		return
	}
	ctx.logf("retaliate: stopped (%s phase): %s", b.run.Phase(), b.run.Summary(b.deps(ctx)))
	if s := b.run.Stopped(); s != "" {
		ctx.logf("retaliate: reason: %s", s)
	}
}
