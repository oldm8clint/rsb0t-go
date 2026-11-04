package combatplan

import "testing"

func items(names ...string) []ValuedItem {
	out := make([]ValuedItem, 0, len(names))
	// Descending values in the order given, so the caller's order is the value order.
	value := 1000 * len(names)
	for _, n := range names {
		out = append(out, ValuedItem{Name: n, Value: value})
		value -= 1000
	}
	return out
}

func names(v []ValuedItem) []string {
	out := make([]string, 0, len(v))
	for _, it := range v {
		out = append(out, it.Name)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestKeptOnDeath(t *testing.T) {
	held := items("Rune platebody", "Rune scimitar", "Rune platelegs", "Rune full helm", "Lobster")

	t.Run("unskulled keeps three", func(t *testing.T) {
		kept, lost := KeptOnDeath(held, false, false)
		if want := []string{"Rune platebody", "Rune scimitar", "Rune platelegs"}; !equal(names(kept), want) {
			t.Fatalf("kept %v, want %v", names(kept), want)
		}
		if want := []string{"Rune full helm", "Lobster"}; !equal(names(lost), want) {
			t.Fatalf("lost %v, want %v", names(lost), want)
		}
	})

	t.Run("skulled keeps nothing", func(t *testing.T) {
		kept, lost := KeptOnDeath(held, true, false)
		if len(kept) != 0 {
			t.Fatalf("kept %v, want nothing", names(kept))
		}
		if len(lost) != len(held) {
			t.Fatalf("lost %d, want all %d", len(lost), len(held))
		}
	})

	t.Run("Protect Item saves exactly one more", func(t *testing.T) {
		kept, _ := KeptOnDeath(held, false, true)
		if len(kept) != 4 {
			t.Fatalf("kept %d, want 4 (three plus the prayer's own)", len(kept))
		}
		skulledKept, _ := KeptOnDeath(held, true, true)
		if len(skulledKept) != 1 {
			t.Fatalf("skulled with Protect Item kept %d, want 1", len(skulledKept))
		}
	})

	t.Run("holding less than it would keep", func(t *testing.T) {
		kept, lost := KeptOnDeath(items("Bronze sword"), false, false)
		if len(kept) != 1 || len(lost) != 0 {
			t.Fatalf("kept %v lost %v, want the one item kept", names(kept), names(lost))
		}
	})

	t.Run("value decides, not order held", func(t *testing.T) {
		held := []ValuedItem{
			{Name: "Bones", Value: 1},
			{Name: "Rune scimitar", Value: 30000},
			{Name: "Bronze dagger", Value: 10},
		}
		kept, _ := KeptOnDeath(held, true, true)
		if len(kept) != 1 || kept[0].Name != "Rune scimitar" {
			t.Fatalf("kept %v, want the most valuable item", names(kept))
		}
	})
}

func TestReGearPlan(t *testing.T) {
	wanted := []string{"Rune scimitar", "Rune platebody", "Rune platelegs", "Amulet of strength"}

	t.Run("what survived is left alone", func(t *testing.T) {
		equip, missing := ReGearPlan(wanted, []string{"Rune scimitar", "Rune platebody"}, []string{"Rune platelegs"})
		if len(equip) != 1 || equip[0].Item != "Rune platelegs" || !equip[0].Have {
			t.Fatalf("equip = %+v, want just the platelegs from the pack", equip)
		}
		if want := []string{"Amulet of strength"}; !equal(missing, want) {
			t.Fatalf("missing = %v, want %v", missing, want)
		}
	})

	t.Run("everything lost", func(t *testing.T) {
		equip, missing := ReGearPlan(wanted, nil, nil)
		if len(equip) != 0 {
			t.Fatalf("equip = %+v, want nothing available", equip)
		}
		if len(missing) != len(wanted) {
			t.Fatalf("missing = %v, want all four", missing)
		}
	})

	t.Run("matching ignores case", func(t *testing.T) {
		equip, missing := ReGearPlan([]string{"Rune Scimitar"}, nil, []string{"rune scimitar"})
		if len(equip) != 1 || len(missing) != 0 {
			t.Fatalf("equip %+v missing %v, want the scimitar matched case-insensitively", equip, missing)
		}
	})

	t.Run("two of the same item need two in the pool", func(t *testing.T) {
		equip, missing := ReGearPlan([]string{"Lobster", "Lobster"}, nil, []string{"Lobster"})
		if len(equip) != 1 || len(missing) != 1 {
			t.Fatalf("equip %+v missing %v, want one satisfied and one missing", equip, missing)
		}
	})
}

func TestLostItems(t *testing.T) {
	before := []string{"Rune scimitar", "Rune platebody", "Lobster", "Lobster"}
	after := []string{"Rune scimitar", "Lobster"}
	lost := LostItems(before, after)
	if want := []string{"Rune platebody", "Lobster"}; !equal(lost, want) {
		t.Fatalf("lost = %v, want %v (one of the two lobsters survived)", lost, want)
	}
	if got := LostItems(before, before); len(got) != 0 {
		t.Fatalf("lost = %v, want nothing when everything survived", got)
	}
}

func TestAtRespawn(t *testing.T) {
	if !AtRespawn(LumbridgeRespawnX, LumbridgeRespawnZ, 0, 0) {
		t.Error("the exact respawn tile must match")
	}
	if !AtRespawn(3223, 3220, 0, 3) {
		t.Error("the server places the player on a nearby walkable tile, so slack must count")
	}
	if AtRespawn(3223, 3220, 0, 1) {
		t.Error("outside the slack must not match")
	}
	if AtRespawn(LumbridgeRespawnX, LumbridgeRespawnZ, 1, 5) {
		t.Error("a different level is never the respawn")
	}
	if AtRespawn(3222, 3218, 0, 5) != true {
		t.Error("one tile east is within five")
	}
}

// Pin the respawn tile itself: it comes from death.rs2's map_findsquare(0_50_50_21_18).
func TestRespawnTileIsLumbridge(t *testing.T) {
	if LumbridgeRespawnX != 3221 || LumbridgeRespawnZ != 3218 {
		t.Fatalf("respawn = %d,%d, want 3221,3218 (map square 50,50 offset 21,18)", LumbridgeRespawnX, LumbridgeRespawnZ)
	}
}
