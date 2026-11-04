package bot

import (
	"testing"
	"time"

	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/mulerunner"
	"rsb0t-go/internal/scripts/orderplan"
)

func kitMule() *muleBot {
	b := orderMule()
	b.watcher = mulerunner.NewRequestWatcher()
	b.incoming = map[string]time.Time{}
	return b
}

func kitMsg(id string) mulebus.Message {
	return mulebus.KitOrderMessage("acct64", "mule", id, []mulebus.KitLine{{Item: "Cake", Qty: 10}, {Item: "Fire rune", Qty: 100}}, 20, 5*time.Minute)
}

func answers(w *mulebus.Watcher, bus *mulebus.Bus) []mulebus.Message {
	var out []mulebus.Message
	for _, m := range w.Poll(bus) {
		if m.Kind == mulebus.KindKitAnswer {
			out = append(out, m)
		}
	}
	return out
}

// A kit is queued and acknowledged; a re-publish of it refreshes the entry and is not answered
// again; a re-publish of a kit just served is dropped.
func TestAKitIsQueuedOnceAndAcknowledged(t *testing.T) {
	b := kitMule()
	ctx := orderCtx()
	w := mulebus.NewWatcher()
	b.takeKit(ctx, kitMsg("k1"))
	got := answers(w, ctx.Bus)
	if len(b.kits) != 1 || len(got) != 1 || !mulebus.IsKitQueued(got[0]) || got[0].KitID != "k1" {
		t.Fatalf("kits %v answers %+v", b.kits, got)
	}
	b.takeKit(ctx, kitMsg("k1"))
	if n := len(answers(w, ctx.Bus)); n != 0 || len(b.kits) != 1 {
		t.Fatalf("a re-publish was answered %d time(s), kits %d", n, len(b.kits))
	}
	if b.ordersPending() != 1 {
		t.Fatal("a queued kit must count as pending work, or a cook round starts over it")
	}

	delete(b.kits, "acct64")
	b.servedKits = map[string]servedKit{"acct64": {id: "k1", at: time.Now()}}
	b.takeKit(ctx, kitMsg("k1"))
	if len(b.kits) != 0 {
		t.Fatal("the re-publish of a kit just served was queued again")
	}
	b.takeKit(ctx, kitMsg("k2"))
	if len(b.kits) != 1 {
		t.Fatal("a NEW kit from the same donor must be queued")
	}
}

// The donor-level half of the screen answers at once, with the words a single order would get.
func TestAKitOverTheDonorsCapIsRefusedAtOnce(t *testing.T) {
	b := kitMule()
	b.handoutLedger.Record(time.Now(), "acct64", "Knife", 1)
	ctx := orderCtx()
	w := mulebus.NewWatcher()
	b.takeKit(ctx, kitMsg("k1"))
	got := answers(w, ctx.Bus)
	if len(b.kits) != 0 || len(got) != 1 || got[0].Reason != "already handed you one recently" {
		t.Fatalf("kits %v answers %+v", b.kits, got)
	}
	b = kitMule()
	b.handout = false
	b.takeKit(ctx, kitMsg("k2"))
	if got := answers(w, ctx.Bus); len(got) != 1 || got[0].Reason != "hand-outs are switched off" {
		t.Fatalf("hand-outs off: %+v", got)
	}
}

// The fishing hand-off comes first: a kit waits while a donor is announced, and is served anyway
// once it has waited kitMaxDefer.
func TestAKitWaitsForTheFishingQueueButNotForever(t *testing.T) {
	b := kitMule()
	b.kits = map[string]queuedKit{"acct64": {msg: kitMsg("k1"), at: time.Now(), until: time.Now().Add(5 * time.Minute)}}
	if _, _, due := b.kitDue(); !due {
		t.Fatal("with nobody queued the kit is due")
	}
	b.incoming["fisher1"] = time.Now()
	if _, _, due := b.kitDue(); due {
		t.Fatal("a kit must wait while a fishing donor is announced")
	}
	b.kits["acct64"] = queuedKit{msg: kitMsg("k1"), at: time.Now().Add(-kitMaxDefer - time.Second), until: time.Now().Add(5 * time.Minute)}
	if _, _, due := b.kitDue(); !due {
		t.Fatal("a kit that has waited kitMaxDefer is served whatever the queue")
	}
}

// kitCtx is a context whose Session is an empty game state: a kit hand-out through it fails at
// once ("you are not in sight of me"), which is all serveKit's bookkeeping needs.
func kitCtx() *Context {
	return NewContext(newAPISession(), "mule", nil, nil, nil, nil, mulebus.NewBus(), nil, nil, nil, func(string) {})
}

func kitDeps(ctx *Context) *mulerunner.Deps {
	return &mulerunner.Deps{Session: ctx.Session, Delay: func(time.Duration) bool { return true }, Log: func(string) {}}
}

// Review of r4, blocking item 1: overdue kits were served back to back while fishers waited. After
// one kit served over waiting fishers, the next is not due until the fishing queue has had a turn.
// CONTROL: with no fisher waiting, the backlog is served straight through, as it should be.
func TestOverdueKitsAlternateWithTheFishingQueue(t *testing.T) {
	b := kitMule()
	ctx := kitCtx()
	d := kitDeps(ctx)
	b.incoming["fisher1"] = time.Now()
	old := time.Now().Add(-kitMaxDefer - time.Second)
	b.kits = map[string]queuedKit{}
	for _, n := range []string{"s1", "s2", "s3"} {
		b.kits[n] = queuedKit{msg: kitMsg("k-" + n), at: old, until: time.Now().Add(5 * time.Minute)}
	}
	if !b.serveKit(ctx, d) {
		t.Fatal("the first overdue kit must be served")
	}
	if !b.kitTurnOwed {
		t.Fatal("a kit served over a waiting fisher must owe the fishing queue a turn")
	}
	if b.serveKit(ctx, d) {
		t.Fatal("a second overdue kit was served before the fishing queue had its turn")
	}
	b.kitTurnOwed = false // what Loop does once RunCycle has run
	if !b.serveKit(ctx, d) {
		t.Fatal("after the fishing queue's turn the next overdue kit is due")
	}

	quiet := kitMule()
	quiet.kits = map[string]queuedKit{}
	for _, n := range []string{"s1", "s2"} {
		quiet.kits[n] = queuedKit{msg: kitMsg("k-" + n), at: old, until: time.Now().Add(5 * time.Minute)}
	}
	if !quiet.serveKit(ctx, d) || !quiet.serveKit(ctx, d) {
		t.Fatal("control: with nobody waiting, kits are served back to back (the turn owed is ignored while the queue is quiet)")
	}
}

// Queued kits count in the published queue depth.
func TestQueuedKitsCountInTheQueueDepth(t *testing.T) {
	b := kitMule()
	b.incoming["fisher1"] = time.Now()
	b.kits = map[string]queuedKit{"s1": {msg: kitMsg("k1"), at: time.Now(), until: time.Now().Add(5 * time.Minute)}}
	if got := b.queueDepth(); got != 2 {
		t.Fatalf("queue depth %d, want 2 (one fisher, one kit)", got)
	}
}

// Review of r4, blocking item 2: a kit nobody waits for any more is dropped, with an answer, and a
// donor's kit cancel drops it; a hand-off's own cancel (no KitID) does not.
func TestDeadKitsAreDroppedNotServed(t *testing.T) {
	b := kitMule()
	ctx := kitCtx()
	w := mulebus.NewWatcher()
	b.kits = map[string]queuedKit{
		"late":  {msg: mulebus.KitOrderMessage("late", "mule", "l1", []mulebus.KitLine{{Item: "Cake", Qty: 1}}, 20, time.Second), at: time.Now().Add(-time.Minute), until: time.Now().Add(-time.Second)},
		"alive": {msg: kitMsg("a1"), at: time.Now(), until: time.Now().Add(5 * time.Minute)},
	}
	b.dropDeadKits(ctx)
	if _, ok := b.kits["late"]; ok {
		t.Fatal("a kit past its donor's wait was kept")
	}
	if _, ok := b.kits["alive"]; !ok {
		t.Fatal("a live kit was dropped")
	}
	got := answers(w, ctx.Bus)
	if len(got) != 1 || got[0].To != "late" || got[0].Reason == "" {
		t.Fatalf("the dropped kit's donor must be answered: %+v", got)
	}

	b.cancelKit(ctx, "alive", "other-id")
	if _, ok := b.kits["alive"]; !ok {
		t.Fatal("a cancel for a different kit id dropped the queued kit")
	}
	b.cancelKit(ctx, "alive", "a1")
	if _, ok := b.kits["alive"]; ok {
		t.Fatal("the donor's kit cancel did not drop its kit")
	}

	// takeKit records the donor's own wait.
	b.takeKit(ctx, mulebus.KitOrderMessage("acct64", "mule", "w1", []mulebus.KitLine{{Item: "Cake", Qty: 1}}, 20, 30*time.Second))
	if q := b.kits["acct64"]; q.until.Sub(q.at) != 30*time.Second {
		t.Fatalf("the kit's wait is %s, want the donor's 30s", q.until.Sub(q.at))
	}
}

// A donor on another world cannot trade with this mule and is told so at once.
func TestAKitFromAnotherWorldIsRefused(t *testing.T) {
	b := kitMule()
	ctx := orderCtx()
	ctx.CurrentWorld = 1
	w := mulebus.NewWatcher()
	b.takeKit(ctx, mulebus.OnWorld(kitMsg("k1"), 2))
	got := answers(w, ctx.Bus)
	if len(b.kits) != 0 || len(got) != 1 || got[0].Reason == "" {
		t.Fatalf("kits %v answers %+v", b.kits, got)
	}
	b.takeKit(ctx, mulebus.OnWorld(kitMsg("k2"), 1))
	if len(b.kits) != 1 {
		t.Fatal("control: a kit from this mule's own world must be queued")
	}
}

// serveKit answers a kit that failed, records it as served so its re-publishes are dropped, and
// tells the donor even when the failure came after a plan.
func TestServeKitAnswersAFailureAndRemembersTheKit(t *testing.T) {
	b := kitMule()
	ctx := kitCtx()
	w := mulebus.NewWatcher()
	b.kits = map[string]queuedKit{"acct64": {msg: kitMsg("k1"), at: time.Now(), until: time.Now().Add(5 * time.Minute)}}
	if !b.serveKit(ctx, kitDeps(ctx)) {
		t.Fatal("a due kit was not served")
	}
	got := answers(w, ctx.Bus)
	if len(got) != 1 || got[0].KitID != "k1" || got[0].Reason != orderplan.DeclineNotInSight {
		t.Fatalf("answers %+v, want one refusal naming %q", got, orderplan.DeclineNotInSight)
	}
	if last, ok := b.servedKits["acct64"]; !ok || last.id != "k1" {
		t.Fatal("the served kit was not remembered")
	}
	b.takeKit(ctx, kitMsg("k1"))
	if len(b.kits) != 0 {
		t.Fatal("a re-publish of the kit just served was queued again")
	}
}

// Review of the kit order (revK, S1): a fisher that announces WHILE a kit is being served must get
// the next turn. kitTurnOwed was decided before the kit started, from a quiet queue, so the next
// overdue kit ran straight after. Every served kit now owes the fishing queue a turn; a queue that
// is still quiet ignores the debt.
func TestAFisherAnnouncingMidKitGetsTheNextTurn(t *testing.T) {
	b := kitMule()
	ctx := kitCtx()
	d := kitDeps(ctx)
	old := time.Now().Add(-kitMaxDefer - time.Second)
	b.kits = map[string]queuedKit{}
	for _, n := range []string{"s1", "s2"} {
		b.kits[n] = queuedKit{msg: kitMsg("k-" + n), at: old, until: time.Now().Add(5 * time.Minute)}
	}
	if !b.serveKit(ctx, d) { // served over a quiet queue
		t.Fatal("the first kit was not served")
	}
	b.incoming["fisher1"] = time.Now() // announced during that kit
	if b.serveKit(ctx, d) {
		t.Fatal("a second kit ran straight after the first although a fisher announced during it")
	}
	b.kitTurnOwed = false // RunCycle's turn
	if !b.serveKit(ctx, d) {
		t.Fatal("after the fishing turn the next kit is due")
	}
}

// revK S2: a kit whose donor will give up before the hand-out can finish is not started. It is
// dropped, with an answer, instead of costing a bank visit and a trade nobody answers.
func TestAKitWithTooLittleTimeLeftIsSkippedWithAnAnswer(t *testing.T) {
	b := kitMule()
	ctx := kitCtx()
	w := mulebus.NewWatcher()
	b.kits = map[string]queuedKit{
		"short": {msg: mulebus.KitOrderMessage("short", "mule", "s1", []mulebus.KitLine{{Item: "Cake", Qty: 1}}, 20, time.Minute), at: time.Now(), until: time.Now().Add(kitMinLeft - 10*time.Second)},
	}
	if b.serveKit(ctx, kitDeps(ctx)) {
		t.Fatal("a kit with too little time left was served")
	}
	if len(b.kits) != 0 {
		t.Fatal("the kit with too little time left was kept")
	}
	got := answers(w, ctx.Bus)
	if len(got) != 1 || got[0].KitID != "s1" || got[0].Reason == "" {
		t.Fatalf("answers %+v, want one refusal for s1", got)
	}
}

// revK surviving mutant: serveOrder is where dead kits are dropped; without the call a dead kit
// would sit in the queue (counting as pending work) forever.
func TestServeOrderDropsDeadKits(t *testing.T) {
	b := kitMule()
	ctx := kitCtx()
	w := mulebus.NewWatcher()
	// A fisher is announced and the kit is not overdue, so no kit is due: only the dead-kit sweep
	// can clear it.
	b.incoming["fisher1"] = time.Now()
	b.kits = map[string]queuedKit{
		"late": {msg: kitMsg("l1"), at: time.Now().Add(-time.Second), until: time.Now().Add(-time.Millisecond)},
	}
	if b.serveOrder(ctx, kitDeps(ctx)) {
		t.Fatal("serveOrder spent a Loop call on a dead kit")
	}
	if len(b.kits) != 0 || b.ordersPending() != 0 {
		t.Fatal("serveOrder left a dead kit queued")
	}
	if got := answers(w, ctx.Bus); len(got) != 1 || got[0].Reason == "" {
		t.Fatalf("the dead kit's donor was not answered: %+v", got)
	}
}

// Round 7: per-item hand-outs come under the same owed-turn rule as kits. Live 2026-09-24 the mule
// made 23 hand-outs and 0 fisher trades in five minutes: every Loop served an order before
// RunCycle. With a fisher waiting, one hand-out of either kind is followed by a fishing turn.
// CONTROL: with no fisher waiting, orders are still served back to back.
func TestItemOrdersAlternateWithTheFishingQueue(t *testing.T) {
	b := kitMule()
	b.handoutPolicy.Allowed = []string{"acct1", "acct2", "acct3"}
	b.handoutPolicy.MaxOrdersPerDonor = 5
	ctx := kitCtx()
	d := kitDeps(ctx)
	b.incoming["fisher1"] = time.Now()
	for _, n := range []string{"acct1", "acct2", "acct3"} {
		b.takeOrder(ctx, mulebus.OrderMessage(n, "need 1 Knife", "mule"))
	}
	if !b.serveOrder(ctx, d) {
		t.Fatal("the first order was not served")
	}
	if b.serveOrder(ctx, d) {
		t.Fatal("a second hand-out ran straight after the first while a fisher waited")
	}
	b.kitTurnOwed = false // RunCycle's turn
	if !b.serveOrder(ctx, d) {
		t.Fatal("after the fishing turn the next order is due")
	}

	quiet := kitMule()
	quiet.handoutPolicy.Allowed = []string{"acct1", "acct2"}
	quiet.handoutPolicy.MaxOrdersPerDonor = 5
	for _, n := range []string{"acct1", "acct2"} {
		quiet.takeOrder(ctx, mulebus.OrderMessage(n, "need 1 Knife", "mule"))
	}
	if !quiet.serveOrder(ctx, d) || !quiet.serveOrder(ctx, d) {
		t.Fatal("control: with nobody waiting, orders are served back to back")
	}
}

// Round 7: per-item orders are served oldest first, not in Go's random map order.
func TestItemOrdersAreServedOldestFirst(t *testing.T) {
	b := kitMule()
	var names []string
	for i := 0; i < 8; i++ {
		names = append(names, "swarm"+string(rune('a'+i)))
	}
	b.handoutPolicy.Allowed = names
	b.handoutPolicy.MaxOrdersPerDonor = 5
	ctx := kitCtx()
	for _, n := range names {
		b.takeOrder(ctx, mulebus.OrderMessage(n, "need 1 Knife", "mule"))
		time.Sleep(2 * time.Millisecond)
	}
	d := kitDeps(ctx)
	for i, want := range names {
		if !b.serveOrder(ctx, d) {
			t.Fatalf("order %d not served", i)
		}
		if last, ok := b.served[want]; !ok || last.text == "" {
			t.Fatalf("order %d served out of order: wanted %s first, served %v", i, want, b.served)
		}
	}
}

// Round 7: a kit that has waited longer than every queued item order goes first once it is due.
func TestAnOverdueKitOlderThanTheItemOrdersGoesFirst(t *testing.T) {
	b := kitMule()
	b.handoutPolicy.Allowed = []string{"acct1", "acct64"}
	b.handoutPolicy.MaxOrdersPerDonor = 5
	ctx := kitCtx()
	d := kitDeps(ctx)
	b.kits = map[string]queuedKit{"acct64": {msg: kitMsg("k1"), at: time.Now().Add(-kitMaxDefer - time.Second), until: time.Now().Add(5 * time.Minute)}}
	b.takeOrder(ctx, mulebus.OrderMessage("acct1", "need 1 Knife", "mule"))
	if !b.serveOrder(ctx, d) {
		t.Fatal("nothing served")
	}
	if _, ok := b.servedKits["acct64"]; !ok {
		t.Fatal("the older overdue kit waited behind a newer item order")
	}
}

func queueKits(b *muleBot, donors ...string) {
	b.kits = map[string]queuedKit{}
	now := time.Now()
	for i, name := range donors {
		msg := kitMsg("k-" + name)
		msg.From = name
		b.kits[name] = queuedKit{msg: msg, at: now.Add(time.Duration(i) * time.Second), until: now.Add(5 * time.Minute)}
	}
}

// With kitBatch above 1 and no fisher waiting, the oldest kit is served with the next oldest ones in ONE visit.
func TestKitsRideTogetherWhileNoFisherWaits(t *testing.T) {
	b := kitMule()
	b.kitBatch = 3
	queueKits(b, "a1", "a2", "a3", "a4")
	got := b.kitCompanions("a1")
	if len(got) != 2 || got[0].key != "a2" || got[1].key != "a3" {
		t.Fatalf("companions %+v, want the two next oldest (a2, a3)", got)
	}
	b.incoming["fisher1"] = time.Now()
	if got := b.kitCompanions("a1"); len(got) != 0 {
		t.Fatalf("a batch is several trades long: %d kit(s) rode along while a fisher waits", len(got))
	}
	b.incoming = map[string]time.Time{}
	b.kitBatch = 1
	if got := b.kitCompanions("a1"); len(got) != 0 {
		t.Fatal("kitBatch 1 serves one kit a visit")
	}
}

// A kit whose donor will stop waiting before the batch gets to it stays queued.
func TestAKitThatWouldTimeOutInTheBatchStaysQueued(t *testing.T) {
	b := kitMule()
	b.kitBatch = 3
	queueKits(b, "a1", "a2", "a3")
	q := b.kits["a2"]
	q.until = time.Now().Add(kitMinLeft + kitServeGuess/2)
	b.kits["a2"] = q
	got := b.kitCompanions("a1")
	if len(got) != 1 || got[0].key != "a3" {
		t.Fatalf("companions %+v, want only a3: a2's donor stops waiting before the first trade is done", got)
	}
}

// A served batch answers, remembers and ledgers every kit, and nothing is left queued.
func TestServeKitServesABatchAndRemembersEveryKit(t *testing.T) {
	b := kitMule()
	b.kitBatch = 3
	ctx := kitCtx()
	w := mulebus.NewWatcher()
	queueKits(b, "a1", "a2", "a3")
	if !b.serveKit(ctx, kitDeps(ctx)) {
		t.Fatal("a due kit was not served")
	}
	got := answers(w, ctx.Bus)
	if len(got) != 3 {
		t.Fatalf("answers %+v, want a refusal for each of the three donors (none is in sight)", got)
	}
	for _, name := range []string{"a1", "a2", "a3"} {
		if last, ok := b.servedKits[name]; !ok || last.id != "k-"+name {
			t.Fatalf("%s's kit was not remembered", name)
		}
	}
	if len(b.kits) != 0 || len(b.servingKit) != 0 {
		t.Fatalf("kits %v serving %v after the batch", b.kits, b.servingKit)
	}
}

// The hand-over a kit order carries is read as the donor sent it: positive lines only.
func TestKitGiveFromKeepsPositiveLines(t *testing.T) {
	msg := mulebus.WithGive(kitMsg("k1"), []mulebus.KitLine{{Item: "Coins", Qty: 5000}, {Item: "Bones", Qty: 0}, {Item: "Big bones", Qty: 30, Noted: true}}, 2)
	got := kitGiveFrom(msg)
	if len(got) != 2 || got[0].Item != "Coins" || !got[1].Noted || msg.GiveSlots != 2 {
		t.Fatalf("give %+v slots %d", got, msg.GiveSlots)
	}
	if len(kitGiveFrom(kitMsg("k2"))) != 0 {
		t.Fatal("a kit that gives nothing back has no hand-over")
	}
}
