package twitchplays

import (
	"testing"
	"time"
)

func TestReplaySourceIsDeterministic(t *testing.T) {
	events := []ChatEvent{
		{MessageID: "a", Author: "one", Text: "!fish", Source: "replay", Received: time.Unix(1, 0)},
		{MessageID: "b", Author: "two", Text: "hello", Source: "replay", Received: time.Unix(2, 0)},
		{MessageID: "c", Author: "one", Text: "!bank", Source: "replay", Received: time.Unix(3, 0)},
	}
	r := NewReplaySource(events)
	got, ok := r.Next()
	if !ok || got.MessageID != "a" {
		t.Fatal("first replay event missing")
	}
	got, ok = r.Next()
	if !ok || got.MessageID != "b" {
		t.Fatal("second replay event missing")
	}
	if _, ok := r.Next(); !ok {
		t.Fatal("third replay event missing")
	}
	if _, ok := r.Next(); ok {
		t.Fatal("replay did not terminate")
	}
}

func TestReplayQueueDropsInvalidMessages(t *testing.T) {
	q := ReplayQueue([]ChatEvent{
		{MessageID: "1", Author: "viewer", Text: "!fish", Source: "replay"},
		{MessageID: "2", Author: "viewer", Text: "!exec nope", Source: "replay"},
		{MessageID: "3", Author: "viewer", Text: "!bank", Source: "replay"},
	}, 10)
	// !bank is a forbidden action (ForbiddenActions), so only !fish is queued.
	if q.Len() != 1 {
		t.Fatalf("got queue length %d, want 1", q.Len())
	}
}
