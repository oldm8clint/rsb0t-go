package controlpanel

import (
	"net/http"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/gamestate"
)

// interfaceTestSource adds the interface archive to the input test source.
type interfaceTestSource struct {
	*inputTestSource
	coms archive.Components
}

func (s *interfaceTestSource) StreamComponents() archive.Components { return s.coms }

// WithStreamSession gives the action a session whose own state agrees with the observation
// about the count dialog, as a real session's does; AnswerCountDialog checks it again.
func (s *interfaceTestSource) WithStreamSession(account string, fn func(actions.Session) error) error {
	s.renderSource.mu.Lock()
	open := s.renderSource.snapshot.UI.CountDialog
	s.renderSource.mu.Unlock()
	d := &discardSession{state: gamestate.New()}
	d.state.DialogInputOpen = open
	err := fn(d)
	s.amu.Lock()
	s.sends += d.sent
	s.amu.Unlock()
	return err
}

// A small interface archive shaped like the game's: the backpack tab (root 3213 with its
// inventory 3214), the logout tab's button (a client-code button), a bank (main 5292 with its
// withdraw inventory 5382 and a close button), and a dialogue (chat modal 4882 with its
// continue button 4886 and an option button 4887).
func testComponents() archive.Components {
	c := archive.Components{}
	add := func(com *archive.Component) { c[com.ID] = com }
	add(&archive.Component{ID: 3213, LayerID: 3213, Type: archive.TypeLayer, Children: []int{3214}})
	add(&archive.Component{ID: 3214, LayerID: 3213, Type: archive.TypeInv, ObjOps: true, ObjUse: true})
	add(&archive.Component{ID: 2449, LayerID: 2449, Type: archive.TypeLayer, Children: []int{2458}})
	add(&archive.Component{ID: 2458, LayerID: 2449, Type: archive.TypeText, ButtonType: archive.ButtonOK, ButtonText: "Logout", ClientCode: 205})
	add(&archive.Component{ID: 5292, LayerID: 5292, Type: archive.TypeLayer, Children: []int{5382, 5384}})
	add(&archive.Component{ID: 5382, LayerID: 5292, Type: archive.TypeInv, Iop: [5]string{"Withdraw 1", "Withdraw 5", "Withdraw 10", "Withdraw All", "Withdraw X"}})
	add(&archive.Component{ID: 5384, LayerID: 5292, Type: archive.TypeGraphic, ButtonType: archive.ButtonClose})
	add(&archive.Component{ID: 4882, LayerID: 4882, Type: archive.TypeLayer, Children: []int{4886, 4887}})
	add(&archive.Component{ID: 4886, LayerID: 4882, Type: archive.TypeText, ButtonType: archive.ButtonContinue, ButtonText: "Continue"})
	add(&archive.Component{ID: 4887, LayerID: 4882, Type: archive.TypeText, ButtonType: archive.ButtonOK, ButtonText: "Yes please"})
	// A button with no text of its own: the game shows it as "Ok" (the random-event box's answers).
	add(&archive.Component{ID: 4888, LayerID: 4882, Type: archive.TypeGraphic, ButtonType: archive.ButtonOK})
	add(&archive.Component{ID: 4889, LayerID: 4882, Type: archive.TypeGraphic})
	return c
}

func TestStreamInterfaceActionsAreCheckedAgainstTheShownInterfaces(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	base := newInputSource("ProgressiveFisher", -1)
	src := &interfaceTestSource{inputTestSource: base, coms: testComponents()}
	setUI := func(edit func(ui *gamestate.RenderUI)) {
		base.renderSource.mu.Lock()
		defer base.renderSource.mu.Unlock()
		edit(&base.renderSource.snapshot.UI)
	}
	setUI(func(ui *gamestate.RenderUI) {
		ui.SideIcons = map[int]int{3: 3213, 10: 2449}
		ui.Inventories = map[int]gamestate.Inventory{
			3214: {Slots: []gamestate.Item{{ID: 317, Count: 1}, {ID: 590, Count: 1}, {}}},
			5382: {Slots: []gamestate.Item{{ID: 995, Count: 1000}}},
		}
	})
	h := inputHarness(t, src, &at, true)
	var open streamSessionBody
	decode(t, h.postJSON("/api/stream/open", map[string]interface{}{"account": "fisher1", "transport": "scene-delta"}), &open)
	var took struct {
		Acknowledged bool `json:"acknowledged"`
	}
	decode(t, h.postJSON("/api/stream/input", map[string]interface{}{"session": open.Session, "enabled": true}), &took)
	if !took.Acknowledged {
		t.Fatal("take control not acknowledged")
	}
	var f streamFrame
	frame := func() {
		at = at.Add(time.Second)
		decode(t, h.get("/api/stream/frame?session="+open.Session), &f)
	}
	frame()
	sends := 0
	check := func(name string, action map[string]interface{}, want int) {
		t.Helper()
		at = at.Add(time.Second)
		res := h.postJSON("/api/stream/action", map[string]interface{}{"session": open.Session, "epoch": f.Epoch, "seq": f.Seq, "action": action})
		if res.StatusCode != want {
			t.Errorf("%s = %d, want %d", name, res.StatusCode, want)
		}
		if want == http.StatusOK {
			sends++
		}
		base.amu.Lock()
		got := base.sends
		base.amu.Unlock()
		if got != sends {
			t.Errorf("%s: %d packets sent in all, want %d", name, got, sends)
			sends = got
		}
	}
	use := map[string]interface{}{"obj": 590, "slot": 1, "com": 3214}

	// The backpack (a side tab's interface): Drop is option 5 for any item, even without
	// the config archive; any other option needs the item's own label.
	check("drop from the backpack", map[string]interface{}{"kind": "if", "menu": menuOpHeld5, "id": 317, "slot": 0, "com": 3214}, http.StatusOK)
	check("held option the item does not have", map[string]interface{}{"kind": "if", "menu": menuOpHeld1, "id": 317, "slot": 0, "com": 3214}, http.StatusConflict)
	check("wrong item in the slot", map[string]interface{}{"kind": "if", "menu": menuOpHeld5, "id": 999, "slot": 0, "com": 3214}, http.StatusConflict)
	check("empty slot", map[string]interface{}{"kind": "if", "menu": menuOpHeld5, "id": 0, "slot": 2, "com": 3214}, http.StatusConflict)
	check("use tinderbox on the fish", map[string]interface{}{"kind": "if", "menu": menuUseHeldOnHeld, "id": 317, "slot": 0, "com": 3214, "use": use}, http.StatusOK)
	check("use an item on itself", map[string]interface{}{"kind": "if", "menu": menuUseHeldOnHeld, "id": 590, "slot": 1, "com": 3214, "use": use}, http.StatusConflict)
	check("use on an npc", map[string]interface{}{"kind": "npcu", "index": 12, "use": use}, http.StatusOK)
	check("use with no used item", map[string]interface{}{"kind": "npcu", "index": 12}, http.StatusConflict)
	check("use on an npc not in view", map[string]interface{}{"kind": "npcu", "index": 99, "use": use}, http.StatusConflict)
	check("logout is a client button", map[string]interface{}{"kind": "if", "menu": menuIfButton, "com": 2458}, http.StatusConflict)
	check("spell on an item is not offered", map[string]interface{}{"kind": "if", "menu": 563, "id": 317, "slot": 0, "com": 3214}, http.StatusConflict)

	// The bank is not open: its buttons cannot be reached.
	check("withdraw with no bank open", map[string]interface{}{"kind": "if", "menu": menuInvButton1, "id": 995, "slot": 0, "com": 5382}, http.StatusConflict)
	setUI(func(ui *gamestate.RenderUI) { ui.Main, ui.Side = 5292, 2005 })
	frame()
	check("withdraw 1", map[string]interface{}{"kind": "if", "menu": menuInvButton1, "id": 995, "slot": 0, "com": 5382}, http.StatusOK)
	check("withdraw X", map[string]interface{}{"kind": "if", "menu": menuInvButton5, "id": 995, "slot": 0, "com": 5382}, http.StatusOK)
	check("a side tab is hidden behind the bank's side modal", map[string]interface{}{"kind": "if", "menu": menuOpHeld5, "id": 317, "slot": 0, "com": 3214}, http.StatusConflict)
	check("an option the bank inventory does not have", map[string]interface{}{"kind": "if", "menu": menuOpHeld5, "id": 995, "slot": 0, "com": 5382}, http.StatusConflict)
	check("amount with no dialog", map[string]interface{}{"kind": "count", "value": 28}, http.StatusConflict)
	setUI(func(ui *gamestate.RenderUI) { ui.CountDialog = true })
	frame()
	check("amount", map[string]interface{}{"kind": "count", "value": 28}, http.StatusOK)
	check("close the bank", map[string]interface{}{"kind": "if", "menu": menuCloseButton, "com": 5384}, http.StatusOK)
	check("close on a non-close component", map[string]interface{}{"kind": "if", "menu": menuCloseButton, "com": 5382}, http.StatusConflict)

	// A dialogue.
	setUI(func(ui *gamestate.RenderUI) { ui.Main, ui.Side, ui.ChatModal, ui.CountDialog = -1, -1, 4882, false })
	frame()
	check("click here to continue", map[string]interface{}{"kind": "if", "menu": menuPauseButton, "com": 4886}, http.StatusOK)
	check("a dialogue option", map[string]interface{}{"kind": "if", "menu": menuIfButton, "com": 4887}, http.StatusOK)
	check("a button with the game's default label", map[string]interface{}{"kind": "if", "menu": menuIfButton, "com": 4888}, http.StatusOK)
	check("a graphic that is no button", map[string]interface{}{"kind": "if", "menu": menuIfButton, "com": 4889}, http.StatusConflict)
	check("continue on an option button", map[string]interface{}{"kind": "if", "menu": menuPauseButton, "com": 4887}, http.StatusConflict)
	check("a component that does not exist", map[string]interface{}{"kind": "if", "menu": menuIfButton, "com": 60000}, http.StatusConflict)

	// Public chat.
	check("chat", map[string]interface{}{"kind": "chat", "text": "hello there"}, http.StatusOK)
	check("a command", map[string]interface{}{"kind": "chat", "text": "::tele 3222 3222"}, http.StatusConflict)
	check("a character the game cannot type", map[string]interface{}{"kind": "chat", "text": "hi~"}, http.StatusConflict)
	check("too long", map[string]interface{}{"kind": "chat", "text": string(make([]byte, 81))}, http.StatusConflict)
	check("empty", map[string]interface{}{"kind": "chat", "text": "   "}, http.StatusConflict)
}

func TestStreamInterfaceClicksNeedTheArchive(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	src := newInputSource("", -1)
	h := inputHarness(t, src, &at, true)
	var open streamSessionBody
	decode(t, h.postJSON("/api/stream/open", map[string]interface{}{"account": "fisher1"}), &open)
	h.postJSON("/api/stream/input", map[string]interface{}{"session": open.Session, "enabled": true})
	at = at.Add(time.Second)
	var f streamFrame
	decode(t, h.get("/api/stream/frame?session="+open.Session), &f)
	res := h.postJSON("/api/stream/action", map[string]interface{}{"session": open.Session, "epoch": f.Epoch, "seq": f.Seq,
		"action": map[string]interface{}{"kind": "if", "menu": menuOpHeld5, "id": 317, "slot": 0, "com": 3214}})
	if res.StatusCode != http.StatusConflict || src.sends != 0 {
		t.Errorf("interface click without an archive = %d (sends %d), want 409 and nothing sent", res.StatusCode, src.sends)
	}
}
