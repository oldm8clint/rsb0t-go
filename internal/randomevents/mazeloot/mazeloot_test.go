package mazeloot

import (
	"testing"

	"rsb0t-go/internal/randomevents/mazegraph"
)

func TestParsePct(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
		ok   bool
	}{
		{"100%", 100, true}, {"93%", 93, true}, {"0%", 0, true}, {" 7% ", 7, true}, {"7", 7, true},
		{"", 0, false}, {"%", 0, false}, {"abc%", 0, false}, {"101%", 0, false}, {"-3%", 0, false},
	} {
		got, ok := ParsePct(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ParsePct(%q) = %d, %v; want %d, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestLegTicks(t *testing.T) {
	for _, tc := range []struct {
		steps int
		run   bool
		want  float64
	}{
		{0, true, 0}, {-2, false, 0}, {1, true, 2}, {2, true, 2}, {10, true, 6}, {11, true, 7}, {10, false, 11},
	} {
		if got := LegTicks(tc.steps, tc.run); got != tc.want {
			t.Errorf("LegTicks(%d, run=%v) = %v, want %v", tc.steps, tc.run, got, tc.want)
		}
	}
}

func TestFreeLeft(t *testing.T) {
	if FreeLeft(0) != 4 || FreeLeft(4) != 0 || FreeLeft(6) != -2 {
		t.Errorf("FreeLeft: %v %v %v", FreeLeft(0), FreeLeft(4), FreeLeft(6))
	}
}

func TestShouldStall(t *testing.T) {
	for _, tc := range []struct {
		name      string
		free, leg float64
		want      bool
	}{
		{"a long leg just after a decrement", 4, 8, true},
		{"a long leg when the timer is overdue", -1, 8, true},
		{"a short leg is walked open", 4, 2, false},
		{"a leg that ends before the timer is due", 4, 3, false},
		{"a leg the timer would interrupt", 2, 3, true},
		{"the shortest leg worth a dialogue, timer nearly due", 0, 3, true},
		{"no leg at all", 0, 0, false},
	} {
		if got := ShouldStall(tc.free, tc.leg); got != tc.want {
			t.Errorf("%s: ShouldStall(%v, %v) = %v, want %v", tc.name, tc.free, tc.leg, got, tc.want)
		}
	}
}

// roomWithChests finds, on the live layout, a door-bounded room holding the given chests' stands.
func roomOf(g *mazegraph.Graph, chest mazegraph.Cell) mazegraph.Cell {
	stands := g.ChestStands(chest)
	return stands[0]
}

func TestPlanChestVisitsOneChest(t *testing.T) {
	g := mazegraph.BuildLayoutGraph()
	chest := mazegraph.Chests[0]
	from := roomOf(g, chest)
	visits := PlanChestVisits(g, mazegraph.Chests, from, from, nil, -1)
	if len(visits) == 0 {
		t.Fatal("no chest planned in a room with a chest")
	}
	found := false
	for _, v := range visits {
		if v.Chest == chest {
			found = true
		}
		if !contains(g.ChestStands(v.Chest), v.Stand) {
			t.Errorf("stand %v is not a stand of chest %v", v.Stand, v.Chest)
		}
	}
	if !found {
		t.Errorf("the chest the room is built around was not planned: %+v", visits)
	}
}

func contains(cells []mazegraph.Cell, c mazegraph.Cell) bool {
	for _, x := range cells {
		if x == c {
			return true
		}
	}
	return false
}

func TestPlanChestVisitsSkipsLootedAndOutOfRoom(t *testing.T) {
	g := mazegraph.BuildLayoutGraph()
	chest := mazegraph.Chests[0]
	from := roomOf(g, chest)
	skip := map[mazegraph.Cell]bool{}
	for _, c := range mazegraph.Chests {
		skip[c] = true
	}
	if v := PlanChestVisits(g, mazegraph.Chests, from, from, skip, -1); len(v) != 0 {
		t.Errorf("everything skipped but %d visits planned", len(v))
	}
	// A room with no chest: the first spawn room.
	if v := PlanChestVisits(g, mazegraph.Chests, mazegraph.Spawns[0], mazegraph.SelectRoute(mazegraph.Spawns[0])[0].From, nil, -1); len(v) != 0 {
		t.Errorf("the first spawn room holds no chest, planned %+v", v)
	}
}

// Along each spawn's route the three chests in its rooms are all planned, once each, with a stand
// that really opens them, and the total walk is the cheapest order: checked against every order.
func TestPlanChestVisitsAlongEveryRoute(t *testing.T) {
	g := mazegraph.BuildLayoutGraph()
	for _, sp := range mazegraph.Spawns {
		route := mazegraph.SelectRoute(sp)
		cur, planned := sp, map[mazegraph.Cell]bool{}
		for _, d := range route {
			visits := PlanChestVisits(g, mazegraph.Chests, cur, d.From, planned, -1)
			for _, v := range visits {
				if planned[v.Chest] {
					t.Errorf("spawn %v: chest %v planned twice", sp, v.Chest)
				}
				planned[v.Chest] = true
				if !contains(g.ChestStands(v.Chest), v.Stand) {
					t.Errorf("spawn %v: stand %v does not open chest %v", sp, v.Stand, v.Chest)
				}
			}
			if len(visits) > 0 {
				if got, best := walkLength(g, cur, d.From, visits), bruteBest(g, cur, d.From, visits); got != best {
					t.Errorf("spawn %v: plan walks %d steps, the best order walks %d", sp, got, best)
				}
			}
			cur = d.To
		}
		if len(planned) != 3 {
			t.Errorf("spawn %v: %d chests planned along the route, want 3: %v", sp, len(planned), planned)
		}
	}
}

// dists memoises Distances for the checks below: they ask for the same sources over and over.
var distCache = map[mazegraph.Cell]map[mazegraph.Cell]int{}

func dists(g *mazegraph.Graph, from mazegraph.Cell) map[mazegraph.Cell]int {
	if d, ok := distCache[from]; ok {
		return d
	}
	d := g.Distances(from, 4000)
	distCache[from] = d
	return d
}

func walkLength(g *mazegraph.Graph, from, end mazegraph.Cell, visits []Visit) int {
	pos, total := from, 0
	for _, v := range visits {
		total += dists(g, pos)[v.Stand]
		pos = v.Stand
	}
	return total + dists(g, pos)[end]
}

// bruteBest is the shortest walk from `from` through a stand of each planned chest, in any order,
// to `end`: an independent check of PlanChestVisits' ordering.
func bruteBest(g *mazegraph.Graph, from, end mazegraph.Cell, visits []Visit) int {
	best := 1 << 30
	idx := make([]int, len(visits))
	for i := range idx {
		idx[i] = i
	}
	var rec func(k int)
	rec = func(k int) {
		if k == len(idx) {
			// every combination of stands is tried by dynamic programming over the order
			dp := map[mazegraph.Cell]int{from: 0}
			for _, i := range idx {
				next := map[mazegraph.Cell]int{}
				for _, s := range g.ChestStands(visits[i].Chest) {
					for pos, d := range dp {
						dd, ok := dists(g, pos)[s]
						if !ok {
							continue
						}
						if cur, seen := next[s]; !seen || d+dd < cur {
							next[s] = d + dd
						}
					}
				}
				dp = next
			}
			for pos, d := range dp {
				if dd, ok := dists(g, pos)[end]; ok && d+dd < best {
					best = d + dd
				}
			}
			return
		}
		for i := k; i < len(idx); i++ {
			idx[k], idx[i] = idx[i], idx[k]
			rec(k + 1)
			idx[k], idx[i] = idx[i], idx[k]
		}
	}
	rec(0)
	return best
}

func TestPlanChestVisitsDetourBudget(t *testing.T) {
	g := mazegraph.BuildLayoutGraph()
	// Find a room with a chest and a distant end tile: the chest costs a detour.
	for _, sp := range mazegraph.Spawns {
		route := mazegraph.SelectRoute(sp)
		cur := sp
		for _, d := range route {
			all := PlanChestVisits(g, mazegraph.Chests, cur, d.From, nil, -1)
			if len(all) > 0 {
				direct := dists(g, cur)[d.From]
				full := walkLength(g, cur, d.From, all)
				detour := full - direct
				// a budget one step short of the full detour drops at least one chest
				if detour > 0 {
					less := PlanChestVisits(g, mazegraph.Chests, cur, d.From, nil, detour-1)
					if len(less) >= len(all) {
						t.Errorf("spawn %v: budget %d (full detour %d) still plans %d of %d chests", sp, detour-1, detour, len(less), len(all))
					}
					if len(less) > 0 {
						if got := walkLength(g, cur, d.From, less) - direct; got > detour-1 {
							t.Errorf("spawn %v: trimmed plan detours %d, over the budget %d", sp, got, detour-1)
						}
					}
				}
				// a budget of the full detour keeps them all
				if same := PlanChestVisits(g, mazegraph.Chests, cur, d.From, nil, detour); len(same) != len(all) {
					t.Errorf("spawn %v: budget %d plans %d chests, want %d", sp, detour, len(same), len(all))
				}
				// a budget of zero plans only chests that cost no extra step
				for _, v := range PlanChestVisits(g, mazegraph.Chests, cur, d.From, nil, 0) {
					if got := walkLength(g, cur, d.From, []Visit{v}) - direct; got != 0 {
						t.Errorf("spawn %v: a zero budget planned %v at detour %d", sp, v, got)
					}
				}
				return
			}
			cur = d.To
		}
	}
	t.Fatal("no room with a chest found")
}

func TestSubsets(t *testing.T) {
	count := 0
	subsets(5, 3, func(p []int) {
		count++
		for i := 1; i < len(p); i++ {
			if p[i] <= p[i-1] {
				t.Errorf("not increasing: %v", p)
			}
		}
	})
	if count != 10 {
		t.Errorf("C(5,3) = %d, want 10", count)
	}
}

func TestChestHasRoom(t *testing.T) {
	for _, tc := range []struct {
		free int
		want bool
	}{
		{28, true}, {5, true}, {4, true}, {3, false}, {2, false}, {0, false}, {-1, false},
	} {
		if got := ChestHasRoom(tc.free); got != tc.want {
			t.Errorf("ChestHasRoom(%d) = %v, want %v", tc.free, got, tc.want)
		}
	}
	// The rule is the reward's three slots plus one for the prize; if either changes the doc
	// comments above and docs/maze_reward_hold_2026-10-13.md say so.
	if RewardSlots != 3 || PrizeSlots != 1 {
		t.Errorf("RewardSlots=%d PrizeSlots=%d, want 3 and 1 (macro_maze_give_reward rolls three times; a chest gives one item)", RewardSlots, PrizeSlots)
	}
}
