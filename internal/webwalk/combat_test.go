package webwalk

import (
	"strings"
	"testing"
	"time"
)

// fightWorld is a fakeWorld whose player can be attacked, hurt and killed.
type fightWorld struct {
	*fakeWorld
	combat    int
	knownCmb  bool
	attackers []Point
	fighting  func() bool
	hp, maxHP int
	run       bool
	setRuns   int
	diedAt    *time.Time
}

func (f *fightWorld) CombatLevel() (int, bool) { return f.combat, f.knownCmb }
func (f *fightWorld) InCombat() bool           { return f.fighting != nil && f.fighting() }
func (f *fightWorld) HP() (int, int, bool)     { return f.hp, f.maxHP, f.maxHP > 0 }
func (f *fightWorld) RunEnabled() bool         { return f.run }
func (f *fightWorld) SetRun(on bool) error {
	f.setRuns++
	f.run = on
	return nil
}
func (f *fightWorld) DiedSince(mark time.Time) bool { return f.diedAt != nil && !f.diedAt.Before(mark) }
func (f *fightWorld) Attackers(radius int) []Point {
	var out []Point
	for _, a := range f.attackers {
		if Chebyshev(a, f.me) <= radius {
			out = append(out, a)
		}
	}
	return out
}

// field is a 41x21 open grid.
func field(t *testing.T, me Point) (*fightWorld, *Walker, *[]string) {
	col := newGrid().fill(0, 0, 40, 20, 0)
	f := &fightWorld{fakeWorld: newFakeWorld(t, col, me)}
	w := NewWalker(NewPlanner(col, nil), f)
	var logs []string
	w.Log = func(s string) { logs = append(logs, s); t.Log(s) }
	return f, w, &logs
}

func TestWalkerFleesAroundAnAttackerAndStillArrives(t *testing.T) {
	f, w, logs := field(t, Point{X: 0, Z: 10})
	f.attackers = []Point{{X: 12, Z: 10}}
	f.fighting = func() bool { return Chebyshev(f.me, f.attackers[0]) <= 3 }
	res := w.WalkTo(Point{X: 40, Z: 10}, WalkOptions{Combat: CombatOptions{FleeWhenAttacked: true, RunWhenAttacked: true}})
	if !res.Arrived() {
		t.Fatalf("result = %+v", res)
	}
	all := strings.Join(*logs, "\n")
	if !strings.Contains(all, "fleeing: attacked by 1 attacker") {
		t.Errorf("no flee logged:\n%s", all)
	}
	if f.setRuns != 1 || !f.run {
		t.Errorf("run toggled %d times, run=%v", f.setRuns, f.run)
	}
	if w.walk.flees != 1 {
		t.Errorf("flees = %d, want 1 (the same attacker must not re-trigger)", w.walk.flees)
	}
}

func TestWalkerRunsOnLowHitpoints(t *testing.T) {
	f, w, _ := field(t, Point{X: 0, Z: 10})
	f.hp, f.maxHP = 3, 10
	if res := w.WalkTo(Point{X: 20, Z: 10}, WalkOptions{Combat: CombatOptions{RunBelowHP: 30}}); !res.Arrived() {
		t.Fatalf("result = %+v", res)
	}
	if !f.run {
		t.Error("run was not turned on at 30% hitpoints")
	}
	f2, w2, _ := field(t, Point{X: 0, Z: 10})
	f2.hp, f2.maxHP = 4, 10
	w2.WalkTo(Point{X: 20, Z: 10}, WalkOptions{Combat: CombatOptions{RunBelowHP: 30}})
	if f2.run {
		t.Error("run turned on at 40% hitpoints")
	}
}

func TestWalkerStopsWhenThePlayerDies(t *testing.T) {
	f, w, _ := field(t, Point{X: 0, Z: 10})
	f.onDelay = func(n int) {
		if n == 5 {
			at := f.now
			f.diedAt = &at
			f.me = Point{X: 40, Z: 0} // the respawn point
		}
	}
	res := w.WalkTo(Point{X: 40, Z: 20}, WalkOptions{})
	if res.Outcome != OutcomeDied {
		t.Fatalf("result = %+v", res)
	}
	if f.delays > 8 {
		t.Errorf("kept walking for %d delays after dying", f.delays)
	}
}

func TestWalkerHoldsCourseUnderAttackByDefault(t *testing.T) {
	f, w, logs := field(t, Point{X: 0, Z: 10})
	// The fight pins the player for 10 ticks at x=5 (auto retaliate), then lets go.
	pinned := 0
	f.fighting = func() bool { return f.me.X == 5 && pinned < 10 }
	f.onDelay = func(int) {
		if f.me.X == 5 && pinned < 10 {
			pinned++
			f.path = nil
		}
	}
	res := w.WalkTo(Point{X: 30, Z: 10}, WalkOptions{})
	if !res.Arrived() {
		t.Fatalf("result = %+v", res)
	}
	all := strings.Join(*logs, "\n")
	if !strings.Contains(all, "holding course") {
		t.Errorf("did not hold course:\n%s", all)
	}
	if strings.Contains(all, "repathing") || strings.Contains(all, ": scene") {
		t.Errorf("a fight escalated the ladder:\n%s", all)
	}
}

func TestWalkerAvoidsNamedZonesAndAutomaticOnes(t *testing.T) {
	f, w, _ := field(t, Point{X: 0, Z: 10})
	box := []Rect{{MinX: 15, MaxX: 25, MinZ: 5, MaxZ: 15, Level: 0}}
	var visited []Point
	f.onDelay = func(int) { visited = append(visited, f.me) }
	if res := w.WalkTo(Point{X: 40, Z: 10}, WalkOptions{AvoidRects: box}); !res.Arrived() {
		t.Fatalf("result = %+v", res)
	}
	for _, p := range visited {
		if InZones(p, box) {
			t.Fatalf("walked into the zone at %s", p)
		}
	}
	automatic := 0
	for _, z := range KnownDangerZones {
		if z.Automatic {
			automatic += len(z.Rects)
		}
	}
	if len(w.walk.zones) != 1+automatic {
		t.Errorf("walk zones = %d (unknown combat level keeps the automatic zones)", len(w.walk.zones))
	}
	f.knownCmb, f.combat = true, 90
	w.WalkTo(Point{X: 0, Z: 10}, WalkOptions{NoAutomaticZones: true})
	if len(w.walk.zones) != 0 {
		t.Errorf("NoAutomaticZones left %d rects", len(w.walk.zones))
	}
}
