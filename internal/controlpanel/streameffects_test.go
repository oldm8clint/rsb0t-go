package controlpanel

import (
	"rsb0t-go/internal/gamestate"
	"testing"
)

func TestStreamCombatAndEffectDeltas(t *testing.T) {
	snap := sceneSnapshot()
	previous := projectStreamScene(&snap, streamStatus{}, nil)
	snap.Combat = gamestate.RenderCombat{Seen: true, Health: 8, Total: 10, Tick: 900}
	snap.Combat.Hits[0] = gamestate.RenderHit{Damage: 2, Type: 1, Seen: true, Tick: 900}
	npcCombat := snap.Combat
	snap.NPCs[0].Combat = &npcCombat
	snap.Effects = []gamestate.RenderEffect{{ID: 10, Tick: 900, Projectile: true}}
	next := projectStreamScene(&snap, streamStatus{}, nil)
	d := diffStreamScene(1, &previous, &next)
	if d.empty() || d.Self == nil || d.Self.Combat.Hits[0].Damage != 2 || len(d.Upsert) != 1 || d.Effects == nil || len(*d.Effects) != 1 {
		t.Fatalf("combat delta = %+v", d)
	}
	snap.Effects = nil
	expired := projectStreamScene(&snap, streamStatus{}, nil)
	clear := diffStreamScene(2, &next, &expired)
	if clear.Effects == nil || len(*clear.Effects) != 0 {
		t.Fatal("effect removal must carry an explicit empty array")
	}
}

func TestStreamCombatEqualityAndExpiry(t *testing.T) {
	snap := sceneSnapshot()
	before := projectStreamScene(&snap, streamStatus{}, nil)
	if before.Self.Combat != nil || before.Entities[0].Combat != nil {
		t.Fatal("idle combat should be omitted")
	}
	snap.Combat = gamestate.RenderCombat{Seen: true, Health: 8, Total: 10, Tick: 900}
	a := projectStreamScene(&snap, streamStatus{}, nil)
	b := projectStreamScene(&snap, streamStatus{}, nil)
	if d := diffStreamScene(1, &a, &b); !d.empty() {
		t.Fatal("equal combat values must not resend every observation")
	}
	snap.UI.Tick = 910
	expired := projectStreamScene(&snap, streamStatus{}, nil)
	if expired.Self.Combat != nil {
		t.Fatal("expired combat should be omitted")
	}
	if d := diffStreamScene(2, &a, &expired); d.Self == nil {
		t.Fatal("combat expiration needs a self update")
	}
}
