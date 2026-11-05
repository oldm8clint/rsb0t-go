package questplan

import (
	"sync"
	"testing"
)

func boardCatalogue() Catalogue {
	a := row("a", "mining", 1, 20)
	a.Sites = []TrainerSite{{Key: "rock1", Capacity: 2}, {Key: "rock2", Capacity: 3}}
	a.Slot, a.SlotCap = "ore", 4
	b := row("b", "smithing", 1, 20)
	b.Sites = []TrainerSite{{Key: "anvil", Capacity: 1}}
	c := row("c", "fishing", 1, 20) // no site, no slot: nothing to crowd
	d := row("d", "cooking", 1, 20)
	d.Slot, d.SlotCap = "ore", 4 // shares the ore slot with a
	return NewCatalogue(a, b, c, d)
}

func TestSiteBoardHoldsEachSiteToItsCapacity(t *testing.T) {
	b := NewSiteBoard(boardCatalogue())
	l1, err := b.Acquire("b", "anvil")
	if err != nil || l1.Site != "anvil" {
		t.Fatalf("first anvil: %v", err)
	}
	if _, err := b.Acquire("b", "anvil"); err == nil {
		t.Error("a second account at a one-anvil site")
	}
	if _, err := b.Acquire("b", ""); err == nil {
		t.Error("with every site full an empty site finds none")
	}
	l1.Release()
	l1.Release() // a second release gives nothing back
	if b.Load("anvil") != 0 {
		t.Errorf("load after release: %d", b.Load("anvil"))
	}
	if _, err := b.Acquire("b", "anvil"); err != nil {
		t.Errorf("the place came back: %v", err)
	}
	var nilLease *Lease
	nilLease.Release() // does nothing
}

func TestSiteBoardPicksTheLeastLoadedSiteAndHonoursTheSharedSlot(t *testing.T) {
	b := NewSiteBoard(boardCatalogue())
	var leases []*Lease
	got := map[string]int{}
	for i := 0; i < 4; i++ {
		l, err := b.Acquire("a", "")
		if err != nil {
			t.Fatalf("acquire %d: %v", i, err)
		}
		leases = append(leases, l)
		got[l.Site]++
	}
	// rock1 holds 2, rock2 holds 3: by load over capacity the order is rock1, rock2, rock1, rock2.
	if got["rock1"] != 2 || got["rock2"] != 2 {
		t.Errorf("spread: %v", got)
	}
	// The ore slot (4) is full across trainers: a fifth account, here a cook, is refused though rock2 has room.
	if _, err := b.Acquire("a", ""); err == nil {
		t.Error("the slot is full")
	}
	if _, err := b.Acquire("d", ""); err == nil {
		t.Error("the slot is shared with trainer d")
	}
	st := b.State()
	if st.SiteLoad["rock1"] != 2 || st.SiteLoad["rock2"] != 2 || st.SlotLoad["ore"] != 4 {
		t.Errorf("state: %+v", st)
	}
	leases[0].Release()
	if _, err := b.Acquire("d", ""); err != nil {
		t.Errorf("a place in the slot came back: %v", err)
	}
}

func TestSiteBoardRefusesWhatDoesNotExist(t *testing.T) {
	b := NewSiteBoard(boardCatalogue())
	for _, c := range [][2]string{{"nope", ""}, {"a", "nowhere"}, {"c", "rock1"}} {
		if _, err := b.Acquire(c[0], c[1]); err == nil {
			t.Errorf("Acquire(%q, %q) succeeded", c[0], c[1])
		}
	}
	// A trainer with no site and no slot has nothing to crowd.
	for i := 0; i < 50; i++ {
		if _, err := b.Acquire("c", ""); err != nil {
			t.Fatal(err)
		}
	}
}

// Under contention the board never lets more accounts in than the capacity.
func TestSiteBoardNeverExceedsCapacityUnderContention(t *testing.T) {
	b := NewSiteBoard(boardCatalogue())
	var wg sync.WaitGroup
	var mu sync.Mutex
	held := 0
	peak := 0
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l, err := b.Acquire("a", "")
			if err != nil {
				return
			}
			mu.Lock()
			held++
			peak = max(peak, held)
			mu.Unlock()
			if b.Load("rock1") > 2 || b.Load("rock2") > 3 {
				t.Error("a site is over capacity")
			}
			mu.Lock()
			held--
			mu.Unlock()
			l.Release()
		}()
	}
	wg.Wait()
	if peak > 4 {
		t.Errorf("%d accounts held the ore slot at once, the cap is 4", peak)
	}
	if st := b.State(); len(st.SiteLoad) != 0 || len(st.SlotLoad) != 0 {
		t.Errorf("everyone released: %+v", st)
	}
}
