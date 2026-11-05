package questplan

import "testing"

// 5 Nov 2026: the Manni kit stalled on a Tinderbox and the Sigli kit on a Mithril sword with "no ground, skill or shop source listed".
// Every line a kit asks for that the mule may not hold needs a source the provisioner can use.
func TestFremenniksKitLinesHaveShopSources(t *testing.T) {
	var s Snapshot
	for _, c := range []struct {
		trial FremTrial
		id    int
	}{{TrialManni, ObjTinderbox}, {TrialSigli, ObjMithrilSword}} {
		l, ok := kitLine(fremKit(c.trial, s), c.id)
		if !ok {
			t.Fatalf("trial %d: line %d missing", c.trial, c.id)
		}
		shops := 0
		for _, src := range l.Sources {
			if src.Kind == SourceShop && src.Shop.Npc != "" && src.Price > 0 {
				shops++
			}
		}
		if shops == 0 {
			t.Errorf("trial %d: line %d has no shop source", c.trial, c.id)
		}
	}
}
