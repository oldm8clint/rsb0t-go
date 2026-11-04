package consolidateplan

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/combatplan"
	"rsb0t-go/internal/scripts/levels"
)

func stage5Kit() KitInput {
	return KitInput{
		Keep:       []string{"Mind rune", "Yew shortbow"},
		Worn:       []string{"Adamant scimitar", "Adamant platebody", "Adamant full helm"},
		Shed:       []string{"Adamant platelegs"},
		Levels:     combatplan.Levels{Attack: 35, Strength: 35, Defence: 35},
		MeleeGoal:  40,
		PrayerGoal: 0,
	}
}

func TestClassifyLootKitAndSurplus(t *testing.T) {
	in := stage5Kit()
	cases := map[string]Kind{
		"Coins":             KindCoins,
		"Chaos rune":        KindRune,
		"Death rune":        KindRune,
		"Shark":             KindFood,
		"Cake":              KindFood,
		"Mind rune":         KindRune, // a rune first; the keep list holds it back in PlanDeposit
		"Staff of air":      KindKit,
		"Magic staff":       KindKit,
		"Lava battlestaff":  KindKit,
		"Adamant scimitar":  KindKit, // worn
		"Adamant platelegs": KindKit, // shed for Magic
		"Rope":              KindKit,
		"Shantay pass":      KindKit,
		"Waterskin(2)":      KindKit,
		"Knife":             KindKit,
		"Bronze sword":      KindKit, // the Cows rung's blade
		"Raw shark":         KindKit, // a cook round's fish
		"Yew shortbow":      KindKit, // an alchable on the keep list
		"Clue scroll":       KindKit, // consolidateLootClues off
		"Uncut sapphire":    KindLoot,
		"Uncut diamond":     KindLoot,
		"Ruby":              KindLoot,
		"Mithril bolts":     KindLoot,
		"Rune arrow":        KindLoot,
		"Casket":            KindLoot,
		"Amulet of power":   KindLoot,
		"Herb":              KindLoot,
		"Grimy ranarr":      KindLoot,
		"Coal":              KindLoot,
		"Iron ore":          KindLoot,
		"Steel bar":         KindLoot, // no quest pending
		"Logs":              KindLoot,
		"Cowhide":           KindLoot,
		"Big bones":         KindLoot, // no Prayer goal
		"Nature talisman":   KindLoot,
		"Limpwurt root":     KindLoot,
		"Rune essence":      KindLoot, // not a rune: no " rune" suffix
	}
	for name, want := range cases {
		if got, why := Classify(name, in); got != want {
			t.Errorf("%s: got %s (%s), want %s", name, got, why, want)
		}
	}
	in.QuestsPending = true
	for _, n := range []string{"Steel bar", "Logs", "Bronze arrow", "Garlic", "Unfired bowl", "Black bead"} {
		if got, _ := Classify(n, in); got != KindKit {
			t.Errorf("%s with the quest ladder running: got %s, want kit", n, got)
		}
	}
	in.LootClues = true
	if got, _ := Classify("Clue scroll", in); got != KindLoot {
		t.Errorf("clue scroll with consolidateLootClues: got %s", got)
	}
}

func TestPlanLootKeepsOneUpgradePerSlotAndTheBonesThePrayerGoalNeeds(t *testing.T) {
	in := stage5Kit()
	in.PrayerGoal = 31
	// 150 XP short of 31 is 10 Big bones.
	in.PrayerXP = levels.XPAtLevel(31) - 150
	rows := []BankRow{
		{Name: "Big bones", ID: 532, Count: 25, Value: 1},
		{Name: "Bones", ID: 526, Count: 40, Value: 1},
		{Name: "Rune scimitar", ID: 1333, Count: 2, Value: 25600},   // Attack 40: an upgrade by the goal
		{Name: "Mithril scimitar", ID: 1329, Count: 1, Value: 1040}, // below the worn adamant: loot
		{Name: "Rune platebody", ID: 1127, Count: 1, Value: 65000},  // Dragon Slayer: never wearable here
		{Name: "Uncut ruby", ID: 1619, Count: 6, Value: 100},
		{Name: "Staff of air", ID: 1381, Count: 1, Value: 1500},
		{Name: "Quest thing", ID: 9999, Count: 1, Value: 1, Untradeable: true},
	}
	lines, kept := PlanLoot(rows, in)
	got := map[string]int{}
	for _, l := range lines {
		got[l.Name] = l.Count
	}
	if got["Big bones"] != 15 || kept["Big bones"] == "" {
		t.Errorf("10 Big bones cover the 150 XP: want 15 handed over, got %d (%q)", got["Big bones"], kept["Big bones"])
	}
	if got["Bones"] != 40 {
		t.Errorf("the Big bones cover the goal, so every Bones is loot: got %d", got["Bones"])
	}
	if got["Rune scimitar"] != 1 || kept["Rune scimitar"] != "a gear upgrade" {
		t.Errorf("one Rune scimitar is kept as the upgrade, the second is loot: got %d (%q)", got["Rune scimitar"], kept["Rune scimitar"])
	}
	if got["Mithril scimitar"] != 1 || got["Rune platebody"] != 1 || got["Uncut ruby"] != 6 {
		t.Errorf("non-upgrades and gems are loot: %v", got)
	}
	if _, ok := got["Staff of air"]; ok || kept["Staff of air"] == "" {
		t.Error("a staff never crosses")
	}
	if _, ok := got["Quest thing"]; ok || kept["Quest thing"] != "untradeable" {
		t.Error("an untradeable never crosses")
	}
	if lines[0].Name != "Rune platebody" {
		t.Errorf("lines go dearest first: %v", lines)
	}
}

func TestPlanHandOffSurplusAndLootTogether(t *testing.T) {
	s := Defaults()
	in := HandOffInput{
		Holdings: Holdings{Coins: 60_000, Runes: map[string]int{"Death rune": 30, "Chaos rune": 1200}, Food: map[string]int{"Shark": 40}},
		Keep:     Keep{Coins: 50_000, Runes: map[string]int{"Chaos rune": 1000}, Food: 16},
		Bank: []BankRow{
			{Name: "Uncut sapphire", ID: 1623, Count: 9, Value: 25},
			{Name: "Rune arrow", ID: 892, Count: 100, Value: 400},
			{Name: "Herb", ID: 207, Count: 3, Value: 1},
		},
		Kit:       stage5Kit(),
		MuleKinds: map[string]bool{"uncut sapphire": true, "rune arrow": true, "herb": true},
		MuleKnown: true,
	}
	h := PlanHandOff(s, in)
	if !h.Walk || h.Why != "food over the keep" {
		t.Fatalf("24 sharks over the keep justify the trip: %+v", h)
	}
	if h.Deposit.Coins != 10_000 || h.Deposit.Runes["Chaos rune"] != 200 || h.Deposit.Runes["Death rune"] != 30 || h.Deposit.Food["Shark"] != 24 {
		t.Fatalf("surplus: %+v", h.Deposit)
	}
	if len(h.Loot) != 3 || h.LootValue != 9*25+100*400+3 {
		t.Fatalf("loot: %+v value %d", h.Loot, h.LootValue)
	}
	lines := h.Lines()
	if lines[0].Kind != KindCoins || lines[len(lines)-1].Kind != KindLoot {
		t.Fatalf("coins first, loot last: %+v", lines)
	}
	if LoadsFor(lines, 25) != 1 || LoadsFor(make([]Line, 60), 25) != 3 {
		t.Fatal("loads")
	}
}

func TestPlanHandOffLootAloneJustifiesTheTrip(t *testing.T) {
	s := Defaults()
	in := HandOffInput{Kit: stage5Kit(), MuleKnown: true, MuleKinds: map[string]bool{"rune arrow": true}}
	in.Bank = []BankRow{{Name: "Rune arrow", ID: 892, Count: 100, Value: 400}}
	if h := PlanHandOff(s, in); !h.Walk || h.Why != "loot worth the trip" {
		t.Fatalf("40,000 of arrows: %+v", h)
	}
	in.Bank = []BankRow{{Name: "Rune arrow", ID: 892, Count: 1, Value: 400}}
	if h := PlanHandOff(s, in); h.Walk {
		t.Fatalf("one arrow is not worth the trip: %+v", h)
	}
	// Fifteen cheap rows free bank space, so they go.
	in.Bank = nil
	for i := 0; i < 15; i++ {
		name := "Junk " + string(rune('a'+i))
		in.Bank = append(in.Bank, BankRow{Name: name, ID: 5000 + i, Count: 1, Value: 1})
		in.MuleKinds[strings.ToLower(name)] = true
	}
	if h := PlanHandOff(s, in); !h.Walk || h.Why != "enough loot rows to free bank space" {
		t.Fatalf("15 rows: %+v", h)
	}
	s.Loot = false
	if h := PlanHandOff(s, in); h.Walk || len(h.Loot) != 0 {
		t.Fatalf("consolidateLoot off moves no loot: %+v", h)
	}
}

func TestPlanHandOffRespectsTheMulesBankRoom(t *testing.T) {
	s := Defaults()
	kinds := map[string]bool{"uncut ruby": true}
	for i := len(kinds); i < MuleBankSlots-s.MuleBankReserve-1; i++ {
		kinds[strings.Repeat("x", i+1)] = true
	}
	in := HandOffInput{
		Kit: stage5Kit(), MuleKinds: kinds, MuleKnown: true,
		Bank: []BankRow{
			{Name: "Uncut ruby", ID: 1619, Count: 200, Value: 100},
			{Name: "Rune arrow", ID: 892, Count: 100, Value: 400},
			{Name: "Casket", ID: 405, Count: 1, Value: 50},
		},
	}
	// One new type fits (239 of 240 usable rows used): the dearer new type goes, the other waits.
	h := PlanHandOff(s, in)
	got := map[string]bool{}
	for _, l := range h.Loot {
		got[l.Name] = true
	}
	if !got["Uncut ruby"] || !got["Rune arrow"] || got["Casket"] || len(h.Held) != 1 || h.Held[0] != "Casket" {
		t.Fatalf("room for one new type: loot %v held %v", h.Loot, h.Held)
	}
	in.MuleKnown, in.MuleKinds = false, nil
	if h := PlanHandOff(s, in); len(h.Loot) != 0 || len(h.Held) != 3 {
		t.Fatalf("an unpublished mule bank takes no loot: %+v", h)
	}
	if MuleRoom(s, 300, true) != 0 || MuleRoom(s, 0, true) != MuleBankSlots-s.MuleBankReserve {
		t.Fatal("MuleRoom")
	}
}

func TestKeepAddsTheTravelKitsRunes(t *testing.T) {
	k := KeepFor(Defaults(), KeepInput{ExtraRunes: map[string]int{"Law rune": 20, "Nature rune": 200}})
	p := PlanDeposit(Defaults(), Holdings{Runes: map[string]int{"Law rune": 25, "Nature rune": 150}}, k, 0)
	if p.Runes["Law rune"] != 5 || p.Runes["Nature rune"] != 0 {
		t.Fatalf("the travel kits stay: %+v", p.Runes)
	}
}
