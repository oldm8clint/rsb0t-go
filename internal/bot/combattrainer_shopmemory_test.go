package bot

import "testing"

// A host that restarts its trainer must not hand the new run a second free buying run, nor send
// it back to a shelf the last run found empty (the 29 September Cassie walks).
func TestTrainerShopMemorySurvivesARestart(t *testing.T) {
	var mem combatTrainerShopMemory
	first := &combatTrainerBot{embed: &combatTrainerEmbed{shop: &mem}}
	first.unavailable = map[string]bool{}
	first.loadShopMemory()
	first.unavailable["steel sq shield"] = true
	first.gearTrips, first.shopBandKey, first.shopRunsThisBand = 1, "alkharid", 1
	first.saveShopMemory()

	next := &combatTrainerBot{embed: &combatTrainerEmbed{shop: &mem}}
	next.unavailable = map[string]bool{}
	next.loadShopMemory()
	if !next.unavailable["steel sq shield"] {
		t.Fatal("the restarted run forgot the empty shelf")
	}
	if next.gearTrips != 1 || next.shopBandKey != "alkharid" || next.shopRunsThisBand != 1 {
		t.Fatalf("the restarted run lost its buying-run budget: trips %d band %q runs %d", next.gearTrips, next.shopBandKey, next.shopRunsThisBand)
	}
}

// The registered script, with no host, starts every run from nothing, as before.
func TestTrainerShopMemoryIsOffWithoutAHost(t *testing.T) {
	b := &combatTrainerBot{}
	b.unavailable = map[string]bool{}
	b.loadShopMemory()
	b.saveShopMemory()
	if b.gearTrips != 0 || len(b.unavailable) != 0 {
		t.Fatal("a trainer with no host picked up shop memory")
	}
}
