// Command locbake bakes the static loc index (internal/locindex) from an Engine-TS 289
// checkout's packed map files, the same files rsb0t/tools/nav/build-collision.ts reads.
// Those are also the loc files the engine serves to game clients (verified identical for
// every mapsquare), so the index holds exactly the placements the server loads.
//
// By default only interactive locs are kept: the ones whose type is "active", which are
// the only static locs the engine registers for OPLOC. Pass -all to keep every loc.
//
//	go run ./cmd/locbake -engine <Engine-TS checkout> -config <config archive> -out locs.lcli.gz
//
// The config archive is the engine's GET /config<crc> (cmd/cachecfgtest -save fetches it).
// Like the collision pack, the output is generated, not committed; pass its path to
// whatever loads it.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
)

func main() {
	engine := flag.String("engine", "", "Engine-TS checkout whose data/pack holds the packed maps")
	configPath := flag.String("config", "", "config archive, to keep only active locs")
	all := flag.Bool("all", false, "keep every loc, not only active ones (no -config needed)")
	out := flag.String("out", "", "index file to write")
	flag.Parse()
	if *engine == "" || *out == "" || (*configPath == "" && !*all) {
		fmt.Fprintln(os.Stderr, "usage: locbake -engine <dir> -out <file> (-config <config archive> | -all)")
		os.Exit(2)
	}

	t0 := time.Now()
	maps, err := locindex.ReadEngineMaps(*engine)
	if err != nil {
		fail(err)
	}
	var keep func(int) bool
	if !*all {
		cfg, err := cachecfg.LoadFile(*configPath)
		if err != nil {
			fail(err)
		}
		keep = func(id int) bool {
			l, ok := cfg.Loc(id)
			return ok && l.Active
		}
	}
	ix, err := locindex.Bake(maps, keep)
	if err != nil {
		fail(err)
	}
	f, err := os.Create(*out)
	if err != nil {
		fail(err)
	}
	if err := locindex.Write(f, ix); err != nil {
		f.Close()
		fail(err)
	}
	if err := f.Close(); err != nil {
		fail(err)
	}
	st, _ := os.Stat(*out)
	fmt.Printf("baked %d locs in %d mapsquares from %d map files in %v: %d record bytes, %d bytes on disk\n",
		ix.Count(), ix.Squares(), len(maps), time.Since(t0).Round(time.Millisecond), ix.RecordBytes(), st.Size())
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "locbake:", err)
	os.Exit(1)
}
