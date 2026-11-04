package actions

import (
	"strings"
	"time"

	"rsb0t-go/internal/gamestate"
)

// Interface component ids for the trade screens. All hardcoded literals in
// rsb0t-main (src/bot/api/trade/Trade.ts and src/bot/adapter/ClientAdapter.ts) and in
// production use against the live server there — not dynamically resolved the way the bank's
// components are, which is what makes trade implementable without the interface archive.
const (
	TradeOfferScreen   = 3323 // trademain: main modal while offering (pack in side 3321)
	TradeConfirmScreen = 3443 // tradeconfirm: main modal on the second screen

	TradeOfferInv   = 3322 // tradeside:inv — your pack while trading; option4 = Offer All, option5 = Offer X
	TradeMyOffer    = 3415 // trademain:inv — your side of the offer; option4 = remove all
	TradeTheirOffer = 3416 // trademain:otherinv
	TradePartner    = 3417 // trademain text: "Trading With: <name>"

	TradeAcceptOffer   = 3420 // trademain:accept
	TradeDecline       = 3422 // trademain:decline (wired to if_close; see Decline)
	TradeAcceptConfirm = 3546 // tradeconfirm:accept
	// TradeConfirmStatus is tradeconfirm:com_91, the confirm screen's one line of status. trade.rs2 sets it to "Are you sure you want to
	// make this trade?" when the screen opens (the line after the open, before the offers are transmitted), to "Waiting for other
	// player." on the side that accepted first and to "Other player has accepted." on the side that did not.
	TradeConfirmStatus = 3535

	TradeConfirmMineSmall   = 3542 // tradeconfirm:inv1 (< 14 items)
	TradeConfirmMineLarge   = 3538 // tradeconfirm:inv2 (>= 14 items)
	TradeConfirmTheirsSmall = 3532
	TradeConfirmTheirsLarge = 3539

	tradeOfferAllOption = 4
	tradeOfferXOption   = 5
	tradeRemoveAllOpt   = 4
)

// Trade is the Trade.* surface from docs/reference/api-dialogue.md, bound to a session.
type Trade struct {
	s Session
}

func NewTrade(s Session) *Trade { return &Trade{s: s} }

func (t *Trade) OnOfferScreen() bool {
	main, _, _ := t.s.State().Modals()
	return main == TradeOfferScreen
}

func (t *Trade) OnConfirmScreen() bool {
	main, _, _ := t.s.State().Modals()
	return main == TradeConfirmScreen
}

func (t *Trade) Active() bool { return t.OnOfferScreen() || t.OnConfirmScreen() }

// Partner ports Trade.partner + parseTradePartnerHeader: the offer-screen header text
// with its "Trading With:" prefix stripped, or "" while the label hasn't filled.
func (t *Trade) Partner() string {
	header := strings.TrimSpace(t.s.State().Text(TradePartner))
	if header == "" {
		return ""
	}
	if i := strings.IndexByte(header, ':'); i != -1 {
		return strings.TrimSpace(header[i+1:])
	}
	lower := strings.ToLower(header)
	if strings.HasPrefix(lower, "trading with") {
		return strings.TrimSpace(header[len("trading with"):])
	}
	return header
}

func (t *Trade) MyOffer() []gamestate.SlotItem    { return items(t.s.State(), TradeMyOffer) }
func (t *Trade) TheirOffer() []gamestate.SlotItem { return items(t.s.State(), TradeTheirOffer) }

// SidePack is the pack as shown on the trade screen's side panel (what you can offer from).
func (t *Trade) SidePack() []gamestate.SlotItem { return items(t.s.State(), TradeOfferInv) }

// OtherAccepted reports that the OTHER player has accepted the confirm screen and this side has not: the status line says so
// ([if_button,tradeconfirm:accept] sends it to the partner's screen only when the partner's own accept finds this side not yet
// pending). It is the moment this side's accept completes the trade -- both_moveinv and if_close run inside that packet, so a packet read
// after it in the same tick finds the trade already over. False off the confirm screen. The text is reset by the engine on every confirm
// screen it opens, before the offers are transmitted: a caller that has read the offers has read a text that is this screen's own.
func (t *Trade) OtherAccepted() bool {
	return t.OnConfirmScreen() && strings.Contains(strings.ToLower(t.s.State().Text(TradeConfirmStatus)), "other player has accepted")
}

// ConfirmOffers ports tradeConfirmOffers: both size variants concatenated, since the
// engine picks the LARGE pair once a side offers 14 or more items.
func (t *Trade) ConfirmOffers() (mine, theirs []gamestate.SlotItem) {
	st := t.s.State()
	mine = append(items(st, TradeConfirmMineSmall), items(st, TradeConfirmMineLarge)...)
	theirs = append(items(st, TradeConfirmTheirsSmall), items(st, TradeConfirmTheirsLarge)...)
	return mine, theirs
}

func items(st *gamestate.State, comID int) []gamestate.SlotItem {
	inv := st.Inventory(comID)
	if inv == nil {
		return nil
	}
	return inv.NonEmpty()
}

// Request ports Trade.request's send: "Trade with" (player op 4) on the target's index.
// Resolving a name to an index needs PLAYER_INFO decoding (not yet ported); callers
// that control both accounts can pass the target's own UPDATE_PID slot directly.
func (t *Trade) Request(targetIndex int32) error {
	return OpPlayer4(t.s, targetIndex)
}

// OfferAll ports Trade.offerAll: inv option 4 on the first side-pack slot holding itemID.
func (t *Trade) OfferAll(itemID int32) error {
	if !t.OnOfferScreen() {
		return ErrNotOnScreen
	}
	for _, it := range t.SidePack() {
		if it.ID == itemID {
			return InvButton(t.s, tradeOfferAllOption, it.ID, it.Slot, TradeOfferInv)
		}
	}
	return ErrItemNotFound
}

// Offer ports Trade.offer: Offer-X (inv option 5), then answer the count dialog with n.
// waitForDialog bounds how long to wait for P_COUNTDIALOG to arrive after the click.
func (t *Trade) Offer(itemID int32, n int32, waitForDialog time.Duration) error {
	if n <= 0 || !t.OnOfferScreen() {
		return ErrNotOnScreen
	}
	var found *gamestate.SlotItem
	for _, it := range t.SidePack() {
		if it.ID == itemID {
			cp := it
			found = &cp
			break
		}
	}
	if found == nil {
		return ErrItemNotFound
	}
	if err := InvButton(t.s, tradeOfferXOption, found.ID, found.Slot, TradeOfferInv); err != nil {
		return err
	}
	deadline := time.Now().Add(waitForDialog)
	for !t.s.State().CountDialogOpen() {
		if time.Now().After(deadline) {
			return errNoCountDialog
		}
		time.Sleep(50 * time.Millisecond)
	}
	return AnswerCountDialog(t.s, n)
}

// RemoveAll ports Trade.removeAll: inv option 4 on each of your offered slots in turn.
func (t *Trade) RemoveAll() error {
	if !t.OnOfferScreen() {
		return ErrNotOnScreen
	}
	for guard := 0; guard < 28; guard++ {
		mine := t.MyOffer()
		if len(mine) == 0 {
			return nil
		}
		it := mine[0]
		if err := InvButton(t.s, tradeRemoveAllOpt, it.ID, it.Slot, TradeMyOffer); err != nil {
			return err
		}
		time.Sleep(600 * time.Millisecond) // ~1 game tick, like Execution.delayTicks(1)
	}
	return nil
}

// Accept ports Trade.accept: the confirm-screen accept if on it, else the offer-screen accept.
func (t *Trade) Accept() error {
	if t.OnConfirmScreen() {
		return IfButton(t.s, TradeAcceptConfirm)
	}
	if t.OnOfferScreen() {
		return IfButton(t.s, TradeAcceptOffer)
	}
	return ErrNotOnScreen
}

// Decline ports Trade.decline: press decline, and — because component 3422 is wired to
// if_close rather than if_button on some content, leaving the trade open — fall back to
// CLOSE_MODAL if the screen is still up after a short wait.
func (t *Trade) Decline(wait time.Duration) error {
	if !t.Active() {
		return nil
	}
	if err := IfButton(t.s, TradeDecline); err != nil {
		return err
	}
	deadline := time.Now().Add(wait)
	for t.Active() {
		if time.Now().After(deadline) {
			return CloseModal(t.s)
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil
}
