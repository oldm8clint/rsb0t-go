package questmods

import (
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// The Khazard warlord fight, the I/O half. The rules are questplan's (treegnome_warlord.go): cast
// only from a tile 12 or more off his spawn (he drops a target further than maxrange+1 from it,
// so he never chases or hits there), at a warlord the spell reaches now and after his next step,
// no sooner than scarabplan.CastGap after the last cast; watch the tile after every cast and,
// when the cast walked the account towards him (he stepped out of reach while it was read), stop
// and walk straight back out; eat at WarlordEatAt and leave with no food at WarlordLeaveAt.

// warlordGroundData is the warlord's ground and cast tiles, built once from the static pack, loc
// index and config.
var warlordGroundData struct {
	sync.Mutex
	built bool
	sight func(x, z int) fleetplan.Sight
	cands []questplan.WarlordCastTile
}

// warlordGround is the line of sight casts at him trust (the pack's wall faces and the loc
// index's projectile blockers, scarabplan.BlockerSight) and the cast tiles. nil sight without a
// pack, loc index or config: then no cast is judged and the step refuses to fight.
func warlordGround(d *questrunner.Deps) (func(x, z int) fleetplan.Sight, []questplan.WarlordCastTile) {
	if d == nil || d.Pack == nil || d.Locs == nil || d.Config == nil {
		return nil, nil
	}
	g := &warlordGroundData
	g.Lock()
	defer g.Unlock()
	if g.built {
		return g.sight, g.cands
	}
	pk := d.Pack
	spawn := questplan.WarlordSpawn
	blocked := map[[2]int]bool{}
	r := questplan.WarlordSafeDist + questplan.WarlordCastRing + fleetplan.CastRange + 2
	d.Locs.Each(spawn.X-r, spawn.Z-r, spawn.X+r, spawn.Z+r, spawn.Level, func(l locindex.Loc) {
		if l.Shape < 9 || l.Shape > 21 {
			return
		}
		t, ok := d.Config.Loc(l.ID)
		if !ok || !t.BlockRange {
			return
		}
		w, h := max(t.Width, 1), max(t.Length, 1)
		if l.Angle == 1 || l.Angle == 3 {
			w, h = h, w
		}
		for dx := 0; dx < w; dx++ {
			for dz := 0; dz < h; dz++ {
				blocked[[2]int{l.X + dx, l.Z + dz}] = true
			}
		}
	})
	sight := fleetplan.BlockerSight(
		func(x, z int) uint8 { return pk.WallMask(x, z, spawn.Level) },
		func(x, z int) bool { return blocked[[2]int{x, z}] })
	// His ground is what he walks to inside his wander range; the cast tiles are on his side of
	// the rivers (a walk from his spawn reaches them), so the account that walked in to him walks
	// to any of them.
	from := pathfind.NavPoint{X: spawn.X, Z: spawn.Z, Level: spawn.Level}
	his, ours := pk.Flood(from, questplan.WarlordWanderRange), pk.Flood(from, questplan.WarlordStandFlood)
	roam := questplan.WarlordRoam(func(t tile.Tile) bool { return his[[2]int{t.X, t.Z}] })
	g.sight, g.built = sight, true
	g.cands = questplan.WarlordCastTiles(func(t tile.Tile) bool { return ours[[2]int{t.X, t.Z}] }, roam, sight)
	return g.sight, g.cands
}

// warlordFightTimeout bounds one fight: 170 hitpoints at a few a cast, a cast every 3.6 s, with
// the waits for him to wander back into reach.
const warlordFightTimeout = 15 * time.Minute

// warlordUnseen is how long the fight waits on its tile for a warlord out of view (killed by
// someone else and respawning, WarlordRespawn) before it gives the step back.
const warlordUnseen = questplan.WarlordRespawn + time.Minute

// treeWarlord kills the Khazard warlord by the owner's tactic; at stage 7 his death hands over
// the orbs.
func treeWarlord(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	sight, cands := warlordGround(d)
	if sight == nil || len(cands) == 0 {
		logf(d, "no collision pack, loc index or cast tile to judge a cast at the Khazard warlord from: not fighting him")
		d.Delay(5 * time.Second)
		return false
	}
	if _, ok := questrunner.BestCastableSpell(d, questplan.WarlordMinCasts); !ok {
		logf(d, "no combat spell with runes for %d casts (Magic %d): not fighting the Khazard warlord", questplan.WarlordMinCasts, questrunner.BaseLevels(d)["magic"])
		return false
	}
	foods := questplan.TreeGnomeVillage.Sustain.FoodIDs
	if hp, _ := questrunner.Hitpoints(d); questplan.WarlordShouldLeave(hp, foodHeld(d, foods)) {
		logf(d, "%d hitpoints and no food: not fighting the Khazard warlord", hp)
		return false
	}
	wait, _ := questplan.WarlordWaitTile(questrunner.CurrentTile(d), cands)
	if !questrunner.Travel(d, wait, 3) || !questrunner.WalkNear(d, wait, 0) {
		logf(d, "could not reach the cast tile %d,%d off the Khazard warlord", wait.X, wait.Z)
		return false
	}
	healToFull(d, foods)
	// Never melee him: auto-retaliate would answer his hit by walking up to him, and autocast
	// continues a hand cast with an approach.
	if err := actions.SetAutoRetaliate(d.Session, false); err != nil {
		logf(d, "auto-retaliate off: %v", err)
	}
	defer func() { _ = actions.SetAutoRetaliate(d.Session, true) }()
	if actions.AutocastEnabled(d.Session) {
		_ = actions.ToggleAutocast(d.Session)
		d.Delay(1200 * time.Millisecond)
	}
	logf(d, "casting at the Khazard warlord from out of his reach (%d cast tiles, the best covers %d tiles of his ground)", len(cands), cands[0].Cover)

	mark := time.Now()
	deadline := mark.Add(warlordFightTimeout)
	lastSeen := time.Now()
	var lastCast time.Time
	casts, drifts := 0, 0
	won := func() bool { return questrunner.HeldID(d, questplan.ObjOrbsOfProt) > 0 }
	for time.Now().Before(deadline) {
		if questrunner.DiedSince(d, mark) {
			return false
		}
		if won() {
			logf(d, "the Khazard warlord is dead: the orbs after %d casts (%d drifts walked back)", casts, drifts)
			e.InvalidateStage()
			return true
		}
		here := questrunner.CurrentTile(d)
		// Inside his reach (walked in by a cast, or by anything else): straight back out first.
		if !questplan.WarlordOutOfReach(here) {
			back, _ := questplan.WarlordStepBack(here, here, cands)
			stopOnTile(d)
			if !questrunner.WalkNear(d, back, 0) && !d.Delay(600*time.Millisecond) {
				return false
			}
			continue
		}
		hp, maxHP := questrunner.Hitpoints(d)
		questrunner.EatIfNeeded(d, foods, questplan.WarlordEatAt(maxHP))
		if hp, _ = questrunner.Hitpoints(d); questplan.WarlordShouldLeave(hp, foodHeld(d, foods)) {
			logf(d, "%d hitpoints and no food left: leaving the Khazard warlord after %d casts", hp, casts)
			return false
		}
		spell, ok := questrunner.BestCastableSpell(d, 1)
		if !ok {
			logf(d, "out of runes after %d casts at the Khazard warlord", casts)
			return false
		}
		npc, seen := questrunner.FindNpcByID(d, questplan.NpcKhazardWarlord, "", 24)
		if !seen {
			if time.Since(lastSeen) > warlordUnseen {
				logf(d, "no Khazard warlord in view for %s", warlordUnseen)
				return false
			}
			if w, ok := questplan.WarlordWaitTile(here, cands); ok && w != here {
				questrunner.WalkNear(d, w, 0)
			}
			if !d.Delay(1200 * time.Millisecond) {
				return false
			}
			continue
		}
		lastSeen = time.Now()
		at := tile.Tile{X: npc.X, Z: npc.Z, Level: npc.Level}
		spot, ok := questplan.ChooseWarlordCast(here, at, cands, sight)
		if !ok {
			// He is out of every cast tile's reach: wait for his wander to bring him back.
			if !d.Delay(600 * time.Millisecond) {
				return false
			}
			continue
		}
		if spot != here {
			if !questrunner.WalkNear(d, spot, 0) && !d.Delay(600*time.Millisecond) {
				return false
			}
			continue
		}
		if gap := time.Until(lastCast.Add(fleetplan.CastGap)); gap > 0 {
			if !d.Delay(gap) {
				return false
			}
			continue // he moved meanwhile: judge the cast again
		}
		at0 := time.Now()
		if err := actions.CastOnNpc(d.Session, spell.Name, npc.Index); err != nil {
			logf(d, "casting %s at the Khazard warlord: %v", spell.Name, err)
			return false
		}
		lastCast, casts = at0, casts+1
		if drifted := watchWarlordCast(d, spot, at0, cands); drifted {
			drifts++
		}
		if d.Chat != nil && actions.IsNoRunesLine(d.Chat.Since(at0)) {
			logf(d, "no runes for %s at the Khazard warlord", spell.Name)
		}
	}
	logf(d, "the Khazard warlord fight ran out its %s (%d casts)", warlordFightTimeout, casts)
	return won()
}

// watchWarlordCast watches the tile for the cast's delay: the server walks a player towards a
// target out of reach at the tick it reads the cast, and the sooner the walk is stopped the
// shorter it is. When the account moved off spot it stops, walks back to the nearest cast tile
// (questplan.WarlordStepBack) and reports the drift.
func watchWarlordCast(d *questrunner.Deps, spot tile.Tile, at time.Time, cands []questplan.WarlordCastTile) bool {
	for time.Since(at) < fleetplan.CastGap {
		if !d.Delay(300 * time.Millisecond) {
			return false
		}
		here := questrunner.CurrentTile(d)
		back, step := questplan.WarlordStepBack(spot, here, cands)
		if !step {
			continue
		}
		logf(d, "the cast at the Khazard warlord moved the account from %d,%d to %d,%d: back out to %d,%d", spot.X, spot.Z, here.X, here.Z, back.X, back.Z)
		stopOnTile(d)
		questrunner.WalkNear(d, back, 0)
		return true
	}
	return false
}

// stopOnTile clicks the tile the account stands on, which ends the approach the server is
// running for it.
func stopOnTile(d *questrunner.Deps) {
	h := questrunner.CurrentTile(d)
	_ = actions.Walk(d.Session, []pathfind.NavPoint{{X: h.X, Z: h.Z, Level: h.Level}}, true)
}

// foodHeld counts the foods in the pack.
func foodHeld(d *questrunner.Deps, foods []int) int {
	pack, n := questrunner.PackCounts(d), 0
	for _, f := range foods {
		n += pack[f]
	}
	return n
}
