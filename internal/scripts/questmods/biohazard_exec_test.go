package questmods

import (
	"strings"
	"testing"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
)

// The executors' paths that need no map: the early returns that must not send an account anywhere, the refusals that say why, and the
// pieces of the module that are plain functions of the pack and the position.

func (w *execWorld) stand(at tile.Tile) { w.sim.StandAtLevel(at.X, at.Z, at.Level) }

func (w *execWorld) logged() string { return strings.Join(*w.log, "\n") }

func TestBioWearGownPutsTheGownOnByIdAndRefusesWithoutOne(t *testing.T) {
	w := newExecWorld(actionsim.Stack{ID: questplan.BioObjDoctorGown, Count: 1})
	w.sim.SetWearPos(questplan.BioObjDoctorGown, 4)
	if !bioWearGown(w.e.Deps()) {
		t.Fatalf("the gown would not go on: %v", *w.log)
	}
	if w.held(questplan.BioObjDoctorGown) != 0 {
		t.Errorf("the gown is still in the pack: %+v", w.sim.Pack())
	}
	// Worn already: nothing to do.
	if !bioWearGown(w.e.Deps()) {
		t.Error("a worn gown was refused")
	}
	w = newExecWorld()
	if bioWearGown(w.e.Deps()) || !strings.Contains(w.logged(), "no doctors' gown") {
		t.Errorf("a gown-less account put a gown on: %v", *w.log)
	}
}

func TestBioKeyAndCrateReturnAtOnceOrRefuseWithTheirReason(t *testing.T) {
	// A key in the pack is done; no key and not upstairs is a refusal that names the place.
	w := newExecWorld(actionsim.Stack{ID: questplan.BioObjKey, Count: 1})
	if !bioKey(w.e, questplan.Step{}) {
		t.Errorf("an account that holds the key failed the key leg: %v", *w.log)
	}
	w = newExecWorld()
	if bioKey(w.e, questplan.Step{}) || !strings.Contains(w.logged(), "upstairs") {
		t.Errorf("the key leg from the mainland: %v", *w.log)
	}
	// Upstairs with a full pack: no room for the key, and nothing is talked to.
	var full []actionsim.Stack
	for id := 3000; id < 3028; id++ {
		full = append(full, actionsim.Stack{ID: int32(id), Count: 1})
	}
	w = newExecWorld(full...)
	w.stand(questplan.BioSickRoom)
	if bioKey(w.e, questplan.Step{}) || !strings.Contains(w.logged(), "no room") {
		t.Errorf("the key leg with a full pack: %v", *w.log)
	}
	// The crate: the distillator in the pack is done; upstairs with no key is the gate's refusal; elsewhere is a refusal that names the place.
	w = newExecWorld(actionsim.Stack{ID: questplan.BioObjDistillator, Count: 1})
	if !bioCrate(w.e, questplan.Step{}) {
		t.Errorf("an account that holds the distillator failed the crate leg: %v", *w.log)
	}
	w = newExecWorld()
	w.stand(questplan.BioSickRoom)
	if bioCrate(w.e, questplan.Step{}) || !strings.Contains(w.logged(), "no key") {
		t.Errorf("the crate leg with no key: %v", *w.log)
	}
	w = newExecWorld(actionsim.Stack{ID: questplan.BioObjKey, Count: 1})
	if bioCrate(w.e, questplan.Step{}) || !strings.Contains(w.logged(), "upstairs") {
		t.Errorf("the crate leg from the mainland: %v", *w.log)
	}
}

func TestBioHeadquartersLegsKnowWhichSideTheyAreOn(t *testing.T) {
	// In the headquarters already: the door and the stairs are done; from the mainland they are refusals that name the place.
	w := newExecWorld(actionsim.Stack{ID: questplan.BioObjDoctorGown, Count: 1})
	w.stand(questplan.BioHQDoorInside)
	if !bioHQ(w.e, questplan.Step{}) {
		t.Errorf("the door leg from inside the hall: %v", *w.log)
	}
	w = newExecWorld(actionsim.Stack{ID: questplan.BioObjDoctorGown, Count: 1})
	w.stand(questplan.BioSickRoom)
	if !bioUp(w.e, questplan.Step{}) || !bioHQ(w.e, questplan.Step{}) {
		t.Errorf("the stairs or the door from upstairs: %v", *w.log)
	}
	w = newExecWorld(actionsim.Stack{ID: questplan.BioObjDoctorGown, Count: 1})
	if bioHQ(w.e, questplan.Step{}) || !strings.Contains(w.logged(), "West Ardougne") {
		t.Errorf("the door leg from the mainland: %v", *w.log)
	}
	if bioUp(w.e, questplan.Step{}) || !strings.Contains(w.logged(), "in the headquarters") {
		t.Errorf("the stairs leg from the mainland: %v", *w.log)
	}
	// Leaving: from outside it is done at once.
	w = newExecWorld()
	if !bioLeaveHQLeg(w.e, questplan.Step{}) {
		t.Errorf("the way out of the headquarters from the street: %v", *w.log)
	}
	// The way home from the mainland and from the quarter is done at once.
	for _, at := range []tile.Tile{{X: 2616, Z: 3332}, questplan.BioDonkey} {
		w = newExecWorld()
		w.stand(at)
		if !bioHome(w.e, questplan.Step{}) {
			t.Errorf("the way home from %v: %v", at, *w.log)
		}
	}
	// Omart's ladder is done from the west; from anywhere but the mainland it is a refusal.
	w = newExecWorld()
	w.stand(questplan.BioWestLanding)
	if !bioCross(w.e, questplan.Step{}) {
		t.Errorf("the ladder from the west side: %v", *w.log)
	}
	w = newExecWorld()
	w.stand(questplan.BioDonkey)
	if bioCross(w.e, questplan.Step{}) || !strings.Contains(w.logged(), "mainland") {
		t.Errorf("the ladder from Varrock's quarter: %v", *w.log)
	}
}

func TestBioEastLegsRefuseWithoutTheirItems(t *testing.T) {
	for name, c := range map[string]struct {
		f    func(w *execWorld) bool
		say  string
		pack []actionsim.Stack
	}{
		"the bag on the tower with no bag": {func(w *execWorld) bool { return bioFeed(w.e, questplan.Step{}) }, "no bird feed", nil},
		"the cage opened with no cage":     {func(w *execWorld) bool { return bioRelease(w.e, questplan.Step{}) }, "no full pigeon cage", nil},
		"the cauldron with no apples":      {func(w *execWorld) bool { return bioStew(w.e, questplan.Step{}) }, "no rotten apples", nil},
	} {
		w := newExecWorld(c.pack...)
		if c.f(w) || !strings.Contains(w.logged(), c.say) {
			t.Errorf("%s: %v", name, *w.log)
		}
	}
	// Held already: the takes are done at once, and a full pack is a refusal before any walk.
	w := newExecWorld(actionsim.Stack{ID: questplan.BioObjBirdfeed, Count: 1}, actionsim.Stack{ID: questplan.BioObjPigeons, Count: 1},
		actionsim.Stack{ID: questplan.BioObjRottenApples, Count: 1}, actionsim.Stack{ID: questplan.BioObjDoctorGown, Count: 1})
	if !bioBirdfeed(w.e, questplan.Step{}) || !bioPigeons(w.e, questplan.Step{}) || !bioApples(w.e, questplan.Step{}) || !bioGown(w.e, questplan.Step{}) {
		t.Errorf("a take of an item already held: %v", *w.log)
	}
	var full []actionsim.Stack
	for id := 3000; id < 3028; id++ {
		full = append(full, actionsim.Stack{ID: int32(id), Count: 1})
	}
	w = newExecWorld(full...)
	if bioPigeons(w.e, questplan.Step{}) || !strings.Contains(w.logged(), "no room") {
		t.Errorf("a full pack took a cage: %v", *w.log)
	}
	if bioApples(w.e, questplan.Step{}) || !strings.Contains(w.logged(), "no room") {
		t.Errorf("a full pack took apples: %v", *w.log)
	}
}

func TestBioVarrockLegsKnowWhichSideOfTheGateTheyAreOn(t *testing.T) {
	// Already inside: the gate leg inward is done; already outside: the gate leg outward is done; the gate inward from the quarter's
	// far side of a plague pocket is a refusal naming the mainland.
	w := newExecWorld()
	w.stand(questplan.BioDonkey)
	if !bioGateIn(w.e, questplan.Step{}) {
		t.Errorf("the gate inward from inside: %v", *w.log)
	}
	w = newExecWorld()
	if !bioGateOut(w.e, questplan.Step{}) {
		t.Errorf("the gate outward from outside: %v", *w.log)
	}
	w = newExecWorld()
	w.stand(questplan.BioWestLanding)
	if bioGateIn(w.e, questplan.Step{}) || !strings.Contains(w.logged(), "mainland") {
		t.Errorf("the gate inward from the city: %v", *w.log)
	}
	// No vials in the pack: the boys have nothing to take and the leg says it succeeded (nothing was wanted).
	w = newExecWorld()
	if !bioErrands(w.e, questplan.Step{}) {
		t.Errorf("the errands with no vial to give: %v", *w.log)
	}
	// Guidor's suit: without both halves the leg refuses before any door.
	w = newExecWorld(actionsim.Stack{ID: questplan.BioObjPriestGown, Count: 1})
	w.stand(questplan.BioDonkey)
	if bioGuidor(w.e, questplan.Step{}) || !strings.Contains(w.logged(), "priest's garment") {
		t.Errorf("Guidor with half a suit: %v", *w.log)
	}
}

func TestBioHoldsTheKitAndMissingVials(t *testing.T) {
	w := newExecWorld()
	d := w.e.Deps()
	if bioHoldsTheKit(d) || len(missingVials(d)) != 3 {
		t.Error("an empty pack holds the kit")
	}
	for _, id := range append([]int{questplan.BioObjSample}, questplan.BioVials()...) {
		w.sim.Give(int32(id), 1)
	}
	if !bioHoldsTheKit(d) || len(missingVials(d)) != 0 {
		t.Errorf("the pack %+v does not hold the kit", w.sim.Pack())
	}
	w.sim.Remove(int32(questplan.BioObjEthenea), 1)
	if bioHoldsTheKit(d) || len(missingVials(d)) != 1 || missingVials(d)[0] != questplan.BioObjEthenea {
		t.Errorf("missing vials %v", missingVials(d))
	}
	w.sim.Give(int32(questplan.BioObjEthenea), 1)
	w.sim.Remove(int32(questplan.BioObjSample), 1)
	if bioHoldsTheKit(d) {
		t.Error("a pack with no sample holds the kit")
	}
}

// The hunters' zones are rectangles in the walker's own type, one for each hunter, and a walk inside the city uses them (a walk to a
// destination outside it does not: the pocket's way out is not this walk's business).
func TestBioHunterRectsAreTheHuntersReach(t *testing.T) {
	rects := bioHunterRects()
	if len(rects) != len(questplan.BioWestHunters) {
		t.Fatalf("%d rectangles for %d hunters", len(rects), len(questplan.BioWestHunters))
	}
	for i, h := range questplan.BioWestHunters {
		r := h.Reach()
		if rects[i].MinX != r.MinX || rects[i].MaxX != r.MaxX || rects[i].MinZ != r.MinZ || rects[i].MaxZ != r.MaxZ {
			t.Errorf("hunter %d: rectangle %+v, reach %+v", i, rects[i], r)
		}
	}
}
