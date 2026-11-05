package questplan

import (
	"fmt"
	"sync"
)

// THE SITE BOARD (plan 3.4, completeness critic item 18). The plan spoke of the fleet's SiteBoard as though it existed; only
// questrunner.SlotSite and questmods.impSiteFor did, each for one quest. Three trainer lanes (T1 Mining, T3 Fishing, T4
// Firemaking) were about to write one apiece, so the pure half is here, where the catalogue's sites and slots are:
// the count of accounts at each trainer site and in each trainer slot, and the rule that 280 accounts must not crowd one rock,
// one pen or one wheel. It is the process-wide board the fleet shares (every account runs in one process, as questrunner's
// quest slots do); lane N3 puts the stints on it and feeds State to the planner.

// SiteBoard counts the accounts at each trainer site and in each trainer slot, against the catalogue's capacities.
type SiteBoard struct {
	mu    sync.Mutex
	cat   Catalogue
	sites map[string]int
	slots map[string]int
}

// NewSiteBoard is an empty board over the catalogue's trainers.
func NewSiteBoard(cat Catalogue) *SiteBoard {
	return &SiteBoard{cat: cat, sites: map[string]int{}, slots: map[string]int{}}
}

// Lease is one account's place on the board. Release gives it back, once: a second call, and a nil lease, do nothing.
type Lease struct {
	b             *SiteBoard
	Trainer, Site string
	slot          string
	once          sync.Once
}

// Acquire takes a place for one account at a site of a trainer. An empty site takes the least loaded site with room. It
// refuses an unknown trainer or site, a site at its capacity, and a trainer whose fleet-wide slot is full, saying which.
func (b *SiteBoard) Acquire(trainer, site string) (*Lease, error) {
	t, ok := b.cat.ByID(trainer)
	if !ok {
		return nil, fmt.Errorf("no trainer %q", trainer)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if t.SlotCap > 0 && b.slots[t.Slot] >= t.SlotCap {
		return nil, fmt.Errorf("slot %s of %s is full (%d of %d)", t.Slot, trainer, b.slots[t.Slot], t.SlotCap)
	}
	if len(t.Sites) > 0 {
		var pick *TrainerSite
		for i := range t.Sites {
			s := &t.Sites[i]
			if site != "" && s.Key != site {
				continue
			}
			if b.sites[s.Key] >= s.Capacity {
				if site != "" {
					return nil, fmt.Errorf("site %s of %s is at capacity (%d)", site, trainer, s.Capacity)
				}
				continue
			}
			if pick == nil || float64(b.sites[s.Key])/float64(s.Capacity) < float64(b.sites[pick.Key])/float64(pick.Capacity) {
				pick = s
			}
		}
		if pick == nil {
			if site != "" {
				return nil, fmt.Errorf("trainer %s has no site %q", trainer, site)
			}
			return nil, fmt.Errorf("every site of %s is at capacity", trainer)
		}
		site = pick.Key
		b.sites[site]++
	} else if site != "" {
		return nil, fmt.Errorf("trainer %s has no site %q", trainer, site)
	}
	if t.SlotCap > 0 {
		b.slots[t.Slot]++
	}
	return &Lease{b: b, Trainer: trainer, Site: site, slot: slotOf(t)}, nil
}

func slotOf(t TrainerSpec) string {
	if t.SlotCap > 0 {
		return t.Slot
	}
	return ""
}

// Release gives the place back.
func (l *Lease) Release() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		b := l.b
		b.mu.Lock()
		defer b.mu.Unlock()
		if l.Site != "" && b.sites[l.Site] > 0 {
			b.sites[l.Site]--
		}
		if l.slot != "" && b.slots[l.slot] > 0 {
			b.slots[l.slot]--
		}
	})
}

// Load is the accounts at a site now.
func (b *SiteBoard) Load(site string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sites[site]
}

// State is the board as the planner reads it (FleetState.SiteLoad and SlotLoad); SlotBusy, the quests' slots, is the caller's.
func (b *SiteBoard) State() FleetState {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := FleetState{SiteLoad: make(map[string]int, len(b.sites)), SlotLoad: make(map[string]int, len(b.slots))}
	for k, v := range b.sites {
		if v > 0 {
			st.SiteLoad[k] = v
		}
	}
	for k, v := range b.slots {
		if v > 0 {
			st.SlotLoad[k] = v
		}
	}
	return st
}
