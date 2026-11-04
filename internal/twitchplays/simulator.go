package twitchplays

import (
	"time"
)

// ChatEvent is the platform-neutral event produced by a chat source or replay.
type ChatEvent struct {
	MessageID string
	Author    string
	Text      string
	Source    string
	Received  time.Time
}

// ReplaySource makes recorded chat deterministic and testable without a live platform.
type ReplaySource struct {
	events []ChatEvent
	index  int
}

func NewReplaySource(events []ChatEvent) *ReplaySource {
	return &ReplaySource{events: append([]ChatEvent(nil), events...)}
}

func (r *ReplaySource) Next() (ChatEvent, bool) {
	if r.index >= len(r.events) {
		return ChatEvent{}, false
	}
	e := r.events[r.index]
	r.index++
	return e, true
}

// ReplayQueue parses and queues all replay events, preserving source ordering.
func ReplayQueue(events []ChatEvent, maxQueue int) *Queue {
	q := NewQueue(maxQueue)
	for _, e := range events {
		if c, ok := Parse(e.Text, e.Source, e.Author, e.MessageID, e.Received); ok {
			_ = q.Push(c)
		}
	}
	return q
}
