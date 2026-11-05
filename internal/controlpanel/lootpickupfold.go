package controlpanel

import (
	"rsb0t-go/internal/loot"
)

// RUNE AND BONE PICKUP CARDS, BROUGHT UNDER THE KILLS THAT LEFT THEM WHEN THE LIFETIME FILE IS READ BACK.
//
// a user request: "death runes show as their own panel, they should not; runes go under the
// activity that used/produced them. Big bones show as their own panel, put them under the activity
// (the npc kills) that produced them." The live file (build 46a4ace1) held "take: Death rune" with
// 136 pickups and "take: Big bones" with 10: a pile no recent kill could claim was recorded as a
// pickup of the item itself. The recorders now put such a pile under the account's newest kill
// (loot.PickupSource); this carries what was already written.
//
// Each such record is merged into the same account's busiest kill record, as foldLegacyGroundPickups
// does for the old Ground openable: the items add by id, First and Last widen, and the pickups are NOT
// added as kills (a take is not a kill; the kill was counted when it happened). An account with no kill
// record keeps its pickup record, because nothing is lost by leaving it and nothing else could claim
// it. The recent-drops ticker follows its records, and the history buckets lose the pickup event key
// (a pickup is not an event on the kill). Idempotent: a second pass finds no such record.

// foldRunePickups folds every rune or bone pickup record onto its account's busiest kill. It returns
// the keys it removed, so the history buckets can drop their event counts, and the kill each account's
// pickups moved to, so the recent ticker can follow.
func foldRunePickups(lifetime map[string]map[string]*lootRec) (removed map[string]bool, target map[string]loot.Source) {
	removed = map[string]bool{}
	target = map[string]loot.Source{}
	for acct, sources := range lifetime {
		for key, rec := range sources {
			if rec.Source.Kind != loot.KindPickup || rec.Source.Action != loot.ActionTake || !loot.FoldsIntoKill(rec.Source.ID) {
				continue
			}
			var kill *lootRec
			for _, cand := range sources {
				if cand.Source.Kind != loot.KindNPC || cand.Source.Action != loot.ActionKill || loot.DropsNothing(cand.Source) {
					continue
				}
				if kill == nil || cand.Events > kill.Events {
					kill = cand
				}
			}
			if kill == nil {
				continue
			}
			kill.addItems(rec.Items)
			if rec.First > 0 && (kill.First == 0 || rec.First < kill.First) {
				kill.First = rec.First
			}
			if rec.Last > kill.Last {
				kill.Last = rec.Last
			}
			delete(sources, key)
			removed[key] = true
			target[acct] = kill.Source
		}
	}
	return removed, target
}

// unfileNoDropKills moves the items an older build credited to a kill of an npc that drops nothing (loot.DropsNothing: the Khazard
// trooper) onto pickup cards of the items themselves, one per item, in the same account's records. foldRunePickups then folds the runes
// and bones among them under a kill that can drop them, and leaves the rest on their own pickup cards. The kill keeps its events, XP and
// Spent. Idempotent: a second pass finds no items on such a kill.
func unfileNoDropKills(lifetime map[string]map[string]*lootRec) {
	for _, sources := range lifetime {
		for _, rec := range sources {
			if !loot.DropsNothing(rec.Source) || len(rec.Items) == 0 {
				continue
			}
			for _, it := range rec.Items {
				if it.Count <= 0 {
					continue
				}
				src := loot.Pickup(nil, it.ID, it.Name)
				key := src.Key()
				dst := sources[key]
				if dst == nil {
					dst = &lootRec{Source: src}
					sources[key] = dst
				}
				dst.addItems([]loot.Item{it})
				if rec.First > 0 && (dst.First == 0 || rec.First < dst.First) {
					dst.First = rec.First
				}
				if rec.Last > dst.Last {
					dst.Last = rec.Last
				}
			}
			rec.Items = nil
		}
	}
}
