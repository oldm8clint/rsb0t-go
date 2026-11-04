package randomevents

import (
	"os"
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
)

// componentSize counts the tiles walkable from s on its level, up to max. The step order is the pack's own
// exit-bit order (pathfind's DX/DZ).
func componentSize(pack *pathfind.Pack, s StrandTile, max int) int {
	dx := [8]int{0, 1, 0, -1, 1, 1, -1, -1}
	dz := [8]int{1, 0, -1, 0, 1, -1, -1, 1}
	seen := map[StrandTile]bool{s: true}
	queue := []StrandTile{s}
	for len(queue) > 0 && len(seen) < max {
		cur := queue[0]
		queue = queue[1:]
		mask := pack.ExitMask(cur.X, cur.Z, cur.Level)
		for i := 0; i < 8; i++ {
			if mask&(1<<uint(i)) == 0 {
				continue
			}
			n := StrandTile{X: cur.X + dx[i], Z: cur.Z + dz[i], Level: cur.Level}
			if seen[n] || !pack.Walkable(n.X, n.Z, n.Level) {
				continue
			}
			seen[n] = true
			queue = append(queue, n)
		}
	}
	return len(seen)
}

// TestStrandSitesAgainstTheRealPack proves every number in strandsites.go against the 289 map data, not
// against this file's own beliefs: each step's loc is placed where it says with the id and the option it
// says, each stand tile can be stood on, the stand is reachable on foot from where the server drops the
// account, a door's far side is open ground that joins the world, and the sites really are sealed (the
// reason a walk cannot get out of them).
//
// Needs RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG; without them it skips.
func TestStrandSitesAgainstTheRealPack(t *testing.T) {
	pack := realCollisionPack(t)
	ixPath, cfgPath := os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG")
	if ixPath == "" || cfgPath == "" {
		t.Skip("set RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG")
	}
	ix, err := locindex.Load(ixPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := cachecfg.LoadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	nav := func(p StrandTile) pathfind.NavPoint { return pathfind.NavPoint{X: p.X, Z: p.Z, Level: p.Level} }

	for _, site := range strandSites {
		for _, st := range site.Steps {
			// The loc is where the step says, with the id and the option it says.
			found := false
			for _, l := range ix.At(st.LocX, st.LocZ, st.Where.Level) {
				if l.ID != st.LocID {
					continue
				}
				found = true
				lt, ok := cfg.Loc(l.ID)
				if !ok {
					t.Errorf("%s / %s: loc %d is not in the config", site.Name, st.Name, l.ID)
					continue
				}
				if got := lt.Ops[st.Op-1]; got != st.OpLabel {
					t.Errorf("%s / %s: loc %d option %d is %q in the config, the catalogue says %q (ops %v)", site.Name, st.Name, l.ID, st.Op, got, st.OpLabel, lt.Ops)
				}
			}
			if !found {
				t.Errorf("%s / %s: no loc %d at %d,%d on level %d in the map", site.Name, st.Name, st.LocID, st.LocX, st.LocZ, st.Where.Level)
			}
			for _, s := range st.Stands {
				if !pack.Walkable(s.X, s.Z, s.Level) {
					t.Errorf("%s / %s: stand %v cannot be stood on", site.Name, st.Name, s)
				}
			}
			// Reachable on foot from where the server drops the account (same level, same room).
			for _, a := range site.Arrivals {
				if !st.Where.Contains(a.X, a.Z, a.Level) {
					continue
				}
				for _, s := range st.Stands {
					if r := pack.FindPath(nav(a), nav(s)); !r.OK {
						t.Errorf("%s / %s: stand %v is not reachable on foot from the arrival tile %v", site.Name, st.Name, s, a)
					}
				}
			}
			if st.Kind == StepDoor {
				if !pack.Walkable(st.Beyond.X, st.Beyond.Z, st.Beyond.Level) {
					t.Errorf("%s / %s: the far side %v cannot be stood on", site.Name, st.Name, st.Beyond)
				}
				// And it is the open world, not another room: its walkable component is thousands of
				// tiles (the real one is 15,379, bbox 2944..3321 by 3904..3966).
				n := componentSize(pack, st.Beyond, 5000)
				if st.BeyondRoom {
					// Another room of the same building (the Wizards' Tower's 24-tile east hall): room-sized,
					// and not the room the account is stuck in.
					if n < 20 || n >= 5000 {
						t.Errorf("%s / %s: the far side %v is meant to be a room, but its component is %d tiles", site.Name, st.Name, st.Beyond, n)
					}
					if r := pack.FindPath(nav(st.Stands[0]), nav(st.Beyond)); r.OK {
						t.Errorf("%s / %s: the far side %v has a plain walk from the stand %v, so the door is not what separates them", site.Name, st.Name, st.Beyond, st.Stands[0])
					}
				} else if n < 5000 {
					t.Errorf("%s / %s: the far side %v is in a component of only %d tiles, not the open ground", site.Name, st.Name, st.Beyond, n)
				}
			}
		}
		// Sealed: no walk from any arrival tile reaches the world, which is why a walk never got out.
		for _, a := range site.Arrivals {
			for _, home := range []pathfind.NavPoint{{X: 3158, Z: 3950, Level: 0}, {X: 2655, Z: 3283, Level: 0}, {X: 3222, Z: 3218, Level: 0}} {
				if r := pack.FindPath(nav(a), home); r.OK {
					t.Errorf("%s: arrival %v has a plain walk to %v, so the site is not sealed and this catalogue entry is wrong", site.Name, a, home)
				}
			}
		}
	}
}
