package bot

import (
	"slices"
	"testing"
)

// unprovenFillerTail is the owner ladder for an account that allows unproven quests, from Cook's Assistant to the finale: the
// quest-point fillers in the order they run. Every filler lane (W13 of docs/quest_plan_2026-10-15.md) registered its quests after
// Romeo & Juliet and ahead of Dragon Slayer, so the merged tail is those lanes' quests in the order they were merged; Black Knights'
// Fortress wants 12 quest points before Sir Amik will speak to the account, so it is last of the fillers, once the others have paid
// theirs. A new filler is added here and in questplan.OwnerLadder, and this is the one place the bot tests spell the order.
var unprovenFillerTail = []string{"cook", "sheep", "gobdip", "romeo", "haunted", "hunt", "prince", "doric", "fluffs", "drunkmonk", "cog", "demon", "druid", "junglepotion", "tbwt", "elena", "biohazard", "totem", "fishingcompo", "itexam", "itgronigen", "seaslug", "sheepherder", "mcannon", "hazeelcult", "elemental_workshop", "blackknight", "dragon"}

// wantFillerTail fails the test unless ids, an expansion of the owner's ladder with unproven quests allowed, ends with
// unprovenFillerTail: Cook's Assistant and everything after it, with the finale last.
func wantFillerTail(t *testing.T, ids []string) {
	t.Helper()
	cook := slices.Index(ids, unprovenFillerTail[0])
	if cook < 0 || !slices.Equal(ids[cook:], unprovenFillerTail) {
		t.Fatalf("the unproven owner ladder is %v, want it to end %v", ids, unprovenFillerTail)
	}
}
