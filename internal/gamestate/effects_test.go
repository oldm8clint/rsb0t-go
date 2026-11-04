package gamestate

import (
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
	"testing"
)

func TestCombatRetainsSimultaneousHitsAndSpot(t *testing.T) {
	s := New()
	s.scene.tick = 10
	n := &npcEntry{}
	for _, damage := range []byte{0, 3, 9, 1} {
		if !s.readNpcHitmark(packet.NewReader([]byte{damage, 1, 7, 10}), n) {
			t.Fatal("valid hit rejected")
		}
	}
	for i, damage := range []int{0, 3, 9, 1} {
		if n.combat.Hits[i].Damage != damage || !n.combat.Hits[i].Seen {
			t.Fatalf("hit slot %d: %+v", i, n.combat)
		}
	}
	s.readPlayerCombat(packet.NewReader([]byte{5, 1, 20, 25}), -1, 11)
	if s.self.combat.Hits[0].Damage != 5 || s.self.combat.Total != 25 {
		t.Fatal("self combat discarded")
	}
	n.combat.readSpot(packet.NewReader([]byte{0, 100, 0, 64, 0, 12}), 10)
	if n.combat.Spot.ID != 100 || n.combat.Spot.Height != 64 || n.combat.Spot.Delay != 12 {
		t.Fatalf("spot = %+v", n.combat.Spot)
	}
	before := n.combat
	if s.readNpcHitmark(packet.NewReader([]byte{1, 2, 3}), n) || before != n.combat {
		t.Fatal("truncated hit changed state")
	}
}

func TestProjectilePacketsDirectAndEnclosed(t *testing.T) {
	for _, enclosed := range []bool{false, true} {
		s := New()
		s.mapBuildBaseX, s.mapBuildBaseZ = 2800, 3400
		s.scene.tick = 10
		s.zoneUpdateX, s.zoneUpdateZ = 8, 16
		s.watchRender() // effects are kept only while someone watches the account
		// Tile offset 2,3; signed destination -2,+3; player target -8.
		data := []byte{0x23, 254, 3, 255, 248, 0, 10, 16, 8, 0, 5, 0, 35, 16, 24}
		if enclosed {
			s.Handle(serverprot.UPDATE_ZONE_PARTIAL_ENCLOSED, append([]byte{8, 16, serverprot.MAP_PROJANIM}, data...))
		} else {
			s.Handle(serverprot.MAP_PROJANIM, data)
		}
		if len(s.effects) != 1 {
			t.Fatalf("enclosed=%v effects=%v", enclosed, s.effects)
		}
		e := s.effects[0]
		if e.X != 2810 || e.Z != 3419 || e.ToX != 2808 || e.ToZ != 3422 || e.Target != -8 || e.Height != 64 || e.EndHeight != 32 || e.Start != 5 || e.End != 35 {
			t.Fatalf("projectile = %+v", e)
		}
		s.haveTile = true
		s.scene.state = SceneReady
		snapshot, ok := s.RenderSnapshot()
		if !ok || len(snapshot.Effects) != 1 {
			t.Fatal("effect missing from snapshot")
		}
		snapshot.Effects[0].ID = 99
		if s.effects[0].ID == 99 {
			t.Fatal("snapshot aliases effect state")
		}
		s.scene.tick = 13
		snapshot, _ = s.RenderSnapshot()
		if len(snapshot.Effects) != 0 {
			t.Fatal("expired projectile retained in snapshot")
		}
	}
}

func TestEffectRetentionBoundAndMalformedPacket(t *testing.T) {
	s := New()
	s.scene.tick = 1
	s.watchRender()
	for i := 0; i < 300; i++ {
		s.Handle(serverprot.MAP_ANIM, []byte{0, 0, 10, 32, 0, 0})
	}
	if len(s.effects) != 256 {
		t.Fatalf("effects=%d", len(s.effects))
	}
	s.Handle(serverprot.MAP_PROJANIM, []byte{0, 1})
	if len(s.effects) != 256 {
		t.Fatal("short packet changed effects")
	}
	s.scene.tick = 100
	s.watchRender() // still watched
	s.Handle(serverprot.MAP_ANIM, []byte{0, 0, 10, 32, 0, 0})
	if len(s.effects) != 1 {
		t.Fatal("expired effects not pruned")
	}
}

// An account nobody watches skips spot and projectile animations as it always did: nothing is
// kept, and the zone batch after them still decodes. Watching starts keeping them; 50 ticks
// without a RenderSnapshot stops it again and drops what was kept.
func TestUnwatchedAccountSkipsEffects(t *testing.T) {
	s := New()
	s.mapBuildBaseX, s.mapBuildBaseZ = 2800, 3400
	s.scene.tick = 10
	s.zoneUpdateX, s.zoneUpdateZ = 8, 16
	anim := []byte{0x23, 0, 10, 32, 0, 0}
	batch := append([]byte{8, 16, serverprot.MAP_ANIM}, anim...)
	batch = append(batch, serverprot.OBJ_ADD, 0x23, 0x02, 0x10, 0, 7) // obj 528 x7 on the same tile
	s.Handle(serverprot.UPDATE_ZONE_PARTIAL_ENCLOSED, batch)
	if len(s.effects) != 0 {
		t.Fatalf("unwatched account kept %v", s.effects)
	}
	if items := s.GroundItems(); len(items) != 1 || items[0].ID != 528 || items[0].Count != 7 || items[0].X != 2810 || items[0].Z != 3419 {
		t.Fatalf("the batch after a skipped animation decoded as %+v", items)
	}
	s.watchRender()
	s.Handle(serverprot.MAP_ANIM, anim)
	if len(s.effects) != 1 {
		t.Fatalf("watched account effects = %v", s.effects)
	}
	s.scene.tick += effectsWatchTicks + 1
	s.Handle(serverprot.MAP_ANIM, anim)
	if len(s.effects) != 0 {
		t.Fatalf("effects kept %d ticks after the last watch: %v", effectsWatchTicks+1, s.effects)
	}
}
