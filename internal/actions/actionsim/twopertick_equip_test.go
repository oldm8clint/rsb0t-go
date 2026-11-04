package actionsim_test

import (
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
)

// Two actions a tick (twopertick-1012): wearing and removing several pieces in one burst.

const (
	eqDagger = 1205
	eqBody   = 1129
	eqHelm   = 1155
	eqLegs   = 1075
	eqBoots  = 1061
	eqShield = 1171
	eqCape   = 1007
	eqAmulet = 1478
	eqJunk   = 1925
)

type eqTypes map[int]cachecfg.Obj

func (t eqTypes) Obj(id int) (cachecfg.Obj, bool) { o, ok := t[id]; return o, ok }

func eqObjs() eqTypes {
	wear := func(id int, name string, verb string) cachecfg.Obj {
		return cachecfg.Obj{ID: id, Name: name, IOps: [5]string{"", verb}}
	}
	return eqTypes{
		eqDagger: wear(eqDagger, "Bronze dagger", "Wield"),
		eqBody:   wear(eqBody, "Leather body", "Wear"),
		eqHelm:   wear(eqHelm, "Iron full helm", "Wear"),
		eqLegs:   wear(eqLegs, "Iron platelegs", "Wear"),
		eqBoots:  wear(eqBoots, "Leather boots", "Wear"),
		eqShield: wear(eqShield, "Wooden shield", "Wield"),
		eqCape:   wear(eqCape, "Red cape", "Wear"),
		eqAmulet: wear(eqAmulet, "Amulet of magic", "Wear"),
		eqJunk:   {ID: eqJunk, Name: "Bucket", IOps: [5]string{"Fill"}},
	}
}

// A clampWaiter is realWaiter with every wait capped, so a confirm that never comes costs less than half a
// second instead of the production three (and a loaded machine's late tick still lands inside it).
type clampWaiter struct{ realWaiter }

func (c clampWaiter) DelayUntil(cond func() bool, timeout time.Duration) bool {
	if timeout > 400*time.Millisecond {
		timeout = 400 * time.Millisecond
	}
	return c.realWaiter.DelayUntil(cond, timeout)
}

// equipSim is a pack of the given pieces (and a bucket of junk between), with every piece's worn slot set.
func equipSim(t *testing.T, ids ...int32) (*actionsim.Session, *actions.Equipment) {
	t.Helper()
	slotOf := map[int32]int{eqHelm: 0, eqCape: 1, eqAmulet: 2, eqDagger: 3, eqBody: 4, eqShield: 5, eqLegs: 7, eqBoots: 10}
	var pack []actionsim.Stack
	for i, id := range ids {
		pack = append(pack, actionsim.Stack{ID: id, Count: 1})
		if i == 1 {
			pack = append(pack, actionsim.Stack{ID: eqJunk, Count: 1})
		}
	}
	sim := actionsim.New(actionsim.Objs{}, pack)
	for id, slot := range slotOf {
		sim.SetWearPos(id, slot)
	}
	sim.ShowTabs()
	return sim, actions.NewEquipment(sim, actionsim.TabComponents())
}

func maxPerTick(sim *actionsim.Session) (most, ticksWithEvents int) {
	for _, n := range sim.PerTick() {
		if n > 0 {
			ticksWithEvents++
		}
		if n > most {
			most = n
		}
	}
	return most, ticksWithEvents
}

// Three pieces are three clicks in ONE tick, not three round trips, and the one confirm sees all three.
func TestEquipManyWearsThreePiecesInOneTick(t *testing.T) {
	sim, eq := equipSim(t, eqBody, eqHelm, eqLegs)
	runTicks(t, sim, 30*time.Millisecond)

	r := eq.EquipMany(eqObjs(), []int32{eqBody, eqHelm, eqLegs}, realWaiter{})
	if !r.OK() || len(r.Done) != 3 || len(r.Retried) != 0 {
		t.Fatalf("batch = %+v", r)
	}
	if r.Bursts != 1 {
		t.Fatalf("%d bursts, want the three clicks in one", r.Bursts)
	}
	if most, ticks := maxPerTick(sim); most != 3 || ticks != 1 {
		t.Fatalf("the engine read %v (most in a tick %d, ticks with events %d), want 3 events in 1 tick", sim.PerTick(), most, ticks)
	}
	if len(sim.Worn()) != 3 {
		t.Fatalf("worn panel = %v, want the three pieces", sim.Worn())
	}
	for _, id := range []int32{eqBody, eqHelm, eqLegs} {
		if !eq.IsWorn(id) {
			t.Fatalf("%d not worn", id)
		}
	}
	// The junk the pieces sat around is untouched, and only the three clicks were sent.
	if got := sim.Pack(); len(got) != 1 || got[0].ID != eqJunk {
		t.Fatalf("pack = %v, want only the bucket left", got)
	}
	if n := len(sim.Sent()); n != 3 {
		t.Fatalf("%d packets sent, want 3", n)
	}
	t.Log(r.Log("wore"))
}

// Seven pieces are five in the first tick and two in the next: nothing is queued ahead of the engine.
func TestEquipManyPacesAtFiveATick(t *testing.T) {
	sim, eq := equipSim(t, eqBody, eqHelm, eqLegs, eqBoots, eqShield, eqCape, eqAmulet)
	runTicks(t, sim, 30*time.Millisecond)

	r := eq.EquipMany(eqObjs(), []int32{eqBody, eqHelm, eqLegs, eqBoots, eqShield, eqCape, eqAmulet}, realWaiter{})
	if !r.OK() || len(r.Done) != 7 {
		t.Fatalf("batch = %+v", r)
	}
	if r.Bursts != 2 {
		t.Fatalf("%d bursts, want 5 + 2", r.Bursts)
	}
	if got := sim.MaxBacklog(); got > actionsim.UserEventsPerTick {
		t.Fatalf("%d packets waiting when a tick began, want at most %d", got, actionsim.UserEventsPerTick)
	}
	most, _ := maxPerTick(sim)
	if most != 5 {
		t.Fatalf("per tick %v, want a tick of 5", sim.PerTick())
	}
	if len(sim.Worn()) != 7 {
		t.Fatalf("worn = %v", sim.Worn())
	}
}

// The player is delayed (a spell just cast) when the burst lands: every click is refused, and the pieces are
// worn one at a time afterwards, the old path, once the delay is over.
func TestEquipManyRetriesWhatTheServerRefused(t *testing.T) {
	sim, eq := equipSim(t, eqBody, eqHelm)
	runTicks(t, sim, 30*time.Millisecond)
	sim.Delay(2)

	r := eq.EquipMany(eqObjs(), []int32{eqBody, eqHelm}, clampWaiter{})
	if !r.OK() {
		t.Fatalf("batch = %+v, want both worn after the retry", r)
	}
	if len(r.Done) != 0 || len(r.Retried) != 2 {
		t.Fatalf("done %v retried %v, want both on the retry", r.Done, r.Retried)
	}
	if sim.Refused() < 2 {
		t.Fatalf("the engine refused %d packets, want the burst's two", sim.Refused())
	}
	if len(sim.Worn()) != 2 {
		t.Fatalf("worn = %v", sim.Worn())
	}
	t.Log(r.Log("wore"))
}

// A piece the pack does not hold, one already worn, one with no wear option and a duplicate id send nothing
// and are reported; the rest still goes in one burst.
func TestEquipManyReportsWhatItCouldNotSend(t *testing.T) {
	sim, eq := equipSim(t, eqBody, eqHelm)
	sim.Wear(3, eqDagger) // the dagger is already worn
	runTicks(t, sim, 30*time.Millisecond)

	r := eq.EquipMany(eqObjs(), []int32{eqBody, eqBody, eqDagger, eqCape, eqJunk, eqHelm}, realWaiter{})
	if len(r.Done) != 2 || len(r.Already) != 1 || r.Already[0] != eqDagger {
		t.Fatalf("batch = %+v", r)
	}
	if len(r.Missing) != 1 || r.Missing[0] != eqCape || len(r.Unusable) != 1 || r.Unusable[0] != eqJunk {
		t.Fatalf("missing %v unusable %v, want the cape and the bucket", r.Missing, r.Unusable)
	}
	if r.Wanted != 5 {
		t.Fatalf("wanted %d, want 5 distinct", r.Wanted)
	}
	if n := len(sim.Sent()); n != 2 {
		t.Fatalf("%d packets sent, want just the two wearable pieces", n)
	}
}

// Taking pieces off is the same: the clicks name the worn slots read once, five to a tick, and the pack
// holds them all afterwards.
func TestUnequipManyTakesPiecesOffInOneTick(t *testing.T) {
	sim, eq := equipSim(t, eqJunk)
	sim.Wear(0, eqHelm)
	sim.Wear(4, eqBody)
	sim.Wear(7, eqLegs)
	runTicks(t, sim, 30*time.Millisecond)

	r := eq.UnequipMany([]int32{eqHelm, eqBody, eqLegs, eqCape}, realWaiter{})
	if !r.OK() || len(r.Done) != 3 || len(r.Already) != 1 {
		t.Fatalf("batch = %+v", r)
	}
	if most, ticks := maxPerTick(sim); most != 3 || ticks != 1 {
		t.Fatalf("per tick %v, want 3 events in 1 tick", sim.PerTick())
	}
	if len(sim.Worn()) != 0 || len(sim.Pack()) != 4 {
		t.Fatalf("worn %v pack %v", sim.Worn(), sim.Pack())
	}
	t.Log(r.Log("took off"))
}

// By name, with a name nothing worn carries skipped.
func TestUnequipManyByName(t *testing.T) {
	sim, eq := equipSim(t, eqJunk)
	sim.Wear(0, eqHelm)
	sim.Wear(4, eqBody)
	runTicks(t, sim, 30*time.Millisecond)

	r := eq.UnequipManyByName(eqObjs(), []string{"iron FULL helm", "Dragon boots", "Leather body"}, realWaiter{})
	if !r.OK() || len(r.Done) != 2 {
		t.Fatalf("batch = %+v", r)
	}
	if len(sim.Worn()) != 0 {
		t.Fatalf("worn = %v", sim.Worn())
	}
}
