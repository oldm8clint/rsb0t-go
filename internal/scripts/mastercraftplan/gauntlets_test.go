package mastercraftplan

import (
	"strconv"
	"strings"
	"testing"
)

func TestGoldBarXPWithTheGauntlets(t *testing.T) {
	var gold, steel Bar
	for _, b := range Bars {
		if b.Gold {
			gold = b
		}
		if b.Name == "Steel bar" {
			steel = b
		}
	}
	if gold.XPTenths != 225 || gold.XPTenthsWith(false) != 225 || gold.XPTenthsWith(true) != 562 {
		t.Errorf("gold: %d / %d / %d", gold.XPTenths, gold.XPTenthsWith(false), gold.XPTenthsWith(true))
	}
	if steel.XPTenthsWith(true) != steel.XPTenths {
		t.Errorf("only gold is scaled: %d", steel.XPTenthsWith(true))
	}
	if GoldBarXPString(false) != "22.5" || GoldBarXPString(true) != "56.2" {
		t.Errorf("strings %q %q", GoldBarXPString(false), GoldBarXPString(true))
	}
}

func TestGoldInPlay(t *testing.T) {
	set := DefaultSettings()
	if GoldInPlay(set, 99) {
		t.Error("gold is off by default")
	}
	set.SuperheatGold = true
	if GoldInPlay(set, 39) || !GoldInPlay(set, 40) {
		t.Errorf("Smithing 40: 39 %v, 40 %v", GoldInPlay(set, 39), GoldInPlay(set, 40))
	}
	set.SuperheatBars = []string{"Steel bar"}
	if GoldInPlay(set, 99) {
		t.Error("the bar list leaves gold out")
	}
	set.SuperheatBars = []string{"Steel bar", "Gold bar"}
	if !GoldInPlay(set, 99) {
		t.Error("the bar list names gold")
	}
}

func TestGauntletStep(t *testing.T) {
	for _, tc := range []struct {
		name           string
		play, worn     bool
		inPack, inBank int
		want           GauntletAction
	}{
		{"gold not in play", false, false, 1, 1, GauntletsNotNeeded},
		{"worn", true, true, 0, 0, GauntletsWorn},
		{"worn, and a spare in the bank", true, true, 0, 1, GauntletsWorn},
		{"in the pack", true, false, 1, 1, GauntletsWear},
		{"in the bank", true, false, 0, 1, GauntletsDraw},
		{"nowhere", true, false, 0, 0, GauntletsMissing},
	} {
		if got := GauntletStep(tc.play, tc.worn, tc.inPack, tc.inBank); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestGauntletLines(t *testing.T) {
	if l := GauntletLine(GauntletsWorn, nil); !strings.Contains(l, "56.2") || !strings.Contains(l, "not 22.5") {
		t.Errorf("worn: %q", l)
	}
	if l := GauntletLine(GauntletsDraw, nil); !strings.Contains(l, "from the bank") {
		t.Errorf("draw: %q", l)
	}
	if l := GauntletLine(GauntletsWear, nil); !strings.Contains(l, "from the pack") {
		t.Errorf("wear: %q", l)
	}
	l := GauntletLine(GauntletsMissing, []string{"Mining 30 of 40"})
	for _, want := range []string{"no Goldsmith gauntlets", "22.5", "Family Crest", "NOT in the quest ladder", "short of Mining 30 of 40"} {
		if !strings.Contains(l, want) {
			t.Errorf("missing %q: %q", want, l)
		}
	}
	if GauntletLine(GauntletsNotNeeded, nil) != "" {
		t.Error("nothing to say when gold is not in play")
	}
}

func TestFamilyCrestShort(t *testing.T) {
	if got := FamilyCrestShort(map[string]int{"Mining": 40, "Crafting": 92, "Smithing": 63, "Magic": 59}); len(got) != 0 {
		t.Errorf("all met: %v", got)
	}
	got := FamilyCrestShort(map[string]int{"Mining": 27, "Crafting": 92, "Smithing": 63, "Magic": 40})
	if len(got) != 2 || got[0] != "Mining 27 of 40" || got[1] != "Magic 40 of 59" {
		t.Errorf("short: %v", got)
	}
}

// THE FACTS the gauntlets rest on, from a Content checkout (RSB0T_CONTENT, skipped without one).
func TestGoldsmithGauntletsAreContents(t *testing.T) {
	if got := packID(contentRoot(t, "pack/obj.pack"), "gauntlets_of_goldsmithing"); got != int(GoldGauntletsID) {
		t.Errorf("gauntlets_of_goldsmithing: content's id %d, the plan's %d", got, GoldGauntletsID)
	}
	obj := contentFile(t, "quests/quest_crest/configs/quest_crest.obj")
	if b := block(obj, "gauntlets_of_goldsmithing"); !strings.Contains(b, "name="+GoldGauntlets+"\n") || !strings.Contains(b, "tradeable=no") {
		t.Errorf("[gauntlets_of_goldsmithing]: %q", b)
	}
	sh := contentFile(t, "skill_magic/scripts/spells/superheat.rs2")
	for _, want := range []string{
		"if ($bar = gold_bar & inv_total(worn, gauntlets_of_goldsmithing) > 0) {",
		"$smith_xp = scale(5, 2, $smith_xp);",
	} {
		if !strings.Contains(sh, want) {
			t.Errorf("superheat.rs2 lacks %q", want)
		}
	}
	// The quest's requirements, as its journal lists them.
	journal := contentFile(t, "quests/quest_crest/scripts/crest_journal.rs2")
	for _, r := range FamilyCrestRequirements {
		if want := "@dre@Level " + strconv.Itoa(r.Level) + " " + r.Skill + "|"; !strings.Contains(journal, want) {
			t.Errorf("crest_journal.rs2 lacks %q", want)
		}
	}
	// Avan makes the gauntlets goldsmith ones from the steel gauntlets the quest gives.
	man := contentFile(t, "quests/quest_crest/scripts/crest_man.rs2")
	for _, want := range []string{"inv_del(inv, steel_gauntlets, 1);", "inv_add(inv, gauntlets_of_goldsmithing, 1);"} {
		if !strings.Contains(man, want) {
			t.Errorf("crest_man.rs2 lacks %q", want)
		}
	}
	done := contentFile(t, "quests/quest_crest/scripts/quest_crest.rs2")
	if !strings.Contains(done, "inv_add(inv, steel_gauntlets, 1);") {
		t.Errorf("quest_crest.rs2 no longer rewards the steel gauntlets")
	}
}
