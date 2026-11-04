package gamestate

// RenderSnapshot is copied only on explicit capture requests. The headless account
// retains appearance bytes already decoded from its own stream; it never renders.
type RenderSnapshot struct {
	Combat     RenderCombat   `json:"combat"`
	Effects    []RenderEffect `json:"effects"`
	X          int            `json:"x"`
	Z          int            `json:"z"`
	Level      int            `json:"level"`
	CentreX    int            `json:"centreX"`
	CentreZ    int            `json:"centreZ"`
	SelfSlot   int            `json:"selfSlot"`
	Appearance []byte         `json:"appearance"`
	Animation  int            `json:"animation"`
	// AnimationTick is the scene tick Animation arrived on (additive).
	AnimationTick int            `json:"animationTick,omitempty"`
	NPCs          []NPC          `json:"npcs"`
	Players       []RenderPlayer `json:"players"`
	Locs          []LocInstance  `json:"locs"`
	// Objs are the ground item stacks this session knows about (GroundItems), additive:
	// capture consumers that predate the field ignore it.
	Objs []GroundItem `json:"objs,omitempty"`
	UI   RenderUI     `json:"ui"`
}

type RenderPlayer struct {
	Combat RenderCombat `json:"combat"`
	Index  int          `json:"index"`
	// Name is the display name from this player's own APPEARANCE update (see players.go);
	// empty until one has been decoded. Additive: capture consumers that predate it ignore it.
	Name       string `json:"name,omitempty"`
	X          int    `json:"x"`
	Z          int    `json:"z"`
	Appearance []byte `json:"appearance"`
	// Anim/AnimTick are the last sequence the server started on this player (-1 none) and
	// the scene tick it arrived on; FaceEntity is who it faces (-1 none, a player is its slot
	// plus 32768). Additive, for rendering.
	Anim       int `json:"anim"`
	AnimTick   int `json:"animTick,omitempty"`
	FaceEntity int `json:"faceEntity"`
}

func (s *State) RenderSnapshot() (RenderSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.haveTile || s.LoggedOut || s.scene.tick == 0 || s.scene.state != SceneReady {
		return RenderSnapshot{}, false
	}
	out := RenderSnapshot{X: s.mapBuildBaseX + s.localX, Z: s.mapBuildBaseZ + s.localZ, Level: s.level, CentreX: s.scene.centreX, CentreZ: s.scene.centreZ, SelfSlot: s.SelfSlot, Appearance: append([]byte(nil), s.self.appearance...), Animation: s.self.anim, AnimationTick: s.self.animTick}
	s.watchRender()
	out.Combat = s.self.combat
	for _, e := range s.effects {
		if e.Level == s.level && (s.scene.tick-e.Tick)*30 <= e.End+30 {
			out.Effects = append(out.Effects, e)
		}
	}
	out.UI = s.renderUILocked()
	combats := make([]RenderCombat, 0, len(s.npcs))
	for index, n := range s.npcs {
		combats = append(combats, n.combat)
		out.NPCs = append(out.NPCs, NPC{Combat: &combats[len(combats)-1], Index: index, Type: n.typeID, X: s.mapBuildBaseX + n.localX, Z: s.mapBuildBaseZ + n.localZ, Level: s.level, Anim: n.anim, AnimTick: n.animTick, FaceEntity: n.faceEntity})
	}
	for index, p := range s.players {
		out.Players = append(out.Players, RenderPlayer{Combat: p.combat, Index: index, Name: p.name, X: s.mapBuildBaseX + p.localX, Z: s.mapBuildBaseZ + p.localZ, Appearance: append([]byte(nil), s.playerAppearances[index]...), Anim: p.anim, AnimTick: p.animTick, FaceEntity: p.faceEntity})
	}
	for _, loc := range s.locs {
		out.Locs = append(out.Locs, *loc)
	}
	for _, items := range s.groundItems {
		out.Objs = append(out.Objs, items...)
	}
	return out, true
}
