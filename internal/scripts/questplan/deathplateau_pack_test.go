package questplan

import (
	"os"
	"testing"

	"rsb0t-go/internal/cachecfg"
)

// The items the decision withdraws, asks the mule for and counts are the ids the cache names them
// by (skipped without RSB0T_CONFIG_JAG). The mule hand-out and the mule's published bank are keyed
// by NAME, so a name that two objs share (the noted copy of the spiked boots) must still resolve to
// the un-noted one, or an ask for a spare pair would be handed nothing.
func TestDeathItemsAreTheContentItems(t *testing.T) {
	cp := os.Getenv("RSB0T_CONFIG_JAG")
	if cp == "" {
		t.Skip("RSB0T_CONFIG_JAG not set")
	}
	cfg, err := cachecfg.LoadFile(cp)
	if err != nil {
		t.Fatal(err)
	}
	askedOfTheMule := map[int]bool{ObjSpikedBoots: true, ObjIronBar: true, ObjTrout: true, ObjBread: true}
	for id, want := range map[int]string{
		ObjDeathIOU: "Iou", ObjCombination: "Combination", ObjSecretMap: "Secret way map", ObjClimbingBoots: "Climbing boots",
		ObjSpikedBoots: "Spiked boots", ObjCertificate: "Certificate", ObjIronBar: "Iron bar", ObjTrout: "Trout", ObjBread: "Bread",
	} {
		o, ok := cfg.Obj(id)
		if !ok || o.Name != want {
			t.Errorf("item %d is %q, want %q", id, o.Name, want)
			continue
		}
		if o.Noted() {
			t.Errorf("item %d (%s) is a noted copy", id, want)
		}
		if !askedOfTheMule[id] {
			continue // withdrawn by id: another obj sharing the name (769 is a Certificate too) does not matter
		}
		plain := 0
		for _, other := range cfg.ObjIDs(want) {
			if oo, ok := cfg.Obj(other); ok && !oo.Noted() {
				plain++
				if other != id {
					t.Errorf("the name %q resolves to un-noted item %d as well as %d", want, other, id)
				}
			}
		}
		if plain != 1 {
			t.Errorf("the name %q has %d un-noted items, want exactly the one", want, plain)
		}
	}
}
