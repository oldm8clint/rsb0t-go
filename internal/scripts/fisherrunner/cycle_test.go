package fisherrunner

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/webwalk"
)

func realConfig(t *testing.T) *cachecfg.Config {
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

// unnotedID is name's unnoted obj id in cfg.
func unnotedID(t *testing.T, cfg *cachecfg.Config, name string, noted bool) int32 {
	t.Helper()
	for _, id := range cfg.ObjIDs(name) {
		if o, ok := cfg.Obj(id); ok && o.Noted() == noted {
			return int32(id)
		}
	}
	t.Fatalf("no %s (noted %v) in the config", name, noted)
	return 0
}

// ---- an engine that plays the cycle's side, tick by tick ----

type slotRow struct{ id, n int32 }

// cycleEngine plays the engine's side of the drop cycle as content 289 and engine 289 run it: five
// handled user events a tick read in order, the rest queued; an OPHELD or OPHELDU whose slot does not
// hold its item is refused and does not count; a casket removes its own slot and adds a scripted reward
// (coins and tips stack, the rest take the lowest empty slot) behind a reward box; an oyster removes
// the LOWEST slot holding one and adds a scripted pearl or Empty oyster; a drop empties the slot; the
// two key halves join into a crystal key; a chisel on an Oyster pearl is six Pearl bolttips at
// Fletching 41 and nothing below. Every item is looked up by name in the real config.
type cycleEngine struct {
	t      *testing.T
	st     *gamestate.State
	cheats anticheat.Counters
	cfg    *cachecfg.Config
	pack   [28]slotRow

	frames  [][][]byte
	pending [][]byte
	box     bool
	ticks   int
	perTick map[int]int
	sent    map[int]int // packets sent by opcode
	did     map[string]int
	refuse  map[int]bool // slots whose clicks the engine refuses (a stale view)

	fletching int
	// The scripted rewards, used in turn.
	casketRewards, oysterRewards []int32
	nextCasket, nextOyster       int

	id struct {
		casket, oyster, empty, pearl, pearls, tips, key, chisel, coins, sapphire int32
		half                                                                     [2]int32
	}
}

func newCycleEngine(t *testing.T, cfg *cachecfg.Config) *cycleEngine {
	e := &cycleEngine{t: t, st: gamestate.New(), cfg: cfg, perTick: map[int]int{}, sent: map[int]int{}, did: map[string]int{}}
	e.cheats.SetEnabled(false)
	e.id.casket = unnotedID(t, cfg, "Casket", false)
	e.id.oyster = unnotedID(t, cfg, "Oyster", false)
	e.id.empty = unnotedID(t, cfg, "Empty oyster", false)
	e.id.pearl = unnotedID(t, cfg, "Oyster pearl", false)
	e.id.pearls = unnotedID(t, cfg, "Oyster pearls", false)
	e.id.tips = unnotedID(t, cfg, "Pearl bolttips", false)
	e.id.key = unnotedID(t, cfg, "Crystal key", false)
	e.id.chisel = unnotedID(t, cfg, "Chisel", false)
	e.id.coins = unnotedID(t, cfg, "Coins", false)
	e.id.sapphire = unnotedID(t, cfg, "Uncut sapphire", false)
	n := 0
	for _, id := range cfg.ObjIDs("Half of a key") {
		if o, ok := cfg.Obj(id); ok && !o.Noted() && n < 2 {
			e.id.half[n] = int32(id)
			n++
		}
	}
	if n != 2 {
		t.Fatalf("the config names %d unnoted halves of a key, want 2", n)
	}
	e.setFletching(41)
	return e
}

func (e *cycleEngine) setFletching(level int) {
	e.fletching = level
	e.st.Handle(serverprot.UPDATE_STAT, []byte{byte(FletchingStat), 0, 0, 0, 0, byte(level)})
}

func (e *cycleEngine) name(id int32) string {
	o, _ := e.cfg.Obj(int(id))
	return o.Name
}

// fill puts the named items in the pack from slot 0, in order; a leading "-" is the bank note and a
// trailing " xN" repeats the item N times.
func (e *cycleEngine) fill(items ...string) {
	slot := 0
	for _, it := range items {
		n := 1
		if i := strings.LastIndex(it, " x"); i > 0 {
			fmt.Sscanf(it[i+2:], "%d", &n)
			it = it[:i]
		}
		noted := strings.HasPrefix(it, "-")
		it = strings.TrimPrefix(it, "-")
		id := unnotedID(e.t, e.cfg, it, noted)
		for k := 0; k < n; k++ {
			e.pack[slot] = slotRow{id: id, n: 1}
			slot++
		}
	}
	e.push()
}

func (e *cycleEngine) Send(build func(w *packet.Packet)) error {
	w := packet.NewWriter(256)
	build(w)
	b := append([]byte(nil), w.Data[:w.Pos]...)
	var pkts [][]byte
	for len(b) > 0 {
		n := 7
		switch int(b[0]) {
		case clientprot.CLOSE_MODAL:
			n = 1
		case clientprot.OPHELDU:
			n = 13
		}
		e.sent[int(b[0])]++
		pkts = append(pkts, b[:n])
		b = b[n:]
	}
	e.frames = append(e.frames, pkts)
	return nil
}
func (e *cycleEngine) State() *gamestate.State     { return e.st }
func (e *cycleEngine) Cheats() *anticheat.Counters { return &e.cheats }

// refuseSlot makes the engine refuse every click that names the slot.
func (e *cycleEngine) refuseSlot(slot int) {
	if e.refuse == nil {
		e.refuse = map[int]bool{}
	}
	e.refuse[slot] = true
}

func (e *cycleEngine) packetsSent() int {
	n := 0
	for _, c := range e.sent {
		n += c
	}
	return n
}

func g2(b []byte, at int) int { return int(b[at])<<8 | int(b[at+1]) }

func (e *cycleEngine) stackable(id int32) bool {
	o, _ := e.cfg.Obj(int(id))
	return o.Stackable
}

func (e *cycleEngine) add(id int32, n int32) {
	if e.stackable(id) {
		for i := range e.pack {
			if e.pack[i].id == id {
				e.pack[i].n += n
				return
			}
		}
	}
	for i := range e.pack {
		if e.pack[i].id == 0 {
			e.pack[i] = slotRow{id: id, n: n}
			return
		}
	}
	e.t.Fatalf("the engine had no free slot for %s: a reward must always fit", e.name(id))
}

func (e *cycleEngine) removeFirst(id int32) bool {
	for i := range e.pack {
		if e.pack[i].id == id {
			e.pack[i] = slotRow{}
			return true
		}
	}
	return false
}

func (e *cycleEngine) read(b []byte) bool {
	switch int(b[0]) {
	case clientprot.CLOSE_MODAL:
		e.box = false
		return true
	case clientprot.OPHELD1, clientprot.OPHELD5:
		obj, slot, com := int32(g2(b, 1)), g2(b, 3), g2(b, 5)
		if com != PlayerPack || slot >= 28 || e.pack[slot].id != obj || e.refuse[slot] {
			return false
		}
		e.box = false // clearPendingAction: the backpack is not the main modal
		if int(b[0]) == clientprot.OPHELD5 {
			e.pack[slot] = slotRow{}
			e.did["drop"]++
			return true
		}
		switch obj {
		case e.id.casket:
			e.pack[slot] = slotRow{}
			reward := e.casketRewards[e.nextCasket%len(e.casketRewards)]
			e.nextCasket++
			if reward == e.id.coins {
				e.add(reward, 80)
			} else {
				e.add(reward, 1)
			}
			e.box = true
			e.did["casket"]++
		case e.id.oyster:
			e.removeFirst(e.id.oyster)
			e.add(e.oysterRewards[e.nextOyster%len(e.oysterRewards)], 1)
			e.nextOyster++
			e.did["oyster"]++
		default:
			return false
		}
		return true
	case clientprot.OPHELDU:
		target, tslot := int32(g2(b, 1)), g2(b, 3)
		used, uslot := int32(g2(b, 7)), g2(b, 9)
		if tslot >= 28 || uslot >= 28 || e.pack[tslot].id != target || e.pack[uslot].id != used || e.refuse[tslot] {
			return false
		}
		e.box = false
		pair := func(a, b int32) bool { return (target == a && used == b) || (target == b && used == a) }
		switch {
		case pair(e.id.pearl, e.id.chisel) || pair(e.id.pearls, e.id.chisel):
			if e.fletching < 41 {
				return true // "You need a Fletching level of 41": the use is handled and nothing happens
			}
			p, per := e.id.pearl, int32(6)
			if target == e.id.pearls || used == e.id.pearls {
				p, per = e.id.pearls, 24
			}
			e.removeFirst(p)
			e.add(e.id.tips, per)
			e.did["cut"]++
		case pair(e.id.half[0], e.id.half[1]):
			e.removeFirst(e.id.half[0])
			e.removeFirst(e.id.half[1])
			e.add(e.id.key, 1)
			e.did["join"]++
		default:
			return false
		}
		return true
	}
	return false
}

func (e *cycleEngine) tick() {
	e.ticks++
	for len(e.frames) > 0 {
		e.pending = append(e.pending, e.frames[0]...)
		e.frames = e.frames[1:]
	}
	user, client := 0, 0
	wasBox := e.box
	for len(e.pending) > 0 && user < 5 && client < 20 {
		b := e.pending[0]
		e.pending = e.pending[1:]
		if e.read(b) {
			user++
		} else {
			client++
		}
	}
	e.perTick[e.ticks] = user
	e.push()
	if e.box && !wasBox {
		e.st.Handle(serverprot.IF_OPENCHAT, []byte{0x01, 0x00})
	} else if !e.box && wasBox {
		e.st.Handle(serverprot.IF_CLOSE, nil)
	}
	e.st.Handle(serverprot.PLAYER_INFO, []byte{0x00, 0x7f, 0xf0})
}

func (e *cycleEngine) push() {
	p := packet.NewWriter(512)
	p.P2(uint16(PlayerPack))
	p.P2(28)
	for _, s := range e.pack {
		if s.id == 0 {
			p.P2(0)
			p.P1(0)
			continue
		}
		p.P2(uint16(s.id + 1))
		if s.n >= 255 {
			p.P1(255)
			p.P4(s.n)
		} else {
			p.P1(byte(s.n))
		}
	}
	e.st.Handle(serverprot.UPDATE_INV_FULL, p.Data[:p.Pos])
}

func (e *cycleEngine) delay(time.Duration) bool { e.tick(); return true }

// count is how many SLOTS hold id; total how many items of it the pack holds.
func (e *cycleEngine) count(id int32) int {
	n := 0
	for _, s := range e.pack {
		if s.id == id {
			n++
		}
	}
	return n
}

func (e *cycleEngine) total(id int32) int {
	n := 0
	for _, s := range e.pack {
		if s.id == id {
			n += int(s.n)
		}
	}
	return n
}

func (e *cycleEngine) maxPerTick() int {
	most := 0
	for _, n := range e.perTick {
		most = max(most, n)
	}
	return most
}

// cycleRig is a cycle engine with a Deps wired to it, its Loot sink and its log.
type cycleRig struct {
	e     *cycleEngine
	d     *Deps
	drops []loot.Drop
	logs  []string
}

func newCycleRig(t *testing.T) *cycleRig {
	cfg := realConfig(t)
	e := newCycleEngine(t, cfg)
	r := &cycleRig{e: e}
	r.d = &Deps{
		Session: e, Config: cfg, Delay: e.delay, Junk: fisherplan.IsBassOysterJunk,
		Cycle: &Cycle{Opts: fisherplan.DefaultCycleOptions(), OnWalks: true},
		Loot:  func(dr loot.Drop) { r.drops = append(r.drops, dr) },
		Log:   func(s string) { r.logs = append(r.logs, s) },
	}
	e.casketRewards = []int32{e.id.coins, e.id.half[0], e.id.half[1], e.id.sapphire}
	e.oysterRewards = []int32{e.id.pearl, e.id.empty, e.id.pearl, e.id.pearl}
	return r
}

// fullPack is a full pack: the net, a chisel and the catch of a fishing session: three caskets, four
// oysters, four junk slots (two of them raw fish) and fifteen bass.
func (r *cycleRig) fullPack() {
	r.e.fill("Big fishing net", "Raw mackerel", "Raw bass", "Casket", "Oyster", "Leather boots", "Casket", "Oyster", "Chisel",
		"Seaweed", "Oyster", "Casket", "Raw cod", "Oyster", "Raw bass x14")
}

func (r *cycleRig) dropFor(kind string) (loot.Drop, bool) {
	for _, dr := range r.drops {
		if dr.Source.Name == kind {
			return dr, true
		}
	}
	return loot.Drop{}, false
}

func itemNames(dr loot.Drop) map[string]int64 {
	out := map[string]int64{}
	for _, it := range dr.Items {
		out[it.Name] += it.Count
	}
	return out
}

// ---- the tests ----

// THE POINT: one cycle opens the caskets and the oysters, joins the two key halves a casket gave,
// cuts the pearls into tips at Fletching 41, and drops the junk and the Empty oyster, in two bursts of
// at most five clicks a tick. What is left is the net, the chisel, the bass and what the opens made.
func TestRunCycleTidiesAWholePackRealConfig(t *testing.T) {
	r := newCycleRig(t)
	e := r.e
	r.fullPack()
	if FreeSlots(r.d) != 0 || !CycleDue(r.d) {
		t.Fatalf("a full pack with work is not due: %d free slots", FreeSlots(r.d))
	}
	res := RunCycle(r.d, "fishing")
	if !res.Progress() || res.Stopped {
		t.Fatalf("%+v", res)
	}
	if res.Caskets != 3 || res.Oysters != 4 || res.Pearls != 3 || res.Empties != 1 || res.Tips != 18 || res.Raw != 2 {
		t.Fatalf("result %+v: want 3 caskets, 4 oysters, 3 pearls, 1 empty, 18 tips, 2 raw fish dropped", res)
	}
	// Seven opens, one join, three cuts, and five drops: the four junk slots (mackerel, boots, seaweed,
	// cod) and the Empty oyster the opens made.
	if res.Counts.Opens != 7 || res.Counts.Joins != 1 || res.Counts.Cuts != 3 || res.Counts.Drops != 5 || res.Counts.Total() != 16 {
		t.Fatalf("clicks %+v, want 7 opens, 1 join, 3 cuts, 5 drops", res.Counts)
	}
	for _, name := range []string{"Casket", "Oyster", "Empty oyster", "Oyster pearl", "Raw mackerel", "Leather boots", "Seaweed", "Raw cod", "Half of a key"} {
		if n := e.count(unnotedID(t, e.cfg, name, false)); n != 0 {
			t.Errorf("%d %s left in the pack", n, name)
		}
	}
	if e.count(e.id.key) != 1 || e.total(e.id.tips) != 18 || e.count(e.id.tips) != 1 || e.total(e.id.coins) != 80 || e.count(e.id.sapphire) != 0 {
		t.Errorf("rewards: %d keys, %d tips in %d slots, %d coins",
			e.count(e.id.key), e.total(e.id.tips), e.count(e.id.tips), e.total(e.id.coins))
	}
	if e.count(unnotedID(t, e.cfg, "Raw bass", false)) != 15 || e.count(unnotedID(t, e.cfg, "Big fishing net", false)) != 1 || e.count(e.id.chisel) != 1 {
		t.Errorf("the bass, the net or the chisel was touched")
	}
	if m := e.maxPerTick(); m > 5 {
		t.Fatalf("a tick handled %d user events: the engine's limit is five", m)
	}
	if len(r.logs) != 1 || !strings.Contains(r.logs[0], "drop cycle (fishing)") {
		t.Fatalf("log %q: want one line saying what the cycle did", r.logs)
	}
	for _, part := range []string{"opened 3 casket(s)", "opened 4 oyster(s) (3 pearl(s), 1 empty shell(s) dropped)", "joined 1 key half pair(s)", "cut 3 oyster pearl(s) into 18 Pearl bolttips", "dropped 4 junk slot(s) (2 raw fish)"} {
		if !strings.Contains(r.logs[0], part) {
			t.Errorf("the log line %q lacks %q", r.logs[0], part)
		}
	}
	// The last casket's reward box is closed, by the second burst's first click.
	e.tick()
	if e.box {
		t.Error("a reward box was left open")
	}
}

// What reaches the Loot panel: the oysters' card shows the pearls they gave and never the Empty oyster
// that was thrown away (a user request: "dropped empty oysters shouldnt be considered in the oyster
// loot table"), the casket card its rewards and no crystal key, the tips go on the Fletching card, and
// no junk is recorded anywhere.
func TestDroppedEmptyOystersAndJunkNeverReachTheLootPanelRealConfig(t *testing.T) {
	r := newCycleRig(t)
	r.fullPack()
	RunCycle(r.d, "fishing")

	oyster, ok := r.dropFor(loot.OysterPearlsName)
	if !ok || oyster.Events != 4 {
		t.Fatalf("the oyster card: %+v (ok %v), want 4 opens", oyster, ok)
	}
	if got := itemNames(oyster); len(got) != 1 || got["Oyster pearl"] != 3 {
		t.Fatalf("the oyster card's items %v, want only the 3 pearls kept, never the Empty oyster", got)
	}
	casket, ok := r.dropFor("Casket")
	if !ok || casket.Events != 3 {
		t.Fatalf("the casket card: %+v (ok %v), want 3 opens", casket, ok)
	}
	got := itemNames(casket)
	if got["Coins"] != 80 || got["Half of a key"] != 2 || got["Crystal key"] != 0 || len(got) != 2 {
		t.Fatalf("the casket card's items %v, want 80 coins and the two halves (the join happens after the count)", got)
	}
	fletch, ok := r.dropFor("Oyster pearl")
	if !ok || fletch.Source.Skill != "Fletching" || fletch.Events != 3 || itemNames(fletch)["Pearl bolttips"] != 18 {
		t.Fatalf("the Fletching card: %+v (ok %v), want 3 pearls cut into 18 tips", fletch, ok)
	}
	for _, dr := range r.drops {
		for _, it := range dr.Items {
			switch it.Name {
			case "Empty oyster", "Raw mackerel", "Raw cod", "Leather boots", "Seaweed":
				t.Errorf("%s was recorded as loot on the %s card", it.Name, dr.Source.Name)
			}
		}
	}
	if len(r.drops) != 3 {
		t.Fatalf("%d drops reported, want the casket, the oyster and the Fletching card: %+v", len(r.drops), r.drops)
	}
}

// A script that banks everything (no Junk) still records the Empty oyster an oyster gave: only what the
// script throws away is kept off the panel, and the opens count either way.
func TestAScriptWithNoJunkStillRecordsTheEmptyOyster(t *testing.T) {
	cfg := realConfig(t)
	id := func(name string) int { return int(unnotedID(t, cfg, name, false)) }
	nameOf := func(i int) (string, bool) {
		o, _ := cfg.Obj(i)
		return o.Name, o.Noted()
	}
	before := loot.Counts{id("Oyster"): 4}
	after := loot.Counts{id("Oyster pearl"): 3, id("Empty oyster"): 1}
	y := measureOpenables(before, after, nameOf, nil)
	if got := y.gained["Oyster"]; got[id("Empty oyster")] != 1 || got[id("Oyster pearl")] != 3 {
		t.Fatalf("with no junk list the oyster rewards are %v, want 3 pearls and the Empty oyster", got)
	}
	y = measureOpenables(before, after, nameOf, fisherplan.IsBassOysterJunk)
	if got := y.gained["Oyster"]; got[id("Empty oyster")] != 0 || got[id("Oyster pearl")] != 3 || y.opened["Oyster"] != 4 {
		t.Fatalf("with the Empty oyster as junk the oyster rewards are %v and %d opens, want 3 pearls and 4 opens", got, y.opened["Oyster"])
	}
}

// Below Fletching 41, or with no chisel, an Oyster pearl is kept as a pearl: nothing is cut, and the
// Empty oyster is dropped all the same.
func TestRunCycleKeepsThePearlsBelow41OrWithoutAChisel(t *testing.T) {
	for name, setup := range map[string]func(r *cycleRig){
		"Fletching 40": func(r *cycleRig) { r.e.setFletching(40) },
		"no chisel": func(r *cycleRig) {
			r.e.pack = [28]slotRow{}
			r.e.fill("Big fishing net", "Oyster", "Oyster", "Oyster", "Oyster")
		},
		"cutting off": func(r *cycleRig) { r.d.Cycle.Opts.CutPearls = false },
	} {
		r := newCycleRig(t)
		r.e.fill("Big fishing net", "Chisel", "Oyster", "Oyster", "Oyster", "Oyster")
		setup(r)
		res := RunCycle(r.d, "fishing")
		if res.Counts.Cuts != 0 || res.Tips != 0 || r.e.count(r.e.id.pearl) != 3 || r.e.count(r.e.id.empty) != 0 || res.Empties != 1 {
			t.Errorf("%s: %+v, %d pearls and %d Empty oysters in the pack, want 3 pearls kept and the shell dropped", name, res, r.e.count(r.e.id.pearl), r.e.count(r.e.id.empty))
		}
	}
}

// With a part switched off the cycle does only the rest: no casket opened and no join with the casket
// part off, the oysters left shut with the oyster part off, and the junk dropped whatever else is off.
func TestRunCycleHonoursEachSetting(t *testing.T) {
	r := newCycleRig(t)
	r.d.Cycle.Opts = fisherplan.CycleOptions{OpenOysters: true}
	r.fullPack()
	res := RunCycle(r.d, "fishing")
	if res.Caskets != 0 || r.e.count(r.e.id.casket) != 3 || res.Counts.Joins != 0 || res.Oysters != 4 || res.Counts.Cuts != 0 {
		t.Fatalf("oysters only: %+v, %d caskets left", res, r.e.count(r.e.id.casket))
	}

	r = newCycleRig(t)
	r.d.Cycle.Opts = fisherplan.CycleOptions{OpenCaskets: true}
	r.fullPack()
	res = RunCycle(r.d, "fishing")
	if res.Oysters != 0 || r.e.count(r.e.id.oyster) != 4 || res.Caskets != 3 || res.Counts.Joins != 1 {
		t.Fatalf("caskets only: %+v, %d oysters left", res, r.e.count(r.e.id.oyster))
	}

	r = newCycleRig(t)
	r.d.Cycle.Opts = fisherplan.CycleOptions{}
	r.fullPack()
	res = RunCycle(r.d, "fishing")
	if res.Counts.Opens != 0 || res.Counts.Drops != 4 || r.e.count(r.e.id.casket) != 3 {
		t.Fatalf("every part off still drops the junk (and only that): %+v", res)
	}
}

// A Deps with no Cycle (ProgressiveFisher) never opens or drops anything and is never due.
func TestNoCycleNeverClicksAnythingRealConfig(t *testing.T) {
	r := newCycleRig(t)
	r.fullPack()
	r.d.Cycle = nil
	if CycleDue(r.d) || CycleWork(r.d) {
		t.Fatal("a Deps with no cycle is due")
	}
	if res := RunCycle(r.d, "fishing"); res.Progress() || r.e.packetsSent() != 0 {
		t.Fatalf("a Deps with no cycle ran: %+v, %d packets", res, r.e.packetsSent())
	}
	if r.d.walkAside() != nil {
		t.Fatal("a Deps with no cycle has side work on a walk")
	}
	r.d.Cycle = &Cycle{Opts: fisherplan.DefaultCycleOptions()} // OnWalks off
	if r.d.walkAside() != nil {
		t.Fatal("a cycle with cycleOnWalks off has side work on a walk")
	}
}

// CycleDue is the nearly-full pack with something to do; work with room to spare waits.
func TestCycleDueNeedsANearlyFullPackRealConfig(t *testing.T) {
	r := newCycleRig(t)
	r.e.fill("Big fishing net", "Raw bass x10", "Oyster", "Raw mackerel")
	if CycleDue(r.d) || !CycleWork(r.d) {
		t.Fatalf("a pack with 15 free slots: due %v, work %v", CycleDue(r.d), CycleWork(r.d))
	}
	r.e.fill("Big fishing net", "Raw bass x24", "Oyster")
	if !CycleDue(r.d) {
		t.Fatalf("two free slots and an oyster: not due (%d free)", FreeSlots(r.d))
	}
	r.e.pack = [28]slotRow{}
	r.e.fill("Big fishing net", "Raw bass x26")
	if CycleDue(r.d) || CycleWork(r.d) {
		t.Fatal("a pack of bass and a net has cycle work")
	}
}

// A bank or shop on screen: the backpack component is not visible and the engine would refuse every
// click, so none is sent.
func TestRunCycleSendsNothingWhileABankIsOpen(t *testing.T) {
	r := newCycleRig(t)
	r.fullPack()
	r.e.st.Handle(serverprot.IF_OPENMAIN_SIDE, []byte{byte(actions.BankMainModal >> 8), byte(actions.BankMainModal & 0xff), 0x23, 0x28})
	if main, _, _ := r.e.st.Modals(); main == gamestate.NoModal {
		t.Fatal("test setup: the bank is not the main modal")
	}
	res := RunCycle(r.d, "at the bank")
	if res.Progress() || res.Reason == "" || r.e.packetsSent() != 0 {
		t.Fatalf("%+v, %d packets sent", res, r.e.packetsSent())
	}
}

// A caller that shares the tick lowers the cycle's budget, and the engine then never reads more than
// that many of the cycle's clicks in a tick.
func TestRunCycleWithALoweredBudgetNeverExceedsIt(t *testing.T) {
	r := newCycleRig(t)
	r.d.Cycle.PerTick = 2
	r.fullPack()
	res := RunCycle(r.d, "fishing")
	if !res.Progress() || res.Counts.Total() != 16 {
		t.Fatalf("%+v", res)
	}
	if m := r.e.maxPerTick(); m > 2 {
		t.Fatalf("a tick handled %d of the cycle's clicks with a budget of 2", m)
	}
}

// A stop request ends the cycle: nothing more is sent and the result says so.
func TestRunCycleStopsWhenTheDelayStops(t *testing.T) {
	r := newCycleRig(t)
	r.fullPack()
	r.d.Delay = func(time.Duration) bool { return false }
	res := RunCycle(r.d, "fishing")
	if !res.Stopped {
		t.Fatalf("%+v: a stop did not stop the cycle", res)
	}
	if r.e.sent[clientprot.OPHELDU] != 0 {
		t.Fatal("the second burst went out after a stop")
	}
}

// FishUntil's cycle: the raw fish it drops leave the pack count and the session's target alike, and a
// cycle that did nothing reports false (the session then stops cycling).
func TestCycleMidSessionCountsTheRawFishItDroppedRealConfig(t *testing.T) {
	r := newCycleRig(t)
	r.fullPack()
	count := CurrentRawFish(r.d) // 15 bass, the mackerel and the cod
	target := count + 5
	if !cycleMidSession(r.d, &count, &target) {
		t.Fatal("a cycle with work reported no progress")
	}
	if r.d.RawDropped != 2 || count != 15 || target != 20 {
		t.Fatalf("raw dropped %d, count %d, target %d: want 2, 15 and 20", r.d.RawDropped, count, target)
	}
	if cycleMidSession(r.d, &count, &target) {
		t.Fatal("a cycle with nothing left to do reported progress")
	}
}

// A walk runs the cycle when the click the walker is following is far enough ahead for the cycle to fit
// inside the walk, or when the pack is nearly full (the cycle is due anyway), and at most once every
// walkCycleEvery. A short hop with room to spare in the pack waits.
func TestWalkCycleRunsOnAHopWithWorkOrANearlyFullPackRealConfig(t *testing.T) {
	r := newCycleRig(t)
	poll := func(ahead int) webwalk.AsidePoll {
		return webwalk.AsidePoll{Me: webwalk.Point{X: 2600, Z: 3416}, Target: webwalk.Point{X: 2600 + ahead, Z: 3416}}
	}
	// One junk slot and plenty of room: a short hop does not spend a cycle on it.
	r.e.fill("Big fishing net", "Raw mackerel", "Raw bass x5")
	walkCycle(r.d, poll(4))
	if r.e.packetsSent() != 0 {
		t.Fatalf("a one-click cycle ran on a 4-tile hop with room to spare (%d packets)", r.e.packetsSent())
	}
	// Real work on the same hop: dropped on the hop, not at the next spot (a user request).
	r.e.fill("Big fishing net", "Oyster", "Oyster", "Casket", "Raw mackerel", "Raw bass x5")
	r.d.Cycle.lastWalkCheck = time.Time{}
	walkCycle(r.d, poll(4))
	if r.e.did["casket"] != 1 || r.e.did["oyster"] != 2 {
		t.Fatalf("no cycle ran on a 4-tile hop with work to do: %v", r.e.did)
	}
	// A nearly full pack: due anyway, so even a hop runs it.
	r.e.pack = [28]slotRow{}
	r.fullPack()
	r.d.Cycle.lastWalkCheck = time.Time{}
	before := r.e.packetsSent()
	walkCycle(r.d, poll(2))
	if r.e.packetsSent() == before || r.e.did["casket"] != 4 {
		t.Fatalf("no cycle ran on a 2-tile hop with a full pack (%d packets, %v)", r.e.packetsSent()-before, r.e.did)
	}
	// Inside the throttle, or with the setting off, nothing runs.
	sent := r.e.packetsSent()
	r.fullPack()
	walkCycle(r.d, poll(40))
	if r.e.packetsSent() != sent {
		t.Fatal("a second cycle ran inside the throttle")
	}
	r.d.Cycle.OnWalks = false
	r.d.Cycle.lastWalkCheck = time.Time{}
	walkCycle(r.d, poll(40))
	if r.e.packetsSent() != sent {
		t.Fatal("a cycle ran on a walk with cycleOnWalks off")
	}
}

// A pack with nothing for the cycle costs a walk poll one read and sends nothing.
func TestWalkCycleWithNothingToDoSendsNothingRealConfig(t *testing.T) {
	r := newCycleRig(t)
	r.e.fill("Big fishing net", "Raw bass x20", "Chisel")
	walkCycle(r.d, webwalk.AsidePoll{Me: webwalk.Point{X: 2600, Z: 3416}, Target: webwalk.Point{X: 2650, Z: 3416}})
	if r.e.packetsSent() != 0 {
		t.Fatalf("%d packets sent for a pack with nothing to tidy", r.e.packetsSent())
	}
}

// The Empty oyster is a thing the script throws away, so the fishing card never records it (it can only
// ever arrive from an open) while the catch it is for still is.
func TestTheEmptyOysterIsNotACatchRealConfig(t *testing.T) {
	cfg := realConfig(t)
	nameOf := func(id int) (string, bool) {
		o, ok := cfg.Obj(id)
		return o.Name, ok
	}
	skip := notACatchNamed(nameOf, fisherplan.IsBassOysterJunk)
	if !skip(int(unnotedID(t, cfg, "Empty oyster", false))) {
		t.Error("an Empty oyster is a catch of the spot")
	}
	for _, name := range []string{"Raw bass", "Oyster", "Casket", "Oyster pearl"} {
		if skip(int(unnotedID(t, cfg, name, false))) {
			t.Errorf("%s is skipped as not a catch", name)
		}
	}
}

// The hand-off's opener (openHeldOpenables: banked caskets and oysters withdrawn and opened before the trade)
// records through the same measure, so a script that throws the Empty oyster away never records it there
// either, and one that banks everything still does.
func TestTheHandoffOpenerRecordsTheSameWayRealConfig(t *testing.T) {
	for _, tc := range []struct {
		name      string
		junk      func(string) bool
		wantEmpty bool
	}{
		{"BassOysterCaskets: the Empty oyster is junk", fisherplan.IsBassOysterJunk, false},
		{"ProgressiveFisher: it banks everything", nil, true},
	} {
		r := newCycleRig(t)
		r.d.Junk = tc.junk
		r.e.fill("Big fishing net", "Oyster", "Oyster", "Oyster", "Oyster")
		if n := openHeldOpenables(r.d); n != 4 {
			t.Fatalf("%s: opened %d, want 4", tc.name, n)
		}
		oyster, ok := r.dropFor(loot.OysterPearlsName)
		if !ok || oyster.Events != 4 {
			t.Fatalf("%s: the oyster card %+v (ok %v)", tc.name, oyster, ok)
		}
		got := itemNames(oyster)
		if got["Oyster pearl"] != 3 {
			t.Errorf("%s: pearls recorded %v, want 3", tc.name, got)
		}
		if recorded := got["Empty oyster"] == 1; recorded != tc.wantEmpty {
			t.Errorf("%s: the Empty oyster was recorded %v, want %v (items %v)", tc.name, recorded, tc.wantEmpty, got)
		}
	}
}

// A walk cycle whose clicks the engine took none of (a stale view: the items are not where they were
// clicked) backs the walk's cycles off, so a long walk does not send the same refused clicks every poll.
func TestAWalkCycleTheEngineRefusedBacksOffRealConfig(t *testing.T) {
	r := newCycleRig(t)
	r.fullPack()
	for slot := range r.e.pack {
		r.e.refuseSlot(slot)
	}
	poll := webwalk.AsidePoll{Me: webwalk.Point{X: 2600, Z: 3416}, Target: webwalk.Point{X: 2640, Z: 3416}}
	walkCycle(r.d, poll)
	first := r.e.packetsSent()
	if first == 0 || r.d.Cycle.walkOffUntil.IsZero() {
		t.Fatalf("the first walk cycle sent %d packets and set no back-off", first)
	}
	r.d.Cycle.lastWalkCheck = time.Time{}
	walkCycle(r.d, poll)
	if r.e.packetsSent() != first {
		t.Fatal("a walk cycle ran again inside the back-off")
	}
	r.d.Cycle.walkOffUntil = time.Time{}
	walkCycle(r.d, poll)
	if r.e.packetsSent() == first {
		t.Fatal("the walk's cycles never came back after the back-off")
	}
}

// A pack whose kept catch fills all but one slot banks rather than dropping one junk slot a roll (live
// 7 Oct 2026: each of those rolls opened content's "You can't carry any more fish." mesbox first).
func TestKeptFullBanksAPackOfKeptCatchRealConfig(t *testing.T) {
	r := newCycleRig(t)
	r.e.fill("Big fishing net", "Raw bass x26", "Raw mackerel")
	if !KeptFull(r.d) {
		t.Fatal("27 kept slots (the net and 26 bass) and one mackerel: want KeptFull")
	}
	r.e.fill("Big fishing net", "Raw bass x25", "Raw mackerel", "Raw mackerel")
	if KeptFull(r.d) {
		t.Fatal("26 kept slots: want room to fish on")
	}
	r.d.Junk = nil
	r.e.fill("Big fishing net", "Raw bass x27")
	if KeptFull(r.d) {
		t.Fatal("no junk list (ProgressiveFisher): KeptFull must stay false")
	}
}
