package questplan

import (
	"math"
	"strings"
	"testing"
	"time"
)

// pipSnap is a stage-4 snapshot of an account with the given base levels, hitpoints and pack.
func pipSnap(att, str, def, mag, hp, maxHP int, inv map[int]int) Snapshot {
	return Snapshot{
		Journal: StatusInProgress, Stage: 4, StageKnown: true,
		HP: hp, MaxHP: maxHP,
		Levels: map[string]int{"attack": att, "strength": str, "defence": def, "magic": mag, "hitpoints": maxHP},
		InvIDs: inv,
		Failed: map[string]bool{}, Blocked: map[string]bool{}, Ran: map[string]bool{},
	}
}

// Content, priestperil.npc and evil_monks.rs2, hand-checked: the key monk is npc 1046 (npc.pack
// 1046=priestperilevilmonk3), Attack/Strength/Defence/Hitpoints 25, Magic 40, a zap of up to 8 one
// attack in four, and a melee maximum of 3 at Strength 25.
func TestPipKeyMonkIsTheContentOne(t *testing.T) {
	m := PipKeyMonk
	if m.Attack != 25 || m.Strength != 25 || m.Defence != 25 || m.Hitpoints != 25 || m.Magic != 40 || m.ZapMax != 8 || m.ZapOneIn != 4 {
		t.Fatalf("%+v", m)
	}
	if got := npcMaxHit(m.Strength); got != 3 {
		t.Fatalf("melee maximum %d, want 3", got)
	}
	// The other two monks of the name are weaker, which is why only 1046 is worth the gate.
	if npcMaxHit(18) != 3 || npcMaxHit(8) != 2 {
		t.Fatalf("1044 / 1045 maxima %d / %d", npcMaxHit(18), npcMaxHit(8))
	}
	if NpcZamorakKeyMonk != 1046 {
		t.Fatalf("NpcZamorakKeyMonk %d", NpcZamorakKeyMonk)
	}
}

func TestRollHitIsTheContentChance(t *testing.T) {
	// A=1, D=1 (two rolls of 0..1): only (1,0) wins, 1 in 4. A=2, D=1: three of six pairs.
	if got := rollHit(1, 1); math.Abs(got-0.25) > 1e-9 {
		t.Errorf("rollHit(1,1) = %v", got)
	}
	if got := rollHit(2, 1); math.Abs(got-0.5) > 1e-9 {
		t.Errorf("rollHit(2,1) = %v", got)
	}
	if got := rollHit(0, 100); got != 0 {
		t.Errorf("rollHit(0,100) = %v", got)
	}
	// Against a defence far above the attack the chance is about attack/(2*defence).
	if got := rollHit(1000, 100000); got < 0.004 || got > 0.006 {
		t.Errorf("rollHit(1000,100000) = %v", got)
	}
}

// The model against the live fights of 5 Oct 2026. The accounts that killed the monk with no bite
// lost 12 to 21 hitpoints with a sword and armour on (spare28 32 to 15, spare8 33 to 21, acct22
// 36 to 18, Unforgivable's second try 33 to 20); the model, which assumes neither, is a ceiling.
func TestMeleeExchangeAgainstTheMonkIsACeilingOnTheLiveLosses(t *testing.T) {
	// Unforgivable: Attack 35, Strength 33, Defence 25, Magic 43.
	x := MeleeExchange(MeleeFighter{Attack: 35, Strength: 33, Defence: 25, Magic: 43}, PipKeyMonk)
	t.Logf("Unforgivable: hit %.3f max %d per swing %.2f; monk %.2f per attack; kill %.0f ticks; loss %.1f",
		x.OurHitChance, x.OurMaxHit, x.OurPerSwing, x.TheirPerAttack, x.KillTicks, x.DamageTaken)
	if x.OurMaxHit != 4 {
		t.Errorf("max hit %d, want 4", x.OurMaxHit)
	}
	if x.DamageTaken < 21 || x.DamageTaken > 27 {
		t.Errorf("expected loss %.1f outside the 21-27 the hand arithmetic gives", x.DamageTaken)
	}
	if x.DamageTaken < 17 {
		t.Errorf("expected loss %.1f is below the worst live success (17), so it is not a ceiling", x.DamageTaken)
	}
	// The quest's own minimum (Attack 30, Defence 20, no weapon, no armour) loses about 30.
	min := MeleeExchange(MeleeFighter{Attack: 30, Strength: 30, Defence: 20, Magic: 20}, PipKeyMonk)
	t.Logf("minimum: kill %.0f ticks, loss %.1f", min.KillTicks, min.DamageTaken)
	if min.DamageTaken < 27 || min.DamageTaken > 35 {
		t.Errorf("minimum account loses %.1f, want about 31", min.DamageTaken)
	}
	// A better weapon (iron scimitar: +10 attack, +9 strength) shortens it.
	sword := MeleeExchange(MeleeFighter{Attack: 30, Strength: 30, Defence: 20, Magic: 20, AttackBonus: 10, StrengthBonus: 9}, PipKeyMonk)
	if sword.DamageTaken >= min.DamageTaken {
		t.Errorf("a scimitar did not shorten the fight: %.1f against %.1f", sword.DamageTaken, min.DamageTaken)
	}
	// An account whose attack roll is nothing (a -64 attack bonus zeroes it) never hurts the foe: an
	// endless fight, not a short one.
	if got := MeleeExchange(MeleeFighter{Attack: 1, Strength: 1, AttackBonus: -64}, MeleeFoe{Defence: 1, Hitpoints: 10}); !math.IsInf(got.DamageTaken, 1) {
		t.Errorf("an account that cannot hit: %v", got.DamageTaken)
	}
}

func TestPipMonkReadyNeedsFoodForASmallAccount(t *testing.T) {
	// Attack 30, Strength 30, Defence 20, Magic 20, 30 hitpoints.
	if v := PipMonkReady(pipSnap(30, 30, 20, 20, 30, 30, nil)); v.OK || !strings.Contains(v.Why, "hitpoints") {
		t.Fatalf("a minimum account with no food: %+v", v)
	}
	if v := PipMonkReady(pipSnap(30, 30, 20, 20, 30, 30, map[int]int{379: 4})); !v.OK {
		t.Fatalf("four lobsters: %+v", v)
	}
	if v := PipMonkReady(pipSnap(30, 30, 20, 20, 30, 30, map[int]int{379: 1})); v.OK {
		t.Fatalf("one lobster is not enough: %+v", v)
	}
	// Food that is not on the quest's list is not counted.
	if v := PipMonkReady(pipSnap(30, 30, 20, 20, 30, 30, map[int]int{1234: 20})); v.OK {
		t.Fatalf("unknown items counted as food: %+v", v)
	}
}

func TestPipMonkReadyLetsAnOrdinaryStage5AccountIn(t *testing.T) {
	// Unforgivable at the live arrival: 24/33, a pack of sharks and lobsters.
	if v := PipMonkReady(pipSnap(35, 33, 25, 43, 24, 33, map[int]int{385: 12, 379: 8})); !v.OK {
		t.Fatalf("%+v", v)
	}
	// The same account with no food at 33/33 is not safe for a 2x margin (loss about 23).
	if v := PipMonkReady(pipSnap(35, 33, 25, 43, 33, 33, nil)); v.OK {
		t.Fatalf("full hitpoints and no food: %+v", v)
	}
}

func TestPipMonkReadyDoesNotJudgeAnUnreadAccount(t *testing.T) {
	if v := PipMonkReady(Snapshot{}); !v.OK {
		t.Fatalf("%+v", v)
	}
	s := pipSnap(35, 33, 25, 43, 24, 33, nil)
	s.Levels = map[string]int{}
	if v := PipMonkReady(s); !v.OK {
		t.Fatalf("no levels read: %+v", v)
	}
}

// Every food the quest eats has a heal figure, and only those.
func TestPipFoodHealCoversExactlyThePipFoods(t *testing.T) {
	for _, id := range pipFoods {
		if pipFoodHeal[id] <= 0 {
			t.Errorf("food %d has no heal figure", id)
		}
	}
	if len(pipFoodHeal) != len(pipFoods) {
		t.Errorf("%d heal figures for %d foods", len(pipFoodHeal), len(pipFoods))
	}
}

// At stage 4, after the guardian, the monk step needs the food kit first (a death or a lost fight
// empties the pack and acct50 went back to the temple with nothing to eat, twice), then the odds.
func TestDecideStage4ProvisionsFoodBeforeTheMonk(t *testing.T) {
	s := pipSnap(35, 33, 25, 43, 33, 33, nil)
	step := DecidePriestInPeril(s)
	if step.Kind != StepProvision || step.KitName != KitPipFood {
		t.Fatalf("%s", step.Describe())
	}
	// The kit ran and the pack has food: the monk.
	s.Ran["provision:"+KitPipFood] = true
	s.InvIDs = map[int]int{379: 4, 385: 2}
	if step = DecidePriestInPeril(s); step.Kind != StepCustom || step.Custom != CustomPipMonk {
		t.Fatalf("%s", step.Describe())
	}
	// The kit ran and found nothing: the account waits (and parks) instead of walking in.
	s.InvIDs = nil
	if step = DecidePriestInPeril(s); step.Kind != StepWait || !strings.Contains(step.Reason, "Monk of Zamorak") {
		t.Fatalf("%s: %q", step.Describe(), step.Reason)
	}
	// The kit failed on the way, but the pack holds food: go.
	delete(s.Ran, "provision:"+KitPipFood)
	s.Failed["provision:"+KitPipFood] = true
	s.InvIDs = map[int]int{379: 6}
	if step = DecidePriestInPeril(s); step.Kind != StepCustom || step.Custom != CustomPipMonk {
		t.Fatalf("%s", step.Describe())
	}
	// A lost key at stage 5 goes through the same gate.
	s.Stage = 5
	s.InvIDs = map[int]int{ObjBucket: 1, 379: 6}
	if step = DecidePriestInPeril(s); step.Kind != StepCustom || step.Custom != CustomPipMonk {
		t.Fatalf("stage 5, key lost: %s", step.Describe())
	}
	s.InvIDs = map[int]int{ObjBucket: 1}
	s.Failed["provision:"+KitPipFood] = true
	if step = DecidePriestInPeril(s); step.Kind != StepWait {
		t.Fatalf("stage 5, key lost, no food: %s", step.Describe())
	}
	// Holding the key, none of this applies.
	s.InvIDs = map[int]int{ObjGoldenKey: 1}
	s.Stage = 4
	if step = DecidePriestInPeril(s); step.Custom != CustomPipCellTalk {
		t.Fatalf("with the key: %s", step.Describe())
	}
}

// Two deaths at the monk inside a day skip the quest for a day.
func TestPipMonkStepHasADeathCap(t *testing.T) {
	c, ok := PriestInPeril.StepDeathCaps[CustomPipMonk]
	if !ok || c.Max != 2 || c.Window != 24*time.Hour || c.Skip != 24*time.Hour {
		t.Fatalf("%+v %v", c, ok)
	}
}
