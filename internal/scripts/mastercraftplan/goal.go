package mastercraftplan

// The skill goal. a user request: "we want mastercrafter to aim to max out every skill to 99 and resorting to
// scarabswarm when its waiting on supplies or already 99 in a skill". With craftSkillGoal set, a training round is not
// started once every skill it trains is at the goal; Decide then goes on to the next round, and to ScarabSwarm when none is
// due. craftPastGoal names the rounds that run past the goal anyway (owner, the same day: keep stringing the Magic
// longbows at Fletching 99, since experience still counts to 200 million).
//
// Only the training rounds are held. The rounds that move the fleet's supplies (the hand-over, the storage round, the shop
// trip, the jewellery round's run-stall stock, the Nature gate and campaign, the chest) are not training rounds and run
// whatever the levels are; the Runecrafting levelling round already stops at its own level.

import (
	"fmt"
	"strconv"
	"strings"
)

// Setting keys of the skill goal.
const (
	KeySkillGoal = "craftSkillGoal"
	KeyPastGoal  = "craftPastGoal"
)

// MaxSkillGoal is the highest level a skill reaches.
const MaxSkillGoal = 99

func init() {
	knownKeys[KeySkillGoal] = true
	knownKeys[KeyPastGoal] = true
}

// goalRounds are the training rounds and the skills each trains.
var goalRounds = map[Mode][]string{
	ModeGems:      {"Crafting"},
	ModeBows:      {"Fletching"},
	ModePearls:    {"Crafting", "Fletching"},
	ModeSuperheat: {"Magic", "Smithing"},
	ModeSmith:     {"Smithing"},
	ModeStaves:    {"Crafting"},
	ModePlatebody: {"Smithing"},
	ModeArrows:    {"Fletching"},
	ModeHerbs:     {"Herblore"},
}

// validateGoal reports every problem with the skill goal's settings.
func (s Settings) validateGoal() []string {
	var p []string
	if s.SkillGoal < 0 || s.SkillGoal > MaxSkillGoal {
		p = append(p, fmt.Sprintf("%s %d is outside 0..%d (0 turns the goal off)", KeySkillGoal, s.SkillGoal, MaxSkillGoal))
	}
	for _, m := range s.PastGoal {
		if _, ok := goalRounds[Mode(m)]; !ok {
			p = append(p, fmt.Sprintf("%s: %q is not a training round (%s)", KeyPastGoal, m, strings.Join(goalRoundNames(), ", ")))
		}
	}
	return p
}

func goalRoundNames() []string {
	return []string{string(ModeGems), string(ModeBows), string(ModePearls), string(ModeStaves), string(ModeSuperheat), string(ModePlatebody), string(ModeSmith), string(ModeArrows), string(ModeHerbs)}
}

// skillLevel is the snapshot's level of a skill the goal reads, and whether it is known.
func (s Snapshot) skillLevel(skill string) (int, bool) {
	switch skill {
	case "Crafting":
		return s.CraftingLevel, s.CraftingLevel > 0
	case "Fletching":
		return s.FletchingLevel, s.FletchingKnown
	case "Magic":
		return s.MagicLevel, s.MagicLevel > 0
	case "Smithing":
		return s.SmithingLevel, s.SmithingLevel > 0
	case "Herblore":
		return s.HerbloreLevel, s.HerbloreKnown
	}
	return 0, false
}

// MaxSkillXP is the experience a skill stops gaining at.
const MaxSkillXP = 200_000_000

// AtGoal reports whether the skill goal holds a training round off: the goal is on, the round is not one of craftPastGoal's,
// and every skill it trains is known and at the goal. A round whose skills are all at MaxSkillXP is held whatever the goal
// and craftPastGoal say (a user request: "master blocked from fletching once he hits 200m exp in fletching"): it can earn
// nothing more. why says so for Decide's log line.
func AtGoal(set Settings, s Snapshot, mode Mode) (why string, held bool) {
	skills, ok := goalRounds[mode]
	if !ok {
		return "", false
	}
	// Every Fletching round (bows, arrows, the pearl bolt tips) stops at the cap: the owner runs them past 99 for the
	// Fletching experience alone.
	for _, sk := range skills {
		if sk == "Fletching" && s.FletchingXP >= MaxSkillXP {
			return string(mode) + ": Fletching is at " + strconv.Itoa(MaxSkillXP) + " experience, the most it can earn", true
		}
	}
	if set.SkillGoal <= 0 {
		return "", false
	}
	for _, m := range set.PastGoal {
		if Mode(m) == mode {
			return "", false
		}
	}
	var at []string
	for _, sk := range skills {
		lvl, known := s.skillLevel(sk)
		if !known || lvl < set.SkillGoal {
			return "", false
		}
		at = append(at, sk+" "+strconv.Itoa(lvl))
	}
	return string(mode) + ": " + strings.Join(at, ", ") + " is at the " + KeySkillGoal + " " + strconv.Itoa(set.SkillGoal), true
}
