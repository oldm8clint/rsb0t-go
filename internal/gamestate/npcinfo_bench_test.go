package gamestate

import (
	"testing"

	"rsb0t-go/internal/protocol/serverprot"
)

// npcInfoPackets is a first NPC_INFO that adds n NPCs near the player and a steady one that keeps them
// all with no update: what a busy tile sends every game tick.
func npcInfoPackets(n int) (first, steady []byte) {
	f := []bitField{{8, 0}}
	for i := 0; i < n; i++ {
		f = append(f, bitField{14, uint32(i + 1)}, bitField{11, uint32(100 + i%7)}, bitField{5, uint32(i % 15)}, bitField{5, uint32((i * 3) % 15)}, bitField{1, 0}, bitField{1, 0})
	}
	f = append(f, bitField{14, 16383})
	total := 0
	for _, x := range f {
		total += x.n
	}
	first = bitPack((total+7)/8+4, f...)

	g := []bitField{{8, uint32(n)}}
	for i := 0; i < n; i++ {
		g = append(g, bitField{1, 0})
	}
	g = append(g, bitField{14, 16383})
	total = 0
	for _, x := range g {
		total += x.n
	}
	steady = bitPack((total+7)/8+4, g...)
	return first, steady
}

func newNpcState(tb testing.TB, n int) (*State, []byte) {
	s := newZoneState()
	first, steady := npcInfoPackets(n)
	s.Handle(serverprot.NPC_INFO, first)
	if got := len(s.npcs); got != n {
		tb.Fatalf("%d npcs tracked after the first packet, want %d", got, n)
	}
	s.Handle(serverprot.NPC_INFO, steady)
	if got := len(s.npcs); got != n {
		tb.Fatalf("%d npcs tracked after a steady packet, want %d", got, n)
	}
	return s, steady
}

// BenchmarkNpcInfoSteady is the per-tick cost of keeping 40 visible NPCs: every account pays it every 600 ms.
func BenchmarkNpcInfoSteady(b *testing.B) {
	s, steady := newNpcState(b, 40)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Handle(serverprot.NPC_INFO, steady)
	}
}

func TestNpcInfoSteadyKeepsAndDropsNpcs(t *testing.T) {
	s, steady := newNpcState(t, 12)
	// A packet that keeps only 5 of the 12 and removes the rest (type 3 = removed) must drop them from s.npcs.
	f := []bitField{{8, 12}}
	for i := 0; i < 12; i++ {
		if i < 5 {
			f = append(f, bitField{1, 0})
		} else {
			f = append(f, bitField{1, 1}, bitField{2, 3})
		}
	}
	f = append(f, bitField{14, 16383})
	total := 0
	for _, x := range f {
		total += x.n
	}
	s.Handle(serverprot.NPC_INFO, bitPack((total+7)/8+4, f...))
	if got := len(s.npcs); got != 5 {
		t.Fatalf("%d npcs left, want 5", got)
	}
	_ = steady
}

// playerInfoPackets is a first PLAYER_INFO that adds n other players and a steady one that keeps them all.
func playerInfoPackets(n int) (first, steady []byte) {
	f := []bitField{{1, 0}, {8, 0}} // no local update; no player seen before
	for i := 0; i < n; i++ {
		f = append(f, bitField{11, uint32(i + 1)}, bitField{5, uint32(i % 15)}, bitField{5, uint32((i * 3) % 15)}, bitField{1, 0}, bitField{1, 0})
	}
	f = append(f, bitField{11, 2047})
	total := 0
	for _, x := range f {
		total += x.n
	}
	first = bitPack((total+7)/8+4, f...)

	g := []bitField{{1, 0}, {8, uint32(n)}}
	for i := 0; i < n; i++ {
		g = append(g, bitField{1, 0})
	}
	g = append(g, bitField{11, 2047})
	total = 0
	for _, x := range g {
		total += x.n
	}
	steady = bitPack((total+7)/8+4, g...)
	return first, steady
}

// BenchmarkPlayerInfoSteady is the per-tick cost of keeping 25 visible players.
func BenchmarkPlayerInfoSteady(b *testing.B) {
	s := newZoneState()
	first, steady := playerInfoPackets(25)
	s.Handle(serverprot.PLAYER_INFO, first)
	if got := len(s.players); got != 25 {
		b.Fatalf("%d players tracked after the first packet, want 25", got)
	}
	s.Handle(serverprot.PLAYER_INFO, steady)
	if got := len(s.players); got != 25 {
		b.Fatalf("%d players tracked after a steady packet, want 25", got)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Handle(serverprot.PLAYER_INFO, steady)
	}
}

func TestPlayerInfoSteadyDropsPlayersTheServerRemoves(t *testing.T) {
	s := newZoneState()
	first, _ := playerInfoPackets(9)
	s.Handle(serverprot.PLAYER_INFO, first)
	f := []bitField{{1, 0}, {8, 9}}
	for i := 0; i < 9; i++ {
		if i < 4 {
			f = append(f, bitField{1, 0})
		} else {
			f = append(f, bitField{1, 1}, bitField{2, 3})
		}
	}
	f = append(f, bitField{11, 2047})
	total := 0
	for _, x := range f {
		total += x.n
	}
	s.Handle(serverprot.PLAYER_INFO, bitPack((total+7)/8+4, f...))
	if got := len(s.players); got != 4 {
		t.Fatalf("%d players left, want 4", got)
	}
}
