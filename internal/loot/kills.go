package loot

import (
	"sync"
	"time"
)

// KILL ATTRIBUTION: which kill a drop on the ground came from.
//
// A kill is reported as one event on its npc's source, with no items, the moment the fight
// verdict lands. What it dropped is picked up afterwards, by a different piece of code, and is
// measured as the pack's own change around the take. The two have to meet somewhere or the drop
// has no source to go on — which is exactly what happened to ScarabSwarm's cows: 97 kills on
// kill:cow:81 with no items, and their 106 Bones filed under a generic "Ground" openable with no
// events (live /api/loot, 2026-09-23), because the pickup code had nothing that remembered the
// kill.
//
// KillLog is that memory. A kill notes its source and tile; a later pickup asks which kill a drop
// at a tile most plausibly came from. The rule is the one internal/bot's combat scripts already
// used for their own bones (lootcombat.go, before this moved here so a runner package can share
// it without importing internal/bot): the newest kill within KillNearTiles of the drop on the
// same level, else the newest kill at all, both inside KillWindow.
//
// THE FLEET'S OWN KILLS. An account's log knows only what it killed, but a swarm of accounts shares
// one pen: live on 73ff4228 (5 Oct 2026) the panel had 74 moss giant kills with 5 Big bones and 235
// ogre kills with 35, while a separate "take: big bones" card held 164 takes from 44 accounts, and
// Black sq shields, runes, coins and herbs each had a one-take card of their own. Those were drops
// from kills another account made, or ones taken after this account's own kill had aged out. A
// KillLog that Shares a world's FleetKills therefore asks it next: a kill by any account of this
// process within KillNearTiles of the drop on the same level and inside KillWindow. Only after
// that does the newest kill of its own at any distance claim the drop. A drop credited that way
// adds items to the npc, never a kill: the kill was counted by the account that made it.

// DropLifetime is how long a kill's drop lies on the ground: content adds it with
// ^lootdrop_duration = 200 ticks (scripts/drop tables/configs/lootdrop.constant; npc_default_death and
// every table in scripts/drop tables use it), World.addObj sets the obj's lifecycle to that duration,
// and for the hero (npc_findhero) it is private for the first Obj.REVEAL = 100 ticks and public for the
// rest: a minute of the killer's, a minute of anyone's. Checked on 5 Oct 2026 against Engine-TS 289.
const DropLifetime = 200 * 600 * time.Millisecond

// KillWindow is how long a kill can still claim a drop: DropLifetime (two minutes) plus a minute of
// slack for a kill that was noted late, so a pile taken in its last seconds still finds its kill. It was
// three minutes on the comment "content drops stay on the ground for about three minutes"; the drop's
// real life is two, so no pile can outlive its kill's note.
const KillWindow = 3 * time.Minute

// KillNearTiles is how close a drop must be to a kill's tile to be matched to that kill first.
const KillNearTiles = 4

// killLogCap bounds the memory; a fight loop kills far fewer than this inside KillWindow.
const killLogCap = 16

// fleetKillCap bounds a world's shared log: a few hundred fighting accounts make well under this
// many kills inside KillWindow.
const fleetKillCap = 2048

type killNote struct {
	src         Source
	x, z, level int
	at          time.Time
}

// KillLog remembers one account's recent kills for drop attribution. The zero value is ready to
// use and it is safe for concurrent use; a nil *KillLog remembers nothing and attributes nothing.
type KillLog struct {
	mu     sync.Mutex
	recent []killNote
	// limit is how many kills a shared log keeps at least (it trims at twice that); 0 is an
	// account's own log, which keeps exactly killLogCap.
	limit int
	// fleet returns the shared log of the world the account is on now (Share); nil for none.
	fleet func() *KillLog
}

// Share makes this log note every kill in, and ask about a drop, the log fleet returns: the
// world's FleetKills, read at each call so a world hop changes which one. A nil fleet stops
// sharing.
func (k *KillLog) Share(fleet func() *KillLog) {
	if k == nil {
		return
	}
	k.mu.Lock()
	k.fleet = fleet
	k.mu.Unlock()
}

// sharedLog is the log Share named, nil for none or when it is this log itself.
func (k *KillLog) sharedLog() *KillLog {
	k.mu.Lock()
	fleet := k.fleet
	k.mu.Unlock()
	if fleet == nil {
		return nil
	}
	if f := fleet(); f != k {
		return f
	}
	return nil
}

var fleetKills = struct {
	mu sync.Mutex
	by map[int]*KillLog
}{by: map[int]*KillLog{}}

// FleetKills is the shared kill log of one world, for every account of this process on it.
func FleetKills(world int) *KillLog {
	fleetKills.mu.Lock()
	defer fleetKills.mu.Unlock()
	k := fleetKills.by[world]
	if k == nil {
		k = &KillLog{limit: fleetKillCap}
		fleetKills.by[world] = k
	}
	return k
}

// Note remembers one kill of src at a tile, here and in the shared log (Share).
func (k *KillLog) Note(src Source, x, z, level int, at time.Time) {
	if k == nil {
		return
	}
	k.add(killNote{src: src, x: x, z: z, level: level, at: at})
	if f := k.sharedLog(); f != nil {
		f.add(killNote{src: src, x: x, z: z, level: level, at: at})
	}
}

func (k *KillLog) add(n killNote) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.recent = append(k.recent, n)
	if k.limit <= 0 {
		if len(k.recent) > killLogCap {
			k.recent = append([]killNote(nil), k.recent[len(k.recent)-killLogCap:]...)
		}
		return
	}
	// A shared log is trimmed in halves rather than by one, so a full one does not copy itself on
	// every kill.
	if len(k.recent) > 2*k.limit {
		k.recent = append([]killNote(nil), k.recent[len(k.recent)-k.limit:]...)
	}
}

// SourceFor is the kill a drop at x,z,level came from: the newest kill of this account within
// KillNearTiles of it on the same level, else the newest such kill in the shared log (Share),
// else the newest kill of this account at all, each inside KillWindow of now. ok is false when no
// kill in the window can claim it.
func (k *KillLog) SourceFor(x, z, level int, now time.Time) (Source, bool) {
	if k == nil {
		return Source{}, false
	}
	own, near, ok := k.find(x, z, level, now)
	if near {
		return own, true
	}
	if f := k.sharedLog(); f != nil {
		if src, fleetNear, _ := f.find(x, z, level, now); fleetNear {
			return src, true
		}
	}
	return own, ok
}

// find is the newest kill within KillNearTiles of the drop (near true), else the newest kill in
// the window at all (near false, ok true), else nothing.
func (k *KillLog) find(x, z, level int, now time.Time) (src Source, near, ok bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	for i := len(k.recent) - 1; i >= 0; i-- {
		kill := &k.recent[i]
		if now.Sub(kill.at) > KillWindow {
			break
		}
		if DropsNothing(kill.src) {
			continue // a pile cannot be the loot of an npc that drops nothing (nodrop.go)
		}
		if !ok {
			src, ok = kill.src, true
		}
		dx, dz := kill.x-x, kill.z-z
		if dx < 0 {
			dx = -dx
		}
		if dz < 0 {
			dz = -dz
		}
		if kill.level == level && dx <= KillNearTiles && dz <= KillNearTiles {
			return kill.src, true, true
		}
	}
	return src, false, ok
}

// Latest is the newest kill this account's log remembers, however long ago: the card of last resort
// for a pile that no kill within KillWindow can claim (PickupSource). It never asks the shared log:
// it is what THIS account was last fighting. ok is false for a nil log or one with no kill.
func (k *KillLog) Latest() (Source, bool) {
	if k == nil {
		return Source{}, false
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	for i := len(k.recent) - 1; i >= 0; i-- {
		if !DropsNothing(k.recent[i].src) {
			return k.recent[i].src, true
		}
	}
	return Source{}, false
}
