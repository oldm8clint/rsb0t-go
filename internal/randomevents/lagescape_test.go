package randomevents

import (
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/pathfind"
)

// lagescape-1018: the escape's two entry points here. EscapeTeleport is lastResortExit with the decision made by the caller;
// FleeAttackers is the guardian's walking flee asked for by a script whose combat is expected.

const (
	escLaw, escAir, escFire, escBass = 563, 556, 554, 365
)

func escapeKit(t *testing.T) (*overlapSession, *Guardian) {
	t.Helper()
	withResolver(t, foodResolver{
		fakeResolver: fakeResolver{objNames: map[int]string{escLaw: "Law rune", escAir: "Air rune", escFire: "Fire rune", escBass: "Bass"}},
		objOps:       map[int][5]string{escBass: {"Eat"}},
	})
	s := &overlapSession{state: overlapState()}
	g := NewGuardian(s, nil)
	t.Cleanup(g.Stop)
	t.Cleanup(func() { lastResortCasts.Delete(s) })
	setStat(s.state, magicStat, 66)
	return s, g
}

// The caller has judged the account must leave, so no share of hitpoints is asked of it here: 40 of 55 with a Bass in the pack is no
// lastResortExit (food and over 30%), and is an EscapeTeleport. With no kit it casts nothing, and it never casts twice inside its gap.
func TestEscapeTeleportCastsWhateverTheHitpointsWhenTheKitIsThere(t *testing.T) {
	s, _ := escapeKit(t)
	sends := 0
	s.onSend = func(byte) { sends++ }
	setHP(s.state, 40, 55)

	if PayableTeleport(s) != "" || EscapeTeleport(s, "test", time.Minute, nil) || sends != 0 {
		t.Fatal("cast, or called a teleport payable, with no runes")
	}
	setFullInventory(s.state, 3214, [2]int32{escLaw, 5}, [2]int32{escAir, 30}, [2]int32{escFire, 10}, [2]int32{escBass, 4})
	if lastResortExit(s, "test", nil) {
		t.Fatal("the last resort cast at 40 of 55 with food: its own rule changed")
	}
	if PayableTeleport(s) == "" {
		t.Fatal("a pack with Law, Air and Fire runes and Magic 66 pays for no teleport")
	}
	var lines []string
	if !EscapeTeleport(s, "lag escape", time.Minute, func(m string) { lines = append(lines, m) }) || sends == 0 {
		t.Fatalf("the escape cast nothing (sends %d)", sends)
	}
	if len(lines) != 1 {
		t.Fatalf("log: %v", lines)
	}
	sends = 0
	if EscapeTeleport(s, "lag escape", time.Minute, nil) || sends != 0 {
		t.Error("a second cast went out inside the gap")
	}
	// The last resort shares the ledger: it does not cast on the escape's heels either.
	setHP(s.state, 10, 55)
	if lastResortExit(s, "test", nil) || sends != 0 {
		t.Error("the last resort cast within the gap of the escape's cast")
	}
}

// The gap is the caller's, never under the last resort's own.
func TestEscapeTeleportGapIsTheLongerOfTheTwo(t *testing.T) {
	s, _ := escapeKit(t)
	setFullInventory(s.state, 3214, [2]int32{escLaw, 5}, [2]int32{escAir, 30}, [2]int32{escFire, 10})
	setHP(s.state, 10, 55)
	sends := 0
	s.onSend = func(byte) { sends++ }
	if !EscapeTeleport(s, "test", 0, nil) {
		t.Fatal("no cast")
	}
	sends = 0
	if EscapeTeleport(s, "test", 0, nil) || sends != 0 {
		t.Error("a zero gap let a second cast through inside the last resort's own")
	}
	lastResortCasts.Store(s, time.Now().Add(-20*time.Second))
	if !EscapeTeleport(s, "test", 15*time.Second, nil) {
		t.Error("no cast 20 s after the last, with a 15 s gap")
	}
}

// A session with no Guardian, or an account whose Magic is under the lowest teleport, casts and pays for nothing.
func TestEscapeTeleportNeedsAGuardianAndTheLevel(t *testing.T) {
	withResolver(t, fakeResolver{objNames: map[int]string{escLaw: "Law rune", escAir: "Air rune", escFire: "Fire rune"}})
	bare := &overlapSession{state: overlapState()}
	setStat(bare.state, magicStat, 66)
	setFullInventory(bare.state, 3214, [2]int32{escLaw, 5}, [2]int32{escAir, 30}, [2]int32{escFire, 10})
	if EscapeTeleport(bare, "test", time.Minute, nil) || PayableTeleport(bare) != "" {
		t.Error("a session with no Guardian cast or paid for a teleport")
	}
	s, _ := escapeKit(t)
	setStat(s.state, magicStat, 20)
	setFullInventory(s.state, 3214, [2]int32{escLaw, 5}, [2]int32{escAir, 30}, [2]int32{escFire, 10})
	if EscapeTeleport(s, "test", time.Minute, nil) || PayableTeleport(s) != "" {
		t.Error("Magic 20 paid for a teleport")
	}
}

// FleeAttackers runs the flee with CombatExpected clear, puts it back, and only for an account that has been hit lately.
func TestFleeAttackersRunsTheFleeWithTheFlagClear(t *testing.T) {
	s := &overlapSession{state: overlapState()}
	g := NewGuardian(s, nil)
	defer g.Stop()
	g.CombatExpected = true
	var sawExpected []bool
	prev := fleeAttackersFlee
	fleeAttackersFlee = func(sess actions.Session, _ *pathfind.Pack, dist int, _ func(string)) {
		sawExpected = append(sawExpected, g.CombatExpected)
		if dist != baseFleeDist {
			t.Errorf("flee dist = %d, want %d", dist, baseFleeDist)
		}
	}
	t.Cleanup(func() { fleeAttackersFlee = prev })

	// Nothing has hit the account: no flee.
	if FleeAttackers(s, nil, nil) || len(sawExpected) != 0 {
		t.Fatal("fled an account nothing had hit")
	}
	underAttackBy(s, 77, 114)
	if !FleeAttackers(s, nil, nil) || len(sawExpected) != 1 {
		t.Fatalf("did not flee an account under attack (%v)", sawExpected)
	}
	if sawExpected[0] {
		t.Error("CombatExpected was still set while the flee ran: walkerThreatened would have called the hit an expected cost")
	}
	if !g.CombatExpected {
		t.Error("CombatExpected was not put back")
	}
	// The guardian's own flee holds the action lock: this leaves it alone.
	unlock, _ := TryLock(s)
	if FleeAttackers(s, nil, nil) || len(sawExpected) != 1 {
		t.Error("fled while the guardian held the action lock")
	}
	unlock()
}
