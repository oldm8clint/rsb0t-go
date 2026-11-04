package wcfletchplan

import (
	"testing"
	"time"
)

// A hand-off trip holds the account where it is for TripTTL and no longer, and is ended by End. It needs
// no place in any world registry: the magic accounts that live on the world their homeWorld setting names
// hand off through it too.
func TestTripHoldsTheAccountForTheTTL(t *testing.T) {
	h := NewHandoffTrips()
	if h.On("tripper", t0) {
		t.Fatal("a trip nobody began")
	}
	h.Begin("tripper", t0)
	if !h.On("Tripper ", t0.Add(TripTTL-time.Second)) {
		t.Error("the trip ended early, or the name was not matched as the registry matches names")
	}
	if h.On("tripper", t0.Add(TripTTL+time.Second)) {
		t.Error("the trip outlived its TTL")
	}
	if h.On("tripper", t0) {
		t.Error("a lapsed trip came back")
	}
	h.Begin("tripper", t0)
	h.End("tripper")
	if h.On("tripper", t0) {
		t.Error("End did not end it")
	}
	// An account no registry holds (a magic account on its settings' world) has a trip all the same.
	s := NewWorldSpread()
	if _, ok := s.World("loner", t0); ok {
		t.Fatal("the loner should hold no place")
	}
	h.Begin("loner", t0)
	if !h.On("loner", t0.Add(time.Minute)) {
		t.Error("a trip without a world place was dropped")
	}
	// A place given back keeps the trip: the trip is not the registry's.
	s.Assign("loner", []int{1, 2}, 2, nil, t0)
	s.Release("loner")
	if !h.On("loner", t0.Add(time.Minute)) {
		t.Error("releasing the world place ended the trip")
	}
}
