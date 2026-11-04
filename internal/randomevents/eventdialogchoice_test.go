package randomevents

import (
	"testing"

	"rsb0t-go/internal/actions"
)

// The Games necklace's rub menu is answered "Nowhere.", every other menu with its first option.
func TestEventDialogChoiceRefusesAJewelleryTeleport(t *testing.T) {
	rub := []actions.Option{{Text: "Burthorpe Games Rooms."}, {Text: "Nowhere."}}
	if got := eventDialogChoice(rub); got != "Nowhere." {
		t.Errorf("the necklace menu chose %q", got)
	}
	if got := eventDialogChoice([]actions.Option{{Text: "Yes please."}, {Text: "No thanks."}}); got != "" {
		t.Errorf("an event's own menu chose %q, want the first option", got)
	}
}
