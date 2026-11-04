package gamestate

import (
	"testing"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

func synthSound(s *State, id int) {
	p := packet.NewWriter(8)
	p.P2(uint16(id))
	p.P1(1)
	p.P2(0)
	s.Handle(serverprot.SYNTH_SOUND, p.Data[:p.Pos])
}

func TestSynthSoundIsRecordedAgainstTheTick(t *testing.T) {
	s := New()
	if heard, any := s.HeardSoundNear(SoundTriffidAppear, 0, 3); heard || any {
		t.Fatalf("fresh state heard=%v any=%v", heard, any)
	}
	synthSound(s, SoundSwarmAppear)
	if heard, any := s.HeardSoundNear(SoundTriffidAppear, s.Tick(), 3); heard || !any {
		t.Fatalf("after a different sound: heard=%v any=%v", heard, any)
	}
	synthSound(s, SoundTriffidAppear)
	if heard, _ := s.HeardSoundNear(SoundTriffidAppear, s.Tick(), 3); !heard {
		t.Fatal("triffid_appear this tick not heard")
	}
	if heard, _ := s.HeardSoundNear(SoundTriffidAppear, s.Tick()+10, 3); heard {
		t.Fatal("a sound ten ticks away counted as near")
	}
}
