package questplan

import (
	"fmt"
	"strings"
)

// The ladder's hard gates, enforced where the quest is chosen.
//
// LadderFacts is the one table of what each rung needs, and PlanLadder reads it, but the live
// ladder (questrunner.Engine.RunLadder, under Stage 5 and under the QuestLadder bot) does not plan:
// it takes the first quest of its list that reads incomplete and whose Record.Requirements hold.
// A requirement a quest's Record never declared therefore held nothing back. FremennikRecord,
// ScorpionCatcherRecord and HorrorRecord declare none of the levels LadderFacts gates them on, and
// on 14 October 2026 twenty-two accounts at Woodcutting 2 and Fletching 1 walked to Rellekka for
// the Fremennik Trials, over the basalt rocks where accounts slip, and stood at the swaying tree
// ("Woodcutting 40 and an axe") until Olaf's step parked.
//
// LadderHolds reads the gates from LadderFacts itself, so that the table PlanLadder trusts and the
// check the ladder runs cannot disagree, and a quest added to the table is gated the moment it is
// added. Only the gates the content or the owner make binding hold a quest back (Hard): a
// GateItem is waived by an item the account or its mule owns, which only the provisioner can see,
// and a GateAdvised never was a requirement.

// Hard reports whether the gate holds a quest back when the account's level is below it: a server
// gate (content refuses the step), the owner's own rule, or a level the ported module waits for.
func (g Gate) Hard() bool {
	switch g.Kind {
	case GateServer, GateOwner, GateModule:
		return true
	}
	return false
}

// Label is how the gate's kind reads in a log line.
func (g Gate) Label() string {
	switch g.Kind {
	case GateServer:
		return "server gate"
	case GateOwner:
		return "owner's rule"
	case GateModule:
		return "module gate"
	case GateItem:
		return "item gate"
	}
	return "advised"
}

// LadderHold is one hard requirement of a rung that the account does not meet.
type LadderHold struct {
	// Skill is the lower-case skill; empty for the quest-point minimum.
	Skill string
	// Need and Have are the level (or quest points) wanted and held, base values: the ladder never
	// drinks a boost, so a Boostable gate is judged on the base level too.
	Need, Have int
	// Gate is the LadderFacts gate; zero for the quest-point minimum.
	Gate Gate
	// Rung is the ladder rung that trains the level (GateRungID); empty for quest points.
	Rung string
}

// Reason reads like CheckRequirements's, with the gate's kind and note.
func (h LadderHold) Reason() string {
	if h.Skill == "" {
		return fmt.Sprintf("needs %d quest points (have %d; the ladder's own minimum)", h.Need, h.Have)
	}
	why := h.Gate.Label()
	if h.Gate.Note != "" {
		why += ": " + h.Gate.Note
	}
	return fmt.Sprintf("needs %s %d (have %d; %s)", skillLabel(h.Skill), h.Need, h.Have, why)
}

// LadderHolds lists the hard requirements LadderFacts names for quest r that the account p does
// not meet, in the table's order, leaving out one r.Requirements already states (a record that
// declares Attack 20 reports it itself, through CheckRequirements, and is not told twice).
//
// A skill the account's levels do not mention reads as level 0: a level that has not arrived is a
// level not held, and the ladder asks again on its next pass.
func LadderHolds(r Record, p PlayerState) []LadderHold {
	f, ok := LadderFacts[r.ID]
	if !ok {
		return nil
	}
	var out []LadderHold
	if f.QPNeeded > 0 && p.QuestPoints < f.QPNeeded && r.Requirements.MinQuestPoints < f.QPNeeded {
		out = append(out, LadderHold{Need: f.QPNeeded, Have: p.QuestPoints})
	}
	for _, g := range f.Gates {
		if !g.Hard() {
			continue
		}
		skill := strings.ToLower(g.Skill)
		have := p.SkillLevels[skill]
		if have >= g.Level || recordStatesLevel(r, skill, g.Level) {
			continue
		}
		out = append(out, LadderHold{Skill: skill, Need: g.Level, Have: have, Gate: g, Rung: GateRungID(g)})
	}
	return out
}

// recordStatesLevel reports whether r.Requirements.Skills already demands skill at level or more.
func recordStatesLevel(r Record, skill string, level int) bool {
	for _, s := range r.Requirements.Skills {
		if strings.EqualFold(s.Skill, skill) && s.Level >= level {
			return true
		}
	}
	return false
}

// HoldReasons is each hold's Reason.
func HoldReasons(holds []LadderHold) []string {
	out := make([]string, 0, len(holds))
	for _, h := range holds {
		out = append(out, h.Reason())
	}
	return out
}

// HeldRungs are the rungs the live ladder does not run yet, whatever the account holds, each with the
// reason the ladder's log gives. It is a deployment decision, not a fact about the quest, so it lives
// here and not in LadderFacts: PlanLadder still plans the rung in the owner's order (it is a report of
// where the ladder is going), and RunLadder will not start it.
//
// Dragon Slayer is held because it has never run live (docs/quest_plan_2026-10-15.md, W19): the module
// checks no combat level (the plan wants 45; the maze's lesser demon is level 82, with MaxDeaths 3 as the
// only brake), its nails wait on W14, and its only live gate is the 32 quest points the Champions' Guild
// door wants. On 16 October 2026 the live log showed it blocked at 20 quest points (ten lines), 26 and 31
// (one each), so nobody had reached it. The two quest-point fillers (Goblin Diplomacy and Romeo & Juliet,
// W13) give +10, which carries the 26 and the 31 over the line, and any account the other quests lift to
// 22 or more, into a module nobody has watched run. W19 removes this entry when it lands; the test that pins it
// (TestTheFillersDoNotCarryAnAccountIntoDragonSlayer) goes with it.
//
// Tree Gnome Village was held here from 16 Oct 2026 until W18: its orb step climbed the Khazard tower with no food
// and the walker had no way back down (two accounts died up there at 03:15 and 03:41). It runs again with food, the
// shared fight odds against the Khazard commanders, the tower's two doors in the walk graph and the stronghold as a
// pocket (questplan/treegnome_stronghold.go, webwalk/khazard.go, questmods/treegnome.go).
var HeldRungs = map[string]string{
	DragonSlayerID: "its module has never run live and the quest-point fillers put 32 quest points in reach; held until W19 (docs/quest_plan_2026-10-15.md)",
}

// RungHeld is why the live ladder will not run rung id yet (HeldRungs), or "" when it will.
func RungHeld(id string) string { return HeldRungs[id] }
