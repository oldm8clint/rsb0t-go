package questplan

import (
	"fmt"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Every id, tile, menu, message, stage and shop Pirate's Treasure rests on, read from a Content 289 checkout (RSB0T_CONTENT,
// skipped without one) so that none of them can drift away from the scripts and maps they were derived from:
//
//	RSB0T_CONTENT=<Content checkout> go test -run HuntContent ./internal/scripts/questplan/
//
// The helpers (csContent, csPackIDs, csConfigBlock, csMapRows) are the Cook's Assistant and Sheep Shearer content test's.

func hasAll(t *testing.T, file, src string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(src, w) {
			t.Errorf("%s lacks %q", file, w)
		}
	}
}

// The object, npc and loc ids the plan carries are the pack's.
func TestHuntContentIDs(t *testing.T) {
	obj, npc, loc := csPackIDs(t, "obj"), csPackIDs(t, "npc"), csPackIDs(t, "loc")
	for _, c := range []struct {
		kind string
		ids  map[string]int
		name string
		want int
	}{
		{"obj", obj, "karamja_rum", ObjKaramjaRum}, {"obj", obj, "chest_key", ObjChestKey}, {"obj", obj, "piratemessage", ObjPirateMessage},
		{"obj", obj, "spade", ObjSpade}, {"obj", obj, "white_apron", ObjWhiteApron}, {"obj", obj, "banana", ObjBanana}, {"obj", obj, "coins", CoinsID},
		{"npc", npc, "redbeard_frank", NpcRedbeardFrank}, {"npc", npc, "luthas", NpcLuthas}, {"npc", npc, "zambo", NpcZambo},
		{"npc", npc, "falador_gardener", NpcFaladorGardener}, {"npc", npc, "wydin", NpcWydin}, {"npc", npc, "thessalia", 548},
		{"loc", loc, "wydindoor", LocWydinDoor}, {"loc", loc, "grocerycrate", LocGroceryCrate}, {"loc", loc, "bananacrate", LocBananaCrate},
		{"loc", loc, "piratechest", LocPirateChest}, {"loc", loc, "piratechestopen", LocPirateChest + 1},
		{"loc", loc, "bananatreefull", BananaTreeLocs[0]}, {"loc", loc, "bananatreefour", BananaTreeLocs[1]},
		{"loc", loc, "bananatreethree", BananaTreeLocs[2]}, {"loc", loc, "bananatreetwo", BananaTreeLocs[3]},
		{"loc", loc, "bananatreeone", BananaTreeLocs[4]}, {"loc", loc, "bananatreeempty", BananaTreeLocs[4] + 1},
	} {
		if got, ok := c.ids[c.name]; !ok || got != c.want {
			t.Errorf("%s %s is %d in the pack (found %v), the plan says %d", c.kind, c.name, got, ok, c.want)
		}
	}
}

// The stages, the points and the reward.
func TestHuntContentStages(t *testing.T) {
	hasAll(t, "quest_hunt.constant", csContent(t, "scripts/quests/quest_hunt/configs/quest_hunt.constant"),
		fmt.Sprintf("^hunt_not_started = %d", HuntVarpNotStarted), fmt.Sprintf("^hunt_fetch_rum = %d", HuntVarpFetchRum),
		fmt.Sprintf("^hunt_received_key = %d", HuntVarpReceivedKey), fmt.Sprintf("^hunt_read_note = %d", HuntVarpReadNote))
	hasAll(t, "quest.constant", csContent(t, "scripts/general/configs/quest.constant"),
		fmt.Sprintf("^hunt_complete = %d", HuntVarpComplete), fmt.Sprintf("^hunt_questpoints = %d", PiratesTreasureRecord.QuestPoints))
	hasAll(t, "dig.rs2", csContent(t, "scripts/quests/quest_hunt/scripts/dig.rs2"),
		"inv_add(inv, coins, 450);", "inv_add(inv, gold_ring, 1);", "inv_add(inv, emerald, 1);", "%hunt = ^hunt_complete;",
		"if (%hunt ! 3) {", "p_delay(4);", "queue(hunt_quest_complete, 0, 0);", `mes("You dig a hole in the ground...");`,
		`mes("and find a little bag of treasure.");`, "npc_find(coord, falador_gardener, 10, 0) = true", "npc_setmode(opplayer2);")
	// Two quest points and no experience: the quest-complete call has no stat_advance, and the ladder row carries none.
	if dig := csContent(t, "scripts/quests/quest_hunt/scripts/dig.rs2"); strings.Contains(dig, "stat_advance") {
		t.Error("the dig pays experience; the ladder row says it does not")
	}
	// The dig works within one tile of the X, and the X is the plan's dig site (0_46_52_55_55 is map square 46,52 tile 55,55).
	if spade := csContent(t, "scripts/general_use/scripts/spade.rs2"); !strings.Contains(spade, "if (distance(coord, 0_46_52_55_55) <= 1) {") ||
		!strings.Contains(spade, "@hunt_dig;") {
		t.Errorf("spade.rs2 does not dig the quest at 0_46_52_55_55:\n%s", spade[:min(len(spade), 400)])
	}
	if want := (tile.Tile{X: 46*64 + 55, Z: 52*64 + 55}); DigSite != want {
		t.Errorf("DigSite %v, the script's X is %v", DigSite, want)
	}
	// The gardener: level 3, 7 hitpoints, back 50 ticks after he dies.
	g := csConfigBlock(csContent(t, "scripts/_unpack/274/all.npc"), "falador_gardener")
	hasAll(t, "[falador_gardener]", g, "op2=Attack", "attack=2", "defence=5", "hitpoints=7", "respawnrate=50")
	if FoeFaladorGardener.Attack != 2 || FoeFaladorGardener.Defence != 5 || FoeFaladorGardener.Hitpoints != 7 || FoeFaladorGardener.MaxHit() != 1 {
		t.Errorf("FoeFaladorGardener %+v (max hit %d)", FoeFaladorGardener, FoeFaladorGardener.MaxHit())
	}
}

// The dialogue the stops steer: every preferred option is a menu line of the script, and the lines the plan avoids exist.
func TestHuntContentDialogue(t *testing.T) {
	frank := csContent(t, "scripts/quests/quest_hunt/scripts/redbeard_frank.rs2")
	for _, line := range FrankStop.Prefer {
		if !strings.Contains(frank, `"`+line+`"`) {
			t.Errorf("redbeard_frank.rs2 has no option %q", line)
		}
	}
	hasAll(t, "redbeard_frank.rs2", frank,
		// The hand-over takes one rum from the pack, gives the key and sets the stage.
		"if (inv_total(inv, karamja_rum) >= 1) {", "inv_del(inv, karamja_rum, 1);", "inv_add(inv, chest_key, 1);", "%hunt = ^hunt_received_key;",
		// A lost key is re-issued when the PACK holds none (the bank is not looked at).
		"%hunt = ^hunt_received_key & inv_total(inv, chest_key) = 0",
		// "Arr!" loops back to the same menu, so only the trade line ends the talk.
		`[label,redbeard_arr]`, "@redbeard_options;", `@multi2("Arr!", redbeard_arr, "Do you have anything for trade?", redbeard_trade);`,
		"%hunt = ^hunt_fetch_rum;")

	luthas := csContent(t, "scripts/quests/quest_hunt/scripts/luthas.rs2")
	for _, line := range append(LuthasHireStop.Prefer, LuthasShipStop.Prefer...) {
		if !strings.Contains(luthas, `"`+line+`"`) {
			t.Errorf("luthas.rs2 has no option %q", line)
		}
	}
	hasAll(t, "luthas.rs2", luthas,
		"testbit(%hunt_store_employed, ^hunt_not_started) = ^false", "%hunt_store_employed = setbit(%hunt_store_employed, ^hunt_not_started);",
		// The payment ships the crate: employment off, bananas cleared, the rum on to Wydin's.
		"if (%crate_bananas = 10) {", "inv_add(inv, coins, 30);", "%hunt_store_employed = clearbit(%hunt_store_employed, ^hunt_not_started);",
		"%crate_bananas = 0;", "if (%crate_rum = 1) {", "%crate_rum = 2;",
		// The menu after the payment: "another crate full" hires again, which the plan never chooses.
		`"Will you pay me for another crate full?", luthas_another`, `"Thank you, I'll be on my way", luthas_thanks`)

	wydin := csContent(t, "scripts/areas/area_port_sarim/scripts/wydin.rs2")
	for _, line := range WydinJobStop.Prefer {
		if !strings.Contains(wydin, `"`+line+`"`) {
			t.Errorf("wydin.rs2 has no option %q", line)
		}
	}
	hasAll(t, "wydin.rs2", wydin, "else if (%hunt = ^hunt_fetch_rum) {", "inv_total(inv, white_apron) = 0 & inv_total(worn, white_apron) = 0",
		"%hunt_store_employed = setbit(%hunt_store_employed, ^hunt_fetch_rum);")
}

// The crate, the back room and the customs: the messages the module reads and the rules it plans around.
func TestHuntContentRules(t *testing.T) {
	crate := csContent(t, "scripts/quests/quest_hunt/scripts/banana_crate.rs2")
	hasAll(t, "banana_crate.rs2", crate,
		`mes("The crate is completely empty.");`, `mes("There is some rum in here, although with no bananas to cover it.");`, `mes("The crate is full of bananas.");`,
		`mes("There is also some rum stashed in here too.");`, `mes("The crate has <$pluralised> inside.");`, `~pluralise(%crate_bananas, "banana")`,
		// The refusals and the stash.
		`mes("Why would I want to do that?");`, `mes("I see no reason to do that.");`,
		`mes("There is already some rum in Wydin's store, I should go and get that first.");`, `mes("There's already some rum in here...");`,
		`~mesbox("You stash the rum in the crate.");`, `~mesbox("You pack a banana into the crate.");`, `mes("The crate is already full.");`,
		"%crate_rum = 1;", "if (%hunt ! 1) {", "if (%crate_bananas = 10) {")
	// The strings the parsers read are the script's, lower-cased.
	for _, said := range []string{"the crate is completely empty", "there is some rum in here", "the crate is full of bananas",
		"there is also some rum stashed in here", "you stash the rum in the crate", "why would i want to do that",
		"already some rum in wydin's store", "already some rum in here", "i see no reason to do that"} {
		if !strings.Contains(strings.ToLower(crate), said) {
			t.Errorf("banana_crate.rs2 never says %q", said)
		}
	}
	if pl := csContent(t, "scripts/general/scripts/misc/string_procs.rs2"); !strings.Contains(pl, `return ("1 <$noun>");`) ||
		!strings.Contains(pl, `return ("<tostring($amount)> <$noun>s");`) {
		t.Error("~pluralise no longer says '1 banana' and 'N bananas'")
	}

	store := csContent(t, "scripts/quests/quest_hunt/scripts/food_store.rs2")
	hasAll(t, "food_store.rs2", store,
		// The door: only with Wydin near, bit 1 set and the apron on the torso.
		"npc_find(coord, wydin, 10, 0) = true", "testbit(%hunt_store_employed, ^hunt_fetch_rum) = ^false", "inv_getobj(worn, ^wearpos_torso) ! white_apron",
		// The grocery crate gives the rum back when it is in the shop, then asks about a banana.
		"if (%crate_rum = 2) {", "%crate_rum = 0;", "inv_add(inv, karamja_rum, 1);", `~p_choice2_header("Yes.", 1, "No", 2, "Do you want to take a banana?")`)

	chest := csContent(t, "scripts/quests/quest_hunt/scripts/pirate_message.rs2")
	hasAll(t, "pirate_message.rs2", chest, "[oplocu,piratechest]", "last_useitem ! chest_key", "loc_change(piratechestopen, 10);",
		"inv_del(inv, chest_key, 1);", "inv_add(inv, piratemessage, 1);", "[opheld1,piratemessage]", "%hunt = ^hunt_read_note;")
	loc := csContent(t, "scripts/quests/quest_hunt/configs/quest_hunt.loc")
	if b := csConfigBlock(loc, "piratechest"); !strings.Contains(b, "forceapproach=north") || !strings.Contains(b, "op1=Open") {
		t.Errorf("[piratechest] %q: the chest is approached from the north and offers Open", b)
	}
	for _, name := range []string{"karamja_rum", "chest_key", "piratemessage"} {
		if b := csConfigBlock(csContent(t, "scripts/quests/quest_hunt/configs/quest_hunt.obj"), name); !strings.Contains(b, "tradeable=no") {
			t.Errorf("[%s] is tradeable: %q (the plan keeps it out of every mule ask and shop)", name, b)
		}
	}

	// The reason the quest uses a crate: the customs officer takes the rum from anyone boarding for the mainland, and a teleport
	// breaks it.
	customs := csContent(t, "scripts/areas/area_karamja/scripts/customs_officer.rs2")
	hasAll(t, "customs_officer.rs2", customs, "if (%hunt >= 2) {", "inv_del(inv, karamja_rum, 28);", `mes("The customs officer confiscates your rum.");`,
		`~set_sail("Port Sarim"`, "Aha, trying to smuggle rum are we?")
	hasAll(t, "teleport.rs2", csContent(t, "scripts/skill_magic/scripts/spells/teleport.rs2"), "inv_del(inv, karamja_rum, $rum);", "Your Karamjan rum gets broken and spilled.")
	// Banana trees: one banana a search, five stages, 500 ticks to refill after the last pick.
	hasAll(t, "banana_tree.rs2", csContent(t, "scripts/areas/area_karamja/scripts/banana_tree.rs2"), "loc_change(loc_param(next_loc_stage), 500);",
		"inv_add(inv, banana, 1);", `mes("You pick a banana.");`)
	trees := csContent(t, "scripts/areas/area_karamja/configs/plantation.loc")
	for _, name := range []string{"bananatreefull", "bananatreefour", "bananatreethree", "bananatreetwo", "bananatreeone"} {
		if b := csConfigBlock(trees, name); !strings.Contains(b, "op1=Search") || !strings.Contains(b, "name=Banana Tree") {
			t.Errorf("[%s] %q", name, b)
		}
	}
	if b := csConfigBlock(trees, "bananatreeempty"); !strings.Contains(b, "op1=Search") || !strings.Contains(b, "name=Banana Tree") {
		t.Errorf("the empty tree must keep the Search op that makes a name search pick it for ever: %q", b)
	}
}

// Every shop the plan names sells what it says, and the keeper is the npc the plan aims at.
func TestHuntContentShops(t *testing.T) {
	booze := csConfigBlock(csContent(t, "scripts/areas/area_karamja/configs/karamja.inv"), "boozeshop")
	hasAll(t, "[boozeshop]", booze, "stock2=karamja_rum,3,100", "restock=yes")
	zambo := csConfigBlock(csContent(t, "scripts/areas/area_karamja/configs/karamja.npc"), "zambo")
	hasAll(t, "[zambo]", zambo, "param=owned_shop,boozeshop", "param=shop_sell_multiplier,1000", "op3=Trade")
	if c := csContent(t, "scripts/quests/quest_hunt/configs/quest_hunt.obj"); !strings.Contains(csConfigBlock(c, "karamja_rum"), "cost=30") ||
		HuntRumPrice != 30 {
		t.Error("the rum no longer costs 30 at base (sell multiplier 1000 is 1x)")
	}
	clothes := csConfigBlock(csContent(t, "scripts/areas/area_varrock/configs/varrock.inv"), "clotheshop")
	hasAll(t, "[clotheshop]", clothes, "stock1=white_apron,3,100", "restock=yes")
	thessalia := csConfigBlock(csContent(t, "scripts/areas/area_varrock/configs/varrock.npc"), "thessalia")
	hasAll(t, "[thessalia]", thessalia, "param=owned_shop,clotheshop", "op3=Trade")
	if ThessaliaShop.Npc != "Thessalia" {
		t.Errorf("ThessaliaShop %+v", ThessaliaShop)
	}
}

// The tiles: keepers, locs, spawns, and the grove.
func TestHuntContentTiles(t *testing.T) {
	for _, c := range []struct {
		what    string
		section string
		at      tile.Tile
		id      int
	}{
		{"Redbeard Frank", "NPC", FrankAnchor, NpcRedbeardFrank},
		{"Luthas", "NPC", LuthasAnchor, NpcLuthas},
		{"Zambo", "NPC", ZamboShop.Anchor, NpcZambo},
		{"Wydin", "NPC", WydinJobStop.Anchor, NpcWydin},
		{"Thessalia", "NPC", ThessaliaShop.Anchor, 548},
		{"the plantation crate", "LOC", PlantationCrate, LocBananaCrate},
		{"Wydin's grocery crate", "LOC", StoreCrate, LocGroceryCrate},
		{"Hector's chest", "LOC", ChestTile, LocPirateChest},
		{"the back room's door", "LOC", tile.Tile{X: 3012, Z: 3204}, LocWydinDoor},
		{"the spade by Falador park", "OBJ", SpadeSpawn, ObjSpade},
	} {
		if !csHasRow(csMapRows(t, c.section, c.at), c.id) {
			t.Errorf("%s: no %s %d at %d,%d,%d in the map", c.what, c.section, c.id, c.at.X, c.at.Z, c.at.Level)
		}
	}
	// The gardener is spawned inside the radius that refuses the dig, so he cannot be waited out (the NPC rows of the map square
	// are read once: csMapRows would read the file for every tile).
	found := false
	inNPC := false
	for _, line := range strings.Split(csContent(t, fmt.Sprintf("maps/m%d_%d.jm2", DigSite.X>>6, DigSite.Z>>6)), "\n") {
		if strings.HasPrefix(line, "==== ") {
			inNPC = line == "==== NPC ===="
			continue
		}
		var lvl, lx, lz, id int
		if head, tail, ok := strings.Cut(line, ":"); inNPC && ok {
			if n, _ := fmt.Sscanf(head, "%d %d %d", &lvl, &lx, &lz); n == 3 {
				if _, err := fmt.Sscanf(strings.TrimSpace(tail), "%d", &id); err == nil && id == NpcFaladorGardener && lvl == 0 &&
					tile.Chebyshev(tile.Tile{X: DigSite.X&^63 + lx, Z: DigSite.Z&^63 + lz}, DigSite) <= 10 {
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("no Falador gardener spawns within ten tiles of the X")
	}
	// The grove: the first banana trees stand at the east end, and there are enough for a crowd.
	if n := csNearRows(t, "LOC", BananaTreeLocs[0], BananaGrove, 6); n < 6 {
		t.Errorf("%d banana trees within 6 of the grove's anchor", n)
	}
}

// The renderer in the tests is the journal script's: every text it uses is in the file, and the branch conditions are the ones
// it was written from.
func TestHuntJournalStringsAreTheScripts(t *testing.T) {
	src := csContent(t, "scripts/quests/quest_hunt/scripts/hunt_journal.rs2")
	for _, part := range hjAll {
		if !strings.Contains(src, part) {
			t.Errorf("hunt_journal.rs2 has no text %q", part)
		}
	}
	hasAll(t, "hunt_journal.rs2", src,
		"%hunt_store_employed = 0 & %crate_rum < 2",
		"%hunt_store_employed = 0 & %crate_rum = 2 | %hunt_store_employed = 1 & %crate_rum = 2",
		"%hunt_store_employed = 1)", "%hunt_store_employed = 2)", "%hunt_store_employed = 3)",
		"inv_total(inv, karamja_rum) > 0 & %crate_rum = 0", "%crate_bananas < 10", "%crate_rum = 1", "%crate_rum = 2", "%crate_rum = 0",
		"inv_total(inv, piratemessage) > 0", "inv_total(inv, chest_key) > 0", `~quest_journal("Pirate's Treasure", $text);`)
	// The quest tab's own spelling.
	if q := csContent(t, "scripts/player/interfaces/questlist.if"); !strings.Contains(q, "text="+PiratesTreasureName) {
		t.Errorf("questlist.if has no entry %q", PiratesTreasureName)
	}
	// The varps are per player: one account's crate is not another's.
	vp := csContent(t, "scripts/quests/quest_hunt/configs/quest_hunt.varp")
	for _, name := range []string{"hunt", "hunt_store_employed", "crate_bananas", "crate_rum"} {
		if b := csConfigBlock(vp, name); !strings.Contains(b, "scope=perm") {
			t.Errorf("[%s] %q is not a perm varp", name, b)
		}
	}
	// Employment bit 0 is the plantation and bit 1 Wydin's (^hunt_not_started and ^hunt_fetch_rum are the bit indices 0 and 1).
	if HuntEmployedPlantation != 1<<HuntVarpNotStarted || HuntEmployedStore != 1<<HuntVarpFetchRum {
		t.Errorf("the employment bits %d and %d are not 1<<0 and 1<<1", HuntEmployedPlantation, HuntEmployedStore)
	}
}
