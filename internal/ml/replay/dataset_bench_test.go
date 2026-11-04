package replay

import (
	"strings"
	"testing"
)

func BenchmarkDatasetReaderNextSmall(b *testing.B) {
	const episode = `{"steps":[]}`
	benchmarkDatasetReader(b, episode, 1024)
}

func BenchmarkDatasetReaderNextLarge(b *testing.B) {
	const payloadSize = 64 << 10
	var sb strings.Builder
	sb.Grow(payloadSize + 32)
	sb.WriteString(`{"steps":[],"payload":"`)
	for sb.Len() < payloadSize {
		sb.WriteByte('x')
	}
	sb.WriteString(`"}`)
	benchmarkDatasetReader(b, sb.String(), 1024)
}

func benchmarkDatasetReader(b *testing.B, episode string, maxEpisodes int) {
	input := strings.Repeat(episode+"\n", maxEpisodes)
	b.SetBytes(int64(len(input)))
	b.ReportMetric(float64(len(input)), "input_bytes/op")
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		reader := NewBoundedDatasetReader(strings.NewReader(input), int64(len(episode)), maxEpisodes)
		for {
			_, err := reader.Next()
			if err != nil {
				break
			}
		}
	}
}
