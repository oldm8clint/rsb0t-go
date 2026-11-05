package questplan

import (
	"fmt"
	"sort"
	"strings"

	"rsb0t-go/internal/scripts/levels"
)

// THE FLEET'S UNMET NEEDS (plan 3.9, "Blocker report"): cmd/questneeds prints this. It repeats the 19 Oct 2026 audit
// read-only, and every later lane's "done when" is checked against it: after T1 the Mining line moves from "no trainer" to a
// row, after X1 Nature Spirit's silver bar has an open link, after N0 no account is blocked on the Barcrawl.
//
// It counts accounts and never names one.

// NeedCount is one need and how many accounts are short of it.
type NeedCount struct {
	Need     Need
	Accounts int
	// Status and Acquirer say what gets the need met: a stint of a rung, a loop that already runs, a quest that comes
	// first, a named gap.
	Status   NeedStatus
	Acquirer string
	// Lane is the plan lane that closes the gap, "" when the need has an acquirer.
	Lane string
}

// ItemChainReport is an item need and its chain, account-independent.
type ItemChainReport struct {
	Item     ItemNeed
	Accounts int
	Open     string
	Gap      string
}

// QuestReport is one quest across the fleet.
type QuestReport struct {
	Quest string
	Name  string
	QP    int
	// Done is the accounts whose store says the quest is complete, Ready the ones with no hard unmet need, Blocked the ones with at
	// least one. Unjudged counts accounts with a need the holdings were not read for.
	Done, Ready, Blocked, Unjudged int
	Hard                           []NeedCount
	Advised                        []NeedCount
	Items                          []ItemChainReport
}

// SkillBill is the training the fleet is short of in one skill: for each account, the highest level a quest it has not done
// asks of it.
type SkillBill struct {
	Skill string
	// Accounts below the level some quest left to them asks for, MissingXP the sum of what they lack to reach the highest such
	// level each, Hours that at the catalogue's best path (0 when none covers it).
	Accounts  int
	MissingXP int64
	Hours     float64
	Covered   bool
	Acquirer  string
	Lane      string
}

// FleetReport is the whole fleet's needs.
type FleetReport struct {
	Accounts int
	Quests   []QuestReport
	Bill     []SkillBill
	// Unreachable are the (skill, level) needs no trainer covers, with the accounts blocked on each.
	Unreachable []NeedCount
}

// AcquirerOf says, account-independently, what gets a need met: the first trainers of the best path from level 1, a loop, a
// prerequisite quest, or the gap that has none. It also says whether the planner would stint it under the settings.
func AcquirerOf(cat Catalogue, set NeedsSettings, n Need) (label string, status NeedStatus, lane string) {
	switch n.Kind {
	case NeedQuest:
		return "quest " + n.Prereq, NeedWaitQuest, ""
	case NeedQuestPoints:
		return "the quests that pay quest points", NeedWaitQuest, ""
	case NeedCombat:
		return "stage5-combat loop", NeedLoop, ""
	case NeedItem:
		in := ItemNeed{}
		for _, c := range ItemNeeds {
			if c.Item == n.ItemID && c.Qty == n.Qty {
				in = c
				break
			}
		}
		if in.Gap != "" {
			return in.Gap, NeedUnreachable, in.Lane
		}
		return itemOpenLink(in), NeedProvision, in.Lane
	}
	legs, uncovered := cat.Path(n.Skill, 1, n.Level, nil)
	if uncovered != 0 {
		g, ok := GapFor(n.Skill, uncovered)
		if ok {
			return fmt.Sprintf("no trainer for %s %d: %s", n.Skill, uncovered, g.Reason), NeedUnreachable, g.Lane
		}
		return fmt.Sprintf("no trainer for %s %d", n.Skill, uncovered), NeedUnreachable, ""
	}
	var ids []string
	status = NeedStint
	for _, l := range legs {
		ids = append(ids, l.Trainer.ID)
		if l.Trainer.Via == ViaLoop {
			status = NeedLoop
		} else if ok, why := set.allows(l.Trainer); !ok && status == NeedStint {
			status = NeedSettings
			ids[len(ids)-1] += " (" + why + ")"
		}
	}
	return strings.Join(ids, " > "), status, ""
}

// itemOpenLink names the first link of an item's chain that is built, in the owner's rule order.
func itemOpenLink(in ItemNeed) string {
	for _, l := range in.Chain {
		switch l.Kind {
		case SourceBank:
			continue
		case SourceMule:
			if in.Producer == ProducerExclusive {
				return "the mule, stocked by the designated producer"
			}
			return "the mule"
		default:
			if l.Built {
				if l.Method != "" {
					return string(l.Kind) + " " + l.Method
				}
				return string(l.Kind)
			}
		}
	}
	return "the bank"
}

// FleetNeeds judges every subject against every quest of the order and counts what each is short of. The quests the order
// does not name follow it, unless only is set, which leaves them out of the report and of the training bill.
func FleetNeeds(subs []Subject, order []string, table NeedTable, cat Catalogue, set NeedsSettings, only bool) FleetReport {
	rep := FleetReport{Accounts: len(subs)}
	type acc struct {
		hard, advised map[string]*NeedCount
		qr            *QuestReport
	}
	byQuest := map[string]*acc{}
	var ids []string
	seen := map[string]bool{}
	for _, id := range order {
		if _, ok := table.facts[id]; ok && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for _, id := range table.Quests() {
		if !seen[id] && !only {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		f := table.facts[id]
		byQuest[id] = &acc{hard: map[string]*NeedCount{}, advised: map[string]*NeedCount{},
			qr: &QuestReport{Quest: id, Name: f.Name, QP: f.QP}}
	}
	type bill struct {
		accounts int
		missing  int64
		hours    float64
	}
	bills := map[string]*bill{}
	unreach := map[string]*NeedCount{}

	for _, sub := range subs {
		top := map[string]int{} // the highest hard skill level this account lacks across the quests it has not done
		for _, id := range ids {
			a := byQuest[id]
			if sub.Done[id] {
				a.qr.Done++
				continue
			}
			unmet, unjudged := table.Unmet(sub, set, id)
			if len(unjudged) > 0 {
				a.qr.Unjudged++
			}
			hard := 0
			for _, n := range unmet {
				bucket := a.advised
				if n.hard() {
					bucket, hard = a.hard, hard+1
					if n.Kind == NeedSkill {
						top[n.Skill] = max(top[n.Skill], n.Level)
					}
				}
				c := bucket[n.Key()]
				if c == nil {
					label, st, lane := AcquirerOf(cat, set, n)
					c = &NeedCount{Need: n, Status: st, Acquirer: label, Lane: lane}
					bucket[n.Key()] = c
				}
				c.Accounts++
				if n.hard() && c.Status == NeedUnreachable {
					u := unreach[n.Key()]
					if u == nil {
						cp := *c
						cp.Accounts = 0
						u = &cp
						unreach[n.Key()] = u
					}
					u.Accounts++
				}
			}
			if hard == 0 {
				a.qr.Ready++
			} else {
				a.qr.Blocked++
			}
		}
		for skill, level := range top {
			b := bills[skill]
			if b == nil {
				b = &bill{}
				bills[skill] = b
			}
			cur := sub.Level(skill)
			if cur >= level {
				continue
			}
			b.accounts++
			b.missing += int64(sub.XPFor(skill, level))
			legs, uncovered := cat.Path(skill, cur, level, nil)
			if uncovered == 0 {
				xp := sub.XP[skill]
				for _, l := range legs {
					start := max(xp, levels.XPAtLevel(l.From))
					if need := levels.XPAtLevel(l.To) - start; need > 0 && l.Trainer.Rate() > 0 {
						b.hours += float64(need) / float64(l.Trainer.Rate())
					}
					xp = max(xp, levels.XPAtLevel(l.To))
				}
			}
		}
	}

	for _, id := range ids {
		a := byQuest[id]
		a.qr.Hard = sortedCounts(a.hard)
		a.qr.Advised = sortedCounts(a.advised)
		for _, in := range table.Items(id) {
			r := ItemChainReport{Item: in, Gap: in.Gap, Open: itemOpenLink(in)}
			for _, c := range a.qr.Hard {
				if c.Need.Kind == NeedItem && c.Need.ItemID == in.Item && c.Need.Qty == in.Qty {
					r.Accounts = c.Accounts
				}
			}
			a.qr.Items = append(a.qr.Items, r)
		}
		rep.Quests = append(rep.Quests, *a.qr)
	}
	skills := make([]string, 0, len(bills))
	for s := range bills {
		skills = append(skills, s)
	}
	sort.Strings(skills)
	for _, s := range skills {
		b := bills[s]
		if b.accounts == 0 {
			continue
		}
		sb := SkillBill{Skill: s, Accounts: b.accounts, MissingXP: b.missing, Hours: b.hours}
		// The label is the path to the highest level any quest asks of this skill.
		top := 0
		for _, id := range ids {
			for _, n := range table.needs[id] {
				if n.Kind == NeedSkill && n.Skill == s && n.hard() {
					top = max(top, n.Level)
				}
			}
		}
		label, st, lane := AcquirerOf(cat, set, Need{Kind: NeedSkill, Skill: s, Level: top})
		sb.Acquirer, sb.Lane, sb.Covered = label, lane, st != NeedUnreachable
		rep.Bill = append(rep.Bill, sb)
	}
	rep.Unreachable = sortedCounts(unreach)
	return rep
}

func sortedCounts(m map[string]*NeedCount) []NeedCount {
	out := make([]NeedCount, 0, len(m))
	for _, c := range m {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Accounts != out[j].Accounts {
			return out[i].Accounts > out[j].Accounts
		}
		return out[i].Need.Key() < out[j].Need.Key()
	})
	return out
}
