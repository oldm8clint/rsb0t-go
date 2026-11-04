package mastercraftrunner

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
)

// furnaceEngine plays the engine's side of a Steel bar used on a furnace TICK BY TICK, as content 289 runs it
// (skill_smithing/scripts/smelting/cannonballs.rs2): the refusals, then p_delay(4), p_delay(0), p_delay(2) (5 + 1 + 3 ticks), and
// at the end the bar goes and four balls come. A packet read while the player is delayed is dropped without a word.
type furnaceEngine struct {
	st     *gamestate.State
	chat   *chatlog.Log
	cheats anticheat.Counters

	pack    [28]struct{ id, n int32 }
	ticks   int
	frames  []tFrame
	pending [][]byte

	delayedUntil int
	doneAt       int
	smelting     bool

	xpT, level int
	members    bool
	uses       int
	dropped    int
	madeTicks  []int
}

const feX, feZ, feID = 2975, 3369, 11666

func newFurnaceEngine(t *testing.T, pack []struct{ id, n int32 }) (*furnaceEngine, *fleetrunner.Deps) {
	t.Helper()
	e := &furnaceEngine{st: gamestate.New(), chat: chatlog.New(), level: 60, members: true}
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

func (e *furnaceEngine) State() *gamestate.State     { return e.st }
func (e *furnaceEngine) Cheats() *anticheat.Counters { return &e.cheats }

func (e *furnaceEngine) Send(build func(w *packet.Packet)) error {
	w := packet.NewWriter(64)
	build(w)
	b := append([]byte(nil), w.Data[:w.Pos]...)
	var pkts [][]byte
	for len(b) > 0 {
		if int(b[0]) != clientprot.OPLOCU || len(b) < 13 {
			pkts = append(pkts, b)
			break
		}
		pkts = append(pkts, b[:13])
		b = b[13:]
	}
	e.frames = append(e.frames, tFrame{e.ticks + 1, pkts})
	return nil
}

func (e *furnaceEngine) count(id int32) int32 {
	var n int32
	for _, s := range e.pack {
		if s.id == id {
			n += s.n
		}
	}
	return n
}

func (e *furnaceEngine) del(id, n int32) {
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

// add puts n of a stackable id on its stack (or the first free slot).
func (e *furnaceEngine) addStack(id, n int32) {
	for i := range e.pack {
		if e.pack[i].id == id {
			e.pack[i].n += n
			return
		}
	}
	for i := range e.pack {
		if e.pack[i].id == 0 {
			e.pack[i].id, e.pack[i].n = id, n
			return
		}
	}
}

func (e *furnaceEngine) read(b []byte) bool {
	if int(b[0]) != clientprot.OPLOCU {
		return false
	}
	if e.ticks <= e.delayedUntil {
		e.dropped++
		return false
	}
	x, z, loc, obj, slot, com := tg2(b, 1), tg2(b, 3), tg2(b, 5), int32(tg2(b, 7)), tg2(b, 9), tg2(b, 11)
	if com != 3214 || slot >= 28 || e.pack[slot].id != obj || x != feX || z != feZ || loc != feID {
		return false
	}
	e.uses++
	switch {
	case obj != mastercraftplan.SteelBarID:
		e.chat.Record("Nothing interesting happens.")
	case !e.members:
		e.chat.Record("You need to be on a members' world to make cannonballs.")
	case e.level < 35:
		e.chat.Record("You need a Smithing level of 35 to make cannonballs.")
	case e.count(mastercraftplan.AmmoMouldID) < 1:
		e.chat.Record("You need a cannonball mould to make cannonballs.")
	default:
		e.smelting = true
		e.doneAt = e.ticks + 9 // p_delay(4) + p_delay(0) + p_delay(2): 5 + 1 + 3 ticks
		e.delayedUntil = e.doneAt
	}
	return true
}

func (e *furnaceEngine) tick() {
	e.ticks++
	for len(e.frames) > 0 && e.frames[0].at <= e.ticks {
		e.pending = append(e.pending, e.frames[0].pkts...)
		e.frames = e.frames[1:]
	}
	for len(e.pending) > 0 {
		b := e.pending[0]
		e.pending = e.pending[1:]
		e.read(b)
	}
	if e.smelting && e.ticks >= e.doneAt {
		e.smelting = false
		e.del(mastercraftplan.SteelBarID, 1)
		e.addStack(mastercraftplan.CannonballID, mastercraftplan.CannonballsPerBar)
		e.xpT += mastercraftplan.CannonballXPTenths
		e.madeTicks = append(e.madeTicks, e.ticks)
	}
	e.push()
	e.st.Handle(serverprot.PLAYER_INFO, []byte{0x00, 0x7f, 0xf0})
}

func (e *furnaceEngine) push() {
	p := packet.NewWriter(512)
	p.P2(uint16(actions.PlayerPack))
	p.P2(uint16(len(e.pack)))
	for _, r := range e.pack {
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
	e.st.Handle(serverprot.UPDATE_STAT, []byte{byte(SmithingStat), byte(e.xpT / 10 >> 24), byte(e.xpT / 10 >> 16), byte(e.xpT / 10 >> 8), byte(e.xpT / 10), byte(e.level)})
}

func feFurnace() locindex.Match {
	return locindex.Match{Loc: locindex.Loc{ID: feID, X: feX, Z: feZ}}
}

func fePack(mould bool, steel int) []struct{ id, n int32 } {
	var out []struct{ id, n int32 }
	if mould {
		out = append(out, struct{ id, n int32 }{mastercraftplan.AmmoMouldID, 1})
	}
	for i := 0; i < steel; i++ {
		out = append(out, struct{ id, n int32 }{mastercraftplan.SteelBarID, 1})
	}
	return out
}

// 27 Steel bars are 27 uses, a bar every ten ticks (the use is sent as the balls show, the tick after the delay ends), 108 cannonballs,
// 1,012 Smithing XP, and no packet read while the player was delayed.
func TestSmeltCannonballsIsTenTicksABarAndFourBallsEach(t *testing.T) {
	e, d := newFurnaceEngine(t, fePack(true, 27))
	r := SmeltCannonballs(d, feFurnace(), nil)
	if r.Reason != "done" || r.Bars != 27 || r.Balls != 108 || r.Uses != 27 {
		t.Fatalf("%+v", r)
	}
	if e.count(mastercraftplan.CannonballID) != 108 || e.count(mastercraftplan.SteelBarID) != 0 || e.count(mastercraftplan.AmmoMouldID) != 1 {
		t.Fatalf("pack %v", e.pack)
	}
	if e.dropped != 0 {
		t.Errorf("%d packet(s) read while the player was delayed", e.dropped)
	}
	gaps := map[int]int{}
	for i := 1; i < len(e.madeTicks); i++ {
		gaps[e.madeTicks[i]-e.madeTicks[i-1]]++
	}
	if gaps[mastercraftplan.CannonBarTicks] < 24 {
		t.Errorf("gaps between bars %v: ten ticks apart (the constant says %d)", gaps, mastercraftplan.CannonBarTicks)
	}
	if r.Ticks < 27*10-1 || r.Ticks > 27*10+12 {
		t.Errorf("%d ticks for 27 bars", r.Ticks)
	}
	if want := 27 * mastercraftplan.CannonballXPTenths / 10; r.XP != want {
		t.Errorf("XP %d, want %d", r.XP, want)
	}
	if !strings.Contains(r.Describe(), "27 Steel bar(s) -> 108 cannonballs from 27 use(s)") {
		t.Errorf("describe: %s", r.Describe())
	}
}

// A use read while the player is still delayed is dropped without a word; it is sent again.
func TestSmeltCannonballsSendsADroppedUseAgain(t *testing.T) {
	e, d := newFurnaceEngine(t, fePack(true, 3))
	e.delayedUntil = 5
	r := SmeltCannonballs(d, feFurnace(), nil)
	if r.Reason != "done" || r.Bars != 3 || e.dropped < 1 || r.Uses < 4 {
		t.Fatalf("%+v dropped %d", r, e.dropped)
	}
}

func TestSmeltCannonballsRefusals(t *testing.T) {
	// No mould in the pack: nothing is sent.
	e, d := newFurnaceEngine(t, fePack(false, 5))
	if r := SmeltCannonballs(d, feFurnace(), nil); r.Reason != "no-mould" || e.uses != 0 {
		t.Errorf("no mould: %+v uses %d", r, e.uses)
	}
	// Smithing under 35: the furnace says so.
	e, d = newFurnaceEngine(t, fePack(true, 5))
	e.level = 34
	if r := SmeltCannonballs(d, feFurnace(), nil); r.Reason != "level" || r.Bars != 0 || e.uses != 1 {
		t.Errorf("level: %+v uses %d", r, e.uses)
	}
	// A free world.
	e, d = newFurnaceEngine(t, fePack(true, 5))
	e.members = false
	if r := SmeltCannonballs(d, feFurnace(), nil); r.Reason != "members" || r.Bars != 0 {
		t.Errorf("free world: %+v", r)
	}
	// A hold that says stop before the first use.
	e, d = newFurnaceEngine(t, fePack(true, 5))
	if r := SmeltCannonballs(d, feFurnace(), func() bool { return false }); r.Reason != "held" || e.uses != 0 {
		t.Errorf("held: %+v", r)
	}
	// No steel bar: done at once, nothing sent.
	e, d = newFurnaceEngine(t, fePack(true, 0))
	if r := SmeltCannonballs(d, feFurnace(), nil); r.Reason != "done" || e.uses != 0 {
		t.Errorf("no bar: %+v", r)
	}
	// A use the engine never answers (the furnace is not where the use went): three tries, then stalled.
	defer func(w time.Duration) { cannonWait = w }(cannonWait)
	cannonWait = 100 * time.Millisecond
	e, d = newFurnaceEngine(t, fePack(true, 5))
	bad := locindex.Match{Loc: locindex.Loc{ID: feID, X: feX + 5, Z: feZ}}
	if r := SmeltCannonballs(d, bad, nil); (r.Reason != "stalled" && r.Reason != "reach") || r.Bars != 0 || e.uses != 0 {
		t.Errorf("unanswered: %+v", r)
	}
}

func TestCannonReadersAndProducts(t *testing.T) {
	row := func(id int32, n int32) gamestate.SlotItem {
		return gamestate.SlotItem{Item: gamestate.Item{ID: id, Count: n}}
	}
	banked := []gamestate.SlotItem{row(mastercraftplan.SteelBarID, 40), row(mastercraftplan.CannonballID, 1500), row(mastercraftplan.AmmoMouldID, 1)}
	steel, balls, mould := CannonOwn(banked)
	if steel != 40 || balls != 1500 || !mould {
		t.Errorf("%d %d %v", steel, balls, mould)
	}
	if steel, balls, mould := CannonOwn(nil); steel != 0 || balls != 0 || mould {
		t.Error("an empty bank")
	}
	if ps := CannonProducts(); len(ps) != 1 || ps[0].ID != 2 || ps[0].Name != "Cannonball" {
		t.Errorf("%+v", ps)
	}
}

// ---- the kit, over the bank simulator ----

func cannonObjs() actionsim.Objs {
	const steel, balls = mastercraftplan.SteelBarID, mastercraftplan.CannonballID
	notes := map[int32]int32{steel + 1000: steel}
	noteOf := map[int32]int32{steel: steel + 1000}
	return actionsim.Objs{
		Stackable: func(id int32) bool { return id == balls || id == steel+1000 },
		NoteOf:    func(id int32) int32 { return noteOf[id] },
		UnnoteOf:  func(id int32) int32 { return notes[id] },
	}
}

func cannonVisit(t *testing.T, pack, bank []actionsim.Stack, remaining int) (*actionsim.Session, CannonKitResult) {
	t.Helper()
	sim := actionsim.New(cannonObjs(), pack)
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
	return sim, CannonKit(d, b, remaining)
}

// A visit banks the balls and the rest of the pack, keeps the mould, and draws a full pack of 27 Steel bars.
func TestCannonKitKeepsTheMouldAndDrawsAFullPack(t *testing.T) {
	pack := []actionsim.Stack{
		{ID: mastercraftplan.AmmoMouldID, Count: 1}, {ID: mastercraftplan.CannonballID, Count: 108}, {ID: 1115, Count: 1},
	}
	bank := []actionsim.Stack{{ID: mastercraftplan.SteelBarID, Count: 8000}}
	sim, res := cannonVisit(t, pack, bank, 500)
	if res.Err != "" || res.NoMould || !res.Mould || res.Drew != 27 {
		t.Fatalf("%+v", res)
	}
	if got := ckCount(sim.Pack(), mastercraftplan.SteelBarID); got != 27 {
		t.Errorf("pack steel %d: %v", got, sim.Pack())
	}
	if ckCount(sim.Pack(), mastercraftplan.AmmoMouldID) != 1 || ckCount(sim.Pack(), mastercraftplan.CannonballID) != 0 || ckCount(sim.Pack(), 1115) != 0 {
		t.Errorf("pack %v", sim.Pack())
	}
	if _, balls, _ := CannonOwn(res.Banked); balls != 108 {
		t.Errorf("the balls were not banked: %d", balls)
	}
}

// The remaining budget bounds the draw, and a mould in the bank is drawn when the pack has none.
func TestCannonKitDrawsTheMouldAndStopsAtTheBudget(t *testing.T) {
	bank := []actionsim.Stack{{ID: mastercraftplan.AmmoMouldID, Count: 1}, {ID: mastercraftplan.SteelBarID, Count: 8000}}
	sim, res := cannonVisit(t, nil, bank, 10)
	if res.Err != "" || res.NoMould || !res.Mould || res.Drew != 10 {
		t.Fatalf("%+v", res)
	}
	if ckCount(sim.Pack(), mastercraftplan.AmmoMouldID) != 1 || ckCount(sim.Pack(), mastercraftplan.SteelBarID) != 10 {
		t.Errorf("pack %v", sim.Pack())
	}
}

// No mould in the pack or the bank: nothing is drawn and the visit says so (the round never smelts blind).
func TestCannonKitWithNoMouldDrawsNothing(t *testing.T) {
	bank := []actionsim.Stack{{ID: mastercraftplan.SteelBarID, Count: 8000}}
	sim, res := cannonVisit(t, nil, bank, 500)
	if !res.NoMould || res.Drew != 0 || ckCount(sim.Pack(), mastercraftplan.SteelBarID) != 0 {
		t.Fatalf("%+v pack %v", res, sim.Pack())
	}
}
