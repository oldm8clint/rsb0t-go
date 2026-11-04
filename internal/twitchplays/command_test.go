package twitchplays

import (
	"testing"
	"time"
)

func TestValidateRejectsUnboundedActions(t *testing.T) {
	if Validate(Command{Action: "exec", Author: "viewer", MessageID: "1"}) {
		t.Fatal("unexpectedly accepted unsafe action")
	}
	if Validate(Command{Action: "walk", Author: "", MessageID: "1"}) {
		t.Fatal("accepted command without author")
	}
}

func TestQueueIsBoundedFIFO(t *testing.T) {
	q := NewQueue(2)
	base := Command{Action: "wait", Author: "viewer", MessageID: "1", Received: time.Unix(1, 0)}
	if !q.Push(base) {
		t.Fatal("first push failed")
	}
	base.MessageID = "2"
	if !q.Push(base) {
		t.Fatal("second push failed")
	}
	base.MessageID = "3"
	if q.Push(base) {
		t.Fatal("queue exceeded configured bound")
	}
	got, ok := q.Pop()
	if !ok || got.MessageID != "1" {
		t.Fatalf("unexpected FIFO result: %+v", got)
	}
}
