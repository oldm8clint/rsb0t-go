package bot

// ProgressiveWCFletch's yew cohort spread over the fleet's worlds, and the quarter-hourly count of the
// seconds it spends waiting for a yew.
//
// The measurement, the model and the rules are wcfletchplan/yewworlds.go's and yewidle.go's (read the header
// of the first before this). What is here is the wiring: when the script asks the registry for its world,
// how a hand-off survives the hops it needs, and what it says in the log.
//
// THE ORDER MATTERS, and it is the reason this is a file of its own and not three lines in Loop:
//
//  1. OnStart: homeWorld.start resets the fleet's home to the SETTINGS' homeWorld (1 for the whole live
//     cohort), then yewWorldStart adopts the assigned world straight away. The other way round, or one
//     pass later, the first Loop would see an account that a hop has just put on world 2 standing away
//     from its home (1) and hop it back, for ever.
//  2. Loop: yewWorldStep runs BEFORE homeWorld.step, so a changed assignment (a rebalance, a lapsed place)
//     is the home that step hops to.
//  3. A hand-off trip. The mule is on world 1 and donorrunner.Run hops there first and home afterwards;
//     the script restarts on the far side of each hop. On world 1 the restarted script would find itself
//     away from its home (the assigned world 2 for a yew account, the homeWorld setting for any other, the
//     magic cohort's among them) and hop home before the trade. So the hand-off marks a trip
//     (wcfletchplan.SharedHandoffTrips, which survives the restart and is held for EVERY account, spread or
//     not), the restarted script stays where it is while the trip stands and resumes the hand-off
//     (handoffPending), and the end of the hand-off ends the trip.

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/wcfletchplan"
	"rsb0t-go/internal/scripts/wcfletchrunner"
	"rsb0t-go/internal/scripts/woodcutplan"
	"rsb0t-go/internal/scripts/woodcutrunner"
)

// stumpLogEvery is the least time between two "every yew is a stump" lines from one account.
const stumpLogEvery = 3 * time.Minute

// yewWorldState is one run's side of the world spread.
type yewWorldState struct {
	// on is true when this run spreads the account over worlds; worlds is the list it spreads over.
	on     bool
	worlds []int
	// refused is true once the fleet would not take the assigned world as a home (a world the
	// deployment does not list, or no fleet behind the Context): the run then stays where it is.
	refused bool
	// assigned is the world the registry holds the account on (0: none, not on the yew rung).
	assigned int
	// tripLogged is true while the "staying on the mule's world" line for the current trip is said.
	tripLogged bool
	// unreadSince is when this run first found the Woodcutting level unread (the zero time once it is read).
	unreadSince time.Time
}

// yewLevelsWait is the longest a run waits for its Woodcutting level to arrive before it judges the rung
// without it (and the settings' homeWorld hop goes ahead).
const yewLevelsWait = 30 * time.Second

// yewStrangers is the strangers each world's yew board counts, for the registry's loads.
func yewStrangers(now time.Time) func(world int) int {
	return func(world int) int { return wcfletchplan.SharedYewBoardFor(world).Standing(now).Strangers }
}

// yewWorldStart reads the settings, resumes a hand-off that a hop to the mule's world interrupted, and
// makes the first assignment. Called from OnStart straight after homeWorld.start.
func (b *wcfletchBot) yewWorldStart(ctx *Context, d *woodcutrunner.Deps) {
	b.yewW = yewWorldState{}
	now := time.Now()
	// The trip is the account's own, whatever it chops and wherever it lives: a magic account on the world its
	// homeWorld setting names comes back from the hop to the mule's world exactly as a dealt yew account does.
	if b.cfg.Donor().Enabled() && wcfletchplan.SharedHandoffTrips().On(ctx.Username, now) {
		b.handoffPending = true
		ctx.logf("wcfletch: back from a hop on a hand-off trip (world %d) - resuming the hand-off to %q", ctx.World(), b.cfg.Wood.Mule)
	}
	if !b.cfg.SpreadYew || !b.cfg.SpreadYewWorlds {
		return
	}
	worlds := wcfletchplan.ParseWorlds(b.cfg.YewWorlds)
	if len(worlds) < 2 {
		ctx.logf("wcfletch: yew world spread is off: yewWorlds %q names fewer than two worlds", b.cfg.YewWorlds)
		return
	}
	b.yewW.worlds, b.yewW.on = worlds, true
	b.yewWorldStep(ctx, d, now)
}

// yewWorldStep keeps the account on the world the registry assigns it while it is on the yew rung, and gives the
// assignment back when it is not. It is cheap: one registry lookup a pass.
//
// It reports false while the Woodcutting level has not arrived yet (at most yewLevelsWait), and the caller then
// sends nothing that moves the account: no hop home. A run starts before the server's UPDATE_STAT, reads level 0,
// takes the account for a Normal-tree beginner off the yew rung, and the settings' homeWorld hop went out at once.
// Live, 12 October 2026: after the yew accounts' homeWorld was set per world, 234 "going home to world N" hops in
// 7 hours, each run hopping to the settings' world and the next run, its levels read, hopping back to the world the
// spread assigned (Acct195: 20 hops in two hours, "Woodcutting 0 / Fletching 0 -> Normal tree" at every start).
func (b *wcfletchBot) yewWorldStep(ctx *Context, d *woodcutrunner.Deps, now time.Time) bool {
	st := &b.yewW
	if !st.on || st.refused {
		return true
	}
	if _, read := d.Session.State().Skill(woodcutrunner.WoodcuttingStat); !read || woodcutrunner.WoodcuttingLevel(d) <= 0 {
		if st.unreadSince.IsZero() {
			st.unreadSince = now
		}
		if now.Sub(st.unreadSince) < yewLevelsWait {
			return false
		}
	} else {
		st.unreadSince = time.Time{}
	}
	reg := wcfletchplan.SharedWorldSpread()
	wood, fletch := woodcutrunner.WoodcuttingLevel(d), wcfletchrunner.FletchingLevel(d)
	idx := wcfletchplan.TierIndex(b.cfg.LadderSnapshotFor(wood, fletch, b.fletching()))
	if !wcfletchplan.UsesYewCamps(idx) {
		if st.assigned != 0 {
			reg.Release(ctx.Username)
			ctx.logf("wcfletch: yew world spread: off the yew rung (now %s), so world %d's yews are no longer this account's to share",
				wcfletchplan.Ladder[idx].Tier.Label, st.assigned)
			st.assigned = 0
			b.home.release(ctx)
		}
		return true
	}
	a := reg.Assign(ctx.Username, st.worlds, ctx.World(), yewStrangers(now), now)
	if a.Fresh || a.World != st.assigned {
		verb := fmt.Sprintf("staying on world %d", a.World)
		if a.World != ctx.World() {
			verb = fmt.Sprintf("going to world %d from world %d", a.World, ctx.World())
		}
		loads := a.Loads
		if loads == nil {
			loads = reg.Loads(st.worlds, yewStrangers(now), now)
		}
		ctx.logf("wcfletch: yew world spread: %s - the yew cohort is dealt over worlds %s, each world's 11 yews feeding its share (loads: %s; spreadYewWorlds=false keeps the account where it is)",
			verb, wcfletchplan.DescribeWorlds(st.worlds), wcfletchplan.DescribeLoads(st.worlds, loads))
	}
	st.assigned = a.World
	stay := a.World
	if ctx.World() != a.World && wcfletchplan.SharedHandoffTrips().On(ctx.Username, now) {
		stay = ctx.World()
		if !st.tripLogged {
			st.tripLogged = true
			ctx.logf("wcfletch: yew world spread: on world %d for a hand-off to the mule (assigned world %d) - staying until it is done, for at most %s",
				stay, a.World, wcfletchplan.TripTTL)
		}
	} else {
		st.tripLogged = false
	}
	if err := b.home.adopt(ctx, stay, a.World); err != nil {
		st.refused = true
		reg.Release(ctx.Username)
		st.assigned = 0
		if errors.Is(err, ErrNoWorlds) {
			ctx.logf("wcfletch: yew world spread is off for this run: no fleet behind this account to hop it")
		} else {
			ctx.logf("wcfletch: yew world spread is off for this run: the fleet would not take world %d as this account's home (%v) - it stays where the settings put it", a.World, err)
		}
	}
	return true
}

// goHome is Loop's hop home: homeWorld.step, unless a hand-off trip holds the account on the mule's world.
// True means a hop was accepted or is running, and the caller's pass is over.
func (b *wcfletchBot) goHome(ctx *Context, now time.Time) bool {
	return !b.tripHolds(ctx, now) && b.home.step(ctx, "wcfletch", now)
}

// tripHolds reports whether a hand-off trip holds the account where it is, so Loop sends no hop home: the
// restarted script is on the mule's world for the trade and goes home when it is over (donorrunner hops it, and
// the trip's end, or wcfletchplan.TripTTL, gives the hop back to homeWorld.step). A dealt yew account is held
// by adoptWorld's stay already; this is the hold for every other, the magic cohort's on its settings' worlds
// above all. Said once per trip, and only when it holds the account away from its home.
func (b *wcfletchBot) tripHolds(ctx *Context, now time.Time) bool {
	if !wcfletchplan.SharedHandoffTrips().On(ctx.Username, now) {
		b.yewW.tripLogged = false
		return false
	}
	if home := b.home.home; home != 0 && ctx.World() != home && !b.yewW.tripLogged {
		b.yewW.tripLogged = true
		ctx.logf("wcfletch: on world %d for a hand-off to the mule (home world %d) - staying until it is done, for at most %s",
			ctx.World(), home, wcfletchplan.TripTTL)
	}
	return true
}

// tripBegin marks a hand-off trip before donorrunner.Run, which may hop to the mule's world.
func (b *wcfletchBot) tripBegin(ctx *Context) {
	wcfletchplan.SharedHandoffTrips().Begin(ctx.Username, time.Now())
}

// tripEnd ends the trip once the hand-off is over: traded, given up, or stopped by the user. A run that
// ended only because it hopped to the mule's world (or to reach it) keeps the trip for the script that
// starts on the far side.
func (b *wcfletchBot) tripEnd(ctx *Context, hopping, traded, gaveUp bool) {
	if hopping && !traded && !gaveUp {
		return
	}
	wcfletchplan.SharedHandoffTrips().End(ctx.Username)
	b.yewW.tripLogged = false
}

// ---- the quarter-hourly count of idle seconds ----

// noteChopPass records what a chop pass cost: a pass that found no yew standing or only excluded ones is
// idle time, anything else ends the spell of waiting. took is the pass's wall time, the Delay the outcome
// took included. It also says, once a spell has lasted StumpSpellLogAfter, that every yew at the camp is a
// stump and what the account does about it.
func (b *wcfletchBot) noteChopPass(ctx *Context, d *woodcutrunner.Deps, tier woodcutplan.Tier, reason string, took time.Duration) {
	now := time.Now()
	switch reason {
	case "no-tree":
		b.idleClock.Pass(wcfletchplan.IdleStumps, took, now)
	case "all-blocked":
		b.idleClock.Pass(wcfletchplan.IdleExcluded, took, now)
	default:
		b.idleClock.Worked(now)
		return
	}
	spell := b.idleClock.Spell()
	if spell < wcfletchplan.StumpSpellLogAfter || now.Sub(b.stumpLogAt) < stumpLogEvery {
		return
	}
	b.stumpLogAt = now
	carried := wcfletchrunner.CountByName(d, tier.Logs)
	what := "nothing to fletch meanwhile"
	if carried > 0 && b.fletching() {
		what = fmt.Sprintf("fletching the %d %s carried meanwhile", carried, strings.ToLower(tier.Logs))
	}
	kind := "a stump"
	if reason == "all-blocked" {
		kind = "excluded"
	}
	regrow := ""
	if s, ok := woodcutplan.TierSupply[tier.Label]; ok {
		regrow = fmt.Sprintf("; they regrow in about %ds", int(s.RespawnTicks*woodcutplan.ProductionTick.Seconds()*woodcutplan.PlayerCountScale(wcfletchplan.BoardPlayers)))
	}
	ctx.logf("wcfletch: every %s at %s is %s on world %d (%ds so far%s): %s",
		strings.ToLower(tier.Loc), b.campLabel(ctx, tier), kind, ctx.World(), int(spell/time.Second), regrow, what)
}

// campLabel names where the account chops: its yew or magic camp, else the tier's own label.
func (b *wcfletchBot) campLabel(ctx *Context, tier woodcutplan.Tier) string {
	if c, ok := b.seatedCamp(ctx); ok {
		return c.Name
	}
	return "the " + strings.ToLower(tier.Label) + " camp"
}

// reportYewIdle says what the last quarter hour cost, once it is due: the account's own line, the cohort's
// line when the ledger's window closes, and, when the account lost enough of it, a move to a lighter world.
func (b *wcfletchBot) reportYewIdle(ctx *Context, d *woodcutrunner.Deps, now time.Time) {
	if !b.idleClock.Due(now) {
		return
	}
	r := b.idleClock.Take(now)
	chopped, made := b.chopped-b.idleSeen.chopped, b.fletched-b.idleSeen.fletched
	b.idleSeen.chopped, b.idleSeen.fletched = b.chopped, b.fletched
	where := "its camp"
	if b.haveRung {
		where = b.campLabel(ctx, wcfletchplan.Ladder[b.rung].Tier)
	}
	ctx.logf("wcfletch: idle over the last %d min on world %d at %s: %s; %d logs chopped, %d cut into bows or shafts",
		int(r.Window/time.Minute), ctx.World(), where, r.Describe(), chopped, made)
	if line, ok := wcfletchplan.SharedIdleLedger().Add(ctx.World(), r, now); ok {
		ctx.logf("%s", line)
	}
	b.rebalanceYewWorld(ctx, r, now)
}

// rebalanceYewWorld asks the registry whether an account that lost this much of the last quarter hour to
// waiting would do better on another world, and reassigns it when so. The next Loop pass hops it.
func (b *wcfletchBot) rebalanceYewWorld(ctx *Context, r wcfletchplan.IdleReport, now time.Time) {
	st := &b.yewW
	if !st.on || st.refused || st.assigned == 0 {
		return
	}
	reg := wcfletchplan.SharedWorldSpread()
	from := st.assigned
	to, ok := reg.Rebalance(ctx.Username, st.worlds, r.Fraction(), yewStrangers(now), now)
	if !ok {
		return
	}
	ctx.logf("wcfletch: yew world spread: %.0f%% of the last quarter hour went to waiting for a yew on world %d, which carries more of ours than world %d (%s) - moving to world %d",
		100*r.Fraction(), from, to, wcfletchplan.DescribeLoads(st.worlds, reg.Loads(st.worlds, yewStrangers(now), now)), to)
}
