package loot

import (
	"strings"
	"testing"

	"rsb0t-go/internal/gamestate"
)

// TestFletchingIsItsOwnCardAndNotTheWoodcuttingOne is the owner's ask in one assertion
// (2026-09-21: "arrow shafts are showing up in the woodcutting section probably should have a
// fletching section for all fletching materials the fleet makes").
//
// Both halves matter. Every material the fleet cuts must land on ONE Fletching card, the way
// every tree lands on one Woodcutting card — and that card must not be the Woodcutting one, which
// is the bug. A test that only checked the first half would pass if Fletching were folded into
// KindGathering, which is exactly the shortcut production.go argues against.
func TestFletchingIsItsOwnCardAndNotTheWoodcuttingOne(t *testing.T) {
	logs := Fletching(nil, 1511, "Logs")
	oakLogs := Fletching(nil, 1521, "Oak logs")
	tree := Woodcutting(nil, 1276, "Tree")
	oakTree := Woodcutting(nil, 1281, "Oak")

	if logs.GroupKey() != oakLogs.GroupKey() {
		t.Fatalf("every fletched material belongs on one Fletching card: %q vs %q", logs.GroupKey(), oakLogs.GroupKey())
	}
	if tree.GroupKey() != oakTree.GroupKey() {
		t.Fatalf("the Woodcutting card regressed: %q vs %q", tree.GroupKey(), oakTree.GroupKey())
	}
	if logs.GroupKey() == tree.GroupKey() {
		t.Fatalf("fletching and woodcutting share a card (%q); the shafts are back in the woodcutting section", logs.GroupKey())
	}
	// A material named identically to a tree must still not merge, because the KIND is in the key.
	// "Oak" the tree and a hypothetical "Oak" material are different rows of the panel.
	sameName := Production(SkillWoodcutting, ActionFletch, 1521, "Oak")
	if sameName.GroupKey() == oakTree.GroupKey() {
		t.Fatalf("a production source merged with a gathering source of the same skill name: %q", sameName.GroupKey())
	}
	// Each material keeps its own identity underneath, so the per-source rows survive the grouping
	// and a persisted total cannot collide with another material's.
	if logs.Key() == oakLogs.Key() {
		t.Fatalf("per-source keys collapsed: %q", logs.Key())
	}
}

// TestFletchingSourceShape: the panel picks a card title from Skill, a verb from Action and a
// model from Model, so all three have to be what the page expects.
func TestFletchingSourceShape(t *testing.T) {
	got := Fletching(nil, 1511, "Logs")
	if got.Kind != KindProduction {
		t.Errorf("Kind = %q, want %q", got.Kind, KindProduction)
	}
	if got.Skill != SkillFletching {
		t.Errorf("Skill = %q, want %q", got.Skill, SkillFletching)
	}
	if got.Action != ActionFletch {
		t.Errorf("Action = %q, want %q", got.Action, ActionFletch)
	}
	// The material is an ITEM in the pack, not a tree or an npc, so the panel must draw an obj
	// model for it — and its ID has to be the obj id that model is looked up by.
	if got.Model != ModelObj {
		t.Errorf("Model = %q, want %q", got.Model, ModelObj)
	}
	if got.ID != 1511 {
		t.Errorf("ID = %d, want the material's obj id 1511", got.ID)
	}
	// A nil config falls back to the caller's name rather than leaving the row blank.
	if !strings.EqualFold(got.Name, "Logs") {
		t.Errorf("fallback name = %q, want Logs", got.Name)
	}
	// And Migrate leaves it alone: only the retired fishing kind is rewritten.
	if Migrate(got) != got {
		t.Errorf("Migrate changed a current production source: %+v", Migrate(got))
	}
}

// TestFletchingIsARealSkill guards the card's title against drift the same way
// TestGatheringSkillsAreRealSkills guards the gathering ones: a card titled from a string the
// game does not use would be a typo nobody notices.
func TestFletchingIsARealSkill(t *testing.T) {
	known := map[string]bool{}
	for _, n := range gamestate.SkillNames {
		if n != "" {
			known[n] = true
		}
	}
	if !known[SkillFletching] {
		t.Fatalf("%q is not a skill in gamestate.SkillNames", SkillFletching)
	}
	// gather.go's reasoning explicitly files Fletching on the production side of its line. If it
	// ever became a gathering skill too, both would claim it and the panel would show two cards.
	for _, s := range []string{SkillFishing, SkillWoodcutting, SkillMining} {
		if s == SkillFletching {
			t.Fatalf("%q is claimed by gather.go as well as production.go", s)
		}
	}
}

// TestIsFletchingProductMatchesByIdOrName. The skip filters inside a measurement window have the
// obj id and may have no cache config at all, while a caller holding a resolved obj has the name;
// either has to be enough, or the filter silently stops working on an account whose cache never
// loaded.
func TestIsFletchingProductMatchesByIdOrName(t *testing.T) {
	if !IsFletchingProduct(ObjArrowShaft, "") {
		t.Error("an arrow shaft's id alone should identify it")
	}
	if !IsFletchingProduct(-1, "Arrow shaft") || !IsFletchingProduct(-1, " arrow SHAFT ") {
		t.Error("the name alone should identify it, case- and space-insensitively")
	}
	// What woodcutting produced must NOT be swept up: the logs stay on the Woodcutting card.
	for id, name := range map[int]string{1511: "Logs", 1521: "Oak logs", 1519: "Willow logs"} {
		if IsFletchingProduct(id, name) {
			t.Errorf("%s (%d) is what woodcutting produced, not what fletching made", name, id)
		}
	}
	// So must a bow: the fleet BUYS bows from Lowe and does not fletch any, which is why this is
	// a named list and not "anything that sounds like archery" (see production.go).
	if IsFletchingProduct(841, "Shortbow") {
		t.Error("a bought shortbow is not a fletching product on this fleet")
	}
}

// ---- the unstrung bows (2026-09-22) ----
//
// Owner's report, verbatim: "fletching loot panel: unstrung bows cut from various logs arent
// showing in the fletching panel, some bows are showing in woodcutting panel". Read off the live
// panel the same day: `chop:oak:1281` carried "Oak shortbow x1" (obj 54) and "Oak longbow x4"
// (obj 56), `chop:willow:1308` carried obj 58 and obj 60 — four UNSTRUNG bow ids on gathering
// cards — while the only Fletching source on the whole panel was BankFletcher's arrow shafts.

// unstrungBows is the twelve ids content 289 gives the knife-on-logs bows, with the display name
// each one shares with its STRUNG counterpart. Read out of the content checkout
// (content/pack/obj.pack and scripts/skill_fletching/configs/stringing/bows.obj), not recalled.
var unstrungBows = []struct {
	unstrung int
	strung   int
	name     string
}{
	{48, 839, "Longbow"},
	{50, 841, "Shortbow"},
	{54, 843, "Oak shortbow"},
	{56, 845, "Oak longbow"},
	{58, 847, "Willow longbow"},
	{60, 849, "Willow shortbow"},
	{62, 851, "Maple longbow"},
	{64, 853, "Maple shortbow"},
	{66, 855, "Yew longbow"},
	{68, 857, "Yew shortbow"},
	{70, 859, "Magic longbow"},
	{72, 861, "Magic shortbow"},
}

// TestUnstrungBowsAreFletchingProducts is the owner's first complaint. Every unstrung bow must be
// recognised BY ID ALONE, because the callers that matter are skip filters inside a measurement
// window (woodcutrunner.notAChop, wcfletchrunner.notAFletch) which run before any cache lookup
// and may have no *cachecfg.Config at all.
func TestUnstrungBowsAreFletchingProducts(t *testing.T) {
	for _, b := range unstrungBows {
		if !IsFletchingProduct(b.unstrung, "") {
			t.Errorf("unstrung %s (obj %d): wanted a fletching product from the id alone, got false — "+
				"a skip filter with no cache config would let a tree claim it", b.name, b.unstrung)
		}
	}
}

// TestStrungBowsAreNotFletchingProducts is THE case that fails if anyone reverts this to name
// matching, and the reason every count in this area keys on the obj id.
//
// In content 289 the unstrung and the strung bow of a species share a display name with no "(u)"
// suffix anywhere — only `desc` differs — so the identical string is asked here with two ids and
// must get two different answers. Nothing in this fleet strings a bow; a STRUNG one is always
// something a combat script bought from Lowe, and putting it on the Fletching card (or skipping
// it off a card where it was genuinely dropped) would be a new lie in place of the old one.
func TestStrungBowsAreNotFletchingProducts(t *testing.T) {
	for _, b := range unstrungBows {
		if !IsFletchingProduct(b.unstrung, b.name) {
			t.Errorf("unstrung %s (obj %d) should be a fletching product", b.name, b.unstrung)
		}
		if IsFletchingProduct(b.strung, b.name) {
			t.Errorf("strung %s (obj %d) was called a fletching product from the name %q that the "+
				"unstrung obj %d shares with it — this is name matching, which cannot tell a bow the "+
				"fleet cut from one it bought at Lowe's", b.name, b.strung, b.name, b.unstrung)
		}
	}
	// The same trap stated the other way: the name on its own must never be enough for a bow.
	for _, b := range unstrungBows {
		if IsFletchingProduct(-1, b.name) {
			t.Errorf("the name %q alone identified a fletching product; it cannot, because obj %d "+
				"(unstrung) and obj %d (strung) both carry it", b.name, b.unstrung, b.strung)
		}
	}
}

// TestCutLogsProductsAreExactlyTheThirteen pins the derived set against content.
//
// production.go derives it from internal/bankxp's generated table rather than panicking on drift,
// because this package is linked into the live control panel and a panel that will not start is
// worse than a card that under-reports. That makes this test the only thing standing between a
// regenerated bankxp table and the bug coming back silently, so it names every id rather than
// counting them.
func TestCutLogsProductsAreExactlyTheThirteen(t *testing.T) {
	want := map[int]bool{ObjArrowShaft: true}
	for _, b := range unstrungBows {
		want[b.unstrung] = true
	}
	for id := range want {
		if !fletchingProductIDs[id] {
			t.Errorf("obj %d is a knife-on-logs product in content 289 and is missing from "+
				"fletchingProductIDs — regenerate internal/bankxp/table.go", id)
		}
	}
	for id := range fletchingProductIDs {
		if !want[id] {
			t.Errorf("obj %d is in fletchingProductIDs but is not one of the thirteen knife-on-logs "+
				"products; a strung bow, an arrow or a bolt in here would put a BOUGHT item on the "+
				"Fletching card and skip a genuine drop off an npc card", id)
		}
	}
}

// TestFletchingProductNamesHoldOnlyTheArrowShaft guards the derivation's one hand-written half.
//
// A future session adding a product will reasonably add its name beside its id. That is correct
// for anything with a unique name and WRONG for a bow, so the rule is asserted rather than left
// in a comment: the name lookup holds exactly one entry.
func TestFletchingProductNamesHoldOnlyTheArrowShaft(t *testing.T) {
	if len(fletchingProductNames) != 1 || !fletchingProductNames[strings.ToLower(ArrowShaft)] {
		t.Fatalf("fletchingProductNames should hold the arrow shaft and nothing else, got %v — a bow "+
			"name in here matches the strung bow the combat scripts buy (see TestStrungBowsAreNotFletchingProducts)",
			fletchingProductNames)
	}
}
