package actions

import (
	"testing"
	"time"

	"rsb0t-go/internal/protocol/clientprot"
)

func TestTradeOnOfferAndConfirmScreens(t *testing.T) {
	s := newFakeSession()
	tr := NewTrade(s)
	if tr.OnOfferScreen() || tr.OnConfirmScreen() || tr.Active() {
		t.Fatal("Trade screens reported active with nothing open")
	}
	openMainSide(s.state, TradeOfferScreen, TradeOfferInv)
	if !tr.OnOfferScreen() || tr.OnConfirmScreen() || !tr.Active() {
		t.Fatal("OnOfferScreen/Active should be true once TradeOfferScreen is the main modal")
	}
	openMain(s.state, TradeConfirmScreen)
	if tr.OnOfferScreen() || !tr.OnConfirmScreen() || !tr.Active() {
		t.Fatal("OnConfirmScreen/Active should be true once TradeConfirmScreen is the main modal")
	}
}

func TestTradePartnerParsesHeaderVariants(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   string
	}{
		{"colon prefix", "Trading With: Zezima", "Zezima"},
		{"lowercase no colon", "trading with Zezima", "Zezima"},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		s := newFakeSession()
		tr := NewTrade(s)
		setText(s.state, TradePartner, tc.header)
		if got := tr.Partner(); got != tc.want {
			t.Errorf("%s: Partner() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestTradeOffersAndConfirmScreensReadInventories(t *testing.T) {
	s := newFakeSession()
	tr := NewTrade(s)
	setFullInventory(s.state, TradeMyOffer, [2]int32{995, 1})
	setFullInventory(s.state, TradeTheirOffer, [2]int32{1777, 5})
	setFullInventory(s.state, TradeOfferInv, [2]int32{995, 10}, [2]int32{1779, 3})

	mine := tr.MyOffer()
	if len(mine) != 1 || mine[0].ID != 995 || mine[0].Count != 1 {
		t.Fatalf("MyOffer() = %+v", mine)
	}
	theirs := tr.TheirOffer()
	if len(theirs) != 1 || theirs[0].ID != 1777 || theirs[0].Count != 5 {
		t.Fatalf("TheirOffer() = %+v", theirs)
	}
	side := tr.SidePack()
	if len(side) != 2 {
		t.Fatalf("SidePack() = %+v, want 2 items", side)
	}

	setFullInventory(s.state, TradeConfirmMineSmall, [2]int32{995, 1})
	setFullInventory(s.state, TradeConfirmMineLarge, [2]int32{1780, 2})
	setFullInventory(s.state, TradeConfirmTheirsSmall, [2]int32{1777, 5})
	confMine, confTheirs := tr.ConfirmOffers()
	if len(confMine) != 2 {
		t.Fatalf("ConfirmOffers mine = %+v, want small+large concatenated (2 items)", confMine)
	}
	if len(confTheirs) != 1 {
		t.Fatalf("ConfirmOffers theirs = %+v, want 1 item", confTheirs)
	}
}

func TestTradeRequestSendsOpPlayer4(t *testing.T) {
	s := newFakeSession()
	tr := NewTrade(s)
	if err := tr.Request(42); err != nil {
		t.Fatalf("Request: %v", err)
	}
	want := []byte{clientprot.OPPLAYER4, 0, 42}
	if got := s.lastSent(); string(got) != string(want) {
		t.Fatalf("Request(42) = % x, want % x", got, want)
	}
}

func TestTradeOfferAllRequiresOfferScreenAndTheItem(t *testing.T) {
	s := newFakeSession()
	tr := NewTrade(s)
	if err := tr.OfferAll(995); err != ErrNotOnScreen {
		t.Fatalf("OfferAll off-screen: err = %v, want ErrNotOnScreen", err)
	}
	openMainSide(s.state, TradeOfferScreen, TradeOfferInv)
	setFullInventory(s.state, TradeOfferInv, [2]int32{995, 10})
	if err := tr.OfferAll(1779); err != ErrItemNotFound {
		t.Fatalf("OfferAll(missing item): err = %v, want ErrItemNotFound", err)
	}
	if err := tr.OfferAll(995); err != nil {
		t.Fatalf("OfferAll: %v", err)
	}
	want := []byte{clientprot.INV_BUTTON4, 0x03, 0xE3, 0, 0, 0x0C, 0xFA} // 995, slot0, TradeOfferInv(3322=0xCFA)
	if got := s.lastSent(); string(got) != string(want) {
		t.Fatalf("OfferAll(995) = % x, want % x", got, want)
	}
}

func TestTradeOfferSendsCountAndAnswersDialog(t *testing.T) {
	s := newFakeSession()
	tr := NewTrade(s)
	openMainSide(s.state, TradeOfferScreen, TradeOfferInv)
	setFullInventory(s.state, TradeOfferInv, [2]int32{995, 100})
	openCountDialog(s.state) // pre-open so Offer's wait loop returns immediately

	if err := tr.Offer(995, 7, 200*time.Millisecond); err != nil {
		t.Fatalf("Offer: %v", err)
	}
	// Two sends expected: the Offer-X inv button, then the count-dialog answer.
	if s.sendCount() != 2 {
		t.Fatalf("Offer(): %d packets sent, want 2 (inv button + count answer)", s.sendCount())
	}
	wantFirst := []byte{clientprot.INV_BUTTON5, 0x03, 0xE3, 0, 0, 0x0C, 0xFA}
	if got := s.sent[0]; string(got) != string(wantFirst) {
		t.Fatalf("Offer(): first packet = % x, want % x", got, wantFirst)
	}
	wantSecond := []byte{clientprot.RESUME_P_COUNTDIALOG, 0, 0, 0, 7}
	if got := s.sent[1]; string(got) != string(wantSecond) {
		t.Fatalf("Offer(): second packet = % x, want % x", got, wantSecond)
	}
}

func TestTradeOfferTimesOutWithoutCountDialog(t *testing.T) {
	s := newFakeSession()
	tr := NewTrade(s)
	openMainSide(s.state, TradeOfferScreen, TradeOfferInv)
	setFullInventory(s.state, TradeOfferInv, [2]int32{995, 100})
	// No P_COUNTDIALOG ever arrives; a short wait bounds the test to one 50ms poll.
	if err := tr.Offer(995, 7, 5*time.Millisecond); err != errNoCountDialog {
		t.Fatalf("Offer without a dialog: err = %v, want errNoCountDialog", err)
	}
}

func TestTradeOfferRejectsNonPositiveAmountOrOffScreen(t *testing.T) {
	s := newFakeSession()
	tr := NewTrade(s)
	if err := tr.Offer(995, 0, time.Millisecond); err != ErrNotOnScreen {
		t.Fatalf("Offer(n=0): err = %v, want ErrNotOnScreen", err)
	}
	openMainSide(s.state, TradeOfferScreen, TradeOfferInv)
	if err := tr.Offer(995, -1, time.Millisecond); err != ErrNotOnScreen {
		t.Fatalf("Offer(n=-1): err = %v, want ErrNotOnScreen", err)
	}
}

func TestTradeRemoveAllClicksUntilEmpty(t *testing.T) {
	s := newFakeSession()
	tr := NewTrade(s)
	openMainSide(s.state, TradeOfferScreen, TradeOfferInv)
	setFullInventory(s.state, TradeMyOffer, [2]int32{995, 1})
	// Simulate the server actually removing the item after the click, the way a real
	// round trip would send a fresh UPDATE_INV_PARTIAL/FULL — otherwise RemoveAll would
	// spin its full 28-iteration guard sleeping 600ms each time.
	s.afterSend = func(f *fakeSession) {
		setFullInventory(f.state, TradeMyOffer, [2]int32{0, 0})
	}
	start := time.Now()
	if err := tr.RemoveAll(); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("RemoveAll took %v, want ~1 tick (one item removed)", elapsed)
	}
	if s.sendCount() != 1 {
		t.Fatalf("RemoveAll sent %d packets, want 1 (one item to remove)", s.sendCount())
	}
	want := []byte{clientprot.INV_BUTTON4, 0x03, 0xE3, 0, 0, 0x0D, 0x57} // TradeMyOffer=3415=0xD57
	if got := s.lastSent(); string(got) != string(want) {
		t.Fatalf("RemoveAll click = % x, want % x", got, want)
	}
}

func TestTradeRemoveAllNoOpWhenNothingOffered(t *testing.T) {
	s := newFakeSession()
	tr := NewTrade(s)
	openMainSide(s.state, TradeOfferScreen, TradeOfferInv)
	if err := tr.RemoveAll(); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if s.sendCount() != 0 {
		t.Fatal("RemoveAll with nothing offered should never call Send")
	}
}

func TestTradeAcceptPicksScreenAppropriateButton(t *testing.T) {
	s := newFakeSession()
	tr := NewTrade(s)
	if err := tr.Accept(); err != ErrNotOnScreen {
		t.Fatalf("Accept off-screen: err = %v, want ErrNotOnScreen", err)
	}
	openMainSide(s.state, TradeOfferScreen, TradeOfferInv)
	if err := tr.Accept(); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	want := []byte{clientprot.IF_BUTTON, 0x0D, 0x5C} // TradeAcceptOffer=3420
	if got := s.lastSent(); string(got) != string(want) {
		t.Fatalf("Accept(offer screen) = % x, want % x", got, want)
	}

	closeModal(s.state)
	openMain(s.state, TradeConfirmScreen)
	if err := tr.Accept(); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	want = []byte{clientprot.IF_BUTTON, 0x0D, 0xDA} // TradeAcceptConfirm=3546=0xDDA
	if got := s.lastSent(); string(got) != string(want) {
		t.Fatalf("Accept(confirm screen) = % x, want % x", got, want)
	}
}

func TestTradeDeclineFallsBackToCloseModalIfScreenStays(t *testing.T) {
	s := newFakeSession()
	tr := NewTrade(s)
	openMainSide(s.state, TradeOfferScreen, TradeOfferInv)
	if err := tr.Decline(5 * time.Millisecond); err != nil {
		t.Fatalf("Decline: %v", err)
	}
	// Decline + fallback CloseModal = 2 packets, since the screen never actually closes.
	if s.sendCount() != 2 {
		t.Fatalf("Decline sent %d packets, want 2 (decline click + fallback close)", s.sendCount())
	}
	if got := s.sent[0]; len(got) != 3 || got[0] != clientprot.IF_BUTTON {
		t.Fatalf("Decline(): first packet = % x, want an IF_BUTTON click", got)
	}
	if got := s.lastSent(); len(got) != 1 || got[0] != clientprot.CLOSE_MODAL {
		t.Fatalf("Decline(): fallback packet = % x, want CLOSE_MODAL", got)
	}
}

func TestTradeDeclineNoOpWhenNotActive(t *testing.T) {
	s := newFakeSession()
	tr := NewTrade(s)
	if err := tr.Decline(time.Millisecond); err != nil {
		t.Fatalf("Decline: %v", err)
	}
	if s.sendCount() != 0 {
		t.Fatal("Decline with no trade active should never call Send")
	}
}

func TestTradeDeclineReturnsCleanlyWhenScreenClosesItself(t *testing.T) {
	s := newFakeSession()
	tr := NewTrade(s)
	openMainSide(s.state, TradeOfferScreen, TradeOfferInv)
	// Simulate the server closing the trade screen right after the decline click.
	s.afterSend = func(f *fakeSession) { closeModal(f.state) }
	if err := tr.Decline(time.Second); err != nil {
		t.Fatalf("Decline: %v", err)
	}
	if s.sendCount() != 1 {
		t.Fatalf("Decline sent %d packets, want 1 (no fallback close needed)", s.sendCount())
	}
}

// OtherAccepted is the confirm screen's status line (tradeconfirm:com_91): "Other player has accepted." is what trade.rs2 sets on the side that
// has NOT accepted, when the partner does. The side that accepted first reads "Waiting for other player." and a fresh screen "Are you sure".
// It is false off the confirm screen, whatever the line says (the line outlives the screen).
func TestTradeOtherAcceptedReadsTheConfirmScreenStatusLine(t *testing.T) {
	s := newFakeSession()
	tr := NewTrade(s)
	setText(s.state, TradeConfirmStatus, "Other player has accepted.")
	if tr.OtherAccepted() {
		t.Fatal("OtherAccepted is true with no trade screen open (the line is a leftover)")
	}
	openMain(s.state, TradeConfirmScreen)
	if !tr.OtherAccepted() {
		t.Fatal("OtherAccepted is false with the line on the confirm screen")
	}
	for _, line := range []string{"Are you sure you want to make this trade?", "Waiting for other player.", ""} {
		setText(s.state, TradeConfirmStatus, line)
		if tr.OtherAccepted() {
			t.Fatalf("OtherAccepted is true for %q", line)
		}
	}
	setText(s.state, TradeConfirmStatus, "other player has accepted.")
	if !tr.OtherAccepted() {
		t.Fatal("the line is read case-insensitively")
	}
	openMainSide(s.state, TradeOfferScreen, TradeOfferInv)
	if tr.OtherAccepted() {
		t.Fatal("OtherAccepted is true on the offer screen")
	}
}
