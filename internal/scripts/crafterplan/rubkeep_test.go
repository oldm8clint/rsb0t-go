package crafterplan

import (
	"testing"

	"rsb0t-go/internal/scripts/freshstart"
)

// A carried rub item is not a reason to bank everything: the Seers step spins or picks instead.
func TestDecideSeersKeepsTheRubJewellery(t *testing.T) {
	v := SeersView{Pack: []freshstart.PackItem{{Name: "Ring of dueling(8)", Count: 1}}}
	if got := DecideSeers(v); got == SeersBankAll {
		t.Fatalf("a Ring of dueling alone made the Seers step %q", got)
	}
	v = SeersView{Pack: []freshstart.PackItem{{Name: "Games necklace(7)", Count: 1}, {Name: "Bronze axe", Count: 1}}}
	if got := DecideSeers(v); got != SeersBankAll {
		t.Fatalf("a foreign axe beside the necklace: %q, want bank everything", got)
	}
}
