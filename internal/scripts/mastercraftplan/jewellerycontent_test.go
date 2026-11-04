package mastercraftplan

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// The facts the jewellery round rests on, read from a Content checkout (RSB0T_CONTENT, skipped
// without one), so that none of them can drift away from the scripts they were derived from.

// contentRoot reads a file under the Content checkout's root (the pack files), not under scripts/.
func contentRoot(t *testing.T, rel string) string {
	t.Helper()
	root := os.Getenv("RSB0T_CONTENT")
	if root == "" {
		t.Skip("set RSB0T_CONTENT to a Content checkout to check the jewellery round's facts against it")
	}
	b, err := os.ReadFile(strings.TrimRight(root, "/\\") + "/" + rel)
	if err != nil {
		t.Skipf("no %s under %s: %v", rel, root, err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// packID is the id a content pack file gives a name ("1155=magic:enchant_lvl1"), -1 when it has none.
func packID(pack, name string) int {
	for _, line := range strings.Split(pack, "\n") {
		if id, n, ok := strings.Cut(strings.TrimSpace(line), "="); ok && n == name {
			if v, err := strconv.Atoi(id); err == nil {
				return v
			}
		}
	}
	return -1
}

func TestJewelleryObjectIDsAreContents(t *testing.T) {
	pack := contentRoot(t, "pack/obj.pack")
	for name, want := range map[string]int{
		"gold_bar": int(GoldBarID), "sapphire": int(CutSapphireID), "necklace_mould": int(NecklaceMouldID),
		"sapphire_necklace": int(SapphireNecklaceID), "invis_necklace1": int(MakeMenuNecklaceID),
		"necklace_of_minigames_8": int(GamesNecklaceID), "cosmicrune": int(RuneCosmicID), "waterrune": int(RuneWaterID),
		"coins": int(CoinsID),
	} {
		if got := packID(pack, name); got != want {
			t.Errorf("%s: content's id is %d, the plan's %d", name, got, want)
		}
	}
	// The charges run 8 down to 1, two ids apart (the note of each is the id after it).
	for c := 8; c >= 1; c-- {
		if got, want := packID(pack, "necklace_of_minigames_"+strconv.Itoa(c)), int(GamesNecklaceID)+2*(8-c); got != want {
			t.Errorf("necklace_of_minigames_%d: id %d, want %d", c, got, want)
		}
		if got, want := packID(pack, "ring_of_dueling_"+strconv.Itoa(c)), 2552+2*(8-c); got != want {
			t.Errorf("ring_of_dueling_%d: id %d, want %d", c, got, want)
		}
	}
	obj := contentFile(t, "skill_magic/configs/enchanted_jewelry.obj")
	for c := 8; c >= 1; c-- {
		if b := block(obj, "necklace_of_minigames_"+strconv.Itoa(c)); !strings.Contains(b, "name=Games necklace("+strconv.Itoa(c)+")") || !strings.Contains(b, "members=yes") {
			t.Errorf("necklace_of_minigames_%d: %q", c, b)
		}
		if b := block(obj, "ring_of_dueling_"+strconv.Itoa(c)); !strings.Contains(b, "name=Ring of dueling("+strconv.Itoa(c)+")") {
			t.Errorf("ring_of_dueling_%d: %q", c, b)
		}
	}
	iface := contentRoot(t, "pack/interface.pack")
	for name, want := range map[string]int{
		"magic:enchant_lvl1": 1155, "crafting_jewelry": 4161, "crafting_jewelry:necklaces_inv": 4239,
	} {
		if got := packID(iface, name); got != want {
			t.Errorf("%s: content's interface id is %d, want %d", name, got, want)
		}
	}
}

func TestLvl1EnchantIsContents(t *testing.T) {
	spells := contentFile(t, "skill_magic/configs/magic_spells.dbrow")
	b := block(spells, "magic_spell_enchant_level1")
	for _, want := range []string{
		"data=spell,^enchant_lvl1\n", "data=members,false\n", "data=levelrequired," + strconv.Itoa(EnchantLevel) + "\n",
		"data=runesrequired,waterrune,1,cosmicrune,1,null,null\n", "data=experience," + strconv.Itoa(EnchantXPTenths) + "\n",
		"data=convertobj,sapphire_necklace,necklace_of_minigames_8,",
	} {
		if !strings.Contains(b+"\n", want) {
			t.Errorf("[magic_spell_enchant_level1] lacks %q:\n%s", want, b)
		}
	}
	enchant := contentFile(t, "skill_magic/scripts/spells/enchant.rs2")
	for _, want := range []string{
		"[opheldt,magic:enchant_lvl1]@magic_spell_enchant(^enchant_lvl1, last_slot);",
		"inv_setslot(inv, $slot, $final_obj, 1);", // the necklace is replaced in its slot
		"p_delay(1);", // one cast every third tick
		"You can only make that on a members' server.",
	} {
		if !strings.Contains(enchant, want) {
			t.Errorf("enchant.rs2 lacks %q", want)
		}
	}
	// The messages EnchantRefusal reads.
	magic := contentFile(t, "skill_magic/scripts/magic.rs2")
	for _, want := range []string{
		"Your Magic level is not high enough for this spell.",
		"You do not have enough <~string_removeright(oc_name($rune1), 5)> Runes to cast this spell.",
		"You need to be on a members' server to use this spell.",
	} {
		if !strings.Contains(magic, want) {
			t.Errorf("magic.rs2 lacks %q", want)
		}
	}
	if !strings.Contains(b, "This spell can only be cast on Sapphire Amulets, Necklaces and Rings.") {
		t.Errorf("the item refusal EnchantRefusal reads is not in the spell's block")
	}
}

func TestNecklaceCraftingIsContents(t *testing.T) {
	st := contentFile(t, "skill_crafting/configs/jewellery/jewellery.struct")
	b := block(st, "sapphire_necklace")
	for _, want := range []string{"param=gem,sapphire\n", "param=levelrequired," + strconv.Itoa(NecklaceLevel) + "\n", "param=productexp," + strconv.Itoa(NecklaceXPTenths) + "\n"} {
		if !strings.Contains(b+"\n", want) {
			t.Errorf("[sapphire_necklace] lacks %q:\n%s", want, b)
		}
	}
	rs2 := contentFile(t, "skill_crafting/scripts/jewellery/jewellery.rs2")
	for _, want := range []string{
		// Make 1, 5 and 10 are options 1, 2 and 3 of the necklaces inventory; there is no Make X.
		"[inv_button1,crafting_jewelry:necklaces_inv] @craft_gold(crafting_make_necklaces, last_slot, 1);",
		"[inv_button2,crafting_jewelry:necklaces_inv] @craft_gold(crafting_make_necklaces, last_slot, 5);",
		"[inv_button3,crafting_jewelry:necklaces_inv] @craft_gold(crafting_make_necklaces, last_slot, 10);",
		"if (inv_total(inv, necklace_mould) > 0) {",
		"inv_setslot(crafting_make_necklaces, 1, invis_necklace1, 1);", // the Sapphire necklace is slot 1
		"You need at least level <tostring($level)> Crafting to make that.",
		"p_delay(2);", // a necklace every third tick
		"$count = calc($count - 1);",
		"if ($count < 1) {",
	} {
		if !strings.Contains(rs2, want) {
			t.Errorf("jewellery.rs2 lacks %q", want)
		}
	}
	// The run of Make 10 ends silently only when the count is used up: a short count ends in a message box.
	if !strings.Contains(rs2, "You don't have enough gold bars to make any more.") || !strings.Contains(rs2, "~mesbox(") {
		t.Errorf("jewellery.rs2 no longer ends a short count with a message box")
	}
	// The interface opens only from a gold bar used on a furnace.
	smelting := contentFile(t, "skill_smithing/scripts/smelting/smelting.rs2")
	for _, want := range []string{"[oplocu,_smithing_furnace]", "case gold_bar, perfect_gold_bar : @craft_gold_interface;"} {
		if !strings.Contains(smelting, want) {
			t.Errorf("smelting.rs2 lacks %q", want)
		}
	}
	// The make menu's options.
	ifs := contentFile(t, "skill_crafting/interfaces/crafting_jewelry.if")
	if b := block(ifs, "necklaces_inv"); !strings.Contains(b, "option1=Make\n") || !strings.Contains(b, "option2=Make 5\n") || !strings.Contains(b, "option3=Make 10\n") || strings.Contains(b, "option4") {
		t.Errorf("[necklaces_inv]: %q", b)
	}
}

func TestNecklaceMouldShopsAreContents(t *testing.T) {
	moulds := contentFile(t, "skill_crafting/configs/jewellery/moulds.obj")
	if b := block(moulds, "necklace_mould"); !strings.Contains(b, "name=Necklace mould\n") || !strings.Contains(b, "cost=5\n") {
		t.Errorf("[necklace_mould]: %q", b)
	}
	for file, shops := range map[string][]string{
		"areas/area_rimmington/configs/rimmington.inv": {"craftingshop", "craftingshop_free"},
		"areas/area_alkharid/configs/alkharid.inv":     {"craftingshop2", "craftingshop2_free"},
	} {
		inv := contentFile(t, file)
		for _, shop := range shops {
			if b := block(inv, shop); !strings.Contains(b, "stock3=necklace_mould,2,100\n") {
				t.Errorf("%s [%s] does not stock the mould: %q", file, shop, b)
			}
		}
	}
	// Rommik's shop opens from his Trade option (op 3).
	npc := contentFile(t, "areas/area_rimmington/configs/rimmington.npc")
	if b := block(npc, "rommik"); !strings.Contains(b, "name=Rommik\n") || !strings.Contains(b, "op3=Trade\n") {
		t.Errorf("[rommik]: %q", b)
	}
}
