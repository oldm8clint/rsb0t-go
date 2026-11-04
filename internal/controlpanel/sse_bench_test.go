package controlpanel

import "testing"

func BenchmarkSSEFrameJSON(b *testing.B) {
	data := []byte(`{"account":"fisher01","state":"ingame","tick":12345,"xp":987654}`)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		_ = sseFrame(sseEvent, uint64(i+1), data)
	}
}

func BenchmarkSSEFrameMultiline(b *testing.B) {
	data := []byte("first\nsecond\r\nthird\rfourth")
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		_ = sseFrame(sseEvent, uint64(i+1), data)
	}
}

func BenchmarkEventFrame(b *testing.B) {
	ev := Event{Seq: 1, Account: "fisher01", Text: "inventory updated", Kind: kindEvents}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ev.Seq = uint64(i + 1)
		_ = eventFrame(ev)
	}
}
