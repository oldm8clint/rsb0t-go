package runerunnerplan

import (
	"encoding/json"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/settings"
)

// Test 19: the state survives a save and a load, including a field a newer build wrote.
func TestStateRoundTrip(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	st := NewState()
	st.StartRound(BuildRing(RingInput{M: 1, O: 2, Shops: BuiltSites(), OBlock: true}), 0, now)
	st.Finish(StationResult{Outcome: OutcomeSkipped, Reason: "held 10", At: now})
	st.Finish(StationResult{Outcome: OutcomeDrew, At: now})
	st.Finish(StationResult{Outcome: OutcomeBought, Bought: map[string]int{"Mind rune": 900}, Spent: 12000, At: now})
	st.Trip = fleetplan.Trip{Active: true, PlannedCoins: 1227, Withdrawn: true}
	st.TripWorld = 2
	st.HopPending = 2
	st.NoteLundailDeath(now)

	bag := settings.NewBag()
	if err := bag.Encode(st); err != nil {
		t.Fatal(err)
	}
	bag.Set("futureField", map[string]interface{}{"x": 1.0})

	var back State
	keep, err := bag.DecodeCompat(&back)
	if err != nil {
		t.Fatal(err)
	}
	back.Normalise()
	if back.Cursor != 3 || back.Ring[back.Cursor].Key() != "Aubury@1" {
		t.Errorf("cursor %d", back.Cursor)
	}
	if back.Trip != st.Trip || back.TripWorld != 2 || back.HopPending != 2 {
		t.Errorf("trip %+v world %d hop %d", back.Trip, back.TripWorld, back.HopPending)
	}
	if back.Done["Betty@1"].Bought["Mind rune"] != 900 || back.Done["deliver@1"].Outcome != OutcomeSkipped {
		t.Errorf("done %+v", back.Done)
	}
	if back.Bought["Mind rune"] != 900 || back.Spent != 12000 || len(back.LundailDeaths) != 1 {
		t.Errorf("totals %v %d deaths %v", back.Bought, back.Spent, back.LundailDeaths)
	}
	if keep.Empty() || keep.Paths()[0] != "futureField" {
		t.Fatalf("unknown fields %v", keep.Paths())
	}
	out := settings.NewBag()
	if err := out.EncodeCompat(back, keep); err != nil {
		t.Fatal(err)
	}
	if _, ok := out.Raw()["futureField"]; !ok {
		t.Error("the newer build's field must be written back")
	}
	data, _ := out.MarshalIndent()
	var again State
	if err := json.Unmarshal(data, &again); err != nil || again.Cursor != 3 {
		t.Errorf("second round trip: %v cursor %d", err, again.Cursor)
	}
}

func TestFinishEndsTheRound(t *testing.T) {
	st := NewState()
	st.StartRound(BuildRing(RingInput{M: 1, Shops: []string{SiteAubury}}), 0, time.Unix(0, 0))
	if st.Finish(StationResult{Outcome: OutcomeSkipped}) || st.Finish(StationResult{Outcome: OutcomeDrew}) {
		t.Fatal("the round ended early")
	}
	if !st.Finish(StationResult{Outcome: OutcomeBought, Bought: map[string]int{"Mind rune": 5}, Spent: 15}) {
		t.Fatal("the last station ends the round")
	}
	bought, spent, _ := st.RoundTotals()
	if bought["Mind rune"] != 5 || spent != 15 {
		t.Errorf("round totals %v %d", bought, spent)
	}
	if FormatCounts(map[string]int{"Mind rune": 5, "Chaos rune": 2}) != "Chaos rune=2,Mind rune=5" || FormatCounts(nil) != "none" {
		t.Error("FormatCounts")
	}
}

func TestEndTrip(t *testing.T) {
	now := time.Unix(9_000_000, 0)
	st := NewState()
	st.Trip = fleetplan.Trip{Active: true, Bought: true, PlannedCoins: 500}
	st.TripWorld = 2
	st.TripResult = &StationResult{Outcome: OutcomeBought, Bought: map[string]int{"Mind rune": 140}, Spent: 1227}
	r := st.EndTrip(now, "unused")
	if r.Outcome != OutcomeBought || r.Bought["Mind rune"] != 140 || !r.At.Equal(now) {
		t.Errorf("a trip that bought ends as its purchase: %+v", r)
	}
	if st.Trip.Active || st.TripWorld != 0 || st.TripResult != nil {
		t.Errorf("the trip is reset: %+v", st)
	}
	st.Trip.Active = true
	if r := st.EndTrip(now, "died on the trip"); r.Outcome != OutcomeAborted || r.Reason != "died on the trip" {
		t.Errorf("a trip that bought nothing is aborted with the reason: %+v", r)
	}
}

// The round's coin shortfall is carried by the stations' own results, so it survives the hop restarts
// a round is made of, and a result from before the field existed reads as no shortfall.
func TestRoundShortSurvivesTheStateFile(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	st := NewState()
	st.StartRound(BuildRing(RingInput{M: 1, O: 2, Shops: BuiltSites(), OBlock: true}), 0, now)
	st.Finish(StationResult{Outcome: OutcomeSkipped, Reason: "cannot afford", NoCoins: true, At: now})
	st.Finish(StationResult{Outcome: OutcomeBought, ShortRunes: 120, ShortCoins: 9000, At: now})
	st.Finish(StationResult{Outcome: OutcomeBought, ShortRunes: 30, ShortCoins: 1500, At: now})
	st.Finish(StationResult{Outcome: OutcomeSkipped, Reason: "worth 3", At: now})
	bag := settings.NewBag()
	if err := bag.Encode(st); err != nil {
		t.Fatal(err)
	}
	var back State
	if _, err := bag.DecodeCompat(&back); err != nil {
		t.Fatal(err)
	}
	back.Normalise()
	stations, runes, coins := back.RoundShort()
	if len(stations) != 1 || runes != 150 || coins != 10500 {
		t.Errorf("short: stations %v runes %d coins %d", stations, runes, coins)
	}
	// A result written before the fields existed.
	var old StationResult
	if err := json.Unmarshal([]byte(`{"outcome":"skipped","reason":"no coins","at":"2026-10-12T12:00:00Z"}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.NoCoins || old.ShortRunes != 0 || old.ShortCoins != 0 {
		t.Errorf("old result: %+v", old)
	}
	if _, runes, _ := NewState().RoundShort(); runes != 0 {
		t.Error("a new state is not short")
	}
}

// Lundail's keep rides in the saved state (a hop restarts the script mid-round) and a new round clears it.
func TestLundailKeepIsSavedAndClearedByANewRound(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	st := NewState()
	st.StartRound(liveRoundRing(), 0, now)
	st.LundailKeep = 39216
	bag := settings.NewBag()
	if err := bag.Encode(st); err != nil {
		t.Fatal(err)
	}
	var back State
	if _, err := bag.DecodeCompat(&back); err != nil {
		t.Fatal(err)
	}
	if back.LundailKeep != 39216 {
		t.Fatalf("keep after a save and a load: %d", back.LundailKeep)
	}
	back.StartRound(liveRoundRing(), 0, now.Add(time.Hour))
	if back.LundailKeep != 0 {
		t.Errorf("a new round kept %d", back.LundailKeep)
	}
	// An older file without the field loads as no keep.
	var old State
	b2 := settings.NewBag()
	if err := b2.Encode(NewState()); err != nil {
		t.Fatal(err)
	}
	if _, err := b2.DecodeCompat(&old); err != nil || old.LundailKeep != 0 {
		t.Errorf("old state: keep %d, err %v", old.LundailKeep, err)
	}
}
