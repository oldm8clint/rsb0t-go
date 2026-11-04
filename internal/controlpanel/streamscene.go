package controlpanel

import (
	"encoding/base64"
	"sort"
	"strconv"
	"time"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
)

// The stream client's opt-in "scene-delta" frame transport.
//
// The original bridge (streamclient.go) hands the browser a whole gamestate.RenderSnapshot
// per observation: every npc, player and loc plus every interface map, ~23 KB a frame. That
// is the right input for the capture adapter's frozen full-window replay, but it is the
// wrong shape for a view that wants to paint 30-60 times a second: most of it never
// changes between two observations 200ms apart, and the part a live view needs to follow —
// who is standing where, what is in the pack, the latest chat line — is small.
//
// A provider that asks for this transport at open time gets instead:
//
//   - a compact streamScene, projected server-side from the same RenderSnapshot the
//     full-snapshot transport sends (so nothing here reads the game any differently, and
//     nothing here can send anything to it): the account's own tile, the npcs and other
//     players it can see with their cache/display names, its backpack, its last few chat
//     lines and its dashboard status line;
//   - on the first frame, on any frame whose base the browser does not confirm it holds,
//     and at least every streamSceneResync, the WHOLE scene (a resync);
//   - otherwise only a streamDelta against the scene the server last sent: entities that
//     appeared or moved, keys that left, backpack slots that changed, and the self, chat
//     and status sections only when they changed.
//
// What it deliberately does NOT carry: locs (no provider draws loc geometry yet, and they
// are most of a full snapshot's bytes), appearance bytes (no provider draws player models),
// and interface text/varps (no provider draws the authentic interfaces). A provider that
// needs any of those uses the full-snapshot transport, which is unchanged.
//
// The observation rate is NOT raised by this transport. The server still takes one
// RenderSnapshot per observation, capped at DefaultStreamMaxFPS, because the game itself
// only changes once per 600ms tick. What delta frames buy is that each observation is small
// enough that the browser can render at its own 15/30/60 FPS, interpolating movement between
// observations, without the transport being the bottleneck.

const (
	streamTransportFull  = "full-snapshot"
	streamTransportScene = "scene-delta"
)

// streamTransports is what /api/meta.streamClient.frameTransports advertises.
var streamTransports = []string{streamTransportFull, streamTransportScene}

// streamSceneResync bounds how long a viewer can go on deltas alone. A delta stream is only
// as good as the base the browser applied it to; a periodic whole scene means any drift (a
// bug on either side, a tab that was throttled in the background) heals itself within this
// window instead of persisting until the view is reopened.
const streamSceneResync = 10 * time.Second

// streamSceneMaxEntities caps how many npcs and players one scene carries, nearest first.
// The game's own client caps visible npcs and players at 255 each; a crowded bank can show
// well over a hundred players, and a provider draws a marker and a label per entity, so the
// frame and the render budget both stay bounded whatever the account is standing in.
const streamSceneMaxEntities = 255

// streamSceneChatLines is how many of the most recent chat lines a scene carries — the
// chat line users asked to see, not the whole 100-line capture history.
const streamSceneChatLines = 5

// streamBackpackComponent is the backpack interface component (inventory 3214 in this
// revision's interface archive — the same confirmed-stable literal fisherrunner.PlayerPack
// and actions/bury.go use).
const streamBackpackComponent = 3214

// StreamNameSource is an optional Source capability: the config archive the fleet already
// loaded, so a scene can carry npc names instead of bare type ids. A Source without it
// (the -fake fleet) sends ids only and the provider labels them by id.
type StreamNameSource interface {
	StreamNames() *cachecfg.Config
}

// StreamNames implements StreamNameSource from the manager's own loaded config archive.
func (f *FleetSource) StreamNames() *cachecfg.Config { return f.objCache() }

type streamScene struct {
	Effects  []gamestate.RenderEffect `json:"effects"`
	Tick     int                      `json:"tick"`
	Self     streamSelf               `json:"self"`
	Entities []streamEntity           `json:"entities"`
	Inv      [][2]int32               `json:"inv"`
	Chat     []streamChatLine         `json:"chat"`
	Status   streamStatus             `json:"status"`
	// Looks is each player's raw PLAYER_INFO appearance block, base64, keyed like
	// entities ("p<index>", and "self" for the account): what a provider needs to build
	// the player's real cache model (body kits, worn items, colours). Kept apart from
	// Entities so a player walking does not resend its appearance every step.
	Looks map[string]string `json:"looks"`
	// Objs are ground item stacks on the account's level as [x, z, id, count], nearest
	// streamSceneMaxObjs first, sorted by tile then id.
	Objs [][4]int `json:"objs"`
	// Locs are the session's LIVE loc changes as [x, z, level, type, shape, rotate] (type
	// -1 for a removed loc: an opened gate, a cut tree), nearest streamSceneMaxLocs first,
	// sorted. The static map's own locs are not sent: a provider decodes those from the cache
	// and applies these on top, exactly as the game client does.
	Locs [][6]int `json:"locs"`
	// UI is the session's interface state (streamui.go), for the game's own interfaces.
	UI gamestate.RenderUI `json:"ui"`
}

// streamSceneMaxObjs caps ground item stacks per scene, nearest first.
const streamSceneMaxObjs = 256

// streamSceneMaxLocs caps live loc changes per scene, nearest first.
const streamSceneMaxLocs = 1024

// streamSelf is the account itself. X/Z/Level/CentreX/CentreZ are exactly the
// RenderSnapshot fields the capture adapter's terrainGpuMesh needs, under the same names.
type streamSelf struct {
	Combat *gamestate.RenderCombat `json:"combat,omitempty"`
	// A is the account's current animation sequence plus one (0 = none) and AT the scene
	// tick it started on, as the session decoded them from PLAYER_INFO.
	A       int    `json:"a,omitempty"`
	AT      int    `json:"at,omitempty"`
	X       int    `json:"x"`
	Z       int    `json:"z"`
	Level   int    `json:"level"`
	CentreX int    `json:"centreX"`
	CentreZ int    `json:"centreZ"`
	Slot    int    `json:"slot"`
	Name    string `json:"name,omitempty"`
}

// streamEntity is one visible npc ("n<index>") or other player ("p<index>"). Type is the
// npc type id; Size is the npc's tile footprint when it is larger than one.
type streamEntity struct {
	Combat *gamestate.RenderCombat `json:"combat,omitempty"`
	Key    string                  `json:"k"`
	X      int                     `json:"x"`
	Z      int                     `json:"z"`
	Type   int                     `json:"t,omitempty"`
	Size   int                     `json:"s,omitempty"`
	Name   string                  `json:"n,omitempty"`
	// A is the entity's current animation sequence plus one (0 = none), AT the scene tick
	// it started on (a new tick is a restart), F who it faces plus one (0 = nobody; a player
	// is its slot + 32768, as in the protocol).
	A  int `json:"a,omitempty"`
	AT int `json:"at,omitempty"`
	F  int `json:"f,omitempty"`
}

type streamChatLine struct {
	Type     int    `json:"type"`
	Username string `json:"user,omitempty"`
	Text     string `json:"text"`
}

// streamStatus is the dashboard's own status line for the account (its bot, activity and
// hitpoints, from the Source's account list) plus the run energy/weight the session
// decoded. Unknown values are omitted rather than sent as zero.
type streamStatus struct {
	Bot       string `json:"bot,omitempty"`
	BotState  string `json:"botState,omitempty"`
	Activity  string `json:"activity,omitempty"`
	HPKnown   bool   `json:"hpKnown,omitempty"`
	HP        int    `json:"hp,omitempty"`
	MaxHP     int    `json:"maxHp,omitempty"`
	RunEnergy *int   `json:"run,omitempty"`
	Weight    *int   `json:"weight,omitempty"`
}

func (a streamStatus) equal(b streamStatus) bool {
	intPtrEqual := func(x, y *int) bool { return (x == nil) == (y == nil) && (x == nil || *x == *y) }
	return a.Bot == b.Bot && a.BotState == b.BotState && a.Activity == b.Activity && a.HPKnown == b.HPKnown &&
		a.HP == b.HP && a.MaxHP == b.MaxHP && intPtrEqual(a.RunEnergy, b.RunEnergy) && intPtrEqual(a.Weight, b.Weight)
}

func statusFromAccount(a *AccountStatus) streamStatus {
	return streamStatus{Bot: a.Bot, BotState: a.BotState, Activity: a.Activity, HPKnown: a.HPKnown, HP: a.HP, MaxHP: a.MaxHP}
}

// streamDelta is everything that changed between the scene the server last sent (Base is
// that frame's seq) and this one. Absent sections did not change.
type streamDelta struct {
	Effects *[]gamestate.RenderEffect `json:"effects,omitempty"`
	Base    int64                     `json:"base"`
	Self    *streamSelf               `json:"self,omitempty"`
	Upsert  []streamEntity            `json:"upsert,omitempty"`
	Remove  []string                  `json:"remove,omitempty"`
	// Inv lists changed backpack slots as [slot, id, count]; count 0 empties the slot.
	// InvLen is set only when the backpack's slot count itself changed.
	Inv    [][3]int32        `json:"inv,omitempty"`
	InvLen *int              `json:"invLen,omitempty"`
	Chat   *[]streamChatLine `json:"chat,omitempty"`
	Status *streamStatus     `json:"status,omitempty"`
	// Looks carries only appearances that are new or changed; a removed player's look
	// leaves with its entity.
	Looks map[string]string `json:"looks,omitempty"`
	Objs  *[][4]int         `json:"objs,omitempty"`
	Locs  *[][6]int         `json:"locs,omitempty"`
	UI    *streamUIDelta    `json:"ui,omitempty"`
}

// npcLookup resolves an npc type id to its cache name and tile footprint ("", 0 when unknown).
type npcLookup func(typeID int) (name string, size int)

// npcLookupFrom adapts a loaded config archive; nil stays nil (ids only).
func npcLookupFrom(c *cachecfg.Config) npcLookup {
	if c == nil {
		return nil
	}
	return func(id int) (string, int) {
		if t, ok := c.Npc(id); ok {
			return t.Name, t.Size
		}
		return "", 0
	}
}

// projectStreamScene builds the compact scene from one observation. Pure: no Source, lock
// or clock, so the whole projection is unit-testable.
func projectStreamScene(snap *gamestate.RenderSnapshot, status streamStatus, names npcLookup) streamScene {
	out := streamScene{
		Tick:    snap.UI.Tick,
		Effects: snap.Effects,
		Self:    streamSelf{Combat: streamCombat(snap.Combat, snap.UI.Tick), A: snap.Animation + 1, AT: snap.AnimationTick, X: snap.X, Z: snap.Z, Level: snap.Level, CentreX: snap.CentreX, CentreZ: snap.CentreZ, Slot: snap.SelfSlot, Name: snap.UI.Name},
	}
	entities := make([]streamEntity, 0, len(snap.NPCs)+len(snap.Players))
	for _, n := range snap.NPCs {
		e := streamEntity{Combat: streamCombatOf(n.Combat, snap.UI.Tick), Key: "n" + strconv.Itoa(n.Index), X: n.X, Z: n.Z, Type: n.Type, A: n.Anim + 1, AT: n.AnimTick, F: n.FaceEntity + 1}
		if names != nil {
			name, size := names(n.Type)
			e.Name = name
			if size > 1 {
				e.Size = size
			}
		}
		entities = append(entities, e)
	}
	for _, p := range snap.Players {
		if p.Index == snap.SelfSlot {
			continue
		}
		entities = append(entities, streamEntity{Combat: streamCombat(p.Combat, snap.UI.Tick), Key: "p" + strconv.Itoa(p.Index), X: p.X, Z: p.Z, Name: p.Name, A: p.Anim + 1, AT: p.AnimTick, F: p.FaceEntity + 1})
	}
	if len(entities) > streamSceneMaxEntities {
		// Nearest first (Chebyshev tiles, the game's own view distance measure), ties by key
		// so two observations of the same crowd keep the same subset.
		dist := func(e streamEntity) int {
			dx, dz := e.X-snap.X, e.Z-snap.Z
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
		sort.Slice(entities, func(i, j int) bool {
			di, dj := dist(entities[i]), dist(entities[j])
			if di != dj {
				return di < dj
			}
			return entities[i].Key < entities[j].Key
		})
		entities = entities[:streamSceneMaxEntities]
	}
	sort.Slice(entities, func(i, j int) bool { return entities[i].Key < entities[j].Key })
	out.Entities = entities

	out.Inv = [][2]int32{}
	if inv, ok := snap.UI.Inventories[streamBackpackComponent]; ok {
		out.Inv = make([][2]int32, len(inv.Slots))
		for i, it := range inv.Slots {
			if it.Count > 0 {
				out.Inv[i] = [2]int32{it.ID, it.Count}
			}
		}
	}
	chat := snap.UI.Chat
	if len(chat) > streamSceneChatLines {
		chat = chat[len(chat)-streamSceneChatLines:]
	}
	out.Chat = make([]streamChatLine, len(chat))
	for i, line := range chat {
		out.Chat[i] = streamChatLine{Type: line.Type, Username: line.Username, Text: line.Text}
	}
	status.RunEnergy = snap.UI.RunEnergy
	status.Weight = snap.UI.Weight
	out.Status = status

	out.Looks = make(map[string]string, len(entities)+1)
	if len(snap.Appearance) > 0 {
		out.Looks["self"] = base64.StdEncoding.EncodeToString(snap.Appearance)
	}
	kept := make(map[string]bool, len(entities))
	for _, e := range entities {
		kept[e.Key] = true
	}
	for _, p := range snap.Players {
		key := "p" + strconv.Itoa(p.Index)
		if kept[key] && len(p.Appearance) > 0 {
			out.Looks[key] = base64.StdEncoding.EncodeToString(p.Appearance)
		}
	}

	out.Objs = [][4]int{}
	for _, o := range snap.Objs {
		if o.Level == snap.Level && o.Count > 0 {
			out.Objs = append(out.Objs, [4]int{o.X, o.Z, o.ID, o.Count})
		}
	}
	cheb := func(o [4]int) int {
		dx, dz := o[0]-snap.X, o[1]-snap.Z
		if dx < 0 {
			dx = -dx
		}
		if dz < 0 {
			dz = -dz
		}
		return max(dx, dz)
	}
	less := func(a, b [4]int) bool {
		for i := range a {
			if a[i] != b[i] {
				return a[i] < b[i]
			}
		}
		return false
	}
	if len(out.Objs) > streamSceneMaxObjs {
		sort.Slice(out.Objs, func(i, j int) bool {
			if di, dj := cheb(out.Objs[i]), cheb(out.Objs[j]); di != dj {
				return di < dj
			}
			return less(out.Objs[i], out.Objs[j])
		})
		out.Objs = out.Objs[:streamSceneMaxObjs]
	}
	sort.Slice(out.Objs, func(i, j int) bool { return less(out.Objs[i], out.Objs[j]) })

	out.Locs = make([][6]int, 0, len(snap.Locs))
	for _, l := range snap.Locs {
		t := l.Type
		if l.Deleted {
			t = -1
		}
		out.Locs = append(out.Locs, [6]int{l.X, l.Z, l.Level, t, l.Shape, l.Rotate})
	}
	lessLoc := func(a, b [6]int) bool {
		for i := range a {
			if a[i] != b[i] {
				return a[i] < b[i]
			}
		}
		return false
	}
	if len(out.Locs) > streamSceneMaxLocs {
		d := func(l [6]int) int { return cheb([4]int{l[0], l[1]}) }
		sort.Slice(out.Locs, func(i, j int) bool {
			if di, dj := d(out.Locs[i]), d(out.Locs[j]); di != dj {
				return di < dj
			}
			return lessLoc(out.Locs[i], out.Locs[j])
		})
		out.Locs = out.Locs[:streamSceneMaxLocs]
	}
	sort.Slice(out.Locs, func(i, j int) bool { return lessLoc(out.Locs[i], out.Locs[j]) })
	out.UI = streamUIOf(snap.UI)
	return out
}

// diffStreamScene returns the delta that turns prev into next. Pure.
func diffStreamScene(base int64, prev, next *streamScene) streamDelta {
	d := streamDelta{Base: base}
	if !selfEqual(prev.Self, next.Self) {
		self := next.Self
		d.Self = &self
	}
	if !effectsEqual(prev.Effects, next.Effects) {
		effects := next.Effects
		if effects == nil {
			effects = []gamestate.RenderEffect{}
		}
		d.Effects = &effects
	}
	old := make(map[string]streamEntity, len(prev.Entities))
	for _, e := range prev.Entities {
		old[e.Key] = e
	}
	for _, e := range next.Entities {
		if was, ok := old[e.Key]; !ok || !entityEqual(was, e) {
			d.Upsert = append(d.Upsert, e)
		}
		delete(old, e.Key)
	}
	for key := range old {
		d.Remove = append(d.Remove, key)
	}
	sort.Strings(d.Remove)
	if len(prev.Inv) != len(next.Inv) {
		n := len(next.Inv)
		d.InvLen = &n
	}
	for i, slot := range next.Inv {
		if i >= len(prev.Inv) || prev.Inv[i] != slot {
			d.Inv = append(d.Inv, [3]int32{int32(i), slot[0], slot[1]})
		}
	}
	if !chatEqual(prev.Chat, next.Chat) {
		chat := next.Chat
		d.Chat = &chat
	}
	if !prev.Status.equal(next.Status) {
		status := next.Status
		d.Status = &status
	}
	for key, look := range next.Looks {
		if prev.Looks[key] != look {
			if d.Looks == nil {
				d.Looks = map[string]string{}
			}
			d.Looks[key] = look
		}
	}
	if !objsEqual(prev.Objs, next.Objs) {
		objs := next.Objs
		d.Objs = &objs
	}
	if !locsEqual(prev.Locs, next.Locs) {
		locs := next.Locs
		d.Locs = &locs
	}
	if ui := diffStreamUI(&prev.UI, &next.UI); !ui.empty() {
		d.UI = &ui
	}
	return d
}

func locsEqual(a, b [][6]int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func objsEqual(a, b [][4]int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func chatEqual(a, b []streamChatLine) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// empty reports whether the delta carries no change at all.
func (d *streamDelta) empty() bool {
	return d.Effects == nil && d.Self == nil && len(d.Upsert) == 0 && len(d.Remove) == 0 && len(d.Inv) == 0 && d.InvLen == nil && d.Chat == nil && d.Status == nil && len(d.Looks) == 0 && d.Objs == nil && d.Locs == nil && d.UI == nil
}

func effectsEqual(a, b []gamestate.RenderEffect) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// streamCombatOf is streamCombat for an NPC's combat state, which is nil outside a render
// snapshot.
func streamCombatOf(c *gamestate.RenderCombat, tick int) *gamestate.RenderCombat {
	if c == nil {
		return nil
	}
	return streamCombat(*c, tick)
}

// Drop expired combat state from compact observations; preserve comparable state values.
func streamCombat(c gamestate.RenderCombat, tick int) *gamestate.RenderCombat {
	if c.Seen && tick-c.Tick >= 10 {
		c.Seen = false
	}
	if c.Spot.Seen && tick-c.Spot.Tick >= 12+c.Spot.Delay/30 {
		c.Spot.Seen = false
	}
	active := c.Seen || c.Spot.Seen
	for i := range c.Hits {
		if c.Hits[i].Seen && tick-c.Hits[i].Tick >= 3 {
			c.Hits[i] = gamestate.RenderHit{}
		}
		active = active || c.Hits[i].Seen
	}
	if !active {
		return nil
	}
	return &c
}
func combatEqual(a, b *gamestate.RenderCombat) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}
func selfEqual(a, b streamSelf) bool {
	if !combatEqual(a.Combat, b.Combat) {
		return false
	}
	a.Combat = nil
	b.Combat = nil
	return a == b
}
func entityEqual(a, b streamEntity) bool {
	if !combatEqual(a.Combat, b.Combat) {
		return false
	}
	a.Combat = nil
	b.Combat = nil
	return a == b
}
