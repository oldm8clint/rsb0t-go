package bot

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/mastercraftrunner"
)

// The chest round comes ahead of a NEW gem round once the mule holds 1,000 Crystal keys (craftChestStartKeys, a user request) and
// the gem round follows it straight on from the mule's post; then ScarabSwarm again.
func TestMasterCrafterChestRoundComesAheadOfGemsAndChainsOn(t *testing.T) {
	r := newMasterRig(t)
	stock := map[string]int{"Uncut sapphire": 900, "Chisel": 1, mastercraftplan.ChestKeyItem: 1040}
	r.b.decide = func(*Context) mastercraftplan.Decision {
		r.checks++
		return mastercraftplan.DecideWithChest(r.b.set, mastercraftplan.Snapshot{
			Now: time.Now(), MuleHeard: true, MuleSameWorld: true,
			Stock: func(item string) (int, bool) { return stock[item], true }, CraftingLevel: 30,
			Ledger: &r.b.ledger, Denied: r.b.denied,
		}, mastercraftplan.ChestInput{})
	}
	r.b.round = func(_ *Context, dec mastercraftplan.Decision) masterRoundOutcome {
		r.rounds = append(r.rounds, dec.Mode)
		switch dec.Mode {
		case mastercraftplan.ModeGems:
			stock["Uncut sapphire"] = 0
		case mastercraftplan.ModeChest:
			stock[mastercraftplan.ChestKeyItem] = 0
		}
		return masterRoundOutcome{Mode: dec.Mode, Produced: 10, Handed: 10, HandoverTried: true}
	}
	r.dueNow()
	r.loop(t) // the check: the chest is due (1,040 keys)
	r.loop(t) // the safe point: the chest round, then the gem round straight on
	if len(r.rounds) != 2 || r.rounds[0] != mastercraftplan.ModeChest || r.rounds[1] != mastercraftplan.ModeGems {
		t.Fatalf("rounds %v, want chest then gems", r.rounds)
	}
	if r.b.swarm == nil {
		t.Fatal("ScarabSwarm must start again after the chest round")
	}
}

// A chest round's waits: one that made something clears them, a passing failure backs off, one
// with nothing to do pauses.
func TestMasterCrafterChestWaits(t *testing.T) {
	r := newMasterRig(t)
	b := r.b
	b.chestWaits(r.ctx, masterRoundOutcome{Mode: mastercraftplan.ModeChest, Transient: true, Why: "the walk did not arrive"})
	if b.ledger.Fails(mastercraftplan.ModeChest) != 1 {
		t.Fatalf("transient: %+v", b.ledger.Backoffs)
	}
	b.chestWaits(r.ctx, masterRoundOutcome{Mode: mastercraftplan.ModeChest, Produced: 6})
	if b.ledger.Fails(mastercraftplan.ModeChest) != 0 || !b.ledger.Chest.PauseUntil.IsZero() {
		t.Fatalf("produced: %+v %v", b.ledger.Backoffs, b.ledger.Chest.PauseUntil)
	}
	b.chestWaits(r.ctx, masterRoundOutcome{Mode: mastercraftplan.ModeChest, Why: "nothing to do"})
	if b.ledger.Chest.PauseUntil.Before(time.Now().Add(b.set.FruitlessPause - time.Minute)) {
		t.Fatalf("nothing to do must pause: %v", b.ledger.Chest.PauseUntil)
	}
}

// Two ids can share a name (both key halves are "Half of a key"): what crossed by name is shared
// out over the ids drawn under it, never more than was drawn of each.
func TestCreditByNameSharesAName(t *testing.T) {
	wants := []mastercraftrunner.LootWant{
		{ID: 985, Name: "Half of a key", Qty: 2},
		{ID: 987, Name: "Half of a key", Qty: 3},
		{ID: 2363, Name: "Runite bar", Qty: 6},
	}
	got := map[int32]int{985: 2, 987: 3, 2363: 6}
	handed := map[int]int{}
	creditByName(map[string]int{"half of a key": 4, "Runite bar": 6}, wants, got, handed)
	if handed[985] != 2 || handed[987] != 2 || handed[2363] != 6 {
		t.Fatalf("%v", handed)
	}
}
