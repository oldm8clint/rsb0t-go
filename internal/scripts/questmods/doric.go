package questmods

import (
	"time"

	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// DoricsQuest is Doric's Quest ready for the engine. Its talks and every provisioning pass are the engine's generic steps; the one
// live executor is the mining of what the bank and the mule could not give, doricMine.
func DoricsQuest() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.DoricsQuest,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomDoricMine: doricMine,
		},
	}
}

const (
	// doricMineBudget bounds one run of the mining step: six clay, four copper and two iron take a few minutes, and the engine
	// decides again after whatever this run left.
	doricMineBudget = 9 * time.Minute
	// doricSwingWait is how long one Mine is given to yield an ore: a bronze pickaxe swings every few ticks and a clay rock fails
	// about half the rolls at Mining 1 (rock_successchance 128,400), so a swing sequence that has not paid in this long found its
	// rock taken or empty.
	doricSwingWait = 20 * time.Second
	// doricRockWait is how long to wait for a rock to come back when every rock of an ore in view is mined out (clay: six ticks).
	doricRockWait = 15 * time.Second
	// doricRockSearch is how far from the ore's stand a rock may be and still be mined.
	doricRockSearch = 14
	// doricSkipFor is how long a rock that could not be reached or mined is left alone.
	doricSkipFor = 30 * time.Second
	// doricOreFailures ends one ore's mining after this many swings in a row that paid nothing.
	doricOreFailures = 8
)

// doricOre is one ore the quest mines, in the order it is mined (the rocks nearest the walk in first).
type doricOre struct {
	name  string
	item  int
	rocks []int
	stand tile.Tile
	want  int
	level int
}

var doricOres = []doricOre{
	{"clay", questplan.DoricClay, questplan.DoricClayRocks, questplan.RimmingtonClay, questplan.DoricClayQty, 1},
	{"copper ore", questplan.DoricCopperOre, questplan.DoricCopperRocks, questplan.RimmingtonCopper, questplan.DoricCopperQty, 1},
	{"iron ore", questplan.DoricIronOre, questplan.DoricIronRocks, questplan.RimmingtonIron, questplan.DoricIronQty, questplan.DoricIronLevel},
}

// doricRock is the nearest live rock of one of ids within doricRockSearch tiles of near that is not in skip. The ore rocks are all
// named "Rocks" with the one op "Mine", so the type ids are what tell clay from copper from iron; a rock another account (or this
// one) mined out has changed type and is not found until it comes back (the live loc changes are merged in).
func doricRock(d *questrunner.Deps, ids []int, near tile.Tile, skip map[tile.Tile]time.Time) (locindex.Match, bool) {
	if d.Locs == nil || d.Config == nil {
		return locindex.Match{}, false
	}
	q := locindex.Query{X: near.X, Z: near.Z, Level: near.Level, Radius: doricRockSearch, IDs: ids, Op: "Mine"}
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	for _, m := range d.Locs.Find(d.Config, q, live) {
		if until, ok := skip[tile.Tile{X: m.X, Z: m.Z, Level: m.Level}]; ok && time.Now().Before(until) {
			continue
		}
		return m, true
	}
	return locindex.Match{}, false
}

// doricMine mines the clay, copper ore and iron ore the pack still lacks in Rimmington's mine, with the best pickaxe the account
// holds at its Mining level. Iron is mined only at Mining 15 (iron_rock_table); below it the step leaves the iron to the bank and
// the mule and the module's decision parks the quest. It reports whether the pack gained any ore: a partial gain is progress,
// and the next decision asks for what is left.
func doricMine(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	snap := questplan.Snapshot{InvIDs: questrunner.PackCounts(d), WornIDs: questrunner.WornCounts(d), Levels: questrunner.BaseLevels(d)}
	if questplan.DoricUsablePickaxe(snap) == 0 {
		logf(d, "doric: no pickaxe in the pack or worn that Mining %d can use", snap.Level("mining"))
		return false
	}
	owed := func(o doricOre) int { return o.want - questrunner.HeldID(d, o.item) }
	var todo []doricOre
	for _, o := range doricOres {
		if owed(o) > 0 && snap.Level("mining") >= o.level {
			todo = append(todo, o)
		}
	}
	if len(todo) == 0 {
		logf(d, "doric: nothing the account can mine is still owed")
		return false
	}
	before := 0
	for _, o := range doricOres {
		before += questrunner.HeldID(d, o.item)
	}
	deadline := time.Now().Add(doricMineBudget)
	logf(d, "doric: mining in Rimmington (%d ore kind(s) owed, Mining %d), up to %s", len(todo), snap.Level("mining"), doricMineBudget)
	for _, o := range todo {
		if !doricMineOre(d, o, deadline) {
			break
		}
	}
	after := 0
	for _, o := range doricOres {
		after += questrunner.HeldID(d, o.item)
	}
	logf(d, "doric: the pack holds %d clay, %d copper ore and %d iron ore (+%d)", questrunner.HeldID(d, questplan.DoricClay),
		questrunner.HeldID(d, questplan.DoricCopperOre), questrunner.HeldID(d, questplan.DoricIronOre), after-before)
	return after > before
}

// doricMineOre mines one ore until the pack holds the quest's quantity, the budget ends or the swings stop paying. It reports
// false only when the whole run should stop (the bot is stopping, the pack is full); an ore that gave nothing is simply left.
func doricMineOre(d *questrunner.Deps, o doricOre, deadline time.Time) bool {
	if !questrunner.TravelCautious(d, o.stand, 6) {
		logf(d, "doric: could not reach the %s rocks at %d,%d", o.name, o.stand.X, o.stand.Z)
		return true
	}
	skip := map[tile.Tile]time.Time{}
	failures := 0
	for questrunner.HeldID(d, o.item) < o.want && time.Now().Before(deadline) && failures < doricOreFailures {
		if !questrunner.WaitForGuardian(d) {
			return false
		}
		if questrunner.FreeSlots(d) == 0 {
			logf(d, "doric: the pack is full")
			return false
		}
		rock, ok := doricRock(d, o.rocks, o.stand, skip)
		if !ok {
			// Every rock of this ore in view is mined out (or another account is on it): they come back in a few ticks.
			if !waitUntil(d, doricRockWait, func() bool { _, found := doricRock(d, o.rocks, o.stand, skip); return found }) {
				failures++
				questrunner.Travel(d, o.stand, 4)
			}
			continue
		}
		had := questrunner.HeldID(d, o.item)
		if err := questrunner.OperateLoc(d, rock, "Mine"); err != nil {
			logf(d, "doric: %s rock at %d,%d: %v", o.name, rock.X, rock.Z, err)
			skip[tile.Tile{X: rock.X, Z: rock.Z, Level: rock.Level}] = time.Now().Add(doricSkipFor)
			failures++
			continue
		}
		if waitUntil(d, doricSwingWait, func() bool { return questrunner.HeldID(d, o.item) > had }) {
			failures = 0
			logf(d, "doric: mined %s (%d of %d)", o.name, questrunner.HeldID(d, o.item), o.want)
			continue
		}
		// The rock was mined out under the swings, or the rolls failed for the whole wait: another rock, or the same one again.
		skip[tile.Tile{X: rock.X, Z: rock.Z, Level: rock.Level}] = time.Now().Add(5 * time.Second)
		failures++
	}
	return true
}
