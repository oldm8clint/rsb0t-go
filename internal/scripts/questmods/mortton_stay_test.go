package questmods

// Shades of Mort'ton's stay at the temple on a simulated session: a fight at the walls that is lost (the account died, or walked away to the quest's bank) ends the stay at once
// and never walks the account back; a pack that has eaten down to its last bite ends it; a fight that is won does not end it by itself; an account outside the town is not
// walked back across the swamp. The temple's and the altar's loops are the same stay, and are tested alike.

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
)

// moStay is a builder inside the temple with a hammer and some sharks, and the two seams of the stay under the test's control: attacks is how many passes find a shade hitting
// the account (the first n), fight what the fight with it ends as, and onFight what the world does meanwhile.
type moStay struct {
	*execWorld
	attackers, attacksLeft int
	fights, looks          int
	result                 questrunner.FightResult
	onFight                func()
}

func newMoStay(t *testing.T, sharks int, attacks int, result questrunner.FightResult, onFight func(w *moStay)) *moStay {
	t.Helper()
	pack := []actionsim.Stack{{ID: questplan.ObjHammer, Count: 1}}
	if sharks > 0 {
		pack = append(pack, actionsim.Stack{ID: questplan.ObjShark, Count: sharks})
	}
	w := &moStay{execWorld: newExecWorld(pack...), attacksLeft: attacks, result: result}
	w.e.D.Chat = chatlog.New()
	w.sim.StandAtLevel(questplan.MtTempleInside.X, questplan.MtTempleInside.Z, 0)
	if onFight != nil {
		w.onFight = func() { onFight(w) }
	}
	oldAttacker, oldFight := mtAttacker, mtFight
	t.Cleanup(func() { mtAttacker, mtFight = oldAttacker, oldFight })
	mtAttacker = func(*questrunner.Deps) (entityquery.Entity, bool) {
		w.looks++
		if w.attacksLeft > 0 {
			return entityquery.Entity{ID: questplan.NpcMtShade, Index: 7}, true
		}
		return entityquery.Entity{}, false
	}
	mtFight = func(_ *questrunner.Deps, _ entityquery.Entity, _ questplan.Sustain, _ time.Duration, _ func() bool) questrunner.FightResult {
		w.fights++
		w.attacksLeft--
		if w.fights > 3 {
			t.Fatalf("the stay went on fighting after %d fights: %s", w.fights, w.logged())
		}
		if w.onFight != nil {
			w.onFight()
		}
		return w.result
	}
	return w
}

// noWalk fails the test if the stay asked for a walk back to the temple: the log of an attempted Travel (which cannot succeed in this world) says it was not reached, and the
// stay's own refusal to walk an account in from outside the town says it is not walking back. refusalOK allows the second, for a test whose account is outside the town.
func (w *moStay) noWalk(t *testing.T, what string, refusalOK ...bool) {
	t.Helper()
	log := w.logged()
	bad := []string{"was not reached"}
	if len(refusalOK) == 0 || !refusalOK[0] {
		bad = append(bad, "not walking back")
	}
	for _, b := range bad {
		if strings.Contains(log, b) {
			t.Errorf("%s: the stay tried to walk back (%q): %v", what, b, *w.log)
		}
	}
}

// A shade is fought and the fight is lost: the account walked away (a retreat to the quest's bank) or died. The step ends on that very pass, whether it is the temple's loop or the
// altar's, and the account is not walked back.
func TestMorttonALostFightEndsTheStayWithoutAWalkBack(t *testing.T) {
	retreat := func(w *moStay) { w.sim.StandAtLevel(questplan.VarrockEastBank.X, questplan.VarrockEastBank.Z, 0) }
	die := func(w *moStay) { w.e.D.Chat.Record("Oh dear, you are dead!") }
	stay := map[string]func(w *moStay) bool{
		"the temple": func(w *moStay) bool { return mtTemple(w.e, questplan.Step{}) },
		"the altar":  func(w *moStay) bool { return mtAltar(w.e, questplan.Step{}) },
	}
	for name, run := range stay {
		for how, onFight := range map[string]func(w *moStay){"walked away to the bank": retreat, "died": die, "lost where it stood": nil} {
			w := newMoStay(t, 8, 100, questrunner.FightLost, onFight)
			if run(w) {
				t.Errorf("%s, %s: the stay reported a success", name, how)
			}
			if w.fights != 1 {
				t.Errorf("%s, %s: %d fights, want the one that was lost", name, how, w.fights)
			}
			if !strings.Contains(w.logged(), "was lost: the stay is over") {
				t.Errorf("%s, %s: the log does not say the fight was lost: %v", name, how, *w.log)
			}
			w.noWalk(t, name+", "+how)
		}
	}
	// A fight cut short by a stop ends the stay as well (the bot is stopping).
	w := newMoStay(t, 8, 100, questrunner.FightStopped, nil)
	if mtTemple(w.e, questplan.Step{}) || w.fights != 1 || !strings.Contains(w.logged(), "cut short by a stop") {
		t.Errorf("a stopped fight: %d fights, %v", w.fights, *w.log)
	}
}

// The tick reports each way a fight can end: lost and stopped end the stay, the others go on.
func TestMorttonBuildTickSaysWhetherTheStayGoesOn(t *testing.T) {
	for _, c := range []struct {
		result questrunner.FightResult
		want   mtTick
	}{
		{questrunner.FightLost, mtTickLeave}, {questrunner.FightStopped, mtTickLeave}, {questrunner.FightWon, mtTickWork}, {questrunner.FightTimedOut, mtTickWork},
		{questrunner.FightGaveUp, mtTickWork},
	} {
		w := newMoStay(t, 8, 1, c.result, nil)
		var last time.Time
		if got := mtBuildTick(w.e, &last); got != c.want {
			t.Errorf("a fight that ended %s: tick %d, want %d", mtFightEnd(c.result), got, c.want)
		}
		if !last.IsZero() {
			t.Errorf("a fight that ended %s left the click timer at %v: the next wall is clicked at once", mtFightEnd(c.result), last)
		}
	}
	// No shade and nothing to repair: the stay goes on.
	w := newMoStay(t, 8, 0, questrunner.FightWon, nil)
	var last time.Time
	if got := mtBuildTick(w.e, &last); got != mtTickWork {
		t.Errorf("a quiet pass: tick %d", got)
	}
}

// A fight that is won does not end the stay, but the pack it left is looked at on the next pass: one bite of food is under the stay's floor.
func TestMorttonAWonFightIsFollowedByALookAtThePack(t *testing.T) {
	eaten := func(w *moStay) { w.sim.Remove(questplan.ObjShark, 7) }
	w := newMoStay(t, 8, 1, questrunner.FightWon, eaten)
	if mtTemple(w.e, questplan.Step{}) {
		t.Error("the stay reported a success")
	}
	log := w.logged()
	if w.fights != 1 || strings.Contains(log, "was lost") || !strings.Contains(log, "bite(s) of food left") || !strings.Contains(log, "leaving the temple") {
		t.Errorf("%d fights; the log: %v", w.fights, *w.log)
	}
	w.noWalk(t, "a won fight")
}

// The food floor at the start of a pass: one bite is not a stay, two shades are never looked for.
func TestMorttonAStayOnOneBiteEndsBeforeAnyFight(t *testing.T) {
	for name, run := range map[string]func(w *moStay) bool{
		"the temple": func(w *moStay) bool { return mtTemple(w.e, questplan.Step{}) },
		"the altar":  func(w *moStay) bool { return mtAltar(w.e, questplan.Step{}) },
	} {
		w := newMoStay(t, 1, 5, questrunner.FightWon, nil)
		if run(w) || w.fights != 0 || w.looks != 0 {
			t.Errorf("%s: a stay on one shark ran: %d fights, %d looks, %v", name, w.fights, w.looks, *w.log)
		}
		if !strings.Contains(w.logged(), "bite(s) of food left") {
			t.Errorf("%s: the log does not give the food as the reason: %v", name, *w.log)
		}
	}
}

// A death with no fight to show for it (the line arrives while the account walks, or the shade is not seen) ends the stay on the next pass; so does a position outside the town.
func TestMorttonAStayEndsOnADeathOrOutsideTheTown(t *testing.T) {
	// The death line arrives during a pass in which nothing is hitting the account.
	for name, run := range map[string]func(w *moStay) bool{
		"the temple": func(w *moStay) bool { return mtTemple(w.e, questplan.Step{}) },
		"the altar":  func(w *moStay) bool { return mtAltar(w.e, questplan.Step{}) },
	} {
		w := newMoStay(t, 8, 0, questrunner.FightWon, nil)
		looked := 0
		mtAttacker = func(*questrunner.Deps) (entityquery.Entity, bool) {
			looked++
			w.e.D.Chat.Record("Oh dear, you are dead!")
			return entityquery.Entity{}, false
		}
		if run(w) || looked != 1 || !strings.Contains(w.logged(), "the account died") {
			t.Errorf("%s: a death with no fight: %d looks, %v", name, looked, *w.log)
		}
		w.noWalk(t, name+", a death")
	}
	// Outside the town at the start of a pass (respawned at Lumbridge, or walked to the bank by a fight that was not seen): not walked back across the swamp.
	for name, run := range map[string]func(w *moStay) bool{
		"the temple": func(w *moStay) bool { return mtTemple(w.e, questplan.Step{}) },
		"the altar":  func(w *moStay) bool { return mtAltar(w.e, questplan.Step{}) },
	} {
		w := newMoStay(t, 8, 0, questrunner.FightWon, nil)
		w.sim.StandAtLevel(questplan.VarrockEastBank.X, questplan.VarrockEastBank.Z, 0)
		if run(w) || w.fights != 0 {
			t.Errorf("%s: a builder outside the town ran: %d fights, %v", name, w.fights, *w.log)
		}
		if !strings.Contains(w.logged(), "outside the town") {
			t.Errorf("%s: the log does not say the account is outside the town: %v", name, *w.log)
		}
		w.noWalk(t, name+", outside the town", true)
	}
	// mtStandInside refuses to walk an account in from outside the town, and still takes one that is only a few tiles off.
	w := newMoStay(t, 8, 0, questrunner.FightWon, nil)
	w.sim.StandAtLevel(questplan.VarrockEastBank.X, questplan.VarrockEastBank.Z, 0)
	if mtStandInside(w.e.D) || !strings.Contains(w.logged(), "not walking back") {
		t.Errorf("a walk back from Varrock was made: %v", *w.log)
	}
	w = newMoStay(t, 8, 0, questrunner.FightWon, nil)
	if !mtStandInside(w.e.D) {
		t.Error("an account standing inside the temple was not accepted as inside")
	}
}
