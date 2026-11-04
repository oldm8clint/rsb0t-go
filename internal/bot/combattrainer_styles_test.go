package bot

import (
	"testing"

	"rsb0t-go/internal/scripts/combatplan"
	"rsb0t-go/internal/scripts/combatrunner"
)

// TestBandLevelUsesTheTrainedStatsOwnLevel guards the substitution that makes the melee ladder
// usable by Ranged and Magic at all.
//
// The ladder bands on a level, and for melee that level has always been the lowest of the three
// melee stats. An account that has finished melee is 75/75/75, so a Ranged 1 run banding on
// "lowest melee" would ask the ladder for a rung covering level 75, get BlockedNoBand back, and
// stop with its Ranged goal untouched. This is the test that fails if anyone puts LowestMelee
// back in the general path.
func TestBandLevelUsesTheTrainedStatsOwnLevel(t *testing.T) {
	b := &combatTrainerBot{}
	maxedMelee := combatplan.Levels{Attack: 75, Strength: 75, Defence: 75, Ranged: 1, Magic: 8}

	if got := b.bandLevel(maxedMelee, combatplan.StyleMelee); got != 75 {
		t.Fatalf("melee band = %d, want the lowest melee stat, 75", got)
	}
	if got := b.bandLevel(maxedMelee, combatplan.StyleRanged); got != 1 {
		t.Fatalf("ranged band = %d, want the Ranged level, 1 — banding on melee here is what stops the run", got)
	}
	if got := b.bandLevel(maxedMelee, combatplan.StyleMagic); got != 8 {
		t.Fatalf("magic band = %d, want the Magic level, 8", got)
	}

	// The melee case still picks the LOWEST of the three rather than any one of them.
	uneven := combatplan.Levels{Attack: 60, Strength: 31, Defence: 44}
	if got := b.bandLevel(uneven, combatplan.StyleMelee); got != 31 {
		t.Fatalf("melee band = %d, want 31", got)
	}
}

// TestBandLevelNeverReturnsZero matters because a level of 0 falls below every rung's FromLevel,
// so the ladder would answer BlockedNoBand for an account whose stat the server has not sent yet.
// A stat that has not arrived should read as level 1, the bottom of the ladder, not as "no rung".
func TestBandLevelNeverReturnsZero(t *testing.T) {
	b := &combatTrainerBot{}
	empty := combatplan.Levels{}
	for _, style := range []combatplan.Style{combatplan.StyleMelee, combatplan.StyleRanged, combatplan.StyleMagic} {
		if got := b.bandLevel(empty, style); got < 1 {
			t.Fatalf("%s band = %d for an account with no levels read yet, want at least 1", style, got)
		}
	}
}

func TestSpellsOfElement(t *testing.T) {
	options := combatrunner.SpellOptions()
	if len(options) == 0 {
		t.Fatal("the autocast spell table should not be empty")
	}

	fire := spellsOfElement(options, "Fire")
	if len(fire) == 0 {
		t.Fatal("there are four Fire spells in content 289")
	}
	for _, sp := range fire {
		element, _, _, ok := combatplan.ElementOf(sp.Name)
		if !ok || element != "Fire" {
			t.Fatalf("%q is not a Fire spell", sp.Name)
		}
	}
	// Filtering must actually remove things, or the fallback below is never exercised.
	if len(fire) >= len(options) {
		t.Fatalf("filtering to Fire kept %d of %d spells", len(fire), len(options))
	}
	// An empty element means no preference, which is what makes the fallback path work.
	if got := len(spellsOfElement(options, "")); got != len(options) {
		t.Fatalf("an empty element kept %d of %d spells, want all", got, len(options))
	}
}

// TestChooseSpellFallsBackBelowTheChosenElement is the low-level case that would otherwise leave
// a run casting nothing at all: an account configured for Fire cannot cast any Fire spell until
// Magic 13, because Wind Strike is the only spell a level 1 account has.
func TestChooseSpellFallsBackBelowTheChosenElement(t *testing.T) {
	b := &combatTrainerBot{cfg: combatTrainerSettings{magicElement: "Fire", members: false}}

	spell, staff, _, ok := b.chooseSpell(combatplan.Levels{Magic: 1})
	if !ok {
		t.Fatal("a Magic 1 account should still have Wind Strike to cast")
	}
	if spell.Name != "Wind Strike" {
		t.Fatalf("at Magic 1 got %q, want Wind Strike", spell.Name)
	}
	if staff != "Staff of air" {
		t.Fatalf("the staff must match the spell that was actually chosen, got %q", staff)
	}

	// Once Fire is available the preference takes effect.
	spell, staff, staffRune, ok := b.chooseSpell(combatplan.Levels{Magic: 13})
	if !ok || spell.Name != "Fire Strike" {
		t.Fatalf("at Magic 13 got %q %v, want Fire Strike", spell.Name, ok)
	}
	if staff != "Staff of fire" || staffRune != "Fire rune" {
		t.Fatalf("got staff %q supplying %q, want the staff of fire", staff, staffRune)
	}
}

// TestChooseSpellStopsAtFireBlast is the blood-rune wall seen from the bot's side: at Magic 75 on
// a members world every Wave is castable, and choosing one would leave the run autocasting a
// spell whose rune has exactly one source in the build, behind a Magic 66 door.
func TestChooseSpellStopsAtFireBlast(t *testing.T) {
	b := &combatTrainerBot{cfg: combatTrainerSettings{magicElement: "Fire", members: true}}
	spell, _, _, ok := b.chooseSpell(combatplan.Levels{Magic: 75})
	if !ok {
		t.Fatal("expected a spell at Magic 75")
	}
	if spell.Name != "Fire Blast" {
		t.Fatalf("got %q at Magic 75, want Fire Blast: the Waves need blood runes no reachable shop sells", spell.Name)
	}
}

func TestDescribeTraining(t *testing.T) {
	b := &combatTrainerBot{cfg: combatTrainerSettings{goals: combatplan.DefaultGoals()}}
	l := combatplan.Levels{Attack: 40, Strength: 20, Defence: 30, Ranged: 5, Magic: 5}

	if got := b.describeTraining(l, combatplan.StyleRanged); got != "ranged" {
		t.Fatalf("ranged described as %q", got)
	}
	if got := b.describeTraining(l, combatplan.StyleMagic); got != "magic" {
		t.Fatalf("magic described as %q", got)
	}
	// Melee names the individual stat, because that is the thing that rotates.
	if got := b.describeTraining(l, combatplan.StyleMelee); got != "strength" {
		t.Fatalf("melee described as %q, want the stat furthest from its goal", got)
	}
}

// TestMeleeOnlySettingsStayMeleeOnly is the backward-compatibility guard for every proof and
// fleet config written before Ranged and Magic existed. Setting both goals to 0 must produce a
// run that finishes on melee alone rather than one that meets its melee goals and carries on.
func TestMeleeOnlySettingsStayMeleeOnly(t *testing.T) {
	s := resolveTrainer(map[string]interface{}{
		"goalAttack": 40.0, "goalStrength": 40.0, "goalDefence": 40.0,
		"goalRanged": 0.0, "goalMagic": 0.0, "goalPrayer": 1.0,
	})
	if s.goals.Ranged != 0 || s.goals.Magic != 0 {
		t.Fatalf("goals = %+v, want Ranged and Magic switched off", s.goals)
	}
	done := combatplan.Levels{Attack: 40, Strength: 40, Defence: 40}
	if _, ok := combatplan.StyleToTrain(done, s.goals); ok {
		t.Fatal("a melee-only run should have nothing left to train once melee is done")
	}
	if !combatplan.AllGoalsMet(done, s.goals, 1) {
		t.Fatal("a melee-only run should read as finished, so the script logs out for good")
	}
}

// TestMagicElementResolvesFromAString is the panel case, where every value arrives as text.
func TestMagicElementResolvesFromAString(t *testing.T) {
	if got := resolveTrainer(map[string]interface{}{"magicElement": "Earth"}).magicElement; got != "Earth" {
		t.Fatalf("magicElement = %q, want Earth", got)
	}
	// An element the schema does not offer falls back to the default rather than being accepted,
	// which matters because an unknown element would filter the spell list down to nothing.
	if got := resolveTrainer(map[string]interface{}{"magicElement": "Smoke"}).magicElement; got != "Fire" {
		t.Fatalf("magicElement = %q for an element no option matches, want the Fire default", got)
	}
}
