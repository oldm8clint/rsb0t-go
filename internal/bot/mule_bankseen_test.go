package bot

import (
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/peerbank"
)

func bankSeenCtx() *Context {
	ctx := NewContext(newAPISession(), "mule", nil, nil, nil, nil, mulebus.NewBus(), nil, nil, nil, func(string) {})
	ctx.Peers = peerbank.New()
	return ctx
}

func published(ctx *Context) bool {
	for _, e := range ctx.Peers.Entries(time.Now()) {
		if e.Account == "mule" {
			return true
		}
	}
	return false
}

var someBank = []gamestate.SlotItem{{Slot: 0, Item: gamestate.Item{ID: 995, Count: 5000}}}

// A fresh mule has published nothing, so its first bank open publishes; the next opens within
// bankPublishEvery do not write again; one after it does.
func TestEveryBankOpenPublishesRateLimited(t *testing.T) {
	ctx := bankSeenCtx()
	b := &muleBot{bank: true}
	now := time.Now()
	if !b.publishBankThrottled(ctx, someBank, now) || !published(ctx) {
		t.Fatal("the first bank open since start did not publish")
	}
	if b.publishBankThrottled(ctx, someBank, now.Add(2*time.Second)) {
		t.Fatal("a second open two seconds later published again")
	}
	if !b.publishBankThrottled(ctx, someBank, now.Add(bankPublishEvery+time.Second)) {
		t.Fatal("an open after the throttle window did not publish")
	}
	if b.publishBankThrottled(ctx, nil, now.Add(time.Hour)) {
		t.Fatal("an empty reading was published")
	}
}

// A mule that has never published is due for a read at once; one that published lately is not;
// one gone quiet past bankRefreshEvery is due again. A fleet with no directory never is.
func TestAQuietMuleRefreshesItsPublishedBank(t *testing.T) {
	ctx := bankSeenCtx()
	b := &muleBot{bank: true}
	now := time.Now()
	if !b.bankRefreshDue(ctx, now) {
		t.Fatal("a mule that has never published must read its bank to publish it")
	}
	b.bankPublishedAt = now
	if b.bankRefreshDue(ctx, now.Add(time.Minute)) {
		t.Fatal("a bank published a minute ago was refreshed")
	}
	if !b.bankRefreshDue(ctx, now.Add(bankRefreshEvery)) {
		t.Fatal("a bank not published for bankRefreshEvery was not refreshed")
	}
	ctx.Peers = nil
	if b.bankRefreshDue(ctx, now.Add(time.Hour)) {
		t.Fatal("with no fleet directory there is nothing to publish to")
	}
}

// A visit's closing reading always goes out, even right after its open published (review of
// claude/handoff-muleseen: the 5 s throttle dropped the freshest reading).
func TestTheClosingReadingIsAlwaysPublished(t *testing.T) {
	ctx := bankSeenCtx()
	b := &muleBot{bank: true}
	now := time.Now()
	b.publishBankThrottled(ctx, someBank, now)
	if !b.publishBankNow(ctx, someBank, now.Add(time.Second)) {
		t.Fatal("the closing reading a second after the open was dropped")
	}
	if b.publishBankNow(ctx, nil, now.Add(2*time.Second)) {
		t.Fatal("an empty closing reading was published")
	}
}

// The refresh opens the bank only when nothing is waiting on the mule; a refresh that could not
// open the booth backs off at least a minute.
func TestTheBankRefreshWaitsForAQuietMule(t *testing.T) {
	ctx := bankSeenCtx()
	now := time.Now()
	quiet := func() *muleBot {
		return &muleBot{bank: true, orders: map[string]string{}, incoming: map[string]time.Time{}}
	}
	if b := quiet(); !b.refreshAllowed(ctx, now) {
		t.Fatal("control: a quiet mule that has never published may refresh")
	}
	for name, busy := range map[string]func(*muleBot){
		"a donor is announced":    func(b *muleBot) { b.incoming["fisher1"] = now },
		"a hand-out is queued":    func(b *muleBot) { b.orders["acct1"] = "need 1 Knife" },
		"the mule is on a trip":   func(b *muleBot) { b.trip = &muleTrip{} },
		"it published just now":   func(b *muleBot) { b.bankPublishedAt = now },
		"there is no directory":   func(b *muleBot) {},
		"it does not bank at all": func(b *muleBot) { b.bank = false },
	} {
		b := quiet()
		busy(b)
		c := ctx
		if name == "there is no directory" {
			c = NewContext(newAPISession(), "mule", nil, nil, nil, nil, mulebus.NewBus(), nil, nil, nil, func(string) {})
		}
		if b.refreshAllowed(c, now) {
			t.Errorf("%s: the refresh was allowed", name)
		}
	}
	if bankOpenBackoff < time.Minute {
		t.Fatalf("a booth that will not open is retried after %s, want at least a minute", bankOpenBackoff)
	}
	// The backoff as refreshPublishedBank sets it: due again only after bankOpenBackoff.
	b := quiet()
	b.bankPublishedAt = now.Add(bankOpenBackoff - bankRefreshEvery)
	if b.bankRefreshDue(ctx, now.Add(bankOpenBackoff-time.Second)) || !b.bankRefreshDue(ctx, now.Add(bankOpenBackoff)) {
		t.Fatal("the failed-open backoff does not hold for bankOpenBackoff")
	}
}
