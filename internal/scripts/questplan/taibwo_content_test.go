package questplan

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Tai Bwo Wannai Trio's plan against the content checkout (RSB0T_CONTENT): the ids it carries, the spawns its tiles and its fences
// are made from, and the facts of the scripts its design rests on. Each test skips without the checkout.

var tbMaps struct {
	once   sync.Once
	spawns map[int][]tile.Tile
	err    error
}

// tbMapSpawns is every npc spawn of the content maps (maps/mX_Z.jm2, the "==== NPC ====" section's "level x z: id" rows), by npc id.
func tbMapSpawns(t *testing.T, root string) map[int][]tile.Tile {
	t.Helper()
	tbMaps.once.Do(func() {
		tbMaps.spawns = map[int][]tile.Tile{}
		files, err := filepath.Glob(filepath.Join(root, "maps", "m*_*.jm2"))
		if err != nil {
			tbMaps.err = err
			return
		}
		name := regexp.MustCompile(`m(\d+)_(\d+)\.jm2$`)
		row := regexp.MustCompile(`^(\d+) (\d+) (\d+): (\d+)$`)
		for _, f := range files {
			m := name.FindStringSubmatch(filepath.ToSlash(f))
			if m == nil {
				continue
			}
			rx, _ := strconv.Atoi(m[1])
			rz, _ := strconv.Atoi(m[2])
			raw, err := os.ReadFile(f)
			if err != nil {
				tbMaps.err = err
				return
			}
			in := false
			for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
				switch {
				case strings.HasPrefix(line, "==== "):
					in = strings.HasPrefix(line, "==== NPC")
				case in:
					if r := row.FindStringSubmatch(strings.TrimSpace(line)); r != nil {
						lv, _ := strconv.Atoi(r[1])
						x, _ := strconv.Atoi(r[2])
						z, _ := strconv.Atoi(r[3])
						id, _ := strconv.Atoi(r[4])
						tbMaps.spawns[id] = append(tbMaps.spawns[id], tile.Tile{X: rx*64 + x, Z: rz*64 + z, Level: lv})
					}
				}
			}
		}
	})
	if tbMaps.err != nil {
		t.Fatal(tbMaps.err)
	}
	return tbMaps.spawns
}

// The ids the plan carries are the content packs' names for them.
func TestTbwtIdsAreTheContents(t *testing.T) {
	root := contentRoot(t)
	objs := packNames(t, filepath.Join(root, "pack", "obj.pack"))
	npcs := packNames(t, filepath.Join(root, "pack", "npc.pack"))
	locs := packNames(t, filepath.Join(root, "pack", "loc.pack"))
	for id, want := range map[int]string{
		TbObjNet: "net", TbObjPestle: "pestle_and_mortar", TbObjNatureRune: "naturerune", TbObjSlicedBanana: "tbwt_sliced_banana",
		TbObjRumSlicedBanana: "tbwt_sliced_banana_in_karamja_rum", TbObjVessel: "tbwt_karambwan_vessel", TbObjVesselLoaded: "tbwt_karambwan_vessel_loaded_with_karambwanji",
		TbObjManual: "tbwt_crafting_manual", TbObjRawKarambwanji: "tbwt_raw_karambwanji", TbObjKarambwanjiPaste: "tbwt_raw_karambwanji_paste",
		TbObjRawKarambwan: "tbwt_raw_karambwan", TbObjPoorKarambwan: "tbwt_poorly_cooked_karambwan", TbObjBurntKarambwan: "tbwt_burnt_karambwan",
		TbObjPoisonPaste: "tbwt_poisonous_karambwan_paste", TbObjMonkeyCorpse: "tbwt_monkey_corpse", TbObjMonkeySkin: "tbwt_monkey_skin",
		TbObjSandwich: "tbwt_seaweed_in_monkey_skin_sandwich", TbObjJogreBones: "tbwt_jogre_bones", TbObjBurntJogreBones: "tbwt_burnt_jogre_bones",
		TbObjBurntBonesInPaste: "tbwt_burnt_jogre_bones_in_raw_karambwanji_paste", TbObjMarinatedBones: "tbwt_burnt_jogre_bones_marinated_in_karambwanji",
		ObjKnife: "knife", ObjSeaweed: "seaweed", ObjFireRune: "firerune", ObjAirRune: "airrune", ObjChaosRune: "chaosrune", ObjBanana: "banana",
		ObjKaramjaRum: "karamja_rum", CoinsID: "coins", ObjShark: "shark", tbObjRawShrimpID: "raw_shrimp",
	} {
		if objs[id] != want {
			t.Errorf("obj %d is %q in the content, the plan means %q", id, objs[id], want)
		}
	}
	for _, sp := range TbSpears {
		base := strings.ToLower(strings.Fields(sp.Name)[0])
		if objs[sp.ID] != base+"_spear" || objs[sp.PoisonID] != "tbwt_"+base+"_spear_kp" {
			t.Errorf("%s: obj %d is %q and obj %d is %q", sp.Name, sp.ID, objs[sp.ID], sp.PoisonID, objs[sp.PoisonID])
		}
	}
	for _, p := range TbPotions {
		want := strconv.Itoa(p.Doses) + "dose1agility"
		if objs[p.ID] != want {
			t.Errorf("%s: obj %d is %q, want %q", p.Name, p.ID, objs[p.ID], want)
		}
	}
	for id, want := range map[int]string{
		TbNpcTimfraku: "tbwt_timfraku", TbNpcTiadeche: "tbwt_tiadeche", TbNpcTinsay: "tbwt_tinsay", TbNpcTamayu: "tbwt_tamayu", TbNpcLubufu: "tbwt_lubufu",
		TbNpcShaikahan: "tbwt_beast", TbNpcMonkey: "monkey", TbNpcJiminua: "jiminua", TbNpcKarambwanjiSpot: "0_43_47_karambwanji",
		TbNpcKarambwanSpotA: "0_45_48_karambwan", TbNpcKarambwanSpotB: "general_karambwan", TbNpcKarambwanSpotC: "lubufu_karambwan",
		NpcJogre: "jogre", NpcJungleSpider: "jungle_spider", NpcTribesman: "tribesman", NpcGuardDog: "guarddog",
	} {
		if npcs[id] != want {
			t.Errorf("npc %d is %q in the content, the plan means %q", id, npcs[id], want)
		}
	}
	// The fire is "Fire" in the loc pack's names too (its loc index lookup is by id, in the pack test).
	if locs[TbLocLadder] == "" || locs[TbLocFire] == "" {
		t.Errorf("loc %d (%q) or loc %d (%q) is not in the pack", TbLocFire, locs[TbLocFire], TbLocLadder, locs[TbLocLadder])
	}
}

// tbObjRawShrimpID is the shrimp the net brings up, as the glue spells it (questmods/tbwt.go keeps its own copy).
const tbObjRawShrimpID = 317

// The brothers stand where the plan says: the plan's anchors are the maps' spawns of their npcs, and the Shaikahan's is the box centre
// the price is written for.
func TestTbwtAnchorsAreTheSpawns(t *testing.T) {
	root := contentRoot(t)
	spawns := tbMapSpawns(t, root)
	for name, c := range map[string]struct {
		npc  int
		want tile.Tile
	}{
		"Timfraku": {TbNpcTimfraku, TbTimfrakuAnchor}, "Lubufu": {TbNpcLubufu, TbLubufuAnchor}, "Tiadeche": {TbNpcTiadeche, TbTiadecheAnchor},
		"Tamayu": {TbNpcTamayu, TbTamayuAnchor}, "Tinsay": {TbNpcTinsay, TbTinsayAnchor}, "Jiminua": {TbNpcJiminua, TbJiminuaShop.Anchor},
	} {
		found := false
		for _, at := range spawns[c.npc] {
			if at == c.want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: the plan's anchor %v is no spawn of npc %d (%v)", name, c.want, c.npc, spawns[c.npc])
		}
	}
	// The shoals of the lake are the three spawns of the Karambwanji spot.
	got := append([]tile.Tile(nil), spawns[TbNpcKarambwanjiSpot]...)
	want := append([]tile.Tile(nil), TbLakeShoals...)
	less := func(s []tile.Tile) func(i, j int) bool {
		return func(i, j int) bool { return s[i].X < s[j].X }
	}
	sort.Slice(got, less(got))
	sort.Slice(want, less(want))
	if len(got) != len(want) {
		t.Fatalf("the lake's shoals: %v in the maps, %v in the plan", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("shoal %d: %v in the maps, %v in the plan", i, got[i], want[i])
		}
	}
	// The monkeys of the plan's cluster stand within a few tiles of its middle, and the Jogre target is a spawn.
	near := 0
	for _, m := range spawns[TbNpcMonkey] {
		if tile.Chebyshev(m, TbMonkeys) <= 6 {
			near++
		}
	}
	if near < 5 {
		t.Errorf("%d monkeys within 6 tiles of %v", near, TbMonkeys)
	}
	found := false
	for _, j := range spawns[NpcJogre] {
		if j == TbJogreTarget {
			found = true
		}
	}
	if !found {
		t.Errorf("the Jogre target %v is no Jogre spawn", TbJogreTarget)
	}
	// The beast's own spawn is where its box is written.
	beast := spawns[TbNpcShaikahan]
	if len(beast) != 1 || beast[0] != (tile.Tile{X: 2906, Z: 3095}) {
		t.Errorf("the Shaikahan spawns at %v, the plan says 2906,3095", beast)
	}
}

// The fenced spawns are the maps' own, over the box the legs lie in: no hunter is missing and none is invented.
func TestTbwtHostilesAreTheMapsOwn(t *testing.T) {
	root := contentRoot(t)
	spawns := tbMapSpawns(t, root)
	inBox := func(at tile.Tile) bool {
		return at.Level == 0 && at.X >= 2740 && at.X <= 2960 && at.Z >= 2930 && at.Z <= 3200
	}
	for name, c := range map[string]struct {
		npc  int
		have []tile.Tile
	}{
		"jungle spiders": {NpcJungleSpider, tbwtSpiderSpawns}, "tribesmen": {NpcTribesman, tbwtTribesmanSpawns},
		"guard dogs": {NpcGuardDog, tbwtGuardDogSpawns}, "Jogres": {NpcJogre, tbwtJogreSpawns},
	} {
		var want []tile.Tile
		for _, at := range spawns[c.npc] {
			if inBox(at) {
				want = append(want, at)
			}
		}
		sort.Slice(want, func(i, j int) bool { return want[i].X < want[j].X || want[i].X == want[j].X && want[i].Z < want[j].Z })
		have := append([]tile.Tile(nil), c.have...)
		sort.Slice(have, func(i, j int) bool { return have[i].X < have[j].X || have[i].X == have[j].X && have[i].Z < have[j].Z })
		if len(have) != len(want) {
			t.Errorf("%s: %d in the plan and %d in the maps' box", name, len(have), len(want))
			continue
		}
		for i := range have {
			if have[i] != want[i] {
				t.Errorf("%s: the plan has %v where the maps have %v", name, have[i], want[i])
			}
		}
	}
	// Everything the island legs of Jungle Potion fenced is fenced here too (the spiders and tribesmen west of x 2900).
	fenced := map[tile.Tile]bool{}
	for _, h := range TbwtHostiles() {
		fenced[h.Tile] = true
	}
	for _, h := range JungleHostiles() {
		if !fenced[h.Tile] {
			t.Errorf("Jungle Potion fences %v (npc %d) and this quest does not", h.Tile, h.NPC)
		}
	}
	if got, want := len(TbwtAvoid()), len(TbwtHostiles()); got != want || got < 80 {
		t.Errorf("%d rectangles for %d hostiles", got, want)
	}
}

// The beast and the Jogre are priced from the config.
func TestTbwtFoesAreTheContents(t *testing.T) {
	root := contentRoot(t)
	blocks := npcBlocks(t, root)
	beast := blocks["tbwt_beast"]
	if intOf(beast, "vislevel", 0) != 83 || beast["huntmode"] != "aggressive_melee" || intOf(beast, "huntrange", 0) != 8 || intOf(beast, "wanderrange", 0) != 32 ||
		intOf(beast, "size", 0) != 3 {
		t.Errorf("the Shaikahan's block: %v", beast)
	}
	// The cutscene's copy cannot be fought or fought by: no ops, no mode, no moving.
	cut := blocks["tbwt_beast_cutscene"]
	if cut["op2"] != "" || cut["defaultmode"] != "none" || cut["moverestrict"] != "nomove" {
		t.Errorf("the cutscene beast's block: %v", cut)
	}
	for _, name := range []string{"jungle_spider", "tribesman", "guarddog", "jogre"} {
		if got := blocks[name]["huntmode"]; got != "cowardly" {
			t.Errorf("%s hunts as %q, the fences are written for cowardly", name, got)
		}
	}
	for name, want := range map[string][2]int{"jungle_spider": {1, 3}, "guarddog": {1, 3}, "jogre": {2, 3}} {
		b := blocks[name]
		if intOf(b, "huntrange", 0) != want[0] || intOf(b, "wanderrange", 0) != want[1] {
			t.Errorf("%s: hunt range %d wander %d, the pads are written for %d and %d", name, intOf(b, "huntrange", 0), intOf(b, "wanderrange", 0), want[0], want[1])
		}
	}
	if b := blocks["tribesman"]; intOf(b, "huntrange", 0) != 1 || b["wanderrange"] != "" {
		t.Errorf("tribesman: %v (hunt range 1 and the default wander of 5 are what its pad is written for)", b)
	}
	if got := JungleTribesmanPad; got != 5+1+1 {
		t.Errorf("the tribesman's pad is %d", got)
	}
	if got := TbwtJogrePad; got != 3+2+1+1 {
		t.Errorf("the Jogre's pad is %d", got)
	}
}

// grep reports whether the file under the content checkout holds the text (CRLF folded).
func tbHas(t *testing.T, text string, rel ...string) bool {
	t.Helper()
	return strings.Contains(tbContentFile(t, rel...), text)
}

// The facts the design rests on, each in the script that makes it true.
func TestTbwtDesignFactsAreTheScripts(t *testing.T) {
	q := []string{"scripts", "quests", "quest_tbwt", "scripts"}
	at := func(file string) []string { return append(append([]string(nil), q...), file) }
	for _, c := range []struct {
		why  string
		file []string
		text string
	}{
		// There is no fight with the Shaikahan: the hunt is a cutscene, and the beast of it is not an attackable npc.
		{"the hunt is a cutscene that teleports the account away", at("tbwt_tamayu.rs2"), "p_teleport(^tbwt_player_cutscene_spawn)"},
		{"the killing hunt completes Tamayu from the cutscene itself", at("tbwt_tamayu.rs2"), "%tbwt_tamayu = ^tbwt_tamayu_complete;"},
		{"the account is returned beside Tamayu", at("tbwt_tamayu.rs2"), "p_teleport(0_44_47_32_31)"},
		{"potions and spears are taken only at the watched stage", at("tbwt_tamayu.rs2"), "if (%tbwt_tamayu ! ^tbwt_tamayu_watched_cutscene) {"},
		{"four doses are the count", at("tbwt_tamayu.rs2"), "if (~get_tbwt_tamayu_agility_count >= 4) {"},
		{"bronze spears do nothing", at("tbwt_tamayu.rs2"), "// Bronze spear does nothing"},
		// The monkey: melee and arrows are dodged, a cast is not an opnpc2.
		{"the monkey dodges attack-option blows", at("monkey.rs2"), "[opnpc2,monkey]"},
		{"the monkey drops its corpse to the killer while the quest runs", at("monkey.rs2"), "obj_add(npc_coord, tbwt_monkey_corpse, 1, ^lootdrop_duration);"},
		{"only Tamayu skins, and only once his hunt is over", at("quest_tbwt.rs2"), "[opheldu,tbwt_monkey_corpse]"},
		// Burning the bones: a tinderbox wants Firemaking 30, Superheat Item wants nothing but the runes.
		{"the tinderbox wants Firemaking 30", at("tbwt_jogre_bones.rs2"), "if (stat(firemaking) < 30) {"},
		{"Superheat Item burns the bones", at("tbwt_jogre_bones.rs2"), "inv_setslot(inv, last_slot, tbwt_burnt_jogre_bones, 1);"},
		{"Superheat Item takes four fire runes and a nature rune", at("tbwt_jogre_bones.rs2"), "inv_del(inv, naturerune, 1);"},
		// Lubufu.
		{"the brush-off queue is ten ticks", at("tbwt_lubufu.rs2"), "longqueue(tbwt_lubufu_queue, 10, 0, ^discard);"},
		{"Lubufu counts the Karambwanji in himself", at("tbwt_lubufu.rs2"), "def_int $amount  = min(inv_total(inv, tbwt_raw_karambwanji), $needed);"},
		{"the lost vessel is given again", at("tbwt_lubufu.rs2"), "inv_add(inv, tbwt_karambwan_vessel, 1);"},
		// Tiadeche.
		{"his first catch is a raw Karambwan", at("tbwt_tiadeche.rs2"), "inv_add(inv, tbwt_raw_karambwan, 1);"},
		{"his second gift needs the bit clear and no raw Karambwan in the pack", at("tbwt_tiadeche.rs2"), "if (inv_total(inv, tbwt_raw_karambwan) < 1) {"},
		{"the manual is taken by the talk", at("tbwt_tiadeche.rs2"), "@tbwt_tiadeche_give_crafting_manual;"},
		// Tinsay.
		{"a hand-in is a use-on at the stage", at("tbwt_tinsay.rs2"), "case tbwt_sliced_banana_in_karamja_rum : if (%tbwt_tinsay = ^tbwt_tinsay_fetch_bananarum)"},
		{"the vessel is given to Tinsay only when his deliveries are done", at("tbwt_tinsay.rs2"), "if (%tbwt_tinsay ! ^tbwt_tinsay_complete) {"},
		// The fishing.
		{"Karambwan fishing is Fishing 65", []string{"scripts", "skill_fishing", "scripts", "fishing_spots", "tbwt.rs2"}, "if (stat(fishing) < 65) {"},
		{"the net is Fishing 5", []string{"scripts", "skill_fishing", "scripts", "fishing_spots", "tbwt.rs2"}, "if (stat(fishing) < 5) {"},
		// The marinade cannot burn and wants Cooking 30.
		{"the marinade is certain", []string{"scripts", "skill_cooking", "configs", "cooking_source", "cooking_generic.dbrow"}, "data=uncooked,tbwt_burnt_jogre_bones_in_raw_karambwanji_paste"},
		{"the marinade wants Cooking 30", []string{"scripts", "skill_cooking", "configs", "cooking_source", "cooking_generic.dbrow"},
			"data=levelrequired_message,You need a Cooking level of 30 to cook burnt jogre bones covered in raw karambwanji paste."},
		{"the Karambwan roll is 70 in 255", []string{"scripts", "skill_cooking", "configs", "cooking_source", "cooking_generic.dbrow"}, "data=successchance,70,255"},
		{"Superheat Item is Magic 43", []string{"scripts", "skill_magic", "configs", "magic_spells.dbrow"}, "data=levelrequired,43"},
		// Rum.
		{"the customs officer takes the rum", []string{"scripts", "areas", "area_karamja", "scripts", "customs_officer.rs2"}, "The customs officer confiscates your rum."},
		// The Jogre.
		{"a Jogre always drops its bones", []string{"scripts", "drop tables", "scripts", "jogre.rs2"}, "obj_add(npc_coord, npc_param(death_drop), 1, ^lootdrop_duration);"},
		{"a Jogre drops an iron spear 4 in 129", []string{"scripts", "drop tables", "scripts", "jogre.rs2"}, "} else if ($random < 34) {"},
		// The start.
		{"the start wants Jungle Potion complete", []string{"scripts", "areas", "area_karamja", "scripts", "tbwt_timfraku.rs2"}, "if (%junglepotion >= ^junglepotion_complete) {"},
		{"the reward is 2,000 coins", []string{"scripts", "areas", "area_karamja", "scripts", "tbwt_timfraku.rs2"}, "inv_add(inv, coins, 2000);"},
	} {
		if !tbHas(t, c.text, c.file...) {
			t.Errorf("%s: %s has no %q", c.why, filepath.Join(c.file...), c.text)
		}
	}
	// No script answers a spell cast at a monkey: the dodge is in [opnpc2,monkey] alone.
	root := contentRoot(t)
	n := 0
	err := filepath.Walk(filepath.Join(root, "scripts"), func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".rs2") {
			return err
		}
		raw, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		if strings.Contains(string(raw), "[opnpct,monkey]") || strings.Contains(string(raw), "[apnpct,monkey]") {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d scripts answer a spell cast at the monkey: the Wind Bolt plan is wrong", n)
	}
}

// Every option the stops prefer is a line of the script the stop talks to (the strings are matched as substrings of the menus).
func TestTbwtPreferencesAreTheMenus(t *testing.T) {
	scripts := map[string]string{
		"Timfraku": tbContentFile(t, "scripts", "areas", "area_karamja", "scripts", "tbwt_timfraku.rs2"),
		"Lubufu":   tbContentFile(t, "scripts", "quests", "quest_tbwt", "scripts", "tbwt_lubufu.rs2"),
		"Tiadeche": tbContentFile(t, "scripts", "quests", "quest_tbwt", "scripts", "tbwt_tiadeche.rs2"),
		"Tamayu":   tbContentFile(t, "scripts", "quests", "quest_tbwt", "scripts", "tbwt_tamayu.rs2"),
		"Tinsay":   tbContentFile(t, "scripts", "quests", "quest_tbwt", "scripts", "tbwt_tinsay.rs2"),
	}
	for _, stop := range []NpcStop{TbTimfrakuStartStop, TbTimfrakuRewardStop, TbLubufuMeetStop, TbLubufuGiveStop, TbLubufuAskStop, TbLubufuVesselStop,
		TbTiadecheStop, TbTamayuStop, TbTinsayStop} {
		src := scripts[stop.Npc]
		if src == "" {
			t.Fatalf("no script for %s", stop.Npc)
		}
		if stop.ID == 0 || stop.Leash <= 0 || len(stop.Prefer) == 0 {
			t.Errorf("%s: a malformed stop %+v", stop.Npc, stop)
		}
		for _, p := range stop.Prefer {
			// An option may open with an ellipsis ("... a shark ate it!"), and the preference is matched as a substring of it.
			if !strings.Contains(src, `"`+p) && !strings.Contains(src, `"... `+p) {
				t.Errorf("%s prefers %q and its script has no such option or line", stop.Npc, p)
			}
		}
	}
}
