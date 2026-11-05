package bot

// ProgressiveWCFletch's 2-TICK OAKS (a user request: "ProgressiveWCFletch gets 2-tick oaks", reusing retaliateChopperBot, the chopper
// master's oak round runs; then, the same day: "ensure the wcfletch script is tick manipulating throughout the levels and utilizing 2t oaks
// to boost wc levels significantly ... might not beable to be done by all accounts at once but theres 3 worlds").
//
// twoTickOaks has three readings:
//
//	absent or "auto"   LEVEL-DRIVEN (the default): from Woodcutting 15 an account chops the Falador farm's oaks on the rats' two-tick rhythm
//	                   while wcfletchplan.DecideOaks says the oaks pay more Woodcutting experience an hour than its own ladder rung with
//	                   the knife manipulation, and its world has a free slot (the farm has two rats, so ONE chopper a world, three worlds
//	                   three choppers). A stint lasts twoTickOakStintMinutes (120), then the account rests twoTickOakRestMinutes (360) so
//	                   the others have their turn. The chopper needs a strung shortbow the account can wield; when it cannot start or stops
//	                   for any reason the account rests and the script carries on as before.
//	true or "window"   the old fixed window: twoTickOakMinutes (120) of every twoTickOakEveryMinutes (1,440), whatever the level.
//	false or "off"     never.
//
//	twoTickOakSlots     int   default 1    oak choppers one world holds at once.
//	twoTickOakMaxLevel  int   default 0    stop the oaks at this Woodcutting level whatever the model says (0: the model alone).
//	twoTickOakMargin    float default 1.15 how many times the rung's experience an hour the oaks must pay.
//	twoTickOakRealised  float default 0.7  the share of the oaks' rolls that land on the rhythm (the chopper's own minGain).
//
// The slot board is in-process (wcfletchplan.ClaimedOaks), so it holds for every account of this process on a world. The day's clock of the
// window mode is kept in memory: a restart gives the allowance again (master's own schedule keeps its clock in the ledger). Grep
// "wcfletch: 2-tick oaks" in the log.

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/wcfletchplan"
	"rsb0t-go/internal/scripts/woodcutplan"
	"rsb0t-go/internal/scripts/woodcutrunner"
)

// wcfletchOaksMinWoodcutting is the Woodcutting level an oak needs.
const wcfletchOaksMinWoodcutting = wcfletchplan.OakMinWoodcutting

// wcfletchOaks is the oak rule's state.
type wcfletchOaks struct {
	mode          wcfletchplan.OakMode
	window, every time.Duration
	set           wcfletchplan.OakSettings
	// target is the ladder's target tree (the settings'), for the model's rung.
	target string

	chop     *retaliateChopperBot
	dayStart time.Time
	used     time.Duration
	last     time.Time
	// since is when the auto stint began and restUntil the end of its rest; lastWhy throttles the log.
	since, restUntil time.Time
	world            int
	lastWhy          string
	lastWhyAt        time.Time
}

// oakMode reads twoTickOaks: a bool keeps its old meaning (true: the fixed window, false: never), a string names the mode, absent is auto.
func oakMode(m map[string]interface{}) wcfletchplan.OakMode {
	switch v := m["twoTickOaks"].(type) {
	case bool:
		if v {
			return wcfletchplan.OakWindow
		}
		return wcfletchplan.OakOff
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "window", "true", "on":
			return wcfletchplan.OakWindow
		case "off", "false", "never":
			return wcfletchplan.OakOff
		}
	}
	return wcfletchplan.OakAuto
}

// configure reads the settings (see the file comment).
func (o *wcfletchOaks) configure(m map[string]interface{}) {
	o.mode = oakMode(m)
	o.window = time.Duration(IntSetting(m, "twoTickOakMinutes", 120)) * time.Minute
	o.every = time.Duration(IntSetting(m, "twoTickOakEveryMinutes", 1440)) * time.Minute
	if o.mode == wcfletchplan.OakWindow && (o.window <= 0 || o.every <= 0 || o.window > o.every) {
		o.mode = wcfletchplan.OakOff
	}
	d := wcfletchplan.DefaultOakSettings()
	o.set = wcfletchplan.OakSettings{
		Mode:     o.mode,
		Stint:    time.Duration(IntSetting(m, "twoTickOakStintMinutes", int(d.Stint/time.Minute))) * time.Minute,
		Rest:     time.Duration(IntSetting(m, "twoTickOakRestMinutes", int(d.Rest/time.Minute))) * time.Minute,
		Slots:    IntSetting(m, "twoTickOakSlots", d.Slots),
		MaxLevel: IntSetting(m, "twoTickOakMaxLevel", d.MaxLevel),
		Realised: FloatSetting(m, "twoTickOakRealised", d.Realised),
		Margin:   FloatSetting(m, "twoTickOakMargin", d.Margin),
	}
	if o.set.Stint <= 0 {
		o.set.Stint = d.Stint
	}
	if o.set.Rest < 0 {
		o.set.Rest = d.Rest
	}
	if o.set.Slots < 1 {
		o.set.Slots = d.Slots
	}
	if o.set.Realised <= 0 || o.set.Realised > 1 {
		o.set.Realised = d.Realised
	}
	if o.set.Margin <= 0 {
		o.set.Margin = d.Margin
	}
}

// step runs the chopper for this pass while the rule allows it, and reports whether it did (the caller returns).
func (o *wcfletchOaks) step(ctx *Context, d *woodcutrunner.Deps) bool {
	switch o.mode {
	case wcfletchplan.OakAuto:
		return o.stepAuto(ctx, d)
	case wcfletchplan.OakWindow:
		return o.stepWindow(ctx, d)
	}
	return false
}

// say logs a decision when it changes, and at most every ten minutes when it does not.
func (o *wcfletchOaks) say(ctx *Context, why string) {
	if why == o.lastWhy && time.Since(o.lastWhyAt) < 10*time.Minute {
		return
	}
	o.lastWhy, o.lastWhyAt = why, time.Now()
	ctx.logf("wcfletch: 2-tick oaks: %s", why)
}

// stepAuto is the level-driven rule (wcfletchplan.DecideOaks).
func (o *wcfletchOaks) stepAuto(ctx *Context, d *woodcutrunner.Deps) bool {
	now := time.Now()
	o.world = ctx.World()
	stopped := ""
	if o.chop != nil {
		stopped = o.chop.stopped
		if stopped == "" && o.chop.run != nil {
			stopped = o.chop.run.Stopped()
		}
	}
	if stopped != "" {
		ctx.logf("wcfletch: 2-tick oaks: the chopper stopped (%s): back to chopping and fletching, resting from the oaks for %s", stopped, o.set.Rest)
		o.endStint(ctx, now)
		return false
	}
	dec := wcfletchplan.DecideOaks(o.set, wcfletchplan.OakAsk{
		Level: woodcutrunner.WoodcuttingLevel(d), Axe: woodcutplan.BestAxe(woodcutrunner.HeldAxes(d)), Target: o.target,
		World: o.world, Now: now, Holding: o.chop != nil, Since: o.since, RestUntil: o.restUntil, Account: ctx.Username, Board: wcfletchplan.ClaimedOaks,
	})
	switch dec.Action {
	case wcfletchplan.OakStop:
		ctx.logf("wcfletch: 2-tick oaks: %s: back to chopping and fletching, resting for %s", dec.Why, o.set.Rest)
		o.endStint(ctx, now)
		return false
	case wcfletchplan.OakChop:
		if o.chop != nil {
			_ = o.chop.Loop(ctx)
			return true
		}
		ctx.logf("wcfletch: 2-tick oaks: %s", dec.Why)
		o.chop, o.since = &retaliateChopperBot{}, now
		if err := o.chop.OnStart(ctx); err != nil {
			ctx.logf("wcfletch: 2-tick oaks: the chopper did not start: %v", err)
			o.endStint(ctx, now)
			return false
		}
		return true
	}
	o.say(ctx, dec.Why)
	return false
}

// endStint stops the chopper, gives the slot back and starts the rest.
func (o *wcfletchOaks) endStint(ctx *Context, now time.Time) {
	if o.chop != nil {
		o.chop.OnStop(ctx)
		if ctx.Guardian != nil {
			ctx.Guardian.CombatExpected = false
		}
		o.chop = nil
	}
	wcfletchplan.ClaimedOaks.Release(o.world, ctx.Username)
	o.restUntil = now.Add(o.set.Rest)
}

// stepWindow is the fixed daily window: twoTickOakMinutes of every twoTickOakEveryMinutes.
func (o *wcfletchOaks) stepWindow(ctx *Context, d *woodcutrunner.Deps) bool {
	now := time.Now()
	if o.dayStart.IsZero() || now.Sub(o.dayStart) >= o.every {
		o.dayStart, o.used = now, 0
	}
	if o.chop != nil {
		o.used += now.Sub(o.last)
		o.last = now
		stopped := o.chop.stopped
		if stopped == "" && o.chop.run != nil {
			stopped = o.chop.run.Stopped()
		}
		if o.used >= o.window || stopped != "" {
			if stopped != "" {
				ctx.logf("wcfletch: the 2-tick oak chopper stopped (%s): back to chopping and fletching, the day's oak allowance is spent", stopped)
				o.used = o.window
			} else {
				ctx.logf("wcfletch: the day's 2-tick oak allowance (%s) is used up: back to chopping and fletching", o.window)
			}
			o.chop.OnStop(ctx)
			if ctx.Guardian != nil {
				ctx.Guardian.CombatExpected = false
			}
			o.chop = nil
			return false
		}
		_ = o.chop.Loop(ctx)
		return true
	}
	if o.used >= o.window || woodcutrunner.WoodcuttingLevel(d) < wcfletchOaksMinWoodcutting {
		return false
	}
	ctx.logf("wcfletch: starting the 2-tick oaks (%s used of %s in the window since %s)", o.used.Round(time.Minute), o.window, o.dayStart.Format("15:04 2 Jan"))
	o.chop, o.last = &retaliateChopperBot{}, now
	if err := o.chop.OnStart(ctx); err != nil {
		ctx.logf("wcfletch: the 2-tick oak chopper did not start: %v", err)
		o.chop, o.used = nil, o.window
		return false
	}
	return true
}

// stop ends a chopper still at work (the script is stopping).
func (o *wcfletchOaks) stop(ctx *Context) {
	if o.chop != nil {
		o.chop.OnStop(ctx)
		if ctx.Guardian != nil {
			ctx.Guardian.CombatExpected = false
		}
		o.chop = nil
	}
	if o.mode == wcfletchplan.OakAuto {
		wcfletchplan.ClaimedOaks.Release(o.world, ctx.Username)
	}
}
