package gamestate

import (
	"testing"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/levels"
)

func statPacket(stat int, xp int32, level int) []byte {
	p := packet.NewWriter(6)
	p.P1(byte(stat))
	p.P4(xp)
	p.P1(byte(level))
	return p.Data[:p.Pos]
}

func TestHPBeforeAnyUpdateStat(t *testing.T) {
	s := New()
	if hp, ok := s.HP(); ok || hp != 0 {
		t.Fatalf("expected no HP yet, got hp=%d ok=%v", hp, ok)
	}
	if max, ok := s.MaxHP(); ok || max != 0 {
		t.Fatalf("expected no MaxHP yet, got max=%d ok=%v", max, ok)
	}
}

func TestHPTracksLiveDamageAndHealing(t *testing.T) {
	s := New()

	// First sighting at full health: level 10 becomes both HP and the MaxHP high-water
	// mark. XP is pinned to exactly level 10's own threshold so it doesn't push the
	// XP-implied side of MaxHP's max(seen, xpLevel) above the observed level.
	level10XP := int32(levels.XPAtLevel(10))
	s.Handle(serverprot.UPDATE_STAT, statPacket(HitpointsStat, level10XP, 10))

	if hp, ok := s.HP(); !ok || hp != 10 {
		t.Fatalf("expected hp=10, got hp=%d ok=%v", hp, ok)
	}
	if max, ok := s.MaxHP(); !ok || max != 10 {
		t.Fatalf("expected maxHP=10, got max=%d ok=%v", max, ok)
	}

	// Take a hit: current level (HP) drops to 4, XP unchanged — MaxHP must stay 10.
	s.Handle(serverprot.UPDATE_STAT, statPacket(HitpointsStat, level10XP, 4))
	if hp, ok := s.HP(); !ok || hp != 4 {
		t.Fatalf("expected hp=4 after damage, got hp=%d ok=%v", hp, ok)
	}
	if max, ok := s.MaxHP(); !ok || max != 10 {
		t.Fatalf("expected maxHP still 10 after damage, got max=%d ok=%v", max, ok)
	}

	// Heal back to full.
	s.Handle(serverprot.UPDATE_STAT, statPacket(HitpointsStat, level10XP, 10))
	if hp, ok := s.HP(); !ok || hp != 10 {
		t.Fatalf("expected hp=10 after healing, got hp=%d ok=%v", hp, ok)
	}
}

// TestMaxHPIsHighWaterMarkNotXPLevel is a regression test for a real bug found live
// against the local engine (cmd/combatstatetest, account loctest1): a fresh login's own
// UPDATE_STAT for Hitpoints reported current level 10 with an XP value that decodes (via
// the standard level-from-XP curve every other skill uses) to only level 2. Deriving
// MaxHP from XP would report a max lower than the current value it's supposed to bound —
// clearly wrong. MaxHP must track the observed high-water mark instead (see health.go's
// package doc comment for the full story and the sibling-project precedent).
func TestMaxHPIsHighWaterMarkNotXPLevel(t *testing.T) {
	s := New()
	// xp=115, which XPAtLevel-inversion would place at level 2 — far below the real
	// current level 10 in the same packet.
	s.Handle(serverprot.UPDATE_STAT, statPacket(HitpointsStat, 115, 10))

	if hp, ok := s.HP(); !ok || hp != 10 {
		t.Fatalf("expected hp=10, got hp=%d ok=%v", hp, ok)
	}
	if max, ok := s.MaxHP(); !ok || max != 10 {
		t.Fatalf("expected maxHP=10 (observed high-water mark), got max=%d ok=%v", max, ok)
	}
}

func TestMaxHPTracksRealLevelUps(t *testing.T) {
	s := New()
	s.Handle(serverprot.UPDATE_STAT, statPacket(HitpointsStat, 115, 10))
	// A real Hitpoints level-up to 15 (full health at the new level). XP must actually
	// correspond to level 15 on the real curve now that MaxHP also considers the
	// XP-implied level (see TestMaxHPTakesTheHigherOfHighWaterMarkAndXPLevel) — an
	// arbitrary "comfortably above 15" value like the old 50000 (which the real curve
	// places at level 20, not 15) would imply a higher level than this test intends and
	// fail for the right reason, not a bug.
	level15XP := int32(levels.XPAtLevel(15))
	s.Handle(serverprot.UPDATE_STAT, statPacket(HitpointsStat, level15XP, 15))
	if max, ok := s.MaxHP(); !ok || max != 15 {
		t.Fatalf("expected maxHP=15 after a real level-up, got max=%d ok=%v", max, ok)
	}
	// Taking damage down to 5 must not lower the high-water mark.
	s.Handle(serverprot.UPDATE_STAT, statPacket(HitpointsStat, level15XP, 5))
	if hp, ok := s.HP(); !ok || hp != 5 {
		t.Fatalf("expected hp=5, got hp=%d ok=%v", hp, ok)
	}
	if max, ok := s.MaxHP(); !ok || max != 15 {
		t.Fatalf("expected maxHP still 15 after damage, got max=%d ok=%v", max, ok)
	}
}

// TestMaxHPTakesTheHigherOfHighWaterMarkAndXPLevel covers the opposite gap from
// TestMaxHPIsHighWaterMarkNotXPLevel: a Hitpoints level-up that happens while the player
// is damaged (not observed at the new, higher current value) still raises the XP-implied
// level immediately, even though hpMaxSeen hasn't caught up yet. MaxHP must report the
// higher of the two rather than waiting for a future full-health packet.
func TestMaxHPTakesTheHigherOfHighWaterMarkAndXPLevel(t *testing.T) {
	s := New()
	// Seen at full health, level 10 (both the current value and the high-water mark).
	s.Handle(serverprot.UPDATE_STAT, statPacket(HitpointsStat, 115, 10))

	// A real level-up to 15 arrives while damaged: the packet's current level is still
	// only 8 (post-damage), but its XP now implies level 15 — hpMaxSeen stays at 10
	// (8 < 10, no new high-water mark), yet the account's real max HP is genuinely 15.
	level15XP := int32(levels.XPAtLevel(15))
	s.Handle(serverprot.UPDATE_STAT, statPacket(HitpointsStat, level15XP, 8))

	if hp, ok := s.HP(); !ok || hp != 8 {
		t.Fatalf("expected hp=8, got hp=%d ok=%v", hp, ok)
	}
	if max, ok := s.MaxHP(); !ok || max != 15 {
		t.Fatalf("expected maxHP=15 (XP-implied level, ahead of the stale high-water mark), got max=%d ok=%v", max, ok)
	}
}

func TestHPIgnoresOtherSkills(t *testing.T) {
	s := New()
	// Agility (stat 16) gains a level; Hitpoints must remain unknown.
	s.Handle(serverprot.UPDATE_STAT, statPacket(16, 500, 5))
	if hp, ok := s.HP(); ok || hp != 0 {
		t.Fatalf("expected HP still unknown, got hp=%d ok=%v", hp, ok)
	}
	if max, ok := s.MaxHP(); ok || max != 0 {
		t.Fatalf("expected MaxHP still unknown, got max=%d ok=%v", max, ok)
	}
}

func TestDeathDetection(t *testing.T) {
	s := New()
	if s.IsDead() {
		t.Fatal("expected not dead before any message")
	}

	p := packet.NewWriter(64)
	p.PJStr("Welcome to RuneScape.")
	s.Handle(serverprot.MESSAGE_GAME, p.Data[:p.Pos])
	if s.IsDead() {
		t.Fatal("unrelated chat should not mark death")
	}

	p2 := packet.NewWriter(64)
	p2.PJStr("Oh dear you are dead!")
	s.Handle(serverprot.MESSAGE_GAME, p2.Data[:p2.Pos])
	if !s.IsDead() {
		t.Fatal("expected death line to mark IsDead")
	}

	s.ClearDead()
	if s.IsDead() {
		t.Fatal("expected ClearDead to reset IsDead")
	}
}

// TestDeathDetectionExactGameString double-checks against the exact string this engine's
// own content sends (content/scripts/player/scripts/death.rs2: mes("Oh dear you are dead!")
// — no comma, unlike the more commonly quoted "Oh dear, you are dead!").
func TestDeathDetectionExactGameString(t *testing.T) {
	s := New()
	p := packet.NewWriter(64)
	p.PJStr("Oh dear you are dead!")
	s.Handle(serverprot.MESSAGE_GAME, p.Data[:p.Pos])
	if !s.IsDead() {
		t.Fatal("expected the engine's exact death line to mark IsDead")
	}
}
