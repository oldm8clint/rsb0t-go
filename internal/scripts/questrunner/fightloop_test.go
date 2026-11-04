package questrunner

import (
	"errors"
	"testing"
	"time"

	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/scripts/questplan"
)

// simFight is a scripted fight: a target that loses health only while the account is attacking it,
// an attacker that hurts the account every four ticks whatever the account does, a pack of food,
// and the one rule of the engine that Priest in Peril's monk exposed on 5 Oct 2026: eating clears
// the account's attack (OpHeldHandler.handle -> clearPendingAction -> clearInteraction), and with
// auto-retaliate off nothing starts it again. One Pause of 600 ms is one tick.
type simFight struct {
	now  time.Time
	tick int

	hp, maxHP int
	eatAt     int
	food      int
	heal      int
	eaten     int

	targetHP, targetMax int
	hitSeen             bool
	lastHit             time.Time
	engaged             bool
	ourDamage           int // dealt on every fourth tick while engaged
	theirDamage         int // dealt to the account on every fourth tick

	clicks   int
	failNext bool

	// scripted marks the target as a foe the content scripts the account to lose to (Koschei's fourth form);
	// scriptedFrom makes it one only from that long into the fight (a form that changes under a fight).
	scripted     bool
	scriptedFrom time.Duration
	t0           time.Time
}

func newSimFight() *simFight {
	return &simFight{
		now: time.Unix(1_000_000, 0), t0: time.Unix(1_000_000, 0),
		hp: 33, maxHP: 33, eatAt: 10, food: 4, heal: 12,
		targetHP: 25, targetMax: 25, ourDamage: 2, theirDamage: 3,
	}
}

func (s *simFight) View() fightView {
	v := fightView{Alive: s.targetHP > 0, HP: s.hp, MaxHP: s.maxHP, Food: s.food, Eaten: s.eaten}
	v.Scripted = s.scripted || (s.scriptedFrom > 0 && s.now.Sub(s.t0) >= s.scriptedFrom)
	if s.hitSeen {
		v.Health, v.Total = s.targetHP, s.targetMax
		v.InCombat = s.now.Sub(s.lastHit) < 6*time.Second
	}
	return v
}

func (s *simFight) Click() error {
	if s.failNext {
		s.failNext = false
		return errors.New("refused")
	}
	s.clicks++
	s.engaged = true
	return nil
}

func (s *simFight) Eat() bool {
	if s.hp > s.eatAt || s.food == 0 || s.hp <= 0 {
		return false
	}
	s.food--
	s.eaten++
	s.hp = min(s.hp+s.heal, s.maxHP)
	s.engaged = false // the engine drops the attack when the account uses an item
	return true
}

func (s *simFight) Pause(d time.Duration) bool {
	for left := d; left > 0; left -= 600 * time.Millisecond {
		s.now = s.now.Add(600 * time.Millisecond)
		s.tick++
		if s.tick%4 != 0 {
			continue
		}
		if s.engaged && s.targetHP > 0 {
			s.targetHP = max(s.targetHP-s.ourDamage, 0)
			s.hitSeen, s.lastHit = true, s.now
		}
		if s.targetHP > 0 {
			s.hp = max(s.hp-s.theirDamage, 0)
		}
	}
	return true
}

func (s *simFight) Died() bool     { return s.hp <= 0 }
func (s *simFight) Now() time.Time { return s.now }

var simSustain = questplan.Sustain{FoodIDs: []int{379}, EatAt: 10}

// legacyMeleeFight is the loop execFight and FightBack ran before 5 Oct 2026: attack once, then
// watch and eat. Kept here so the live failure stays reproducible against the scripted engine.
func legacyMeleeFight(env fightEnv, budget time.Duration) fightEnd {
	start := env.Now()
	if err := env.Click(); err != nil {
		return fightEnd{fightClickFailed, err.Error()}
	}
	for env.Now().Sub(start) < budget {
		if !env.Pause(600 * time.Millisecond) {
			return fightEnd{Outcome: fightStopped}
		}
		if env.Died() {
			return fightEnd{Outcome: fightDied}
		}
		env.Eat()
		if !env.View().Alive {
			return fightEnd{Outcome: fightWon}
		}
	}
	return fightEnd{Outcome: fightTimedOut}
}

// The live failure: an account that arrives at 19-29 hitpoints must eat mid-fight, eating clears
// the attack, and the old loop never attacked again. It ate until the food was gone and died with
// the monster at full health.
func TestTheOldLoopLosesAFightThatNeedsABite(t *testing.T) {
	s := newSimFight()
	s.hp, s.food = 19, 12
	end := legacyMeleeFight(s, 3*time.Minute)
	if end.Outcome == fightWon {
		t.Fatalf("the old loop won (target %d/%d): the scripted engine no longer reproduces the live failure", s.targetHP, s.targetMax)
	}
	if s.eaten < 3 {
		t.Fatalf("the old loop ate %d times: the scenario did not need food", s.eaten)
	}
	t.Logf("old loop: %v after %d ticks, ate %d, target %d/%d, account %d/%d", end.Outcome, s.tick, s.eaten, s.targetHP, s.targetMax, s.hp, s.maxHP)
}

// The same fight with the new loop: the attack goes out again after every bite and the target dies.
func TestMeleeFightAttacksAgainAfterEating(t *testing.T) {
	s := newSimFight()
	s.hp, s.food = 19, 12
	reengaged := 0
	end := meleeFight(s, simSustain, 3*time.Minute, func() { reengaged++ })
	if end.Outcome != fightWon || s.targetHP != 0 {
		t.Fatalf("outcome %v (%q), target %d/%d, account %d/%d, ate %d", end.Outcome, end.Why, s.targetHP, s.targetMax, s.hp, s.maxHP, s.eaten)
	}
	if s.eaten == 0 {
		t.Fatal("the scenario never made the account eat, so it proves nothing")
	}
	if s.clicks < 2 || reengaged != 1 {
		t.Fatalf("clicks %d, re-engage callbacks %d: the attack was not sent again after a bite", s.clicks, reengaged)
	}
	t.Logf("won in %d ticks: %d clicks, %d bites, %d/%d hitpoints left", s.tick, s.clicks, s.eaten, s.hp, s.maxHP)
}

// A fight that needs no bite is the one the old loop already won: the attack is not re-sent while
// hits land, except by the slow guard (one click every FightClickAny at most).
func TestMeleeFightThatNeedsNoBiteBarelyReclicks(t *testing.T) {
	s := newSimFight()
	s.hp, s.theirDamage = 33, 1
	start := s.now
	end := meleeFight(s, simSustain, 3*time.Minute, nil)
	if end.Outcome != fightWon || s.eaten != 0 {
		t.Fatalf("outcome %v, ate %d", end.Outcome, s.eaten)
	}
	if most := 1 + int(s.now.Sub(start)/questplan.FightClickAny); s.clicks > most {
		t.Fatalf("%d clicks in %s: more than the first one and one per %s", s.clicks, s.now.Sub(start), questplan.FightClickAny)
	}
}

// Down to the last piece of food at the eat line the account walks away WITH the piece: the
// verdict comes before the bite.
func TestMeleeFightRetreatsBeforeTheLastBite(t *testing.T) {
	s := newSimFight()
	s.hp, s.food, s.targetHP, s.targetMax, s.ourDamage = 11, 1, 100, 100, 1
	end := meleeFight(s, simSustain, 3*time.Minute, nil)
	if end.Outcome != fightRetreated {
		t.Fatalf("outcome %v (%q); account %d/%d, ate %d, target %d", end.Outcome, end.Why, s.hp, s.maxHP, s.eaten, s.targetHP)
	}
	if s.food != 1 || s.eaten != 0 {
		t.Fatalf("the last food was eaten (food %d, ate %d): it should be kept for the walk", s.food, s.eaten)
	}
	if s.hp <= 0 {
		t.Fatalf("the account died before it gave up (%d hitpoints)", s.hp)
	}
}

// A target that never loses health (nothing the account does hurts it) costs FightStallEats bites,
// not every bite the account carries.
func TestMeleeFightGivesUpAfterSixBitesWithoutProgress(t *testing.T) {
	s := newSimFight()
	s.food, s.heal, s.theirDamage, s.ourDamage = 40, 12, 3, 0
	end := meleeFight(s, simSustain, 30*time.Minute, nil)
	if end.Outcome != fightRetreated {
		t.Fatalf("outcome %v (%q); ate %d", end.Outcome, end.Why, s.eaten)
	}
	if s.eaten != questplan.FightStallEats {
		t.Fatalf("ate %d times before giving up, want %d", s.eaten, questplan.FightStallEats)
	}
	if s.food <= 0 || s.hp <= 0 {
		t.Fatalf("food %d, hitpoints %d: it should walk away with food and health left", s.food, s.hp)
	}
}

// Health coming off the target between bites is progress: the stall count starts again.
func TestMeleeFightProgressResetsTheStallCount(t *testing.T) {
	s := newSimFight()
	// The account hurts the target 1 a swing, the attacker hurts the account 5: a bite every few
	// swings, far more than six over the fight, and the target never at rest.
	s.hp, s.food, s.heal, s.theirDamage, s.ourDamage = 20, 60, 14, 5, 1
	s.targetHP, s.targetMax = 90, 90
	end := meleeFight(s, simSustain, 20*time.Minute, nil)
	if end.Outcome != fightWon {
		t.Fatalf("outcome %v (%q): ate %d, target %d/%d", end.Outcome, end.Why, s.eaten, s.targetHP, s.targetMax)
	}
	if s.eaten <= questplan.FightStallEats {
		t.Fatalf("ate only %d: the scenario never passed the stall count", s.eaten)
	}
}

// The attack is not spammed: a long fight against a target that hits back hard sends it again after
// bites and never faster than FightClickMin.
func TestMeleeFightNeverClicksFasterThanTheGap(t *testing.T) {
	s := newSimFight()
	var gaps []time.Duration
	var last time.Time
	rec := &clickRecorder{simFight: s, onClick: func(at time.Time) {
		if !last.IsZero() {
			gaps = append(gaps, at.Sub(last))
		}
		last = at
	}}
	s.hp, s.food, s.targetHP, s.targetMax, s.ourDamage, s.theirDamage = 25, 30, 120, 120, 1, 3
	meleeFight(rec, simSustain, 3*time.Minute, nil)
	if len(gaps) == 0 {
		t.Fatal("no re-clicks at all")
	}
	for _, g := range gaps {
		if g < questplan.FightClickMin {
			t.Fatalf("two attack clicks %s apart, the floor is %s", g, questplan.FightClickMin)
		}
	}
}

type clickRecorder struct {
	*simFight
	onClick func(time.Time)
}

func (c *clickRecorder) Click() error {
	c.onClick(c.simFight.now)
	return c.simFight.Click()
}

// A refused first attack ends the fight at once, with the reason.
func TestMeleeFightReportsAFailedFirstClick(t *testing.T) {
	s := newSimFight()
	s.failNext = true
	end := meleeFight(s, simSustain, time.Minute, nil)
	if end.Outcome != fightClickFailed || end.Why != "refused" {
		t.Fatalf("%+v", end)
	}
}

// A refused re-click (the target walked out of view) is not fatal: the loop goes on and sees the
// target gone or clicks again.
func TestMeleeFightSurvivesARefusedReclick(t *testing.T) {
	s := newSimFight()
	s.hp, s.food = 19, 12
	s.failNext = false
	rec := &failingReclick{simFight: s}
	end := meleeFight(rec, simSustain, 3*time.Minute, nil)
	if end.Outcome != fightWon {
		t.Fatalf("outcome %v (%q)", end.Outcome, end.Why)
	}
}

type failingReclick struct {
	*simFight
	n int
}

func (f *failingReclick) Click() error {
	f.n++
	if f.n == 2 { // the first re-click is refused
		return errors.New("refused")
	}
	return f.simFight.Click()
}

// The account's own death ends the loop.
func TestMeleeFightEndsWhenTheAccountDies(t *testing.T) {
	s := newSimFight()
	s.hp, s.food, s.theirDamage, s.targetHP, s.targetMax, s.ourDamage = 6, 0, 8, 100, 100, 1
	s.eatAt = 0 // no sustain: nothing to retreat on, the account just dies
	end := meleeFight(s, questplan.Sustain{}, 3*time.Minute, nil)
	if end.Outcome != fightDied {
		t.Fatalf("outcome %v (%q), hitpoints %d", end.Outcome, end.Why, s.hp)
	}
}

// busyWithAnotherPlayer: an NPC facing another player is busy, one facing the account, nobody or an
// NPC is not.
func TestBusyWithAnotherPlayer(t *testing.T) {
	const self = 7
	cases := []struct {
		face int
		want bool
	}{
		{-1, false},
		{self + 32768, false},
		{9 + 32768, true},
		{12, false}, // an NPC index
	}
	for _, c := range cases {
		if got := busyWithAnotherPlayer(entityquery.Entity{FaceEntity: c.face}, self); got != c.want {
			t.Errorf("face %d: %v, want %v", c.face, got, c.want)
		}
	}
	if busyWithAnotherPlayer(entityquery.Entity{FaceEntity: 9 + 32768}, -1) {
		t.Error("an unknown own slot counted a monster as busy")
	}
}

// A fight against a chosen npc ends, as a won fight the caller can tell from a kill, when the caller's giveUp says the
// target is no longer worth it: an imp that teleported into the monastery is not walked after for the fight's three minutes.
func TestGiveUpEnvEndsTheFightWhenTheCallerSaysSo(t *testing.T) {
	sim := newSimFight()
	sim.targetHP, sim.targetMax = 10_000, 10_000 // it will not die on its own
	polls := 0
	env := &giveUpEnv{fightEnv: sim, giveUp: func() bool { polls++; return polls >= 5 }}
	end := meleeFightVs(env, questplan.Sustain{}, 0, 3*time.Minute, nil)
	if end.Outcome != fightWon || !env.gaveUp {
		t.Fatalf("outcome %v, gave up %v after %d polls, want a won fight that was given up", end.Outcome, env.gaveUp, polls)
	}
	if sim.now.Sub(sim.t0) > 10*time.Second {
		t.Errorf("the fight went on for %s after the caller gave up", sim.now.Sub(sim.t0))
	}
}

func TestGiveUpEnvLeavesAKillAndAnAbsentTargetAlone(t *testing.T) {
	sim := newSimFight()
	sim.theirDamage = 0
	asked := 0
	env := &giveUpEnv{fightEnv: sim, giveUp: func() bool { asked++; return false }}
	end := meleeFightVs(env, questplan.Sustain{}, 0, 5*time.Minute, nil)
	if end.Outcome != fightWon || env.gaveUp {
		t.Fatalf("a kill read as %v, gave up %v", end.Outcome, env.gaveUp)
	}
	if asked == 0 {
		t.Error("giveUp was never asked during the fight")
	}
	// Once the target has left the scene the caller is not asked about it.
	before := asked
	_ = env.View()
	if asked != before {
		t.Error("giveUp was asked about a target that is no longer in view")
	}
}
