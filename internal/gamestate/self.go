package gamestate

// The local player's own extended-info state: Game.myName, reader.combatLevel,
// reader.selfAnim/Game.animating, Game.inCombat and Game.attackedByPlayer from the TS scripting
// API. PLAYER_INFO carries the local player's extended block first in its update list
// (players.go's index -1); this file keeps what it holds.
//
// Timing is in server ticks. The client stamps combatCycle = loopCycle + 400 on a hitmark and
// ClientAdapter counts an entity as in combat while that stamp is more than
// 400 - COMBAT_WINDOW_MS/20 cycles ahead, i.e. for 6000 ms, which is combatWindowTicks ticks.
// A tick count keeps meaning the same on a local engine sped up with ::speed.

// combatWindowTicks is ClientAdapter's COMBAT_WINDOW_MS (6000) in 600 ms ticks.
const combatWindowTicks = 10

type selfTracker struct {
	combat     RenderCombat
	name       string
	appearance []byte

	combatLevel int
	haveLevel   bool

	anim     int
	animTick int
	haveAnim bool

	face     int
	haveFace bool

	hitTick int
	hitSeen bool

	poisonTick int
	poisonSeen bool
}

// HitmarkPoison is the hitmark damage type poison deals (content hitmark.constant
// ^hitmark_poison); 0 is a block and 1 ordinary damage.
const HitmarkPoison = 2

func inCombatWindow(hitSeen bool, hitTick, now int) bool {
	return hitSeen && now-hitTick < combatWindowTicks
}

// playerInfoTick is the tick number a PLAYER_INFO being decoded will have once observeScene
// counts it, so stamps from inside it line up with Tick() afterwards.
func (s *State) playerInfoTick() int {
	return s.scene.tick + 1
}

// SelfName ports Game.myName: the local player's display name from its own appearance update;
// ok is false until one has arrived.
func (s *State) SelfName() (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.self.name, s.self.name != ""
}

// CombatLevel ports reader.combatLevel from the local player's appearance update.
func (s *State) CombatLevel() (int, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.self.combatLevel, s.self.haveLevel
}

// SelfAnim returns the last animation the server started on the local player (-1 for none or
// a cancelled one) and the tick it arrived on.
func (s *State) SelfAnim() (seq int, tick int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.self.haveAnim {
		return -1, 0
	}
	return s.self.anim, s.self.animTick
}

// Animating approximates Game.animating: an animation started within the last withinTicks
// ticks and not cancelled since. The client clears its animation when the sequence's frames run
// out, which needs the seq config; the server sends nothing at that point, so the caller picks
// the window (a skilling animation repeats every few ticks while the action continues).
func (s *State) Animating(withinTicks int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.self.haveAnim && s.self.anim != -1 && s.scene.tick-s.self.animTick < withinTicks
}

// InCombat ports Game.inCombat: a hitmark on the local player within the last
// combatWindowTicks ticks.
func (s *State) InCombat() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return inCombatWindow(s.self.hitSeen, s.self.hitTick, s.scene.tick)
}

// HitWithin reports whether the local player took a hitmark (a block counts) in the last ticks
// server ticks. InCombat's window runs ten ticks from the last hit, so it stays true for seconds
// after a fight is won; a monster still swinging at the account hits it again within its attack
// speed.
func (s *State) HitWithin(ticks int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.self.hitSeen && s.scene.tick-s.self.hitTick < ticks
}

// LastSelfHit is the tick of the newest hitmark on the local player (a block of 0 counts); ok is false when none has been seen.
// The tick is the PLAYER_INFO ordinal Tick() counts, so a loop fed by EventTick can ask "was I hit in this tick" exactly.
func (s *State) LastSelfHit() (tick int, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.self.hitTick, s.self.hitSeen
}

// LastPoisonHit is the tick of the last poison hitmark on the local player; ok is false when none
// has been seen. Poison deals its damage once every 30 ticks while it lasts (poison.rs2
// [timer,poison]), so a poison hit within the last 30-odd ticks means the account is poisoned.
func (s *State) LastPoisonHit() (tick int, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.self.poisonTick, s.self.poisonSeen
}

// SelfFaceEntity ports reader.selfFaceEntity: -1 for nobody, an NPC index, or a player slot
// plus 32768.
func (s *State) SelfFaceEntity() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.self.haveFace {
		return -1
	}
	return s.self.face
}

// AttackedByPlayer ports Game.attackedByPlayer: the local player faces another player, which
// for a bot that never starts PvP means auto retaliate turned it toward an attacker.
func (s *State) AttackedByPlayer() bool {
	return s.SelfFaceEntity() >= playerFaceBase
}

// SelfHits returns the local player's last four hitmarks: damage, damage type and the tick each
// arrived on (Seen false for an empty slot). A slot is reused three ticks after its hit, so a
// caller that wants every hit reads this at least that often.
func (s *State) SelfHits() [4]RenderHit {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.self.combat.Hits
}
