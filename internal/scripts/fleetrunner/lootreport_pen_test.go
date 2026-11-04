package fleetrunner

import (
	"testing"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/scripts/fleetplan"
)

// PEN LOOT ON THE PEN'S MONSTER ("ogrepens", 5 Oct 2026), with the real config (skipped without it): a
// pile taken at the account's pen that no kill of anyone's could claim is the pen's monster's drop, on
// its card with no kill added, not a "take:<item>" card of its own. The live panel had Black sq shield,
// Nature rune, Law rune, Earth rune and Coins grabbed at the Moss giants on cards of their own, a
// "take: Big bones" card with 1,147 takes beside kill:ogre:114 with 2,388 kills and 287 Big bones.

func penTakeDeps(t *testing.T, pen fleetplan.TrainPen) (*Deps, *packSession, *[]loot.Drop) {
	t.Helper()
	d, s, got := testDeps()
	d.Config = travelTestConfig(t)
	d.TrainPen = pen
	return d, s, got
}

func takeAt(t *testing.T, d *Deps, s *packSession, name string, count, x, z, level int) {
	t.Helper()
	id := int(objOf(t, d.Config, name, false))
	before := LootBefore(d)
	s.put(0, id, count)
	RecordGroundTake(d, before, gamestate.GroundItem{ID: id, Count: count, X: x, Z: z, Level: level})
}

func TestPenTakeWithNoKillGoesToThePensMonster(t *testing.T) {
	d, s, got := penTakeDeps(t, fleetplan.YanilleWestOgres)
	takeAt(t, d, s, "Big bones", 1, 2503, 3095, 0)
	if len(*got) != 1 {
		t.Fatalf("recorded %+v", *got)
	}
	take := (*got)[0]
	if take.Source.Kind != loot.KindNPC || take.Source.Action != loot.ActionKill || take.Source.Name != "Ogre" || take.Events != 0 {
		t.Fatalf("take = %+v, want the Ogre's kill source with no kill added", take)
	}
	isOgre := false
	for _, id := range d.Config.NpcIDs("Ogre") {
		if id == take.Source.ID {
			isOgre = true
		}
	}
	if !isOgre || len(take.Items) != 1 || take.Items[0].Count != 1 {
		t.Fatalf("take = %+v: want one Big bones on an npc named Ogre", take)
	}
	// The Moss giants' loot, from the same kind of pile.
	d2, s2, got2 := penTakeDeps(t, fleetplan.MossGiantsNorthArdougne)
	takeAt(t, d2, s2, "Coins", 119, 2553, 3408, 0)
	if len(*got2) != 1 || (*got2)[0].Source.Name != "Moss giant" || (*got2)[0].Source.ID != mossGiantType || (*got2)[0].Events != 0 {
		t.Fatalf("a Moss giant's coins recorded as %+v", *got2)
	}
}

// The monster in view nearest the pile names the type: the Ogres are two types and the kill cards are
// per type.
func TestPenTakeUsesTheTypeOfTheNearestMonsterInView(t *testing.T) {
	d, s, got := penTakeDeps(t, fleetplan.YanilleWestOgres)
	ids := d.Config.NpcIDs("Ogre")
	var other int
	for _, id := range ids {
		if n, ok := d.Config.Npc(id); ok && HasAttackOp(n.Ops) && id != ids[0] {
			other = id
			break
		}
	}
	if other == 0 {
		t.Skip("only one attackable Ogre type in this config")
	}
	simPlace(s.st, 2505, 3100)
	simNPCs(s.st, simNPC{index: 5, typeID: other, dx: -2, dz: -5})
	takeAt(t, d, s, "Big bones", 1, 2503, 3095, 0)
	if len(*got) != 1 || (*got)[0].Source.ID != other {
		t.Fatalf("recorded %+v, want the Ogre type %d that stands nearest", *got, other)
	}
}

// A kill within four tiles still claims the pile first, as before; the pen's monster is the fallback only.
func TestAKillStillClaimsTheTakeBeforeThePen(t *testing.T) {
	d, s, got := penTakeDeps(t, fleetplan.YanilleWestOgres)
	ogre := d.Config.NpcIDs("Ogre")[0]
	NoteKill(d, ogre, 2503, 3094, 0)
	takeAt(t, d, s, "Big bones", 1, 2504, 3095, 0)
	if len(*got) != 2 || (*got)[0].Events != 1 || (*got)[1].Events != 0 || (*got)[1].Source != (*got)[0].Source {
		t.Fatalf("recorded %+v, want the kill and its take on one source", *got)
	}
}

// Away from the pen, on another level, with no pen chosen or with a pen of no monster: a pickup.
func TestATakeAwayFromThePenIsAPickup(t *testing.T) {
	for name, tc := range map[string]struct {
		Pen     fleetplan.TrainPen
		x, z, l int
	}{
		"outside the leash":     {fleetplan.YanilleWestOgres, 2600, 3095, 0},
		"another level":         {fleetplan.YanilleWestOgres, 2503, 3095, 1},
		"no pen chosen":         {fleetplan.TrainPen{}, 2503, 3095, 0},
		"a pen with no monster": {fleetplan.TrainPen{Name: "x", Stand: fleetplan.YanilleWestOgres.Stand, Leash: 22}, 2503, 3095, 0},
	} {
		d, s, got := penTakeDeps(t, tc.Pen)
		takeAt(t, d, s, "Big bones", 1, tc.x, tc.z, tc.l)
		if len(*got) != 1 || (*got)[0].Source.Kind != loot.KindPickup || (*got)[0].Events != 1 {
			t.Errorf("%s: recorded %+v, want a pickup", name, *got)
		}
	}
}

// A pile at an Ogre pen that an Ogre cannot drop is somebody else's, never the Ogre's. Live on
// 1306c66a, 13 Death runes taken at the Feldip pens were filed on the Ogre card; an Ogre drops Big
// bones and nothing else (scarabplan.OgreDrops). Runes never get a card of their own either (owner,
// 7 Oct 2026): with no kill of the account's to file them under, they are not recorded.
func TestPenTakeOfWhatThePensMonsterCannotDropIsAPickup(t *testing.T) {
	d, s, got := penTakeDeps(t, fleetplan.FeldipOgresEast)
	takeAt(t, d, s, "Death rune", 13, 2592, 2982, 0)
	for _, r := range *got {
		if r.Source.Kind == loot.KindNPC && r.Source.Name == "Ogre" {
			t.Fatalf("Death runes at an Ogre pen recorded on the Ogre: %+v", *got)
		}
		if r.Source.Kind == loot.KindPickup {
			t.Fatalf("Death runes recorded as a pickup card of their own: %+v", *got)
		}
	}
	// Control: the same pen still files Big bones on the Ogre.
	d2, s2, got2 := penTakeDeps(t, fleetplan.FeldipOgresEast)
	takeAt(t, d2, s2, "Big bones", 1, 2592, 2982, 0)
	if len(*got2) != 1 || (*got2)[0].Source.Kind != loot.KindNPC || (*got2)[0].Source.Name != "Ogre" {
		t.Fatalf("Big bones at an Ogre pen recorded as %+v, want the Ogre's", *got2)
	}
}
