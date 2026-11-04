package runecraftplan

// The gate nobody could trade with (live 18 Oct 2026): after an event monster the crafter stood on 2864,3020, three tiles from his
// stand, on the far side of the Ruined Pillar at 2865,3020 from the stand and from every tile the carriers wait on. His stand click
// was ONE waypoint, which the engine walks greedily (diagonal, then along x, then along z): it walked him into the pillar and stopped,
// for good. The carriers' requests (an op on a player) need an orthogonally adjacent tile; the
// engine closes the distance with a naive walk to the tile on the asker's side of him, which was the pillar, so no request ever ran:
// no "Sending trade offer...", no ":tradereq:" for him, nothing for either side to read. Fifty-six minutes of a gate that looked open.
//
// This file is the pure part of the two answers: the crafter notices carriers waiting with no request line coming, says why and, when
// no walk is the cure, climbs a ladder to a round that ends (DecideGateDeaf), and a carrier whose requests are not answered stops
// asking from where it stands when that is a dead end (DecideApproach). The tiles and walks are runecraftrunner's (gatewalk.go).
//
// THE LADDER exists because the fleet watchdog cannot be the backstop for a gate that keeps writing: any log line resets its silence
// clock (internal/stallwatch), and a gate with carriers waiting writes its "gate deaf" lines and its "no trade for" line. Before those
// lines a wedged gate went silent and the watchdog's nudge, restart and relogin ran after fifteen minutes; now the gate climbs its own:
// walk him onto the stand (off it), nudge (close what is open, forget the request lines, step off and back), end the round.

import (
	"fmt"
	"strings"
	"time"
)

// ---- the carrier's half: a crafter that does not answer ----

// The patience of a carrier before it moves instead of asking again. A request the game ran is answered within a second or two ("Sending
// trade offer..."), so a carrier that has asked ApproachAsks times (3.6 s apart) and heard nothing is not being answered at all.
const (
	// ApproachAsks and ApproachStill: the engine's own approach from where the carrier stands is a dead end (runecraftrunner computes
	// it from the collision pack), the crafter has stood on one tile for ApproachStill and this many requests were sent without a word.
	ApproachAsks  = 2
	ApproachStill = 3 * time.Second
	// ApproachBlindAsks and ApproachBlindStill: the same without the pack's verdict. Four requests, fourteen seconds and a crafter who
	// has not moved in ten is a gate to look at from another side.
	ApproachBlindAsks  = 4
	ApproachBlindStill = 10 * time.Second
)

// ApproachView is what a carrier knows about a crafter that has not answered its requests.
type ApproachView struct {
	// Operable: the carrier stands where the engine runs a trade op at once (orthogonally beside him, no wall between). Then moving
	// helps nothing: the requests run, whatever stops the answer is on his side.
	Operable bool
	// EngineReaches: the engine's own walk to him from here (the naive approach) ends on an operable tile. Unknown counts as true.
	EngineReaches bool
	// Heard: the game said anything of the requests since the last move: "Sending trade offer...", "is busy at the moment", or his
	// own ":tradereq:" line. CantReach: it said "I can't reach that!".
	Heard, CantReach bool
	// Asks is the requests the game has said nothing of since the carrier last moved (or heard anything); Still how long the crafter
	// has stood on his present tile.
	Asks  int
	Still time.Duration
}

// DecideApproach says whether the carrier should walk to another tile beside the crafter before it asks again, and why.
func DecideApproach(v ApproachView) (move bool, why string) {
	if v.Operable {
		return false, ""
	}
	if v.CantReach {
		return true, `the game said "I can't reach that!" and this tile cannot trade with him`
	}
	if v.Heard {
		return false, ""
	}
	if !v.EngineReaches && v.Asks >= ApproachAsks && v.Still >= ApproachStill {
		return true, fmt.Sprintf("%d request(s) in %s without a word, and the engine's walk to him from here ends against a wall", v.Asks, v.Still.Round(time.Second))
	}
	if v.Asks >= ApproachBlindAsks && v.Still >= ApproachBlindStill {
		return true, fmt.Sprintf("%d request(s) without a word while he stood still for %s, and this tile is not beside him", v.Asks, v.Still.Round(time.Second))
	}
	return false, ""
}

// ---- the crafter's half: carriers waiting and no request lines ----

// The gate's patience with silence.
const (
	// GateDeafAfter is how long carriers may stand at the gate with a load before the quiet is a fault. A healthy gate reads a request
	// line every 3.6 s from each carrier asking (GateRequestPending); he is not in GateStep while he crafts, so the clock starts when
	// he is back.
	GateDeafAfter = 20 * time.Second
	// GateDeafWalks is how many corrective walks onto the stand one quiet gets before it is reported and left to the carriers.
	GateDeafWalks = 2
	// GateDeafNote is the longest between two lines about the same quiet.
	GateDeafNote = 2 * time.Minute
	// GateQuietLine is how often a gate with no trade says how it stands.
	GateQuietLine = 10 * time.Minute
	// GateDeafNudgeAfter is how long carriers may wait at a gate that is deaf with him on the stand (or past the corrective walks)
	// before the first rung: the nudge. GateDeafEndAfter is when the last rung, the end of the round, may follow, and
	// GateDeafNudgeWait the least time the nudge is given to work. A carrier ends its own shift after GateWaitMax (ten minutes) at the
	// gate with a load, so the round ends while they are still there to see it closed.
	GateDeafNudgeAfter = 3 * time.Minute
	GateDeafEndAfter   = 6 * time.Minute
	GateDeafNudgeWait  = 2 * time.Minute
)

// GateDeafAction is what the crafter does about a quiet gate.
type GateDeafAction string

// The actions.
const (
	GateDeafNone GateDeafAction = ""
	// GateDeafWalk walks him onto the stand by the routed walker (the stand click did not).
	GateDeafWalk GateDeafAction = "walk"
	// GateDeafWarn says what is wrong and does nothing.
	GateDeafWarn GateDeafAction = "warn"
	// GateDeafNudge closes what is open, forgets the request lines read and steps him off the stand and back: what a walk cannot
	// fix and a restart used to (a modal, a chat box, a stale interaction, a chat cursor that lost its place).
	GateDeafNudge GateDeafAction = "nudge"
	// GateDeafEnd ends the round as a passing failure: the nudge did not bring a request line either.
	GateDeafEnd GateDeafAction = "end"
)

// GateDeafView is what DecideGateDeaf reads.
type GateDeafView struct {
	// Waiting is the carriers on the bus at the gate with a load, asking; Quiet how long they have been with no request line read.
	Waiting int
	Quiet   time.Duration
	// OnStand: he stands on his stand tile. Walks: corrective walks already made in this quiet. Noted: a line was already written
	// about this quiet, SinceNote ago.
	OnStand   bool
	Walks     int
	Noted     bool
	SinceNote time.Duration
	// Nudged: the nudge was already made in this quiet, SinceNudge ago.
	Nudged     bool
	SinceNudge time.Duration
}

// due says a line about this quiet is due: none has been written, or the last is GateDeafNote old.
func (v GateDeafView) due() bool { return !v.Noted || v.SinceNote >= GateDeafNote }

// GateDeafDecision is DecideGateDeaf's answer.
type GateDeafDecision struct {
	Action GateDeafAction
	Why    string
}

// DecideGateDeaf says what to do when carriers wait and no request line comes. Off the stand the answer is first to walk onto it. Past
// the corrective walks, or on the stand, the fault is not one a walk fixes (a modal, an event, an engine rule, a lost chat cursor), and
// the gate climbs: it says so, then at GateDeafNudgeAfter it nudges, then, once the nudge has had GateDeafNudgeWait and the quiet has
// reached GateDeafEndAfter, the round ends (a gate that sits waiting while the carriers' requests go unanswered is worse than none:
// they go home and the mule's essence waits for a gate that works).
func DecideGateDeaf(v GateDeafView) GateDeafDecision {
	if v.Waiting <= 0 || v.Quiet < GateDeafAfter {
		return GateDeafDecision{}
	}
	quiet := v.Quiet.Round(time.Second)
	if !v.OnStand && v.Walks < GateDeafWalks {
		return GateDeafDecision{GateDeafWalk, fmt.Sprintf("%d carrier(s) waiting with a load and no request line in %s, and he is not on the stand", v.Waiting, quiet)}
	}
	where := "and he is on the stand"
	if !v.OnStand {
		where = fmt.Sprintf("and %d walk(s) onto the stand did not bring one", v.Walks)
	}
	if v.Nudged && v.Quiet >= GateDeafEndAfter && v.SinceNudge >= GateDeafNudgeWait {
		return GateDeafDecision{GateDeafEnd, fmt.Sprintf("%d carrier(s) waiting with a load, no request line in %s, %s, and the nudge %s ago did not bring one: ending the round (the carriers go home, the gate is walked afresh)",
			v.Waiting, quiet, where, v.SinceNudge.Round(time.Second))}
	}
	if !v.Nudged && v.Quiet >= GateDeafNudgeAfter {
		return GateDeafDecision{GateDeafNudge, fmt.Sprintf("%d carrier(s) waiting with a load, no request line in %s, %s: nudging him (close what is open, forget the request lines, a step off the stand and back)",
			v.Waiting, quiet, where)}
	}
	if v.due() {
		return GateDeafDecision{GateDeafWarn, fmt.Sprintf("%d carrier(s) waiting with a load, no request line in %s, %s", v.Waiting, quiet, where)}
	}
	return GateDeafDecision{}
}

// GateQuietView is what GateQuietNote reads: the gate with no trade for a while.
type GateQuietView struct {
	// SinceTrade is how long since essence last arrived (or the round began); Noted says a line was already written, SinceLine ago.
	SinceTrade time.Duration
	Noted      bool
	SinceLine  time.Duration
}

// GateQuietNote says whether the gate writes its "no trade for ..." line now (the first after GateQuietLine of silence, then every
// GateQuietLine): a gate that stood 15 minutes without one log line looked exactly like a healthy one waiting for a carrier.
func GateQuietNote(v GateQuietView) bool {
	return v.SinceTrade >= GateQuietLine && (!v.Noted || v.SinceLine >= GateQuietLine)
}

// StandWord names the stand state for a log line.
func StandWord(onStand bool) string {
	if onStand {
		return "on the stand"
	}
	return "off the stand"
}

// JoinNames is a short list of carrier names for a log line (at most four, then the count).
func JoinNames(names []string) string {
	if len(names) <= 4 {
		return strings.Join(names, ", ")
	}
	return strings.Join(names[:4], ", ") + fmt.Sprintf(" and %d more", len(names)-4)
}
