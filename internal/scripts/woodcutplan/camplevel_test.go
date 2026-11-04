package woodcutplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// campSnapshot is an account that would be sent to the Draynor camp on level alone: the camp is
// on, it holds an axe, and it is standing on the Seers landmass.
func campSnapshot(level, campLevel int, campTarget string) WoodSnapshot {
	at := Seers
	return WoodSnapshot{
		Level:      level,
		CampLevel:  campLevel,
		Target:     "Yew",
		CampTarget: campTarget,
		AxesHeld:   []string{"Rune axe"},
		StartCamp:  true,
		Tile:       &at,
	}
}

// The gate the fleet owner's own config line states — "Draynor start camp on until Woodcutting
// 25" — is judged on the account's real Woodcutting level, not on whatever level a caller has
// substituted for its own tier machinery.
//
// This is the acct150/acct163 bug. ProgressiveWCFletch replaces Level with
// EffectiveWoodLevel, so an account held at rung 0 by Fletching 1 read as Woodcutting 1 here
// however good its axe was. Nineteen accounts of Woodcutting 28 to 61 sailed four legs to a
// camp they were decades past, twelve of them deadlocked there, and the 60-coin fares they
// asked the mule for on the way starved it of pickup capacity.
func TestTheCampGateJudgesTheRealWoodcuttingLevel(t *testing.T) {
	// acct163: Woodcutting 48, presenting as 1 because its Fletching is 1.
	s := campSnapshot(1, 48, "Yew")
	if wantsStartCamp(s) {
		t.Error("a Woodcutting 48 account was sent to the start camp because its rung reads as level 1")
	}
	if _, ok := NeedsCampFare(s); ok {
		t.Error("a Woodcutting 48 account asked the mule for a camp fare it has no use for")
	}
	if KeepFareAtBank(s) {
		t.Error("a Woodcutting 48 account held a camp fare back at the bank")
	}

	// A genuinely fresh account is still sent there, on the same two fields.
	fresh := campSnapshot(1, 3, "Yew")
	if !wantsStartCamp(fresh) {
		t.Error("a Woodcutting 3 account was refused the start camp")
	}

	// And a caller that does NOT set CampLevel is unchanged: ProgressiveWoodcutter sets only
	// Level, and every one of these gates must still read it.
	plain := campSnapshot(3, 0, "")
	if !wantsStartCamp(plain) {
		t.Error("with no CampLevel the gate stopped reading Level — ProgressiveWoodcutter's own path")
	}
	plain = campSnapshot(48, 0, "")
	if wantsStartCamp(plain) {
		t.Error("with no CampLevel a Woodcutting 48 account was sent to the camp")
	}
}

// THE EXACT LIVE SHAPE, which the cases above do not reach: ProgressiveWCFletch substitutes BOTH
// halves of the pair, so an account held at rung 0 arrives here reading "Woodcutting 1, target
// Normal tree" when it is really Woodcutting 48 with a target of Yew.
//
// This case exists because a build that fixed only the level shipped and did nothing. AtOakGate
// answers false for any target below oak WHATEVER the level -- "a target that never reaches oak
// never leaves" -- so the nineteen accounts walked straight back to the camp and were measured
// there within four minutes of the roster filling. A fix to one half alone cannot pass this.
func TestTheCampGateJudgesTheRealTargetToo(t *testing.T) {
	s := campSnapshot(1, 48, "Yew")
	s.Target = "Normal tree" // the rung's label, which is what the script really passes
	if wantsStartCamp(s) {
		t.Error("a Woodcutting 48 account targeting Yew was sent to the camp because its RUNG reads as Normal tree")
	}
	if _, ok := NeedsCampFare(s); ok {
		t.Error("...and it asked the mule for the 60-coin fare to get there")
	}

	// Someone genuinely chopping normal trees on purpose still never leaves, which is the
	// behaviour AtOakGate's target test exists for: with no CampTarget the real Target decides.
	deliberate := campSnapshot(48, 0, "")
	deliberate.Target = "Normal tree"
	if !wantsStartCamp(deliberate) {
		t.Error("an account whose own target is Normal tree was refused the camp")
	}
}

// The camp gate must not accidentally depend on where the account is standing for the level
// half of the question — Seers or not, a level-48 account never belongs there.
func TestTheCampGateHoldsAwayFromSeersToo(t *testing.T) {
	away := tile.Tile{X: 3222, Z: 3218}
	s := campSnapshot(1, 48, "Yew")
	s.Tile = &away
	if wantsStartCamp(s) {
		t.Error("a Woodcutting 48 account off the Seers landmass was still sent to the camp")
	}
}
