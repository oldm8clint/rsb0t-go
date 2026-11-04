package mulerunner

import (
	"os"
	"testing"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/mulereceiverplan"
)

// collisionPackPath is the real baked pack, if this machine has one. Two env names are read
// because the repo has both in use: CLAUDE.md documents RSB0T_COLLISION_PACK and several
// script-package tests use RS2BOT_TEST_COLLISION_PACK.
func collisionPackPath() string {
	if p := os.Getenv("RSB0T_COLLISION_PACK"); p != "" {
		return p
	}
	return os.Getenv("RS2BOT_TEST_COLLISION_PACK")
}

// TestTheMulesOwnBankBoothRefusesADiagonal pins the geometry the booth approach was silently
// losing to, at the tile the mule actually posts at.
//
// Measured against the real pack with `cmd/mulehandofftest -geometry`: every one of the six bank
// booths around mulereceiverplan.SeersStand has SIX walkable neighbours, and the engine accepts a
// click from only TWO of them (the booth north and south tiles). openNearestBankBooth used to
// approach with walkNear(booth, 1), whose own arrival test is Chebyshev — so it stopped and
// clicked from any of the six, and four of those six produce no error, no chat line and no
// refusal, just a bank that never opens and a 20-second wait burned per booth. walkToInteract
// asks pathfind.CanInteractFrom instead.
//
// The booth tile is hardcoded because it is a fact about the baked map, not about this package.
func TestTheMulesOwnBankBoothRefusesADiagonal(t *testing.T) {
	path := collisionPackPath()
	if path == "" {
		t.Skip("set RSB0T_COLLISION_PACK to check the real booths at the mule's post")
	}
	pk, err := pathfind.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	post := mulereceiverplan.SeersStand
	booth := pathfind.NavPoint{X: 2724, Z: 3494, Level: post.Level}

	accepted := pk.InteractTiles(booth)
	if len(accepted) != 2 {
		t.Fatalf("the booth at %d,%d accepts a click from %d tile(s), want the 2 measured (%v) — if the bake moved, re-measure with cmd/mulehandofftest -geometry rather than relaxing this test", booth.X, booth.Z, len(accepted), accepted)
	}
	for _, want := range []pathfind.NavPoint{{X: 2724, Z: 3493}, {X: 2724, Z: 3495}} {
		if !pk.CanInteractFrom(want, booth) {
			t.Errorf("the engine's own rule refuses %d,%d, which is one of the two tiles the booth can actually be used from", want.X, want.Z)
		}
	}
	// The whole bug in one assertion: a tile walkNear(booth, 1) would have stopped on and clicked
	// from, which the engine ignores in silence.
	for _, diagonal := range []pathfind.NavPoint{{X: 2723, Z: 3493}, {X: 2725, Z: 3493}, {X: 2723, Z: 3495}, {X: 2725, Z: 3495}} {
		if !pk.Walkable(diagonal.X, diagonal.Z, booth.Level) {
			continue // a diagonal that cannot be stood on was never part of the risk
		}
		if pk.CanInteractFrom(diagonal, booth) {
			t.Errorf("%d,%d is a diagonal of the booth and answered interactable, which is the bug this fix exists for", diagonal.X, diagonal.Z)
		}
	}
}
