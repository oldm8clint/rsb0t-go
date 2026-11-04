package controlpanel

// CLEARING LOOT THAT WAS RECORDED WRONGLY (a user request: "loot panel should have ability to
// reset/clear panels and certain stacks of items from npcs (to clear up previous data not tracking
// right)").
//
// Earlier builds filed loot on cards it does not belong to: an event's rewards on the Cow, a kill's
// drops as their own "take:<item>" cards (take:nature rune, take:coins, take:big bones...). Those are
// fixed at the source, but lifetime totals are persisted and nothing rewrites them, so what was
// already written stays until somebody takes it out. POST /api/loot/clear is that: one whole card, or
// one item off a card.
//
// SCOPE. A clear removes what had been recorded up to the moment it was made, everywhere this
// aggregator holds it:
//
//   - the lifetime records of every account, and the since-restart ones (the two periods are the same
//     card, and a card cleared in one must not come back by switching to the other);
//   - the recent-drops ticker rows;
//   - the history buckets' per-SOURCE event counts.
//
// It cannot take one card's share out of the history buckets' per-ITEM gains: those are the whole
// fleet's gain of each item id, never split by source (lootBucket.Items), and they feed only the "Top
// drops" sparklines, which are labelled "all sources, fleet-wide". So they stay, and are the one place
// a cleared item's earlier drops still show. Loot taken after the clear counts again from zero, on a
// card that is made afresh.
//
// WHAT A CLEAR NAMES. A card is its loot.Source.GroupKey, exactly what the panel merges sources on. An
// optional part narrows it to one row of a skill card (loot.Source.PartKey: the Guard row of the
// Thieving card), and an optional item is a cache obj id taken off every record that matches, leaving
// the events and the other items (and the Spent runes, which are what a source used up, not a stack on
// its card). A record with no events, experience, items or spent items left is not kept.
//
// WHY NOTHING COMES BACK.
//
//  1. The persisted loot file IS the aggregate (lootFile: every account's lifetime records, the recent
//     rows, the history), so the cleared aggregates are what the next save writes. clearLoot saves at
//     once rather than waiting for the 30 second timer, and save is serialised (statsAggregator.saveMu)
//     so an older periodic write cannot finish after it and put the cleared loot back.
//  2. The per-process readings (lootAggregator.seen) are left alone on purpose. They are what each poll
//     diffs the fleet's own cumulative tallies against, so loot tallied before the clear is not added a
//     second time, and loot after it is the difference from there. Dropping them would re-add every
//     process-lifetime total the fleet still holds. And clearLoot polls first, so loot the fleet had
//     tallied but this aggregator had not yet read goes with the clear instead of appearing a few
//     seconds after it.
//  3. restore's legacy folds (foldLegacyGroundPickups, foldRetiredEventKeys, stripForeignEventItems)
//     only move records the file still carries in an old shape. The file a clear is saved to has
//     already been through them, so it carries none, and they cannot make a cleared card again
//     (TestClearedLegacyCardsAreNotRebuiltByTheFolds).
//
// The loot file also keeps a short audit list of the clears (lootClear): which card or item, and when.
// Nothing reads it back to remove anything; it is there so the file can say what was taken out.
//
// COST. Nothing here runs unless a clear is made: no per-poll work, no extra state in the /api/loot
// body, and the audit list is bounded (maxLootClears).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"rsb0t-go/internal/loot"
)

const (
	// maxLootClearBytes bounds a POST /api/loot/clear body: a group key, an optional part key and an
	// item id.
	maxLootClearBytes = 1 << 10
	// maxLootKeyBytes bounds a group or part key in a request. Real keys are a kind, an action and a
	// name (loot.Source.GroupKey), so this is generous.
	maxLootKeyBytes = 200
	// maxLootItemID is the largest obj id a request may name; the cache's own are a few thousand.
	maxLootItemID = 1 << 20
	// maxLootClears bounds the audit list kept in the loot file.
	maxLootClears = 100
)

// lootClearRequest is the body of POST /api/loot/clear.
type lootClearRequest struct {
	// Group is the card: loot.Source.GroupKey ("npc|kill|cow", "thieving|thieving", "randomevent").
	Group string `json:"group"`
	// Part narrows the clear to one row of the card: loot.Source.PartKey. Empty means every row.
	Part string `json:"part,omitempty"`
	// Item is one obj id to take off the card (or row) instead of clearing it. Nil clears the card.
	Item *int `json:"item,omitempty"`
}

// check reports what is wrong with a request, or "" when it can be acted on.
func (r lootClearRequest) check() string {
	switch {
	case r.Group == "":
		return `group is required: the card's key, such as "npc|kill|cow"`
	case len(r.Group) > maxLootKeyBytes || len(r.Part) > maxLootKeyBytes:
		return fmt.Sprintf("group and part must be at most %d bytes", maxLootKeyBytes)
	case !cleanText(r.Group) || !cleanText(r.Part):
		return "group and part must be printable text"
	case r.Item != nil && (*r.Item < 0 || *r.Item > maxLootItemID):
		return fmt.Sprintf("item must be an obj id from 0 to %d", maxLootItemID)
	}
	return ""
}

// matches reports whether a source is on the card (and row) the request names.
func (r lootClearRequest) matches(src loot.Source) bool {
	return src.GroupKey() == r.Group && (r.Part == "" || src.PartKey() == r.Part)
}

// lootClear is one clear that was made, kept in the loot file as the audit of what was removed.
type lootClear struct {
	At    int64  `json:"at"`
	Group string `json:"group"`
	Part  string `json:"part,omitempty"`
	Item  *int   `json:"item,omitempty"`
	// Name is what the card is called on the panel, ItemName the item's name when one item was taken
	// off it. They only make the file readable; nothing matches on them.
	Name     string `json:"name,omitempty"`
	ItemName string `json:"itemName,omitempty"`
}

// lootSwept is what one sweep over a period's per-account records removed.
type lootSwept struct {
	Records  int    // records removed (a card clear) or that lost the item (an item clear)
	Accounts int    // distinct accounts those records belong to
	Events   int64  // events on the removed records; a card clear only
	Units    int64  // item units that went: all of a removed record's, or the one item's count
	Title    string // the card's name
	ItemName string // the item's name, when the clear named one
}

// lootCleared is everything one clear removed, for the log and for telling a no-op from a clear.
type lootCleared struct {
	Lifetime lootSwept
	Session  int // since-restart records removed or trimmed
	Recent   int // ticker rows removed or trimmed
	History  int // history bucket event counts removed
}

// touched is whether the clear removed anything at all.
func (c lootCleared) touched() bool {
	return c.Lifetime.Records > 0 || c.Session > 0 || c.Recent > 0 || c.History > 0
}

// describe is the one log line for a clear. It names no account.
func (c lootCleared) describe(req lootClearRequest) string {
	what := fmt.Sprintf("card %q", req.Group)
	if req.Part != "" {
		what += fmt.Sprintf(" row %q", req.Part)
	}
	if c.Lifetime.Title != "" {
		what += " (" + c.Lifetime.Title + ")"
	}
	if req.Item != nil {
		item := fmt.Sprintf("item %d", *req.Item)
		if c.Lifetime.ItemName != "" {
			item += " (" + c.Lifetime.ItemName + ")"
		}
		return fmt.Sprintf("%s off %s: %d lifetime records on %d accounts lost %d, since restart %d records, %d recent rows",
			item, what, c.Lifetime.Records, c.Lifetime.Accounts, c.Lifetime.Units, c.Session, c.Recent)
	}
	return fmt.Sprintf("%s: %d lifetime records on %d accounts (%d events, %d item units), since restart %d records, %d recent rows, %d history counts",
		what, c.Lifetime.Records, c.Lifetime.Accounts, c.Lifetime.Events, c.Lifetime.Units, c.Session, c.Recent, c.History)
}

// lootCardTitle is what the panel calls a source's card: a skill card by its skill, every random event by
// the one shared card, everything else by its name. Must match cardTitle in web/panels/loot.js.
func lootCardTitle(src loot.Source) string {
	if src.Skill != "" {
		return src.Skill
	}
	if src.Kind == loot.KindRandomEvent {
		return "Random events"
	}
	return src.Name
}

// withoutItem is items without the one obj id. It returns items itself, untouched, when the id is not
// there, and otherwise a NEW slice: a caller may be reading the old one (the recent rows are marshalled
// outside the lock), so it is never edited in place.
func withoutItem(items []loot.Item, id int) (kept []loot.Item, units int64, name string, found bool) {
	n := 0
	for _, it := range items {
		if it.ID == id {
			n++
		}
	}
	if n == 0 {
		return items, 0, "", false
	}
	kept = make([]loot.Item, 0, len(items)-n)
	for _, it := range items {
		if it.ID != id {
			kept = append(kept, it)
			continue
		}
		units += it.Count
		if name == "" {
			name = it.Name
		}
	}
	return kept, units, name, true
}

// sweep removes what the request names from one period's per-account records. A card clear deletes
// every matching record and notes its source key in keys, which is how the history follows; an item
// clear takes the item off each matching record and deletes one the removal left with nothing at all
// (no events, XP, items or spent items), as stripForeignEventItems does. An account left with no
// records is deleted too, since periodView draws a zero row for an empty one.
func (r lootClearRequest) sweep(m map[string]map[string]*lootRec, keys map[string]bool) lootSwept {
	var out lootSwept
	for acct, recs := range m {
		hit := false
		for key, rec := range recs {
			if !r.matches(rec.Source) {
				continue
			}
			if out.Title == "" {
				out.Title = lootCardTitle(rec.Source)
			}
			if r.Item == nil {
				out.Records++
				out.Events += rec.Events
				for _, it := range rec.Items {
					out.Units += it.Count
				}
				keys[key] = true
				keys[rec.Source.Key()] = true
				delete(recs, key)
				hit = true
				continue
			}
			kept, units, name, found := withoutItem(rec.Items, *r.Item)
			if !found {
				continue
			}
			out.Records++
			out.Units += units
			if out.ItemName == "" {
				out.ItemName = name
			}
			rec.Items = kept
			hit = true
			if rec.Events == 0 && rec.XP == 0 && len(rec.Items) == 0 && len(rec.Spent) == 0 && len(rec.Skills) == 0 {
				delete(recs, key)
			}
		}
		if hit {
			out.Accounts++
		}
		if len(recs) == 0 {
			delete(m, acct)
		}
	}
	return out
}

// sweepRecent is the recent-drops rows without what the request names, and how many rows it changed.
// Newest first is kept. A row an item clear leaves with no items is dropped: a row only exists because
// it had some.
func (r lootClearRequest) sweepRecent(recent []LootRecent, keys map[string]bool) ([]LootRecent, int) {
	var kept []LootRecent
	changed := 0
	for _, row := range recent {
		if !r.matches(row.Source) {
			kept = append(kept, row)
			continue
		}
		if r.Item == nil {
			keys[row.Source.Key()] = true
			changed++
			continue
		}
		items, _, _, found := withoutItem(row.Items, *r.Item)
		if !found {
			kept = append(kept, row)
			continue
		}
		changed++
		if len(items) > 0 {
			row.Items = items
			kept = append(kept, row)
		}
	}
	return kept, changed
}

// sweepHistory removes the event counts of the source keys a card clear matched from every history
// bucket, and returns how many it removed. The buckets themselves stay, so the sparklines keep their
// time axis; their per-item gains are fleet-wide and are not touched (see the file comment).
func sweepHistory(history []lootBucket, keys map[string]bool) int {
	n := 0
	for i := range history {
		for k := range keys {
			if _, ok := history[i].Events[k]; ok {
				delete(history[i].Events, k)
				n++
			}
		}
	}
	return n
}

// clear removes what req names from everything this aggregator holds, and records the clear. ms is the
// time it was made. It does not touch seen, which is what stops the fleet's own tallies adding it all
// back (see the file comment).
func (l *lootAggregator) clear(req lootClearRequest, ms int64) lootCleared {
	l.mu.Lock()
	defer l.mu.Unlock()
	keys := map[string]bool{}
	var res lootCleared
	res.Lifetime = req.sweep(l.lifetime, keys)
	res.Session = req.sweep(l.session, keys).Records
	l.recent, res.Recent = req.sweepRecent(l.recent, keys)
	if req.Item == nil {
		res.History = sweepHistory(l.history, keys)
	}
	if !res.touched() {
		return res
	}
	l.cleared = append(l.cleared, lootClear{At: ms, Group: req.Group, Part: req.Part, Item: req.Item, Name: res.Lifetime.Title, ItemName: res.Lifetime.ItemName})
	if len(l.cleared) > maxLootClears {
		l.cleared = append([]lootClear(nil), l.cleared[len(l.cleared)-maxLootClears:]...)
	}
	l.dirty = true
	return res
}

// clearLoot is the whole clear: it first reads whatever the fleet has tallied since the last poll, so
// that goes with it, then removes what req names, then saves the stats file at once so a restart cannot
// bring it back. A request that matched nothing changes nothing and writes nothing.
func (a *statsAggregator) clearLoot(req lootClearRequest) lootCleared {
	now := a.now()
	a.pollLoot(now)
	res := a.loot.clear(req, now.UnixMilli())
	if res.touched() {
		a.save()
	}
	return res
}

// handleLootClear answers POST /api/loot/clear, the Loot panel's "clear this card" and "remove this
// item" controls: {"group": "<card key>"} clears a card, adding "item": <obj id> takes only that item
// off it, and "part": "<row key>" limits either to one row of a skill card. It answers the new
// /api/loot body, so the panel can draw it without asking again; 404 when nothing was recorded for
// what it named (already cleared, say, in another tab).
//
// Protected like every other panel write: the guard checks the access token and refuses a
// cross-origin request, readJSONBody insists on application/json (which a plain HTML form cannot
// send), and the body is decoded strictly, so a misspelt field is an error rather than a request
// to clear the whole card.
func (s *Server) handleLootClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	var raw json.RawMessage
	if !readJSONBody(w, r, maxLootClearBytes, &raw) {
		return
	}
	var req lootClearRequest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "could not parse the request: "+err.Error())
		return
	}
	if msg := req.check(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	res := s.stats.clearLoot(req)
	if !res.touched() {
		if req.Item != nil {
			writeError(w, http.StatusNotFound, "that card holds none of that item (it may already have been removed)")
		} else {
			writeError(w, http.StatusNotFound, "nothing is recorded for that card (it may already have been cleared)")
		}
		return
	}
	log.Printf("controlpanel: loot clear from %s: %s", r.RemoteAddr, res.describe(req))
	writeJSON(w, http.StatusOK, s.stats.lootSnapshot())
}
