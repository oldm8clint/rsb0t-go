package controlpanel

import (
	"encoding/json"
	"testing"
	"time"

	"rsb0t-go/internal/loot"
)

// Experience on the Loot tab (a user request): a skill card carries the experience its own skill
// earned, an npc's card the experience its kills earned per combat skill, both persisted and diffed
// across readings like every other figure.
func TestExperienceReachesSkillCardsAndNpcKills(t *testing.T) {
	base := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator("", nil, fixedNow(base))
	tally := loot.NewTally(func() time.Time { return base })
	tally.Add(loot.Drop{Source: loot.Woodcutting(nil, 1276, "Tree"), Events: 5, XP: 187, Items: []loot.Item{{ID: 1511, Name: "Logs", Count: 5, Value: 4}}})
	tally.Add(loot.Drop{Source: loot.Woodcutting(nil, 1278, "Oak"), Events: 2, XP: 75, Items: []loot.Item{{ID: 1521, Name: "Oak logs", Count: 2, Value: 20}}})
	cow := loot.NPC(nil, loot.ActionKill, 81, "Cow")
	tally.Add(loot.Drop{Source: cow, Events: 1, Skills: []loot.Item{loot.SkillXP(2, 12), loot.SkillXP(3, 4)}})
	tally.Add(loot.Drop{Source: cow, Events: 1, Skills: []loot.Item{loot.SkillXP(2, 12)}})
	a.loot.record(lootReading("w1", 1, tally.Snapshot()...), base)
	// Another reading of the same cumulative tally adds nothing; a later one adds only the difference.
	a.loot.record(lootReading("w1", 2, tally.Snapshot()...), base)
	tally.Add(loot.Drop{Source: cow, Events: 1, Skills: []loot.Item{loot.SkillXP(2, 6)}})
	a.loot.record(lootReading("w1", 3, tally.Snapshot()...), base)

	lifetime, _, _, _ := a.loot.body()
	var cowView LootSourceView
	var woodXP int64
	for _, s := range lifetime.Sources {
		if s.Source.Name == "Cow" {
			cowView = s
		}
		if s.Source.Skill == loot.SkillWoodcutting {
			woodXP += s.XP
		}
	}
	by := map[string]int64{}
	for _, k := range cowView.Skills {
		by[k.Name] = k.Count
	}
	if cowView.Events != 3 || by["Strength"] != 30 || by["Hitpoints"] != 4 || len(by) != 2 {
		t.Fatalf("cow = %d kills, skills %v, want 3 kills, Strength 30 and Hitpoints 4", cowView.Events, by)
	}
	if woodXP != 262 {
		t.Fatalf("woodcutting xp = %d, want 262", woodXP)
	}
	if lifetime.XP["Woodcutting"] != 262 || lifetime.XP["Strength"] != 30 || lifetime.XP["Hitpoints"] != 4 {
		t.Fatalf("period xp = %v", lifetime.XP)
	}
	// It survives a restart: persisted, then restored, nothing is lost.
	saved := a.loot.persisted()
	raw, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var back lootFile
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	b := newStatsAggregator("", nil, fixedNow(base))
	b.loot.restore(&back)
	restored, _, _, _ := b.loot.body()
	if restored.XP["Strength"] != 30 || restored.XP["Woodcutting"] != 262 {
		t.Fatalf("restored xp = %v", restored.XP)
	}
}
