package gamestate

import (
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
	"strings"
)

// Scene state, server ticks, run energy and weight: the Game.* readers the TS scripting API
// (reference/server-api-docs.md, "Game") builds its TaskBot/TreeBot scene gate and
// Execution.delayTicks on.
//
// Scene state ports Client.ts's sceneState. REBUILD_NORMAL sets it to 1 (building) unless the
// build centre is unchanged and the scene is already ready; the build finishes, setting 2
// (ready), once the map files are loaded and a PLAYER_INFO has arrived since the rebuild
// (awaitingPlayerInfo). A headless client loads no map files, so here the first PLAYER_INFO
// after a rebuild completes it. A logout resets it to 0. Game.sceneReady() is "in game, scene
// state 2, and a known world tile", and TaskBot/TreeBot refuse to dispatch until it holds,
// because input sent into a half-built scene fails silently upstream (#445 there).
//
// A server tick is one observed PLAYER_INFO, the same thing the TS runtime's BotHost.tickCount
// counts (Bot.ts: "N observed Game.tick() / PLAYER_INFO advances"). The engine sends exactly one
// per player per world cycle.
//
// Run energy and weight come from UPDATE_RUNENERGY (Engine-TS UpdateRunEnergyEncoder:
// p1(energy / 100), a 0-100 percentage) and UPDATE_RUNWEIGHT (p2(kg), read by Client.ts as
// g2b, so it can be negative with weight-reducing gear).
//
// This file only observes packets other cases already consumed; it reads the payload with its
// own reader so the REBUILD_NORMAL and PLAYER_INFO cases in state.go (and position.go's
// handling of them) stay untouched.

// Scene build states, mirroring Client.ts's sceneState values.
const (
	SceneIdle     = 0
	SceneBuilding = 1
	SceneReady    = 2
)

// sceneSize is the build area's width in tiles (Client.ts's SCENE_SIZE).
const sceneSize = 104

// Player varps the Game namespace reads, from content's pack/varp.pack and
// interface_controls/configs (all transmit=yes).
const (
	// VarpCombatMode is com_mode: the selected button of the weapon's combat-style tab.
	VarpCombatMode = 43
	// VarpAutoRetaliate is option_nodef, inverted: 0 means auto retaliate is on.
	VarpAutoRetaliate = 172
	// VarpRun is option_run: 1 means running.
	VarpRun = 173
)

type sceneTracker struct {
	state              int
	centreX, centreZ   int
	haveCentre         bool
	awaitingPlayerInfo bool

	tick int
	// view is when the ticks arrived, for ViewLag; viewOverride, when set, is what it reports instead (OverrideViewLag).
	view         viewClock
	viewOverride *ViewLag

	energy, weight         int
	haveEnergy, haveWeight bool

	// multiway is SET_MULTIWAY's flag: the player stands in a multi-combat area.
	multiway, haveMultiway bool
	// rebootAtTick is the tick the server said it will reboot at (UPDATE_REBOOT_TIMER), 0 for
	// none announced.
	rebootAtTick int

	// minimapState is MINIMAP_TOGGLE's value (0 normal, 1 no map clicks, 2 map hidden).
	minimapState int
	haveMinimap  bool
	// playerOps are SET_PLAYER_OP's right-click options on other players (1-5, "" unset).
	playerOps        [5]string
	playerOpPriority [5]bool
}

// observeScene updates the scene tracker for opcode. Called from Handle with s.mu held, after
// the opcode's own case has run.
func (s *State) observeScene(opcode int, payload []byte) []Event {
	sc := &s.scene
	switch opcode {
	case serverprot.REBUILD_NORMAL:
		if len(payload) < 4 {
			return nil
		}
		r := packet.NewReader(payload)
		zoneX, zoneZ := int(r.G2()), int(r.G2())
		if sc.state == SceneReady && sc.haveCentre && zoneX == sc.centreX && zoneZ == sc.centreZ {
			return nil
		}
		sc.centreX, sc.centreZ, sc.haveCentre = zoneX, zoneZ, true
		s.effects = nil
		sc.state = SceneBuilding
		sc.awaitingPlayerInfo = true
	case serverprot.PLAYER_INFO:
		sc.tick++
		sc.view.note(viewNow()) // the wall time of every tick, for ViewLag (viewlag.go)
		if sc.state == SceneBuilding && sc.awaitingPlayerInfo {
			sc.awaitingPlayerInfo = false
			sc.state = SceneReady
		}
		return []Event{{Kind: EventTick, Tick: sc.tick}}
	case serverprot.UPDATE_RUNENERGY:
		if len(payload) < 1 {
			return nil
		}
		sc.energy, sc.haveEnergy = int(payload[0]), true
	case serverprot.UPDATE_RUNWEIGHT:
		if len(payload) < 2 {
			return nil
		}
		sc.weight, sc.haveWeight = int(packet.NewReader(payload).G2b()), true
	case serverprot.SET_MULTIWAY:
		// Client.ts: inMultizone = g1().
		if len(payload) < 1 {
			return nil
		}
		sc.multiway, sc.haveMultiway = payload[0] != 0, true
	case serverprot.UPDATE_REBOOT_TIMER:
		// Client.ts: rebootTimer = g2() * 30 client cycles of 20 ms, i.e. g2() server ticks.
		if len(payload) < 2 {
			return nil
		}
		if ticks := int(packet.NewReader(payload).G2()); ticks > 0 {
			sc.rebootAtTick = sc.tick + ticks
		} else {
			sc.rebootAtTick = 0
		}
	case serverprot.MINIMAP_TOGGLE:
		// Client.ts: minimapState = g1().
		if len(payload) < 1 {
			return nil
		}
		sc.minimapState, sc.haveMinimap = int(payload[0]), true
	case serverprot.SET_PLAYER_OP:
		// Client.ts: index g1, priority g1, op gjstr; "null" clears the option.
		if len(payload) < 3 {
			return nil
		}
		r := packet.NewReader(payload)
		index := int(r.G1())
		priority := r.G1()
		op := r.GJStr()
		if index >= 1 && index <= 5 {
			if strings.EqualFold(op, "null") {
				op = ""
			}
			sc.playerOps[index-1] = op
			sc.playerOpPriority[index-1] = priority == 0
		}
	case serverprot.LOGOUT:
		sc.state = SceneIdle
		sc.awaitingPlayerInfo = false
		sc.view = viewClock{}
	}
	return nil
}

// Tick returns how many server ticks (PLAYER_INFO packets) this session has observed.
func (s *State) Tick() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.scene.tick
}

// SceneState returns the raw scene build state: SceneIdle, SceneBuilding or SceneReady.
func (s *State) SceneState() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.scene.state
}

// InGame reports whether the session has seen at least one PLAYER_INFO and has not been
// logged out.
func (s *State) InGame() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.scene.tick > 0 && !s.LoggedOut
}

// SceneReady ports Game.sceneReady: in game, the scene fully built, and a known world tile.
func (s *State) SceneReady() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.scene.tick > 0 && !s.LoggedOut && s.scene.state == SceneReady && s.haveTile
}

// Energy returns run energy as a 0-100 percentage; ok is false until UPDATE_RUNENERGY arrives.
func (s *State) Energy() (energy int, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.scene.energy, s.scene.haveEnergy
}

// Multiway reports whether the player stands in a multi-combat area, where several monsters may
// attack at once (SET_MULTIWAY); ok is false until the server has sent the flag.
func (s *State) Multiway() (multi, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.scene.multiway, s.scene.haveMultiway
}

// RebootInTicks is how many server ticks (0.6 s each) remain before the server reboot it has
// announced (UPDATE_REBOOT_TIMER, the "System update in" countdown); ok is false when none is
// pending.
func (s *State) RebootInTicks() (ticks int, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.scene.rebootAtTick == 0 {
		return 0, false
	}
	left := s.scene.rebootAtTick - s.scene.tick
	if left < 0 {
		left = 0
	}
	return left, true
}

// MinimapState is the server's MINIMAP_TOGGLE value: 0 normal, 1 the map cannot be clicked, 2
// it is hidden (the client's own minimapState). ok is false until the server has sent one.
func (s *State) MinimapState() (state int, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.scene.minimapState, s.scene.haveMinimap
}

// PlayerOps are the right-click options the server has set on other players (SET_PLAYER_OP),
// options 1-5 at indexes 0-4, "" where none is set: "Trade with" at 4 from login, "Attack" in
// the Wilderness or a duel arena.
func (s *State) PlayerOps() [5]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.scene.playerOps
}

// Weight returns the carried weight in kg (negative with weight-reducing gear); ok is false
// until UPDATE_RUNWEIGHT arrives.
func (s *State) Weight() (kg int, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.scene.weight, s.scene.haveWeight
}

// RunEnabled ports Game.runEnabled: the option_run varp is 1.
func (s *State) RunEnabled() bool {
	return s.Varp(VarpRun) == 1
}

// AutoRetaliateOn ports Game.autoRetaliateOn: the option_nodef varp is 0. Like the TS client,
// an untransmitted varp reads 0, so this reports on until the server says otherwise.
func (s *State) AutoRetaliateOn() bool {
	return s.Varp(VarpAutoRetaliate) == 0
}

// CombatMode ports Game.combatMode: the com_mode varp, the index of the selected combat-style
// button on the current weapon's tab.
func (s *State) CombatMode() int {
	return s.Varp(VarpCombatMode)
}

// MapBuildBase returns the world tile of the build area's local (0, 0); ok is false before the
// first REBUILD_NORMAL.
func (s *State) MapBuildBase() (x, z int, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mapBuildBaseX, s.mapBuildBaseZ, s.scene.haveCentre
}

// ToLocal converts a world tile to build-area coordinates, the form the client's own local
// arrays use; ok is false before the first REBUILD_NORMAL or when the tile is outside the
// 104x104 build area.
func (s *State) ToLocal(x, z int) (lx, lz int, ok bool) {
	baseX, baseZ, have := s.MapBuildBase()
	if !have {
		return 0, 0, false
	}
	lx, lz = x-baseX, z-baseZ
	if lx < 0 || lz < 0 || lx >= sceneSize || lz >= sceneSize {
		return 0, 0, false
	}
	return lx, lz, true
}

// ToWorld converts build-area coordinates to a world tile; ok is false before the first
// REBUILD_NORMAL.
func (s *State) ToWorld(lx, lz int) (x, z int, ok bool) {
	baseX, baseZ, have := s.MapBuildBase()
	if !have {
		return 0, 0, false
	}
	return baseX + lx, baseZ + lz, true
}
