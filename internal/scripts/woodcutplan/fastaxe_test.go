package woodcutplan

import "testing"

// The owner's rule, 2026-09-22: "accounts with 60+ woodcutting and a rune axe should be moving
// onto yew logs... i set the levels to move on higher on purpose but i did say it should only
// come in place if they dont have a rune axe."

func TestARuneAxeOpensYewAtSixty(t *testing.T) {
	if got := CurrentTierWith(60, "Magic", false).Label; got != "Maple" {
		t.Fatalf("without a rune axe, Woodcutting 60 = %q, want Maple (the raised gate holds)", got)
	}
	if got := CurrentTierWith(60, "Magic", true).Label; got != "Yew" {
		t.Fatalf("with a rune axe, Woodcutting 60 = %q, want Yew", got)
	}
}

func TestARuneAxeOpensMagicAtSeventyFive(t *testing.T) {
	if got := CurrentTierWith(75, "Magic", false).Label; got != "Yew" {
		t.Fatalf("without a rune axe, 75 = %q, want Yew", got)
	}
	if got := CurrentTierWith(75, "Magic", true).Label; got != "Magic" {
		t.Fatalf("with a rune axe, 75 = %q, want Magic", got)
	}
}

// It can only ever move an account UP. StartAt >= Level is the invariant that makes the
// exemption safe, so the exempt tier is never below the ordinary one at any level.
func TestTheExemptionNeverChoosesALowerTier(t *testing.T) {
	for _, target := range []string{"Willow", "Maple", "Yew", "Magic"} {
		for level := 1; level <= 99; level++ {
			slow := CurrentTierIndexWith(level, target, false)
			fast := CurrentTierIndexWith(level, target, true)
			if fast < slow {
				t.Fatalf("level %d target %q: rune axe chose tier %d, below the ordinary %d",
					level, target, fast, slow)
			}
		}
	}
}

// ...and never onto a tree the server refuses: the exempt gate IS the content requirement.
func TestTheExemptTierIsAlwaysLegalForTheLevel(t *testing.T) {
	for _, target := range []string{"Willow", "Maple", "Yew", "Magic"} {
		for level := 1; level <= 99; level++ {
			tier := CurrentTierWith(level, target, true)
			if level < tier.Level {
				t.Fatalf("level %d target %q chose %q, which content requires %d for",
					level, target, tier.Label, tier.Level)
			}
		}
	}
}

// The old entry point must be untouched, because wcfletchplan's pairing round-trips through it.
func TestTheUngatedEntryPointIsUnchanged(t *testing.T) {
	for _, target := range []string{"Willow", "Maple", "Yew", "Magic"} {
		for level := 1; level <= 99; level++ {
			if CurrentTierIndex(level, target) != CurrentTierIndexWith(level, target, false) {
				t.Fatalf("CurrentTierIndex drifted from the unexempt gate at level %d, target %q", level, target)
			}
		}
	}
}

func TestFastAxeHeldRecognisesTheRuneAxeOnly(t *testing.T) {
	if !FastAxeHeld([]string{"Bronze axe", "rune axe"}) {
		t.Error("a rune axe in the pack was not recognised (case-insensitively)")
	}
	if FastAxeHeld([]string{"Adamant axe", "Mithril axe"}) {
		t.Error("an adamant axe earned the exemption; only the rune axe does")
	}
	if FastAxeHeld(nil) {
		t.Error("no axes at all earned the exemption")
	}
}
