package antipoisoncollectplan

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/tile"
)

func base() Snapshot {
	return Snapshot{
		Here: Post, FreeSlots: 20, MuleName: "mule1", HandOffAt: 20, HandOffEvery: 30 * time.Minute,
		HandOffRetryOK: true,
	}
}

func TestWaitsAtPostWithNothingVisible(t *testing.T) {
	if got := Decide(base()); got.Kind != StepWait {
		t.Fatalf("got %v, want wait", got.Kind)
	}
}

func TestTravelsWhenFarFromPost(t *testing.T) {
	s := base()
	s.Here = tile.Tile{X: 2725, Z: 3493}
	if got := Decide(s); got.Kind != StepTravel {
		t.Fatalf("got %v, want travel", got.Kind)
	}
	s.Here = Post
	s.Here.Level = 1
	if got := Decide(s); got.Kind != StepTravel {
		t.Fatalf("other level: got %v, want travel", got.Kind)
	}
}

func TestPicksNearestVisibleSpawn(t *testing.T) {
	s := base()
	s.Visible = []Spot{Spawns[0], Spawns[1]}
	s.Here = tile.Tile{X: 2465, Z: 3175}
	got := Decide(s)
	if got.Kind != StepPick || got.Spot != Spawns[1] {
		t.Fatalf("got %+v, want pick of the east spawn", got)
	}
	s.Here = tile.Tile{X: 2438, Z: 3160}
	if got := Decide(s); got.Spot != Spawns[0] {
		t.Fatalf("got %+v, want the west spawn", got)
	}
}

func TestPickBeatsTravel(t *testing.T) {
	s := base()
	s.Here = tile.Tile{X: 2700, Z: 3200}
	s.Visible = []Spot{Spawns[1]}
	if got := Decide(s); got.Kind != StepPick {
		t.Fatalf("got %v, want pick", got.Kind)
	}
}

func TestHandOffAtDoseCount(t *testing.T) {
	s := base()
	s.HeldPots, s.HeldDoses = 19, 19
	if got := Decide(s); got.Kind == StepHandOff {
		t.Fatal("19 doses should not hand off yet")
	}
	s.HeldPots, s.HeldDoses = 20, 20
	if got := Decide(s); got.Kind != StepHandOff {
		t.Fatalf("got %v, want handoff", got.Kind)
	}
}

func TestHandOffOnTimerNeedsSomethingHeld(t *testing.T) {
	s := base()
	s.SinceHandOff = time.Hour
	if got := Decide(s); got.Kind == StepHandOff {
		t.Fatal("nothing held, no hand-off")
	}
	s.HeldPots, s.HeldDoses = 3, 3
	if got := Decide(s); got.Kind != StepHandOff {
		t.Fatalf("got %v, want handoff on the timer", got.Kind)
	}
}

func TestHandOffCooldownKeepsPicking(t *testing.T) {
	s := base()
	s.HeldPots, s.HeldDoses = 22, 22
	s.HandOffRetryOK = false
	s.Visible = []Spot{Spawns[0]}
	if got := Decide(s); got.Kind != StepPick {
		t.Fatalf("got %v, want pick during the cooldown", got.Kind)
	}
}

func TestFullPackBanksWhenHandOffBlocked(t *testing.T) {
	s := base()
	s.HeldPots, s.HeldDoses, s.FreeSlots = 28, 28, 0
	s.HandOffRetryOK = false
	if got := Decide(s); got.Kind != StepBank {
		t.Fatalf("got %v, want bank", got.Kind)
	}
}

func TestNoMuleBanksInsteadOfHandingOff(t *testing.T) {
	s := base()
	s.MuleName = ""
	s.HeldPots, s.HeldDoses, s.FreeSlots = 28, 28, 0
	if got := Decide(s); got.Kind != StepBank {
		t.Fatalf("got %v, want bank", got.Kind)
	}
}

func TestFullOfOtherItemsWaits(t *testing.T) {
	s := base()
	s.FreeSlots = 0
	s.Visible = []Spot{Spawns[0]}
	if got := Decide(s); got.Kind != StepWait {
		t.Fatalf("got %v, want wait", got.Kind)
	}
}

func TestKitIsBankedFirst(t *testing.T) {
	s := base()
	s.Kit = true
	s.HeldPots, s.HeldDoses = 25, 25
	s.Visible = []Spot{Spawns[0]}
	if got := Decide(s); got.Kind != StepClear {
		t.Fatalf("got %v, want clear before anything else", got.Kind)
	}
}

func TestBankedDosesCountTowardTheHandOff(t *testing.T) {
	s := base()
	s.HeldPots, s.HeldDoses = 5, 5
	s.BankPots, s.BankDoses = 15, 15
	if got := Decide(s); got.Kind != StepHandOff {
		t.Fatalf("got %v, want handoff with 20 doses held and banked", got.Kind)
	}
	s.HeldPots, s.HeldDoses = 0, 0
	s.BankDoses = 3
	s.SinceHandOff = time.Hour
	if got := Decide(s); got.Kind != StepHandOff {
		t.Fatalf("got %v, want handoff on the timer with only banked doses", got.Kind)
	}
}

func TestDecantPairPoursSmallestOntoLargest(t *testing.T) {
	one := donorplan.ItemRow{Name: "Antipoison(1)", ID: 179, Count: 1}
	three := donorplan.ItemRow{Name: "Antipoison(3)", ID: 175, Count: 1}
	four := donorplan.ItemRow{Name: "Antipoison(4)", ID: 2446, Count: 1}
	if _, _, ok := DecantPair([]donorplan.ItemRow{one, four, four}); ok {
		t.Fatal("one partial and two (4)s: nothing to pour")
	}
	from, onto, ok := DecantPair([]donorplan.ItemRow{four, one, three, one})
	if !ok || from.ID != 179 || onto.ID != 175 {
		t.Fatalf("got %+v onto %+v (%v), want the (1) onto the (3)", from, onto, ok)
	}
	from, onto, ok = DecantPair([]donorplan.ItemRow{one, one})
	if !ok || from.ID != 179 || onto.ID != 179 {
		t.Fatalf("two (1)s: got %+v onto %+v (%v)", from, onto, ok)
	}
}

func TestDosesAndLoad(t *testing.T) {
	pack := []donorplan.ItemRow{
		{Name: "Antipoison(1)", ID: 179, Count: 1},
		{Name: "Antipoison(1)", ID: 179, Count: 1},
		{Name: "Antipoison(3)", ID: 175, Count: 1},
		{Name: "Shark", ID: 385, Count: 1},
	}
	pots, doses := Doses(pack)
	if pots != 3 || doses != 5 {
		t.Fatalf("pots %d doses %d, want 3 and 5", pots, doses)
	}
	if got := MuleHaul(pack); got != 3 {
		t.Fatalf("haul %d, want 3 (the shark stays)", got)
	}
	slots := 0
	for _, g := range MuleLoad(pack, 23) {
		if !IsAntipoison(g.Name) {
			t.Fatalf("load holds %q", g.Name)
		}
		slots += g.Slots
	}
	if slots != 3 {
		t.Fatalf("load slots %d, want 3", slots)
	}
}

func TestDecodeSettings(t *testing.T) {
	s, err := DecodeSettings(map[string]interface{}{"mule": "mule1", "handOffAt": "10", "pinWorld": 3.0})
	if err != nil || s.Mule != "mule1" || s.HandOffAt != 10 || s.HandOffMinutes != 30 {
		t.Fatalf("got %+v, %v", s, err)
	}
	s, err = DecodeSettings(map[string]interface{}{"mule": ""})
	if err != nil || s.Mule != "" {
		t.Fatalf("explicit blank mule: %+v, %v", s, err)
	}
	for _, bad := range []map[string]interface{}{
		{"handoffAt": 5}, {"handOffAt": 0}, {"handOffAt": 200}, {"handOffMinutes": 0}, {"logDetail": "loud"}, {"handOffAt": "x"}, {"mule": 5},
	} {
		if _, err := DecodeSettings(bad); err == nil {
			t.Fatalf("settings %v should be rejected", bad)
		}
	}
}

func TestSpawnTilesAreTheObservatoryOnes(t *testing.T) {
	if Spawns[0].Tile.X != 2437 || Spawns[0].Tile.Z != 3164 || Spawns[1].Tile.X != 2467 || Spawns[1].Tile.Z != 3177 {
		t.Fatalf("spawns %+v", Spawns)
	}
}
