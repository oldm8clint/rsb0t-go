package loot

import (
	"strconv"
	"strings"
)

// TELEPORTS, on a tab of their own (a user request: "Law runes and other runes spent on
// teleports go in their own Teleports loot tab: cost per teleport cast, count, coin value").
//
// A teleport yields no item, so there is nothing to snapshot around it. What it COSTS is the pack's
// runes, and that is measured, never looked up: content deletes the runes and pays the Magic XP in
// one script step (skill_magic's cast_teleport, content/scripts/skill_magic/scripts), so a cast is a
// Law rune leaving the pack in the same server tick as Magic XP rising. internal/bot's cast watch
// (lootcast.go) pairs the two for every running account, which is why no script that casts a
// teleport (ScarabSwarm's travel spell, RuneRunner, the quest ladder's BeforeTravel hook, ...) has
// to report one.
//
// The card is KindTeleport with Skill "Teleports", so every spell is one row of one card and the
// panel's "Teleports" filter shows only it. Each cast is one event and carries what it measurably
// used as Drop.Spent, which the control panel prices as a cost (lootcost.go spentCost), exactly as
// it does for the runes cast at an npc.
//
// NOT DOUBLE-COUNTED. The Law rune appears nowhere else: an alchemy or a superheat uses a Nature
// rune and Fire runes (magic.go), an offensive spell never uses a Law rune, and the rune cost of a
// kill is the pack's runes between attack and kill (Drop.Spent on the npc), which a teleport cast
// in the middle of a fight could only touch by accident.

// KindTeleport groups every teleport cast on one card. Titled by Skill.
const KindTeleport = "teleport"

// SkillTeleports titles the Teleports card. It is not a game skill; it is only the card's name,
// and being a Skill is what makes GroupKey collapse every spell onto one card.
const SkillTeleports = "Teleports"

// ActionTeleport is one cast, for the panel's verb.
const ActionTeleport = "teleport"

// Rune ids (content pack/obj.pack): the elemental and catalytic runes a spell can delete.
const (
	FireRuneID   = 554
	WaterRuneID  = 555
	AirRuneID    = 556
	EarthRuneID  = 557
	LawRuneID    = 563
	lowestRune   = 554
	highestRune  = 566
	MagicStat    = 6
	teleportName = "Teleport"
)

// IsRuneID reports whether an obj id is one of the thirteen runes a spell can delete (554 fire to
// 566 soul).
func IsRuneID(id int) bool { return id >= lowestRune && id <= highestRune }

// teleportSpell is one law-rune spell: the runes content asks for (magic_spells.dbrow) and the
// experience it pays, in whole points.
type teleportSpell struct {
	name  string
	runes map[int]int64
	xp    int
}

// teleportSpells are the standard spellbook's law-rune spells, read out of content 289's
// skill_magic/configs/magic_spells.dbrow. Telekinetic Grab uses a Law rune too and is never a
// teleport, but it spends the same runes on the same tab, so it is named rather than misfiled.
var teleportSpells = []teleportSpell{
	{"Varrock Teleport", map[int]int64{FireRuneID: 1, AirRuneID: 3, LawRuneID: 1}, 35},
	{"Lumbridge Teleport", map[int]int64{EarthRuneID: 1, AirRuneID: 3, LawRuneID: 1}, 41},
	{"Falador Teleport", map[int]int64{WaterRuneID: 1, AirRuneID: 3, LawRuneID: 1}, 48},
	{"Camelot Teleport", map[int]int64{AirRuneID: 5, LawRuneID: 1}, 55},
	{"Ardougne Teleport", map[int]int64{WaterRuneID: 2, LawRuneID: 2}, 61},
	{"Watchtower Teleport", map[int]int64{EarthRuneID: 2, LawRuneID: 2}, 68},
	{"Trollheim Teleport", map[int]int64{FireRuneID: 2, LawRuneID: 2}, 68},
	{"Telekinetic Grab", map[int]int64{AirRuneID: 1, LawRuneID: 1}, 43},
}

// IsLawCast reports whether a rune loss includes a Law rune, which is what marks a cast as a
// teleport (or Telekinetic Grab) rather than anything else the pack's runes go to.
func IsLawCast(lost Counts) bool { return lost[LawRuneID] > 0 }

// TeleportName names the spell that spent lost (rune id -> count) and paid xp whole Magic
// experience points.
//
// The runes decide it first: a candidate must spend exactly the Law runes it asks for and no more of
// any other rune than it asks for. A staff of an element pays that element's runes, so a candidate
// may be missing some (a Varrock cast from a Staff of air loses a Law and a Fire rune only). Several
// candidates can remain then, and the experience, within a point of the spell's own, picks one. A
// loss that no spell explains is "Teleport": it is still a cast that cost these runes, and the card
// shows it as that.
func TeleportName(lost Counts, xp int) string {
	var cands []teleportSpell
	for _, sp := range teleportSpells {
		if lost[LawRuneID] != sp.runes[LawRuneID] {
			continue
		}
		ok := true
		for id, n := range lost {
			if id == LawRuneID || n <= 0 {
				continue
			}
			if n > sp.runes[id] {
				ok = false
				break
			}
		}
		if ok {
			cands = append(cands, sp)
		}
	}
	// With no staff paying anything, exactly the spell's runes are lost: a certain match, whatever
	// the experience reads (the experience may be scaled on another engine).
	for _, sp := range cands {
		if sameRunes(lost, sp.runes) {
			return sp.name
		}
	}
	// A staff paid some runes: the experience decides, when it picks exactly one.
	var byXP []teleportSpell
	for _, sp := range cands {
		if xp > 0 && abs(xp-sp.xp) <= 1 {
			byXP = append(byXP, sp)
		}
	}
	if len(byXP) == 1 {
		return byXP[0].name
	}
	if len(byXP) == 0 && len(cands) == 1 {
		return cands[0].name
	}
	return teleportName
}

func sameRunes(lost Counts, want map[int]int64) bool {
	for id, n := range lost {
		if n > 0 && want[id] != n {
			return false
		}
	}
	for id, n := range want {
		if lost[id] != n {
			return false
		}
	}
	return true
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// Teleport is the source of one teleport spell's casts: one row of the Teleports card, drawn with
// the Law rune every one of them spends.
func Teleport(spell string) Source {
	spell = strings.TrimSpace(spell)
	if spell == "" {
		spell = teleportName
	}
	return Source{Kind: KindTeleport, Skill: SkillTeleports, Action: ActionTeleport, Name: spell, ID: LawRuneID, Model: ModelObj}
}

// DescribeSpent names what a cast used, for a log line: "3 x Air rune, 1 x Law rune".
func DescribeSpent(items []Item) string {
	parts := make([]string, 0, len(items))
	for _, it := range items {
		name := it.Name
		if name == "" {
			name = "obj " + strconv.Itoa(it.ID)
		}
		parts = append(parts, strconv.FormatInt(it.Count, 10)+" x "+name)
	}
	return strings.Join(parts, ", ")
}
