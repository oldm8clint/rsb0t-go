package questplan

import (
	"fmt"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// The Holy Grail's bank, bell and napkin decisions against the content checkout (skipped without one: RSB0T_CONTENT_DIR,
// or the sibling "content" directory of the repo's parent). They pin the facts grailbank.go and grailcastle.go rest on to
// the scripts themselves.

// bellScript is the `[opheld1,grail_bell]` block of quest_grail.rs2.
func bellScript(t *testing.T) string {
	t.Helper()
	script := scorpContentFile(t, "scripts", "quests", "quest_grail", "scripts", "quest_grail.rs2")
	_, bell, ok := strings.Cut(script, "[opheld1,grail_bell]")
	if !ok {
		t.Fatal("quest_grail.rs2 has no grail_bell script")
	}
	bell, _, _ = strings.Cut(bell, "\n[")
	return bell
}

// The bell needs a maiden within four tiles and a continue click on each of three boxes before the teleport, and lands the
// account on 0_43_73_9_20. The old flow rang it, waited six seconds and walked away; nothing in the script runs without the
// clicks.
func TestTheBellNeedsAMaidenInEarshotAndTheContinueClicks(t *testing.T) {
	bell := bellScript(t)
	for _, want := range []string{
		`~mesbox("Ting-a-ling-a-ling!");`,
		"npc_find(coord, grail_maiden, 4, 0) = true",
		"inzone(0_43_73_0_0, 0_43_73_63_63, coord) = true",
		"p_teleport($dest);",
		`~mesbox("Somehow you are now inside the castle.");`,
		`mes("Nothing happens.");`,
	} {
		if !strings.Contains(bell, want) {
			t.Errorf("the bell's script no longer contains %q:\n%s", want, bell)
		}
	}
	if !strings.Contains(bell, "def_coord $dest = 0_43_73_9_20;") {
		t.Fatalf("the bell's destination changed:\n%s", bell)
	}
	// 0_43_73_9_20: level 0, map square 43,73, local 9,20.
	if got := (tile.Tile{X: 43<<6 + 9, Z: 73<<6 + 20}); got != GrailCastleLanding {
		t.Errorf("the script lands on %v, GrailCastleLanding is %v", got, GrailCastleLanding)
	}
	if GrailBellEarshot != 4 {
		t.Errorf("the script listens within 4 tiles, GrailBellEarshot is %d", GrailBellEarshot)
	}
	// A mesbox is a page of p_pausebutton waits: it needs the continue click and an interface close drops it.
	chat := scorpContentFile(t, "scripts", "interface_chat", "scripts", "chat.rs2")
	_, mes, _ := strings.Cut(chat, "[proc,mesbox](string $string)")
	mes, _, _ = strings.Cut(mes, "\n[")
	if !strings.Contains(mes, "p_pausebutton;") {
		t.Errorf("~mesbox no longer waits on a button:\n%s", mes)
	}
}

// The Grail Maiden is npc 210 and four of them spawn in the castle; only the spawn three tiles south of the ring stand is
// within earshot of it, so the bell is rung when a wandering maiden happens to be near, not on arrival.
func TestTheMaidensAreNpc210AndOnlyOneSpawnsNearTheRingStand(t *testing.T) {
	pack := scorpContentFile(t, "pack", "npc.pack")
	if !strings.Contains("\n"+pack, fmt.Sprintf("\n%d=grail_maiden\n", NpcGrailMaiden)) {
		t.Errorf("npc.pack has no %d=grail_maiden", NpcGrailMaiden)
	}
	m := scorpContentFile(t, "maps", "m43_73.jm2")
	_, npcs, ok := strings.Cut(m, "==== NPC ====")
	if !ok {
		t.Fatal("m43_73.jm2 has no NPC section")
	}
	npcs, _, _ = strings.Cut(npcs, "====")
	var spawns []tile.Tile
	for _, line := range strings.Split(npcs, "\n") {
		var lvl, x, z, id int
		if n, _ := fmt.Sscanf(strings.TrimSpace(line), "%d %d %d: %d", &lvl, &x, &z, &id); n == 4 && id == NpcGrailMaiden {
			spawns = append(spawns, tile.Tile{X: 43<<6 + x, Z: 73<<6 + z, Level: lvl})
		}
	}
	if len(spawns) != 4 {
		t.Fatalf("%d maiden spawns in m43_73, want 4: %v", len(spawns), spawns)
	}
	near := 0
	for _, s := range spawns {
		if GrailMaidenInEarshot(GrailBellRing, s) {
			near++
		}
	}
	if near != 1 {
		t.Errorf("%d of the maidens' spawns %v are within earshot of the ring stand %v, want the one", near, spawns, GrailBellRing)
	}
}

// Galahad's napkin is a plain inv_add after a message box (a full pack loses it), offered at stage 4 to an account whose PACK
// holds none and at stage 7 only to one whose pack AND bank hold none.
func TestGalahadsNapkinNeedsRoomAndAtStageSevenAnEmptyBank(t *testing.T) {
	script := scorpContentFile(t, "scripts", "areas", "area_seers", "scripts", "brother_galahad.rs2")
	_, cloth, ok := strings.Cut(script, "[label,brother_galahad_borrow_cloth]")
	if !ok {
		t.Fatal("brother_galahad.rs2 has no borrow_cloth label")
	}
	if !strings.Contains(cloth, "inv_add(inv, holy_table_napkin, 1);") || strings.Contains(cloth, "inv_freespace") {
		t.Errorf("the napkin is no longer a bare inv_add:\n%s", cloth)
	}
	_, failed, _ := strings.Cut(script, "if(%grail = ^grail_failed_defeat_titan) {")
	failed, _, _ = strings.Cut(failed, "~chatnpc(\"<p,neutral>Welcome to my home.")
	if !strings.Contains(failed, "inv_total(inv, holy_table_napkin) = 0 & inv_total(bank, holy_table_napkin) = 0") {
		t.Errorf("stage 7's napkin offer no longer counts the bank:\n%s", failed)
	}
	if !strings.Contains(script, "if(%grail = ^grail_spoken_crone & inv_total(inv, holy_table_napkin) = 0) { // no bank check") {
		t.Errorf("stage 4's napkin offer changed")
	}
}

// The whistle door gives two to any account carrying fewer than two (the bank is not asked), and the Grail's pickup counts the
// bank, the pack and the worn slots.
func TestTheWhistleDoorAndTheGrailPickupAndTheItemIds(t *testing.T) {
	script := scorpContentFile(t, "scripts", "quests", "quest_grail", "scripts", "quest_grail.rs2")
	if !strings.Contains(script, "inv_total(inv, holy_table_napkin) > 0 & inv_total(inv, magic_whistle) < 2") {
		t.Error("the whistle door's rule changed")
	}
	_, pick, _ := strings.Cut(script, "[opobj3,holy_grail]")
	pick, _, _ = strings.Cut(pick, "\n[")
	if !strings.Contains(pick, "~obj_gettotal(holy_grail) > 0") {
		t.Errorf("the Grail's pickup no longer counts what the account owns:\n%s", pick)
	}
	procs := scorpContentFile(t, "scripts", "general", "scripts", "misc", "inv_procs.rs2")
	if !strings.Contains(procs, "calc(inv_total(inv, $obj) + inv_total(bank, $obj) + inv_total(worn, $obj))") {
		t.Error("obj_gettotal no longer counts the bank")
	}
	objs := scorpContentFile(t, "pack", "obj.pack")
	for name, id := range map[string]int{"holy_table_napkin": ObjNapkin, "magic_whistle": ObjMagicWhistle, "grail_bell": ObjGrailBell, "holy_grail": ObjHolyGrail} {
		if !strings.Contains("\n"+objs, fmt.Sprintf("\n%d=%s\n", id, name)) {
			t.Errorf("obj.pack has no %d=%s", id, name)
		}
	}
}
