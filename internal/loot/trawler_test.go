package loot

import "testing"

func TestTheTrawlerRowIsOnTheFishingCard(t *testing.T) {
	s := FishingTrawler()
	if s.GroupKey() != Fishing(nil, 1518).GroupKey() {
		t.Fatalf("the Fishing Trawler row is on card %q, the fishing spots on %q", s.GroupKey(), Fishing(nil, 1518).GroupKey())
	}
	if s.PartKey() == Fishing(nil, 1518).PartKey() {
		t.Fatal("the trawler must be a row of its own on the Fishing card")
	}
	if stat, ok := ClaimsExperience(s); !ok || stat != 10 {
		t.Fatalf("the trawler row takes Fishing experience (stat 10): %d %v", stat, ok)
	}
	if _, ok := s.Consumes(); ok {
		t.Fatal("a claim consumes nothing: the paste is measured per leak, not per claim")
	}
}

func TestATrawlerClaimRecordsOnlyWhatMoved(t *testing.T) {
	if _, ok := TrawlerClaim(nil, Counts{}); ok {
		t.Fatal("a claim that moved nothing records nothing")
	}
	d, ok := TrawlerClaim(nil, Counts{383: 20, 371: 6})
	if !ok || d.Events != 1 || len(d.Items) != 2 || d.Items[0].ID != 371 || d.Items[1].Count != 20 {
		t.Fatalf("claim drop = %+v %v", d, ok)
	}
	tally := NewTally(nil)
	tally.Add(d)
	snap := tally.Snapshot()
	if len(snap) != 1 || snap[0].Events != 1 || snap[0].Source.Name != FishingTrawlerName {
		t.Fatalf("tally = %+v", snap)
	}
}

func TestTrawlerLeaksAreSpentPasteOnTheTrawlerRow(t *testing.T) {
	if _, ok := TrawlerLeak(nil, 0); ok {
		t.Fatal("no leak, no cost")
	}
	d, ok := TrawlerLeak(nil, 3)
	if !ok || d.Events != 0 || len(d.Items) != 0 || len(d.Spent) != 1 || d.Spent[0].ID != TrawlerPasteID || d.Spent[0].Count != 3 {
		t.Fatalf("leak drop = %+v", d)
	}
	if d.Source.Key() != FishingTrawler().Key() {
		t.Fatal("the paste is a cost of the Fishing Trawler row")
	}
	tally := NewTally(nil)
	tally.Add(d)
	if snap := tally.Snapshot(); len(snap) != 1 || len(snap[0].Spent) != 1 || snap[0].Spent[0].Count != 3 {
		t.Fatalf("a cost-only drop must be kept: %+v", snap)
	}
}

func TestAMendedNetIsCraftingProductionThatConsumesRope(t *testing.T) {
	if _, ok := TrawlerMended(nil, 0); ok {
		t.Fatal("no mend, no event")
	}
	d, ok := TrawlerMended(nil, 2)
	if !ok || d.Events != 2 {
		t.Fatalf("mend drop = %+v", d)
	}
	if d.Source.Kind != KindProduction || d.Source.Skill != SkillCrafting || d.Source.Action != ActionMend {
		t.Fatalf("mend source = %+v", d.Source)
	}
	in := d.Source.Inputs()
	if len(in) != 1 || in[0].ID != TrawlerRopeID || in[0].Per != 1 || in[0].Name != "Rope" {
		t.Fatalf("a mend consumes one Rope: %+v", in)
	}
	if stat, ok := ClaimsExperience(d.Source); !ok || stat != 12 {
		t.Fatalf("a mend takes Crafting experience (stat 12): %d %v", stat, ok)
	}
}
