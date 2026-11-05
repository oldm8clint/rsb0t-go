package controlpanel

// Loot run statistics: what every loot source (an NPC killed or pickpocketed, a casket or
// oyster opened) actually yielded, fleet-wide, lifetime and since this restart — the owner's
// 2026-09-19 ask to see "the loot that Big net fishing brings, especially what comes out of
// caskets and oysters", widened the same day to every source a script reports.
//
// Where it comes from: scripts report each measured drop through bot.Context.Loot
// (internal/loot); internal/fleet tallies them per account for this process's lifetime and
// answers LootSource.LootTotals. This aggregator polls that on the stats ticker (statsAggregator
// .poll), diffs each account's totals against the last reading it saw IN THIS PROCESS, and adds
// the difference to its persisted lifetime record and its in-memory since-restart record. The
// fleet's tallies start at zero on every restart exactly as this aggregator's last readings do,
// so nothing is counted twice across a restart and nothing earned between two polls is lost.
//
// Persistence rides the existing stats file (statsFile.Loot, omitted when empty), so an older
// file without the field still loads and simply starts loot at zero.
//
// What was recorded wrongly can be cleared, a card or one item off it, from the panel
// (POST /api/loot/clear): lootclear.go says what a clear removes and why nothing comes back.
//
// Deliberately kept out of AccountStatus: /api/status is sampled every second for every panel,
// and loot changes a few times a minute, so it has its own endpoint (/api/loot) the loot panel
// polls on its own slow cadence.

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/deathloot"
	"rsb0t-go/internal/fleet"
	"rsb0t-go/internal/loot"
)

// LootSource is optional: a Source that reports per-account loot totals (since the process
// started) feeds the loot panel. internal/fleet via FleetSource and the fake fleet both do.
type LootSource interface {
	LootTotals() []fleet.AccountLoot
}

// LootTotals answers LootSource from the fleet manager's tallies.
func (f *FleetSource) LootTotals() []fleet.AccountLoot {
	if f.manager == nil {
		return nil
	}
	return f.manager.LootTotals()
}

const (
	// lootBucketMs is the width of one history bucket (the panel's sparklines), and
	// maxLootBuckets how many are kept: 10 minutes x 144 = the last 24 hours.
	lootBucketMs   = int64(10 * time.Minute / time.Millisecond)
	maxLootBuckets = 144
	// maxLootRecent bounds the recent-drops ticker.
	maxLootRecent = 80
	// maxLootNotable is how many older records with a notable item the ring keeps past maxLootRecent.
	maxLootNotable = 40
)

// lootRec is one account's accumulated record for one source.
type lootRec struct {
	Source loot.Source `json:"source"`
	Events int64       `json:"events"`
	Items  []loot.Item `json:"items"`
	First  int64       `json:"first"`
	Last   int64       `json:"last"`
	// XP is the experience the source earned (loot.Drop.XP): a bone buried. Omitted when zero, so
	// a file written by this build still loads in an older one, which simply ignores the field.
	XP int64 `json:"xp,omitempty"`
	// Spent is what the source's events measurably used up (loot.Drop.Spent: the runes cast at
	// an npc killed with Magic), netted off its value like an input. Omitted when empty.
	Spent []loot.Item `json:"spent,omitempty"`
	// Skills is the experience the source's events earned, per skill (loot.Drop.Skills: an npc's
	// kills pay Attack, Strength, Defence, Ranged, Magic and Hitpoints). Omitted when empty, so an
	// older build reading a file this one wrote ignores it.
	Skills []loot.Item `json:"skills,omitempty"`
}

func (r *lootRec) add(events int64, items []loot.Item, xp int64, ms int64) {
	if r.First == 0 {
		r.First = ms
	}
	r.Last = ms
	r.Events += events
	r.XP += xp
	r.addItems(items)
}

// addSkills merges experience by skill into the record, by stat index.
func (r *lootRec) addSkills(items []loot.Item) {
	for _, it := range items {
		if it.Count <= 0 {
			continue
		}
		found := false
		for i := range r.Skills {
			if r.Skills[i].ID == it.ID {
				r.Skills[i].Count += it.Count
				found = true
				break
			}
		}
		if !found {
			r.Skills = append(r.Skills, it)
		}
	}
	sort.Slice(r.Skills, func(i, j int) bool { return r.Skills[i].ID < r.Skills[j].ID })
}

// addSpent merges spent items into the record, by id.
func (r *lootRec) addSpent(items []loot.Item) {
	for _, it := range items {
		if it.Count <= 0 {
			continue
		}
		found := false
		for i := range r.Spent {
			if r.Spent[i].ID == it.ID {
				r.Spent[i].Count += it.Count
				found = true
				break
			}
		}
		if !found {
			r.Spent = append(r.Spent, it)
		}
	}
	sort.Slice(r.Spent, func(i, j int) bool { return r.Spent[i].ID < r.Spent[j].ID })
}

// addItems merges items into the record's sorted item list, by id.
func (r *lootRec) addItems(items []loot.Item) {
	for _, it := range items {
		i := sort.Search(len(r.Items), func(i int) bool { return r.Items[i].ID >= it.ID })
		if i < len(r.Items) && r.Items[i].ID == it.ID {
			r.Items[i].Count += it.Count
			if r.Items[i].Name == "" {
				r.Items[i].Name = it.Name
			}
			if it.Value > 0 {
				r.Items[i].Value = it.Value
			}
			continue
		}
		r.Items = append(r.Items, loot.Item{})
		copy(r.Items[i+1:], r.Items[i:])
		r.Items[i] = it
	}
}

// LootRecent is one poll's worth of new loot for one account and source: the recent-drops
// ticker. At is the poll that saw it, so it is accurate to statsInterval, not to the drop.
type LootRecent struct {
	At      int64       `json:"at"`
	Account string      `json:"account"`
	Source  loot.Source `json:"source"`
	Events  int64       `json:"events"`
	Items   []loot.Item `json:"items"`
	// Notable is the ids of the items worth the Notable drops ticker (loot.IsNotable at the market price
	// when it was recorded). A record from before the field has none and falls back to the panel's list.
	Notable []int `json:"notable,omitempty"`
}

// notableIDs is the ids among items that loot.IsNotable accepts at the market price.
func notableIDs(prices *PriceBook, items []loot.Item) []int {
	var out []int
	for _, it := range items {
		if loot.IsNotable(it.ID, lootUnit(prices, it), it.Count) {
			out = append(out, it.ID)
		}
	}
	return out
}

// trimRecent keeps the newest maxLootRecent records, and past them the newest records that carry a
// notable item (up to maxLootNotable more), so a rare drop is not pushed off the ring in minutes by the
// fleet's ordinary pickups.
func trimRecent(recent []LootRecent) []LootRecent {
	if len(recent) <= maxLootRecent {
		return recent
	}
	out := recent[:maxLootRecent]
	extra := 0
	for _, r := range recent[maxLootRecent:] {
		if len(r.Notable) > 0 && extra < maxLootNotable {
			out = append(out, r)
			extra++
		}
	}
	return out
}

// lootBucket is the fleet's gains in one history bucket: per item id, and per source key.
type lootBucket struct {
	Start  int64            `json:"start"`
	Items  map[int]int64    `json:"items"`
	Events map[string]int64 `json:"events"`
}

// lootFile is what the stats file persists.
type lootFile struct {
	Accounts map[string]map[string]*lootRec `json:"accounts"`
	Recent   []LootRecent                   `json:"recent,omitempty"`
	History  []lootBucket                   `json:"history,omitempty"`
	// Cleared is the audit of every clear the owner made (lootclear.go): which card or item, and when.
	// The file's accounts, recent and history are already the cleared state, so nothing re-reads this
	// to remove anything; it is here so the file can say what was taken out. Omitted when empty, and an
	// older build that does not know the field simply ignores it.
	Cleared []lootClear `json:"cleared,omitempty"`
	// Deaths is the Deaths view's record (lootdeaths.go): every death and the pickups matched to it, oldest first. Omitted when empty.
	Deaths []deathloot.Death `json:"deaths,omitempty"`
}

// lootSeen is the last per-process totals this aggregator saw for one account and source.
type lootSeen struct {
	events int64
	xp     int64
	items  map[int]int64
	spent  map[int]int64
	skills map[int]int64
}

type lootAggregator struct {
	mu       sync.Mutex
	lifetime map[string]map[string]*lootRec // persisted
	session  map[string]map[string]*lootRec // this process only
	seen     map[string]map[string]*lootSeen
	epochs   map[string]int64
	recent   []LootRecent      // newest first
	history  []lootBucket      // oldest first
	cleared  []lootClear       // oldest first, at most maxLootClears (lootclear.go)
	deaths   []deathloot.Death // oldest first, persisted (lootdeaths.go)
	// sessionStartMs is the process start, so the since-restart period deducts only the deaths since then.
	sessionStartMs int64
	dirty          bool
	// prices values a drop at what it actually trades for, falling back to high alchemy and
	// then to the cache's own unit cost. Nil switches the market lookup off and the fallbacks
	// still apply, so nothing here needs a special case for it (see PriceBook.Value).
	prices *PriceBook
}

func newLootAggregator() *lootAggregator {
	return &lootAggregator{
		lifetime: map[string]map[string]*lootRec{},
		session:  map[string]map[string]*lootRec{},
		seen:     map[string]map[string]*lootSeen{},
		epochs:   map[string]int64{},
	}
}

func (l *lootAggregator) restore(f *lootFile) {
	if f == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for acct, sources := range f.Accounts {
		if sources == nil {
			continue
		}
		clean := map[string]*lootRec{}
		for key, rec := range sources {
			if rec == nil {
				continue
			}
			// A record written by an older build can carry a Source shape this one has
			// retired (loot.Migrate says which, and why the key is unaffected). Migrating on
			// the way in is what keeps a lifetime total on the card it belongs to instead of
			// stranding it on a second card forever.
			rec.Source = loot.Migrate(rec.Source)
			// A later bite (2/3 cake, Slice of cake) recorded by an older build: the Cake row costs the whole cake.
			if rec.Source.Kind == loot.KindConsumable && loot.LaterBite(rec.Source.Name) {
				continue
			}
			sort.Slice(rec.Items, func(i, j int) bool { return rec.Items[i].ID < rec.Items[j].ID })
			if rec.Source.Kind == loot.KindThieving {
				rec.Items = withoutEatenLeftovers(rec.Items)
			}
			// The closed Oyster and the Empty oyster are never shown (loot.HiddenItem).
			rec.Items = loot.WithoutHiddenItems(rec.Items)
			clean[key] = rec
		}
		l.lifetime[acct] = clean
	}
	// Moved, not rewritten: see foldLegacyGroundPickups.
	folded := foldLegacyGroundPickups(l.lifetime)
	// A random event recorded under an id that is not its own moves onto the right source, and an
	// event's own item comes off every source that is not that event (lootevents.go says why).
	events := foldRetiredEventKeys(l.lifetime)
	stripForeignEventItems(l.lifetime)
	// What an older build filed on a kill of an npc that drops nothing moves to pickup cards (lootpickupfold.go).
	unfileNoDropKills(l.lifetime)
	// A rune or a bone recorded as a pickup of its own moves under the kill that left it
	// (lootpickupfold.go).
	pickupKeys, pickupKills := foldRunePickups(l.lifetime)
	l.recent = f.Recent
	for i := range l.recent {
		l.recent[i].Source = loot.Migrate(l.recent[i].Source)
		if loot.IsLegacyGroundPickup(l.recent[i].Source) {
			l.recent[i].Source = folded.target(l.recent[i].Account)
		}
		if rs := l.recent[i].Source; rs.Kind == loot.KindPickup && loot.FoldsIntoKill(rs.ID) {
			if kill, ok := pickupKills[l.recent[i].Account]; ok {
				l.recent[i].Source = kill
				l.recent[i].Events = 0
			}
		}
		l.recent[i].Items = loot.WithoutHiddenItems(loot.WithoutForeignEventItems(l.recent[i].Source, l.recent[i].Items))
	}
	kept := l.recent[:0]
	for _, r := range l.recent {
		if r.Source.Kind == loot.KindConsumable && loot.LaterBite(r.Source.Name) {
			continue
		}
		kept = append(kept, r)
	}
	l.recent = kept
	l.recent = trimRecent(l.recent)
	l.history = f.History
	for i := range l.history {
		folded.moveEvents(l.history[i].Events)
		events.moveEvents(l.history[i].Events)
		for key := range pickupKeys {
			delete(l.history[i].Events, key)
		}
	}
	if len(l.history) > maxLootBuckets {
		l.history = l.history[len(l.history)-maxLootBuckets:]
	}
	l.cleared = f.Cleared
	if len(l.cleared) > maxLootClears {
		l.cleared = l.cleared[len(l.cleared)-maxLootClears:]
	}
	l.deaths = deathloot.Merge(nil, f.Deaths, 0)
}

// withoutEatenLeftovers drops the partly eaten foods a thieving record picked up when its account
// ate inside the take's measurement window (loot.IsEatenLeftover says why none of them can ever be
// a real pickpocket or stall yield). Idempotent. The live Guard record held four such items.
func withoutEatenLeftovers(items []loot.Item) []loot.Item {
	out := items[:0]
	for _, it := range items {
		if !loot.IsEatenLeftover(it.ID) {
			out = append(out, it)
		}
	}
	return out
}

// groundFold is what foldLegacyGroundPickups moved, so the recent ticker and the history buckets
// can follow their records.
type groundFold struct {
	// by is, per account, the kill source that account's Ground pickups were folded onto.
	by map[string]loot.Source
	// keys maps an old history event key to the kill key it now counts under. Every account's
	// Ground record had the same key, so this has at most one entry.
	keys map[string]string
}

// target is the kill source acct's legacy Ground pickups now belong to.
func (g groundFold) target(acct string) loot.Source {
	if src, ok := g.by[acct]; ok {
		return src
	}
	return loot.LegacyGroundKill
}

// moveEvents re-keys one history bucket's event counts to follow the fold.
func (g groundFold) moveEvents(events map[string]int64) {
	for from, to := range g.keys {
		if n, ok := events[from]; ok {
			delete(events, from)
			if n != 0 {
				events[to] += n
			}
		}
	}
}

// foldLegacyGroundPickups moves every legacy "Ground" pickup record onto the kill that dropped it.
//
// THE COW CARD'S MISSING DROPS (owner, 2026-09-23: "cows show no loot even though theres kills").
// Live /api/loot had kill:cow:81 at 97 events with no items and open:ground:-1 at ZERO events with
// Bones x106, on the same seven accounts: ScarabSwarm recorded the bones it took after each cow
// kill under a generic openable instead of the cow (loot.IsLegacyGroundPickup has the history).
// The recorder is fixed; this carries what it already wrote.
//
// Unlike loot.Migrate this cannot rewrite a Source in place, because the right home is a
// DIFFERENT record under a different key. Each account's Ground record is merged into that
// account's own Cow kill record — the one with the most events, when there is more than one — or,
// when it has none, into a new kill:cow:81 record, since the Ground openable's only producer ran
// only after Cow kills. Events add (the Ground records carry zero, so kill counts are unchanged),
// items add by id, and First/Last widen to the span of both. The old key is deleted, so a second
// restore finds nothing to do: idempotent.
func foldLegacyGroundPickups(lifetime map[string]map[string]*lootRec) groundFold {
	fold := groundFold{by: map[string]loot.Source{}, keys: map[string]string{}}
	for acct, sources := range lifetime {
		for key, rec := range sources {
			if !loot.IsLegacyGroundPickup(rec.Source) {
				continue
			}
			var target *lootRec
			for _, cand := range sources {
				if cand.Source.Kind != loot.KindNPC || cand.Source.Action != loot.ActionKill ||
					!strings.EqualFold(strings.TrimSpace(cand.Source.Name), loot.LegacyGroundKill.Name) {
					continue
				}
				if target == nil || cand.Events > target.Events {
					target = cand
				}
			}
			if target == nil {
				target = &lootRec{Source: loot.LegacyGroundKill}
				sources[loot.LegacyGroundKill.Key()] = target
			}
			target.Events += rec.Events
			target.XP += rec.XP
			target.addItems(rec.Items)
			if rec.First > 0 && (target.First == 0 || rec.First < target.First) {
				target.First = rec.First
			}
			if rec.Last > target.Last {
				target.Last = rec.Last
			}
			delete(sources, key)
			fold.by[acct] = target.Source
			fold.keys[key] = target.Source.Key()
		}
	}
	return fold
}

// persisted is a deep enough copy of the persisted state to marshal outside the lock; nil when
// there is nothing to write, so an old-shaped file stays old-shaped.
func (l *lootAggregator) persisted() *lootFile {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.lifetime) == 0 && len(l.recent) == 0 && len(l.history) == 0 && len(l.cleared) == 0 && len(l.deaths) == 0 {
		return nil
	}
	out := &lootFile{Accounts: make(map[string]map[string]*lootRec, len(l.lifetime))}
	for acct, sources := range l.lifetime {
		cp := make(map[string]*lootRec, len(sources))
		for key, rec := range sources {
			r := *rec
			r.Items = append([]loot.Item(nil), rec.Items...)
			r.Spent = append([]loot.Item(nil), rec.Spent...)
			r.Skills = append([]loot.Item(nil), rec.Skills...)
			cp[key] = &r
		}
		out.Accounts[acct] = cp
	}
	out.Recent = append([]LootRecent(nil), l.recent...)
	for _, b := range l.history {
		cb := lootBucket{Start: b.Start, Items: make(map[int]int64, len(b.Items)), Events: make(map[string]int64, len(b.Events))}
		for k, v := range b.Items {
			cb.Items[k] = v
		}
		for k, v := range b.Events {
			cb.Events[k] = v
		}
		out.History = append(out.History, cb)
	}
	out.Cleared = append([]lootClear(nil), l.cleared...)
	out.Deaths = append([]deathloot.Death(nil), l.deaths...)
	return out
}

func (l *lootAggregator) takeDirty() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	d := l.dirty
	l.dirty = false
	return d
}

func recFor(m map[string]map[string]*lootRec, acct string, src loot.Source) *lootRec {
	sources := m[acct]
	if sources == nil {
		sources = map[string]*lootRec{}
		m[acct] = sources
	}
	key := src.Key()
	rec := sources[key]
	if rec == nil {
		rec = &lootRec{Source: src}
		sources[key] = rec
	}
	if src.Level > 0 {
		rec.Source.Level = src.Level
	}
	return rec
}

// record folds one LootTotals reading in. Only the difference from this process's previous
// reading of each account/source is added; a count that went DOWN (a tally the fleet replaced)
// is taken as a fresh start and added whole.
func (l *lootAggregator) record(all []fleet.AccountLoot, now time.Time) {
	ms := now.UnixMilli()
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, acct := range all {
		if last, ok := l.epochs[acct.Account]; ok && last == acct.Epoch {
			continue
		}
		l.epochs[acct.Account] = acct.Epoch
		seenBy := l.seen[acct.Account]
		if seenBy == nil {
			seenBy = map[string]*lootSeen{}
			l.seen[acct.Account] = seenBy
		}
		for _, tot := range acct.Sources {
			// Cheap and key-preserving, and it means a record this build receives in an
			// older shape (a tally created before a rollout) still lands on today's card.
			tot.Source = loot.Migrate(tot.Source)
			key := tot.Source.Key()
			seen := seenBy[key]
			if seen == nil {
				seen = &lootSeen{items: map[int]int64{}, spent: map[int]int64{}, skills: map[int]int64{}}
				seenBy[key] = seen
			}
			dEvents := tot.Events - seen.events
			if dEvents < 0 {
				dEvents = tot.Events
			}
			dXP := tot.XP - seen.xp
			if dXP < 0 {
				dXP = tot.XP
			}
			seen.xp = tot.XP
			var dItems []loot.Item
			for _, it := range tot.Items {
				d := it.Count - seen.items[it.ID]
				if d < 0 {
					d = it.Count
				}
				seen.items[it.ID] = it.Count
				if d > 0 {
					cp := it
					cp.Count = d
					dItems = append(dItems, cp)
				}
			}
			var dSpent []loot.Item
			for _, it := range tot.Spent {
				d := it.Count - seen.spent[it.ID]
				if d < 0 {
					d = it.Count
				}
				seen.spent[it.ID] = it.Count
				if d > 0 {
					cp := it
					cp.Count = d
					dSpent = append(dSpent, cp)
				}
			}
			var dSkills []loot.Item
			for _, it := range tot.Skills {
				d := it.Count - seen.skills[it.ID]
				if d < 0 {
					d = it.Count
				}
				seen.skills[it.ID] = it.Count
				if d > 0 {
					cp := it
					cp.Count = d
					dSkills = append(dSkills, cp)
				}
			}
			seen.events = tot.Events
			if dEvents == 0 && len(dItems) == 0 && dXP == 0 && len(dSpent) == 0 && len(dSkills) == 0 {
				continue
			}
			recFor(l.lifetime, acct.Account, tot.Source).add(dEvents, dItems, dXP, ms)
			recFor(l.session, acct.Account, tot.Source).add(dEvents, dItems, dXP, ms)
			if len(dSpent) > 0 {
				recFor(l.lifetime, acct.Account, tot.Source).addSpent(dSpent)
				recFor(l.session, acct.Account, tot.Source).addSpent(dSpent)
			}
			if len(dSkills) > 0 {
				recFor(l.lifetime, acct.Account, tot.Source).addSkills(dSkills)
				recFor(l.session, acct.Account, tot.Source).addSkills(dSkills)
			}
			if len(dItems) > 0 {
				l.recent = append([]LootRecent{{At: ms, Account: acct.Account, Source: tot.Source, Events: dEvents, Items: dItems, Notable: notableIDs(l.prices, dItems)}}, l.recent...)
				l.recent = trimRecent(l.recent)
			}
			b := l.bucketLocked(ms)
			b.Events[key] += dEvents
			for _, it := range dItems {
				b.Items[it.ID] += it.Count
			}
			l.dirty = true
		}
	}
}

func (l *lootAggregator) bucketLocked(ms int64) *lootBucket {
	start := ms - ms%lootBucketMs
	if n := len(l.history); n > 0 && l.history[n-1].Start == start {
		return &l.history[n-1]
	}
	l.history = append(l.history, lootBucket{Start: start, Items: map[int]int64{}, Events: map[string]int64{}})
	if len(l.history) > maxLootBuckets {
		l.history = l.history[len(l.history)-maxLootBuckets:]
	}
	return &l.history[len(l.history)-1]
}

// LootSourceView is one source's fleet-wide totals for one period.
type LootSourceView struct {
	Key      string      `json:"key"`
	Source   loot.Source `json:"source"`
	Events   int64       `json:"events"`
	Items    []loot.Item `json:"items"`
	First    int64       `json:"first"`
	Last     int64       `json:"last"`
	Accounts int         `json:"accounts"`
	// GroupAccounts is how many DISTINCT accounts looted anything in this source's card group
	// (loot.Source.GroupKey — every source the panel merges into one card). It is the same as
	// Accounts for a source that is a card on its own.
	//
	// The panel cannot work this out for itself: it only ever sees per-source counts, so it took
	// the maximum across the group, which undercounts whenever no single source was used by
	// every account. The Fishing spot card read 154 against 269 logged in (owner, 2026-09-20)
	// because each account fishes whichever spot it is standing at.
	GroupAccounts int `json:"groupAccounts"`
	// PartAccounts is the same union for this source's ROW inside its card (loot.Source.PartKey):
	// every Man id the Thieving card folds into its one "Man" row, every Tree on the Woodcutting
	// card. The card's per-target breakdown shows it; it equals Accounts for a row of one id.
	PartAccounts int `json:"partAccounts"`
	// XP is the experience this source earned (a bone buried, a log cut, a theft), 0 when none was
	// measured.
	XP int64 `json:"xp,omitempty"`
	// Skills is the experience an npc's kills earned, per skill (loot.Drop.Skills), for its XP per hour.
	Skills []loot.Item `json:"skills,omitempty"`

	// What this source CONSUMED, so the card can show profit rather than turnover (owner,
	// 2026-09-21: "loot panel should consider the cost of a log ... when its putting a profit
	// price", and the same for the crystal key a chest open costs). loot.Source.Consumes decides
	// which sources have one and why; everything here is zero/empty for a source that consumes
	// nothing, which is most of them.
	//
	// CostItem/CostUnits are filled in even when the price is unknown, which is the case this
	// deliberately does NOT round to zero: a Crystal key's own market record can fail the
	// PriceBook's Coherent check, and "the input was free" is a claim, not a default. CostKnown
	// false means Cost is 0 because nobody knows, not because it cost nothing — the panel says
	// so rather than quietly showing a gross figure as if it were net.
	CostItem string `json:"costItem,omitempty"`
	// Spent is what the source's events measurably used up (loot.Drop.Spent): the runes cast at
	// an npc killed with Magic, by rune. Its price is in Cost.
	Spent     []loot.Item `json:"spent,omitempty"`
	CostUnits int64       `json:"costUnits,omitempty"`
	Cost      int64       `json:"cost,omitempty"`
	CostKnown bool        `json:"costKnown,omitempty"`
	// KillsPerHour, RuneValue and RuneGpPerHour are an npc kill source's kills an hour, the value of the runes its kills dropped and that
	// value an hour, over the card's own window (lootrates.go). Zero for any other source, and the rates zero until the window is long enough.
	KillsPerHour  float64 `json:"killsPerHour,omitempty"`
	RuneValue     int64   `json:"runeValue,omitempty"`
	RuneGpPerHour float64 `json:"runeGpPerHour,omitempty"`
}

// LootAccountView is one account's share of one period, for the per-account breakdown.
//
// Value is NET of Cost since 2026-09-21 (see LootSourceView's Cost fields): what the account
// earned, not what passed through its pack. Gross is the old figure, kept so the two can be
// read against each other rather than the change being invisible.
type LootAccountView struct {
	Account string `json:"account"`
	Events  int64  `json:"events"`
	Items   int64  `json:"items"`
	Value   int64  `json:"value"`
	Gross   int64  `json:"gross,omitempty"`
	Cost    int64  `json:"cost,omitempty"`
	Coins   int64  `json:"coins"`
	Last    int64  `json:"last"`
}

// LootPeriod is the whole fleet's loot for one period (lifetime or since restart).
type LootPeriod struct {
	Sources []LootSourceView `json:"sources"`
	// RuneNPCs ranks the npcs by the value of the runes their kills dropped an hour (lootrates.go).
	RuneNPCs []RuneNPC         `json:"runeNpcs,omitempty"`
	Accounts []LootAccountView `json:"accounts"`
	Items    int64             `json:"items"`
	Coins    int64             `json:"coins"`
	// Value is NET: Gross minus every Cost whose input has a trustworthy price. Gross and Cost
	// are carried beside it so the headline figure can be audited and so a build that nets
	// nothing is distinguishable from one that had nothing to net.
	Value int64 `json:"value"`
	Gross int64 `json:"gross"`
	Cost  int64 `json:"cost"`
	// Unnetted is the part of Gross that sits on rows whose consumed input has no price the panel can
	// stand behind (LootSourceView.CostKnown false), so Value is gross for those rows. Unknown is not
	// zero: this is how much of the headline is still unnetted, rather than leaving it to be guessed from
	// the cards that say GROSS.
	Unnetted int64 `json:"unnetted,omitempty"`
	// Events is per action: kills, pickpockets, opens, and per openable name
	// ("open:Casket") so the header can say how many caskets and oysters were opened.
	Events map[string]int64 `json:"events"`
	// XP is the experience earned per skill (by Source.Skill): Prayer from burying today.
	XP map[string]int64 `json:"xp,omitempty"`
	// Runecraft is the Runecrafting section's figures (lootrunecraft.go), omitted when the period holds nothing of it.
	Runecraft *RunecraftPeriod `json:"runecraft,omitempty"`
	// Deaths is the Deaths view's figures (lootdeaths.go), omitted when the period holds no death.
	Deaths *DeathsPeriod `json:"deaths,omitempty"`
}

// LootHistoryBucket is one sparkline bucket: fleet gains per item id and per source key.
type LootHistoryBucket struct {
	Start  int64            `json:"start"`
	Items  map[string]int64 `json:"items"`
	Events map[string]int64 `json:"events"`
}

// LootBody answers GET /api/loot.
type LootBody struct {
	Available     bool                `json:"available"`
	NowUnixMs     int64               `json:"now"`
	TrackingSince int64               `json:"trackingSince"`
	ProcessStart  int64               `json:"processStart"`
	BucketMs      int64               `json:"bucketMs"`
	Lifetime      LootPeriod          `json:"lifetime"`
	SinceRestart  LootPeriod          `json:"sinceRestart"`
	Recent        []LootRecent        `json:"recent"`
	History       []LootHistoryBucket `json:"history"`
	// EssenceStock is the Rune essence the mules' cached banks last showed, for the Runecrafting section; omitted
	// when it cannot be read (lootrunecraft.go).
	EssenceStock []EssenceStock `json:"essenceStock,omitempty"`
}

// coinsID is the cache's Coins obj.
const coinsID = 995

// periodView builds one period's view. lost is what deaths took for good, by account and item
// (deathLossesBy), deducted from the account's own records (deductLost); nil deducts nothing.
func periodView(m map[string]map[string]*lootRec, prices *PriceBook, credited creditBook, lost map[string]map[int]int64) LootPeriod {
	p := LootPeriod{Events: map[string]int64{}}
	type agg struct {
		view   LootSourceView
		items  map[int]*loot.Item
		spent  map[int]*loot.Item
		skills map[int]*loot.Item
	}
	by := map[string]*agg{}
	// Distinct accounts per CARD group, which is what the panel shows and cannot derive from the
	// per-source counts. See LootSourceView.GroupAccounts.
	groupAccounts := map[string]map[string]bool{}
	partAccounts := map[string]map[string]bool{}
	accounts := make([]string, 0, len(m))
	for acct := range m {
		accounts = append(accounts, acct)
	}
	sort.Strings(accounts)
	for _, acct := range accounts {
		av := LootAccountView{Account: acct}
		keys := make([]string, 0, len(m[acct]))
		for key := range m[acct] {
			keys = append(keys, key)
		}
		sort.Strings(keys) // a deduction is drawn off the account's rows in a fixed order
		var acctLost map[int]int64
		if lost != nil {
			acctLost = map[int]int64{}
			for id, n := range lost[acct] {
				acctLost[id] = n
			}
		}
		for _, key := range keys {
			rec := m[acct][key]
			a := by[key]
			if a == nil {
				a = &agg{view: LootSourceView{Key: key, Source: rec.Source, First: rec.First}, items: map[int]*loot.Item{}, spent: map[int]*loot.Item{}, skills: map[int]*loot.Item{}}
				by[key] = a
			}
			if rec.Source.Level > a.view.Source.Level {
				a.view.Source.Level = rec.Source.Level
			}
			a.view.Events += rec.Events
			a.view.XP += rec.XP
			a.view.Accounts++
			gk := rec.Source.GroupKey()
			if groupAccounts[gk] == nil {
				groupAccounts[gk] = map[string]bool{}
			}
			groupAccounts[gk][acct] = true
			pk := rec.Source.PartKey()
			if partAccounts[pk] == nil {
				partAccounts[pk] = map[string]bool{}
			}
			partAccounts[pk][acct] = true
			if rec.First > 0 && (a.view.First == 0 || rec.First < a.view.First) {
				a.view.First = rec.First
			}
			if rec.Last > a.view.Last {
				a.view.Last = rec.Last
			}
			mirror := rec.Source.MirrorsCost()
			if !mirror && rec.Source.Kind != loot.KindSupply {
				// The Combat spells row repeats its npcs' kills and bill; counting it too would count
				// every kill, and every rune, twice. The Essence supply card's events are the essence that
				// crossed to the mule (a count of things moved, not of times the account did something), so
				// they are not added to the account's kills/opens either; the card and the Runecrafting
				// section carry them.
				av.Events += rec.Events
			}
			if rec.Last > av.Last {
				av.Last = rec.Last
			}
			for _, it := range deductLost(rec.Items, acctLost) {
				cur := a.items[it.ID]
				if cur == nil {
					cp := it
					cp.Count = 0
					cp.Raw = 0
					cur = &cp
					a.items[it.ID] = cur
				}
				cur.Count += it.Count
				if it.Raw > 0 {
					cur.Raw += it.Raw
				} else {
					cur.Raw += it.Count
				}
				// What it is WORTH, not what the cache says it costs. loot.Item.Value is the
				// cache's unit cost -- the shop price the content ships with, which on this
				// server has little to do with what a thing trades for. Users asked for
				// real market values, so the order is the market's own median traded price,
				// then high alchemy, then that cost.
				unit := lootUnit(prices, it)
				av.Value += it.Count * unit
				// Coins are counted ONLY as coins, never also as items. They have their own
				// tile in the panel, and counting them twice made the item total meaningless:
				// measured live 2026-09-20, "items looted" read 3,683,761 of which 3,580,168
				// were coins, so 97% of the headline figure was the number already shown in
				// the tile beside it, and the 103,593 things actually picked up were invisible.
				// Value still includes coins, because a coin is worth a coin and the value
				// total is explicitly "what it is all worth".
				if it.ID == coinsID {
					av.Coins += it.Count
				} else {
					av.Items += it.Count
				}
			}
			// And what this source CONSUMED to produce that, so the account's figure is what it
			// EARNED rather than what passed through its pack (owner, 2026-09-21). A cost whose
			// input the PriceBook cannot price is left out of the subtraction entirely — see
			// sourceCost, and LootSourceView's own Cost comment for why zero is not the answer.
			av.Cost += sourceCost(prices, rec.Source, rec.Events, credited)
			// And what its events measurably used up (the runes cast at a Magic kill).
			if c, known := spentCost(prices, rec.Source, rec.Spent, credited); known && !mirror {
				av.Cost += c
			}
			for _, it := range rec.Spent {
				cur := a.spent[it.ID]
				if cur == nil {
					cp := it
					cp.Count = 0
					cur = &cp
					a.spent[it.ID] = cur
				}
				cur.Count += it.Count
			}
			for _, it := range rec.Skills {
				cur := a.skills[it.ID]
				if cur == nil {
					cp := it
					cp.Count = 0
					cur = &cp
					a.skills[it.ID] = cur
				}
				cur.Count += it.Count
			}
		}
		av.Gross = av.Value
		av.Value = av.Gross - av.Cost
		p.Accounts = append(p.Accounts, av)
		p.Items += av.Items
		p.Gross += av.Gross
		p.Cost += av.Cost
		p.Value += av.Value
		p.Coins += av.Coins
	}
	for _, a := range by {
		for _, it := range a.items {
			// The panel DISPLAYS and SORTS BY this per-item value, so it has to carry the
			// resolved price too, not the cache cost. Setting only the account totals above
			// left "sort by value" and the top-drop chips ranking on the shop price: an uncut
			// ruby read 100 while it trades for 2,800 (owner spotted it, 2026-09-20).
			priced := *it
			priced.Value = lootUnit(prices, priced)
			a.view.Items = append(a.view.Items, priced)
		}
		sort.Slice(a.view.Items, func(i, j int) bool { return a.view.Items[i].ID < a.view.Items[j].ID })
		a.view.GroupAccounts = len(groupAccounts[a.view.Source.GroupKey()])
		a.view.PartAccounts = len(partAccounts[a.view.Source.PartKey()])
		if a.view.XP > 0 && a.view.Source.Skill != "" {
			if p.XP == nil {
				p.XP = map[string]int64{}
			}
			p.XP[a.view.Source.Skill] += a.view.XP
		}
		for _, it := range a.skills {
			a.view.Skills = append(a.view.Skills, *it)
			// An npc's experience counts toward the period's per-skill total beside the skill cards'.
			if p.XP == nil {
				p.XP = map[string]int64{}
			}
			p.XP[it.Name] += it.Count
		}
		sort.Slice(a.view.Skills, func(i, j int) bool { return a.view.Skills[i].ID < a.view.Skills[j].ID })
		for _, it := range a.spent {
			a.view.Spent = append(a.view.Spent, *it)
		}
		sort.Slice(a.view.Spent, func(i, j int) bool { return a.view.Spent[i].ID < a.view.Spent[j].ID })
		if len(a.view.Spent) > 0 {
			// Runes cast at the npc: the row's cost is what they would have sold for, and it is
			// unknown (the figure gross) unless every rune has a trustworthy price.
			cost, known := spentCost(prices, a.view.Source, a.view.Spent, credited)
			a.view.CostItem = spentLabel(a.view.Spent)
			for _, it := range a.view.Spent {
				a.view.CostUnits += it.Count
			}
			a.view.CostKnown = known
			if known {
				a.view.Cost = cost
			}
		}
		if c, ok := a.view.Source.Consumes(); ok {
			// The label and the cost cover every input (a Pearl bolts row uses tips AND Bolts:
			// loot.Source.Inputs); CostUnits counts events of the first, which for every recipe
			// the fleet runs is one of each input per event.
			cost, label, known := inputsCost(prices, a.view.Source, a.view.Events, credited)
			a.view.CostItem = label
			a.view.CostUnits = a.view.Events * c.Per
			a.view.CostKnown = known
			if known {
				a.view.Cost = cost
			}
		}
		if a.view.CostItem != "" && !a.view.CostKnown {
			for _, it := range a.view.Items {
				p.Unnetted += it.Count * it.Value
			}
		}
		p.Sources = append(p.Sources, a.view)
		p.Events[a.view.Source.Action] += a.view.Events
		if a.view.Source.Kind == loot.KindOpenable || a.view.Source.Action == loot.ActionOpen {
			name := a.view.Source.Name
			if loot.IsOysterPearlsRow(a.view.Source) {
				// The row is named for what it gives, but the header's tile counts the OYSTERS opened.
				name = loot.OysterName
			}
			p.Events[loot.ActionOpen+":"+name] += a.view.Events
		}
	}
	sort.Slice(p.Sources, func(i, j int) bool { return p.Sources[i].Key < p.Sources[j].Key })
	p.Runecraft = runecraftView(m, prices, credited)
	if p.Sources == nil {
		p.Sources = []LootSourceView{}
	}
	if p.Accounts == nil {
		p.Accounts = []LootAccountView{}
	}
	return p
}

func (l *lootAggregator) body() (lifetime, session LootPeriod, recent []LootRecent, history []LootHistoryBucket) {
	l.mu.Lock()
	defer l.mu.Unlock()
	// Kick the price table if it has gone stale. Non-blocking by design (see Refresh): this
	// is a request path, and a refresh is 32 fetches against someone else's site.
	l.prices.Refresh()
	// An input counts as recorded arriving if it arrived at any time (the lifetime), whichever period is
	// drawn: the Yew longbows strung since the restart were mostly cut before it.
	credited := creditedItems(l.lifetime)
	nowMs := time.Now().UnixMilli()
	lifetime = periodView(l.lifetime, l.prices, credited, deathLossesBy(l.deaths, nowMs, 0))
	session = periodView(l.session, l.prices, credited, deathLossesBy(l.deaths, nowMs, l.sessionStartMs))
	addKillRates(&lifetime, nowMs, 0)
	addKillRates(&session, nowMs, l.sessionStartMs)
	recent = append([]LootRecent{}, l.recent...)
	history = make([]LootHistoryBucket, 0, len(l.history))
	for _, b := range l.history {
		hb := LootHistoryBucket{Start: b.Start, Items: make(map[string]int64, len(b.Items)), Events: make(map[string]int64, len(b.Events))}
		for id, n := range b.Items {
			hb.Items[strconv.Itoa(id)] = n
		}
		for k, n := range b.Events {
			hb.Events[k] = n
		}
		history = append(history, hb)
	}
	return
}

// lootSnapshot builds the /api/loot body.
func (a *statsAggregator) lootSnapshot() LootBody {
	_, available := a.src.(LootSource)
	a.mu.Lock()
	since, start := a.trackingSinceUnixMs, a.processStartUnixMs
	a.mu.Unlock()
	a.loot.mu.Lock()
	a.loot.sessionStartMs = start
	a.loot.mu.Unlock()
	lifetime, session, recent, history := a.loot.body()
	now := a.now().UnixMilli()
	lifetime.Deaths, session.Deaths = a.loot.deathsViews(now, start)
	return LootBody{
		Available:     available,
		NowUnixMs:     a.now().UnixMilli(),
		TrackingSince: since,
		ProcessStart:  start,
		BucketMs:      lootBucketMs,
		Lifetime:      lifetime,
		SinceRestart:  session,
		Recent:        recent,
		History:       history,
		EssenceStock:  a.essenceStock(lifetime.Runecraft != nil),
	}
}

// pollLoot is statsAggregator.poll's loot half.
func (a *statsAggregator) pollLoot(now time.Time) {
	if ls, ok := a.src.(LootSource); ok {
		a.loot.record(ls.LootTotals(), now)
	}
	if ds, ok := a.src.(DeathSource); ok {
		a.loot.noteDeaths(ds.DeathEvents())
	}
}

// handleLoot answers GET /api/loot: the loot panel's data, always available (empty while the
// Source reports no loot).
func (s *Server) handleLoot(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, s.stats.lootSnapshot())
}
