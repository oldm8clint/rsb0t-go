package fleet

import (
	"reflect"
	"testing"
)

func scriptsRank(scripts []string) func(int) int {
	return func(i int) int { return LoginRank(scripts[i]) }
}

// The mule first, then the crafter, then the carriers, then everybody else; each group in the order the roster wrote it.
func TestOrderForLoginPutsTheMuleTheCrafterAndTheCarriersFirst(t *testing.T) {
	scripts := []string{
		"ScarabSwarm", "ScarabSwarm", ScriptCarrier, "ProgressiveFisher", ScriptCrafter, ScriptCarrier, "",
		"EssenceMiner", ScriptMule, ScriptCarrier, "ScarabSwarm",
	}
	got := OrderForLogin(len(scripts), scriptsRank(scripts))
	want := []int{8, 4, 2, 5, 9, 0, 1, 3, 6, 7, 10}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order %v, want %v", got, want)
	}
	if n := PriorityLogins(len(scripts), scriptsRank(scripts)); n != 5 {
		t.Errorf("%d priority accounts, want the mule, the crafter and three carriers", n)
	}
}

// A roster with none of the named scripts launches exactly as it was written, and the order is always a permutation.
func TestOrderForLoginKeepsTheRosterOrderWhenNothingIsNamed(t *testing.T) {
	scripts := []string{"ScarabSwarm", "", "ProgressiveFisher", "ScarabSwarm", "BassOysterCaskets"}
	got := OrderForLogin(len(scripts), scriptsRank(scripts))
	if !reflect.DeepEqual(got, []int{0, 1, 2, 3, 4}) {
		t.Fatalf("order %v", got)
	}
	if n := PriorityLogins(len(scripts), scriptsRank(scripts)); n != 0 {
		t.Errorf("%d priority accounts in a roster with none", n)
	}
	if got := OrderForLogin(0, scriptsRank(nil)); len(got) != 0 {
		t.Errorf("order of nothing: %v", got)
	}
	if got := OrderForLogin(3, nil); !reflect.DeepEqual(got, []int{0, 1, 2}) {
		t.Errorf("no rank function: %v", got)
	}
}

// No account is lost or doubled whatever the mix.
func TestOrderForLoginIsAPermutation(t *testing.T) {
	names := []string{ScriptCarrier, "x", ScriptMule, "y", ScriptCrafter, ScriptCarrier, "z", ScriptCarrier}
	for n := 0; n <= len(names); n++ {
		got := OrderForLogin(n, scriptsRank(names))
		seen := map[int]bool{}
		for _, i := range got {
			if i < 0 || i >= n || seen[i] {
				t.Fatalf("n=%d: %v", n, got)
			}
			seen[i] = true
		}
		if len(got) != n {
			t.Fatalf("n=%d: %d indexes", n, len(got))
		}
	}
}

func TestLoginRankNamesTheThreeScripts(t *testing.T) {
	if !(LoginRank(ScriptMule) < LoginRank(ScriptCrafter) && LoginRank(ScriptCrafter) < LoginRank(ScriptCarrier) && LoginRank(ScriptCarrier) < LoginRank("ScarabSwarm")) {
		t.Fatal("the order is the mule, the crafter, the carriers, the rest")
	}
	if LoginRank("") != LoginRank("EssenceMiner") {
		t.Error("an account with no script ranks with the rest")
	}
}
