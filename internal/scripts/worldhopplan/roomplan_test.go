package worldhopplan

import (
	"testing"
	"time"
)

// These pin the four rules ChooseEvictee's own doc comment states, plus the two guards
// around it. The order matters: the first two rules exist to stop the fleet breaking work
// that was going fine, which is a worse outcome than not making room at all.

func cand(name string, world int, f func(*RoomCandidate)) RoomCandidate {
	c := RoomCandidate{Username: name, World: world, InGame: true, Spread: len(name)}
	if f != nil {
		f(&c)
	}
	return c
}

func TestChooseEvicteeNeverPicksAPinnedAccount(t *testing.T) {
	now := time.Now()
	// The mule is the case that matters: every donor in the fleet is walking toward it, so
	// moving it to free one slot would strand every hand-off at once.
	cands := []RoomCandidate{
		cand("mule", 1, func(c *RoomCandidate) { c.Pinned = true; c.Spread = 0 }),
	}
	if got, ok := ChooseEvictee(1, cands, now, DefaultRevisitCooldown); ok {
		t.Fatalf("chose the pinned account %q — a world holding only pinned accounts has no room to give", got.Username)
	}
}

func TestChooseEvicteePrefersAnUnpinnedAccountOverALowerSpreadPinnedOne(t *testing.T) {
	now := time.Now()
	// The pinned account has the lowest spread, so it would win on the tie-break if the
	// pin were not checked first.
	cands := []RoomCandidate{
		cand("mule", 1, func(c *RoomCandidate) { c.Pinned = true; c.Spread = 0 }),
		cand("fisher", 1, func(c *RoomCandidate) { c.Spread = 500 }),
	}
	got, ok := ChooseEvictee(1, cands, now, DefaultRevisitCooldown)
	if !ok {
		t.Fatal("no evictee chosen, want the unpinned account")
	}
	if got.Username != "fisher" {
		t.Errorf("chose %q, want \"fisher\" — the pin must outrank the spread tie-break", got.Username)
	}
}

func TestChooseEvicteeSkipsAccountsThatFreeNothing(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		c    RoomCandidate
	}{
		// Rule 2: a second hop on a running one is refused anyway, so picking it burns
		// the attempt.
		{"already hopping", cand("hopper", 1, func(c *RoomCandidate) { c.Hopping = true })},
		// Rule 3: moving an account that is not in game frees no slot.
		{"not in game", cand("offline", 1, func(c *RoomCandidate) { c.InGame = false })},
		// Not on this world at all.
		{"another world", cand("elsewhere", 2, nil)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, ok := ChooseEvictee(1, []RoomCandidate{tc.c}, now, DefaultRevisitCooldown); ok {
				t.Errorf("chose %q, want no choice at all", got.Username)
			}
		})
	}
}

func TestChooseEvicteeLeavesARecentlyEvictedAccountAlone(t *testing.T) {
	now := time.Now()
	// Rule 4. Without it the same account is picked every time — it is the one whose own
	// failover most recently put it back — and it ping-pongs between worlds doing no work.
	justMoved := cand("recent", 1, func(c *RoomCandidate) {
		c.LastEvicted = now.Add(-1 * time.Minute)
		c.Spread = 0
	})
	settled := cand("settled", 1, func(c *RoomCandidate) {
		c.LastEvicted = now.Add(-2 * DefaultRevisitCooldown)
		c.Spread = 900
	})

	got, ok := ChooseEvictee(1, []RoomCandidate{justMoved, settled}, now, DefaultRevisitCooldown)
	if !ok {
		t.Fatal("no evictee chosen, want the one that has been settled for a while")
	}
	if got.Username != "settled" {
		t.Errorf("chose %q, want \"settled\" — an account moved a minute ago must be left alone", got.Username)
	}

	// And with only the recent one, there is simply no room to give.
	if got, ok := ChooseEvictee(1, []RoomCandidate{justMoved}, now, DefaultRevisitCooldown); ok {
		t.Errorf("chose %q, want no choice — its cooldown has not elapsed", got.Username)
	}
}

func TestChooseEvicteeIsStableAndTotal(t *testing.T) {
	now := time.Now()
	// Same inputs in a different order must give the same answer, or two managers asking
	// at the same instant would disagree and two accounts would leave for one slot.
	a := cand("alpha", 1, func(c *RoomCandidate) { c.Spread = 10 })
	b := cand("bravo", 1, func(c *RoomCandidate) { c.Spread = 10 }) // exact tie on spread
	cc := cand("charlie", 1, func(c *RoomCandidate) { c.Spread = 99 })

	first, ok1 := ChooseEvictee(1, []RoomCandidate{a, b, cc}, now, DefaultRevisitCooldown)
	second, ok2 := ChooseEvictee(1, []RoomCandidate{cc, b, a}, now, DefaultRevisitCooldown)
	if !ok1 || !ok2 {
		t.Fatal("expected a choice from both orderings")
	}
	if first.Username != second.Username {
		t.Errorf("order changed the answer: %q then %q", first.Username, second.Username)
	}
	if first.Username != "alpha" {
		t.Errorf("chose %q, want \"alpha\" — lowest spread, username breaking the exact tie", first.Username)
	}
}

func TestRoomIsNeededOnlyForAnAccountPinnedToThatWorld(t *testing.T) {
	cases := []struct {
		name        string
		pinnedWorld int
		pinned      bool
		want        int
		expect      bool
	}{
		// The whole distinction: only a pin says another world is not an acceptable answer.
		{"pinned to the world it wants", 1, true, 1, true},
		{"pinned to a different world", 2, true, 1, false},
		{"not pinned at all", 0, false, 1, false},
		// An unpinned account is better served by the ordinary failover move, which costs
		// the fleet nothing and disturbs nobody who was working.
		{"unpinned even with a stale world number", 1, false, 1, false},
		{"world 0 is not a world", 0, true, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RoomIsNeeded(tc.pinnedWorld, tc.pinned, tc.want); got != tc.expect {
				t.Errorf("RoomIsNeeded(%d, %v, %d) = %v, want %v", tc.pinnedWorld, tc.pinned, tc.want, got, tc.expect)
			}
		})
	}
}

func TestCanAskAgainPacesTheAsks(t *testing.T) {
	now := time.Now()
	// Room is made one account at a time: a hop is not instant, so asking ten accounts at
	// once would empty ten slots to fill one.
	if !CanAskAgain(time.Time{}, now, DefaultMakeRoomInterval) {
		t.Error("the first ask on a world must be allowed")
	}
	if CanAskAgain(now.Add(-time.Second), now, DefaultMakeRoomInterval) {
		t.Error("a second ask one second later must be refused")
	}
	if !CanAskAgain(now.Add(-2*DefaultMakeRoomInterval), now, DefaultMakeRoomInterval) {
		t.Error("an ask well past the interval must be allowed")
	}
}
