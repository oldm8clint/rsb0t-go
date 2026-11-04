package controlpanel

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestPublishSequenceAndOfflineHistory(t *testing.T) {
	h := newHub(nil, Config{EventHistory: 16, LogHistory: 16}, testOptions())
	for _, kind := range []string{kindEvents, KindLog, KindChat} {
		h.publish(Event{Account: "fisher01", Kind: kind, Text: "offline"})
	}
	replay := h.keptAfter(0, "")
	if len(replay) != 3 {
		t.Fatalf("offline history length = %d, want 3", len(replay))
	}
	for i, ev := range replay {
		if ev.Seq != uint64(i+1) || ev.Text != "offline" {
			t.Fatalf("offline event %d: %+v", i, ev)
		}
	}
	s := &stream{queue: make(chan []byte, 1)}
	h.streams[s] = struct{}{}
	h.publish(Event{Account: "fisher01", Text: "live", Seq: 999})
	frame := <-s.queue
	if !bytes.HasPrefix(frame, []byte("id: 4\n")) {
		t.Fatalf("wrong frame ID: %s", frame)
	}
	_, data, ok := bytes.Cut(frame, []byte("data: "))
	var ev Event
	if !ok || json.Unmarshal(bytes.TrimSpace(data), &ev) != nil {
		t.Fatalf("invalid event frame: %s", frame)
	}
	if ev.Seq != 4 || ev.Text != "live" {
		t.Fatalf("live JSON must match frame sequence: %+v", ev)
	}
	if replay := h.keptAfter(3, ""); len(replay) != 1 || replay[0] != ev {
		t.Fatalf("live and replay differ: %+v / %+v", ev, replay)
	}
}

func BenchmarkPublishWithoutSubscribers(b *testing.B) {
	h := newHub(nil, Config{EventHistory: 16, LogHistory: 16}, testOptions())
	ev := Event{Account: "fisher01", Text: "inventory updated", Kind: kindEvents}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.publish(ev)
	}
}
