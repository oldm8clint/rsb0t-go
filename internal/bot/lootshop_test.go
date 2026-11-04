package bot

import (
	"testing"

	"rsb0t-go/internal/loot"
)

type shopBuy struct {
	gained loot.Counts
	coins  int64
}

func newTestShopWatch() (*shopWatch, *[]shopBuy) {
	var got []shopBuy
	w := newShopWatch(func(g loot.Counts, c int64) {
		cp := loot.Counts{}
		for k, v := range g {
			cp[k] = v
		}
		got = append(got, shopBuy{cp, c})
	})
	return w, &got
}

func TestShopWatchRecordsABuyWhoseHalvesLandATickApart(t *testing.T) {
	w, got := newTestShopWatch()
	pack := loot.Counts{995: 1000}
	w.observe(true, pack, pack)                          // baseline
	w.observe(true, loot.Counts{995: 900}, nil)          // coins leave
	w.observe(true, loot.Counts{995: 900, 562: 10}, nil) // runes arrive
	w.observe(true, loot.Counts{995: 800, 562: 10}, nil) // a second click
	w.observe(true, loot.Counts{995: 800, 562: 20}, nil) //
	w.observe(true, loot.Counts{995: 800, 562: 20}, nil) // stands still: recorded
	if len(*got) != 1 || (*got)[0].coins != 200 || (*got)[0].gained[562] != 20 {
		t.Fatalf("got %+v, want one buy of 20 runes for 200 coins", *got)
	}
}

func TestShopWatchIgnoresSales(t *testing.T) {
	w, got := newTestShopWatch()
	pack := loot.Counts{995: 10, 377: 20}
	w.observe(true, pack, pack)
	w.observe(true, loot.Counts{995: 110, 377: 10}, nil)
	for i := 0; i < 5; i++ {
		w.observe(true, loot.Counts{995: 110, 377: 10}, nil)
	}
	w.observe(false, nil, nil)
	if len(*got) != 0 {
		t.Fatalf("a sale was recorded: %+v", *got)
	}
}

func TestShopWatchWaitsForTheShopWindowToMatchTheBackpack(t *testing.T) {
	w, got := newTestShopWatch()
	// The shop window still holds the last shop's pack: no baseline yet.
	w.observe(true, loot.Counts{995: 5000}, loot.Counts{995: 300, 562: 50})
	// The full update lands: baseline.
	w.observe(true, loot.Counts{995: 300, 562: 50}, loot.Counts{995: 300, 562: 50})
	w.observe(true, loot.Counts{995: 300, 562: 50}, nil)
	if len(*got) != 0 {
		t.Fatalf("the stale window was read as a purchase: %+v", *got)
	}
}

func TestShopWatchFlushesOnClose(t *testing.T) {
	w, got := newTestShopWatch()
	pack := loot.Counts{995: 100}
	w.observe(true, pack, pack)
	w.observe(true, loot.Counts{995: 40, 1755: 1}, nil)
	w.observe(false, nil, nil)
	if len(*got) != 1 || (*got)[0].coins != 60 || (*got)[0].gained[1755] != 1 {
		t.Fatalf("got %+v", *got)
	}
}

func TestShopWatchNoSessionIsANoOp(t *testing.T) {
	(&Context{}).watchShopPurchases()
}
