package fleetrunner

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/thieverplan"
	"rsb0t-go/internal/scripts/tile"
)

const (
	watchmanEventType = 435 // content's macro_watchman5, the Watchman event of combat 61 to 90
	lagTestHPLevel    = 54
)

var lagTestAccounts atomic.Int64

// lagAccount is a thief of lagTestHPLevel maximum hitpoints at hp, five Bass in the pack, standing in the Ardougne castle, with the view
// current and nothing in its lag memory.
func lagAccount(t *testing.T, hp int) (*Deps, *actionsim.Session) {
	t.Helper()
	cfg := travelTestConfig(t)
	sim := actionsim.New(actionsim.Objs{}, nil)
	setStat(sim, fleetplan.HitpointsStat, levels.XPAtLevel(lagTestHPLevel), hp)
	sim.Give(objOf(t, cfg, "Bass", false), 5)
	simPlace(sim.State(), 2582, 3292)
	n := lagTestAccounts.Add(1)
	d := &Deps{Session: sim, Config: cfg, Delay: func(time.Duration) bool { return false },
		Me: fmt.Sprintf("lageattest%d", n), World: int(9600 + n)}
	return d, sim
}

func lagMemory(ticks int) *gamestate.ViewLag {
	return &gamestate.ViewLag{Recent: gamestate.RecentLag{Ticks: ticks, Ago: 20 * time.Second}}
}

// The walk and wait eater, with the Watchman event facing the account: 11 a hit, so a line of 27 of 54 with a current view and 33 after
// an 11-tick episode (three volleys).
func TestTheWalkEaterEatsAtTheLagLineWithAWatchmanOnTheAccount(t *testing.T) {
	d, sim := lagAccount(t, 33)
	if n, ok := d.Config.Npc(watchmanEventType); !ok || n.Name != "Watchman" {
		t.Fatalf("npc %d = %+v %v, want the Watchman", watchmanEventType, n, ok)
	}
	simNPCsFacing(sim.State(), 5, simNPC{index: 40, typeID: watchmanEventType, dx: 1, dz: 0})
	if hits := AttackerHitList(d); len(hits) != 1 || hits[0] != 11 {
		t.Fatalf("attacker hits = %v, want the event Watchman's 11 (not the name's default 8)", hits)
	}
	// (The simulated server never confirms a bite, so EatIfLow reports false either way: what was sent is the answer.)
	var last time.Time
	EatIfLow(d, &last, "test")
	if eatsSent(sim) != 0 {
		t.Fatal("33 of 54 with a current view and a Watchman on it ate: the line is 27")
	}
	sim.State().OverrideViewLag(lagMemory(11))
	last = time.Time{}
	EatIfLow(d, &last, "test")
	if eatsSent(sim) != 1 {
		t.Fatalf("33 of 54 after an 11-tick lag sent %d eats: the line is 33", eatsSent(sim))
	}
	// 34 is over the line.
	d2, sim2 := lagAccount(t, 34)
	simNPCsFacing(sim2.State(), 5, simNPC{index: 40, typeID: watchmanEventType, dx: 1, dz: 0})
	sim2.State().OverrideViewLag(lagMemory(11))
	EatIfLow(d2, new(time.Time), "test")
	if eatsSent(sim2) != 0 {
		t.Error("34 of 54 after an 11-tick lag ate: the line is 33")
	}
}

// Healthy play keeps the line it had: nothing in the memory, or nothing facing the account, changes nothing.
func TestTheWalkEaterIsUnchangedWithoutALagOrWithoutAnAttacker(t *testing.T) {
	d, sim := lagAccount(t, 30)
	EatIfLow(d, new(time.Time), "test")
	if eatsSent(sim) != 0 {
		t.Error("30 of 54 with nothing on it ate: the line is 27")
	}
	sim.State().OverrideViewLag(lagMemory(15))
	EatIfLow(d, new(time.Time), "test")
	if eatsSent(sim) != 0 {
		t.Error("30 of 54 after a 15-tick lag with nothing facing the account ate: the lag adds nothing without an attacker")
	}
}

func TestLagHitsNamesTheUnseenHitterOnlyAfterALag(t *testing.T) {
	for _, c := range []struct {
		Hits     []int
		InCombat bool
		lag      int
		want     []int
	}{
		{nil, true, 0, nil},
		{nil, false, 12, nil},
		{nil, true, 12, []int{8}},
		{[]int{7}, true, 12, []int{7}},
		{[]int{11, 3}, false, 12, []int{11, 3}},
	} {
		got := LagHits(c.Hits, c.InCombat, c.lag)
		if fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("LagHits(%v, %v, %d) = %v, want %v", c.Hits, c.InCombat, c.lag, got, c.want)
		}
	}
}

// A steal is never sent from a silent view: 18 Oct 04:41 one sent inside a 7 s silence raised the Watchman that killed the thief.
func TestNoStealIsSentWhileTheViewLags(t *testing.T) {
	d, sim := lagAccount(t, 54)
	sim.State().OverrideViewLag(&gamestate.ViewLag{Lagging: true, Silent: true, Age: 4 * time.Second})
	before := len(sim.Sent())
	res := PickpocketRung(d, thieverplan.Method{Label: "Paladin", Anchor: tile.Tile{X: 2582, Z: 3292}, Leash: 8})
	if res.Reason != "lag" || res.Attempted || res.Success {
		t.Fatalf("PickpocketRung on a lagging view = %+v, want reason lag and no attempt", res)
	}
	if n := len(sim.Sent()) - before; n != 0 {
		t.Errorf("%d packet(s) sent while the view lagged", n)
	}
}
