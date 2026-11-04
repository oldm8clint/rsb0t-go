package donorrunner

import (
	"os"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/orderplan"
)

// Content ids (content/pack/obj.pack): 1891 cake, 995 coins, 1351 bronze axe (only a filler).
const (
	cakeID   = 1891
	coinsID  = 995
	fillerID = 1351
)

func filled(n int, id int32) []gamestate.SlotItem {
	out := make([]gamestate.SlotItem, n)
	for i := range out {
		out[i] = gamestate.SlotItem{Slot: i, Item: gamestate.Item{ID: id, Count: 1}}
	}
	return out
}

// The lab arms, through the donor's own pack reading: a starter-kit pack with 9 free slots can
// take 9 cakes, not 10; a full pack can take none; coins already held need no slot at all.
func TestFitFromPackUsesThisPacksFreeSlots(t *testing.T) {
	cake, coins := []int32{cakeID}, []int32{coinsID}
	if got := fitFromPack(filled(19, fillerID), 10, false, cake, nil); got != 9 {
		t.Errorf("10 cake into 9 free slots: fit %d, want 9", got)
	}
	if got := fitFromPack(filled(23, fillerID), 6, false, cake, nil); got != 5 {
		t.Errorf("6 cake into 5 free slots: fit %d, want 5", got)
	}
	if got := fitFromPack(filled(28, fillerID), 10, false, cake, nil); got != 0 {
		t.Errorf("10 cake into a full pack: fit %d, want 0", got)
	}
	full := append(filled(27, fillerID), gamestate.SlotItem{Slot: 27, Item: gamestate.Item{ID: coinsID, Count: 100}})
	if got := fitFromPack(full, 60, true, coins, nil); got != 60 {
		t.Errorf("60 coins into a full pack already holding coins: fit %d, want 60", got)
	}
}

// A full pack is about this account, not the mule's bank, so neither side may remember it as the
// mule's answer about the item.
func TestNoRoomIsNeverRememberedAsTheMulesStock(t *testing.T) {
	for _, reason := range []string{orderplan.DeclineNoRoom, noRoomReason} {
		if k := donorplan.ClassifyRefusal(reason); k.AboutTheItem() {
			t.Errorf("ClassifyRefusal(%q) = %v, want a refusal that is not about the item", reason, k)
		}
	}
}

func TestTheMulesRepeatWindowOutlastsTheDonorsRepublish(t *testing.T) {
	if orderplan.RepeatWindow <= AskRepeatEvery {
		t.Fatalf("orderplan.RepeatWindow %s must be longer than AskRepeatEvery %s, or a waiting donor's re-publish lands after it and re-queues the order it was just served", orderplan.RepeatWindow, AskRepeatEvery)
	}
}

func TestTheReceiverSideRefusalIsRead(t *testing.T) {
	chat := chatlog.New()
	from := chat.Total()
	d := &Deps{Chat: chat}
	if iLackRoom(d, from) {
		t.Fatal("nothing said yet")
	}
	chat.Record("Other player declined trade.")
	if iLackRoom(d, from) {
		t.Fatal("an ordinary decline is not a space refusal")
	}
	chat.Record("You don't have enough inventory space for this trade")
	if !iLackRoom(d, from) {
		t.Fatal("content's receiver-side refusal must be read")
	}
}

// packSession is a confirmSession whose backpack (PlayerPack, 3214) holds n filler items.
func packSession(n int) *confirmSession {
	s := &confirmSession{state: gamestate.New()}
	s.feed(serverprot.UPDATE_INV_FULL, func(p *packet.Packet) {
		p.P2(uint16(PlayerPack))
		p.P2(uint16(n))
		for i := 0; i < n; i++ {
			p.P2(fillerID + 1) // the wire carries id+1
			p.P1(1)
		}
	})
	return s
}

// End to end through Await, with the real object config (skipped without RSB0T_CONFIG_JAG): a
// donor with 9 free slots publishes "need 9 Cake" for a 10-cake ask, and a donor with a full pack
// publishes nothing at all. CONTROL: before the fix Await published the caller's 10 in both cases,
// and the mule then drew them, opened a trade and had it refused by content.
func TestAwaitAsksOnlyForWhatThisPackCanReceive(t *testing.T) {
	path := os.Getenv("RSB0T_CONFIG_JAG")
	if path == "" {
		t.Skip("RSB0T_CONFIG_JAG not set")
	}
	cfg, err := cachecfg.LoadFile(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	ask := func(held int) (AwaitResult, []mulebus.Message) {
		bus := mulebus.NewBus()
		watch := mulebus.NewWatcher()
		d := &Deps{Session: packSession(held), Config: cfg, Chat: chatlog.New(), Log: func(string) {},
			Delay: func(time.Duration) bool { return true }}
		res := Await(d, AwaitOptions{Mule: "mule", Me: "acct64", Item: "Cake", Qty: 10, Bus: bus,
			Timeout: 30 * time.Millisecond, Memory: donorplan.NewRefusalMemory()})
		return res, watch.Poll(bus)
	}
	orders := func(msgs []mulebus.Message) []string {
		var out []string
		for _, m := range msgs {
			if m.Kind == mulebus.KindOrder {
				out = append(out, m.Text)
			}
		}
		return out
	}

	_, msgs := ask(19)
	got := orders(msgs)
	if len(got) == 0 || !strings.EqualFold(got[0], orderplan.Format("Cake", 9)) {
		t.Errorf("19 slots held: published %q, want %q", got, orderplan.Format("Cake", 9))
	}

	res, msgs := ask(28)
	if got := orders(msgs); len(got) != 0 {
		t.Errorf("full pack: published %q, want nothing", got)
	}
	if res.Reason != noRoomReason || res.Declined || res.Got {
		t.Errorf("full pack: result %+v, want Reason %q and nothing else", res, noRoomReason)
	}
}
