package questplan

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/scripts/tile"
)

// Every id, tile, menu, shop shelf and rule Prince Ali Rescue rests on, read from a Content 289 checkout (RSB0T_CONTENT, skipped
// without one) so that none of them can drift away from the scripts and maps they were derived from:
//
//	RSB0T_CONTENT=<Content checkout> go test -run PrinceAliContent ./internal/scripts/questplan/

// The object, npc and loc ids the plan carries are the pack's.
func TestPrinceAliContentIDs(t *testing.T) {
	obj, npc, loc := csPackIDs(t, "obj"), csPackIDs(t, "npc"), csPackIDs(t, "loc")
	for _, c := range []struct {
		kind string
		ids  map[string]int
		name string
		want int
	}{
		{"obj", obj, "princeskey", ObjPrinceKey}, {"obj", obj, "blondwig", ObjBlondWig}, {"obj", obj, "plainwig", ObjPlainWig},
		{"obj", obj, "keyprint", ObjKeyPrint}, {"obj", obj, "skinpaste", ObjSkinPaste}, {"obj", obj, "pink_skirt", ObjPinkSkirt},
		{"obj", obj, "rope", ObjRope}, {"obj", obj, "softclay", ObjSoftClay}, {"obj", obj, "clay", ObjClay},
		{"obj", obj, "jug_water", ObjJugOfWater}, {"obj", obj, "bucket_water", ObjBucketOfWater}, {"obj", obj, "ashes", ObjAshes},
		{"obj", obj, "bronze_bar", ObjBronzeBar}, {"obj", obj, "beer", ObjBeer}, {"obj", obj, "logs", ObjLogs}, {"obj", obj, "tinderbox", ObjTinderbox},
		{"obj", obj, "bronze_pickaxe", ObjBronzePickaxe}, {"obj", obj, "pot_flour", ObjPotOfFlour}, {"obj", obj, "redberries", ObjRedberries},
		{"obj", obj, "yellowdye", ObjYellowDye}, {"obj", obj, "onion", ObjOnion}, {"obj", obj, "ball_of_wool", ObjBallOfWool},
		{"obj", obj, "wool", ObjWool}, {"obj", obj, "shears", ObjShears},
		{"npc", npc, "hassan", NpcHassan}, {"npc", npc, "osman", NpcOsman}, {"npc", npc, "leela", NpcLeela}, {"npc", npc, "joe", NpcJoe},
		{"npc", npc, "lady_keli", NpcLadyKeli}, {"npc", npc, "prince_ali_prison", NpcPrinceAliPrison}, {"npc", npc, "jailguard", NpcJailGuard},
		{"npc", npc, "shantay", NpcShantay}, {"npc", npc, "ned", NpcNed}, {"npc", npc, "aggie", NpcAggie}, {"npc", npc, "wydin", NpcWydin},
		{"npc", npc, "rustyanchor_bartender", NpcRustyAnchorBartender},
		{"loc", loc, "alidoor", LocPrisonDoor}, {"loc", loc, "clayrock1", LocClayRockA}, {"loc", loc, "clayrock2", LocClayRockB},
		{"loc", loc, "onion", LocOnionPlant},
	} {
		if got, ok := c.ids[c.name]; !ok || got != c.want {
			t.Errorf("%s %s is %d (found %v) in the pack, the plan says %d", c.kind, c.name, got, ok, c.want)
		}
	}
}

// The places: every npc stands where the plan sends the account, the guards are the four the danger zone and the odds name, the
// door and the rocks are where the stands are computed from, and the log and pickaxe spawns lie where the kits go.
func TestPrinceAliContentPlaces(t *testing.T) {
	for name, c := range map[string]struct {
		at tile.Tile
		id int
	}{
		"Hassan": {HassanAnchor, NpcHassan}, "Osman": {OsmanAnchor, NpcOsman}, "Leela": {LeelaAnchor, NpcLeela}, "Ned": {NedAnchor, NpcNed},
		"Aggie": {AggieAnchor, NpcAggie}, "Joe": {JoeAnchor, NpcJoe}, "Lady Keli": {KeliAnchor, NpcLadyKeli},
		"Prince Ali": {PrinceAliAnchor, NpcPrinceAliPrison}, "Shantay": {ShantayAnchor, NpcShantay}, "Wydin": {WydinShop.Anchor, NpcWydin},
		"the Rusty Anchor's bartender": {RustyAnchorBartender, NpcRustyAnchorBartender},
		"guard 1":                      {tile.Tile{X: 3109, Z: 3237}, NpcJailGuard}, "guard 2": {tile.Tile{X: 3120, Z: 3238}, NpcJailGuard},
		"guard 3": {tile.Tile{X: 3121, Z: 3249}, NpcJailGuard}, "guard 4": {tile.Tile{X: 3127, Z: 3248}, NpcJailGuard},
	} {
		if !csHasRow(csMapRows(t, "NPC", c.at), c.id) {
			t.Errorf("%s: no npc %d spawns at %d,%d", name, c.id, c.at.X, c.at.Z)
		}
	}
	// Thessalia has no fixed anchor in the plan beyond her shop's: she stands within a few tiles of it.
	if n := csNearRows(t, "NPC", 548, ThessaliaShop.Anchor, 3); n == 0 {
		t.Errorf("no Thessalia (npc 548) within 3 tiles of her shop anchor %v", ThessaliaShop.Anchor)
	}
	// The Prison Door: a straight wall (shape 0), angle 1, on the cell's north row; the key stand is the tile north of it.
	rows := csMapRows(t, "LOC", PrisonDoorTile)
	found := false
	for _, r := range rows {
		found = found || (len(r) >= 3 && r[0] == strconv.Itoa(LocPrisonDoor) && r[1] == "0" && r[2] == "1")
	}
	if !found {
		t.Errorf("no Prison Door (loc %d, shape 0, angle 1) at %v: %v", LocPrisonDoor, PrisonDoorTile, rows)
	}
	if PrisonDoorStand != (tile.Tile{X: PrisonDoorTile.X, Z: PrisonDoorTile.Z + 1}) {
		t.Errorf("the key stand %v is not the tile north of the door %v", PrisonDoorStand, PrisonDoorTile)
	}
	if PrinceAliAnchor.X < PrinceCell.MinX || PrinceAliAnchor.X > PrinceCell.MaxX || PrinceAliAnchor.Z < PrinceCell.MinZ || PrinceAliAnchor.Z > PrinceCell.MaxZ ||
		!InPrinceCell(PrisonDoorTile) || InPrinceCell(PrisonDoorStand) {
		t.Errorf("the cell %+v: the prince (%v) and the door tile are inside it, the stand is not", PrinceCell, PrinceAliAnchor)
	}
	// The clay rocks and the ground spawns.
	for _, c := range []struct {
		id int
		at tile.Tile
	}{{LocClayRockA, tile.Tile{X: 2986, Z: 3239}}, {LocClayRockB, tile.Tile{X: 2987, Z: 3240}}} {
		if !csHasRow(csMapRows(t, "LOC", c.at), c.id) {
			t.Errorf("no clay rock %d at %v", c.id, c.at)
		}
		if tile.Chebyshev(c.at, ClayRocks) > 2 {
			t.Errorf("the clay anchor %v is more than 2 tiles from the rock at %v", ClayRocks, c.at)
		}
	}
	for _, at := range PrinceLogSpawns {
		if !csHasRow(csMapRows(t, "OBJ", at), ObjLogs) {
			t.Errorf("no logs spawn at %v", at)
		}
	}
	if !csHasRow(csMapRows(t, "OBJ", PrincePickaxeSpawn), ObjBronzePickaxe) {
		t.Errorf("no bronze pickaxe spawn at %v", PrincePickaxeSpawn)
	}
	if tile.Chebyshev(PrinceFireSite, PrinceLogSpawns[0]) > 1 {
		t.Errorf("the fire site %v is not where the logs lie %v", PrinceFireSite, PrinceLogSpawns[0])
	}
}

// The shops the kits buy from hold the items on the shelves the kits name.
func TestPrinceAliContentShops(t *testing.T) {
	for _, c := range []struct{ file, shop, item string }{
		{"scripts/areas/area_desert/configs/shantaypass.inv", "shantayshop", "jug_water"},
		{"scripts/areas/area_desert/configs/shantaypass.inv", "shantayshop", "bronze_bar"},
		{"scripts/areas/area_port_sarim/configs/port_sarim.inv", "wydinstore", "pot_flour"},
		{"scripts/areas/area_port_sarim/configs/port_sarim.inv", "wydinstore", "redberries"},
		{"scripts/areas/area_varrock/configs/varrock.inv", "clotheshop", "pink_skirt"},
		{"scripts/areas/area_lumbridge/configs/lumbridge.inv", "generalshop1", "tinderbox"},
		{"scripts/areas/area_lumbridge/configs/lumbridge.inv", "axeshop", "bronze_pickaxe"},
		{"scripts/areas/area_ardougne_east/configs/ardougne_east.inv", "adventurershop", "ball_of_wool"},
		{"scripts/areas/area_ardougne_east/configs/ardougne_east.inv", "adventurershop", "rope"},
		{"scripts/areas/area_ardougne_east/configs/ardougne_east.inv", "adventurershop", "tinderbox"},
		{"scripts/areas/area_ardougne_east/configs/ardougne_east.inv", "adventurershop", "bronze_pickaxe"},
	} {
		block := csConfigBlock(csContent(t, c.file), c.shop)
		if block == "" || !strings.Contains(block, "="+c.item+",") {
			t.Errorf("%s of %s does not stock %s", c.shop, c.file, c.item)
		}
	}
	// The owners: Thessalia runs the clothes shop, Wydin the food store, Shantay his shelf.
	for _, c := range []struct{ file, npc, shop string }{
		{"scripts/areas/area_varrock/configs/varrock.npc", "thessalia", "clotheshop"},
		{"scripts/areas/area_port_sarim/configs/port_sarim.npc", "wydin", "wydinstore"},
		{"scripts/areas/area_desert/configs/shantaypass.npc", "shantay", "shantayshop"},
	} {
		if block := csConfigBlock(csContent(t, c.file), c.npc); !strings.Contains(block, "param=owned_shop,"+c.shop) {
			t.Errorf("%s does not own %s", c.npc, c.shop)
		}
	}
	// The Rusty Anchor's bartender sells a beer for 2 coins to anyone who asks for it, and Ned a rope for 15.
	bar := csContent(t, "scripts/areas/area_port_sarim/scripts/bartender.rs2")
	if !strings.Contains(bar, "Sure, that will be 2 gold coins please.") || !strings.Contains(bar, "inv_del(inv, coins, 2)") || !strings.Contains(bar, "inv_add(inv, beer, 1)") {
		t.Error("the Rusty Anchor's beer is not 2 coins a talk")
	}
	ned := csContent(t, "scripts/areas/area_draynor/scripts/ned.rs2")
	if !strings.Contains(ned, "inv_del(inv, coins, 15)") || !strings.Contains(ned, "inv_add(inv, rope, 1)") || !strings.Contains(ned, "inv_del(inv, ball_of_wool, 3)") || !strings.Contains(ned, "inv_add(inv, plainwig, 1)") {
		t.Error("Ned's rope is not 15 coins, or his wig not 3 balls of wool")
	}
}

// Every menu option the plan picks is one the scripts offer, by its own text.
func TestPrinceAliContentDialogueOptions(t *testing.T) {
	for _, c := range []struct {
		file string
		stop NpcStop
	}{
		{"scripts/areas/area_alkharid/scripts/hassan.rs2", HassanStartStop},
		{"scripts/areas/area_alkharid/scripts/osman.rs2", OsmanBriefStop},
		{"scripts/areas/area_alkharid/scripts/osman.rs2", OsmanForgeStop},
		{"scripts/areas/area_draynor/scripts/leela.rs2", LeelaStop},
		{"scripts/areas/area_draynor/scripts/ned.rs2", NedWigStop},
		{"scripts/areas/area_draynor/scripts/ned.rs2", NedRopeStop},
		{"scripts/areas/area_draynor/scripts/aggie.rs2", AggiePasteStop},
		{"scripts/areas/area_draynor/scripts/aggie.rs2", AggieYellowStop},
		{"scripts/areas/area_draynor/scripts/lady_keli.rs2", KeliPrintStop},
		{"scripts/areas/area_draynor/scripts/joe_guard.rs2", JoeBeerStop},
		{"scripts/areas/area_port_sarim/scripts/bartender.rs2", BartenderBeerStop},
	} {
		src := csContent(t, c.file)
		for _, p := range c.stop.Prefer {
			if !strings.Contains(src, `"`+p+`"`) {
				t.Errorf("%s: %s offers no option %q", c.file, c.stop.Npc, p)
			}
		}
	}
}

// The rules the design rests on, as the scripts state them.
func TestPrinceAliContentRules(t *testing.T) {
	has := func(file string, needles ...string) {
		t.Helper()
		src := csContent(t, file)
		for _, n := range needles {
			if !strings.Contains(src, n) {
				t.Errorf("%s no longer says %q", file, n)
			}
		}
	}
	// Leela promotes only on the sum of key, wig, skirt and paste, and issues no key while the bank holds one.
	has("scripts/areas/area_draynor/scripts/leela.rs2",
		"%princequest = ^prince_spoken_osman & inv_total(inv, princeskey) >= 1 & inv_total(inv, blondwig) >= 1 & inv_total(inv, pink_skirt) >= 1 & inv_total(inv, skinpaste) >= 1",
		"inv_total(inv, princeskey) = 0 & inv_total(bank, princeskey) = 0", "A new key will cost 15 coins")
	// Osman forges from the print and a bar while keystatus is 0; a lost key is 15 coins.
	has("scripts/areas/area_alkharid/scripts/osman.rs2", "%prince_keystatus = 0 & inv_total(inv, keyprint) >= 1 & inv_total(inv, bronze_bar) >= 1", "A new key will cost 15gp")
	// Joe takes one beer, then two more in the same conversation, from stage 30 only.
	has("scripts/areas/area_draynor/scripts/joe_guard.rs2", "case ^prince_prep_finished : @joe_distract", "inv_total(inv, beer) < 2", "inv_del(inv, beer, 2)")
	// Keli: the rope is used on her (opnpcu), needs the guard drunk, and removes her; the print only at stage 20 with soft clay.
	has("scripts/areas/area_draynor/scripts/lady_keli.rs2", "%princequest = ^prince_spoken_osman & inv_total(inv, softclay) >= 1")
	has("scripts/quests/quest_prince/scripts/quest_prince.rs2", "[opnpcu,lady_keli]", "%princequest < ^prince_guard_drunk", "npc_del;",
		"coordz(coord) <= coordz(loc_coord)", "npc_find(coord, lady_keli, 10, 0) = true", "last_useitem ! princeskey")
	// The prince wants all four.
	has("scripts/areas/area_draynor/scripts/prince_ali.rs2", "inv_total(inv, princeskey) >= 1 & inv_total(inv, blondwig) >= 1 & inv_total(inv, pink_skirt) >= 1 & inv_total(inv, skinpaste) >= 1")
	// The toll gate is free from the rescue on.
	has("scripts/areas/area_alkharid/scripts/border_gate.rs2", "%princequest >= ^prince_saved", "inv_del(inv, coins, 10)")
	// Aggie's paste: redberries, flour, ashes and a bucket or a jug of water; the dye: onions and 5 coins.
	has("scripts/areas/area_draynor/scripts/aggie.rs2",
		"inv_total(inv, redberries) > 0 & inv_total(inv, pot_flour) > 0 & (inv_total(inv, bucket_water) > 0 | inv_total(inv, jug_water) > 0) & inv_total(inv, ashes) > 0",
		"%princequest >= ^prince_spoken_osman & %princequest < ^prince_saved", "inv_del(inv, onion, 2)")
	// Soft clay takes a bucket, jug or bowl of water; the cut-logs script lights a fire from a tinderbox on logs.
	has("scripts/general_use/scripts/water_sources.rs2", "case clay : @make_softclay(bucket_water);", "case clay : @make_softclay(jug_water);")
	has("scripts/skill_fletching/scripts/cut_logs.rs2", "case tinderbox : @light_logs_inv(last_slot);")
	has("scripts/skill_firemaking/scripts/firemaking.rs2", "obj_addall($fire_coord, ashes, 1, 100);", "calc(100 + random(100))")
}

// The jail guard is what FoeJailGuard says: the odds are priced from content's own numbers.
func TestTheJailGuardIsWhatTheContentSays(t *testing.T) {
	block := csConfigBlock(csContent(t, "scripts/areas/area_draynor/configs/draynor.npc"), "jailguard")
	if block == "" {
		t.Fatal("no [jailguard] in draynor.npc")
	}
	field := func(key string) int {
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimSpace(line)
			for _, prefix := range []string{key + "=", "param=" + key + ","} {
				if strings.HasPrefix(line, prefix) {
					n, err := strconv.Atoi(strings.TrimPrefix(line, prefix))
					if err != nil {
						t.Fatalf("%s: %v", line, err)
					}
					return n
				}
			}
		}
		return -1
	}
	f := FoeJailGuard
	for key, want := range map[string]int{"attack": f.Attack, "defence": f.Defence, "strength": f.Strength, "hitpoints": f.Hitpoints,
		"attackrate": f.AttackTicks, "attackbonus": f.AttackBonus, "strengthbonus": f.StrengthBonus, "vislevel": 26, "huntrange": 5} {
		if got := field(key); got != want {
			t.Errorf("jail guard %s: content %d, FoeJailGuard %d", key, got, want)
		}
	}
	if !strings.Contains(block, "huntmode=aggressive_melee") {
		t.Error("the jail guard is no longer aggressive_melee")
	}
	// aggressive_melee hunts every combat level (check_nottoostrong=off): the odds do not cap the guards by the account's level.
	hunt := csContent(t, "scripts/_unpack/225/all.hunt")
	if b := csConfigBlock(hunt, "aggressive_melee"); !strings.Contains(b, "check_nottoostrong=off") {
		t.Error("aggressive_melee is no longer level-blind")
	}
}

// The ids and names against the production config archive (RSB0T_CONFIG_JAG; skipped without it): a wrong id here sends an account
// to the wrong npc or asks the provisioner for the wrong item.
func TestPrinceAliIDsAreWhatContent289CallsThem(t *testing.T) {
	path := os.Getenv("RSB0T_CONFIG_JAG")
	if path == "" {
		t.Skip("RSB0T_CONFIG_JAG not set")
	}
	cfg, err := cachecfg.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[int]string{
		ObjPrinceKey: "Bronze key", ObjBlondWig: "Wig", ObjPlainWig: "Wig", ObjKeyPrint: "Key print", ObjSkinPaste: "Paste", ObjPinkSkirt: "Pink skirt",
		ObjRope: "Rope", ObjSoftClay: "Soft clay", ObjClay: "Clay", ObjJugOfWater: "Jug of water", ObjBucketOfWater: "Bucket of water", ObjAshes: "Ashes",
		ObjBronzeBar: "Bronze bar", ObjBeer: "Beer", ObjLogs: "Logs", ObjTinderbox: "Tinderbox", ObjBronzePickaxe: "Bronze pickaxe", ObjPotOfFlour: "Pot of flour",
		ObjRedberries: "Redberries", ObjYellowDye: "Yellow dye", ObjOnion: "Onion", ObjBallOfWool: "Ball of wool", ObjWool: "Wool", ObjShears: "Shears",
		ObjBucket: "Bucket", CoinsID: "Coins",
	} {
		o, ok := cfg.Obj(id)
		if !ok || o.Name != want {
			t.Errorf("obj %d is %q (ok %v), want %q", id, o.Name, ok, want)
		}
		if o.Noted() {
			t.Errorf("obj %d is a note", id)
		}
		// Coins stack; the rest take a slot each, which is what the kits' slot arithmetic follows.
		if o.Stackable != (id == CoinsID) {
			t.Errorf("obj %d stackable %v, want %v", id, o.Stackable, id == CoinsID)
		}
	}
	for _, id := range princePickaxeIDs {
		if o, ok := cfg.Obj(id); !ok || !strings.HasSuffix(o.Name, "pickaxe") {
			t.Errorf("pickaxe %d is %q", id, o.Name)
		}
	}
	for _, id := range princeFoodIDs {
		if o, ok := cfg.Obj(id); !ok || o.IOps[0] != "Eat" && o.IOps[1] != "Eat" && o.IOps[2] != "Eat" && o.IOps[3] != "Eat" && o.IOps[4] != "Eat" {
			t.Errorf("food %d (%q) has no Eat option: %v", id, o.Name, o.IOps)
		}
	}
	for id, want := range map[int]string{NpcHassan: "Hassan", NpcOsman: "Osman", NpcLeela: "Leela", NpcJoe: "Joe", NpcLadyKeli: "Lady Keli",
		NpcPrinceAliPrison: "Prince Ali", NpcJailGuard: "Jail guard", NpcShantay: "Shantay", NpcNed: "Ned", NpcAggie: "Aggie", NpcWydin: "Wydin"} {
		n, ok := cfg.Npc(id)
		if !ok || n.Name != want {
			t.Errorf("npc %d is %q (ok %v), want %q", id, n.Name, ok, want)
			continue
		}
		if cachecfg.OpIndex(n.Ops, "Talk-to") == 0 {
			t.Errorf("npc %d (%s) has no Talk-to op: %v", id, n.Name, n.Ops)
		}
	}
	for _, id := range []int{NpcShantay, NpcWydin} {
		if n, _ := cfg.Npc(id); cachecfg.OpIndex(n.Ops, "Trade") == 0 {
			t.Errorf("npc %d has no Trade op: %v", id, n.Ops)
		}
	}
	if n, _ := cfg.Npc(548); n.Name != "Thessalia" || cachecfg.OpIndex(n.Ops, "Trade") == 0 {
		t.Errorf("Thessalia (548) is %q with %v", n.Name, n.Ops)
	}
	if n, _ := cfg.Npc(NpcJailGuard); n.Level != 26 || cachecfg.OpIndex(n.Ops, "Attack") == 0 {
		t.Errorf("the jail guard shows level %d, ops %v", n.Level, n.Ops)
	}
	for id, c := range map[int]struct{ name, op string }{LocPrisonDoor: {"Prison Door", "Open"}, LocClayRockA: {"Rocks", "Mine"},
		LocClayRockB: {"Rocks", "Mine"}, LocOnionPlant: {"Onion", "Pick"}} {
		l, ok := cfg.Loc(id)
		if !ok || l.Name != c.name || cachecfg.OpIndex(l.Ops, c.op) == 0 {
			t.Errorf("loc %d is %q with %v (ok %v), want %q offering %q", id, l.Name, l.Ops, ok, c.name, c.op)
		}
	}
}
