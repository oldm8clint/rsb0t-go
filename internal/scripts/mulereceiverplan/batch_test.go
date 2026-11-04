package mulereceiverplan

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

func at(sec int) time.Time {
	return time.Date(2026, 9, 20, 0, 0, sec, 0, time.UTC)
}

// The two camps this fleet actually runs: the Catherby bank the fishers hand off at and the
// Seers' Village stand the woodcutters do, 84 tiles apart.
var (
	catherbyBank = tile.Tile{X: 2809, Z: 3441}
	seersStand   = SeersStand
)

func req(name string, t tile.Tile, first int, slots int) PickupRequest {
	return PickupRequest{From: name, Tile: t, Slots: slots, FirstAt: at(first), LastAt: at(first)}
}

func TestGroupByCampSeparatesTheTwoBanksAndKeepsQueueOrder(t *testing.T) {
	pending := []PickupRequest{
		req("fish one", catherbyBank, 10, 20),
		req("wood one", seersStand, 11, 5),
		req("fish two", tile.Tile{X: 2811, Z: 3443}, 12, 8),
		req("wood two", tile.Tile{X: 2726, Z: 3492}, 13, 3),
	}
	camps := GroupByCamp(pending, 25)
	if len(camps) != 2 {
		t.Fatalf("GroupByCamp = %d camps, want 2 (Catherby and Seers are %d tiles apart)", len(camps), tile.Chebyshev(catherbyBank, seersStand))
	}
	if camps[0].At != catherbyBank {
		t.Errorf("first camp anchored at %+v, want the earliest request's own tile %+v", camps[0].At, catherbyBank)
	}
	if camps[1].At != seersStand {
		t.Errorf("second camp anchored at %+v, want %+v", camps[1].At, seersStand)
	}
	if got := camps[0].Names(); len(got) != 2 || got[0] != "fish one" || got[1] != "fish two" {
		t.Errorf("Catherby camp names = %v, want the queue order", got)
	}
	if camps[0].Slots != 28 || camps[1].Slots != 8 {
		t.Errorf("slots = %d/%d, want 28/8", camps[0].Slots, camps[1].Slots)
	}
	if !camps[0].Oldest.Equal(at(10)) || !camps[1].Oldest.Equal(at(11)) {
		t.Errorf("oldest = %v/%v, want the earliest ask in each camp", camps[0].Oldest, camps[1].Oldest)
	}
	if !camps[0].Holds("FISH TWO") || camps[0].Holds("wood one") {
		t.Error("Holds should match a requester case-insensitively and only within its own camp")
	}
}

func TestGroupByCampRadiusAndLevelDecideMembership(t *testing.T) {
	near := req("near", tile.Tile{X: 2809 + 25, Z: 3441}, 11, 1)
	far := req("far", tile.Tile{X: 2809 + 26, Z: 3441}, 12, 1)
	upstairs := req("upstairs", tile.Tile{X: 2809, Z: 3441, Level: 1}, 13, 1)
	camps := GroupByCamp([]PickupRequest{req("anchor", catherbyBank, 10, 1), near, far, upstairs}, 25)
	if len(camps) != 3 {
		t.Fatalf("camps = %d, want 3 (anchor+near, far, upstairs)", len(camps))
	}
	if len(camps[0].Requests) != 2 {
		t.Errorf("exactly on the radius should join: camp holds %d", len(camps[0].Requests))
	}
	if camps[2].At.Level != 1 {
		t.Error("a request on another level never joins a ground-level camp")
	}
}

func TestChooseCampTakesTheLongestWaitingRequestNotTheBiggestCamp(t *testing.T) {
	// The real shape of this fleet: many fishers, few woodcutters. The one woodcutter asked
	// first, so it must win over a camp four times its size.
	pending := []PickupRequest{
		req("wood one", seersStand, 10, 4),
		req("fish a", catherbyBank, 20, 28),
		req("fish b", catherbyBank, 21, 28),
		req("fish c", catherbyBank, 22, 28),
		req("fish d", catherbyBank, 23, 28),
	}
	got, ok := ChooseCamp(ChooseCampInput{Camps: GroupByCamp(pending, 25), Here: catherbyBank, SameCampRadius: 25})
	if !ok {
		t.Fatal("ChooseCamp found nothing")
	}
	if got.Camp.At != seersStand {
		t.Errorf("chose the camp at %+v, want Seers %+v — ranking by size starves the small camp", got.Camp.At, seersStand)
	}
}

func TestChooseCampCapStopsOneCampMonopolisingTheMule(t *testing.T) {
	// Catherby holds the oldest request every time, which is exactly how a busy camp starves a
	// quiet one once give-up-and-retry keeps refreshing its FirstAt. The cap is the backstop.
	pending := []PickupRequest{
		req("fish a", catherbyBank, 10, 28),
		req("wood one", seersStand, 30, 4),
	}
	camps := GroupByCamp(pending, 25)
	in := ChooseCampInput{Camps: camps, Here: catherbyBank, ServedAt: &catherbyBank, SameCampRadius: 25, MaxInARow: 3}

	in.InARow = 2
	if got, _ := ChooseCamp(in); got.Camp.At != catherbyBank {
		t.Errorf("under the cap the oldest request still wins, got %+v", got.Camp.At)
	}
	in.InARow = 3
	got, ok := ChooseCamp(in)
	if !ok || got.Camp.At != seersStand {
		t.Fatalf("at the cap = %+v, want Seers to get its turn", got.Camp.At)
	}
	if got.Reason == "" || got.Reason == "it holds the longest-waiting request" {
		t.Errorf("the cap should say why it overrode age, got %q", got.Reason)
	}
	// With nowhere else to go the cap must not strand the mule: it serves the same camp again.
	in.Camps = GroupByCamp([]PickupRequest{req("fish a", catherbyBank, 10, 28)}, 25)
	if got, ok := ChooseCamp(in); !ok || got.Camp.At != catherbyBank {
		t.Errorf("with only one camp the cap must not refuse it, got %+v ok=%v", got.Camp.At, ok)
	}
}

func TestChooseCampIsDeterministicOnATieAndEmptyIsNoChoice(t *testing.T) {
	if _, ok := ChooseCamp(ChooseCampInput{}); ok {
		t.Error("no camps must be no choice, not an empty camp")
	}
	// Same age: the nearer camp wins.
	pending := []PickupRequest{req("a", catherbyBank, 10, 1), req("b", seersStand, 10, 1)}
	camps := GroupByCamp(pending, 25)
	if got, _ := ChooseCamp(ChooseCampInput{Camps: camps, Here: seersStand, SameCampRadius: 25}); got.Camp.At != seersStand {
		t.Errorf("a tie should go to the nearer camp, got %+v", got.Camp.At)
	}
	if got, _ := ChooseCamp(ChooseCampInput{Camps: camps, Here: catherbyBank, SameCampRadius: 25}); got.Camp.At != catherbyBank {
		t.Errorf("a tie from the other side should go the other way, got %+v", got.Camp.At)
	}
}
