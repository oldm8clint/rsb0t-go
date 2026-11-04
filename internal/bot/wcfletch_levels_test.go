package bot

import (
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/scripts/wcfletchrunner"
	"rsb0t-go/internal/scripts/woodcutrunner"
)

// A run that starts before the server has sent its Woodcutting level must not judge the rung on level 0: Loop
// sends no hop until the level is read (or yewLevelsWait has passed), so the settings' homeWorld does not pull a
// spread account off its assigned world and the next run push it back.
func TestYewWorldStepWaitsForTheWoodcuttingLevel(t *testing.T) {
	tag := yewTestTag()
	r := newWorldRig(t, "Unread_"+tag, 92, "91,92", 91, "mule")
	sim := actionsim.New(actionsim.Objs{}, nil) // nothing read yet
	r.sim, r.d.Session = sim, sim
	r.b.home.start(r.ctx, "wcfletch")
	r.b.yewW = yewWorldState{on: true, worlds: []int{91, 92}}

	now := time.Now()
	if r.b.yewWorldStep(r.ctx, r.d, now) {
		t.Fatal("the Woodcutting level is unread: yewWorldStep must say wait")
	}
	if r.b.yewW.assigned != 0 || len(r.f.hops) != 0 {
		t.Fatalf("nothing may be assigned or hopped on an unread level: assigned %d, hops %v", r.b.yewW.assigned, r.f.hops)
	}
	if r.b.yewWorldStep(r.ctx, r.d, now.Add(yewLevelsWait-time.Second)) {
		t.Fatal("still inside yewLevelsWait: wait")
	}
	setSkillLevel(sim, woodcutrunner.WoodcuttingStat, 90)
	setSkillLevel(sim, wcfletchrunner.FletchingStat, 81)
	if !r.b.yewWorldStep(r.ctx, r.d, now.Add(yewLevelsWait-time.Second)) {
		t.Fatal("the level has arrived: the step goes on")
	}
	if r.b.yewW.assigned == 0 {
		t.Fatal("with the level read the yew account is assigned a world")
	}
}

// The wait is bounded: a level that never arrives does not hold the run for ever.
func TestYewWorldStepStopsWaitingAfterYewLevelsWait(t *testing.T) {
	tag := yewTestTag()
	r := newWorldRig(t, "Never_"+tag, 94, "93,94", 93, "mule")
	sim := actionsim.New(actionsim.Objs{}, nil)
	r.sim, r.d.Session = sim, sim
	r.b.yewW = yewWorldState{on: true, worlds: []int{93, 94}}
	now := time.Now()
	r.b.yewWorldStep(r.ctx, r.d, now)
	if !r.b.yewWorldStep(r.ctx, r.d, now.Add(yewLevelsWait+time.Second)) {
		t.Fatal("past yewLevelsWait the step goes on without the level")
	}
}
