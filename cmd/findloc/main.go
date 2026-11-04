// Command findloc is an offline diagnostic: dump every loc within a radius of a tile from
// a baked static index (cmd/locbake's own output), resolving each id's name/ops via a
// config archive. No live connection needed — for confirming a real fixture's loc id/tile
// (e.g. a gangplank an operator just found live) before wiring a hardcoded handler for it,
// the same way freshstart.ArdougneGangplankID/KaramjaGangplankID were each confirmed.
//
//	go run ./cmd/findloc <locs.lcli.gz> <config.jag> <x> <z> <level> <radius>
package main

import (
	"fmt"
	"os"
	"strconv"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
)

func main() {
	if len(os.Args) != 7 {
		fmt.Fprintln(os.Stderr, "usage: findloc <locs.lcli.gz> <config.jag> <x> <z> <level> <radius>")
		os.Exit(2)
	}
	locsPath, configPath := os.Args[1], os.Args[2]
	x, _ := strconv.Atoi(os.Args[3])
	z, _ := strconv.Atoi(os.Args[4])
	level, _ := strconv.Atoi(os.Args[5])
	radius, _ := strconv.Atoi(os.Args[6])

	ix, err := locindex.Load(locsPath)
	if err != nil {
		panic(err)
	}
	cfg, err := cachecfg.LoadFile(configPath)
	if err != nil {
		panic(err)
	}
	ix.Each(x-radius, z-radius, x+radius, z+radius, level, func(l locindex.Loc) {
		t, ok := cfg.Loc(l.ID)
		name := "?"
		var ops [5]string
		if ok {
			name = t.Name
			ops = t.Ops
		}
		fmt.Printf("id=%d name=%q ops=%v at %d,%d,%d angle=%d shape=%d\n", l.ID, name, ops, l.X, l.Z, l.Level, l.Angle, l.Shape)
	})
}
