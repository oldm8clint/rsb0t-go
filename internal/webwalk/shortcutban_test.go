package webwalk

import "testing"

// TestASkillGatedShortcutThatKeepsFailingIsBannedEvenWhenItIsTheOnlyWay is the Fremennik basalt rocks (6 Oct 2026,
// spare23): the rock at 2522,3600 slips its jumper back to the shore for 1-10 hitpoints, an account under the
// shortcut's odds slips most tries, and "the only way, keep trying" took ten crossings and the account's life. Unlike
// the door of onlyway_test.go, which refuses on the dice and costs nothing, a failed skill shortcut costs hitpoints, so
// from shortcutBanStrikes consecutive failures it is out of every plan even with nothing else to route through.
//
// That is the rule for a shortcut whose slip the graph does not know. One with an Edge.Slip, on a client that can say
// how many hitpoints the account has, is kept in the plans when it is the only way home and rested for instead
// (restForSlip), because banning the basalt rocks' southbound rock stranded accounts on the Lighthouse shore for two
// hours on 14 Oct 2026: see basalt_test.go.
func TestASkillGatedShortcutThatKeepsFailingIsBannedEvenWhenItIsTheOnlyWay(t *testing.T) {
	_, w, g := house(t, Point{X: 1, Z: 2})
	e := doorEdge(g)
	if e == nil {
		t.Fatal("house() no longer has the door this test is about")
	}
	rock := *e
	rock.Kind = KindShortcut
	rock.LocName = "Basalt rock"
	rock.Requires = &Requires{Skills: []SkillReq{{Name: "agility", Level: 35}}}
	key := Point{X: rock.LocX, Z: rock.LocZ, Level: rock.From.Level}
	dest := Point{X: 9, Z: 2}
	avoid := map[Point]bool{}
	opt := w.searchOptions(Options{}, avoid)
	for i := 0; i < shortcutBanStrikes-1; i++ {
		if n := w.noteRefusal(&rock, avoid, dest, opt); n < 0 {
			t.Fatalf("noteRefusal = %d", n)
		}
		if w.Banned(key) {
			t.Fatalf("banned after only %d failures", i+1)
		}
	}
	w.noteRefusal(&rock, avoid, dest, opt)
	if !w.Banned(key) {
		t.Fatalf("a skill-gated shortcut that failed %d times in a row is still in the plan", shortcutBanStrikes)
	}
	if !avoid[key] {
		t.Errorf("the banned shortcut was not excluded from this walk's plans")
	}
}

// A plain door of the same shape is untouched: the ban is for skill shortcuts only, and the Fishing Guild rule
// (TestARefusedBarrierThatIsTheOnlyWayOutStaysInThePlan) stands.
func TestFailureHurtsIsOnlyForSkillGatedShortcuts(t *testing.T) {
	door := Edge{Kind: KindDoor, Requires: &Requires{Skills: []SkillReq{{Name: "agility", Level: 35}}}}
	plain := Edge{Kind: KindShortcut}
	gated := Edge{Kind: KindShortcut, Requires: &Requires{Skills: []SkillReq{{Name: "agility", Level: 35}}}}
	if door.failureHurts() || plain.failureHurts() || !gated.failureHurts() {
		t.Errorf("failureHurts: door %v, plain shortcut %v, gated shortcut %v", door.failureHurts(), plain.failureHurts(), gated.failureHurts())
	}
}
