package questrunner

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
)

// The quest fight loop. questplan.DecideFight (fightwatch.go there) has the rules and the live
// evidence for them: eating clears the account's attack, so a loop that attacks once stands idle
// after its first bite, and a loop with no notion of "losing" feeds a monster every piece of food
// the account carries. This file is the I/O around those rules, written against a small interface
// (fightEnv) so the loop itself is tested against a script rather than a server.

// fightView is one poll of a fight.
type fightView struct {
	// Alive is whether the target is still in view on the account's level.
	Alive bool
	// Health and Total are the target's health bar as the last hitmark on it drew it (Total 0
	// until one has landed); InCombat is a hitmark on it inside the client's combat window.
	Health, Total int
	InCombat      bool
	// HP and MaxHP are the account's; Food the pieces of the sustain's food in its pack and FoodHeal what they
	// restore in all (0: not known, which the race reads as eight hitpoints a piece).
	HP, MaxHP int
	Food      int
	FoodHeal  int
	// Eaten is the account's running count of bites eaten (Deps.eaten), by any loop.
	Eaten int
	// Scripted is whether the target, as it is now, is a foe the content scripts the account to lose to
	// (questplan.ScriptedLoss): the loop never walks away from it.
	Scripted bool
}

// readTarget folds the target's entry in the scene into the view.
func (v *fightView) readTarget(e entityquery.Entity) {
	v.Alive = true
	v.Health, v.Total, v.InCombat = e.Health, e.TotalHealth, e.InCombat
	v.Scripted = questplan.ScriptedLoss(e.ID)
}

// fightEnv is what the loop reads and does.
type fightEnv interface {
	// View reads the world once.
	View() fightView
	// Click sends the attack on the target.
	Click() error
	// Eat eats one bite when the sustain says so and reports whether it did.
	Eat() bool
	// Pause waits; false means the bot is stopping.
	Pause(time.Duration) bool
	// Died is whether the account's death line arrived during this fight.
	Died() bool
	// Now is the clock the loop keeps its gaps by.
	Now() time.Time
}

// fightOutcome is how a fight loop ended.
type fightOutcome int

const (
	// fightWon: the target left the scene.
	fightWon fightOutcome = iota
	// fightDied: the account's death line arrived.
	fightDied
	// fightStopped: the bot was asked to stop.
	fightStopped
	// fightTimedOut: the budget ran out with the target still there.
	fightTimedOut
	// fightRetreated: the fight was being lost and the loop gave it up (fightEnd.Why says why).
	fightRetreated
	// fightClickFailed: the first attack could not be sent.
	fightClickFailed
)

// fightEnd is a loop's outcome and, for a retreat or a failed click, a reason fit for a log line.
type fightEnd struct {
	Outcome fightOutcome
	Why     string
}

// fightWatch folds the polls of one fight into the readings questplan.DecideFight takes.
type fightWatch struct {
	started       bool
	baseEaten     int
	eatenSeen     int
	eats          int // bites since the target's health bar last fell
	health        questplan.FightHealth
	lastClick     time.Time
	ateSinceClick bool
	reengaged     int
	// race is the fight's pace so far (questplan.RaceLost), peak the target's hardest hit when the tables
	// know it.
	race questplan.FightRace
	peak int
}

// observe folds one poll in and returns the reading to judge.
func (w *fightWatch) observe(now time.Time, v fightView, eatAt int) questplan.FightReading {
	if !w.started {
		w.started, w.eatenSeen = true, v.Eaten
	}
	if v.Eaten > w.eatenSeen {
		n := v.Eaten - w.eatenSeen
		w.eatenSeen = v.Eaten
		w.eats += n
		w.ateSinceClick = true
	}
	cur := questplan.FightHealth{Health: v.Health, Total: v.Total}
	if questplan.HealthFell(w.health, cur) {
		w.eats = 0
	}
	if cur.Total > 0 {
		w.health = cur
	}
	w.race.Observe(now, v.HP)
	foodHeal := v.FoodHeal
	if foodHeal == 0 {
		foodHeal = v.Food * defaultBite
	}
	return questplan.FightReading{
		HP: v.HP, MaxHP: v.MaxHP, EatAt: eatAt, Food: v.Food,
		EatsSinceProgress: w.eats,
		Scripted:          v.Scripted,
		SinceClick:        now.Sub(w.lastClick),
		AteSinceClick:     w.ateSinceClick,
		TargetInCombat:    v.InCombat,
		Race: questplan.RaceReading{Elapsed: w.race.Elapsed(now), Taken: w.race.Taken(),
			FoeHealth: w.health.Health, FoeTotal: w.health.Total, HP: v.HP, FoodHeal: foodHeal, PeakHit: w.peak},
	}
}

// clicked records an attack click at now.
func (w *fightWatch) clicked(now time.Time) {
	w.lastClick = now
	w.ateSinceClick = false
}

// meleeFight attacks the env's target and fights until it leaves the scene, the account dies, the
// budget ends, or the fight is being lost (questplan.DecideFight). It sends the attack again when
// eating or a dropped click has stopped it, and judges retreat BEFORE eating, so the last piece of
// food is kept for the walk away.
func meleeFight(env fightEnv, sus questplan.Sustain, budget time.Duration, onReengage func()) fightEnd {
	return meleeFightVs(env, sus, 0, budget, onReengage)
}

// meleeFightVs is meleeFight against a foe whose hardest hit is peakHit (0: not known), which the race keeps
// back for the walk away.
func meleeFightVs(env fightEnv, sus questplan.Sustain, peakHit int, budget time.Duration, onReengage func()) fightEnd {
	w := fightWatch{peak: peakHit}
	start := env.Now()
	w.observe(start, env.View(), sus.EatAt) // the baseline for the bite counter
	if err := env.Click(); err != nil {
		return fightEnd{fightClickFailed, err.Error()}
	}
	w.clicked(start)
	for env.Now().Sub(start) < budget {
		if !env.Pause(600 * time.Millisecond) {
			return fightEnd{Outcome: fightStopped}
		}
		if env.Died() {
			return fightEnd{Outcome: fightDied}
		}
		v := env.View()
		if !v.Alive {
			return fightEnd{Outcome: fightWon}
		}
		now := env.Now()
		verdict := questplan.DecideFight(w.observe(now, v, sus.EatAt))
		switch verdict.Action {
		case questplan.FightRetreat:
			return fightEnd{fightRetreated, verdict.Why}
		case questplan.FightReengage:
			// A failed click (the target moved out of view, the server refused) is not fatal: the next
			// poll sees the target gone, or clicks again.
			if env.Click() == nil {
				w.clicked(now)
				w.reengaged++
				if w.reengaged == 1 && onReengage != nil {
					onReengage()
				}
			}
		}
		env.Eat()
	}
	return fightEnd{Outcome: fightTimedOut}
}

// liveFight is the fightEnv over a live session.
type liveFight struct {
	d      *Deps
	target entityquery.Entity
	sus    questplan.Sustain
	mark   time.Time
}

func newLiveFight(d *Deps, target entityquery.Entity, sus questplan.Sustain) *liveFight {
	return &liveFight{d: d, target: target, sus: sus, mark: time.Now()}
}

func (f *liveFight) View() fightView {
	d := f.d
	v := fightView{Eaten: int(d.eaten.Load())}
	v.HP, v.MaxHP = Hitpoints(d)
	pack := PackCounts(d)
	v.Food = f.sus.FoodHeld(pack)
	v.FoodHeal = questplan.FoodPoolHeal(pack, f.sus.FoodIDs)
	for _, e := range entityquery.Npcs(d.Session.State(), d.Config).Within(20).Results() {
		if e.Index == f.target.Index {
			v.readTarget(e)
			break
		}
	}
	return v
}

func (f *liveFight) Click() error { return entityquery.Interact(f.d.Session, f.target, "Attack") }

func (f *liveFight) Eat() bool { return EatIfNeeded(f.d, f.sus.FoodIDs, f.sus.EatAt) }

func (f *liveFight) Pause(t time.Duration) bool { return f.d.Delay(t) }

func (f *liveFight) Died() bool { return DiedSince(f.d, f.mark) }

func (f *liveFight) Now() time.Time { return time.Now() }

// fightLabel names a target for a log line.
func fightLabel(e entityquery.Entity) string {
	if e.Name != "" {
		return e.Name
	}
	return fmt.Sprintf("npc %d", e.ID)
}

// finishFight turns a melee loop's end into the executor's answer, walking away from a lost fight.
// Only a won fight is true.
func finishFight(d *Deps, f *liveFight, end fightEnd) bool {
	switch end.Outcome {
	case fightWon:
		return true
	case fightRetreated:
		retreatFromFight(d, fightLabel(f.target), end.Why)
	case fightClickFailed:
		d.logf("attack %s: %s", fightLabel(f.target), end.Why)
	}
	return false
}

// retreatFromFight gives a fight up: it says so plainly, records the lost fight for the engine
// (Deps.noteRetreat, which counts it toward the quest's park logic) and walks the account away,
// to the running quest's bank or the nearest one, with auto-retaliate off so a hit in the back
// does not turn it round. The eat loop keeps eating on the way.
func retreatFromFight(d *Deps, what, why string) {
	here := currentTile(d)
	dest, name := d.retreatDest(here)
	d.logf("fight: losing to %s (%s): walking away to %s", what, why, name)
	d.noteRetreat(fmt.Sprintf("%s: %s", what, why))
	if d.Session.State().AutoRetaliateOn() && !d.noRetaliate {
		_ = actions.SetAutoRetaliate(d.Session, false)
	}
	if !Travel(d, dest, 3) {
		d.logf("fight: could not reach %s (%d,%d,%d) from %d,%d,%d", name, dest.X, dest.Z, dest.Level, here.X, here.Z, here.Level)
	}
}

// RetreatFromFight is retreatFromFight for a quest step that runs a fight loop of its own (the
// Witch's House shed's cast loop holds a different tile per form, which FightByMagic cannot): the lost
// fight is recorded for the engine and the account walks away.
func RetreatFromFight(d *Deps, what, why string) { retreatFromFight(d, what, why) }

// NoteLostFight records a fight the account gave up for the engine's park logic (the lostFight check in
// Engine.Run counts it like a retreat) WITHOUT walking anywhere: RetreatFromFight also walks to a bank, which
// a fight inside a keep or the arena's pocket cannot do, and the step has its own way out.
func NoteLostFight(d *Deps, what, why string) { d.noteRetreat(fmt.Sprintf("%s: %s", what, why)) }

// ensureRetaliate turns auto-retaliate on for a melee fight when it is off, so a hit on the account
// (which also queues the server's own answer) puts it back on its target, and returns what restores
// the previous state. A quest that wants it off (Module.NoRetaliate) is left alone.
func ensureRetaliate(d *Deps) func() {
	st := d.Session.State()
	if d.noRetaliate || st.AutoRetaliateOn() {
		return func() {}
	}
	if err := actions.SetAutoRetaliate(d.Session, true); err != nil {
		d.logf("auto-retaliate on for the fight: %v", err)
		return func() {}
	}
	waitQuietly(d, st.AutoRetaliateOn, 2*time.Second)
	return func() { _ = actions.SetAutoRetaliate(d.Session, false) }
}

// busyWithAnotherPlayer reports whether an NPC is facing a player other than the account: in a
// single-combat area (the Paterdomus temple is one) the server refuses an attack on it ("Someone
// else is fighting that.") and the account would stand in front of it doing nothing.
func busyWithAnotherPlayer(e entityquery.Entity, self int) bool {
	return self >= 0 && e.FaceEntity >= 32768 && e.FaceEntity-32768 != self
}

// pickFightTarget is the nearest NPC of a type within radius that is not already fighting another
// player, else the nearest of them all (what FindNpcByID returned). An account outside the Wilderness
// never picks one standing inside it (wildernessChase): Goblin Diplomacy farms mail from Goblin Village's
// goblins, which wander up to 15 tiles, and the Wilderness line is six tiles north of the village.
func pickFightTarget(d *Deps, id int, name string, radius int) (entityquery.Entity, bool) {
	q := entityquery.Npcs(d.Session.State(), d.Config).Within(radius)
	if here, ok := currentTileOK(d); ok {
		q = q.Where(func(e entityquery.Entity) bool { return !wildernessChase(here, e.X, e.Z, e.Level) })
	}
	if id > 0 {
		q = q.Where(func(e entityquery.Entity) bool { return e.ID == id })
	} else {
		q = q.Name(name)
	}
	all := q.Results()
	if len(all) == 0 {
		return entityquery.Entity{}, false
	}
	self := d.Session.State().Self()
	for _, e := range all {
		if !busyWithAnotherPlayer(e, self) {
			return e, true
		}
	}
	return all[0], true
}

// wildernessChase reports a target at x,z,level inside the Wilderness while the account at here is outside it:
// chasing it would carry the account across the line into player-killing ground.
func wildernessChase(here tile.Tile, x, z, level int) bool {
	return randomevents.WildernessLevel(here.X, here.Z, here.Level) == 0 && randomevents.WildernessLevel(x, z, level) > 0
}
