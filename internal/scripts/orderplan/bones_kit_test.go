package orderplan

import (
	"testing"
	"time"
)

// The Prayer rungs ask the mule for hundreds of bones in one kit (questplan.PlanBonesAsk, a thousand a kind at most). The mule serves
// an uncatalogued item up to its per-order ceiling and sends a line this big as bank notes, one trade slot a kind, which the
// account un-notes at the post's bank and banks the rest of (donorrunner.UnnoteKit).
func TestABoneKitCrossesAsOneNotePerKind(t *testing.T) {
	facts := func(string) (KitFacts, bool) { return KitFacts{HasNote: true}, true }
	k := DecideKit(time.Now(), "acct1", []KitLine{{Item: "Big bones", Qty: 800}, {Item: "Bones", Qty: 1000}}, kitPolicy(), NewLedger(time.Hour), plenty, facts, 26)
	if k.Serving() != 2 || k.Slots(facts) != 2 {
		t.Fatalf("serving %d slots %d, want both kinds in two slots: %+v", k.Serving(), k.Slots(facts), k.Lines)
	}
	for _, l := range k.Lines {
		if !l.Noted || l.Qty < 800 {
			t.Errorf("%s: %d noted %v, want the whole line as a note", l.Item, l.Qty, l.Noted)
		}
	}
	// More than the per-order ceiling is clamped, never refused.
	k = DecideKit(time.Now(), "acct1", []KitLine{{Item: "Bones", Qty: 5000}}, kitPolicy(), NewLedger(time.Hour), plenty, facts, 26)
	if l := k.Lines[0]; l.Qty != 1000 || !l.Noted {
		t.Errorf("5,000 Bones: %+v, want 1,000 as a note", l)
	}
	// A bone whose object the mule cannot find a note for is refused: the guard that the mule picks the object its bank holds.
	none := func(string) (KitFacts, bool) { return KitFacts{HasNote: false}, true }
	k = DecideKit(time.Now(), "acct1", []KitLine{{Item: "Bones", Qty: 800}}, kitPolicy(), NewLedger(time.Hour), plenty, none, 26)
	if l := k.Lines[0]; l.Qty != 0 || l.Reason != DeclineKitNoNote {
		t.Errorf("a bone with no note form: %+v, want %q", l, DeclineKitNoNote)
	}
}
