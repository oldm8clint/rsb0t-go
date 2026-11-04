package randomevents

import (
	"testing"

	"rsb0t-go/internal/gamestate"
)

// Covers BigFishNearby (detect.go) — the NPC-name-based signal for the classic OSRS "Big
// fish" random event, added 2026-09-16 after ground-truthing against the local test
// engine's own content scripts found this was a total gap (grep -rni "big fish"
// internal/randomevents/ matched nothing). Only unit-tested here, against synthetic
// gamestate: see COLLAB.md/the task notes for why this specific event was not proven via
// a live reproduction (it fires on a random roll during ordinary fishing, with no known
// cheat to force it in this build) — the recovery half (fisherrunner.RecoverLostTool) has
// its own synthetic-state coverage in internal/scripts/fisherrunner.

func TestBigFishNearbyMatchesByName(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{700: bigFishNpcName}})
	s := gamestate.New()
	setTile(s, 3200, 3200, 0)
	addNPC(s, 1, 700, 3, -2) // chebyshev distance 3
	if !BigFishNearby(s, 3200, 3200, 5) {
		t.Fatalf("expected BigFishNearby to find the npc within radius")
	}
}

func TestBigFishNearbyOutsideRadius(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{700: bigFishNpcName}})
	s := gamestate.New()
	setTile(s, 3200, 3200, 0)
	addNPC(s, 1, 700, 10, 0)
	if BigFishNearby(s, 3200, 3200, 5) {
		t.Fatalf("expected BigFishNearby to ignore an npc outside radius")
	}
}

func TestBigFishNearbyIgnoresOtherNames(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{700: "Fishing spot"}})
	s := gamestate.New()
	setTile(s, 3200, 3200, 0)
	addNPC(s, 1, 700, 1, 0)
	if BigFishNearby(s, 3200, 3200, 5) {
		t.Fatalf("expected BigFishNearby to ignore an ordinarily-named npc")
	}
}

func TestBigFishNearbyNoResolver(t *testing.T) {
	SetNameResolver(nil)
	s := gamestate.New()
	setTile(s, 3200, 3200, 0)
	addNPC(s, 1, 700, 1, 0)
	if BigFishNearby(s, 3200, 3200, 5) {
		t.Fatalf("expected BigFishNearby to report false with no resolver installed")
	}
}

func TestBigFishNearbyCaseInsensitive(t *testing.T) {
	withResolver(t, fakeResolver{npcNames: map[int]string{700: "bIg FiSh"}})
	s := gamestate.New()
	setTile(s, 3200, 3200, 0)
	addNPC(s, 1, 700, 0, 0)
	if !BigFishNearby(s, 3200, 3200, 5) {
		t.Fatalf("expected BigFishNearby to match case-insensitively")
	}
}
