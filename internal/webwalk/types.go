package webwalk

import (
	"fmt"
	"strings"
)

// Point is one absolute world tile.
type Point struct {
	X, Z, Level int
}

func (p Point) String() string { return fmt.Sprintf("%d,%d,%d", p.X, p.Z, p.Level) }

// Chebyshev is the tile distance between two points, ignoring level.
func Chebyshev(a, b Point) int {
	dx, dz := abs(a.X-b.X), abs(a.Z-b.Z)
	if dx > dz {
		return dx
	}
	return dz
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// Kind is a transport edge's kind, as types.ts's TransportKind.
type Kind string

// The transport kinds. KindWalk only appears on a Leg.
const (
	KindWalk      Kind = "walk"
	KindDoor      Kind = "door"
	KindGate      Kind = "gate"
	KindStair     Kind = "stair"
	KindDungeon   Kind = "dungeon"
	KindShip      Kind = "ship"
	KindGangplank Kind = "gangplank"
	KindShortcut  Kind = "shortcut"
	KindPortal    Kind = "portal"
	KindTeleport  Kind = "teleport"
	KindOther     Kind = "other"
)

// DefaultCost is edgeCosts.ts's DEFAULT_EDGE_COST: ticks converted to run-tile units (one
// tick is two tiles of running).
func DefaultCost(k Kind) int {
	switch k {
	case KindDoor, KindGate:
		return 4
	case KindStair:
		return 6
	case KindDungeon, KindGangplank:
		return 8
	case KindShip:
		return 36
	case KindShortcut, KindOther, KindTeleport:
		return 10
	case KindPortal:
		return 20
	}
	return 10
}

// QuestProgress is a quest's status for Requires.
type QuestProgress string

// Quest statuses, as types.ts.
const (
	QuestNotStarted QuestProgress = "not_started"
	QuestStarted    QuestProgress = "started"
	QuestComplete   QuestProgress = "complete"
	QuestUnknown    QuestProgress = "unknown"
)

func questRank(q QuestProgress) int {
	switch q {
	case QuestStarted:
		return 1
	case QuestComplete:
		return 2
	}
	return 0
}

// SkillReq is a minimum skill level.
type SkillReq struct {
	Name  string `json:"name"`
	Level int    `json:"level"`
}

// ItemReq is an item count, carried or worn.
type ItemReq struct {
	Name     string `json:"name"`
	Count    int    `json:"count"`
	Consumed bool   `json:"consumed,omitempty"`
}

// QuestReq is a minimum quest status.
type QuestReq struct {
	Quest     string        `json:"quest"`
	MinStatus QuestProgress `json:"minStatus"`
}

// Currency is a toll or fare.
type Currency struct {
	Name   string `json:"name"`
	Amount int    `json:"amount"`
}

// Requires is types.ts's TransportRequires: plan-time gates on an edge.
type Requires struct {
	Members                 bool       `json:"members,omitempty"`
	Skills                  []SkillReq `json:"skills,omitempty"`
	Items                   []ItemReq  `json:"items,omitempty"`
	Worn                    []ItemReq  `json:"worn,omitempty"`
	Quests                  []QuestReq `json:"quests,omitempty"`
	FreeSlots               *int       `json:"freeSlots,omitempty"`
	Currency                *Currency  `json:"currency,omitempty"`
	ForbidEntranaRestricted bool       `json:"forbidEntranaRestricted,omitempty"`
	SlashTool               bool       `json:"slashTool,omitempty"`
	EssenceExitReturn       string     `json:"essenceExitReturn,omitempty"`
	EssenceEntrySetsReturn  string     `json:"essenceEntrySetsReturn,omitempty"`
}

// WorldState is the player snapshot requires are checked against. Nil funcs read as zero
// counts and an unknown quest status.
type WorldState struct {
	Members   bool
	Skills    map[string]int // keyed by lower-case skill name
	FreeSlots int
	// QuestStatus returns a quest's status by its display name.
	QuestStatus func(quest string) QuestProgress
	// ItemCount returns how many of an item (by name) the pack holds.
	ItemCount func(name string) int
	// WornCount returns how many of an item (by name) are worn.
	WornCount             func(name string) int
	EntranaRestrictedGear bool
	// CanSlashWeb is nil when unknown, which lets slash-tool edges through.
	CanSlashWeb *bool
}

// Check is requires.ts's meetsRequires. A nil state lets every edge through, as the TS
// PathFinder does offline; an unknown quest status fails.
func (r *Requires) Check(s *WorldState) (bool, string) {
	if r == nil || s == nil {
		return true, ""
	}
	if r.Members && !s.Members {
		return false, "members-only"
	}
	if r.FreeSlots != nil && s.FreeSlots < *r.FreeSlots {
		return false, fmt.Sprintf("need %d free inventory slots (have %d)", *r.FreeSlots, s.FreeSlots)
	}
	for _, sk := range r.Skills {
		if have := s.Skills[strings.ToLower(sk.Name)]; have < sk.Level {
			return false, fmt.Sprintf("need %s %d (have %d)", sk.Name, sk.Level, have)
		}
	}
	count := func(f func(string) int, name string) int {
		if f == nil {
			return 0
		}
		return f(name)
	}
	if r.Currency != nil {
		if have := count(s.ItemCount, r.Currency.Name); have < r.Currency.Amount {
			return false, fmt.Sprintf("need %dx %s (have %d)", r.Currency.Amount, r.Currency.Name, have)
		}
	}
	for _, it := range r.Items {
		if have := count(s.ItemCount, it.Name); have < it.Count {
			return false, fmt.Sprintf("need %dx %s (have %d)", it.Count, it.Name, have)
		}
	}
	for _, it := range r.Worn {
		if have := count(s.WornCount, it.Name); have < it.Count {
			return false, fmt.Sprintf("need to wear %s (have %d)", it.Name, have)
		}
	}
	if r.ForbidEntranaRestricted && s.EntranaRestrictedGear {
		return false, "remove weapons/armour before Entrana"
	}
	if r.SlashTool && s.CanSlashWeb != nil && !*s.CanSlashWeb {
		return false, "need Knife or a bladed weapon to slash webs"
	}
	for _, q := range r.Quests {
		status := QuestUnknown
		if s.QuestStatus != nil {
			status = s.QuestStatus(q.Quest)
		}
		if questRank(status) < questRank(q.MinStatus) {
			return false, fmt.Sprintf("need quest %s %s (have %s)", q.Quest, q.MinStatus, status)
		}
	}
	return true, ""
}

// Special marks an edge that needs more than a plain loc op: a toll, a fare, a dialogue or a
// quest-state gate (specialCrossings.ts). A Walker runs it only through a Handler.
type Special struct {
	Label         string   `json:"label"`
	NPC           string   `json:"npc,omitempty"`
	Choose        []string `json:"choose,omitempty"`
	UseItem       string   `json:"useItem,omitempty"`
	ExactApproach bool     `json:"exactApproach,omitempty"`
}

// Edge is one directed non-walk hop.
type Edge struct {
	// From is the tile the hop is operated from; To is where it lands.
	From, To Point
	Kind     Kind
	Cost     int
	LocName  string
	Action   string
	// LocID is the map placement's loc id, or -1 for an NPC or scripted hop with no loc.
	LocID int
	// OpenLocID is the open-state id of a closed trapdoor, or -1.
	OpenLocID int
	// LocX, LocZ is the loc's placement tile (the NPC stand for an NPC hop).
	LocX, LocZ int
	// LandsOnTile is set for hops that put the player on a fixed tile (ships, dungeons,
	// portals, gangplanks, shortcuts) rather than one step or one level away.
	LandsOnTile      bool
	AcceptAnyLanding bool
	Requires         *Requires
	Special          *Special
	// Slip is the crossing's dice roll when content has one that sets the player back instead of carrying it over
	// (the Fremennik basalt rocks), and nil when the baked graph knows of none. It is not in graph.json: graph.go's
	// EdgeCorrections set it where content was read.
	Slip *Slip
	// Override notes the live evidence when ApproachOverrides moved From.
	Override string
}

// Slip is a shortcut's dice roll, read from content: the crossing script rolls stat_random(Skill, Low, High) and on a
// miss does not carry the jumper over but sweeps it to Lands for 1 to MaxHit hitpoints. It is the difference between
// a crossing that is only gated (a level to meet, nothing else to lose) and one that can hurt, strand and kill.
type Slip struct {
	// Skill is the lower-case skill name the roll reads.
	Skill string
	// Low and High are stat_random's two bounds: its chance at level 1 and at level 99.
	Low, High int
	// MaxHit is the most one slip deals.
	MaxHit int
	// Lands is the tile a slip leaves the jumper on.
	Lands Point
}

// Chance is the probability the roll succeeds at a skill level, as the engine's STAT_RANDOM
// (engine/script/handlers/PlayerOps.ts) works it out: value = floor(Low*(99-L)/98) + floor(High*(L-1)/98) + 1, and
// the roll succeeds when value beats a random 0..255. At the basalt rocks' (5, 255) that is 6/256 at level 1, 92/256
// at 35, 105/256 at 40, 130/256 at 50 and a certainty at 99.
func (s *Slip) Chance(level int) float64 {
	level = min(max(level, 1), 99)
	value := s.Low*(99-level)/98 + s.High*(level-1)/98 + 1
	return float64(min(value, 256)) / 256
}

// HasLoc reports whether the hop is operated on a loc (rather than an NPC or a script).
func (e *Edge) HasLoc() bool { return e.LocID >= 0 }

// NeedsSlashTool reports whether the hop crosses a slashable web.
//
// It is worth asking as its own question because a web is the one barrier on the graph whose
// crossing is a DICE ROLL rather than a state change. Content's general_use/scripts/web.rs2
// cuts the web only on `random(2) = 1`, so a correct swing with the right blade in the right
// hand still leaves the web standing half the time. Every other door either opens or is refused
// for a reason that will still be true on the next attempt.
func (e *Edge) NeedsSlashTool() bool { return e.Requires != nil && e.Requires.SlashTool }

// failureHurts reports a skill-gated shortcut: one with a skill level to meet, whose failed attempts are the
// server's own dice (a slip, a fall, damage) rather than a door that is shut for a reason. A shortcut with a Slip
// hurts whether or not a level gates it: the basalt rock that sweeps a jumper back north has no gate at all.
func (e *Edge) failureHurts() bool {
	if e.Kind != KindShortcut {
		return false
	}
	return e.Slip != nil || (e.Requires != nil && len(e.Requires.Skills) > 0)
}

// slipHPLimit is the hitpoints at or below which a walker does not try a shortcut whose failure hurts: one more than
// the worst slip it can deal, so the account survives it (shortcutSlipMaxHit when the worst slip is not known).
func (e *Edge) slipHPLimit() int {
	if e.Slip != nil {
		return e.Slip.MaxHit + 1
	}
	return shortcutSlipMaxHit
}

// ChangesLevel reports whether the hop lands on another level.
func (e *Edge) ChangesLevel() bool { return e.To.Level != e.From.Level }

// LongRange reports whether the hop spans more tiles than it costs, which is what makes a
// plain Chebyshev heuristic inadmissible.
func (e *Edge) LongRange() bool { return Chebyshev(e.From, e.To) > e.Cost }

func (e *Edge) String() string {
	id := ""
	if e.HasLoc() {
		id = fmt.Sprintf(" loc=%d@%d,%d", e.LocID, e.LocX, e.LocZ)
	}
	s := fmt.Sprintf("[%s] %s %q%s %s -> %s (cost %d)", e.Kind, e.Action, e.LocName, id, e.From, e.To, e.Cost)
	if e.Special != nil {
		s += fmt.Sprintf(" special=%q", e.Special.Label)
	}
	if e.Override != "" {
		s += " (approach overridden)"
	}
	return s
}
