package vialrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/protocol/clientprot"
)

const testVial = 229

func fillRig() (*actionsim.Session, *actionsim.Clock, *actionsim.Clicks, *Deps, locindex.Match) {
	sim := actionsim.New(actionsim.Objs{}, []actionsim.Stack{{ID: testVial, Count: 1}})
	sim.StandAt(3000, 3000)
	sim.ShowTabs()
	clock := actionsim.NewClock(sim)
	clicks := sim.RecordClicks()
	d := &Deps{Session: sim, Config: &cachecfg.Config{}, Chat: chatlog.New(), Components: actionsim.TabComponents(), Delay: clock.Delay}
	return sim, clock, clicks, d, locindex.Match{Loc: locindex.Loc{ID: 884, X: 3001, Z: 3000}, Name: "Fountain"}
}

// A caller that uses the vial on the fountain in a loop with nothing to wait on (FillLeg's result wait and tick between uses are
// what normally spaces them) cannot send more than one click per two ticks: the click goes through the shared gate.
func TestUsingTheVialOnTheFountainInATightLoopIsSpacedByTheGate(t *testing.T) {
	_, clock, clicks, d, fountain := fillRig()
	for i := 0; i < 30; i++ {
		if err := useVial(d, testVial, fountain); err != nil {
			t.Fatal(err)
		}
	}
	if n := clicks.Count(clientprot.OPLOCU); n != 30 {
		t.Fatalf("%d uses sent for 30 calls", n)
	}
	if most, _ := clicks.MaxPerTick(clientprot.OPLOCU); most > 1 {
		t.Errorf("%d uses in one tick", most)
	}
	if ticks := int(clock.Now() / (600 * time.Millisecond)); ticks < 29*actions.ClickGapTicks {
		t.Errorf("30 uses took %d ticks; the gate wants at least %d", ticks, 29*actions.ClickGapTicks)
	}
}

// A stop while the use waits for its gap sends nothing and reports it.
func TestAStopWhileTheVialUseWaitsSendsNothing(t *testing.T) {
	_, clock, clicks, d, fountain := fillRig()
	if err := useVial(d, testVial, fountain); err != nil {
		t.Fatal(err)
	}
	clock.Stop()
	if err := useVial(d, testVial, fountain); err != actions.ErrClickStopped {
		t.Errorf("returned %v, want ErrClickStopped", err)
	}
	if n := clicks.Count(clientprot.OPLOCU); n != 1 {
		t.Errorf("%d uses sent, want 1", n)
	}
}
