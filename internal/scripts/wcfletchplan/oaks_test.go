package wcfletchplan

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/woodcutplan"
)

var oakNow = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

func TestOakTablesAreContent(t *testing.T) {
	// trees.dbrow [oak_tree_table] and [normal_tree_table]: rune axe 112,350 and 224,700, bronze 32,100 and 64,200, in rising order.
	for tier, pairs := range oakAndNormalChance {
		prev := [2]int{}
		for _, axe := range woodcutplan.Axes {
			p, ok := pairs[axe]
			if !ok || p[1] <= prev[1] {
				t.Errorf("%s %s %v after %v", tier, axe, p, prev)
			}
			prev = p
		}
	}
	if got := oakAndNormalChance["Oak"][woodcutplan.RuneAxe]; got != [2]int{112, 350} {
		t.Errorf("oak rune pair %v", got)
	}
	if got := oakAndNormalChance["Normal tree"]["Bronze axe"]; got != [2]int{64, 200} {
		t.Errorf("normal bronze pair %v", got)
	}
	if treeRollChance("Oak", 60, woodcutplan.RuneAxe) <= treeRollChance("Willow", 60, woodcutplan.RuneAxe) {
		t.Error("an oak rolls better than a willow")
	}
}

func TestOaksPayAtTheLevelsTheModelSays(t *testing.T) {
	if pays, _, _, _ := OaksPay(14, woodcutplan.RuneAxe, "Magic", DefaultOakRealised, DefaultOakMargin); pays {
		t.Error("an oak needs Woodcutting 15")
	}
	// The rune axe: the oaks pay at every level from 15 on (the crossover is 0).
	if c := OakCrossover(woodcutplan.RuneAxe, "Magic", DefaultOakRealised, DefaultOakMargin); c != 0 {
		t.Errorf("rune axe crossover %d, want the oaks to win everywhere", c)
	}
	for _, l := range []int{25, 40, 60, 75, 99} {
		pays, oak, rung, label := OaksPay(l, woodcutplan.RuneAxe, "Magic", DefaultOakRealised, DefaultOakMargin)
		if !pays || oak < 1.15*rung || label == "" {
			t.Errorf("level %d: pays %v oak %.0f rung %.0f (%s)", l, pays, oak, rung, label)
		}
	}
	if pays, _, _, _ := OaksPay(15, "Bronze axe", "Magic", DefaultOakRealised, DefaultOakMargin); pays {
		t.Error("a bronze axe at 15 is under the margin")
	}
	if pays, _, _, _ := OaksPay(25, "Bronze axe", "Magic", DefaultOakRealised, DefaultOakMargin); !pays {
		t.Error("a bronze axe at 25 pays")
	}
	// A huge margin makes the rung win, and OakCrossover finds the level.
	if c := OakCrossover(woodcutplan.RuneAxe, "Magic", DefaultOakRealised, 3.5); c == 0 || c > 99 {
		t.Errorf("crossover at margin 3.5: %d", c)
	}
	if OakXPPerHour(60, woodcutplan.RuneAxe, 1) <= OakXPPerHour(60, woodcutplan.RuneAxe, 0) {
		t.Error("two-tick rolls must beat three-tick rolls")
	}
	if OakXPPerHour(60, "Dragon axe", 1) != 0 {
		t.Error("an unknown axe prices at zero")
	}
}

func TestDecideOaksStintAndRest(t *testing.T) {
	set := DefaultOakSettings()
	board := NewOakSlots(0)
	in := OakAsk{Level: 50, Axe: woodcutplan.RuneAxe, Target: "Magic", World: 1, Now: oakNow, Account: "a", Board: board}
	if d := DecideOaks(set, in); d.Action != OakChop || !strings.Contains(d.Why, "starting a stint") {
		t.Fatalf("a free world and the oaks paying: %+v", d)
	}
	in.Holding, in.Since = true, oakNow
	in.Now = oakNow.Add(time.Hour)
	if d := DecideOaks(set, in); d.Action != OakChop {
		t.Fatalf("mid-stint: %+v", d)
	}
	in.Now = oakNow.Add(2 * time.Hour)
	if d := DecideOaks(set, in); d.Action != OakStop || board.Held(1, in.Now) != 0 {
		t.Fatalf("end of the stint: %+v held %d", d, board.Held(1, in.Now))
	}
	in.Holding = false
	in.RestUntil = in.Now.Add(set.Rest)
	if d := DecideOaks(set, in); d.Action != OakIdle || !strings.Contains(d.Why, "resting") {
		t.Fatalf("resting: %+v", d)
	}
	in.Now = in.RestUntil
	if d := DecideOaks(set, in); d.Action != OakChop {
		t.Fatalf("rested: %+v", d)
	}
	low := OakAsk{Level: 10, Axe: woodcutplan.RuneAxe, Target: "Magic", World: 1, Now: oakNow, Account: "b", Board: board}
	if d := DecideOaks(set, low); d.Action != OakIdle {
		t.Fatalf("level 10: %+v", d)
	}
	set.MaxLevel = 40
	hi := OakAsk{Level: 60, Axe: woodcutplan.RuneAxe, Target: "Magic", World: 2, Now: oakNow, Account: "c", Board: board}
	if d := DecideOaks(set, hi); d.Action != OakIdle || !strings.Contains(d.Why, "twoTickOakMaxLevel 40") {
		t.Fatalf("over the max level: %+v", d)
	}
	hi.Holding = true
	if d := DecideOaks(set, hi); d.Action != OakStop {
		t.Fatalf("a holder over the max level stops: %+v", d)
	}
}

// THE SLOTS: one account a world, three worlds, three choppers (owner: "theres 3 worlds").
func TestOakSlotsSpreadAccountsOverWorlds(t *testing.T) {
	set := DefaultOakSettings()
	board := NewOakSlots(0)
	ask := func(acct string, world int, now time.Time) OakDecision {
		return DecideOaks(set, OakAsk{Level: 70, Axe: woodcutplan.RuneAxe, Target: "Magic", World: world, Now: now, Account: acct, Board: board})
	}
	chopping := 0
	for i, acct := range []string{"a", "b", "c", "d", "e", "f"} {
		world := i%3 + 1
		if ask(acct, world, oakNow).Action == OakChop {
			chopping++
		}
	}
	if chopping != 3 {
		t.Fatalf("%d choppers over three worlds with one slot each, want 3", chopping)
	}
	for w := 1; w <= 3; w++ {
		if board.Held(w, oakNow) != 1 {
			t.Errorf("world %d holds %d", w, board.Held(w, oakNow))
		}
	}
	d := ask("g", 1, oakNow)
	if d.Action != OakIdle || !strings.Contains(d.Why, "held") {
		t.Fatalf("%+v", d)
	}
	set.Slots = 2
	if ask("g", 1, oakNow).Action != OakChop {
		t.Error("a second slot")
	}
	set.Slots = 1
	later := oakNow.Add(OakSlotStale + time.Minute)
	if ask("h", 2, later).Action != OakChop {
		t.Error("a stale holder's slot is free again")
	}
}

func TestOakSlotBoardReleaseAndClaimAreIdempotent(t *testing.T) {
	b := NewOakSlots(time.Minute)
	if !b.Claim(1, "a", 1, oakNow) || !b.Claim(1, "a", 1, oakNow) {
		t.Fatal("a holder re-claims its own slot")
	}
	if b.Claim(1, "b", 1, oakNow) {
		t.Fatal("the slot is taken")
	}
	b.Release(1, "a")
	if !b.Claim(1, "b", 1, oakNow) {
		t.Fatal("the slot is free after a release")
	}
	b.Release(9, "nobody")
	if got := b.Holders(1, oakNow); len(got) != 1 || got[0] != "b" {
		t.Errorf("%v", got)
	}
}
