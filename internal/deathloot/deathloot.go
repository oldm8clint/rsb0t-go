// Package deathloot is the fleet's record of what its deaths cost and whether any of it came back (a user request:
// "we should have a loot panel that shows deaths and items that are lost and never picked up by the account that died or
// another account in our fleet").
//
// WHAT A DEATH DOES (content 289 scripts/player/scripts/death.rs2, player_death_lose_items): an unskulled player keeps the three
// priciest items it carried (by obj cost, one unit each, backpack before worn on a tie) and one more under Protect Item; everything
// else, backpack and worn, is dropped on the tile of the death with ^lootdrop_duration = 200 ticks of life, and the account is
// teleported to Lumbridge. A drop with a receiver is visible to that player alone for the first 100 ticks (Obj.REVEAL) and to
// everyone after, unless the item is untradable. So the pile can be taken by its owner at once (if he could walk back through the
// respawn) and by anyone from the 100th tick, until the 200th tick takes it.
//
// WHAT THIS PACKAGE IS. Pure: no client, no fleet, no panel. The fleet's death hook builds a Death (the pack and the worn items at
// the moment the death line arrived, less what the server keeps: Lost) and notes it; the fleet's inventory hook notes every gain
// of an item by any account (a Gain), and the Ledger matches a gain to the open death piles it could have come from:
// the same item, the same level, within Radius tiles of the death tile, inside the pile's window. Nothing is guessed: a stack is
// recovered only by gains that were seen, and it is lost for good only when its window has closed without enough of them.
//
// WHAT IT CANNOT KNOW. The ground is not read: a gain near a pile that did not come from the pile (a withdrawal at a bank next
// to the death tile, an item swapped between backpack slots) is taken for a pickup, and a pickup by anyone outside the fleet is
// indistinguishable from the pile despawning. Protect Item and the skull are not read either, so the kept set is the unskulled,
// unprotected one. The panel says so.
package deathloot

import (
	"sort"
	"sync"
	"sync/atomic"
)

// The pile's clock, in milliseconds (a server tick is 600 ms).
const (
	// RevealMs is when a pile becomes visible to accounts other than the one that died (Obj.REVEAL, 100 ticks).
	RevealMs = 100 * 600
	// DespawnMs is when the pile is gone (^lootdrop_duration, 200 ticks after the drop).
	DespawnMs = 200 * 600
	// SlackMs widens both edges: the client reads a tick late and the death line arrives a little before the drop.
	SlackMs = 15 * 1000
	// OwnerGraceMs is how long after its own death the dead account's gains are not counted: the respawn hands the kept
	// items back to its backpack, and those are not pickups.
	OwnerGraceMs = 8 * 1000
	// Radius is how far from the death tile a gain still counts as a pickup of the pile (the account stands on the tile or
	// beside it when the take lands).
	Radius = 2
	// MaxDeaths bounds the ledger; the oldest deaths go first.
	MaxDeaths = 1500
	// KeepUnskulled is how many of the priciest items an unskulled player keeps.
	KeepUnskulled = 3
)

// Held is one stack the account carried, with the cache's unit cost (the server's oc_cost, which picks the kept items).
type Held struct {
	ID    int
	Name  string
	Count int64
	Cost  int64
	// Worn is true for the equipment tab, false for the backpack.
	Worn bool
}

// Recovery is one pickup of a pile's stack: who took how many, and when.
type Recovery struct {
	By    string `json:"by"`
	Count int64  `json:"count"`
	At    int64  `json:"at"`
}

// Stack is one item lost to the ground at a death, and what became of it.
type Stack struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Count int64  `json:"count"`
	// Cost is the cache unit cost, the panel's last-resort price (PriceBook.Value).
	Cost      int64      `json:"cost,omitempty"`
	Recovered []Recovery `json:"recovered,omitempty"`
}

// Taken is how many of the stack were picked up.
func (s Stack) Taken() int64 {
	var n int64
	for _, r := range s.Recovered {
		n += r.Count
	}
	if n > s.Count {
		n = s.Count
	}
	return n
}

// Remaining is how many are still unaccounted for.
func (s Stack) Remaining() int64 { return s.Count - s.Taken() }

// Death is one death and the items it dropped.
type Death struct {
	// ID is unique per death: the account and the death line's moment.
	ID      string `json:"id"`
	Account string `json:"account"`
	// Script is the script the account was running, "" when unknown.
	Script string `json:"script,omitempty"`
	// At is when the death line arrived, unix milliseconds.
	At       int64 `json:"at"`
	HaveTile bool  `json:"haveTile"`
	X        int   `json:"x"`
	Z        int   `json:"z"`
	Level    int   `json:"level"`
	// Place is the area name (deathreport.Area); Cause names what faced the account (deathreport.CauseKey).
	Place string `json:"place"`
	Cause string `json:"cause"`
	// Items is what went to the ground; Kept what the server handed back (not lost).
	Items []Stack `json:"items"`
	Kept  []Stack `json:"kept,omitempty"`
}

// WindowEnd is when the pile can no longer be taken, widened by the slack.
func (d Death) WindowEnd() int64 { return d.At + DespawnMs + SlackMs }

// Open is whether a pile with anything left could still be picked up at now.
func (d Death) Open(now int64) bool {
	if now > d.WindowEnd() {
		return false
	}
	for _, s := range d.Items {
		if s.Remaining() > 0 {
			return true
		}
	}
	return false
}

// Lost splits what a player carried into what the server keeps and what it drops, by the rule in the package comment. skulled keeps
// nothing, and protectItem keeps one more unit. Stacks are merged by id and ordered biggest count first.
func Lost(held []Held, skulled, protectItem bool) (lost, kept []Stack) {
	type slot struct {
		Held
		count int64
	}
	slots := make([]*slot, 0, len(held))
	for _, h := range held {
		if h.Count > 0 {
			slots = append(slots, &slot{Held: h, count: h.Count})
		}
	}
	keptN := map[int]int64{}
	keepOne := func() {
		// Content scans the backpack and then the worn items and takes a strictly greater cost, so the first of equals wins and
		// a zero-cost item is never kept. Worn is scanned after the backpack whatever order held came in.
		var best *slot
		for pass := 0; pass < 2; pass++ {
			for _, s := range slots {
				if s.Worn != (pass == 1) || s.count <= 0 {
					continue
				}
				if s.Cost > 0 && (best == nil || s.Cost > best.Cost) {
					best = s
				}
			}
		}
		if best == nil {
			return
		}
		best.count--
		keptN[best.ID]++
	}
	if !skulled {
		for i := 0; i < KeepUnskulled; i++ {
			keepOne()
		}
	}
	if protectItem {
		keepOne()
	}
	lostN := map[int]*Stack{}
	var order []int
	for _, s := range slots {
		if s.count <= 0 {
			continue
		}
		st := lostN[s.ID]
		if st == nil {
			st = &Stack{ID: s.ID, Name: s.Name, Cost: s.Cost}
			lostN[s.ID] = st
			order = append(order, s.ID)
		}
		st.Count += s.count
	}
	for _, id := range order {
		lost = append(lost, *lostN[id])
	}
	sort.SliceStable(lost, func(i, j int) bool {
		return lost[i].Count*maxI64(lost[i].Cost, 1) > lost[j].Count*maxI64(lost[j].Cost, 1)
	})
	names := map[int]Held{}
	for _, s := range slots {
		names[s.ID] = s.Held
	}
	for id, n := range keptN {
		h := names[id]
		kept = append(kept, Stack{ID: id, Name: h.Name, Count: n, Cost: h.Cost})
	}
	sort.Slice(kept, func(i, j int) bool {
		return kept[i].Cost > kept[j].Cost || kept[i].Cost == kept[j].Cost && kept[i].ID < kept[j].ID
	})
	return lost, kept
}

func maxI64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// Gain is one account's backpack gaining an item: ID of it, how many, when, and where the account stood.
type Gain struct {
	Account string
	At      int64
	X, Z    int
	Level   int
	ID      int
	Count   int64
}

// GainOf is the number of items a backpack slot change added: a slot that held the same item and grew adds the growth, a slot
// that now holds a different item adds all of it. A slot that emptied or shrank adds nothing.
func GainOf(prevID int32, prevCount int32, id int32, count int32) int64 {
	if id == 0 || count <= 0 {
		return 0
	}
	if prevID == id {
		if count > prevCount {
			return int64(count - prevCount)
		}
		return 0
	}
	return int64(count)
}

// Ledger is the live record: deaths, oldest first, with their recoveries.
type Ledger struct {
	mu     sync.Mutex
	deaths []*Death
	max    int
	// horizon is the latest moment any pile could still be taken, so the per-inventory-change hook can skip the lock when no
	// death is recent (Active). Written under mu, read without it.
	horizon atomic.Int64
}

// NewLedger returns an empty ledger that keeps at most max deaths (MaxDeaths when max <= 0).
func NewLedger(max int) *Ledger {
	if max <= 0 {
		max = MaxDeaths
	}
	return &Ledger{max: max}
}

// NoteDeath records a death. A death with no lost items is still kept (it is a death), and one already recorded under the same id is
// ignored.
func (l *Ledger) NoteDeath(d Death) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range l.deaths {
		if e.ID == d.ID {
			return
		}
	}
	cp := d
	cp.Items = cloneStacks(d.Items)
	cp.Kept = cloneStacks(d.Kept)
	l.deaths = append(l.deaths, &cp)
	sort.SliceStable(l.deaths, func(i, j int) bool { return l.deaths[i].At < l.deaths[j].At })
	if len(l.deaths) > l.max {
		l.deaths = l.deaths[len(l.deaths)-l.max:]
	}
	if end := cp.WindowEnd(); end > l.horizon.Load() {
		l.horizon.Store(end)
	}
}

// Active is whether any pile could still be open at now. Cheap and lock-free: the fleet calls it on every backpack change of every
// account.
func (l *Ledger) Active(now int64) bool { return now <= l.horizon.Load() }

// NoteGain matches a gain against the open piles and records the recoveries. It returns how many of the gained items it
// attributed to piles. The oldest pile is served first (it is the one closest to despawning).
func (l *Ledger) NoteGain(g Gain) int64 {
	if g.Count <= 0 || !l.Active(g.At) {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	left := g.Count
	for _, d := range l.deaths {
		if left <= 0 {
			break
		}
		if !d.HaveTile || d.Level != g.Level || g.At > d.WindowEnd() {
			continue
		}
		if chebyshev(d.X, d.Z, g.X, g.Z) > Radius {
			continue
		}
		if g.Account == d.Account {
			if g.At < d.At+OwnerGraceMs {
				continue
			}
		} else if g.At < d.At+RevealMs-SlackMs {
			continue
		}
		for i := range d.Items {
			s := &d.Items[i]
			if s.ID != g.ID || s.Remaining() <= 0 {
				continue
			}
			take := s.Remaining()
			if take > left {
				take = left
			}
			s.Recovered = append(s.Recovered, Recovery{By: g.Account, Count: take, At: g.At})
			left -= take
			break
		}
	}
	return g.Count - left
}

// Deaths is a deep copy of every death, oldest first.
func (l *Ledger) Deaths() []Death {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Death, len(l.deaths))
	for i, d := range l.deaths {
		out[i] = *d
		out[i].Items = cloneStacks(d.Items)
		out[i].Kept = cloneStacks(d.Kept)
	}
	return out
}

func cloneStacks(in []Stack) []Stack {
	if in == nil {
		return nil
	}
	out := make([]Stack, len(in))
	for i, s := range in {
		out[i] = s
		out[i].Recovered = append([]Recovery(nil), s.Recovered...)
	}
	return out
}

func chebyshev(ax, az, bx, bz int) int {
	dx, dz := ax-bx, az-bz
	if dx < 0 {
		dx = -dx
	}
	if dz < 0 {
		dz = -dz
	}
	if dx > dz {
		return dx
	}
	return dz
}

// Merge folds the deaths a running fleet reports (live) into the ones already stored, by id. A live death replaces its stored
// copy (the live ledger saw every gain since the death) unless the stored copy knows more pickups, which is what a fleet
// restart leaves; a stored death the live ledger no longer has is kept. The result is oldest first and holds at most max deaths.
func Merge(stored, live []Death, max int) []Death {
	if max <= 0 {
		max = MaxDeaths
	}
	byID := make(map[string]Death, len(stored)+len(live))
	for _, d := range stored {
		byID[d.ID] = d
	}
	for _, d := range live {
		if old, ok := byID[d.ID]; ok && old.Taken() > d.Taken() {
			continue
		}
		byID[d.ID] = d
	}
	out := make([]Death, 0, len(byID))
	for _, d := range byID {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At < out[j].At || out[i].At == out[j].At && out[i].ID < out[j].ID })
	if len(out) > max {
		out = out[len(out)-max:]
	}
	return out
}

func (d Death) Taken() int64 {
	var n int64
	for _, s := range d.Items {
		n += s.Taken()
	}
	return n
}
