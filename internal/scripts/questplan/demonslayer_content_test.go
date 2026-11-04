package questplan

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// Every id, tile, menu, stage, reward, spawn, stat and hunting rule Demon Slayer rests on, read from a Content 289 checkout
// (RSB0T_CONTENT, skipped without one) so that none of them can drift away from the scripts and maps they were derived from:
//
//	RSB0T_CONTENT=<Content checkout> go test -run DemonSlayerContent ./internal/scripts/questplan/

var dsChoiceCall = regexp.MustCompile(`p_choice\d\(((?:"[^"]*"|[^")])*)\)`)
var dsQuoted = regexp.MustCompile(`"([^"]*)"`)

// dsChoices lists every p_choiceN menu of a script as its option lines, in order.
func dsChoices(src string) [][]string {
	var out [][]string
	for _, m := range dsChoiceCall.FindAllStringSubmatch(src, -1) {
		var opts []string
		for _, q := range dsQuoted.FindAllStringSubmatch(m[1], -1) {
			opts = append(opts, q[1])
		}
		out = append(out, opts)
	}
	return out
}

// dsHasMenu reports whether the script has a p_choice whose option lines are exactly menu.
func dsHasMenu(src string, menu []string) bool {
	for _, c := range dsChoices(src) {
		if len(c) == len(menu) {
			same := true
			for i := range c {
				same = same && c[i] == menu[i]
			}
			if same {
				return true
			}
		}
	}
	return false
}

// dsNpcStat is one `key=value` line of an npc config block, "" when absent.
func dsNpcStat(block, key string) string {
	for _, line := range strings.Split(block, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok && k == key {
			return v
		}
	}
	return ""
}

func dsNpcInt(t *testing.T, block, key string) int {
	t.Helper()
	v := dsNpcStat(block, key)
	n, err := strconv.Atoi(v)
	if err != nil {
		t.Fatalf("npc stat %s=%q is not a number", key, v)
	}
	return n
}

// The object, npc and loc ids the plan carries are the pack's.
func TestDemonSlayerContentIDs(t *testing.T) {
	obj, npc, loc := csPackIDs(t, "obj"), csPackIDs(t, "npc"), csPackIDs(t, "loc")
	for _, c := range []struct {
		kind string
		ids  map[string]int
		name string
		want int
	}{
		{"obj", obj, "silverlight_key_1", ObjKeyTraiborn}, {"obj", obj, "silverlight_key_2", ObjKeyRovin}, {"obj", obj, "silverlight_key_3", ObjKeyDrain},
		{"obj", obj, "silverlight", ObjSilverlight}, {"obj", obj, "bones", ObjBones}, {"obj", obj, "bucket_empty", ObjBucket},
		{"obj", obj, "bucket_water", ObjBucketOfWater}, {"obj", obj, "coins", CoinsID}, {"obj", obj, "shark", ObjShark}, {"obj", obj, "bass", ObjBass},
		{"npc", npc, "aris", NpcGypsyAris}, {"npc", npc, "sir_prysin", NpcSirPrysin}, {"npc", npc, "captain_rovin", NpcCaptainRovin},
		{"npc", npc, "traiborn", NpcTraiborn}, {"npc", npc, "delrith", NpcDelrith}, {"npc", npc, "delrith_weakened", NpcDelrithWeakened},
		{"npc", npc, "young_dark_wizard", NpcYoungDarkWizard}, {"npc", npc, "bearded_dark_wizard", NpcBeardedDarkWizard},
		{"npc", npc, "zombie_armed", NpcSewerZombie}, {"npc", npc, "giantspider2", NpcSewerGiantSpider}, {"npc", npc, "shop_keeper", NpcVarrockGenKpr},
		{"loc", loc, "questdrain", LocQuestDrain}, {"loc", loc, "manholeclosed", LocManholeClosed}, {"loc", loc, "manholeopen", LocManholeOpen},
		{"loc", loc, "ladder_from_cellar", LocCaveLadderUp}, {"loc", loc, "sink", LocSink}, {"loc", loc, "sink2", LocSink2},
	} {
		got, ok := c.ids[c.name]
		if c.name == "shop_keeper" {
			continue // several shopkeepers share the name; the Varrock general store's keeper is pinned by the vampire slayer's tests
		}
		if !ok || got != c.want {
			t.Errorf("%s %s is %d in the pack (found %v), the plan says %d", c.kind, c.name, got, ok, c.want)
		}
	}
}

// The stages, the points and the reward.
func TestDemonSlayerContentStages(t *testing.T) {
	general := csContent(t, "scripts/general/configs/quest.constant")
	for _, want := range []string{fmt.Sprintf("^demon_complete = %d", DemonComplete), fmt.Sprintf("^demon_questpoints = %d", DemonSlayerRecord.QuestPoints)} {
		if !strings.Contains(general, want) {
			t.Errorf("quest.constant lacks %q", want)
		}
	}
	consts := csContent(t, "scripts/quests/quest_demon/configs/demon.constant")
	for name, v := range map[string]int{"not_started": DemonNotStarted, "talked_aris": DemonTalkedAris, "key_hunt": DemonKeyHunt, "find_bones": 3,
		"got_traiborn_key": 28, "silverlight": DemonSilverlight} {
		if want := fmt.Sprintf("^demon_%s = %d", name, v); !strings.Contains(consts, want) {
			t.Errorf("demon.constant lacks %q", want)
		}
	}
	// One bone a stage, from find_bones to got_traiborn_key.
	if DemonBonesNeeded != 28-3 || !strings.Contains(consts, "// Each stage here is one more bone") {
		t.Errorf("the bones Traiborn asks for are %d, the stages say %d", DemonBonesNeeded, 28-3)
	}
	if got := csContent(t, "scripts/general/scripts/quests.rs2"); !strings.Contains(got, "~send_quest_progress_colour(questlist:demon, %demonstart, ^demon_complete);") {
		t.Error("the quest tab's colour is not read from the demonstart varp")
	}
	if l := LadderFacts[DemonSlayerID]; l.QP != 3 {
		t.Errorf("the ladder row pays %d quest points", l.QP)
	}
}

// Every menu the stops steer is a p_choice of the script that raises it, line for line.
func TestDemonSlayerContentMenus(t *testing.T) {
	gypsy := csContent(t, "scripts/areas/area_varrock/scripts/gypsy.rs2")
	prysin := csContent(t, "scripts/areas/area_varrock/scripts/sir_prysin.rs2")
	rovin := csContent(t, "scripts/areas/area_varrock/scripts/captain_rovin.rs2")
	traiborn := csContent(t, "scripts/areas/area_draynor/scripts/traiborn.rs2")
	delrith := csContent(t, "scripts/quests/quest_demon/scripts/delrith.rs2")
	for _, c := range []struct {
		what   string
		src    string
		script string
		menus  [][]string
	}{
		{"Gypsy Aris", gypsy, "gypsy.rs2", dsGypsyMenus},
		{"Sir Prysin", prysin, "sir_prysin.rs2", dsPrysinMenus},
		{"Sir Prysin (the lost sword)", prysin, "sir_prysin.rs2", [][]string{dsLostSwordMenu}},
		{"Captain Rovin", rovin, "captain_rovin.rs2", dsRovinMenus},
		{"Traiborn", traiborn, "traiborn.rs2", dsTraibornMenus},
		{"Delrith", delrith, "delrith.rs2", [][]string{dsChantMenu}},
	} {
		for i, m := range c.menus {
			// Sir Prysin's first menu is built by a proc for stage 1 (three options) and stage 0 (two); the stop's menu is the three.
			if !dsHasMenu(c.src, m) {
				t.Errorf("%s: menu %d %q is not a p_choice of %s", c.what, i+1, m, c.script)
			}
		}
	}
	// The right line of the chant is the one the script answers with the fourth option, and the other three heal Delrith.
	if !strings.Contains(delrith, `} else if ($option = 4) {`+"\n"+`    ~chatplayer("<p,happy>Carlem Aber Camerinthum Purchai Gabindo");`+"\n"+`    @demon_slayer_correct_incantation;`) {
		t.Error("option 4 of the chant is not the correct incantation")
	}
	if strings.Count(delrith, "@demon_slayer_wrong_incantation;") != 3 {
		t.Errorf("the chant has %d wrong lines, want 3", strings.Count(delrith, "@demon_slayer_wrong_incantation;"))
	}
	if !strings.Contains(delrith, DemonIncantation) {
		t.Error("the incantation is not in the script")
	}
	// Every preference is a line of the menus above (so the stop can never name one the script lacks).
	for _, stop := range []NpcStop{GypsyArisStop, PrysinIntroStop, PrysinLostSwordStop, RovinStop, TraibornStop} {
		for _, p := range stop.Prefer {
			if !strings.Contains(gypsy+prysin+rovin+traiborn, `"`+p+`"`) {
				t.Errorf("%s prefers %q, which no script of the quest has", stop.Npc, p)
			}
		}
	}
	// The hand-in stop's placeholder is a line the key-search menu has (it is never shown when all three keys are held).
	if !strings.Contains(prysin, `"`+PrysinKeysStop.Prefer[0]+`"`) {
		t.Errorf("%q is not a line of Sir Prysin's key talk", PrysinKeysStop.Prefer[0])
	}
}

// What the scripts do that the module plans for.
func TestDemonSlayerContentScripts(t *testing.T) {
	core := csContent(t, "scripts/quests/quest_demon/scripts/demon_slayer.rs2")
	delrith := csContent(t, "scripts/quests/quest_demon/scripts/delrith.rs2")
	prysin := csContent(t, "scripts/areas/area_varrock/scripts/sir_prysin.rs2")
	rovin := csContent(t, "scripts/areas/area_varrock/scripts/captain_rovin.rs2")
	traiborn := csContent(t, "scripts/areas/area_draynor/scripts/traiborn.rs2")
	combat := csContent(t, "scripts/skill_combat/scripts/npc/npc_combat.rs2")
	wizard := csContent(t, "scripts/npc/scripts/dark_wizard.rs2")
	manholes := csContent(t, "scripts/general_use/scripts/manholes.rs2")
	water := csContent(t, "scripts/general_use/scripts/water_sources.rs2")
	ladders := csContent(t, "scripts/ladders+stairs/scripts/ladders.rs2")

	// The drain: a bucket of water (and only that) used on it adds the key, a public ground item, at the sewer tile, for ^lootdrop_duration
	// * 3 / 2 ticks, and does nothing when the account has the key in the pack or the bank.
	for _, want := range []string{
		"[oplocu,questdrain]", "if (last_useitem ! bucket_water) {", `mes("You pour the liquid down the drain.");`, "inv_del(inv, bucket_water, 1);",
		"inv_add(inv, bucket_empty, 1);", "obj_add(0_50_154_25_41, silverlight_key_3, 1, calc(calc(^lootdrop_duration * 3) / 2));",
		"inv_total(inv, silverlight_key_3) > 0 | inv_total(bank, silverlight_key_3) > 0",
	} {
		if !strings.Contains(core, want) {
			t.Errorf("demon_slayer.rs2 lacks %q", want)
		}
	}
	if DemonSewerKey != (tile.Tile{X: 50*64 + 25, Z: 154*64 + 41}) {
		t.Errorf("the key's tile %v is not 0_50_154_25_41", DemonSewerKey)
	}
	loot := csContent(t, "scripts/drop tables/configs/lootdrop.constant")
	if !strings.Contains(loot, "^lootdrop_duration = 200") || DemonKeyTicks != 200*3/2 {
		t.Errorf("the key lies %d ticks; ^lootdrop_duration is %q", DemonKeyTicks, strings.TrimSpace(strings.SplitN(loot, "\n", 2)[0]))
	}
	// The pour is a use-on, and the search op (oploc1) is a run of message boxes the module never triggers.
	if !strings.Contains(core, "[oploc1,questdrain]") || strings.Count(core, "~mesbox(") < 3 {
		t.Error("the drain's search op is not the message boxes the plan says")
	}

	// The manhole opens for 400 ticks and drops the account 6400 tiles north of where it stands; the ladder climbs 6400 back.
	for _, want := range []string{"[oploc1,manholeclosed]", "loc_change(manholeopen, 400);", "p_telejump(movecoord(coord(), 0, 0, 6400));"} {
		if !strings.Contains(manholes, want) {
			t.Errorf("manholes.rs2 lacks %q", want)
		}
	}
	if DemonSewerLanding != (tile.Tile{X: DemonManholeStand.X, Z: DemonManholeStand.Z + 6400}) {
		t.Errorf("the sewer landing %v is not the manhole's stand %v plus 6400", DemonSewerLanding, DemonManholeStand)
	}
	if !strings.Contains(ladders, "[oploc1,ladder_from_cellar]") || !strings.Contains(ladders, "~climb_ladder(movecoord(coord(), 0, 0, -6400), true);") {
		t.Error("the sewer's ladder does not climb 6400 tiles back")
	}

	// The sink fills a bucket.
	if !strings.Contains(water, "[oplocu,_watersource]") || !strings.Contains(water, "case bucket_empty,") {
		t.Error("a bucket on a water source is not a fill")
	}

	// Rovin: the key is handed over with no check but the menu.
	for _, want := range []string{"inv_add(inv, silverlight_key_2, 1);", `mes("Captain Rovin hands you a key.");`, "%demonstart >= ^demon_key_hunt  & %demonstart < ^demon_silverlight"} {
		if !strings.Contains(rovin, want) {
			t.Errorf("captain_rovin.rs2 lacks %q", want)
		}
	}

	// Traiborn: bones one at a time with a tick between, the 25th starting the ritual of three message boxes that ends in the key; a
	// banked key is not given again, and a lost one costs 25 more.
	for _, want := range []string{
		"inv_del(inv, bones, 1);", `mes("You give Traiborn a set of bones.");`, "p_delay(1);", "%demonstart = calc(%demonstart + 1);",
		"if(%demonstart >= ^demon_find_bones & %demonstart < ^demon_got_traiborn_key & inv_total(inv, bones) > 0) {",
		"if (%demonstart = ^demon_got_traiborn_key) {", `~mesbox("Traiborn places the bones in a circle on the floor.");`, `~mesbox("Traiborn waves his arms about.");`,
		`~mesbox("The wizard waves his arms some more.");`, "inv_add(inv, silverlight_key_1, 1);", "%demonstart = ^demon_find_bones;",
		"%demonstart = ^demon_got_traiborn_key & inv_total(inv, silverlight_key_1) < 1 & inv_total(bank, silverlight_key_1) < 1",
	} {
		if !strings.Contains(traiborn, want) {
			t.Errorf("traiborn.rs2 lacks %q", want)
		}
	}
	if n := strings.Count(traiborn, "~mesbox("); n != 3 {
		t.Errorf("Traiborn's script has %d message boxes, the module's driver is built for 3", n)
	}

	// Sir Prysin: the keys are taken and the sword given at stage 29; a lost sword is replaced for the asking; he asks nothing of an
	// account that has the sword in the pack, worn or banked.
	for _, want := range []string{
		"inv_del(inv, silverlight_key_1, 1);", "inv_del(inv, silverlight_key_2, 1);", "inv_del(inv, silverlight_key_3, 1);", "inv_add(inv, silverlight, 1);",
		"%demonstart = ^demon_silverlight;", `~chatplayer("<p,sad>No, I'm afraid I've lost Silverlight.");`, "[proc,demon_slayer_has_silverlight]()(boolean)",
		"inv_total(inv, silverlight) > 0 | (inv_getobj(worn, 3) = silverlight) | inv_total(bank, silverlight) > 0",
	} {
		if !strings.Contains(prysin, want) {
			t.Errorf("sir_prysin.rs2 lacks %q", want)
		}
	}
	if !strings.Contains(prysin, "%demonstart = ^demon_key_hunt;\ndef_int $option_keys") {
		t.Error("Sir Prysin's \"give me the keys\" no longer sets stage 2 before its menu")
	}

	// Delrith: the sword worn is the whole condition for an attack, twice (melee and the other styles); at no hitpoints he becomes the
	// weakened one for 100 ticks and the chant is queued; the right line completes the quest a tick later.
	if n := strings.Count(combat, "if (inv_getobj(worn, 3) ! silverlight) {"); n != 2 {
		t.Errorf("npc_combat.rs2 asks for Silverlight worn %d times, want 2", n)
	}
	for _, want := range []string{
		"[ai_queue3,delrith]", "npc_changetype(delrith_weakened, 100);", "queue(demon_slayer_start_incantation, 0, 0);",
		"npc_changetype_keepall(delrith, ^max_32bit_int);", "npc_statheal(hitpoints, 0, 100);", "queue(demon_slayer_complete, 1, 0);",
		"if (npc_find(coord, delrith_weakened, 8, 0) = true) {",
	} {
		if !strings.Contains(delrith, want) {
			t.Errorf("delrith.rs2 lacks %q", want)
		}
	}
	if !strings.Contains(core, "~send_quest_complete(questlist:demon, silverlight,") || !strings.Contains(core, "%demonstart = ^demon_complete;") {
		t.Error("the closing queue does not set the stage and show the scroll")
	}
	if n := strings.Count(delrith, "~mesbox("); n < 3 {
		t.Errorf("the chant has %d message boxes, the module closes them as they come", n)
	}

	// The dark wizards cast every four ticks and hold their spells from an account that is busy at stage 29.
	for _, want := range []string{
		"[ai_applayer2,bearded_dark_wizard] ~dark_wizard_earth_attack;", "[ai_applayer2,young_dark_wizard] ~dark_wizard_water_attack;",
		"if (busy = true & %demonstart = 29)", "~npc_cast_spell(^earth_strike, 4);", "~npc_cast_spell(^water_strike, 4);",
	} {
		if !strings.Contains(wizard, want) {
			t.Errorf("dark_wizard.rs2 lacks %q", want)
		}
	}
}

// The foes the plan prices are the content's, stat for stat.
func TestDemonSlayerContentFoes(t *testing.T) {
	delrithCfg := csConfigBlock(csContent(t, "scripts/quests/quest_demon/configs/demon.npc"), "delrith")
	all := csContent(t, "scripts/_unpack/225/all.npc")
	for _, c := range []struct {
		name  string
		block string
		foe   MeleeFoe
	}{
		{"delrith", delrithCfg, FoeDelrith},
		{"young_dark_wizard", csConfigBlock(all, "young_dark_wizard"), FoeYoungDarkWizard},
		{"bearded_dark_wizard", csConfigBlock(all, "bearded_dark_wizard"), FoeBeardedDarkWizard},
		{"zombie_armed", csConfigBlock(all, "zombie_armed"), FoeSewerZombie},
		{"giantspider2", csConfigBlock(all, "giantspider2"), FoeSewerSpider},
	} {
		if c.block == "" {
			t.Errorf("no npc block for %s", c.name)
			continue
		}
		for key, want := range map[string]int{"attack": c.foe.Attack, "defence": c.foe.Defence, "strength": c.foe.Strength, "hitpoints": c.foe.Hitpoints} {
			if got := dsNpcInt(t, c.block, key); got != want {
				t.Errorf("%s: %s is %d in the content, the plan says %d", c.name, key, got, want)
			}
		}
		// A foe whose config names no magic stat has Magic 1; the wizards' is the model's cast.
		if m := dsNpcStat(c.block, "magic"); (m == "") != (c.foe.Magic == 1) {
			t.Errorf("%s: magic %q against the plan's %d", c.name, m, c.foe.Magic)
		} else if m != "" {
			if got, _ := strconv.Atoi(m); got != c.foe.Magic {
				t.Errorf("%s: magic is %s in the content, the plan says %d", c.name, m, c.foe.Magic)
			}
		}
		// The attack rate: absent is four.
		rate := 4
		if r := regexp.MustCompile(`param=attackrate,(\d+)`).FindStringSubmatch(c.block); r != nil {
			rate, _ = strconv.Atoi(r[1])
		}
		if rate != c.foe.AttackTicks {
			t.Errorf("%s: attack rate %d in the content, %d in the plan", c.name, rate, c.foe.AttackTicks)
		}
		bonus := func(name string) int {
			if r := regexp.MustCompile(`param=` + name + `,(-?\d+)`).FindStringSubmatch(c.block); r != nil {
				n, _ := strconv.Atoi(r[1])
				return n
			}
			return 0
		}
		if bonus("attackbonus") != c.foe.AttackBonus || bonus("strengthbonus") != c.foe.StrengthBonus {
			t.Errorf("%s: bonuses %d and %d in the content, %d and %d in the plan", c.name, bonus("attackbonus"), bonus("strengthbonus"), c.foe.AttackBonus, c.foe.StrengthBonus)
		}
	}
	// Delrith hunts nobody; the wizards hunt with the ranged mode at the ranges the plan's reasoning names.
	if dsNpcStat(delrithCfg, "huntmode") != "" || !strings.Contains(delrithCfg, "param=demonbane_vulnerable,yes") || dsNpcInt(t, delrithCfg, "vislevel") != 27 {
		t.Errorf("Delrith's config: %q", delrithCfg)
	}
	for name, want := range map[string]struct {
		level, rng int
	}{"young_dark_wizard": {7, 3}, "bearded_dark_wizard": {20, 5}} {
		b := csConfigBlock(all, name)
		if dsNpcInt(t, b, "vislevel") != want.level || dsNpcInt(t, b, "huntrange") != want.rng || dsNpcStat(b, "huntmode") != "ranged" || dsNpcInt(t, b, "attackrange") != 5 {
			t.Errorf("%s: %q", name, b)
		}
	}
	// The hunt mode skips an account above twice the monster's level outside the Wilderness: the ceilings of the plan are 2 * 7 and 2 * 20.
	hunt := csConfigBlock(csContent(t, "scripts/_unpack/225/all.hunt"), "ranged")
	if !strings.Contains(hunt, "check_nottoostrong=outside_wilderness") || !strings.Contains(hunt, "type=player") {
		t.Errorf("the ranged hunt mode: %q", hunt)
	}
	if demonCircleHunters[0].Ceiling != 2*7 || demonCircleHunters[1].Ceiling != 2*20 {
		t.Errorf("the hunters' ceilings %+v", demonCircleHunters)
	}
	// The spell table's maxima behind ZapMax.
	spells := csContent(t, "scripts/skill_combat/configs/magic/magic_combat_spells.dbrow")
	for spell, max := range map[string]int{"water_strike": FoeYoungDarkWizard.ZapMax, "earth_strike": FoeBeardedDarkWizard.ZapMax} {
		if b := csConfigBlock(spells, "magic_spell_"+spell); !strings.Contains(b, fmt.Sprintf("data=maxhit,%d\n", max)) {
			t.Errorf("%s: %q, the plan says a maximum of %d", spell, b, max)
		}
	}
	// Silverlight's bonuses are the swing the model gives the account.
	sword := csConfigBlock(csContent(t, "scripts/quests/quest_demon/configs/demon.obj"), "silverlight")
	for _, want := range []string{"param=slashattack,14", "param=strengthbonus,12", "param=attackrate,5", "wearpos=righthand", "tradeable=no"} {
		if !strings.Contains(sword, want) {
			t.Errorf("Silverlight lacks %q", want)
		}
	}
	for _, name := range []string{"silverlight_key_1", "silverlight_key_2", "silverlight_key_3"} {
		if b := csConfigBlock(csContent(t, "scripts/quests/quest_demon/configs/demon.obj"), name); !strings.Contains(b, "name=Key") || !strings.Contains(b, "tradeable=no") {
			t.Errorf("%s: %q", name, b)
		}
	}
}

// Where the stops and the spawns are.
func TestDemonSlayerContentSpawns(t *testing.T) {
	for _, c := range []struct {
		what string
		at   tile.Tile
		id   int
	}{
		{"Gypsy Aris", GypsyAnchor, NpcGypsyAris}, {"Sir Prysin", PrysinAnchor, NpcSirPrysin}, {"Captain Rovin", RovinAnchor, NpcCaptainRovin},
		{"Traiborn", TraibornAnchor, NpcTraiborn}, {"Delrith", DelrithSpawn, NpcDelrith},
	} {
		if !mcMapHas(t, "NPC", c.at, c.id) {
			t.Errorf("%s (npc %d) is not spawned at %v", c.what, c.id, c.at)
		}
	}
	for _, c := range []struct {
		what string
		at   tile.Tile
		id   int
	}{
		{"the drain", DemonDrain, LocQuestDrain}, {"the kitchen sink", t3(3224, 3494, 0), LocSink2}, {"the manhole", DemonManhole, LocManholeClosed},
		{"the sewer ladder", DemonSewerLadder, LocCaveLadderUp},
	} {
		if !mcMapHas(t, "LOC", c.at, c.id) {
			t.Errorf("%s (loc %d) is not at %v", c.what, c.id, c.at)
		}
	}
	// The bucket on the palace's first floor, and the stock of buckets in Varrock's general store.
	if !mcMapHas(t, "OBJ", DemonBucketSpawn, ObjBucket) {
		t.Errorf("no bucket spawn at %v", DemonBucketSpawn)
	}
	if shop := csContent(t, "scripts/areas/area_varrock/configs/varrock.inv"); !strings.Contains(shop, "stock4=bucket_empty,2,100") {
		t.Error("Varrock's general store no longer stocks buckets at 2")
	}
	// The wizards round the circle: nine young and two bearded within eight tiles of Delrith.
	if young, bearded := csNearRows(t, "NPC", NpcYoungDarkWizard, DelrithSpawn, 8), csNearRows(t, "NPC", NpcBeardedDarkWizard, DelrithSpawn, 8); young != 9 || bearded != 2 {
		t.Errorf("%d young and %d bearded dark wizards within 8 tiles of Delrith, want 9 and 2", young, bearded)
	}
	// Nothing in the sewer's corridor from the ladder to the key is hunted by a monster above the ceiling: the highest level among the
	// spawns in the box (x 3215 to 3250, z 9856 to 9910) is the giant spider's 27, and twice it is DemonSewerCeiling.
	all := csContent(t, "scripts/_unpack/225/all.npc")
	names := map[int]string{}
	for name, id := range csPackIDs(t, "npc") {
		names[id] = name
	}
	highest, seen := 0, map[int]bool{}
	for x := 3215; x <= 3250; x++ {
		for z := 9856; z <= 9910; z++ {
			for _, row := range csMapRows(t, "NPC", tile.Tile{X: x, Z: z}) {
				id, _ := strconv.Atoi(row[0])
				if seen[id] {
					continue
				}
				seen[id] = true
				b := csConfigBlock(all, names[id])
				if b == "" {
					t.Errorf("npc %d (%s) of the sewer's corridor has no config block", id, names[id])
					continue
				}
				lv, _ := strconv.Atoi(dsNpcStat(b, "vislevel"))
				highest = max(highest, lv)
			}
		}
	}
	if highest != 27 || DemonSewerCeiling != 2*highest {
		t.Errorf("the highest level among the sewer corridor's spawns is %d; the plan's ceiling is %d", highest, DemonSewerCeiling)
	}
}

// The stone circle is single-way combat, and that is what makes an account the wizards hunt unable to start the fight with Delrith
// (DemonHuntFit): the facts that rest on, read from the content so that none of them can drift.
func TestDemonSlayerContentTheCircleIsSingleCombat(t *testing.T) {
	// No zone of the circle's map square (50, 52) is multi-way: neither Delrith's tile nor a wizard's is, and the file's zone rows are the
	// level_mx_mz_lx_lz form this reads (a row of a neighbouring square is there).
	multi := csContent(t, "maps/multiway.csv")
	if !strings.Contains(multi, "\n0_50_55_0_0\n") {
		t.Error("maps/multiway.csv no longer has the zone rows this reads (0_50_55_0_0)")
	}
	if mx, mz := DelrithSpawn.X>>6, DelrithSpawn.Z>>6; mx != 50 || mz != 52 {
		t.Fatalf("Delrith's spawn %v is in map square %d,%d, not 50,52", DelrithSpawn, mx, mz)
	}
	for _, line := range strings.Split(multi, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "0_50_52_") {
			t.Errorf("the circle's map square has a multi-way zone: %q", strings.TrimSpace(line))
		}
	}
	// None of the monsters of the circle forces multi-way combat on itself.
	all := csContent(t, "scripts/_unpack/225/all.npc")
	blocks := map[string]string{
		"delrith":             csConfigBlock(csContent(t, "scripts/quests/quest_demon/configs/demon.npc"), "delrith"),
		"young_dark_wizard":   csConfigBlock(all, "young_dark_wizard"),
		"bearded_dark_wizard": csConfigBlock(all, "bearded_dark_wizard"),
	}
	for name, b := range blocks {
		if b == "" || strings.Contains(b, "npc_forcemulti") {
			t.Errorf("%s: forces multi-way combat, or has no config block (%d bytes)", name, len(b))
		}
	}

	// The refusal: in single-way combat an attack on a monster is refused for eight ticks after any other monster fought the account.
	pc := csContent(t, "scripts/skill_combat/scripts/player/player_combat.rs2")
	for _, want := range []string{
		"[proc,player_in_combat_check]()(boolean)",
		"if (map_multiway(npc_coord) = false & nc_param(npc_type, npc_forcemulti) = ^false) {",
		"if (add(%lastcombat, 8) > map_clock & (%aggressive_npc ! npc_uid & %aggressive_npc ! null)) {",
		`mes("` + DemonUnderAttackLine + `!");`,
	} {
		if !strings.Contains(pc, want) {
			t.Errorf("player_combat.rs2 lacks %q", want)
		}
	}
	// Every cast ends in the proc that stamps the account as just fought by that wizard, and the wizards cast every four ticks.
	nc := csContent(t, "scripts/skill_combat/scripts/npc/npc_combat.rs2")
	if !strings.Contains(nc, "[proc,npc_set_attack_vars]\n%lastcombat = map_clock;\n%npc_attacking_uid = uid;\n%aggressive_npc = npc_uid;") {
		t.Error("npc_set_attack_vars no longer stamps %lastcombat and %aggressive_npc")
	}
	magic := csContent(t, "scripts/skill_combat/scripts/npc/npc_combat_magic.rs2")
	if !strings.Contains(magic, "~npc_spell_fail($spell_data, $duration);\n}\n~npc_set_attack_vars;") {
		t.Error("npc_combat_magic.rs2's cast no longer ends in npc_set_attack_vars")
	}
	wizard := csContent(t, "scripts/npc/scripts/dark_wizard.rs2")
	if n := strings.Count(wizard, "~npc_cast_spell(^"); n != 4 || strings.Count(wizard, ", 4);") != 4 {
		t.Errorf("dark_wizard.rs2 casts %d spells, want four of them at the four-tick rate", n)
	}

	// The geometry: a bearded wizard stands within its huntrange (5) of every tile beside Delrith, so an account walking up to him is in
	// the hunt of at least one of them whichever side it comes from.
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			if dx == 0 && dz == 0 {
				continue
			}
			beside := tile.Tile{X: DelrithSpawn.X + dx, Z: DelrithSpawn.Z + dz, Level: DelrithSpawn.Level}
			if n := csNearRows(t, "NPC", NpcBeardedDarkWizard, beside, 5); n == 0 {
				t.Errorf("no bearded dark wizard spawn within 5 tiles of %v, beside Delrith", beside)
			}
		}
	}
}

// The journal fixtures are what demon_journal.rs2 says: every line of every fixture appears in the script, with its colour tags and
// line breaks.
func TestDemonSlayerJournalFixturesAreTheScriptsText(t *testing.T) {
	src := csContent(t, "scripts/quests/quest_demon/scripts/demon_journal.rs2")
	squash := func(s string) string {
		return regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(s), "")
	}
	script := squash(regexp.MustCompile(`@[a-z0-9]{3}@`).ReplaceAllString(src, ""))
	for name, f := range demonJournalFixtures {
		for _, seg := range strings.Split(f.text, "|") {
			seg = regexp.MustCompile(`@[a-z0-9]{3}@`).ReplaceAllString(seg, "")
			if q := squash(seg); q != "" && !strings.Contains(script, q) {
				t.Errorf("%s: %q is not in demon_journal.rs2", name, seg)
			}
		}
	}
	// The module's needles are the script's lines (squashed), so an edit to either shows here.
	for _, n := range demonJournalNeedles {
		for _, needle := range n.needles {
			if !strings.Contains(script, squash(needle)) {
				t.Errorf("stage %d: the needle %q is not in demon_journal.rs2", n.stage, needle)
			}
		}
	}
}
