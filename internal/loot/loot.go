// Package loot records what an account actually gains from a loot SOURCE — an NPC it killed
// or pickpocketed, or an item it opened (a casket, an oyster) — so the fleet can show the owner
// the fleet's real drops per source (2026-09-19: "the loot that Big net fishing brings,
// especially what comes out of caskets and oysters", widened the same day to every source).
//
// It is deliberately measurement-only: a script snapshots the pack before the action that
// yields loot and again after it, and Gained reports the positive difference. Nothing here
// predicts or guesses a drop table; an item is counted only when it really arrived.
//
// The package is pure apart from one tiny read of a gamestate.Inventory. A script reports a
// Drop through bot.Context.Loot (nil-safe: a script run by a bare test Context records
// nothing), internal/fleet keeps a Tally per account, and internal/controlpanel folds the
// tallies into its persisted run statistics (lootstats.go).
package loot

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
)

// Kinds group sources for the panel's filter.
const (
	KindNPC      = "npc"
	KindOpenable = "openable"
	// KindTrail is a finished treasure trail. Nothing reports one yet (no clue solver runs on
	// the accounts); the Loot panel, which absorbed the old Treasure trails placeholder, shows
	// it the day one does.
	KindTrail = "trail"
)

// Actions name how the loot was taken. The same NPC killed and pickpocketed is two sources:
// "kills" and "pickpockets" are different counts with different drops.
const (
	ActionKill       = "kill"
	ActionPickpocket = "pickpocket"
	ActionOpen       = "open"
	ActionComplete   = "complete"
)

// Source is where loot came from. ID is the npc type id for an NPC and the obj id for an
// openable, -1 when unknown; Level is the npc's combat level (0 for openables), or, on a Runecraft
// source, the Runecraft level the account last read after a craft (runecraft.go).
type Source struct {
	Kind   string `json:"kind"`
	Action string `json:"action"`
	Name   string `json:"name"`
	ID     int    `json:"id"`
	Level  int    `json:"level,omitempty"`
	// Model is ModelNPC, ModelObj or ModelLoc when Kind alone does not say which cache model the
	// panel draws (a random event: see randomevent.go; a gathering source: see gather.go; a
	// thieving source: see thieving.go). Empty for npc and openable sources.
	Model string `json:"model,omitempty"`
	// Skill names the skill a source belongs to, and titles its card: the gathering skill of a
	// KindGathering source (gather.go), the production skill of a KindProduction one
	// (production.go, prayer.go) or Thieving for a KindThieving one (thieving.go). Empty for
	// every other kind. Setting it is what makes GroupKey collapse every source of that skill
	// onto one card.
	Skill string `json:"skill,omitempty"`
}

// Key is the stable identity a Source is tallied and persisted under. Names are compared
// case-insensitively, so "Man" and "man" are one source.
func (s Source) Key() string {
	return fmt.Sprintf("%s:%s:%d", s.Action, strings.ToLower(strings.TrimSpace(s.Name)), s.ID)
}

// GroupKey is the identity of the CARD a source is shown on, which is not its own identity:
// several ids usually share one name (the three "Man" npcs the fleet pickpockets, every fishing
// spot npc in content 289) and the panel draws them as one card.
//
// It is here rather than only in the panel because a figure like "how many accounts" has to be
// computed over the whole group and cannot be recovered from the per-source numbers afterwards.
// The Loot panel reported 154 accounts on the Fishing spot card against 269 logged in (owner,
// 2026-09-20), because merging took the MAXIMUM accounts across the merged sources — correct as
// far as it went, since the same account fishes several spots and summing would have claimed
// more accounts than the fleet has, but an undercount whenever no single spot was fished by
// everyone. The union is the honest number, and only the server can count it.
//
// Kind and Action are part of it because a Man killed and a Man pickpocketed are genuinely
// different rows of loot. Must match groupKeyOf in web/panels/loot.js.
//
// A source that NAMES A SKILL groups on that skill instead of on its name, which is what puts
// every tree on one Woodcutting card and every rock on one Mining card (owner, 2026-09-20: "one
// card per gathering skill ... can be titled whatever skill they are from"). Grouping them by
// name would give a card per tree species. The per-source rows are untouched either way — only
// the card they appear on changes.
//
// The KIND stays in the key, so a Woodcutting gathering card and a Fletching production card can
// never merge even if a later kind reused a skill name. Only KindGathering set a Skill until
// 2026-09-21; KindProduction now does too (production.go, prayer.go), and KindThieving since
// 2026-09-23 (thieving.go). The rule is written on the Skill rather than on the kind, so none of
// them needed an edit here — which is also why pickpockets and stalls had to become ONE kind to
// share a card: with the kind in the key, a Skill on KindNPC and KindLoc would have made two.
func (s Source) GroupKey() string {
	if s.Skill != "" {
		return fmt.Sprintf("%s|%s", s.Kind, strings.ToLower(s.Skill))
	}
	// EVERY RANDOM EVENT IS ONE CARD (a user request: "all random events should be given just 1
	// consolidated panel"). Seven events were seven cards, each drawing its own npc model (the
	// Genie's, off a record under the wrong id, was a Swarm). They merge on the kind alone, and
	// PartKey (below) still keeps each event as its own row inside the card, so the per-event
	// counts and drops are exactly what they were.
	if s.Kind == KindRandomEvent {
		return s.Kind
	}
	return fmt.Sprintf("%s|%s|%s", s.Kind, s.Action, strings.ToLower(strings.TrimSpace(s.Name)))
}

// PartKey is the row a source contributes to INSIDE its card: kind, action and name, which is
// what GroupKey was for every source before skill cards existed.
//
// A skill card merges sources that differ in name — a Man, a Guard and the Baker's stall on the
// Thieving card, a Tree and an Oak on the Woodcutting card — and the owner still wants to see
// which target gave what (2026-09-23: all thieving on one panel, with the breakdown inside it).
// Several ids that share a name are still one row: the three Man npcs are one person to anyone
// reading the panel. For a card that is not a skill card PartKey equals GroupKey, so the card has
// exactly one row. Must match partKeyOf in web/panels/loot.js.
func (s Source) PartKey() string {
	return fmt.Sprintf("%s|%s|%s", s.Kind, s.Action, strings.ToLower(strings.TrimSpace(s.Name)))
}

// NPC is a kill or pickpocket source for one npc type, resolved through cfg when it is non-nil
// so the name and combat level are the cache's own. A pickpocket comes back as a KindThieving
// source (thieving.go), so every target lands on the one Thieving card; a kill stays KindNPC.
func NPC(cfg *cachecfg.Config, action string, typeID int, fallbackName string) Source {
	src := Source{Kind: KindNPC, Action: action, Name: fallbackName, ID: typeID}
	if cfg != nil {
		if n, ok := cfg.Npc(typeID); ok {
			if n.Name != "" {
				src.Name = n.Name
			}
			src.Level = n.Level
		}
	}
	return thievingShape(sharedKill(src))
}

// Openable is an "open" source for one item (Casket, Oyster).
func Openable(name string, objID int) Source {
	if IsOysterName(name) {
		return oysterOpen(objID, name)
	}
	return Source{Kind: KindOpenable, Action: ActionOpen, Name: name, ID: objID}
}

// OysterName is the openable the Big net spots bring up, whose pearls the fleet sells.
const OysterName = "Oyster"

// IsOysterName reports whether an openable's name is the oyster's.
func IsOysterName(name string) bool { return strings.EqualFold(strings.TrimSpace(name), OysterName) }

// oysterOpen is the source of one oyster opened. It lives on the FISHING card, not an Openables card
// of its own (a user request: "oyster tab: count the oyster's cost and merge it into the fishing
// tab"): an oyster is a catch, it was counted as loot on that card when it was caught, and opening it
// consumes it for its pearls, so the row is net of the oyster itself (Source.Consumes) and the
// Fishing card reads as what fishing really earned. Key() is action:name:id, which does not mention
// the kind, so every oyster record persisted before this keeps the key it was written under (Migrate
// rewrites its shape on load).
//
// THE CLOSED OYSTER IS NEVER SHOWN (a user request: "the closed oyster shouldnt show anywhere on
// the loot panel just oyster pearls"). So the row is named and drawn as what it gives, Oyster pearls,
// and the Oyster and the Empty oyster are never loot anywhere (HiddenItem). With the oyster no longer
// counted as a catch, opening one costs nothing on the card, so the row is not netted of it either:
// the pearls are what the Big net earned.
func oysterOpen(objID int, name string) Source {
	return Source{Kind: KindGathering, Skill: SkillFishing, Action: ActionOpen, Name: OysterPearlsName, ID: OysterPearlID, Model: ModelObj}
}

// The oyster objs (content 289 obj.pack): the closed Oyster the Big net brings up, the Empty oyster
// an open without a pearl leaves, and the Oyster pearl the row is drawn with.
const (
	OysterID         = 407
	EmptyOysterID    = 409
	OysterPearlID    = 411
	OysterPearlsName = "Oyster pearls"
)

// IsOysterPearlsRow reports whether src is the Fishing card's oyster row, in today's shape.
func IsOysterPearlsRow(src Source) bool {
	return src.Kind == KindGathering && src.Action == ActionOpen && strings.EqualFold(strings.TrimSpace(src.Name), OysterPearlsName)
}

// HiddenItem reports whether an obj is never loot on any card: the closed Oyster and the Empty
// oyster (oysterOpen says why).
func HiddenItem(id int) bool { return id == OysterID || id == EmptyOysterID }

// WithoutHiddenItems is items less every HiddenItem, items itself when there is none.
func WithoutHiddenItems(items []Item) []Item {
	hidden := 0
	for _, it := range items {
		if HiddenItem(it.ID) {
			hidden++
		}
	}
	if hidden == 0 {
		return items
	}
	out := make([]Item, 0, len(items)-hidden)
	for _, it := range items {
		if !HiddenItem(it.ID) {
			out = append(out, it)
		}
	}
	return out
}

// SHARED KILL TABLES (a user request: "barbarian and barbarian woman should be combined as they
// have the same loot table"). A kill of an npc named on the left is recorded, and an old record is
// read back (Migrate), as a kill of the npc on the right, so both are one card and one row.
var sharedKillTable = map[string]string{
	"barbarian woman": "Barbarian",
}

// sharedKill renames a kill whose npc shares another's loot table (sharedKillTable).
func sharedKill(s Source) Source {
	if s.Kind != KindNPC || s.Action != ActionKill {
		return s
	}
	if to, ok := sharedKillTable[strings.ToLower(strings.TrimSpace(s.Name))]; ok {
		s.Name = to
	}
	return s
}

// Trail is a "complete" source for one clue scroll tier, named for the scroll (obj id objID).
func Trail(name string, objID int) Source {
	return Source{Kind: KindTrail, Action: ActionComplete, Name: name, ID: objID}
}

// Item is one reward item and how many of it. Value is the cache's unit cost (obj "cost"),
// 0 when unknown; it only orders the panel, it is never a price claim.
type Item struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Count int64  `json:"count"`
	Value int64  `json:"value,omitempty"`
	// Raw is, on an item the control panel serves, the count BEFORE deaths were deducted, set only when
	// a deduction was made (Count is then the net). 0 means Count is the raw figure.
	Raw int64 `json:"raw,omitempty"`
}

// Drop is one report: Events is how many kills/pickpockets/opens this report covers (0 for a
// pickup reported after the kill was already counted), Items what they yielded, and XP the
// experience they earned when that, not an item, is what the action produces — a bone buried
// (prayer.go). XP is measured, never looked up: it is the skill's own reading rising.
type Drop struct {
	Source Source
	Events int
	Items  []Item
	XP     int
	// Spent is what the events MEASURABLY used up beyond what Source.Inputs prices per event: the
	// runes an account cast at an npc it killed with Magic (a user request: "subtract runes
	// spent ... from that NPC's drop value"). Counted from the pack, never predicted; the panel
	// prices it like any other cost.
	Spent []Item
	// Skills is the experience the events measurably earned, by skill, for a source that pays several
	// (an npc killed: Attack, Strength, Defence, Ranged, Magic and Hitpoints per kill, owner 7 Oct
	// 2026). Item.ID is the stat index (gamestate.SkillNames), Item.Name the skill and Item.Count the
	// experience. Measured as the skills' own readings rising (internal/bot's lootxp.go), never
	// looked up. A source with ONE skill (a tree, a fish, a theft) uses XP instead.
	Skills []Item
}

// Counts is an obj id -> total count reading of one inventory.
type Counts map[int]int64

// PackCounts reads inv into Counts. A nil inventory reads as empty.
func PackCounts(inv *gamestate.Inventory) Counts {
	out := Counts{}
	if inv == nil {
		return out
	}
	for _, it := range inv.NonEmpty() {
		out[int(it.ID)] += int64(it.Count)
	}
	return out
}

// Gained is every positive per-id difference from before to after, skipping ids for which
// skip returns true (the caskets and oysters themselves, the coins a pickpocket spent, ...).
// Losses are ignored: loot is what arrived, not the net of everything else that happened.
func Gained(before, after Counts, skip func(id int) bool) Counts {
	out := Counts{}
	for id, n := range after {
		if skip != nil && skip(id) {
			continue
		}
		if d := n - before[id]; d > 0 {
			out[id] = d
		}
	}
	return out
}

// WaitGained is Gained against the pack read by read, polled every 50ms (through delay, so a
// stop request still ends it) until something has arrived or maxWait passes. It exists for a
// success confirmed by something other than the pack itself: a pickpocket is confirmed by the
// Thieving XP rising, and the server's inventory update for the same tick can be processed a
// moment after the XP one. When the pack already shows the gain, it returns at once.
func WaitGained(read func() *gamestate.Inventory, before Counts, delay func(time.Duration) bool, maxWait time.Duration, skip func(id int) bool) Counts {
	deadline := time.Now().Add(maxWait)
	for {
		gained := Gained(before, PackCounts(read()), skip)
		if len(gained) > 0 || !time.Now().Before(deadline) {
			return gained
		}
		if delay != nil && !delay(50*time.Millisecond) {
			return gained
		}
		if delay == nil {
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// Items resolves gained into named Items through cfg (nil leaves names empty), sorted by id so
// a Drop reads the same every time.
func Items(cfg *cachecfg.Config, gained Counts) []Item {
	out := make([]Item, 0, len(gained))
	for id, n := range gained {
		if n <= 0 {
			continue
		}
		it := Item{ID: id, Count: n}
		if cfg != nil {
			if o, ok := cfg.Obj(id); ok {
				it.Name = o.Name
				it.Value = int64(o.Cost)
			}
		}
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Totals is one source's accumulated record in a Tally.
type Totals struct {
	Source      Source `json:"source"`
	Events      int64  `json:"events"`
	Items       []Item `json:"items"`
	FirstUnixMs int64  `json:"first"`
	LastUnixMs  int64  `json:"last"`
	// XP is the experience Drop.XP reported for this source; 0 for every source whose loot is items.
	XP int64 `json:"xp,omitempty"`
	// Spent is Drop.Spent accumulated (the runes cast at a source's kills).
	Spent []Item `json:"spent,omitempty"`
	// Skills is Drop.Skills accumulated: the experience a source's events earned, per skill.
	Skills []Item `json:"skills,omitempty"`
}

type totals struct {
	source      Source
	events      int64
	items       map[int]*Item
	spent       map[int]*Item
	skills      map[int]*Item
	xp          int64
	first, last int64
}

// Tally accumulates Drops per source. Safe for concurrent use: one account's script records
// while the fleet snapshots it.
type Tally struct {
	mu    sync.Mutex
	by    map[string]*totals
	now   func() time.Time
	epoch int64 // bumped on every Add, so a reader can skip an unchanged tally
}

// NewTally returns an empty Tally. now may be nil (time.Now).
func NewTally(now func() time.Time) *Tally {
	if now == nil {
		now = time.Now
	}
	return &Tally{by: map[string]*totals{}, now: now}
}

// Add records d. A drop with no events, no items and no XP is ignored.
//
// An event's OWN item (the Genie's Lamp, the Strange box) is only ever recorded on that event's
// source: on any other it is the pack the account happened to carry when the event paid out, which
// whichever window was open picked up (ForeignEventItem). Applied here, on the one path every
// source's Drops take, so no window has to remember to skip them.
func (t *Tally) Add(d Drop) {
	d.Items = WithoutHiddenItems(WithoutForeignEventItems(d.Source, d.Items))
	if d.Events <= 0 && len(d.Items) == 0 && d.XP <= 0 && len(d.Spent) == 0 && len(d.Skills) == 0 {
		return
	}
	ms := t.now().UnixMilli()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.recordLocked(d, ms)
	// The runes a spell cost at an npc are shown twice: on the npc, which is netted of them, and on the
	// Magic card's Combat spells row, which is the one place that adds up every offensive spell's bill.
	// The row mirrors the npc's, so the panel never subtracts it a second time (Source.MirrorsCost).
	if d.Source.Kind == KindNPC && d.Source.Action == ActionKill && len(d.Spent) > 0 {
		t.recordLocked(Drop{Source: CombatSpells(), Events: d.Events, Spent: d.Spent}, ms)
	}
	t.epoch++
}

// recordLocked folds one drop into its source's totals; the caller holds the lock and bumps the epoch.
func (t *Tally) recordLocked(d Drop, ms int64) {
	key := d.Source.Key()
	rec := t.by[key]
	if rec == nil {
		rec = &totals{source: d.Source, items: map[int]*Item{}, spent: map[int]*Item{}, skills: map[int]*Item{}, first: ms}
		t.by[key] = rec
	}
	if d.Source.Level > 0 {
		rec.source.Level = d.Source.Level
	}
	if d.Events > 0 {
		rec.events += int64(d.Events)
	}
	if d.XP > 0 {
		rec.xp += int64(d.XP)
	}
	mergeItems(rec.items, d.Items)
	mergeItems(rec.spent, d.Spent)
	mergeItems(rec.skills, d.Skills)
	rec.last = ms
}

// mergeItems adds items into a by-id map, keeping the first name and the last cache value.
func mergeItems(into map[int]*Item, items []Item) {
	for _, it := range items {
		if it.Count <= 0 {
			continue
		}
		cur := into[it.ID]
		if cur == nil {
			cp := it
			cp.Count = 0
			cur = &cp
			into[it.ID] = cur
		}
		if cur.Name == "" {
			cur.Name = it.Name
		}
		if it.Value > 0 {
			cur.Value = it.Value
		}
		cur.Count += it.Count
	}
}

// Epoch changes whenever Add records something.
func (t *Tally) Epoch() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.epoch
}

// Snapshot is a deep copy of every source's totals, sorted by key.
func (t *Tally) Snapshot() []Totals {
	t.mu.Lock()
	defer t.mu.Unlock()
	keys := make([]string, 0, len(t.by))
	for k := range t.by {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]Totals, 0, len(keys))
	for _, k := range keys {
		rec := t.by[k]
		tt := Totals{Source: rec.source, Events: rec.events, XP: rec.xp, FirstUnixMs: rec.first, LastUnixMs: rec.last, Items: make([]Item, 0, len(rec.items))}
		for _, it := range rec.items {
			tt.Items = append(tt.Items, *it)
		}
		sort.Slice(tt.Items, func(i, j int) bool { return tt.Items[i].ID < tt.Items[j].ID })
		for _, it := range rec.spent {
			tt.Spent = append(tt.Spent, *it)
		}
		sort.Slice(tt.Spent, func(i, j int) bool { return tt.Spent[i].ID < tt.Spent[j].ID })
		for _, it := range rec.skills {
			tt.Skills = append(tt.Skills, *it)
		}
		sort.Slice(tt.Skills, func(i, j int) bool { return tt.Skills[i].ID < tt.Skills[j].ID })
		out = append(out, tt)
	}
	return out
}

// ---- loc sources: stalls and thievable chests ----

// KindLoc is loot taken from a LOC rather than from an NPC or an openable in the pack: a
// market stall, a thievable chest, anything the account clicks on the map and takes from.
//
// WHY IT DID NOT EXIST UNTIL 2026-09-22. The taxonomy had exactly three kinds — npc,
// openable and trail — plus the gathering and production kinds. A Baker's stall is none of
// them: it is a loc, the account steals from it with an OPLOC, and there was literally no
// Source that could describe it. So `thieverrunner.StealStall` and `.StealChest` recorded
// nothing at all, while `.Pickpocket` beside them recorded every take. That is not a missing
// call site, it was a missing shape.
//
// The consequence, in the fleet owner's words: "if scarabswarm is thieving then all
// pickpockets and steal locations should show in the loot tracker. every form of loot should
// show in the loot tracker from every aspect of the game." ScarabSwarm's Stage 2 works the
// Baker's stall for its whole larder and ProgressiveThiever climbs stalls and chests, and
// none of it was ever counted.
//
// Since 2026-09-23 a steal is recorded as KindThieving instead (thieving.go), so that a stall and
// every pickpocket share one card. KindLoc remains the shape the constructor starts from and the
// shape every steal persisted before then carries, which Migrate rewrites on load.
const KindLoc = "loc"

// ActionSteal is how loot leaves a stall or a thievable chest — distinct from ActionOpen,
// which is an openable in the pack (a casket), and from ActionPickpocket, which is an NPC.
// The same stall stolen from and the same chest opened are different counts.
const ActionSteal = "steal"

// Loc is a source for loot taken from the loc with this id. The name and the panel's model
// come from the cache when cfg knows the loc, exactly as NPC does for an npc; fallbackName is
// used when it does not. A steal comes back as a KindThieving source (thieving.go), so a stall or
// a chest lands on the same Thieving card as every pickpocket.
func Loc(cfg *cachecfg.Config, action string, locID int, fallbackName string) Source {
	src := Source{Kind: KindLoc, Action: action, Name: fallbackName, ID: locID, Model: ModelLoc}
	if cfg != nil {
		if l, ok := cfg.Loc(locID); ok && l.Name != "" {
			src.Name = l.Name
		}
	}
	return thievingShape(src)
}

// Stall is the common case: a steal from a market stall.
func Stall(cfg *cachecfg.Config, locID int, fallbackName string) Source {
	return Loc(cfg, ActionSteal, locID, fallbackName)
}
