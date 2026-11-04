package fleetplan

import "strings"

const (
	// EatBasePercent is the share of the maximum hitpoints at or under which an account on the move eats, whatever hits it
	// (or nothing does: poison, a fall, a hit already healed past).
	EatBasePercent = 50
	// EatCapPercent bounds the line the attackers raise it to. Above it the account would spend its life eating.
	EatCapPercent = 60
	// EatVolleys is how many volleys of the attackers' worst hits the line keeps the account clear of.
	EatVolleys = 2
)

// DefaultHunterMaxHit is the max hit assumed for an NPC that is not in hunterMaxHit: more than any of the pens' hunters but
// the fire giants, so an unknown attacker raises the line and does not lower it.
const DefaultHunterMaxHit = 8

// NPCMaxHit is the most one melee hit of an NPC deals, content's npc_melee_maxhit:
// combat_maxhit(combat_stat(strength + 9, strengthbonus)) with combat_stat(e, b) = e * (b + 64) and combat_maxhit(s) =
// (s + 320) / 640 (skill_combat/scripts/npc/npc_combat_melee.rs2, combat.rs2). The 9 is the 8 every effective stat gets and the
// 'style bonus' of 1 an NPC fights on.
func NPCMaxHit(strength, strengthBonus int) int {
	return ((strength+9)*(strengthBonus+64) + 320) / 640
}

// HunterMaxHit is the largest melee hit, by lower-case NPC name, of the NPCs ScarabSwarm's pens, rungs and walks meet: for each
// name the most NPCMaxHit gives over every config of that name in Content 289 (TestHunterMaxHitsMatchContent re-derives them from a
// checkout). A name is the one cachecfg.Npc.Name returns.
var HunterMaxHit = map[string]int{
	"ogre": 7, "ogre chieftain": 9, "jogre": 7, "giant": 4, "moss giant": 6, "ice giant": 7, "fire giant": 11,
	"ice warrior": 6, "hobgoblin": 5, "bear": 3, "wolf": 6, "giant spider": 3, "pirate": 4, "mugger": 1,
	"warrior woman": 3, "khazard trooper": 3, "barbarian": 2, "barbarian woman": 2, "al-kharid warrior": 2,
	"cow": 1, "goblin": 2, "chicken": 1, "duck": 0, "white knight": 6, "jail guard": 3, "guard": 5,
	"chaos dwarf": 6, "lesser demon": 8, "chaos druid": 2, "dark wizard": 3,
}

// MaxHitOf is the largest melee hit an NPC of this name deals; DefaultHunterMaxHit for a name it does not know.
// KnownHunter reports whether hunterMaxHit names this NPC: one of the pens', rungs' or walks' fighters, not a townsperson or a guard
// standing in a crowd.
func KnownHunter(name string) bool {
	_, ok := HunterMaxHit[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

func MaxHitOf(name string) int {
	if h, ok := HunterMaxHit[strings.ToLower(strings.TrimSpace(name))]; ok {
		return h
	}
	return DefaultHunterMaxHit
}

// EatLine is the hitpoints at or under which an account eats, given its maximum and the worst hit of each NPC now facing it
// (hits; nil or empty for none). With nothing facing it the line is EatBasePercent of the maximum. With attackers it is the
// hitpoints EatVolleys volleys of their worst hits would take (EatVolleys * the sum of hits), at least the base line and at
// most EatCapPercent of the maximum: two Ogres at 55 hitpoints are 28, four are the cap, 33. 0 when the maximum is unknown.
func EatLine(maxHP int, hits []int) int {
	if maxHP <= 0 {
		return 0
	}
	line := maxHP * EatBasePercent / 100
	need := 0
	for _, h := range hits {
		need += EatVolleys * max(h, 0)
	}
	if need > line {
		line = min(need, (maxHP*EatCapPercent+99)/100)
	}
	return line
}
