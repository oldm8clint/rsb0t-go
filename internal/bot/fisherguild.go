package bot

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/combatrunner"
	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/fisherrunner"
	"rsb0t-go/internal/scripts/tile"
)

// ProgressiveFisher's opt-in Fishing Guild migration (docs/fishing_guild_foundations_2026-09-18.md).
//
// Off by default: with the fishingGuild setting false (or missing) nothing in this file changes
// what the fisher does — selectFishingArea keeps the Catherby layout, guildDriftRef answers nil,
// guildTravelTarget answers false and the Guardian's SafeRef is never touched.
//
// On, an account whose live Fishing level is at least fisherplan.FishingGuildEntryLevel (68, the
// content check on loc 2025; 67 is refused at the door) and whose active rung the Guild's spots
// offer (Net/Harpoon or Cage/Harpoon — not the small net's Net/Bait) banks at the Guild bank and
// fishes at the Guild camp. Tools are still bought and fish still sold at Harry's and Arhein's in
// Catherby, and the mule hand-off still meets the receiver at its existing post; the donor walks
// there with the load and back to the Guild afterwards. Every walk goes through
// fisherrunner.WalkNear, the shared door-aware walker, so both Guild doors (2025 at 2611,3394 and
// 1530 at 2611,3398) are opened the same way every other door on a fisher's route is.

// guildTravelFailLimit is how many Guild walks in a row may end with no progress before the
// account falls back to Catherby for guildBackoff: a door that will not open (or a route that
// cannot be walked) must not hold a production account at 0 XP/hour.
const (
	guildTravelFailLimit = 3
	guildBackoff         = 20 * time.Minute
	// guildTravelProgress is how many tiles closer a walk that timed out must have brought the
	// account for it to count as progress rather than a failure.
	guildTravelProgress = 10
	// guildHandoffPreWalk: a Guild donor further than this from the receiver's post walks there
	// with the door-aware walker before donorrunner.Run takes over (Run's own walk is a plain
	// same-level path that cannot open the Guild's doors).
	guildHandoffPreWalk = 20
)

// fisherGuild is the Guild state one fisherBot carries across Loop calls.
type fisherGuild struct {
	enabled bool
	// muleAtExplicit is true when the account's settings name muleAt themselves; the Guild's
	// own threshold then does not replace it.
	muleAtExplicit bool
	// muleAt is fisherplan.FishingGuildMuleAt, spread per account like muleAt (jitterMuleAt).
	muleAt int

	// planned is the planner's own answer this Loop, before displacement or backoff; active is
	// whether the account actually works the Guild this Loop.
	planned fisherplan.FishingArea
	active  bool

	fails        int
	backoffUntil time.Time
	// spawnBackoffUntil: a tool spawn that stayed empty (or could not be reached) sends the
	// account back to buying in Catherby until then. spawnTakes counts tools recovered.
	spawnBackoffUntil time.Time
	spawnTakes        int
	lastArea          fisherplan.FishingArea
	lastWhy           string
	walks             int
}

func readFisherGuild(settings map[string]interface{}, username string) fisherGuild {
	_, explicit := settings["muleAt"]
	return fisherGuild{
		// On unless an account sets "fishingGuild": false. Owner 2026-09-19: every account
		// that can enter the Guild (Fishing 68+, a rung its spots offer) fishes there; the
		// planner keeps everyone else at Catherby.
		enabled:        BoolSetting(settings, "fishingGuild", true),
		muleAtExplicit: explicit,
		muleAt:         jitterMuleAt(fisherplan.FishingGuildMuleAt, username),
	}
}

// guildArea is the planner's decision for s, with the reason it keeps Catherby when it does.
func guildArea(s fisherplan.FishSnapshot, spawnOK bool) (fisherplan.FishingArea, string) {
	owned := append(append([]string{}, s.HeldTools...), s.BankTools...)
	// A Guild account that has lost its rung's tool goes to the Guild for the spawn (and reads
	// the bank there first) rather than to Harry's; spawnOK is false while a spawn attempt is
	// backing off, which leaves the ordinary buy-in-Catherby path below.
	if spawnOK && fisherplan.GuildToolRecoverable(s.Level, s.Target, s.HeldTools, s.BankTools) {
		return fisherplan.AreaFishingGuild, ""
	}
	active, ok := fisherplan.ActiveRung(s.Level, s.Target, owned)
	switch {
	case !ok:
		return fisherplan.AreaCatherby, "no ladder tool owned"
	case s.Level < fisherplan.FishingGuildEntryLevel:
		return fisherplan.AreaCatherby, "Fishing below the Guild's entry level 68"
	case !fisherplan.SupportsGuildRung(active):
		return fisherplan.AreaCatherby, "the Guild's spots do not offer the " + active.Label + " rung"
	}
	route := fisherplan.PlanFishingRoute(true, s.Level, active, tile.Tile{})
	return route.Area, ""
}

// selectFishingArea picks this Loop's layout and marks the snapshot. With fishingGuild off it
// only (re)asserts the Catherby layout, which OnStart already set.
func (b *fisherBot) selectFishingArea(ctx *Context, s *fisherplan.FishSnapshot) {
	b.layout = fisherplan.Catherby()
	b.guild.active = false
	b.guild.planned = fisherplan.AreaCatherby
	if !b.guild.enabled {
		return
	}
	area, why := guildArea(*s, time.Now().After(b.guild.spawnBackoffUntil))
	b.guild.planned = area
	if area == fisherplan.AreaFishingGuild {
		switch {
		case s.Tile == nil || !fisherplan.OnGuildMainland(*s.Tile):
			area, why = fisherplan.AreaCatherby, "away from the Guild's mainland: recovering by way of Catherby first"
		case time.Now().Before(b.guild.backoffUntil):
			area, why = fisherplan.AreaCatherby, "Guild travel kept failing: fishing Catherby until "+b.guild.backoffUntil.Format("15:04:05")
		}
	}
	if area == fisherplan.AreaFishingGuild {
		b.layout = fisherplan.FishingGuild()
		b.guild.active = true
		s.Guild = true
	}
	if area != b.guild.lastArea || why != b.guild.lastWhy {
		b.guild.lastArea, b.guild.lastWhy = area, why
		if area == fisherplan.AreaFishingGuild {
			ctx.logf("fisher: fishing area: the Fishing Guild (Fishing %d, bank %d,%d, camp %d,%d)", s.Level, b.layout.Bank.X, b.layout.Bank.Z, b.layout.Camp.X, b.layout.Camp.Z)
		} else {
			ctx.logf("fisher: fishing area: Catherby (fishingGuild is on; %s)", why)
		}
	}
}

// guildMuleAt is the hand-off threshold: the Guild's own (FishingGuildMuleAt, spread per account) while the
// planner picks the Guild and the account's settings do not name muleAt; otherwise muleAt. The
// control panel's override still wins over both in Loop.
func (b *fisherBot) guildMuleAt(base int) int {
	if b.guild.enabled && !b.guild.muleAtExplicit && b.guild.planned == fisherplan.AreaFishingGuild {
		return b.guild.muleAt
	}
	return base
}

// guildDriftRef is the Guild bank as the drift and flee reference while fishingGuild is on and
// the account is nearer the Guild than Catherby; nil (the Catherby default) otherwise.
func (b *fisherBot) guildDriftRef(ctx *Context) *pathfind.NavPoint {
	if !b.guild.enabled {
		return nil
	}
	x, z, level, ok := ctx.Session.State().Tile()
	if !ok || !fisherplan.NearerGuild(tile.Tile{X: x, Z: z, Level: level}) {
		return nil
	}
	bank := fisherplan.FishingGuild().Bank
	return &pathfind.NavPoint{X: bank.X, Z: bank.Z, Level: bank.Level}
}

// applyGuildRefs points the Guardian's flee bias at the nearer home. Only ever called with
// fishingGuild on, so an account with it off never has its SafeRef touched.
func (b *fisherBot) applyGuildRefs(ctx *Context, ref *pathfind.NavPoint) {
	if ctx.Guardian != nil {
		ctx.Guardian.SafeRef = ref
	}
}

// guildTravelTarget reports whether this Loop is a walk between Catherby and the Guild rather
// than an ordinary phase: a Guild account that must bank or fish while outside the Guild, or a
// Catherby account that must get home while on the Guild's side of the mainland (from inside the
// Guild the Catherby fallback's plain path cannot open the doors).
func (b *fisherBot) guildTravelTarget(phase fisherplan.FishPhase, s fisherplan.FishSnapshot) (tile.Tile, string, bool) {
	if !b.guild.enabled || s.Tile == nil || !fisherplan.OnGuildMainland(*s.Tile) {
		return tile.Tile{}, "", false
	}
	here := *s.Tile
	if b.guild.active {
		switch phase {
		case fisherplan.PhaseBank, fisherplan.PhaseFish, fisherplan.PhaseTravel, fisherplan.PhaseFund:
			if !fisherplan.InFishingGuild(here) {
				return fisherplan.FishingGuild().Bank, "the Fishing Guild", true
			}
		}
		return tile.Tile{}, "", false
	}
	if fisherplan.InCatherby(here) {
		return tile.Tile{}, "", false
	}
	switch phase {
	case fisherplan.PhaseTravel, fisherplan.PhaseFund:
		return fisherplan.Catherby().Bank, "Catherby", true
	}
	return tile.Tile{}, "", false
}

// guildTravelStep walks one leg toward dest with the shared door-aware walker. A walk that
// times out part way but got closer is progress, not failure; guildTravelFailLimit walks in a
// row toward the Guild with no progress send the account back to Catherby for guildBackoff.
func (b *fisherBot) guildTravelStep(ctx *Context, d *fisherrunner.Deps, from tile.Tile, dest tile.Tile, name string) error {
	before := tile.Chebyshev(from, dest)
	b.guild.walks++
	ctx.logf("fisher: walking to %s (%d,%d) from %d,%d, %d tiles away", name, dest.X, dest.Z, from.X, from.Z, before)
	if fisherrunner.WalkNear(d, dest, 8) {
		b.guild.fails = 0
		return nil
	}
	if b.survivalDue(ctx) {
		return nil
	}
	after := before
	if x, z, level, ok := ctx.Session.State().Tile(); ok {
		after = tile.Chebyshev(tile.Tile{X: x, Z: z, Level: level}, dest)
	}
	if after+guildTravelProgress <= before {
		b.guild.fails = 0
		ctx.logf("fisher: still walking to %s: %d tiles to go", name, after)
		return nil
	}
	b.guild.fails++
	ctx.logf("fisher: the walk to %s made no progress (%d tiles to go, %d/%d)", name, after, b.guild.fails, guildTravelFailLimit)
	if dest == fisherplan.FishingGuild().Bank && b.guild.fails >= guildTravelFailLimit {
		b.guild.fails = 0
		b.guild.backoffUntil = time.Now().Add(guildBackoff)
		ctx.logf("fisher: could not get into the Fishing Guild %d times in a row — fishing Catherby for %s", guildTravelFailLimit, guildBackoff)
	}
	b.pause(ctx, 3*time.Second)
	return nil
}

// guildHandoffWalk takes a Guild donor, load in hand, to the receiver's post through the Guild's
// doors, so donorrunner.Run only closes the last few tiles on open ground.
func (b *fisherBot) guildHandoffWalk(ctx *Context, d *fisherrunner.Deps) {
	if !b.guild.enabled {
		return
	}
	x, z, level, ok := ctx.Session.State().Tile()
	if !ok || !fisherplan.OnGuildMainland(tile.Tile{X: x, Z: z, Level: level}) {
		return
	}
	if tile.Chebyshev(tile.Tile{X: x, Z: z, Level: level}, b.muleStand) <= guildHandoffPreWalk {
		return
	}
	ctx.logf("fisher: hand-off: walking from %d,%d to the mule's post at %d,%d", x, z, b.muleStand.X, b.muleStand.Z)
	for i := 0; i < 3; i++ {
		if fisherrunner.WalkNear(d, b.muleStand, 5) {
			return
		}
		if x, z, level, ok := ctx.Session.State().Tile(); ok && tile.Chebyshev(tile.Tile{X: x, Z: z, Level: level}, b.muleStand) <= guildHandoffPreWalk {
			return
		}
	}
	ctx.logf("fisher: hand-off: the walk to the mule's post did not finish — the donor's own walk takes over from here")
}

// guildSpawnWaitTicks bounds the wait at an empty tool spawn: the engine's default obj
// respawnrate is 100 ticks (60s live), so 150 covers one respawn with margin. After that the
// account buys the tool in Catherby as before, for guildBackoff.
const guildSpawnWaitTicks = 150

// guildToolTarget reports the Guild tool spawn this Loop should visit instead of the ordinary
// phase: fishingGuild on, the Guild selected, and the earned rung's tool owned nowhere after a
// real bank read.
func (b *fisherBot) guildToolTarget(s fisherplan.FishSnapshot) (fisherplan.GuildToolSpawn, bool) {
	if !b.guild.enabled || !b.guild.active || s.Tile == nil || !fisherplan.OnGuildMainland(*s.Tile) {
		return fisherplan.GuildToolSpawn{}, false
	}
	return fisherplan.GuildToolWanted(s.Level, s.Target, s.HeldTools, s.BankTools)
}

// guildSpawnGiveUp sends the account back to buying in Catherby for guildBackoff.
func (b *fisherBot) guildSpawnGiveUp(ctx *Context, sp fisherplan.GuildToolSpawn, why string) {
	b.guild.spawnBackoffUntil = time.Now().Add(guildBackoff)
	ctx.logf("fisher: Guild %s spawn: %s — buying it in Catherby instead for the next %s", strings.ToLower(sp.Tool), why, guildBackoff)
	b.pause(ctx, time.Second)
}

// guildToolStep takes the missing tool from its spawn inside the Guild entrance: walk in through
// the doors, wait (bounded) for the item if someone else has it, take it with the shared
// combatrunner.ApproachAndTake, and count it only once the pack actually holds the tool.
func (b *fisherBot) guildToolStep(ctx *Context, d *fisherrunner.Deps, sp fisherplan.GuildToolSpawn) error {
	if fisherrunner.FreeSlots(d) == 0 {
		// A full pack cannot take it; bank at the Guild first (the spawn is on the way).
		ctx.logf("fisher: no %s and a full pack — banking at the Guild before taking one from its spawn", strings.ToLower(sp.Tool))
		return b.bankStep(ctx, d, b.snapshot(ctx, d))
	}
	here, _ := hereTile(ctx)
	if tile.Chebyshev(here, sp.At) > 1 {
		ctx.logf("fisher: no %s owned — fetching one from its Guild spawn at %d,%d", strings.ToLower(sp.Tool), sp.At.X, sp.At.Z)
		if !fisherrunner.WalkNear(d, sp.At, 1) {
			if b.survivalDue(ctx) {
				return nil
			}
			b.guild.fails++
			if b.guild.fails >= guildTravelFailLimit {
				b.guild.fails = 0
				b.guildSpawnGiveUp(ctx, sp, "could not walk to it")
				return nil
			}
			b.pause(ctx, 3*time.Second)
			return nil
		}
	}
	b.guild.fails = 0
	cd := &combatrunner.Deps{
		Session: ctx.Session, Config: ctx.Config, Components: ctx.Components, Pack: ctx.Pack,
		Locs: ctx.Locs, Walker: b.walker, Log: ctx.Log, Delay: combatrunner.Delayer(d.Delay),
		OnKill: ctx.KillRecorder(),
	}
	start := ctx.Session.State().Tick()
	waiting := false
	for {
		if hasToolName(fisherrunner.OwnedTools(d), sp.Tool) {
			b.guild.spawnTakes++
			ctx.logf("fisher: took a %s from the Guild spawn (%d recovered this run)", strings.ToLower(sp.Tool), b.guild.spawnTakes)
			return nil
		}
		if item, ok := groundItemAt(ctx, sp.ObjID, sp.At); ok {
			if err := combatrunner.ApproachAndTake(cd, item, 1, 6, 15*time.Second); err != nil {
				ctx.logf("fisher: Guild %s spawn: %v", strings.ToLower(sp.Tool), err)
			}
			if hasToolName(fisherrunner.OwnedTools(d), sp.Tool) {
				continue // counted and logged at the top
			}
		} else if !waiting {
			waiting = true
			ctx.logf("fisher: the Guild %s spawn is empty (taken by someone else) — waiting up to %d ticks for it to respawn", strings.ToLower(sp.Tool), guildSpawnWaitTicks)
		}
		if ctx.Session.State().Tick()-start > guildSpawnWaitTicks {
			b.guildSpawnGiveUp(ctx, sp, fmt.Sprintf("nothing taken in %d ticks", guildSpawnWaitTicks))
			return nil
		}
		if !b.pause(ctx, time.Second) {
			return nil
		}
	}
}

func hereTile(ctx *Context) (tile.Tile, bool) {
	x, z, level, ok := ctx.Session.State().Tile()
	return tile.Tile{X: x, Z: z, Level: level}, ok
}

// groundItemAt finds the stack of objID lying on t.
func groundItemAt(ctx *Context, objID int, t tile.Tile) (gamestate.GroundItem, bool) {
	for _, g := range ctx.Session.State().GroundItems() {
		if g.ID == objID && g.X == t.X && g.Z == t.Z && g.Level == t.Level {
			return g, true
		}
	}
	return gamestate.GroundItem{}, false
}
