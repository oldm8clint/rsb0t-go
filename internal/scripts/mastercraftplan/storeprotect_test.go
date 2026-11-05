package mastercraftplan

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/consolidateplan"
	"rsb0t-go/internal/scripts/crafterplan"
	"rsb0t-go/internal/scripts/fighterfood"
	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/gemplan"
	"rsb0t-go/internal/scripts/herbplan"
	"rsb0t-go/internal/scripts/orderplan"
	"rsb0t-go/internal/scripts/pearlplan"
	"rsb0t-go/internal/scripts/woodcutplan"
)

// Every name the scripts' own definitions give is protected: the set is DERIVED, so a rung added to
// a food ladder, an axe to the woodcutters' list or a tool to a fishing rung is protected without
// anyone remembering to say so, and this test is what tells the author who forgets that a source is
// not wired in.
func TestStoreProtectedCoversEveryDefinitionTheScriptsUse(t *testing.T) {
	var names []string
	add := func(source string, ns ...string) {
		for _, n := range ns {
			if !StoreProtected(n) {
				t.Errorf("%s: %q is not protected", source, n)
			}
		}
		names = append(names, ns...)
	}
	for _, h := range orderplan.DefaultCatalog() {
		add("orderplan.DefaultCatalog", h.Item)
	}
	for _, h := range orderplan.AxeHandouts() {
		add("orderplan.AxeHandouts", h.Item)
	}
	add("orderplan.NeverOpen", orderplan.NeverOpen()...)
	add("scarabplan.FoodLadderFor", fleetplan.FoodLadderFor(99)...)
	add("scarabplan.FoodLadderFor 10", fleetplan.FoodLadderFor(10)...)
	add("fighterfood.FoodOptions", fighterfood.FoodOptions...)
	add("scarabplan.DefaultAlchItems", fleetplan.DefaultAlchItems...)
	add("scarabplan.StaffPreference", fleetplan.StaffPreference(fleetplan.Spell{})...)
	add("scarabplan.StaffAskLadder", fleetplan.StaffAskLadder...)
	add("scarabplan kit constants", fleetplan.StrangeFruit, fleetplan.RopeItem, fleetplan.ShantayPassItem,
		fleetplan.WaterskinItem, "Waterskin(3)", "Waterskin(1)", fleetplan.MagicStaff, fleetplan.StaffOfAir, fleetplan.PlainStaff)
	for _, f := range fleetplan.CookFishes {
		add("scarabplan.CookFishes", f.Raw, f.Cooked)
	}
	for _, s := range fleetplan.CombatSpells {
		for _, r := range s.Runes {
			add("scarabplan.CombatSpells", r.Name)
		}
	}
	add("woodcutplan", append([]string{woodcutplan.Knife}, woodcutplan.Axes...)...)
	add("crafterplan", crafterplan.Flax)
	add("fisherplan.ToolNames", fisherplan.ToolNames()...)
	for _, g := range gemplan.Gems {
		add("gemplan.Gems", g.Name, g.UncutName)
	}
	add("gemplan", gemplan.Chisel, gemplan.CrushedGemstone)
	add("pearlplan", pearlplan.Pearl, pearlplan.Pearls, pearlplan.Tips, pearlplan.Bolt, pearlplan.PearlBolt)
	for _, h := range herbplan.Herbs {
		add("herbplan.Herbs", h.Name, HerbName(h))
	}
	for _, b := range Bars {
		add("Bars", b.Name, b.Ore)
		if b.Second != "" {
			add("Bars", b.Second)
		}
	}
	add("MasterCrafter", "Coins", "Herb", Coal, StaffOfFire, ArrowShaft, Feather, Headless, ArrowTips, SteelArrow, ChestKeyItem, "Law rune")
	for _, n := range consolidateplan.KitItems {
		if !strings.HasSuffix(n, "(") {
			add("consolidateplan.KitItems", n)
		}
	}
	add("consolidateplan.QuestItems", consolidateplan.QuestItems...)
	if len(names) < 150 {
		t.Fatalf("only %d names were checked: a source is empty", len(names))
	}
	if !StoreProtected("") || !StoreProtected("   ") {
		t.Error("an unnamed row is protected")
	}
}

// What the 5 Oct 2026 live the mule held in rows of five or fewer (the owner's list of 70), by whether
// the derived protection leaves it movable: the rest of the work (tradeable, one obj, a name the
// mule reads) is the bot's row builder's and the plan's.
func TestStoreProtectedOnTheLiveMule1sSmallRows(t *testing.T) {
	stay := []string{
		"Stew", "Chocolate cake", "Chocolate slice", // food
		"Plank", "Bucket of sand", "Sickle mould", "Insect repellent", "Unfired bowl", "Wizard's mind bomb", "Bat bones", "Silk", // quest kits
		"Waterskin(1)", "Waterskin(4)", "Shantay pass", // the Kalphite kit
		"Lobster pot", "Harpoon", "Iron axe", // the mule's catalogue and the woodcutters' axes
		"Steel battleaxe", "Mithril kiteshield", "Ruby ring", // the owner's alch stock (6 Oct 2026), which Magic 55+ accounts ask the mule for
		"Iron platebody", "Rune arrowtips", "Adamant arrowtips", // the anvil round's products (8 Oct 2026): alch stock, and the arrow round's tips
		"Steel platebody",                     // alch stock the owner bought (3 Nov 2026)
		"Rune platebody", "Adamant platebody", // the platebody round's products (5 Nov 2026): alch stock
	}
	for _, n := range stay {
		if !StoreProtected(n) {
			t.Errorf("%q is a kit item and must stay", n)
		}
	}
	move := []string{
		"Mime top", "Mime legs", "Dragon longsword", "Dragon dagger", "Rune scimitar", "Rune sword", "Steel claws",
		"Dragon med helm", "Rune platelegs", "Rune chainbody", "Black full helm",
		"Monk's robe", "Desert robe", "Cape", "Dragonfire shield", "Desert boots", "Beer glass", "Seasoned sardine",
		"Amulet of glory", "Gold ring", "Snape grass", "Limpwurt root", "Adamant pickaxe", "Mithril pickaxe",
		"Steel pickaxe", "Rune pickaxe", "Bucket of milk", "Bucket of water", "Jug of water", "White apron", "Eye patch",
		"Doogle leaves", "Rotten food", "Mithril dagger", "Steel plateskirt", "Bronze platebody",
		"Bronze kiteshield", "Desert shirt",
	}
	for _, n := range move {
		if StoreProtected(n) {
			t.Errorf("%q is gear or a leftover no kit names and may move", n)
		}
	}
}

// A name the stores know several ways is one name: case and spaces do not matter, and a grimy herb
// by its qualified name is protected whatever its id.
func TestStoreProtectedReadsNamesLikeTheMuleDoes(t *testing.T) {
	for _, n := range []string{"coins", " COINS ", "law RUNE", "Herb id199", "herb id3051", "Raw lobster", "Staff of Fire", "waterskin(2)"} {
		if !StoreProtected(n) {
			t.Errorf("%q should be protected", n)
		}
	}
	for _, n := range []string{"Herbert", "Dragon longsword", "Runeblade"} {
		if StoreProtected(n) {
			t.Errorf("%q should not be protected", n)
		}
	}
}

// A per-obj row of the mule's published bank ("Yew shortbow id857", which peerrunner publishes for
// every bow besides the merged "Yew shortbow") is protected exactly when the name it is a row of is: the
// storage round never moves the fleet's bows, strung or not, and still moves what nothing uses.
func TestStoreProtectedFollowsAPerObjRowToItsName(t *testing.T) {
	for _, b := range []string{"Yew shortbow id857", "Yew shortbow id68", "Yew longbow id66", "Maple longbow id851", "Shortbow id841", "Herb id199"} {
		if !StoreProtected(b) {
			t.Errorf("%q is not protected", b)
		}
	}
	for _, n := range fleetplan.DefaultAlchItems {
		if !StoreProtected(n + " id1234") {
			t.Errorf("a per-obj row of the alch stock item %q is not protected", n)
		}
	}
	if StoreProtected("Cabbage id1965") {
		t.Error("a per-obj row of an item nothing uses is protected: the storage round could never move it")
	}
}
