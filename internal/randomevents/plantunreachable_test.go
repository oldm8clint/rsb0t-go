package randomevents

import (
	"strings"
	"testing"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/pathfind"
)

// noSound is kickUnreachablePlant's soundAgo for a session that has heard no sound at all.
const noSound = -1

// kickUnreachablePlant runs n guardian polls against a Strange plant six tiles east of the account
// on an empty pack, so no route to it exists (the live 5 Oct 2026 shape: a plant over the Ranging
// Guild's fence). soundAgo is how many ticks before the plant came into view the account heard
// triffid_appear, which content plays to the plant's owner alone: 0 is the tick it appeared, noSound
// is a session that has heard nothing, which plantMayBeOurs treats as "check every plant". The retry
// window a failed approach sets is cleared between polls, as the next poll two seconds on would find
// it expired.
func kickUnreachablePlant(t *testing.T, soundAgo, n int) (lines []string) {
	t.Helper()
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Strange plant"}, npcOps: map[int][5]string{500: {"Pick"}}})
	state := overlapState()
	if soundAgo != noSound {
		playSound(state, gamestate.SoundTriffidAppear)
		advanceTicks(state, soundAgo)
	}
	addNPC(state, 1, 500, 6, 0)
	s := &overlapSession{state: state}
	g := NewGuardian(s, &pathfind.Pack{})
	defer g.Stop()
	g.Log = func(l string) { lines = append(lines, l) }
	if ev := g.nextEvent(); ev == nil || ev.Name != "Strange plant" {
		t.Fatalf("setup: the plant was not detected: %+v", ev)
	}
	for i := 0; i < n; i++ {
		g.mu.Lock()
		for sig := range g.retryUntil {
			delete(g.retryUntil, sig)
		}
		g.mu.Unlock()
		g.kick()
	}
	return lines
}

// Live 5 Oct 2026: acct141, Acct171 and Acct226 answered "no approach path to Strange plant" 7, 12
// and 8 times in a row, a Guardian attempt every two seconds, for a plant they could not reach and had
// not heard appear. It is rejected on the first try and left alone, as a plant that answers "not here
// for you" is.
func TestAnUnreachablePlantNobodyProvedOursIsRejectedOnce(t *testing.T) {
	lines := kickUnreachablePlant(t, noSound, 8)
	all := strings.Join(lines, "\n")
	if n := strings.Count(all, "no approach path"); n != 0 {
		t.Fatalf("%d \"no approach path\" errors for a plant that cannot be ours:\n%s", n, all)
	}
	if n := strings.Count(all, "not this account's Strange plant"); n != 1 {
		t.Fatalf("want one rejection, got %d:\n%s", n, all)
	}
	if n := strings.Count(all, "walking to"); n != 1 {
		t.Fatalf("the plant was approached %d times, want once:\n%s", n, all)
	}
	for _, want := range []string{"no route to 3206,3200", "spawn sound heard=false"} {
		if !strings.Contains(all, want) {
			t.Errorf("the rejection does not say %q:\n%s", want, all)
		}
	}
}

// The plant this account did hear appear is its own, and an unreachable own plant is tried again
// every poll, as before: its owner has nothing else to do with it.
func TestAnUnreachablePlantWeHeardAppearIsStillTried(t *testing.T) {
	lines := kickUnreachablePlant(t, 0, 6)
	all := strings.Join(lines, "\n")
	if n := strings.Count(all, "no approach path"); n != 6 {
		t.Fatalf("want 6 attempts at our own plant, got %d:\n%s", n, all)
	}
	if strings.Contains(all, "not this account's") {
		t.Fatalf("our own plant was rejected:\n%s", all)
	}
}

// An owner's plant can come into view some time after its sound: the plant leaves the account's view
// and returns (acct16 at 17:28:11, a plant it picked 18 seconds later), and the ticks it has been in
// view start again, so a sound heard before that is not within a few ticks of it. The sound counts
// anywhere in the plant's lifetime, as plantMayBeOurs counts it.
func TestAnUnreachablePlantHeardEarlierInItsLifetimeIsStillTried(t *testing.T) {
	lines := kickUnreachablePlant(t, 60, 6)
	all := strings.Join(lines, "\n")
	if n := strings.Count(all, "no approach path"); n != 6 {
		t.Fatalf("want 6 attempts at a plant whose sound came 60 ticks before it was in view, got %d:\n%s", n, all)
	}
	if strings.Contains(all, "not this account's") {
		t.Fatalf("an owner's plant was rejected:\n%s", all)
	}
}

// The rule is for a Strange plant only. Every other dialog event keeps its own ownership signals
// (a greeting that names its owner, the face, the owner-only sound) and its own retry behaviour.
func TestOnlyAStrangePlantIsRejectedForBeingUnreachable(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{500: "Strange plant", 501: "Mysterious Old Man"}})
	state := overlapState()
	addNPCs(state, npcSpec{1, 500, 6, 0}, npcSpec{2, 501, 6, 1})
	if !unreachableForeignPlant(state, &Event{Name: "Strange plant", NpcIndex: 1}) {
		t.Error("an unheard plant was not judged someone else's")
	}
	if unreachableForeignPlant(state, &Event{Name: "Mysterious Old Man", NpcIndex: 2}) {
		t.Error("the rule reached a Mysterious Old Man")
	}
	if unreachableForeignPlant(state, &Event{Name: "Strange plant", NpcIndex: 99}) {
		t.Error("a plant no longer in view was rejected")
	}
	if unreachableForeignPlant(state, nil) {
		t.Error("a nil event was rejected")
	}
}
