// Package bankxp answers "how much experience is sitting unprocessed in a bank, and in which
// skill" — the account owner's own ask for the Combined bank panel: "combined bank should also
// show banked exp from processing all our goods in various means."
//
// # Where the numbers come from
//
// Every figure in Routes (table.go) is GENERATED from the LostCityRS Content checkout that this
// server runs (branch 289), by internal/bankxp/gen. Nothing here is typed from memory or from a
// wiki, because this repository has a documented history of confident numbers that were wrong
// (docs/trustworthy_harnesses.md). Each Route carries the content file and the row or section it
// came from, so any single number can be checked against the source in one grep.
//
// # Units
//
// XP10 is the raw content figure, which is TENTHS of an experience point. That is the engine's
// own storage unit, not an invention here: Player.addXp's own comment says the 200m cap "is
// represented as '2 billion' because we use 32-bit signed integers and divide by 10 to give us a
// decimal point" (engine/src/engine/entity/Player.ts). Content's cooking row for raw beef says
// experience=300 and cooked beef is 30 XP.
//
// # What this deliberately does NOT claim
//
//   - It is not a prediction of XP the fleet will actually earn. It is the experience the banked
//     items are WORTH if somebody processed them. Whether anybody will is a scheduling question.
//   - A server-side XP multiplier is not knowable from content. Engine-TS reads NODE_XPRATE into
//     Environment.node.xpRate and multiplies every stat_advance by it (Player.addXp), defaulting
//     to 1. The live server's own rate has NOT been measured, so Plan takes a caller-supplied rate and the
//     panel shows 1x unless an operator sets otherwise.
//   - Ingredients shared by two chosen routes are counted for both. Only the primary input of a
//     route is exclusively allocated (one route per banked item), so a bank holding both steel
//     bars and the coal that would smelt more of them can read slightly high. Plan reports the
//     limiting ingredient so this is visible rather than hidden.
package bankxp

import (
	"sort"
	"strings"
)

// Ingredient is one input a route consumes per craft.
type Ingredient struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Route is one way to process one banked item into experience.
//
// Inputs[0] is the PRIMARY input — the banked item this route is "for". A route is offered for
// that item and for no other, which is what makes "at most one route per banked item" a
// well-defined rule (see Plan).
type Route struct {
	// Key is the content row or struct section this came from, e.g. "cooking_generic_lobster".
	// It is stable across regenerations and is what a doubter greps for.
	Key string `json:"key"`
	// Skill is the display name of the skill advanced, as the panel spells skills elsewhere.
	Skill string `json:"skill"`
	// Via is a short human phrase for what is done, e.g. "Cook on a range".
	Via string `json:"via"`
	// Inputs[0] is the primary banked item; the rest are other items the craft needs.
	Inputs []Ingredient `json:"inputs"`
	// Product is what comes out, with its per-craft count.
	Product Ingredient `json:"product"`
	// XP10 is experience in TENTHS of a point per craft, exactly as content stores it.
	XP10 int `json:"xp10"`
	// Level is the skill level content refuses the craft below.
	Level int `json:"level"`
	// Roll says content gives this craft a stat_random success roll that can fail and pay
	// nothing (a cooking burn, a shattered opal). It is a separate flag rather than
	// "both bounds are zero", because content uses 0,0 to mean ALWAYS BURN — cooking.rs2's
	// own comment: "field can specify 0 to always burn, or 1 to always cook".
	Roll        bool `json:"roll,omitempty"`
	SuccessLow  int  `json:"successLow,omitempty"`
	SuccessHigh int  `json:"successHigh,omitempty"`
	// Fleet names the registered script in this repo that actually runs this route today,
	// empty when no script does. This is what lets the panel separate "XP the fleet is set up
	// to collect" from "XP that would need a script somebody has not written".
	Fleet string `json:"fleet,omitempty"`
	// Source is the content file the numbers were read from.
	Source string `json:"source"`
}

// Primary is the banked item the route is offered for.
func (r Route) Primary() Ingredient {
	if len(r.Inputs) == 0 {
		return Ingredient{}
	}
	return r.Inputs[0]
}

// SuccessChance is the probability one craft pays its experience at the given level, using the
// engine's own roll rather than a remembered approximation:
//
//	value  = floor(low*(99-L)/98) + floor(high*(L-1)/98) + 1
//	chance = floor(random()*256)          // 0..255
//	success iff value > chance
//
// (engine/src/engine/script/handlers/PlayerOps.ts, ScriptOpcode.STAT_RANDOM). So exactly
// `value` of the 256 possible rolls succeed, and the probability is value/256 clamped to [0,1].
// A route with no roll bounds always succeeds.
func (r Route) SuccessChance(level int) float64 {
	if !r.Roll {
		return 1
	}
	// Content's own escape hatches, read straight out of cooking.rs2: "field can specify 0 to
	// always burn, or 1 to always cook".
	if r.SuccessLow == 0 && r.SuccessHigh == 0 {
		return 0
	}
	if r.SuccessLow == 1 && r.SuccessHigh == 1 {
		return 1
	}
	if level > 99 {
		level = 99
	}
	if level < 1 {
		level = 1
	}
	value := (r.SuccessLow*(99-level))/98 + (r.SuccessHigh*(level-1))/98 + 1
	if value <= 0 {
		return 0
	}
	if value >= 256 {
		return 1
	}
	return float64(value) / 256
}

// Policy chooses which route wins when a banked item has more than one.
type Policy string

const (
	// PolicyFleet counts only routes a registered script in this repo actually runs. Anything
	// else is reported as unprocessed with no route, which is the honest reading of "what is
	// this fleet's banked XP".
	PolicyFleet Policy = "fleet"
	// PolicyBest takes the highest-XP route per item and NAMES it, so "best" is never passed
	// off as "what will happen".
	PolicyBest Policy = "best"
)

// Levels is a skill display name -> current level.
//
// A skill ABSENT from a non-empty map reads as level 1, not level 0, because 1 is the game's
// own floor for every skill and a fleet reports a skill only once it has sent a stat update
// for it. Reading an absent skill as 0 made every level-1 route (burying a bone, cutting logs
// into shafts, cooking a shrimp) look blocked on an account that simply had not mentioned that
// stat yet — a false "you cannot do this" is exactly the kind of quietly wrong figure this
// package is built to avoid. Whether levels are known AT ALL is the caller's question, and
// internal/controlpanel answers it separately with LevelsKnown.
type Levels map[string]int

// Level is the account's level in one skill, with the game's own floor of 1 for a skill the
// fleet has not reported.
func (l Levels) Level(skill string) int {
	if v, ok := l[skill]; ok {
		return v
	}
	return 1
}

// Options control one Plan.
type Options struct {
	// Policy picks the route per item.
	Policy Policy
	// Levels, when non-nil, gates routes the account cannot perform and scales a fallible
	// route by its real success chance at that level. Nil means "ignore levels", which
	// produces an explicitly aspirational figure.
	Levels Levels
	// XPRate is the server's stat_advance multiplier (Engine-TS Environment.node.xpRate).
	// 0 is read as 1.
	XPRate float64
}

// Line is one item's contribution to the plan.
type Line struct {
	ItemID   int    `json:"id"`
	ItemName string `json:"name"`
	Have     int64  `json:"have"`
	// Route is the chosen route's key, empty when none was chosen.
	RouteKey string `json:"routeKey,omitempty"`
	Skill    string `json:"skill,omitempty"`
	Via      string `json:"via,omitempty"`
	// Crafts is how many times the route can run against this bank.
	Crafts int64 `json:"crafts"`
	// XP is the experience those crafts pay, already scaled by XPRate and, when Levels was
	// given, by the route's success chance. It is a whole number of experience points.
	XP int64 `json:"xp"`
	// LimitedBy names the ingredient that ran out first when it was not the primary item.
	LimitedBy string `json:"limitedBy,omitempty"`
	// Blocked says why nothing was counted: "no-route", "level", or "missing-ingredient".
	Blocked string `json:"blocked,omitempty"`
	// NeedLevel is the level the route wanted when Blocked is "level".
	NeedLevel int `json:"needLevel,omitempty"`
	// Chance is the per-craft success probability that was applied, 1 when none was.
	Chance float64 `json:"chance,omitempty"`
	// Alternatives is how many other routes this item had, so the panel can say "1 of 3".
	Alternatives int `json:"alternatives,omitempty"`
}

// SkillTotal is one skill's share of the plan.
type SkillTotal struct {
	Skill string `json:"skill"`
	XP    int64  `json:"xp"`
	Items int64  `json:"items"`
}

// Plan is the answer for one bank (or for a merged set of banks).
type Plan struct {
	Lines  []Line       `json:"lines"`
	Skills []SkillTotal `json:"skills"`
	// XP is the total across every skill.
	XP int64 `json:"xp"`
	// Unrouted is how many individual items had no route at all under this policy.
	Unrouted int64 `json:"unrouted"`
	// UnroutedKinds is how many distinct item kinds had no route.
	UnroutedKinds int `json:"unroutedKinds"`
	// LevelBlocked is how many individual items had a route the levels refused.
	LevelBlocked int64 `json:"levelBlocked"`
}

// byPrimary indexes Routes by the id of their primary input. Built once.
var byPrimary = func() map[int][]Route {
	m := map[int][]Route{}
	for _, r := range Routes {
		p := r.Primary()
		m[p.ID] = append(m[p.ID], r)
	}
	return m
}()

// RoutesFor returns every route offered for one banked item id, in table order.
func RoutesFor(id int) []Route { return byPrimary[id] }

// Compute builds the plan for a bank given as item id -> count.
//
// One route at most is chosen per banked item, so the same physical item is never processed
// twice. Secondary ingredients are NOT exclusively allocated — see this package's doc comment
// for exactly what that can overstate and why the limiting ingredient is reported.
func Compute(have map[int]int64, names map[int]string, opt Options) Plan {
	rate := opt.XPRate
	if rate <= 0 {
		rate = 1
	}
	ids := make([]int, 0, len(have))
	for id, count := range have {
		if count > 0 {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)

	var p Plan
	// Each positive-count item emits exactly one line. Reserve it once, while
	// preserving nil Lines for an empty bank.
	if len(ids) > 0 {
		p.Lines = make([]Line, 0, len(ids))
	}
	bySkill := map[string]*SkillTotal{}
	for _, id := range ids {
		count := have[id]
		line := Line{ItemID: id, ItemName: names[id], Have: count, Chance: 1}
		options := byPrimary[id]
		if line.ItemName == "" && len(options) > 0 {
			line.ItemName = options[0].Primary().Name
		}
		line.Alternatives = len(options)
		chosen, ok := choose(options, opt)
		if !ok {
			line.Blocked = "no-route"
			if len(options) > 0 && opt.Levels != nil {
				// Distinguish "the fleet runs nothing for this" from "every route wants a
				// level nobody has": they mean very different things to an operator.
				if need, gated := lowestLevel(options, opt); gated {
					line.Blocked = "level"
					line.NeedLevel = need
					line.Skill = options[0].Skill
					p.LevelBlocked += count
				}
			}
			if line.Blocked == "no-route" {
				p.Unrouted += count
				p.UnroutedKinds++
			}
			p.Lines = append(p.Lines, line)
			continue
		}
		line.RouteKey = chosen.Key
		line.Skill = chosen.Skill
		line.Via = chosen.Via
		crafts := count / int64(max1(chosen.Primary().Count))
		for _, in := range chosen.Inputs[1:] {
			n := have[in.ID] / int64(max1(in.Count))
			if n < crafts {
				crafts = n
				line.LimitedBy = in.Name
			}
		}
		if crafts <= 0 {
			line.Blocked = "missing-ingredient"
			p.Lines = append(p.Lines, line)
			continue
		}
		line.Crafts = crafts
		if opt.Levels != nil {
			line.Chance = chosen.SuccessChance(opt.Levels.Level(chosen.Skill))
		}
		// XP10 is tenths, so divide by 10 at the end and only once.
		line.XP = int64(float64(crafts) * float64(chosen.XP10) * line.Chance * rate / 10)
		p.Lines = append(p.Lines, line)
		t := bySkill[chosen.Skill]
		if t == nil {
			t = &SkillTotal{Skill: chosen.Skill}
			bySkill[chosen.Skill] = t
		}
		t.XP += line.XP
		t.Items += crafts
		p.XP += line.XP
	}
	for _, t := range bySkill {
		p.Skills = append(p.Skills, *t)
	}
	sort.Slice(p.Skills, func(i, j int) bool {
		if p.Skills[i].XP != p.Skills[j].XP {
			return p.Skills[i].XP > p.Skills[j].XP
		}
		return p.Skills[i].Skill < p.Skills[j].Skill
	})
	return p
}

// choose applies the policy and the level gate.
func choose(options []Route, opt Options) (Route, bool) {
	var best Route
	found := false
	for _, r := range options {
		if opt.Policy == PolicyFleet && r.Fleet == "" {
			continue
		}
		if opt.Levels != nil && r.Level > opt.Levels.Level(r.Skill) {
			continue
		}
		if !found || r.XP10 > best.XP10 {
			best, found = r, true
		}
	}
	return best, found
}

// lowestLevel reports the cheapest level requirement among routes that the policy would have
// allowed but the level gate refused, so a blocked line can say what it is waiting for.
func lowestLevel(options []Route, opt Options) (int, bool) {
	need, found := 0, false
	for _, r := range options {
		if opt.Policy == PolicyFleet && r.Fleet == "" {
			continue
		}
		if opt.Levels != nil && r.Level > opt.Levels.Level(r.Skill) {
			if !found || r.Level < need {
				need, found = r.Level, true
			}
		}
	}
	return need, found
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

// Skills lists every skill the table can produce experience in, sorted, for a panel legend.
func Skills() []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range Routes {
		if !seen[r.Skill] {
			seen[r.Skill] = true
			out = append(out, r.Skill)
		}
	}
	sort.Strings(out)
	return out
}

// FleetScripts lists the registered scripts credited in the table, sorted.
func FleetScripts() []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range Routes {
		if r.Fleet != "" && !seen[r.Fleet] {
			seen[r.Fleet] = true
			out = append(out, r.Fleet)
		}
	}
	sort.Strings(out)
	return out
}

// NameMatches reports whether a cache-resolved item name agrees with the name content gave the
// same id. Used as an honesty guard: if the running server's cache disagrees with the content
// checkout the table was generated from, the route is not applied (see internal/controlpanel).
func NameMatches(cacheName, tableName string) bool {
	return strings.EqualFold(strings.TrimSpace(cacheName), strings.TrimSpace(tableName))
}
