package bot

import (
	"testing"

	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/scripts/essenceplan"
)

// THE LOOT PANEL'S ESSENCE SUPPLY CARD (a user request: "runecrafting loot panel is needed"): every hand-over the
// supply loop sees the mule's trade complete is reported with the essence that crossed, and nothing else is.

// A completed hand-over is reported once, with the essence that left the pack, as many times as the loop hands over.
func TestEveryCompletedHandoverIsReportedWithTheEssenceThatCrossed(t *testing.T) {
	f := newFakeSupply()
	st := guildRun(t, nil) // muleAt 5,000
	var crossed []int
	st.onHandover = func(n int) { crossed = append(crossed, n) }
	for f.handoffs < 2 && len(f.goals) < 200 {
		runSteps(st, f, 1)
	}
	if f.handoffs != 2 || len(crossed) != 2 {
		t.Fatalf("handoffs %d, reported %v", f.handoffs, crossed)
	}
	if crossed[0] != f.sends[0] || crossed[1] != f.sends[1] || crossed[0]+crossed[1] != st.delivered {
		t.Errorf("reported %v against sends %v and %d delivered", crossed, f.sends, st.delivered)
	}
	for _, n := range crossed {
		if n < 5000 {
			t.Errorf("a hand-over of %d: the stack is the whole bank at muleAt", n)
		}
	}
}

// A hand-over the mule did not take, or one that crossed nothing, reports nothing: essence that did not move is not supply.
func TestAHandoverThatDidNotCrossIsNotReported(t *testing.T) {
	f := newFakeSupply()
	f.bank = 5200
	f.handoffFail = 1
	st := guildRun(t, nil)
	var crossed []int
	st.onHandover = func(n int) { crossed = append(crossed, n) }
	runSteps(st, f, 2) // read, hand-over (fails)
	if len(f.sends) != 1 || len(crossed) != 0 {
		t.Fatalf("sends %v reported %v", f.sends, crossed)
	}
	for i := 0; i < 20 && f.handoffs == 0; i++ {
		runSteps(st, f, 1)
	}
	if f.handoffs != 1 || len(crossed) != 1 || crossed[0] != f.sends[1] {
		t.Fatalf("the retry: handoffs %d reported %v sends %v", f.handoffs, crossed, f.sends)
	}
}

// A loop with no listener (a test world, or a build before the wiring) hands over exactly as before.
func TestAHandoverWithNoListenerStillHandsOver(t *testing.T) {
	f := newFakeSupply()
	f.bank = 5200
	st := guildRun(t, nil)
	runSteps(st, f, 2)
	if f.handoffs != 1 || st.handoffs != 1 {
		t.Fatalf("handoffs %d / %d", f.handoffs, st.handoffs)
	}
}

// The report is the Essence supply card's Drop: the essence as the event count, no items, and nothing from a nil Context or
// a Context with no Loot sink.
func TestLootHandoverReportsTheEssenceSupplyDrop(t *testing.T) {
	var drops []loot.Drop
	lootHandover(&Context{Loot: func(d loot.Drop) { drops = append(drops, d) }}, 2021)
	lootHandover(&Context{Loot: func(d loot.Drop) { drops = append(drops, d) }}, 0)
	lootHandover(&Context{}, 2021)
	lootHandover(nil, 2021)
	if len(drops) != 1 {
		t.Fatalf("got %d drops, want 1: %+v", len(drops), drops)
	}
	d := drops[0]
	if d.Events != 2021 || len(d.Items) != 0 || !loot.IsEssenceSupply(d.Source) {
		t.Errorf("drop %+v", d)
	}
}

// supplyStep wires the report to its Context's sink: what the run's callback is told reaches that Context's Loot, whichever
// Context the step is called with. (A stopped Context returns from the step at once, after the wiring.)
func TestSupplyStepReportsThroughItsContextsLootSink(t *testing.T) {
	set := essenceplan.Settings{Gateway: "guild", Mule: "themule", MuleAt: 5000, Pickaxe: essenceplan.BestAvailable, Batch: 270}.Normalised()
	b := &essenceBot{settings: set}
	stop := make(chan struct{})
	close(stop)
	var drops []loot.Drop
	ctx := &Context{stop: stop, Loot: func(d loot.Drop) { drops = append(drops, d) }}
	b.supplyStep(ctx, 5000)
	if b.supply == nil || b.supply.onHandover == nil {
		t.Fatal("the step did not wire the hand-over report")
	}
	b.supply.onHandover(321)
	if len(drops) != 1 || drops[0].Events != 321 || !loot.IsEssenceSupply(drops[0].Source) {
		t.Fatalf("drops %+v", drops)
	}
}
