package waterfallrunner

import (
	"fmt"
	"os"
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/webwalk"
)

func TestProbeWaterfallLocs(t *testing.T) {
	lp, cp, pp := os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG"), os.Getenv("RSB0T_COLLISION_PACK")
	if lp == "" || cp == "" || pp == "" {
		t.Skip("no data")
	}
	ix, err := locindex.Load(lp)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := cachecfg.LoadFile(cp)
	if err != nil {
		t.Fatal(err)
	}
	ids := []int{1987, 1989, 1990, 1991, 1992, 1993, 1994, 1995, 1996, 1999, 2000, 2002, 2004, 2006, 2010, 2014, 2020, 2022, 1754, 1757, 1516, 1519}
	for _, id := range ids {
		ms := ix.Find(cfg, locindex.Query{X: 2560, Z: 6500, Level: 0, Radius: 4000, IDs: []int{id}}, nil)
		typ, _ := cfg.Loc(id)
		for _, m := range ms {
			if m.X < 2480 || m.X > 2640 {
				continue
			}
			fmt.Printf("loc %d %q ops=%v w=%d l=%d fa=%d at %d,%d,%d shape=%d angle=%d\n", id, typ.Name, typ.Ops, typ.Width, typ.Length, typ.ForceApproach, m.X, m.Z, m.Level, m.Shape, m.Angle)
		}
	}
	for _, name := range []string{"Almera", "Hudon", "Golrie", "Hadley", "Fire giant", "Moss giant", "Shadow spider", "Giant skeleton", "Hobgoblin"} {
		fmt.Println("npc", name, cfg.NpcIDs(name))
	}
	for _, id := range []int{292, 293, 294, 295, 296, 297, 298, 299, 385, 954, 555, 556, 557, 1601, 2357} {
		o, _ := cfg.Obj(id)
		fmt.Printf("obj %d %q iops=%v\n", id, o.Name, o.IOps)
	}
	pk, err := pathfind.Load(pp)
	if err != nil {
		t.Fatal(err)
	}
	g, err := webwalk.Default()
	if err != nil {
		t.Fatal(err)
	}
	pl := webwalk.NewPlanner(pk, g)
	type leg struct {
		name     string
		from, to webwalk.Point
	}
	P := func(x, z int) webwalk.Point { return webwalk.Point{X: x, Z: z} }
	avoid := map[webwalk.Point]bool{P(2566, 9901): true, P(2604, 9900): true, P(2568, 9893): true, P(2540, 9900): true, P(2542, 9892): true, P(2606, 9892): true}
	legs := []leg{
		{"entry->crate", P(2575, 9861), P(2588, 9888)},
		{"crate->southdoor", P(2588, 9888), P(2568, 9892)},
		{"southdoor->entry", P(2568, 9892), P(2575, 9862)},
		{"north of south door ->puzzle door", P(2568, 9894), P(2566, 9900)},
		{"puzzle->pillar", P(2566, 9902), P(2563, 9911)},
		{"pillar->statue", P(2563, 9911), P(2565, 9915)},
		{"raised door->chalice", P(2604, 9901), P(2603, 9909)},
		{"tomb landing->chest", P(2554, 9844), P(2530, 9845)},
		{"chest->coffin", P(2530, 9845), P(2542, 9810)},
		{"coffin->ladder", P(2542, 9810), P(2557, 9844)},
		{"tgv landing->crate", P(2533, 9556), P(2548, 9564)},
		{"tgv crate->gate", P(2548, 9564), P(2515, 9574)},
		{"inside gate->golrie", P(2515, 9576), P(2515, 9580)},
		{"golrie->ladder", P(2515, 9580), P(2533, 9556)},
		{"mound->swimstand", P(2512, 3481), P(2512, 3477)},
		{"bank->almera", P(2616, 3332), P(2522, 3497)},
		{"bank->raft", P(2616, 3332), P(2509, 3493)},
		{"washout->bookcase", P(2527, 3413), webwalk.Point{X: 2519, Z: 3426, Level: 1}},
		{"bookcase->bank", webwalk.Point{X: 2519, Z: 3426, Level: 1}, P(2616, 3332)},
		{"bank->tgv ladder", P(2616, 3332), P(2533, 3156)},
		{"tgv ladder->bank", P(2533, 3156), P(2616, 3332)},
		{"bank->tombstone", P(2616, 3332), P(2558, 3444)},
		{"tomb exit->bank", P(2556, 3445), P(2616, 3332)},
		{"washout->bank", P(2527, 3413), P(2616, 3332)},
	}
	for _, l := range legs {
		r := pl.Plan(l.from, l.to, webwalk.Options{Avoid: avoid, AllowSpecial: true})
		fmt.Printf("== %s: ok=%v reason=%s cost=%d\n%s\n", l.name, r.OK, r.Reason, r.Cost, r.String())
	}
}
