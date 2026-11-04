// Package mazeloot is the pure decision logic behind the maze solver's two reward levers: holding
// the prize percentage while the account walks, and looting the maze's chests on the way. It has no
// client, action or session imports; internal/randomevents/maze_loot.go is the live glue.
//
// # What the reward is
//
// content/scripts/macro events/scripts/general/macro_event_maze.rs2: start_macro_maze sets
// %xplamp = 100; the mapzone script settimer(macro_maze, 5)s the timer that takes 1 off %xplamp
// each time it runs; the finish statue ([oploc1,macro_maze_complete] -> end_macro_maze) clears the
// timer, returns the player and, if %xplamp > 0, rolls ~macro_maze_give_reward three times, each
// roll quantity scaled linearly by %xplamp. A chest ([oploc1,macro_maze_chest]) gives one small
// prize whatever the percentage is, as long as it is above 0.
//
// # What stops the percentage falling (Engine-TS 289, src/engine/)
//
// The macro_maze timer is a NORMAL timer. entity/Player.ts processTimers(), line 945:
//
//	if (World.currentTick >= timer.clock + timer.interval && (timer.type === SOFT || this.canAccess())) {
//	    timer.clock = World.currentTick; ... executeScript(...)
//
// and canAccess() (line 825) is `World.shutdown || (!this.protect && !this.busy())`, busy()
// (line 821) being `this.delayed || this.containsModalInterface()` and containsModalInterface()
// (line 816) `modalState & (MAIN | CHAT)`. So the percentage does not fall on any tick where the
// player is
//
//   - delayed (p_delay / p_arrivedelay: PlayerOps.ts P_DELAY line 376, P_ARRIVEDELAY line 359), or
//   - holding a chat or main dialogue (mesbox, objbox, chatnpc ...: p_pausebutton leaves the player
//     suspended with the interface open), or
//   - mid-script with protected access (executeScript line 2204 keeps `protect` set while a script
//     is suspended; closeModal() line 760 clears it).
//
// A blocked timer is NOT reset and does not accumulate: timer.clock only moves when the timer runs,
// and processTimers runs a due timer once per tick. So a stall of any length costs exactly ONE
// decrement, on the first tick the player can be accessed again, and then the clock is fresh: the
// next decrement is 5 ticks after that. A player who is accessible for at most 4 ticks per stall
// loses nothing but that one decrement.
//
// What ends a stall is anything that closes the dialogue, and every ordinary click does:
// OpLocHandler.ts (line 53) calls player.clearPendingAction() -> closeModal() when the packet is
// read, and MoveClickHandler.ts does the same for every walk except MOVE_OPCLICK ("if
// (!message.opClick) player.clearPendingAction()", line 30 of the handler). processClientsIn()
// (World.ts line 601)
// runs before processPlayers() (line 687), whose order is: resume a suspended script, queues,
// processTimers(NORMAL), engine queue, processInteraction. So the click that closes the dialogue
// and the due timer meet in the same tick: opening a door, a chest or the statue costs the one
// decrement the stall had been holding back, never less, and a leg that is walked with the
// dialogue up costs nothing at all.
//
// # How to walk while the dialogue is up
//
// MOVE_OPCLICK (opcode 67) carries exactly MOVE_GAMECLICK's payload, but the engine does not clear
// the player's modal for it, because the real client always follows it with the op packet that does
// the clearing. Player.updateMovement() (line 674) refuses to move a busy player only when the
// click set moveClickRequest AND a script is queued (queue or engineQueue); in the maze nothing is
// queued after the first tick. A MOVE_OPCLICK sent on its own therefore walks the player with the
// timer blocked. actions.WalkOpClick sends it; internal/randomevents/maze_loot.go checks that the
// player really moves and falls back to ordinary walking if it does not.
//
// # What does not help
//
//   - Re-entering the mapzone. NetworkPlayer.updateMap() (lines 251-262) fires [mapzone,0_45_71] only
//     when the 64x64 mapsquare changes, and Player.triggerMapzone() (line 580) just enqueues it; it
//     settimer()s again but never touches %xplamp. The matching [mapzoneexit,0_45_71] runs
//     ~macro_maze_cleanup, which sets %xplamp to 0: leaving the maze any other way than the statue
//     forfeits the reward.
//   - Starting late. The mapzone script is an ENGINE-queue script and processEngineQueue()
//     (line 660) runs only when canAccess(), so while the Mysterious Old Man's briefing is up the
//     timer does not exist yet; the clock starts on the tick it is dismissed.
//   - Anything that needs access for longer than 4 ticks in a row: the fifth tick fires the timer.
//
// # The cost model the solver plays to
//
// Every "action" the route needs (a door, a chest, the statue) must close the dialogue, so each
// separate action window costs one decrement; actions that fall within 4 ticks of one another share
// it. Walking between windows with a dialogue up is free. A 16-door route that takes 100 s walking
// freely loses about 32%; the same route with every leg stalled loses about one point per door.
package mazeloot

import (
	"sort"
	"strconv"
	"strings"

	"rsb0t-go/internal/randomevents/mazegraph"
)

const (
	// OverlayComponent is mazetimer:com_2 (content/pack/interface.pack, 6572), the text the server
	// sets to "<%xplamp>%" on the mazetimer overlay every time the timer runs and when the overlay
	// opens. The client sees it as IF_SETTEXT, so the live percentage is readable.
	OverlayComponent = 6572
	// FireTicks is the timer interval: settimer(macro_maze, 5).
	FireTicks = 5
	// FreeTicks is how many ticks after a decrement the player can stay accessible before the next
	// one: FireTicks-1.
	FreeTicks = FireTicks - 1
	// MinStallLegTicks is the shortest leg worth opening a dialogue for. A shorter one is walked
	// open: it ends inside the 4 free ticks of the window that began it.
	MinStallLegTicks = 3
	// MaxChestsPerRoom caps the chests one room's plan considers (permutations grow factorially);
	// no room of the maze holds more than three.
	MaxChestsPerRoom = 6
	// RewardSlots is the backpack room the final reward needs. macro_maze_give_reward rolls three
	// times and each roll is one of nine item types (coins, feathers, iron arrows, chaos runes,
	// steel arrows, death runes, certed coal, certed mithril ore, nature runes), all stackable, so
	// up to three of them can be new to the backpack and each takes a slot. What does not fit is not
	// given: InvOps.ts INV_ADD (line 73) drops the overflow on the ground under the player for 200
	// ticks, and end_macro_maze has already run ~macro_return_teleport by then, so it lands at the
	// tile the account was returned to, where nothing picks it up. The reward is worth hundreds of
	// coins.
	RewardSlots = 3
	// PrizeSlots is the room one chest's prize can take: the runes, arrows and bolts stack, but a
	// potion (strength, and attack and defence for members) does not, and the roll is random.
	PrizeSlots = 1
)

// ChestHasRoom reports whether a chest may be opened with free empty backpack slots: the prize it
// gives must fit and the final reward must still have its RewardSlots afterwards. A chest's prize
// is worth about one point of reward, so a backpack that could lose the whole reward to it is the
// worst trade in the maze.
func ChestHasRoom(free int) bool { return free >= RewardSlots+PrizeSlots }

// ParsePct reads the overlay text ("93%") into the percentage.
func ParsePct(text string) (int, bool) {
	text = strings.TrimSpace(text)
	text = strings.TrimSuffix(text, "%")
	n, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || n < 0 || n > 100 {
		return 0, false
	}
	return n, true
}

// LegTicks is how many ticks a walk of `steps` tiles takes, counting the tick the click itself
// spends: two tiles a tick running, one walking.
func LegTicks(steps int, run bool) float64 {
	if steps <= 0 {
		return 0
	}
	if run {
		return float64((steps+1)/2) + 1
	}
	return float64(steps) + 1
}

// FreeLeft is how many more ticks the account can be accessible before the timer next runs, given
// the ticks since it last did (negative once it is overdue).
func FreeLeft(sinceFire float64) float64 { return FreeTicks - sinceFire }

// ShouldStall decides whether a leg of legTicks, starting with freeLeft ticks until the timer is
// due, should be walked with a dialogue up. A short leg is not worth the clicks, and a leg that
// ends before the timer is due costs nothing open.
func ShouldStall(freeLeft, legTicks float64) bool {
	return legTicks >= MinStallLegTicks && legTicks+1 > freeLeft
}

// Visit is one chest to open and the tile to open it from.
type Visit struct {
	Chest, Stand mazegraph.Cell
}

type candidate struct {
	chest  mazegraph.Cell
	stands []mazegraph.Cell
}

// PlanChestVisits picks which of chests to open on the way from `from` to `end` (both tiles of
// the same room: `end` is the next door's approach tile) and in what order, over open floor only.
// A chest is a candidate when it has a stand in the room and is not in skip.
//
// With maxDetour < 0 every candidate is visited. Otherwise the plan is the largest set of chests
// whose walk costs at most maxDetour more steps than going straight to `end` (fewest extra steps
// among sets of the same size): the budget for a solver that cannot hold the percentage and pays
// for the time. The order within a set is the cheapest, choosing for each chest the stand nearest
// the previous stop.
func PlanChestVisits(g *mazegraph.Graph, chests []mazegraph.Cell, from, end mazegraph.Cell, skip map[mazegraph.Cell]bool, maxDetour int) []Visit {
	const bound = 4000
	fromDist := g.Distances(from, bound)
	direct, ok := fromDist[end]
	if !ok {
		return nil
	}
	var cands []candidate
	for _, c := range chests {
		if skip[c] {
			continue
		}
		var stands []mazegraph.Cell
		for _, s := range g.ChestStands(c) {
			if _, in := fromDist[s]; in {
				stands = append(stands, s)
			}
		}
		if len(stands) > 0 {
			cands = append(cands, candidate{chest: c, stands: stands})
		}
	}
	if len(cands) == 0 {
		return nil
	}
	nearest := func(c candidate) int {
		best := 1 << 30
		for _, s := range c.stands {
			if d := fromDist[s]; d < best {
				best = d
			}
		}
		return best
	}
	sort.SliceStable(cands, func(i, j int) bool {
		ni, nj := nearest(cands[i]), nearest(cands[j])
		if ni != nj {
			return ni < nj
		}
		if cands[i].chest.X != cands[j].chest.X {
			return cands[i].chest.X < cands[j].chest.X
		}
		return cands[i].chest.Z < cands[j].chest.Z
	})
	if len(cands) > MaxChestsPerRoom {
		cands = cands[:MaxChestsPerRoom]
	}

	// distances from every stand, lazily.
	memo := map[mazegraph.Cell]map[mazegraph.Cell]int{from: fromDist}
	dist := func(a, b mazegraph.Cell) int {
		m, ok := memo[a]
		if !ok {
			m = g.Distances(a, bound)
			memo[a] = m
		}
		if d, ok := m[b]; ok {
			return d
		}
		return 1 << 20
	}

	// plan returns the cheapest order of the chosen candidates and its total steps to `end`.
	plan := func(set []candidate) ([]Visit, int) {
		bestTotal := 1 << 30
		var best []Visit
		idx := make([]int, len(set))
		for i := range idx {
			idx[i] = i
		}
		var permute func(k int)
		permute = func(k int) {
			if k == len(idx) {
				// The cheapest choice of stands for this order, by dynamic programming over them.
				type step struct {
					cost int
					prev int // index into the previous layer's stands, -1 for `from`
				}
				layers := make([][]step, len(idx))
				for li, i := range idx {
					layers[li] = make([]step, len(set[i].stands))
					for si, s := range set[i].stands {
						if li == 0 {
							layers[li][si] = step{cost: dist(from, s), prev: -1}
							continue
						}
						best := step{cost: 1 << 30, prev: -1}
						for pi, ps := range set[idx[li-1]].stands {
							if c := layers[li-1][pi].cost + dist(ps, s); c < best.cost {
								best = step{cost: c, prev: pi}
							}
						}
						layers[li][si] = best
					}
				}
				last := len(idx) - 1
				total, at := 1<<30, 0
				for si, s := range set[idx[last]].stands {
					if c := layers[last][si].cost + dist(s, end); c < total {
						total, at = c, si
					}
				}
				if total >= bestTotal {
					return
				}
				visits := make([]Visit, len(idx))
				for li := last; li >= 0; li-- {
					visits[li] = Visit{Chest: set[idx[li]].chest, Stand: set[idx[li]].stands[at]}
					at = layers[li][at].prev
				}
				bestTotal, best = total, visits
				return
			}
			for i := k; i < len(idx); i++ {
				idx[k], idx[i] = idx[i], idx[k]
				permute(k + 1)
				idx[k], idx[i] = idx[i], idx[k]
			}
		}
		permute(0)
		return best, bestTotal
	}

	if maxDetour < 0 {
		visits, _ := plan(cands)
		return visits
	}
	for size := len(cands); size >= 1; size-- {
		var bestVisits []Visit
		bestTotal := 1 << 30
		subsets(len(cands), size, func(pick []int) {
			set := make([]candidate, len(pick))
			for i, p := range pick {
				set[i] = cands[p]
			}
			if visits, total := plan(set); total < bestTotal {
				bestTotal, bestVisits = total, visits
			}
		})
		if bestVisits != nil && bestTotal-direct <= maxDetour {
			return bestVisits
		}
	}
	return nil
}

// subsets calls fn with every size-k subset of 0..n-1 in lexicographic order.
func subsets(n, k int, fn func([]int)) {
	pick := make([]int, k)
	var rec func(start, depth int)
	rec = func(start, depth int) {
		if depth == k {
			fn(pick)
			return
		}
		for i := start; i <= n-(k-depth); i++ {
			pick[depth] = i
			rec(i+1, depth+1)
		}
	}
	rec(0, 0)
}
