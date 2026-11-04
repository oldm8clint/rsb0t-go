package mulerunner

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/scripts/orderplan"
)

// content/pack/obj.pack ids.
const (
	kitCake     = 1891
	kitFireRune = 554
	kitShark    = 385
	kitBones    = 526
)

func kitConfig(t *testing.T) *cachecfg.Config {
	t.Helper()
	path := os.Getenv("RSB0T_CONFIG_JAG")
	if path == "" {
		t.Skip("RSB0T_CONFIG_JAG not set")
	}
	cfg, err := cachecfg.LoadFile(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	return cfg
}

func simObjs(cfg *cachecfg.Config) actionsim.Objs {
	return actionsim.Objs{
		Stackable: func(id int32) bool { o, ok := cfg.Obj(int(id)); return ok && o.Stackable },
		NoteOf: func(id int32) int32 {
			n, _ := noteFor(cfg, id)
			return n
		},
		UnnoteOf: func(id int32) int32 {
			o, ok := cfg.Obj(int(id))
			if !ok || !o.Noted() {
				return 0
			}
			return int32(o.CertLink)
		},
	}
}

func simDeps(sim *actionsim.Session, cfg *cachecfg.Config) *Deps {
	return &Deps{Session: sim, Config: cfg, Components: actionsim.Components(), Log: func(string) {},
		Delay: func(time.Duration) bool { time.Sleep(time.Millisecond); return true }}
}

func held(rows []actionsim.Stack, id int32) int {
	n := 0
	for _, r := range rows {
		if r.ID == id {
			n += r.Count
		}
	}
	return n
}

// A name two objs share has a note for each, and a noted kit line must carry the note of the obj the
// bank holds: content 289 names the unstrung Yew shortbow (66) and the strung one (857) alike, and
// the first note of the name was the unstrung bow's, so every walk-alch kit line of strung Yew
// shortbows came out "could not draw it out of the bank" (live 6 Oct 2026).
func TestDrawKitDrawsTheStrungBowsNotesNotTheUnstrungOnes(t *testing.T) {
	cfg := kitConfig(t)
	var strung, unstrung int32
	for _, id := range cfg.ObjIDs("Yew shortbow") {
		o, ok := cfg.Obj(id)
		if !ok || o.Noted() {
			continue
		}
		if note, ok := noteFor(cfg, int32(id)); !ok {
			t.Fatalf("Yew shortbow %d has no note", id)
		} else if n, _ := cfg.Obj(int(note)); n.CertLink != id {
			t.Fatalf("note %d of %d links %d", note, id, n.CertLink)
		}
		if id == 857 {
			strung = int32(id)
		} else {
			unstrung = int32(id)
		}
	}
	if strung == 0 || unstrung == 0 {
		t.Fatalf("want the strung (857) and the unstrung Yew shortbow, got %v", cfg.ObjIDs("Yew shortbow"))
	}
	sim := actionsim.New(simObjs(cfg), nil)
	sim.OpenBank([]actionsim.Stack{{ID: strung, Count: 130712}, {ID: kitFireRune, Count: 5000}})
	d := simDeps(sim, cfg)
	bank := &lockedBank{Bank: actions.NewBank(sim, d.Components), unlock: func() {}}
	lines := []orderplan.KitLineDecision{
		{Item: "Fire rune", Asked: 760, Qty: 760},
		{Item: "Yew shortbow", Asked: 200, Qty: 200, Noted: true},
	}
	got, offers, stopped := drawKit(d, bank, "spare104", lines)
	if stopped {
		t.Fatal("stopped")
	}
	note, _ := noteFor(cfg, strung)
	if pack := sim.Pack(); held(pack, note) != 200 || held(pack, kitFireRune) != 760 {
		t.Fatalf("pack %+v, want 200 noted strung Yew shortbows (%d) and 760 fire runes", pack, note)
	}
	if got[1].Qty != 200 || offers[1].id != note {
		t.Fatalf("lines %+v offers %+v", got, offers)
	}
}

// drawKit empties the pack, then draws every served line -- the noted ones as notes in one pass,
// the bank left back on items -- so the kit alone sits in the pack.
func TestDrawKitDrawsTheWholeKitIntoAnEmptiedPack(t *testing.T) {
	cfg := kitConfig(t)
	sim := actionsim.New(simObjs(cfg), []actionsim.Stack{{ID: kitBones, Count: 1}, {ID: kitBones, Count: 1}})
	sim.OpenBank([]actionsim.Stack{{ID: kitCake, Count: 20}, {ID: kitFireRune, Count: 500}, {ID: kitShark, Count: 10}})
	d := simDeps(sim, cfg)
	bank := &lockedBank{Bank: actions.NewBank(sim, d.Components), unlock: func() {}}
	lines := []orderplan.KitLineDecision{
		{Item: "Fire rune", Asked: 100, Qty: 100},
		{Item: "Cake", Asked: 10, Qty: 10, Noted: true},
		{Item: "Shark", Asked: 3, Qty: 3},
		{Item: "Rope", Asked: 1, Qty: 0, Reason: "none in the bank"},
	}
	got, offers, stopped := drawKit(d, bank, "hofdon", lines)
	if stopped {
		t.Fatal("stopped")
	}
	pack := sim.Pack()
	noteCake, _ := noteFor(cfg, kitCake)
	if held(pack, kitBones) != 0 {
		t.Fatalf("the pack's own items were not banked first: %+v", pack)
	}
	if held(pack, kitFireRune) != 100 || held(pack, noteCake) != 10 || held(pack, kitShark) != 3 || held(pack, kitCake) != 0 {
		t.Fatalf("pack %+v, want 100 fire runes, 10 noted cake, 3 sharks", pack)
	}
	if len(pack) != 5 {
		t.Fatalf("the kit takes %d slots, want 5 (runes, a note stack, three sharks)", len(pack))
	}
	if sim.State().Varp(actions.BankCertVarp) != 0 {
		t.Fatal("the bank was left in note mode")
	}
	if offers[1].id != noteCake || offers[1].qty != 10 || offers[3] != (kitOffer{}) {
		t.Fatalf("offers %+v", offers)
	}
	if got[3].Qty != 0 || got[0].Qty != 100 {
		t.Fatalf("lines %+v", got)
	}
}

// A line the bank holds too few of is cut to what was drawn, with a reason, not left claiming more.
func TestDrawKitCutsALineToWhatWasDrawn(t *testing.T) {
	cfg := kitConfig(t)
	sim := actionsim.New(simObjs(cfg), nil)
	sim.OpenBank([]actionsim.Stack{{ID: kitShark, Count: 2}})
	d := simDeps(sim, cfg)
	bank := &lockedBank{Bank: actions.NewBank(sim, d.Components), unlock: func() {}}
	got, _, _ := drawKit(d, bank, "hofdon", []orderplan.KitLineDecision{{Item: "Shark", Asked: 5, Qty: 5}})
	if got[0].Qty != 2 || got[0].Reason == "" {
		t.Fatalf("line %+v, want cut to 2 with a reason", got[0])
	}
}

// deliverKit offers every line, one at a time, and proves the kit left the pack.
func TestDeliverKitHandsEveryLineOverInOneTrade(t *testing.T) {
	cfg := kitConfig(t)
	noteCake, _ := noteFor(cfg, kitCake)
	sim := actionsim.New(simObjs(cfg), []actionsim.Stack{{ID: kitFireRune, Count: 100}, {ID: noteCake, Count: 10}, {ID: kitShark, Count: 1}, {ID: kitShark, Count: 1}})
	trade := &actionsim.Trade{Partner: "Hofdon", AcceptOffer: true, AcceptConfirm: true}
	requests := 0
	sim.OnSend = func(b []byte) {
		if len(b) >= 3 && int(b[len(b)-3]) == clientprot.OPPLAYER4 {
			requests++
			if requests == 1 {
				sim.OpenTrade(trade)
			}
		}
	}
	d := simDeps(sim, cfg)
	res := deliverKit(d, "hofdon", 7, []kitOffer{{kitFireRune, 100}, {noteCake, 10}, {kitShark, 2}})
	if !res.Delivered {
		t.Fatalf("result %+v", res)
	}
	if !sim.TradeDone(trade) || len(sim.Pack()) != 0 {
		t.Fatalf("the trade did not complete with the whole kit: done=%v pack=%+v", sim.TradeDone(trade), sim.Pack())
	}
}

// A partner that never accepts the confirm screen gets no kit: the close is not a delivery.
func TestDeliverKitIsNotDeliveredWithoutTheConfirm(t *testing.T) {
	cfg := kitConfig(t)
	sim := actionsim.New(simObjs(cfg), []actionsim.Stack{{ID: kitFireRune, Count: 100}})
	trade := &actionsim.Trade{Partner: "Hofdon", AcceptOffer: true, AcceptConfirm: false}
	opened := false
	sim.OnSend = func(b []byte) {
		if !opened && len(b) >= 3 && int(b[len(b)-3]) == clientprot.OPPLAYER4 {
			opened = true
			sim.OpenTrade(trade)
		}
	}
	d := simDeps(sim, cfg)
	d.Delay = func(time.Duration) bool { time.Sleep(time.Millisecond); return true }
	done := make(chan KitHandoutResult, 1)
	go func() { done <- deliverKit(d, "hofdon", 7, []kitOffer{{kitFireRune, 100}}) }()
	select {
	case res := <-done:
		if res.Delivered {
			t.Fatalf("a kit whose confirm screen was never accepted by the partner was reported delivered: %+v", res)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("deliverKit did not return")
	}
}

// The kit's four lines (three whole stacks and an exact count of coins, so five user events) go on the offer
// screen in ONE game tick, then the trade completes (twopertick-1012: they used to go one line at a time, each
// waited on before the next).
func TestDeliverKitPlacesEveryLineInOneTick(t *testing.T) {
	cfg := kitConfig(t)
	noteCake, _ := noteFor(cfg, kitCake)
	sim := actionsim.New(simObjs(cfg), []actionsim.Stack{{ID: kitFireRune, Count: 100}, {ID: noteCake, Count: 10}, {ID: kitShark, Count: 1}, {ID: kitShark, Count: 1}, {ID: 995, Count: 1000}})
	trade := &actionsim.Trade{Partner: "Hofdon", AcceptOffer: true, AcceptConfirm: true}
	requests := 0
	var mu sync.Mutex
	sim.OnSend = func(b []byte) {
		if len(b) >= 3 && int(b[len(b)-3]) == clientprot.OPPLAYER4 {
			mu.Lock()
			requests++
			first := requests == 1
			mu.Unlock()
			if first {
				sim.OpenTrade(trade)
			}
		}
	}
	tickedSim(t, sim, 15*time.Millisecond)
	d := realDeps(sim)
	d.Config = cfg
	var logs []string
	var logMu sync.Mutex
	d.Log = func(s string) { logMu.Lock(); logs = append(logs, s); logMu.Unlock() }

	res := deliverKit(d, "hofdon", 7, []kitOffer{{kitFireRune, 100}, {noteCake, 10}, {kitShark, 2}, {995, 400}})
	if !res.Delivered {
		t.Fatalf("result %+v", res)
	}
	most, ticks := 0, 0
	for _, n := range sim.PerTick() {
		if n > 0 {
			ticks++
		}
		if n > most {
			most = n
		}
	}
	// The offer burst is the only tick with five events; the two accepts are single events in later ticks.
	if most != 5 {
		t.Fatalf("per tick %v, want one tick of 5 events (all four lines)", sim.PerTick())
	}
	if !sim.TradeDone(trade) || held(sim.Pack(), 995) != 600 {
		t.Fatalf("trade done %v, pack %+v", sim.TradeDone(trade), sim.Pack())
	}
	logMu.Lock()
	defer logMu.Unlock()
	found := false
	for _, l := range logs {
		if strings.Contains(l, "trade: offered 4 of 4 line(s) in 1 tick(s)") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no offer log line in %q", logs)
	}
}

// opensTradesOnRequest opens the next of trades each time the mule requests a trade.
func opensTradesOnRequest(sim *actionsim.Session, trades ...*actionsim.Trade) {
	var mu sync.Mutex
	next := 0
	sim.OnSend = func(b []byte) {
		if len(b) >= 3 && int(b[len(b)-3]) == clientprot.OPPLAYER4 {
			mu.Lock()
			i := next
			next++
			mu.Unlock()
			if i < len(trades) {
				sim.OpenTrade(trades[i])
			}
		}
	}
}

// A kit trade that takes the donor's agreed hand-over in as well: the donor's coins cross to the mule in the same
// trade that hands the kit out, and the result says what came in.
func TestDeliverKitTakesTheAgreedHandOverInTheSameTrade(t *testing.T) {
	cfg := kitConfig(t)
	sim := actionsim.New(simObjs(cfg), []actionsim.Stack{{ID: kitFireRune, Count: 100}})
	trade := &actionsim.Trade{Partner: "Hofdon", Theirs: []actionsim.Stack{{ID: 995, Count: 5000}}, AcceptOffer: true, AcceptConfirm: true}
	opensTradesOnRequest(sim, trade)
	d := simDeps(sim, cfg)
	give := []orderplan.KitLine{{Item: "Coins", Qty: 5000}}
	res := deliverKitWith(d, "hofdon", 7, []kitOffer{{kitFireRune, 100}}, give, 1)
	if !res.Delivered {
		t.Fatalf("result %+v", res)
	}
	if len(res.Took) != 1 || res.Took[0].Item != "Coins" || res.Took[0].Qty != 5000 {
		t.Fatalf("took %+v, want 5000 coins", res.Took)
	}
	if held(sim.Pack(), 995) != 5000 || held(sim.Pack(), kitFireRune) != 0 {
		t.Fatalf("pack %+v, want the coins in and the runes out", sim.Pack())
	}
}

// Something on the donor's side that was not agreed ends the trade with the kit still in the mule's pack.
func TestDeliverKitDeclinesAHandOverThatIsNotTheAgreedOne(t *testing.T) {
	cfg := kitConfig(t)
	for name, c := range map[string]struct {
		theirs []actionsim.Stack
		give   []orderplan.KitLine
	}{
		"something else":          {[]actionsim.Stack{{ID: kitBones, Count: 5}}, []orderplan.KitLine{{Item: "Coins", Qty: 5000}}},
		"more than agreed":        {[]actionsim.Stack{{ID: 995, Count: 5001}}, []orderplan.KitLine{{Item: "Coins", Qty: 5000}}},
		"nothing agreed, offered": {[]actionsim.Stack{{ID: 995, Count: 1}}, nil},
	} {
		sim := actionsim.New(simObjs(cfg), []actionsim.Stack{{ID: kitFireRune, Count: 100}})
		trade := &actionsim.Trade{Partner: "Hofdon", Theirs: c.theirs, AcceptOffer: true, AcceptConfirm: true}
		opensTradesOnRequest(sim, trade)
		d := simDeps(sim, cfg)
		res := deliverKitWith(d, "hofdon", 7, []kitOffer{{kitFireRune, 100}}, c.give, 1)
		if res.Delivered || sim.TradeDone(trade) {
			t.Fatalf("%s: delivered %v, trade done %v", name, res.Delivered, sim.TradeDone(trade))
		}
		if held(sim.Pack(), kitFireRune) != 100 {
			t.Fatalf("%s: the kit left the pack: %+v", name, sim.Pack())
		}
	}
}

// The donor's hand-over may be the very item the kit carries: the kit-left check allows for what came back.
func TestDeliverKitHandOverOfTheSameItemAsTheKit(t *testing.T) {
	cfg := kitConfig(t)
	sim := actionsim.New(simObjs(cfg), []actionsim.Stack{{ID: 995, Count: 400}})
	trade := &actionsim.Trade{Partner: "Hofdon", Theirs: []actionsim.Stack{{ID: 995, Count: 600}}, AcceptOffer: true, AcceptConfirm: true}
	opensTradesOnRequest(sim, trade)
	d := simDeps(sim, cfg)
	res := deliverKitWith(d, "hofdon", 7, []kitOffer{{995, 400}}, []orderplan.KitLine{{Item: "Coins", Qty: 600}}, 1)
	if !res.Delivered || held(sim.Pack(), 995) != 600 {
		t.Fatalf("result %+v pack %+v", res, sim.Pack())
	}
}

// Two kits drawn together share a stack of notes in the pack; each trade offers only its own count of it, and
// nothing else of the other kit goes on the screen.
func TestTwoKitsShareOneStackAndTradeOutOneAfterTheOther(t *testing.T) {
	cfg := kitConfig(t)
	noteCake, _ := noteFor(cfg, kitCake)
	sim := actionsim.New(simObjs(cfg), []actionsim.Stack{{ID: noteCake, Count: 15}, {ID: kitFireRune, Count: 300}})
	t1 := &actionsim.Trade{Partner: "Hofone", AcceptOffer: true, AcceptConfirm: true}
	t2 := &actionsim.Trade{Partner: "Hoftwo", AcceptOffer: true, AcceptConfirm: true}
	opensTradesOnRequest(sim, t1, t2)
	d := simDeps(sim, cfg)
	r1 := deliverKit(d, "hofone", 7, []kitOffer{{noteCake, 10}})
	if !r1.Delivered || held(sim.Pack(), noteCake) != 5 || held(sim.Pack(), kitFireRune) != 300 {
		t.Fatalf("first kit: %+v pack %+v", r1, sim.Pack())
	}
	r2 := deliverKit(d, "hoftwo", 8, []kitOffer{{noteCake, 5}, {kitFireRune, 300}})
	if !r2.Delivered || len(sim.Pack()) != 0 {
		t.Fatalf("second kit: %+v pack %+v", r2, sim.Pack())
	}
}
