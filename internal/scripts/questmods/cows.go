package questmods

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// CowPenMax is the most fleet accounts cowsTo puts in the Lumbridge cow pen at once, and cowPenFullWait how long one that
// finds it full waits before giving the rung back.
const (
	CowPenMax      = 6
	cowPenFullWait = 2 * time.Minute
)

// cowPen counts the fleet accounts farming the pen in this process (the whole fleet runs in one).
var cowPen penCount

type penCount struct {
	mu sync.Mutex
	n  int
}

func (p *penCount) enter() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.n >= CowPenMax {
		return false
	}
	p.n++
	return true
}

func (p *penCount) leave() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.n > 0 {
		p.n--
	}
}

func (p *penCount) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.n
}

// The Lumbridge cow field (east of the river), and the bones.
var (
	CowField = tile.Tile{X: 3259, Z: 3270}
)

const objBones = questplan.ObjBones

// CowsRung is a training rung on the Lumbridge cows: kill a cow, take its bones, bury them,
// until skill reaches level. It trains Hitpoints and the melee style (the Stage 5 cows20 rung)
// and Prayer through the burying.
// id is the ladder's rung id (cows20, prayer31, prayerkq, prayer43).
//
// A PRAYER rung kills cows last. Cow bones are 4.5 xp apiece at about 175 xp per rung-hour (16 Oct 2026), so
// Prayer 31 was eighty-five hours and every slot timed out, while the mule held 17,154 Bones and 1,488 Big bones. Its
// first step is the bones loop (prayerbones.go): the pack's bones, then the own bank's, then an ask of the mule
// (prayer31 and prayerkq only, questplan.PrayerRungAsksMule). When it finds no bone it fails, and for the rest of the
// run questplan.CowsRungStep sends the account to the cows below. The other rungs have no bones to draw and kill cows
// as they always did.
//
// The Prayer rungs differ in WHY they exist, not in how they train. prayerkq is the Queen's rung
// (questplan.KQPrayerRungID): its level is whatever kqPrayer holds at the Kalphite Queen (37 Protect from Magic,
// 40 Missiles; internal/bot kqPrayerRungLevel), it opens the Stage 5 ladder's unproven tail, and an account at or
// above the level reads it done at once. prayer31 and prayer43 are GATE rungs, inserted only in front of the
// quest whose gate names them (Scorpion Catcher's 31, Horror from the Deep's 43): 43 is about 50,000 xp of cow
// bones that no account reached (best 38, 15 Oct 2026), so it is never a training target of its own.
func CowsRung(id, skill string, level int) questrunner.Quest {
	custom := "cows-" + skill
	decide := func(s questplan.Snapshot) questplan.Step { return questplan.CowsRungStep(s, skill, level) }
	customs := map[string]questrunner.CustomFunc{custom: func(e *questrunner.Engine, _ questplan.Step) bool {
		return cowsTo(e, skill, level, cowBudget(e))
	}}
	if strings.EqualFold(skill, "prayer") {
		customs[questplan.CustomPrayerBones] = func(e *questrunner.Engine, _ questplan.Step) bool {
			return prayerBones(e, id, level, questplan.PrayerRungAsksMule(id))
		}
		customs[questplan.CustomPrayerCows] = func(e *questrunner.Engine, _ questplan.Step) bool {
			return prayerCows(e, id, level)
		}
	}
	return questrunner.Quest{
		Module: questplan.Module{Record: questplan.Record{ID: id, Name: fmt.Sprintf("%s %d", skill, level)},
			Anchor: CowField, Decide: decide, Sustain: questplan.Sustain{FoodIDs: []int{385, 379, 373, 365, 333, 2309, 2142}, EatAt: 6}},
		Customs: customs,
		Done:    func(e *questrunner.Engine) bool { return questrunner.BaseLevels(e.Deps())[skill] >= level },
	}
}

// cowBudget is how long a cows stint may run: what is left of the run, so that the stint ends with the slot and the
// engine does not start a second one after it (live 16 Oct 2026: a 20 minute slot ran 31.8 minutes on average, one
// 15 minute stint after another). 15 minutes outside a run.
func cowBudget(e *questrunner.Engine) time.Duration {
	if left := e.RunLeft(); left > 0 {
		return left
	}
	return 15 * time.Minute
}

// buryAll buries every bone in the pack, the most experience first.
func buryAll(d *questrunner.Deps) {
	for _, k := range questplan.BoneKinds {
		for i := 0; i < 28 && held(d, k.ID); i++ {
			before := questrunner.HeldID(d, k.ID)
			if err := questrunner.OpHeldByName(d, k.ID, "Bury"); err != nil {
				return
			}
			waitUntil(d, 3*time.Second, func() bool { return questrunner.HeldID(d, k.ID) < before })
		}
	}
}

// cowsMaxFailures ends a stint after this many fights in a row fail to start.
const cowsMaxFailures = 20

// cowsTo kills cows, buries their bones, and stops at the level or the budget. It is the Lumbridge pasture's farm (runCowFarm,
// cowfarm.go): the account walks in by the gate, fights the cows a walk over open ground reaches, walks the pasture's beat when none
// is in reach, and says what the stint yields. Until 18 October 2026 it walked to "within 12" of the pasture's middle, which is the
// road outside the gate, picked the nearest cow by distance and attacked it through the fence ("I can't reach that!"): of the 712
// Prayer rung stints in the fleet's logs of 15 to 18 October 399 made no experience at all, the mean was 33 xp (7 bones) in twenty
// minutes and the best 212 (47 bones).
func cowsTo(e *questrunner.Engine, skill string, level int, budget time.Duration) bool {
	d := e.Deps()
	// The pen holds a dozen cows: a crowd shares them and every account trains at a crawl (3 Nov 2026 17:45, 19 accounts in
	// it, 1.5 kills a minute each and 16 of 39 cows lost to another player). Past CowPenMax fleet accounts the rung waits
	// where it stands and gives the turn back; the ladder offers it again later.
	if !cowPen.enter() {
		logf(d, "cows: the Lumbridge cow pen already holds %d fleet accounts (the most is %d); not joining, trying again later", cowPen.count(), CowPenMax)
		d.Delay(cowPenFullWait)
		return false
	}
	defer cowPen.leave()
	reached := func() bool { return questrunner.BaseLevels(d)[skill] >= level }
	// Train the named skill: the melee style that gives its XP (hitpoints comes with any).
	switch strings.ToLower(skill) {
	case "attack":
		_, _ = actions.SetCombatStyle(d.Session, actions.StyleAttack)
	case "strength":
		_, _ = actions.SetCombatStyle(d.Session, actions.StyleStrength)
	case "defence":
		_, _ = actions.SetCombatStyle(d.Session, actions.StyleDefence)
	}
	w := newLiveCows(d, "cows", e.Current().Module.Sustain)
	w.bury = true
	w.done = reached
	w.room = func() bool {
		if questrunner.FreeSlots(d) == 0 {
			buryAll(d)
		}
		return questrunner.FreeSlots(d) > 0
	}
	xp0, _ := skillXP(d, skill)
	w.progress = func() string {
		xp, _ := skillXP(d, skill)
		return fmt.Sprintf("%s +%d xp (%d bones held)", skill, max(xp-xp0, 0), questrunner.HeldID(d, objBones))
	}
	logf(d, "cows: %s %d: up to %s in the pasture; at most %d kills expected in that (%.1f a minute alone, %.1f among %d accounts)",
		skill, level, budget.Round(time.Second), questplan.CowExpectedKills(budget, 1), questplan.CowRatePerMinute(1),
		questplan.CowRatePerMinute(questplan.DemonCowFarmers), questplan.DemonCowFarmers)
	t := runCowFarm(w, "cows", cowSpec{Budget: budget, MaxFailures: cowsMaxFailures})
	logf(d, "cows: %s in the pasture: %d killed, %d given up on, %d timed out (%.1f a minute); %d walks in by the gate, %d stops of the pasture's beat; ended on %s",
		t.Elapsed.Round(time.Second), t.Won, t.GaveUp, t.TimedOut, t.perMinute(), t.Entries, t.Sweeps, t.End)
	return reached()
}

// skillXP is a skill's experience and whether the server has reported it yet.
func skillXP(d *questrunner.Deps, skill string) (int, bool) {
	for i, sk := range d.Session.State().Skills() {
		if strings.EqualFold(gamestate.SkillName(i), skill) {
			return sk.XP, sk.Generation > 0
		}
	}
	return 0, false
}
