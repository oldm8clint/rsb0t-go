package gemplan

import "testing"

func keys(gems []Gem) []string {
	out := make([]string, 0, len(gems))
	for _, g := range gems {
		out = append(out, g.Key)
	}
	return out
}

func sameKeys(got []Gem, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i, k := range keys(got) {
		if k != want[i] {
			return false
		}
	}
	return true
}

func TestDecideStage(t *testing.T) {
	cases := []struct {
		name string
		in   Snapshot
		want Stage
	}{
		{"chisel and gems in the pack cuts", Snapshot{HasChisel: true, EligibleUncutHeld: 4, TargetsRemaining: 3}, StageCut},
		{"no chisel banks even with gems held", Snapshot{HasChisel: false, EligibleUncutHeld: 4, TargetsRemaining: 3}, StageBank},
		{"empty pack banks", Snapshot{HasChisel: true, EligibleUncutHeld: 0, TargetsRemaining: 3}, StageBank},
		{"no targets left and nothing to cut is done", Snapshot{HasChisel: true, EligibleUncutHeld: 0, TargetsRemaining: 0}, StageDone},
		// The pack is finished before the run ends: a last load must not be abandoned just
		// because the bank is known empty, which is what GemCutter.ts's own task order does.
		{"a held load still gets cut after the bank runs dry", Snapshot{HasChisel: true, EligibleUncutHeld: 2, TargetsRemaining: 0}, StageCut},
		{"no chisel and no targets is done, not a pointless bank trip", Snapshot{HasChisel: false, EligibleUncutHeld: 0, TargetsRemaining: 0}, StageDone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DecideStage(c.in); got != c.want {
				t.Errorf("DecideStage(%+v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestEligibleGatesOnLevel(t *testing.T) {
	// Crafting 20 on a members world: opal (1), jade (13), red topaz (16), sapphire (20).
	got := Eligible(20, nil, nil, true)
	if !sameKeys(got, []string{"opal", "jade", "red_topaz", "sapphire"}) {
		t.Errorf("Eligible(20) = %v, want opal..sapphire lowest level first", keys(got))
	}
	if len(Eligible(0, nil, nil, true)) != 0 {
		t.Error("Crafting 0 should allow no gem at all")
	}
	if n := len(Eligible(99, nil, nil, true)); n != len(Gems) {
		t.Errorf("Crafting 99 should allow every gem, got %d of %d", n, len(Gems))
	}
}

func TestEligibleDropsMembersGemsOnAFreeWorld(t *testing.T) {
	got := Eligible(99, nil, nil, false)
	if !sameKeys(got, []string{"sapphire", "emerald", "ruby", "diamond"}) {
		t.Errorf("a free world should leave only the four non-members gems, got %v", keys(got))
	}
	for _, g := range got {
		if g.Members {
			t.Errorf("%s is members-only and must not survive a free-world filter", g.Key)
		}
	}
}

func TestEligibleHonoursSelectionAndDenials(t *testing.T) {
	// Selection matches ignoring case and accepts the key spelling too.
	got := Eligible(99, []string{"ruby", "SAPPHIRE", "red_topaz"}, nil, true)
	if !sameKeys(got, []string{"red_topaz", "sapphire", "ruby"}) {
		t.Errorf("selection should match ignoring case, got %v", keys(got))
	}
	// An unknown name in the selection must not widen the run back to everything.
	got = Eligible(99, []string{"Ruby", "Moonstone"}, nil, true)
	if !sameKeys(got, []string{"ruby"}) {
		t.Errorf("an unknown selected name should be ignored, not widen the run: %v", keys(got))
	}
	denied := map[string]bool{"ruby": true}
	got = Eligible(99, []string{"Ruby", "Diamond"}, denied, true)
	if !sameKeys(got, []string{"diamond"}) {
		t.Errorf("a denied gem should be filtered out, got %v", keys(got))
	}
}

func TestTargetsDropsConfirmedEmpty(t *testing.T) {
	eligible := Eligible(99, nil, nil, true)
	got := Targets(eligible, map[string]bool{"opal": true, "diamond": true})
	if !sameKeys(got, []string{"jade", "red_topaz", "sapphire", "emerald", "ruby", "dragonstone"}) {
		t.Errorf("Targets should drop gems confirmed empty in the bank, got %v", keys(got))
	}
	if len(Targets(eligible, nil)) != len(eligible) {
		t.Error("Targets with nothing confirmed empty should be the whole eligible list")
	}
}

// TestTableMatchesContent pins the numbers this port was grounded on, so a later edit cannot
// silently drift from content/scripts/skill_crafting/configs/gem/gem.dbrow and the object ids
// read back from the live engine's own config archive.
func TestTableMatchesContent(t *testing.T) {
	want := map[string]Gem{
		"opal":        {UncutID: 1625, CutID: 1609, Level: 1, XP: 150, CanCrush: true, Members: true},
		"jade":        {UncutID: 1627, CutID: 1611, Level: 13, XP: 200, CanCrush: true, Members: true},
		"red_topaz":   {UncutID: 1629, CutID: 1613, Level: 16, XP: 250, CanCrush: true, Members: true},
		"sapphire":    {UncutID: 1623, CutID: 1607, Level: 20, XP: 500},
		"emerald":     {UncutID: 1621, CutID: 1605, Level: 27, XP: 675},
		"ruby":        {UncutID: 1619, CutID: 1603, Level: 34, XP: 850},
		"diamond":     {UncutID: 1617, CutID: 1601, Level: 43, XP: 1075},
		"dragonstone": {UncutID: 1631, CutID: 1615, Level: 55, XP: 1375, Members: true},
	}
	if len(Gems) != len(want) {
		t.Fatalf("table holds %d gems, content's gem_cutting_table has %d", len(Gems), len(want))
	}
	for _, g := range Gems {
		w, ok := want[g.Key]
		if !ok {
			t.Errorf("%s is not a content gem_cutting_table row", g.Key)
			continue
		}
		if g.UncutID != w.UncutID || g.CutID != w.CutID {
			t.Errorf("%s ids = %d/%d, live config archive says %d/%d", g.Key, g.UncutID, g.CutID, w.UncutID, w.CutID)
		}
		if g.Level != w.Level || g.XP != w.XP {
			t.Errorf("%s level/xp = %d/%d, gem.dbrow says %d/%d", g.Key, g.Level, g.XP, w.Level, w.XP)
		}
		if g.CanCrush != w.CanCrush {
			t.Errorf("%s CanCrush = %v, gem.dbrow success_rate says %v", g.Key, g.CanCrush, w.CanCrush)
		}
		if g.Members != w.Members {
			t.Errorf("%s Members = %v, the live config archive says %v", g.Key, g.Members, w.Members)
		}
	}
}

func TestDisplayXPIsTenthsOfContentXP(t *testing.T) {
	g, ok := GemByName("Sapphire")
	if !ok {
		t.Fatal("Sapphire should be in the table")
	}
	// content stat_advance(crafting, 500) is the 50 XP a player sees.
	if g.DisplayXP() != 50 {
		t.Errorf("Sapphire DisplayXP = %d, want 50", g.DisplayXP())
	}
}

func TestLookups(t *testing.T) {
	if g, ok := GemByUncutID(1623); !ok || g.Key != "sapphire" {
		t.Errorf("GemByUncutID(1623) = %+v/%v, want sapphire", g, ok)
	}
	if g, ok := GemByCutID(1607); !ok || g.Key != "sapphire" {
		t.Errorf("GemByCutID(1607) = %+v/%v, want sapphire", g, ok)
	}
	// The noted variant sits at id+1 and shares the display name; it must not resolve as a
	// cuttable gem, which is the whole reason this package matches by id.
	if _, ok := GemByUncutID(1624); ok {
		t.Error("the noted uncut sapphire (1624) must not resolve as a cuttable gem")
	}
	if g, ok := GemByName("  red TOPAZ "); !ok || g.Key != "red_topaz" {
		t.Errorf("GemByName should trim and ignore case, got %+v/%v", g, ok)
	}
	if _, ok := GemByName("Moonstone"); ok {
		t.Error("GemByName should not invent gems")
	}
	if n := len(GemNames()); n != len(Gems) {
		t.Errorf("GemNames listed %d of %d gems", n, len(Gems))
	}
}

func TestChatClassifiers(t *testing.T) {
	// content builds this line as "You need a Crafting level of <level> to cut <name>."
	if level, ok := LevelRefusal("You need a Crafting level of 34 to cut rubys."); !ok || level != 34 {
		t.Errorf("LevelRefusal = %d/%v, want 34/true", level, ok)
	}
	if _, ok := LevelRefusal("You cut the Sapphire."); ok {
		t.Error("a success line is not a refusal")
	}
	if !IsCutMessage("You cut the Sapphire.") {
		t.Error("IsCutMessage should match content's success line")
	}
	if IsCutMessage("You need a Crafting level of 20 to cut sapphires.") {
		t.Error("a refusal is not a success")
	}
	if !IsSmashMessage("You mis-hit the chisel and smash the Opal to pieces!") {
		t.Error("IsSmashMessage should match content's failure line")
	}
	if IsSmashMessage("You cut the Opal.") {
		t.Error("a success is not a smash")
	}
	// content/scripts/general/configs/free_to_play.constant defines this exact line.
	if !IsMembersRefusal("You need to be on a members' server to use this feature.") {
		t.Error("IsMembersRefusal should match content's free-to-play line")
	}
	if IsMembersRefusal("You need a Crafting level of 55 to cut dragonstones.") {
		t.Error("a level refusal is not a members refusal — they have different fixes")
	}
}

func TestSettingsNormalised(t *testing.T) {
	s := Settings{}.Normalised()
	if s.BankBooth != BoothName {
		t.Errorf("blank bankBooth should fall back to %q, got %q", BoothName, s.BankBooth)
	}
	if len(s.Gems) != 0 {
		t.Errorf("no gems selected should stay empty (meaning all), got %v", s.Gems)
	}
	if s.Verbose() {
		t.Error("default LogDetail should not be verbose")
	}
	custom := Settings{Gems: []string{"ruby", "RUBY", "Moonstone", " sapphire "}, BankBooth: " Bank chest ", LogDetail: " Verbose "}.Normalised()
	if len(custom.Gems) != 2 || custom.Gems[0] != "Ruby" || custom.Gems[1] != "Sapphire" {
		t.Errorf("gems should be canonicalised and de-duplicated, got %v", custom.Gems)
	}
	if custom.BankBooth != "Bank chest" {
		t.Errorf("bankBooth should be trimmed, not replaced, got %q", custom.BankBooth)
	}
	if !custom.Verbose() {
		t.Error("Verbose logDetail should report Verbose() true")
	}
}

func TestUnknownGems(t *testing.T) {
	got := UnknownGems([]string{"Ruby", " ", "Moonstone", "sapphire"})
	if len(got) != 1 || got[0] != "Moonstone" {
		t.Errorf("UnknownGems = %v, want just Moonstone", got)
	}
	if len(UnknownGems(nil)) != 0 {
		t.Error("no names means nothing unknown")
	}
}
