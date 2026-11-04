package fleetrunner

import (
	"fmt"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/fighterfood"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/webwalk"
)

// WalkEatGap is the least time between two bites on a walk: content's eat delay (2 ticks) with room.
const WalkEatGap = 2 * time.Second

// WalkEatDueAgainstLag is WalkEatDueAgainst for a view whose worst recent lag episode hid lagTicks ticks (gamestate.RecentLag): the line
// is scarabplan.EatLineLag's, which raises it to what the attackers' hits could have done in the episode (lageat-1018). 0 is no lag.
func WalkEatDueAgainstLag(hp, maxHP int, hits []int, foods int, sinceBite time.Duration, lagTicks int) bool {
	return fleetplan.EatDueLag(hp, maxHP, hits, lagTicks) && foods > 0 && sinceBite >= WalkEatGap
}

// LagHits is the hits the lag-aware eat line is built from: the NPCs facing the account, and when the view has lagged recently and
// something is hitting the account that nothing names (the account is in combat, no NPC faces it), one fighterfood.LagUnseenHit. With
// no lag the list is exactly the NPCs facing it, so healthy play keeps the line it always had.
func LagHits(hits []int, inCombat bool, lagTicks int) []int {
	if lagTicks > 0 && inCombat && len(hits) == 0 {
		return []int{fighterfood.LagUnseenHit}
	}
	return hits
}

// AttackerHitList is the worst melee hit of every NPC now facing the account, from content's strength and strength bonus
// (scarabplan.MaxHitOf by the NPC's name): what the eat line is built from. nil when nothing faces it.
func AttackerHitList(d *Deps) []int {
	st := d.Session.State()
	self := st.Self()
	if self < 0 {
		return nil
	}
	var hits []int
	for _, n := range st.NPCs() {
		if !n.TargetsPlayer(self) {
			continue
		}
		name := ""
		if d.Config != nil {
			if info, ok := d.Config.Npc(n.Type); ok {
				name = info.Name
			}
		}
		hits = append(hits, fleetplan.NPCTypeMaxHit(n.Type, name)) // a random event monster by its tier (lageat-1018), else by name
	}
	return hits
}

// WalkEater is the eater a WalkTo carries (see the comment above).
func WalkEater(d *Deps) func(webwalk.AsidePoll) {
	var last time.Time
	return func(webwalk.AsidePoll) { EatIfLow(d, &last, "walk eat") }
}

// EatIfLow eats one bite of carried food when WalkEatDue says so (last is the caller's own last-bite time,
// updated on a bite), and reports whether it ate. The walks' eater and the waits' (live 9 Oct: an account
// broke off a fight at 22/50 hitpoints "to eat", then waited four minutes beside the warrior women without a
// bite and died at 1 with 15 food) both use it.
func EatIfLow(d *Deps, last *time.Time, what string) bool {
	if d == nil || d.Session == nil || d.Config == nil {
		return false
	}
	st := d.Session.State()
	hp, ok1 := st.HP()
	maxHP, ok2 := st.MaxHP()
	if !ok1 || !ok2 {
		return false
	}
	if main, _, _ := st.Modals(); main != gamestate.NoModal {
		return false
	}
	since := WalkEatGap
	if last != nil && !last.IsZero() {
		since = time.Since(*last)
	}
	// The cheap tests first: the pack's food is read only once a bite is due by hitpoints and gap. Every walk
	// poll and wait tick calls this, and reading the food first cost 6% of the fleet's CPU (pprof, 14 Oct 2026). The NPCs
	// facing the account are listed only once the hitpoints are under the highest line they could raise it to (after a lag, the
	// lag cap).
	lag := st.RecentViewLag().Ticks
	if hp <= 0 || maxHP <= 0 || hp > fleetplan.EatLineLag(maxHP, []int{maxHP}, lag) {
		return false
	}
	// LEAVING BEFORE EATING (lagescape-1018). A walk or a wait that finds the account hit by more than its bites can outrun, on a view
	// or a server running late, teleports out when it can pay for one (lagescape.go). It cannot walk from here: the walk is its caller's.
	if LagEscape(d, false, what).Acted() {
		return true
	}
	if since < WalkEatGap {
		return false
	}
	hits := LagHits(AttackerHitList(d), st.InCombat(), lag)
	if !WalkEatDueAgainstLag(hp, maxHP, hits, 1, since, lag) {
		return false
	}
	// EatableFoodNames is every unnoted pack item, one name per unit (coins, runes and a knife included); only
	// the foods count (live 12 Oct: the first build of this "ate" Fire runes, Coins and Knives).
	carried := EatableFoods(d)
	foods := 0
	for _, f := range carried {
		foods += f.Count
	}
	if !WalkEatDueAgainstLag(hp, maxHP, hits, foods, since, lag) {
		return false
	}
	// At or under half the hitpoints any bite is worth it: the biggest heal carried (the emergency rule).
	choice, ok := fleetplan.ChooseFood(carried, hp, maxHP, maxHP)
	if !ok {
		return false
	}
	if last != nil {
		*last = time.Now()
	}
	if !EatAnyFood(d, choice.Food) {
		return false
	}
	facing := ""
	if len(hits) > 0 {
		facing = fmt.Sprintf(", %d facing it", len(hits))
	}
	if lag > 0 && hp > fleetplan.EatLine(maxHP, hits) {
		facing += fmt.Sprintf(", over the plain line %d: the view hid %d ticks lately", fleetplan.EatLine(maxHP, hits), lag)
	}
	d.Logf("%s: a bite of %s at %d/%d hitpoints (%d food left%s)", what, choice.Food, hp, maxHP, foods-1, facing)
	return true
}
