package questrunner

import "rsb0t-go/internal/loot"

// What the quest ladder's training rungs report to the Loot tab (a user request: bow strings
// strung and every other item made while Crafting or Fletching is trained inside a ScarabSwarm quest
// stage were nowhere on the tracker, because the rungs run on the quest engine's own primitives and
// none of them reported a thing). Every helper is nil-safe: a Deps with no Loot sink (a harness, a
// test) measures and records nothing.

// LootCounts is the backpack as loot.Counts, or nil when nothing is being recorded, so a harness
// does not even read the pack.
func LootCounts(d *Deps) loot.Counts {
	if d == nil || d.Loot == nil || d.Session == nil {
		return nil
	}
	out := loot.Counts{}
	for id, n := range PackCounts(d) {
		out[id] += int64(n)
	}
	return out
}

// ReportMade reports one production batch: events materials consumed, and what the pack gained
// since before (loot.MadeFrom), apart from the ids in skip (tools, the materials themselves). It
// reads the pack now, so call it BEFORE banking the products.
func ReportMade(d *Deps, src loot.Source, events int, before loot.Counts, skip ...int) {
	if d == nil || d.Loot == nil || before == nil || events <= 0 {
		return
	}
	skipped := map[int]bool{}
	for _, id := range skip {
		skipped[id] = true
	}
	loot.MadeFrom(d.Loot, d.Config, src, events, before, LootCounts(d), func(id int) bool { return skipped[id] })
}

// ReportSpun reports flax spun into Bow strings, or wool into Balls of wool.
func ReportSpun(d *Deps, flax bool, n int) {
	if d == nil || d.Loot == nil {
		return
	}
	loot.Spun(d.Loot, d.Config, flax, n)
}

// ReportGathered reports what a gathering rung took out of the world: events gathered (logs cut),
// and the pack's gain of the ids in want since before. Only the wanted ids count, so a pack that
// moved for another reason (a withdrawal, a random event's hand-out) is not filed as a catch.
func ReportGathered(d *Deps, src loot.Source, events int, before loot.Counts, want ...int) {
	if d == nil || d.Loot == nil || before == nil || events <= 0 {
		return
	}
	wanted := map[int]bool{}
	for _, id := range want {
		wanted[id] = true
	}
	got := loot.Gained(before, LootCounts(d), func(id int) bool { return !wanted[id] })
	if len(got) == 0 {
		return
	}
	d.Loot(loot.Drop{Source: src, Events: events, Items: loot.Items(d.Config, got)})
}
