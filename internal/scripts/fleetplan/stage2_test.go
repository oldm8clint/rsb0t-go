package fleetplan

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/thieverplan"
	"rsb0t-go/internal/scripts/tile"
)

// stallStandTile is one of the Baker's stall's own stand tiles, out of the merchant's line of
// sight. Note it is inside BOTH rungs' leashes — see
// TestTheStallAndKnightRungsDeliberatelyOverlap.
func stallStandTile() *tile.Tile {
	t := BakersStall().Stands[0]
	return &t
}

// knightTile is the Knight rung's own anchor, the middle of the East Ardougne market. Also
// inside both leashes, for the same reason.
func knightTile() *tile.Tile {
	t := ArdougneKnight().Anchor
	return &t
}

// The one test that would catch a silent retable of the two rungs this stage depends on. The
// numbers are the local Content checkout's own
// (scripts/skill_thieving/configs/pickpocking/pickpocket.dbrow, .../stalls/stealing.dbrow), and
// docs/fleet_expansion_2026-09-18.md's own "Verified Content corrections" subsection names the
// same ones: knights 55, paladins 70, heroes 80.
func TestStage2RungsMatchTheVerifiedContentGates(t *testing.T) {
	stall := BakersStall()
	if stall.Label != "Baker's stall" {
		t.Fatalf("BakersStall = %q, want the Baker's stall row", stall.Label)
	}
	if stall.Level != BakersStallLevel {
		t.Errorf("Baker's stall level = %d, want %d", stall.Level, BakersStallLevel)
	}
	if stall.Kind != thieverplan.KindStall {
		t.Errorf("Baker's stall kind = %q, want a stall", stall.Kind)
	}
	if stall.RespawnTicks != 8 {
		t.Errorf("Baker's stall respawn = %d ticks, want the content's 8", stall.RespawnTicks)
	}
	if len(stall.Stands) < 2 {
		t.Errorf("Baker's stall has %d stand tiles; the stand rotation needs at least 2 to rotate between", len(stall.Stands))
	}

	knight := ArdougneKnight()
	if knight.Label != "Knight of Ardougne" {
		t.Fatalf("ArdougneKnight = %q, want the Knight of Ardougne row", knight.Label)
	}
	if knight.Level != KnightLevel {
		t.Errorf("Knight level = %d, want the content's %d", knight.Level, KnightLevel)
	}
	if knight.Kind != thieverplan.KindPickpocket {
		t.Errorf("Knight kind = %q, want a pickpocket", knight.Kind)
	}
	if knight.StunDamage != 3 {
		t.Errorf("Knight stun damage = %d, want the content's 3 — the whole reason this stage carries food", knight.StunDamage)
	}
	if PaladinLevel != 70 || HeroLevel != 80 {
		t.Errorf("paladin/hero gates = %d/%d, want the content's 70/80", PaladinLevel, HeroLevel)
	}
}

// SuccessChance is ported from this engine's own STAT_RANDOM opcode, so these are arithmetic
// facts about that formula, not estimates — if the port drifts, this catches it.
func TestSuccessChanceMatchesTheEngineFormula(t *testing.T) {
	knight := ArdougneKnight() // success_chance 50,240
	cases := []struct {
		level int
		value int // floor(50*(99-L)/98) + floor(240*(L-1)/98) + 1
	}{
		{1, 51},   // 50*98/98=50, 240*0/98=0, +1
		{55, 155}, // 22 + 132 + 1
		{99, 241}, // 0 + 240 + 1
	}
	for _, c := range cases {
		want := float64(c.value) / 256
		if got := SuccessChance(knight, c.level); got != want {
			t.Errorf("SuccessChance(Knight, %d) = %v, want %v (engine value %d/256)", c.level, got, want, c.value)
		}
	}
	// Out-of-range levels clamp rather than producing nonsense.
	if got := SuccessChance(knight, 0); got != SuccessChance(knight, 1) {
		t.Errorf("SuccessChance below level 1 = %v, want the level-1 value", got)
	}
	if got := SuccessChance(knight, 500); got != SuccessChance(knight, 99) {
		t.Errorf("SuccessChance above 99 = %v, want the level-99 value", got)
	}
	// A stall has no roll at all.
	if got := SuccessChance(BakersStall(), 5); got != 1 {
		t.Errorf("SuccessChance(stall) = %v, want 1 — a stall steal cannot fail", got)
	}
}

// The whole point of classifying: only the silent case is worth walking for.
func TestShouldMoveStandOnlyForTheSilentRefusal(t *testing.T) {
	want := map[StallRefusal]bool{
		StallRefusalUnknown:  true,
		StallRefusalCombat:   false,
		StallRefusalPackFull: false,
		StallRefusalLevel:    false,
		StallRefusalNone:     false,
	}
	for refusal, expect := range want {
		if got := ShouldMoveStand(refusal); got != expect {
			t.Errorf("ShouldMoveStand(%q) = %v, want %v", refusal, got, expect)
		}
	}
}

// The fleet has already lost a production account and 6.7 GB of log to this exact shape (a
// primitive returning instantly inside a loop that re-derives its phase from the result), so the
// numbers that bound it are worth pinning rather than leaving to judgement.
func TestIdleStepFloorBoundsASpinningLoop(t *testing.T) {
	if IdleStepFloor <= 0 {
		t.Fatal("IdleStepFloor is not positive — a spinning dispatch would be unbounded")
	}
	if IdleStepFloor >= GameTick {
		t.Fatalf("IdleStepFloor %v is not under one game tick (%v) — it would slow a loop that is doing real work, including on an accelerated lab engine",
			IdleStepFloor, GameTick)
	}
	// The concrete property: a loop that does nothing cannot exceed this many iterations a
	// second. The live incident this guards against ran at ~14,800/s.
	perSecond := float64(time.Second) / float64(IdleStepFloor)
	if perSecond > 20 {
		t.Errorf("a fully idle dispatch would still run %.0f iterations a second; that is not a floor", perSecond)
	}
}
