package fisherrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/protocol/clientprot"
)

// The fishing spot is clicked through the shared gate (docs/click_flood_audit_2026-10-15.md): FishUntil's re-find loop can come
// round to the spot it has just clicked within a tick or two (a whirlpool guard, a junk drop, a chat), and nothing in the loop
// waits between a break and the next click.
func TestTheSpotIsClickedAtMostOnceEveryTwoTicksHoweverFastTheLoopComesRound(t *testing.T) {
	sim := actionsim.New(actionsim.Objs{}, nil)
	sim.StandAt(3000, 3000)
	clock := actionsim.NewClock(sim)
	clicks := sim.RecordClicks()
	d := &Deps{Session: sim, Config: &cachecfg.Config{}, Chat: chatlog.New(), Delay: clock.Delay}
	spot := gamestate.NPC{Index: 41, X: 3001, Z: 3000}
	for i := 0; i < 25; i++ {
		if err := manipClick(d, 1, spot); err != nil {
			t.Fatal(err)
		}
	}
	if n := clicks.Count(clientprot.OPNPC1); n != 25 {
		t.Fatalf("%d clicks for 25 calls", n)
	}
	if most, _ := clicks.MaxPerTick(clientprot.OPNPC1); most > 1 {
		t.Errorf("%d clicks on the spot in one tick", most)
	}
	if ticks := int(clock.Now() / (600 * time.Millisecond)); ticks < 24*actions.ClickGapTicks {
		t.Errorf("25 clicks on one spot took %d ticks; the gate wants at least %d", ticks, 24*actions.ClickGapTicks)
	}
}

// Another spot is never held back by the last one's gap.
func TestAnotherSpotIsClickedAtOnce(t *testing.T) {
	sim := actionsim.New(actionsim.Objs{}, nil)
	sim.StandAt(3000, 3000)
	clock := actionsim.NewClock(sim)
	d := &Deps{Session: sim, Config: &cachecfg.Config{}, Chat: chatlog.New(), Delay: clock.Delay}
	if err := manipClick(d, 1, gamestate.NPC{Index: 41}); err != nil {
		t.Fatal(err)
	}
	before := clock.Delays()
	if err := manipClick(d, 1, gamestate.NPC{Index: 42}); err != nil {
		t.Fatal(err)
	}
	if clock.Delays() != before {
		t.Errorf("the click on another spot waited %d delay(s)", clock.Delays()-before)
	}
}
