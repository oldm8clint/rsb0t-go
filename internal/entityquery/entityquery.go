// Package entityquery ports the TS scripting API's EntityQuery builder and its Npcs, Players,
// Locs and GroundItems roots (reference/server-api-docs.md, "Entities"; upstream
// src/bot/api/query/Query.ts). A query filters snapshots of one entity kind by name, option,
// distance and area, then answers with all results, the nearest, the first, whether any exist,
// or how many.
//
// Semantics follow upstream:
//
//   - Name matches the whole config name, trimmed, ignoring case; an entity without a name
//     never matches.
//   - Action matches an option label exactly, ignoring case (hidden options are already
//     blank in internal/cachecfg).
//   - Distance is Chebyshev from the player's tile when the snapshot was taken, and only the
//     player's level is included, as the client's scene arrays are per level.
//   - Terminals re-read the source, so a query can be kept and asked again next tick.
//
// Unlike the TS scene arrays, gamestate keeps entities in maps, so each snapshot is sorted by
// distance, then x, z, index and id: Results, First and Nearest are deterministic.
//
// Locs merges internal/locindex's map-file locs, which the server never sends, with the live
// loc changes gamestate decodes, the same way locindex.Index.Find does. Upstream notes loc
// queries can come back empty for about a tick after a level change; do not treat a blank
// result straight after one as absence.
package entityquery

import (
	"errors"
	"sort"
	"strings"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
)

// Kind is the kind of entity an Entity describes.
type Kind int

const (
	KindNpc Kind = iota
	KindPlayer
	KindLoc
	KindGroundItem
)

// SceneRadius is half the client's 104-tile build area, the reach of a loc query when the
// caller gives no radius.
const SceneRadius = 52

// Types resolves config names and options; *cachecfg.Config satisfies it.
type Types interface {
	Npc(id int) (cachecfg.Npc, bool)
	Obj(id int) (cachecfg.Obj, bool)
	Loc(id int) (cachecfg.Loc, bool)
}

// ErrNoActions is returned by Interact for a player, which has no interact options upstream.
var ErrNoActions = errors.New("entityquery: this entity kind has no interact options")

// Entity is one snapshot row.
type Entity struct {
	Kind Kind
	// Index is the NPC or player index, -1 for locs and ground items.
	Index int
	// ID is the NPC type, loc type or object id, -1 for players.
	ID   int
	Name string
	// Ops are the option labels, "" where absent. Ground items get the default "Take".
	Ops         [5]string
	X, Z, Level int
	// Distance is the Chebyshev distance from the player when the snapshot was taken.
	Distance int
	// CombatLevel is an NPC's config combat level (-1 when it has none).
	CombatLevel int
	// Count is a ground item stack's size.
	Count int
	// Shape and Angle place a loc; Live marks a loc from a server change rather than the map.
	Shape, Angle int
	Live         bool

	// Anim, FaceEntity, Health, TotalHealth and InCombat mirror gamestate.NPC for NPCs (Anim and
	// FaceEntity are -1 when absent); players carry FaceEntity and InCombat.
	Anim, FaceEntity    int
	Health, TotalHealth int
	InCombat            bool
}

// Actions ports the entities' actions(): the present option labels in slot order.
func (e Entity) Actions() []string {
	var out []string
	for _, op := range e.Ops {
		if op != "" {
			out = append(out, op)
		}
	}
	return out
}

// Option returns the 1-based option number labelled action, or 0.
func (e Entity) Option(action string) int {
	return actions.OptionIndex(e.Ops, action)
}

// Area is an inclusive tile rectangle, the argument of Query.Inside.
type Area struct {
	MinX, MaxX, MinZ, MaxZ int
}

// Query is a chainable filter over a snapshot source. Filter methods add to the query and
// return it.
type Query struct {
	supply  func() []Entity
	filters []func(Entity) bool
}

// FromFunc builds a query over a source called afresh by every terminal.
func FromFunc(supply func() []Entity) *Query {
	return &Query{supply: supply}
}

// FromEntities builds a query over a fixed snapshot.
func FromEntities(entities []Entity) *Query {
	return FromFunc(func() []Entity { return entities })
}

// Name keeps entities whose name equals one of names, trimmed and ignoring case.
func (q *Query) Name(names ...string) *Query {
	wanted := make([]string, len(names))
	for i, n := range names {
		wanted[i] = strings.ToLower(strings.TrimSpace(n))
	}
	return q.Where(func(e Entity) bool {
		if e.Name == "" {
			return false
		}
		name := strings.ToLower(strings.TrimSpace(e.Name))
		for _, w := range wanted {
			if name == w {
				return true
			}
		}
		return false
	})
}

// Action keeps entities with an option labelled action, ignoring case.
func (q *Query) Action(action string) *Query {
	return q.Where(func(e Entity) bool { return e.Option(action) != 0 })
}

// Within keeps entities at most dist tiles from the player.
func (q *Query) Within(dist int) *Query {
	return q.Where(func(e Entity) bool { return e.Distance <= dist })
}

// WithinOf keeps entities within a Chebyshev radius of (x, z); a negative dist is 0.
func (q *Query) WithinOf(x, z, dist int) *Query {
	if dist < 0 {
		dist = 0
	}
	return q.Where(func(e Entity) bool { return chebyshev(e.X, e.Z, x, z) <= dist })
}

// Inside keeps entities whose tile lies in area, bounds included.
func (q *Query) Inside(area Area) *Query {
	return q.Where(func(e Entity) bool {
		return e.X >= area.MinX && e.X <= area.MaxX && e.Z >= area.MinZ && e.Z <= area.MaxZ
	})
}

// Where keeps entities pred accepts.
func (q *Query) Where(pred func(Entity) bool) *Query {
	q.filters = append(q.filters, pred)
	return q
}

// Results returns every entity passing all filters, in snapshot order.
func (q *Query) Results() []Entity {
	var out []Entity
	for _, e := range q.supply() {
		if q.matches(e) {
			out = append(out, e)
		}
	}
	return out
}

func (q *Query) matches(e Entity) bool {
	for _, f := range q.filters {
		if !f(e) {
			return false
		}
	}
	return true
}

// Nearest returns the closest result; ties go to the earlier one in snapshot order.
func (q *Query) Nearest() (Entity, bool) {
	var best Entity
	found := false
	for _, e := range q.supply() {
		if q.matches(e) && (!found || e.Distance < best.Distance) {
			best, found = e, true
		}
	}
	return best, found
}

// NearestPreferLocal ports nearestPreferLocal: when any result lies within preferRadius of the
// player, only those are considered.
func (q *Query) NearestPreferLocal(preferRadius int) (Entity, bool) {
	// If any match is within the radius, the minimum-distance match is too.
	// Otherwise the contract falls back to that same global minimum.
	return q.Nearest()
}

// First returns the first result.
func (q *Query) First() (Entity, bool) {
	var first Entity
	found := false
	for _, e := range q.supply() {
		// Keep evaluating predicates after finding a result, as Results did.
		if q.matches(e) && !found {
			first, found = e, true
		}
	}
	return first, found
}

// Exists reports whether any entity passes the filters.
func (q *Query) Exists() bool { return q.Count() > 0 }

// Count returns how many entities pass the filters.
func (q *Query) Count() int {
	count := 0
	for _, e := range q.supply() {
		if q.matches(e) {
			count++
		}
	}
	return count
}

// Npcs ports Npcs.query().
func Npcs(st *gamestate.State, types Types) *Query {
	return FromFunc(func() []Entity { return NpcEntities(st, types) })
}

// NearestNpcs ports Npcs.nearest(count): the count closest NPCs.
func NearestNpcs(st *gamestate.State, types Types, count int) []Entity {
	all := NpcEntities(st, types)
	if count < len(all) {
		all = all[:count]
	}
	return all
}

// NpcEntities ports Npcs.all(): every visible NPC on the player's level, nearest first. Names,
// options and combat levels come from types, which may be nil.
func NpcEntities(st *gamestate.State, types Types) []Entity {
	px, pz, level, ok := st.Tile()
	if !ok {
		return nil
	}
	var out []Entity
	for _, n := range st.NPCs() {
		if n.Level != level {
			continue
		}
		e := Entity{Kind: KindNpc, Index: n.Index, ID: n.Type, X: n.X, Z: n.Z, Level: n.Level, CombatLevel: -1, Distance: chebyshev(n.X, n.Z, px, pz), Anim: n.Anim, FaceEntity: n.FaceEntity, Health: n.Health, TotalHealth: n.TotalHealth, InCombat: n.InCombat}
		if types != nil {
			if cfg, ok := types.Npc(n.Type); ok {
				e.Name, e.Ops, e.CombatLevel = cfg.Name, cfg.Ops, cfg.Level
			}
		}
		out = append(out, e)
	}
	sortEntities(out)
	return out
}

// Players ports Players.query().
func Players(st *gamestate.State) *Query {
	return FromFunc(func() []Entity { return PlayerEntities(st) })
}

// PlayerEntities is every other visible player on the player's level, nearest first. Players
// have no options.
func PlayerEntities(st *gamestate.State) []Entity {
	px, pz, level, ok := st.Tile()
	if !ok {
		return nil
	}
	var out []Entity
	for _, p := range st.Players() {
		if p.Level != level {
			continue
		}
		out = append(out, Entity{Kind: KindPlayer, Index: p.Index, ID: -1, Name: p.Name, X: p.X, Z: p.Z, Level: p.Level, CombatLevel: -1, Distance: chebyshev(p.X, p.Z, px, pz), Anim: -1, FaceEntity: p.FaceEntity, InCombat: p.InCombat})
	}
	sortEntities(out)
	return out
}

// GroundItems ports GroundItems.query().
func GroundItems(st *gamestate.State, types Types) *Query {
	return FromFunc(func() []Entity { return GroundItemEntities(st, types) })
}

// GroundItemEntities is every known ground item stack on the player's level, nearest first,
// with "Take" filled in as the client does.
func GroundItemEntities(st *gamestate.State, types Types) []Entity {
	px, pz, level, ok := st.Tile()
	if !ok {
		return nil
	}
	var out []Entity
	for _, g := range st.GroundItems() {
		if g.Level != level {
			continue
		}
		e := Entity{Kind: KindGroundItem, Index: -1, ID: g.ID, Count: g.Count, X: g.X, Z: g.Z, Level: g.Level, CombatLevel: -1, Distance: chebyshev(g.X, g.Z, px, pz)}
		var ops [5]string
		if types != nil {
			if cfg, ok := types.Obj(g.ID); ok {
				e.Name, ops = cfg.Name, cfg.Ops
			}
		}
		e.Ops = actions.GroundOps(ops)
		out = append(out, e)
	}
	sortEntities(out)
	return out
}

// Locs ports Locs.query() over map-file locs from ix (nil for live changes only) merged with
// the live loc changes, within radius tiles of the player (SceneRadius when radius <= 0).
func Locs(st *gamestate.State, types Types, ix *locindex.Index, radius int) *Query {
	return FromFunc(func() []Entity { return LocEntities(st, types, ix, radius) })
}

type tileLayer struct{ x, z, layer int }

// LocEntities is the snapshot behind Locs: a live LOC_ADD_CHANGE or LOC_DEL overrides whatever
// the map files place on that tile's layer, as locindex.Index.Find and the client do.
func LocEntities(st *gamestate.State, types Types, ix *locindex.Index, radius int) []Entity {
	px, pz, level, ok := st.Tile()
	if !ok {
		return nil
	}
	if radius <= 0 {
		radius = SceneRadius
	}
	live := locindex.ChangesFrom(st.LocChanges())
	changed := map[tileLayer]bool{}
	for _, c := range live {
		if c.Level == level {
			changed[tileLayer{c.X, c.Z, locindex.Layer(c.Shape)}] = true
		}
	}

	var out []Entity
	add := func(id, x, z, shape, angle int, isLive bool) {
		d := chebyshev(x, z, px, pz)
		if d > radius {
			return
		}
		e := Entity{Kind: KindLoc, Index: -1, ID: id, X: x, Z: z, Level: level, Shape: shape, Angle: angle, Live: isLive, CombatLevel: -1, Distance: d}
		if types != nil {
			if cfg, ok := types.Loc(id); ok {
				e.Name, e.Ops = cfg.Name, cfg.Ops
			}
		}
		out = append(out, e)
	}
	if ix != nil {
		ix.Each(px-radius, pz-radius, px+radius, pz+radius, level, func(l locindex.Loc) {
			if !changed[tileLayer{l.X, l.Z, locindex.Layer(l.Shape)}] {
				add(l.ID, l.X, l.Z, l.Shape, l.Angle, false)
			}
		})
	}
	for _, c := range live {
		if !c.Deleted && c.Level == level {
			add(c.ID, c.X, c.Z, c.Shape, c.Angle, true)
		}
	}
	sortEntities(out)
	return out
}

// Interact ports Npc/Loc/GroundItem.interact: the OP_NPCn, OP_LOCn or OP_OBJn for the option
// labelled action. Like those actions it does not walk; players return ErrNoActions.
func Interact(s actions.Session, e Entity, action string) error {
	if e.Kind == KindPlayer {
		return ErrNoActions
	}
	option := e.Option(action)
	if option == 0 {
		return actions.ErrNoSuchAction
	}
	switch e.Kind {
	case KindNpc:
		return actions.OpNpc(s, option, e.Index)
	case KindLoc:
		return actions.OpLoc(s, option, e.X, e.Z, e.ID)
	case KindGroundItem:
		return actions.OpObj(s, option, e.X, e.Z, e.ID)
	}
	return ErrNoActions
}

func sortEntities(entities []Entity) {
	sort.SliceStable(entities, func(i, j int) bool {
		a, b := entities[i], entities[j]
		if a.Distance != b.Distance {
			return a.Distance < b.Distance
		}
		if a.X != b.X {
			return a.X < b.X
		}
		if a.Z != b.Z {
			return a.Z < b.Z
		}
		if a.Index != b.Index {
			return a.Index < b.Index
		}
		return a.ID < b.ID
	})
}

func chebyshev(ax, az, bx, bz int) int {
	dx, dz := ax-bx, az-bz
	if dx < 0 {
		dx = -dx
	}
	if dz < 0 {
		dz = -dz
	}
	if dx > dz {
		return dx
	}
	return dz
}
