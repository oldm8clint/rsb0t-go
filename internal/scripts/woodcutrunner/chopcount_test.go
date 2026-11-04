package woodcutrunner

import (
	"testing"

	"rsb0t-go/internal/loot"
)

// Obj ids read out of the content checkout, not recalled: content/pack/obj.pack has
// `54=unstrung_oak_shortbow`, `56=unstrung_oak_longbow`, `58=unstrung_willow_longbow`,
// `60=unstrung_willow_shortbow`, `843=oak_shortbow` (STRUNG) and `1521=oak_logs`.
const (
	objUnstrungOakShortbow    = 54
	objUnstrungOakLongbow     = 56
	objUnstrungWillowLongbow  = 58
	objUnstrungWillowShortbow = 60
	objStrungOakShortbow      = 843
	objOakLogs                = 1521
)

// TestNotAChopRefusesTheUnstrungBowsThatLandedOnLiveTreeCards is the owner's second complaint,
// with the exact ids it was reported with.
//
// Read off the live panel on 2026-09-22, read-only: the Woodcutting card's `chop:oak:1281` source
// was carrying "Oak shortbow x1" and "Oak longbow x4", and `chop:willow:1308" was carrying
// "Willow longbow x2" and "Willow shortbow x2" — objs 54, 56, 58 and 60, every one of them an
// UNSTRUNG bow that ProgressiveWCFletch cut with a knife. A tree gives logs; it cannot give a bow
// however the pack moved while the account was chopping.
//
// The nil *Deps config is the point of the test, not an omission. notAChop returns early once it
// has to resolve a name, so a rule that only worked with a loaded cache would silently stop
// protecting an account whose sidebar tabs or cache never arrived — the state CLAUDE.md's OpLocU
// gotcha (gate 5) describes and that a live run hit 26.9 million times in one session.
func TestNotAChopRefusesTheUnstrungBowsThatLandedOnLiveTreeCards(t *testing.T) {
	skip := notAChop(&Deps{})
	for _, id := range []int{
		objUnstrungOakShortbow, objUnstrungOakLongbow,
		objUnstrungWillowLongbow, objUnstrungWillowShortbow,
	} {
		if !skip(id) {
			t.Errorf("obj %d is an unstrung bow FLETCHING made and the chop window kept it; it would "+
				"be reported as something the tree yielded, which is what the owner saw", id)
		}
	}
	// The shaft, which is the same bug one product earlier (2026-09-21) and must not regress.
	if !skip(loot.ObjArrowShaft) {
		t.Errorf("obj %d is an arrow shaft; the 2026-09-21 fix regressed", loot.ObjArrowShaft)
	}
}

// TestNotAChopKeepsTheLogsAndABoughtBow. The filter has to stay narrow at both ends.
//
// Logs are what the tree actually produced and are the whole content of the card. A STRUNG bow
// (obj 843) shares its display name with the unstrung obj 54, so a name-keyed rule would refuse
// both; asserting that 843 survives is how this test would fail if the check were reverted to
// matching "Oak shortbow".
func TestNotAChopKeepsTheLogsAndABoughtBow(t *testing.T) {
	skip := notAChop(&Deps{})
	if skip(objOakLogs) {
		t.Errorf("obj %d is Oak logs — what the tree produced — and the chop window skipped it", objOakLogs)
	}
	if skip(objStrungOakShortbow) {
		t.Errorf("obj %d is a STRUNG oak shortbow, which shares the name %q with the unstrung obj %d; "+
			"refusing it means this filter is matching on the name, and nothing in this fleet strings "+
			"a bow, so the only bows with that id are bought ones",
			objStrungOakShortbow, "Oak shortbow", objUnstrungOakShortbow)
	}
}
