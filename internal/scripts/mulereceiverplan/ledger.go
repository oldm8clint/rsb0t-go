package mulereceiverplan

// What a trade brought in or handed out, tallied across a run, and the queue of accounts asking
// for a pickup. Ported from mulePlan.ts's own "what the trades brought in", "the activity log" and
// "the pickup queue" sections.
//
// mulePlan.ts's Collection/ActivityLog/PickupQueue each took an injectable `now: () => number`
// defaulting to Date.now, so a test could control time without touching the wall clock. A
// Go package that must stay free of I/O cannot default to reading the wall clock at all, so every
// mutating method here takes the moment as an explicit time.Time parameter instead — the same
// choice donorplan's Handover already made. mulePlan.ts's own defensive `parseCollectionSnapshot`/
// `parseActivity` (re-validating an `unknown` decoded from JSON.parse field by field) has no
// equivalent here: CollectionSnapshot/ActivityEntry are ordinary typed structs, and a future
// caller reading them back out of persisted state does so with encoding/json against these types
// directly rather than through a hand-rolled validator.

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// ItemCount is one item and how many, by name.
type ItemCount struct {
	Name  string
	Count int
}

// PackTotals is items by name with stacks summed, matching mulePlan.ts's packTotals: a noted stack
// of 5,000 is 5,000, not one slot. An empty Name with a Count of 0 or less is skipped; an empty
// Name with a positive Count is folded into "unknown item".
func PackTotals(items []ItemCount) map[string]int {
	out := map[string]int{}
	for _, item := range items {
		name := item.Name
		if name == "" {
			if item.Count <= 0 {
				continue
			}
			name = "unknown item"
		}
		count := item.Count
		if count < 1 {
			count = 1
		}
		out[name] += count
	}
	return out
}

// GainedItems is every item the pack now holds more of than before, most first, ties by name.
func GainedItems(before, after map[string]int) []ItemCount {
	var out []ItemCount
	for name, count := range after {
		if more := count - before[name]; more > 0 {
			out = append(out, ItemCount{Name: name, Count: more})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// IDCount is one item id and how many, for totals kept by id rather than by name.
type IDCount struct {
	ID    int
	Count int
}

// TotalsByID is the pack by item id, stacks summed. Negative ids (an empty slot) are skipped.
func TotalsByID(items []IDCount) map[int]int {
	out := map[int]int{}
	for _, item := range items {
		if item.ID < 0 {
			continue
		}
		count := item.Count
		if count < 1 {
			count = 1
		}
		out[item.ID] += count
	}
	return out
}

// GainedIDs is the ids the pack holds more of than before — what a withdrawal put there. mulePlan.ts
// iterates its Map in insertion order, which JS guarantees but Go's map does not; this returns ids
// ascending instead, a deterministic order carrying the same set.
func GainedIDs(before, after map[int]int) []int {
	var out []int
	for id, count := range after {
		if count > before[id] {
			out = append(out, id)
		}
	}
	sort.Ints(out)
	return out
}

// DefaultDescribeMax is describeItems' own default cutoff in mulePlan.ts.
const DefaultDescribeMax = 3

// DescribeItems is "5,000 yew logs, 120 coins and 2 more" — for a log line.
func DescribeItems(items []ItemCount, max int) string {
	if max > len(items) {
		max = len(items)
	}
	if max < 0 {
		max = 0
	}
	shown := make([]string, max)
	for i := 0; i < max; i++ {
		shown[i] = commas(items[i].Count) + " " + items[i].Name
	}
	more := len(items) - max
	if more > 0 {
		return strings.Join(shown, ", ") + " and " + commas(more) + " more"
	}
	return strings.Join(shown, ", ")
}

// commas is "5,000" for 5000 — toLocaleString('en-US')'s thousands grouping, the only formatting
// describeItems/describeCollected need.
func commas(n int) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := strconv.Itoa(n)
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

// NamedID is an item id behind a name, for LabelIDs.
type NamedID struct {
	ID   int
	Name string
}

// LabelIDs is the first item id behind each name in a pack — for the icon a tallied item is drawn
// with. The first id seen for a name is kept; a blank name or a negative id is skipped.
func LabelIDs(items []NamedID) map[string]int {
	out := map[string]int{}
	for _, item := range items {
		if item.Name == "" || item.ID < 0 {
			continue
		}
		if _, ok := out[item.Name]; !ok {
			out[item.Name] = item.ID
		}
	}
	return out
}

// ---- the collection tally ----

// CollectedItem is one item's running tally: what crossed, how many trades moved some of it, and
// who it crossed with.
type CollectedItem struct {
	Name     string
	Count    int
	Trades   int
	Accounts []string
	// ID is nil when no trade this tally saw named an id for it.
	ID          *int
	First, Last time.Time
}

// CollectionSnapshot is a tally as plain data, to keep between runs.
type CollectionSnapshot struct {
	// Since is when the tally began: the first run that kept it, or its last reset.
	Since time.Time
	Rows  []CollectedItem
}

type collectionEntry struct {
	name        string
	count       int
	trades      int
	accounts    map[string]bool
	id          *int
	first, last time.Time
}

// Collection is items that crossed in trades this run, by item: what came in, or with a second
// one, what went out. Each keeps who it crossed with.
type Collection struct {
	started time.Time
	byName  map[string]*collectionEntry
}

// NewCollection starts a tally at startedAt.
func NewCollection(startedAt time.Time) *Collection {
	return &Collection{started: startedAt, byName: map[string]*collectionEntry{}}
}

// Add records items crossing at at, optionally with who it crossed with and an id→name map for
// the icon each is first seen with.
func (c *Collection) Add(at time.Time, items []ItemCount, who string, ids map[string]int) {
	for _, item := range items {
		key := strings.ToLower(item.Name)
		entry, ok := c.byName[key]
		if !ok {
			entry = &collectionEntry{name: item.Name, accounts: map[string]bool{}, first: at, last: at}
			c.byName[key] = entry
		}
		entry.count += item.Count
		entry.trades++
		entry.last = at
		if who != "" {
			entry.accounts[who] = true
		}
		if entry.id == nil {
			if id, ok := ids[item.Name]; ok {
				v := id
				entry.id = &v
			}
		}
	}
}

// Since is when this tally began: the first run that kept it, or its last reset.
func (c *Collection) Since() time.Time { return c.started }

// Clear starts the tally over from at.
func (c *Collection) Clear(at time.Time) {
	c.byName = map[string]*collectionEntry{}
	c.started = at
}

// Rows is every item's tally, most first, ties by name, so a table does not reshuffle rows that
// have not changed.
func (c *Collection) Rows() []CollectedItem {
	out := make([]CollectedItem, 0, len(c.byName))
	for _, e := range c.byName {
		var accounts []string
		for a := range e.accounts {
			accounts = append(accounts, a)
		}
		sort.Strings(accounts)
		out = append(out, CollectedItem{Name: e.name, Count: e.count, Trades: e.trades, Accounts: accounts, ID: e.id, First: e.first, Last: e.last})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Snapshot is the tally as plain data, to keep between runs.
func (c *Collection) Snapshot() CollectionSnapshot {
	return CollectionSnapshot{Since: c.started, Rows: c.Rows()}
}

// Restore puts a kept tally back in place of this one.
func (c *Collection) Restore(snap CollectionSnapshot) {
	c.byName = map[string]*collectionEntry{}
	c.started = snap.Since
	for _, r := range snap.Rows {
		accounts := map[string]bool{}
		for _, a := range r.Accounts {
			accounts[a] = true
		}
		c.byName[strings.ToLower(r.Name)] = &collectionEntry{
			name: r.Name, count: r.Count, trades: r.Trades, accounts: accounts, id: r.ID, first: r.First, last: r.Last,
		}
	}
}

// Total is every item's count, summed.
func (c *Collection) Total() int {
	sum := 0
	for _, e := range c.byName {
		sum += e.count
	}
	return sum
}

// Stacks is how many distinct items the tally holds.
func (c *Collection) Stacks() int { return len(c.byName) }

// ---- the activity log ----

// Tally is which direction a trade moved items.
type Tally string

// The two tallies.
const (
	TallyCollected Tally = "collected"
	TallyDelivered Tally = "delivered"
)

// ActivityItem is one item on one activity entry.
type ActivityItem struct {
	Name  string
	Count int
	ID    *int
}

// ActivityEntry is one trade that moved items: when, which way, with whom, and what crossed.
type ActivityEntry struct {
	At    time.Time
	Kind  Tally
	Who   string
	Items []ActivityItem
}

// ActivityMax is the newest trades kept; older ones fall off the end.
const ActivityMax = 200

const activityItemsMax = 20

// ActivityLog is every trade that moved items, newest first, the newest ActivityMax of them.
type ActivityLog struct {
	entries []ActivityEntry
}

// Add records one trade, unless it moved nothing.
func (l *ActivityLog) Add(at time.Time, kind Tally, who string, items []ItemCount, ids map[string]int) {
	if len(items) == 0 {
		return
	}
	if who == "" {
		who = "?"
	}
	n := len(items)
	if n > activityItemsMax {
		n = activityItemsMax
	}
	entryItems := make([]ActivityItem, n)
	for i := 0; i < n; i++ {
		it := ActivityItem{Name: items[i].Name, Count: items[i].Count}
		if id, ok := ids[items[i].Name]; ok {
			v := id
			it.ID = &v
		}
		entryItems[i] = it
	}
	l.entries = append([]ActivityEntry{{At: at, Kind: kind, Who: who, Items: entryItems}}, l.entries...)
	if len(l.entries) > ActivityMax {
		l.entries = l.entries[:ActivityMax]
	}
}

// List is every kept entry, newest first.
func (l *ActivityLog) List() []ActivityEntry {
	out := make([]ActivityEntry, len(l.entries))
	copy(out, l.entries)
	return out
}

// Clear drops one kind's entries, for a tally that was reset.
func (l *ActivityLog) Clear(kind Tally) {
	var kept []ActivityEntry
	for _, e := range l.entries {
		if e.Kind != kind {
			kept = append(kept, e)
		}
	}
	l.entries = kept
}

// Restore puts a kept log back in place of this one, newest ActivityMax kept.
func (l *ActivityLog) Restore(entries []ActivityEntry) {
	if len(entries) > ActivityMax {
		entries = entries[:ActivityMax]
	}
	l.entries = append([]ActivityEntry(nil), entries...)
}

// ---- the pickup queue ----

// PickupRequest is an account waiting for a pickup.
type PickupRequest struct {
	From string
	Tile tile.Tile
	// Slots the donor wants to hand over.
	Slots int
	// FirstAt is when this account first asked — the queue is served in that order.
	FirstAt time.Time
	// LastAt is its latest heartbeat.
	LastAt time.Time
}

// PickupQueue is accounts waiting for a pickup, oldest ask first. A request lives on its
// heartbeat: an account that stops sending falls out on its own, so a request nobody keeps up
// cannot hold the mule's next trip. Heartbeats are remembered apart from the queue, so the donor
// of a trip already under way can still be checked for silence after its request was taken.
type PickupQueue struct {
	ttl   time.Duration
	queue map[string]PickupRequest
	heard map[string]time.Time
}

// NewPickupQueue is an empty queue whose requests expire after ttl without a heartbeat.
func NewPickupQueue(ttl time.Duration) *PickupQueue {
	return &PickupQueue{ttl: ttl, queue: map[string]PickupRequest{}, heard: map[string]time.Time{}}
}

func pickupKey(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// Offer adds or refreshes a request at at. The first ask keeps its place; the tile and slots
// update.
func (q *PickupQueue) Offer(at time.Time, from string, t tile.Tile, slots int) {
	key := pickupKey(from)
	q.heard[key] = at
	firstAt := at
	if prior, ok := q.queue[key]; ok {
		firstAt = prior.FirstAt
	}
	q.queue[key] = PickupRequest{From: from, Tile: t, Slots: slots, FirstAt: firstAt, LastAt: at}
}

func (q *PickupQueue) sweep(now time.Time) {
	cutoff := now.Add(-q.ttl)
	for key, req := range q.queue {
		if !req.LastAt.After(cutoff) {
			delete(q.queue, key)
		}
	}
}

// Heartbeat notes a heartbeat at at without queueing anything — for the donor of a trip under way.
func (q *PickupQueue) Heartbeat(at time.Time, from string) {
	q.heard[pickupKey(from)] = at
}

// Pending is every live request as of now, earliest ask first.
func (q *PickupQueue) Pending(now time.Time) []PickupRequest {
	q.sweep(now)
	out := make([]PickupRequest, 0, len(q.queue))
	for _, r := range q.queue {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FirstAt.Before(out[j].FirstAt) })
	return out
}

// Next is the request to serve next, still queued; ok is false when the queue is empty.
func (q *PickupQueue) Next(now time.Time) (PickupRequest, bool) {
	pending := q.Pending(now)
	if len(pending) == 0 {
		return PickupRequest{}, false
	}
	return pending[0], true
}

// Take removes a request, for a trip started or refused.
func (q *PickupQueue) Take(from string) (PickupRequest, bool) {
	key := pickupKey(from)
	req, ok := q.queue[key]
	delete(q.queue, key)
	return req, ok
}

// Drop removes a request without returning it.
func (q *PickupQueue) Drop(from string) {
	delete(q.queue, pickupKey(from))
}

// LastHeard is when this account last sent anything; ok is false if it never has.
func (q *PickupQueue) LastHeard(from string) (time.Time, bool) {
	t, ok := q.heard[pickupKey(from)]
	return t, ok
}

// Size is how many live requests are queued as of now.
func (q *PickupQueue) Size(now time.Time) int {
	q.sweep(now)
	return len(q.queue)
}
