package mastercraftrunner

import (
	"os"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/tile"
)

// anvilEngine plays the engine's side of the anvil TICK BY TICK, as content 289 runs it
// (skill_smithing/scripts/smithing/smithing.rs2):
//
//   - a bar used on the anvil (OPLOCU) opens interface 994 with the metal's columns when the Smithing level and
//     a hammer allow it, and says why not when they do not;
//   - Make 1, 5 or 10 (INV_BUTTON1..3 on the product's column slot) closes it, p_delay(1), then the bars go and
//     the product comes, p_delay(2), and the next make follows at once: one make every FIFTH tick, the first two
//     ticks after the click. A packet read while the player is delayed is dropped without a word.
type anvilEngine struct {
	st     *gamestate.State
	chat   *chatlog.Log
	cheats anticheat.Counters

	pack    [28]struct{ id, n int32 }
	ticks   int
	frames  []tFrame
	pending [][]byte

	delayedUntil int
	makeLeft     int
	makeAt       int
	menu         *mastercraftplan.SmithProduct
	menuOut      bool
	anvilX       int
	anvilZ       int
	anvilID      int

	xpT, level int // Smithing: XP in tenths (the packet carries xpT/10), level
	members    bool

	made, dropped, refused, uses, mesboxes int
	madeTicks                              []int
}

const (
	aeX, aeZ, aeID = 3188, 3426, 2783
)

func aeStack(id int32) bool { return id >= 41 && id <= 44 }

func newAnvilEngine(t *testing.T, pack []struct{ id, n int32 }) (*anvilEngine, *fleetrunner.Deps) {
	t.Helper()
	e := &anvilEngine{st: gamestate.New(), chat: chatlog.New(), level: 99, members: true, anvilX: aeX, anvilZ: aeZ, anvilID: aeID}
	e.cheats.SetEnabled(false)
	copy(e.pack[:], pack)
	e.st.Handle(serverprot.IF_SETICON, []byte{byte(3213 >> 8), byte(3213 & 0xff), 3})
	e.push()
	comps := archive.Components{
		3213: {ID: 3213, Type: archive.TypeLayer, Children: []int{3214}},
		3214: {ID: 3214, Type: archive.TypeInv, ObjOps: true},
	}
	d := &fleetrunner.Deps{Session: e, Components: comps, Chat: e.chat, Delay: func(time.Duration) bool { e.tick(); return true }}
	return e, d
}

func (e *anvilEngine) State() *gamestate.State     { return e.st }
func (e *anvilEngine) Cheats() *anticheat.Counters { return &e.cheats }

var aeLen = map[int]int{clientprot.OPLOCU: 13, clientprot.INV_BUTTON1: 7, clientprot.INV_BUTTON2: 7, clientprot.INV_BUTTON3: 7, clientprot.CLOSE_MODAL: 1}

func (e *anvilEngine) Send(build func(w *packet.Packet)) error {
	w := packet.NewWriter(64)
	build(w)
	b := append([]byte(nil), w.Data[:w.Pos]...)
	var pkts [][]byte
	for len(b) > 0 {
		n, ok := aeLen[int(b[0])]
		if !ok || n > len(b) {
			pkts = append(pkts, b)
			break
		}
		pkts = append(pkts, b[:n])
		b = b[n:]
	}
	e.frames = append(e.frames, tFrame{e.ticks + 1, pkts})
	return nil
}

func (e *anvilEngine) count(id int32) int32 {
	var n int32
	for _, s := range e.pack {
		if s.id == id {
			n += s.n
		}
	}
	return n
}

func (e *anvilEngine) del(id, n int32) {
	for i := range e.pack {
		if n <= 0 {
			return
		}
		if e.pack[i].id != id {
			continue
		}
		take := min(n, e.pack[i].n)
		e.pack[i].n -= take
		n -= take
		if e.pack[i].n == 0 {
			e.pack[i].id = 0
		}
	}
}

func (e *anvilEngine) add(id, n int32) {
	if aeStack(id) {
		for i := range e.pack {
			if e.pack[i].id == id {
				e.pack[i].n += n
				return
			}
		}
	}
	for i := range e.pack {
		if n == 0 {
			return
		}
		if e.pack[i].id == 0 {
			e.pack[i].id, e.pack[i].n = id, 1
			if aeStack(id) {
				e.pack[i].n = n
				return
			}
			n--
		}
	}
}

func (e *anvilEngine) productOfBar(bar int32) (mastercraftplan.SmithProduct, bool) {
	for _, p := range mastercraftplan.SmithAllProducts() {
		if p.BarID == bar {
			return p, true
		}
	}
	return mastercraftplan.SmithProduct{}, false
}

func (e *anvilEngine) read(b []byte) bool {
	delayed := e.ticks <= e.delayedUntil
	switch op := int(b[0]); op {
	case clientprot.OPLOCU:
		if delayed {
			e.dropped++
			return false
		}
		x, z, loc, obj, slot, com := tg2(b, 1), tg2(b, 3), tg2(b, 5), int32(tg2(b, 7)), tg2(b, 9), tg2(b, 11)
		if com != 3214 || slot >= 28 || e.pack[slot].id != obj || x != e.anvilX || z != e.anvilZ || loc != e.anvilID {
			return false
		}
		e.uses++
		// Every metal's arrowtips and platebody sit in the same column slots: the menu shows the product the
		// test asks about (the bar's, found by the first matching product for its level).
		var pick *mastercraftplan.SmithProduct
		for _, p := range mastercraftplan.SmithAllProducts() {
			if p.BarID == obj {
				pp := p
				pick = &pp
				break
			}
		}
		switch {
		case pick == nil:
			e.chat.Record("Nothing interesting happens.")
		case e.count(mastercraftplan.HammerID) == 0:
			e.chat.Record("You need a hammer to work the metal with.")
		case e.level < 15:
			e.chat.Record("You need a Smithing level of at least 15 to work irons.")
		default:
			e.menu = pick
		}
		return true
	case clientprot.INV_BUTTON1, clientprot.INV_BUTTON2, clientprot.INV_BUTTON3:
		if delayed {
			e.dropped++
			return false
		}
		obj, slot, com := int32(tg2(b, 1)), tg2(b, 3), tg2(b, 5)
		if e.menu == nil || com != e.menu.Com || slot != e.menu.Slot || obj != e.menu.ID {
			return false
		}
		p := *e.menu
		e.menu = nil // attempt_smithing: if_close
		switch {
		case e.count(p.BarID) < int32(p.BarsPer):
			e.mesboxes++
			return true
		case e.level < p.Level:
			e.mesboxes++
			return true
		case p.Members() && !e.members:
			e.chat.Record("You can only make that on a members' server.")
			return true
		}
		switch op {
		case clientprot.INV_BUTTON1:
			e.makeLeft = 1
		case clientprot.INV_BUTTON2:
			e.makeLeft = 5
		default:
			e.makeLeft = 10
		}
		e.delayedUntil = e.ticks + 1 + 1 // p_delay(1)
		e.makeAt = e.delayedUntil
		return true
	case clientprot.CLOSE_MODAL:
		e.menu = nil
		return true
	}
	return false
}

func (e *anvilEngine) tick() {
	e.ticks++
	for len(e.frames) > 0 && e.frames[0].at <= e.ticks {
		e.pending = append(e.pending, e.frames[0].pkts...)
		e.frames = e.frames[1:]
	}
	user := 0
	for len(e.pending) > 0 && user < 5 {
		b := e.pending[0]
		e.pending = e.pending[1:]
		if e.read(b) {
			user++
		} else {
			e.refused++
		}
	}
	if e.makeLeft > 0 && e.ticks >= e.makeAt {
		// Which product: the one whose bars are in the pack, best first (the tests smith one metal at a time).
		for _, p := range mastercraftplan.SmithAllProducts() {
			if e.count(p.BarID) >= int32(p.BarsPer) {
				e.del(p.BarID, int32(p.BarsPer))
				e.add(p.ID, int32(p.Per))
				e.xpT += p.XPTenthsPerBar * p.BarsPer // the server keeps tenths and sends whole XP
				break
			}
		}
		e.made++
		e.madeTicks = append(e.madeTicks, e.ticks)
		e.makeLeft--
		if e.makeLeft > 0 {
			e.delayedUntil = e.ticks + 1 + 2 + 1 + 1 // p_delay(2), then the recursion's p_delay(1)
			e.makeAt = e.delayedUntil
		} else {
			e.delayedUntil = e.ticks + 1 + 2 // p_delay(2)
		}
	}
	switch {
	case e.menu != nil && !e.menuOut:
		e.st.Handle(serverprot.IF_OPENMAIN, []byte{byte(mastercraftplan.SmithInterface >> 8), byte(mastercraftplan.SmithInterface & 0xff)})
		e.menuOut = true
	case e.menu == nil && e.menuOut:
		e.st.Handle(serverprot.IF_CLOSE, nil)
		e.menuOut = false
	}
	e.push()
	e.st.Handle(serverprot.PLAYER_INFO, []byte{0x00, 0x7f, 0xf0})
}

func (e *anvilEngine) writeInv(com int, rows []struct{ id, n int32 }) {
	p := packet.NewWriter(512)
	p.P2(uint16(com))
	p.P2(uint16(len(rows)))
	for _, r := range rows {
		if r.id == 0 || r.n <= 0 {
			p.P2(0)
			p.P1(0)
			continue
		}
		p.P2(uint16(r.id + 1))
		if r.n >= 255 {
			p.P1(255)
			p.P4(r.n)
		} else {
			p.P1(byte(r.n))
		}
	}
	e.st.Handle(serverprot.UPDATE_INV_FULL, p.Data[:p.Pos])
}

func (e *anvilEngine) push() {
	e.writeInv(actions.PlayerPack, e.pack[:])
	if e.menu != nil {
		col := make([]struct{ id, n int32 }, 5)
		col[e.menu.Slot] = struct{ id, n int32 }{e.menu.ID, int32(e.menu.Per)}
		e.writeInv(e.menu.Com, col)
	}
	e.st.Handle(serverprot.UPDATE_STAT, []byte{byte(SmithingStat), byte(e.xpT / 10 >> 24), byte(e.xpT / 10 >> 16), byte(e.xpT / 10 >> 8), byte(e.xpT / 10), byte(e.level)})
}

func aeAnvil() locindex.Match {
	return locindex.Match{Loc: locindex.Loc{ID: aeID, X: aeX, Z: aeZ}}
}

func aePack(hammer bool, rows ...jrow) []jrow {
	var out []jrow
	if hammer {
		out = append(out, jrow{mastercraftplan.HammerID, 1})
	}
	return append(out, rows...)
}

func aeBars(id int32, n int) []jrow {
	var out []jrow
	for i := 0; i < n; i++ {
		out = append(out, jrow{id, 1})
	}
	return out
}

const (
	aeSteel, aeIron, aeRune int32 = 2353, 2351, 2363
)

// 25 Steel bars are Make 10, Make 10, Make 5: three uses, one make every fifth tick, a hundred and
// twenty-five ticks of smithing, 375 arrowtips, and no packet read while the player was delayed.
func TestSmithAtAnvilTwentyFiveSteelBarsIsThreeUsesAndFiveTicksABar(t *testing.T) {
	e, d := newAnvilEngine(t, aePack(true, aeBars(aeSteel, 25)...))
	steel := mastercraftplan.SmithArrowtips[3]
	r := SmithAtAnvil(d, aeAnvil(), steel, nil)
	if r.Reason != "done" || r.Bars != 25 || r.Makes != 25 || r.Uses != 3 {
		t.Fatalf("%+v", r)
	}
	if e.count(41) != 375 || e.count(aeSteel) != 0 || e.count(mastercraftplan.HammerID) != 1 {
		t.Fatalf("pack %v", e.pack)
	}
	if e.dropped != 0 {
		t.Errorf("%d packet(s) read while the player was delayed", e.dropped)
	}
	// A bar every fifth tick inside a batch.
	gaps := map[int]int{}
	for i := 1; i < len(e.madeTicks); i++ {
		gaps[e.madeTicks[i]-e.madeTicks[i-1]]++
	}
	if gaps[5] < 22 {
		t.Errorf("gaps between makes %v: the makes of a batch are five ticks apart", gaps)
	}
	if r.Ticks < 125 || r.Ticks > 125+3*8 {
		t.Errorf("%d ticks for 25 bars: 125 of smithing and a few between the batches", r.Ticks)
	}
	if r.XP != 25*375/10 {
		t.Errorf("XP %d, want %d", r.XP, 25*375/10)
	}
	if !strings.Contains(r.Describe(steel), "25 x Steel bar -> 25 make(s) of Steel arrowtips from 3 use(s)") {
		t.Errorf("describe: %s", r.Describe(steel))
	}
}

// 25 Iron bars are five platebodies in one Make 5, a make every fifth tick: 25 ticks.
func TestSmithAtAnvilIronBarsAreFivePlatebodiesInOneUse(t *testing.T) {
	e, d := newAnvilEngine(t, aePack(true, aeBars(aeIron, 25)...))
	r := SmithAtAnvil(d, aeAnvil(), mastercraftplan.SmithIronPlatebody, nil)
	if r.Reason != "done" || r.Makes != 5 || r.Uses != 1 || e.count(1115) != 5 || e.count(aeIron) != 0 {
		t.Fatalf("%+v pack %v", r, e.pack)
	}
	if r.XP != 5*125 || e.dropped != 0 {
		t.Errorf("XP %d (want %d), %d dropped", r.XP, 5*125, e.dropped)
	}
	// Four bars left over make nothing and are left alone.
	e, d = newAnvilEngine(t, aePack(true, aeBars(aeIron, 4)...))
	if r := SmithAtAnvil(d, aeAnvil(), mastercraftplan.SmithIronPlatebody, nil); r.Reason != "done" || r.Uses != 0 || e.uses != 0 {
		t.Errorf("4 iron bars: %+v", r)
	}
}

func TestSmithAtAnvilSendsADroppedUseAgain(t *testing.T) {
	e, d := newAnvilEngine(t, aePack(true, aeBars(aeSteel, 10)...))
	e.delayedUntil = 4
	r := SmithAtAnvil(d, aeAnvil(), mastercraftplan.SmithArrowtips[3], nil)
	if r.Reason != "done" || r.Makes != 10 || e.dropped < 1 || r.Uses < 2 {
		t.Fatalf("%+v dropped %d", r, e.dropped)
	}
}

func TestSmithAtAnvilRefusals(t *testing.T) {
	steel := mastercraftplan.SmithArrowtips[3]
	// No hammer: nothing is sent.
	e, d := newAnvilEngine(t, aePack(false, aeBars(aeSteel, 10)...))
	if r := SmithAtAnvil(d, aeAnvil(), steel, nil); r.Reason != "no-hammer" || e.uses != 0 {
		t.Errorf("no hammer: %+v", r)
	}
	// The anvil says so: a hammer the pack lost since the check cannot happen here, so a level refusal.
	e, d = newAnvilEngine(t, aePack(true, aeBars(aeSteel, 10)...))
	e.level = 10
	if r := SmithAtAnvil(d, aeAnvil(), steel, nil); r.Reason != "level" || r.Makes != 0 {
		t.Errorf("level: %+v", r)
	}
	// A stop before the first use.
	_, d = newAnvilEngine(t, aePack(true, aeBars(aeSteel, 10)...))
	if r := SmithAtAnvil(d, aeAnvil(), steel, func() bool { return false }); r.Reason != "held" || r.Uses != 0 {
		t.Errorf("held: %+v", r)
	}
	// A free world refuses arrowtips (a members' server message) and the batch stalls.
	e, d = newAnvilEngine(t, aePack(true, aeBars(aeSteel, 10)...))
	e.members = false
	if r := SmithAtAnvil(d, aeAnvil(), steel, nil); r.Reason != "members" || r.Makes != 0 {
		t.Errorf("free world: %+v", r)
	}
}

// ---- the kit, over the bank simulator ----

func smithObjs() actionsim.Objs {
	notes := map[int32]int32{}
	noteOf := map[int32]int32{}
	for _, id := range []int32{aeSteel, aeIron, aeRune, 2359, 2361} {
		notes[id+1], noteOf[id] = id, id+1
	}
	return actionsim.Objs{
		Stackable: func(id int32) bool { return aeStack(id) },
		NoteOf:    func(id int32) int32 { return noteOf[id] },
		UnnoteOf:  func(id int32) int32 { return notes[id] },
	}
}

func smithVisit(t *testing.T, pack, bank []actionsim.Stack, products []mastercraftplan.SmithProduct) (*actionsim.Session, SmithKitResult) {
	t.Helper()
	sim := actionsim.New(smithObjs(), pack)
	ckRun(t, sim)
	sim.OpenBank(bank)
	d := &fleetrunner.Deps{Session: sim, Components: actionsim.Components(), Delay: func(dur time.Duration) bool { time.Sleep(dur); return true }}
	b := actions.NewBank(sim, actionsim.Components())
	deadline := time.Now().Add(2 * time.Second)
	for !b.IsOpen() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !b.IsOpen() {
		t.Fatal("the simulated bank never opened")
	}
	time.Sleep(60 * time.Millisecond)
	return sim, SmithKit(d, b, products, nil)
}

// A visit banks the arrowtips and what is left (a note becomes the item), keeps the hammer, and draws a FULL
// pack: 27 free slots less nothing, the best bar first, then the next.
func TestSmithKitKeepsTheHammerAndDrawsAFullPack(t *testing.T) {
	pack := []actionsim.Stack{
		{ID: mastercraftplan.HammerID, Count: 1}, {ID: 41, Count: 375}, {ID: 1115, Count: 1}, {ID: 1115, Count: 1},
		{ID: aeSteel + 1, Count: 300}, // a mule note
	}
	bank := []actionsim.Stack{{ID: aeRune, Count: 3}, {ID: 2359, Count: 100}, {ID: aeSteel, Count: 8000}, {ID: aeIron, Count: 900}}
	products := mastercraftplan.SmithProductsFor(mastercraftplan.DefaultSettings(), 99)
	sim, res := smithVisit(t, pack, bank, products)
	if res.Err != "" || res.NoHammer {
		t.Fatalf("%+v", res)
	}
	got := sim.Pack()
	if ckCount(got, mastercraftplan.HammerID) != 1 || ckCount(got, aeRune) != 3 || ckCount(got, 2359) != 24 || ckCount(got, 41) != 0 || ckCount(got, 1115) != 0 {
		t.Errorf("pack %v", got)
	}
	rows := 0
	for _, r := range got {
		if r.ID != 0 {
			rows++
		}
	}
	if rows != 28 {
		t.Errorf("%d rows: the pack is not full: %v", rows, got)
	}
	b := sim.Bank()
	if ckCount(b, 41) != 375 || ckCount(b, 1115) != 2 || ckCount(b, aeSteel) != 8300 || ckCount(b, 2359) != 76 {
		t.Errorf("bank %v", b)
	}
	if len(res.Draws) != 2 || res.Draws[0].N != 3 || res.Draws[1].N != 24 {
		t.Errorf("draws %+v", res.Draws)
	}
}

// With no hammer anywhere the visit says so and draws nothing.
func TestSmithKitWithNoHammer(t *testing.T) {
	bank := []actionsim.Stack{{ID: aeSteel, Count: 800}}
	sim, res := smithVisit(t, nil, bank, mastercraftplan.SmithProductsFor(mastercraftplan.DefaultSettings(), 99))
	if !res.NoHammer || len(sim.Pack()) != 0 && ckCount(sim.Pack(), aeSteel) != 0 {
		t.Errorf("%+v pack %v", res, sim.Pack())
	}
	// A hammer in the bank is drawn, and the pack then holds 26 bars.
	bank = append(bank, actionsim.Stack{ID: mastercraftplan.HammerID, Count: 5})
	sim, res = smithVisit(t, nil, bank, mastercraftplan.SmithProductsFor(mastercraftplan.DefaultSettings(), 99))
	if res.NoHammer || ckCount(sim.Pack(), mastercraftplan.HammerID) != 1 || ckCount(sim.Pack(), aeSteel) != 27 {
		t.Errorf("%+v pack %v", res, sim.Pack())
	}
}

func TestSmithReadersAndProducts(t *testing.T) {
	it := func(id, n int32) gamestate.SlotItem {
		return gamestate.SlotItem{Item: gamestate.Item{ID: id, Count: n}}
	}
	banked := []gamestate.SlotItem{it(aeSteel, 9), it(mastercraftplan.HammerID, 2), it(41, 30), it(1115, 3)}
	own := SmithOwn(banked)
	if own("Steel bar") != 9 || own("Hammer") != 2 || own("Iron bar") != 0 {
		t.Errorf("own %d %d", own("Steel bar"), own("Hammer"))
	}
	c := SmithProductCounts(banked)
	if c["Steel arrowtips"] != 30 || c["Iron platebody"] != 3 || len(c) != 2 {
		t.Errorf("products %v", c)
	}
	if got := Names(SmithProducts()); len(got) != 5 || got[0] != "Rune arrowtips" || got[4] != "Iron platebody" {
		t.Errorf("hand-over names %v", got)
	}
}

// ---- the Varrock west anvils, over the real loc index and collision pack ----

// The anvil is found by NAME near the Varrock west bank (content loc 2783 at 3188,3421/3424/3426), a tile the
// engine accepts a use from exists, and the collision pack has a route from the bank to it: the hop the round
// runs back and forth is 10 to 20 tiles.
func TestTheVarrockWestAnvilIsFoundByNameAndReachableFromTheBank(t *testing.T) {
	locsPath, cfgPath, packPath := os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG"), os.Getenv("RSB0T_COLLISION_PACK")
	if locsPath == "" || cfgPath == "" || packPath == "" {
		t.Skip("set RSB0T_LOC_INDEX, RSB0T_CONFIG_JAG and RSB0T_COLLISION_PACK to check the real anvils")
	}
	ix, err := locindex.Load(locsPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := cachecfg.LoadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	pk, err := pathfind.Load(packPath)
	if err != nil {
		t.Fatal(err)
	}
	_, d := newAnvilEngine(t, nil)
	d.Locs, d.Config = ix, cfg
	bank := VarrockWestBank
	anvil, ok := FindAnvil(d, bank, 20)
	if !ok || anvil.ID != aeID || anvil.X != 3188 || !strings.EqualFold(anvil.Name, AnvilName) {
		t.Fatalf("the anvil near the Varrock west bank: %+v %v", anvil, ok)
	}
	if far, ok := FindAnvil(d, tile.Tile{X: 2809, Z: 3441}, 20); ok {
		t.Errorf("an anvil at the mule's post: %+v", far)
	}
	stands := AnvilStands(d, anvil, bank)
	if len(stands) == 0 {
		t.Fatal("no tile the engine accepts a use on the anvil from")
	}
	res := pk.FindPath(pathfind.NavPoint{X: bank.X, Z: bank.Z}, pathfind.NavPoint{X: stands[0].X, Z: stands[0].Z})
	if !res.OK {
		t.Fatalf("no route from the bank to %v: %s", stands[0], res.Reason)
	}
	t.Logf("anvil %d,%d; nearest stand %v; route %d step(s) from the bank", anvil.X, anvil.Z, stands[0], res.Steps())
	var walked []tile.Tile
	if !ApproachAnvil(d, anvil, func(dest tile.Tile, radius int, timeout time.Duration) bool {
		walked = append(walked, dest)
		return true
	}, time.Minute) || len(walked) != 1 {
		t.Fatalf("walked %v, want one walk to a stand", walked)
	}
	// (The session has no tile here, so "nearest" is measured from the map's origin: any stand will do.)
	found := false
	for _, s := range stands {
		found = found || s == walked[0]
	}
	if !found {
		t.Errorf("walked to %v, not one of the stands %v", walked[0], stands)
	}
}
