package questplan

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/levels"
)

// THE PROVISIONER TRAINS (docs/quest_selfsufficiency_plan_2026-10-19.md, 3.2 F; lane N4).
//
// The owner's item rule reads "its own skills/levels (training the level if needed)". Until now a skill source below its level
// was skipped and a kit whose only way left was such a source was BLOCKED, which the quest read as "wait, then park for five
// minutes": a Mining 20 silver ore, asked for by an account at Mining 1, was retried every few minutes for good.
//
// A skill source marked Source.Train is the one the needs engine can work for: when every other source of the line is gone
// or below its level, the plan says PhaseTrain, naming the skill and the level, instead of PhaseBlocked. Three rules keep that
// honest.
//
//   - A trainer must exist. The stretch from the account's level to the source's must be covered by the trainer catalogue
//     (Catalogue.FirstUncovered); one that is not is a named, counted gap (SkillGaps), reported as the blocked reason and never
//     a training that cannot happen (plan 3.1, rule 6).
//   - Training is the LAST resort. Every other source of every line is tried first (a shop that sells the item is not
//     bypassed by hours of Mining), the phase is answered only when nothing else is blocked, and an optional line never asks for
//     it. The item rule's order is unchanged for everything that can be used now.
//   - Training never runs inside the quest's slot (the completeness critic's item 19). The provisioner only SAYS it is needed;
//     the engine ends the run, which releases the fleet-wide slot of a crowded quest (Demon 3, Druid 4, Monk 4 ...), and parks
//     the quest under NeedsParkPrefix with the skill and level. The stint is the needs tier's (lane N3), run outside the slot,
//     and the park ends the moment the level is there (ParseNeedsPark).

// TrainParkFor is how long a quest that ended on a train need is parked when no stint has raised the level: the side store's
// two-hour backoff step. The park ends early, the moment the level is reached (ParseNeedsPark), so a long park costs a quest
// nothing once the stint has run, and a short one would only re-run the provisioner every few minutes to find the same answer.
const TrainParkFor = 2 * time.Hour

// NeedsParkPrefix starts the reason of a park that waits for a stint. The skill and the level follow, "needs:mining:20", so the
// ladder (to end the park when the level is there), the needs tier (to find what an account is waiting for) and the report
// (cmd/questneeds) all read the same code.
const NeedsParkPrefix = "needs:"

// TrainNeed is one stint a kit waits for: a kit line whose only source left is a skill source the account is below the level
// of, and which the catalogue can train.
type TrainNeed struct {
	Line   KitLine
	Source Source
	// Skill is the lower-case skill and Level the base level its source wants; From is the account's own (1 when unread).
	Skill string
	From  int
	Level int
	// Qty is how many of the line the kit is short.
	Qty int
	// Trainer is the id of the catalogue row that takes the account from From, for the log.
	Trainer string
}

// Describe is the need's log form.
func (n TrainNeed) Describe() string {
	return fmt.Sprintf("%s %d for %d %s via %s, trained by %s", n.Skill, n.Level, n.Qty, n.Line.Name, n.Source.Method, n.Trainer)
}

// NeedsParkCode is the stable part of a park reason for skill at level: "needs:mining:20".
func NeedsParkCode(skill string, level int) string {
	return NeedsParkPrefix + strings.ToLower(skill) + ":" + strconv.Itoa(level)
}

// TrainReason is the reason a quest parks under when its kit waits for the first of needs: the stable code, then a space and the
// words for the log.
func TrainReason(needs []TrainNeed) string {
	if len(needs) == 0 {
		return ""
	}
	n := needs[0]
	r := fmt.Sprintf("%s for %d %s (%s is its only source left, the account is at %s %d): a stint trains it outside the quest's slot",
		NeedsParkCode(n.Skill, n.Level), n.Qty, n.Line.Name, n.Source.Method, n.Skill, n.From)
	if len(needs) > 1 {
		r += fmt.Sprintf("; %d more line(s) wait for training too", len(needs)-1)
	}
	return r
}

// ParseNeedsPark reads the skill and level out of a park reason written by TrainReason: ok is false for any other reason.
func ParseNeedsPark(reason string) (skill string, level int, ok bool) {
	rest, found := strings.CutPrefix(reason, NeedsParkPrefix)
	if !found {
		return "", 0, false
	}
	skill, tail, found := strings.Cut(rest, ":")
	if !found || skill == "" || skill != strings.ToLower(skill) || strings.ContainsAny(skill, " \t") {
		return "", 0, false
	}
	end := 0
	for end < len(tail) && tail[end] >= '0' && tail[end] <= '9' {
		end++
	}
	if end == 0 {
		return "", 0, false
	}
	level, err := strconv.Atoi(tail[:end])
	if err != nil || level < 1 || level > levels.MaxLevel {
		return "", 0, false
	}
	return skill, level, true
}

// trainability judges a Train-marked skill source the account is below the level of: the need it is, or why no stint can
// meet it. levelNow is the account's base level (0 when unread, taken as 1).
func trainability(l KitLine, s Source, short, levelNow int, cat Catalogue) (TrainNeed, string) {
	skill := strings.ToLower(s.Skill)
	from := max(levelNow, 1)
	if u := cat.FirstUncovered(skill, from, s.Level); u != 0 {
		why := fmt.Sprintf("no trainer for %s %d (no row trains it from level %d)", skill, s.Level, u)
		if g, ok := GapFor(skill, u); ok {
			lane := ""
			if g.Lane != "" && g.Lane != "-" {
				lane = "lane " + g.Lane + ": "
			}
			why = fmt.Sprintf("no trainer for %s %d (%s%s)", skill, s.Level, lane, g.Reason)
		}
		return TrainNeed{}, why
	}
	need := TrainNeed{Line: l, Source: s, Skill: skill, From: from, Level: s.Level, Qty: short}
	if rows := cat.For(skill, from); len(rows) > 0 {
		need.Trainer = rows[0].ID
	}
	return need, ""
}
