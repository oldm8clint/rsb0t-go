package trawlerplan

import (
	"reflect"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

func TestPlanClaimDropsJunkFirstAndTheBestFishLast(t *testing.T) {
	items := []CatchItem{
		{Slot: 0, ID: 317, Count: 300, Cost: 5},  // shrimp
		{Slot: 1, ID: 383, Count: 20, Cost: 300}, // shark
		{Slot: 2, ID: 371, Count: 40, Cost: 200}, // swordfish
		{Slot: 3, ID: 1, Count: 1, Cost: 200},    // junk at the swordfish's price: the fish first
		{Slot: 4, ID: 2, Count: 5, Cost: 1},      // junk that does not fit
	}
	type click struct{ id, option, times, moves int }
	clicks := func(mvs []CatchMove) []click {
		var out []click
		for _, mv := range mvs {
			out = append(out, click{mv.ID, mv.Option, mv.Times, mv.Moves})
		}
		return out
	}
	want := []click{
		{383, CatchWithdrawAll, 1, 20}, // the sharks fit
		{371, CatchWithdrawOne, 6, 6},  // six swordfish fill the pack
		{1, CatchWithdrawAll, 1, 0},    // then the junk drops, before any fish
		{2, CatchWithdrawAll, 1, 0},
		{317, CatchWithdrawAll, 1, 0}, // and the fish, cheapest first: the swordfish are the zone's last drops
		{371, CatchWithdrawAll, 1, 0},
	}
	if got := clicks(PlanClaim(items, 26)); !reflect.DeepEqual(got, want) {
		t.Fatalf("26 free slots:\n got %+v\nwant %+v", got, want)
	}
	want = []click{{1, CatchWithdrawAll, 1, 0}, {2, CatchWithdrawAll, 1, 0}, {317, CatchWithdrawAll, 1, 0}, {371, CatchWithdrawAll, 1, 0}, {383, CatchWithdrawAll, 1, 0}}
	if got := clicks(PlanClaim(items, 0)); !reflect.DeepEqual(got, want) {
		t.Fatalf("a full pack still empties the window, the best fish last:\n got %+v\nwant %+v", got, want)
	}
	if got := PlanClaim(items, 400); len(got) != 5 || got[2].ID != 1 || got[3].ID != 317 || got[3].Moves != 300 {
		t.Fatalf("400 free slots take everything into the pack: %+v", got)
	}
}

func TestLootersBankTheBoatKitAndPickTheDearestFish(t *testing.T) {
	if !LooterKit(PasteID) || !LooterKit(BucketFullID) || LooterKit(561) || LooterKit(389) {
		t.Fatal("a looter banks paste, rope and buckets only")
	}
	if !(FishRank(389) < FishRank(383) && FishRank(383) < FishRank(317) && FishRank(317) < FishRank(1)) {
		t.Fatal("manta before shark before shrimp, anything else last")
	}
}

func TestMuleFishAreTheOnesTheCrewCannotCook(t *testing.T) {
	if !IsMuleFish("Raw manta ray") || !IsMuleFish(" raw sea turtle") || IsMuleFish("Raw shark") || IsMuleFish("Manta ray") {
		t.Fatal("the mule takes the raw manta and turtle only")
	}
}

func TestHeldDropsEveryFishDearestFirst(t *testing.T) {
	var items []CatchItem
	for i := 0; i < 21; i++ {
		items = append(items, CatchItem{Slot: i, ID: 389, Count: 1, Cost: 300 + i}) // raw manta rays
	}
	items = append(items, CatchItem{Slot: 21, ID: 561, Count: 5000, Cost: 300}) // Nature runes stay
	got := HeldDrops(items)
	if len(got) != 21 || got[0].Slot != 20 || got[20].Slot != 0 {
		t.Fatalf("all 21 fish, dearest first: %+v", got)
	}
	if ZoneDrops < 28 {
		t.Fatal("a zone keeps a whole pack of drops")
	}
	if got := HeldDrops(items[21:]); len(got) != 0 {
		t.Fatalf("no fish held, nothing dropped: %+v", got)
	}
	if !NearNet(HeldDropStand) || HeldDropStand.X/8 == 2666/8 {
		t.Fatal("the held drops lie by the net for the crew, in another zone than the claim's spill")
	}
}

func TestNearestBankAndNearNet(t *testing.T) {
	if b := NearestBank(LobbyStand); b != Banks[1] {
		t.Fatalf("from the lobby Yanille is nearer: %+v", b)
	}
	if b := NearestBank(tile.Tile{X: 2660, Z: 3260}); b != Banks[0] {
		t.Fatalf("from north of Port Khazard East Ardougne is nearer: %+v", b)
	}
	if !NearNet(tile.Tile{X: 2668, Z: 3162}) || NearNet(tile.Tile{X: 2641, Z: 3171}) {
		t.Fatal("near the net is the net's few tiles, not the store")
	}
	if !IsTrawlerFish(383) || IsTrawlerFish(401) {
		t.Fatal("a shark is a trawler fish; seaweed is not")
	}
}

func TestShouldCrossOnlyOnTheAnchorsCall(t *testing.T) {
	if ShouldCross(true, false) {
		t.Fatal("with a bus, a crew member waits for the anchor's call")
	}
	if !ShouldCross(true, true) || !ShouldCross(false, false) {
		t.Fatal("the call, or no bus at all, crosses")
	}
}

func TestDecideCatchPhases(t *testing.T) {
	set := DefaultSettings()
	set.World = 1
	byNet := tile.Tile{X: 2668, Z: 3168, Level: 1}
	lobby := tile.Tile{X: 2671, Z: 3170, Level: 1} // the lobby deck, past the gangplank
	base := Snapshot{Here: byNet, World: 1, Paste: 10, Rope: 3, Bucket: 1, SecondsLeft: -1, BusKnown: true}
	cases := []struct {
		name string
		s    Snapshot
		want Phase
	}{
		{"fish on the floor, room in the pack", with(base, func(s *Snapshot) { s.GroundFish, s.FreeSlots = 5, 10 }), PhasePickup},
		{"pack full of fish", with(base, func(s *Snapshot) { s.GroundFish, s.FreeSlots, s.CatchHeld = 5, 0, 20 }), PhaseBank},
		{"floor empty, fish held", with(base, func(s *Snapshot) { s.FreeSlots, s.CatchHeld = 10, 4 }), PhaseBank},
		{"the anchor calls: board first", with(base, func(s *Snapshot) { s.Here, s.GroundFish, s.FreeSlots, s.AnchorCalled = lobby, 5, 10, true }), PhaseLobby},
		{"the alch kit wants a bank", with(base, func(s *Snapshot) { s.Here, s.NeedBank = tile.Tile{X: 2641, Z: 3171}, true }), PhaseBank},
		{"the alch kit does not pull a crew member out of the lobby", with(base, func(s *Snapshot) { s.Here, s.NeedBank = lobby, true }), PhaseLobby},
		{"aboard beats the floor", with(base, func(s *Snapshot) { s.Here, s.GroundFish, s.FreeSlots = tile.Tile{X: 1880, Z: 4830, Level: 1}, 5, 10 }), PhaseAboard},
	}
	for _, c := range cases {
		if got := Decide(c.s, set); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

func with(s Snapshot, f func(*Snapshot)) Snapshot {
	f(&s)
	return s
}

func TestDecodeSettingsAlchAndMule(t *testing.T) {
	s, err := DecodeSettings(map[string]interface{}{"alch": "false", "mule": "mule1"})
	if err != nil || s.Alch || s.Mule != "mule1" {
		t.Fatalf("decoded: %+v %v", s, err)
	}
	if !DefaultSettings().Alch {
		t.Fatal("alchemy is on by default")
	}
	if _, err := DecodeSettings(map[string]interface{}{"alch": "maybe"}); err == nil {
		t.Fatal("alch must be true or false")
	}
}
