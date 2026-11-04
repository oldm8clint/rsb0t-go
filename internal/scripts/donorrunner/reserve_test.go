package donorrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/tile"
)

// A hand-off reserves its place with the mule before the walk starts, and takes it back however
// the run ends -- here a stop before it ever arrived, so it never announced. Without the cancel
// the mule would count a donor that is not coming until enrouteTTL, and publish a queue deeper
// than the real one to every donor deciding whether to draw.
func TestARunReservesItsPlaceAndGivesItBackEvenIfItNeverArrived(t *testing.T) {
	bus := mulebus.NewBus()
	w := mulebus.NewWatcher()
	s := &confirmSession{state: gamestate.New()}
	keep := donorplan.Rules{Keep: []string{"Knife"}}
	noted := []donorplan.ItemRow{{Name: "Knife", ID: 946, Count: 1}, {Name: "Arrow shaft", ID: 52, Count: 900}}
	res := Run(&Deps{Session: s, Bus: bus, Me: "acct171", Delay: func(time.Duration) bool { return false }}, Options{
		Mule: "mule", RowsInBank: 1, Drawn: 1,
		Load:     func() []donorplan.LoadGroup { return donorplan.PlanLoad(noted, donorplan.DefaultMuleFreeSlots, keep) },
		HaulLeft: func() int { return donorplan.HaulOf(noted, keep) },
	})
	if !res.Stopped {
		t.Fatalf("result %+v, want the stop", res)
	}
	msgs := w.Poll(bus)
	if len(msgs) != 2 || msgs[0].Kind != mulebus.KindReserve || msgs[0].To != "mule" ||
		msgs[1].Kind != mulebus.KindCancel || msgs[1].To != "mule" {
		t.Fatalf("bus = %+v, want exactly a reserve then a cancel, both to mule", msgs)
	}
}

func TestMuleQueueReadsTheMulesOwnHeartbeat(t *testing.T) {
	bus := mulebus.NewBus()
	now := time.Now()
	if _, _, ok := MuleQueue(bus, "mule", now); ok {
		t.Fatal("no heartbeat must read as unknown, not as an empty queue")
	}
	bus.Publish(mulebus.Heartbeat("mule", tile.Tile{}, 23, mulebus.StateHome, nil)) // an older mule: no queue carried
	if _, _, ok := MuleQueue(bus, "mule", time.Now()); ok {
		t.Error("a heartbeat without a cycle must read as unknown")
	}
	bus.Publish(mulebus.WithQueue(mulebus.Heartbeat("mule", tile.Tile{}, 23, mulebus.StateHome, nil), 7, 15*time.Second))
	bus.Publish(mulebus.WithQueue(mulebus.Heartbeat("othermule", tile.Tile{}, 23, mulebus.StateHome, nil), 40, time.Minute))
	q, c, ok := MuleQueue(bus, "mule", time.Now())
	if !ok || q != 7 || c != 15*time.Second {
		t.Errorf("MuleQueue = %d, %s, %v; want 7, 15s, true", q, c, ok)
	}
}
