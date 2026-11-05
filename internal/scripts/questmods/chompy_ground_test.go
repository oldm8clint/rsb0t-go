package questmods

// Big Chompy Bird Hunting's hostile ground on a simulated session: the legs off the enclave are walked with TravelCautious and topped up, the pool's legs and the west patches
// price the pack in hand before the account goes (and park the quest for six hours when it loses), a stationary wait gives up when something starts fighting the account, and the
// legs inside the enclave are walked as before. The routes themselves, and that the enclave's walks keep clear of every wolf and ogre, are checked on the real pack in
// chompy_pack_test.go.

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// chWalks records the legs chTravel asked for, split by the walk it chose.
type chWalks struct {
	plain, cautious []tile.Tile
}

// chStubWalks replaces the two walks with recorders that answer ok.
func chStubWalks(t *testing.T, ok bool) *chWalks {
	t.Helper()
	w := &chWalks{}
	oldPlain, oldCautious := chWalk, chWalkCautious
	chWalk = func(_ *questrunner.Deps, dest tile.Tile, _ int) bool { w.plain = append(w.plain, dest); return ok }
	chWalkCautious = func(_ *questrunner.Deps, dest tile.Tile, _ int) bool {
		w.cautious = append(w.cautious, dest)
		return ok
	}
	t.Cleanup(func() { chWalk, chWalkCautious = oldPlain, oldCautious })
	return w
}

func chLogged(r *chRig) string { return strings.Join(r.log, "\n") }

func TestChompyLegsOffTheEnclaveAreWalkedCautiously(t *testing.T) {
	cave := tile.Tile{X: 2630, Z: 2997}
	for _, c := range []struct {
		name     string
		from, to tile.Tile
		cautious bool
	}{
		{"Rantz to the grove", questplan.ChRantzAnchor, questplan.ChGrove, false},
		{"Rantz to the potato patch", questplan.ChRantzAnchor, questplan.ChPotatoPatch, false},
		{"the clearing to the spit", questplan.ChBaitSpot, questplan.ChSpitNear, false},
		{"the grove to the cave", questplan.ChGrove, cave, false},
		{"Rantz to the pool's lip", questplan.ChRantzAnchor, questplan.ChBubbleStand, true},
		{"the lip to the clearing", questplan.ChBubbleStand, questplan.ChBaitSpot, true},
		{"Rantz to the onion patch", questplan.ChRantzAnchor, questplan.ChOnionPatch, true},
		{"the doogle leaves to Rantz", questplan.ChDoogleSpawn, questplan.ChRantzAnchor, true},
		{"Yanille's bank to Rantz", questplan.YanilleBank, questplan.ChRantzAnchor, true},
	} {
		r := newChRig(t, questplan.ChToadies)
		walks := chStubWalks(t, true)
		r.at(c.from)
		if !chTravel(r.e.Deps(), c.to, 3) {
			t.Errorf("%s: the walk was refused", c.name)
		}
		if c.cautious && (len(walks.cautious) != 1 || len(walks.plain) != 0 || walks.cautious[0] != c.to) {
			t.Errorf("%s: plain %v cautious %v, want one cautious walk to %v", c.name, walks.plain, walks.cautious, c.to)
		}
		if !c.cautious && (len(walks.plain) != 1 || len(walks.cautious) != 0 || walks.plain[0] != c.to) {
			t.Errorf("%s: plain %v cautious %v, want one plain walk to %v", c.name, walks.plain, walks.cautious, c.to)
		}
	}
	// An account whose position has not been read is not trusted to be inside the enclave.
	walks := chStubWalks(t, true)
	sim := actionsim.New(actionsim.Objs{}, nil)
	d := &questrunner.Deps{Session: sim, Components: actionsim.Components(), Log: func(string) {}, Delay: func(time.Duration) bool { return true }}
	if _, ok := questrunner.CurrentTileOK(d); ok {
		t.Fatal("the fresh session knows its position")
	}
	if chTravel(d, questplan.ChGrove, 3); len(walks.cautious) != 1 || len(walks.plain) != 0 {
		t.Errorf("an unread position: plain %v cautious %v", walks.plain, walks.cautious)
	}
}

// A conversation opened from off the enclave starts with the hostile leg to its stop, walked cautiously, before the walk to the NPC itself.
func TestChompyTalksFromOffTheEnclaveWalkTheHostileLegFirst(t *testing.T) {
	r := newChRig(t, questplan.ChToadies)
	walks := chStubWalks(t, false)
	r.at(questplan.ChBubbleStand)
	if chOpen(r.e.Deps(), questplan.ChRantzStop) {
		t.Error("a conversation was opened though the walk to Rantz failed")
	}
	if len(walks.cautious) != 1 || walks.cautious[0] != questplan.ChRantzAnchor || len(walks.plain) != 0 {
		t.Errorf("plain %v cautious %v, want one cautious walk to Rantz", walks.plain, walks.cautious)
	}
	if !strings.Contains(chLogged(r), "was not reached") {
		t.Errorf("the log does not say the stop was not reached: %v", r.log)
	}
}

// The pool's lip and the west patches are priced with the pack in hand before the account walks there: with no food the step ends at once, before any walk, saying why; with
// food the walk goes on; and a patch beside Rantz is not priced at all.
func TestChompyHostileStaysPriceThePackBeforeTheyWalk(t *testing.T) {
	// The gate itself.
	r := newChRig(t, questplan.ChToadies)
	r.stock()
	r.at(questplan.ChRantzAnchor)
	if !chGroundGate(r.e, "the swamp pool's lip") {
		t.Fatalf("a strong account with eight sharks was refused: %v", r.log)
	}
	r.take(questplan.ObjShark, 8)
	if chGroundGate(r.e, "the swamp pool's lip") || !strings.Contains(chLogged(r), "hostile ground") || !strings.Contains(chLogged(r), "no food") {
		t.Errorf("an account with no food: %v", r.log)
	}

	// The pool: bellows in hand and no food, the toad step ends before the walk.
	r = newChRig(t, questplan.ChToadies)
	r.stock()
	r.give(questplan.ObjChBellows3, 1)
	r.at(questplan.ChRantzAnchor)
	r.take(questplan.ObjShark, 8)
	walks := chStubWalks(t, true)
	if chInflate(r.e, 0) || len(walks.cautious)+len(walks.plain) != 0 {
		t.Errorf("a toad round with no food walked: plain %v cautious %v", walks.plain, walks.cautious)
	}
	if !strings.Contains(chLogged(r), "swamp pool's lip") {
		t.Errorf("the log does not name the pool: %v", r.log)
	}

	// The west patches: the same pack is refused at the onion, the tomato and the doogle leaves, and at none of the east.
	season := func(r *chRig, id int) bool {
		return chSeason(r.e, questplan.Step{Kind: questplan.StepCustom, Custom: questplan.CustomChSeason, ItemID: id})
	}
	for _, id := range []int{questplan.ObjChOnion, questplan.ObjChTomato, questplan.ObjChDoogle} {
		r = newChRig(t, questplan.ChToldToCook)
		walks = chStubWalks(t, true)
		r.at(questplan.ChRantzAnchor)
		if season(r, id) || len(walks.cautious)+len(walks.plain) != 0 || !strings.Contains(chLogged(r), "hostile ground") {
			t.Errorf("seasoning %d with no food: plain %v cautious %v, log %v", id, walks.plain, walks.cautious, r.log)
		}
	}
	for _, c := range []struct {
		id int
		at tile.Tile
	}{{questplan.ObjChPotato, questplan.ChPotatoPatch}, {questplan.ObjChCabbage, questplan.ChCabbagePatch}} {
		r = newChRig(t, questplan.ChToldToCook)
		walks = chStubWalks(t, true)
		r.at(questplan.ChRantzAnchor)
		season(r, c.id) // the patch is not in this world; what matters is the walk it asked for
		if strings.Contains(chLogged(r), "hostile ground") || len(walks.plain) != 1 || walks.plain[0] != c.at || len(walks.cautious) != 0 {
			t.Errorf("the east seasoning %d with no food: plain %v cautious %v, log %v", c.id, walks.plain, walks.cautious, r.log)
		}
	}
	// With food the west patch is walked to, cautiously.
	r = newChRig(t, questplan.ChToldToCook)
	r.stock()
	walks = chStubWalks(t, false)
	r.at(questplan.ChRantzAnchor)
	if season(r, questplan.ObjChOnion) || len(walks.cautious) != 1 || walks.cautious[0] != questplan.ChOnionPatch || len(walks.plain) != 0 {
		t.Errorf("the onion with eight sharks: plain %v cautious %v, log %v", walks.plain, walks.cautious, r.log)
	}
}

// The pack that passed the decision may not pass the leg: a toad round whose food has gone between the two parks the whole quest for six hours, with the reason, through the engine.
func TestChompyAPoolLegWithNoFoodLeftParksTheQuestForSixHours(t *testing.T) {
	r := newChRig(t, questplan.ChToadies)
	r.stock()
	r.give(questplan.ObjChBellows3, 1)
	r.at(questplan.ChRantzAnchor)
	walks := chStubWalks(t, true)
	rounds := 0
	res := r.run(BigChompy(), func(step questplan.Step) bool {
		if step.Kind == questplan.StepCustom && step.Custom == questplan.CustomChToad {
			rounds++
			r.take(questplan.ObjShark, r.held(questplan.ObjShark)) // the wolves ate the pack's food on the way
			return chToadRound(r.e, step)
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || rounds != 1 || !strings.Contains(res.Reason, "hostile ground") || !strings.Contains(res.Reason, "swamp pool") {
		t.Fatalf("the run ended %s (%q) after %d toad rounds: %v", res.Outcome, res.Reason, rounds, r.log)
	}
	if parked, _ := r.e.Store.Parked(questplan.ChompyID, time.Now().Add(5*time.Hour)); !parked {
		t.Error("the quest is not parked for five hours")
	}
	if parked, _ := r.e.Store.Parked(questplan.ChompyID, time.Now().Add(7*time.Hour)); parked {
		t.Errorf("the quest is parked past the six hours (the park is %s)", questplan.ChFightParkFor)
	}
	if len(walks.cautious)+len(walks.plain) != 0 {
		t.Errorf("the account walked to the pool with no food: plain %v cautious %v", walks.plain, walks.cautious)
	}
}

// A stationary wait (the pick beside an ogre, the bellows at the lip) ends the moment something fights the account, so the step fails at once and the engine's fight-back answers.
func TestChompyAStationaryWaitGivesUpWhenSomethingFightsTheAccount(t *testing.T) {
	r := newChRig(t, questplan.ChToldToCook)
	d := r.e.Deps()
	old := chAttackerNow
	t.Cleanup(func() { chAttackerNow = old })

	chAttackerNow = func(*questrunner.Deps) (entityquery.Entity, bool) { return entityquery.Entity{}, false }
	if !chWaitCalm(d, "a thing", 5*time.Second, func() bool { return true }) {
		t.Error("a condition that holds was not reported")
	}
	start := time.Now()
	if chWaitCalm(d, "a thing", 40*time.Millisecond, func() bool { return false }) || time.Since(start) < 40*time.Millisecond {
		t.Errorf("a calm wait for nothing returned after %s", time.Since(start))
	}

	chAttackerNow = func(*questrunner.Deps) (entityquery.Entity, bool) {
		return entityquery.Entity{ID: questplan.NpcOgre, Index: 3}, true
	}
	start = time.Now()
	if chWaitCalm(d, "the onion to be picked", 20*time.Second, func() bool { return false }) {
		t.Error("the wait reported a condition that never held")
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("the wait went on for %s with an ogre fighting the account", took)
	}
	if !strings.Contains(chLogged(r), "fighting the account") || !strings.Contains(chLogged(r), "the onion to be picked") {
		t.Errorf("the log does not say what happened: %v", r.log)
	}
	// A condition that holds wins over the attack: the pick that landed is a pick.
	if !chWaitCalm(d, "a thing", 5*time.Second, func() bool { return true }) {
		t.Error("a condition that holds was lost to an attack")
	}
}
