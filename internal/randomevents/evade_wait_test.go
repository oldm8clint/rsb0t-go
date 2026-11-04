package randomevents

import (
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/protocol/clientprot"
)

func TestEvadeHeldActionPolicy(t *testing.T) {
	prev := evadeHeldPatience
	evadeHeldPatience = 10 * time.Second
	t.Cleanup(func() { evadeHeldPatience = prev })
	cases := []struct {
		name     string
		hp, max  int
		known    bool
		held     time.Duration
		attempt  int
		expected heldAction
	}{
		{"well, just after the walk: wait", 55, 55, true, time.Second, 1, heldWait},
		{"well, nearly out of patience: wait", 55, 55, true, 9 * time.Second, 1, heldWait},
		{"well, out of patience on the first attempt: hand the lock back", 55, 55, true, 10 * time.Second, 1, heldHandBack},
		{"well, out of patience on the third attempt: hand the lock back", 55, 55, true, 12 * time.Second, maxAttempts - 1, heldHandBack},
		{"well, out of patience on the last attempt: stand down", 55, 55, true, 10 * time.Second, maxAttempts, heldStandDown},
		{"exactly half: wait", 28, 55, true, time.Second, 1, heldWait},
		{"under half: stand down at once", 27, 55, true, 0, 1, heldStandDown},
		{"nearly dead: stand down at once", 3, 55, true, 0, 1, heldStandDown},
		{"hitpoints unread: stand down, the script knows better", 0, 0, false, 0, 1, heldStandDown},
	}
	for _, c := range cases {
		if got := evadeHeldAction(c.hp, c.max, c.known, c.held, c.attempt); got != c.expected {
			t.Errorf("%s: action = %v, want %v", c.name, got, c.expected)
		}
	}
}

// A scene for the wait: the account stands at origin among the cave's herd with a Shade on it (leashed,
// maxrange 3), its walk "succeeds" (the first walk packet moves it six tiles west) and the Shade is still facing
// it, hitting it. The server did not honour the engine rule the leash relies on: the case the review named.
type heldScene struct {
	icePen
	g *Guardian
}

func newHeldScene(t *testing.T, hp, max int) *heldScene {
	t.Helper()
	c := caveCases[2] // the stand at 3062,9580 and a Shade
	sc := newCaveScene(t, c.origin, c.monsterType, c.monster)
	prevPoll, prevLeg, prevDeadline, prevPatience := fleeWalkPollInterval, fleeWalkLegInterval, fleeWalkDeadline, evadeHeldPatience
	fleeWalkPollInterval, fleeWalkLegInterval, fleeWalkDeadline, evadeHeldPatience = 20*time.Millisecond, 150*time.Millisecond, 600*time.Millisecond, 400*time.Millisecond
	t.Cleanup(func() {
		fleeWalkPollInterval, fleeWalkLegInterval, fleeWalkDeadline, evadeHeldPatience = prevPoll, prevLeg, prevDeadline, prevPatience
	})
	forgetNoEscape(sc.s)
	t.Cleanup(func() { forgetNoEscape(sc.s) })
	setHP(sc.s.state, hp, max)
	g := NewGuardian(sc.s, sc.pack)
	g.CombatExpected = true
	t.Cleanup(g.Stop)
	return &heldScene{icePen: sc, g: g}
}

// run walks the account on its first walk packet and runs HandleEvade at attempt, failing the test if it holds the
// lock for more than 20 s (the old wait was 45). It returns the log and the opcodes sent.
func (h *heldScene) run(t *testing.T, attempt int) (lines []string, ops []byte, took time.Duration) {
	t.Helper()
	moved := false
	h.s.onSend = func(op byte) {
		ops = append(ops, op)
		if op == clientprot.MOVE_GAMECLICK && !moved {
			moved = true
			setExactTile(h.s.state, h.origin.X-6, h.origin.Z, 0)
			hitSelf(h.s.state, 8)
		}
	}
	ev := &Event{Kind: KindEvade, Name: "event monster", NpcIndex: 1, NpcType: caveCases[2].monsterType, ThreatX: caveCases[2].monster.X, ThreatZ: caveCases[2].monster.Z}
	var mu sync.Mutex
	done := make(chan error, 1)
	started := time.Now()
	go func() {
		done <- HandleEvade(h.s, h.pack, ev, attempt, func(m string) { mu.Lock(); lines = append(lines, m); mu.Unlock() })
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("HandleEvade held the action lock for 20 s with the monster still on the account and nothing eating, teleporting or handing back")
	}
	took = time.Since(started)
	if !moved {
		t.Fatal("the scene is wrong: the walker sent no walk")
	}
	mu.Lock()
	defer mu.Unlock()
	t.Logf("returned in %s:\n%s", took.Round(time.Millisecond), strings.Join(lines, "\n"))
	return lines, ops, took
}

func attackedThe(ops []byte) bool {
	for _, op := range ops {
		if op == clientprot.OPNPC2 {
			return true
		}
	}
	return false
}

// The walk completed and the Shade still has the account: a well account hands the lock back after the patience,
// with the event pending for the guardian's next attempt, and does not attack a Shade.
func TestEvadeStillHitAfterTheWalkHandsTheLockBackInsteadOfIdling(t *testing.T) {
	h := newHeldScene(t, 55, 55)
	lines, ops, took := h.run(t, 1)
	if took > 8*time.Second {
		t.Fatalf("HandleEvade took %s with a 400 ms patience", took)
	}
	if !containsLine(lines, "handing the lock back") {
		t.Fatalf("no hand-back line was logged:\n%s", strings.Join(lines, "\n"))
	}
	if h.g.cornered() {
		t.Fatal("a well account was put in the cornered window: it should be walked again by the next attempt")
	}
	if attackedThe(ops) {
		t.Fatal("the account attacked a Shade")
	}
}

// Hurt, it does not wait for the patience: it stands down at once, the script has the lock and the cornered window
// is open, and a Shade (level 120 for this owner) is not attacked.
func TestEvadeStillHitAndHurtStandsDownForTheScript(t *testing.T) {
	h := newHeldScene(t, 20, 55)
	lines, ops, took := h.run(t, 1)
	if took > 8*time.Second {
		t.Fatalf("HandleEvade took %s", took)
	}
	if !containsLine(lines, "standing down for the script") {
		t.Fatalf("no stand-down line was logged:\n%s", strings.Join(lines, "\n"))
	}
	if !h.g.cornered() {
		t.Fatal("the script was not handed the account (no cornered window)")
	}
	if attackedThe(ops) {
		t.Fatal("the account attacked a Shade")
	}
}

// A monster that has outlasted maxAttempts walks is not walked from again and again: the account goes to its script.
func TestEvadeStillHitOnTheLastAttemptStandsDown(t *testing.T) {
	h := newHeldScene(t, 55, 55)
	lines, _, _ := h.run(t, maxAttempts)
	if !containsLine(lines, "standing down for the script") || !h.g.cornered() {
		t.Fatalf("attempt %d did not stand down (cornered %v):\n%s", maxAttempts, h.g.cornered(), strings.Join(lines, "\n"))
	}
}

// The attempt the guardian is on reaches the walker: attempt 3 aims leashBreakFor(3, 3) from the Shade's spawn,
// where every attempt used to be walked as the first (6 tiles from it).
func TestWalkerAimsFartherOnALaterAttempt(t *testing.T) {
	c := caveCases[2]
	head := regexp.MustCompile(`heading to (\d+),(\d+),0`)
	for _, attempt := range []int{1, 3} {
		sc := newCaveScene(t, c.origin, c.monsterType, c.monster)
		prevPoll, prevLeg, prevDeadline := fleeWalkPollInterval, fleeWalkLegInterval, fleeWalkDeadline
		fleeWalkPollInterval, fleeWalkLegInterval, fleeWalkDeadline = 20*time.Millisecond, 150*time.Millisecond, 300*time.Millisecond
		t.Cleanup(func() { fleeWalkPollInterval, fleeWalkLegInterval, fleeWalkDeadline = prevPoll, prevLeg, prevDeadline })
		forgetNoEscape(sc.s)
		g := NewGuardian(sc.s, sc.pack)
		g.CombatExpected = true
		noteLeashSpawn(sc.s, 1, c.monsterType, c.monster.X, c.monster.Z, 0)
		var lines []string
		if _, err := fleeWalkingFrom(sc.s, sc.pack, 20, attempt, func(m string) { lines = append(lines, m) }); err != nil {
			t.Fatal(err)
		}
		g.Stop()
		forgetNoEscape(sc.s)
		var dest pathfind.NavPoint
		for _, l := range lines {
			if m := head.FindStringSubmatch(l); m != nil {
				x, _ := strconv.Atoi(m[1])
				z, _ := strconv.Atoi(m[2])
				dest = pathfind.NavPoint{X: x, Z: z}
				break
			}
		}
		if dest == (pathfind.NavPoint{}) {
			t.Fatalf("attempt %d: no heading line in %v", attempt, lines)
		}
		want := leashBreakFor(3, attempt)
		if d := chebyshev(dest.X, dest.Z, c.monster.X, c.monster.Z); d < want {
			t.Fatalf("attempt %d walked to %v, %d from the Shade's spawn, short of %d", attempt, dest, d, want)
		}
		t.Logf("attempt %d: walks to %v, %d from the spawn (wanted %d)", attempt, dest, chebyshev(dest.X, dest.Z, c.monster.X, c.monster.Z), want)
	}
}

func containsLine(lines []string, sub string) bool {
	for _, l := range lines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}
