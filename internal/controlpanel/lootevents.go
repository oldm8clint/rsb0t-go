package controlpanel

import (
	"sort"

	"rsb0t-go/internal/loot"
)

// RANDOM-EVENT RECORDS, BROUGHT ONTO TODAY'S SHAPE WHEN THE LIFETIME FILE IS READ BACK.
//
// Two things in the live file (stats-state.json, build 73ff4228, 5 Oct 2026) were wrong and would
// have stayed wrong forever, because lifetime totals are persisted and nothing rewrote them:
//
//   - Two random events sat under a npc id that is not theirs. `event:genie:411` is the Genie
//     recorded when the reward table still said 411 -- npc 411 is macro_SWARM, so the panel drew the
//     Genie as a Swarm (owner: "genie was also showing as a swarm last time it displayed") -- and
//     `event:mysterious old man:2344`, which is no npc at all (content/pack/npc.pack ends at 1595).
//     The table has been right since: new records go to `event:genie:409` and `...:410`, so the old
//     ones were stranded beside them as a second Genie and a second Old Man.
//   - An event's OWN item sat on cards it does not belong to: 195 Lamps and Strange boxes across
//     fourteen cards (Cow, Chicken, the Goblins, a Giant, a Casket, a stall...), picked up by
//     whatever window was open when an event paid out (owner: "currently lamps are showing up in
//     wrong sections").
//
// Both are fixed at the source now (loot.Migrate and the table; loot.Tally.Add and the take windows),
// so this only carries what is already written. What it does NOT undo: an event's REWARDS that
// landed on kill cards (a Maze's Feather x5,014 on the Cow). They are ordinary items that real
// drops share -- cows drop none of them, giants drop runes and arrows -- so no rule can take them
// back out of a total without deleting real loot. They stay in history; the cards stop growing.

// eventFold is what foldRetiredEventKeys moved, so the history buckets follow their records.
type eventFold struct {
	// keys maps an old source key to the key its record now counts under. Every account's record
	// had the same old key, so this is small.
	keys map[string]string
}

// moveEvents re-keys one history bucket's event counts to follow the fold.
func (f eventFold) moveEvents(events map[string]int64) {
	groundFold{keys: f.keys}.moveEvents(events)
}

// foldRetiredEventKeys merges every random-event record whose key no longer matches its Source (loot.Migrate
// has already rewritten the Source to the id the reward table has now) into that source's record,
// creating it when the account has none, and deletes the old key. Events and XP add, items and spent
// add by id, and First/Last widen to the span of both. Idempotent: a second pass finds every key
// already matching.
func foldRetiredEventKeys(lifetime map[string]map[string]*lootRec) eventFold {
	fold := eventFold{keys: map[string]string{}}
	for _, sources := range lifetime {
		var stale []string
		for key, rec := range sources {
			if rec.Source.Kind == loot.KindRandomEvent && rec.Source.Key() != key {
				stale = append(stale, key)
			}
		}
		sort.Strings(stale)
		for _, key := range stale {
			rec := sources[key]
			canon := rec.Source.Key()
			target := sources[canon]
			if target == nil {
				target = &lootRec{Source: rec.Source}
				sources[canon] = target
			}
			target.Events += rec.Events
			target.XP += rec.XP
			target.addItems(rec.Items)
			target.addSpent(rec.Spent)
			target.addSkills(rec.Skills)
			if rec.First > 0 && (target.First == 0 || rec.First < target.First) {
				target.First = rec.First
			}
			if rec.Last > target.Last {
				target.Last = rec.Last
			}
			delete(sources, key)
			fold.keys[key] = canon
		}
	}
	return fold
}

// stripForeignEventItems takes an event's own item off every record that is not that event
// (loot.ForeignEventItem): the Lamps and Strange boxes a kill or a casket window picked up while an
// event paid out. A record left with nothing at all by it (no events, no items, no XP) is deleted
// rather than shown as an empty card. Idempotent.
func stripForeignEventItems(lifetime map[string]map[string]*lootRec) {
	for _, sources := range lifetime {
		for key, rec := range sources {
			kept := loot.WithoutForeignEventItems(rec.Source, rec.Items)
			if len(kept) == len(rec.Items) {
				continue
			}
			rec.Items = kept
			if rec.Events == 0 && rec.XP == 0 && len(rec.Items) == 0 && len(rec.Spent) == 0 && len(rec.Skills) == 0 {
				delete(sources, key)
			}
		}
	}
}
