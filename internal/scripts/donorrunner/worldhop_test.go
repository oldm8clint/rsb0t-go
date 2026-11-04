package donorrunner

import (
	"fmt"
	"testing"
)

// These test the hop hooks in isolation from Run, which needs a live session. What they pin
// is the contract the rest of the codebase relies on: hopping is OFF unless all of it is
// wired, "unknown" never becomes "world 1", and a refused hop is not a failed hand-off.

func TestHoppingIsOffUnlessEveryHookIsWired(t *testing.T) {
	// The shape every existing caller is in: no hooks at all.
	var d Deps
	if d.hopEnabled() {
		t.Fatal("a Deps with no hooks must not hop")
	}
	if hopping, _ := d.hopToMule("mulecon"); hopping {
		t.Fatal("an unwired Deps asked for a hop")
	}
	if hopping, _ := d.hopHome(); hopping {
		t.Fatal("an unwired Deps asked to hop home")
	}

	// Two of three is still off — a partial wiring must not half-work.
	d.SetWorlds(1, 1, func(string) (int, bool) { return 2, true }, nil)
	if d.hopEnabled() {
		t.Fatal("a nil RequestHop must leave hopping off")
	}
	d.SetWorlds(0, 0, func(string) (int, bool) { return 2, true }, func(int, string) error { return nil })
	if d.hopEnabled() {
		t.Fatal("world 0 must leave hopping off")
	}
}

func TestHopsWhenTheMuleIsElsewhereAndNotWhenItIsHere(t *testing.T) {
	var asked []int
	d := &Deps{}
	d.SetWorlds(1, 1,
		func(string) (int, bool) { return 2, true },
		func(w int, _ string) error { asked = append(asked, w); return nil },
	)
	hopping, reason := d.hopToMule("mulecon")
	if !hopping {
		t.Fatal("the mule is on world 2 and this donor on world 1; it must hop")
	}
	if len(asked) != 1 || asked[0] != 2 {
		t.Fatalf("asked to hop to %v, want [2]", asked)
	}
	if reason == "" {
		t.Error("a hop must carry a reason for the Result")
	}

	asked = nil
	d.SetWorlds(2, 1, func(string) (int, bool) { return 2, true }, func(w int, _ string) error { asked = append(asked, w); return nil })
	if hopping, _ := d.hopToMule("mulecon"); hopping {
		t.Fatal("the mule is already on this world; hopping to it is a wasted pair of logins")
	}
	if len(asked) != 0 {
		t.Fatalf("a no-op hop still asked: %v", asked)
	}
}

// The single most dangerous default in this whole feature: "nobody has said which world the
// mule is on" must NOT become "world 1". A donor that hops on that reading spends two logins
// on no evidence, and on a single-world fleet nothing publishes a world at all, so this is
// the permanent state there.
func TestAnUnknownMuleWorldNeverCausesAHop(t *testing.T) {
	var asked []int
	d := &Deps{}
	d.SetWorlds(2, 2,
		func(string) (int, bool) { return 0, false },
		func(w int, _ string) error { asked = append(asked, w); return nil },
	)
	if hopping, _ := d.hopToMule("mulecon"); hopping {
		t.Fatal("hopped on an unknown mule world")
	}
	if len(asked) != 0 {
		t.Fatalf("asked to hop to %v on no evidence", asked)
	}
}

// A refused hop (the fleet says this deployment has one world, say) must leave the donor
// getting on with the hand-off it was already going to attempt, not treating it as a failure.
func TestARefusedHopFallsBackToAnOrdinaryHandOff(t *testing.T) {
	var logged []string
	d := &Deps{Log: func(s string) { logged = append(logged, s) }}
	d.SetWorlds(1, 1,
		func(string) (int, bool) { return 2, true },
		func(int, string) error { return fmt.Errorf("fleet: this deployment has only one world") },
	)
	hopping, reason := d.hopToMule("mulecon")
	if hopping {
		t.Fatal("a refused hop must not report hopping")
	}
	if reason != "" {
		t.Fatalf("a refused hop returned a reason %q; the run continues as normal", reason)
	}
	if len(logged) == 0 {
		t.Error("a refused hop should say so once, so an operator can see why nothing moved")
	}
}

func TestHopHomeOnlyWhenAwayFromHome(t *testing.T) {
	var asked []int
	hook := func(w int, _ string) error { asked = append(asked, w); return nil }

	d := &Deps{}
	d.SetWorlds(2, 1, func(string) (int, bool) { return 2, true }, hook)
	if hopping, _ := d.hopHome(); !hopping {
		t.Fatal("an account on world 2 whose home is world 1 must come back")
	}
	if len(asked) != 1 || asked[0] != 1 {
		t.Fatalf("asked to hop to %v, want [1]", asked)
	}

	asked = nil
	d.SetWorlds(1, 1, func(string) (int, bool) { return 2, true }, hook)
	if hopping, _ := d.hopHome(); hopping {
		t.Fatal("an account already home must not hop home")
	}
	if len(asked) != 0 {
		t.Fatalf("a no-op hop home still asked: %v", asked)
	}
}

// A mule name of "" (a script with no mule configured) must never hop.
func TestNoMuleNameNeverHops(t *testing.T) {
	var asked []int
	d := &Deps{}
	d.SetWorlds(1, 1, func(string) (int, bool) { return 2, true }, func(w int, _ string) error { asked = append(asked, w); return nil })
	if hopping, _ := d.hopToMule(""); hopping || len(asked) != 0 {
		t.Fatal("a hand-off with no mule configured must not hop anywhere")
	}
}
