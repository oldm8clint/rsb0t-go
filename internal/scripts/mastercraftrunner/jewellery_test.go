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

// jewelEngine plays the engine's side of the jewellery round's three interactions TICK BY TICK, as
// engine 289 runs them (World.cycle, NetworkPlayer.decodeIn, Player.delayed), so the round's paces
// and its answers to a dropped packet can be measured:
//
//   - a gold bar used on the furnace (OPLOCU, [oplocu,_smithing_furnace] -> craft_gold_interface)
//     opens the jewellery interface, and its necklaces inventory only when a Necklace mould is in
//     the pack: slot 1 holds the make menu's dummy necklace while a Sapphire is;
//   - Make 1, 5 or 10 (INV_BUTTON1..3 on that inventory) closes it, delays the player two ticks and
//     makes a necklace when the delay ends, again after two more, until the count (or the bars or
//     Sapphires) is used up: one every third tick, the first after the same; a count that stops
//     short of what was asked ends in a message box;
//   - Lvl-1 Enchant (OPHELDT on the spell, component 1155) replaces the necklace in its slot, takes a
//     Water and a Cosmic rune, then delays the player one tick: the next cast is read on the third
//     tick after it. A packet read while the player is delayed is dropped without a word.
//
// Every Delay is one tick; the server's output (modal, inventory, stats, PLAYER_INFO) reaches the
// client at the end of it.
type jewelEngine struct {
	st     *gamestate.State
	chat   *chatlog.Log
	cheats anticheat.Counters

	pack    [28]struct{ id, n int32 }
	ticks   int
	frames  []tFrame
	sent    int
	pending [][]byte

	// delayedUntil is the last tick whose packets are dropped.
	delayedUntil  int
	makeLeft      int
	makeAt        int
	menu, menuInv bool
	menuOut       bool // the interface's open was pushed
	furnace       struct{ x, z, id int }
	reached       bool

	craftXP, magicXP int // displayed points
	craftLevel       int
	magicLevel       int
	members          bool
	staffWater       bool

	crafted, enchanted, dropped, refused, uses, mesboxes int
	madeTicks, castTicks                                 []int
	badBytes                                             int
}

const (
	jeFurnaceX, jeFurnaceZ, jeFurnaceID = 2975, 3368, 2781
	jeGoldNecklace                      = 1654
)

var jeLen = map[int]int{
	clientprot.OPLOCU: 13, clientprot.INV_BUTTON1: 7, clientprot.INV_BUTTON2: 7, clientprot.INV_BUTTON3: 7,
	clientprot.OPHELDT: 9, clientprot.CLOSE_MODAL: 1,
}

func jeStack(id int32) bool {
	switch id {
	case mastercraftplan.RuneCosmicID, mastercraftplan.RuneWaterID, mastercraftplan.CoinsID:
		return true
	}
	return false
}

func newJewelEngine(t *testing.T, pack []struct{ id, n int32 }) (*jewelEngine, *fleetrunner.Deps) {
	t.Helper()
	e := &jewelEngine{st: gamestate.New(), chat: chatlog.New(), craftLevel: 92, magicLevel: 40, members: true, reached: true}
	e.furnace.x, e.furnace.z, e.furnace.id = jeFurnaceX, jeFurnaceZ, jeFurnaceID
	e.cheats.SetEnabled(false) // a frame is then nothing but the round's own packets
	copy(e.pack[:], pack)
	e.st.Handle(serverprot.IF_SETICON, []byte{byte(3213 >> 8), byte(3213 & 0xff), 3})
	e.push()
	comps := archive.Components{
		3213: {ID: 3213, Type: archive.TypeLayer, Children: []int{3214}},
		3214: {ID: 3214, Type: archive.TypeInv, ObjOps: true},
	}
	d := &fleetrunner.Deps{
		Session: e, Components: comps, Chat: e.chat,
		Delay: func(time.Duration) bool { e.tick(); return true },
	}
	return e, d
}

func (e *jewelEngine) State() *gamestate.State     { return e.st }
func (e *jewelEngine) Cheats() *anticheat.Counters { return &e.cheats }

func (e *jewelEngine) Send(build func(w *packet.Packet)) error {
	w := packet.NewWriter(64)
	build(w)
	b := append([]byte(nil), w.Data[:w.Pos]...)
	var pkts [][]byte
	for len(b) > 0 {
		n, ok := jeLen[int(b[0])]
		if !ok || n > len(b) {
			e.badBytes++
			pkts = append(pkts, b)
			break
		}
		pkts = append(pkts, b[:n])
		b = b[n:]
	}
	e.frames = append(e.frames, tFrame{e.ticks + 1, pkts})
	e.sent++
	return nil
}

func (e *jewelEngine) count(id int32) int32 {
	var n int32
	for _, s := range e.pack {
		if s.id == id {
			n += s.n
		}
	}
	return n
}

func (e *jewelEngine) del(id, n int32) {
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

func (e *jewelEngine) add(id, n int32) {
	if jeStack(id) {
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
			if jeStack(id) {
				e.pack[i].n = n
				return
			}
			n--
		}
	}
}

func (e *jewelEngine) say(text string) { e.chat.Record(text) }

func (e *jewelEngine) read(b []byte) bool {
	delayed := e.ticks <= e.delayedUntil
	switch op := int(b[0]); op {
	case clientprot.OPLOCU:
		if delayed {
			e.dropped++
			return false
		}
		x, z, loc, obj, slot, com := tg2(b, 1), tg2(b, 3), tg2(b, 5), int32(tg2(b, 7)), tg2(b, 9), tg2(b, 11)
		if com != 3214 || slot >= 28 || e.pack[slot].id != obj {
			return false
		}
		if x != e.furnace.x || z != e.furnace.z || loc != e.furnace.id { // OpLocUHandler's World.getLoc gate: silent
			return false
		}
		e.uses++
		switch {
		case obj != mastercraftplan.GoldBarID:
			e.say("Nothing interesting happens.")
		case !e.reached:
			e.say("I can't reach that!")
		default:
			e.menu, e.menuInv = true, e.count(mastercraftplan.NecklaceMouldID) > 0
		}
		return true
	case clientprot.INV_BUTTON1, clientprot.INV_BUTTON2, clientprot.INV_BUTTON3:
		if delayed {
			e.dropped++
			return false
		}
		obj, slot, com := int32(tg2(b, 1)), tg2(b, 3), tg2(b, 5)
		if !e.menu || !e.menuInv || com != NecklacesInv || slot != MakeMenuSlot || obj != mastercraftplan.MakeMenuNecklaceID || e.count(mastercraftplan.CutSapphireID) == 0 {
			return false
		}
		e.menu = false // craft_gold: if_close
		if e.craftLevel < mastercraftplan.NecklaceLevel {
			e.say("You need at least level 20 Crafting to make that.")
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
		e.delayedUntil = e.ticks + 1 + 2 // p_delay(2)
		e.makeAt = e.delayedUntil
		return true
	case clientprot.OPHELDT:
		if delayed {
			e.dropped++
			return false
		}
		obj, slot, com, spell := int32(tg2(b, 1)), tg2(b, 3), tg2(b, 5), tg2(b, 7)
		if spell != 1155 || com != 3214 || slot >= 28 || e.pack[slot].id != obj {
			return false
		}
		switch {
		case e.magicLevel < mastercraftplan.EnchantLevel:
			e.say("Your Magic level is not high enough for this spell.")
		case e.count(mastercraftplan.RuneCosmicID) < 1:
			e.say("You do not have enough Cosmic Runes to cast this spell.")
		case e.count(mastercraftplan.RuneWaterID) < 1 && !e.staffWater:
			e.say("You do not have enough Water Runes to cast this spell.")
		case obj != mastercraftplan.SapphireNecklaceID:
			e.say("This spell can only be cast on Sapphire Amulets, Necklaces and Rings.")
		case !e.members:
			e.say("You can only make that on a members' server.")
		default:
			e.pack[slot].id = mastercraftplan.GamesNecklaceID
			e.del(mastercraftplan.RuneCosmicID, 1)
			if !e.staffWater {
				e.del(mastercraftplan.RuneWaterID, 1)
			}
			e.magicXP += 17 // 17.5, floored once per cast here: the tests count casts, not XP
			e.enchanted++
			e.castTicks = append(e.castTicks, e.ticks)
			e.say("You successfully enchant the sapphire necklace.")
			e.delayedUntil = e.ticks + 1 + 1 // p_delay(1)
		}
		return true
	case clientprot.CLOSE_MODAL:
		e.menu = false
		return true
	}
	return false
}

func (e *jewelEngine) tick() {
	e.ticks++
	for len(e.frames) > 0 && e.frames[0].at <= e.ticks {
		e.pending = append(e.pending, e.frames[0].pkts...)
		e.frames = e.frames[1:]
	}
	user, client := 0, 0
	for len(e.pending) > 0 && user < 5 && client < 20 {
		b := e.pending[0]
		e.pending = e.pending[1:]
		if e.read(b) {
			user++
		} else {
			client++
			e.refused++
		}
	}
	// The make's script resumes when its delay is over (processPlayers, after the reads).
	if e.makeLeft > 0 && e.ticks >= e.makeAt {
		e.del(mastercraftplan.GoldBarID, 1)
		e.del(mastercraftplan.CutSapphireID, 1)
		e.add(mastercraftplan.SapphireNecklaceID, 1)
		e.craftXP += 55
		e.crafted++
		e.madeTicks = append(e.madeTicks, e.ticks)
		e.say("You make a sapphire necklace.")
		e.makeLeft--
		if e.makeLeft > 0 {
			if e.count(mastercraftplan.GoldBarID) < 1 || e.count(mastercraftplan.CutSapphireID) < 1 {
				e.mesboxes++ // "You don't have enough gold bars to make any more."
				e.makeLeft = 0
			} else {
				e.delayedUntil = e.ticks + 1 + 2
				e.makeAt = e.delayedUntil
			}
		}
	}
	// Output.
	switch {
	case e.menu && !e.menuOut:
		e.st.Handle(serverprot.IF_OPENMAIN, []byte{byte(JewelleryInterface >> 8), byte(JewelleryInterface & 0xff)})
		e.menuOut = true
	case !e.menu && e.menuOut:
		e.st.Handle(serverprot.IF_CLOSE, nil)
		e.menuOut = false
	}
	e.push()
	e.st.Handle(serverprot.PLAYER_INFO, []byte{0x00, 0x7f, 0xf0})
}

func (e *jewelEngine) writeInv(com int, rows []struct{ id, n int32 }) {
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

func (e *jewelEngine) push() {
	e.writeInv(actions.PlayerPack, e.pack[:])
	if e.menu && e.menuInv {
		menu := make([]struct{ id, n int32 }, 6)
		menu[0] = struct{ id, n int32 }{jeGoldNecklace, 1}
		if e.count(mastercraftplan.CutSapphireID) > 0 {
			menu[1] = struct{ id, n int32 }{mastercraftplan.MakeMenuNecklaceID, 1}
		}
		e.writeInv(NecklacesInv, menu)
	}
	stat := func(id, xp, level int) {
		e.st.Handle(serverprot.UPDATE_STAT, []byte{byte(id), byte(xp >> 24), byte(xp >> 16), byte(xp >> 8), byte(xp), byte(level)})
	}
	stat(CraftingStat, e.craftXP, e.craftLevel)
	stat(MagicStat, e.magicXP, e.magicLevel)
}

type jrow = struct{ id, n int32 }

func jpack(rows ...jrow) []jrow { return rows }

func necklaceLoad(pairs int) []jrow {
	rows := []jrow{{mastercraftplan.NecklaceMouldID, 1}}
	for i := 0; i < pairs; i++ {
		rows = append(rows, jrow{mastercraftplan.GoldBarID, 1})
	}
	for i := 0; i < pairs; i++ {
		rows = append(rows, jrow{mastercraftplan.CutSapphireID, 1})
	}
	return append(rows, jrow{mastercraftplan.RuneCosmicID, 50}, jrow{mastercraftplan.RuneWaterID, 50})
}

func jeFurnace() locindex.Match {
	return locindex.Match{Loc: locindex.Loc{ID: jeFurnaceID, X: jeFurnaceX, Z: jeFurnaceZ}, Op: 2}
}

// MAKE 10 IS ONE USE AND THIRTY TICKS: the click, a two-tick delay, then a necklace every third tick,
// and the run ends silently, with no message box, because the count is used up.
func TestMakeNecklacesTenIsOneUseAndAboutThirtyTicks(t *testing.T) {
	e, d := newJewelEngine(t, necklaceLoad(10))
	r := MakeNecklaces(d, jeFurnace(), nil)
	if r.Reason != "done" || r.Made != 10 || r.Uses != 1 {
		t.Fatalf("%+v", r)
	}
	if e.crafted != 10 || e.mesboxes != 0 || e.count(mastercraftplan.SapphireNecklaceID) != 10 || e.count(mastercraftplan.GoldBarID) != 0 {
		t.Fatalf("crafted %d mesboxes %d pack %v", e.crafted, e.mesboxes, e.pack)
	}
	// Every third tick, from the third after the click.
	for i := 1; i < len(e.madeTicks); i++ {
		if e.madeTicks[i]-e.madeTicks[i-1] != 3 {
			t.Fatalf("necklaces at ticks %v: one every third tick", e.madeTicks)
		}
	}
	if r.XP != 550 || e.refused != 0 {
		t.Errorf("XP %d (want 550 displayed), %d refused packets", r.XP, e.refused)
	}
	if r.Ticks < 30 || r.Ticks > 38 {
		t.Errorf("%d ticks for ten necklaces", r.Ticks)
	}
	if !strings.Contains(r.Describe(), "10 Sapphire necklace(s) from 1 use(s)") {
		t.Errorf("describe: %s", r.Describe())
	}
}

// A load that is not ten is finished by the buttons that make exactly its count, so none of the runs
// ends in a message box: 12 is Make 10, then Make 1 twice; 7 is Make 5, then Make 1 twice; 5 is Make 5.
func TestMakeNecklacesFinishesWithExactCounts(t *testing.T) {
	for pairs, uses := range map[int]int{12: 3, 7: 3, 5: 1, 1: 1, 4: 4, 9: 5, 10: 1} {
		e, d := newJewelEngine(t, necklaceLoad(pairs))
		r := MakeNecklaces(d, jeFurnace(), nil)
		if r.Reason != "done" || r.Made != pairs || r.Uses != uses {
			t.Errorf("%d pairs: %+v, want %d uses", pairs, r, uses)
		}
		if e.mesboxes != 0 {
			t.Errorf("%d pairs: %d message box(es): a count the bars ran short of", pairs, e.mesboxes)
		}
		if e.dropped != 0 {
			t.Errorf("%d pairs: %d packet(s) read while the player was delayed", pairs, e.dropped)
		}
	}
}

// makeButton picks the button that finishes the count.
func TestMakeButton(t *testing.T) {
	for n, want := range map[int][2]int{1: {1, 1}, 4: {1, 1}, 5: {2, 5}, 9: {2, 5}, 10: {3, 10}, 25: {3, 10}} {
		if o, per := makeButton(n); o != want[0] || per != want[1] {
			t.Errorf("makeButton(%d) = %d,%d, want %v", n, o, per, want)
		}
	}
}

// A use read while the player is still delayed is dropped without a word: it is sent again.
func TestMakeNecklacesSendsADroppedUseAgain(t *testing.T) {
	e, d := newJewelEngine(t, necklaceLoad(10))
	e.delayedUntil = 4 // the first use is read inside it
	r := MakeNecklaces(d, jeFurnace(), nil)
	if r.Reason != "done" || r.Made != 10 {
		t.Fatalf("%+v", r)
	}
	if e.dropped < 1 || r.Uses < 2 {
		t.Fatalf("dropped %d uses %d: the first use was read while delayed", e.dropped, r.Uses)
	}
}

func TestMakeNecklacesRefusals(t *testing.T) {
	// No mould: nothing is used on the furnace.
	pack := necklaceLoad(10)[1:]
	e, d := newJewelEngine(t, pack)
	if r := MakeNecklaces(d, jeFurnace(), nil); r.Reason != "no-mould" || r.Uses != 0 || e.uses != 0 {
		t.Errorf("no mould: %+v", r)
	}
	// No bars, no Sapphires: done, nothing sent.
	_, d = newJewelEngine(t, jpack(jrow{mastercraftplan.NecklaceMouldID, 1}))
	if r := MakeNecklaces(d, jeFurnace(), nil); r.Reason != "done" || r.Uses != 0 {
		t.Errorf("nothing to make: %+v", r)
	}
	// The account is not where the engine accepts a use: "I can't reach that!" is heard.
	e, d = newJewelEngine(t, necklaceLoad(10))
	e.reached = false
	if r := MakeNecklaces(d, jeFurnace(), nil); r.Reason != "reach" || r.Made != 0 {
		t.Errorf("not reached: %+v", r)
	}
	// A stop before the first use.
	_, d = newJewelEngine(t, necklaceLoad(10))
	if r := MakeNecklaces(d, jeFurnace(), func() bool { return false }); r.Reason != "held" || r.Uses != 0 {
		t.Errorf("held: %+v", r)
	}
}

func TestCastEnchantEveryThirdTick(t *testing.T) {
	pack := jpack()
	for i := 0; i < 10; i++ {
		pack = append(pack, jrow{mastercraftplan.SapphireNecklaceID, 1})
	}
	pack = append(pack, jrow{mastercraftplan.RuneCosmicID, 50}, jrow{mastercraftplan.RuneWaterID, 50})
	e, d := newJewelEngine(t, pack)
	r := CastEnchant(d, nil)
	if r.Reason != "done" || r.Casts != 10 || r.Sent != 10 {
		t.Fatalf("%+v", r)
	}
	if e.count(mastercraftplan.GamesNecklaceID) != 10 || e.count(mastercraftplan.SapphireNecklaceID) != 0 ||
		e.count(mastercraftplan.RuneCosmicID) != 40 || e.count(mastercraftplan.RuneWaterID) != 40 {
		t.Fatalf("pack %v", e.pack)
	}
	for i := 1; i < len(e.castTicks); i++ {
		if got := e.castTicks[i] - e.castTicks[i-1]; got != mastercraftplan.EnchantTicks {
			t.Fatalf("casts at ticks %v: one every %d ticks, got %d", e.castTicks, mastercraftplan.EnchantTicks, got)
		}
	}
	if e.dropped != 0 || e.refused != 0 {
		t.Errorf("%d dropped, %d refused: no cast may be read while the player is delayed", e.dropped, e.refused)
	}
	if !strings.Contains(r.Describe(), "10 Games necklace(s) from 10 cast(s)") {
		t.Errorf("describe: %s", r.Describe())
	}
}

// A cast read while the player is delayed (the account has just come from a delaying action) is
// dropped and sent again, and the gap is widened for the rest of the run.
func TestCastEnchantSendsADroppedCastAgain(t *testing.T) {
	pack := jpack(jrow{mastercraftplan.SapphireNecklaceID, 1}, jrow{mastercraftplan.SapphireNecklaceID, 1}, jrow{mastercraftplan.SapphireNecklaceID, 1},
		jrow{mastercraftplan.RuneCosmicID, 9}, jrow{mastercraftplan.RuneWaterID, 9})
	e, d := newJewelEngine(t, pack)
	e.delayedUntil = 3
	r := CastEnchant(d, nil)
	if r.Reason != "done" || r.Casts != 3 || e.dropped < 1 || r.Sent < 4 {
		t.Fatalf("%+v dropped %d", r, e.dropped)
	}
}

func TestCastEnchantRefusals(t *testing.T) {
	necklace := jrow{mastercraftplan.SapphireNecklaceID, 1}
	for name, tc := range map[string]struct {
		pack  []jrow
		setup func(e *jewelEngine)
		want  string
	}{
		"no Cosmic":          {jpack(necklace, jrow{mastercraftplan.RuneWaterID, 5}), nil, "runes"},
		"no Water":           {jpack(necklace, jrow{mastercraftplan.RuneCosmicID, 5}), nil, "runes"},
		"low Magic":          {jpack(necklace, jrow{mastercraftplan.RuneCosmicID, 5}, jrow{mastercraftplan.RuneWaterID, 5}), func(e *jewelEngine) { e.magicLevel = 6 }, "level"},
		"a free world":       {jpack(necklace, jrow{mastercraftplan.RuneCosmicID, 5}, jrow{mastercraftplan.RuneWaterID, 5}), func(e *jewelEngine) { e.members = false }, "members"},
		"a different item":   {jpack(jrow{mastercraftplan.GoldBarID, 1}, jrow{mastercraftplan.RuneCosmicID, 5}, jrow{mastercraftplan.RuneWaterID, 5}), nil, "done"},
		"nothing to enchant": {jpack(jrow{mastercraftplan.RuneCosmicID, 5}), nil, "done"},
	} {
		e, d := newJewelEngine(t, tc.pack)
		if tc.setup != nil {
			tc.setup(e)
		}
		if r := CastEnchant(d, nil); r.Reason != tc.want || r.Casts != 0 {
			t.Errorf("%s: %+v, want %s", name, r, tc.want)
		}
	}
	// A Staff of water pays the Water rune.
	e, d := newJewelEngine(t, jpack(necklace, jrow{mastercraftplan.RuneCosmicID, 5}))
	e.staffWater = true
	if r := CastEnchant(d, nil); r.Reason != "done" || r.Casts != 1 {
		t.Errorf("a Staff of water: %+v", r)
	}
	// A hold that says stop.
	_, d = newJewelEngine(t, jpack(necklace, jrow{mastercraftplan.RuneCosmicID, 5}, jrow{mastercraftplan.RuneWaterID, 5}))
	if r := CastEnchant(d, func() bool { return false }); r.Reason != "held" || r.Sent != 0 {
		t.Errorf("held: %+v", r)
	}
}

// What the pack readers say.
func TestJewelleryPackReaders(t *testing.T) {
	cfg := jewelCfg(t)
	pack := necklaceLoad(3)
	pack = append(pack, jrow{mastercraftplan.GamesNecklaceID, 1}, jrow{mastercraftplan.SapphireNecklaceID, 1}, jrow{mastercraftplan.CoinsID, 90},
		jrow{mastercraftplan.GoldBarID + 1, 40}, // the noted gold bars: neither an item of the round nor a rune
		jrow{563, 4}, jrow{1333, 1})             // a Law rune, and a rune scimitar: only the second is "other"
	_, d := newJewelEngine(t, pack)
	d.Config = cfg
	h := JewelleryPack(d)
	want := mastercraftplan.JewelleryHeld{Bars: 3, Sapphires: 3, Necklaces: 1, Games: 1, Mould: 1, Cosmic: 50, Water: 50, Coins: 90}
	if h != want {
		t.Errorf("pack %+v, want %+v", h, want)
	}
	if got := JewelleryPackOther(d); got != 2 {
		t.Errorf("%d other rows, want 2 (the notes and the scimitar)", got)
	}
}

func jewelCfg(t *testing.T) *cachecfg.Config {
	t.Helper()
	path := os.Getenv("RSB0T_CONFIG_JAG")
	if path == "" {
		t.Skip("set RSB0T_CONFIG_JAG to the real object config")
	}
	cfg, err := cachecfg.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// ---- the kit, over the bank simulator with the real items ----

func jkObjs() actionsim.Objs {
	notes := map[int32]int32{}
	noteOf := map[int32]int32{}
	for _, id := range []int32{mastercraftplan.GoldBarID, mastercraftplan.CutSapphireID, mastercraftplan.NecklaceMouldID, mastercraftplan.SapphireNecklaceID, mastercraftplan.GamesNecklaceID} {
		notes[id+1], noteOf[id] = id, id+1
	}
	return actionsim.Objs{
		Stackable: func(id int32) bool { return jeStack(id) || id == 563 || id == 554 || id == 556 },
		NoteOf:    func(id int32) int32 { return noteOf[id] },
		UnnoteOf:  func(id int32) int32 { return notes[id] },
	}
}

func jkVisit(t *testing.T, pack, bank []actionsim.Stack, plan func([]gamestate.SlotItem) []mastercraftplan.JewelleryDraw) (*actionsim.Session, JewelleryKitResult) {
	t.Helper()
	sim := actionsim.New(jkObjs(), pack)
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
	res := JewelleryKit(d, b, plan)
	if res.Err != "" {
		t.Fatalf("the visit failed: %s", res.Err)
	}
	return sim, res
}

// A kit visit banks the whole pack, the mule's notes included (a bank takes a note back as the item),
// and draws the load exactly: the mould, ten gold bars and ten Sapphires, the runes of ten enchants.
func TestJewelleryKitBanksThePackAndDrawsTheLoad(t *testing.T) {
	const (
		bar, sapph, mould, neck, games = mastercraftplan.GoldBarID, mastercraftplan.CutSapphireID, mastercraftplan.NecklaceMouldID, mastercraftplan.SapphireNecklaceID, mastercraftplan.GamesNecklaceID
		cosmic, water                  = mastercraftplan.RuneCosmicID, mastercraftplan.RuneWaterID
	)
	pack := []actionsim.Stack{
		{ID: bar + 1, Count: 50}, {ID: sapph + 1, Count: 50}, {ID: cosmic, Count: 50}, {ID: water, Count: 51}, // the mule's answer
		{ID: games, Count: 1}, {ID: games, Count: 1}, {ID: mould, Count: 1},
	}
	bank := []actionsim.Stack{{ID: bar, Count: 7}, {ID: sapph, Count: 7}, {ID: 563, Count: 5}, {ID: 995, Count: 5000}}
	var seen mastercraftplan.JewelleryHeld
	sim, res := jkVisit(t, pack, bank, func(banked []gamestate.SlotItem) []mastercraftplan.JewelleryDraw {
		seen = JewelleryOwn(banked)
		draws := mastercraftplan.PlanJewelleryDraws(mastercraftplan.JewelleryVisit{
			Bank: seen, Left: 50, CraftOK: true, EnchantOK: true, ShopOK: true,
			BankOf: func(item string) int {
				if item == mastercraftplan.RuneLaw {
					return 5
				}
				return 0
			},
			Travel: map[string]int{mastercraftplan.RuneLaw: 1},
		})
		for i := range draws {
			if draws[i].Name == mastercraftplan.RuneLaw {
				draws[i].ID = 563 // the caller resolves a rune known by name
			}
		}
		return draws
	})
	// What the plan saw: the deposit had made the notes items.
	if seen.Bars != 57 || seen.Sapphires != 57 || seen.Mould != 1 || seen.Games != 2 || seen.Necklaces != 0 || seen.Cosmic != 50 || seen.Water != 51 {
		t.Fatalf("the bank after the deposit: %+v", seen)
	}
	got := sim.Pack()
	want := map[int32]int{mould: 1, bar: 10, sapph: 10, cosmic: 10, water: 10, 563: 1}
	for id, n := range want {
		if c := ckCount(got, id); c != n {
			t.Errorf("pack holds %d of item %d, want %d", c, id, n)
		}
	}
	for _, id := range []int32{bar + 1, sapph + 1, games, neck, 995} {
		if c := ckCount(got, id); c != 0 {
			t.Errorf("item %d is still in the pack: %d", id, c)
		}
	}
	rows := 0
	for _, r := range got {
		if r.ID != 0 {
			rows++
		}
	}
	if rows != 1+10+10+3 {
		t.Errorf("the pack holds %d rows: %v", rows, got)
	}
	if res.Drew[mastercraftplan.GoldBar] != 10 || res.Drew[mastercraftplan.CutSapphire] != 10 || res.Drew[mastercraftplan.NecklaceMould] != 1 ||
		res.Drew[mastercraftplan.RuneCosmic] != 10 || res.Drew[mastercraftplan.RuneWater] != 10 {
		t.Errorf("drew %v", res.Drew)
	}
	// The bank keeps the rest, the Games necklaces included.
	b := sim.Bank()
	if ckCount(b, bar) != 47 || ckCount(b, games) != 2 || ckCount(b, cosmic) != 40 || ckCount(b, 563) != 4 || ckCount(b, 995) != 5000 {
		t.Errorf("bank %v", b)
	}
}

// A visit that plans nothing only banks, and the bank is read after the deposit.
func TestJewelleryKitWithNothingToDrawOnlyBanks(t *testing.T) {
	pack := []actionsim.Stack{{ID: mastercraftplan.GamesNecklaceID, Count: 1}, {ID: mastercraftplan.NecklaceMouldID, Count: 1}, {ID: mastercraftplan.RuneCosmicID, Count: 9}}
	sim, res := jkVisit(t, pack, nil, func(banked []gamestate.SlotItem) []mastercraftplan.JewelleryDraw { return nil })
	if len(sim.Pack()) != 0 && ckCount(sim.Pack(), mastercraftplan.GamesNecklaceID) != 0 {
		t.Errorf("pack %v", sim.Pack())
	}
	if h := JewelleryOwn(res.Banked); h.Games != 1 || h.Mould != 1 || h.Cosmic != 9 {
		t.Errorf("the bank after the deposit: %+v", h)
	}
}

// ---- the furnace, over the real loc index and collision pack ----

func TestTheFaladorFurnaceIsFoundByNameAndOption(t *testing.T) {
	locsPath, cfgPath, packPath := os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG"), os.Getenv("RSB0T_COLLISION_PACK")
	if locsPath == "" || cfgPath == "" || packPath == "" {
		t.Skip("set RSB0T_LOC_INDEX, RSB0T_CONFIG_JAG and RSB0T_COLLISION_PACK to check the real furnace")
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
	_, d := newJewelEngine(t, nil)
	d.Locs, d.Config = ix, cfg
	// Through the loc index by name and option, never by an id at a coordinate.
	bank := mastercraftplan.JewelleryWorkBank
	furn, ok := FindFurnace(d, bank, mastercraftplan.JewelleryFurnaceRadius)
	if !ok || furn.X != jeFurnaceX || furn.Z != jeFurnaceZ || furn.ID != jeFurnaceID || furn.Op != 2 || !strings.EqualFold(furn.Name, FurnaceName) {
		t.Fatalf("the furnace near the Falador west bank: %+v %v", furn, ok)
	}
	// Nothing within the radius of somewhere with no furnace.
	if m, ok := FindFurnace(d, tile.Tile{X: 2809, Z: 3441}, 45); ok {
		t.Errorf("a furnace at the mule's post: %+v", m)
	}
	// The tiles a use is accepted from: the west side, nearest the bank first, none of them the furnace's own.
	stands := FurnaceStands(d, furn, bank)
	if len(stands) == 0 || stands[0].X != 2974 {
		t.Fatalf("stands %v", stands)
	}
	for _, s := range stands {
		if s.X >= 2975 && s.X <= 2977 && s.Z >= 3368 && s.Z <= 3370 {
			t.Errorf("stand %v is inside the furnace", s)
		}
	}
	// The collision pack has a route from the bank to the first of them, and the walk comes there.
	res := pk.FindPath(pathfind.NavPoint{X: bank.X, Z: bank.Z}, pathfind.NavPoint{X: stands[0].X, Z: stands[0].Z})
	if !res.OK {
		t.Fatalf("no route from the bank to %v: %s", stands[0], res.Reason)
	}
	var walked []tile.Tile
	if !ApproachFurnace(d, furn, func(dest tile.Tile, radius int, timeout time.Duration) bool {
		walked = append(walked, dest)
		return true
	}, time.Minute) {
		t.Fatal("the approach failed")
	}
	if len(walked) != 1 || walked[0] != stands[0] {
		t.Errorf("walked %v, want the nearest stand %v once", walked, stands[0])
	}
}

// The craft and enchant lines say what was made: rings for a ring batch (they said Sapphire necklaces and Games
// necklaces for every batch before 7 Oct 2026).
func TestResultDescribesNameTheKind(t *testing.T) {
	n := NecklaceResult{Made: 10, Uses: 1, XP: 550, Ticks: 30, Reason: "done"}
	if got := n.Describe(); !strings.Contains(got, "10 Sapphire necklace(s)") {
		t.Errorf("necklaces: %s", got)
	}
	n.Kind = mastercraftplan.KindRing
	if got := n.Describe(); !strings.Contains(got, "10 Emerald ring(s)") || strings.Contains(got, "necklace") {
		t.Errorf("rings: %s", got)
	}
	e := EnchantResult{Casts: 10, Sent: 10, XP: 370, Ticks: 30, Reason: "done"}
	if got := e.Describe(); !strings.Contains(got, "10 Games necklace(s)") {
		t.Errorf("necklaces: %s", got)
	}
	e.Kind = mastercraftplan.KindRing
	if got := e.Describe(); !strings.Contains(got, "10 Ring of dueling(s)") || strings.Contains(got, "necklace") {
		t.Errorf("rings: %s", got)
	}
}
