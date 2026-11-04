package actions

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/archive"
)

// Make menus and dialogue text: ChatDialog.isMakeMenu/makeProducts/make/makeOne/makeX/texts
// from the TS scripting API, ported from ClientAdapter's makeProducts and chatModalTexts.
//
// A make menu is the chat modal (or, failing that, the main modal) holding one object picture
// per product, set by IF_SETOBJECT, and BUTTON_OK buttons captioned "Make 1", "Make 5",
// "Make 10", "Make X" (or "Smelt ..."). Upstream pairs them by order: product i owns buttons
// 4i..4i+3. Make and MakeOne wait for the modal to change when given a Waiter; MakeX clicks
// Make-X, waits for the count dialog, answers it and waits for it to close, so a caller never
// races a still-open make interface.

var makeCaption = regexp.MustCompile(`(?i)(?:make|smelt)\s+(x|\d+)`)

// ErrNotConfirmed is returned when an action was sent but the interface did not respond
// within the wait.
var ErrNotConfirmed = errors.New("actions: the interface did not respond")

const (
	makeConfirmTimeout    = 3 * time.Second
	countDialogOpenWait   = 5 * time.Second
	countDialogCloseWait  = 3 * time.Second
	makeButtonsPerProduct = 4
)

// MakeButton is one quantity button of a make menu.
type MakeButton struct {
	// Qty is the quantity, or -1 for Make-X.
	Qty   int
	ComID int
}

// MakeProduct is one product of a make menu.
type MakeProduct struct {
	ObjID   int
	Buttons []MakeButton
}

func (d *ChatDialog) makeRoot() int {
	main, _, chat := d.s.State().Modals()
	if chat != -1 {
		return chat
	}
	return main
}

// walk visits comID and its descendants depth first, in child order.
func (d *ChatDialog) walk(comID int, visit func(*archive.Component)) {
	com, ok := d.archive[comID]
	if !ok {
		return
	}
	visit(com)
	for _, child := range com.Children {
		d.walk(child, visit)
	}
}

// MakeProducts ports makeProducts.
func (d *ChatDialog) MakeProducts() []MakeProduct {
	root := d.makeRoot()
	if root == -1 {
		return nil
	}
	st := d.s.State()
	var objs []int
	var buttons []MakeButton
	d.walk(root, func(com *archive.Component) {
		if obj, ok := st.InterfaceObject(com.ID); ok && obj > 0 {
			objs = append(objs, obj)
		}
		if com.ButtonType == archive.ButtonOK && com.ButtonText != "" {
			if m := makeCaption.FindStringSubmatch(com.ButtonText); m != nil {
				qty := -1
				if !strings.EqualFold(m[1], "x") {
					qty, _ = strconv.Atoi(m[1])
				}
				buttons = append(buttons, MakeButton{Qty: qty, ComID: com.ID})
			}
		}
	})
	products := make([]MakeProduct, 0, len(objs))
	for i, obj := range objs {
		lo, hi := i*makeButtonsPerProduct, (i+1)*makeButtonsPerProduct
		if lo > len(buttons) {
			lo = len(buttons)
		}
		if hi > len(buttons) {
			hi = len(buttons)
		}
		own := buttons[lo:hi]
		// Why: a picture with no Make button of its own is not a product, and counting it as one
		// is what made a live bot spin for four minutes. Content's level-up unlock box is a plain
		// "click here to continue" message that carries exactly one if_setobject picture and no
		// buttons at all (`[proc,objbox_page]` opens objbox1..objbox4 in chat.rs2), so it used to
		// satisfy IsMakeMenu. Every caller that gates on "a make menu is open" then refused to
		// press continue past it, and the run deadlocked against a dialogue it could have closed.
		if len(own) == 0 {
			continue
		}
		products = append(products, MakeProduct{ObjID: obj, Buttons: append([]MakeButton(nil), own...)})
	}
	return products
}

// IsMakeMenu ports isMakeMenu: a make menu with at least one clickable product is open.
//
// This is deliberately NOT the question a caller about to make something should ask. It cannot
// tell the furnace's smelting menu from the anvil's smithing one, so a runner that gates on it
// will happily drive whichever menu happens to be open. Ask Offers instead, and keep IsMakeMenu
// for the one thing it is honest about: "is the open chat modal a make menu rather than a
// dialogue I should click through".
func (d *ChatDialog) IsMakeMenu() bool {
	return len(d.MakeProducts()) > 0
}

// Offers reports whether the open make menu actually offers the product named by match.
//
// This is the check a caller about to MakeX should wait on. "A make menu is open" is true of
// every make menu in the game, and a live run proved what that costs: a bot at a furnace found
// a menu offering "Bronze mace", decided its work was already set up, and span for four minutes
// making nothing. Gating on the product turns that into one recovery. It is the same "prefer
// the check that is false until the thing has happened" rule docs/trustworthy_harnesses.md
// states for harnesses; it applies just as much to runners.
func (d *ChatDialog) Offers(types ObjTypes, match string) bool {
	_, ok := FindMakeProduct(d.MakeProducts(), types, match)
	return ok
}

// DescribeProducts names what the open make menu offers, so a failed match is diagnosable from
// the log instead of guessed at.
func (d *ChatDialog) DescribeProducts(types ObjTypes) string {
	products := d.MakeProducts()
	names := make([]string, 0, len(products))
	for _, p := range products {
		if types != nil {
			if obj, ok := types.Obj(p.ObjID); ok {
				names = append(names, obj.Name)
				continue
			}
		}
		names = append(names, fmt.Sprintf("obj %d", p.ObjID))
	}
	if len(names) == 0 {
		return "nothing"
	}
	return fmt.Sprintf("%v", names)
}

// FindMakeProduct picks the product whose config name contains match ignoring case, or the
// first product when match is "".
func FindMakeProduct(products []MakeProduct, types ObjTypes, match string) (MakeProduct, bool) {
	if match == "" {
		if len(products) == 0 {
			return MakeProduct{}, false
		}
		return products[0], true
	}
	if types == nil {
		return MakeProduct{}, false
	}
	want := strings.ToLower(match)
	for _, p := range products {
		if obj, ok := types.Obj(p.ObjID); ok && strings.Contains(strings.ToLower(obj.Name), want) {
			return p, true
		}
	}
	return MakeProduct{}, false
}

// Make ports make: the largest fixed quantity for the product matching match. With a Waiter it
// waits for the modal to change and returns ErrNotConfirmed if it does not.
func (d *ChatDialog) Make(types ObjTypes, match string, w Waiter) error {
	return d.makeWith(types, match, w, func(buttons []MakeButton) (MakeButton, bool) {
		best, found := MakeButton{}, false
		for _, b := range buttons {
			if b.Qty > 0 && (!found || b.Qty > best.Qty) {
				best, found = b, true
			}
		}
		return best, found
	})
}

// MakeOne ports makeOne: the Make-1 button, which never opens a count dialog.
func (d *ChatDialog) MakeOne(types ObjTypes, match string, w Waiter) error {
	return d.makeWith(types, match, w, func(buttons []MakeButton) (MakeButton, bool) {
		for _, b := range buttons {
			if b.Qty == 1 {
				return b, true
			}
		}
		return MakeButton{}, false
	})
}

func (d *ChatDialog) makeWith(types ObjTypes, match string, w Waiter, pick func([]MakeButton) (MakeButton, bool)) error {
	product, ok := FindMakeProduct(d.MakeProducts(), types, match)
	if !ok {
		return ErrNotOnScreen
	}
	button, ok := pick(product.Buttons)
	if !ok {
		return ErrNoSuchAction
	}
	main, _, chat := d.s.State().Modals()
	usingChat := chat != -1
	if err := IfButton(d.s, button.ComID); err != nil {
		return err
	}
	if w == nil {
		return nil
	}
	changed := w.DelayUntil(func() bool {
		m, _, c := d.s.State().Modals()
		if usingChat {
			return c != chat
		}
		return m != main
	}, makeConfirmTimeout)
	if !changed {
		return ErrNotConfirmed
	}
	return nil
}

// MakeX ports makeX: Make-X for the product matching match, count entered in the dialog, then
// a wait for the dialog to close. A nil Waiter sleeps.
func (d *ChatDialog) MakeX(types ObjTypes, match string, count int32, w Waiter) error {
	w = waiterOr(w)
	product, ok := FindMakeProduct(d.MakeProducts(), types, match)
	if !ok {
		return ErrNotOnScreen
	}
	var x *MakeButton
	for i := range product.Buttons {
		if product.Buttons[i].Qty == -1 {
			x = &product.Buttons[i]
			break
		}
	}
	if x == nil {
		return ErrNoSuchAction
	}
	if err := IfButton(d.s, x.ComID); err != nil {
		return err
	}
	// The count dialog can lag a tick behind the click.
	if !w.DelayUntil(d.s.State().CountDialogOpen, countDialogOpenWait) {
		return errNoCountDialog
	}
	if err := AnswerCountDialog(d.s, count); err != nil {
		return err
	}
	w.DelayUntil(func() bool { return !d.s.State().CountDialogOpen() }, countDialogCloseWait)
	return nil
}

// Texts ports texts/chatModalTexts: every non-empty text component under the chat modal, in
// tree order, with IF_SETTEXT's live text where the server has set one.
func (d *ChatDialog) Texts() []string {
	chat := d.chatModal()
	if chat == -1 {
		return nil
	}
	st := d.s.State()
	var out []string
	d.walk(chat, func(com *archive.Component) {
		if com.Type != archive.TypeText {
			return
		}
		// A line the server cleared to "" shows nothing (chatModalTexts drops it); only a
		// line it never set shows the archive's text.
		text, set := st.TextKnown(com.ID)
		if !set {
			text = com.Text
		}
		if text != "" {
			out = append(out, text)
		}
	})
	return out
}
