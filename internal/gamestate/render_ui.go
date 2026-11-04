package gamestate

// RenderUI is the versioned, server-observed interface state frozen under the
// same lock as the scene. It contains no credentials or outgoing game commands.
// Unknown skill generations/energy/weight remain distinguishable from zero.
// Headless clients have no user-selected sidebar tab or camera; a capture viewer
// may choose a diagnostic tab without sending any inputs to the game.
type RenderUI struct {
	PlayerOps        [5]string         `json:"playerOps"`
	PlayerOpPriority [5]bool           `json:"playerOpPriority"`
	Version          int               `json:"version"`
	Tick             int               `json:"tick"`
	Name             string            `json:"name"`
	Main             int               `json:"main"`
	Side             int               `json:"side"`
	ChatModal        int               `json:"chatModal"`
	Inventories      map[int]Inventory `json:"inventories"`
	Texts            map[int]string    `json:"texts"`
	Colours          map[int]Colour    `json:"colours"`
	Varps            map[int]int       `json:"varps"`
	SideIcons        map[int]int       `json:"sideIcons"`
	Objects          map[int]int       `json:"objects"`
	Skills           Skills            `json:"skills"`
	Chat             []RenderChatLine  `json:"chat"`
	RunEnergy        *int              `json:"runEnergy,omitempty"`
	Weight           *int              `json:"weight,omitempty"`
	// CountDialog is true while the server's "Enter amount" count dialog is open
	// (P_COUNTDIALOG), so a viewer can draw the prompt and answer it.
	CountDialog bool `json:"countDialog,omitempty"`
	// Overlay is IF_OPENOVERLAY's interface (drawn over the world viewport, alongside
	// Main rather than instead of it), and the five maps below are the look-only
	// interface state a renderer needs to draw a component the way the game does:
	// Heads is IF_SETNPCHEAD/IF_SETPLAYERHEAD, Anims IF_SETANIM, Hidden IF_SETHIDE,
	// ScrollPos IF_SETSCROLLPOS and Positions IF_SETPOSITION. See ifvisual.go. All are
	// omitted when the session has never received that packet, so a build with nothing
	// to say adds nothing to the wire.
	Overlay   int                    `json:"overlay"`
	Heads     map[int]InterfaceHead  `json:"heads,omitempty"`
	Anims     map[int]int            `json:"anims,omitempty"`
	Hidden    map[int]bool           `json:"hidden,omitempty"`
	ScrollPos map[int]int            `json:"scrollPos,omitempty"`
	Positions map[int]InterfacePoint `json:"positions,omitempty"`
}

type RenderChatLine struct {
	Type     int    `json:"type"`
	Username string `json:"username"`
	Text     string `json:"text"`
	Crown    int    `json:"crown"`
}

const renderChatLimit = 100

// rememberRenderChat is called with State.mu held, only for actual received
// chat events. A bounded history permits on-demand rendering of the chatbox
// without an extra game connection or an always-running rendering client.
func (s *State) rememberRenderChat(events []Event) {
	for _, e := range events {
		if e.Kind != EventChatMessage {
			continue
		}
		line := RenderChatLine{Type: e.ChatType, Username: e.Username, Text: e.Text, Crown: e.Crown}
		if len(s.chat.renderLines) == renderChatLimit {
			copy(s.chat.renderLines, s.chat.renderLines[1:])
			s.chat.renderLines[renderChatLimit-1] = line
		} else {
			s.chat.renderLines = append(s.chat.renderLines, line)
		}
	}
}

func renderMapCopy[V any](source map[int]V) map[int]V {
	copy := make(map[int]V, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}

func (s *State) renderUILocked() RenderUI {
	out := RenderUI{Version: 1, Tick: s.scene.tick, Name: s.self.name, Main: s.MainModalID, Side: s.SideModalID, ChatModal: s.ChatModalID,
		Inventories: make(map[int]Inventory, len(s.inventories)), Texts: renderMapCopy(s.texts), Colours: renderMapCopy(s.colours),
		Varps: renderMapCopy(s.varps), SideIcons: renderMapCopy(s.sideIcons), Objects: renderMapCopy(s.ifObjects), Skills: s.skills,
		Chat: append([]RenderChatLine(nil), s.chat.renderLines...), CountDialog: s.DialogInputOpen,
		Overlay: s.OverlayID, PlayerOps: s.scene.playerOps, PlayerOpPriority: s.scene.playerOpPriority}
	if len(s.ifHeads) > 0 {
		out.Heads = renderMapCopy(s.ifHeads)
	}
	if len(s.ifAnims) > 0 {
		out.Anims = renderMapCopy(s.ifAnims)
	}
	if len(s.ifHidden) > 0 {
		out.Hidden = renderMapCopy(s.ifHidden)
	}
	if len(s.ifScroll) > 0 {
		out.ScrollPos = renderMapCopy(s.ifScroll)
	}
	if len(s.ifPositions) > 0 {
		out.Positions = renderMapCopy(s.ifPositions)
	}
	for id, inv := range s.inventories {
		out.Inventories[id] = Inventory{Generation: inv.Generation, FullGeneration: inv.FullGeneration, Slots: append([]Item(nil), inv.Slots...)}
	}
	if s.scene.haveEnergy {
		energy := s.scene.energy
		out.RunEnergy = &energy
	}
	if s.scene.haveWeight {
		weight := s.scene.weight
		out.Weight = &weight
	}
	return out
}
