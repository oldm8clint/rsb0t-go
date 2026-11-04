package controlpanel

import (
	"fmt"
	"strings"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
)

// Stream client input, part 2: clicks on the game's own interfaces (inventory item options,
// "Use" one item on another, interface buttons, bank/trade inventory buttons, dialogue
// continue and options, closing an interface), the count dialog and public chat. Same rules
// as streaminput.go — the -stream-input flag, the view lease, the current epoch, a recent
// observation, the rate limit, the account's bot paused, every request audited — plus, here,
// a check of the click against the interface archive and the observed interface state
// before anything is sent.
//
// The browser builds the option list with the reference client's own menu code
// (Client.ts addComponentOptions, via the capture adapter's interfaceMenu export) and sends
// back the game's MiniMenuAction code with its three parameters (object, slot, component)
// exactly as the game client's useMenuOption would read them. Nothing is trusted: the
// component must exist, belong to an interface the game is showing, be the kind of
// component that offers that option, and name an item that is actually in that slot.

// StreamInterfaceSource is an optional Source capability: the parsed interface archive. A
// source without it (the simulated fleet) refuses interface clicks.
type StreamInterfaceSource interface {
	StreamComponents() archive.Components
}

// StreamComponents implements StreamInterfaceSource from the fleet's interface archive.
func (f *FleetSource) StreamComponents() archive.Components {
	if f.manager == nil {
		return nil
	}
	return f.manager.Components()
}

// The game client's MiniMenuAction codes (src/client/shell/MiniMenuAction.ts) this path
// accepts. Anything else — spells on items (TGT_*), friends/ignore, abuse reports, player
// options — is refused.
const (
	menuOpHeld1       = 694
	menuOpHeld2       = 962
	menuOpHeld3       = 795
	menuOpHeld4       = 681
	menuOpHeld5       = 100
	menuInvButton1    = 582
	menuInvButton2    = 113
	menuInvButton3    = 555
	menuInvButton4    = 331
	menuInvButton5    = 354
	menuIfButton      = 231
	menuToggleButton  = 435
	menuSelectButton  = 225
	menuPauseButton   = 997
	menuCloseButton   = 737
	menuUseHeldOnHeld = 398
)

var (
	menuOpHeld    = map[int]int{menuOpHeld1: 1, menuOpHeld2: 2, menuOpHeld3: 3, menuOpHeld4: 4, menuOpHeld5: 5}
	menuInvButton = map[int]int{menuInvButton1: 1, menuInvButton2: 2, menuInvButton3: 3, menuInvButton4: 4, menuInvButton5: 5}
)

// streamUse is the item a "Use" was started on: its object id, slot and inventory component.
type streamUse struct {
	Obj  int `json:"obj"`
	Slot int `json:"slot"`
	Com  int `json:"com"`
}

// streamChatLimit and the character range are the game client's own for typed public chat
// (Client.ts: key >= 32 && key <= 122, at most 80 characters).
const streamChatLimit = actions.PublicChatLimit

func (s *Server) streamComponents() archive.Components {
	if c, ok := s.src.(StreamInterfaceSource); ok {
		return c.StreamComponents()
	}
	return nil
}

// interfaceShown reports whether the interface rooted at root is one the game is showing,
// so a click could reach it: an open main, side or chat modal, or (only while no side modal
// replaces them) one of the side tabs' interfaces.
func interfaceShown(ui gamestate.RenderUI, root int) bool {
	if root < 0 {
		return false
	}
	if root == ui.Main || root == ui.Side || root == ui.ChatModal {
		return true
	}
	if ui.Side != -1 {
		return false
	}
	for _, id := range ui.SideIcons {
		if id == root {
			return true
		}
	}
	return false
}

// shownComponent returns the component comID when it exists and belongs to a shown interface.
func shownComponent(coms archive.Components, ui gamestate.RenderUI, comID int) (*archive.Component, error) {
	c := coms[comID]
	if c == nil {
		return nil, fmt.Errorf("component %d does not exist", comID)
	}
	if !interfaceShown(ui, c.LayerID) && !interfaceShown(ui, c.ID) {
		return nil, fmt.Errorf("component %d is not on an interface the game is showing", comID)
	}
	return c, nil
}

// heldItemAt checks that the observed inventory of component com holds obj at slot.
func heldItemAt(ui gamestate.RenderUI, com, slot, obj int) error {
	inv, ok := ui.Inventories[com]
	if !ok {
		return fmt.Errorf("component %d has no observed inventory", com)
	}
	if slot < 0 || slot >= len(inv.Slots) {
		return fmt.Errorf("slot %d is outside component %d's inventory", slot, com)
	}
	it := inv.Slots[slot]
	if it.Count <= 0 || int(it.ID) != obj {
		return fmt.Errorf("slot %d of component %d does not hold item %d", slot, com, obj)
	}
	return nil
}

// validUse checks the item a "Use" was started on: a shown inventory that offers Use, with
// that item in that slot.
func validUse(coms archive.Components, ui gamestate.RenderUI, u *streamUse) error {
	if u == nil {
		return fmt.Errorf("no item is being used")
	}
	c, err := shownComponent(coms, ui, u.Com)
	if err != nil {
		return err
	}
	if c.Type != archive.TypeInv || !c.ObjUse {
		return fmt.Errorf("component %d does not offer Use", u.Com)
	}
	return heldItemAt(ui, u.Com, u.Slot, u.Obj)
}

// buttonLabel is a button's label as the game client shows it: IfType.ts gives a button with
// no text of its own "Ok", "Select" or "Continue" by kind (internal/archive keeps the raw text).
func buttonLabel(c *archive.Component) string {
	if c.ButtonText != "" {
		return c.ButtonText
	}
	switch c.ButtonType {
	case archive.ButtonOK:
		return "Ok"
	case archive.ButtonToggle, archive.ButtonSelect:
		return "Select"
	case archive.ButtonContinue:
		return "Continue"
	}
	return ""
}

func objName(names *cachecfg.Config, id int) string {
	if names != nil {
		if t, ok := names.Obj(id); ok && t.Name != "" {
			return t.Name
		}
	}
	return fmt.Sprintf("item %d", id)
}

// streamInterfaceAction validates one interface click ("if"), a count-dialog answer
// ("count") or a line of public chat ("chat") against the observation, and returns what to
// send and the option's label. An error is the refusal, already worded for the viewer.
func (s *Server) streamInterfaceAction(snap *gamestate.RenderSnapshot, a streamAction) (func(actions.Session) error, string, error) {
	ui := snap.UI
	switch a.Kind {
	case "chat":
		text := strings.TrimSpace(a.Text)
		if text == "" {
			return nil, "", fmt.Errorf("nothing to say")
		}
		if len(text) > streamChatLimit {
			return nil, "", fmt.Errorf("chat is at most %d characters", streamChatLimit)
		}
		for _, r := range text {
			if r < 32 || r > 122 {
				return nil, "", fmt.Errorf("chat may only use the game's own characters (space to z)")
			}
		}
		if strings.HasPrefix(text, "::") {
			return nil, "", fmt.Errorf("commands (::) are never sent from the stream view")
		}
		return func(ss actions.Session) error { return actions.SayPublic(ss, text) }, "Say: " + text, nil
	case "count":
		if !ui.CountDialog {
			return nil, "", fmt.Errorf("no amount is being asked for")
		}
		if a.Value < 0 || a.Value > 2147483647 {
			return nil, "", fmt.Errorf("amount out of range")
		}
		v := int32(a.Value)
		return func(ss actions.Session) error { return actions.AnswerCountDialog(ss, v) }, fmt.Sprintf("Enter amount: %d", v), nil
	case "if":
	default:
		return nil, "", fmt.Errorf("unknown action kind")
	}

	coms := s.streamComponents()
	if coms == nil {
		return nil, "", fmt.Errorf("this build has no interface archive, so interface clicks cannot be checked")
	}
	c, err := shownComponent(coms, ui, a.Com)
	if err != nil {
		return nil, "", err
	}
	names := s.streamNamesConfig()
	obj, slot, com := a.ID, a.Slot, a.Com
	if op, ok := menuOpHeld[a.Menu]; ok {
		if c.Type != archive.TypeInv || !c.ObjOps {
			return nil, "", fmt.Errorf("component %d offers no item options", com)
		}
		if err := heldItemAt(ui, com, slot, obj); err != nil {
			return nil, "", err
		}
		label := ""
		if names != nil {
			if t, ok := names.Obj(obj); ok {
				label = t.IOps[op-1]
			}
		}
		if label == "" && op == 5 {
			label = "Drop" // the game offers Drop as option 5 for any item that leaves it unset
		}
		if label == "" {
			return nil, "", fmt.Errorf("that item has no such option")
		}
		label += " " + objName(names, obj)
		return func(ss actions.Session) error { return actions.OpHeld(ss, op, int32(obj), slot, com) }, label, nil
	}
	if op, ok := menuInvButton[a.Menu]; ok {
		if c.Type != archive.TypeInv || c.Iop[op-1] == "" {
			return nil, "", fmt.Errorf("component %d has no such button", com)
		}
		if err := heldItemAt(ui, com, slot, obj); err != nil {
			return nil, "", err
		}
		label := c.Iop[op-1] + " " + objName(names, obj)
		return func(ss actions.Session) error { return actions.InvButton(ss, op, int32(obj), slot, com) }, label, nil
	}
	switch a.Menu {
	case menuUseHeldOnHeld:
		if c.Type != archive.TypeInv || !c.ObjOps {
			return nil, "", fmt.Errorf("component %d offers no item options", com)
		}
		if err := heldItemAt(ui, com, slot, obj); err != nil {
			return nil, "", err
		}
		u := a.Use
		if err := validUse(coms, ui, u); err != nil {
			return nil, "", err
		}
		if u.Com == com && u.Slot == slot {
			return nil, "", fmt.Errorf("an item cannot be used on itself")
		}
		label := "Use " + objName(names, u.Obj) + " with " + objName(names, obj)
		return func(ss actions.Session) error {
			return actions.OpHeldU(ss, int32(obj), slot, com, int32(u.Obj), u.Slot, u.Com)
		}, label, nil
	case menuIfButton:
		if c.ButtonType != archive.ButtonOK {
			return nil, "", fmt.Errorf("component %d is not a button", com)
		}
		if c.ClientCode != 0 {
			// Logout, friends, ignore and the like are handled by the game client itself.
			return nil, "", fmt.Errorf("%q is a game client button, not available in the stream view", buttonLabel(c))
		}
		return func(ss actions.Session) error { return actions.IfButton(ss, com) }, buttonLabel(c), nil
	case menuToggleButton, menuSelectButton:
		want := archive.ButtonToggle
		if a.Menu == menuSelectButton {
			want = archive.ButtonSelect
		}
		if c.ButtonType != want {
			return nil, "", fmt.Errorf("component %d is not that kind of button", com)
		}
		return func(ss actions.Session) error { return actions.IfButton(ss, com) }, buttonLabel(c), nil
	case menuPauseButton:
		if c.ButtonType != archive.ButtonContinue {
			return nil, "", fmt.Errorf("component %d is not a continue button", com)
		}
		return func(ss actions.Session) error { return actions.ResumePauseButton(ss, com) }, buttonLabel(c), nil
	case menuCloseButton:
		if c.ButtonType != archive.ButtonClose {
			return nil, "", fmt.Errorf("component %d is not a close button", com)
		}
		return func(ss actions.Session) error { return actions.CloseModal(ss) }, "Close", nil
	}
	return nil, "", fmt.Errorf("that option (%d) is not available in the stream view", a.Menu)
}
