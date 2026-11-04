package questrunner

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/questplan"
)

// seedSkill gives the fake session's state one skill at a level, as the server's UPDATE_STAT does.
func seedSkill(st *gamestate.State, stat, level int) {
	p := packet.NewWriter(6)
	p.P1(byte(stat))
	p.P4(int32(levels.XPAtLevel(level)))
	p.P1(byte(level))
	st.Handle(serverprot.UPDATE_STAT, p.Data[:p.Pos])
}

// seedQuestPoints sets the quest-point varp (101).
func seedQuestPoints(st *gamestate.State, qp int) {
	p := packet.NewWriter(3)
	p.P2(101)
	p.P1(byte(qp))
	st.Handle(serverprot.VARP_SMALL, p.Data[:p.Pos])
}

// fakeEngineAt is a fake engine whose account holds the given base levels by lower-case skill (a
// skill not named is level 1). A nil map is an account that meets every level gate: level 99 in
// every skill, which is what the tests that are about slots, parks and prerequisites want.
func fakeEngineAt(sink *logSink, lv map[string]int) *Engine {
	e := fakeEngine0(sink)
	st := e.D.Session.State()
	for stat, name := range gamestate.SkillNames {
		if name == "" {
			continue
		}
		level := 99
		if lv != nil {
			if level = lv[strings.ToLower(name)]; level < 1 {
				level = 1
			}
		}
		seedSkill(st, stat, level)
	}
	return e
}

func skillStat(t *testing.T, name string) int {
	t.Helper()
	for stat, n := range gamestate.SkillNames {
		if strings.EqualFold(n, name) {
			return stat
		}
	}
	t.Fatalf("no skill %q", name)
	return -1
}

// recordQuest is a ladder quest that completes at once but carries the record the real module
// does, so the ladder's own checks are the only thing between it and "running".
func recordQuest(m questplan.Module, done map[string]bool) Quest {
	q := instantQuest(m.Record.ID, done)
	q.Module.Record = m.Record
	return q
}

// trainingRung is a rung that raises one skill to level when it finishes, as its module does.
func trainingRung(id, skill string, level int, st *gamestate.State, stat int, done map[string]bool) Quest {
	q := instantQuest(id, done)
	q.Done = func(*Engine) bool { return done[id] }
	q.Finish = func(*Engine) {
		seedSkill(st, stat, level)
		done[id] = true
	}
	return q
}

// The live account of 14 October 2026: Woodcutting 2, Fletching 1, Crafting 40.
func fremennikAccount() map[string]int {
	return map[string]int{"woodcutting": 2, "fletching": 1, "crafting": 40, "attack": 45, "strength": 45,
		"defence": 40, "hitpoints": 49, "prayer": 37, "magic": 60, "agility": 35}
}

// THE BUG: the live ladder ran the Fremennik Trials for twenty-two accounts at Woodcutting 2 and
// Fletching 1, because FremennikRecord declares no level and the ladder read only the record.
func TestRunLadderDoesNotRunFremennikBelowItsLyreLevels(t *testing.T) {
	sink := &logSink{}
	e := fakeEngineAt(sink, fremennikAccount())
	done := map[string]bool{}
	res := e.RunLadder([]Quest{recordQuest(questplan.Fremennik, done)}, time.Hour, time.Hour)
	if done["fremennik"] || sink.has("running fremennik") {
		t.Fatalf("the Fremennik Trials ran at Woodcutting 2 and Fletching 1: %v", sink.lines)
	}
	if res.Outcome != LadderStuck {
		t.Errorf("outcome %s, want stuck (the only quest is held back)", res.Outcome)
	}
	got := strings.Join(res.Blocked["fremennik"], " | ")
	for _, want := range []string{"needs Woodcutting 40 (have 2; server gate: Olaf's lyre)", "needs Fletching 25 (have 1; server gate: Olaf's lyre)"} {
		if !strings.Contains(got, want) {
			t.Errorf("blocked reasons %q lack %q", got, want)
		}
	}
	if strings.Contains(got, "Crafting") {
		t.Errorf("Crafting 40 is held, it must not be named: %q", got)
	}
	// The log line names the gates and the rungs that train them.
	if !sink.has("[ladder] fremennik is held back, not run: needs Woodcutting 40") ||
		!sink.has("woodcutting40 (not in this ladder), fletching25 (not in this ladder)") {
		t.Errorf("the gate that held the quest back is not in the log: %v", sink.lines)
	}
}

// At the levels the quest runs, whatever it is called, and the hold is said once a call.
func TestRunLadderRunsFremennikOnceItsGatesAreMet(t *testing.T) {
	lv := fremennikAccount()
	lv["woodcutting"], lv["fletching"] = 40, 25
	sink := &logSink{}
	e := fakeEngineAt(sink, lv)
	done := map[string]bool{}
	res := e.RunLadder([]Quest{recordQuest(questplan.Fremennik, done)}, time.Hour, time.Hour)
	if res.Outcome != LadderDone || !done["fremennik"] || sink.has("held back") {
		t.Fatalf("outcome %s, done %v: %v", res.Outcome, done, sink.lines)
	}
}

// The hold is logged once per call however many passes the loop makes over the quest.
func TestRunLadderSaysAHoldOncePerCall(t *testing.T) {
	sink := &logSink{}
	e := fakeEngineAt(sink, fremennikAccount())
	st := e.D.Session.State()
	done := map[string]bool{}
	// Two rungs ahead of it each finish and send the ladder round again; Fremennik is evaluated
	// every time and is held every time.
	qs := []Quest{
		recordQuest(questplan.Fremennik, done),
		instantQuest("hetty", done),
		trainingRung("agility35", "agility", 35, st, skillStat(t, "agility"), done),
	}
	res := e.RunLadder(qs, time.Hour, time.Hour)
	if res.Outcome != LadderStuck {
		t.Fatalf("outcome %s: %v", res.Outcome, sink.lines)
	}
	n := 0
	for _, l := range sink.lines {
		if strings.Contains(l, "fremennik is held back") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the hold was logged %d times, want once: %v", n, sink.lines)
	}
}

// With its training rungs in the list, ahead of it or behind it, the quest waits for them and runs
// after: behind it, the ladder holds it, says which rungs are in the ladder, trains, comes round
// again and runs it.
func TestRunLadderTrainsTheGateRungsThenRunsTheQuest(t *testing.T) {
	for _, quest := range []string{"first", "last"} {
		sink := &logSink{}
		e := fakeEngineAt(sink, fremennikAccount())
		st := e.D.Session.State()
		done := map[string]bool{}
		wc := trainingRung("woodcutting40", "woodcutting", 40, st, skillStat(t, "woodcutting"), done)
		fl := trainingRung("fletching25", "fletching", 25, st, skillStat(t, "fletching"), done)
		fr := recordQuest(questplan.Fremennik, done)
		qs := []Quest{wc, fl, fr}
		if quest == "first" {
			qs = []Quest{fr, wc, fl}
		}
		res := e.RunLadder(qs, time.Hour, time.Hour)
		if res.Outcome != LadderDone || !done["fremennik"] {
			t.Fatalf("%s: outcome %s, done %v: %v", quest, res.Outcome, done, sink.lines)
		}
		at := func(sub string) int {
			for i, l := range sink.lines {
				if strings.Contains(l, sub) {
					return i
				}
			}
			return -1
		}
		w, f, r := at("running woodcutting40"), at("running fletching25"), at("running fremennik")
		if w < 0 || f < w || r < f {
			t.Errorf("%s: the rungs run in order and the quest last: woodcutting40 %d, fletching25 %d, fremennik %d: %v", quest, w, f, r, sink.lines)
		}
		held := sink.has("woodcutting40 (in this ladder), fletching25 (in this ladder)")
		if held != (quest == "first") {
			t.Errorf("%s: hold line said=%v: %v", quest, held, sink.lines)
		}
	}
}

// An unrunnable training rung BLOCKS the quest behind it: it is not skipped. Here the Woodcutting
// rung is parked (its slot ran out with nothing to show), the Fletching rung runs, and the quest
// still waits.
func TestRunLadderAParkedTrainingRungDoesNotLetTheQuestThrough(t *testing.T) {
	sink := &logSink{}
	e := fakeEngineAt(sink, fremennikAccount())
	st := e.D.Session.State()
	e.Store.NotePark("woodcutting40", "no axe from the bank, the mule or Aemad", time.Now())
	done := map[string]bool{}
	qs := []Quest{
		trainingRung("woodcutting40", "woodcutting", 40, st, skillStat(t, "woodcutting"), done),
		trainingRung("fletching25", "fletching", 25, st, skillStat(t, "fletching"), done),
		recordQuest(questplan.Fremennik, done),
	}
	res := e.RunLadder(qs, time.Hour, time.Hour)
	if done["fremennik"] || done["woodcutting40"] {
		t.Fatalf("the quest ran past a parked Woodcutting rung: %v (%v)", done, sink.lines)
	}
	if !done["fletching25"] || res.Outcome != LadderStuck {
		t.Errorf("fletching25 should still have run and the pass end stuck: %v %s", done, res.Outcome)
	}
	if got := strings.Join(res.Blocked["fremennik"], "|"); !strings.Contains(got, "Woodcutting 40 (have 2") || strings.Contains(got, "Fletching") {
		t.Errorf("fremennik is held by Woodcutting alone: %q", got)
	}
	if got := res.Blocked["woodcutting40"]; len(got) != 1 || !strings.HasPrefix(got[0], "parked: ") {
		t.Errorf("woodcutting40 is parked: %v", got)
	}
}

// An account already parked on the Olaf step by the build that had no gate: while it lacks the
// levels the ladder never retries the quest, backoff over or not, and says the gate rather than
// "parked"; when it has them the old park is an ordinary park again.
func TestRunLadderDoesNotRetryAnOlafParkWithoutTheLevels(t *testing.T) {
	const reason = `no progress for 8 ticks on "frem-olaf (Olaf's trial)"`
	for _, tc := range []struct {
		name    string
		parkAt  time.Time
		ranWant bool
		lv      func(map[string]int)
	}{
		{"backoff over, levels missing", time.Now().Add(-3 * time.Hour), false, func(map[string]int) {}},
		{"backoff running, levels missing", time.Now(), false, func(map[string]int) {}},
		{"backoff over, levels there", time.Now().Add(-3 * time.Hour), true, func(m map[string]int) { m["woodcutting"], m["fletching"] = 40, 25 }},
		{"backoff running, levels there", time.Now(), false, func(m map[string]int) { m["woodcutting"], m["fletching"] = 40, 25 }},
	} {
		lv := fremennikAccount()
		tc.lv(lv)
		sink := &logSink{}
		e := fakeEngineAt(sink, lv)
		e.Store.NotePark("fremennik", reason, tc.parkAt)
		done := map[string]bool{}
		res := e.RunLadder([]Quest{recordQuest(questplan.Fremennik, done)}, time.Hour, time.Hour)
		if done["fremennik"] != tc.ranWant {
			t.Errorf("%s: ran %v, want %v (%v)", tc.name, done["fremennik"], tc.ranWant, sink.lines)
		}
		got := strings.Join(res.Blocked["fremennik"], "|")
		missing := lv["woodcutting"] < 40
		if missing && (!strings.Contains(got, "Woodcutting 40") || strings.Contains(got, "parked")) {
			t.Errorf("%s: blocked %q, want the gate and not the park", tc.name, got)
		}
		if !missing && !tc.ranWant && !strings.HasPrefix(got, "parked: ") {
			t.Errorf("%s: blocked %q, want the ordinary park", tc.name, got)
		}
		if att := e.Store.Record("fremennik").Attempts; missing && att != 0 {
			t.Errorf("%s: %d attempts started without the levels", tc.name, att)
		}
	}
}

// A record that states a level itself is told once: CheckRequirements says it, the table does not.
func TestRunLadderDoesNotSayARecordedLevelTwice(t *testing.T) {
	sink := &logSink{}
	e := fakeEngineAt(sink, map[string]int{"attack": 5})
	done := map[string]bool{questplan.MerlinsCrystalID: true}
	res := e.RunLadder([]Quest{instantQuest(questplan.MerlinsCrystalID, done), recordQuest(questplan.HolyGrail, done)}, time.Hour, time.Hour)
	if done[questplan.HolyGrailID] {
		t.Fatalf("The Holy Grail ran at Attack 5: %v", sink.lines)
	}
	n := strings.Count(strings.Join(res.Blocked[questplan.HolyGrailID], "|"), "needs Attack 20")
	if n != 1 {
		t.Errorf("Attack 20 stated %d times, want once: %v", n, res.Blocked[questplan.HolyGrailID])
	}
}

// EVERY rung of LadderFacts: none runs below a hard gate (server, owner, module) or the
// quest-point minimum, and each runs at exactly the levels. The record is bare on purpose: the
// table, not what a module's Record happens to declare, is the guard. An item gate (Smithing 34
// for nails, waived by an owned item the provisioner sees) is left to the provisioner.
func TestEveryLadderFactHardGateHoldsItsQuestBack(t *testing.T) {
	checked := 0
	for id, f := range questplan.LadderFacts {
		var hard []questplan.Gate
		for _, g := range f.Gates {
			if g.Hard() {
				hard = append(hard, g)
			}
		}
		if len(hard) == 0 && f.QPNeeded == 0 {
			continue
		}
		checked++
		rec := questplan.Record{ID: id, Name: f.Name}

		// Below: the quest never runs, and the reasons name every gate.
		sink := &logSink{}
		e := fakeEngineAt(sink, map[string]int{})
		done := map[string]bool{}
		q := instantQuest(id, done)
		q.Module.Record = rec
		res := e.RunLadder([]Quest{q}, time.Hour, time.Hour)
		if done[id] || sink.has("running "+id) {
			t.Errorf("%s ran below its gates: %v", id, sink.lines)
			continue
		}
		got := strings.Join(res.Blocked[id], " | ")
		for _, g := range hard {
			want := "needs " + strings.ToUpper(g.Skill[:1]) + g.Skill[1:] + " " + strconv.Itoa(g.Level)
			if !strings.Contains(got, want) {
				t.Errorf("%s: blocked %q does not name %q", id, got, want)
			}
		}
		if f.QPNeeded > 0 && !strings.Contains(got, "needs "+strconv.Itoa(f.QPNeeded)+" quest points") {
			t.Errorf("%s: blocked %q does not name its %d quest points", id, got, f.QPNeeded)
		}
		if !sink.has("[ladder] " + id + " is held back, not run:") {
			t.Errorf("%s: no log line says which gate held it: %v", id, sink.lines)
		}

		// At the levels: it runs (its prerequisite quests, which the table names, are done first).
		lv := map[string]int{}
		for _, g := range hard {
			if g.Level > lv[g.Skill] {
				lv[g.Skill] = g.Level
			}
		}
		sink2 := &logSink{}
		e2 := fakeEngineAt(sink2, lv)
		if f.QPNeeded > 0 {
			seedQuestPoints(e2.D.Session.State(), f.QPNeeded)
		}
		done2 := map[string]bool{}
		var qs []Quest
		for _, p := range f.Prereqs {
			qs = append(qs, instantQuest(p, done2))
		}
		q2 := instantQuest(id, done2)
		q2.Module.Record = rec
		qs = append(qs, q2)
		res2 := e2.RunLadder(qs, time.Hour, time.Hour)
		if questplan.RungHeld(id) != "" {
			// A rung the ladder is told not to run yet (questplan.HeldRungs) meets every gate here and is still
			// not started; heldrung_test.go pins that, this only keeps the table's gates honest for the others.
			if done2[id] || !sink2.has("[ladder] "+id+" is held back, not run:") {
				t.Errorf("%s is held (HeldRungs) yet ran or did not say so at its gate levels %v: %v", id, lv, sink2.lines)
			}
			continue
		}
		if res2.Outcome != LadderDone || !done2[id] {
			t.Errorf("%s did not run at its gate levels %v: outcome %s, blocked %v, %v", id, lv, res2.Outcome, res2.Blocked, sink2.lines)
		}
	}
	// The table has at least the three that leaked and the three their records already covered.
	if checked < 6 {
		t.Fatalf("only %d rungs with hard gates were checked", checked)
	}
}
