package controlpanel

import "rsb0t-go/internal/gamestate"

// The scene-delta transport's interface section: the session's own interface state
// (gamestate.RenderUI — open modals, every server-set interface text/colour/object,
// inventories, varps, side-tab icons, skills, recent chat, run energy/weight), which a
// provider replays through the reference client's interface code (the capture adapter's
// hydrateUI) to draw the game's own side tabs, chatbox and open bank/trade/dialogue.
//
// A whole scene carries the whole RenderUI (minus its tick, which changes every observation
// and draws nothing). A delta carries only the keys that changed, with null for a key that
// went away, so an XP drop or one fish landing costs a few bytes, not the interface state.

// streamUIChatLines is how much chat history the interface section carries: the chatbox
// shows about eight lines at the bottom of its scroll.
const streamUIChatLines = 10

// streamUIOf copies what the interface section sends from one observation.
func streamUIOf(ui gamestate.RenderUI) gamestate.RenderUI {
	out := ui
	out.Tick = 0
	if len(out.Chat) > streamUIChatLines {
		out.Chat = out.Chat[len(out.Chat)-streamUIChatLines:]
	}
	return out
}

// streamUIDelta is what changed between two interface sections. Absent fields and keys did
// not change; a null map value means the key was removed.
type streamUIDelta struct {
	PlayerOps        *[5]string                   `json:"playerOps,omitempty"`
	PlayerOpPriority *[5]bool                     `json:"playerOpPriority,omitempty"`
	Version          *int                         `json:"version,omitempty"`
	Name             *string                      `json:"name,omitempty"`
	Main             *int                         `json:"main,omitempty"`
	Side             *int                         `json:"side,omitempty"`
	ChatModal        *int                         `json:"chatModal,omitempty"`
	Inventories      map[int]*gamestate.Inventory `json:"inventories,omitempty"`
	Texts            map[int]*string              `json:"texts,omitempty"`
	Colours          map[int]*gamestate.Colour    `json:"colours,omitempty"`
	Varps            map[int]*int                 `json:"varps,omitempty"`
	SideIcons        map[int]*int                 `json:"sideIcons,omitempty"`
	Objects          map[int]*int                 `json:"objects,omitempty"`
	Skills           *gamestate.Skills            `json:"skills,omitempty"`
	Chat             *[]gamestate.RenderChatLine  `json:"chat,omitempty"`
	RunEnergy        *int                         `json:"runEnergy,omitempty"`
	Weight           *int                         `json:"weight,omitempty"`
	CountDialog      *bool                        `json:"countDialog,omitempty"`
	// The look-only interface state (gamestate/ifvisual.go): the head model in a
	// dialogue box, component animations, hidden components, an overlay interface,
	// scroll positions and moved components. A delta that left these out would draw
	// every dialogue OPENED AFTER the view was — which is nearly all of them — with an
	// empty head, since only the first whole snapshot would ever carry one.
	Overlay   *int                              `json:"overlay,omitempty"`
	Heads     map[int]*gamestate.InterfaceHead  `json:"heads,omitempty"`
	Anims     map[int]*int                      `json:"anims,omitempty"`
	Hidden    map[int]*bool                     `json:"hidden,omitempty"`
	ScrollPos map[int]*int                      `json:"scrollPos,omitempty"`
	Positions map[int]*gamestate.InterfacePoint `json:"positions,omitempty"`
}

func (d *streamUIDelta) empty() bool {
	return d.PlayerOps == nil && d.PlayerOpPriority == nil && d.Version == nil && d.Name == nil && d.Main == nil && d.Side == nil && d.ChatModal == nil && len(d.Inventories) == 0 &&
		len(d.Texts) == 0 && len(d.Colours) == 0 && len(d.Varps) == 0 && len(d.SideIcons) == 0 && len(d.Objects) == 0 &&
		d.Skills == nil && d.Chat == nil && d.RunEnergy == nil && d.Weight == nil && d.CountDialog == nil &&
		d.Overlay == nil && len(d.Heads) == 0 && len(d.Anims) == 0 && len(d.Hidden) == 0 &&
		len(d.ScrollPos) == 0 && len(d.Positions) == 0
}

func intChanged(a, b int) *int {
	if a == b {
		return nil
	}
	return &b
}

func diffMap[V comparable](prev, next map[int]V) map[int]*V {
	var out map[int]*V
	for k, v := range next {
		if old, ok := prev[k]; !ok || old != v {
			if out == nil {
				out = map[int]*V{}
			}
			v := v
			out[k] = &v
		}
	}
	for k := range prev {
		if _, ok := next[k]; !ok {
			if out == nil {
				out = map[int]*V{}
			}
			out[k] = nil
		}
	}
	return out
}

func inventoryEqual(a, b gamestate.Inventory) bool {
	if a.Generation != b.Generation || a.FullGeneration != b.FullGeneration || len(a.Slots) != len(b.Slots) {
		return false
	}
	for i := range a.Slots {
		if a.Slots[i] != b.Slots[i] {
			return false
		}
	}
	return true
}

func chatLinesEqual(a, b []gamestate.RenderChatLine) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func optionalIntChanged(a, b *int) *int {
	if b == nil || (a != nil && *a == *b) {
		return nil
	}
	v := *b
	return &v
}

// diffStreamUI returns the interface delta from prev to next. Pure.
func diffStreamUI(prev, next *gamestate.RenderUI) streamUIDelta {
	d := streamUIDelta{
		Version: intChanged(prev.Version, next.Version), Main: intChanged(prev.Main, next.Main),
		Side: intChanged(prev.Side, next.Side), ChatModal: intChanged(prev.ChatModal, next.ChatModal),
		Texts: diffMap(prev.Texts, next.Texts), Colours: diffMap(prev.Colours, next.Colours),
		Varps: diffMap(prev.Varps, next.Varps), SideIcons: diffMap(prev.SideIcons, next.SideIcons),
		Objects: diffMap(prev.Objects, next.Objects),
		Overlay: intChanged(prev.Overlay, next.Overlay),
		Heads:   diffMap(prev.Heads, next.Heads), Anims: diffMap(prev.Anims, next.Anims),
		Hidden: diffMap(prev.Hidden, next.Hidden), ScrollPos: diffMap(prev.ScrollPos, next.ScrollPos),
		Positions: diffMap(prev.Positions, next.Positions),
		RunEnergy: optionalIntChanged(prev.RunEnergy, next.RunEnergy), Weight: optionalIntChanged(prev.Weight, next.Weight),
	}
	if prev.Name != next.Name {
		name := next.Name
		d.Name = &name
	}
	if prev.PlayerOps != next.PlayerOps {
		ops := next.PlayerOps
		d.PlayerOps = &ops
	}
	if prev.PlayerOpPriority != next.PlayerOpPriority {
		priority := next.PlayerOpPriority
		d.PlayerOpPriority = &priority
	}
	for k, inv := range next.Inventories {
		if old, ok := prev.Inventories[k]; !ok || !inventoryEqual(old, inv) {
			if d.Inventories == nil {
				d.Inventories = map[int]*gamestate.Inventory{}
			}
			inv := inv
			d.Inventories[k] = &inv
		}
	}
	for k := range prev.Inventories {
		if _, ok := next.Inventories[k]; !ok {
			if d.Inventories == nil {
				d.Inventories = map[int]*gamestate.Inventory{}
			}
			d.Inventories[k] = nil
		}
	}
	if prev.CountDialog != next.CountDialog {
		open := next.CountDialog
		d.CountDialog = &open
	}
	if prev.Skills != next.Skills {
		skills := next.Skills
		d.Skills = &skills
	}
	if !chatLinesEqual(prev.Chat, next.Chat) {
		chat := next.Chat
		d.Chat = &chat
	}
	return d
}
