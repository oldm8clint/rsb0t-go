package wcfletchplan

// FLETCHING WHILE WALKING TO THE BANK (owner queue, 7 Oct 2026: "fletch while walking").
//
// Where the cutting is done: Decide answers ActFletch the moment the account is about to walk to the bank (Banking), so a full
// pack of yew logs is cut where the account stands, at the trees, and only then walked to the Seers bank: 28 logs at one log
// every two ticks is 56 ticks of standing before a walk of 30 to 40 ticks that cuts nothing. Both can overlap.
//
// The engine facts (read from Engine-TS and the 289 content, 7 Oct 2026):
//
//   - A knife on logs (OPHELDU) is [opheldu,_category_22] -> @fletch_log: it opens ~skill_multi2, and the chosen make count
//     is run by a WEAK queue, weakqueue*(fletch_logs, 2) after every log ([label,process_fletch_logs]), with no p_delay and
//     no anim lock. OpHeldUHandler ends in clearPendingAction (the interaction and the modal, not the walk queue), so the
//     account keeps walking while the queue cuts.
//   - A movement click (MoveClickHandler, not an op-click move) calls clearPendingAction too, and Player.closeModal clears the
//     weak queue (closeModal(clearWeakQueue = true)): every walk click of the walker ends the batch. So the walk is made in
//     as few clicks as it can be (webwalk ClickSteps 100: one click for an 80-tile walk), and a new batch is started whenever
//     the log count has stopped falling and no menu is waiting.
//   - The weak queue does not run while a modal is open (canAccess), so a menu left unanswered holds the account's cutting;
//     and a CLOSE_MODAL read in the same tick as a use closes the make menu the use opened (gamestate/modalclose.go), so no
//     use goes out within two ticks of a close.
//   - The "Make 10" button needs no count dialogue, which Make X does: one click instead of three packets, a tick sooner. A
//     batch of 10 is 20 ticks, about what one walk click lasts.
//
// DecideWalkFletch is one poll of the walk. Pure: the runner reads the pack, the menu and the clock.

import (
	"fmt"
	"time"
)

const (
	// WalkFletchMinDistance is how far from the bank (tiles) the account must be for the walk to be worth cutting on: a
	// shorter walk is over before the first log lands.
	WalkFletchMinDistance = 14
	// WalkFletchStartAhead is the least distance (tiles) still ahead of the walk's click target for a batch to be started: a
	// batch begun at the end of a click is cut by the next click, or by the bank booth.
	WalkFletchStartAhead = 6
	// WalkFletchBatchAlive is how recently the log count must have fallen for a batch to count as running: the gap between two
	// logs is two ticks, so this is three.
	WalkFletchBatchAlive = 1800 * time.Millisecond
	// WalkFletchUseWait is how long after a knife-on-logs the menu has to show before another is sent: one tick to open and one
	// to arrive.
	WalkFletchUseWait = 1800 * time.Millisecond
	// WalkFletchAnswerWait is how long after the menu was answered the first log has: the queue's first delay is two ticks.
	WalkFletchAnswerWait = 1800 * time.Millisecond
	// WalkFletchFailLimit is how many walks in a row that cut nothing turn the walking cut off for WalkFletchOffFor.
	WalkFletchFailLimit = 3
	WalkFletchOffFor    = 30 * time.Minute
)

// WalkAct is what one poll does.
type WalkAct int

const (
	WalkNone   WalkAct = iota
	WalkUse            // knife on the logs
	WalkAnswer         // press Make 10 on the open menu
)

// WalkFletchPoll is one poll of a walk as the decision sees it.
type WalkFletchPoll struct {
	// HaveKnife and LogsToCut (the logs held less the carrier the chop manipulation keeps) say there is work.
	HaveKnife bool
	LogsToCut int
	// MenuOffers is a make menu offering the product open now; OtherModal any other interface (a bank, a shop, the rub's
	// teleport menu, a dialogue) or a chat modal that is not that menu.
	MenuOffers, OtherModal bool
	// CloseSettled is gamestate.State.ModalCloseSettled.
	CloseSettled bool
	// SinceFall is how long ago the log count last fell (a large value when it has not); SinceUse and SinceAnswer how long
	// ago the knife-on-logs and the menu answer went.
	SinceFall, SinceUse, SinceAnswer time.Duration
	// Ahead is the tiles from the account to the walk's click target; InCombat a fight under way.
	Ahead    int
	InCombat bool
}

// DecideWalkFletch decides one poll of a walk.
func DecideWalkFletch(p WalkFletchPoll) WalkAct {
	switch {
	case !p.HaveKnife || p.LogsToCut < 1 || p.InCombat:
		return WalkNone
	case p.MenuOffers:
		// The menu is ours: answer it once (the answer closes it, and the next poll sees it closed).
		if p.SinceAnswer < WalkFletchAnswerWait/2 {
			return WalkNone
		}
		return WalkAnswer
	case p.OtherModal, !p.CloseSettled:
		return WalkNone
	case p.SinceFall < WalkFletchBatchAlive:
		return WalkNone // a batch is running
	case p.SinceUse < WalkFletchUseWait, p.SinceAnswer < WalkFletchAnswerWait:
		return WalkNone // a use or an answer is still on its way to the first log
	case p.Ahead < WalkFletchStartAhead:
		return WalkNone
	}
	return WalkUse
}

// WalkFletchWorth reports whether a walk of dist tiles to the bank is worth cutting on: far enough, and the product is one the
// plan cuts (a stacking product never waits for the bank, so its logs are cut at the trees as before).
func WalkFletchWorth(dist int, on bool) bool { return on && dist >= WalkFletchMinDistance }

// WalkFletchGuard counts walks that cut nothing, so a setup that cannot start a batch (a menu that never offers the product)
// stops costing walks their clicks.
type WalkFletchGuard struct {
	fails    int
	offUntil time.Time
}

// Allowed reports whether the walking cut may be tried now.
func (g *WalkFletchGuard) Allowed(now time.Time) bool { return !now.Before(g.offUntil) }

// Note records a walk's outcome: logs cut during it. It reports a non-empty reason when the cut was just switched off.
func (g *WalkFletchGuard) Note(logs int, now time.Time) string {
	if logs > 0 {
		g.fails = 0
		return ""
	}
	g.fails++
	if g.fails < WalkFletchFailLimit {
		return ""
	}
	g.fails = 0
	g.offUntil = now.Add(WalkFletchOffFor)
	return fmt.Sprintf("%d walks in a row cut no log", WalkFletchFailLimit)
}

// WalkFletchLine is the log line of one walk: "wcfletch manip: walk-fletch: 14 logs in 31 walking ticks (45.2 per 100) vs plain
// 0.0 per 100 (the walk alone; the same logs cost 28 ticks standing) = 28 ticks saved". Standing cuts a log every two ticks.
func WalkFletchLine(logs, ticks int, totalLogs, totalTicks int) string {
	per := 0.0
	if ticks > 0 {
		per = 100 * float64(logs) / float64(ticks)
	}
	allPer := 0.0
	if totalTicks > 0 {
		allPer = 100 * float64(totalLogs) / float64(totalTicks)
	}
	return fmt.Sprintf("wcfletch manip: walk-fletch: %d logs in %d walking ticks (%.1f per 100) vs plain 0.0 per 100 (the walk alone; the same logs cost %d ticks standing) = %d ticks saved; this run %d logs in %d ticks (%.1f per 100)",
		logs, ticks, per, 2*logs, 2*logs, totalLogs, totalTicks, allPer)
}
