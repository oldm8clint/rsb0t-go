package bot

import (
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// bitesOf is how many times a run of the simulator was told to eat id (OPHELD1, the item's first
// option: actions.Eat).
func bitesOf(sim *actionsim.Session, id int32) int {
	n := 0
	for _, b := range sim.Sent() {
		if len(b) == 7 && int(b[0]) == clientprot.OPHELD1 && int32(int(b[1])<<8|int(b[2])) == id {
			n++
		}
	}
	return n
}

func fruitRows(id int32, n int) []actionsim.Stack {
	rows := make([]actionsim.Stack, n)
	for i := range rows {
		rows[i] = actionsim.Stack{ID: id, Count: 1}
	}
	return rows
}

// "crystal chest runs could be sped up by using strange fruit to restore run energy while running
// from the bank to the chest" (a user request): the walk's poll hook eats one when run energy is
// at or under mastercraftplan.ChestFruitRunAt, never with none in the pack or an interface open (a bite
// closes it), and not twice inside the time content refuses a second bite.
func TestWalkAsideEatsAStrangeFruitWhenRunEnergyIsLow(t *testing.T) {
	cfg := chestCfg(t)
	fruit := itemID(t, cfg, "Strange fruit", false)
	for _, c := range []struct {
		name   string
		energy int
		fruit  int
		bank   bool
		want   int
	}{
		{"at the threshold", mastercraftplan.ChestFruitRunAt, 3, false, 1},
		{"nearly empty", 4, 3, false, 1},
		{"one over", mastercraftplan.ChestFruitRunAt + 1, 3, false, 0},
		{"plenty", 90, 3, false, 0},
		{"no fruit", 5, 0, false, 0},
		{"a bank is open: a bite would close it", 5, 3, true, 0},
	} {
		r, _ := chestRig(t, nil, nil, nil)
		r.freshSession(cfg, fruitRows(fruit, c.fruit)) // no bank open: the rig opens one
		r.ctx.requestStop()                            // a bite the simulator does not consume gives up at once
		cc := r.chest(t, cfg, tile.Tile{X: 2951, Z: 3430})
		r.sim.State().Handle(serverprot.UPDATE_RUNENERGY, []byte{byte(c.energy)})
		if c.bank {
			r.sim.OpenBank(nil)
		}
		cc.walkAside(webwalk.AsidePoll{})
		if got := bitesOf(r.sim, fruit); got != c.want {
			t.Errorf("%s: %d bite(s), want %d", c.name, got, c.want)
		}
		// The next poll inside the look interval does not look again.
		cc.walkAside(webwalk.AsidePoll{})
		if got := bitesOf(r.sim, fruit); got != c.want {
			t.Errorf("%s: %d bite(s) after a second poll straight away, want %d", c.name, got, c.want)
		}
	}
	// Run energy not yet read: nothing is guessed.
	r, _ := chestRig(t, nil, nil, nil)
	r.freshSession(cfg, fruitRows(fruit, 3))
	r.ctx.requestStop()
	cc := r.chest(t, cfg, tile.Tile{X: 2951, Z: 3430})
	cc.walkAside(webwalk.AsidePoll{})
	if got := bitesOf(r.sim, fruit); got != 0 {
		t.Errorf("run energy unknown: %d bite(s)", got)
	}
}

// At the chest with a pack too full for the next open that eating its fruit would make room in, the
// round eats one a step (ChestStepEat); the bites are held ChestBiteGap apart.
func TestEatForRoomEatsOneAndCountsIt(t *testing.T) {
	cfg := chestCfg(t)
	fruit := itemID(t, cfg, "Strange fruit", false)
	r, _ := chestRig(t, fruitRows(fruit, 3), nil, nil)
	r.ctx.requestStop()
	cc := r.chest(t, cfg, tile.Tile{X: 2914, Z: 3451})
	if ok, end, why := cc.eatForRoom(); ok || end != "" || why == "" {
		t.Fatalf("a bite the simulator never consumes must fail the step so it is tried again: %v %q %q", ok, end, why)
	}
	if got := bitesOf(r.sim, fruit); got != 1 {
		t.Fatalf("%d bite(s) sent", got)
	}
	if cc.lastBite.IsZero() || time.Since(cc.lastBite) > time.Minute {
		t.Errorf("the bite was not timed: %v", cc.lastBite)
	}
}
