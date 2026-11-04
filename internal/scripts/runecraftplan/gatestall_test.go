package runecraftplan

import (
	"strings"
	"testing"
	"time"
)

func TestDecideApproachLeavesATileThatCanTradeAlone(t *testing.T) {
	// On an operable tile the requests run: nothing a move fixes, whatever else is wrong.
	for _, v := range []ApproachView{
		{Operable: true, EngineReaches: false, Asks: 9, Still: time.Minute},
		{Operable: true, CantReach: true, Asks: 9, Still: time.Minute},
	} {
		if move, why := DecideApproach(v); move {
			t.Errorf("%+v moved: %s", v, why)
		}
	}
}

func TestDecideApproachMovesWhenTheEngineWalkIsADeadEnd(t *testing.T) {
	// The incident: east of the pillar, the engine's walk to him parks the asker against it, and nothing is ever heard.
	v := ApproachView{EngineReaches: false, Asks: ApproachAsks, Still: ApproachStill}
	move, why := DecideApproach(v)
	if !move || !strings.Contains(why, "without a word") || !strings.Contains(why, "wall") {
		t.Fatalf("a dead end after %d asks: move %v, why %q", v.Asks, move, why)
	}
	// Too early: one ask, or a crafter who has only just arrived on his tile.
	for _, early := range []ApproachView{
		{EngineReaches: false, Asks: ApproachAsks - 1, Still: time.Minute},
		{EngineReaches: false, Asks: 9, Still: ApproachStill - time.Second},
	} {
		if move, _ := DecideApproach(early); move {
			t.Errorf("%+v moved too early", early)
		}
	}
	// Heard something (executed, busy, his reverse line): the game is answering, however slowly.
	if move, _ := DecideApproach(ApproachView{EngineReaches: false, Heard: true, Asks: 9, Still: time.Minute}); move {
		t.Error("moved although the game answered the requests")
	}
}

func TestDecideApproachBlindRuleAndTheGamesOwnWord(t *testing.T) {
	// The engine's approach would reach him, and nothing came back for four asks while he stood still for ten seconds: still moves.
	if move, _ := DecideApproach(ApproachView{EngineReaches: true, Asks: ApproachBlindAsks, Still: ApproachBlindStill}); !move {
		t.Error("four silent asks and ten seconds of a still crafter did not move the carrier")
	}
	if move, _ := DecideApproach(ApproachView{EngineReaches: true, Asks: ApproachBlindAsks - 1, Still: time.Minute}); move {
		t.Error("moved after three asks on the blind rule")
	}
	if move, _ := DecideApproach(ApproachView{EngineReaches: true, Asks: 4, Still: ApproachBlindStill - time.Second}); move {
		t.Error("moved before ten seconds on the blind rule")
	}
	// "I can't reach that!" moves at once, off a tile that cannot trade.
	move, why := DecideApproach(ApproachView{EngineReaches: true, CantReach: true, Asks: 1})
	if !move || !strings.Contains(why, "can't reach") {
		t.Fatalf("the game's own word did not move the carrier: %v %q", move, why)
	}
}

func TestDecideGateDeafWalksOffTheStandThenWarns(t *testing.T) {
	v := GateDeafView{Waiting: 3, Quiet: GateDeafAfter}
	d := DecideGateDeaf(v)
	if d.Action != GateDeafWalk || !strings.Contains(d.Why, "not on the stand") || !strings.Contains(d.Why, "3 carrier(s)") {
		t.Fatalf("off the stand: %+v", d)
	}
	// After the corrective walks the gate says so, once, and then at most every GateDeafNote.
	v.Walks = GateDeafWalks
	if d := DecideGateDeaf(v); d.Action != GateDeafWarn || !strings.Contains(d.Why, "walk(s) onto the stand") {
		t.Fatalf("walks used up: %+v", d)
	}
	v.Noted, v.SinceNote = true, GateDeafNote-time.Second
	if d := DecideGateDeaf(v); d.Action != GateDeafNone {
		t.Fatalf("warned twice inside %s: %+v", GateDeafNote, d)
	}
	v.SinceNote = GateDeafNote
	if d := DecideGateDeaf(v); d.Action != GateDeafWarn {
		t.Fatalf("no second warning after %s: %+v", GateDeafNote, d)
	}
}

func TestDecideGateDeafOnTheStandOnlyWarns(t *testing.T) {
	d := DecideGateDeaf(GateDeafView{Waiting: 1, Quiet: 45 * time.Second, OnStand: true})
	if d.Action != GateDeafWarn || !strings.Contains(d.Why, "on the stand") {
		t.Fatalf("on the stand: %+v", d)
	}
	if d := DecideGateDeaf(GateDeafView{Waiting: 1, Quiet: 45 * time.Second, OnStand: true, Noted: true, SinceNote: time.Minute}); d.Action != GateDeafNone {
		t.Fatalf("warned again after a minute: %+v", d)
	}
}

func TestDecideGateDeafNeedsCarriersAndTimeAndThenSaysNothing(t *testing.T) {
	for _, v := range []GateDeafView{
		{Waiting: 0, Quiet: time.Hour},                                  // nobody is waiting: a quiet gate is a healthy one
		{Waiting: 4, Quiet: GateDeafAfter - time.Second},                // not long enough
		{Waiting: 4, Quiet: 0, OnStand: true},                           // just heard a line
		{Waiting: 4, Quiet: GateDeafAfter - time.Millisecond, Walks: 5}, // still early after any number of walks
	} {
		if d := DecideGateDeaf(v); d.Action != GateDeafNone {
			t.Errorf("%+v acted: %+v", v, d)
		}
	}
}

// THE LADDER. On the stand (or past the corrective walks) the gate nudges once at GateDeafNudgeAfter and ends the round once the quiet
// has reached GateDeafEndAfter and the nudge has had GateDeafNudgeWait; it never ends a round it has not nudged.
func TestDecideGateDeafClimbsFromAWarningToANudgeToTheEndOfTheRound(t *testing.T) {
	v := GateDeafView{Waiting: 2, OnStand: true}
	v.Quiet = GateDeafNudgeAfter - time.Second
	if d := DecideGateDeaf(v); d.Action != GateDeafWarn {
		t.Fatalf("just before the nudge: %+v", d)
	}
	v.Quiet = GateDeafNudgeAfter
	d := DecideGateDeaf(v)
	if d.Action != GateDeafNudge || !strings.Contains(d.Why, "nudging him") || !strings.Contains(d.Why, "he is on the stand") {
		t.Fatalf("at the nudge: %+v", d)
	}
	// Once nudged, the nudge is not repeated and the end waits for both the quiet and the nudge's time.
	v.Nudged, v.SinceNudge = true, time.Second
	v.Quiet = GateDeafEndAfter - time.Second
	if d := DecideGateDeaf(v); d.Action == GateDeafNudge || d.Action == GateDeafEnd {
		t.Fatalf("nudged again or ended early: %+v", d)
	}
	v.Quiet = GateDeafEndAfter
	if d := DecideGateDeaf(v); d.Action == GateDeafEnd {
		t.Fatalf("ended with the nudge a second old: %+v", d)
	}
	v.SinceNudge = GateDeafNudgeWait
	d = DecideGateDeaf(v)
	if d.Action != GateDeafEnd || !strings.Contains(d.Why, "ending the round") || !strings.Contains(d.Why, "the nudge 2m0s ago") {
		t.Fatalf("at the end: %+v", d)
	}
	// A quiet that jumped past the end (the watch did not run for a while) is nudged first and ended only after the nudge had its time.
	v = GateDeafView{Waiting: 1, OnStand: true, Quiet: time.Hour}
	if d := DecideGateDeaf(v); d.Action != GateDeafNudge {
		t.Fatalf("an hour of quiet with no nudge made: %+v", d)
	}
}

func TestDecideGateDeafWalksFirstOffTheStandAndClimbsAfterTheWalks(t *testing.T) {
	// Off the stand with walks left the cure is the walk, however long the quiet.
	v := GateDeafView{Waiting: 3, Quiet: time.Hour, Walks: GateDeafWalks - 1}
	if d := DecideGateDeaf(v); d.Action != GateDeafWalk {
		t.Fatalf("walks left: %+v", d)
	}
	// With the walks spent it is the same ladder, and says what the walks did.
	v.Walks = GateDeafWalks
	v.Quiet = GateDeafNudgeAfter
	d := DecideGateDeaf(v)
	if d.Action != GateDeafNudge || !strings.Contains(d.Why, "2 walk(s) onto the stand did not bring one") {
		t.Fatalf("off the stand, walks spent: %+v", d)
	}
	v.Nudged, v.SinceNudge, v.Quiet = true, GateDeafNudgeWait, GateDeafEndAfter
	if d := DecideGateDeaf(v); d.Action != GateDeafEnd {
		t.Fatalf("off the stand, nudged: %+v", d)
	}
}

// Nobody waiting, or a quiet just begun, climbs nothing: a healthy gate with no carrier at it can stand for ever.
func TestDecideGateDeafLadderNeedsWaitingCarriers(t *testing.T) {
	for _, v := range []GateDeafView{
		{Waiting: 0, Quiet: time.Hour, OnStand: true, Nudged: true, SinceNudge: time.Hour},
		{Waiting: 5, Quiet: GateDeafAfter - time.Second, OnStand: true, Nudged: true, SinceNudge: time.Hour},
	} {
		if d := DecideGateDeaf(v); d.Action != GateDeafNone {
			t.Errorf("%+v acted: %+v", v, d)
		}
	}
	// The ladder fits inside a carrier's patience: the round has ended before the carriers' ten minutes at the gate are up.
	if GateDeafEndAfter+GateDeafAfter >= GateWaitMax {
		t.Errorf("the end rung (%s) comes too late for carriers that give up at %s", GateDeafEndAfter, GateWaitMax)
	}
	if !(GateDeafAfter < GateDeafNudgeAfter && GateDeafNudgeAfter+GateDeafNudgeWait <= GateDeafEndAfter) {
		t.Errorf("the rungs are out of order: %s %s %s %s", GateDeafAfter, GateDeafNudgeAfter, GateDeafNudgeWait, GateDeafEndAfter)
	}
}

func TestGateQuietNoteIsTheFirstAfterTheLimitThenEveryLimit(t *testing.T) {
	if GateQuietNote(GateQuietView{SinceTrade: GateQuietLine - time.Second}) {
		t.Error("noted before the limit")
	}
	if !GateQuietNote(GateQuietView{SinceTrade: GateQuietLine}) {
		t.Error("no first note at the limit")
	}
	if GateQuietNote(GateQuietView{SinceTrade: 3 * GateQuietLine, Noted: true, SinceLine: time.Minute}) {
		t.Error("noted again a minute after a note")
	}
	if !GateQuietNote(GateQuietView{SinceTrade: 3 * GateQuietLine, Noted: true, SinceLine: GateQuietLine}) {
		t.Error("no second note a limit later")
	}
}

func TestStandWordAndJoinNames(t *testing.T) {
	if StandWord(true) != "on the stand" || StandWord(false) != "off the stand" {
		t.Error("stand words")
	}
	if got := JoinNames([]string{"A", "B"}); got != "A, B" {
		t.Errorf("two names: %q", got)
	}
	if got := JoinNames([]string{"A", "B", "C", "D", "E", "F"}); got != "A, B, C, D and 2 more" {
		t.Errorf("six names: %q", got)
	}
}
