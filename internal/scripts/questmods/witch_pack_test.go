package questmods

import (
	"os"
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/locreach"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// The Witch's House fountain and shed on the fleet's real data (skipped without RSB0T_COLLISION_PACK,
// RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG).
//
// 262 of 523 runs of the quest from 6 to 15 Oct 2026 ended at the fountain with "no progress for 8 ticks", and
// the log said nothing about why. The first suspect was the click: the fountain is a 2x2 loc whose only
// option is op 2, and the runner stands on 2911,3470 to use it. This pins that the click is sound, so the
// refusal is content's: [oploc2,witchfountain] gives the key to an account that owns none ANYWHERE
// (obj_gettotal counts the bank), and all 13 accounts that parked there held the shed key in the bank.
func loadWitchData(t *testing.T) (*pathfind.Pack, *locindex.Index, *cachecfg.Config) {
	t.Helper()
	pp, ip, cp := os.Getenv("RSB0T_COLLISION_PACK"), os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG")
	if pp == "" || ip == "" || cp == "" {
		t.Skip("RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX or RSB0T_CONFIG_JAG not set")
	}
	pk, err := pathfind.Load(pp)
	if err != nil {
		t.Fatal(err)
	}
	ix, err := locindex.Load(ip)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := cachecfg.LoadFile(cp)
	if err != nil {
		t.Fatal(err)
	}
	return pk, ix, cfg
}

func TestWitchFountainIsCheckableFromItsStandOnTheRealData(t *testing.T) {
	pk, ix, cfg := loadWitchData(t)
	typ, ok := cfg.Loc(questplan.LocWitchFount)
	if !ok || typ.Name != "Fountain" || typ.Width != 2 || typ.Length != 2 {
		t.Fatalf("loc %d is %+v, want the 2x2 Fountain", questplan.LocWitchFount, typ)
	}
	if n := cachecfg.OpIndex(typ.Ops, "Check"); n != 2 {
		t.Fatalf("the fountain's Check is option %d (%v), content's [oploc2,witchfountain] wants 2", n, typ.Ops)
	}
	ms := ix.Find(cfg, locindex.Query{X: questplan.WitchFountTile.X, Z: questplan.WitchFountTile.Z, Radius: 3, IDs: []int{questplan.LocWitchFount}, Op: "Check"}, nil)
	if len(ms) != 1 {
		t.Fatalf("%d fountains within 3 of %v", len(ms), questplan.WitchFountTile)
	}
	m := ms[0]
	d := &questrunner.Deps{Pack: pk, Locs: ix, Config: cfg}
	loc := locreach.Loc{X: m.X, Z: m.Z, Level: m.Level, Width: typ.Width, Length: typ.Length, Angle: m.Angle, Shape: m.Shape, ForceApproach: typ.ForceApproach}
	stands := questrunner.LocStands(d, m, questplan.WitchFountStand)
	found := false
	for _, s := range stands {
		if s == questplan.WitchFountStand {
			found = true
		}
		if reached, ok := locreach.Reached(s.X, s.Z, loc); !ok || !reached {
			t.Errorf("stand %v is not a tile the engine runs the fountain's trigger from (reached %v, known %v)", s, reached, ok)
		}
	}
	if !found {
		t.Errorf("the garden walk ends on %v, which the runner does not offer as a stand for the fountain at %d,%d (stands %v)",
			questplan.WitchFountStand, m.X, m.Z, stands)
	}
	if !pk.Walkable(questplan.WitchFountStand.X, questplan.WitchFountStand.Z, 0) {
		t.Errorf("%v is not walkable", questplan.WitchFountStand)
	}
}

// The items the decision draws from the bank and the foods the gate counts are the ids the cache names
// them by, so a withdrawal by id asks for the item the content scripts count.
func TestWitchItemIDsAreTheContentItems(t *testing.T) {
	_, _, cfg := loadWitchData(t)
	for id, want := range map[int]string{
		questplan.ObjWitchBall: "Ball", questplan.ObjWitchDiary: "Diary", questplan.ObjWitchDoorKey: "Door key",
		questplan.ObjWitchMagnet: "Magnet", questplan.ObjWitchShedKey: "Key", questplan.ObjCheese: "Cheese",
		questplan.ObjLeatherGlove: "Leather gloves", 385: "Shark", 379: "Lobster", 373: "Swordfish", 2309: "Bread",
		562: "Chaos rune", 556: "Air rune", 558: "Mind rune", 554: "Fire rune", 1381: "Staff of air",
	} {
		o, ok := cfg.Obj(id)
		if !ok || o.Name != want {
			t.Errorf("item %d is %q, want %q", id, o.Name, want)
		}
	}
}

// The shed fight's tiles are ground and the shed door is the loc the key opens (witchshed_pack_test.go in
// questplan has who can reach the cast tile).
func TestWitchShedTilesOnTheRealData(t *testing.T) {
	pk, ix, cfg := loadWitchData(t)
	for name, tl := range map[string]tile.Tile{
		"one-front": questplan.WitchOneFront, "glob spot": questplan.WitchGlobSpot, "ball stand": questplan.WitchBallStand,
		"door stand": questplan.WitchShedStand, "corridor": {X: 2936, Z: 3460},
	} {
		if !pk.Walkable(tl.X, tl.Z, tl.Level) {
			t.Errorf("%s %v is not walkable", name, tl)
		}
	}
	doors := ix.Find(cfg, locindex.Query{X: questplan.WitchShedDoor.X, Z: questplan.WitchShedDoor.Z, Radius: 2, IDs: []int{questplan.LocWitchShed}, Op: "Open"}, nil)
	if len(doors) != 1 {
		t.Fatalf("%d shed doors near %v", len(doors), questplan.WitchShedDoor)
	}
}
