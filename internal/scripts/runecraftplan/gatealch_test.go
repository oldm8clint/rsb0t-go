package runecraftplan

import (
	"strings"
	"testing"
	"time"
)

// The pay is 0.6 of the cost (alchemy.rs2: max(scale(6,10,cost),1)) and the bows are listed dearest first.
func TestAlchPaysAndTheBowsRankByIt(t *testing.T) {
	want := map[string]int{"Magic longbow": 1536, "Magic shortbow": 960, "Yew longbow": 768, "Yew shortbow": 480}
	last := 1 << 30
	for _, b := range AlchBows {
		if got := AlchPays(b.Cost); got != want[b.Name] || got >= last {
			t.Errorf("%s pays %d, want %d (and less than the one before, %d)", b.Name, got, want[b.Name], last)
		}
		last = AlchPays(b.Cost)
	}
	if AlchPays(1) != 1 || AlchPays(0) != 1 {
		t.Error("a cast pays at least a coin")
	}
	if !IsAlchBow(" magic SHORTBOW ") || IsAlchBow("Maple longbow") || IsAlchBow("Rune platelegs") {
		t.Error("IsAlchBow")
	}
	if strings.Join(AlchBowNames(), ",") != "Magic longbow,Magic shortbow,Yew longbow,Yew shortbow" {
		t.Errorf("names %v", AlchBowNames())
	}
}

func TestPlanAlchDrawTakesTheDearestFirst(t *testing.T) {
	bank := map[string]int{"Magic longbow": 30, "Magic shortbow": 72000, "Yew longbow": 5}
	have := func(n string) int { return bank[n] }
	got := PlanAlchDraw(have, 100)
	if len(got) != 2 || got[0] != (AlchDraw{"Magic longbow", 30}) || got[1] != (AlchDraw{"Magic shortbow", 70}) {
		t.Fatalf("%v", got)
	}
	// A bank that holds less than is wanted: all of it, dearest first.
	got = PlanAlchDraw(func(n string) int { return map[string]int{"Yew shortbow": 12, "Yew longbow": 3}[n] }, 100)
	if len(got) != 2 || got[0].Name != "Yew longbow" || got[0].Qty != 3 || got[1].Name != "Yew shortbow" || got[1].Qty != 12 {
		t.Fatalf("%v", got)
	}
	if got := PlanAlchDraw(have, 0); len(got) != 0 {
		t.Fatalf("0 wanted: %v", got)
	}
	if got := PlanAlchDraw(func(string) int { return 0 }, 50); len(got) != 0 {
		t.Fatalf("an empty bank: %v", got)
	}
}

// The kit: what a round draws. Nothing for an account that cannot cast; less when loaded carriers already wait (the road alone takes the
// stack away before the first trade); never so many that the pack's Natures run under their floor. A stack held through the trades is
// not bounded by the road.
func TestGateAlchKit(t *testing.T) {
	for _, c := range []struct {
		name                 string
		on, hold             bool
		want, magic, natures int
		staff                bool
		waiting              int
		expect               int
	}{
		{"the default", true, false, DefaultAlchBows, 78, 6000, true, 0, DefaultAlchBows},
		{"switched off", false, false, DefaultAlchBows, 78, 6000, true, 0, 0},
		{"none wanted", true, false, 0, 78, 6000, true, 0, 0},
		{"Magic 54", true, false, 100, 54, 6000, true, 0, 0},
		{"Magic 55", true, false, 100, 55, 6000, true, 0, 100},
		{"no fire staff worn", true, false, 100, 78, 6000, false, 0, 0},
		{"a big ask is bounded by what the road can cast", true, false, 500, 78, 6000, true, 0, AlchRoadCasts},
		{"carriers waiting at the gate: the road alone must take the stack", true, false, 500, 78, 6000, true, 3, AlchRoadCasts * 2 / 3},
		{"carriers waiting, a small ask stays small", true, false, 40, 78, 6000, true, 3, 40},
		{"the Natures run to their floor", true, false, 100, 78, AlchNaturesFloor + 30, true, 0, 30},
		{"fewer Natures than the floor", true, false, 100, 78, 20, true, 0, 0},
		{"held through the trades: the road does not bound it", true, true, 1500, 78, 6000, true, 0, 1500},
		{"held, carriers waiting: it is cast in the idle seconds, not on the road", true, true, 1500, 78, 6000, true, 4, 1500},
		{"held, bounded by its own limit", true, true, 5000, 78, 6000, true, 0, MaxAlchBowsHold},
		{"held, the Natures still bound it", true, true, 1500, 78, AlchNaturesFloor + 300, true, 0, 300},
	} {
		if got := GateAlchKit(c.on, c.hold, c.want, c.magic, c.natures, c.staff, c.waiting); got != c.expect {
			t.Errorf("%s: %d, want %d", c.name, got, c.expect)
		}
	}
	if AlchBowsLimit(false) != AlchRoadCasts || AlchBowsLimit(true) != MaxAlchBowsHold {
		t.Errorf("limits %d %d", AlchBowsLimit(false), AlchBowsLimit(true))
	}
}

// The carriers load what the crafter says he can take: 24 while he holds a stack of bows, a full 25 otherwise.
func TestShelfWantFollowsTheCrafterCapacity(t *testing.T) {
	for _, c := range []struct{ cap, want int }{
		{0, CarrierOffer}, {25, CarrierOffer}, {24, 24}, {20, 20}, {MinLoad - 1, CarrierOffer}, {26, CarrierOffer}, {-3, CarrierOffer},
	} {
		if got := (GateInfo{Cap: c.cap}).ShelfWant(); got != c.want {
			t.Errorf("capacity %d: load %d, want %d", c.cap, got, c.want)
		}
	}
}

func idleGate() GateAlchView {
	return GateAlchView{Enabled: true, Magic: 78, FireStaff: true, Natures: 6000, Stack: 50, Nearest: -1}
}

// THE IDLE-TIME RULE: a cast goes out only when nothing is waiting or open and no carrier could be asking before its lock is over.
func TestDecideGateAlchTable(t *testing.T) {
	for _, c := range []struct {
		name string
		mod  func(v *GateAlchView)
		cast bool
		in   string
	}{
		{"nothing in the way", nil, true, "no carrier is near"},
		{"switched off", func(v *GateAlchView) { v.Enabled = false }, false, "off"},
		{"no bows held", func(v *GateAlchView) { v.Stack = 0 }, false, "no bows"},
		{"Magic 54", func(v *GateAlchView) { v.Magic = 54 }, false, "Magic 54"},
		{"no fire staff", func(v *GateAlchView) { v.FireStaff = false }, false, "fire staff"},
		{"no Natures", func(v *GateAlchView) { v.Natures = 0 }, false, "Nature rune"},
		{"a trade screen is open: the cast's if_close would close it for both", func(v *GateAlchView) { v.TradeOpen = true }, false, "screen is open"},
		{"an answer is out: the screen is about to open", func(v *GateAlchView) { v.Awaiting = true }, false, "answer"},
		{"a request waits", func(v *GateAlchView) { v.Pending = 1 }, false, "wait to be answered"},
		{"a random event", func(v *GateAlchView) { v.Busy = true }, false, "random event"},
		{"the last cast's lock", func(v *GateAlchView) { v.Locked = true }, false, "lock"},
		{"a carrier at the stand", func(v *GateAlchView) { v.Nearest = 1 }, false, "could be asking"},
		{"a carrier at the edge of the clearance", func(v *GateAlchView) { v.Nearest = AlchClearTiles }, false, "could be asking"},
		{"a carrier just beyond it", func(v *GateAlchView) { v.Nearest = AlchClearTiles + 1 }, true, ""},
		{"none in view", func(v *GateAlchView) { v.Nearest = -1 }, true, ""},
		{"burning the stack down before the gate trades: the clearance is waived", func(v *GateAlchView) { v.Burning, v.Nearest = true, 1 }, true, ""},
		{"burning does not waive an open screen", func(v *GateAlchView) { v.Burning, v.TradeOpen = true, true }, false, "screen is open"},
		{"burning does not waive the lock", func(v *GateAlchView) { v.Burning, v.Locked = true, true }, false, "lock"},
		// The reviewer's deadlock (16 Oct 2026): a burn-down answers nobody, so a request that waits is waived or the stack never goes.
		{"burning casts through a request that waits: Hold answers nobody until the bows are gone", func(v *GateAlchView) { v.Burning, v.Pending = true, 2 }, true, ""},
		{"burning does not waive an answer that is out", func(v *GateAlchView) { v.Burning, v.Awaiting = true, true }, false, "answer"},
		{"holding the stack through the trades still waits for the request", func(v *GateAlchView) { v.Burning, v.Pending = false, 1 }, false, "wait to be answered"},
	} {
		v := idleGate()
		if c.mod != nil {
			c.mod(&v)
		}
		got := DecideGateAlch(v)
		if got.Cast != c.cast || !strings.Contains(strings.ToLower(got.Why), strings.ToLower(c.in)) {
			t.Errorf("%s: cast %v (%q), want %v containing %q", c.name, got.Cast, got.Why, c.cast, c.in)
		}
	}
	// The clearance is what a carrier can cover while the lock lasts: two tiles a tick for five ticks, and the tile beside the stand.
	if AlchClearTiles != 12 {
		t.Errorf("AlchClearTiles %d", AlchClearTiles)
	}
}

// A stack held at a trade costs the 25th essence: the gate does not answer while it can still be cast away, and the round ends when it
// cannot be.
func TestBurnDownAndStuck(t *testing.T) {
	if !BurnDown(10, true, 0) || !BurnDown(10, true, AlchBurnMax-time.Second) {
		t.Error("a stack that can be cast away holds the gate")
	}
	if BurnDown(0, true, 0) || BurnDown(10, false, 0) || BurnDown(10, true, AlchBurnMax) {
		t.Error("no stack, nothing to cast with, or too long: the gate does not wait")
	}
	if AlchStuck(0, false, time.Hour) != "" || AlchStuck(10, true, time.Minute) != "" {
		t.Error("not stuck")
	}
	if why := AlchStuck(10, false, 0); !strings.Contains(why, "10 noted bow(s) are held and cannot be cast away") {
		t.Errorf("%q", why)
	}
	if why := AlchStuck(10, true, AlchBurnMax); !strings.Contains(why, "still held after 6m0s") {
		t.Errorf("%q", why)
	}
}

// The draw follows the road: the first round draws AlchFirstDraw, a round that left bows draws the road's casts plus a margin, one that cast
// all it drew draws a fifth more, never above the setting and never under AlchMinDraw.
func TestAlchRoadDrawFollowsTheRoad(t *testing.T) {
	for _, c := range []struct {
		name                                 string
		want, lastDrawn, lastCasts, lastLeft int
		got                                  int
	}{
		{"nothing measured yet", 100, 0, 0, 0, AlchFirstDraw},
		{"nothing measured yet, a small setting", 30, 0, 0, 0, 30},
		{"the road took 60 of 100 drawn and left 40", 100, 100, 60, 40, 60 + AlchDrawMargin},
		{"the road took all 60 drawn", 100, 60, 60, 0, 72},
		{"it grows by the margin at least", 100, 20, 20, 0, 25},
		{"never above the setting", 70, 60, 60, 0, 70},
		{"never under the minimum", 100, 100, 3, 97, AlchMinDraw},
		{"a setting under the minimum is the setting", 10, 100, 3, 97, 10},
	} {
		if got := AlchRoadDraw(c.want, c.lastDrawn, c.lastCasts, c.lastLeft); got != c.got {
			t.Errorf("%s: drew %d, want %d", c.name, got, c.got)
		}
	}
}

func TestGateAlchLine(t *testing.T) {
	got := GateAlchLine(1200, 1152000, 78000, 3600*time.Second, 40, "Magic shortbow")
	for _, want := range []string{"1,200 High Level Alchemy cast(s)", "1,152,000 gp", "78,000 Magic xp", "1h0m0s of casting", "40 bow(s) left (Magic shortbow)"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q lacks %q", got, want)
		}
	}
}
