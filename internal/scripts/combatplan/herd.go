package combatplan

import "strings"

// A herd of more than one NPC name ("combatspeed", 6 Oct 2026). Owner: "accounts training at barbarians
// dont consider attacking the barbarian women". The Barbarian Village rung and pen named ONE monster, and
// every target search, foreign-attacker test and drop attribution matched that single name, so the five
// Barbarian women (npc 17, lvl 8) that stand in the same streets as the twelve Barbarians (npc 12, lvl 7)
// were never targets: 5 spawns of 17 unused, and a woman that attacked an account at a broken-off fight read
// as a foreign attacker and sent the whole pen away for half an hour.
//
// Content 289 (scripts/_unpack/225/all.npc, scripts/drop tables/scripts/barbarian.rs2): the woman has 14
// hitpoints and respawnrate 50 like the Barbarian, wanders 10 and chases 12, attack 6, defence 5, strength 5
// +10 (max hit 2, one attack every 6 ticks where the Barbarian's is every 5), and rolls the same drop table
// ("Level 7 & 8 in Barbarian Village", [ai_queue3,_barbarian]). So she is the Barbarian with a slightly
// slower swing: the same pen, the same food budget.
//
// A herd travels through the runners' single `name string` parameters as one key: the names joined with
// HerdSep, which no NPC name contains. HerdMatch is the one place that splits it.

// HerdSep joins a herd's names into one key.
const HerdSep = "|"

// HerdKey is the key for a herd of these names (the first is the primary one); empty names are dropped.
func HerdKey(names ...string) string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return strings.Join(out, HerdSep)
}

// HerdNames is the names in a key (a plain name is a herd of one).
func HerdNames(key string) []string {
	var out []string
	for _, n := range strings.Split(key, HerdSep) {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// HerdMatch reports whether name (case-insensitive) is one of the names in the herd key. An empty key
// matches nothing.
func HerdMatch(key, name string) bool {
	for _, n := range HerdNames(key) {
		if strings.EqualFold(n, strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

// Names is the monster's own name, then the others of its herd (Also).
func (m Monster) Names() []string {
	out := make([]string, 0, 1+len(m.Also))
	if m.Name != "" {
		out = append(out, m.Name)
	}
	return append(out, m.Also...)
}

// HerdKey is the key the runners match NPCs by: Name and Also joined (HerdKey).
func (m Monster) HerdKey() string { return HerdKey(m.Names()...) }

// Is reports whether name is the monster or one of the others of its herd.
func (m Monster) Is(name string) bool { return HerdMatch(m.HerdKey(), name) }
