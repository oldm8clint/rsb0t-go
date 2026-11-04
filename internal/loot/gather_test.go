package loot

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
)

// TestGatheringGroupsBySkillNotByName is the owner's "one card per gathering skill".
//
// Every tree species has its own name, so grouping on the name would give a card per species.
// Grouping on the skill puts them on one Woodcutting card while each species keeps its own row
// underneath — the per-source detail the panel shows in the drop grid.
func TestGatheringGroupsBySkillNotByName(t *testing.T) {
	tree := Woodcutting(nil, 1276, "Tree")
	oak := Woodcutting(nil, 1281, "Oak")
	willow := Woodcutting(nil, 1308, "Willow")
	rock := Mining(nil, 2491, "Rocks")
	spotA := Fishing(nil, 316)
	spotB := Fishing(nil, 313)

	if tree.GroupKey() != oak.GroupKey() || tree.GroupKey() != willow.GroupKey() {
		t.Fatalf("tree species should share one card: %q %q %q", tree.GroupKey(), oak.GroupKey(), willow.GroupKey())
	}
	if spotA.GroupKey() != spotB.GroupKey() {
		t.Fatalf("fishing spots should share one card: %q %q", spotA.GroupKey(), spotB.GroupKey())
	}
	// ...but the three skills must NOT collapse into each other.
	for _, pair := range [][2]Source{{tree, rock}, {tree, spotA}, {rock, spotA}} {
		if pair[0].GroupKey() == pair[1].GroupKey() {
			t.Fatalf("%s and %s share a card; each skill gets its own", pair[0].Skill, pair[1].Skill)
		}
	}
	// Each source still has its own identity underneath, so nothing about which tree is lost.
	if tree.Key() == oak.Key() || spotA.Key() == spotB.Key() {
		t.Fatal("per-source keys must stay distinct so the breakdown survives the grouping")
	}
}

// TestGatheringSkillsAreRealSkills guards the judgement call in gather.go against drift: every
// skill this package claims to gather must actually exist in the game's own table, spelled the
// same way, so a card can be titled from it.
func TestGatheringSkillsAreRealSkills(t *testing.T) {
	known := map[string]bool{}
	for _, n := range gamestate.SkillNames {
		if n != "" {
			known[n] = true
		}
	}
	for _, skill := range []string{SkillFishing, SkillWoodcutting, SkillMining} {
		if !known[skill] {
			t.Errorf("%q is not a skill in gamestate.SkillNames", skill)
		}
	}
	// And the ones deliberately left out are real skills too — that is why leaving them out was
	// a judgement rather than an oversight (see gather.go).
	for _, skill := range []string{"Thieving", "Cooking", "Smithing", "Runecraft"} {
		if !known[skill] {
			t.Errorf("%q was expected to exist; gather.go's reasoning cites it", skill)
		}
	}
}

// TestGatheringSourcesCarryTheRightModelKind: the panel picks a model from Model, and a tree or
// rock is a scenery loc while a fishing spot is an npc.
func TestGatheringSourcesCarryTheRightModelKind(t *testing.T) {
	if got := Fishing(nil, 316); got.Model != ModelNPC || got.Kind != KindGathering || got.Action != ActionFish {
		t.Fatalf("fishing source = %+v", got)
	}
	if got := Woodcutting(nil, 1276, "Tree"); got.Model != ModelLoc || got.Action != ActionChop {
		t.Fatalf("woodcutting source = %+v", got)
	}
	if got := Mining(nil, 2491, "Rocks"); got.Model != ModelLoc || got.Action != ActionMine {
		t.Fatalf("mining source = %+v", got)
	}
	// A nil config falls back to the name the caller supplied rather than an empty card title.
	if got := Woodcutting(nil, 1276, "Tree"); !strings.EqualFold(got.Name, "Tree") {
		t.Fatalf("fallback name = %q", got.Name)
	}
}

// TestMigrateFoldsTheRetiredFishingKindOntoTheGatheringCard.
//
// Fishing sources carried Kind "fishing" until all three gathering skills became one kind, and
// that change migrated nothing — so on the live panel every catch taken before it stayed on a
// separate card forever (owner, 2026-09-20: the Loot panel showed a "Fishing" card beside a
// "Fishing spot" card). What matters about the migration is BOTH halves: the card must change
// and the identity must not, because a record is persisted under Source.Key and a key that
// moved would strand or overwrite a total.
func TestMigrateFoldsTheRetiredFishingKindOntoTheGatheringCard(t *testing.T) {
	// Exactly what a pre-change build wrote: the retired kind, no skill, no model.
	legacy := Source{Kind: "fishing", Action: "fish", Name: FishingSpotName, ID: 313}
	current := Fishing(nil, 320)

	got := Migrate(legacy)
	if got.Kind != KindGathering || got.Skill != SkillFishing || got.Model != ModelNPC || got.Action != ActionFish {
		t.Fatalf("migrated legacy source = %+v; want the gathering/Fishing shape", got)
	}
	if got.Key() != legacy.Key() {
		t.Fatalf("migration moved the persisted key: %q -> %q", legacy.Key(), got.Key())
	}
	if got.GroupKey() != current.GroupKey() {
		t.Fatalf("migrated group key %q does not match a current spot's %q — still two cards",
			got.GroupKey(), current.GroupKey())
	}
	if legacy.GroupKey() == current.GroupKey() {
		t.Fatal("the legacy kind already grouped with the current one; this test proves nothing")
	}

	// Every other source is returned untouched, including one that is already current.
	for _, src := range []Source{current, Woodcutting(nil, 1276, "Tree"), Mining(nil, 450, "Rocks"),
		Openable("Casket", 405), NPC(nil, ActionKill, 100, "Goblin")} {
		if Migrate(src) != src {
			t.Fatalf("Migrate changed an already-current source: %+v -> %+v", src, Migrate(src))
		}
	}
}

// TestAnOysterOpenedIsTheOysterPearlsRowOfTheFishingCard: the owner's "merge it into the fishing tab"
// and "the closed oyster shouldnt show anywhere on the loot panel just oyster pearls" (7 Oct 2026),
// including for records written when oysters were an openable of their own or a row named Oyster.
func TestAnOysterOpenedIsTheOysterPearlsRowOfTheFishingCard(t *testing.T) {
	oyster := Openable("Oyster", 407)
	fishing := Fishing(nil, 324)
	if oyster.GroupKey() != fishing.GroupKey() || oyster.PartKey() == fishing.PartKey() {
		t.Fatalf("oyster card %q row %q vs fishing card %q row %q: want one card, two rows", oyster.GroupKey(), oyster.PartKey(), fishing.GroupKey(), fishing.PartKey())
	}
	if oyster.Name != OysterPearlsName || oyster.ID != OysterPearlID || !IsOysterPearlsRow(oyster) {
		t.Fatalf("oyster row = %+v, want it named and drawn as Oyster pearls", oyster)
	}
	if c, ok := oyster.Consumes(); ok {
		t.Fatalf("opening an oyster consumes %+v: the closed oyster is never loot, so it costs nothing", c)
	}
	if !HiddenItem(407) || !HiddenItem(409) || HiddenItem(411) || HiddenItem(413) {
		t.Fatal("hidden items: want the Oyster and the Empty oyster, never the pearls")
	}
	tally := NewTally(time.Now)
	tally.Add(Drop{Source: fishing, Events: 3, Items: []Item{{ID: 407, Name: "Oyster", Count: 2}, {ID: 401, Name: "Seaweed", Count: 1}}})
	tally.Add(Drop{Source: oyster, Events: 2, Items: []Item{{ID: 411, Name: "Oyster pearl", Count: 1}, {ID: 409, Name: "Empty oyster", Count: 1}}})
	for _, tot := range tally.Snapshot() {
		for _, it := range tot.Items {
			if HiddenItem(it.ID) {
				t.Fatalf("%s kept hidden item %+v", tot.Source.Name, it)
			}
		}
	}
	// A casket is still an openable of its own, with nothing netted.
	casket := Openable("Casket", 405)
	if casket.Kind != KindOpenable || casket.GroupKey() == fishing.GroupKey() {
		t.Fatalf("casket = %+v", casket)
	}
	if _, ok := casket.Consumes(); ok {
		t.Fatal("a casket now costs something")
	}
	// What an older build wrote comes back onto the fishing card with its key unchanged.
	old := Source{Kind: KindOpenable, Action: ActionOpen, Name: "Oyster", ID: 407}
	got := Migrate(old)
	if got.Kind != KindGathering || got.Skill != SkillFishing || got.PartKey() != oyster.PartKey() || got.GroupKey() != fishing.GroupKey() {
		t.Fatalf("migrated = %+v", got)
	}
	// A row written as "Oyster" on the Fishing card comes back as the Oyster pearls row too.
	if got := Migrate(Source{Kind: KindGathering, Skill: SkillFishing, Action: ActionOpen, Name: "Oyster", ID: 407, Model: ModelObj}); got.PartKey() != oyster.PartKey() {
		t.Fatalf("an Oyster row migrated to %+v", got)
	}
	if again := Migrate(got); again != got {
		t.Fatalf("migrate is not idempotent: %+v then %+v", got, again)
	}
}

// TestBarbarianWomanKillsAreBarbarianKills: the owner's "barbarian and barbarian woman should be
// combined as they have the same loot table" (7 Oct 2026), for new kills and for records read back.
func TestBarbarianWomanKillsAreBarbarianKills(t *testing.T) {
	woman := NPC(nil, ActionKill, 3264, "Barbarian woman")
	man := NPC(nil, ActionKill, 3262, "Barbarian")
	if woman.GroupKey() != man.GroupKey() || woman.PartKey() != man.PartKey() {
		t.Fatalf("woman %q/%q vs man %q/%q: want one card, one row", woman.GroupKey(), woman.PartKey(), man.GroupKey(), man.PartKey())
	}
	old := Migrate(Source{Kind: KindNPC, Action: ActionKill, Name: "Barbarian woman", ID: 3264})
	if old.GroupKey() != man.GroupKey() {
		t.Fatalf("migrated %+v", old)
	}
	// A pickpocket is not a kill: its card is the Thieving card, untouched.
	if pp := NPC(nil, ActionPickpocket, 3264, "Barbarian woman"); pp.Name != "Barbarian woman" {
		t.Fatalf("pickpocket renamed: %+v", pp)
	}
}
