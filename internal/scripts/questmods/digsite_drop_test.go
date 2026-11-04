package questmods

// The one thing in the Digsite Quest that can hurt an account by itself: the liquid, the nitroglycerin, both mixtures and the compound explode
// on Drop for 25 to 65 hitpoints. digDropJunk is the module's only dropper, and it drops by allow-list. These tests run it on the simulated session
// (which implements OPHELD5 as the server does) over a pack full of explosives, tools and loot.

import (
	"testing"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
)

// dgTabs gives the rig the backpack and worn tabs a held op needs (the server refuses an op on a component it does not consider visible) and the
// production config the op names are read from.
func dgTabs(r *csRig, cfg *cachecfg.Config) {
	r.e.D.Config = cfg
	r.e.D.Components = actionsim.TabComponents()
	r.sim.ShowTabs()
}

// dgJunk is the loot a pocket and a dig leave, seven kinds of it.
var dgJunk = []int{952, 1059, 1925, 526, 687, 694, 674}

// dgTools is the tools the full pack holds besides the explosives, the spare brushes and ropes and the food.
var dgTools = []int{questplan.DigObjTrowel, questplan.DigObjSpecimenJar, questplan.DigObjVial, questplan.DigObjPestle, questplan.DigObjTinderbox,
	questplan.DigObjArceniaRoot, questplan.DigObjChestKey}

// dgFullPack fills the rig's 28 slots with every kind of thing the quest carries: 5 explosives, 7 tools, 3 brushes and 4 ropes (two spare of
// each kind past the limits), 2 sharks and 7 kinds of loot.
func dgFullPack(r *csRig) {
	for _, id := range questplan.DigVolatileIDs() {
		r.give(id, 1)
	}
	for _, id := range dgTools {
		r.give(id, 1)
	}
	r.give(questplan.DigObjSpecimenBrush, 3)
	r.give(questplan.DigObjRope, 4)
	r.give(questplan.ObjShark, 2)
	for _, id := range dgJunk {
		r.give(id, 1)
	}
}

func TestTheDigsiteDropsLootAndNeverAnExplosive(t *testing.T) {
	_, _, cfg := csData(t)
	r := newCSRig(t, questplan.DigStudentsAnchor)
	dgTabs(r, cfg)
	dgFullPack(r)
	d := r.e.D
	if questrunner.FreeSlots(d) != 0 {
		t.Fatalf("the pack has %d free slots, want it full", questrunner.FreeSlots(d))
	}
	// Asked to free every slot there is, the dropper stops at the allow-list.
	digDropJunk(d, 28)
	pack := questrunner.PackCounts(d)
	for _, id := range questplan.DigVolatileIDs() {
		if pack[id] != 1 {
			t.Errorf("explosive object %d: %d held, want the 1 that was never dropped", id, pack[id])
		}
	}
	for _, id := range dgTools {
		if pack[id] != 1 {
			t.Errorf("tool %d: %d held, want 1", id, pack[id])
		}
	}
	// The spares went, down to the limits; the loot is gone; the food is not touched.
	if pack[questplan.DigObjSpecimenBrush] != 1 || pack[questplan.DigObjRope] != 2 {
		t.Errorf("%d brushes and %d ropes held, want 1 and 2", pack[questplan.DigObjSpecimenBrush], pack[questplan.DigObjRope])
	}
	for _, id := range dgJunk {
		if pack[id] != 0 {
			t.Errorf("loot %d still held: %d", id, pack[id])
		}
	}
	if pack[questplan.ObjShark] != 2 {
		t.Errorf("%d sharks held, want the 2 the pack began with", pack[questplan.ObjShark])
	}
	if want := 28 - (5 + 7 + 1 + 2 + 2); questrunner.FreeSlots(d) != want {
		t.Errorf("%d free slots, want %d", questrunner.FreeSlots(d), want)
	}
}

// Asked for a few free slots only, it drops the surplus first and then only as much loot as it takes, never touching a tool.
func TestTheDigsiteDropsOnlyAsMuchAsItNeeds(t *testing.T) {
	_, _, cfg := csData(t)
	r := newCSRig(t, questplan.DigStudentsAnchor)
	dgTabs(r, cfg)
	dgFullPack(r)
	d := r.e.D
	free := questrunner.FreeSlots(d)
	digDropJunk(d, free+3)
	if got := questrunner.FreeSlots(d); got != free+3 {
		t.Errorf("%d free slots, want exactly the %d asked for", got, free+3)
	}
	pack := questrunner.PackCounts(d)
	for _, id := range questplan.DigVolatileIDs() {
		if pack[id] != 1 {
			t.Errorf("explosive object %d: %d held", id, pack[id])
		}
	}
	// Already enough room: nothing is dropped.
	free = questrunner.FreeSlots(d)
	digDropJunk(d, free)
	if got := questrunner.FreeSlots(d); got != free {
		t.Errorf("%d free slots after a drop that was not needed, want %d", got, free)
	}
}

// A pack of nothing but explosives and tools has nothing to drop and keeps every item.
func TestTheDigsiteDropsNothingFromAPackOfChemicals(t *testing.T) {
	_, _, cfg := csData(t)
	r := newCSRig(t, questplan.DigStudentsAnchor)
	dgTabs(r, cfg)
	for _, id := range questplan.DigVolatileIDs() {
		r.give(id, 1)
	}
	r.give(questplan.DigObjTrowel, 1)
	d := r.e.D
	digDropJunk(d, 28)
	pack := questrunner.PackCounts(d)
	for _, id := range append(questplan.DigVolatileIDs(), questplan.DigObjTrowel) {
		if pack[id] != 1 {
			t.Errorf("object %d: %d held after a drop with nothing to drop", id, pack[id])
		}
	}
}
