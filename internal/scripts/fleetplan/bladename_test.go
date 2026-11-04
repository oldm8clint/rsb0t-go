package fleetplan

import "testing"

// At the kit check itself: a pack holding only a Swordfish has NO blade, and the lever is not pulled.
func TestAKitWithOnlyASwordfishIsRefused(t *testing.T) {
	v := CheckWildernessKit(Kit{Pack: []KitItem{{Name: "Swordfish", Count: 1}}})
	if v.OK || v.Blade != "" {
		t.Fatalf("kit with a Swordfish: %+v, want refused with no blade", v)
	}
	// Even worn (the server would not let it be) it is not a blade.
	if w := CheckWildernessKit(Kit{Worn: []KitItem{{Name: "Swordfish", Count: 1}}}); w.OK || w.BladeWorn {
		t.Fatalf("a worn Swordfish: %+v, want refused", w)
	}
	// Control: a real sword in the pack beside nothing else is a good kit, and a Swordfish beside it is
	// food to be banked, not a second blade.
	if ok := CheckWildernessKit(Kit{Pack: []KitItem{{Name: "Bronze sword", Count: 1}}}); !ok.OK {
		t.Fatalf("control: a lone sword %+v", ok)
	}
	both := CheckWildernessKit(Kit{Pack: []KitItem{{Name: "Bronze sword", Count: 1}, {Name: "Swordfish", Count: 3}}})
	if both.OK || both.Blade != "Bronze sword" {
		t.Fatalf("sword plus Swordfish: %+v, want the sword as the blade and the fish a problem", both)
	}
}
