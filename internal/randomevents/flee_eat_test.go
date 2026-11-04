package randomevents

import (
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/protocol/clientprot"
)

// THE EATER (plantfisher-1018). The account that died at the Fishing Guild bank had one Kebab, which heals 4 of 24 hitpoints;
// the flee ate "the first edible in pack order" with a bare click, ten times, and none of them was confirmed or credited.

func foodOf(id int32, slot int, name, op string) holdGroundFood {
	return holdGroundFood{id: id, slot: slot, name: name, op: op}
}

func TestEdibleHealRanksKnownFoodByWhatItHeals(t *testing.T) {
	for _, c := range []struct {
		f    holdGroundFood
		heal int
		ok   bool
	}{
		{foodOf(1, 0, "Kebab", "Eat"), 3, true},
		{foodOf(2, 0, "Shark", "Eat"), 20, true},
		{foodOf(3, 0, "Bread", "Eat"), 4, true},
		{foodOf(4, 0, "Jug of wine", "Drink"), 11, true},      // a food whose first op is Drink
		{foodOf(5, 0, "Half a meat pie", "Eat"), 4, true},     // a partial form of a table food
		{foodOf(6, 0, "Chocolate bomb", "Eat"), 1, true},      // unknown to the table: last
		{foodOf(7, 0, "Strange fruit", "Eat"), 0, false},      // run energy, not hitpoints
		{foodOf(8, 0, "Beer", "Drink"), 0, false},             // a drink that is not a food
		{foodOf(9, 0, "Raw shark", "Eat"), 0, false},          // raw
		{foodOf(10, 0, "Poison chalice", "Eat"), 0, false},    // poisoned
		{foodOf(11, 0, "Super attack(4)", "Drink"), 0, false}, // a potion
	} {
		heal, ok := edibleHeal(c.f)
		if ok != c.ok || (ok && heal != c.heal) {
			t.Errorf("edibleHeal(%q, %q) = %d, %v; want %d, %v", c.f.name, c.f.op, heal, ok, c.heal, c.ok)
		}
	}
}

func TestBestFoodTakesTheBiggestHealNotTheFirstInThePack(t *testing.T) {
	pack := []holdGroundFood{
		foodOf(7, 0, "Strange fruit", "Eat"),
		foodOf(1, 1, "Kebab", "Eat"),
		foodOf(8, 2, "Beer", "Drink"),
		foodOf(3, 3, "Bread", "Eat"),
		foodOf(2, 9, "Shark", "Eat"),
		foodOf(4, 10, "Shark", "Eat"),
	}
	best, ok := bestFood(pack)
	if !ok || best.name != "Shark" || best.slot != 9 {
		t.Fatalf("best = %+v, %v; want the first Shark (slot 9)", best, ok)
	}
	if _, ok := bestFood([]holdGroundFood{foodOf(7, 0, "Strange fruit", "Eat"), foodOf(8, 1, "Beer", "Drink")}); ok {
		t.Fatal("a pack of a Strange fruit and a Beer has food")
	}
}

func TestHoldGroundEatsTheShark_NotTheKebabOrTheFruit(t *testing.T) {
	v := holdGroundView{hp: 6, max: 24, hpKnown: true, food: []holdGroundFood{
		foodOf(7, 0, "Strange fruit", "Eat"), foodOf(1, 1, "Kebab", "Eat"), foodOf(2, 2, "Shark", "Eat"),
	}}
	if c := holdGroundChoice(v); c.kind != "eat" || c.food.name != "Shark" {
		t.Fatalf("choice = %+v, want the Shark", c)
	}
	v.food = v.food[:1]
	if c := holdGroundChoice(v); c.kind == "eat" {
		t.Fatalf("a Strange fruit was eaten for hitpoints: %+v", c)
	}
}

func eatScene(t *testing.T, hp int) (*payloadSession, func() int) {
	t.Helper()
	withResolver(t, foodResolver{
		fakeResolver: fakeResolver{objNames: map[int]string{333: "Kebab"}},
		objOps:       map[int][5]string{333: {"Eat"}},
	})
	s := &payloadSession{state: overlapState()}
	setFullInventory(s.state, 3214, [2]int32{333, 3})
	setHP(s.state, hp, 24)
	t.Cleanup(func() { actions.SettleBite(actions.BiteEnv{Session: s}) })
	prev := fleeEatConfirmWait
	fleeEatConfirmWait = 150 * time.Millisecond
	t.Cleanup(func() { fleeEatConfirmWait = prev })
	return s, func() int {
		n := 0
		for _, op := range s.ops() {
			if op == clientprot.OPHELD1 {
				n++
			}
		}
		return n
	}
}

// A bite that shows within the wait is confirmed; one that does not is left in the ledger as in flight, and when it lands late the
// next bite is credited and no second click is sent.
func TestFleeEatGoesThroughTheLedger(t *testing.T) {
	s, bites := eatScene(t, 12)
	s.onSend = func(op byte) {
		if op == clientprot.OPHELD1 {
			setHP(s.state, 17, 24) // the bite lands at once
			setFullInventory(s.state, 3214, [2]int32{333, 2})
		}
	}
	var lines []string
	if !fleeEatBite(s, foodOf(333, 0, "Kebab", "Eat"), func(m string) { lines = append(lines, m) }) || bites() != 1 {
		t.Fatalf("a bite that landed: bites %d, log %v", bites(), lines)
	}

	// The next one is sent and does not show inside the wait: it is in flight, which the flee counts as having eaten.
	s2, bites2 := eatScene(t, 12)
	if !fleeEatBite(s2, foodOf(333, 0, "Kebab", "Eat"), nil) || bites2() != 1 {
		t.Fatalf("a bite in flight: bites %d", bites2())
	}
	if _, inFlight := actions.BiteInFlight(s2.state); !inFlight {
		t.Fatal("the unconfirmed bite is not in the ledger")
	}
	// It lands late: the next ask credits it instead of eating again.
	setHP(s2.state, 17, 24)
	setFullInventory(s2.state, 3214, [2]int32{333, 2})
	if !fleeEatBite(s2, foodOf(333, 0, "Kebab", "Eat"), nil) || bites2() != 1 {
		t.Fatalf("a late landing was eaten again: %d clicks", bites2())
	}
}

// With nothing to eat in the slot the click is not sent, and the flee is told so.
func TestFleeEatWithTheFoodGoneSendsNothing(t *testing.T) {
	s, bites := eatScene(t, 12)
	setFullInventory(s.state, 3214, [2]int32{0, 0})
	if fleeEatBite(s, foodOf(333, 0, "Kebab", "Eat"), nil) || bites() != 0 {
		t.Fatalf("ate food that is not there: %d clicks", bites())
	}
}

// An open dialogue keeps a random event's text up at 60%, and is not worth the account at a quarter of its hitpoints.
func TestHoldGroundEatsPastAnOpenDialogueOnlyInAnEmergency(t *testing.T) {
	v := holdGroundView{hp: 10, max: 30, hpKnown: true, dialogueOpen: true, food: []holdGroundFood{foodOf(1, 1, "Kebab", "Eat")}}
	if c := holdGroundChoice(v); c.kind == "eat" {
		t.Fatalf("ate at 10 of 30 with a dialogue open: %+v", c)
	}
	v.hp = 7 // under a quarter
	if c := holdGroundChoice(v); c.kind != "eat" {
		t.Fatalf("did not eat at 7 of 30 with a dialogue open: %+v", c)
	}
}

// The pack's "food" for the last-resort and cornered rules is the same food: a Strange fruit alone is not fed.
func TestStrandFoodIsRealFoodOnly(t *testing.T) {
	withResolver(t, foodResolver{
		fakeResolver: fakeResolver{objNames: map[int]string{464: "Strange fruit", 333: "Kebab", 365: "Shark"}},
		objOps:       map[int][5]string{464: {"Eat"}, 333: {"Eat"}, 365: {"Eat"}},
	})
	s := &overlapSession{state: overlapState()}
	g := NewGuardian(s, nil)
	defer g.Stop()
	setFullInventory(s.state, 3214, [2]int32{464, 1})
	if _, ok := g.strandFood(); ok {
		t.Fatal("a Strange fruit alone reads as food")
	}
	setFullInventory(s.state, 3214, [2]int32{464, 1}, [2]int32{333, 1}, [2]int32{365, 1})
	if f, ok := g.strandFood(); !ok || f.name != "Shark" || f.slot != 2 {
		t.Fatalf("food = %+v, %v; want the Shark in slot 2", f, ok)
	}
}
