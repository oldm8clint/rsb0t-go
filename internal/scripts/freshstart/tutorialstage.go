package freshstart

import (
	"fmt"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Where an account is relative to Tutorial Island, and a bound on how long it may take to
// leave. Pure: snapshots in, decisions out, no client and no I/O.
//
// Why this is a decision worth naming rather than an `if InTutorialIsland` at each call site:
// the answer "the tutorial is finished" has TWO observable halves, and a script that checks
// only one of them acts on a blinded account. Content's [label,tutorial_complete]
// (content/scripts/tutorial/scripts/tutorial.rs2) does `p_telejump` to Lumbridge AND
// `~initalltabs` in the same label, so the account leaves the island and gets its sidebar back
// together. Until that lands, `@start_tutorial` has stripped every tab
// (`if_settab(null, ^tab_inventory)` is its first act), so every backpack and worn read comes
// back EMPTY rather than unknown. A caller that resumes on position alone starts making gear,
// food and bank decisions from a pack it cannot see.

// TutorialStage is where an account is on its way off Tutorial Island.
type TutorialStage string

// The stages, in the order a brand-new account meets them.
const (
	// TutorialStageDesign is the appearance screen a brand-new account logs in behind.
	TutorialStageDesign TutorialStage = "the appearance screen"
	// TutorialStageIsland is anywhere inside the tutorial, above ground or below it.
	TutorialStageIsland TutorialStage = "Tutorial Island"
	// TutorialStageUnknown is before the first position update has arrived.
	TutorialStageUnknown TutorialStage = "no position yet"
	// TutorialStageOff is off the island. Not the same as finished — see TutorialProgress.Readable.
	TutorialStageOff TutorialStage = "off the island"
)

// DecideTutorialStage picks the stage from live state alone, so a relog anywhere resumes where
// it was. mainModal is the open main interface, t the account's tile or nil when unknown.
func DecideTutorialStage(mainModal int, t *tile.Tile) TutorialStage {
	if mainModal == DesignModal {
		return TutorialStageDesign
	}
	if t == nil {
		return TutorialStageUnknown
	}
	if InTutorialIsland(*t) {
		return TutorialStageIsland
	}
	return TutorialStageOff
}

// The actions the stages outside the island's own dialogue loop are about to take. The island
// itself reuses NextTutorialAction's vocabulary, so the description a give-up prints and the
// decision SkipTutorial makes cannot drift apart.
const (
	TutorialAccept    TutorialAction = "accept the appearance"
	TutorialWaitPos   TutorialAction = "wait for a position"
	TutorialWaitTabs  TutorialAction = "wait for the sidebar to come back"
	TutorialNoArchive TutorialAction = "wait: no interface archive"
)

// TutorialProgress is one pass's worth of observable state.
//
// Readable is whether the backpack and worn panels resolve through the sidebar
// (actions.Equipment.Readable). It is deliberately part of the completion test rather than a
// separate check by the caller: see this file's own package comment for why position alone is
// not enough.
type TutorialProgress struct {
	Stage TutorialStage
	// Action is what the tutorial step is about to do, from NextTutorialAction. Carried so a
	// give-up can name what it was waiting for instead of only that it waited.
	Action   TutorialAction
	Readable bool
}

// The tutorial's budget.
//
// Both numbers are deliberately far longer than the work: answering the guide is one dialogue
// with one option list, and content's own skip sets %tutorial and teleports in a single label.
// The margin is for a random event landing on the island and for the walk to the guide, not for
// hope. What matters is that both are FINITE — this script has grown five separate zero-progress
// loops, each from a pair of individually reasonable rules, so a new wait is a new risk and gets
// a ceiling rather than a retry.
const (
	// TutorialLimit bounds the whole thing, whatever it is doing.
	TutorialLimit = 8 * time.Minute
	// TutorialStuckLimit bounds one unchanging situation, so the common failure (no guide in
	// reach) is reported in three minutes with a precise reason rather than in eight with a
	// vague one.
	TutorialStuckLimit = 3 * time.Minute
)

// TutorialWatch bounds the tutorial so a run that cannot finish it stops instead of spinning.
// The zero value is ready to use; the first Note starts the clock.
type TutorialWatch struct {
	started  bool
	since    time.Time
	lastKey  string
	lastMove time.Time
}

// Note records one pass and says what to do.
//
// done is true once the account is genuinely through: off the island AND able to see its own
// containers again. giveUp is empty while there is budget left, and otherwise carries a reason
// naming the stage and the action it was waiting on — never a bare timeout.
func (w *TutorialWatch) Note(p TutorialProgress, now time.Time) (done bool, giveUp string) {
	if !w.started {
		w.started, w.since, w.lastMove = true, now, now
	}
	if p.Stage == TutorialStageOff && p.Readable {
		return true, ""
	}
	// The progress key is the whole situation, not a counter: a tutorial that is actually
	// advancing changes stage or changes what it is about to do, and one that is wedged repeats
	// the identical pair forever. This cannot mistake a dialogue alternating between pages for
	// a wedge, and TutorialLimit bounds the case where it alternates without ever finishing.
	key := string(p.Stage) + "/" + string(p.Action)
	if key != w.lastKey {
		w.lastKey, w.lastMove = key, now
	}
	if held := now.Sub(w.since); held >= TutorialLimit {
		return false, fmt.Sprintf("%s after %s, still at %q with %q to do", tutorialGaveUp, held.Round(time.Second), p.Stage, p.Action)
	}
	if stuck := now.Sub(w.lastMove); stuck >= TutorialStuckLimit {
		return false, fmt.Sprintf("%s: %q with %q to do has not changed for %s", tutorialGaveUp, p.Stage, p.Action, stuck.Round(time.Second))
	}
	return false, ""
}

// Elapsed is how long the tutorial has been running, for a log line. Zero before the first Note.
func (w *TutorialWatch) Elapsed(now time.Time) time.Duration {
	if !w.started {
		return 0
	}
	return now.Sub(w.since)
}

const tutorialGaveUp = "could not get off Tutorial Island"

// TutorialSkipUnavailable is the one thing that can make this unfinishable on a given world,
// recorded here because it is invisible from the client and easy to misdiagnose as a broken
// script.
//
// Content gates the whole skip on the world, not on the account:
// content/scripts/tutorial/scripts/guides/runescape_guide.rs2 wraps its
// `~p_choice2("Yes please.", ...)` in `if (map_live = false)`, and `map_live` pushes
// `Environment.node.production` (engine/src/engine/script/handlers/ServerOps.ts). On a world
// with production true the guide still answers Talk-to and still speaks, but never offers the
// option, and no amount of clicking will finish the tutorial.
//
// The cheap, cheat-free tell is in the same login script: content/scripts/login_logout/login.rs2
// runs `last_login_info` if and only if `map_live = true`, and that is server packet
// LAST_LOGIN_INFO (opcode 253). So an account that receives opcode 253 at login is on a world
// where this skip does not exist.
const TutorialSkipUnavailable = "the RuneScape Guide never offered the skip option. Content only offers it when the world's map_live is false (runescape_guide.rs2), so this world may have production set, in which case the tutorial cannot be skipped by talking at all — a login that delivers LAST_LOGIN_INFO (opcode 253) confirms it"
