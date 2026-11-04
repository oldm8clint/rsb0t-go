package loot

import (
	"testing"
	"time"
)

// A drop in a shared pen goes to the kill beside it, whichever account of the fleet made it,
// before this account's own newest kill elsewhere; the account's own kill beside it still wins.
func TestSharedKillLogClaimsAnotherAccountsDrop(t *testing.T) {
	const world = 9101 // a world no other test uses: FleetKills is process-wide
	now := time.Now()
	giant := Source{Kind: KindNPC, Action: ActionKill, Name: "Moss giant", ID: 112}
	ogre := Source{Kind: KindNPC, Action: ActionKill, Name: "Ogre", ID: 114}
	cow := Source{Kind: KindNPC, Action: ActionKill, Name: "Cow", ID: 81}

	var a, b KillLog
	a.Share(func() *KillLog { return FleetKills(world) })
	b.Share(func() *KillLog { return FleetKills(world) })
	a.Note(giant, 2555, 3405, 0, now.Add(-time.Minute))
	b.Note(ogre, 2500, 3100, 0, now.Add(-30*time.Second))

	// b took a's giant's Big bones: the giant beside them, not b's own ogre far away.
	if src, ok := b.SourceFor(2556, 3406, 0, now); !ok || src != giant {
		t.Fatalf("b's take beside a's giant went to %+v (%v), want the giant", src, ok)
	}
	// Nothing of the fleet's beside the drop: b's own newest kill, as before.
	if src, ok := b.SourceFor(2700, 3500, 0, now); !ok || src != ogre {
		t.Fatalf("a drop by nobody's kill went to %+v (%v), want b's own newest", src, ok)
	}
	// b's own kill beside the drop beats a's.
	b.Note(cow, 2554, 3404, 0, now.Add(-10*time.Second))
	if src, ok := b.SourceFor(2555, 3405, 0, now); !ok || src != cow {
		t.Fatalf("b's own adjacent kill lost to the fleet's: %+v (%v)", src, ok)
	}
	// Another world's log is not consulted, and an unshared log only knows its own kills.
	var c KillLog
	c.Share(func() *KillLog { return FleetKills(world + 1) })
	if _, ok := c.SourceFor(2556, 3406, 0, now); ok {
		t.Fatal("a kill on another world claimed the drop")
	}
	var lone KillLog
	if _, ok := lone.SourceFor(2556, 3406, 0, now); ok {
		t.Fatal("an unshared log claimed a drop it never saw")
	}
	// Past KillWindow nothing claims it.
	if _, ok := b.SourceFor(2556, 3406, 0, now.Add(KillWindow+time.Minute)); ok {
		t.Fatal("a stale kill claimed the drop")
	}
}

// The shared log keeps at least fleetKillCap kills and trims in halves.
func TestFleetKillLogBounded(t *testing.T) {
	k := FleetKills(9103)
	now := time.Now()
	src := Source{Kind: KindNPC, Action: ActionKill, Name: "Cow", ID: 81}
	for i := 0; i < 5*fleetKillCap; i++ {
		k.Note(src, 3000+i%50, 3000, 0, now)
	}
	k.mu.Lock()
	n := len(k.recent)
	k.mu.Unlock()
	if n < fleetKillCap || n > 2*fleetKillCap {
		t.Fatalf("shared log holds %d kills, want %d..%d", n, fleetKillCap, 2*fleetKillCap)
	}
}

// A kill can claim a drop for as long as the drop can lie on the ground: content adds it for 200 ticks
// (^lootdrop_duration), private to the killer for the first 100 (Obj.REVEAL) and public for the rest, so
// a pile an account takes in the drop's last second still finds the kill, and one a second later is gone.
func TestKillWindowOutlastsADrop(t *testing.T) {
	if KillWindow < DropLifetime+30*time.Second {
		t.Fatalf("KillWindow %s does not outlast a drop's life %s with room for a late note", KillWindow, DropLifetime)
	}
	const world = 9102
	now := time.Now()
	ogre := Source{Kind: KindNPC, Action: ActionKill, Name: "Ogre", ID: 114}
	var a, b KillLog
	a.Share(func() *KillLog { return FleetKills(world) })
	b.Share(func() *KillLog { return FleetKills(world) })
	a.Note(ogre, 2503, 3095, 0, now.Add(-DropLifetime+time.Second))
	// The public minute of a's kill's drop: b takes it 100 seconds after the kill, and finds the kill.
	if src, ok := b.SourceFor(2503, 3095, 0, now); !ok || src != ogre {
		t.Fatalf("a pile taken %s after the kill found %+v (%v)", DropLifetime-time.Second, src, ok)
	}
}
