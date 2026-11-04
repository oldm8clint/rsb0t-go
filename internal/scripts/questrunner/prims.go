package questrunner

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/locreach"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/fighterfood"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// The live primitives behind the step kinds questport did not need: locs by id or name with a
// stand the engine accepts, items by exact id, held-item ops, eating, ground items, levels and
// deaths. Every sibling runner has its own copy of most of these (the per-package duplication
// convention); these are the quest engine's.

// ---------------------------------------------------------------------------
// Items
// ---------------------------------------------------------------------------

// PackCounts is the backpack by exact object id.
func PackCounts(d *Deps) map[int]int {
	out := map[int]int{}
	for _, it := range actions.NewEquipment(d.Session, d.Components).Inventory() {
		if it.ID > 0 {
			n := int(it.Count)
			if n < 1 {
				n = 1
			}
			out[int(it.ID)] += n
		}
	}
	return out
}

// WornCounts is the worn slots by exact object id.
func WornCounts(d *Deps) map[int]int {
	out := map[int]int{}
	for _, it := range actions.NewEquipment(d.Session, d.Components).Worn() {
		if it.ID > 0 {
			out[int(it.ID)]++
		}
	}
	return out
}

// HeldID is how many of an exact object id the pack holds.
func HeldID(d *Deps, id int) int { return PackCounts(d)[id] }

// FreeSlots is the pack's free slot count.
func FreeSlots(d *Deps) int {
	used := 0
	for _, it := range actions.NewEquipment(d.Session, d.Components).Inventory() {
		if it.ID > 0 {
			used++
		}
	}
	return PackSlots - used
}

// ResolveItem picks an object id for a step: the id when set, otherwise the first held (then
// any) object with that display name. ok is false when neither resolves.
func ResolveItem(d *Deps, id int, name string) (int, bool) {
	if id > 0 {
		return id, true
	}
	if name == "" || d.Config == nil {
		return 0, false
	}
	pack := PackCounts(d)
	ids := d.Config.ObjIDs(name)
	for _, cand := range ids {
		if pack[cand] > 0 {
			return cand, true
		}
	}
	if len(ids) > 0 {
		return ids[0], true
	}
	return 0, false
}

// ObjName is an object's display name, or "obj N".
func ObjName(d *Deps, id int) string {
	if d.Config != nil {
		if o, ok := d.Config.Obj(id); ok && o.Name != "" {
			return o.Name
		}
	}
	return fmt.Sprintf("obj %d", id)
}

// heldOpIndex is the 1-based inventory op slot labelled op, with the client's default "Drop" in
// the fifth slot when the type leaves it empty (actions.HeldOps). Without that default a cake,
// whose config names only "Eat", had no "Drop" op, and Waterfall's freeSlots looped at full
// hitpoints (live 2026-09-26: "2/3 cake has no \"Drop\" op (it has [Eat    ])").
func heldOpIndex(iops [5]string, op string) int {
	return cachecfg.OpIndex(actions.HeldOps(iops), op)
}

// OpHeldByName sends the held item's op whose label is op (Read, Eat, Empty, Wear ...).
func OpHeldByName(d *Deps, id int, op string) error {
	if !PackVisible(d) {
		return errors.New("the backpack is not visible to the server")
	}
	eq := actions.NewEquipment(d.Session, d.Components)
	obj, ok := d.Config.Obj(id)
	if !ok {
		return fmt.Errorf("no config for obj %d", id)
	}
	n := heldOpIndex(obj.IOps, op)
	if n == 0 {
		return fmt.Errorf("%s has no %q op (it has %v)", obj.Name, op, actions.HeldOps(obj.IOps))
	}
	for _, it := range eq.Inventory() {
		if int(it.ID) == id {
			return actions.OpHeld(d.Session, n, it.ID, it.Slot, eq.InventoryComponent())
		}
	}
	return fmt.Errorf("%s is not in the pack", obj.Name)
}

// Equip wears a held item and waits for it to land in a worn slot.
func Equip(d *Deps, id int) bool {
	if WornCounts(d)[id] > 0 {
		return true
	}
	if !PackVisible(d) {
		return false
	}
	eq := actions.NewEquipment(d.Session, d.Components)
	if err := eq.Equip(int32(id)); err != nil {
		d.logf("equip %s: %v", ObjName(d, id), err)
		return false
	}
	return waitQuietly(d, func() bool { return WornCounts(d)[id] > 0 }, 4*time.Second)
}

// ---------------------------------------------------------------------------
// Levels, hitpoints, deaths
// ---------------------------------------------------------------------------

// BaseLevels is every skill's base level by lower-case name: the level the XP implies, or the
// server's current level when that is higher (Hitpoints on this build; see gamestate/health.go).
func BaseLevels(d *Deps) map[string]int {
	out := map[string]int{}
	skills := d.Session.State().Skills()
	for i, sk := range skills {
		name := strings.ToLower(gamestate.SkillName(i))
		if name == "" || sk.Generation == 0 {
			continue
		}
		lvl := levels.LevelFromXP(sk.XP)
		if i == gamestate.HitpointsStat {
			if max, ok := d.Session.State().MaxHP(); ok && max > lvl {
				lvl = max
			}
		}
		out[name] = lvl
	}
	return out
}

// Hitpoints returns current and maximum hitpoints (0, 0 before the first stat packet).
func Hitpoints(d *Deps) (int, int) {
	st := d.Session.State()
	hp, ok := st.HP()
	if !ok {
		return 0, 0
	}
	max, _ := st.MaxHP()
	return hp, max
}

// DiedSince reports whether the game's death line arrived since mark.
func DiedSince(d *Deps, mark time.Time) bool {
	if d.Chat == nil {
		return false
	}
	return fighterfood.IsDeathLine(d.Chat.Since(mark))
}

// EatIfNeeded eats one food from the sustain's list when hitpoints are at or below eatAt, and
// reports whether it ate. eatAt 0 never eats.
func EatIfNeeded(d *Deps, foods []int, eatAt int) bool {
	return EatIfNeededVs(d, foods, eatAt, 0)
}

// EatIfNeededVs is EatIfNeeded against a foe whose hardest hit is peakHit (0 for unknown). After a view lag (gamestate.RecentLag,
// lageat-1018) and while the account is being hit, the line is raised to what the foe could have done in the worst recent episode,
// up to 70% of the maximum (fighterfood.LagEatLine): the hitpoints read are up to a dozen ticks old.
func EatIfNeededVs(d *Deps, foods []int, eatAt, peakHit int) bool {
	if eatAt <= 0 || len(foods) == 0 {
		return false
	}
	if hp, max := Hitpoints(d); max > 0 {
		eatAt = lagEatAt(d, hp, max, eatAt, peakHit)
	}
	// The engine's background sustain and a step's own eating must not both send an Eat for
	// the same bite. The lock is the account's own (Deps.eatMu): one process runs a whole
	// cohort, and a shared lock queued every account's bite behind every other's.
	d.eatMu.Lock()
	defer d.eatMu.Unlock()
	hp, max := Hitpoints(d)
	if max == 0 || hp > eatAt {
		return false
	}
	pack := PackCounts(d)
	for _, f := range foods {
		if pack[f] <= 0 {
			continue
		}
		before := hp
		if err := OpHeldByName(d, f, "Eat"); err != nil {
			d.logf("eating %s: %v", ObjName(d, f), err)
			continue
		}
		d.eaten.Add(1)
		waitQuietly(d, func() bool { h, _ := Hitpoints(d); return h > before }, 2*time.Second)
		h, _ := Hitpoints(d)
		d.logf("ate %s at %d/%d hitpoints, now %d", ObjName(d, f), before, max, h)
		return true
	}
	return false
}

// lagEatAt raises a quest eat line (hitpoints, at or under which a bite is due) for a view that has lagged lately: only while the account
// is being hit, to what peakHit (fighterfood.LagUnseenHit when 0) for as many volleys as the worst recent episode held could take, once
// for each NPC facing the account and at least one. hp is the hitpoints just read: over the lag cap nothing can change the answer, and
// the NPCs are not listed.
func lagEatAt(d *Deps, hp, maxHP, line, peakHit int) int {
	st := d.Session.State()
	lag := st.RecentViewLag().Ticks
	if lag <= 0 || maxHP <= 0 || hp > fighterfood.LagCap(maxHP) || !st.InCombat() {
		return line
	}
	hit := peakHit
	if hit <= 0 {
		hit = fighterfood.LagUnseenHit
	}
	facing, self := 0, st.Self()
	for _, n := range st.NPCs() {
		if n.TargetsPlayer(self) {
			facing++
		}
	}
	return fighterfood.LagEatLine(maxHP, line, hit*max(1, facing), lag)
}

// EatFloor eats one bite of any food the pack carries when the account is at or under questplan.FloorEatPercent of its
// hitpoints, and reports whether it ate: the floor under EatIfNeeded, which eats only the foods the module's Sustain names and
// only at the module's own EatAt (and not at all for a module with no Sustain). The biggest heal goes first and a quest's own
// tools (Deps.isQuestTool) never do. Engine.Run's eat loop calls it while a Travel leg is under way; see questplan.FloorEatPercent
// for the deaths it is for.
func EatFloor(d *Deps) bool {
	// The cheap test first: this runs every 600 ms of every quest leg of every account, and above half the hitpoints it costs
	// one stat read, not a read of the pack.
	if hp, max := Hitpoints(d); !questplan.FloorEatDueAt(hp, max, lagEatAt(d, hp, max, questplan.FloorEatLine(max), 0), time.Hour) {
		return false
	}
	d.eatMu.Lock()
	defer d.eatMu.Unlock()
	hp, max := Hitpoints(d)
	gap := d.floorGap
	if gap <= 0 {
		gap = questplan.FloorEatGap
	}
	since := time.Hour
	if !d.floorBiteAt.IsZero() {
		since = time.Since(d.floorBiteAt)
	}
	if !questplan.FloorEatDueAt(hp, max, lagEatAt(d, hp, max, questplan.FloorEatLine(max), 0), since) || since < gap {
		return false
	}
	foods := questplan.RankFloorFoods(carriedFoods(d), d.isQuestTool)
	total := 0
	for _, f := range foods {
		total += f.Count
	}
	for _, f := range foods {
		if err := OpHeldByName(d, f.ID, "Eat"); err != nil {
			continue // a food whose first op is not Eat (Jug of wine says Drink) is not a floor bite
		}
		d.floorBiteAt = time.Now()
		d.eaten.Add(1)
		before := hp
		waitQuietly(d, func() bool { h, _ := Hitpoints(d); return h > before }, 2*time.Second)
		h, _ := Hitpoints(d)
		d.logf("quest walk eat: a bite of %s at %d/%d hitpoints, now %d (%d food left)", ObjName(d, f.ID), before, max, h, total-1)
		return true
	}
	return false
}

// carriedFoods is the pack's unnoted stacks that have an Eat op, by id, name and count, for questplan.RankFloorFoods (which
// decides by name which of them are food).
func carriedFoods(d *Deps) []questplan.FloorFood {
	if d.Config == nil {
		return nil
	}
	var out []questplan.FloorFood
	for id, n := range PackCounts(d) {
		o, ok := d.Config.Obj(id)
		if !ok || o.Noted() || heldOpIndex(o.IOps, "Eat") == 0 {
			continue
		}
		out = append(out, questplan.FloorFood{ID: id, Name: o.Name, Count: n})
	}
	// Map order is random: the same pack must give the same bite.
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ---------------------------------------------------------------------------
// Locs
// ---------------------------------------------------------------------------

// FindLoc finds the nearest loc by exact type id (when id > 0) or by name, within radius of
// near, with live loc changes merged in. op, when set, requires the type to offer it.
func FindLoc(d *Deps, id int, name, op string, near tile.Tile, radius int) (locindex.Match, bool) {
	if d.Locs == nil || d.Config == nil {
		return locindex.Match{}, false
	}
	q := locindex.Query{X: near.X, Z: near.Z, Level: near.Level, Radius: radius, Op: op}
	if id > 0 {
		q.IDs = []int{id}
	} else {
		q.Name = name
	}
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	ms := d.Locs.Find(d.Config, q, live)
	if len(ms) == 0 {
		return locindex.Match{}, false
	}
	return ms[0], true
}

// LocStands lists the tiles the engine accepts an interaction with m from, nearest to here
// first. Rectangle-strategy locs (shapes 10, 11, 22) go through internal/locreach, which models
// forceapproach, and so do wall decorations (shapes 4 to 8: a straight one is reached from its own tile
// and no other); other walls use the collision pack's own interact tiles.
func LocStands(d *Deps, m locindex.Match, here tile.Tile) []tile.Tile {
	var cands []tile.Tile
	if m.Shape == 10 || m.Shape == 11 || m.Shape == 22 {
		typ, _ := d.Config.Loc(m.ID)
		cands = locreach.Tiles(locreach.Loc{
			X: m.X, Z: m.Z, Level: m.Level, Width: typ.Width, Length: typ.Length,
			Angle: m.Angle, Shape: m.Shape, ForceApproach: typ.ForceApproach,
		})
		if d.Pack != nil {
			w, l := typ.Width, typ.Length
			if m.Angle&1 == 1 {
				w, l = l, w
			}
			kept := cands[:0]
			for _, t := range cands {
				if d.Pack.Walkable(t.X, t.Z, t.Level) && !walledFromRect(d.Pack.WallMask(t.X, t.Z, t.Level), t, m.X, m.Z, w, l) {
					kept = append(kept, t)
				}
			}
			cands = kept
		}
	} else if locreach.DecorShape(m.Shape) {
		for _, s := range locreach.WallDecorStands(locreach.Loc{X: m.X, Z: m.Z, Level: m.Level, Angle: m.Angle, Shape: m.Shape}) {
			if d.Pack != nil && (!d.Pack.Walkable(s.Tile.X, s.Tile.Z, s.Tile.Level) ||
				s.Open != 0 && d.Pack.WallMask(s.Tile.X, s.Tile.Z, s.Tile.Level)&byte(s.Open) != 0) {
				continue
			}
			cands = append(cands, s.Tile)
		}
	} else if m.Shape == 0 {
		// A straight wall (a door, a gate) is reached from its own tile or the tile across the
		// edge it sits on, and from nowhere else. The collision pack's interact tiles offered
		// the tile BEHIND Golrie's gate too (2515,9574 for the gate at 2515,9575 angle 1), and
		// two Waterfall lab runs stood there under attack sending "I can't reach that!" until
		// they died.
		dx, dz := [4]int{-1, 0, 1, 0}[m.Angle&3], [4]int{0, 1, 0, -1}[m.Angle&3]
		cands = []tile.Tile{{X: m.X, Z: m.Z, Level: m.Level}, {X: m.X + dx, Z: m.Z + dz, Level: m.Level}}
	} else if d.Pack != nil {
		for _, p := range d.Pack.InteractTiles(pathfind.NavPoint{X: m.X, Z: m.Z, Level: m.Level}) {
			cands = append(cands, tile.Tile{X: p.X, Z: p.Z, Level: p.Level})
		}
	}
	if len(cands) == 0 {
		cands = []tile.Tile{{X: m.X, Z: m.Z - 1, Level: m.Level}, {X: m.X + 1, Z: m.Z, Level: m.Level},
			{X: m.X, Z: m.Z + 1, Level: m.Level}, {X: m.X - 1, Z: m.Z, Level: m.Level}}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		return tile.Chebyshev(here, cands[i]) < tile.Chebyshev(here, cands[j])
	})
	return cands
}

// walledFromRect reports whether stand t, orthogonally beside the x,z w-by-l footprint, has a
// wall on the face toward it (wall nibble on t: bit 0 north, 1 east, 2 south, 3 west, as
// pathfind.Pack.WallMask). The engine's rectangle reach refuses such a tile: Murder Mystery's
// flour barrel stands against the Sinclair kitchen's east wall, and the questproof EMPTY lab run
// murder-empty-2 sent "Take From" from the far side of it, answered "I can't reach that!" until
// it parked. A tile inside the footprint, or not beside it, is not judged here.
func walledFromRect(wall byte, t tile.Tile, x, z, w, l int) bool {
	if w < 1 {
		w = 1
	}
	if l < 1 {
		l = 1
	}
	alongX := t.X >= x && t.X < x+w
	alongZ := t.Z >= z && t.Z < z+l
	switch {
	case alongX && t.Z == z+l:
		return wall&(1<<2) != 0 // north of it: the south face
	case alongX && t.Z == z-1:
		return wall&(1<<0) != 0 // south of it: the north face
	case alongZ && t.X == x+w:
		return wall&(1<<3) != 0 // east of it: the west face
	case alongZ && t.X == x-1:
		return wall&(1<<1) != 0 // west of it: the east face
	}
	return false
}

// standingAt reports whether here is one of stands.
func standingAt(here tile.Tile, stands []tile.Tile) bool {
	for _, s := range stands {
		if s == here {
			return true
		}
	}
	return false
}

// ApproachLoc walks onto a tile the engine will run m's trigger from. The engine runs
// clientRoutefinder, so an op sent from anywhere else is dropped with no reply at all.
func ApproachLoc(d *Deps, m locindex.Match) bool {
	here, ok := currentTileOK(d)
	if !ok {
		return false
	}
	stands := LocStands(d, m, here)
	if standingAt(here, stands) {
		return true
	}
	for i, s := range stands {
		if i >= 4 {
			break
		}
		if tile.Chebyshev(here, s) > 12 || here.Level != s.Level {
			Travel(d, s, 0)
		}
		if WalkNear(d, s, 0) {
			return true
		}
		here = currentTile(d)
	}
	// The plain walk has no door edges: Murder Mystery's smashed window is reached only through
	// a Sinclair Mansion inner door, and the first questproof EMPTY lab run (murder-empty-1)
	// parked two tiles from it on "no path ... unreachable" with that door shut. When no stand
	// is reachable on foot, the webwalker, which opens doors, gets one try at each.
	if d.Walker != nil {
		for i, s := range stands {
			if i >= 4 {
				break
			}
			if Travel(d, s, 0) && standingAt(currentTile(d), []tile.Tile{s}) {
				return true
			}
		}
	}
	d.logf("could not stand on any tile that reaches the loc %d at %d,%d,%d", m.ID, m.X, m.Z, m.Level)
	return false
}

// OperateLoc sends op on m from an accepted stand.
func OperateLoc(d *Deps, m locindex.Match, op string) error {
	typ, ok := d.Config.Loc(m.ID)
	if !ok {
		return fmt.Errorf("no config for loc %d", m.ID)
	}
	n := cachecfg.OpIndex(typ.Ops, op)
	if n == 0 {
		return fmt.Errorf("%s at %d,%d has no %q op (it has %v)", typ.Name, m.X, m.Z, op, typ.Ops)
	}
	if !ApproachLoc(d, m) {
		return fmt.Errorf("could not reach %s at %d,%d", typ.Name, m.X, m.Z)
	}
	ClearLeftoverModal(d)
	return actions.OpLoc(d.Session, n, m.X, m.Z, m.ID)
}

// UseItemOnLoc uses a held item on m from an accepted stand.
func UseItemOnLoc(d *Deps, itemID int, m locindex.Match) error {
	if !PackVisible(d) {
		return errors.New("the backpack is not visible to the server")
	}
	if HeldID(d, itemID) == 0 {
		return fmt.Errorf("%s is not in the pack", ObjName(d, itemID))
	}
	if !ApproachLoc(d, m) {
		return fmt.Errorf("could not reach the loc %d at %d,%d", m.ID, m.X, m.Z)
	}
	ClearLeftoverModal(d)
	return actions.NewEquipment(d.Session, d.Components).UseOnLoc(int32(itemID), m.X, m.Z, m.ID)
}

// ---------------------------------------------------------------------------
// NPCs and ground items
// ---------------------------------------------------------------------------

// FindNpcByID finds the nearest NPC by exact id (when id > 0) or name within radius.
func FindNpcByID(d *Deps, id int, name string, radius int) (entityquery.Entity, bool) {
	q := entityquery.Npcs(d.Session.State(), d.Config).Within(radius)
	if id > 0 {
		q = q.Where(func(e entityquery.Entity) bool { return e.ID == id })
	} else {
		q = q.Name(name)
	}
	return q.Nearest()
}

// UseItemOnNpc walks adjacent and uses a held item on the NPC.
func UseItemOnNpc(d *Deps, itemID int, npc entityquery.Entity) error {
	if !PackVisible(d) {
		return errors.New("the backpack is not visible to the server")
	}
	at := tile.Tile{X: npc.X, Z: npc.Z, Level: npc.Level}
	if tile.Chebyshev(currentTile(d), at) > 1 {
		WalkNear(d, at, 1)
	}
	ClearLeftoverModal(d)
	return actions.NewEquipment(d.Session, d.Components).UseOnNpc(int32(itemID), npc.Index)
}

// GroundItemsNear lists visible ground items of an id within radius of near.
func GroundItemsNear(d *Deps, id int, near tile.Tile, radius int) []gamestate.GroundItem {
	var out []gamestate.GroundItem
	for _, g := range d.Session.State().GroundItems() {
		if int(g.ID) != id || g.Level != near.Level {
			continue
		}
		if tile.Chebyshev(tile.Tile{X: g.X, Z: g.Z, Level: g.Level}, near) <= radius {
			out = append(out, g)
		}
	}
	return out
}

// TakeGround walks to and picks up one ground item of id near near, and reports whether the
// pack gained it.
func TakeGround(d *Deps, id int, near tile.Tile, radius int) bool {
	items := GroundItemsNear(d, id, near, radius)
	if len(items) == 0 {
		return false
	}
	g := items[0]
	obj, _ := d.Config.Obj(id)
	n := cachecfg.OpIndex(obj.Ops, "Take")
	if n == 0 {
		n = 3
	}
	at := tile.Tile{X: g.X, Z: g.Z, Level: g.Level}
	if here := currentTile(d); here != at {
		if !WalkNear(d, at, 0) && d.Walker != nil {
			// The plain walk has no door edges: Merlin's insect repellent lies in a Catherby house
			// behind a shut door, and the second Merlin EMPTY lab run parked on "no path ...
			// unreachable" standing outside its wall. The webwalker opens the door.
			if !Travel(d, at, 0) {
				Travel(d, at, 1)
			}
		}
	}
	before := HeldID(d, id)
	if err := actions.OpObj(d.Session, n, g.X, g.Z, int(g.ID)); err != nil {
		d.logf("take %s: %v", ObjName(d, id), err)
		return false
	}
	return waitQuietly(d, func() bool { return HeldID(d, id) > before }, 5*time.Second)
}

// TravelAvoiding is Travel with rectangles the long walk must keep out of.
func TravelAvoiding(d *Deps, dest tile.Tile, radius int, avoid []webwalk.Rect) bool {
	if len(avoid) == 0 || d.Walker == nil {
		return Travel(d, dest, radius)
	}
	here, ok := currentTileOK(d)
	if ok && here.Level == dest.Level && tile.Chebyshev(here, dest) <= radius {
		return true
	}
	defer d.beginWalk()()
	if ok {
		leavePocketFor(d, here, dest)
	}
	res := d.Walker.WalkTo(webwalk.Point{X: dest.X, Z: dest.Z, Level: dest.Level},
		webwalk.WalkOptions{Radius: radius, Timeout: TravelTimeout, AvoidRects: avoid,
			Combat: webwalk.CombatOptions{RunWhenAttacked: true}})
	if res.Arrived() {
		return true
	}
	d.logf("travel to %d,%d,%d: %s (%s), best %d", dest.X, dest.Z, dest.Level, res.Outcome, res.Reason, res.Best)
	at := currentTile(d)
	if at.Level == dest.Level && tile.Chebyshev(at, dest) <= radius+8 {
		return WalkNear(d, dest, radius)
	}
	return false
}

// CurrentLevel is a skill's current (boosted or drained) level: Prayer points, say.
func CurrentLevel(d *Deps, skill string) int {
	skills := d.Session.State().Skills()
	for i, sk := range skills {
		if strings.EqualFold(gamestate.SkillName(i), skill) {
			return sk.Level
		}
	}
	return 0
}

// IsMakeMenu reports an open make menu (a furnace's, a range's).
func IsMakeMenu(d *Deps) bool {
	return actions.NewChatDialog(d.Session, d.Components).IsMakeMenu()
}

// MakeOne answers an open make menu with one of the product whose name matches.
func MakeOne(d *Deps, match string) error {
	return actions.NewChatDialog(d.Session, d.Components).MakeOne(d.Config, match, waiterFor(d))
}

// UseItemOnItem uses one held item on another (flour on a dagger, flypaper on a dusted cup).
func UseItemOnItem(d *Deps, itemID, targetID int) error {
	if !PackVisible(d) {
		return errors.New("the backpack is not visible to the server")
	}
	if HeldID(d, itemID) == 0 || HeldID(d, targetID) == 0 {
		return fmt.Errorf("%s or %s is not in the pack", ObjName(d, itemID), ObjName(d, targetID))
	}
	ClearLeftoverModal(d)
	return actions.NewEquipment(d.Session, d.Components).UseOnHeld(int32(itemID), int32(targetID))
}

// Attacker is an NPC that is fighting the account: the one auto-retaliate turned it toward, or
// any NPC facing it that offers Attack.
func Attacker(d *Deps) (entityquery.Entity, bool) {
	st := d.Session.State()
	self := st.Self()
	face := st.SelfFaceEntity()
	var best entityquery.Entity
	found := false
	for _, e := range entityquery.Npcs(st, d.Config).Within(8).Results() {
		targets := e.Index == face || (self >= 0 && e.FaceEntity == self+32768)
		if !targets {
			continue
		}
		attackable := false
		for _, op := range e.Ops {
			if strings.EqualFold(op, "Attack") {
				attackable = true
			}
		}
		if attackable && (!found || e.Distance < best.Distance) {
			best, found = e, true
		}
	}
	return best, found
}

// FightBack attacks one NPC by its index (not the nearest of its kind) until it is gone, it
// stops fighting the account, the account dies, or a minute passes, eating by the sustain. It is
// the same loop as a fight step (meleeFight): the attack is sent again after a bite, and a fight
// that is being lost is given up. A foe the content scripts the account to lose to (questplan.ScriptedLoss,
// Koschei's fourth form) is never given up: the loop reads it off the npc now in view, so a fight that began
// against an earlier form is covered when the form changes under it.
func FightBack(d *Deps, npc entityquery.Entity, sus questplan.Sustain) bool {
	defer ensureRetaliate(d)()
	sus, foe, _ := VsFoe(d, sus, npc.ID) // the eat line from the foe's hardest hit when the tables know it
	f := newLiveFight(d, npc, sus)
	return finishFight(d, f, meleeFightVs(f, sus, foe.PeakHit(), time.Minute, nil))
}
