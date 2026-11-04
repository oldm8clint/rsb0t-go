package bot

import (
	"testing"
	"time"

	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/tile"
)

func TestTheDrawGateHoldsBackOnlyALongQueueAndNeverForever(t *testing.T) {
	bus := mulebus.NewBus()
	ctx := &Context{Bus: bus}
	g := &drawGate{}
	if _, _, hold := g.check(ctx, "mule"); hold {
		t.Fatal("no heartbeat: the gate must not hold anything back")
	}
	bus.Publish(mulebus.WithQueue(mulebus.Heartbeat("mule", tile.Tile{}, 23, mulebus.StateHome, nil), 4, 15*time.Second))
	if _, _, hold := g.check(ctx, "mule"); hold {
		t.Fatal("a one-minute queue must not hold the draw")
	}
	bus.Publish(mulebus.WithQueue(mulebus.Heartbeat("mule", tile.Tile{}, 23, mulebus.StateHome, nil), 30, 15*time.Second))
	for i := 0; i < donorplan.DrawGateMaxDeferrals; i++ {
		wait, why, hold := g.check(ctx, "mule")
		if !hold || wait < donorplan.DrawGateMinDefer || why == "" {
			t.Fatalf("deferral %d: hold=%v wait=%s why=%q", i+1, hold, wait, why)
		}
	}
	if _, _, hold := g.check(ctx, "mule"); hold {
		t.Error("after the most deferrals in a row the donor must go anyway")
	}
	g.force = true
	if _, _, hold := g.check(ctx, "mule"); hold {
		t.Error("a manual hand-off request must not be held back")
	}
}
