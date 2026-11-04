package wcfletchplan

import (
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/woodcutplan"
)

var t0 = time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)

func TestParseWorlds(t *testing.T) {
	cases := []struct {
		in   string
		want []int
	}{
		{"1,2,3", []int{1, 2, 3}},
		{" 2 ; 3 ,2, 0, 256, x, -1, 4", []int{2, 3, 4}},
		{"3 1", []int{3, 1}},
		{"", nil},
		{"none", nil},
	}
	for _, c := range cases {
		if got := ParseWorlds(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseWorlds(%q) = %v, want %v", c.in, got, c.want)
		}
	}
	if got := DescribeWorldsCompact([]int{1, 2, 3}); got != "1,2,3" {
		t.Errorf("compact = %q", got)
	}
	if got := DescribeWorlds([]int{1, 2, 3}); got != "1, 2, 3" {
		t.Errorf("described = %q", got)
	}
	if def := DefaultSettings(); !def.SpreadYewWorlds || !reflect.DeepEqual(ParseWorlds(def.YewWorlds), AdviceWorlds) {
		t.Errorf("defaults: spread %v over %q, want on over the fleet's worlds %v", def.SpreadYewWorlds, def.YewWorlds, AdviceWorlds)
	}
}

// The live cohort, 23 accounts logging in one after another on world 1 (as a relaunch does), is dealt
// out 8 / 8 / 7: the least loaded world each time, the world the account is on winning an exact tie.
func TestAssignDealsTheLiveCohortOutEvenly(t *testing.T) {
	s := NewWorldSpread()
	worlds := []int{1, 2, 3}
	counts := map[int]int{}
	var order []int
	for i := 0; i < 23; i++ {
		a := s.Assign(fmt.Sprintf("acct%d", i), worlds, 1, nil, t0)
		if !a.Fresh {
			t.Fatalf("account %d: a first assignment is Fresh", i)
		}
		counts[a.World]++
		order = append(order, a.World)
	}
	if counts[1] != 8 || counts[2] != 8 || counts[3] != 7 {
		t.Errorf("23 accounts dealt %v (%v), want 8 / 8 / 7", counts, order)
	}
	// The first account stays where it is: it ties every world, and its own wins the tie.
	if order[0] != 1 || order[1] != 2 || order[2] != 3 || order[3] != 1 {
		t.Errorf("the deal should run 1, 2, 3, 1 ...: %v", order[:6])
	}
	for w, want := range map[int]int{1: 8, 2: 8, 3: 7} {
		if got := len(s.Assigned(w)); got != want {
			t.Errorf("world %d holds %d, want %d", w, got, want)
		}
	}
}

// A place stands: the same account asked again gets the same world, however the others have moved.
func TestAssignIsStickyAndRefreshes(t *testing.T) {
	s := NewWorldSpread()
	worlds := []int{1, 2, 3}
	first := s.Assign("sticky", worlds, 1, nil, t0)
	for i := 0; i < 10; i++ {
		s.Assign(fmt.Sprintf("other%d", i), worlds, 1, nil, t0)
	}
	again := s.Assign("sticky", worlds, 3, nil, t0.Add(time.Minute))
	if again.Fresh || again.World != first.World || again.Loads != nil {
		t.Errorf("a standing place moved, was Fresh or priced the loads: first %+v again %+v", first, again)
	}
	// Refreshed each pass, it outlives any number of TTLs as long as it is asked within one.
	now := t0
	for i := 0; i < 8; i++ {
		now = now.Add(WorldSpreadTTL - time.Minute)
		if a := s.Assign("sticky", worlds, 1, nil, now); a.Fresh || a.World != first.World {
			t.Fatalf("pass %d: %+v", i, a)
		}
	}
}

// An account on a world the cohort is already split onto is not hopped: on an empty registry each
// ties every world, and the world it stands on wins.
func TestAssignLeavesACohortSplitByHandAlone(t *testing.T) {
	s := NewWorldSpread()
	worlds := []int{1, 2, 3}
	for i := 0; i < 9; i++ {
		on := i%3 + 1
		if a := s.Assign(fmt.Sprintf("hand%d", i), worlds, on, nil, t0); a.World != on {
			t.Errorf("account %d standing on world %d was sent to %d (loads %v)", i, on, a.World, a.Loads)
		}
	}
}

// The strangers a world's yews hold count as accounts of ours would, so a crowded world is dealt
// fewer, capped so a crowd of forty is not a world closed for ever.
func TestAssignCountsAndCapsStrangers(t *testing.T) {
	strangers := func(w int) int {
		if w == 1 {
			return 2
		}
		return 0
	}
	s := NewWorldSpread()
	worlds := []int{1, 2, 3}
	counts := map[int]int{}
	for i := 0; i < 8; i++ {
		counts[s.Assign(fmt.Sprintf("st%d", i), worlds, 1, strangers, t0).World]++
	}
	// Two strangers count as two accounts: world 1 gets 2 fewer than 2 and 3 do.
	if counts[1] != 2 || counts[2] != 3 || counts[3] != 3 {
		t.Errorf("with 2 strangers on world 1, 8 accounts went %v, want 2 / 3 / 3", counts)
	}
	loads := NewWorldSpread().Loads(worlds, func(w int) int { return 40 }, t0)
	for _, w := range worlds {
		if loads[w] != MaxStrangerLoad {
			t.Errorf("world %d: load %d for forty strangers, want the cap %d", w, loads[w], MaxStrangerLoad)
		}
	}
}

// An assignment for a world the list no longer holds is placed again, and an empty list assigns nothing.
func TestAssignFollowsTheListAndLapses(t *testing.T) {
	s := NewWorldSpread()
	a := s.Assign("walker", []int{1, 2, 3}, 3, nil, t0)
	if a.World != 3 {
		t.Fatalf("an empty registry should keep the account on world 3, got %d", a.World)
	}
	b := s.Assign("walker", []int{1, 2}, 3, nil, t0.Add(time.Minute))
	if !b.Fresh || b.World == 3 {
		t.Errorf("world 3 left the list but the account stayed: %+v", b)
	}
	if e := s.Assign("walker", nil, 2, nil, t0); e.World != 2 || e.Fresh {
		t.Errorf("an empty list must hand the current world back, not place anything: %+v", e)
	}
	// Not seen for longer than the TTL: the place is free again and the load drops.
	s.Assign("quiet", []int{1, 2}, 1, nil, t0)
	if got := len(s.Assigned(1)) + len(s.Assigned(2)); got != 2 {
		t.Fatalf("two places expected, have %d", got)
	}
	late := t0.Add(WorldSpreadTTL + 2*time.Minute)
	if loads := s.Loads([]int{1, 2}, nil, late); loads[1]+loads[2] != 0 {
		t.Errorf("places not refreshed for the TTL still count: %v", loads)
	}
	if _, ok := s.World("quiet", late); ok {
		t.Error("a lapsed place is still reported")
	}
	s.Assign("quiet", []int{1, 2}, 1, nil, late)
	s.Release("quiet")
	if _, ok := s.World("quiet", late); ok {
		t.Error("Release did not free the place")
	}
}

// The account that loses a quarter hour to waiting moves to a world carrying fewer of ours, once, and
// not into a world that is no lighter.
func TestRebalanceMovesTheIdleToALighterWorldOnly(t *testing.T) {
	s := NewWorldSpread()
	worlds := []int{1, 2, 3}
	// World 1 holds 9, world 2 holds 7, world 3 holds 7.
	place := func(prefix string, n, on int) {
		for i := 0; i < n; i++ {
			s.Assign(fmt.Sprintf("%s%d", prefix, i), []int{on}, on, nil, t0)
		}
	}
	place("a", 9, 1)
	place("b", 7, 2)
	place("c", 7, 3)
	idle := WorldRebalanceIdle + 0.01
	if _, ok := s.Rebalance("a0", worlds, WorldRebalanceIdle-0.01, nil, t0); ok {
		t.Error("an account that is not idle moved")
	}
	to, ok := s.Rebalance("a0", worlds, idle, nil, t0)
	if !ok || (to != 2 && to != 3) {
		t.Fatalf("9 / 7 / 7: the idle account should move to a 7, got %d %v", to, ok)
	}
	if w, _ := s.World("a0", t0); w != to {
		t.Errorf("the registry says world %d after a move to %d", w, to)
	}
	// 8 / 8 / 7 now: nobody gains by another move.
	for _, who := range []string{"a1", "b0", "c0", "a0"} {
		if to, ok := s.Rebalance(who, worlds, 1, nil, t0.Add(time.Minute)); ok {
			t.Errorf("%s moved to %d on a 8 / 8 / 7 split", who, to)
		}
	}
	// And a moved account is left alone for an hour even if the cohort goes lopsided again. (The
	// places are refreshed as every Loop pass would, or they lapse after WorldSpreadTTL.)
	place("d", 3, to)
	refresh := func(now time.Time) {
		for _, w := range worlds {
			for _, who := range s.Assigned(w) {
				s.Assign(who, []int{w}, w, nil, now)
			}
		}
	}
	for now := t0; now.Before(t0.Add(WorldRebalanceEvery + 2*time.Minute)); now = now.Add(10 * time.Minute) {
		refresh(now)
	}
	if _, ok := s.Rebalance("a0", worlds, 1, nil, t0.Add(WorldRebalanceEvery-time.Minute)); ok {
		t.Error("moved twice inside the hour")
	}
	if _, ok := s.Rebalance("a0", worlds, 1, nil, t0.Add(WorldRebalanceEvery+time.Minute)); !ok {
		t.Error("never moved again after the hour")
	}
	// One world only, or an unplaced account: nothing to do.
	if _, ok := s.Rebalance("a0", []int{1}, 1, nil, t0); ok {
		t.Error("moved with a single world")
	}
	if _, ok := s.Rebalance("nobody", worlds, 1, nil, t0); ok {
		t.Error("moved an account with no place")
	}
}

// A world the yews of which strangers are working is heavier, and the idle account leaves it for an
// empty one.
func TestRebalanceLeavesAWorldTheStrangersHold(t *testing.T) {
	s := NewWorldSpread()
	worlds := []int{1, 2}
	for i := 0; i < 4; i++ {
		s.Assign(fmt.Sprintf("p%d", i), []int{1}, 1, nil, t0)
		s.Assign(fmt.Sprintf("q%d", i), []int{2}, 2, nil, t0)
	}
	stranger := func(w int) int {
		if w == 1 {
			return 3
		}
		return 0
	}
	to, ok := s.Rebalance("p0", worlds, 0.5, stranger, t0)
	if !ok || to != 2 {
		t.Errorf("4 + 3 strangers against 4: want a move to world 2, got %d %v", to, ok)
	}
}

// THE HEADER'S TABLE, recomputed: the model prices the live cohort (Woodcutting 89, Fletching 81,
// rune axes) better the more worlds it is on, and the arrangement the registry deals is the one priced.
func TestSplitOverWorldsPaysAsTheHeaderSays(t *testing.T) {
	ch := Chopper{Wood: 89, Fletch: 81, Axe: woodcutplan.RuneAxe}
	perAccount := func(n int) float64 {
		b := NewCampBoardFor(YewCamps, ch, BoardPlayers)
		accts := make([]Chopper, n)
		at := make([]int, n)
		for i := range accts {
			accts[i] = ch
			at[i] = b.Seat(fmt.Sprintf("m%d", i), woodcutplan.Seers, false, t0).Camp
		}
		total, _ := CohortXP(YewCamps, accts, at, BoardPlayers)
		return total / float64(n)
	}
	near := func(name string, got, want float64) {
		t.Helper()
		if math.Abs(got-want)/want > 0.03 {
			t.Errorf("%s: %.0f xp/h an account, the header says %.0f", name, got, want)
		}
	}
	one := perAccount(23)
	two := (12*perAccount(12) + 11*perAccount(11)) / 23
	three := (8*perAccount(8) + 8*perAccount(8) + 7*perAccount(7)) / 23
	t.Logf("one world %.0f, two worlds %.0f, three worlds %.0f, alone %.0f", one, two, three, PriceCamp(YewCamps[1], ch, BoardPlayers).Share(1))
	near("23 on one world", one, 23551)
	near("12 / 11 over two worlds", two, 31600)
	near("8 / 8 / 7 over three worlds", three, 35300)
	if !(one < two && two < three) {
		t.Errorf("more worlds must pay more: %.0f %.0f %.0f", one, two, three)
	}
	if alone := PriceCamp(YewCamps[1], ch, BoardPlayers).Share(1); alone < three || alone > 1.25*three {
		t.Errorf("one account alone earns %.0f, which must top (and be within a quarter of) the three-world split %.0f", alone, three)
	}
}
