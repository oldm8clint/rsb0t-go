package mastercraftplan

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/trawlerplan"
)

func trawlerSet() Settings {
	set := orderSettings()
	set.Trawler = true
	return set
}

func TestTrawlerIsOffByDefault(t *testing.T) {
	d := DefaultSettings()
	if d.Trawler || d.TrawlerQuorum != DefaultTrawlerQuorum {
		t.Fatalf("defaults: trawler %v quorum %d", d.Trawler, d.TrawlerQuorum)
	}
	s := readySnap(time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC))
	s.Trawler.Crew = 500
	if got := Decide(d, s); got.Mode == ModeTrawler {
		t.Fatalf("off by default, whatever the lobby holds: %+v", got)
	}
	if TrawlerDue(d, s.Trawler, s.Ledger, s.Now) {
		t.Fatal("off: never due")
	}
}

func TestTrawlerBlockComesBeforeTrainingRounds(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	set := trawlerSet()
	s := readySnap(now)                           // a gem round is due on this snapshot
	s.Trawler.Food, s.Trawler.FoodKnown = 0, true // the mule is out of raw fish: the trigger
	d := Decide(set, s)
	if d.Mode != ModeTrawler || d.Resume || !d.Continue || !strings.Contains(d.Why, "block is on") {
		t.Fatalf("a block comes first, whatever the lobby holds (the round musters the crew): %+v", d)
	}
	if !TrawlerDue(set, s.Trawler, s.Ledger, now) {
		t.Fatal("due while the block runs")
	}
	// It needs no mule.
	s.MuleHeard = false
	if d := Decide(set, s); d.Mode != ModeTrawler {
		t.Fatalf("the mule unheard: %+v", d)
	}
	// Chain: a trawler round follows a trawler round straight on.
	if !Chain(ModeTrawler, d) {
		t.Fatal("a trawler round chains onto the last")
	}
	// 200M Fishing: no more blocks.
	s.Trawler.FishingXP = MaxSkillXP
	if d := Decide(set, s); d.Mode == ModeTrawler {
		t.Fatalf("at 200M Fishing XP: %+v", d)
	}
}

func TestTrawlerBlocksRunBackToBack(t *testing.T) {
	t0 := time.Date(2026, 11, 2, 16, 0, 0, 0, time.UTC)
	set := trawlerSet()
	set.Sched.TrawlerRest = 0 // craftTrawlerRestMinutes 0: no rest between blocks
	s := readySnap(t0)        // a gem round is due
	s.Trawler.Food, s.Trawler.FoodKnown = 0, true
	led := s.Ledger
	step := func(now time.Time) Decision {
		s.Now = now
		TrawlerCycleBefore(set, s.Trawler, led, now)
		d := Decide(set, s)
		TrawlerCycleAfter(set, led, now, d)
		return d
	}
	if d := step(t0); d.Mode != ModeTrawler || led.Trawler.BlockStart != t0 {
		t.Fatalf("the block starts: %+v %+v", d, led.Trawler)
	}
	// Aboard when the three hours are up: the game is played out first.
	s.Trawler.Here, s.Trawler.HereKnown = tile.Tile{X: 1880, Z: 4830, Level: 1}, true
	if d := step(t0.Add(3*time.Hour + time.Minute)); d.Mode != ModeTrawler || !d.Resume {
		t.Fatalf("the game under way finishes: %+v", d)
	}
	s.Trawler.HereKnown = false
	// Off the boat, the raw fish still out: the next block starts at once, on its own clock.
	t1 := t0.Add(3*time.Hour + 12*time.Minute)
	if d := step(t1); d.Mode != ModeTrawler || led.Trawler.BlockStart != t1 || led.Trawler.RestCrafted {
		t.Fatalf("the next block starts at once: %+v %+v", d, led.Trawler)
	}
	if !TrawlerDue(set, s.Trawler, led, t1.Add(time.Minute)) {
		t.Fatal("no rest holds the block")
	}
}

func TestTrawlerBlocksFollowTheMulesRawFish(t *testing.T) {
	t0 := time.Date(2026, 11, 5, 22, 0, 0, 0, time.UTC)
	set := trawlerSet()
	s := readySnap(t0) // a gem round is due
	led := s.Ledger
	step := func(now time.Time) Decision {
		s.Now = now
		TrawlerCycleBefore(set, s.Trawler, led, now)
		d := Decide(set, s)
		TrawlerCycleAfter(set, led, now, d)
		return d
	}
	// Some raw fish left on the mule: the other rounds run.
	s.Trawler.Food, s.Trawler.FoodKnown = 3, true
	if d := step(t0); d.Mode == ModeTrawler || !led.Trawler.BlockStart.IsZero() {
		t.Fatalf("raw fish left: the other rounds run: %+v", d)
	}
	// Out of all three: a block starts and runs on while the stock stays out or comes back (no early end by default).
	s.Trawler.Food = 0
	if d := step(t0.Add(time.Minute)); d.Mode != ModeTrawler || led.Trawler.BlockStart.IsZero() || !strings.Contains(d.Why, "3h0m0s") {
		t.Fatalf("out of raw fish: a 3 hour block starts: %+v", d)
	}
	s.Trawler.Food = 50000
	if d := step(t0.Add(2*time.Hour + 50*time.Minute)); d.Mode != ModeTrawler {
		t.Fatalf("a block under way runs its three hours whatever the stock: %+v", d)
	}
	// At three hours it ends; with raws restocked, the next does not start.
	t1 := t0.Add(3*time.Hour + 2*time.Minute)
	if d := step(t1); d.Mode == ModeTrawler || !led.Trawler.BlockStart.IsZero() {
		t.Fatalf("three hours are up and the fish are back: %+v %+v", d, led.Trawler)
	}
	// The mule's bank not read: no new block (the trigger needs the reading).
	s.Trawler.Food, s.Trawler.FoodKnown = 0, false
	if d := step(t1.Add(5 * time.Hour)); d.Mode == ModeTrawler {
		t.Fatalf("raw fish unknown: no block: %+v", d)
	}
	// A high mark, when set, ends a block early.
	set.TrawlerFoodHigh = 1000
	s.Trawler.Food, s.Trawler.FoodKnown = 0, true
	t2 := t1.Add(6 * time.Hour)
	step(t2)
	s.Trawler.Food = 1500
	if d := step(t2.Add(time.Hour)); d.Mode == ModeTrawler || !led.Trawler.BlockStart.IsZero() {
		t.Fatalf("at the high mark the block ends: %+v", d)
	}
}

func TestTrawlerFoodMarksDecode(t *testing.T) {
	d := DefaultSettings()
	if d.TrawlerFoodLow != DefaultTrawlerFoodLow || d.TrawlerFoodHigh != DefaultTrawlerFoodHigh {
		t.Fatalf("defaults %+v", d)
	}
	s, err := DecodeSettings(map[string]interface{}{KeyTrawlerFoodLow: 0})
	if err != nil || s.TrawlerFoodLow != 0 {
		t.Fatalf("0 turns the schedule off: %+v %v", s, err)
	}
	if _, err := DecodeSettings(map[string]interface{}{KeyTrawlerFoodLow: 9000, KeyTrawlerFoodHigh: 9000}); err == nil {
		t.Fatal("a high mark not over the low mark is refused")
	}
	if s, err := DecodeSettings(map[string]interface{}{KeyTrawlerBlockMinutes: 90}); err != nil || s.Sched.TrawlerBlock != 90*time.Minute {
		t.Fatalf("the block length is a setting: %+v %v", s.Sched, err)
	}
}

func TestTrawlerWaitsUnderTheClaimsFishingLevel(t *testing.T) {
	set := trawlerSet()
	s := readySnap(time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC))
	s.Stock = nil // nothing else due: the reasons are all there is
	s.Trawler = TrawlerInput{FishingLevel: 14, FoodKnown: true}
	d := Decide(set, s)
	if d.Mode == ModeTrawler || !strings.Contains(d.Why, "Fishing 14") {
		t.Fatalf("Fishing 14 cannot claim: %+v", d)
	}
	s.Trawler.FishingLevel = 0 // not read yet: not held back
	if d := Decide(set, s); d.Mode != ModeTrawler {
		t.Fatalf("an unread level does not hold the round back: %+v", d)
	}
}

func TestTrawlerBacksOffAfterFailures(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	set := trawlerSet()
	s := readySnap(now)
	s.Stock = nil
	s.Trawler.FoodKnown = true
	s.Ledger.Fail(ModeTrawler, now, set)
	d := Decide(set, s)
	if d.Mode == ModeTrawler || !strings.Contains(d.Why, "trawler rounds are backing off") {
		t.Fatalf("backing off: %+v", d)
	}
}

func TestTrawlerRoundUnderWayResumesFirst(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	set := trawlerSet()
	s := readySnap(now)
	s.Trawler = TrawlerInput{Here: tile.Tile{X: 1880, Z: 4830, Level: 1}, HereKnown: true}
	s.Ledger.Backoffs = map[string]Backoff{string(ModeDeliver): {Fails: 10}} // a hand-over give-up
	d := Decide(set, s)
	if d.Mode != ModeTrawler || !d.Resume || !strings.Contains(d.Why, "aboard") {
		t.Fatalf("aboard: %+v", d)
	}
	// Aboard resumes even with the round switched off: ScarabSwarm cannot start on the boat.
	off := orderSettings()
	if d := Decide(off, s); d.Mode != ModeTrawler {
		t.Fatalf("aboard with the round off: %+v", d)
	}
	// A catch to claim resumes while the round is on.
	s = readySnap(now)
	s.Ledger.Trawler.Claim = true
	if d := Decide(set, s); d.Mode != ModeTrawler || !d.Resume || !strings.Contains(d.Why, "claimed") {
		t.Fatalf("claim: %+v", d)
	}
	if d := Decide(off, s); d.Mode == ModeTrawler {
		t.Fatalf("a claim with the round switched off does not resume: %+v", d)
	}
}

func TestTrawlerQuorumSettingDecodes(t *testing.T) {
	s, err := DecodeSettings(map[string]interface{}{KeyTrawler: "true", KeyTrawlerQuorum: "8"})
	if err != nil || !s.Trawler || s.TrawlerQuorum != 8 {
		t.Fatalf("%+v %v", s, err)
	}
	if _, err := DecodeSettings(map[string]interface{}{KeyTrawlerQuorum: 0}); err == nil {
		t.Fatal("a quorum of 0 is refused")
	}
}

func TestTrawlerBoatIsTheCrewsBoat(t *testing.T) {
	// The resume reads the boat with trawlerplan's own zones, so the anchor and the crew agree on what "aboard" is.
	if !trawlerplan.OnBoat(tile.Tile{X: 1880, Z: 4830}) || trawlerplan.OnBoat(trawlerplan.LobbyStand) {
		t.Fatal("zones")
	}
}
