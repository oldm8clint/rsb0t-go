package randomevents

import (
	"sync"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
)

var (
	liveLocsMu sync.Mutex
	liveLocs   *locindex.Index
)

// SetLocIndex registers the static map-loc index the combat flee overlays live door and wall
// changes on (see fleePack). Process-wide and idempotent, like SetNameResolver; nil (the
// default) leaves every flee planning on the static collision pack exactly as before.
func SetLocIndex(ix *locindex.Index) {
	liveLocsMu.Lock()
	liveLocs = ix
	liveLocsMu.Unlock()
}

func currentLocIndex() *locindex.Index {
	liveLocsMu.Lock()
	defer liveLocsMu.Unlock()
	return liveLocs
}

// fleePack is pack with this session's observed straight/corner wall changes applied: a door
// the map places open but somebody has closed becomes a wall, and one the map places closed
// but is standing open stops being one. The same overlay freshstartrunner.liveNavigationPack
// gives ordinary walking (CLAUDE.md, "Live door walls require a per-search overlay"); the
// combat flee used to plan on the static pack alone, so a closed door on the chosen route
// left the account pressed against it beside its attacker. Returns pack unchanged when the
// loc index or the loc configs are not registered.
func fleePack(s actions.Session, pack *pathfind.Pack) *pathfind.Pack {
	ix := currentLocIndex()
	res := currentResolver()
	if pack == nil || ix == nil || res == nil {
		return pack
	}
	locs, ok := res.(LocResolver)
	if !ok {
		return pack
	}
	var removed, added []pathfind.WallEdit
	for _, c := range s.State().LocChanges() {
		if c.Shape != 0 && c.Shape != 2 {
			continue
		}
		for _, base := range ix.At(c.X, c.Z, c.Level) {
			if base.Shape != 0 && base.Shape != 2 {
				continue
			}
			if info, ok := locs.Loc(base.ID); !ok || !info.BlockWalk {
				continue
			}
			removed = append(removed, pathfind.WallEdit{X: base.X, Z: base.Z, Level: base.Level, Shape: base.Shape, Angle: base.Angle})
		}
		if c.Deleted {
			continue
		}
		if info, ok := locs.Loc(c.Type); !ok || !info.BlockWalk {
			continue
		}
		added = append(added, pathfind.WallEdit{X: c.X, Z: c.Z, Level: c.Level, Shape: c.Shape, Angle: c.Rotate, Add: true})
	}
	return pack.WithWalls(append(removed, added...))
}
