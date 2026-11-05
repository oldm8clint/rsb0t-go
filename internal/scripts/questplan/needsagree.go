package questplan

import (
	"fmt"
	"sort"
	"strings"
)

// RECORD AND FACTS AGREE (plan 3.2 A, the 14 October 2026 lesson).
//
// A quest's Record is what CheckRequirements holds it back on, and LadderFacts is what the ladder's planner and LadderHolds
// read. On 14 Oct 2026 FremennikRecord, ScorpionCatcherRecord and HorrorRecord declared none of the levels LadderFacts gated them
// on, and twenty-two accounts at Woodcutting 2 walked to Rellekka. LadderHolds now reads the facts, so the ladder is safe; what is
// left is that the two tables can still say different things, and the needs engine merges both. RecordFactsDisagreements is the
// check that they do not, used by a test over every record (questplan) and over the records the registry cannot reach (the bot).
//
// The rules, for a quest with a record:
//
//   - every skill level the Record states is in the facts at the same level (as a gate of any kind), except the ones listed in
//     RecordOnlyGates, which the facts deliberately leave to the Record;
//   - every HARD gate of the facts (server, owner, module, item) is in the Record at the same level, except the ladder-only gates
//     listed in FactsOnlyGates, each with its reason;
//   - the quest-point minimum is the same in both;
//   - every prerequisite quest of the Record is a prerequisite of the facts, and every prerequisite of the facts is the Record's
//     except those in FactsOnlyPrereqs.
//
// The ladder enforces both halves of the exceptions (LadderHolds the facts-only gates, CheckRequirements the record-only ones), so
// no level falls between them; the lists exist so that a NEW difference is a decision somebody wrote down. ExceptionProblems holds
// the three lists exact.

// FactsOnlyGates are the hard gates LadderFacts states and the Record deliberately does not, by "quest:skill". The ladder
// enforces them (LadderHolds, since 14 Oct 2026) and the module waits for them itself; a direct run of the module (a lab harness,
// questladder with a list) would not, and that is the price of leaving a Record alone that a module's Decide reads. A new entry
// is a decision; a stale one fails TestTheExceptionListsAreExact.
var FactsOnlyGates = map[string]string{
	"dragon:smithing":       "the nails' Smithing 34 is an item gate the provisioner waives by 12 owned nails; the Record states none",
	"fremennik:agility":     "the basalt rocks to Rellekka: a module gate (LadderHolds holds it); the Record states none",
	"fremennik:crafting":    "Olaf's lyre: the 14 Oct 2026 failure; LadderHolds holds it, the Record is silent",
	"fremennik:fletching":   "Olaf's lyre: the 14 Oct 2026 failure; LadderHolds holds it, the Record is silent",
	"fremennik:woodcutting": "Olaf's lyre: the 14 Oct 2026 failure; LadderHolds holds it, the Record is silent",
	"horror:agility":        "the lighthouse key's Barbarian pipe: a server gate LadderHolds holds; the Record states none",
	"horror:prayer":         "Protect from Missiles: a module gate LadderHolds holds; the Record states none",
	"horror:smithing":       "the nails' Smithing 34 is an item gate the provisioner waives by 8 owned nails; the Record states none",
	"scorpion:prayer":       "the Edgeville monastery's Prayer 31: a server gate LadderHolds holds; the Record states none",
	"spirit:crafting":       "the silver sickle's Crafting 18 is an item gate the sickle itself waives; the Record states none",
}

// RecordOnlyGates are the levels a Record states and the facts row deliberately leaves out, by "quest:skill". A quest-point
// filler's row carries no gate, no prerequisite and no combat advice: its safety floor is the Record's (the fillers' ladder
// tests pin it, so that the ladder's planner never inserts training for one filler's floor). The same holds for the other
// floors listed, which the Record's CheckRequirements holds and the facts leave to it.
var RecordOnlyGates = map[string]string{
	"cog:hitpoints":          "a filler's row carries no gate: the ogre cave's floor is the Record's",
	"demon:hitpoints":        "a filler's row carries no gate: Delrith's and the sewer's floor is the Record's (DemonMinHitpoints)",
	"druid:hitpoints":        "a filler's row carries no gate: the dungeon's floor is the Record's (DruidMinHitpoints)",
	"drunkmonk:hitpoints":    "a filler's row carries no gate: its floor is the Record's",
	"fishingcompo:fishing":   "a filler's row carries no gate: Fishing 10 is the Record's",
	"junglepotion:herblore":  "a filler's row carries no gate: the identify step's Herblore 3 is the Record's (Druidic Ritual pays the XP)",
	"junglepotion:hitpoints": "a filler's row carries no gate: its floor is the Record's (JungleMinHitpoints)",
	"pip:attack":             "the fleet's safety floor for the Monk of Zamorak and the guardian: the Record's alone",
	"pip:defence":            "the fleet's safety floor for the Monk of Zamorak and the guardian: the Record's alone",
	"pip:hitpoints":          "the fleet's safety floor for the Monk of Zamorak and the guardian: the Record's alone",
	"tbwt:hitpoints":         "a filler's row carries the journal's levels only: the module's own floor is the Record's (TbMinHitpoints)",
	"totem:thieving":         "a filler's row carries no gate: the trapped stairs' Thieving 21 is the Record's",
	"vampire:defence":        "the fleet's safety floor for the Count: the Record's alone",
	"vampire:hitpoints":      "the fleet's safety floor for the Count: the Record's alone",
	"waterfall:defence":      "the fleet's safety floor (waterfallplan.Record); the facts carry the owner's Hitpoints 20 only",
}

// FactsOnlyPrereqs are the prerequisites LadderFacts names and the Record does not, by "quest:prerequisite".
var FactsOnlyPrereqs = map[string]string{
	"spirit:ghost":      "content checks a WORN Ghostspeak amulet, not the quest; the facts keep the quest as the ladder's order",
	"horror:barcrawl":   "the Barcrawl is a miniquest with no quest tab (the Barbarian Outpost gate); the ladder reads it from its own Done",
	"scorpion:barcrawl": "the Barcrawl is a miniquest with no quest tab (the Barbarian Outpost gate); the ladder reads it from its own Done",
}

// RecordFactsDisagreements lists the ways quest id's facts and Record disagree, "" for none.
func RecordFactsDisagreements(id string, f RungFacts, r Record) []string {
	var out []string
	factLevel := map[string]int{}
	factHard := map[string]int{}
	for _, g := range f.Gates {
		s := strings.ToLower(g.Skill)
		factLevel[s] = max(factLevel[s], g.Level)
		if g.Kind != GateAdvised {
			factHard[s] = max(factHard[s], g.Level)
		}
	}
	recLevel := map[string]int{}
	for _, s := range r.Requirements.Skills {
		recLevel[strings.ToLower(s.Skill)] = max(recLevel[strings.ToLower(s.Skill)], s.Level)
	}
	skills := map[string]bool{}
	for s := range factLevel {
		skills[s] = true
	}
	for s := range recLevel {
		skills[s] = true
	}
	names := make([]string, 0, len(skills))
	for s := range skills {
		names = append(names, s)
	}
	sort.Strings(names)
	for _, s := range names {
		rl, rok := recLevel[s]
		fl, fok := factLevel[s]
		switch {
		case rok && !fok:
			if _, listed := RecordOnlyGates[id+":"+s]; !listed {
				out = append(out, fmt.Sprintf("%s: the Record requires %s %d and LadderFacts has no gate for it; add the gate or list it in RecordOnlyGates", id, s, rl))
			}
		case rok && fok && rl != fl:
			out = append(out, fmt.Sprintf("%s: the Record requires %s %d and LadderFacts gates it at %d", id, s, rl, fl))
		case !rok && factHard[s] > 0:
			if _, listed := FactsOnlyGates[id+":"+s]; !listed {
				out = append(out, fmt.Sprintf("%s: LadderFacts gates %s %d (hard) and the Record states none; state it or list it in FactsOnlyGates", id, s, factHard[s]))
			}
		}
	}
	if f.QPNeeded != r.Requirements.MinQuestPoints {
		out = append(out, fmt.Sprintf("%s: LadderFacts wants %d quest points and the Record %d", id, f.QPNeeded, r.Requirements.MinQuestPoints))
	}
	facts := map[string]bool{}
	for _, p := range f.Prereqs {
		facts[p] = true
	}
	rec := map[string]bool{}
	for _, p := range r.Requirements.Quests {
		rec[p] = true
		if !facts[p] {
			out = append(out, fmt.Sprintf("%s: the Record requires quest %s and LadderFacts does not list it", id, p))
		}
	}
	for p := range facts {
		if !rec[p] {
			if _, listed := FactsOnlyPrereqs[id+":"+p]; !listed {
				out = append(out, fmt.Sprintf("%s: LadderFacts requires quest %s and the Record does not; state it or list it in FactsOnlyPrereqs", id, p))
			}
		}
	}
	sort.Strings(out)
	return out
}

// ExceptionProblems holds the three exception lists exact: an entry must name a difference that exists, so that a gate which is
// now stated on both sides, or has left one, comes off its list. recordOf gives the records; an entry whose quest has no record in
// it is skipped (Waterfall's is not in questplan's own tests, and internal/bot checks it).
func ExceptionProblems(facts map[string]RungFacts, recordOf func(id string) (Record, bool)) []string {
	var out []string
	for key := range FactsOnlyGates {
		id, skill, _ := strings.Cut(key, ":")
		f, fok := facts[id]
		r, rok := recordOf(id)
		if !fok {
			out = append(out, "FactsOnlyGates names "+key+", which has no facts")
			continue
		}
		if !rok {
			continue
		}
		hard := 0
		for _, g := range f.Gates {
			if strings.EqualFold(g.Skill, skill) && g.Kind != GateAdvised {
				hard = max(hard, g.Level)
			}
		}
		if hard == 0 || recordStatesLevel(r, skill, 1) {
			out = append(out, fmt.Sprintf("FactsOnlyGates lists %s but the facts' hard gate is %d and the Record states it: %v", key, hard, recordStatesLevel(r, skill, 1)))
		}
	}
	for key := range RecordOnlyGates {
		id, skill, _ := strings.Cut(key, ":")
		f, fok := facts[id]
		r, rok := recordOf(id)
		if !fok {
			out = append(out, "RecordOnlyGates names "+key+", which has no facts")
			continue
		}
		if !rok {
			continue
		}
		inFacts := false
		for _, g := range f.Gates {
			inFacts = inFacts || strings.EqualFold(g.Skill, skill)
		}
		if inFacts || !recordStatesLevel(r, skill, 1) {
			out = append(out, fmt.Sprintf("RecordOnlyGates lists %s but the facts gate it: %v and the Record states it: %v", key, inFacts, recordStatesLevel(r, skill, 1)))
		}
	}
	for key := range FactsOnlyPrereqs {
		id, pre, _ := strings.Cut(key, ":")
		f, fok := facts[id]
		r, rok := recordOf(id)
		if !fok {
			out = append(out, "FactsOnlyPrereqs names "+key+", which has no facts")
			continue
		}
		if !rok {
			continue
		}
		inFacts, inRecord := false, false
		for _, p := range f.Prereqs {
			inFacts = inFacts || p == pre
		}
		for _, p := range r.Requirements.Quests {
			inRecord = inRecord || p == pre
		}
		if !inFacts || inRecord {
			out = append(out, fmt.Sprintf("FactsOnlyPrereqs lists %s but the facts list it: %v and the Record states it: %v", key, inFacts, inRecord))
		}
	}
	sort.Strings(out)
	return out
}
