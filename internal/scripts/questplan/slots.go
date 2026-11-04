package questplan

import "time"

// How long a quest may run in one ladder slot (Stage 5's quest ladder).
//
// Live 29 Sep - 2 Oct (run/logs, 160 MB): the Stage 5 cohort ran every quest under ONE slot
// (stage5QuestMinutes, 20) inside a 45 minute pass (stage5QuestBudgetMinutes), whatever the quest.
// LadderFacts says Waterfall and Priest in Peril take about 40 minutes: 96 Waterfall and 164 Priest
// in Peril runs ended in a timeout, the median at the 20 minute mark, and each timeout parked the
// quest (5, 10, 20, 40 minutes, then 2 hours for ever) while the pass's own budget clipped the
// quests that came second (Hetty timeouts as short as 6 minutes). The quest's own estimate sets
// its slot instead.

// A slot is a rung's estimate times 5/4. The estimate is the lab's and the studies' (it holds no
// travel from wherever the Stage 5 trainer stands), so the quarter is a margin, not a guess at the
// walk.
const (
	questSlotNum = 5
	questSlotDen = 4
)

// QuestSlot is the time one run of quest id may take: the larger of base (the settings'
// stage5QuestMinutes) and the rung's EstMinutes plus a quarter. A quest with no estimate (an id
// LadderFacts does not know, or a training rung) keeps base.
func QuestSlot(id string, base time.Duration) time.Duration {
	est := LadderFacts[id].EstMinutes
	if est <= 0 {
		return base
	}
	slot := time.Duration(est) * time.Minute * questSlotNum / questSlotDen
	if slot < base {
		return base
	}
	return slot
}

// LadderBudget is the pass budget a ladder of ids needs: the configured budget, raised only as
// far as the longest slot in the ladder, so a quest whose slot is longer than the budget can still
// run whole. raised reports whether it had to be raised.
func LadderBudget(ids []string, base, budget time.Duration) (eff time.Duration, raised bool) {
	eff = budget
	for _, id := range ids {
		if s := QuestSlot(id, base); s > eff {
			eff = s
		}
	}
	return eff, eff > budget
}

// LadderSlot is the time a quest with slot gets when left of the pass budget remains. A quest
// that fits runs its whole slot; one that fits at least half of it runs what is left (clipped, so
// a timeout is the pass budget's doing and the quest is not parked for it); one that does not fit
// half is not started (ok false), because a stub of a run only walks to the quest and back.
func LadderSlot(slot, left time.Duration) (run time.Duration, clipped, ok bool) {
	switch {
	case slot <= 0 || left <= 0:
		return 0, false, false
	case left >= slot:
		return slot, false, true
	case left+ladderSlotSlack >= slot:
		// A quest whose slot IS the pass budget (the first quest of a 50 minute pass, a 50 minute
		// slot) finds the budget a few milliseconds short by the time it is chosen: not a cut.
		return left, false, true
	case left*2 >= slot:
		return left, true, true
	}
	return 0, true, false
}

// ladderSlotSlack is how much short of its slot a quest may start without counting as clipped.
const ladderSlotSlack = 30 * time.Second
