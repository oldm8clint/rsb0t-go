package mastercraftplan

import (
	"os"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
)

// The facts the chest round's kit rests on, read from a Content checkout (RSB0T_CONTENT, skipped
// without one) so that none of them can drift away from the scripts they were derived from.

func contentFile(t *testing.T, rel string) string {
	t.Helper()
	root := os.Getenv("RSB0T_CONTENT")
	if root == "" {
		t.Skip("set RSB0T_CONTENT to a Content checkout to check the chest kit's facts against it")
	}
	b, err := os.ReadFile(strings.TrimRight(root, "/\\") + "/scripts/" + rel)
	if err != nil {
		t.Skipf("no %s under %s: %v", rel, root, err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// debugName is the name content's config files use for an item: lower case, underscores.
func debugName(name string) string { return strings.ReplaceAll(strings.ToLower(name), " ", "_") }

// block is the text of one [section] of a content config file.
func block(src, name string) string {
	i := strings.Index(src, "["+name+"]")
	if i < 0 {
		return ""
	}
	rest := src[i+len(name)+2:]
	if j := strings.Index(rest, "\n["); j >= 0 {
		return rest[:j]
	}
	return rest
}

// "masters taking air runes when it has an air staff equipped": the staves that pay a rune are the
// ones in content's magic_staff_table, and the staff is the one in the weapon slot; a members staff
// pays nothing on a free world.
func TestStaffTableIsContents(t *testing.T) {
	table := contentFile(t, "skill_magic/configs/magic_staff.dbrow")
	for _, c := range chestStaves {
		element := strings.ToLower(strings.TrimSuffix(c.Rune, " rune"))
		b := block(table, "magic_staff_"+element)
		if b == "" {
			t.Fatalf("no [magic_staff_%s] block", element)
		}
		if !strings.Contains(b, "data=rune,"+element+"rune") {
			t.Errorf("%s: the block pays another rune:\n%s", c.Rune, b)
		}
		for _, name := range append(append([]string(nil), c.Staves...), c.Members...) {
			if !strings.Contains(b, "data=staff,"+debugName(name)+"\n") {
				t.Errorf("%s: %q is not in content's table:\n%s", c.Rune, name, b)
			}
		}
		for _, line := range strings.Split(b, "\n") {
			if rest, ok := strings.CutPrefix(line, "data=staff,"); ok {
				found := false
				for _, name := range append(append([]string(nil), c.Staves...), c.Members...) {
					found = found || debugName(name) == strings.TrimSpace(rest)
				}
				if !found {
					t.Errorf("%s: content's table has %q, which RunesPaidByStaff does not know", c.Rune, rest)
				}
			}
		}
	}
	magic := contentFile(t, "skill_magic/scripts/magic.rs2")
	for _, want := range []string{
		"def_obj $staff = inv_getobj(worn, ^wearpos_rhand);",
		"if (map_members = ^false & $staff ! null & oc_members($staff) = true) {",
	} {
		if !strings.Contains(magic, want) {
			t.Errorf("magic.rs2 no longer says %q: RunesPaidByStaff is derived from it", want)
		}
	}
}

// The spells' runes, levels and landings are the ones the kit and the way home are built on.
func TestTeleportRowsAreContents(t *testing.T) {
	spells := contentFile(t, "skill_magic/configs/magic_spells.dbrow")
	for _, c := range []struct{ name, row, level, runes, coord string }{
		{VarrockTeleport, "magic_spell_teleport_varrock", "25", "firerune,1,airrune,3,lawrune,1", "0_50_53_13_32"},
		{FaladorTeleport, "magic_spell_teleport_falador", "37", "waterrune,1,airrune,3,lawrune,1", "0_46_52_21_50"},
		{CamelotTeleport, "magic_spell_teleport_camelot", "45", "airrune,5,lawrune,1,null,null", "0_43_54_5_22"},
	} {
		b := block(spells, c.row)
		for _, want := range []string{"data=levelrequired," + c.level + "\n", "data=runesrequired," + c.runes + "\n", "data=tele_coord," + c.coord + "\n"} {
			if !strings.Contains(b, want) {
				t.Errorf("%s: content's row lacks %q:\n%s", c.name, strings.TrimSpace(want), b)
			}
		}
		if got := ChestSpellLevel(c.name); got == 0 || c.level != itoa(got) {
			t.Errorf("%s: the plan's level is %d, content's %s", c.name, got, c.level)
		}
	}
	var varrock fleetplan.Teleport
	for _, tp := range fleetplan.Teleports {
		if tp.Spell.Name == VarrockTeleport {
			varrock = tp
		}
	}
	if varrock.DestX != 3213 || varrock.DestZ != 3424 {
		t.Errorf("Varrock Teleport lands at %d,%d in scarabplan, 0_50_53_13_32 is 3213,3424", varrock.DestX, varrock.DestZ)
	}
}

// "crystal chest runs could be sped up by using strange fruit to restore run energy": a bite gives 30%
// of the bar, the first option eats it, and a second bite inside three ticks is refused.
func TestStrangeFruitIsContents(t *testing.T) {
	fruit := contentFile(t, "player/scripts/consumption/effects/scripts/strange_fruit.rs2")
	if !strings.Contains(fruit, "healenergy(3000);") {
		t.Errorf("a Strange fruit no longer heals 3000 of the 10000 run energy:\n%s", fruit)
	}
	consume := contentFile(t, "player/scripts/consumption/consume.rs2")
	for _, want := range []string{
		"[opheld1,macro_triffidfruit]@player_consume_item(consume_effect_strange_fruit, 2, 3);",
		"if (%eat_delay >= map_clock) {",
		"%eat_delay = calc(map_clock + $eat_delay);",
	} {
		if !strings.Contains(consume, want) {
			t.Errorf("consume.rs2 no longer says %q: the bite spacing (ChestBiteGap) is derived from it", want)
		}
	}
	if ChestBiteGap < 3*600*time.Millisecond {
		t.Errorf("ChestBiteGap %v is under the three ticks content refuses a second bite for", ChestBiteGap)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for ; n > 0; n /= 10 {
		s = string(rune('0'+n%10)) + s
	}
	return s
}
