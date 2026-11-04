package freshstartrunner

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/freshstart"
)

// The boarding dialogues are short, linear and fully enumerated. That is the property
// BoardShip's step counter relies on to know when it has run off the end of one.
//
// It matters because the counter advances on FAILED matches too. Live on 2026-09-20 an
// account displaced to Karamja emitted "no option matching \"yes\" — choosing the first
// option" every 800ms indefinitely: the step ran past the scripted line, `want` fell back to
// "yes", matched nothing, clicked option 1, advanced, and repeated. BoardShip now stops
// driving once the step passes the last scripted answer instead of guessing — so if a leg's
// choice list ever grows open-ended, that rule needs revisiting rather than silently
// allowing more blind clicks.
func TestSailDialogueChoicesAreShortAndLinear(t *testing.T) {
	for _, c := range []struct {
		leg   freshstart.Leg
		steps int
		first string
	}{
		{freshstart.LegPier, 1, "yes please"},
		{freshstart.LegDestination, 3, "journey"},
	} {
		names, choices := sailDialogueChoices(c.leg)
		if len(names) == 0 {
			t.Errorf("leg %v names no boarding npc", c.leg)
		}
		if len(choices) != c.steps {
			t.Errorf("leg %v has %d scripted steps, want %d — BoardShip stops driving past the last one, so this count is load-bearing", c.leg, len(choices), c.steps)
		}
		if len(choices) > 0 && choices[0] != c.first {
			t.Errorf("leg %v first answer = %q, want %q", c.leg, choices[0], c.first)
		}
		for i, ch := range choices {
			if ch == "" {
				t.Errorf("leg %v step %d has an empty answer, which would match any option", c.leg, i)
			}
		}
	}
}

// THE PLANTATION BRANCH, which is why the dialogue is now driven by MATCHING rather than by
// a step index.
//
// content/scripts/areas/area_karamja/scripts/customs_officer.rs2, [label,customs_journey]:
// an account the officer recognises — "Hey, I know you, you work at the plantation" — skips
// the search entirely and goes straight to the 30-coin charge. Every account that has done
// Luthas's banana job takes that branch, and freshstartrunner's own bananafare.go exists to
// do exactly that job. So the customs tree is 3 screens for a stranger and 2 for a worker,
// and a fixed index answers the second screen with "search away" when it is really
// "Ok." / "Oh, I'll not bother then."
//
// Measured live 2026-09-22 before the change: 707 boat legs started against 66 gangplank
// crossings, 61 "no option matching \"search away\"", 62 "ran past its scripted steps".
func TestCustomsAnswersCoverBothTreeLengths(t *testing.T) {
	_, choices := sailDialogueChoices(freshstart.LegDestination)

	// The stranger's tree, in order.
	stranger := [][]string{
		{"Can I journey on this ship?", "Does Karamja have unusual customs then?"},
		{"Why?", "Search away, I have nothing to hide.", "You're not putting your hands on my things!"},
		{"Ok.", "Oh, I'll not bother then."},
	}
	// The plantation worker's tree: the search screen never appears.
	worker := [][]string{
		{"Can I journey on this ship?", "Does Karamja have unusual customs then?"},
		{"Ok.", "Oh, I'll not bother then."},
	}

	for name, tree := range map[string][][]string{"stranger": stranger, "plantation worker": worker} {
		for i, screen := range tree {
			matched := ""
			for _, want := range choices {
				if optionOnScreen(screen, want) {
					matched = want
					break
				}
			}
			if matched == "" {
				t.Errorf("%s screen %d %v: none of %v answers it — the account cannot board",
					name, i, screen, choices)
			}
			// And the answer chosen must never be the one that refuses the trip.
			if matched != "" && optionOnScreen([]string{"You're not putting your hands on my things!"}, matched) {
				t.Errorf("%s screen %d: chose the refusal %q", name, i, matched)
			}
		}
	}
}

// optionOnScreen is the same case-insensitive substring match actions.ChatDialog uses to pick
// an option, reproduced here so the test does not need a live dialog.
func optionOnScreen(screen []string, want string) bool {
	for _, o := range screen {
		if strings.Contains(strings.ToLower(o), strings.ToLower(want)) {
			return true
		}
	}
	return false
}
