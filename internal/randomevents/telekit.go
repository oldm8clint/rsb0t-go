package randomevents

import (
	"rsb0t-go/internal/actions"
)

// A teleport kit: what the last-resort exit needs in the pack (deathsweep-1017, 17 Oct 2026).
//
// lastResortExit and the strand ladder cast the teleport the account can pay for (PickTeleport): Varrock, Lumbridge or Falador,
// one Law rune, three Air runes and one Fire, Earth or Water rune each, the elements and the air paid by a wielded staff of
// that element. A cornered account that cannot pay for one has only its food. The Wizards' Tower account that a Swarm killed on
// 17 Oct had neither food nor a Law rune; /api/inventory the same morning showed, of the 37 accounts idling in the Stage 5 quest
// hold, 27 able to pay for a teleport and 10 not (3 with no Law rune, 5 with no Air rune and no staff of air, 2 with no
// element rune and a staff of air). TeleportShortfall says what an account is missing, so a hold at a bank can take a set
// out of its own bank.

// KitLine is an item and how many of it.
type KitLine struct {
	Name  string
	Count int
}

// teleportShortfall is, for the cheapest teleport the Magic level opens that the pack and the worn items nearly pay for,
// what is still missing. Nil short with a spell: the account can cast it now. Nil short and no spell: Magic opens none.
// Pure.
func teleportShortfall(magic int, pack map[string]int, worn []string) (spell string, short []KitLine) {
	bestTotal := -1
	for _, s := range teleportSpells {
		if magic < s.level {
			continue
		}
		var lines []KitLine
		total := 0
		add := func(name string, n int) {
			lines = append(lines, KitLine{Name: name, Count: n})
			total += n
		}
		if have := packCount(pack, "Law rune"); have < s.law {
			add("Law rune", s.law-have)
		}
		covered := false
		switch s.elementRune {
		case "Fire rune":
			covered = wornAny(worn, fireStaves)
		case "Earth rune":
			covered = wornAny(worn, earthStaves)
		case "Water rune":
			covered = wornAny(worn, waterStaves)
		}
		if have := packCount(pack, s.elementRune); !covered && have < s.elementCount {
			add(s.elementRune, s.elementCount-have)
		}
		if have := packCount(pack, "Air rune"); !wornAny(worn, airStaves) && have < s.air {
			add("Air rune", s.air-have)
		}
		if len(lines) == 0 {
			return s.name, nil
		}
		if bestTotal < 0 || total < bestTotal {
			spell, short, bestTotal = s.name, lines, total
		}
	}
	return spell, short
}

// TeleportShortfall is teleportShortfall for the session's own pack, staff and Magic level: what it lacks to cast the nearest
// teleport it has the level for, nil when it can already cast one. Nil too when its Magic is under the lowest teleport's, not yet
// reported, or no name resolver is registered.
func TeleportShortfall(s actions.Session) (spell string, short []KitLine) {
	if s == nil || s.State() == nil {
		return "", nil
	}
	magic, _, pack, worn, ok := teleportInputs(s.State())
	if !ok {
		return "", nil
	}
	return teleportShortfall(magic, pack, worn)
}
