package loot

import (
	"testing"
	"time"
)

// collect is a sink that keeps every Drop it is given.
func collect(into *[]Drop) func(Drop) { return func(d Drop) { *into = append(*into, d) } }

// One verified craft is ONE event carrying the runes the pack gained, the experience the stat gained and the
// essence it used up, on the altar's own row.
func TestRuneCraftedIsOneEventWithTheRunesTheEssenceAndTheExperience(t *testing.T) {
	var got []Drop
	// 27 essence bound at Runecraft 35 gave 54 runes: the count is whatever the pack measured, so it is passed in and
	// never recomputed here.
	RuneCrafted(collect(&got), nil, "Body", 559, "Body rune", 27, 54, 202, 35)
	if len(got) != 1 {
		t.Fatalf("got %d drops, want 1", len(got))
	}
	d := got[0]
	if d.Events != 1 || d.XP != 202 {
		t.Errorf("events %d xp %d, want 1 and 202", d.Events, d.XP)
	}
	if len(d.Items) != 1 || d.Items[0].ID != 559 || d.Items[0].Count != 54 || d.Items[0].Name != "Body rune" {
		t.Errorf("items %+v, want 54 Body rune (559)", d.Items)
	}
	if len(d.Spent) != 1 || d.Spent[0].ID != ObjRuneEssence || d.Spent[0].Count != 27 || d.Spent[0].Name != RuneEssenceName {
		t.Errorf("spent %+v, want 27 Rune essence (1436)", d.Spent)
	}
	s := d.Source
	if s.Kind != KindProduction || s.Skill != SkillRunecraft || s.Action != ActionRunecraft || s.Name != "Body altar" || s.ID != 559 || s.Model != ModelObj || s.Level != 35 {
		t.Errorf("source %+v", s)
	}
}

// The panel nets a card by what its source Consumes. A craft's essence is measured (Drop.Spent), and the row is
// named for an altar, which is no item: if Consumes answered, the card would price "Air altar" per event.
func TestARunecraftRowConsumesNothingPerEvent(t *testing.T) {
	s := Runecrafting("Air", 556)
	if c, ok := s.Consumes(); ok {
		t.Errorf("Consumes = %+v, want none: the essence is Drop.Spent", c)
	}
	if ins := s.Inputs(); len(ins) != 0 {
		t.Errorf("Inputs = %+v, want none", ins)
	}
}

// Every altar is a row of the ONE Runecraft card, apart from every other production card and from the supply card.
func TestEveryAltarIsARowOfOneRunecraftCard(t *testing.T) {
	air, body, nature := Runecrafting("Air", 556), Runecrafting("Body", 559), Runecrafting("Nature", 561)
	if air.GroupKey() != body.GroupKey() || body.GroupKey() != nature.GroupKey() {
		t.Errorf("the altars are not one card: %q %q %q", air.GroupKey(), body.GroupKey(), nature.GroupKey())
	}
	if air.PartKey() == body.PartKey() || air.Key() == body.Key() {
		t.Error("two altars are one row")
	}
	for _, other := range []Source{Fletching(nil, 1511, "Logs"), Crafting(nil, 1623, "Uncut sapphire"), Bury(nil, 526, "Bones"), SuperheatCast(2353, "Iron ore", true), EssenceHandover()} {
		if other.GroupKey() == air.GroupKey() {
			t.Errorf("%s shares the Runecraft card", other.Name)
		}
	}
	if !IsRunecraft(air) || IsRunecraft(Fletching(nil, 1511, "Logs")) || IsRunecraft(EssenceHandover()) {
		t.Error("IsRunecraft")
	}
}

// The level is a latest reading: it rides Source.Level, which a Tally keeps as the last non-zero one, and it does not
// make a second record (Key ignores it).
func TestTheRunecraftLevelIsTheLatestReadingOnTheSameRecord(t *testing.T) {
	var got []Drop
	sink := collect(&got)
	RuneCrafted(sink, nil, "Air", 556, "Air rune", 27, 54, 135, 19)
	RuneCrafted(sink, nil, "Air", 556, "Air rune", 27, 54, 135, 20)
	RuneCrafted(sink, nil, "Air", 556, "Air rune", 27, 54, 135, 0) // a reading that failed leaves the level where it was
	tally := NewTally(func() time.Time { return time.Unix(1_700_000_000, 0) })
	for _, d := range got {
		tally.Add(d)
	}
	snap := tally.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("got %d records, want 1: %+v", len(snap), snap)
	}
	r := snap[0]
	if r.Events != 3 || r.XP != 405 || r.Source.Level != 20 {
		t.Errorf("events %d xp %d level %d, want 3, 405 and 20", r.Events, r.XP, r.Source.Level)
	}
	if len(r.Items) != 1 || r.Items[0].Count != 162 || len(r.Spent) != 1 || r.Spent[0].Count != 81 {
		t.Errorf("items %+v spent %+v, want 162 runes from 81 essence", r.Items, r.Spent)
	}
}

// Nothing is reported for what was not a craft: no sink, no essence, nothing that came of it.
func TestRuneCraftedReportsNothingForAnythingButACraft(t *testing.T) {
	var got []Drop
	RuneCrafted(nil, nil, "Air", 556, "Air rune", 27, 54, 135, 1)
	RuneCrafted(collect(&got), nil, "Air", 556, "Air rune", 0, 54, 135, 1)
	RuneCrafted(collect(&got), nil, "Air", 556, "Air rune", 27, 0, 0, 1)
	if len(got) != 0 {
		t.Errorf("reported %+v", got)
	}
	// An experience reading below zero is clamped, not recorded.
	RuneCrafted(collect(&got), nil, "Air", 556, "Air rune", 27, 54, -5, 1)
	if len(got) != 1 || got[0].XP != 0 || len(got[0].Items) != 1 {
		t.Errorf("a negative experience: %+v", got)
	}
}

// The essence went and the experience came but the pack did not show the runes (the inventory update read late): the
// spend and the experience are real and recorded, the runes are not guessed.
func TestACraftWhoseRunesWereNotSeenStillRecordsTheSpendAndTheExperience(t *testing.T) {
	var got []Drop
	RuneCrafted(collect(&got), nil, "Air", 556, "Air rune", 27, 0, 135, 7)
	if len(got) != 1 {
		t.Fatalf("got %d drops", len(got))
	}
	d := got[0]
	if len(d.Items) != 0 || d.XP != 135 || len(d.Spent) != 1 || d.Spent[0].Count != 27 || d.Events != 1 {
		t.Errorf("drop %+v", d)
	}
}

// A blank altar name still names a row.
func TestARunecraftRowIsNeverUnnamed(t *testing.T) {
	if n := Runecrafting("  ", 556).Name; n != "Rune altar" {
		t.Errorf("name %q", n)
	}
}

// A hand-over MOVES essence that was counted when it was mined: it is an event count and nothing else, so it adds no
// item (and so no item total, no value and no ticker line) to the panel.
func TestAHandoverIsTheEssenceCountAndNoItems(t *testing.T) {
	var got []Drop
	EssenceHandedOver(collect(&got), 2021)
	EssenceHandedOver(collect(&got), 0)
	EssenceHandedOver(nil, 5)
	if len(got) != 1 {
		t.Fatalf("got %d drops, want 1", len(got))
	}
	d := got[0]
	if d.Events != 2021 || len(d.Items) != 0 || len(d.Spent) != 0 || d.XP != 0 {
		t.Errorf("drop %+v, want 2,021 events and nothing else", d)
	}
	s := d.Source
	if s.Kind != KindSupply || s.Skill != SkillEssenceSupply || s.Action != ActionHandover || s.ID != ObjRuneEssence || s.Model != ModelObj || !IsEssenceSupply(s) {
		t.Errorf("source %+v", s)
	}
	if _, ok := s.Consumes(); ok {
		t.Error("a hand-over consumes something")
	}
	tally := NewTally(nil)
	tally.Add(d)
	if snap := tally.Snapshot(); len(snap) != 1 || snap[0].Events != 2021 || len(snap[0].Items) != 0 {
		t.Errorf("tallied %+v", snap)
	}
}

// Runecraft is a game skill, so the experience watch would attach its pending experience to a craft that reported none;
// a craft reports its own, so what it carries is kept (internal/bot's xpWatch.attach only fills a zero).
func TestARunecraftSourceClaimsRunecraftExperienceButTheSupplyCardDoesNot(t *testing.T) {
	if stat, ok := ClaimsExperience(Runecrafting("Air", 556)); !ok || stat != 20 {
		t.Errorf("ClaimsExperience = %d, %v, want Runecraft's stat 20", stat, ok)
	}
	if _, ok := ClaimsExperience(EssenceHandover()); ok {
		t.Error("the supply card claims experience")
	}
	if !IsRuneEssence(1436) || !IsRuneEssence(1437) || IsRuneEssence(556) {
		t.Error("IsRuneEssence")
	}
}
