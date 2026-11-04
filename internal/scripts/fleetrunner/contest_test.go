package fleetrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/fleetplan"
)

// The contested-target skip NearestReachableMonster applies (survival.go) to every candidate, at
// the runner: another player's fight, another swarm account's claim and a refusal this account was
// given are all skipped; a free target and this account's own are not.
func TestContestedSkipsEveryoneElsesTarget(t *testing.T) {
	Targets = fleetplan.NewTargetBoard()
	defer func() { Targets = fleetplan.NewTargetBoard() }()
	d := &Deps{Session: &packSession{st: gamestate.New()}, World: 1, Me: "spxa"}
	now := time.Now()
	self := d.Session.State().Self()
	const playerFace = 32768

	free := gamestate.NPC{Index: 10, FaceEntity: -1}
	if Contested(d, free, now) {
		t.Fatal("a free Cow is not contested")
	}
	other := gamestate.NPC{Index: 11, FaceEntity: playerFace + self + 5}
	if !Contested(d, other, now) {
		t.Fatal("a Cow facing another player is contested")
	}
	fighting := gamestate.NPC{Index: 12, FaceEntity: -1, InCombat: true}
	if !Contested(d, fighting, now) {
		t.Fatal("a Cow in combat that is not fighting this account is contested")
	}
	Targets.Claim(1, 13, "spxb", now, fleetplan.TargetClaimTTL)
	if !Contested(d, gamestate.NPC{Index: 13, FaceEntity: -1}, now) {
		t.Fatal("another swarm account's claim is contested")
	}
	Targets.Skip(1, 14, "spxa", now.Add(time.Minute))
	if !Contested(d, gamestate.NPC{Index: 14, FaceEntity: -1}, now) {
		t.Fatal("a target this account was refused is skipped")
	}
	// Our own claim, in combat with us: not contested.
	Targets.Claim(1, 15, "spxa", now, fleetplan.TargetClaimTTL)
	if Contested(d, gamestate.NPC{Index: 15, FaceEntity: -1, InCombat: true}, now) {
		t.Fatal("this account's own claimed fight is not contested")
	}
	// The same index on ANOTHER world is a different npc.
	if Contested(&Deps{Session: d.Session, World: 2, Me: "spxa"}, gamestate.NPC{Index: 13, FaceEntity: -1}, now) {
		t.Fatal("a claim on world 1 contests world 2's npc of the same index")
	}
}
