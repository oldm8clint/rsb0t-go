package gamestate

import (
	"testing"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

func TestUpdateStat(t *testing.T) {
	p := packet.NewWriter(6)
	p.P1(6)     // Fishing
	p.P4(12345) // 12,345 XP — the wire already carries whole XP, not tenths
	p.P1(37)

	s := New()
	s.Handle(serverprot.UPDATE_STAT, p.Data[:p.Pos])

	sk, ok := s.Skill(6)
	if !ok || sk.Level != 37 || sk.XP != 12345 || sk.Generation != 1 {
		t.Fatalf("unexpected skill: ok=%v skill=%+v", ok, sk)
	}
}

func TestUpdateStatGenerationAndSnapshot(t *testing.T) {
	s := New()
	for _, v := range []struct {
		level int
		xp    int32
	}{{10, 5000}, {11, 7500}} {
		p := packet.NewWriter(6)
		p.P1(0)
		p.P4(v.xp)
		p.P1(byte(v.level))
		s.Handle(serverprot.UPDATE_STAT, p.Data[:p.Pos])
	}

	sk, _ := s.Skill(0)
	if sk.Level != 11 || sk.XP != 7500 || sk.Generation != 2 {
		t.Fatalf("unexpected updated skill: %+v", sk)
	}
	all := s.Skills()
	all[0].Level = 99
	sk, _ = s.Skill(0)
	if sk.Level != 11 {
		t.Fatal("Skills returned mutable internal state")
	}
}

func TestUpdateStatInvalidIndexIgnored(t *testing.T) {
	for _, idx := range []int{-1, SkillCount, 255} {
		p := packet.NewWriter(6)
		p.P1(byte(idx))
		p.P4(100)
		p.P1(1)
		s := New()
		s.Handle(serverprot.UPDATE_STAT, p.Data[:p.Pos])
		if got := s.Skills(); got != (Skills{}) {
			t.Fatalf("invalid stat %d changed state: %+v", idx, got)
		}
	}
}
