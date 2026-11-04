package bot

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/freshstart"
)

// The fleet loader is STRICT: an unknown settings key is an error that fails the whole roster at
// startup, not just the account that carries it. So handling Tutorial Island had to earn a key
// or do without one, and it does without: the guide's name and the skip option come from
// freshstart's own constants, which are the values content actually uses.
//
// This pins the count so a later edit cannot quietly add one. If a key is genuinely needed, the
// number here changes in the same commit that adds it and with the roster in mind.
func TestCombatTrainerTutorialAddsNoSettingsKey(t *testing.T) {
	const want = 15
	if got := len(CombatTrainerSettings); got != want {
		keys := make([]string, 0, len(CombatTrainerSettings))
		for k := range CombatTrainerSettings {
			keys = append(keys, k)
		}
		t.Fatalf("CombatTrainer declares %d settings keys, want %d — the loader rejects unknown keys and a wrong one breaks the whole fleet at startup. Keys: %s",
			got, want, strings.Join(keys, ", "))
	}
	for _, banned := range []string{"guideNpc", "guideNPC", "skipOption", "tutorial", "skipTutorial", "doTutorial"} {
		if _, ok := CombatTrainerSettings[banned]; ok {
			t.Fatalf("CombatTrainer declares a %q key; the tutorial leg is meant to need none", banned)
		}
	}
}

// The values the wiring feeds SkipTutorial have to be the ones content answers to, not a
// plausible-looking string. content/scripts/tutorial/scripts/guides/runescape_guide.rs2 offers
// ~p_choice2("Yes please.", 1, "No, thank you.", 2), and tutorial.npc names the NPC
// "RuneScape Guide" with op1=Talk-to.
func TestCombatTrainerTutorialUsesContentsOwnGuideStrings(t *testing.T) {
	if freshstart.DefaultGuideNPC != "RuneScape Guide" {
		t.Errorf("guide NPC is %q; tutorial.npc names it \"RuneScape Guide\"", freshstart.DefaultGuideNPC)
	}
	if !strings.Contains(strings.ToLower(freshstart.DefaultSkipOption), "yes") {
		t.Errorf("skip option %q would not match content's \"Yes please.\"", freshstart.DefaultSkipOption)
	}
	if _, match := freshstart.ChooseSkipOption([]string{"Yes please.", "No, thank you."}, freshstart.DefaultSkipOption); match != freshstart.SkipWanted {
		t.Errorf("the default skip option must match content's own option list exactly, got match %v", match)
	}
	if freshstart.TalkOp != "Talk-to" {
		t.Errorf("talk op is %q; tutorial.npc declares op1=Talk-to", freshstart.TalkOp)
	}
}

// A brand-new account is blind for the whole tutorial, and Loop's containers gate turns five
// minutes of that into a FATAL error. The tutorial budget therefore has to be able to outlast
// the tutorial without the containers gate ever being reached — which is only true because the
// tutorial step returns BEFORE that gate. This pins the relationship so neither number can be
// changed in isolation without a failing test explaining why it matters.
func TestTutorialBudgetOutlivesTheContainersGate(t *testing.T) {
	if freshstart.TutorialLimit <= containersUnreadableLimit {
		t.Fatalf("the tutorial budget (%s) is not longer than the containers gate (%s). That is only safe while the tutorial step returns before the gate; if the ordering is ever changed, a fresh account will be killed by its own blindness mid-tutorial",
			freshstart.TutorialLimit, containersUnreadableLimit)
	}
}

// An account that is already off the island must cost nothing and say nothing: this is every
// account in the running fleet, on every single Loop call.
func TestTutorialIsSilentAndDoneForAnAccountThatNeverSawTheIsland(t *testing.T) {
	var st tutorialState
	done, giveUp := st.watch.Note(freshstart.TutorialProgress{
		Stage: freshstart.TutorialStageOff, Action: freshstart.TutorialWaitTabs, Readable: true,
	}, time.Now())
	if !done || giveUp != "" {
		t.Fatalf("a finished account must be done on its first pass, got done=%v giveUp=%q", done, giveUp)
	}
	if st.announced {
		t.Fatal("nothing should have been announced for an account that was never on the island")
	}
}
