package wcfletchrunner

import (
	"testing"

	"rsb0t-go/internal/loot"
)

// The obj ids these tests use, read out of the content checkout rather than recalled:
// content/pack/obj.pack lines 55/57 (`54=unstrung_oak_shortbow`, `56=unstrung_oak_longbow`) and
// 845/846 (`843=oak_shortbow`, `845=oak_longbow`), and `1521=oak_logs`.
const (
	objUnstrungOakShortbow = 54
	objUnstrungOakLongbow  = 56
	objStrungOakShortbow   = 843
	objOakLogs             = 1521
	objCoins               = 995
)

// TestNotAFletchKeepsTheBowsItJustCut is the whole point of the window: a skip filter that threw
// the product away would leave the Fletching card as empty as it was before this file existed.
//
// It runs with a NIL cache config on purpose. That is the state of an account whose cache never
// loaded, and it is the state every id check in notAFletch has to survive — the nil-Config return
// sits above the name checks, so anything that has to hold for such an account must be answered
// by id before it.
func TestNotAFletchKeepsTheBowsItJustCut(t *testing.T) {
	skip := notAFletch(&Deps{})
	for _, id := range []int{objUnstrungOakShortbow, objUnstrungOakLongbow, loot.ObjArrowShaft} {
		if skip(id) {
			t.Errorf("obj %d is what this batch MADE and the window skipped it; the Fletching card "+
				"would report an event with no items", id)
		}
	}
}

// TestNotAFletchRefusesWhatAKnifeCannotMake covers the ids that must be answered without a cache.
//
// Coins and an event's own reward are the live failure this rule exists for: a random event that
// fires mid-batch drops straight into the pack the window is measuring, and on 2026-09-21 that
// put a Beer and an Axe handle on live woodcutting cards. A casket is refused for the same reason
// a tree refuses one — big-net fishing genuinely yields caskets, cutting a log never does.
func TestNotAFletchRefusesWhatAKnifeCannotMake(t *testing.T) {
	skip := notAFletch(&Deps{})
	for name, id := range map[string]int{"coins": objCoins, "a casket": loot.ObjCasket} {
		if !skip(id) {
			t.Errorf("%s (obj %d) arrived while a batch was running and was reported as fletched", name, id)
		}
	}
}

// TestNotAFletchDoesNotSkipAStrungBow. The strung bow shares its display name with the unstrung
// one, so a name-keyed filter would also catch the bow a combat account bought at Lowe's. Nothing
// in this fleet strings a bow, so obj 843 can only ever be a bought one — but it must reach the
// filter as "not mine to judge" rather than being silently swallowed, which is what proves the
// filter is not matching on the word "shortbow".
//
// With a nil config this is the id path only, which is exactly the path that would break first if
// someone replaced the id checks with a name rule.
func TestNotAFletchDoesNotSkipAStrungBow(t *testing.T) {
	if notAFletch(&Deps{})(objStrungOakShortbow) {
		t.Errorf("obj %d is a STRUNG oak shortbow, which shares the name %q with the unstrung obj %d; "+
			"skipping it means this filter is matching on the name",
			objStrungOakShortbow, "Oak shortbow", objUnstrungOakShortbow)
	}
}

// TestFletchWindowIsKeyedOnTheMaterialAndLandsOnTheFletchingCard.
//
// The owner's report was that bows "arent showing in the fletching panel": before this change
// wcfletchrunner opened no window at all, so there was nothing to key. Asserting the source here
// rather than only in internal/loot is what ties this runner to that card — a future edit that
// passed the PRODUCT id instead of the log id would still compile and would split the card into a
// row per bow with no material, and loot.Source.Consumes would then price the card's cost against
// a bow instead of the log it consumed.
func TestFletchWindowIsKeyedOnTheMaterialAndLandsOnTheFletchingCard(t *testing.T) {
	src := loot.Fletching(nil, objOakLogs, "Oak logs")
	if src.ID != objOakLogs {
		t.Fatalf("the fletch source should be keyed on the LOGS consumed (obj %d), got obj %d", objOakLogs, src.ID)
	}
	if src.Skill != loot.SkillFletching || src.Action != loot.ActionFletch {
		t.Fatalf("wanted skill %q action %q, got %q/%q", loot.SkillFletching, loot.ActionFletch, src.Skill, src.Action)
	}
	if got, want := src.GroupKey(), loot.Fletching(nil, 1511, "Logs").GroupKey(); got != want {
		t.Fatalf("every rung of the ladder belongs on ONE Fletching card: oak logs grouped as %q, plain logs as %q", got, want)
	}
	if src.GroupKey() == loot.Woodcutting(nil, 1281, "Oak").GroupKey() {
		t.Fatalf("the fletch source grouped onto the Woodcutting card (%q) — that is the bug", src.GroupKey())
	}
}
