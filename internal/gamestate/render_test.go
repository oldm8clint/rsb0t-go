package gamestate

import "testing"

func TestRenderSnapshotReadinessAndIsolation(t *testing.T) {
	s := New()
	if _, ok := s.RenderSnapshot(); ok {
		t.Fatal("fresh state must not produce a render snapshot")
	}
	s.haveTile = true
	s.mapBuildBaseX, s.mapBuildBaseZ = 2800, 3400
	s.localX, s.localZ = 9, 41
	s.scene.tick, s.scene.state = 1, SceneReady
	s.self.appearance = []byte{1, 2, 3}
	s.players[7] = &playerEntry{name: "Other Player", localX: 10, localZ: 42, anim: 808, animTick: 5, faceEntity: 32768 + 1}
	s.playerAppearances[7] = []byte{4, 5, 6}
	s.groundItems[groundItemKey{0, 2810, 3442}] = []GroundItem{{ID: 995, Count: 12, X: 2810, Z: 3442}}
	view, ok := s.RenderSnapshot()
	if !ok || view.X != 2809 || view.Z != 3441 || len(view.Players) != 1 {
		t.Fatalf("snapshot: %+v, ready=%v", view, ok)
	}
	if len(view.Objs) != 1 || view.Objs[0].ID != 995 || view.Objs[0].Count != 12 {
		t.Fatalf("snapshot ground items = %+v", view.Objs)
	}
	if p := view.Players[0]; p.Anim != 808 || p.AnimTick != 5 || p.FaceEntity != 32769 {
		t.Fatalf("snapshot player anim/face = %+v", p)
	}
	if view.Players[0].Name != "Other Player" {
		t.Fatalf("snapshot player name = %q, want the decoded appearance name", view.Players[0].Name)
	}
	view.Appearance[0] = 99
	view.Players[0].Appearance[0] = 99
	if s.self.appearance[0] != 1 || s.playerAppearances[7][0] != 4 {
		t.Fatal("snapshot appearance aliases live game state")
	}
	s.scene.state = SceneBuilding
	if _, ok := s.RenderSnapshot(); ok {
		t.Fatal("rebuilding scene must not produce a snapshot")
	}
	s.scene.state = SceneReady
	s.LoggedOut = true
	if _, ok := s.RenderSnapshot(); ok {
		t.Fatal("logged-out state must not produce a snapshot")
	}
}
