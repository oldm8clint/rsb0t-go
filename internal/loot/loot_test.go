package loot

import (
	"reflect"
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
)

func TestPackCountsSumsStacksAndSlots(t *testing.T) {
	inv := &gamestate.Inventory{Slots: []gamestate.Item{{ID: 995, Count: 20}, {}, {ID: 405, Count: 1}, {ID: 405, Count: 1}}}
	got := PackCounts(inv)
	want := Counts{995: 20, 405: 2}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PackCounts = %v, want %v", got, want)
	}
	if n := len(PackCounts(nil)); n != 0 {
		t.Fatalf("nil inventory read %d ids", n)
	}
}

func TestGainedKeepsOnlyArrivalsAndSkipsTheSource(t *testing.T) {
	const casket, coins, sapphire, food = 405, 995, 1623, 379
	before := Counts{casket: 3, coins: 100, food: 5}
	after := Counts{casket: 0, coins: 180, sapphire: 1, food: 4}
	got := Gained(before, after, func(id int) bool { return id == casket })
	want := Counts{coins: 80, sapphire: 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Gained = %v, want %v (losses and the opened item itself must not count)", got, want)
	}
	if n := len(Gained(after, after, nil)); n != 0 {
		t.Fatalf("an unchanged pack gained %d ids", n)
	}
}

func TestTallyAccumulatesPerSource(t *testing.T) {
	clock := time.UnixMilli(1_000)
	tally := NewTally(func() time.Time { return clock })
	casket := Openable("Casket", 405)
	man := Source{Kind: KindNPC, Action: ActionPickpocket, Name: "Man", ID: 3, Level: 2}
	tally.Add(Drop{Source: casket, Events: 2, Items: []Item{{ID: 995, Name: "Coins", Count: 40}, {ID: 1623, Name: "Uncut sapphire", Count: 1, Value: 25}}})
	epoch := tally.Epoch()
	tally.Add(Drop{}) // nothing: ignored, epoch unchanged
	if tally.Epoch() != epoch {
		t.Fatal("an empty drop changed the epoch")
	}
	clock = time.UnixMilli(5_000)
	tally.Add(Drop{Source: casket, Events: 1, Items: []Item{{ID: 995, Name: "Coins", Count: 320}}})
	tally.Add(Drop{Source: Source{Kind: KindNPC, Action: ActionPickpocket, Name: "man", ID: 3}, Events: 1, Items: []Item{{ID: 995, Name: "Coins", Count: 3}}})
	tally.Add(Drop{Source: man, Events: 1, Items: []Item{{ID: 995, Name: "Coins", Count: 3}}})

	snap := tally.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("got %d sources, want 2 (\"Man\" and \"man\" are one): %+v", len(snap), snap)
	}
	byKey := map[string]Totals{}
	for _, s := range snap {
		byKey[s.Source.Key()] = s
	}
	c := byKey[casket.Key()]
	if c.Events != 3 || c.FirstUnixMs != 1_000 || c.LastUnixMs != 5_000 {
		t.Fatalf("casket totals %+v", c)
	}
	if want := []Item{{ID: 995, Name: "Coins", Count: 360}, {ID: 1623, Name: "Uncut sapphire", Count: 1, Value: 25}}; !reflect.DeepEqual(c.Items, want) {
		t.Fatalf("casket items %+v, want %+v", c.Items, want)
	}
	m := byKey[man.Key()]
	if m.Events != 2 || m.Source.Level != 2 || len(m.Items) != 1 || m.Items[0].Count != 6 {
		t.Fatalf("man totals %+v", m)
	}
	// A snapshot is a copy: changing it must not reach the tally.
	c.Items[0].Count = 0
	if tally.Snapshot()[0].Items[0].Count == 0 {
		t.Fatal("Snapshot shares its item slice with the tally")
	}
}

// TestTallyCarriesExperienceBySkill: an npc's kills pay several skills, which must survive the tally and
// add up per skill.
func TestTallyCarriesExperienceBySkill(t *testing.T) {
	tally := NewTally(func() time.Time { return time.Unix(100, 0) })
	cow := NPC(nil, ActionKill, 81, "Cow")
	tally.Add(Drop{Source: cow, Events: 1, Skills: []Item{SkillXP(2, 12), SkillXP(3, 4)}})
	tally.Add(Drop{Source: cow, Events: 1, Skills: []Item{SkillXP(2, 8)}})
	got := tally.Snapshot()
	if len(got) != 1 || len(got[0].Skills) != 2 || got[0].Skills[0].Name != "Strength" || got[0].Skills[0].Count != 20 || got[0].Skills[1].Name != "Hitpoints" || got[0].Skills[1].Count != 4 {
		t.Fatalf("snapshot = %+v", got)
	}
	// Experience alone is a drop worth keeping.
	other := NewTally(nil)
	other.Add(Drop{Source: cow, Skills: []Item{SkillXP(4, 3)}})
	if len(other.Snapshot()) != 1 {
		t.Fatal("a drop of only experience was ignored")
	}
}

func TestClaimsExperienceNamesTheSkillCards(t *testing.T) {
	for src, want := range map[string]int{
		"woodcutting": 8, "fishing": 10, "fletching": 9, "thieving": 17, "crafting": 12, "magic": 6,
	} {
		var s Source
		switch src {
		case "woodcutting":
			s = Woodcutting(nil, 1, "Tree")
		case "fishing":
			s = Fishing(nil, 316)
		case "fletching":
			s = Fletching(nil, 1511, "Logs")
		case "thieving":
			s = NPC(nil, ActionPickpocket, 1, "Man")
		case "crafting":
			s = Crafting(nil, 1779, "Flax")
		case "magic":
			s = Alchemy(1, "Bow", true, true)
		}
		if stat, ok := ClaimsExperience(s); !ok || stat != want {
			t.Errorf("%s claims %d (%v), want %d", src, stat, ok, want)
		}
	}
	for _, s := range []Source{Bury(nil, 526, ""), CombatSpells(), Teleport("Varrock Teleport"), Consumable(nil, 385, ActionEat, "Shark"), NPC(nil, ActionKill, 81, "Cow"), Openable("Casket", 405)} {
		if _, ok := ClaimsExperience(s); ok {
			t.Errorf("%s (%s) claims experience", s.Name, s.Kind)
		}
	}
}
