package combatrunner

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/combatplan"
	"rsb0t-go/internal/scripts/tile"
)

// One timeout was serving a walk to the bank and the only walk home after a fail-teleport threw
// an account 677 tiles across the map. These are the two ends of that, asserted apart.
func TestTravelBudgetSizesTheWalkFromItsDistance(t *testing.T) {
	giants, ok := combatplan.RungByKey(combatplan.MeleeLadder(), "giants")
	if !ok {
		t.Fatal("the ladder has no giants rung")
	}

	// A walk somewhere nearby keeps exactly what it always had. Nothing that used to work is
	// given less time by this change.
	if got := TravelBudget(giants.Stand, giants.Stand); got != TravelWalkTimeout {
		t.Errorf("a zero-distance walk got %s, want the %s floor", got, TravelWalkTimeout)
	}
	// The bank trip too: about 95 tiles once the trapdoor is charged, which is under the floor.
	// The dungeon fold is what makes this true — unfolded it would read 6,345 tiles and take the
	// cap, so every bank trip would wait ten minutes before reporting a failure.
	if got := TravelBudget(giants.Stand, giants.Bank); got != TravelWalkTimeout {
		t.Errorf("the giants bank trip got %s, want the %s floor (route is %d tiles)",
			got, TravelWalkTimeout, combatplan.RouteTiles(giants.Stand, giants.Bank))
	}

	// The 2026-09-20 fail-teleport: 677 tiles from the Al Kharid rung to Ardougne Monastery,
	// which the fixed two-minute budget timed out on.
	far := TravelBudget(tile.Tile{X: 3288, Z: 3172}, tile.Tile{X: 2613, Z: 3208})
	if far <= TravelWalkTimeout {
		t.Errorf("a 677-tile displacement got %s, no more than the nearby-walk floor of %s", far, TravelWalkTimeout)
	}
	if far != TravelMaxTimeout {
		t.Errorf("a 677-tile displacement got %s, want the %s cap", far, TravelMaxTimeout)
	}

	// And it is still bounded: a walk that cannot be made has to end so the caller can retire
	// the rung rather than stand in it.
	if got := TravelBudget(tile.Tile{X: 0, Z: 0}, tile.Tile{X: 30000, Z: 30000}); got != TravelMaxTimeout {
		t.Errorf("an absurd distance got %s, want the %s cap", got, TravelMaxTimeout)
	}
}

// The engine honours an obj click from the stack's own tile or from one sharing an EDGE with it
// (Player.inOperableDistance answers reachedEntity || reachedObj for an Obj) and silently drops
// one sent from a diagonal. "Silently" is the whole problem: OpObjHandler accepts the click and
// queues the interaction, tryInteract never satisfies inOperableDistance, and the caller sees no
// refusal, no chat line and the stack still lying there. A Chebyshev radius of 1 let that
// through, which is what cost about one big bones in nine at the giants rung.
func TestTouchesTileAcceptsOnlyTheTileAndItsEdgeNeighbours(t *testing.T) {
	stack := tile.Tile{X: 3113, Z: 9833}
	for _, c := range []struct {
		name string
		at   tile.Tile
		want bool
	}{
		{"the stack's own tile", stack, true},
		{"north", tile.Tile{X: 3113, Z: 9834}, true},
		{"south", tile.Tile{X: 3113, Z: 9832}, true},
		{"east", tile.Tile{X: 3114, Z: 9833}, true},
		{"west", tile.Tile{X: 3112, Z: 9833}, true},
		{"north east, the diagonal the engine drops", tile.Tile{X: 3114, Z: 9834}, false},
		{"south west, the diagonal the engine drops", tile.Tile{X: 3112, Z: 9832}, false},
		{"two tiles out", tile.Tile{X: 3115, Z: 9833}, false},
		{"a different level", tile.Tile{X: 3113, Z: 9833, Level: 1}, false},
	} {
		if got := touchesTile(c.at, stack); got != c.want {
			t.Errorf("%s: touchesTile(%d,%d,%d) = %v, want %v", c.name, c.at.X, c.at.Z, c.at.Level, got, c.want)
		}
	}
}

// describeOffset goes into the one error a reader acts on, so it has to name the cause it can
// actually support. Calling a long-distance displacement "DIAGONAL" reads like a diagnosis and
// is a lie: it was observed live with the account at 2434,3346 and the bones at 3113,9833, which
// is a death or a random event's fail-teleport moving the account after the approach, not a
// click geometry problem at all.
func TestDescribeOffsetDoesNotCallADisplacementDiagonal(t *testing.T) {
	stack := tile.Tile{X: 3113, Z: 9833}

	if got := describeOffset(stack, stack); !strings.Contains(got, "own tile") {
		t.Errorf("standing on the stack: got %q", got)
	}
	if got := describeOffset(tile.Tile{X: 3113, Z: 9834}, stack); !strings.Contains(got, "orthogonally adjacent") {
		t.Errorf("orthogonal neighbour: got %q", got)
	}

	diag := describeOffset(tile.Tile{X: 3114, Z: 9834}, stack)
	if !strings.Contains(diag, "DIAGONAL") {
		t.Errorf("diagonal neighbour should be named as such: got %q", diag)
	}

	// The regression this test exists for.
	far := describeOffset(tile.Tile{X: 2434, Z: 3346}, stack)
	if strings.Contains(far, "DIAGONAL") {
		t.Errorf("a 679-tile displacement must not be reported as DIAGONAL: got %q", far)
	}
	if !strings.Contains(far, "too far") {
		t.Errorf("a displacement should say so plainly: got %q", far)
	}
}

// The reading a deposit's log line has to carry, and the two shapes it must keep apart.
//
// A live finish banked and logged out, and the log said nothing either way: DepositAllExcept
// logs only when an individual deposit errors and the caller discarded the struct, so a deposit
// that moved everything and one that moved nothing produced identical output. The half that
// makes the difference sayable is the bank's own rows, because an emptied pack is equally true
// of a run that dropped its load on the floor.
func TestDepositSummaryTellsARealDepositFromOneThatMovedNothing(t *testing.T) {
	worked := Deposit{Clicked: 3, PanelBefore: 25, PanelAfter: 13, PackBefore: 25, PackAfter: 13, BankBefore: 40, BankAfter: 67}
	if got := worked.BankGained(); got != 27 {
		t.Errorf("a bank that went 40 -> 67 gained 27, got %d", got)
	}
	got := worked.Summary()
	if !strings.Contains(got, "the bank rose by 27") {
		t.Errorf("a deposit that landed must say what the bank gained: got %q", got)
	}
	if !strings.Contains(got, "pack 25/28 -> 13/28") {
		t.Errorf("the pack reading belongs in the line too: got %q", got)
	}

	// Same clicks, same lack of errors, nothing moved. This is the line the fleet operator never
	// saw.
	noop := Deposit{Clicked: 3, PanelBefore: 25, PanelAfter: 25, PackBefore: 25, PackAfter: 25, BankBefore: 40, BankAfter: 40}
	quiet := noop.Summary()
	if !strings.Contains(quiet, "deposited NOTHING") {
		t.Errorf("a deposit that moved nothing has to say so: got %q", quiet)
	}
	if strings.Contains(quiet, "the bank rose") {
		t.Errorf("nothing was banked, so nothing may claim a rise: got %q", quiet)
	}
	if quiet == got {
		t.Error("the whole point is that these two cannot read the same")
	}
}

// The discriminating case, and the reason the reading is the bank rather than the pack: a pack
// emptied onto the floor frees every slot the real deposit would have and must still not read as
// a deposit.
func TestDepositSummaryRefusesToCallAnEmptiedPackADeposit(t *testing.T) {
	floor := Deposit{Clicked: 3, PanelBefore: 25, PanelAfter: 13, PackBefore: 25, PackAfter: 13, BankBefore: 40, BankAfter: 40}
	got := floor.Summary()
	if strings.Contains(got, "the bank rose") {
		t.Errorf("twelve slots came free and the bank did not move, which is not a deposit: got %q", got)
	}
	if !strings.Contains(got, "the bank did not rise") {
		t.Errorf("the line has to say the bank did not move: got %q", got)
	}

	// An ordinary withdrawal misread as a deposit is the same failure with the sign flipped.
	fell := Deposit{Clicked: 1, BankBefore: 67, BankAfter: 40}
	if got := fell.Summary(); !strings.Contains(got, "the bank FELL by 27") {
		t.Errorf("a bank that went down must not read as a deposit: got %q", got)
	}
}

// Nothing in the product recorded what the account walked away still holding. Real finishes left
// with ten slots in one case and five in another, and the log named neither.
func TestDepositSummaryNamesWhatIsStillCarried(t *testing.T) {
	dep := Deposit{Clicked: 2, PanelBefore: 20, PanelAfter: 10, PackBefore: 20, PackAfter: 10, BankBefore: 0, BankAfter: 30, Stuck: []string{"Big bones", "Bronze sword"}, Unnamed: 2}
	got := dep.Summary()
	if !strings.Contains(got, "still carrying [Big bones, Bronze sword]") {
		t.Errorf("the residue is the thing nobody could see: got %q", got)
	}
	if !strings.Contains(got, "2 slot(s) hold an object id the config cannot name") {
		t.Errorf("ids the config cannot name are banked by nothing and must be said: got %q", got)
	}
}
