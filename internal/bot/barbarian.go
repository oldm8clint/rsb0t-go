package bot

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/barbarianplan"
	"rsb0t-go/internal/scripts/barbarianrunner"
	"rsb0t-go/internal/scripts/fighterfood"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
	"rsb0t-go/internal/webwalk/hops"
	"rsb0t-go/internal/webwalk/live"
)

func init() {
	Register("BarbarianAgility", func() Bot { return &barbarianBot{} })
}

// barbarianBot runs the Barbarian Outpost agility course, one stage-appropriate action per
// Loop call: recover from a death, eat, bank for food, travel back to the course, squeeze
// through the pipe, or run one obstacle — ports the TS original's own decideStage/main-loop
// structure (barbarianplan.DecideStage, already pure-ported and unit-tested but never wired
// to a live action before this) rather than the earlier, narrower version of this file that
// only ever ran the lap and treated HP/food as explicitly out of scope. One stage-action per
// call means a Stop() takes effect within a single obstacle/eat/bank step, not a whole lap.
//
// Settings, all optional (read via IntSetting/BoolSetting/StringSetting/FloatSetting, so
// both Go values and the float64/bool/string a JSON-decoded fleet.config.json roster entry
// produces work):
//
//	cheatSetup   bool    default true      — ::setstat agility + ::tele to the pipe's north
//	                                         stand before the first lap, exactly like
//	                                         cmd/barbtest's non-live mode. LOCAL-ENGINE-ONLY:
//	                                         leave this false for an account that's already
//	                                         positioned and levelled for real.
//	agilityLevel int     default 60        — the level cheatSetup sets, when enabled.
//	maxLaps      int     default 0         — stop after this many full laps; 0 means run
//	                                         until Stop() is called.
//	food         string  default "Lobster" — matched via fighterfood.IsFoodItem (partial
//	                                         multi-bite forms included); blank means the default.
//	foodWithdraw int     default 20        — food drawn from the bank on each trip, clamped 1-28.
//	minFood      int     default 1         — bank once the pack holds fewer than this many,
//	                                         clamped 0-28.
//	eatBelowHp   int     default 50        — eat once current HP is below this percent of
//	                                         max, clamped to 10-90 (matches the TS original's
//	                                         own eatAt() clamp) — always threshold mode, same
//	                                         as the TS original hardcodes for this script.
//	bankTile     tile    default Seers' Village (2725,3491,0) — the bank stand, as "x,z[,level]",
//	                                         {"x","z","level"} or [x,z,level] (TS bankTile).
//	lap          string  default the eight-step lap — obstacle names separated by commas
//	                                         (TS lap, parsed by barbarianplan.ParseLap).
//
// The long walks to the bank and back to the course go through internal/webwalk with ship
// hops installed, as the TS original's Traversal.walkResilient does, falling back to the
// same-level walker when webwalk cannot be built or does not arrive.
type barbarianBot struct {
	lap      *barbarianrunner.LapState
	lapsDone int
	deaths   int
	cfg      barbarianSettings
	walker   *webwalk.Walker

	// banking mirrors the TS original's own `this.banking` flag: forced true after a
	// death (recover()) so a bank trip happens even if food is still above minFood,
	// cleared once a trip leaves the pack holding enough food.
	banking bool
	// lastNoFoodWarn throttles the empty-bank warning to once a minute, as TS warn() does.
	lastNoFoodWarn time.Time
}

// barbarianSettings is the resolved settings manifest.
type barbarianSettings struct {
	maxLaps      int
	food         string
	foodWithdraw int
	minFood      int
	eatBelowFrac float64
	bankTile     tile.Tile
	lap          []string
	cheatSetup   bool
	agilityLevel int
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// readBarbarianSettings resolves the settings with BarbarianAgility.ts's defaults and clamps.
func readBarbarianSettings(m map[string]interface{}) barbarianSettings {
	s := barbarianSettings{
		maxLaps:      IntSetting(m, "maxLaps", 0),
		food:         strings.TrimSpace(StringSetting(m, "food", barbarianplan.DefaultFood)),
		foodWithdraw: clampInt(IntSetting(m, "foodWithdraw", barbarianplan.DefaultFoodWithdraw), 1, 28),
		minFood:      clampInt(IntSetting(m, "minFood", 1), 0, 28),
		bankTile:     fisherTileSetting(m, "bankTile", barbarianplan.DefaultBank),
		lap:          barbarianplan.ParseLap(StringSetting(m, "lap", "")),
		cheatSetup:   BoolSetting(m, "cheatSetup", true),
		agilityLevel: IntSetting(m, "agilityLevel", 60),
	}
	if s.food == "" {
		s.food = barbarianplan.DefaultFood
	}
	eatBelowHp := FloatSetting(m, "eatBelowHp", 50)
	if eatBelowHp < 10 {
		eatBelowHp = 10
	} else if eatBelowHp > 90 {
		eatBelowHp = 90
	}
	s.eatBelowFrac = eatBelowHp / 100
	return s
}

// respawnWait bounds how long recovery waits for the respawn teleport after the death line.
var respawnWait = 10 * time.Second

func (b *barbarianBot) deps(ctx *Context) *barbarianrunner.Deps {
	return &barbarianrunner.Deps{
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

func (b *barbarianBot) freshLap() *barbarianrunner.LapState {
	lap := make([]string, len(b.cfg.lap))
	copy(lap, b.cfg.lap)
	return barbarianrunner.NewLapState(lap)
}

func (b *barbarianBot) OnStart(ctx *Context) error {
	if ctx.Pack == nil {
		return fmt.Errorf("barbarian: no collision pack configured (fleet.config.json needs a packPath) — refusing to start rather than panic later inside pathfinding")
	}
	if ctx.Locs == nil {
		return fmt.Errorf("barbarian: no loc index configured (fleet.config.json needs a locsPath) — refusing to start rather than panic later looking up an obstacle")
	}
	b.cfg = readBarbarianSettings(ctx.Settings)
	b.lap = b.freshLap()

	if w, err := live.NewWalker(live.Deps{
		Session: ctx.Session, Pack: ctx.Pack, Locs: ctx.Locs, Config: ctx.Config,
		Components: ctx.Components, Chat: ctx.Chat, Delay: ctx.Delay, Log: ctx.Log,
	}); err != nil {
		ctx.logf("barbarian: no webwalk (%v) — the bank and course walks use the same-level walker", err)
	} else {
		if ctx.Components != nil && ctx.Config != nil {
			hops.Install(w, hops.New(hops.Deps{
				Client: w.Client, Session: ctx.Session, Config: ctx.Config,
				Components: ctx.Components, Log: ctx.Log,
			}))
		}
		b.walker = w
	}

	if b.cfg.cheatSetup {
		if err := barbarianrunner.CheatSetup(b.deps(ctx), b.cfg.agilityLevel); err != nil {
			return fmt.Errorf("barbarian: cheat setup: %w", err)
		}
	}
	ctx.logf("barbarian: food %q (withdraw %d, bank below %d), bank at %d,%d,%d, lap of %d",
		b.cfg.food, b.cfg.foodWithdraw, b.cfg.minFood, b.cfg.bankTile.X, b.cfg.bankTile.Z, b.cfg.bankTile.Level, len(b.cfg.lap))
	// Entering the course is now Loop's own first stage-driven action (DecideStage
	// naturally picks StageTravel/StageEnter before StageLap when starting off/outside
	// the course) rather than something OnStart does once upfront — this is what lets a
	// mid-run displacement (a death, an unhandled random event's own teleport) recover
	// the same way a cold start does, instead of only ever routing back once.
	return nil
}

// snapshot builds the Snapshot barbarianplan.DecideStage reads, from the live session.
func (b *barbarianBot) snapshot(ctx *Context, d *barbarianrunner.Deps) barbarianplan.Snapshot {
	dead := ctx.Session.State().IsDead()
	food := barbarianrunner.FoodCount(d, b.cfg.food)
	needEat := !dead && barbarianrunner.NeedEat(d, b.cfg.food, fighterfood.EatThreshold, b.cfg.eatBelowFrac)
	var here *tile.Tile
	if x, z, level, ok := ctx.Session.State().Tile(); ok {
		here = &tile.Tile{X: x, Z: z, Level: level}
	}
	return barbarianplan.Snapshot{
		Dead: dead, NeedEat: needEat, Food: food, MinFood: b.cfg.minFood,
		Here: here, Banking: b.banking,
	}
}

// recover is BarbarianAgility.ts's recover(): wait for the respawn, settle three ticks, clear
// the death, restart the lap and force a bank trip. The death flag is cleared here and only
// here, so the wait must not depend on it (an earlier version waited for !IsDead and never
// finished).
func (b *barbarianBot) recover(ctx *Context) {
	b.deaths++
	st := ctx.Session.State()
	dx, dz, dl, known := st.Tile()
	ctx.logf("barbarian: died (death %d) — waiting for the respawn", b.deaths)
	// The death line arrives before the respawn teleport; wait for the jump so the bank walk
	// plans from Lumbridge, not from where the account fell.
	ctx.DelayUntil(func() bool {
		x, z, l, ok := st.Tile()
		if !ok {
			return false
		}
		if !known {
			return true
		}
		return l != dl || tile.Chebyshev(tile.Tile{X: x, Z: z, Level: l}, tile.Tile{X: dx, Z: dz, Level: dl}) > 10
	}, respawnWait)
	if _, _, _, ok := st.Tile(); !ok {
		return
	}
	if !ctx.DelayTicks(3) {
		return
	}
	st.ClearDead()
	b.lap = b.freshLap()
	b.banking = true
	x, z, l, _ := st.Tile()
	ctx.logf("barbarian: respawned at %d,%d,%d — banking before the course", x, z, l)
}

func (b *barbarianBot) Loop(ctx *Context) error {
	d := b.deps(ctx)
	// TS continues any open chat dialog first (level-ups and the like).
	if ctx.Components != nil {
		if dlg := actions.NewChatDialog(ctx.Session, ctx.Components); dlg.CanContinue() {
			_ = dlg.Continue()
			ctx.Delay(600 * time.Millisecond)
			return nil
		}
	}
	stage := barbarianplan.DecideStage(b.snapshot(ctx, d))

	switch stage {
	case barbarianplan.StageRecover:
		b.recover(ctx)
		return nil

	case barbarianplan.StageEat:
		if !barbarianrunner.EatOnce(d, b.cfg.food) {
			ctx.Delay(600 * time.Millisecond)
		}
		return nil

	case barbarianplan.StageBank:
		count, ok := barbarianrunner.BankForFood(d, b.cfg.bankTile, b.cfg.food, b.cfg.foodWithdraw)
		if !ok {
			// Couldn't reach/open the bank this attempt — DecideStage tries again next
			// Loop call, since nothing else changes state in the meantime. Without this
			// delay a genuinely unreachable bank turns into a tight busy-loop: Runner calls
			// Loop back to back with no delay of its own.
			if ctx.Session.State().IsDead() {
				return nil
			}
			ctx.logf("barbarian: could not reach or open the bank — waiting before retrying")
			ctx.Delay(15 * time.Second)
			return nil
		}
		if !barbarianEnoughFood(count, b.cfg.minFood) {
			// TS keeps banking until the pack holds max(1, minFood): an empty bank never sends
			// the account back to the course without food.
			if time.Since(b.lastNoFoodWarn) >= time.Minute {
				b.lastNoFoodWarn = time.Now()
				ctx.logf("barbarian: only %d %s after banking (need %d) — waiting for food in the bank", count, b.cfg.food, barbarianMinHeld(b.cfg.minFood))
			}
			ctx.Delay(6 * time.Second) // 10 ticks
			return nil
		}
		b.banking = false
		ctx.logf("barbarian: restocked %d %s", count, b.cfg.food)
		return nil

	case barbarianplan.StageTravel, barbarianplan.StageEnter:
		// Matches the TS original's own travelToCourse/enterCourse exactly: neither ever
		// throws on failure there — every failure path just logs a warning and returns,
		// letting decideStage naturally retry next tick.
		if _, err := barbarianrunner.EnterCourse(d); err != nil {
			if ctx.Session.State().IsDead() {
				return nil
			}
			ctx.logf("barbarian: %v — retrying", err)
			ctx.Delay(3 * time.Second)
		}
		return nil
	}

	if b.lap.Done() {
		b.lapsDone++
		ctx.logf("lap %d complete (%d/%d obstacles cleared)", b.lapsDone, b.lap.Cleared, len(b.lap.Lap))
		if b.cfg.maxLaps > 0 && b.lapsDone >= b.cfg.maxLaps {
			ctx.requestStop()
			return nil
		}
		b.lap = b.freshLap()
		return nil
	}

	_, err := b.lap.Advance(d)
	if err != nil {
		return fmt.Errorf("barbarian: %w", err)
	}
	return nil
}

// barbarianMinHeld is the food a bank trip must leave in the pack: max(1, minFood), as TS.
func barbarianMinHeld(minFood int) int {
	if minFood < 1 {
		return 1
	}
	return minFood
}

func barbarianEnoughFood(held, minFood int) bool { return held >= barbarianMinHeld(minFood) }

func (b *barbarianBot) OnStop(ctx *Context) {
	cleared, total := 0, 0
	if b.lap != nil {
		cleared, total = b.lap.Cleared, len(b.lap.Lap)
	}
	ctx.logf("barbarian: stopped after %d complete lap(s), %d death(s), %d/%d obstacles cleared this lap", b.lapsDone, b.deaths, cleared, total)
}
