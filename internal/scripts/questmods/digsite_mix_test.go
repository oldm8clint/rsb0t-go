package questmods

// The mixing executor on the simulated session (which scripts item-on-item rules both ways round, as content's [opheldu] pairs do): the four
// recipes in the order the plan asks for them, the order flip, and the Herblore level the content asks of the second and third mix.

import (
	"testing"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
)

var dgRecipes = []struct{ a, b, product, herblore int }{
	{questplan.DigObjCharcoal, questplan.DigObjPestle, questplan.DigObjGroundCharcoal, 0},
	{questplan.DigObjNitrate, questplan.DigObjNitro, questplan.DigObjPreMix, 0},
	{questplan.DigObjPreMix, questplan.DigObjGroundCharcoal, questplan.DigObjPostMix, 10},
	{questplan.DigObjPostMix, questplan.DigObjArceniaRoot, questplan.DigObjCompound, 10},
}

func dgMixRig(t *testing.T, herblore int) *csRig {
	_, _, cfg := csData(t)
	r := newCSRig(t, questplan.DigStudentsAnchor)
	dgTabs(r, cfg)
	osSeed(r, dgHerblore, herblore)
	for _, rc := range dgRecipes {
		r.sim.Combine(int32(rc.a), int32(rc.b), int32(rc.product))
	}
	return r
}

func dgMix(r *csRig, a, b int) bool {
	return digCustomMix(r.e, questplan.Step{Kind: questplan.StepCustom, Custom: questplan.CustomDigMix, ItemID: a, TargetID: b})
}

func TestTheDigsiteMixesEachRecipe(t *testing.T) {
	for _, rc := range dgRecipes {
		for _, flip := range []bool{false, true} {
			r := dgMixRig(t, 10)
			r.give(rc.a, 1)
			r.give(rc.b, 1)
			a, b := rc.a, rc.b
			if flip {
				a, b = b, a
			}
			if !dgMix(r, a, b) || r.held(rc.product) != 1 || r.held(rc.a) != 0 || r.held(rc.b) != 0 {
				t.Errorf("%d with %d (flipped %v): product %d held %d, ingredients %d and %d", rc.a, rc.b, flip, rc.product, r.held(rc.product), r.held(rc.a), r.held(rc.b))
			}
		}
	}
	// A product already in the pack is not mixed again.
	r := dgMixRig(t, 10)
	r.give(questplan.DigObjPreMix, 1)
	r.give(questplan.DigObjNitrate, 1)
	r.give(questplan.DigObjNitro, 1)
	if !dgMix(r, questplan.DigObjNitrate, questplan.DigObjNitro) || r.held(questplan.DigObjPreMix) != 1 || r.held(questplan.DigObjNitrate) != 1 {
		t.Error("a second premix was made")
	}
}

// The second and third mix need Herblore 10; below it nothing is mixed, the ingredients are kept, and the quest is parked by the executor.
func TestTheDigsiteWillNotMixTheSecondOrThirdBelowHerblore10(t *testing.T) {
	for _, rc := range dgRecipes[2:] {
		r := dgMixRig(t, 9)
		r.give(rc.a, 1)
		r.give(rc.b, 1)
		if dgMix(r, rc.a, rc.b) || r.held(rc.product) != 0 || r.held(rc.a) != 1 || r.held(rc.b) != 1 {
			t.Errorf("%d with %d at Herblore 9 was mixed", rc.a, rc.b)
		}
	}
	// The first mix and the grinding ask for no level.
	for _, rc := range dgRecipes[:2] {
		r := dgMixRig(t, 2)
		r.give(rc.a, 1)
		r.give(rc.b, 1)
		if !dgMix(r, rc.a, rc.b) || r.held(rc.product) != 1 {
			t.Errorf("%d with %d at Herblore 2 was not mixed", rc.a, rc.b)
		}
	}
	// Ingredients that are not in the pack, or no recipe: nothing happens.
	r := dgMixRig(t, 30)
	r.give(questplan.DigObjNitrate, 1)
	if dgMix(r, questplan.DigObjNitrate, questplan.DigObjNitro) || dgMix(r, questplan.DigObjNitrate, questplan.DigObjCharcoal) {
		t.Error("a mix without its ingredient or without a recipe went through")
	}
	if got := questrunner.HeldID(r.e.D, questplan.DigObjNitrate); got != 1 {
		t.Errorf("%d nitrates held, want the 1 that was never used", got)
	}
}
