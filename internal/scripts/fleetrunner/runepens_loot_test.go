package fleetrunner

import (
	"strings"
	"testing"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
)

// THE RUNE PENS' LOOT ("runepens", 13 Oct 2026), with the real config (skipped without it): a rune pile at a rune pen is
// worth a cast's reach, not four tiles; what the pen leaves is filed on its herd's card; and the log says what it paid.

func logDeps(t *testing.T, pen fleetplan.TrainPen, px, pz int) (*Deps, *packSession, *[]string) {
	t.Helper()
	d, s, _ := penTakeDeps(t, pen)
	var lines []string
	d.Log = func(l string) { lines = append(lines, l) }
	simPlace(s.st, px, pz)
	return d, s, &lines
}

// A cast kills from up to ten tiles away and the pile lies where the NPC fell: at the Dwarf pen a pile of Chaos or Nature
// rune within RunePileRadius of the account, at the pen and off the Scorpions' hazard, is the nearest loot; four tiles is
// all anything else is worth, and so is everything at a pen that is not a rune pen.
func TestARunePileAtARunePenIsWorthACastsReach(t *testing.T) {
	pen := fleetplan.DwarvesFaladorMine
	d, s, _ := logDeps(t, pen, 3017, 9826)
	chaos, nature := int(objOf(t, d.Config, "Chaos rune", false)), int(objOf(t, d.Config, "Nature rune", false))
	coins := int(objOf(t, d.Config, "Coins", false))
	anchor := pen.Stand
	pickOf := func() (gamestate.GroundItem, bool) { return nearestTrainLoot(d, anchor, map[string]bool{}) }

	simObj(s.st, 3017, 9826, 3025, 9828, chaos, 2) // eight tiles east
	if g, ok := pickOf(); !ok || g.ID != chaos || g.X != 3025 {
		t.Fatalf("a Chaos rune pile eight tiles off at the Dwarf pen: %+v %v", g, ok)
	}
	// The same pile at a pen that is not a rune pen is out of reach (the four tiles of BoneLootRadius).
	d.TrainPen = fleetplan.KhazardTroopers
	if g, ok := pickOf(); ok {
		t.Fatalf("a rune pile eight tiles off at the Khazard troopers was worth a walk: %+v", g)
	}
	d.TrainPen = pen
	// Coins eight tiles off are not (only the runes a rune pen pays).
	simObj(s.st, 3017, 9826, 3024, 9826, coins, 30)
	if g, ok := pickOf(); !ok || g.ID == coins {
		t.Fatalf("the nearest loot is the coins: %+v %v", g, ok)
	}
	// Beyond a cast's reach, nothing.
	d2, s2, _ := logDeps(t, pen, 3017, 9826)
	simObj(s2.st, 3017, 9826, 3028, 9826, nature, 2) // eleven tiles
	if g, ok := nearestTrainLoot(d2, anchor, map[string]bool{}); ok {
		t.Fatalf("a pile eleven tiles off was worth a walk: %+v", g)
	}
	// On the Scorpions' hazard, never: the account does not walk into their notice for a pile.
	d3, s3, _ := logDeps(t, pen, 3027, 9822)
	simObj(s3.st, 3027, 9822, 3030, 9812, chaos, 2) // inside x 3022-3062, z 9764-9818
	if g, ok := nearestTrainLoot(d3, tile.Tile{X: 3027, Z: 9822}, map[string]bool{}); ok {
		t.Fatalf("a pile on the Scorpions' hazard was worth a walk: %+v", g)
	}
	// At four tiles or less every pen takes what it always took.
	d4, s4, _ := logDeps(t, fleetplan.KhazardTroopers, 2522, 3243)
	simObj(s4.st, 2522, 3243, 2524, 3243, chaos, 2)
	if g, ok := nearestTrainLoot(d4, tile.Tile{X: 2522, Z: 3243}, map[string]bool{}); !ok || g.ID != chaos {
		t.Fatalf("a rune pile two tiles off at the troopers: %+v %v", g, ok)
	}
}

// What a rune pen paid, in the log: the pen, the count, the rune. Nothing for coins, for a rune away from a rune pen, or for a
// rune a rune pen does not pay.
func TestWhatARunePenPaidIsInTheLog(t *testing.T) {
	pen := fleetplan.DwarvesFaladorMine
	d, _, lines := logDeps(t, pen, 3017, 9826)
	chaos, coins, law := int(objOf(t, d.Config, "Chaos rune", false)), int(objOf(t, d.Config, "Coins", false)), int(objOf(t, d.Config, "Law rune", false))
	NoteRunePay(d, gamestate.GroundItem{ID: chaos, Count: 2, X: 3025, Z: 9828})
	if len(*lines) != 1 || !strings.Contains((*lines)[0], "runepen: Falador Dwarven Mine dwarves paid 2 Chaos rune (taken at 3025,9828)") {
		t.Fatalf("log = %q", *lines)
	}
	NoteRunePay(d, gamestate.GroundItem{ID: coins, Count: 30})
	NoteRunePay(d, gamestate.GroundItem{ID: law, Count: 3})
	d.TrainPen = fleetplan.KhazardTroopers
	NoteRunePay(d, gamestate.GroundItem{ID: chaos, Count: 2})
	if len(*lines) != 1 {
		t.Fatalf("coins, a Law rune and a rune away from a rune pen were logged: %q", *lines)
	}
	d.TrainPen = fleetplan.IceWarriorsAsgarnianDungeon
	NoteRunePay(d, gamestate.GroundItem{ID: int(objOf(t, d.Config, "Nature rune", false)), Count: 4, X: 3045, Z: 9582})
	if len(*lines) != 2 || !strings.Contains((*lines)[1], "runepen: Asgarnian ice dungeon ice warriors paid 4 Nature rune (taken at 3045,9582)") {
		t.Fatalf("log = %q", *lines)
	}
}

// The piles of the rune pens' herds go on the herd's card, and a rune the herd cannot drop does not (the Ogre pens'
// Feldip Death rune, 5 Oct).
func TestRunePenPilesAreFiledOnTheirHerd(t *testing.T) {
	for _, tc := range []struct {
		Pen         fleetplan.TrainPen
		herd        string
		item        string
		count       int
		x, z        int
		notDropped  string
		notDroppedN int
	}{
		{fleetplan.DwarvesFaladorMine, "Dwarf", "Chaos rune", 2, 3020, 9828, "Death rune", 1},
		{fleetplan.IceWarriorsAsgarnianDungeon, "Ice warrior", "Nature rune", 4, 3045, 9582, "Fire rune", 5},
	} {
		d, s, got := penTakeDeps(t, tc.Pen)
		takeAt(t, d, s, tc.item, tc.count, tc.x, tc.z, 0)
		if len(*got) != 1 {
			t.Fatalf("%s: recorded %+v", tc.Pen.Name, *got)
		}
		take := (*got)[0]
		if take.Source.Kind != loot.KindNPC || take.Source.Action != loot.ActionKill || take.Source.Name != tc.herd || take.Events != 0 {
			t.Errorf("%s: %s taken at the pen was recorded as %+v, want the %s's kill source with no kill added", tc.Pen.Name, tc.item, take, tc.herd)
		}
		if info, ok := d.Config.Npc(take.Source.ID); !ok || !strings.EqualFold(info.Name, tc.herd) {
			t.Errorf("%s: the card is npc %d (%+v), not a %s", tc.Pen.Name, take.Source.ID, info, tc.herd)
		}
		d2, s2, got2 := penTakeDeps(t, tc.Pen)
		takeAt(t, d2, s2, tc.notDropped, tc.notDroppedN, tc.x, tc.z, 0)
		for _, r := range *got2 {
			if r.Source.Kind == loot.KindNPC && r.Source.Name == tc.herd {
				t.Errorf("%s: %s taken at the pen was filed on the %s: %+v", tc.Pen.Name, tc.notDropped, tc.herd, *got2)
			}
		}
	}
}
