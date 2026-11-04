package questrunner

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/questplan"
)

// pickBite closes the gap with the bite that fits it: a shark is not eaten for four missing hitpoints, and when
// nothing fits the smallest bite goes first.
func TestPickBiteChoosesTheBiteThatFits(t *testing.T) {
	pack := map[int]int{385: 3, 1891: 2, 2309: 1, 995: 900}
	foods := []int{385, 1891, 2309}
	for _, c := range []struct {
		deficit int
		want    int
	}{
		{35, 385},  // the shark fits
		{20, 385},  // exactly
		{19, 1891}, // the cake (4) is the largest of the rest that fits... bread is 4 too, the first of equals wins
		{6, 1891},
		{4, 1891},
		{3, 1891}, // nothing fits: the smallest bite there is (4), the first of the 4s
	} {
		got, ok := pickBite(pack, foods, c.deficit)
		if !ok || got != c.want {
			t.Errorf("deficit %d: %d/%v, want %d", c.deficit, got, ok, c.want)
		}
	}
	// No food held, or none of the listed: no bite.
	if _, ok := pickBite(map[int]int{995: 5}, foods, 10); ok {
		t.Error("picked a bite from a pack with no food")
	}
	// A food the tables do not know is assumed to heal eight.
	if got, ok := pickBite(map[int]int{9999: 1, 1891: 1}, []int{9999, 1891}, 10); !ok || got != 9999 {
		t.Errorf("unknown food: %d/%v, want 9999 (assumed 8, the largest that fits 10)", got, ok)
	}
}

// With no hitpoints read yet nothing is eaten and nothing is sent: TopUp and the guard are safe to call early.
func TestTopUpAndGuardBeforeTheStatsArrive(t *testing.T) {
	d := &Deps{Session: fakeSession{gamestate.New()}, Delay: func(time.Duration) bool { return true }, Log: func(string) {}}
	if n := TopUp(d, []int{385}, 40); n != 0 {
		t.Errorf("TopUp ate %d bites with no hitpoints read", n)
	}
	if n := PrepareForFight(d, []int{385}, 8); n != 0 {
		t.Errorf("PrepareForFight ate %d bites with no hitpoints read", n)
	}
	g := NewFightGuard(d, questplan.NpcArenaOgre, questplan.FightArena.Sustain)
	if foe, ok := g.Foe(); !ok || foe.Name != "Khazard Ogre" {
		t.Fatalf("the guard does not know the ogre: %+v %v", foe, ok)
	}
	// 14 raised to twice the ogre's 8.
	if got := g.Sustain().EatAt; got != 16 {
		t.Errorf("eat line %d, want 16", got)
	}
	if v := g.Poll(0, 0); v.Action == questplan.FightRetreat {
		t.Errorf("a guard with nothing read retreats: %s", v.Why)
	}
	// An id the tables do not know is guarded by the module's own line.
	u := NewFightGuard(d, 1, questplan.FightArena.Sustain)
	if _, ok := u.Foe(); ok {
		t.Error("npc 1 is a known foe")
	}
	if got := u.Sustain().EatAt; got != 14 {
		t.Errorf("unknown foe's eat line %d, want the module's 14", got)
	}
	if ok, _ := u.MeleeWinnable(0); !ok {
		t.Error("an unknown foe is judged unwinnable")
	}
	if ok, _ := g.MeleeWinnable(0); !ok {
		t.Error("with no hitpoints read the swing is judged")
	}
}

// The race is read by the shared loop: a target that is never hurt while the account is, loses the fight at a
// minute, with the old loop still eating (hitpoints above its line, so it had not eaten once and the stall rule, six bites
// with no progress, had nothing to count). Live 16 Oct 2026 Merlin's account went on for three minutes.
func TestMeleeFightRetreatsFromAFightItIsNotHurting(t *testing.T) {
	s := newSimFight()
	s.hp, s.maxHP, s.eatAt = 100, 100, 10
	s.ourDamage, s.theirDamage = 0, 3
	sus := questplan.Sustain{FoodIDs: []int{379}, EatAt: 10}
	start := s.Now()
	end := meleeFightVs(s, sus, 5, 10*time.Minute, nil)
	if end.Outcome != fightRetreated || !strings.Contains(end.Why, "has not moved") {
		t.Fatalf("end %+v", end)
	}
	if took := s.Now().Sub(start); took < questplan.RaceNoProgress || took > questplan.RaceNoProgress+30*time.Second {
		t.Errorf("retreated after %s, want about %s", took, questplan.RaceNoProgress)
	}
	if s.eaten != 0 {
		t.Errorf("ate %d bites: the race was meant to fire before the stall rule could", s.eaten)
	}
}

// ...and a fight the account is winning is not touched by it: the scripted fight of the older tests, with the race wired in.
func TestMeleeFightStillWinsAWinnableFight(t *testing.T) {
	s := newSimFight()
	end := meleeFightVs(s, simSustain, 5, 10*time.Minute, nil)
	if end.Outcome != fightWon {
		t.Fatalf("end %+v after %d bites at %d hitpoints", end, s.eaten, s.hp)
	}
}

// The watch folds the race fields in: elapsed, hitpoints lost, the target's bar as the last hitmark drew it, the food's
// worth (eight a piece when the view does not say), and the foe's hardest hit.
func TestFightWatchCarriesTheRace(t *testing.T) {
	w := fightWatch{peak: 8}
	t0 := time.Unix(1_000_000, 0)
	r := w.observe(t0, fightView{Alive: true, HP: 50, MaxHP: 53, Food: 3}, 14)
	if r.Race.Elapsed != 0 || r.Race.FoodHeal != 24 || r.Race.PeakHit != 8 {
		t.Errorf("first look: %+v", r.Race)
	}
	r = w.observe(t0.Add(10*time.Second), fightView{Alive: true, HP: 41, MaxHP: 53, Food: 3, FoodHeal: 36, Health: 30, Total: 40}, 14)
	if r.Race.Elapsed != 10*time.Second || r.Race.Taken != 9 || r.Race.FoeHealth != 30 || r.Race.FoeTotal != 40 || r.Race.FoodHeal != 36 {
		t.Errorf("second look: %+v", r.Race)
	}
	// A bite raises the hitpoints and adds nothing to the damage taken; the next hit does.
	r = w.observe(t0.Add(12*time.Second), fightView{Alive: true, HP: 45, MaxHP: 53, Food: 2, FoodHeal: 24, Health: 30, Total: 40, Eaten: 1}, 14)
	r = w.observe(t0.Add(14*time.Second), fightView{Alive: true, HP: 42, MaxHP: 53, Food: 2, FoodHeal: 24, Health: 28, Total: 40, Eaten: 1}, 14)
	if r.Race.Taken != 12 || r.Race.FoeHealth != 28 {
		t.Errorf("after a bite: %+v", r.Race)
	}
}

// A top-up never eats the quest's own tools: Merlin's bread is the beggar's, and the floor bite leaves it alone too.
func TestTopUpLeavesTheQuestToolsAlone(t *testing.T) {
	d := &Deps{Session: fakeSession{gamestate.New()}, Delay: func(time.Duration) bool { return true }, Log: func(string) {}}
	protect := func(id int) bool { return id == 2309 }
	d.toolProtect.Store(&protect)
	// Only the tool is on the food list: nothing to eat, and nothing read from the session to find that out.
	if n := TopUp(d, []int{2309}, 50); n != 0 {
		t.Errorf("ate %d bites of a quest tool", n)
	}
	if !d.isQuestTool(2309) || d.isQuestTool(385) {
		t.Error("the protection test itself is wrong")
	}
}

// The Fremennik Trials' Thorvald fight, which FightBack runs with an empty Sustain against Koschei: an account at 33
// hitpoints losing one a hit to a form with 255 hitpoints it takes one a hit off. Against an ordinary foe the race
// calls that lost inside half a minute and walks the account out (a counted near-death that parks the quest); against
// the fourth form, whose pass is the account's hitpoints landing on exactly 0, the loop fights on to the end.
func TestMeleeFightDoesNotRetreatFromAScriptedLoss(t *testing.T) {
	koschei := func() *simFight {
		s := newSimFight()
		s.hp, s.maxHP, s.food = 33, 33, 0
		s.targetHP, s.targetMax = 255, 255
		s.ourDamage, s.theirDamage = 1, 1
		return s
	}
	// The premise: the same fight against a foe that is not scripted is given up, early, with the race's reason.
	s := koschei()
	start := s.Now()
	end := meleeFightVs(s, questplan.Sustain{}, 0, 10*time.Minute, nil)
	if end.Outcome != fightRetreated || s.Now().Sub(start) > 45*time.Second {
		t.Fatalf("the ordinary foe: %+v after %s, want a retreat inside 45s (the premise of the test)", end, s.Now().Sub(start))
	}
	if !strings.Contains(end.Why, "at this pace") {
		t.Errorf("the ordinary foe's retreat: %q, want the pace verdict", end.Why)
	}

	// The scripted form: fought to the end, which is the account's hitpoints reaching 0.
	s = koschei()
	s.scripted = true
	end = meleeFightVs(s, questplan.Sustain{}, 0, 10*time.Minute, nil)
	if end.Outcome != fightDied || s.hp > 0 {
		t.Fatalf("the scripted form: %+v at %d hitpoints, want the fight to run to the account's 0", end, s.hp)
	}
	if s.Now().Sub(start) < 60*time.Second {
		t.Errorf("the scripted form ended after %s: the loop gave the fight up and something else ended it", s.Now().Sub(start))
	}
}

// A fight that began against an earlier form is covered when the form changes under it: the first three forms change
// into the next under the same npc index, and FightBack's one call can span the change. The race is read off the form
// in view at each look, so a fight already 10 seconds old when the fourth form arrives is not retreated from.
func TestMeleeFightDoesNotRetreatOnceTheFormBecomesScripted(t *testing.T) {
	s := newSimFight()
	s.hp, s.maxHP, s.food = 33, 33, 0
	s.targetHP, s.targetMax = 255, 255
	s.ourDamage, s.theirDamage = 1, 1
	s.scriptedFrom = 10 * time.Second
	end := meleeFightVs(s, questplan.Sustain{}, 0, 10*time.Minute, nil)
	if end.Outcome != fightDied {
		t.Fatalf("%+v at %d hitpoints, want the fight to run to the account's 0", end, s.hp)
	}
}

// The view reads the form off the npc in the scene now: the first three forms are judged, the fourth is flagged.
func TestFightViewFlagsKoscheisFourthForm(t *testing.T) {
	for id := questplan.NpcKoscheiFirst; id <= questplan.NpcKoscheiFourth; id++ {
		var v fightView
		v.readTarget(entityquery.Entity{ID: id, Index: 7, Health: 20, TotalHealth: 30, InCombat: true})
		if !v.Alive || v.Health != 20 || v.Total != 30 || !v.InCombat {
			t.Errorf("npc %d: the view lost the target's bar: %+v", id, v)
		}
		if want := id == questplan.NpcKoscheiFourth; v.Scripted != want {
			t.Errorf("npc %d: Scripted %v, want %v", id, v.Scripted, want)
		}
	}
	// The watch carries the flag into the reading DecideFight judges.
	var w fightWatch
	r := w.observe(time.Unix(1_000_000, 0), fightView{Alive: true, HP: 30, MaxHP: 53, Scripted: true}, 0)
	if !r.Scripted {
		t.Error("the watch dropped the scripted flag")
	}
	// ...and the guard a quest's own cast loop builds is told of it by the foe's id.
	d := &Deps{Session: fakeSession{gamestate.New()}, Delay: func(time.Duration) bool { return true }, Log: func(string) {}}
	if !NewFightGuard(d, questplan.NpcKoscheiFourth, questplan.Sustain{}).scripted || NewFightGuard(d, questplan.NpcKoscheiFirst, questplan.Sustain{}).scripted {
		t.Error("the guard's scripted flag is not the fourth form's alone")
	}
}
