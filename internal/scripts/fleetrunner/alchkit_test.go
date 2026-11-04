package fleetrunner

import (
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/scripts/fleetplan"
)

// certObjs is the server's note table of the real item config: a note is the obj whose CertLink is the item.
func certObjs(cfg *cachecfg.Config) actionsim.Objs {
	return actionsim.Objs{
		Stackable: func(id int32) bool { o, ok := cfg.Obj(int(id)); return ok && o.Stackable },
		NoteOf: func(id int32) int32 {
			o, ok := cfg.Obj(int(id))
			if !ok || o.Noted() {
				return 0
			}
			for _, n := range cfg.ObjIDs(o.Name) {
				if no, ok := cfg.Obj(n); ok && no.Noted() && no.CertLink == int(id) {
					return int32(n)
				}
			}
			return 0
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

type testWaiter struct{ d *Deps }

func (w testWaiter) Delay(t time.Duration) bool { return w.d.Delay(t) }

func (w testWaiter) DelayUntil(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for !cond() {
		if !time.Now().Before(deadline) || !w.d.Delay(time.Millisecond) {
			return false
		}
	}
	return true
}

func (w testWaiter) DelayTicks(n int) bool { return w.d.Delay(time.Duration(n) * time.Millisecond) }

func fastDeps(sim *actionsim.Session, cfg *cachecfg.Config) *Deps {
	return &Deps{Session: sim, Config: cfg, Components: actionsim.Components(), Log: func(string) {},
		Delay: func(time.Duration) bool { time.Sleep(time.Millisecond); return true }}
}

// walk alch's stock is asked as notes and kept as notes; nothing else is.
func TestKitKeepsNotedForTheAlchStockOnly(t *testing.T) {
	on := &Travel{Alch: fleetplan.WalkAlchSettings{Enabled: true}.Normalised()}
	for item, want := range map[string]bool{
		"Yew shortbow id857": true, "yew shortbow ID857": true, "Yew shortbow": true, "Adamant platelegs": true, "Ruby ring id1641": true,
		"Magic staff": true, "Rune javelin": true,
		"Cake": false, "Nature rune": false, "Staff of air": false, "Willow longbow": false, "Willow longbow id847": false, "Coins": false,
	} {
		if got := on.KitKeepsNoted(item); got != want {
			t.Errorf("KitKeepsNoted(%q) = %v, want %v", item, got, want)
		}
	}
	var nilTravel *Travel
	off := &Travel{Alch: fleetplan.WalkAlchSettings{Enabled: false}}
	if nilTravel.KitKeepsNoted("Adamant platelegs") || off.KitKeepsNoted("Adamant platelegs") {
		t.Error("with walk alch off (or no Travel) no line is kept noted")
	}
	custom := &Travel{Alch: fleetplan.WalkAlchSettings{Enabled: true, Items: []string{"Cabbage"}}.Normalised()}
	if !custom.KitKeepsNoted("Cabbage") || custom.KitKeepsNoted("Adamant platelegs") {
		t.Error("the allow-list of the settings decides")
	}
}

// THE OWN-BANK KIT (DrawAlchKit): down the allow-list best-paying first, for each name the obj a cast
// pays most for as notes (the strung bow, never the unstrung one that shares its name), until the kit's
// casts are covered; nature runes and fire runes first. A bank holding 150 strung and 9,000 unstrung Yew
// shortbows, 44 Adamant platelegs and 3 Magic staffs fills a kit of 200 casts with the platelegs, the
// strung bows and the staffs.
func TestDrawAlchKitDrawsDownTheLadderTheDearestObjFirst(t *testing.T) {
	cfg := travelTestConfig(t)
	obj := func(name string) int32 { return objOf(t, cfg, name, false) }
	note := func(name string) int32 { return objOf(t, cfg, name, true) }
	nature, fire := obj("Nature rune"), obj("Fire rune")
	unstrung, strung := int32(68), int32(857)
	if o, _ := cfg.Obj(int(note("Yew shortbow"))); o.CertLink != int(strung) {
		t.Fatalf("fixture: the note objOf finds for Yew shortbow is %+v, want the strung bow's", o)
	}
	sim := actionsim.New(certObjs(cfg), nil)
	sim.OpenBank([]actionsim.Stack{
		{ID: nature, Count: 500}, {ID: fire, Count: 2000}, {ID: unstrung, Count: 9000}, {ID: strung, Count: 150},
		{ID: obj("Adamant platelegs"), Count: 44}, {ID: obj("Magic staff"), Count: 3}, {ID: obj("Cabbage"), Count: 700},
	})
	setStat(sim, fleetplan.MagicStat, 200000, 60)
	d := fastDeps(sim, cfg)
	tr := &Travel{Alch: fleetplan.WalkAlchSettings{Enabled: true, Kit: 200, Carry: 200}.Normalised()}
	tr.DrawAlchKit(d, actions.NewBank(sim, d.Components), testWaiter{d})

	p := sim.Pack()
	count := func(id int32) int {
		n := 0
		for _, s := range p {
			if s.ID == id {
				n += s.Count
			}
		}
		return n
	}
	if count(nature) != 200 || count(fire) != 1000 {
		t.Errorf("runes: %d natures %d fire, want 200 and 1000 (%+v)", count(nature), count(fire), p)
	}
	for name, want := range map[string]int{"Adamant platelegs": 44, "Yew shortbow": 150, "Magic staff": 3} {
		if got := count(note(name)); got != want {
			t.Errorf("noted %s: %d, want %d (pack %+v)", name, got, want, p)
		}
	}
	// 44 + 150 + 3 casts' worth is short of 200 only by what the bank does not hold: the unstrung bows
	// (9,000 of them) and the cabbages are never drawn.
	if count(unstrung) != 0 || count(int32(69)) != 0 || count(obj("Cabbage")) != 0 {
		t.Errorf("the pack holds unstrung bows or cabbages: %+v", p)
	}
	if b := sim.Bank(); held(b, unstrung) != 9000 || held(b, strung) != 0 {
		t.Errorf("bank %+v: the strung bows are drawn, the unstrung stay", b)
	}
	// The bank is left on items.
	if sim.State().Varp(actions.BankCertVarp) != 0 {
		t.Error("the bank was left in note mode")
	}
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

// THE KIT ASK (RequestKitBundle): a line that is walk alch's stock is asked as notes (the published
// order carries Noted), and the notes that arrive are KEPT as notes and counted as what the ask got;
// any other line keeps the old rules (food that crossed as notes is reported as notes still held).
func TestRequestKitBundleAsksTheAlchStockNotedAndCountsItsNotes(t *testing.T) {
	cfg := travelTestConfig(t)
	obj := func(name string) int32 { return objOf(t, cfg, name, false) }
	note := func(name string) int32 { return objOf(t, cfg, name, true) }
	sim := actionsim.New(certObjs(cfg), nil)
	sim.ShowPlayers("mule")
	chat := chatlog.New()
	bus := mulebus.NewBus()
	const yew = "Yew shortbow id857"
	var mu sync.Mutex
	var ask []mulebus.KitLine
	stop := make(chan struct{})
	defer close(stop)
	mw := mulebus.NewWatcher()
	opened := false
	sim.OnSend = func(b []byte) {
		mu.Lock()
		defer mu.Unlock()
		if opened || len(b) < 3 || int(b[len(b)-3]) != clientprot.OPPLAYER4 {
			return
		}
		opened = true
		tr := &actionsim.Trade{Partner: "mule", AcceptOffer: true, AcceptConfirm: true}
		go func() {
			sim.OpenTrade(tr)
			var placed []actionsim.Stack
			for _, st := range []actionsim.Stack{{ID: note("Yew shortbow"), Count: 150}, {ID: note("Cake"), Count: 30}} {
				time.Sleep(10 * time.Millisecond)
				placed = append(placed, st)
				sim.SetTheirs(placed)
			}
		}()
	}
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Millisecond):
			}
			for _, msg := range mw.Poll(bus) {
				if msg.Kind != mulebus.KindKitOrder {
					continue
				}
				mu.Lock()
				ask = append([]mulebus.KitLine(nil), msg.Kit...)
				mu.Unlock()
				plan := []mulebus.KitLine{{Item: yew, Qty: 150, Noted: true}, {Item: "Cake", Qty: 30, Noted: true}}
				bus.Publish(mulebus.KitAnswerMessage("mule", msg.From, msg.KitID, plan, "", 0))
				chat.Record("Mule:tradereq:")
			}
		}
	}()
	d := fastDeps(sim, cfg)
	d.Chat = chat
	d.Travel = &Travel{Alch: fleetplan.WalkAlchSettings{Enabled: true}.Normalised()}
	res, info := RequestKitBundle(d, bus, "acct1", "mule",
		[]fleetplan.ItemCount{{Item: yew, Count: 150}, {Item: "Cake", Count: 30}},
		KitBundleOptions{Timeout: 20 * time.Second, FallbackTimeout: 10 * time.Millisecond})
	if !info.Bundled {
		t.Fatalf("the kit was not bundled: %+v", info)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ask) != 2 || ask[0].Item != yew || !ask[0].Noted || ask[1].Item != "Cake" || ask[1].Noted {
		t.Fatalf("the published order: %+v, want the alch line noted and the food line not", ask)
	}
	if res.Got[yew] != 150 {
		t.Errorf("Got %v: the 150 noted strung bows that arrived are what the ask got", res.Got)
	}
	if res.Got["Cake"] != 0 || info.NotedHeld["Cake"] != 30 {
		t.Errorf("food that crossed as notes is not eatable: Got %v, notes held %v", res.Got, info.NotedHeld)
	}
	if _, ok := info.NotedHeld[yew]; ok {
		t.Errorf("the alch notes are reported as notes still to be un-noted: %v", info.NotedHeld)
	}
	if held(sim.Pack(), note("Yew shortbow")) != 150 || held(sim.Pack(), obj("Cake")) != 0 {
		t.Errorf("pack %+v", sim.Pack())
	}
}

// The same bank under the default carry (120 casts of a 200-cast kit) draws a trip's worth, the dearest first; with a
// budget of nothing (a pen with no safespot, where a death is likely) it draws no runes and no alchables at all, and
// at a safespot pen's 60 it stops there.
func TestDrawAlchKitForDrawsOnlyTheVisitsBudget(t *testing.T) {
	cfg := travelTestConfig(t)
	obj := func(name string) int32 { return objOf(t, cfg, name, false) }
	note := func(name string) int32 { return objOf(t, cfg, name, true) }
	nature, fire := obj("Nature rune"), obj("Fire rune")
	strung := int32(857)
	for _, c := range []struct {
		name                  string
		budget                int
		natures, fire, plates int
		bows                  int
	}{
		{"the walk carry", 120, 120, 600, 44, 76},
		{"a safespot pen", 60, 60, 300, 44, 16},
		{"no budget", 0, 0, 0, 0, 0},
	} {
		sim := actionsim.New(certObjs(cfg), nil)
		sim.OpenBank([]actionsim.Stack{
			{ID: nature, Count: 500}, {ID: fire, Count: 2000}, {ID: strung, Count: 150}, {ID: obj("Adamant platelegs"), Count: 44},
		})
		setStat(sim, fleetplan.MagicStat, 200000, 60)
		d := fastDeps(sim, cfg)
		tr := &Travel{Alch: fleetplan.WalkAlchSettings{Enabled: true, Kit: 200}.Normalised()}
		tr.DrawAlchKitFor(d, actions.NewBank(sim, d.Components), testWaiter{d}, c.budget)
		count := func(id int32) int {
			n := 0
			for _, s := range sim.Pack() {
				if s.ID == id {
					n += s.Count
				}
			}
			return n
		}
		if count(nature) != c.natures || count(fire) != c.fire || count(note("Adamant platelegs")) != c.plates || count(note("Yew shortbow")) != c.bows {
			t.Errorf("%s (budget %d): %d natures %d fire %d platelegs %d bows, want %d %d %d %d (%+v)", c.name, c.budget,
				count(nature), count(fire), count(note("Adamant platelegs")), count(note("Yew shortbow")), c.natures, c.fire, c.plates, c.bows, sim.Pack())
		}
	}
}
