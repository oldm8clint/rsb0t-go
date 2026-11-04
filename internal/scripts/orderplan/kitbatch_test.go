package orderplan

import (
	"testing"
	"time"
)

// Three noted kits fit one visit, and the noted sharks two of them carry share one pack stack.
func TestThreeNotedKitsShareOneVisit(t *testing.T) {
	b := NewKitBatch(PackSlots, KitBatchMax)
	k1 := []KitLineDecision{{Item: "Shark", Qty: 200, Noted: true}, {Item: "Fire rune", Qty: 500}}
	k2 := []KitLineDecision{{Item: "Shark", Qty: 100, Noted: true}, {Item: "Rope", Qty: 1}}
	k3 := []KitLineDecision{{Item: "Coins", Qty: 5000}, {Item: "Cake", Qty: 0, Reason: "out of stock"}}
	for i, k := range [][]KitLineDecision{k1, k2, k3} {
		if !b.Add(k, factsOf, 0) {
			t.Fatalf("kit %d did not fit (slots %d)", i+1, b.Slots())
		}
	}
	// shark notes, fire runes, rope, coins: four stacks; the second kit's sharks join the first's.
	if b.Slots() != 4 || b.Kits() != 3 {
		t.Fatalf("slots %d kits %d, want 4 and 3", b.Slots(), b.Kits())
	}
	if b.Add([]KitLineDecision{{Item: "Mind rune", Qty: 10}}, factsOf, 0) {
		t.Fatal("a fourth kit was taken: a visit draws at most KitBatchMax")
	}
}

// A kit that would overfill the pack ends the batch; the first kit is always taken, even a full one.
func TestABatchStopsAtThePack(t *testing.T) {
	b := NewKitBatch(PackSlots, KitBatchMax)
	if !b.Add([]KitLineDecision{{Item: "Staff", Qty: 26}}, factsOf, 0) {
		t.Fatal("the first kit must always be taken")
	}
	if b.Add([]KitLineDecision{{Item: "Staff", Qty: 3}}, factsOf, 0) {
		t.Fatal("29 slots in a 28-slot pack")
	}
	if !b.Add([]KitLineDecision{{Item: "Rope", Qty: 2}}, factsOf, 0) {
		t.Fatal("a kit of two slots fits beside 26")
	}
	if b.Slots() != 28 {
		t.Fatalf("slots %d, want 28", b.Slots())
	}
}

// A kit carrying a hand-over back reserves the slots that hand-over takes in the mule's pack.
func TestAHandOverBackReservesItsSlots(t *testing.T) {
	b := NewKitBatch(PackSlots, KitBatchMax)
	b.Add([]KitLineDecision{{Item: "Shark", Qty: 50, Noted: true}}, factsOf, 20)
	if b.Slots() != 21 {
		t.Fatalf("slots %d, want 21 (one shark stack and a 20-slot hand-over)", b.Slots())
	}
	if b.Add([]KitLineDecision{{Item: "Coins", Qty: 100}}, factsOf, 8) {
		t.Fatal("a second hand-over of 8 slots cannot fit beside 21")
	}
	if !b.Add([]KitLineDecision{{Item: "Coins", Qty: 100}}, factsOf, 0) {
		t.Fatal("the same kit with no hand-over fits")
	}
}

func TestGiveRoomOK(t *testing.T) {
	cases := []struct {
		free, leaving, give int
		ok                  bool
	}{
		{0, 0, 0, true},
		{3, 2, 5, true},
		{3, 1, 5, false},
		{28, 0, 28, true},
	}
	for _, c := range cases {
		if got := GiveRoomOK(c.free, c.leaving, c.give); got != c.ok {
			t.Errorf("GiveRoomOK(%d,%d,%d) = %v", c.free, c.leaving, c.give, got)
		}
	}
}

// A clone records without touching the real ledger.
func TestLedgerCloneIsSeparate(t *testing.T) {
	now := time.Now()
	l := NewLedger(time.Hour)
	l.Record(now, "a", "Shark", 5)
	c := l.Clone()
	c.RecordKit(now, "b", "k1", []ItemCount{{Name: "Shark", Count: 10}})
	if l.UnitsOf(now, "Shark") != 5 || c.UnitsOf(now, "Shark") != 15 {
		t.Fatalf("ledger %d clone %d", l.UnitsOf(now, "Shark"), c.UnitsOf(now, "Shark"))
	}
}

// The two-way kit trade: the donor may give exactly its hand-over and waits until all of it shows.
func TestAKitScreenWithAHandOverBack(t *testing.T) {
	v := KitReceiveView{
		Screen: ScreenOffer, Partner: "mule", Mule: "mule", Planned: true,
		Want:   []KitLine{{Item: "Shark", Qty: 100}},
		Theirs: []ItemCount{{Name: "Shark", Count: 100}},
		Give:   []ItemCount{{Name: "Coins", Count: 50000}, {Name: "Bones", Count: 30}},
	}
	if a, why := DecideReceiveKit(v); a != ReceiveWait {
		t.Fatalf("accepted before the hand-over showed: %v %s", a, why)
	}
	v.Mine = []ItemCount{{Name: "Coins", Count: 50000}}
	if a, _ := DecideReceiveKit(v); a != ReceiveWait {
		t.Fatalf("accepted with half the hand-over on the screen: %v", a)
	}
	v.Mine = append(v.Mine, ItemCount{Name: "Bones", Count: 30})
	if a, why := DecideReceiveKit(v); a != ReceiveAccept {
		t.Fatalf("the whole kit and the whole hand-over are on the screen: %v %s", a, why)
	}
	v.Mine = append(v.Mine, ItemCount{Name: "Rune scimitar", Count: 1})
	if a, _ := DecideReceiveKit(v); a != ReceiveDecline {
		t.Fatalf("something not in the hand-over is on my side: %v", a)
	}
	// Without a hand-over, giving anything is still refused.
	v.Give = nil
	v.Mine = []ItemCount{{Name: "Coins", Count: 1}}
	if a, _ := DecideReceiveKit(v); a != ReceiveDecline {
		t.Fatalf("a kit with no hand-over had my side filled: %v", a)
	}
}

func TestJudgeGive(t *testing.T) {
	agreed := []KitLine{{Item: "Coins", Qty: 50000}, {Item: "Big bones", Qty: 30, Noted: true}}
	cases := []struct {
		name   string
		theirs []KitLine
		want   GiveState
	}{
		{"nothing yet", nil, GivePartial},
		{"half", []KitLine{{Item: "coins", Qty: 50000}}, GivePartial},
		{"less coins", []KitLine{{Item: "Coins", Qty: 100}}, GivePartial},
		{"all of it", []KitLine{{Item: "Coins", Qty: 50000}, {Item: "Big bones", Qty: 30, Noted: true}}, GiveComplete},
		{"more coins", []KitLine{{Item: "Coins", Qty: 50001}}, GiveBad},
		{"unnoted where noted was agreed", []KitLine{{Item: "Big bones", Qty: 30}}, GiveBad},
		{"something else", []KitLine{{Item: "Rune scimitar", Qty: 1}}, GiveBad},
	}
	for _, c := range cases {
		if got, _ := JudgeGive(agreed, c.theirs); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	// A kit that agreed no hand-over: anything on the donor's side is bad, nothing is complete.
	if got, _ := JudgeGive(nil, nil); got != GiveComplete {
		t.Errorf("no hand-over, empty side: %v", got)
	}
	if got, why := JudgeGive(nil, []KitLine{{Item: "Coins", Qty: 1}}); got != GiveBad || why == "" {
		t.Errorf("no hand-over, coins offered: %v %q", got, why)
	}
}

func TestGiveSlotsFor(t *testing.T) {
	lines := []KitLine{{Item: "Coins", Qty: 9}, {Item: "Rope", Qty: 3}, {Item: "Cake", Qty: 40, Noted: true}}
	if got := GiveSlotsFor(lines, 0, factsOf); got < 3 {
		t.Fatalf("slots %d", got)
	}
	if got := GiveSlotsFor(nil, 50, factsOf); got != PackSlots {
		t.Fatalf("a claimed 50 slots is capped to a pack: %d", got)
	}
}
