package crafterplan

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/orderplan"
)

// The mule's side of a flax kit, decided by orderplan exactly as MuleReceiver decides it: Flax is
// not in the curated catalogue, so the open-stock path serves it, 1000 at a time as ONE slot of
// notes; a second kit inside the window is refused at handoutPerDonor 1 and served at the mule's 8;
// and an account off the allow list is refused. No MuleReceiver change is needed for flax.
func TestTheMuleServesAFlaxKitAsNotes(t *testing.T) {
	pol := orderplan.DefaultPolicy([]string{"flaxer1"})
	led := orderplan.NewLedger(orderplan.DefaultWindow)
	now := time.UnixMilli(1_000_000)
	stock := func(item string) int {
		if item == Flax {
			return 200_263
		}
		return 0
	}
	facts := func(string) (orderplan.KitFacts, bool) { return orderplan.KitFacts{HasNote: true}, true }
	ask := []orderplan.KitLine{{Item: Flax, Qty: DefaultFlaxKitSize}}

	k := orderplan.DecideKit(now, "flaxer1", ask, pol, led, stock, facts, 27)
	if len(k.Lines) != 1 || k.Lines[0].Qty != DefaultFlaxKitSize || !k.Lines[0].Noted || k.Slots(facts) != 1 {
		t.Fatalf("a flax kit = %+v, want 1000 Flax as one slot of notes", k.Lines)
	}
	big := orderplan.DecideKit(now, "flaxer1", []orderplan.KitLine{{Item: Flax, Qty: 5000}}, pol, led, stock, facts, 27)
	if big.Lines[0].Qty != orderplan.OpenMaxPerOrder {
		t.Errorf("a 5000 ask is clamped to the open per-order cap, got %+v", big.Lines[0])
	}
	if off := orderplan.DecideKit(now, "stranger", ask, pol, led, stock, facts, 27); off.Lines[0].Qty != 0 {
		t.Errorf("off the allow list: %+v", off.Lines[0])
	}

	led.RecordKit(now, "flaxer1", "k1", []orderplan.ItemCount{{Name: Flax, Count: DefaultFlaxKitSize}})
	if again := orderplan.DecideKit(now.Add(time.Minute), "flaxer1", ask, pol, led, stock, facts, 27); again.Lines[0].Qty != 0 {
		t.Errorf("a second kit inside the window at handoutPerDonor 1: %+v", again.Lines[0])
	}
	pol.MaxOrdersPerDonor = 8 // the mule's roster
	if again := orderplan.DecideKit(now.Add(time.Minute), "flaxer1", ask, pol, led, stock, facts, 27); again.Lines[0].Qty != DefaultFlaxKitSize {
		t.Errorf("a second kit at handoutPerDonor 8: %+v", again.Lines[0])
	}

	empty := orderplan.DecideKit(now, "flaxer1", ask, pol, orderplan.NewLedger(orderplan.DefaultWindow), func(string) int { return 0 }, facts, 27)
	if empty.Lines[0].Qty != 0 || ClassifyFlaxKit(0, empty.Lines[0].Reason) != FlaxKitEmpty {
		t.Errorf("a mule with no flax: %+v, classified %s", empty.Lines[0], ClassifyFlaxKit(0, empty.Lines[0].Reason))
	}
}
