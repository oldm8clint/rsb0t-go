package bot

// Running CombatTrainer as one stage of another script (ScarabSwarm's Stage 5,
// scarabswarm_trainer.go), without a second copy of its 1,900 lines.
//
// The registered "CombatTrainer" run owns its whole Context: it reads its settings from
// ctx.Settings, and when it is done -- goals met, out of food, a bank loop that makes no progress
// -- it ends the RUN, logging out for good (RequestFinish) or stopping the Context. Neither is
// right for a host script: the host's Settings are the host's, and "this stage is over" must hand
// control back rather than log the account out. An embed changes exactly those two things and
// nothing else; with embed nil every path is the registered script's, unchanged.
//
// It can also watch the account's hitpoints DURING a fight (guardFights, fightOne below), which
// the registered run does not do: combatrunner.FightOne waits up to 90s for the kill with no eye
// on the account, and the trainer only eats between fights. The stage5ab lab A/B of 2026-09-25
// lost two Scarab accounts to exactly that, each with food in the pack and no "ate a" line: 14 hp
// to 0 among the Al-Kharid warriors (who help each other), and 26 hp to 0 at the Giants.

import (
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/combatplan"
	"rsb0t-go/internal/scripts/combatrunner"
	"rsb0t-go/internal/scripts/fighterfood"
	"rsb0t-go/internal/webwalk"
)

// combatTrainerShopMemory is what one trainer run learns about the gear shops and hands to the
// next: the shelves found empty and the buying-run budget already spent.
type combatTrainerShopMemory struct {
	unavailable      map[string]bool
	gearTrips        int
	shopBandKey      string
	shopRunsThisBand int
}

// loadShopMemory starts a run from the host's shop memory, if it has one.
func (b *combatTrainerBot) loadShopMemory() {
	if b.embed == nil || b.embed.shop == nil {
		return
	}
	m := b.embed.shop
	if m.unavailable == nil {
		m.unavailable = map[string]bool{}
	}
	b.unavailable = m.unavailable // shared, so a shelf struck mid-run is kept even if the run dies
	b.gearTrips, b.shopBandKey, b.shopRunsThisBand = m.gearTrips, m.shopBandKey, m.shopRunsThisBand
}

// saveShopMemory hands this run's buying-run budget back to the host.
func (b *combatTrainerBot) saveShopMemory() {
	if b.embed == nil || b.embed.shop == nil {
		return
	}
	m := b.embed.shop
	m.gearTrips, m.shopBandKey, m.shopRunsThisBand = b.gearTrips, b.shopBandKey, b.shopRunsThisBand
}

// combatTrainerEmbed is set on a combatTrainerBot that another bot runs.
type combatTrainerEmbed struct {
	// settings replaces ctx.Settings as the trainer's settings source. Context.Settings is
	// shared with the panel (Runner.Settings), so it is never swapped.
	settings map[string]interface{}
	// ended is called, once per run, where the registered script would end its run: goalsMet
	// true from finish (every goal met), false from a stop (food, a wedged bank loop).
	ended func(reason string, goalsMet bool)
	done  bool
	// shop, when set, is the host's memory of the gear shops across trainer runs. A host that
	// restarts its trainer (ScarabSwarm stops it for every quest-ladder pass) would otherwise
	// hand each new run a free first buying run and an empty list of shelves found empty: on
	// 29 September Stage 5 accounts spent ten of every eleven minutes walking from Al Kharid to
	// Falador and back to learn, again, that Cassie keeps no Steel sq shield.
	shop *combatTrainerShopMemory
	// guardFights watches every fight the trainer waits out (fightOne): its own eat rule runs
	// between the polls of the wait, and a fight with no food left and the account under that
	// threshold is broken off. Off, each fight is combatrunner.FightOne exactly as the registered
	// script runs it.
	guardFights bool
	// eat, when set, eats one bite of the named food and reports whether it was taken; nil is
	// combatrunner.EatOnce. A host passes its own when its eater is the better one -- ScarabSwarm's
	// confirms a bite by the food leaving the pack, because under fire HP often never rises past
	// its pre-bite value and EatOnce reports a bite it took as a failure.
	eat func(food string) bool
	// foodCount, when set, counts the carried food a bite can come from; nil is
	// combatrunner.FoodCount. It has to agree with eat: combatrunner.FoodCount counts bank notes
	// too, which ScarabSwarm's eater (rightly) will not touch, so with only noted food left the
	// trainer believed it held food it could never eat -- fightStep retried the bite every pass and
	// a watched fight never broke off (review of 95ed10ad, finding 1).
	foodCount func(food string) int
	// ladder, when set, replaces combatplan.MeleeLadder() as the rung table, and pickRung, when
	// set, chooses the rung from it for a band and combat level (the host's spread over the ladder:
	// Stage 5's PickRungSpread with the swarm's board). Nil is the registered script's PickRung
	// over MeleeLadder exactly.
	ladder   []combatplan.Rung
	pickRung func(lowestMelee, combatLevel int) combatplan.RungChoice
	// shedRung, when set, is asked on every pass at the rung r the run fights at, and answers true when the host
	// has moved this account to another rung (the host's board says r is over its cap and the account one of its
	// newest): the run then picks its rung again through pickRung, which keeps the host's choice.
	shedRung func(r combatplan.Rung) bool
	// clickRange is combatrunner.Deps.ClickRange for the run's fights (the host's attackFirstRange).
	clickRange int
	// idle, when set, is the host's use of a respawn wait (no target to attack): it reports whether it spent the wait
	// on something (the fight alch), given the time of the run's last kill and the monster's respawn in ticks.
	idle func(lastKill time.Time, respawnTicks int) bool
	// keepExtra, when set, names more items the trainer's bank step leaves in the pack (ScarabSwarm's
	// walk-alch kit, scarabrunner.Travel.AlchKeeps). Nil keeps what the registered script keeps.
	keepExtra func(name string) bool
	// atBank, when set, runs at the end of every bank trip the trainer makes (bankStep), with the bank still open:
	// ScarabSwarm draws its run staller's jewellery there (scarabrunner.Travel.DrawRubItem), so a necklace banked
	// by anything else comes back on the account's next trip. Nil draws nothing.
	atBank func(bank *actions.Bank)
	// escape, when set, is asked by a watched fight (guardFights) before it eats: it reports whether the account left the fight instead
	// (a teleport out of what its bites cannot outrun on a lagging server, lagescape-1018), which ends the fight as a break-off. Nil
	// never leaves.
	escape func() bool
	// onWalker, when set, is handed the Walker the trainer builds for itself at OnStart (it does not use the host's):
	// ScarabSwarm installs its run staller on it (webwalk.Walker.Prepare), so the trainer's trips to a rung and its
	// bank are stalled like every other walk the account makes. Nil leaves the Walker as built.
	onWalker func(w *webwalk.Walker)
	// walkHold, when set, is asked before the trainer starts a shop trip (gearStep's buying run, supplyStep's arrows, runes and staff:
	// each a multi-stop walk across the map) and answers true when the host will not have that walk made now: ScarabSwarm holds it
	// while the account is hurt with nothing to eat (errandsafe-1018; scarabplan.DecideWalkRest). what names the walk for the host's log.
	// The trainer then trains on exactly as when no purchase is wanted, and asks again on a later pass. Nil holds nothing.
	walkHold func(ctx *Context, what string) bool
	// liftGoals lets the host's melee goals pass the schema's 75 (the ladder's ceiling): the endgame melee stints of a
	// graduate (scarabswarm_endgamemelee.go) train to 99 on a ladder whose top bands the host has extended
	// (combatplan.ExtendTopBands). Off, the goals are the schema's clamped values, exactly as before.
	liftGoals bool
}

// liftTrainerGoals gives the melee goals the host's raw values (up to 99) where they pass the schema's clamp. A raw value
// under the resolved one, or missing, leaves the resolved one.
func liftTrainerGoals(g combatplan.Goals, raw map[string]interface{}) combatplan.Goals {
	lift := func(resolved int, key string) int {
		return max(resolved, min(IntSetting(raw, key, resolved), 99))
	}
	g.Attack, g.Strength, g.Defence = lift(g.Attack, "goalAttack"), lift(g.Strength, "goalStrength"), lift(g.Defence, "goalDefence")
	return g
}

// shopHeld reports whether the host holds the trainer's shop trip (combatTrainerEmbed.walkHold). The registered run has no embed and no host:
// its shop trips are never held.
func (b *combatTrainerBot) shopHeld(ctx *Context) bool {
	return b.embed != nil && b.embed.walkHold != nil && b.embed.walkHold(ctx, "the trainer's shop trip")
}

// pickRungChoice is the rung for the lowest melee level: the host's chooser when it gave one, else
// combatplan.PickRung over the run's ladder.
func (b *combatTrainerBot) pickRungChoice(lowestMelee, combatLevel int) combatplan.RungChoice {
	if b.embed != nil && b.embed.pickRung != nil {
		return b.embed.pickRung(lowestMelee, combatLevel)
	}
	return combatplan.PickRung(b.ladder, lowestMelee, combatLevel, b.cfg.members)
}

// foodCount is the run's carried food, as the eater that will be asked to eat it counts it.
func (b *combatTrainerBot) foodCount(d *combatrunner.Deps) int {
	if b.embed != nil && b.embed.foodCount != nil {
		return b.embed.foodCount(b.cfg.food)
	}
	return combatrunner.FoodCount(d, b.cfg.food)
}

// settingsSource is the map the trainer resolves its schema against.
func (b *combatTrainerBot) settingsSource(ctx *Context) map[string]interface{} {
	if b.embed != nil {
		return b.embed.settings
	}
	return ctx.Settings
}

// endRun is the registered script's ctx.requestStop(), or the host's ended callback.
func (b *combatTrainerBot) endRun(ctx *Context, reason string, goalsMet bool) {
	if b.embed == nil {
		ctx.requestStop()
		return
	}
	if !b.embed.done {
		b.embed.done = true
		if b.embed.ended != nil {
			b.embed.ended(reason, goalsMet)
		}
	}
}

// embedEnded reports whether an embedded trainer has handed control back.
func (b *combatTrainerBot) embedEnded() bool { return b.embed != nil && b.embed.done }

// eatOne eats one bite of the run's food: the host's eater when it passed one, else
// combatrunner.EatOnce as the registered script always has.
func (b *combatTrainerBot) eatOne(d *combatrunner.Deps) bool {
	if b.embed != nil && b.embed.eat != nil {
		if b.embed.eat(b.cfg.food) {
			return true
		}
		// A bite that did not happen costs a tick, so nothing that keeps asking for one (fightStep's
		// eat branch continues straight back into it) can spin.
		d.Delay(600 * time.Millisecond)
		return false
	}
	return combatrunner.EatOnce(d, b.cfg.food)
}

// inFight is what an embedded trainer does about its own hitpoints between two polls of a fight.
type inFight int

const (
	// inFightOn carries on.
	inFightOn inFight = iota
	// inFightEat eats a bite, then carries on.
	inFightEat
	// inFightBreakOff leaves the fight: no food left and the account under the eat threshold.
	inFightBreakOff
)

// decideInFight is fightStep's own between-fights rules asked DURING the fight: its eat rule
// (fighterfood.ShouldEat at eatAtHp) and its "out of food and hurt" rule, which between fights
// sends the run to the bank and inside one breaks the fight off so it can go. Unknown hitpoints
// (0) decide nothing.
func decideInFight(hp, maxHP, food, heal int, eatFrac float64) inFight {
	if hp <= 0 || maxHP <= 0 {
		return inFightOn
	}
	if fighterfood.ShouldEat(fighterfood.EatInput{
		HP: hp, MaxHP: maxHP, Heal: heal, FoodCount: food,
		Mode: fighterfood.EatThreshold, ThresholdFrac: eatFrac,
	}) {
		return inFightEat
	}
	if food <= 0 && float64(hp)/float64(maxHP) < eatFrac {
		return inFightBreakOff
	}
	return inFightOn
}

// fightWatch is one watched fight: the readings and actions decideInFight needs, and what it did.
type fightWatch struct {
	hp       func() (hp, maxHP int)
	food     func() int
	eat      func() bool
	afterEat func()
	// reserve, when set, is the bites fightStep breaks off to the bank at; 0 is none.
	reserve func() int
	heal    int
	eatFrac float64
	// lagFrac, when set, is the share to eat under now (the setting raised after a view lag: combatTrainerBot.eatFracNow); eatFrac
	// otherwise.
	lagFrac func(hp int) float64
	// escape, when set, is asked before every bite and reports whether the account left instead (combatTrainerEmbed.escape).
	escape func() bool

	bites    int
	brokeOff bool
	brokeAt  int
	// atReserve says the break-off was a bite that took the food down to the reserve rather than
	// the food running out: the fight is left while the walk to the bank still has bites to eat
	// (review of 95ed10ad, finding 5), and fightStep's own restock rule then sends it there.
	atReserve bool
}

// step watches once, and reports false once the fight is broken off (and on every call after).
func (w *fightWatch) step() bool {
	if w.brokeOff {
		return false
	}
	hp, maxHP := w.hp()
	frac := w.eatFrac
	if w.lagFrac != nil {
		frac = w.lagFrac(hp)
	}
	switch decideInFight(hp, maxHP, w.food(), w.heal, frac) {
	case inFightEat:
		if w.escape != nil && w.escape() {
			w.brokeOff, w.brokeAt = true, hp // it teleported out: the fight is over, and not for want of food
			return false
		}
		if w.eat() {
			w.bites++
		}
		if w.reserve != nil {
			if left, keep := w.food(), w.reserve(); keep > 0 && left > 0 && left <= keep {
				w.brokeOff, w.brokeAt, w.atReserve = true, hp, true
				return false
			}
		}
		if w.afterEat != nil {
			w.afterEat()
		}
	case inFightBreakOff:
		w.brokeOff, w.brokeAt = true, hp
		return false
	}
	return true
}

// watchedDeps is d with its Delay running w.step first. Every wait inside combatrunner.FightOne
// -- the approach walk and WaitForKill's 200ms poll -- goes through Deps.Delay, so this watches
// the whole fight without a line of combatrunner changing; a break-off answers false, which both
// read as "stop now". w's own actions run on the original d, so a bite never re-enters the watch.
func watchedDeps(d *combatrunner.Deps, w *fightWatch) *combatrunner.Deps {
	fd := *d
	inner := d.Delay
	fd.Delay = func(dur time.Duration) bool {
		if !w.step() {
			return false
		}
		return inner(dur)
	}
	return &fd
}

// newFightWatch is this run's watch over one fight, nil for the registered trainer (and for an
// embed that did not ask for one).
func (b *combatTrainerBot) newFightWatch(ctx *Context, d *combatrunner.Deps, npc gamestate.NPC) *fightWatch {
	if b.embed == nil || !b.embed.guardFights {
		return nil
	}
	st := ctx.Session.State()
	return &fightWatch{
		hp: func() (int, int) {
			hp, _ := st.HP()
			maxHP, _ := st.MaxHP()
			return hp, maxHP
		},
		food: func() int { return b.foodCount(d) },
		eat:  func() bool { return b.eatOne(d) },
		// A bite clears the interaction (the engine's OpHeldHandler calls clearPendingAction), so
		// the account stands idle until the monster's next hit re-triggers auto-retaliate. Pick
		// the target back up at once instead.
		afterEat: func() {
			if st.InCombat() && combatrunner.ObserveTarget(d, npc.Index).Present {
				_ = combatrunner.AttackNPC(d, npc)
			}
		},
		// fightStep's own break-off reserve, under its own conditions.
		reserve: func() int {
			if b.foodEmpty || b.foodScarce || !b.haveRung {
				return 0
			}
			return combatplan.FoodReserve(b.breakOffLoad())
		},
		heal:    fighterfood.FoodHealAmount(b.cfg.food),
		eatFrac: b.cfg.eatFrac,
		lagFrac: func(hp int) float64 { return b.eatFracNow(ctx, hp) },
		escape:  b.embed.escape,
	}
}

// fightOne is combatrunner.FightOne for this run: unchanged for the registered trainer, and for
// an embedded one with guardFights on, watched (the returned watch says what it did; nil when
// unwatched).
func (b *combatTrainerBot) fightOne(ctx *Context, d *combatrunner.Deps, npc gamestate.NPC) (combatrunner.FightResult, *fightWatch) {
	w := b.newFightWatch(ctx, d, npc)
	if w == nil {
		return combatrunner.FightOne(d, npc, fightTimeout), nil
	}
	return combatrunner.FightOne(watchedDeps(d, w), npc, fightTimeout), w
}
